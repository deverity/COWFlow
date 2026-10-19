# Spec gaps

Inconsistencies and unspecified points found in [SCOPE.md](SCOPE.md) while
writing the implementation skills (`docs/skill/`). They are **not decided**.
Agents must not resolve them silently in code: ask the maintainer, then record
the decision in SCOPE.md (§23 "Settled decisions") and remove the item here in
the same PR.

Distinct from SCOPE.md §23 "Open questions", which the spec already lists.

## Contradictions

| # | Topic | Conflict | Sections |
|---|---|---|---|
| C1 | Go version | Spec says Go 1.23+; `go.mod` targets 1.25 (1.23 is out of security support). | header, §16, §23 |
| C2 | Task branch naming | `cowflow/<project>/<task-slug>` vs `<kind>/<slug>` under the gitflow preset. | §8.1, §24.2 |
| C3 | `promotion_mode` values | `manual\|auto` vs also `pr`; remote/forge config for `pr` undefined. | §10.3, §22, §24.2 |
| C4 | Sync strategy name | `squash-merge` vs CLI `--strategy squash`. | §8.4, §14 |
| C5 | Per-target integration limit | `max_concurrent_per_target` vs `max_integrations_per_target`. | §10.3, §11, §22 |
| C6 | Invariant example import path | `cowflow/cmd` does not match module `github.com/deverity/cowflow`. | §9.6, §22 |
| C7 | Golden tree location | `<root>/golden/<sha>` vs separate `workspace.golden_root`. | §6.4, §22 |
| C8 | Contract edit precondition | publish needs an *open* `confirm_contract` vs edit needs an *accepted* one. | §24.8, §24.14 |
| C9 | Cross-request escalation | per-priority SLA (15/60 min) vs single `escalation_after_minutes = 30`. | §9.4, §22, §24.11 |
| C10 | Runner config | Aider `prompt_mode = "message"` not in `arg\|stdin\|file`. | §15.3, §22 |
| C11 | `[scheduler.llm.anthropic]` | Two examples with different contents. | §15.3, §22 |
| C12 | Never-commit lists | §24.13 list and `[security] blocked_patterns` differ; relationship unstated. | §22, §24.13 |
| C13 | Cache keys | `gradle_home`, `pip_cache` in §6.7 but missing from `[cache]` example. | §6.7, §22 |
| C14 | Defender exclusion | "suggest" vs required configuration. | §6.4, §7 |

## Unspecified

### Data model and events

- Columns of `events`, `outbox`, `event_deliveries`; where `op_id` is stored (§13).
- Prefixes for "prefixed ULIDs" (§13).
- Whether the event cursor is the ULID; ULIDs from concurrent daemonless writers are not commit-ordered, so a ULID cursor can skip events (§9.9).
- Concrete `cross_request.*` event names; `scope.exceeded` missing from the event list (§9.9, §24.9).
- States of integration groups, workspaces (beyond `ready`, `pending_mount`, `pending_cleanup`), and `resource_leases.state` (§6, §9.8, §10).
- Where contract `Status` lives (file header vs table) and who moves it to `implemented` (§9.5, §24.8).

### Git and changes

- Diff base of the change manifest (`base_sha`, previous change, or parent) (§8.2).
- How `Cowflow-Task`/`Cowflow-Change` trailers are added to an immutable published commit; value and casing of `x-cowflow-source` (§8, §24.3).
- Whether a non-descendant `HEAD` is refused or becomes a new change (§8.1).
- Retention cleanup for promoted/superseded refs (`change_retention_days`) (§8, §22).
- How changes are applied in the integration candidate; whether `promoted_sha == candidate_sha` (§10.1).
- `pattern` invariants in diff-only gates: whole blob vs added lines (§9.6, §10).
- Behavior when `rerun_on_stale = false` (§10.3).
- Gate → resource mapping (e.g. build → `compile`) (§9.8, §10).

### Workspaces and platform

- How a `cow_clone` tree gets its own HEAD and index (§6.1).
- Rescue-before-destroy format and storage location (§17).
- Sparse `always` set beyond "root, configs" (§6.6).
- Same-volume requirement between `golden_root` and `workspace.root` for block clone (§6.4).
- Auto-attach of the VHDX at boot: scheduled task vs service (§6.4).
- `CTRL_BREAK` cancellation when running as a Windows service (no console) (§7).
- Build tag for the container backend; "restricted network" for containers (§6.3, §15.4).
- Hermetic harness "Git repo on tmpfs" on Windows/macOS (§18.1).

### Daemon, API, CLI, MCP

- SQLite file name/location under `state_dir`; `config.toml` location; lead token storage (§5, §15, §22).
- How the daemon starts on demand; how CLI/MCP detect daemon vs daemonless (§5.3).
- `/metrics` format (§21).
- HTTP status ↔ CLI exit code mapping (§12, §14).
- `request_id` in logs: cross-request vs HTTP request (§21).
- Default role of `cowflow mcp` without `--role` (§14, §15).
- Codex MCP config passes no role/token; Gemini `.gemini/settings.json` path and writer; generic runner has no permission/MCP flags (§15.3).
- How session tokens are validated in daemonless mode (§5.3, §15).
- Watchdog tool set: worker base + extras, or read-only + requests only (§5.4, §15.1).
- `init --provider` omits `container` (§14, §22).
- Which §22 values are defaults vs examples.

### Scheduler and budgets

- Whether leases and SLA escalation work daemonless (§5.3, §9.8).
- `acquire(wait=true, timeout)` timing out while queued (§9.8).
- Precedence between `max_builds`/`max_tests` and `[[resources]]` slots (§11, §22).
- Budget actions other than `on_budget_exceeded = "pause"`; whether 80%/100% applies to provider hourly/daily budgets; how usage is measured per runner (§11, §24.15).
- `depends` depth: edges or nodes; tie-breaking within a priority (§11, §24.7).
- Lead `close` from any request state (§9.3).
- Kind/config of the `require_resource_exec` invariant (§9.8).
