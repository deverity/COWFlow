# Spec gaps

Inconsistencies and unspecified points found in [SCOPE.md](SCOPE.md) while
writing the implementation skills (`docs/skill/`). They are **not decided**.
Agents must not resolve them silently in code: ask the maintainer, then record
the decision in SCOPE.md (§23 "Settled decisions") and remove the item here in
the same PR.

Distinct from SCOPE.md §23 "Open questions", which the spec already lists.

**IDs are stable** — cite them from sprint documents, issues, and PRs:
`C<n>` for contradictions, `U-<GROUP>-<n>` for unspecified points
(`DATA`, `GIT`, `PLAT`, `API`, `SCHED`). New items get the next number in
their group; resolved items are struck through, never renumbered.

## Contradictions

| # | Topic | Conflict | Sections |
|---|---|---|---|
| ~~C1~~ | ~~Go version~~ | Resolved: Go 1.26+ (SCOPE.md §23 settled decision 1). | header, §16, §23 |
| C2 | Task branch naming | `cowflow/<project>/<task-slug>` vs `<kind>/<slug>` under the gitflow preset. | §8.1, §24.2 |
| ~~C3~~ | ~~`promotion_mode` values~~ | Resolved: `manual\|auto` in v1; `pr` is open question 7 (SCOPE.md §23 decision 14). | §10.3, §22, §24.2 |
| ~~C4~~ | ~~Sync strategy name~~ | Resolved: `squash` everywhere (SCOPE.md §23 decision 15). | §8.4, §14 |
| ~~C5~~ | ~~Per-target integration limit~~ | Resolved: `max_integrations_per_target` only (SCOPE.md §23 decision 16). | §10.3, §11, §22 |
| ~~C6~~ | ~~Invariant example import path~~ | Resolved: `github.com/deverity/cowflow/cmd` (SCOPE.md §23 decision 17). | §9.6, §22 |
| ~~C7~~ | ~~Golden tree location~~ | Resolved: `<golden_root>/<sha>`, empty = `<root>/golden` (SCOPE.md §23 decision 18). | §6.4, §22 |
| C8 | Contract edit precondition | publish needs an *open* `confirm_contract` vs edit needs an *accepted* one. | §24.8, §24.14 |
| ~~C9~~ | ~~Cross-request escalation~~ | Resolved: `[collaboration.escalation_after_minutes]` table (SCOPE.md §23 decision 19). | §9.4, §22, §24.11 |
| ~~C10~~ | ~~Runner config~~ | Resolved: `prompt_mode` stays `arg\|stdin\|file` (SCOPE.md §23 decision 20). | §15.3, §22 |
| ~~C11~~ | ~~`[scheduler.llm.anthropic]`~~ | Resolved: the §15.3 block is canonical (SCOPE.md §23 decision 21). | §15.3, §22 |
| ~~C12~~ | ~~Never-commit lists~~ | Resolved: two independent lists (SCOPE.md §23 decision 22). | §22, §24.13 |
| ~~C13~~ | ~~Cache keys~~ | Resolved: six `[cache]` keys (SCOPE.md §23 decision 23). | §6.7, §22 |
| ~~C14~~ | ~~Defender exclusion~~ | Resolved: suggested only; `doctor` never fails (SCOPE.md §23 decision 24). | §6.4, §7 |
| C15 | Runner host before the daemon | The runner is a daemon-only capability, but runners ship in Phase 3 and the daemon in Phase 4. | §5.3, §19 |
| C16 | Integration group creation | Phase 3 lead tools and §20.27 need `cowflow_create_integration`; integration groups arrive in Phase 5 (engine is daemon-only). | §5.3, §15.1, §19, §20 |
| C17 | `cowflow_wait_events` signature | `(filter, after_cursor, timeout_seconds)` vs `until=tasks_completed` in the lead flow. | §5.4, §15.1 |

## Unspecified

### Data model and events

- **U-DATA-1** *(Sprint 2, with U-DATA-3)* Columns of `events`, `outbox`, `event_deliveries`; where `op_id` is stored (§13).
  Recommended shape: `events(seq INTEGER PK AUTOINCREMENT, id TEXT UNIQUE evt_ULID, project_id, type, subject_kind, subject_id, task_id, session_id, payload JSON, op_id, created_at)` with `seq` as the public cursor; `outbox(event_seq PK -> events.seq, state pending|delivered|failed, attempts, next_attempt_at, last_error)`; `event_deliveries(event_seq, consumer, delivered_at, acked_at, PK(event_seq, consumer))`; `operations(op_id TEXT PK, project_id, kind, result JSON, created_at)` written in the same transaction by `WithTx`.
- ~~**U-DATA-2** Prefixes for "prefixed ULIDs" (§13).~~ Resolved: `<prefix>_<ULID>` with a fixed prefix table (SCOPE.md §23 decision 37).
- **U-DATA-3** Whether the event cursor is the ULID; ULIDs from concurrent daemonless writers are not commit-ordered, so a ULID cursor can skip events (§9.9).
- **U-DATA-4** Concrete `cross_request.*` event names; `scope.exceeded` missing from the event list (§9.9, §24.9).
- **U-DATA-5** States of integration groups, workspaces (beyond `ready`, `pending_mount`, `pending_cleanup`), and `resource_leases.state` (§6, §9.8, §10).
- **U-DATA-6** Where contract `Status` lives (file header vs table) and who moves it to `implemented` (§9.5, §24.8).
- **U-DATA-7** States of intents (only `active` is named) and of `sync_attempts` (only `conflict` is named); concrete `sync.*` event names (§9.2, §9.9, §18, §24.9).
- **U-DATA-8** How a cross-request identifies *which* contract it targets (e.g. `confirm_contract`) (§9.3, §9.5, §24.8).
- **U-DATA-9** Columns of `agent_sessions` and `budgets` (§11, §13, §24.15).

### Git and changes

- **U-GIT-1** Diff base of the change manifest (`base_sha`, previous change, or parent) (§8.2).
- **U-GIT-2** How `Cowflow-Task`/`Cowflow-Change` trailers are added to an immutable published commit; value and casing of `x-cowflow-source` (§8, §24.3).
- **U-GIT-3** Whether a non-descendant `HEAD` is refused or becomes a new change (§8.1).
- **U-GIT-4** Retention cleanup for promoted/superseded refs (`change_retention_days`) (§8, §22).
- **U-GIT-5** How changes are applied in the integration candidate; whether `promoted_sha == candidate_sha` (§10.1).
- **U-GIT-6** `pattern` invariants in diff-only gates: whole blob vs added lines (§9.6, §10).
- **U-GIT-7** Behavior when `rerun_on_stale = false` (§10.3).
- **U-GIT-8** Gate → resource mapping (e.g. build → `compile`) (§9.8, §10).
- **U-GIT-9** Grammar of `deny` entries in `pattern` invariants: literal vs regex, glob dialect (§9.6).
- **U-GIT-10** How worker `git push`, `git tag`, branch deletion and `refs/cowflow/*` edits are prevented or detected (§24.14).
- **U-GIT-11** Contents of the "manifest lint" gate; whether overlap and lint findings fail the group or only warn (§2.2, §10.1, §24.10, §24.13).
- **U-GIT-12** Manual approval: which actor counts as the human approval; `cowflow_approve_promotion` vs `cowflow_promote`; whether an approval survives a stale revalidation (§5.4, §10.3, §15.1).
- **U-GIT-13** Release flow: how the lead confirms the tag version; annotated vs signed tag (§24.2).
- **U-GIT-14** Post-promotion: behavior with `auto_revert = false`; change/task states after a revert; `kind`/`expected_output` of the cross-requests opened; whether `required = false` gates trigger a revert (§10.2).
- **U-GIT-15** Integration candidate workspace: branch vs detached `HEAD`, `.env.cowflow`/ports, sparse, row in `workspaces` (§10.1, §24.2).
- **U-GIT-16** Which task is "flagged by the gate" for `fix_incompat` on a conflict between two members or a repo-wide build/test failure (§24.12).
- **U-GIT-17** Meaning of `require_validation = false` and `diff_only_first = false` (§10.3, §22).

### Workspaces and platform

- **U-PLAT-1** How a `cow_clone` tree gets its own HEAD and index (§6.1).
- **U-PLAT-2** Rescue-before-destroy format and storage location (§17).
- **U-PLAT-3** Sparse `always` set beyond "root, configs" (§6.6).
- ~~**U-PLAT-4** Same-volume requirement between `golden_root` and `workspace.root` for block clone (§6.4).~~ Resolved: same volume required, else fall back to `git_worktree` (SCOPE.md §23 decision 31).
- ~~**U-PLAT-5** Auto-attach of the VHDX at boot: scheduled task vs service (§6.4).~~ Resolved: Task Scheduler task at logon (SCOPE.md §23 decision 32).
- **U-PLAT-6** *(Phase 4, S4-E9)* `CTRL_BREAK` cancellation when running as a Windows service (no console) (§7). Interim rule (SCOPE.md §23 decision 33): when `GenerateConsoleCtrlEvent(CTRL_BREAK)` fails (no console), skip the grace wait and go straight to `TerminateJobObject`; log at debug.
- **U-PLAT-7** Build tag for the container backend; "restricted network" for containers (§6.3, §15.4).
- ~~**U-PLAT-8** Hermetic harness "Git repo on tmpfs" on Windows/macOS (§18.1).~~ Resolved: tmpfs on Linux only (SCOPE.md §23 decision 34).
- **U-PLAT-9** Form of the mount point under `C:\cf\` (§6.4, §23 open question 8). Flags and defaults of `devdrive` and `init --devdrive` are resolved (SCOPE.md §23 decision 35); v1 mounts on a drive letter only.
- ~~**U-PLAT-10** Where the Dev Drive is "registered in the project state": `[devdrive]` in `config.toml`, SQLite, or both (§6.4).~~ Resolved: `[devdrive]` in `config.toml` only (SCOPE.md §23 decision 36).
- **U-PLAT-11** Workspace directory name under `workspace.root` and location of per-workspace logs (§5.4, §6.1).
- **U-PLAT-12** Benchmark definitions: which timing counts for "< 2 s each" (in-process vs CLI, sequential vs concurrent); what "setup" means for the shared-cache gain (§19, §20).
- **U-PLAT-13** What "in use" means for golden GC; states of `golden_trees` rows (§6.4, §11, §13).
- **U-PLAT-14** Variable names in `.env.cowflow` for `port_range` and workspace identity (§6.1).
- **U-PLAT-15** Explicit `provider = "cow_clone"` on a volume without COW: fail, or fall back with a warning (§6.2, §6.3).
- **U-PLAT-16** Whether git-ignored files count as "unpreserved changes" for destroy (§17, §20).
- **U-PLAT-17** Windows service account (LocalSystem vs user) given per-user tokens, repos and VHDX attach (§6.4, §17, §19).
- **U-PLAT-18** ProjFS callback timeout value; no config key exists (§6.5, §18).
- **U-PLAT-19** Whether projected ProjFS content applies `.gitattributes` filters (eol, LFS) (§6.5).
- **U-PLAT-20** Meaning of "lazy" enumeration in §23 Open question 5 (§6.5, §23).
- **U-PLAT-21** Denominator of "bytes of a full worktree" in the ProjFS materialization metric (§6.5, §20).

### Daemon, API, CLI, MCP

- ~~**U-API-1** SQLite file name/location under `state_dir`; `config.toml` location; lead token storage (§5, §15, §22).~~ Resolved: `<project root>/.cowflow/config.toml`, `config.Locate()`, `<state_dir>/cowflow.db`, per-user lead token (SCOPE.md §23 decision 25).
- **U-API-2** How the daemon starts on demand; how CLI/MCP detect daemon vs daemonless (§5.3).
- **U-API-3** `/metrics` format (§21).
- **U-API-4** HTTP status ↔ CLI exit code mapping (§12, §14).
- **U-API-5** `request_id` in logs: cross-request vs HTTP request (§21).
- ~~**U-API-6** Default role of `cowflow mcp` without `--role` (§14, §15).~~ Resolved: role of the session token, else `lead` (SCOPE.md §23 decision 26).
- **U-API-7** Codex MCP config passes no role/token; Gemini `.gemini/settings.json` path and writer; generic runner has no permission/MCP flags (§15.3).
- **U-API-8** How session tokens are validated in daemonless mode (§5.3, §15).
- **U-API-9** Watchdog tool set: worker base + extras, or read-only + requests only (§5.4, §15.1).
- ~~**U-API-10** `init --provider` omits `container` (§14, §22).~~ Resolved: exactly `auto|cow_clone|projfs|git_worktree` (SCOPE.md §23 decision 27).
- ~~**U-API-11** Which §22 values are defaults vs examples.~~ Resolved: §22 is the defaults file with listed exceptions (SCOPE.md §23 decision 28).
- ~~**U-API-12** Exit codes for cases §14 does not classify: `doctor` with failing checks, a not-yet-implemented command, an unclassified internal error, UAC elevation declined, `workspace destroy` refused for unpreserved work (`2` or `4`) (§14).~~ Resolved: exit-code rule in §14 (SCOPE.md §23 decision 29).
- ~~**U-API-13** The bootstrap `version` subcommand is not in §14: keep it, or only `--version` (§14).~~ Resolved: `--version` only (SCOPE.md §23 decision 30).
- **U-API-14** No CLI flag for a task's `sparse = false` (§6.6, §14).
- **U-API-15** No CLI verb to create/update an intent or report presence; only MCP and API (§9.1, §9.2, §14).
- **U-API-16** CLI caller identity in daemonless mode: origin of `request send`, actor of `accept`/`deliver`, session of a publish (§5.4, §24.14).
- **U-API-17** §14 `request` flags omit `eta_minutes` on accept, a reject reason, and request priority (§14, §24.11).
- **U-API-18** Protocol injection when `context_file` is tracked: writing it dirties a tracked file workers may not publish (§15.3, §15.5, §24.14).
- **U-API-19** Daemon process model: `daemon start` detached vs foreground; how `daemon stop` reaches it; one daemon per `state_dir` or per machine; service name (§5.3, §14).
- **U-API-20** No `/v1` routes for resources, budgets, or session spawn, which the CLI needs in daemon mode (§9.8, §11, §12).
- **U-API-21** Allowed values of `task create --kind watchdog --watch` (§9.7, §14).
- **U-API-22** Session token lifetime beyond "until the session ends or is revoked" (§12, §17).

### Scheduler and budgets

- **U-SCHED-1** Whether leases and SLA escalation work daemonless (§5.3, §9.8).
- **U-SCHED-2** `acquire(wait=true, timeout)` timing out while queued (§9.8).
- **U-SCHED-3** Precedence between `max_builds`/`max_tests` and `[[resources]]` slots (§11, §22).
- **U-SCHED-4** Budget actions other than `on_budget_exceeded = "pause"`; whether 80%/100% applies to provider hourly/daily budgets; how usage is measured per runner (§11, §24.15).
- **U-SCHED-5** `depends` depth: edges or nodes; tie-breaking within a priority (§11, §24.7).
- **U-SCHED-6** Lead `close` from any request state (§9.3).
- **U-SCHED-7** Kind/config of the `require_resource_exec` invariant (§9.8).
- **U-SCHED-8** Unit of `max_concurrent_requests`: COWFlow can only count running sessions, not vendor HTTP requests (§11, §15.3).
- **U-SCHED-9** Channel for the 80% budget warning "to the agent and the lead"; §9.9 has no warning event (§9.9, §24.15).
- **U-SCHED-10** Meaning of "paused" for a headless LLM CLI session: stop the tree and keep the workspace vs suspend (§11, §24.15).
- **U-SCHED-11** Lease holder identity (`resource_leases.session_id`/`task_id`) when the integration engine holds a lease (§9.8, §13).
- **U-SCHED-12** Meaning of an absent or zero scheduler limit (no limit vs nothing allowed) (§11, §22).
