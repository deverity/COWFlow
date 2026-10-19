# Sprint 2 — Collaboration plane (daemonless)

> **Phase:** 2 of 6 ([SCOPE.md §19](../SCOPE.md), weeks 5–6) · **Length:** 2 weeks (10 working days) · **Exit criterion:** intent → request → publish → sync cycle in two terminals without a daemon; `who --path` answered from the index.

---

## 1. Goal

Until now COWFlow can create isolated workspaces (Sprint 1), but the agents
inside them cannot talk to each other. This sprint builds the
**Collaboration Plane** (§5.1) in **daemonless** mode (§5.3): every `cowflow`
process opens the same SQLite file directly, so two terminals (or two agent
processes) can coordinate with no background service.

At the end of the sprint a person in terminal A and a person in terminal B can:

1. declare what they are about to change (**intent**) and get warned when they
   overlap (`intent.overlap`) — a warning, never a lock (§2.2 principle 2);
2. ask the other side for something (**cross-request**) and track it from
   `open` to `closed`;
3. **publish** a commit as an immutable **change** with a retention ref and a
   per-file **manifest** — refused when there is no intent or when a
   `pattern` invariant is violated;
4. **sync** that change into their own workspace by merge (or explicit
   cherry-pick / squash), with conflicts recorded and `abort` restoring the
   workspace;
5. ask "who is working on `src/auth`?" (`cowflow who --path`) and get the
   answer from a derived index in SQLite, without running Git;
6. see a **contract** change notify its dependents;
7. follow all of this as **events** by polling with a cursor.

The daemon, SSE, heartbeats, the scheduler, MCP and runners come later
(Sprints 3–4). Everything built here must be reusable by them unchanged: the
CLI is only a thin front end over services that the API (S4) and MCP (S3)
will call too (§15.5: enforcement lives in the server/domain, not in a front end).

---

## 2. Exit criteria

Each box is ticked by `AG-LEAD` in the sprint-closing PR once the
verification listed after it has passed.

- [ ] **§19 Phase 2 output:** intent → request → publish → sync cycle in two terminals without a daemon — *verified by:* `S2-T26` two-process test (`test/integration/twoterminal_test.go`) runs two OS processes against one SQLite file; demo script §8 steps 1–9 run by `AG-QA` on Windows with no `cowflow daemon` process alive (`Get-Process cowflow` shows only the CLI invocations)
- [ ] **§19 Phase 2 output:** `who --path` via the index — *verified by:* demo §8 step 10; unit test from `S2-T08`
- [ ] **§20.3** Changes attributable by task/session — *verified by:* `change show --json` returns `task_id` and `session_id` for every change; `S2-T15` test asserts both columns are set and match the publishing session; trailers are blocked by U-GIT-2 (see §3) and are **not** required for this criterion in this sprint
- [ ] **§20.4 (publish part)** Publishing refused without intent; publishing refused with a violated invariant — *verified by:* demo §8 steps 4–5: exit code `4`, `invariant.violated` event present; tests in `S2-T15` and `S2-T20`. (The "integration refused without change" part is Sprint 5.)
- [ ] **§20.5** Change consumed via merge, with conflict recorded when there is one — *verified by:* demo §8 steps 8 and 11; `S2-T17` harness test: conflict → `sync_attempts` row in `conflict` with file list, exit `3`; `sync abort` restores pre-sync `HEAD` and a clean tree
- [ ] **§20.6** Complete cross-request with linked change — *verified by:* demo §8 steps 3, 6–9: request goes `open → accepted → delivered → closed`, `deliver` carries `--change`; `S2-T11` + `S2-T26`
- [ ] **§20.7** `blocked` agent unblocked by an event — *verified by:* demo §8 step 3/7: session B in `blocked` waits with a cursor poll and returns when the delivery event arrives; `S2-T12` test with fake clock
- [ ] **§20.20** Changed contract notifies dependents and requires the lead — *verified by:* demo §8 step 12: `contract.changed` addressed to the dependent task; `change show` reports that promotion requires the lead. Enforcement *at promotion time* is Sprint 5 (`S5-E7`); this sprint records and exposes the requirement (`S2-T19`)
- [ ] **§20.23** `who --path` answers from the index without invoking Git (verified by mock) — *verified by:* `S2-T08` test injects a recording `git.Runner` and asserts zero calls; `S2-T22` CLI test does the same through the command

All tests pass with `CGO_ENABLED=0 go test -race ./...` on Windows, Linux and macOS (CI from `S0-E8`).

---

## 3. Prerequisites

### 3.1 Earlier epics (must be merged to `develop` before day 1)

| Epic | What this sprint uses from it |
|---|---|
| `S0-E1` | cobra root, `--json`, `ExitError` with codes `0/1/2/3/4` |
| `S0-E2` | config structs for `[git]`, `[collaboration]`, `[[invariants]]`, `[security]`, `[conventions]` |
| `S0-E3` | `store.Open` (WAL, `busy_timeout`, immediate tx), embedded migration runner, base schema |
| `S0-E4` | `git.Runner` (argv, `-C`, `GIT_TERMINAL_PROMPT=0`, timeout), `check-ref-format` helper |
| `S0-E5` | path canonicalization/confinement (`platform.Confine`) |
| `S0-E8` | `test/harness`: fake clock, temp Git repo, Git fault injection; CI on 3 OSes |
| `S1-E1`, `S1-E2`, `S1-E4` | workspace records (path, branch, `base_sha`, provider, `CowKind`) |
| `S1-E5` | sparse updater used by `expand_scope` |
| `S1-E6` | dirty detection (reused before sync) |
| `S1-E8` | `task create` (`--contract`, `--paths`, `--depends`), `workspace list\|inspect` |

### 3.2 Spec gaps that affect this sprint

IDs are the stable ones from [SPEC-GAPS.md](../SPEC-GAPS.md). `AG-LEAD` resolves these with the maintainer on day 1
(README §4); **no task decides them**. Each blocked task codes everything else
behind the named interface and lands the decision-dependent piece afterwards.

| Gap | Topic | Blocks (until decided) |
|---|---|---|
| `U-DATA-1` | Columns of `events`, `outbox`, `event_deliveries`; where `op_id` is stored | final migration in `S2-T02`/`S2-T04`; `op_id` table in `S2-T03` |
| `U-DATA-2` | Prefixes of the prefixed ULIDs | `domain.IDGen` prefix table in `S2-T01` (code uses `IDKind`, never literal prefixes) |
| `U-DATA-3` | Whether the public event cursor is the ULID (cross-process ULIDs are not commit-ordered) | encoding of `domain.Cursor` in `S2-T05` (`Cursor` stays opaque to consumers) |
| `U-DATA-4` | Concrete `cross_request.*` event names; `scope.exceeded` not in the event list | event constants in `S2-T11`; whether `S2-T15` emits a `scope.exceeded` event (it records the warning in the manifest either way, §24.9) |
| `U-DATA-6` | Where contract `Status` lives (file header vs table); who moves it to `implemented` | status source in `S2-T18`/`S2-T19` (`ContractStatusSource` interface) |
| `U-GIT-1` | Diff base of the change manifest (`base_sha`, previous change, or parent) | `git.DiffBaseResolver` implementation in `S2-T13`; commit range checked by `enforce_commit_format` in `S2-T14` |
| `U-GIT-2` | How `Cowflow-Task`/`Cowflow-Change`/`Cowflow-Request` trailers reach an immutable published commit; value and casing of `x-cowflow-source` | trailer step of `S2-T14`; cherry-pick trailer in `S2-T16` |
| `U-GIT-3` | Non-descendant `HEAD`: refused or new change? | one branch of `S2-T15` step 4 |
| `U-GIT-4` | Retention cleanup (`change_retention_days`) | nothing is deleted this sprint; no GC task |
| `U-GIT-6` | `pattern` invariants: whole blob vs added lines | evaluation mode in `S2-T20` |
| `U-PLAT-1` | How a `cow_clone` tree gets its own HEAD and index | object transfer between workspace and project repo in `S2-T14`/`S2-T16` (decided or not in Sprint 1; confirm) |
| `U-API-1` | SQLite file name/location; `config.toml` location | assumed settled in Sprint 0; confirm before `S2-T06` multi-process tests |
| `U-API-2` | How CLI detects daemon vs daemonless | `S2-T21` ships the daemonless backend only, behind `cli.Backend` |
| `U-API-8` | How session tokens are validated in daemonless mode | `S2-T21` actor resolution (see U-API-16) |
| `U-SCHED-1` | Whether SLA escalation works daemonless | escalation *trigger* in `S2-T12` (bookkeeping only) |
| `U-SCHED-6` | Lead `close` from any request state | lead path of `Close` in `S2-T11` (refused until decided) |
| `C2` | Task branch naming `cowflow/<project>/<slug>` vs `<kind>/<slug>` | none directly; publish reads the branch from the workspace record, never rebuilds it |
| `C4` | `squash-merge` (§8.4) vs `--strategy squash` (§14) | stored strategy value in `S2-T16`; CLI flag value is `squash` exactly as §14 |
| `C6` | Invariant example import path | fixtures in `S2-T20`/`S2-T26` use their own module path |
| `C8` | Contract publish needs an *open* vs an *accepted* `confirm_contract` | rule in `S2-T19` (`ConfirmRule` interface; tests parametrized over both) |
| `C9` | Per-priority SLA (15/60 min) vs `escalation_after_minutes = 30` | `due_at` computation in `S2-T12` |
| `C12` | §24.13 never-commit list vs `[security] blocked_patterns` | `S2-T20` evaluates both lists and reports them separately; relationship undecided |

**Gaps found while planning** (already in SPEC-GAPS.md; not decided here):

| ID | Gap | Blocks |
|---|---|---|
| `U-API-15` | §14 has no CLI verb to create/update an intent or report presence (only `intents`, `agents`, `status` listings); intents/presence are MCP (`cowflow_announce_intent`, `cowflow_report_status`) and API only | a product CLI for those steps. Until decided, tests and the demo drive them with the test-only `simagent` (`S2-T25`), which calls the same services the MCP server will call in Sprint 3 |
| `U-API-16` | Caller identity for the CLI in daemonless mode (origin of `request send`, actor of `accept`/`deliver`, session of a publish). §5.4/§24.14 say the lead can do everything via CLI, audited | `S2-T21` ships `ActorLead` only for the CLI; session actors exist in the services |
| `U-API-17` | §14 `request` flags omit fields §24.11 needs: `eta_minutes` on `accept`, reason on `reject`, request priority | CLI flags for these in `S2-T23`; services accept the fields |
| `U-DATA-7` | Enumerated states of intents (only "active" is named, §24.9) and of `sync_attempts` (only `conflict` is named, §18); `sync.*` sub-event names | constants in `S2-T01`, `S2-T16`; code uses named constants so values change in one place |
| `U-GIT-9` | `deny` entry grammar (literal vs regex, glob dialect) | matcher in `S2-T20` (`DenyMatcher` interface) |

---

## 4. Concepts you need

**Intents and overlap.** An intent is a session's public statement "I am
going to touch these paths for this goal" (§9.2): task, session, workspace,
goal, `paths`, artifacts, dependencies, contracts, `base_sha`, state. It does
**not** reserve anything. When two active intents (or an intent and a
published change) cover overlapping paths, COWFlow emits `intent.overlap` to
**both** sessions and carries on. One active intent per session; changing
goals means updating it (§24.9). No intent → no publish (§2.2.3). See §9.2, §24.9.

**Cross-request state machine.** A cross-request is a tracked ask from one
task to another: kind (`implement | expose_interface | publish | review | test
| confirm_contract | fix_incompat`), `expected_output` (`commit | interface |
test | contract | answer`), origin, destination, state, comments. States move
`open → accepted → delivered → closed`; `rejected` only from `open` or
`accepted`. Every other move is refused. `deliver` needs a `change_id` unless
the output is `answer`; `reject` needs a reason (§24.11). Encode the legal
moves as a table, not as scattered `if`s. See §9.3, §24.11.

**Contracts.** A contract is a Markdown file `contracts/<slug>.md` with a
header (`Owner`, `Consumers`, `Status: draft | agreed | implemented |
deprecated`) and sections Interface, Errors, Examples, Changelog (§24.8). It
holds signatures, not code. Changing an `agreed` contract requires a
`confirm_contract` request to every consumer before publish; the change emits
`contract.changed` to dependents and its promotion needs the lead (§9.5).
`contract_refs(contract_path, task_id)` records who depends on what. See §9.5, §24.8.

**Invariants.** Project rules declared once in TOML (§9.6). `kind = "pattern"`
rules have `deny` entries like `internal/**: import "x/cmd` — a path glob and
a forbidden text — and are checked against the published change without a
workspace. `kind = "cmd"` rules run a command and are a Sprint 5 workspace
gate. With `check_on_publish = true`, a violated `pattern` invariant makes
publish fail with a diagnostic and an `invariant.violated` event. See §9.6.

**Transactional outbox.** Every state change and the event describing it are
written in **one** SQLite transaction: `UPDATE cross_requests …` +
`INSERT INTO events …` + `INSERT INTO outbox …`, then `COMMIT`. Either both
exist or neither does, so a crash can never produce a state without its event
or an event without its state. Later (Sprint 4) the daemon drains `outbox` to
SSE and the watchdog; in daemonless mode readers simply poll `events`.
Delivery is **at-least-once**: consumers must tolerate seeing an event twice.
See §9.9, §13, [Microservices.io: Transactional outbox](https://microservices.io/patterns/data/transactional-outbox.html).

**Event cursors, ACK, dedup.** A consumer remembers "the last event I
processed" as an opaque **cursor** and asks "give me events after this
cursor". ACK stores that cursor durably (`event_deliveries`) so a restarted
consumer resumes where it stopped. Dedup = the consumer drops an event whose
ID it has already seen (redelivery after a crash). Careful: ULIDs minted in
two processes are not ordered by commit time, so "events with ID > X" can
skip an event committed late with a smaller ULID — that is exactly gap
`U-DATA-3`. Guidance (store skill): order by a sequence assigned inside the
write transaction. See §9.9, §18 "Lost event".

**Derived index.** `path_index`, `contract_refs`, `change_ancestry` (§8.3) are
*caches* computed from `intents`, `changes`, `change_files`. They are updated
in the same transaction as the intent/publish that causes them, and
`cowflow index rebuild` deletes and recomputes them. Because the manifest is
stored per file at publish, questions like "who touches `src/auth`?" are
answered by SQL — no Git process (§20.23). If rebuild and incremental results
ever differ, that is a bug. See §8.2, §8.3.

**Git refs and retention refs.** A ref is a name pointing at a commit
(`refs/heads/develop`, `refs/tags/v1`). Publishing creates
`refs/cowflow/changes/<id>` (namespace from `[git] shared_ref_namespace`)
pointing at the published commit. It keeps the commit alive even if the task
branch moves or is deleted, and it is what others sync from. It never merges
into the target branch (§8.1). Refs are global state, so exactly one per
change (§2.2.13). Create it atomically with
`git update-ref <ref> <sha> ""` (empty old value = "must not exist yet"). See
§8.1, [git-update-ref](https://git-scm.com/docs/git-update-ref).

**Merge vs cherry-pick vs squash.** `merge` (default) brings the change's
commits into your branch and keeps their identity (same SHAs) — later merges
of the same change are no-ops. `cherry-pick` re-creates one commit on your
branch with a new SHA (trailer `x-cowflow-source` records where it came from).
`squash` applies all of the change's diff as one new local commit. Only
`merge` preserves identity, which is why it is the default (§23 decision 7).
See §8.4, [git-merge](https://git-scm.com/docs/git-merge), [git-cherry-pick](https://git-scm.com/docs/git-cherry-pick).

**Conflict detection and abort.** When Git cannot combine two edits of the
same lines it stops mid-operation, leaves conflict markers, records
`MERGE_HEAD`/`CHERRY_PICK_HEAD`, and lists the files as *unmerged*
(`git diff --name-only --diff-filter=U`). COWFlow must then record the attempt
as `conflict` with those files, overwrite nothing, and offer `sync abort`,
which returns the workspace to exactly the pre-sync `HEAD` (§18 "Sync
conflict"). A non-zero exit is not always a conflict (locked file, disk full):
check for unmerged files before calling it one. See §8.4, §18.

**Multi-process SQLite.** In daemonless mode several `cowflow` processes write
one file. WAL lets readers proceed while one writer writes; `busy_timeout`
makes a second writer wait instead of failing; `BEGIN IMMEDIATE` takes the
write lock at the start of the transaction, avoiding the "deferred tx upgrades
to write and fails with `SQLITE_BUSY`" trap. Rule: keep write transactions
short and **never** hold one open across a Git subprocess. Pragmas go in the
DSN so every pooled connection gets them. See §13, store-and-events skill,
[SQLite WAL](https://www.sqlite.org/wal.html).

---

## 5. Agents and lanes

| Agent | Tasks | Epics |
|---|---|---|
| `AG-LEAD` | day-1 kickoff, gap decisions, contract freeze, all reviews, demo sign-off | — |
| `AG-STORE` | `S2-T01` `S2-T02` `S2-T03` `S2-T04` `S2-T05` `S2-T06` `S2-T07` `S2-T08` | `S2-E1`, `S2-E2`, `S2-E6` |
| `AG-GIT` | `S2-T13` `S2-T14` `S2-T15` `S2-T16` `S2-T17` | `S2-E5`, `S2-E7` |
| `AG-COLLAB` | `S2-T09` `S2-T10` `S2-T11` `S2-T12` `S2-T18` `S2-T19` `S2-T20` | `S2-E3`, `S2-E4`, `S2-E8`, `S2-E9` |
| `AG-CLI` | `S2-T21` `S2-T22` `S2-T23` `S2-T24` | `S2-E10` |
| `AG-QA` | `S2-T25` `S2-T26` | `S2-E11` |
| `AG-SEC` | reviews every `Security: yes` task | — |

```text
Day        D1   D2   D3   D4   D5   D6   D7   D8   D9   D10
AG-LEAD   [KO+contracts]  ......... reviews / merges .........  [demo][close]
AG-STORE  [T01-----][T02][T03-----][T04][T05][T07-----][T08][T06]
AG-GIT    [T13-----]     [T14-----][T15-----][T16-----][T17----]
AG-COLLAB      [T09][T10-----][T11-----][T12][T18-----][T20][T19]
AG-CLI         [T21-----][T22-----]     [T23-----][T24---------]
AG-QA          [T25------------------][T26--------------------------]
AG-SEC              ^T13  ^T11 ^T14   ^T15 ^T16 ^T20  ^T17 ^T21 ^T24
Legend: [Tnn---] = task in progress; ^Tnn = security review of that PR
```

Consumers start against fakes on the day the contracts freeze (D1–D2): the
in-memory `storefake` (delivered with `S2-T01`), `gittest.FakeRunner`
(`S0-E8`) and a `PublishPolicy` stub.

**Critical path:** `S2-T01 → S2-T02 → S2-T03 → S2-T04 → S2-T07 (RecordPublish) → S2-T15 → S2-T16 → S2-T17 → S2-T24 → S2-T26`.
Any slip on `AG-STORE` before D5 slips the demo; `AG-LEAD` moves `S2-T06` to
`AG-QA` if needed.

---

## 6. Contracts

Frozen by `AG-LEAD` on day 1. Changing a signature after D2 needs a comment
on the owning task and `AG-LEAD` approval. Names of types/functions are
Guidance; the spec strings inside them (states, kinds, event names, table
columns from §8.3) are not.

### 6.1 Domain (`internal/domain`, provider `AG-STORE`, consumers: everyone)

```go
package domain

type ID string
type IDKind string // "project","task","session","intent","request","comment","change","sync","contract","event"

// IDGen mints prefixed ULIDs (§13). The prefix per kind is U-DATA-2: the table
// lives in one place (ids.go) and code never hardcodes a prefix.
type IDGen interface{ New(k IDKind) ID }

type Clock interface{ Now() time.Time; NewTicker(d time.Duration) Ticker }

type ActorKind string
const (
	ActorLead    ActorKind = "lead"    // §5.4, §24.14: the lead can do everything via CLI (audited)
	ActorSession ActorKind = "session" // a worker session (MCP in S3, simagent in tests)
)
type Actor struct {
	Kind        ActorKind
	SessionID   ID // empty for ActorLead
	TaskID      ID
	WorkspaceID ID
}

type PresenceState string // §9.1, exact strings
const (
	PresenceStarting   PresenceState = "starting"
	PresenceWorking    PresenceState = "working"
	PresenceWaiting    PresenceState = "waiting"
	PresenceBlocked    PresenceState = "blocked"
	PresencePublishing PresenceState = "publishing"
	PresenceSyncing    PresenceState = "syncing"
	PresenceValidating PresenceState = "validating"
	PresenceCompleted  PresenceState = "completed"
	PresenceFailed     PresenceState = "failed"
	PresenceStopped    PresenceState = "stopped"
)

type RequestState string   // open|accepted|delivered|closed|rejected (§9.3)
type RequestKind string    // implement|expose_interface|publish|review|test|confirm_contract|fix_incompat
type ExpectedOutput string // commit|interface|test|contract|answer
type ChangeState string    // published|validated|promoted|superseded|discarded (§8.5)
type ContractStatus string // draft|agreed|implemented|deprecated (§24.8)
type SyncStrategy string   // CLI values merge|cherry-pick|squash (§14); stored value of squash = C4
type IntentState string    // only IntentActive is named by the spec (U-DATA-7)
type SyncState string      // only SyncConflict ("conflict") is named by the spec (U-DATA-7)

type EventType string
const (
	EvPresenceChanged   EventType = "agent.presence_changed"
	EvIntentCreated     EventType = "intent.created"
	EvIntentUpdated     EventType = "intent.updated"
	EvIntentOverlap     EventType = "intent.overlap"
	EvChangePublished   EventType = "change.published"
	EvChangeSuperseded  EventType = "change.superseded"
	EvChangeDiscarded   EventType = "change.discarded"
	EvContractChanged   EventType = "contract.changed"
	EvInvariantViolated EventType = "invariant.violated"
	// cross_request.* (U-DATA-4) and sync.* (U-DATA-7) live in events_pending.go,
	// filled in once AG-LEAD records the names.
)

type Event struct {
	ID              ID // ULID: identity and dedup key
	Type            EventType
	ProjectID       ID
	TaskID          ID
	SessionID       ID // emitter, if any
	TargetSessionID ID // addressee, e.g. each side of intent.overlap; empty = broadcast
	TargetTaskID    ID // addressee task, e.g. contract.changed dependents
	SubjectID       ID // intent/request/change/sync the event is about
	Payload         json.RawMessage
	CreatedAt       time.Time
}

// Cursor is opaque to consumers (encoding = U-DATA-3). Never parse it outside internal/store.
type Cursor string

type PathHit struct {
	PathPrefix string; TaskID, IntentID, ChangeID ID; Kind string; UpdatedAt time.Time
}
```

Sentinel errors (`internal/domain/errors.go`) and their exit codes (§14), mapped once in `internal/cli/exit.go`:

| Error | Exit |
|---|---|
| `ErrNoIntent`, `ErrInvariantViolated`, `ErrBlockedPath`, `ErrProtectedBranch`, `ErrCommitFormat`, `ErrContractUnconfirmed`, `ErrMissingExpectedOutput`, `ErrRejectWithoutReason`, `ErrDeliverWithoutChange`, `ErrNotLead` | `4` refused by policy |
| `ErrInvalidTransition`, `ErrNotFound`, `ErrIntentExists`, `ErrDirtyWorkspace`, `ErrNothingToAbort` | `2` state |
| `ErrSyncConflict` | `3` conflict |

### 6.2 Store (`internal/store`, provider `AG-STORE`, consumers `AG-COLLAB`, `AG-GIT`, `AG-CLI`, `AG-QA`)

```go
package store

type Store interface {
	// WithTx runs fn in one BEGIN IMMEDIATE transaction. If opID was already
	// applied, fn is not run and ErrOpReplayed-wrapped stored result is returned.
	WithTx(ctx context.Context, opID domain.ID, fn func(Tx) error) error
	Read() Reader
	Events() EventReader
	Index() IndexQuery
}

type Tx interface {
	Sessions() SessionRepo
	Presence() PresenceRepo
	Intents() IntentRepo
	Requests() RequestRepo
	Changes() ChangeRepo
	Syncs() SyncRepo
	Contracts() ContractRepo
	IndexWriter() IndexWriter
	Emit(ev domain.Event) error         // INSERT events + outbox, same tx (§9.9)
	Audit(e domain.AuditEntry) error    // audit_log, same tx (§17)
}

type EventReader interface {
	After(ctx context.Context, projectID domain.ID, after domain.Cursor, f EventFilter, limit int) ([]domain.Event, domain.Cursor, error)
	Ack(ctx context.Context, consumer string, projectID domain.ID, c domain.Cursor) error
	Acked(ctx context.Context, consumer string, projectID domain.ID) (domain.Cursor, error)
}
type EventFilter struct {
	Types           []domain.EventType // exact or "prefix.*"
	TargetSessionID domain.ID
	TargetTaskID    domain.ID
	SubjectID       domain.ID
}

type IndexQuery interface {
	WhoPath(ctx context.Context, projectID domain.ID, path string) ([]domain.PathHit, error)
	Overlaps(ctx context.Context, projectID domain.ID, paths []string, excludeSession domain.ID) ([]domain.PathHit, error)
	Rebuild(ctx context.Context, projectID domain.ID) (RebuildStats, error)
}
```

Fakes: `internal/store/storefake` (in-memory, same interfaces, for unit tests
of collab/git/cli until `S2-T03` lands). Guidance: once the real store exists,
prefer a real SQLite file in `t.TempDir()` — it is fast and catches SQL bugs a
fake hides. Keep the fake for pure-logic tests.

### 6.3 Change publisher (`internal/git`, provider `AG-GIT`, consumers `AG-CLI`, `AG-QA`, later `AG-MCP`/`AG-API`)

```go
package git

type ChangePublisher interface {
	Publish(ctx context.Context, in PublishInput) (PublishResult, error)
	Discard(ctx context.Context, opID domain.ID, actor domain.Actor, changeID domain.ID, reason string) error
}
type PublishInput struct {
	OpID      domain.ID
	Actor     domain.Actor
	ProjectID domain.ID
	TaskID    domain.ID
	RequestID domain.ID // optional; Cowflow-Request trailer (U-GIT-2)
}
type PublishResult struct {
	Change   domain.Change
	Warnings []Warning // e.g. scope.exceeded (§24.9), manifest size (§24.10)
	Overlaps []domain.PathHit
}

// Implemented by AG-COLLAB (collab.PublishGate). Called after the manifest is
// built and BEFORE the retention ref exists. A non-nil error refuses publish.
type PublishPolicy interface {
	CheckPublish(ctx context.Context, c PublishCandidate) (PolicyReport, error)
}
type PublishCandidate struct {
	Actor      domain.Actor
	ProjectID  domain.ID
	TaskID     domain.ID
	Workspace  domain.Workspace
	HeadSHA    string
	BaseSHA    string
	Manifest   Manifest
	BlobReader BlobReader // reads blob_after content for pattern invariants/contracts
}
type BlobReader interface{ Blob(ctx context.Context, sha string) ([]byte, error) }

// Implemented by AG-STORE (S2-T07). One transaction: changes, change_files,
// supersede, index, ancestry, events, audit.
type ChangeRecorder interface {
	RecordPublish(ctx context.Context, opID domain.ID, rec PublishRecord) (domain.Change, error)
}

// U-GIT-1: which commit the manifest diffs against. Injected; no default until decided.
type DiffBaseResolver interface {
	DiffBase(ctx context.Context, task domain.Task, ws domain.Workspace, head string) (string, error)
}
```

### 6.4 Sync service (`internal/git`, provider `AG-GIT`, consumers `AG-CLI`, `AG-QA`)

```go
type SyncService interface {
	Sync(ctx context.Context, in SyncInput) (domain.SyncAttempt, error) // ErrSyncConflict on conflict (attempt still returned)
	Status(ctx context.Context, syncID domain.ID) (domain.SyncAttempt, error)
	Abort(ctx context.Context, opID domain.ID, actor domain.Actor, syncID domain.ID) (domain.SyncAttempt, error)
}
type SyncInput struct {
	OpID        domain.ID
	Actor       domain.Actor
	ChangeID    domain.ID
	WorkspaceID domain.ID           // explicit --into (§14); MCP derives it from the token (§15)
	Strategy    domain.SyncStrategy // empty → [git] default_sync_strategy
	Reason      string              // §24.10 --reason
}
```

### 6.5 Collaboration service (`internal/collab`, provider `AG-COLLAB`, consumers `AG-CLI`, `AG-QA`, later `AG-MCP`/`AG-API`)

```go
type Service interface {
	StartSession(ctx context.Context, opID domain.ID, taskID domain.ID) (domain.Session, error)
	ReportStatus(ctx context.Context, opID domain.ID, a domain.Actor, s domain.PresenceState, reason string) error
	AnnounceIntent(ctx context.Context, opID domain.ID, a domain.Actor, in IntentInput) (domain.Intent, []domain.PathHit, error)
	ExpandScope(ctx context.Context, opID domain.ID, a domain.Actor, intentID domain.ID, add []string) (domain.Intent, []domain.PathHit, error)
	SendRequest(ctx context.Context, opID domain.ID, a domain.Actor, in RequestInput) (domain.Request, error)
	Accept(ctx context.Context, opID domain.ID, a domain.Actor, id domain.ID, etaMinutes *int) error
	Reject(ctx context.Context, opID domain.ID, a domain.Actor, id domain.ID, reason string) error
	Deliver(ctx context.Context, opID domain.ID, a domain.Actor, id, changeID domain.ID) error
	Close(ctx context.Context, opID domain.ID, a domain.Actor, id domain.ID) error
	Comment(ctx context.Context, opID domain.ID, a domain.Actor, id domain.ID, body string) error
	Reassign(ctx context.Context, opID domain.ID, a domain.Actor, id, toTask domain.ID) error
	WaitEvents(ctx context.Context, q WaitQuery) (WaitResult, error) // polling, timeout ≤ 300 s (§15.1)
}

// SparseUpdater comes from S1-E5 (AG-WS); collab calls it from ExpandScope (§6.6).
type SparseUpdater interface{ SetScope(ctx context.Context, ws domain.Workspace, paths []string) error }
```

### 6.6 CLI backend (`internal/cli`, provider `AG-CLI`)

```go
// Backend is what commands call. S2 ships only the daemonless implementation
// (local.go) wiring store + git + collab in-process. S4 adds the API client.
// How the CLI picks one is U-API-2.
type Backend interface {
	Collab() collab.Service
	Publisher() git.ChangePublisher
	Sync() git.SyncService
	Store() store.Store
}
type ActorResolver interface{ Resolve(ctx context.Context) (domain.Actor, error) } // S2: always ActorLead (U-API-16)
```

---

## 7. Tasks

### S2-T01 — Domain model for the collaboration plane

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-STORE` | `AG-LEAD`, `AG-GIT` | `S2-E1` | `S0-E3` | `feat/domain-collab-model` | no |

**Skills:** `store-and-events`, `collaboration-protocol`, `go-conventions`, `gitflow`   **Spec:** §8.5, §9.1–9.3, §9.5, §9.9, §13
**Files:** `internal/domain/{ids.go,actor.go,presence.go,intent.go,request.go,change.go,contract.go,sync.go,event.go,events_pending.go,errors.go}`, `internal/store/storefake/fake.go`

**Why / what** — Every other task in the sprint imports these types. Pure Go,
no I/O (`domain` imports nothing internal). Getting the exact spec strings
right here means nobody re-types `"expose_interface"` by hand later.

**Steps**
1. `git switch develop && git pull --ff-only && git switch -c feat/domain-collab-model`.
2. Write the types from Contracts §6.1. Each enum gets a `Valid() bool` and a
   `ParseX(string) (X, error)` used by CLI flag parsing.
3. Request transition table (pure function, used by `S2-T11`):
   ```go
   var requestNext = map[RequestState][]RequestState{
       RequestOpen:      {RequestAccepted, RequestRejected},
       RequestAccepted:  {RequestDelivered, RequestRejected},
       RequestDelivered: {RequestClosed},
   }
   func CanTransition(from, to RequestState) bool { return slices.Contains(requestNext[from], to) }
   ```
   Lead `close` from other states is **not** in this table (`U-SCHED-6` "Lead
   `close` from any request state"); it is a separate, audited path in `S2-T11`.
4. Change transitions: `published → validated → promoted`; `published|validated → superseded|discarded`.
5. `ids.go`: `IDGen` with `ulid.Monotonic(crypto/rand.Reader, 0)` per process,
   guarded by a mutex. Prefix map `map[IDKind]string` left **empty with a
   TODO referencing U-DATA-2**; until decided, `New` returns the bare ULID.
6. `events_pending.go`: placeholders `// U-DATA-4: cross_request.* names` and
   `// U-DATA-7: sync.* names`; no constants yet.
7. `storefake`: in-memory implementation of `store.Store` interfaces (map +
   mutex; `Emit` appends to a slice with an incrementing counter as cursor).

**Tests** — `internal/domain/*_test.go`: table tests for every enum value
(exact strings), all 25 request `from×to` pairs against `CanTransition`,
change transitions. `go test -race ./internal/domain/...`.

**Acceptance**
- [ ] All spec strings match §8.5, §9.1, §9.3, §9.9, §24.8 character for character.
- [ ] `internal/domain` has no imports from `internal/*`.
- [ ] No ID prefix, `cross_request.*` or `sync.*` name invented.
- [ ] `storefake` compiles against the frozen interfaces.

**Pitfalls** — `type PresenceState string` constants declared without the
type on every line become untyped strings; declare each with the type.
Do not put `time.Now()` in constructors; take a `Clock`.

---

### S2-T02 — Migrations for collaboration tables

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-STORE` | `AG-LEAD`, `AG-GIT` | `S2-E1` | `S2-T01` | `feat/store-collab-schema` | no |

**Skills:** `store-and-events`, `go-conventions`, `gitflow`   **Spec:** §8.2, §8.3, §9.1–9.5, §13
**Files:** `internal/store/migrations/00NN_collab.sql` (next free number), `internal/store/migrate_test.go`

**Why / what** — Creates (or extends, if `S0-E3`/`S1-E8` already created
them) the §13 tables this sprint needs: `task_dependencies`, `contracts`,
`invariants`, `agent_sessions`, `presence`, `intents`, `cross_requests`,
`cross_request_comments`, `changes`, `change_files`, `sync_attempts`,
`path_index`, `contract_refs`, `change_ancestry`, `audit_log`. Events tables
are `S2-T04`.

**Steps**
1. Check which tables exist: `grep -n "CREATE TABLE" internal/store/migrations/*.sql`.
   Never edit an applied migration; add `ALTER TABLE` in a new file.
2. Column sets: §8.3 is exact for the index tables; the rest derive from the
   field lists in §9.1–9.3/§8.2 and are Guidance:
   ```sql
   CREATE TABLE intents (
     id TEXT PRIMARY KEY, project_id TEXT NOT NULL, task_id TEXT NOT NULL,
     session_id TEXT NOT NULL, workspace_id TEXT, goal TEXT NOT NULL,
     paths_json TEXT NOT NULL,          -- scope (paths)
     artifacts_json TEXT NOT NULL DEFAULT '[]', dependencies_json TEXT NOT NULL DEFAULT '[]',
     contracts_json TEXT NOT NULL DEFAULT '[]', base_sha TEXT, stub INTEGER NOT NULL DEFAULT 0,
     stub_json TEXT,                    -- SCOPE.md §23 Open question 2: stub format open; store opaque JSON
     touches_deps INTEGER NOT NULL DEFAULT 0,
     state TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
   );
   -- one active intent per session (§24.9)
   CREATE UNIQUE INDEX intents_one_active ON intents(session_id) WHERE state = 'active';

   CREATE TABLE cross_requests (
     id TEXT PRIMARY KEY, project_id TEXT NOT NULL,
     from_task_id TEXT, from_actor TEXT NOT NULL,   -- 'lead' or session id
     to_task_id TEXT NOT NULL, kind TEXT NOT NULL, title TEXT NOT NULL,
     expected_output TEXT NOT NULL, priority TEXT,  -- U-API-17
     state TEXT NOT NULL, eta_minutes INTEGER, change_id TEXT, due_at TEXT,
     created_at TEXT NOT NULL, updated_at TEXT NOT NULL
   );
   CREATE TABLE changes (
     id TEXT PRIMARY KEY, project_id TEXT NOT NULL, task_id TEXT NOT NULL,
     session_id TEXT, workspace_id TEXT NOT NULL, ref TEXT NOT NULL UNIQUE,
     head_sha TEXT NOT NULL, base_sha TEXT NOT NULL, diff_base_sha TEXT NOT NULL,
     state TEXT NOT NULL, stat_files INTEGER, stat_add INTEGER, stat_del INTEGER,
     touches_contracts_json TEXT NOT NULL DEFAULT '[]', warnings_json TEXT NOT NULL DEFAULT '[]',
     published_at TEXT NOT NULL, op_id TEXT
   );
   CREATE TABLE change_files (
     change_id TEXT NOT NULL REFERENCES changes(id), path TEXT NOT NULL, op TEXT NOT NULL,
     blob_before TEXT, blob_after TEXT, add_lines INTEGER, del_lines INTEGER,
     PRIMARY KEY (change_id, path)
   );
   CREATE TABLE path_index (path_prefix TEXT NOT NULL, task_id TEXT NOT NULL,
     intent_id TEXT, change_id TEXT, kind TEXT NOT NULL, updated_at TEXT NOT NULL);
   CREATE INDEX path_index_prefix ON path_index(path_prefix);
   CREATE TABLE contract_refs (contract_path TEXT NOT NULL, task_id TEXT NOT NULL,
     PRIMARY KEY (contract_path, task_id));
   CREATE TABLE change_ancestry (change_id TEXT PRIMARY KEY, parent_change_id TEXT);
   CREATE TABLE sync_attempts (
     id TEXT PRIMARY KEY, change_id TEXT NOT NULL, workspace_id TEXT NOT NULL,
     strategy TEXT NOT NULL, reason TEXT, state TEXT NOT NULL,
     pre_head_sha TEXT NOT NULL, post_head_sha TEXT, conflict_files_json TEXT,
     started_at TEXT NOT NULL, finished_at TEXT, op_id TEXT
   );
   ```
   `add`/`del` are SQL-safe as column names in SQLite but confusing; Guidance: `add_lines`/`del_lines`, exposed as `add`/`del` in JSON (§8.2).
3. Times as RFC 3339 UTC text from the injected clock (sortable, readable).
4. Indexes for the hot queries: `cross_requests(project_id, to_task_id, state)`,
   `changes(task_id, state)`, `intents(project_id, state)`.

**Tests** — migrate from empty; migrate twice (no-op); open an `S1`-era DB
fixture and migrate forward; partial unique index refuses a second active
intent for one session.

**Acceptance**
- [ ] §8.3 columns exactly as specified; extra columns only where noted.
- [ ] Migration applies on all three OSes under `CGO_ENABLED=0`.
- [ ] No edit to an already-merged migration file.

**Pitfalls** — `CREATE UNIQUE INDEX … WHERE` (partial index) needs the
literal `'active'` value; if U-DATA-7 renames it, a new migration recreates the
index. Do not add `FOREIGN KEY` to `path_index` — it is derived and must be
deletable in bulk by `index rebuild`.

---

### S2-T03 — Repositories, transactions, `op_id`, audit

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-STORE` | `AG-LEAD`, `AG-GIT`, `AG-SEC` | `S2-E1` | `S2-T02` | `feat/store-collab-repos` | yes |

**Skills:** `store-and-events`, `security-review`, `go-conventions`, `gitflow`   **Spec:** §13, §17, §18
**Files:** `internal/store/{tx.go,ops.go,audit.go,sessions.go,presence.go,intents.go,requests.go,changes.go,syncs.go,contracts.go}`

**Why / what** — Gives services typed, transactional access to the tables,
plus `op_id` idempotency (§18 "idempotent `op_id`") and `audit_log` rows for
publish/sync/reassign (§17).

**Steps**
1. `WithTx`: `db.BeginTx(ctx, nil)` on a DSN with `_txlock=immediate`; defer
   rollback; on `fn` success commit. Wrap `SQLITE_BUSY` after `busy_timeout`
   as `ErrBusy` (retryable, exit `2`).
2. `op_id` (U-DATA-1 decides storage). Guidance pending decision:
   ```sql
   CREATE TABLE operations (op_id TEXT PRIMARY KEY, kind TEXT NOT NULL,
     result_id TEXT, created_at TEXT NOT NULL);
   ```
   Inside the tx: `INSERT … ON CONFLICT(op_id) DO NOTHING RETURNING op_id`;
   no row returned → replay → return `ErrOpReplayed{ResultID}` without
   running `fn`.
3. One repo per table; methods take/return domain types. Example:
   ```go
   func (r intentRepo) Active(ctx context.Context, sessionID domain.ID) (domain.Intent, error) {
       row := r.tx.QueryRowContext(ctx, `SELECT … FROM intents WHERE session_id = ? AND state = ?`,
           sessionID, domain.IntentActive)
       …
   }
   ```
4. `Audit(e)`: `audit_log(id, at, actor_kind, actor_id, action, subject_id, detail_json)` (Guidance columns).
   Never store tokens or file contents in `detail_json`.
5. Prepared statements per tx are fine; do not cache across connections.

**Tests** — repo round-trips; tx rollback leaves no row; `op_id` replay
returns the original result and does not double insert; two goroutines with
two `*sql.DB` on one file both commit (no `SQLITE_BUSY` surfaced).

**Acceptance**
- [ ] Every mutating repo method only runs inside `WithTx`.
- [ ] `op_id` replay test green; storage decision for U-DATA-1 linked in the PR.
- [ ] Audit rows contain no secrets (reviewed by `AG-SEC`).

**Pitfalls** — Passing `context.Background()` into queries inside a tx hides
cancellation. A tx left open by an early `return` without rollback locks
every other process until `busy_timeout`.

---

### S2-T04 — Event store and transactional outbox

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-STORE` | `AG-LEAD`, `AG-GIT` | `S2-E2` | `S2-T03` | `feat/store-event-outbox` | no |

**Skills:** `store-and-events`, `testing-harness`, `go-conventions`, `gitflow`   **Spec:** §9.9, §13, §18
**Files:** `internal/store/migrations/00NN_events.sql`, `internal/store/outbox.go`, `internal/store/outbox_test.go`

**Why / what** — `Tx.Emit` is how every state change announces itself.
State + event in one transaction is the core guarantee (§13).

**Steps**
1. Migration (columns = U-DATA-1; Guidance until decided):
   ```sql
   CREATE TABLE events (
     seq INTEGER PRIMARY KEY AUTOINCREMENT, -- commit order under BEGIN IMMEDIATE
     id TEXT NOT NULL UNIQUE,               -- ULID (identity, dedup)
     project_id TEXT NOT NULL, type TEXT NOT NULL,
     task_id TEXT, session_id TEXT, target_session_id TEXT, target_task_id TEXT,
     subject_id TEXT, payload TEXT NOT NULL, created_at TEXT NOT NULL
   );
   CREATE INDEX events_project_seq ON events(project_id, seq);
   CREATE TABLE outbox (event_seq INTEGER PRIMARY KEY REFERENCES events(seq),
     state TEXT NOT NULL DEFAULT 'pending', attempts INTEGER NOT NULL DEFAULT 0,
     next_attempt_at TEXT);
   CREATE TABLE event_deliveries (consumer TEXT NOT NULL, project_id TEXT NOT NULL,
     acked_seq INTEGER NOT NULL, updated_at TEXT NOT NULL,
     PRIMARY KEY (consumer, project_id));
   ```
   Why `seq` works: with `BEGIN IMMEDIATE` only one writer holds the lock from
   begin to commit, so `seq` values are allocated in commit order across
   processes. A ULID minted *before* the lock is not.
2. `Emit`: validate `Type` is known (constants + decided pending names), mint
   ULID, `json.Marshal` payload (≤ 64 KiB, Guidance), insert `events` then `outbox`.
3. Payloads carry IDs and small facts, never file contents or tokens.
4. Nobody drains `outbox` in this sprint (daemon = `S4-E8`). Document that in
   the package comment.

**Tests** — atomicity: `fn` emits then returns an error → zero `events`
rows, zero state rows; harness crash point `outbox.after_write_before_delivery`
(`S0-E8`) → after reopen the event is readable by `After` exactly once per
cursor; unknown event type rejected.

**Acceptance**
- [ ] No code path writes `events` outside `Tx.Emit`.
- [ ] Atomicity and crash tests green with `-race`.
- [ ] Column decision for U-DATA-1 linked in PR description.

**Pitfalls** — Emitting from a goroutine or after `Commit` breaks the outbox
guarantee. `AUTOINCREMENT` (not just `INTEGER PRIMARY KEY`) prevents seq reuse
after deletes.

---

### S2-T05 — Cursor polling, ACK, dedup, wait

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-STORE` | `AG-LEAD`, `AG-GIT` | `S2-E2` | `S2-T04` | `feat/store-event-cursor` | no |

**Skills:** `store-and-events`, `testing-harness`, `go-conventions`, `gitflow`   **Spec:** §5.3 (polling), §9.9, §15.1 (`wait_events` ≤ 300 s), §18
**Files:** `internal/store/cursor.go`, `internal/store/events_read.go`, `internal/store/events_read_test.go`

**Why / what** — Daemonless consumers (CLI `events`, the waiting session in
§20.7, later MCP `cowflow_wait_events`) need "events after X" with filters,
durable ACK, and dedup.

**Steps**
1. `Cursor` encoding is U-DATA-3. Implement `encodeCursor(seq int64) Cursor` and
   `decodeCursor(Cursor) (int64, error)` as the only two functions that know
   the format. Guidance until decided: base-10 seq string. Empty cursor = from start.
2. `After`: `SELECT … FROM events WHERE project_id = ? AND seq > ? [filters] ORDER BY seq LIMIT ?`.
   `prefix.*` filter → `type LIKE 'prefix.%'` with the prefix escaped.
   Return the cursor of the last row **scanned**, even if filtered out, so
   filtered consumers do not rescan.
3. `Ack`: upsert `event_deliveries`; never move `acked_seq` backwards.
4. Dedup helper for consumers: `type Seen struct{ ids map[domain.ID]struct{}; order []domain.ID; cap int }` (bounded, Guidance cap 10 000).
5. `collab.WaitEvents` (in `S2-T12`) builds on `After` + clock ticker; this
   task only provides `After`/`Ack`.

**Tests** — 3 consumers with independent cursors see every event once;
filtered read returns advanced cursor; ACK is monotonic; cursor round-trip;
invalid cursor → `ErrInvalidCursor` (exit `1`).

**Acceptance**
- [ ] No caller outside `internal/store` parses a cursor.
- [ ] U-DATA-3 decision linked; switching encoding touches only `cursor.go`.

**Pitfalls** — `LIKE` treats `_` as a wildcard: escape it or use
`substr(type,1,n) = ?`. Returning the last *matching* row's cursor causes
infinite rescans on sparse filters.

---

### S2-T06 — Multi-process concurrency suite

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-STORE` | `AG-LEAD`, `AG-QA` | `S2-E2` | `S2-T05` | `test/store-multiprocess` | no |

**Skills:** `store-and-events`, `testing-harness`, `gitflow`   **Spec:** §5.3, §13, §20.15 (groundwork)
**Files:** `internal/store/multiproc_test.go`

**Why / what** — Goroutines share one process's file locks; real daemonless
use is separate **processes**. This proves WAL + `busy_timeout` + immediate
tx hold with 20 OS processes writing one file (§20.15 is demonstrated in
Sprint 4; this lays its foundation).

**Steps**
1. Re-exec pattern: in `TestMain`, if `os.Getenv("COWFLOW_STORE_CHILD") == "1"`, run the child role and exit.
   ```go
   cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestChild$")
   cmd.Env = append(os.Environ(), "COWFLOW_STORE_CHILD=1", "DB="+path, "N=50", "WHO="+strconv.Itoa(i))
   ```
2. Each of 20 children opens the store and runs 50 `WithTx` that update a row
   and `Emit` an event.
3. Parent reads with `After` in pages of 37 while children run; collects IDs.
4. Assert: 1 000 distinct events, no gaps in per-child counters, no duplicate
   IDs across pages, no child exited with `SQLITE_BUSY`.

**Tests** — this task *is* the test; run with `-race -count=3`.

**Acceptance**
- [ ] Green on Windows, Linux, macOS CI.
- [ ] Runtime < 60 s on CI (tune N).

**Pitfalls** — On Windows, antivirus can hold `-wal` files; use
`t.TempDir()` on the Dev Drive runner when available. Child output must go
to the parent's `t.Log` for debugging.

---

### S2-T07 — Derived index write path and `RecordPublish`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-STORE` | `AG-LEAD`, `AG-GIT` | `S2-E6` | `S2-T04` | `feat/store-derived-index` | no |

**Skills:** `store-and-events`, `git-changes-sync`, `gitflow`   **Spec:** §8.2, §8.3, §8.5, §24.10
**Files:** `internal/store/index.go`, `internal/store/publish_record.go`, tests beside them

**Why / what** — The index must be updated in the **same** transaction as
the intent/publish (§8.3). `RecordPublish` is the single transaction that
makes a publish visible (implements `git.ChangeRecorder`).

**Steps**
1. `IndexWriter.IndexIntent(in)`: delete rows `WHERE intent_id = ?`, insert one
   row per normalized path with `kind = 'intent'` (Guidance value; §8.3 says
   "who declared or changed").
2. `IndexWriter.IndexChange(c, files)`: one row per file path, `kind = 'change'`.
   Guidance: when a change becomes `superseded`/`discarded`, delete its rows so
   the index shows current work only; `Rebuild` must apply the same rule.
3. `contract_refs`: rows from the task's `contracts` and the intent's
   `contracts` (§9.2) — upsert, never duplicate.
4. `change_ancestry`: `parent_change_id` = the task's previous change (chain per task, §8.3).
5. `RecordPublish(opID, rec)` in one `WithTx`:
   1. insert `changes` (`published`), `change_files`;
   2. previous `published` change of the task → `superseded` (§24.10) + `Emit(change.superseded)`;
   3. `IndexChange`, `change_ancestry`;
   4. `Emit(change.published)`; one `contract.changed` per dependent task (list
      computed by `S2-T19` and passed in `rec`); `invariant.violated` is
      emitted on the refusal path, not here;
   5. `Audit("publish")`.
6. Path normalization: forward slashes, no leading `./`, no trailing `/`, Git
   case (case-sensitive). One function `normPath`, shared with `S2-T08`.

**Tests** — publish twice → first `superseded`, ancestry chain correct, index
holds only second change's files; failure injected at step 5.4 → nothing
committed; contract_refs dedup.

**Acceptance**
- [ ] One transaction; no Git call inside it.
- [ ] Supersede automatic and evented.
- [ ] Index rows match `Rebuild` output (asserted again in `S2-T08`).

**Pitfalls** — Holding this tx while the caller still runs `git update-ref`
blocks every other CLI; the publisher creates the ref first, then calls
`RecordPublish` (reconciliation covers a crash in between, `S2-T15`).

---

### S2-T08 — `index rebuild` and Git-free `who --path`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-STORE` | `AG-LEAD`, `AG-GIT` | `S2-E6` | `S2-T07` | `feat/store-index-query` | no |

**Skills:** `store-and-events`, `testing-harness`, `gitflow`   **Spec:** §8.3, §20.23
**Files:** `internal/store/index_query.go`, `internal/store/index_rebuild.go`, `internal/store/index_query_test.go`

**Why / what** — `who --path` and overlap detection read only SQLite
(§20.23). `index rebuild` recomputes everything to recover from drift (§23).

**Steps**
1. Prefix match on path segments (Guidance): query path `p` hits row `r` if
   `r == p`, or `p` starts with `r + "/"` (row declares a parent dir), or
   `r` starts with `p + "/"` (row is inside the queried dir). In SQL:
   ```sql
   SELECT … FROM path_index
   WHERE path_prefix = :p
      OR substr(:p, 1, length(path_prefix) + 1) = path_prefix || '/'
      OR substr(path_prefix, 1, length(:p) + 1) = :p || '/'
   ```
2. `WhoPath` joins `tasks`, `intents`, `changes` for display fields; ordered
   by `updated_at DESC`.
3. `Overlaps(paths, excludeSession)` = union of `WhoPath` per path, excluding
   rows of the caller's own session/intent.
4. `Rebuild`: one tx — delete `path_index`, `contract_refs`, `change_ancestry`
   rows of the project; recompute from active intents, tasks, live changes,
   `change_files`. Return counts.

**Tests**
- `who` with `src/auth` vs rows `src/auth`, `src/auth/jwt.go`, `src/authz` (must **not** match), `src`.
- §20.23: construct the store with a `gittest.RecordingRunner` reachable from
  the same dependency graph; run `WhoPath`/`Overlaps`; `require.Zero(t, rec.Calls())`.
- Equivalence: random sequence of intents/publishes/supersedes → snapshot
  index → `Rebuild` → identical rows (sorted).

**Acceptance**
- [ ] `src/authz` never matches `src/auth`.
- [ ] Zero Git calls asserted (AC#23).
- [ ] Rebuild equals incremental.

**Pitfalls** — `LIKE 'src/auth%'` matches `src/authz`: that is the classic
bug. Windows users type `src\auth`; normalize in the CLI before the query.

---

### S2-T09 — Presence and sessions

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-COLLAB` | `AG-LEAD`, `AG-INT` | `S2-E3` | `S2-T01` (fake), `S2-T03` | `feat/collab-presence` | no |

**Skills:** `collaboration-protocol`, `store-and-events`, `gitflow`   **Spec:** §5.3, §9.1, §22 `[collaboration]`
**Files:** `internal/collab/{service.go,presence.go,presence_test.go}`

**Why / what** — Sessions say what state they are in; `blocked` must carry a
reason (§9.1). Daemonless there is no heartbeat process (`S4-E4`), so TTL is
evaluated on read.

**Steps**
1. `NewService(deps Deps)` with `Store`, `Clock`, `IDGen`, `Config`, `SparseUpdater`; no globals.
2. `StartSession(taskID)`: insert `agent_sessions` + `presence(starting)`, emit `agent.presence_changed`.
3. `ReportStatus`: validate state; `blocked` with empty reason → `ErrMissingReason` (exit `4`);
   write `presence(state, reason, updated_at, expires_at = now + presence_ttl_seconds)`; emit.
4. Reads (`agents`): a row with `expires_at < now` is shown as stale (Guidance
   field `stale: true`). Do **not** emit `session.lost` daemonless — that is a
   daemon capability (§5.3, `S4-E4`).

**Tests** — every §9.1 state accepted; unknown rejected; `blocked` without
reason refused; fake clock advances past TTL → stale on read; event emitted in same tx.

**Acceptance**
- [ ] Exact §9.1 strings; TTL from config, clock injected.
- [ ] No background goroutine.

**Pitfalls** — Do not mark sessions `failed` on TTL expiry; that rewrites
history without a daemon observing the process.

---

### S2-T10 — Intents, `expand_scope`, overlap detection

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-COLLAB` | `AG-LEAD`, `AG-INT`, `AG-SEC` | `S2-E3` | `S2-T09`, `S2-T07` | `feat/collab-intents-overlap` | yes |

**Skills:** `collaboration-protocol`, `store-and-events`, `security-review`, `gitflow`   **Spec:** §6.6, §9.2, §24.4, §24.9
**Files:** `internal/collab/{intents.go,overlap.go,publish_gate.go,intents_test.go}`

**Why / what** — The intent is the entry ticket to publishing and the input
to overlap warnings. Paths come from agents → they are untrusted input.

**Steps**
1. `AnnounceIntent(a, in)`: requires `ActorSession`. Validate `paths`: non-empty
   (Guidance; §24.7 warns on a task without paths, an intent without scope
   is meaningless), each path relative, no `..` segment, no drive letter or
   leading `/`, ≤ 4 096 bytes, normalized with `normPath`.
2. If the session already has an active intent → update it (`intent.updated`),
   never create a second (§24.9). Else insert (`intent.created`).
3. In the **same** tx: `IndexIntent`, `contract_refs` from `in.Contracts`, then
   `Index().Overlaps(paths, a.SessionID)` (read in tx); for each overlapping
   session emit `intent.overlap` twice — `TargetSessionID` = caller and = other side (§9.2 "to both").
   Payload: `{paths, other_task_id, other_intent_id|change_id}`.
4. Return overlaps to the caller; exit code stays `0` (warning, never block).
5. `ExpandScope`: union paths, same tx flow; **after commit** call
   `SparseUpdater.SetScope` (Git work outside the tx). If sparse fails, the
   intent is still widened; return a wrapped warning (Guidance).
6. `publish_gate.go`: `collab.PublishGate` implements `git.PublishPolicy`.
   First check: task has an active intent (lead publishing via CLI: any active
   intent of a session of that task) else `ErrNoIntent`. Leave hook slots for
   `S2-T19` and `S2-T20`. Compute `scope.exceeded` warning: manifest paths not
   covered by intent paths (§24.9) — warning in the report, not an error.

**Tests** — create/update one-active rule; overlap on `src/auth` vs
`src/auth/jwt.go` emits two events, both targets correct; `src/authz` no
overlap; path traversal inputs rejected (table test incl. `..\\x`, `C:/x`,
`/etc`); `ErrNoIntent` from the gate; `scope.exceeded` warning listed.

**Acceptance**
- [ ] Overlap only via `IndexQuery` (no Git).
- [ ] Never blocks; exit `0` with warnings.
- [ ] Path validation reviewed by `AG-SEC`.

**Pitfalls** — Emitting overlap events after commit (in a second tx) loses
them on crash. Calling sparse inside the tx holds the write lock during Git.

---

### S2-T11 — Cross-request lifecycle

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-COLLAB` | `AG-LEAD`, `AG-INT`, `AG-SEC` | `S2-E4` | `S2-T09` | `feat/collab-cross-requests` | yes |

**Skills:** `collaboration-protocol`, `security-review`, `gitflow`   **Spec:** §9.3, §9.4, §12.3, §24.6, §24.11
**Files:** `internal/collab/{requests.go,requests_test.go}`, `internal/domain/events_pending.go` (names after U-DATA-4)

**Why / what** — Traceable collaboration (§2.2.4): origin, destination,
expected result, state. Server-side validation, not caller goodwill.

**Steps**
1. `SendRequest`: validate kind, `expected_output` present and valid (else
   `ErrMissingExpectedOutput`, exit `4`, §24.11), title non-empty, destination
   task exists and differs from origin. `from_actor` = `lead` or session ID.
2. `Accept(eta)`: only the destination task's session or the lead; store `eta_minutes` if given (U-API-17).
3. `Reject(reason)`: empty reason → `ErrRejectWithoutReason`; insert the reason
   as a `cross_request_comments` row in the same tx (§24.6 "carries the reason in a comment").
4. `Deliver(changeID)`: empty `changeID` allowed only when
   `expected_output = answer` (§24.11); else `ErrDeliverWithoutChange`. The
   change must exist, belong to the destination task, and be `published` or later.
5. `Close`: origin side or lead (§24.11 "whoever asks, closes"). From
   `delivered` via the table. Lead closing from another state = U-SCHED-6 →
   refuse with `ErrInvalidTransition` until decided; audit the attempt.
6. `Comment`, `Reassign(toTask)` (lead only → `ErrNotLead`; audited, §17).
7. One generic `transition(ctx, opID, actor, id, to, mutate func(*domain.Request))`:
   load → `domain.CanTransition` → mutate → update → `Emit(cross_request.<name>)` → audit.

**Tests** — table over all from×to pairs via the service; each refusal;
`deliver` with change of another task refused; reassign by non-lead refused;
state + event atomic.

**Acceptance**
- [ ] `rejected` impossible from `delivered`/`closed`.
- [ ] Event names come from the U-DATA-4 decision (no invented names merged).
- [ ] Lead-only paths audited.

**Pitfalls** — Checking "is lead" from a CLI flag instead of the resolved
`Actor`. Forgetting that `answer` deliveries have no change.

---

### S2-T12 — Request SLA bookkeeping and blocked → unblocked flow

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-COLLAB` | `AG-LEAD`, `AG-INT` | `S2-E4` | `S2-T11`, `S2-T05` | `feat/collab-wait-events` | no |

**Skills:** `collaboration-protocol`, `testing-harness`, `gitflow`   **Spec:** §9.3 (waiting), §9.4, §15.1 (`wait_events`), §20.7, §24.11
**Files:** `internal/collab/{wait.go,sla.go,wait_test.go}`

**Why / what** — §20.7: a `blocked` agent must be unblocked by an event.
Daemonless, "unblocked" means: its `WaitEvents` poll returns the event, then
it reports `working`.

**Steps**
1. `WaitEvents(q)`: `q.Timeout` clamped to 300 s (§15.1). Loop: `After(cursor, filter)`;
   if events → return them + new cursor; else wait on `clock.NewTicker(pollInterval)`
   or `ctx.Done()`. Guidance: `pollInterval` 500 ms constant (no config key in §22).
2. Return `{events, cursor, timed_out}`; never block beyond the timeout.
3. `sla.go`: at `SendRequest`, persist `due_at` from priority. Rule = **C9**
   (per-priority 15/60 min vs `escalation_after_minutes`) → `DueAtPolicy`
   interface; whether a daemonless process triggers escalation = U-SCHED-1 → no
   trigger this sprint, only `due_at` and an `overdue` flag on read.

**Tests** — fake clock: session B blocked, waits on `cross_request.*` with
subject = request; another goroutine delivers; wait returns that event before
timeout; timeout returns `timed_out: true` with unchanged cursor; ctx cancel
returns promptly.

**Acceptance**
- [ ] No `time.Sleep`; fake clock drives all tests.
- [ ] C9/U-SCHED-1 left behind interfaces.

**Pitfalls** — Re-reading from the empty cursor each loop returns old events
forever; start from the caller's cursor (or the latest seq at call time).

---

### S2-T13 — Change manifest builder

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-GIT` | `AG-LEAD`, `AG-STORE`, `AG-SEC` | `S2-E5` | `S0-E4` | `feat/git-change-manifest` | yes |

**Skills:** `git-changes-sync`, `security-review`, `testing-harness`, `gitflow`   **Spec:** §8.2, §18.1 (fuzz)
**Files:** `internal/git/{manifest.go,manifest_parse.go,manifest_test.go,manifest_fuzz_test.go}`, `internal/git/testdata/fuzz/`

**Why / what** — One Git call at publish produces the per-file manifest;
after that nobody needs Git for overlap/index (§8.2).

**Steps**
1. Diff base via `DiffBaseResolver` (U-GIT-1). Do not hardcode.
2. One call:
   ```go
   out, err := r.Run(ctx, repo, "diff-tree", "-r", "-z", "--raw", "--numstat",
       "--no-renames", "--end-of-options", from, to)
   ```
   Guidance: `--no-renames` keeps one path per entry (a rename = delete + add);
   AG-LEAD may revisit.
3. Parse NUL-separated output: raw records `:<srcmode> <dstmode> <srcsha> <dstsha> <status>\0<path>\0`
   then numstat records `<add>\t<del>\t<path>\0` (`-\t-` for binary → `add`/`del` = 0, Guidance flag `binary`).
4. Status → `op` (Guidance; spec only shows `modify`): `A→add`, `M→modify`, `D→delete`, `T→modify`.
   Zero SHA (`000…`) → empty `blob_before`/`blob_after`.
5. `stat` = sums; `touches_contracts` = paths under `contracts/` ending `.md` (§24.8).
6. Reject truncated/malformed output with `ErrManifestParse` (no partial manifest).

**Tests** — fixture repo with add/modify/delete/binary/space-in-name/newline-in-name;
harness truncated output → error; fuzz `FuzzParseManifest` with seeds.

**Acceptance**
- [ ] Exactly one `git` invocation per manifest (counted by recorder).
- [ ] Names with spaces/newlines/unicode parsed correctly.
- [ ] Fuzz target with seed corpus committed.

**Pitfalls** — Parsing without `-z` breaks on unusual names. `--numstat` and
`--raw` order in output is raw first, then numstat; do not assume interleaving.

---

### S2-T14 — Retention refs, object transfer, Git-side publish checks

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-GIT` | `AG-LEAD`, `AG-STORE`, `AG-SEC` | `S2-E5` | `S2-T13` | `feat/git-retention-refs` | yes |

**Skills:** `git-changes-sync`, `security-review`, `gitflow`   **Spec:** §8.1, §17, §22 `[git]` `[conventions]`, §24.2, §24.3
**Files:** `internal/git/{refs.go,head.go,commitfmt.go,transfer.go}` + tests

**Why / what** — The pieces of publish that talk to Git, each small and
testable: resolve `HEAD`, refuse protected branches, check commit format,
make the commit reachable in the project repo, create the retention ref.

**Steps**
1. `ResolveHead(ws)`: `rev-parse --verify --end-of-options HEAD^{commit}`;
   branch: `symbolic-ref -q --short HEAD` (detached → `ErrDetachedHead`, exit `2`, Guidance).
2. Protected branches: when `[conventions] enforce_protected_branches` is on,
   branch `main` or `develop` → `ErrProtectedBranch` (§24.2).
3. `commitfmt.go`: when `enforce_commit_format`, every commit in the range
   (range = U-GIT-1) must match `^(feat|fix|chore|refactor|test|docs|perf|build|ci)(\([^)]+\))?!?: .+`,
   summary ≤ 72 chars, no trailing `.` (§24.3). Read messages with
   `log -z --format=%H%x00%B <from>..<to>` (one call).
4. Trailers (`Cowflow-Task`, `Cowflow-Change`, `Cowflow-Request`): **blocked by
   U-GIT-2** — a published commit is immutable (§8.1), so adding trailers means
   either the agent writes them before publish or COWFlow creates a commit.
   Leave `TrailerPolicy` interface with a no-op implementation and a skipped test.
5. `transfer.go`: if the workspace shares the project's object store
   (`git_worktree`, capability from `S1-E1`) nothing to do; otherwise (U-PLAT-1):
   ```go
   r.Run(ctx, projectRepo, "fetch", "--no-tags", "--no-write-fetch-head", "--end-of-options", wsPath, "HEAD")
   ```
   Guidance; then verify `cat-file -e <sha>^{commit}` in the project repo.
   `wsPath` passes through `platform.Confine(workspaceRoot, …)` first.
6. `CreateRetentionRef(id, sha)`:
   ```go
   ref := cfg.Git.SharedRefNamespace + "/" + string(id) // refs/cowflow/changes/<id>
   if err := CheckRefFormat(ctx, r, ref); err != nil { return err }
   _, err := r.Run(ctx, projectRepo, "update-ref", "-m", "cowflow publish", ref, sha, "")
   ```
   Already exists with same SHA → success (idempotent); different SHA → error.

**Tests** — harness repo: detached HEAD; `develop` refused; commit format
table; fetch from a non-shared clone; ref exists twice with same SHA ok;
`check-ref-format` failure for an ID with `..`; fault injection on `update-ref`.

**Acceptance**
- [ ] All argv slices, `--end-of-options` before user-derived refs/paths.
- [ ] Ref creation atomic (`""` old value) and idempotent.
- [ ] Trailer piece left behind interface with U-GIT-2 referenced.

**Pitfalls** — Inherited `GIT_DIR`/`GIT_WORK_TREE` env vars from the user's
shell redirect every call; `S0-E4`'s runner must strip them — verify.
Do not push or touch remotes (§24.14).

---

### S2-T15 — Change publisher orchestration

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-GIT` | `AG-LEAD`, `AG-STORE`, `AG-SEC` | `S2-E5` | `S2-T14`, `S2-T07`, `S2-T10` | `feat/git-change-publish` | yes |

**Skills:** `git-changes-sync`, `collaboration-protocol`, `store-and-events`, `security-review`, `gitflow`   **Spec:** §2.2.3, §8.1, §8.2, §8.5, §15.5, §17, §20.3, §20.4, §24.10
**Files:** `internal/git/publish.go`, `internal/git/publish_test.go`, `internal/git/reconcile.go`

**Why / what** — `change publish` end to end, in the order that keeps Git
and SQLite consistent and refusals cheap.

**Steps**
1. Load task + workspace (read-only, no tx).
2. `ResolveHead`, protected branch check (`S2-T14`).
3. Same `HEAD` as the task's latest live change → Guidance: return that change
   unchanged (idempotent), print "nothing new"; AG-LEAD confirms day 1.
4. `HEAD` not a descendant of the previous change's head
   (`merge-base --is-ancestor`) → **U-GIT-3**: `NonDescendantPolicy` interface;
   no default merged until decided.
5. Manifest (`S2-T13`), commit format (`S2-T14`).
6. `PublishPolicy.CheckPublish` (intent, blocked paths, invariants, contracts).
   On `ErrInvariantViolated`: in its own short tx emit `invariant.violated`
   (payload: invariant name, path, line if known) and audit the refusal; return
   exit `4` with the diagnostic.
7. Object transfer (`S2-T14`), then mint change ID and create the retention ref.
8. `ChangeRecorder.RecordPublish(opID, rec)` with `session_id` = actor's
   session or the task's active-intent session when the lead publishes (§20.3).
9. Crash between 7 and 8 leaves a ref with no row: `reconcile.go` lists
   `for-each-ref --format=%(refname)%00%(objectname) refs/cowflow/changes/` and
   reports orphans; Guidance: daemonless `cowflow change publish` retried with
   the same `op_id` re-uses the existing ref (idempotent update-ref). Full
   reconciliation is `S4-E8`.
10. `Discard(changeID, reason)`: lead only (§24.10), state `discarded`,
    `change.discarded`, index rows removed, audit. The ref is kept (U-GIT-4).

**Tests** — harness: no intent → `ErrNoIntent`, no ref created, no row;
violated invariant → `ErrInvariantViolated`, event present, no ref; happy
path → ref, row with `task_id` and `session_id` (AC#3); second publish
supersedes; crash injected after ref → retry with same `op_id` succeeds once.

**Acceptance**
- [ ] Refusals happen before any ref is created.
- [ ] No SQLite tx open during Git calls.
- [ ] AC#3 and AC#4 (publish part) covered.

**Pitfalls** — Checking policy after creating the ref leaves orphan refs on
every refusal. Emitting `invariant.violated` inside the (rolled-back)
publish tx loses it.

---

### S2-T16 — Sync: merge, cherry-pick, squash

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-GIT` | `AG-LEAD`, `AG-STORE`, `AG-SEC` | `S2-E7` | `S2-T15` | `feat/git-sync-strategies` | yes |

**Skills:** `git-changes-sync`, `security-review`, `gitflow`   **Spec:** §2.2.5, §8.1, §8.4, §17, §22 `[git] default_sync_strategy`, §24.10
**Files:** `internal/git/{sync.go,sync_strategies.go,sync_test.go}`

**Why / what** — Explicit consumption of a published change into a chosen
workspace (§2.2.5), recording SHAs and result (§8.1).

**Steps**
1. Validate: change exists and is not `discarded`; workspace exists; strategy
   valid (`merge` default from config). Only published changes are syncable (§24.10).
2. Workspace must have no tracked modifications:
   `status --porcelain=v1 -z --untracked-files=no` empty, else `ErrDirtyWorkspace`
   (exit `2`). Guidance: this is how "nothing overwritten" (§18) is guaranteed;
   Git itself refuses to overwrite untracked files.
3. Record attempt row (`pre_head_sha`, strategy, reason) in a short tx; emit `sync.<started>` (U-DATA-7 name).
4. Make the change's commit reachable in the workspace (U-PLAT-1, same helper
   as `S2-T14`, reversed direction), then run:
   - `merge`: `merge --no-edit --end-of-options <head_sha>`
   - `cherry-pick`: `cherry-pick --no-commit --end-of-options <head_sha>` then
     `commit --no-edit -C <head_sha> --trailer "x-cowflow-source: <value>"` — value/casing **U-GIT-2**;
     only the head commit is picked ("isolated commit", §8.4).
   - `squash`: `merge --squash --end-of-options <head_sha>` then `commit --no-edit` (Git's `SQUASH_MSG`).
5. Success: `post_head_sha`, state (U-DATA-7 name), event, audit `sync`.
6. Non-zero exit → hand to `S2-T17` conflict classifier.

**Tests** — harness two-workspace repo: merge keeps the original SHA reachable
(`merge-base --is-ancestor head_sha HEAD`); cherry-pick creates new SHA;
squash creates one commit; dirty workspace refused; `discarded` change refused.

**Acceptance**
- [ ] Merge is the default; strategy recorded.
- [ ] SHAs and `--reason` recorded; audit row.
- [ ] Stored squash value awaits C4 (constant in one place).

**Pitfalls** — Using the ref name inside a `cow_clone` workspace where the
ref does not exist: sync by SHA after the transfer. Never `git reset --hard`
before the dirty check passed.

---

### S2-T17 — Conflict detection, `sync status`, `sync abort`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-GIT` | `AG-LEAD`, `AG-STORE`, `AG-SEC` | `S2-E7` | `S2-T16` | `feat/git-sync-conflict-abort` | yes |

**Skills:** `git-changes-sync`, `testing-harness`, `security-review`, `gitflow`   **Spec:** §8.4, §14 (exit `3`), §18 "Sync conflict", §20.5
**Files:** `internal/git/{sync_conflict.go,sync_abort.go,sync_conflict_test.go}`

**Why / what** — §18: nothing overwritten; `conflict` with files; `abort` restores.

**Steps**
1. After a non-zero exit: `diff --name-only -z --diff-filter=U`. Non-empty →
   conflict: persist state `conflict` + files, emit `sync.<conflict>`, return
   `ErrSyncConflict` (exit `3`) with the attempt.
2. Empty → not a conflict (lock, disk full, timeout): restore immediately
   (step 3) and record failure (U-DATA-7 state name); return the wrapped Git error.
3. `Abort(syncID)`: only for an attempt in `conflict`. Restore by strategy:
   merge → `merge --abort`; cherry-pick → `cherry-pick --abort`; squash (no
   `MERGE_HEAD`) → `reset --hard <pre_head_sha>` (safe: step 2 of `S2-T16`
   proved no tracked edits existed). Verify `HEAD == pre_head_sha` and clean;
   record aborted state; emit; audit.
4. `Status(syncID)`: return the row (files, SHAs, strategy, state).

**Tests** — harness: both sides edit line 3 of the same file → conflict with
that file, exit `3`; abort → `HEAD` = pre, tree clean, file content = pre;
injected Git failure (exit 128, no unmerged files) → not reported as conflict,
workspace restored; abort on non-conflict attempt → `ErrNothingToAbort`.

**Acceptance**
- [ ] AC#5 conflict half covered; §18 "Sync conflict" row has a harness test.
- [ ] Abort never touches untracked files.

**Pitfalls** — On Windows an editor holding a file makes `merge` fail with
"unable to unlink" — that is step 2, not a conflict. `reset --hard` is only
allowed for attempts this code started.

---

### S2-T18 — Contract files and references

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-COLLAB` | `AG-LEAD`, `AG-INT` | `S2-E8` | `S2-T07` | `feat/collab-contracts` | no |

**Skills:** `collaboration-protocol`, `store-and-events`, `gitflow`   **Spec:** §9.5, §12.2 (`GET /v1/projects/{id}/contracts`), §24.7, §24.8
**Files:** `internal/collab/{contracts.go,contract_parse.go,contracts_test.go}`, `internal/collab/testdata/contracts/`

**Why / what** — Know each contract's owner, consumers and status so publish
can apply §24.8 and notify dependents.

**Steps**
1. Parser for the §24.8 header: `# Contract: <slug>`, `Owner: task <id>`,
   `Consumers: a, b`, `Status: <draft|agreed|implemented|deprecated>`. Unknown
   status → parse error. Body sections not validated beyond presence (Guidance).
2. `ContractStatusSource` interface (U-DATA-6): file header implementation reads
   the blob at a given SHA through `BlobReader`; a table-backed one can be
   added after the decision. `contracts` table caches `(path, owner_task_id,
   status, sha)`.
3. Dependents of a contract = `contract_refs` tasks ∪ header `Consumers`, minus
   the owner (Guidance: union, because §14 sets contracts on the owner task
   and §9.2 on intents). Exposed as `Dependents(ctx, path)`.

**Tests** — parser table (valid, missing status, extra spaces, CRLF); dependents union and dedup.

**Acceptance**
- [ ] Spec status strings only; parser fuzzed (small seed corpus).
- [ ] Status source swappable (U-DATA-6).

**Pitfalls** — CRLF files on Windows: trim `\r`. Contract file read from the
working tree instead of the commit SHA gives the wrong status.

---

### S2-T19 — Contract publish rule, `contract.changed`, lead requirement

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-COLLAB` | `AG-LEAD`, `AG-INT`, `AG-SEC` | `S2-E8` | `S2-T18`, `S2-T11`, `S2-T15` | `feat/collab-contract-publish-rule` | yes |

**Skills:** `collaboration-protocol`, `security-review`, `gitflow`   **Spec:** §9.5, §20.20, §24.8, §24.14
**Files:** `internal/collab/{contract_rule.go,contract_rule_test.go}`, hook in `publish_gate.go`

**Why / what** — §24.8: changing an `agreed` contract needs a
`confirm_contract` request to every consumer before publish; §9.5: dependents
are notified and promotion needs the lead.

**Steps**
1. For each path in `manifest.touches_contracts`, read status **at the diff
   base** (before the change). If `agreed`: for every dependent, require a
   `confirm_contract` request from the owner task to that dependent in the
   state demanded by **C8** (`ConfirmRule` interface: `OpenRule` vs
   `AcceptedRule`). How a request identifies *which* contract is unspecified (`U-DATA-8`) —
   raise with C8; until then any qualifying request owner→dependent counts. Missing → `ErrContractUnconfirmed` (exit `4`).
2. Publishing task is not the owner and the actor is a session → §24.14 says
   workers must not edit another task's `contracts/*` without an accepted
   `confirm_contract`; enforcement for worker sessions lands with `S3-E6`;
   here return the same `ErrContractUnconfirmed` when no accepted request exists (shared helper).
3. Pass the dependents list in `PublishRecord` so `RecordPublish` emits one
   `contract.changed` per dependent (`TargetTaskID`), in the publish tx.
4. `RequiresLead(change)` (computed on read, Guidance): true when the change
   touches a contract referenced by a task not in a terminal state. Exposed by
   `change show` (`S2-T24`). Promotion enforcement = `S5-E7`.

**Tests** — agreed contract, no request → refused; with request (both C8
variants) → published, `contract.changed` to `api`; draft contract → no
request needed; `RequiresLead` true/false cases.

**Acceptance**
- [ ] AC#20 notify half via event; lead requirement visible via `change show`.
- [ ] C8 not decided in code; both rules tested.

**Pitfalls** — Reading status from the new blob lets an author flip
`agreed → draft` in the same commit to skip the rule.

---

### S2-T20 — `pattern` invariants and path policy at publish

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-COLLAB` | `AG-LEAD`, `AG-INT`, `AG-SEC` | `S2-E9` | `S2-T10`, `S2-T13` | `feat/collab-pattern-invariants` | yes |

**Skills:** `collaboration-protocol`, `security-review`, `testing-harness`, `gitflow`   **Spec:** §9.6, §17, §20.4, §22 `[[invariants]]` `[security]`, §24.13
**Files:** `internal/collab/{invariants.go,glob.go,pathpolicy.go,invariants_test.go,invariants_fuzz_test.go}`

**Why / what** — "The engineer says no once, in the TOML; the runtime
repeats it" (§9.6). Plus never publishing secrets (§17, §24.13).

**Steps**
1. Load `[[invariants]]` with `kind = "pattern"` and `check_on_publish = true`.
   (`kind = "cmd"` is `S5-E5`; skip here.)
2. Parse each `deny` entry `"<glob>: <text>"` — split on the first `": "`.
   Grammar (literal substring vs regex; `**` semantics) is **U-GIT-9**: implement
   `DenyMatcher` interface; Guidance default: `**` any depth, `*` within one
   segment, text = literal substring. Unit tests pin the chosen behavior.
3. For each manifest file with `op != delete` matching the glob, read
   `blob_after` via `BlobReader`; check per **U-GIT-6** (whole blob vs added
   lines) through `EvalMode`. Collect violations `{invariant, path, line}`.
4. Path policy: `[security] blocked_patterns` and §24.13 never-commit
   (`node_modules/`, `.cowflow/`, `*.log`, build artifacts, `.env*`) checked
   on every manifest path, reported separately (C12). Any hit → `ErrBlockedPath`.
   "Build artifacts" has no list → report under C12, do not invent one.
5. Return `ErrInvariantViolated` with all violations (diagnostic text for the
   agent: invariant name, file, line, matched text).

**Tests** — fixture `deny = ["internal/**: import \"example.com/demo/cmd"]`:
file in `internal/x/a.go` with the import → violation; same text in
`cmd/main.go` → ok; deleted file ignored; nested `.env` under `src/` refused;
`key.pem` refused; fuzz glob matcher.

**Acceptance**
- [ ] AC#4 invariant half covered with event emitted by `S2-T15`.
- [ ] Gaps U-GIT-6, C12, U-GIT-9 behind interfaces; decisions linked.

**Pitfalls** — Using `filepath.Match` (OS separators, no `**`) on Git paths;
use a `/`-based matcher. Checking file names in the working tree instead of
the manifest misses renamed or nested files.

---

### S2-T21 — CLI daemonless backend, actor, exit codes

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-CLI` | `AG-LEAD`, `AG-MCP`, `AG-SEC` | `S2-E10` | `S2-T01` (fakes) | `feat/cli-daemonless-backend` | yes |

**Skills:** `cli-and-config`, `security-review`, `go-conventions`, `gitflow`   **Spec:** §5.3, §14, §24.14
**Files:** `internal/cli/{backend.go,local.go,actor.go,exit.go}` (+ tests)

**Why / what** — One place wires store + git + collab for every command, so
commands stay free of business logic and S4 can swap in an API client.

**Steps**
1. `local.go`: open store (path per `U-API-1` decision from `S0-E3`), build
   `IDGen`, real clock, `git.Runner`, `collab.Service`, `git.Publisher`
   (with `collab.PublishGate` as policy, store as recorder), `git.SyncService`.
2. `actor.go`: `ActorResolver` returning `ActorLead` (U-API-16). Every mutating
   CLI action is audited with actor `lead` (§24.14).
3. `exit.go`: map sentinel errors (Contracts §6.1 table) with `errors.Is`.
4. `--json`: one JSON document on stdout; warnings (overlaps, `scope.exceeded`)
   included in the document; human text otherwise; diagnostics to stderr.
5. `op_id`: each invocation mints one; Guidance: no user-facing flag.

**Tests** — exit-code table with fake backend; `--json` schema snapshot per
error class; no store opened for `--help`.

**Acceptance**
- [ ] Commands never import `internal/store` directly except through `Backend`.
- [ ] Exit codes per §14.

**Pitfalls** — Printing warnings to stdout in `--json` mode breaks parsers.

---

### S2-T22 — CLI: `intents`, `who`, `agents`, `index rebuild`, `events`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-CLI` | `AG-LEAD`, `AG-MCP` | `S2-E10` | `S2-T21`, `S2-T08`, `S2-T05`, `S2-T09` | `feat/cli-team-index-events` | no |

**Skills:** `cli-and-config`, `testing-harness`, `gitflow`   **Spec:** §14 (Team, Index and diagnostics), §20.23
**Files:** `internal/cli/{team.go,index.go,events.go}` + tests

**Why / what** — Read-side commands from §14 that need the collaboration data.

**Steps**
1. `cowflow intents [--path src/auth]`: list active intents (filtered via `WhoPath` when `--path`).
2. `cowflow who --path src/auth`: normalize (`\` → `/`, trim), `Index().WhoPath`; table columns task, kind, intent/change, updated.
3. `cowflow agents`: sessions with presence state, reason, stale flag.
4. `cowflow index rebuild`: `Index().Rebuild`, print counts.
5. `cowflow events [--follow] [--since <cursor>]`: page through `After`;
   `--follow` polls with the same interval as `WaitEvents` until Ctrl+C
   (ctx cancel), printing one JSON line per event in `--json` mode and the
   last cursor on exit.
6. Do not implement `status` here (S4 owns `/v1/status`; keep any existing stub).

**Tests** — `who` through the cobra command with recording Git runner → zero
calls (AC#23); `events --since` resumes; `--follow` stops on ctx cancel.

**Acceptance**
- [ ] Flags exactly as §14.
- [ ] AC#23 verified at CLI level.

**Pitfalls** — `--follow` busy-looping without a ticker burns CPU and holds read snapshots.

---

### S2-T23 — CLI: `request` commands

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-CLI` | `AG-LEAD`, `AG-MCP` | `S2-E10` | `S2-T21`, `S2-T11` | `feat/cli-requests` | no |

**Skills:** `cli-and-config`, `collaboration-protocol`, `gitflow`   **Spec:** §14 (Cross-requests), §24.11
**Files:** `internal/cli/request.go` + tests

**Why / what** — §14 surface for cross-requests.

**Steps**
1. `cowflow request send <to> --kind <kind> --title <t> --expect <output>`; validate enums at flag level (exit `1`), service validates again.
2. `cowflow request list [--open] [--to <task>]`.
3. `cowflow request accept|reject|close|comment|reassign <id>`; `comment` takes the body as the next argument (Guidance); `reassign` takes the target task (Guidance).
4. `cowflow request deliver <id> --change <change-id>`.
5. Fields without a §14 flag (ETA, reject reason, priority) are U-API-17: do not
   add flags until decided. Consequence: `request reject` via CLI returns
   `ErrRejectWithoutReason` today; the demo does not use it.

**Tests** — each subcommand happy path + one refusal with exit code; `--json` output shape.

**Acceptance**
- [ ] No flag outside §14 merged without the U-API-17 decision.

**Pitfalls** — Accepting `--expect` free text; it must be one of the five values.

---

### S2-T24 — CLI: `change` and `sync` commands

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-CLI` | `AG-LEAD`, `AG-MCP`, `AG-SEC` | `S2-E10` | `S2-T21`, `S2-T15`, `S2-T17`, `S2-T19` | `feat/cli-change-sync` | yes |

**Skills:** `cli-and-config`, `git-changes-sync`, `security-review`, `gitflow`   **Spec:** §14 (Changes), §8.2, §8.5, §20.3–20.5
**Files:** `internal/cli/{change.go,sync.go}` + tests

**Why / what** — The publish/sync front end used in the demo.

**Steps**
1. `cowflow change publish --task <id>` → `Publisher.Publish`; print change ID, ref, stat, warnings, overlaps.
2. `cowflow change list [--task] [--status]` (Guidance: mirror §12.4 query params), `change show <id>` (state, task, session, SHAs, stat, `touches_contracts`, requires-lead), `change manifest <id>` (the §8.2 JSON shape exactly), `change discard <id>`.
3. `cowflow sync <change-id> --into <workspace-id> [--strategy merge|cherry-pick|squash] [--reason <text>]` (`--reason` from §24.10).
4. `cowflow sync status <sync-id>`, `cowflow sync abort <sync-id>`.
5. `sync` and `sync status|abort` share the `sync` command: positional
   `status`/`abort` are subcommands; anything else is a change ID. Guard: a
   change ID literally named `status` is impossible because IDs are ULIDs.

**Tests** — end-to-end on harness repo through cobra: publish refused without
intent (exit `4`), publish ok, manifest JSON matches §8.2 keys, sync conflict
exit `3`, abort exit `0`.

**Acceptance**
- [ ] `change manifest --json` keys: `files[].path|op|blob_before|blob_after|add|del`, `stat.files|add|del`, `touches_contracts`.
- [ ] Exit codes `0/2/3/4` as specified.

**Pitfalls** — Cobra treating `sync status` as `sync` with arg `status`
unless subcommands are registered before positional handling; test it.

---

### S2-T25 — `simagent` test driver and two-process harness

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD`, `AG-SEC` | `S2-E11` | `S2-T01` (contracts), `S2-T09`–`S2-T12` | `test/simagent-driver` | no |

**Skills:** `testing-harness`, `collaboration-protocol`, `gitflow`   **Spec:** §15 (tool names), §18.1, §19 Phase 2, §20.7
**Files:** `test/harness/cmd/simagent/main.go`, `test/harness/twoproc.go`

**Why / what** — U-API-15/U-API-16: the product CLI has no intent/presence verbs
and no session identity yet. `simagent` is a **test-only** binary (never
shipped; excluded from `.goreleaser.yaml`) that acts as one worker session
against the same SQLite, with subcommands named after the §15 worker tools so
Sprint 3's MCP server maps 1:1 onto the same services.

**Steps**
1. Subcommands (Guidance; arguments mirror the MCP tools): `start`,
   `announce_intent --goal --paths --contracts`, `expand_scope --paths`,
   `report_status <state> [--reason]`, `send_request --to --kind --title --expect`,
   `reply_request <id> accept|reject [--eta] [--reason]`, `deliver_request <id> --change`,
   `wait_events --type <t|prefix.*> [--subject] [--timeout 300]`.
2. Flags `--project <dir> --task <id> --session <id> --json`. Session ID from
   `start` is passed explicitly — no hidden state file.
3. Uses `cli.Backend` wiring with `ActorSession` instead of `ActorLead`.
4. `twoproc.go`: helper that builds `cowflow` and `simagent` once per test
   run (`go build -o`), runs commands as separate processes with a shared
   `--project`, and captures stdout/exit codes.

**Tests** — smoke: two `simagent` processes announce overlapping intents and
both receive `intent.overlap` via `wait_events`.

**Acceptance**
- [ ] Not part of release artifacts; lives under `test/`.
- [ ] No logic duplicated from services (thin flag → service call).

**Pitfalls** — Letting `simagent` grow features the product lacks; if a step
needs new behavior, it belongs in a service task.

---

### S2-T26 — Two-terminal end-to-end scenario and acceptance suite

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD`, `AG-SEC` | `S2-E11` | `S2-T22`–`S2-T25` | `test/two-terminal-e2e` | no |

**Skills:** `testing-harness`, `collaboration-protocol`, `git-changes-sync`, `gitflow`   **Spec:** §19 Phase 2, §20.3–7, §20.20, §20.23
**Files:** `test/integration/twoterminal_test.go`, `test/fixtures/demo-repo/` (generator script in Go), `CHANGELOG.md`

**Why / what** — Automates the demo (§8) so the exit criterion is proven on
3 OSes on every PR, and produces evidence for the closing PR.

**Steps**
1. Fixture generator: Go module `example.com/demo`; `contracts/auth-api.md`
   (`Status: agreed`, owner auth, consumer api); `src/auth/jwt.go`,
   `src/api/orders.go`, `internal/x/x.go`; `config.toml` with the pattern
   invariant from `S2-T20` and `provider = "git_worktree"` (fast, all OSes).
2. Encode demo §8 steps 1–12 as subtests in order; each asserts exit code,
   key JSON fields and the events seen by the other process.
3. Assert process isolation: every step is a separate OS process; no daemon
   binary started (no `daemon start` call; Guidance: assert no listener on `[daemon] bind`).
4. Tag each subtest with `// AC#n`. Record timings in test output.
5. Add `CHANGELOG.md` `[Unreleased]` entry: "Daemonless collaboration plane".

**Tests** — this task is the test. `go test -race ./test/integration/ -run TwoTerminal -v`.

**Acceptance**
- [ ] Green on Windows, Linux, macOS CI.
- [ ] Each §2 criterion mapped to a subtest.
- [ ] Evidence (log excerpt + timings) attached to the sprint-closing PR.

**Pitfalls** — Shared global Git config in CI: harness must set
`GIT_CONFIG_GLOBAL`/`GIT_CONFIG_SYSTEM` to empty files and fixed author env.

---

## 8. Demo script

Run on Windows 11 (PowerShell 7), from a fresh clone of the fixture repo
produced by `S2-T26` (`go run ./test/fixtures/demo-repo/gen C:\demo`). Build
binaries first:

```powershell
go build -o C:\demo-bin\cowflow.exe ./cmd/cowflow
go build -o C:\demo-bin\simagent.exe ./test/harness/cmd/simagent
$env:PATH = "C:\demo-bin;$env:PATH"
Set-Location C:\demo
```

IDs below are placeholders (`<…>`); prefixes are U-DATA-2. Event names written
`cross_request.<…>`/`sync.<…>` follow the U-DATA-4/U-DATA-7 decisions. Output is
abbreviated.

**0. Setup (either terminal)**

```powershell
cowflow init --provider git_worktree
cowflow task create auth --base develop --title "Implement JWT" --contract contracts/auth-api.md --paths src/auth
cowflow task create api  --base develop --title "Orders endpoint" --depends auth --paths src/api
cowflow workspace list --json        # note <ws-auth>, <ws-api> and their paths
Get-Process cowflow -ErrorAction SilentlyContinue   # expected: nothing (no daemon)
```

**1. Sessions and intents — overlap warning** (§9.2)

Terminal A:
```powershell
$SA = (simagent --task auth start --json | ConvertFrom-Json).session_id
simagent --task auth --session $SA announce_intent --goal "JWT validation" --paths src/auth --contracts contracts/auth-api.md
# intent <intent-a> created (active); overlaps: none
```
Terminal B:
```powershell
$SB = (simagent --task api start --json | ConvertFrom-Json).session_id
simagent --task api --session $SB announce_intent --goal "Orders endpoint" --paths src/api,src/auth/middleware.go --contracts contracts/auth-api.md
# intent <intent-b> created (active)
# warning: overlap with task auth on src/auth (intent <intent-a>)    exit 0
```
Terminal A:
```powershell
cowflow events --json | Select-String intent.overlap
# {"type":"intent.overlap","target_session_id":"<SA>",...}
# {"type":"intent.overlap","target_session_id":"<SB>",...}
```

**2. `who` from the index**
```powershell
cowflow who --path src/auth
# TASK  KIND    REF           UPDATED
# auth  intent  <intent-a>    ...
# api   intent  <intent-b>    ...
```

**3. Cross-request and blocked session** (§9.3, §20.6, §20.7)

Terminal B:
```powershell
$R = (simagent --task api --session $SB send_request --to auth --kind expose_interface --title "Expose validateJWT" --expect commit --json | ConvertFrom-Json).id
simagent --task api --session $SB report_status blocked --reason "waiting for $R"
simagent --task api --session $SB wait_events --type "cross_request.*" --subject $R --timeout 300
# (waits; leave this running)
```
Terminal A:
```powershell
cowflow request list --open --to auth
# <R>  open  expose_interface  "Expose validateJWT"  expect=commit  from=api
simagent --task auth --session $SA reply_request $R accept --eta 30
```
Terminal B prints the acceptance event and returns; run `wait_events` again for the delivery:
```powershell
simagent --task api --session $SB wait_events --type "cross_request.*" --subject $R --timeout 300
```

**4. Publish refused without intent** (§20.4) — Terminal A, using a third task with no intent:
```powershell
cowflow task create docs --base develop --title "Docs" --paths docs
cowflow workspace list --json   # <ws-docs> path
git -C <ws-docs-path> commit --allow-empty -m "docs: placeholder"
cowflow change publish --task docs ; $LASTEXITCODE
# error: publish refused: task docs has no active intent
# 4
```

**5. Publish refused with a violated invariant** (§20.4) — Terminal A:
```powershell
# add to <ws-auth-path>/internal/x/x.go:  import "example.com/demo/cmd"
simagent --task auth --session $SA expand_scope --paths internal/x
git -C <ws-auth-path> add internal/x/x.go
git -C <ws-auth-path> commit -m "feat(auth): wire cmd helper"
cowflow change publish --task auth ; $LASTEXITCODE
# error: invariant no-cmd-import-in-internal violated: internal/x/x.go:3 import "example.com/demo/cmd
# 4
cowflow events --json | Select-String invariant.violated     # one event
git -C <ws-auth-path> reset --hard HEAD~1   # never published, so dropping it is allowed (§8.1 forbids rewriting published commits)
```

**6. Publish the delivery** (§8.1, §8.2, §20.3) — Terminal A:
```powershell
# edit <ws-auth-path>/src/auth/jwt.go: add func ValidateJWT
git -C <ws-auth-path> commit -am "feat(auth): add validateJWT"
cowflow change publish --task auth
# change <chg-1> published  ref refs/cowflow/changes/<chg-1>  files 1 (+12 -0)
cowflow change show <chg-1> --json      # "task_id":"auth","session_id":"<SA>","state":"published"
cowflow change manifest <chg-1> --json  # §8.2 shape
git for-each-ref refs/cowflow/changes/   # one ref; develop unchanged
```

**7. Deliver → B unblocked** (§20.6, §20.7) — Terminal A:
```powershell
cowflow request deliver $R --change <chg-1>
```
Terminal B's `wait_events` returns `cross_request.<delivered>` for `$R`; then:
```powershell
simagent --task api --session $SB report_status working
cowflow agents     # api  working
```

**8. Sync by merge** (§8.4, §20.5) — Terminal B:
```powershell
cowflow sync <chg-1> --into <ws-api> --reason "needs validateJWT"
# sync <sync-1> merge ok  pre <sha> post <sha>
git -C <ws-api-path> merge-base --is-ancestor <chg-1-head-sha> HEAD ; $LASTEXITCODE   # 0 → identity preserved
```

**9. Close the request** — Terminal B:
```powershell
cowflow request close $R
cowflow request list --to auth --json   # state "closed"
```

**10. `who --path` without Git** (§20.23)
```powershell
$saved = $env:PATH; $env:PATH = "C:\demo-bin"   # git not on PATH
cowflow who --path src/auth                      # same answer as step 2 plus change <chg-1>
$env:PATH = $saved
```
(Guidance demonstration; the authoritative check is the mock test in `S2-T08`/`S2-T22`.)

**11. Conflict and abort** (§18, §20.5)
```powershell
# Terminal A: edit line 3 of src/auth/jwt.go, commit, publish → <chg-2> (supersedes <chg-1>)
# Terminal B: edit line 3 of src/auth/jwt.go in <ws-api-path>, commit
cowflow sync <chg-2> --into <ws-api> ; $LASTEXITCODE
# conflict in src/auth/jwt.go  sync <sync-2>
# 3
cowflow sync status <sync-2> --json   # "state":"conflict","conflict_files":["src/auth/jwt.go"]
cowflow sync abort <sync-2>
git -C <ws-api-path> status --porcelain    # empty; HEAD = pre_head_sha
cowflow change show <chg-1> --json            # "state":"superseded"
```

**12. Contract change** (§9.5, §20.20) — Terminal A:
```powershell
# edit contracts/auth-api.md Interface section in <ws-auth-path>; commit "docs(auth): extend auth-api"
simagent --task auth --session $SA expand_scope --paths contracts
cowflow change publish --task auth ; $LASTEXITCODE
# error: contract contracts/auth-api.md is agreed; confirm_contract request to api required   → 4
simagent --task auth --session $SA send_request --to api --kind confirm_contract --title "auth-api v2" --expect contract
# (if C8 = accepted: Terminal B replies accept)
cowflow change publish --task auth
# change <chg-3> published; contract.changed → api; promotion requires lead
cowflow events --json | Select-String contract.changed   # target_task_id = api
cowflow change show <chg-3> --json                          # "requires_lead": true
```

---

## 9. Definition of Done

- [ ] Every task's Acceptance boxes ticked; PRs merged to `develop` by squash.
- [ ] `go build ./...`, `go test -race -cover ./...`, `golangci-lint run`, `go vet ./...`, `govulncheck ./...` clean; `go mod tidy` no-op; `CGO_ENABLED=0`.
- [ ] CI green on Windows, Linux, macOS, including `S2-T06` and `S2-T26`.
- [ ] §2 exit criteria all checked, with evidence in the sprint-closing PR.
- [ ] Every state mutation writes its §9.9 event in the same transaction; every publish/sync/discard/reassign writes `audit_log`.
- [ ] No Git call reachable from `who --path` or overlap detection (tests).
- [ ] Every gap in §3.2 is either decided (SCOPE.md §23 updated, SPEC-GAPS.md item removed in the same PR) or still isolated behind its named interface; the gaps found while planning (`U-API-15`…`U-API-17`, `U-DATA-7`, `U-GIT-9`) are likewise decided or still isolated.
- [ ] `AG-SEC` reviewed all `Security: yes` PRs against the security-review checklist.
- [ ] `CHANGELOG.md` `[Unreleased]` updated; skills updated with retro learnings (`docs/skill/`).
- [ ] All commits Conventional, `git commit -S -s`, no AI attribution.

---

## 10. Risks and mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Day-1 gap decisions slip (U-GIT-1, U-DATA-1, U-DATA-3, C8) | publish/manifest/events cannot finish | interfaces in §6 let code land; `AG-LEAD` timeboxes decisions to D1–D2; tests parametrize over options |
| `SQLITE_BUSY` under multi-process writes | flaky CLI, lost operations | DSN pragmas on every connection, `BEGIN IMMEDIATE`, short tx, no Git inside tx; `S2-T06` 20-process test |
| Event cursor skips events (ULID ordering) | §20.7/§20.15 failures | commit-ordered `seq` (Guidance) pending U-DATA-3; three-consumer tests |
| Orphan retention refs after crash between ref and row | refs without changes | ref created after all refusals; idempotent `update-ref`; orphan report in `reconcile.go`; full reconciliation `S4-E8` |
| `cow_clone` workspaces do not share objects with the project repo | publish/sync fail | explicit transfer step (`S2-T14`/`S2-T16`) keyed on provider capability; U-PLAT-1 confirmed day 1; demo uses `git_worktree`, CI covers both |
| Windows file locks during merge | false conflicts, half-applied sync | classify by unmerged files only; restore on non-conflict failure; harness test |
| No product CLI for intents/presence (U-API-15) | demo depends on test driver | `simagent` thin over the same services; MCP in S3 replaces it; flagged to maintainer |
| `AG-STORE` overloaded (8 tasks, critical path) | demo slips | storefake on D2 unblocks consumers; `S2-T06` movable to `AG-QA` |
| Path-prefix bugs (`src/auth` vs `src/authz`, `\` vs `/`) | wrong overlaps / `who` answers | one `normPath`, segment-boundary SQL, table tests |
| Pattern invariant semantics undecided (U-GIT-6, U-GIT-9) | false positives/negatives | `DenyMatcher`/`EvalMode` interfaces; fixture-driven tests pin behavior once decided |
| Scope creep into daemon features (escalation, `session.lost`) | sprint overrun, fake daemon behavior | §5.3 table enforced in review; those stay in `S4-E4`, `S4-E10` |
