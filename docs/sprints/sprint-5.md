# Sprint 5 — Integration

> Phase 5 of [SCOPE.md §19](../SCOPE.md#phase-5--integration-wk-1112) · 2 weeks (10 working days) · Exit: two features validated and promoted with evidence; automatic revert demonstrated in auto mode.

---

## 1. Goal

Until now, agents could publish changes and sync each other's work, but nothing
ever reached `develop` or `main`. This sprint builds the **integration engine**:
the part of COWFlow that takes a set of published changes, proves they work
*together* on top of the current target branch, and only then moves the target
branch forward — atomically, with evidence, and with the lead's approval.

By the end of the sprint a lead (human via CLI, or agent via MCP) can:

1. Group two features (`auth` and `api`) into an integration group targeting `develop`.
2. Have the engine run cheap **diff-only gates** first (no workspace), then build a
   **candidate workspace**, apply the changes in a deterministic order, and run
   **workspace gates** (build, test, lint, `cmd` invariants) through the same
   resource queues agents use.
3. Review the result **by intent** (`integration show`), not line by line.
4. Promote with a **compare-and-swap** on `target_sha`; if `develop` moved in the
   meantime, the group goes `stale` and is revalidated automatically.
5. In `promotion_mode = "auto"` with post-promotion enabled, see a post-promotion
   failure produce an automatic revert commit, cross-requests to the tasks
   involved, and a lead notification.
6. Under the `gitflow` preset, see a hotfix promoted to `main` automatically
   create an integration group back to `develop`.

Principle reminder (§2.2): *published ≠ integrated ≠ approved* (7), *no change,
no integration — the server refuses* (3), *human in the loop by default* (12),
*promotion via compare-and-swap on `target_sha`* (13).

---

## 2. Exit criteria

- [ ] **§19 Phase 5 output:** two features validated and promoted with evidence  
  *Verified by:* Demo §8.3: `integration show <gid> --json` lists both members, every gate result with duration/exit code, `target_sha`, `candidate_sha`, `promoted_sha`; `git rev-parse develop` equals `promoted_sha`. Evidence attached to the sprint-closing PR.
- [ ] **§19 Phase 5 output:** automatic revert demonstrated in auto mode  
  *Verified by:* Demo §8.6: `integration.promoted` followed by `integration.reverted`; `develop` tip is a revert commit authored by the engine; one cross-request open per task in the group; lead notified.
- [ ] **§20.4** (this sprint's part) integration refused without change  
  *Verified by:* Demo §8.2 + test `TestCreateRefusedWithoutChange`: `cowflow integration create docs --target develop` exits `4`; no row in `integration_groups`; no event emitted.
- [ ] **§20.9** published / validated / promoted are distinct  
  *Verified by:* Demo §8.3: `cowflow change list --json` shows `published` → `validated` (after `validate`) → `promoted` (after `promote`) as separate states. Test `TestChangeStatesDistinct`.
- [ ] **§20.10** group with two features, diff-only gates before workspace, manual promotion  
  *Verified by:* Demo §8.3: event order `integration.started` → diff-only results recorded before any candidate workspace exists (`workspace list` empty for the group); `cowflow_promote` via MCP returns `pending_human_approval`; human `integration promote` promotes. Test `TestDiffOnlyFailureCreatesNoWorkspace`.
- [ ] **§20.11** target advanced → revalidation  
  *Verified by:* Demo §8.4 + harness test `TestStaleRevalidates` (§18 row "Target advances"): CAS fails, `integration.stale`, new attempt on the new `target_sha`, gates rerun.
- [ ] **§20.24** post-promotion in auto mode performs a revert and opens requests on failure  
  *Verified by:* Demo §8.6 + harness test `TestPostPromotionRevert` (§18 row "Post-promotion fails").
- [ ] **§20.31** (this sprint's part) hotfix promoted to `main` creates an automatic integration group for `develop`  
  *Verified by:* Demo §8.5 + test `TestHotfixBackMergeGroup`: after promotion to `main`, a new group with `target = develop` exists, created by the engine, lead notified.
- [ ] Gates share the resource queues (§9.8)  
  *Verified by:* Test `TestGatesUseCompileQueue`: with `compile.slots = 1` held by an agent, the build gate waits (`resource.queued`) and runs after `resource.granted`.

All of the above run green with `go test -race ./...` on Windows, Linux and macOS CI.

---

## 3. Prerequisites

### 3.1 Earlier epics that must be merged

| Epic | Why this sprint needs it |
|---|---|
| `S0-E1`, `S0-E2` | Cobra tree stubs for `integration …`, exit codes; `[integration]`, `[integration.post_promotion]`, `[[gates]]`, `[[invariants]]`, `[conventions]` parsed and validated. |
| `S0-E3` | SQLite, migrations, tables `integration_groups`, `integration_group_members`, `integration_attempts` (§13). |
| `S0-E4` | Safe git runner (`-C`, `GIT_TERMINAL_PROMPT=0`, timeout), `check-ref-format`. |
| `S0-E5` | Process-tree kill — gate timeouts must kill `go test` and its children. |
| `S0-E8` | Hermetic harness: fake clock, git fault injection, crash points. |
| `S1-E1`, `S1-E2`, `S1-E4` | `WorkspaceProvider`, `git_worktree` fallback, `cow_clone` + golden trees (candidate = clone of the golden at `target_sha`). |
| `S1-E6` | Destroy/retention — failed candidates kept `keep_failed_hours`. |
| `S2-E1`, `S2-E2` | Domain tables (tasks, intents, changes, requests); event store + transactional outbox. |
| `S2-E3`, `S2-E4` | Intents (review by intent, stub check); cross-requests (`fix_incompat`, post-promotion requests). |
| `S2-E5`, `S2-E6` | Changes, manifests, retention refs `refs/cowflow/changes/<id>`; derived index (overlap). |
| `S2-E7` | Sync machinery — `AG-GIT` reuses its merge/conflict code for applying changes in the candidate. |
| `S2-E8`, `S2-E9` | Contracts + `contract_refs`; `pattern` invariant evaluator (reused as a diff-only gate). |
| `S3-E1`, `S3-E2`, `S3-E3`, `S3-E6` | Lead MCP tool set, scoped tokens, `init --conventions gitflow` (task kinds), server-side enforcement. |
| `S4-E1`, `S4-E2`, `S4-E3` | Daemon (the engine is daemon-only, §5.3), loopback API, SSE. |
| `S4-E5`, `S4-E6` | Scheduler limits and resource leases — gates go through the same queue (§9.8). |
| `S4-E8` | Reconciliation after restart — extended here for in-flight attempts/promotions. |

### 3.2 Spec gaps affecting this sprint

IDs are the stable ones from `docs/SPEC-GAPS.md`.
**None of these is decided here.** `AG-LEAD` takes them to the maintainer on
Day 1; blocked work proceeds behind the interfaces in §6.

| Ref | SPEC-GAPS entry | What is blocked until decided |
|---|---|---|
| `C1` | Go version 1.23+ vs `go.mod` 1.25 | Nothing; code targets `go.mod` (1.25). |
| `C2` | Task branch naming `cowflow/<project>/<task-slug>` vs `<kind>/<slug>` | Nothing blocked, but S5-T17 must detect hotfix/release by **task kind**, never by parsing branch names. |
| `C3` | `promotion_mode` `manual\|auto` vs also `pr`; forge config undefined | S5-T16 `pr` path (only the seam ships). |
| `C5` | `max_concurrent_per_target` vs `max_integrations_per_target` | S5-T11 (`integrate:<target>` resource), S5-T16 (what the per-target serializer limits: validations or only the ref). |
| `C6` | Invariant example import path vs module path | Demo config (§8) uses a real pattern; no code blocked. |
| `C12` | Never-commit lists vs `blocked_patterns` | S5-T10 manifest-lint rules (with U-GIT-11 below). |
| `U-DATA-5` | *Data model and events:* "States of integration groups …" | S5-T01 public state names, S5-T02 transitions, S5-T19 output. Work uses internal constants behind `domain.GroupState`; only event names (§9.9) are public. |
| `U-GIT-5` | *Git and changes:* "How changes are applied in the integration candidate; whether `promoted_sha == candidate_sha`" | S5-T08 apply strategy, S5-T09, S5-T16 (fast-forward vs new commit), S5-T20 (revert shape). |
| `U-GIT-6` | *Git and changes:* "`pattern` invariants in diff-only gates: whole blob vs added lines" | S5-T10 pattern gate scope (reuse whatever `S2-E9` uses; do not fork semantics). |
| `U-GIT-7` | *Git and changes:* "Behavior when `rerun_on_stale = false`" | S5-T14 `false` branch. |
| `U-GIT-8` | *Git and changes:* "Gate → resource mapping (e.g. build → `compile`)" | S5-T11 mapper, S5-T12, S5-T13. |
| `U-SCHED-2` | *Scheduler:* "`acquire(wait=true, timeout)` timing out while queued" | S5-T12 gate behavior when a lease wait times out. |
| `U-SCHED-3` | *Scheduler:* "Precedence between `max_builds`/`max_tests` and `[[resources]]` slots" | S5-T11 slot count used by gates (consume scheduler's answer, never recompute). |
| `U-DATA-2` | *Data model and events:* "Prefixes for prefixed ULIDs" | S5-T01 group/attempt ID prefix (use whatever `S0-E3` settled). |
| `U-API-4` | *Daemon, API, CLI, MCP:* "HTTP status ↔ CLI exit code mapping" | S5-T03/S5-T04 error mapping (reuse the `S4-E2` table). |
| `U-GIT-4` | *Git and changes:* "Retention cleanup for promoted/superseded refs" | Nothing deleted this sprint: the engine never removes `refs/cowflow/changes/*`. |
| `U-API-11` | *Daemon, API, CLI, MCP:* "Which §22 values are defaults vs examples" | Defaults for `keep_failed_hours`, `rerun_on_stale`, `diff_only_first`, `post_promotion.gates` — read from config, never hardcode. |
| `U-PLAT-1` | *Workspaces and platform:* "How a `cow_clone` tree gets its own HEAD and index" | S5-T07 inherits the `S1-E4` answer. |

**Found while planning** (already in SPEC-GAPS.md; `AG-LEAD` asks the maintainer on Day 1):

| Ref | Question | Blocks |
|---|---|---|
| `U-GIT-11` | Diff-only gate semantics: what "manifest lint" checks (§24.10 size, §24.13 never-commit paths, `blocked_patterns`, see `C12`), and whether overlap / manifest-lint findings **fail** the group or only **warn** (§2.2 principle 2 says overlap never blocks). | S5-T10 `Required()` of the overlap and manifest-lint gates. |
| `U-SCHED-11` | Lease holder identity for engine gates: `resource_leases.session_id` / `task_id` when the holder is the engine, not an agent session (§9.8, §13). | S5-T11. |
| `U-GIT-12` | Manual approval: which actor counts as the human approval (§5.4 shows the human approving via CLI); semantics of `cowflow_approve_promotion` vs `cowflow_promote` (§15.1); whether an approval survives a stale revalidation. | S5-T05, S5-T14, S5-T16. |
| `U-GIT-13` | Release flow: how the lead confirms the tag version; annotated/signed tag (§24.2). | S5-T17 release branch (hotfix path is not blocked). |
| `U-GIT-14` | Post-promotion: behavior with `auto_revert = false`; change/task states after a revert; `kind`/`expected_output` of the cross-requests opened (§10.2); whether `required = false` gates trigger a revert. | S5-T21 those branches. |
| `U-GIT-15` | Candidate workspace identity: branch vs detached `HEAD` (no workspace on `main`/`develop`, §24.2), `.env.cowflow`/ports, sparse off, row in `workspaces`. | S5-T07. |
| `U-GIT-16` | Failure attribution: which task is "flagged by the gate" (§24.12) for an apply conflict between two members, or a repo-wide build/test failure. | S5-T09/S5-T12 `fix_incompat` target. |
| `U-GIT-17` | Meaning of `require_validation = false` and `diff_only_first = false` (§22). | S5-T16, S5-T10 (`false` branches refuse with a "not decided" diagnostic until answered — `AG-LEAD` confirms this stopgap on Day 1). |

---

## 4. Concepts you need

**Integration group.** A set of tasks/changes plus a `target` branch, created by the
lead (§10.1 step 1). Members are each task's latest `published` change — never a
`superseded` one, never a task with an active stub (§24.12). The group is the
*only* path into `develop`/`main` (§24.2). One group may have many **attempts**
(first validation, revalidations after `stale`); each attempt pins a `target_sha`.
See [§10.1](../SCOPE.md#101-flow), [§24.12](../SCOPE.md#2412-integration-groups).

**Candidate workspace.** A temporary workspace created as a `cow_clone` of the golden
tree at `target_sha` (§10.1 step 4). The engine applies the members' changes into
it; the resulting commit is the `candidate_sha` — the exact tree that will become the
target if promoted. It is not an agent workspace: no agent ever writes there. On
failure it is kept `keep_failed_hours` so a human can inspect it (`integration open`).
See [§6.3](../SCOPE.md#63-backends), [§10.1](../SCOPE.md#101-flow).

**Deterministic ordering.** Changes are applied in topological order of
`task_dependencies` (if `api` depends on `auth`, `auth` goes first); ties are broken
by publishing order (§10.1 step 5). Same inputs → same order → same `candidate_sha`
inputs. This matters because conflict reports and gate failures must be
reproducible, and revalidation must not reshuffle. Algorithm: Kahn's topological
sort with a sorted "ready" set. See [§10.1](../SCOPE.md#101-flow).

**Gates: diff-only vs workspace.** A gate is a check that can pass or fail.
*Diff-only* gates read only stored data — manifests (`change_files`), the derived
index, contracts, blob contents — and need no workspace: `pattern` invariants,
touched contracts, overlap between members, manifest lint. They run **first**
because they are cheap: a failure stops the group before any disk or CPU is spent
(§10.1 step 3, §23 decision 12). *Workspace* gates run commands in the candidate:
`[[gates]]` (`build`, `test`, `lint`) and `cmd` invariants (§10.1 step 6).
See [§10.1](../SCOPE.md#101-flow), [§22](../SCOPE.md#22-configuration).

**Invariants: `pattern` vs `cmd`.** Project rules written once in TOML (§9.6).
`kind = "pattern"` has `deny` globs matched against file content — evaluated
diff-only. `kind = "cmd"` runs an argv command (`cmd = ["go", "run", …]`); non-zero
exit = violation — evaluated in the candidate. Both "run as a gate in every
integration group"; a violation emits `invariant.violated` with a diagnostic.
`S2-E9` already evaluates `pattern` invariants at publish; this sprint reuses that
evaluator. See [§9.6](../SCOPE.md#96-invariants).

**Stale detection.** Validation proves "these changes work on top of `target_sha`".
If someone else promotes first, `develop` moves and the proof is about an old base.
The engine detects this at promotion (the CAS fails, or the pinned `target_sha` no
longer matches), emits `integration.stale`, rebuilds the candidate on the new
`target_sha` and reruns gates (`rerun_on_stale`). Stale is never the agent's fault;
agents are only called if reapplying fails (§24.12).
See [§10.1](../SCOPE.md#101-flow) step 9, [§18](../SCOPE.md#18-resilience).

**Compare-and-swap ref updates.** `git update-ref <ref> <new> <old>` moves `<ref>`
to `<new>` **only if** it currently points to `<old>`; otherwise it fails and
changes nothing. Git does this under its own ref lock, so two promoters cannot both
win. COWFlow's rule "promotion = CAS on `target_sha` under a short lock" (§8.1,
§2.2.13) maps to:
`git -C <repo> update-ref -m "cowflow: promote <gid>" refs/heads/develop <promoted_sha> <target_sha>`.
Always pass full ref names and full 40-hex SHAs.
See [git-update-ref](https://git-scm.com/docs/git-update-ref), [§8.1](../SCOPE.md#81-rules).

**Promotion modes.** `promotion_mode = "manual"` (default, §23 decision 6): the
engine validates, the lead/human promotes; an MCP `cowflow_promote` returns
`pending_human_approval` and does not promote (§15.1). `"auto"`: the engine
promotes right after validation passes (§10.3). `"pr"`: when a remote and forge are
configured, promotion opens a PR instead of moving the ref locally (§24.2) — forge
config is undefined (`C3`). See [§10.3](../SCOPE.md#103-policies).

**Post-promotion and revert.** With `promotion_mode = "auto"` and
`[integration.post_promotion] enabled = true`, after promoting the engine runs
`post_promotion.gates` on the target branch. If they fail and `auto_revert = true`:
an automatic revert commit is promoted (again by CAS), `integration.reverted` is
emitted, cross-requests go to the tasks involved, and the lead is notified (§10.2,
§18). It is the "test later, fix fast" model for high throughput.
See [§10.2](../SCOPE.md#102-post-promotion-optional).

**Review by intent.** The lead does not read diffs first. `integration show <gid>`
presents, per task: the intent (goal, scope, stubs), manifest stat, touched
contracts, gate results and invariants, plus warnings (manifest > 20 files or > 800
lines, `feat` without a new test, session without `report_result` → `incomplete`).
Lines appear only with `--diff` (§9.4, §24.4, §24.10, §24.13).
See [§9.4](../SCOPE.md#94-lead).

**Gitflow preset effects.** With `[conventions] preset = "gitflow"`: features go to
`develop` only via a group; promoting a hotfix to `main` automatically creates a
`hotfix → develop` group and notifies the lead; promoting `release/*` to `main`
creates a tag per `tag_format` (lead confirms the version) and a return group to
`develop`; only the engine creates tags (§24.2). `trunk` and `none` presets change
this (§24 footer). See [§24.2](../SCOPE.md#242-how-cowflow-applies-them).

---

## 5. Agents and lanes

| Agent | Tasks |
|---|---|
| `AG-LEAD` | Day 1 kickoff (gaps, contract freeze), all reviews, demo co-owner |
| `AG-INT` (lane a — engine core) | S5-T02, S5-T09, S5-T14, S5-T16, S5-T21 |
| `AG-INT` (lane b — gates & review) | S5-T06, S5-T10, S5-T12, S5-T13, S5-T17, S5-T18 |
| `AG-STORE` | S5-T01 |
| `AG-GIT` | S5-T08, S5-T15, S5-T20 |
| `AG-WS` | S5-T07 |
| `AG-SCHED` | S5-T11 |
| `AG-API` | S5-T03 |
| `AG-CLI` | S5-T04, S5-T19 |
| `AG-MCP` | S5-T05 |
| `AG-QA` | S5-T22, S5-T23 |
| `AG-SEC` | Reviews every `Security: yes` task |

Guidance: `AG-INT` carries 11 tasks; run it as two agents on disjoint files
(lane a: `engine.go`, `candidate.go`, `stale.go`, `promotion.go`,
`postpromotion.go`; lane b: `order.go`, `gates.go`, `diffonly.go`,
`wsgates.go`, `invariants.go`, `gitflow.go`, `review.go`).

```text
Day          1     2     3     4     5     6     7     8     9     10
AG-LEAD    [KICK][---------------- reviews / gap follow-up ----------][DEMO]
AG-STORE   [T01======]
AG-INT a         [T02=======]     [T09=======]  [T14=][T16=======][T21==]
AG-INT b   [T06=][T10===========][T12========][T13=][T17======][T18=]
AG-WS      [T07==========]
AG-GIT     [T08==========][T15========]  [T20==========]
AG-SCHED   [T11==========]
AG-API           [T03==========]
AG-CLI                 [T04==========]        [T19==========]
AG-MCP                       [T05=====]
AG-QA      [fixtures/harness seams][T22 ========================][T23]
AG-SEC           .... reviews T03 T05 T07 T08 T09 T12 T13 T15 T16 T17 T20 T21 ....
```

**Critical path:** S5-T01 → S5-T02 → S5-T09 (needs S5-T06, S5-T07, S5-T08) →
S5-T12 (needs S5-T10, S5-T11) → S5-T16 (needs S5-T15) → S5-T21 (needs S5-T20) →
S5-T23. Any slip on S5-T08 (apply, blocked by `U-GIT-5`) or S5-T15 (CAS) moves the demo.
Mitigation: lane a codes against the fakes in §6 from Day 2.

---

## 6. Contracts

Frozen by `AG-LEAD` on Day 1. Field shapes are Guidance unless the name comes
from the spec (spec names are in backticks in comments). Changes after Day 1 need
an `AG-LEAD`-approved PR that updates this section.

### 6.1 Domain records — provider `AG-STORE` (S5-T01), consumers `AG-INT`, `AG-API`, `AG-CLI`, `AG-MCP`

```go
// internal/domain/integration.go
package domain

// GroupState values are blocked by U-DATA-5. Keep them internal: CLI/API show them,
// but tests assert on events (§9.9), not on these strings.
type GroupState string

type IntegrationGroup struct {
	ID            string // prefixed ULID (prefix per S0-E3 / U-DATA-2)
	ProjectID     string
	Target        string // branch name, e.g. "develop" (§10.1 `target`)
	State         GroupState
	CreatedBy     Actor
	OriginGroupID string // set for gitflow back-merge groups (§24.2)
	CreatedAt     time.Time
}

type IntegrationMember struct {
	GroupID     string
	TaskID      string
	ChangeID    string
	HeadSHA     string    // pinned (§10.1 step 2, `head_sha`)
	PublishedAt time.Time // ordering tie-break (§10.1 step 5)
	OrderIndex  int       // filled by the engine after ordering
}

type IntegrationAttempt struct {
	ID           string
	GroupID      string
	TargetSHA    string // `target_sha`, pinned per attempt
	CandidateSHA string // `candidate_sha`
	PromotedSHA  string // `promoted_sha`; may differ from CandidateSHA (U-GIT-5)
	WorkspaceID  string // candidate workspace, "" until created
	Stage        string // last stage reached: "diff_only" | "apply" | "workspace" | "promotion" | "post_promotion" (Guidance)
	Outcome      string // "running" | "passed" | "failed" | "stale" | "promoted" | "reverted" (Guidance, see U-DATA-5)
	Results      []GateResult
	OpID         string
	StartedAt    time.Time
	FinishedAt   time.Time
}

type Actor struct {
	Kind      string // "human_cli" | "lead_mcp" | "engine" (Guidance; see U-GIT-12)
	SessionID string // empty for human_cli and engine
}
```

### 6.2 Engine — provider `AG-INT`, consumers `AG-API`, `AG-MCP` (via API), `AG-CLI` (via API), daemon

```go
// internal/integration/engine.go
package integration

type Engine interface {
	Create(ctx context.Context, req CreateRequest) (domain.IntegrationGroup, error)
	Validate(ctx context.Context, req ValidateRequest) (domain.IntegrationAttempt, error) // returns the started attempt; runs async
	Promote(ctx context.Context, req PromoteRequest) (PromoteResult, error)
	Cancel(ctx context.Context, req CancelRequest) error
	Review(ctx context.Context, groupID string, opts ReviewOptions) (Review, error)
	Candidate(ctx context.Context, groupID string) (CandidateInfo, error) // for `integration open`
}

type CreateRequest struct {
	OpID      string
	ProjectID string
	Target    string
	TaskIDs   []string // CLI: `integration create auth api --target develop`
	ChangeIDs []string // optional explicit changes (§10.1 "tasks/changes")
	By        domain.Actor
}
type ValidateRequest struct{ OpID, GroupID string; By domain.Actor }
type PromoteRequest  struct{ OpID, GroupID string; By domain.Actor }
type CancelRequest   struct{ OpID, GroupID string; By domain.Actor }

type PromoteStatus string

const (
	PromotePendingHumanApproval PromoteStatus = "pending_human_approval" // spec §15.1
	PromotePromoted             PromoteStatus = "promoted"
	PromoteStaleRevalidating    PromoteStatus = "stale" // Guidance
)

type PromoteResult struct {
	Status      PromoteStatus
	AttemptID   string
	PromotedSHA string
}

// Sentinel errors; AG-API/AG-CLI map them to exit codes (§14) via the S4-E2 table (U-API-4).
var (
	ErrNoChange          = errors.New("integration refused: task has no published change") // exit 4
	ErrSupersededChange  = errors.New("integration refused: change is superseded")         // exit 4
	ErrActiveStub        = errors.New("integration refused: task has an active stub")      // exit 4
	ErrTargetNotAllowed  = errors.New("integration refused: target not allowed by preset")  // exit 4
	ErrNotValidated      = errors.New("group has no passed attempt")                        // exit 2
	ErrRequiresLead      = errors.New("promotion requires the lead")                        // exit 4
	ErrDaemonRequired    = errors.New("integration engine requires the daemon")             // exit 2
	ErrApplyConflict     = errors.New("conflict applying change in candidate")              // exit 3
	ErrUndecided         = errors.New("behavior not decided; see SPEC-GAPS")                // exit 4
)
```

### 6.3 Gates — provider `AG-INT` (S5-T10, S5-T12, S5-T13); consumers: engine, review

```go
// internal/integration/gates.go
type Stage string

const (
	StageDiffOnly      Stage = "diff_only"
	StageWorkspace     Stage = "workspace"
	StagePostPromotion Stage = "post_promotion"
)

type Gate interface {
	Name() string
	Stage() Stage
	Required() bool
	Run(ctx context.Context, in GateInput) domain.GateResult
}

type GateInput struct {
	Group     domain.IntegrationGroup
	Members   []domain.IntegrationMember // already ordered
	Manifests map[string]domain.Manifest // by change ID; diff-only gates use only this + Blobs + Index
	Blobs     BlobReader                 // git cat-file --batch, no workspace
	Index     IndexReader                // path_index / contract_refs
	Dir       string                     // candidate dir; "" for diff-only gates (assert it!)
	TargetSHA string
	CandidateSHA string
}

// internal/domain/integration.go (AG-STORE)
type GateResult struct {
	Name         string
	Stage        string
	Required     bool
	Passed       bool
	Files        []string // files involved (§10.1 step 7)
	FlaggedTasks []string // for fix_incompat (§24.12; attribution U-GIT-16)
	ExitCode     int
	Duration     time.Duration
	Output       string // tail, bounded (Guidance: 64 KiB)
	Diagnostic   string
	LeaseID      string // evidence that the gate went through the queue (§9.8)
	ResourceWait time.Duration
}
```

### 6.4 Candidate builder — `AG-WS` + `AG-GIT` primitives, composed by `AG-INT`

```go
// internal/workspace (AG-WS, S5-T07)
type CandidateSpec struct {
	ProjectID, GroupID, AttemptID string
	BaseSHA string // = target_sha; detached HEAD vs branch is U-GIT-15
}
type CandidateManager interface {
	CreateCandidate(ctx context.Context, spec CandidateSpec) (*Workspace, error) // cow_clone of golden at BaseSHA; auto → git_worktree fallback
	ReleaseCandidate(ctx context.Context, workspaceID string, keepUntil time.Time) error // keep_failed_hours or destroy
}

// internal/git (AG-GIT, S5-T08)
type ApplyResult struct{ HeadSHA string }
type ConflictError struct{ ChangeID string; Files []string } // errors.As target; Unwrap() → integration.ErrApplyConflict is mapped by AG-INT
type Applier interface {
	ApplyChange(ctx context.Context, dir, retentionRef, headSHA string) (ApplyResult, error) // method blocked by U-GIT-5
	AbortApply(ctx context.Context, dir string) error
}

// internal/integration/candidate.go (AG-INT, S5-T09)
type Candidate struct {
	WorkspaceID, Dir, TargetSHA, CandidateSHA string
}
type CandidateBuilder interface {
	Build(ctx context.Context, a domain.IntegrationAttempt, ordered []domain.IntegrationMember) (Candidate, error)
}
```

### 6.5 Promotion primitives — provider `AG-GIT` (S5-T15, S5-T20), consumer `AG-INT`

```go
// internal/git/refs.go
var ErrRefMoved = errors.New("ref does not point to expected old value")

type RefUpdater interface {
	// git -C <repo> update-ref -m <msg> <ref> <newSHA> <oldSHA>
	UpdateRefCAS(ctx context.Context, repo, ref, newSHA, oldSHA, msg string) error
	ResolveRef(ctx context.Context, repo, ref string) (string, error) // rev-parse --verify <ref>^{commit}
	CreateTag(ctx context.Context, repo, name, sha, msg string) error  // engine-only (§24.2); U-GIT-13 for release
}

// internal/git/revert.go
type Reverter interface {
	// Creates, in dir (a temporary workspace at promotedSHA), a commit that undoes
	// promotedSHA relative to previousSHA. Shape depends on U-GIT-5.
	RevertPromotion(ctx context.Context, dir, previousSHA, promotedSHA, msg string) (revertSHA string, err error)
}
```

### 6.6 Leases — provider `AG-SCHED` (S5-T11, on top of `S4-E6`), consumer `AG-INT`

```go
// internal/scheduler/gates.go
type GateHolder struct{ ProjectID, GroupID, AttemptID string } // identity mapping is U-SCHED-11

type GateLeaser interface {
	ResourceFor(gateName string) (resource string, ok bool) // U-GIT-8: config-driven, no hardcoded table
	Acquire(ctx context.Context, h GateHolder, resource string, timeout time.Duration) (leaseID string, err error) // waits in the SAME queue as agents
	Release(ctx context.Context, h GateHolder, leaseID string) error
	TargetSlot(ctx context.Context, h GateHolder, target string) (release func(), err error) // `integrate:<target>`; scope per C5
}
```

### 6.7 Collaboration — provider `AG-COLLAB` (existing `S2-E4`, `S2-E9`), consumer `AG-INT`

```go
// already exists or is added by AG-COLLAB on request:
type Requests interface {
	Open(ctx context.Context, tx *store.Tx, r collab.NewRequest) (string, error) // kind `fix_incompat`, expected_output `commit`
}
type PatternInvariants interface {
	Check(ctx context.Context, m domain.Manifest, blobs collab.BlobReader) ([]collab.Violation, error) // same evaluator as publish (U-GIT-6)
}
type LeadNotifier interface {
	NotifyLead(ctx context.Context, tx *store.Tx, n collab.LeadNotice) error // path used by escalation (S2-E4/S4-E10)
}
```

### 6.8 Events emitted (all via `tx.Emit` in the same transaction as the state change, §13)

| Event | When | Payload (Guidance) |
|---|---|---|
| `integration.created` | group row inserted (incl. gitflow back-merge groups) | `group_id`, `target`, `members[]`, `origin_group_id` |
| `integration.started` | an attempt begins | `group_id`, `attempt_id`, `target_sha`, `head_shas{}` |
| `integration.failed` | any gate/apply failure | `stage`, `target_sha`, `candidate_sha`, `files[]`, `gate`, `flagged_tasks[]`, `workspace_id` (§10.1 step 7) |
| `integration.passed` | all required gates passed | `attempt_id`, `candidate_sha`, gate summary |
| `integration.stale` | target moved at promotion | `old_target_sha`, `new_target_sha` |
| `integration.promoted` | CAS succeeded and recorded | `promoted_sha`, `target_sha`, evidence ref |
| `integration.reverted` | post-promotion revert promoted | `promoted_sha`, `revert_sha`, failing gate |
| `invariant.violated` | `pattern` or `cmd` invariant fails | `invariant`, `stage`, `files[]`, `diagnostic` |

`resource.*` are emitted by `AG-SCHED`; `cross_request.*` by `AG-COLLAB` (concrete
names per SPEC-GAPS `U-DATA-4`).

### 6.9 Fakes (in `internal/integration/integrationtest/`, owned by `AG-INT`)

`FakeApplier` (scripted conflicts per change ID), `FakeRefUpdater` (in-memory refs
with CAS semantics and an injectable "someone moved it" hook), `FakeCandidateManager`
(temp dirs), `FakeGateLeaser` (counts concurrent holders), `FakeRequests`,
`FakeLeadNotifier`, `FakeClock` (from `test/harness`). Lane a uses them from Day 2.

---

## 7. Tasks

### S5-T01 — Integration domain records and store methods

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-STORE` | `AG-LEAD`, `AG-GIT` | `S5-E1` | `S0-E3`, `S2-E1`, `S2-E2` | `feat/store-integration-groups` | no |

**Skills:** `store-and-events`, `integration-engine`, `go-conventions`, `gitflow`   **Spec:** §10.1, §13, §9.9, §18
**Files:** `internal/domain/integration.go`, `internal/store/migrations/NNNN_integration.sql`, `internal/store/integration.go`, `internal/store/integration_test.go`

**Why / what** — Everything the engine does must survive a daemon restart (§18) and
emit events atomically with state (§13). This task gives the engine its tables and
typed methods so no other package writes SQL.

**Steps**
1. Check what `S0-E3` already created for `integration_groups`, `integration_group_members`, `integration_attempts`. Add a **new** migration for missing columns; never edit an applied one.
2. Guidance schema (column names are not in the spec; `state`/`outcome` values blocked by `U-DATA-5`):
   ```sql
   CREATE TABLE IF NOT EXISTS integration_groups (
     id TEXT PRIMARY KEY, project_id TEXT NOT NULL, target TEXT NOT NULL,
     state TEXT NOT NULL, created_by_kind TEXT NOT NULL, created_by_session TEXT,
     origin_group_id TEXT REFERENCES integration_groups(id),
     created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
   CREATE TABLE IF NOT EXISTS integration_group_members (
     group_id TEXT NOT NULL REFERENCES integration_groups(id), task_id TEXT NOT NULL,
     change_id TEXT NOT NULL REFERENCES changes(id), head_sha TEXT NOT NULL,
     published_at TEXT NOT NULL, order_index INTEGER, PRIMARY KEY (group_id, task_id));
   CREATE TABLE IF NOT EXISTS integration_attempts (
     id TEXT PRIMARY KEY, group_id TEXT NOT NULL REFERENCES integration_groups(id),
     target_sha TEXT NOT NULL, candidate_sha TEXT, promoted_sha TEXT, workspace_id TEXT,
     stage TEXT NOT NULL, outcome TEXT NOT NULL, results_json TEXT NOT NULL DEFAULT '[]',
     op_id TEXT UNIQUE, started_at TEXT NOT NULL, finished_at TEXT);
   ```
3. Add the types from §6.1 to `internal/domain` (pure, no I/O).
4. Add `Tx` methods: `InsertIntegrationGroup`, `InsertIntegrationMembers`, `SetMemberOrder`, `InsertAttempt`, `UpdateAttempt`, `SetGroupState`, `SetChangeState(changeID, domain.ChangeState)` (`validated`, `promoted`, §8.5), `SetTaskState(taskID, "done")` (§24.12), and reads `GetGroup`, `ListMembers`, `LatestAttempt`, `AttemptsByOutcome(outcome)` (for reconciliation).
5. Every mutating method is called inside `WithTx(ctx, opID, fn)`; callers emit events with `tx.Emit` in the same transaction.
6. `go test -race ./internal/store/...`; commit `feat(store): add integration group tables and methods` with `git commit -S -s`.

**Tests** — table-driven round trips; `op_id` replay returns the stored attempt without a second row; event + state rollback together when `fn` returns an error; migration applies on an `S4`-era database fixture.

**Acceptance**
- [ ] New migration only; `go test -race ./internal/store/...` green on 3 OSes.
- [ ] State + event atomicity test present.
- [ ] No public state string documented as final (comment references `U-DATA-5`).

**Pitfalls** — holding a tx across a Git call (never); storing gate output unbounded in `results_json` (engine truncates, store enforces a cap too); forgetting an index on `integration_group_members(change_id)` used by retention checks (`U-GIT-4`).

---

### S5-T02 — Engine core: admission, create, cancel, attempt lifecycle

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-INT` | `AG-LEAD`, `AG-COLLAB` | `S5-E1` | S5-T01 | `feat/integration-engine-core` | no |

**Skills:** `integration-engine`, `collaboration-protocol`, `store-and-events`, `go-conventions`   **Spec:** §2.2 (3, 7, 12), §5.3, §10.1 steps 1–2, §15.5, §20.4, §24.2, §24.6, §24.12
**Files:** `internal/integration/engine.go`, `internal/integration/admission.go`, `internal/integration/reconcile.go`, `*_test.go`, `internal/integration/integrationtest/fakes.go`

**Why / what** — The engine is the server-side gatekeeper: "no change, no integration"
must be enforced here, not in CLI or MCP, so every front end gets the same refusal
(§15.5). This task implements `Create`, `Cancel`, the attempt runner skeleton
(`Validate` dispatches stages implemented by later tasks), and restart reconciliation.

**Steps**
1. Define the `Engine` interface, request types and sentinel errors exactly as §6.2.
2. `Create` admission, in this order (each refusal → typed error, no row, no event):
   1. Resolve each task; for each, pick the latest change whose state is not `superseded`/`discarded`/`promoted` (§24.12). None → `ErrNoChange` (§20.4). An explicitly passed change that is `superseded` → `ErrSupersededChange`.
   2. Task intent has an active stub (`stub: true`) → `ErrActiveStub` (§24.6).
   3. Target exists (`RefUpdater.ResolveRef("refs/heads/<target>")`) and passes `check-ref-format`.
   4. Preset rules (§24.2, `gitflow`): `feature`/`bugfix`/`chore` tasks → target `develop` only; `hotfix` → `main`; `release` → `main`. Violation → `ErrTargetNotAllowed`. Use task **kind**, not branch name (`C2`).
   5. In one tx: insert group + members (pin `head_sha` from the change row, §10.1 step 2), emit `integration.created`, write `audit_log`.
3. `Validate`: refuse if a non-terminal attempt exists; in one tx insert an attempt with `target_sha = ResolveRef(target)` and emit `integration.started`; then run stages in a goroutine owned by the engine's `errgroup` (bounded by a semaphore; Guidance: size from `max_integrations_per_target` resolved by `AG-SCHED`, `C5`).
   ```go
   func (e *engine) runAttempt(ctx context.Context, a domain.IntegrationAttempt) {
       ordered, err := e.order(ctx, a.GroupID)            // S5-T06
       if err != nil { e.fail(ctx, a, "order", err); return }
       if !e.runDiffOnly(ctx, a, ordered) { return }      // S5-T10, before any workspace
       cand, err := e.builder.Build(ctx, a, ordered)       // S5-T09
       if err != nil { e.fail(ctx, a, "apply", err); return }
       if !e.runWorkspaceGates(ctx, a, cand) { return }    // S5-T12, S5-T13
       e.pass(ctx, a, cand)                                // changes → validated, integration.passed
   }
   ```
4. `pass`: one tx — attempt outcome, `candidate_sha`, every member change → `validated` (§8.5), emit `integration.passed`. In `auto` mode, enqueue promotion (S5-T16).
5. `fail`: one tx — outcome, stage, emit `integration.failed` with stage, SHAs, files; open `fix_incompat` via `Requests.Open` to `FlaggedTasks` (§24.12; attribution `U-GIT-16` — until decided, flag the member whose change was being applied, or none for repo-wide gates, and let the lead route). Changes stay `published`.
6. `Cancel`: allowed for non-promoted groups; cancels the running attempt's context; candidate kept per `keep_failed_hours` (Guidance); emits no new event type (none listed in §9.9) — record in `audit_log`.
7. Daemon-only (§5.3): the CLI/API return `ErrDaemonRequired` in daemonless mode.
8. `reconcile.go`: on daemon start, attempts in `running` → mark failed with stage `daemon_restart` and let the lead revalidate (Guidance); attempts recorded as "promoting" → handled by S5-T16.

**Tests** — `TestCreateRefusedWithoutChange` (§20.4), superseded change, active stub, gitflow target refusal, idempotent `op_id`, `Validate` twice refused, cancellation stops a fake long gate, outbox events present per transition.

**Acceptance**
- [ ] Refusals happen in the engine, before any write; exit `4` mapping documented.
- [ ] Every state change + event in one tx; audit rows for create/cancel.
- [ ] No goroutine leak (`goleak`-style check via `t.Cleanup` waiting on the errgroup; no new dependency).

**Pitfalls** — choosing "latest change" by ULID order (concurrent writers are not ordered — use `published_at` + change state); emitting events after commit; treating overlap as an admission refusal (principle 2).

---

### S5-T03 — API endpoints `/v1/integration-groups`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-API` | `AG-LEAD`, `AG-CLI`, `AG-SEC` | `S5-E1` | S5-T02, `S4-E2` | `feat/api-integration-groups` | yes |

**Skills:** `local-api-daemon`, `security-review`, `integration-engine`   **Spec:** §12, §12.5, §14 (exit codes), §15.1, §17
**Files:** `internal/api/integration.go`, `internal/api/integration_test.go`

**Why / what** — The engine only lives in the daemon, so CLI and MCP reach it through
these endpoints (§12.5). The API is also where authorization happens.

**Steps**
1. Register exactly: `POST /v1/integration-groups`, `GET /v1/integration-groups/{id}`, `GET /v1/integration-groups/{id}/review`, `POST /v1/integration-groups/{id}/validate`, `POST /v1/integration-groups/{id}/promote`, `POST /v1/integration-groups/{id}/cancel`.
2. Authorization (deny by default): lead token (project scope, §15.1) for every mutating call; Guidance: workers get 403 on all of them (integration tools are lead-only in §15.1). The human CLI uses the lead token issued by `init`; set `Actor{Kind: "human_cli"}` only for CLI-originated calls and `lead_mcp` for MCP — how the API tells them apart is part of `U-GIT-12`; until decided, carry an explicit `actor` field the API validates against the token type.
3. `http.MaxBytesReader` 1 MiB, strict JSON (`DisallowUnknownFields`), validate IDs and `target` (`check-ref-format` happens in the engine too).
4. Map sentinel errors to HTTP status via the `S4-E2` table (`U-API-4`); include a stable machine `code` (e.g. `no_change`, `pending_human_approval`).
5. `promote` response for manual MCP calls: `{"status":"pending_human_approval"}` (§15.1).
6. Add a fuzz target for the create/promote payloads (§18.1).

**Tests** — 401/403 matrix (no token, worker token, lead token); payload > 1 MiB → 413; unknown field rejected; error→status table; fuzz seeds.

**Acceptance**
- [ ] All six routes; lead-only mutations; `AG-SEC` checklist ticked.
- [ ] No token or path leaks in error bodies.

**Pitfalls** — trusting a `group_id` from a worker; leaking candidate workspace absolute paths to non-lead callers.

---

### S5-T04 — CLI `integration create|validate|promote|cancel|open` and config checks

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-CLI` | `AG-LEAD`, `AG-API` | `S5-E1` | S5-T03 | `feat/cli-integration-commands` | no |

**Skills:** `cli-and-config`, `integration-engine`   **Spec:** §5.4, §14, §22 (`[integration]`, `[[gates]]`)
**Files:** `internal/cli/integration.go`, `internal/config/validate.go`, `config.example.toml`, tests

**Why / what** — The human lead drives integration from the terminal (§5.4). Commands
are thin: parse, call API, render, exit with the right code.

**Steps**
1. Commands exactly per §14: `cowflow integration create <tasks...> --target <branch>`; `cowflow integration validate|promote|cancel <group-id>`; `cowflow integration open <group-id>`. (`show` is S5-T19.) `--json` on all.
2. `validate`: Guidance — after the API accepts, follow `/v1/events/stream` filtered by `group_id` until `integration.passed|failed`, printing each gate result; exit `0` on passed, `2` on failed (state), `3` on apply conflict.
3. `promote` from the CLI is the human approval path (§5.4); print `promoted_sha` or the stale/revalidation progress.
4. `open`: get `CandidateInfo` and write a `.code-workspace` JSON (`{"folders":[{"path":"<candidate dir>"}]}`) (§5.4). Guidance: write under the state dir, print the path; confine paths (§17).
5. Config validation (Guidance, all `errors.Join`): every `post_promotion.gates` entry names an existing `[[gates]].name`; gate/invariant `cmd` non-empty argv; `promotion_mode` ∈ `manual|auto|pr` (`C3`: keep `pr` accepted or rejected exactly as `S0-E2` did); unique gate names. Mirror §22 in `config.example.toml`.

**Tests** — command tree golden list includes all six subcommands; exit codes `0/1/2/3/4`; `--json` single document; `.code-workspace` content; config validation table.

**Acceptance**
- [ ] Commands match §14 verbatim; no extra flags invented.
- [ ] Daemonless invocation prints a clear "requires the daemon" error (exit `2`) or triggers on-demand start per `S4-E1`.

**Pitfalls** — implementing logic in the CLI (admission belongs to S5-T02); printing human text to stdout in `--json` mode.

---

### S5-T05 — Wire lead MCP tools to the engine

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-MCP` | `AG-LEAD`, `AG-API`, `AG-SEC` | `S5-E1` | S5-T03, `S3-E1` | `feat/mcp-integration-tools` | yes |

**Skills:** `mcp-and-runners`, `security-review`, `integration-engine`   **Spec:** §5.4, §15.1, §15.5, §20.27
**Files:** `internal/mcp/lead_integration.go`, tests

**Why / what** — The lead agent in VS Code creates and validates groups via MCP only
(§20.27). `S3-E1` registered the tools; now they call the real engine.

**Steps**
1. `cowflow_create_integration(tasks, target)`, `cowflow_validate(group)`, `cowflow_promote(group)`, `cowflow_approve_promotion(group)` → API calls with `Actor{Kind:"lead_mcp"}`.
2. `cowflow_promote` with `promotion_mode = "manual"` returns `pending_human_approval` and does not promote (§15.1).
3. `cowflow_approve_promotion`: blocked by `U-GIT-12`. Until decided, return `ErrUndecided` with a diagnostic pointing to SPEC-GAPS; do **not** promote.
4. `cowflow_wait_events` already filters by type; add `integration.*` filter tests.

**Tests** — role matrix (worker/watchdog cannot see the tools); manual-mode promote returns `pending_human_approval`; refusal codes propagate.

**Acceptance**
- [ ] Lead-only; enforcement stays in the engine/API (§15.5).
- [ ] `U-GIT-12` stopgap documented in the tool description.

**Pitfalls** — letting `cowflow_promote` move the ref in manual mode; accepting a `workspace_id` or session in tool input.

---

### S5-T06 — Deterministic application order

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-INT` | `AG-LEAD`, `AG-SCHED` | `S5-E2` | contracts (Day 1) | `feat/integration-order` | no |

**Skills:** `integration-engine`, `go-conventions`   **Spec:** §10.1 step 5, §24.7
**Files:** `internal/integration/order.go`, `internal/integration/order_test.go`

**Why / what** — Applying `api` before `auth` may conflict or fail to build even though
the pair is fine. A deterministic order makes failures reproducible and revalidation
stable.

**Steps**
1. Input: members + `task_dependencies` edges restricted to tasks **inside** the group. Edges to tasks outside the group are ignored for ordering and surfaced as a review warning (Guidance; §24.12 asks for cohesive groups but defines no refusal).
2. Kahn's algorithm with a ready set sorted by `(PublishedAt, ChangeID)`:
   ```go
   func Order(ms []domain.IntegrationMember, deps map[string][]string) ([]domain.IntegrationMember, error) {
       byTask := map[string]domain.IntegrationMember{}
       indeg := map[string]int{}
       for _, m := range ms { byTask[m.TaskID] = m; indeg[m.TaskID] = 0 }
       next := map[string][]string{}
       for task, ds := range deps {
           if _, in := byTask[task]; !in { continue }
           for _, d := range ds {
               if _, in := byTask[d]; in { next[d] = append(next[d], task); indeg[task]++ }
           }
       }
       less := func(a, b string) bool {
           ma, mb := byTask[a], byTask[b]
           if !ma.PublishedAt.Equal(mb.PublishedAt) { return ma.PublishedAt.Before(mb.PublishedAt) }
           return ma.ChangeID < mb.ChangeID // final, deterministic tie-break (Guidance)
       }
       var ready, out []string
       for t, n := range indeg { if n == 0 { ready = append(ready, t) } }
       for len(ready) > 0 {
           slices.SortFunc(ready, func(a, b string) int { if less(a, b) { return -1 }; if less(b, a) { return 1 }; return 0 })
           t := ready[0]; ready = ready[1:]; out = append(out, t)
           for _, n := range next[t] { if indeg[n]--; indeg[n] == 0 { ready = append(ready, n) } }
       }
       if len(out) != len(ms) { return nil, ErrDependencyCycle }
       res := make([]domain.IntegrationMember, len(out))
       for i, t := range out { m := byTask[t]; m.OrderIndex = i; res[i] = m }
       return res, nil
   }
   ```
3. Cycle → `ErrDependencyCycle` (Guidance: §24.7 rejects cycles at creation, but never trust it).
4. Persist `order_index` via `SetMemberOrder` in the attempt tx.

**Tests** — chain, diamond, independent tasks ordered by publish time, equal timestamps, cycle, out-of-group dependency; property test: shuffled input → identical output.

**Acceptance**
- [ ] Pure function, no I/O, 100% branch coverage.
- [ ] Same input in any order → same output.

**Pitfalls** — iterating Go maps without sorting (random order); using change ULID as the primary key (concurrent writers are not time-ordered).

---

### S5-T07 — Candidate workspace

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-WS` | `AG-LEAD`, `AG-PLAT`, `AG-SEC` | `S5-E2` | `S1-E4`, `S1-E6` | `feat/workspace-candidate` | yes |

**Skills:** `cow-workspaces`, `windows-platform`, `security-review`   **Spec:** §6.1, §6.3, §6.4, §10.1 steps 4 & 7, §18, §22 (`keep_failed_workspaces`, `keep_failed_hours`), §24.2
**Files:** `internal/workspace/candidate.go`, `internal/workspace/candidate_test.go`

**Why / what** — The engine needs a throwaway tree at exactly `target_sha`, created in
seconds by cloning the golden tree, never touching agent workspaces.

**Steps**
1. Implement `CandidateManager` (§6.4). Reuse `golden.go` get-or-build at `BaseSHA`, then the provider chosen by `provider = "auto"` (`cow_clone` → `git_worktree`), honoring `pending_mount` (never fall back to NTFS silently, §18).
2. HEAD/branch: blocked by `U-GIT-15`. The workspace must not be on `main`/`develop` (§24.2). Implement behind an option; Guidance candidate for the maintainer: detached `HEAD` at `BaseSHA` (`git worktree add --detach`), which adds no ref.
3. No sparse checkout for candidates (gates build the whole tree). Guidance; confirm under `U-GIT-15`.
4. Record the workspace row with a purpose marker so `workspace list` can show it and reconciliation can find it.
5. `ReleaseCandidate(id, keepUntil)`: failed → keep until `now + keep_failed_hours` when `keep_failed_workspaces = true`; otherwise destroy. Stuck handles → `pending_cleanup` (§18).
6. Confine the path under `workspace.root`; sanitize `GroupID`/`AttemptID` for the directory name.

**Tests** — create at a given SHA → `git rev-parse HEAD` equals it; writes in candidate do not alter the golden or an agent workspace (§6.1); keep/destroy with fake clock; simulated VHDX unmounted → `pending_mount`.

**Acceptance**
- [ ] No partial `ready` on failure; compensation removes the tree.
- [ ] `U-GIT-15` stopgap behind an option, documented.

**Pitfalls** — reusing an agent's golden-derived workspace; copying `.git` from the golden (§6.1); destroying a failed candidate before `keep_failed_hours`.

---

### S5-T08 — Apply a change into the candidate

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-GIT` | `AG-LEAD`, `AG-STORE`, `AG-SEC` | `S5-E2` | `S2-E7` | `feat/git-apply-change` | yes |

**Skills:** `git-changes-sync`, `security-review`   **Spec:** §8.1, §8.4, §10.1 steps 5 & 7, §18
**Files:** `internal/git/apply.go`, `internal/git/apply_test.go`

**Why / what** — The engine needs one primitive: "apply the retention ref of this change
on the candidate's current HEAD; report conflict files if it fails". **How** it is
applied (merge, cherry-pick, squash) is `U-GIT-5` — the primitive keeps it pluggable.

**Steps**
1. Verify the ref first: `git -C <dir> rev-parse --verify refs/cowflow/changes/<id>^{commit}` must equal the pinned `headSHA`; mismatch → error (integrity).
2. Strategy behind an enum injected by config/decision. Guidance sketch for the merge variant (reuses `S2-E7`):
   ```go
   args := []string{"merge", "--no-ff", "--no-edit", "-m", msg, "--end-of-options", headSHA}
   if _, err := g.Run(ctx, dir, args...); err != nil {
       files, _ := g.Run(ctx, dir, "diff", "--name-only", "-z", "--diff-filter=U")
       return ApplyResult{}, &ConflictError{ChangeID: id, Files: splitNUL(files)}
   }
   head, err := g.Run(ctx, dir, "rev-parse", "--verify", "HEAD^{commit}")
   ```
3. Engine commits (merge commits, if any) use a fixed engine identity via `-c user.name=… -c user.email=…` argv, never the user's global config (Guidance; identity value to confirm with `AG-LEAD`).
4. `AbortApply`: `git merge --abort` (or the strategy's abort), then verify a clean tree.
5. All calls via the `S0-E4` runner: `-C`, `GIT_TERMINAL_PROMPT=0`, timeout, no shell.

**Tests** — clean apply; conflict returns files; ref/head mismatch; fault injection (exit code, timeout, truncated output) via harness; abort restores.

**Acceptance**
- [ ] Strategy pluggable; default left to the `U-GIT-5` decision.
- [ ] No user-supplied string reaches git without `--end-of-options`/`--`.

**Pitfalls** — reading porcelain without `-z`; applying a live task branch instead of the retention ref (§24.10).

---

### S5-T09 — Candidate builder

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-INT` | `AG-LEAD`, `AG-SCHED`, `AG-SEC` | `S5-E2` | S5-T02, S5-T06, S5-T07, S5-T08 | `feat/integration-candidate` | yes |

**Skills:** `integration-engine`, `cow-workspaces`, `git-changes-sync`   **Spec:** §10.1 steps 2, 4, 5, 7, 8
**Files:** `internal/integration/candidate.go`, `internal/integration/candidate_test.go`

**Why / what** — Compose S5-T07 and S5-T08 into "pinned SHAs in, `candidate_sha` out".

**Steps**
1. `CreateCandidate(BaseSHA = attempt.TargetSHA)`; store `workspace_id` on the attempt (tx).
2. For each ordered member: `ApplyChange(dir, "refs/cowflow/changes/<id>", m.HeadSHA)`. On `*git.ConflictError` → return an error carrying stage `apply`, files, the change ID; the engine fails the attempt (event §6.8), keeps the workspace (`ReleaseCandidate(keepUntil)`), flags tasks per `U-GIT-16`.
3. `candidate_sha = rev-parse HEAD`; must be a descendant of `target_sha` (Guidance assertion: `git merge-base --is-ancestor <target_sha> <candidate_sha>`; if `U-GIT-5` chooses a non-descendant model, revisit).
4. Respect `ctx` cancellation (Cancel, daemon stop) between members.

**Tests** — with fakes: order respected; conflict on the 2nd member keeps workspace and reports files; cancellation mid-apply; with real git (harness tmp repo): two features → candidate contains both.

**Acceptance**
- [ ] Failure event carries stage, SHAs, files (§10.1 step 7).
- [ ] Candidate never written to by anything but the applier.

**Pitfalls** — leaking candidate on success before review (`integration open` needs it until promotion).

---

### S5-T10 — Gate framework and diff-only gates

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-INT` | `AG-LEAD`, `AG-COLLAB` | `S5-E3` | S5-T02, `S2-E6`, `S2-E8`, `S2-E9` | `feat/integration-diff-only-gates` | no |

**Skills:** `integration-engine`, `collaboration-protocol`   **Spec:** §1.2, §9.5, §9.6, §10.1 step 3, §20.10, §22 (`diff_only_first`), §23 decision 12
**Files:** `internal/integration/gates.go`, `internal/integration/diffonly.go`, tests

**Why / what** — Cheap checks first: if a group violates a `pattern` invariant, there is
no reason to clone, build and test. This task also defines the `Gate` interface used
by every stage.

**Steps**
1. Implement `Gate`/`GateInput` per §6.3; `runStage(ctx, stage, gates)` records every result in the attempt (tx per result batch, Guidance) and returns `false` on the first required failure after all gates of the stage ran (Guidance: diff-only gates are cheap — report all of them).
2. Assert `in.Dir == ""` for diff-only gates (panic-free: return a failed result with diagnostic "diff-only gate received a workspace").
3. Gates:
   - **`pattern` invariants**: for each `[[invariants]] kind = "pattern"`, call the `S2-E9` evaluator on each member manifest, reading `blob_after` via `git cat-file --batch` (no workspace). Scope (whole blob vs added lines) is `U-GIT-6` — reuse the evaluator unchanged. Violation → `invariant.violated` + failed, required.
   - **touched contracts**: union of `touches_contracts`; if any contract is referenced by an active task (`contract_refs`), mark the group "requires lead" (§9.5, §20.20) — a flag consumed by S5-T16, not a failure.
   - **overlap between members**: intersect manifest paths across members (from `change_files`, never Git). Blocking vs warning is `U-GIT-11`; implement with `Required()` read from a setting the lead fills after the decision; until then report as non-blocking with a diagnostic and record that it is pending `U-GIT-11`.
   - **manifest lint**: contents are `U-GIT-11`/`C12`. Ship the gate with an empty, configurable rule list; do not invent rules.
4. `diff_only_first = true` → stage order diff-only → workspace. `false` → `U-GIT-17` (return `ErrUndecided`).

**Tests** — `TestDiffOnlyFailureCreatesNoWorkspace` (fake `CandidateManager` asserts zero calls) (§20.10); pattern violation emits `invariant.violated`; overlap reported with both task IDs; contract flag set; no `exec` calls during diff-only (fake git runner records only `cat-file`).

**Acceptance**
- [ ] Diff-only gates never create or touch a workspace.
- [ ] Same `pattern` evaluator as publish.

**Pitfalls** — reading files from an agent workspace to evaluate patterns (use blobs); diverging publish vs integration pattern semantics.

---

### S5-T11 — Gate leases and per-target slot

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-SCHED` | `AG-LEAD`, `AG-INT` | `S5-E4` | `S4-E5`, `S4-E6` | `feat/scheduler-gate-leases` | no |

**Skills:** `scheduler-and-leases`, `integration-engine`   **Spec:** §9.8, §10.3, §11, §22
**Files:** `internal/scheduler/gates.go`, `internal/scheduler/gates_test.go`

**Why / what** — "Integration gates go through the **same** queue (`compile`, `test`).
There is no parallel queue for the engine" (§9.8). The engine needs a holder identity
and a mapping from gate to resource.

**Steps**
1. Implement `GateLeaser` (§6.6) on top of the `S4-E6` lease tables and transactions; grants/queues/expiry emit the existing `resource.*` events.
2. Holder identity is `U-SCHED-11`. Implement through a single adapter function `holderRow(h GateHolder) (sessionID, taskID string)` so the decision lands in one place.
3. `ResourceFor(gate)` is `U-GIT-8`: read the mapping from configuration provided by `AG-LEAD` after the decision; with no mapping, return `ok=false` and the engine runs the gate **without** a lease but records `LeaseID = ""` and a warning (Guidance stopgap — confirm Day 1).
4. Slot counts come from the scheduler's resolved resources (aliases `max_builds`→`compile`, `max_tests`→`test`, §9.8; precedence `U-SCHED-3`) — never recompute in the engine.
5. `TargetSlot(target)`: resource `integrate:<target>` (alias of `max_integrations_per_target`, §9.8). Whether it wraps the whole validation or only the ref update is `C5`; expose both call sites, enable one by decision.
6. Lease renewal while a long gate runs: renew from the engine's heartbeat ticker (fake clock in tests) before `lease_ttl_seconds` (§24.5).

**Tests** — `compile.slots = 1` held by an agent session → gate acquire queues, `resource.granted` after release; TTL renewal for a 2×TTL gate; revoke by lead cancels the gate context.

**Acceptance**
- [ ] No new queue/table for the engine.
- [ ] `U-SCHED-11`, `U-GIT-8`, `C5` isolated behind single functions.

**Pitfalls** — polling queue position (use the event path, §9.8); holding a lease past a gate timeout.

---

### S5-T12 — Workspace gates (`[[gates]]`)

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-INT` | `AG-LEAD`, `AG-SCHED`, `AG-SEC` | `S5-E4` | S5-T09, S5-T10, S5-T11, `S0-E5` | `feat/integration-workspace-gates` | yes |

**Skills:** `integration-engine`, `scheduler-and-leases`, `windows-platform`, `security-review`   **Spec:** §7, §9.8, §10.1 step 6, §17, §22 (`[[gates]]`), §24.4
**Files:** `internal/integration/wsgates.go`, tests

**Why / what** — Build, test and lint the candidate. Commands come from config as argv
arrays; they run under timeouts with process-tree kill, and each acquires the right lease.

**Steps**
1. Config example the gates read (§22):
   ```toml
   [[gates]]
   name = "build"
   cmd = ["go", "build", "./..."]
   timeout_seconds = 600
   required = true

   [[gates]]
   name = "test"
   cmd = ["go", "test", "./...", "-race"]
   timeout_seconds = 900
   required = true
   ```
2. Run gates in config order (Guidance: deterministic). For each:
   ```go
   func (g cmdGate) Run(ctx context.Context, in GateInput) domain.GateResult {
       res := domain.GateResult{Name: g.cfg.Name, Stage: string(StageWorkspace), Required: g.cfg.Required}
       if res2, ok := g.leaser.ResourceFor(g.cfg.Name); ok {
           id, err := g.leaser.Acquire(ctx, g.holder, res2, g.leaseWait) // U-SCHED-2 on timeout
           if err != nil { res.Diagnostic = "lease: " + err.Error(); return res }
           defer g.leaser.Release(context.WithoutCancel(ctx), g.holder, id)
           res.LeaseID = id
       }
       cctx, cancel := context.WithTimeout(ctx, time.Duration(g.cfg.TimeoutSeconds)*time.Second)
       defer cancel()
       cmd := exec.CommandContext(cctx, g.cfg.Cmd[0], g.cfg.Cmd[1:]...) // argv only, never a shell
       cmd.Dir = in.Dir
       cmd.Env = g.env(in) // os.Environ minus GIT_* overrides + .env.cowflow (Guidance)
       out := newTailBuffer(64 << 10)
       cmd.Stdout, cmd.Stderr = out, out
       start := g.clock.Now()
       err := g.proc.RunTree(cmd) // S0-E5: Job Object / process group, kill tree on cancel
       res.Duration, res.Output = g.clock.Now().Sub(start), out.String()
       res.ExitCode, res.Passed = exitCode(err), err == nil
       return res
   }
   ```
3. Required failure → attempt fails (stage `workspace`); `required = false` failure → recorded, attempt continues (shown as warning in review). `test` is required (§24.4) — config validation should not let it be `false` silently (Guidance: warn).
4. Gate timeouts kill the whole tree (§20.16 behavior from `S0-E5`).
5. `FlaggedTasks` for repo-wide failures is `U-GIT-16`.

**Tests** — pass/fail/timeout with fake process runner; `required=false` does not fail the attempt; lease released on success, failure, timeout, cancel; `TestGatesUseCompileQueue`; output truncated; no `sh -c`/`cmd /c` anywhere (grep test from `S0-E4`).

**Acceptance**
- [ ] Argv only; per-gate timeout; tree kill; lease in the shared queue.
- [ ] Evidence (exit code, duration, output tail, lease ID) stored per gate.

**Pitfalls** — inheriting `GIT_DIR`/`GIT_WORK_TREE` from the daemon env; releasing the lease with the cancelled ctx (use `context.WithoutCancel`).

---

### S5-T13 — `cmd` invariants as gates

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-INT` | `AG-LEAD`, `AG-COLLAB`, `AG-SEC` | `S5-E5` | S5-T12 | `feat/integration-cmd-invariants` | yes |

**Skills:** `integration-engine`, `collaboration-protocol`, `security-review`   **Spec:** §9.6, §10.1 step 6, §19 Phase 5
**Files:** `internal/integration/invariants.go`, tests

**Why / what** — "The engineer says 'no' once, in the TOML; the runtime repeats it"
(§9.6). `cmd` invariants need the candidate tree, so they run as workspace gates in
every group.

**Steps**
1. Build one gate per `[[invariants]] kind = "cmd"`:
   ```toml
   [[invariants]]
   name = "no-linear-log-scan"
   kind = "cmd"
   cmd = ["go", "run", "./tools/invariants", "--check", "log-scan"]
   ```
2. Reuse the S5-T12 runner (timeout: Guidance — `command_timeout_seconds`, no per-invariant timeout key exists in §22); resource per `U-GIT-8`.
3. Always required (a violated invariant refuses, §9.6). Non-zero exit → `invariant.violated` (same tx as the result) with `diagnostic` = output tail; the attempt fails at stage `workspace`.
4. Run after `[[gates]]` (Guidance; order deterministic).

**Tests** — passing and failing invariant; `invariant.violated` payload; timeout; argv preserved exactly (no splitting).

**Acceptance**
- [ ] Runs in every group; violation emits `invariant.violated` and fails the group.

**Pitfalls** — running `cmd` invariants diff-only (they need a tree); re-tokenizing `cmd` strings.

---

### S5-T14 — Stale detection and revalidation

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-INT` | `AG-LEAD`, `AG-GIT` | `S5-E6` | S5-T09, S5-T12, S5-T15 | `feat/integration-stale` | no |

**Skills:** `integration-engine`, `testing-harness`   **Spec:** §10.1 step 9, §18 ("Target advances"), §20.11, §22 (`rerun_on_stale`), §24.12
**Files:** `internal/integration/stale.go`, tests

**Why / what** — A validation is only valid for its `target_sha`. When the target moves,
the engine must notice, not promote stale results, and redo the proof automatically.

**Steps**
1. Detection points: (a) at promotion, the current target differs from `attempt.TargetSHA`; (b) `UpdateRefCAS` returns `git.ErrRefMoved`. Both → stale.
2. In one tx: attempt outcome `stale`, emit `integration.stale` (`old_target_sha`, `new_target_sha`).
3. `rerun_on_stale = true`: start a new attempt with the new `target_sha` (same members, same order) through `runAttempt` — diff-only, candidate rebuild, workspace gates.
4. `rerun_on_stale = false`: `U-GIT-7` — return `ErrUndecided`, leave the group stale, notify the lead.
5. Reapply conflict on the new target → normal failure path → `fix_incompat` to the flagged task (agents are called only now, §24.12).
6. Whether a manual approval carries over to the revalidated attempt is `U-GIT-12`; until decided, after revalidation passes the group waits for a new `promote` (safe default: never move a ref without an approval for that exact `candidate_sha` — confirm Day 1).
7. Loop guard: Guidance — stop after `[scheduler] retry_limit` consecutive stale cycles and notify the lead; record the choice in `docs/decisions/`.

**Tests** — `TestStaleRevalidates` (harness: fake ref moved between validate and promote → `integration.stale` → new attempt on new SHA → `integration.passed`); CAS failure path; conflict after stale opens `fix_incompat`; loop guard with fake ref that keeps moving.

**Acceptance**
- [ ] No promotion ever uses a validation of a different `target_sha`.
- [ ] `rerun_on_stale=false` blocked cleanly.

**Pitfalls** — checking "target moved" only before taking the lock (race): the CAS is the real check.

---

### S5-T15 — CAS ref update, target resolution and engine tags

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-GIT` | `AG-LEAD`, `AG-STORE`, `AG-SEC` | `S5-E7` | `S0-E4` | `feat/git-ref-cas` | yes |

**Skills:** `git-changes-sync`, `security-review`   **Spec:** §2.2 (13), §8.1, §17, §24.2, §24.14
**Files:** `internal/git/refs.go`, `internal/git/tag.go`, tests

**Why / what** — The single, atomic operation that moves `develop`/`main`.

**Steps**
1. `ResolveRef`: `git -C <repo> rev-parse --verify --end-of-options <ref>^{commit}` → 40-hex SHA.
2. `UpdateRefCAS`:
   ```go
   func (g *Git) UpdateRefCAS(ctx context.Context, repo, ref, newSHA, oldSHA, msg string) error {
       if !strings.HasPrefix(ref, "refs/heads/") || !isFullSHA(newSHA) || !isFullSHA(oldSHA) {
           return fmt.Errorf("update-ref: invalid arguments")
       }
       if err := g.CheckRefFormat(ctx, ref); err != nil { return err }
       _, err := g.Run(ctx, repo, "update-ref", "-m", msg, ref, newSHA, oldSHA)
       if err != nil {
           if cur, rerr := g.ResolveRef(ctx, repo, ref); rerr == nil && cur != oldSHA {
               return fmt.Errorf("%w: now %s", ErrRefMoved, cur)
           }
           return fmt.Errorf("update-ref %s: %w", ref, err)
       }
       return nil
   }
   ```
3. `CreateTag(name, sha, msg)`: Guidance — `git tag -a -m <msg> -- <name> <sha>` after `check-ref-format refs/tags/<name>`; refuse if it exists. Signed vs unsigned and version confirmation are `U-GIT-13`. Only the engine imports this function (enforce with a package-level comment + review; workers can never reach it, §24.14).
4. Warn (do not block): if the target branch is checked out in the user's main worktree, `update-ref` leaves that worktree's index/files behind the ref. Detect via `git worktree list --porcelain -z` and return a warning for the engine to surface (Guidance).

**Tests** — success; CAS failure returns `ErrRefMoved`; short SHA rejected; injection attempts (`-x`, `--foo`) refused; harness fault: timeout during update-ref → error, ref unchanged or at new value (verify by re-read).

**Acceptance**
- [ ] Only full ref names and full SHAs; `ErrRefMoved` distinguishable.
- [ ] Tag creation unreachable from worker paths.

**Pitfalls** — passing `develop` instead of `refs/heads/develop` (could match a tag); assuming a failed exit means "not moved" (re-read).

---

### S5-T16 — Promoter: modes, CAS under a short lock, evidence

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-INT` | `AG-LEAD`, `AG-SCHED`, `AG-SEC` | `S5-E7` | S5-T02, S5-T14, S5-T15 | `feat/integration-promotion` | yes |

**Skills:** `integration-engine`, `store-and-events`, `security-review`   **Spec:** §2.2 (12, 13), §8.1, §8.5, §9.5, §10.1 steps 9–10, §10.3, §15.1, §17, §22 (`promotion_mode`, `require_validation`), §23 decision 6, §24.12
**Files:** `internal/integration/promotion.go`, tests

**Why / what** — Promotion is the only place that changes shared history, so it must be
correct under crashes and races.

**Steps**
1. Preconditions: latest attempt `passed` (`require_validation = true`; `false` is `U-GIT-17`); group not cancelled; "requires lead" flag from S5-T10 → only a lead/human actor may promote, never `auto` (§9.5).
2. Mode switch:
   ```go
   switch e.cfg.Integration.PromotionMode {
   case "manual":
       if req.By.Kind == "lead_mcp" { return PromoteResult{Status: PromotePendingHumanApproval}, nil } // §15.1; audit it
       // human_cli is the approval (§5.4); U-GIT-12 for other actors
   case "auto":
       // called by the engine after integration.passed, or by the lead
   case "pr":
       return PromoteResult{}, fmt.Errorf("%w: promotion_mode pr (SPEC-GAPS C3)", ErrUndecided)
   }
   ```
3. Short per-target lock: an in-process `map[target]*sync.Mutex` in the daemon (engine is daemon-only, §5.3), plus `TargetSlot` per `C5`. Hold it only for: re-read target → CAS → record.
4. Promoted SHA: `U-GIT-5`. Guidance for the maintainer: when the candidate is a descendant of `target_sha`, fast-forward the target to `candidate_sha` (`promoted_sha == candidate_sha`); keep the two fields separate regardless.
5. Crash safety (store skill "pending row before side effect"): tx1 marks the attempt "promoting" with `op_id`; CAS; tx2 records `promoted_sha`, changes → `promoted`, tasks → `done` (§24.12), group state, `integration.promoted`, `audit_log` (§17). Reconciliation: attempt "promoting" + target == candidate → finish tx2; target == old → retry or fail; else → stale.
6. After tx2: candidate released; task workspaces handed to the cleanup policy (`cleanup_completed`, `S1-E6`); gitflow hooks (S5-T17); post-promotion (S5-T21) when enabled.
7. Evidence: attempt `results_json` + SHAs + approver actor; exposed by `GET …/review`.

**Tests** — manual + MCP → `pending_human_approval`, ref unchanged; manual + CLI → promoted; auto promotes after pass; contract flag blocks auto; crash between CAS and tx2 (harness crash point) → reconciliation completes once (idempotent); two groups promoting concurrently on one target → exactly one wins, other goes stale.

**Acceptance**
- [ ] Ref moves only via `UpdateRefCAS` inside the lock.
- [ ] `published`/`validated`/`promoted` transitions visible separately (§20.9).
- [ ] Audit row for every promote.

**Pitfalls** — holding the lock while running gates; marking changes `promoted` before the CAS succeeds.

---

### S5-T17 — Gitflow preset effects after promotion

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-INT` | `AG-LEAD`, `AG-COLLAB`, `AG-SEC` | `S5-E7` | S5-T16, `S3-E3` | `feat/integration-gitflow-effects` | yes |

**Skills:** `integration-engine`, `cli-and-config`, `security-review`   **Spec:** §24.1, §24.2, §24 footer, §20.31
**Files:** `internal/integration/gitflow.go`, tests

**Why / what** — In GitFlow a hotfix must land on `main` **and** `develop`. COWFlow
automates the second half so it is never forgotten.

**Steps**
1. Hook runs after `integration.promoted` when `[conventions] preset = "gitflow"`:
   ```toml
   [conventions]
   preset = "gitflow"
   tag_format = "v{major}.{minor}.{patch}"
   ```
2. **Hotfix** (task kind `hotfix`, `C2` — not branch parsing) promoted to `main` → `Create` a group with `Target = "develop"`, same members (same change IDs and pinned `head_sha`), `By = engine`, `OriginGroupID` = the promoted group; emits `integration.created`; notify the lead via `LeadNotifier` (§24.2). Guidance: do not auto-validate — the spec only says "creates"; the lead runs `validate` (in `auto` mode the normal auto flow applies — confirm Day 1).
3. **Release** (`release` kind) promoted to `main` → tag per `tag_format` after the lead confirms the version, plus a return group to `develop`. The confirmation mechanism is `U-GIT-13`: ship the return group; the tag step returns `ErrUndecided` until decided.
4. `trunk`: no back-merge groups. `none`: no enforcement, auditing only (§24 footer).
5. Idempotency: the back-merge `Create` uses `op_id = "backmerge:" + promotedGroupID`, so reconciliation after a crash never creates two groups.

**Tests** — `TestHotfixBackMergeGroup` (§20.31); feature to develop creates nothing; preset `trunk`/`none` create nothing; crash after promotion → exactly one back-merge group.

**Acceptance**
- [ ] Back-merge group created by the engine, lead notified, idempotent.
- [ ] No tag created without `U-GIT-13` decided.

**Pitfalls** — assuming the hotfix applies cleanly to `develop` (a conflict is the normal failure path, reported to the hotfix task).

---

### S5-T18 — Review document assembly

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-INT` | `AG-LEAD`, `AG-COLLAB` | `S5-E8` | S5-T10, S5-T12 | `feat/integration-review` | no |

**Skills:** `integration-engine`, `collaboration-protocol`   **Spec:** §1.2, §9.4, §12.5 (`/review`), §24.4, §24.9, §24.10, §24.13, §24.16
**Files:** `internal/integration/review.go`, tests

**Why / what** — The lead approves by intent. The engine assembles one structure; CLI,
API and MCP render it.

**Steps**
1. `Review` shape (Guidance):
   ```go
   type Review struct {
       Group    domain.IntegrationGroup
       Attempt  domain.IntegrationAttempt // latest
       Tasks    []TaskReview
       Warnings []string
       RequiresLead bool // §9.5
   }
   type TaskReview struct {
       TaskID, ChangeID, HeadSHA, Goal string
       Paths       []string // intent scope
       Stub        bool
       Stat        domain.Stat // files/add/del (§8.2)
       Contracts   []string    // touches_contracts
       Gates       []domain.GateResult
       Invariants  []domain.GateResult
       Warnings    []string
       Incomplete  bool // session without report_result (§24.13)
   }
   ```
2. Warnings: manifest > 20 files or > 800 lines (§24.10); `scope.exceeded` from the manifest (§24.9); `feat` commit type without a new test (§24.4 — Guidance detection: manifest `op = add|modify` on test files by language convention, e.g. `_test.go`; confirm with `AG-LEAD`); `required = false` gate failures; out-of-group dependencies (S5-T06).
3. Data only from SQLite (intents, manifests, attempts); no Git unless `Diff: true`, then `git diff --stat`/full diff between `target_sha` and `candidate_sha` (Guidance).

**Tests** — golden JSON for a two-task fixture; each warning triggers; no git calls without `--diff` (mock records zero calls).

**Acceptance**
- [ ] Review answers without Git by default.

**Pitfalls** — rendering text here (rendering is S5-T19).

---

### S5-T19 — CLI `integration show [--diff]`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-CLI` | `AG-LEAD`, `AG-API` | `S5-E8` | S5-T18, S5-T03 | `feat/cli-integration-show` | no |

**Skills:** `cli-and-config`, `integration-engine`   **Spec:** §9.4, §14, §1.2
**Files:** `internal/cli/integration_show.go`, golden test files

**Why / what** — The human's review screen: intent first, lines on demand.

**Steps**
1. `cowflow integration show <group-id> [--diff] [--json]` → `GET /v1/integration-groups/{id}/review`.
2. Human layout (Guidance):
   ```text
   Group ig_01J… → develop   target 3f2a…  candidate 9c1d…   state: passed
   [auth] Implement JWT          src/auth/**        4 files +120 -8   contracts: auth-api.md
          gates: diff_only ✓ pattern ✓ overlap ✓ | build ✓ 41s (compile) test ✓ 88s (test) lint ✗ (optional)
   [api]  Orders endpoint        src/api/**         3 files +95 -2
   Warnings: api: feat without new test
   ```
3. `--json` prints the `Review` document unchanged.

**Tests** — golden outputs (with/without `--diff`, `--json`); width-independent layout.

**Acceptance**
- [ ] Shows intent, stat, contracts, gates, invariants, warnings (§9.4).

**Pitfalls** — printing diffs by default.

---

### S5-T20 — Revert primitive

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-GIT` | `AG-LEAD`, `AG-STORE`, `AG-SEC` | `S5-E9` | S5-T08, S5-T15 | `feat/git-revert-promotion` | yes |

**Skills:** `git-changes-sync`, `security-review`   **Spec:** §10.2, §17, §18, §24.3
**Files:** `internal/git/revert.go`, tests

**Why / what** — An automatic revert must produce a normal, auditable commit that undoes
exactly what was promoted — never a history rewrite.

**Steps**
1. Input: temp workspace at `promotedSHA`, `previousSHA` (the target before promotion).
2. Shape depends on `U-GIT-5`. Guidance options for the maintainer: promoted range is a single merge → `git revert --no-edit -m 1 <promotedSHA>`; several commits → `git revert --no-commit <previousSHA>..<promotedSHA>` then one commit. Implement both behind the strategy chosen for S5-T08.
3. Message respects `enforce_commit_format` (§24.3), e.g. `revert: undo integration ig_… (post-promotion gate test failed)`; engine identity as S5-T08.
4. Return `revert_sha`; the engine promotes it with `UpdateRefCAS(target, revert_sha, promoted_sha)`.

**Tests** — revert of merge and of range; tree after revert equals tree at `previousSHA`; conflict on revert (target moved) returns an error, nothing pushed.

**Acceptance**
- [ ] Never `reset`/force: history only grows.

**Pitfalls** — reverting against a target that moved after promotion (the CAS catches it; surface to lead).

---

### S5-T21 — Post-promotion gates and auto-revert

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-INT` | `AG-LEAD`, `AG-COLLAB`, `AG-SEC` | `S5-E9` | S5-T16, S5-T20, S5-T12 | `feat/integration-post-promotion` | yes |

**Skills:** `integration-engine`, `collaboration-protocol`, `security-review`   **Spec:** §1.2, §10.2, §17, §18 ("Post-promotion fails"), §20.24, §22 (`[integration.post_promotion]`)
**Files:** `internal/integration/postpromotion.go`, tests

**Why / what** — In auto mode the project trades pre-merge certainty for throughput; the
safety net is "test after, revert fast".

**Steps**
1. Active only when `promotion_mode = "auto"` **and** `post_promotion.enabled = true`:
   ```toml
   [integration]
   promotion_mode = "auto"

   [integration.post_promotion]
   enabled = true
   gates = ["test"]
   auto_revert = true
   ```
2. After `integration.promoted`: create a temporary workspace at `promoted_sha` (S5-T07 manager), run the named `[[gates]]` through the S5-T12 runner (same leases), stage `post_promotion`.
3. All pass → done (record results as evidence).
4. Failure with `auto_revert = true`: `RevertPromotion` → `UpdateRefCAS(target, revert_sha, promoted_sha)`; one tx: attempt outcome `reverted`, `integration.reverted`, one cross-request per task involved (`Requests.Open`; kind/expected output `U-GIT-14`), `LeadNotifier.NotifyLead`, `audit_log` (§17). Change/task state after revert is `U-GIT-14`: leave them unchanged until decided and record the revert on the attempt.
5. CAS fails during revert (target moved) → do not retry blindly: notify the lead with both SHAs (Guidance).
6. `auto_revert = false` → `U-GIT-14` (`ErrUndecided` + lead notified).

**Tests** — `TestPostPromotionRevert` (harness, §18 row, §20.24): failing post gate → revert commit on target, `integration.reverted`, requests to both tasks, lead notice; passing post gate → no revert; manual mode never runs post-promotion; revert CAS failure path.

**Acceptance**
- [ ] Revert promoted by CAS; event + requests + lead notice in one tx.

**Pitfalls** — running post-promotion in manual mode; reverting with the user's git identity.

---

### S5-T22 — Harness and integration tests for the sprint criteria

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD`, `AG-SEC` | `S5-E10` | S5-T02 … S5-T21 (incrementally) | `test/integration-engine-scenarios` | no |

**Skills:** `testing-harness`, `integration-engine`   **Spec:** §18, §18.1, §20 (4, 9, 10, 11, 24, 31)
**Files:** `test/integration/integration_engine_test.go`, `test/fixtures/integration-demo/`, `test/harness/crashpoints.go` (new crash points)

**Why / what** — Each acceptance criterion needs an automated, hermetic test traceable
by `// AC#n` and `// §18:<row>`.

**Steps**
1. Fixture repo `test/fixtures/integration-demo/`: a tiny Go module with `src/auth`, `src/api` (api imports auth), `contracts/auth-api.md`, a `tools/flaky` program (see §8.6) and a `cmd` invariant tool.
2. Tests (real git ≥ 2.40 in a temp repo, fake clock, `-race`):
   - `TestCreateRefusedWithoutChange` // AC#4
   - `TestChangeStatesDistinct` // AC#9
   - `TestTwoFeaturesDiffOnlyThenWorkspaceManualPromotion`, `TestDiffOnlyFailureCreatesNoWorkspace` // AC#10
   - `TestStaleRevalidates` // AC#11, §18:"Target advances"
   - `TestPostPromotionRevert` // AC#24, §18:"Post-promotion fails"
   - `TestHotfixBackMergeGroup` // AC#31
   - `TestGatesUseCompileQueue` // §9.8
   - `TestPromotionCrashReconciles` // §18:"Daemon restarts" — crash points `promotion.after_cas_before_record`, `outbox.after_write_before_delivery`
   - `TestConcurrentPromotionsOneWins`
3. Git fault injection on `update-ref`, `merge`, `revert` (exit code, timeout, truncated output).
4. CI: runs on the existing 3-OS matrix; tests needing `cow_clone` fall back to `git_worktree` with `t.Log` of the `CowKind`.

**Tests** — this task *is* tests; each must fail when the corresponding feature is disabled (mutation sanity check, run locally once).

**Acceptance**
- [ ] Every §2 criterion has a named, green test on Windows/Linux/macOS.

**Pitfalls** — wall-clock sleeps; developer git config leaking (set `GIT_CONFIG_GLOBAL`/`GIT_CONFIG_SYSTEM` to empty files).

---

### S5-T23 — Sprint demo and evidence

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD` | `S5-E10` | S5-T22 | `docs/sprint-5-demo-evidence` | no |

**Skills:** `testing-harness`, `integration-engine`, `gitflow`   **Spec:** §19 Phase 5, §20.9, §20.10
**Files:** `CHANGELOG.md` (`[Unreleased]`), PR body with evidence; `test/fixtures/integration-demo/demo.config.toml`

**Why / what** — Prove the phase output on a clean machine, end to end through the CLI.

**Steps**
1. Clean Windows 11 machine with Dev Drive (plus one Linux run): build `CGO_ENABLED=0 go build ./cmd/cowflow`.
2. Run §8 exactly; capture `--json` outputs, `cowflow events` log, gate durations, `git log --oneline -5 develop main`.
3. Attach evidence to the sprint-closing PR; update `CHANGELOG.md` under `[Unreleased]`.
4. Commit: `git commit -S -s -m "docs(changelog): record sprint 5 integration demo"`.

**Tests** — the demo itself; any deviation from expected output → issue + fix before closing.

**Acceptance**
- [ ] All §2 boxes ticked with linked evidence.

**Pitfalls** — running the demo on a machine with a stale daemon/state dir; editing expected output to match a bug.

---

## 8. Demo script

Run from the fixture copy. Output below is illustrative: exact wording is
`AG-CLI`'s; the **checked** facts are the ones in "Expect".

### 8.1 Setup

```powershell
$repo = "X:\demo\shop"; Copy-Item -Recurse test\fixtures\integration-demo $repo
cd $repo; git init -b main; git add -A; git commit -m "chore: initial import"; git branch develop
cowflow init --conventions gitflow
Copy-Item test\fixtures\integration-demo\demo.config.toml <config.toml location per S0-E2>
cowflow daemon start
```

`demo.config.toml` (relevant part; real import pattern per `C6`):

```toml
[integration]
require_validation = true
promotion_mode = "manual"
rerun_on_stale = true
diff_only_first = true

[[gates]]
name = "build"
cmd = ["go", "build", "./..."]
timeout_seconds = 600
required = true

[[gates]]
name = "test"
cmd = ["go", "test", "./...", "-race"]
timeout_seconds = 900
required = true

[[invariants]]
name = "no-cmd-import-in-src"
kind = "pattern"
deny = ["src/**: import \"example.com/shop/cmd"]

[[invariants]]
name = "api-uses-contract"
kind = "cmd"
cmd = ["go", "run", "./tools/invariants", "--check", "api-contract"]

[[resources]]
name = "compile"
slots = 1
lease_ttl_seconds = 600
```

Produce two published changes with a dependency (`auth` → `api`), using the
Sprint 3 flow (lead + workers) or the scripted workers from `S2-E11`:

```powershell
cowflow task create auth --kind feature --title "Implement JWT" --contract contracts/auth-api.md --paths src/auth
cowflow task create api  --kind feature --title "Orders endpoint" --depends auth --paths src/api
cowflow task create docs --kind chore --title "Docs refresh" --paths docs
# ... workers announce intents and publish ...
cowflow change list --json
```

Expect: one `published` change for `auth`, one for `api`, none for `docs`.

### 8.2 Integration refused without change (§20.4)

```powershell
cowflow integration create docs --target develop; $LASTEXITCODE
```
```text
error: integration refused: task docs has no published change
4
```
Expect: exit `4`; `cowflow events --json` shows no `integration.created`.

### 8.3 Two features, diff-only first, manual promotion (§20.9, §20.10, Phase 5 output)

```powershell
cowflow integration create auth api --target develop --json   # → {"id":"ig_01J…","target":"develop",…}
cowflow integration validate ig_01J…
```
```text
attempt ia_01J…  target_sha 3f2a9e1…
[diff_only] pattern:no-cmd-import-in-src ✓  contracts ✓  overlap ✓  manifest-lint ✓
[candidate] cow_clone of golden 3f2a9e1… → applied auth (1/2), api (2/2) → candidate 9c1d07b…
[workspace] build ✓ 38s (lease compile)  test ✓ 91s  invariant:api-uses-contract ✓
integration.passed
```
```powershell
cowflow events --since <cursor> --json        # order: integration.created, integration.started, …, integration.passed
cowflow change list --json                    # auth/api changes: "validated"
cowflow integration show ig_01J…              # intent, stat, contracts, gates, invariants
```
Lead agent (VS Code) calls `cowflow_promote(ig_01J…)` → `{"status":"pending_human_approval"}`;
`git rev-parse develop` unchanged. Human:

```powershell
cowflow integration promote ig_01J…
git rev-parse develop                          # equals promoted_sha
cowflow change list --json                     # "promoted"
cowflow integration show ig_01J… --json        # evidence: gate results, target/candidate/promoted SHAs, approver
```
Expect: diff-only results recorded before the candidate workspace's creation time;
states `published` → `validated` → `promoted` observed separately.

### 8.4 Target advances → revalidation (§20.11)

```powershell
# Two new published changes: task "search" (group A) and task "cart" (group B)
cowflow integration create search --target develop --json   # ig_A
cowflow integration create cart   --target develop --json   # ig_B
cowflow integration validate ig_A; cowflow integration validate ig_B
cowflow integration promote ig_A               # develop moves
cowflow integration promote ig_B
```
```text
integration.stale  old_target_sha 9c1d07b…  new_target_sha 51be2c4…
revalidating ig_B on 51be2c4… (rerun_on_stale = true)
[diff_only] … ✓  [workspace] build ✓  test ✓
integration.passed
```
Expect: a second attempt for `ig_B` pinned to the new `target_sha`; the final promote
behavior follows the `U-GIT-12` decision (re-run `cowflow integration promote ig_B` if approval
does not carry over); `develop` ends at `ig_B`'s `promoted_sha`.

### 8.5 Hotfix promoted to `main` → automatic group for `develop` (§20.31)

```powershell
cowflow task create fix-login --kind hotfix --title "Fix login redirect" --paths src/auth
# ... worker publishes ...
cowflow integration create fix-login --target main --json      # ig_H
cowflow integration validate ig_H; cowflow integration promote ig_H
cowflow events --since <cursor> --json
```
```text
integration.promoted  ig_H  target main
integration.created   ig_01J…X  target develop  origin_group_id ig_H  created_by engine
```
Expect: a new group targeting `develop` with the hotfix change, created by the engine;
the lead received a notification.

### 8.6 Auto mode: post-promotion failure → revert + requests (§20.24, Phase 5 output)

Switch config to `promotion_mode = "auto"` and add:

```toml
[[gates]]
name = "smoke"
cmd = ["go", "run", "./tools/flaky", "--fail-on-run", "2", "--state", "X:/demo/flaky.state"]
timeout_seconds = 300
required = true

[integration.post_promotion]
enabled = true
gates = ["smoke"]
auto_revert = true
```

`tools/flaky` is a fixture that passes on its first run (candidate validation) and fails
on its second (post-promotion) — a deterministic stand-in for a failure only seen on
the target.

```powershell
cowflow daemon stop; cowflow daemon start
cowflow integration create reports --target develop --json   # ig_R (task "reports" already published)
cowflow integration validate ig_R
cowflow events --follow
```
```text
integration.passed     ig_R
integration.promoted   ig_R  promoted_sha 7a0c…
integration.reverted   ig_R  revert_sha e41b…  gate smoke (post_promotion) exit 1
```
```powershell
git log --oneline -2 develop        # e41b… revert: undo integration ig_R …
cowflow request list --open --json  # one request per task in ig_R
```
Expect: revert commit on `develop` authored by the engine; open cross-requests to the
involved task(s); lead notified; `integration show ig_R` shows promotion and revert evidence.

---

## 9. Definition of Done

- [ ] Every task's Acceptance boxes ticked; PRs reviewed by `AG-LEAD`, pod peer, and `AG-SEC` where `Security: yes`.
- [ ] §2 exit criteria green in CI (`go test -race -cover ./...`) on Windows, Linux, macOS; `CGO_ENABLED=0` build.
- [ ] `golangci-lint run`, `go vet ./...`, `govulncheck ./...` clean; `go mod tidy` no-op; no new dependency.
- [ ] No gate, invariant or git call built from a shell string; every git ref/SHA argument validated.
- [ ] Every state change and its event in one transaction; promote/revert/cancel audited (§17).
- [ ] Each SPEC-GAPS item in §3.2 is either decided (recorded in SCOPE.md §23, removed from SPEC-GAPS in the same PR) or still isolated behind its seam with an `ErrUndecided` path — none decided silently in code.
- [ ] The gaps found while planning (`U-GIT-11`…`U-GIT-17`, `U-SCHED-11`) are decided or still isolated behind their seams.
- [ ] Demo §8 run on a clean machine; evidence attached; `CHANGELOG.md` `[Unreleased]` updated.
- [ ] Retro learnings moved into `docs/skill/integration-engine/SKILL.md`.
- [ ] Commits Conventional, `git commit -S -s`, no AI attribution; PRs target `develop`.

---

## 10. Risks and mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| `U-GIT-5` (how changes are applied; `promoted_sha` vs `candidate_sha`) undecided past Day 3 | S5-T08/T09/T16/T20 stall; critical path slips | Pluggable strategy in S5-T08; lane a works on fakes; `AG-LEAD` escalates Day 1 with options and the merge-based Guidance sketch. |
| `update-ref` on a branch checked out in the user's main worktree | User's working copy looks dirty/behind after promotion | S5-T15 detects and warns; surfaced in `integration show`; document in skill; maintainer decides policy. |
| Gates starve agents (or vice versa) in the shared `compile` queue | Slow validations; watchdog noise | Same queue by spec (§9.8); measure `ResourceWait` per gate; `queue = "priority"` available per resource. |
| Engine lease holder identity (`U-SCHED-11`) conflicts with session-scoped leases | Leases leak or are never renewed | Single adapter in S5-T11; reconciliation expires engine leases of dead attempts; tests with fake clock. |
| Crash between CAS and the record tx | Target moved but DB says not promoted | Pending "promoting" row + reconciliation by comparing ref to `candidate_sha` (S5-T16, S5-T22 crash test). |
| Stale livelock when the target moves continuously | Group never promotes | Loop guard (S5-T14) + lead notification; per-target serialization (`C5`). |
| Long gates exceed `lease_ttl_seconds` | Lease expires mid-gate; slot double-booked | Engine renews via heartbeat ticker (S5-T11); test with 2×TTL gate. |
| Gate commands are untrusted project code running with daemon privileges | Host compromise from a malicious change | Documented: workspace is not a sandbox (§6.8); recommend `provider = "container"` for untrusted repos; argv only; env scrubbed of `GIT_*`. |
| Large gate output bloats SQLite | DB growth, slow reads | Output tail bounded (64 KiB, Guidance) in engine and store. |
| Windows file locks keep failed candidates from being destroyed | Disk fills with candidates | `pending_cleanup` retry (§18); disk budget via `workspace_disk_budget_gb`; candidate count in `/metrics`. |
| Too many undecided items block the demo | Exit criterion missed | Demo path (§8) uses only decided behavior plus explicit stopgaps confirmed by `AG-LEAD` on Day 1; blocked branches (`pr`, release tag, `auto_revert = false`) are not part of the exit criteria. |
