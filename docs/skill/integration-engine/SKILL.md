---
name: integration-engine
description: Use when implementing or changing internal/integration — integration groups, the temporary candidate workspace, deterministic change ordering, diff-only and workspace gates, invariants as gates, stale detection and revalidation, CAS promotion on `target_sha`, `promotion_mode` (manual/auto/pr), post-promotion auto-revert, and gitflow preset effects (hotfix back-merge group, release tags).
---
# Integration engine

## When to use
- Writing or reviewing `internal/integration/` (engine, gates, diffonly, promotion, postpromotion).
- Implementing `cowflow integration create|validate|promote|cancel|show|open`, `/v1/integration-groups/*`, or lead MCP tools `cowflow_create_integration`, `cowflow_validate`, `cowflow_promote`, `cowflow_approve_promotion`.
- Touching tables `integration_groups`, `integration_group_members`, `integration_attempts`, or `[integration]`, `[[gates]]`, `[[invariants]]`, `[conventions]` config.
- Not for: publish/sync/manifests (see `git-changes-sync`), workspace providers.

## Source of truth
SCOPE.md wins on any conflict. Derived from:
- §2.2 Principles (3, 7, 12, 13); §3.1 Included; §4 Glossary (Invariant, Integration group/attempt)
- §5.3 Modes (engine is daemon-only); §5.4 lead flow; §6.3/§6.4 (`cow_clone`, golden trees)
- §8.1 Rules (promotion = CAS), §8.5 Change states
- §9.4 Lead, §9.5 Contracts, §9.6 Invariants, §9.8 Resource leases (gates share the queue), §9.9 Events
- §10 Integration groups — §10.1 Flow, §10.2 Post-promotion, §10.3 Policies
- §11 Scheduler; §12.5 Integration, events, status; §13 Data model; §14 CLI (Integration, exit codes); §15.1 (lead tools, `pending_human_approval`), §15.5
- §16.1 Layout; §17 Security; §18 Resilience, §18.1 Hermetic harness; §19 Phase 5; §20 Acceptance criteria; §21 Observability
- §22 Configuration (`[integration]`, `[integration.post_promotion]`, `[[gates]]`, `[conventions]`); §23 decisions 6, 12
- §24.1 GitFlow, §24.2 How COWFlow applies them, §24.4, §24.6, §24.10, §24.12 Integration groups, §24.16

## Rules
Admission
- Refuse integration without a change (§2.2.3, §15.5, §20.4). Members are the latest `published` change per task, never `superseded` (§24.12).
- A task with an active stub (`stub: true`) does not enter (§24.6, §24.12).
- Lead creates, engine validates, lead promotes until `promotion_mode = auto` (§24.12). Under `gitflow`, `target = develop` is the only path for features; promotion only via integration group (§24.2).

Flow (§10.1, in this order)
1. Group created with tasks/changes and a `target` → `integration.created`.
2. Pin each change's `head_sha` and the `target_sha`.
3. Diff-only gates first, without a workspace (`diff_only_first = true`): `pattern` invariants, touched contracts, overlap between the group's changes, manifest lint. Fail → stop here (§10.1, §23 decision 12, §20.10).
4. Temporary workspace = `cow_clone` of the golden at `target_sha`.
5. Apply changes in topological order (task dependencies); tie → publishing order.
6. Workspace gates: build, test, lint, `cmd` invariants. Invariants run as a gate in every group (§9.6).
7. Conflict/failure → `integration.failed` with stage, SHAs, files; workspace preserved for `keep_failed_hours` (`keep_failed_workspaces`) (§10.1, §22). Opens a `fix_incompat` cross-request to the task flagged by the gate; the group re-enters when the new change is published (§24.12).
8. Success → `candidate_sha` validated on top of `target_sha` → `integration.passed`; changes become `validated` (§8.5).
9. Promotion: CAS on `target_sha` under a short lock; target advanced → `integration.stale`, reapply, rerun gates with `rerun_on_stale` (§8.1, §10.1, §18, §20.11). Stale is not the agent's fault; the agent is called only if reapplying fails (§24.12).
10. Record `promoted_sha` and evidence → `integration.promoted`; changes `promoted`; tasks `done`; workspaces enter the cleanup policy (§10.1, §24.12).

Gates
- `[[gates]]`: `name`, `cmd` (array, never a shell string), `timeout_seconds`, `required` (§7, §22). The `test` gate is `required` (§24.4).
- Gates go through the same resource queue (`compile`, `test`); there is no parallel queue for the engine. `max_integrations_per_target` aliases `integrate:<target>` (§9.8).
- Invariants: `kind = "pattern"` (`deny` globs) in diff-only; `kind = "cmd"` in the workspace (§9.6, §19 Phase 5). Violation → `invariant.violated`.

Promotion policy
- `promotion_mode = "manual"` (default): `promote` returns `pending_human_approval` and does not promote (§15.1, §22, §23 decision 6). `require_validation = true` (§22).
- `promotion_mode = "auto"`: engine promotes after validation (§10.3).
- `promotion_mode = "pr"`: when remote and forge are configured, promotion opens a PR instead of merging locally (§24.2).
- Promoting a change that alters a contract referenced by an active task requires the lead (§9.5, §20.20).
- `max_concurrent_per_target = 1` serializes only the ref (§10.3).
- Audit promote/revert (§17). Engine runs only in daemon mode (§5.3).

Post-promotion (§10.2)
- Only with `promotion_mode = "auto"` and `post_promotion.enabled = true`: run `post_promotion.gates` on the target branch.
- Fail (`auto_revert = true`) → automatic revert commit, `integration.reverted`, cross-request to the tasks involved, lead notified (§10.2, §18, §20.24).

Gitflow preset (§24.2, `[conventions].preset = "gitflow"`)
- Hotfix promoted to `main` → automatically create integration group `hotfix → develop` and notify the lead (§20.31).
- `release/*` promoted to `main` → tag per `tag_format` (`v{major}.{minor}.{patch}`), lead confirms the version, plus a return integration group to `develop`.
- Only the engine creates tags; agents have no permission (§24.2, §24.14).
- `trunk`: short-lived branches off `main`, direct promotion; `none`: auditing only (§24 footer).

Review
- `integration show` / `GET .../review`: each task's intent, manifest/stat, touched contracts, gate results, invariants; lines only with `--diff` (§9.4, §24.4). Warnings: manifest > 20 files or > 800 lines (§24.10), `feat` without a new test (§24.4), session without `report_result` → `incomplete` (§24.13). `integration open` generates a `.code-workspace` with the candidate (§5.4).

## Implementation guide
Layout (§16.1): `internal/integration/{engine.go, gates.go, diffonly.go, promotion.go, postpromotion.go}`; uses `internal/git`, `internal/workspace` (golden + `cow_clone`), `internal/scheduler` (leases), `internal/collab` (invariants, cross-requests), `internal/store` (outbox).

```go
// Field names from §10.1; shapes are Guidance.
type Group struct { ID, ProjectID, Target, TargetSHA string; Members []Member }
type Member struct { TaskID, ChangeID, HeadSHA string; PublishedAt time.Time } // ordering tie-break
type Attempt struct { ID, GroupID, TargetSHA, CandidateSHA, PromotedSHA, Stage string; Results []GateResult }
type Gate interface { Name() string; Required() bool; Run(ctx context.Context, in GateInput) GateResult }
type GateResult struct { Name, Stage string; Passed bool; Files []string; Duration time.Duration; Output string }
```

Steps
1. `engine.go`: state machine driven by events `integration.created|started|passed|failed|stale|promoted|reverted`; each transition + outbox event in one SQLite tx with an `op_id` (§13, §18).
2. `diffonly.go`: evaluate from stored manifests (`change_files`) and the index; no workspace. Overlap = intersecting paths across members.
3. Order: topological sort over `task_dependencies`; tie by publish order; deterministic for equal inputs. Guidance: detect cycles even though §24.7 rejects them at creation.
4. Candidate: provider `Create` from golden at `target_sha`; apply each retention ref; gates via `exec.CommandContext` with arrays, per-gate `timeout_seconds`, acquiring the matching lease first.
5. `promotion.go`: take the per-target short lock, then CAS. Guidance: `git update-ref refs/heads/<target> <candidate_sha> <target_sha>` fails atomically if the ref moved; treat that failure as `stale`.
6. `postpromotion.go`: run gates on the target; on failure create the revert commit (Guidance: `git revert` of the promoted range in a temporary workspace, then CAS again), emit `integration.reverted`, open cross-requests, notify lead.
7. Gitflow hooks after promotion: create the back-merge group / tag (after lead confirms version) via the engine only.

## Pitfalls
- `promotion_mode` values: §10.3 lists `manual|auto`; §24.2 adds `"pr"` — implement all three, but forge/remote config for `pr` is not specified.
- Group states are not enumerated in the spec; only events are. Do not invent public state names without confirming.
- How changes are applied in the candidate (merge vs other) and whether `promoted_sha` equals `candidate_sha` is not explicit.
- `pattern` invariant scope (whole `blob_after` vs added lines only) is not specified.
- Gate → resource mapping (`build`→`compile`?) is not explicit; §9.8 names `compile`, `test`.
- `max_concurrent_per_target` (§10.3) vs `max_integrations_per_target` (§11, §22) overlap; behavior with `rerun_on_stale = false` is unspecified.
- Never promote without validation, never move the target outside the CAS, never delete a failed workspace before `keep_failed_hours`.
- Revert commit and tags are engine-authored; respect `enforce_commit_format`.

## Tests required
- Unit: topological order + tie-break, diff-only gates on fixture manifests, gate timeout/required semantics, CAS success/failure, mode handling (`pending_human_approval`).
- Harness (§18.1): Git on tmpfs; fake clock for gate timeouts; fault injection into Git subprocess during apply/CAS; daemon crash between outbox write and delivery mid-promotion.
- §18 rows: Target advances (`stale` → revalidates); Post-promotion fails (revert + requests + lead); Daemon restarts (resume attempt idempotently).
- §20: 4 (integration refused without change), 9, 10 (two features, diff-only before workspace, manual promotion), 11 (target advanced → revalidation), 20 (contract change requires lead), 24 (auto-mode revert + requests), 27 (lead creates group via MCP only), 30 (gates respect `compile` slots), 31 (hotfix to `main` creates `develop` group).
- §19 Phase 5 output: two features validated and promoted with evidence; automatic revert demonstrated in auto mode.

## Done checklist
- [ ] Admission refuses: no change, superseded change, active stub.
- [ ] `head_sha`/`target_sha` pinned; diff-only gates run before any workspace.
- [ ] Candidate from golden at `target_sha`; deterministic order; workspace gates via leases.
- [ ] Failure event carries stage, SHAs, files; workspace kept `keep_failed_hours`; `fix_incompat` opened.
- [ ] Promotion is CAS under a short lock; stale → reapply + rerun per `rerun_on_stale`.
- [ ] `manual` returns `pending_human_approval`; `auto` promotes; `pr` opens a PR when configured.
- [ ] `promoted_sha` + evidence recorded; changes `promoted`, tasks `done`; audit written.
- [ ] Post-promotion revert path emits `integration.reverted`, opens requests, notifies lead.
- [ ] Gitflow: hotfix back-merge group, release tag + return group, engine-only tags.
- [ ] `integration show` review by intent; `--diff` on demand.
- [ ] Tests above pass on all 3 platforms with `-race`.
