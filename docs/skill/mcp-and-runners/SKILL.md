---
name: mcp-and-runners
description: Use when implementing or changing internal/mcp (the mandatory `cowflow mcp` server, role-based tool sets, `.mcp.json` generation, lead mode) or internal/runner (`[runner.<name>]` adapters, protocol injection, headless permissions, per-provider dispatch). Also load when touching server-side protocol enforcement or agent boundaries.
---
# MCP Server and Runners

## When to use
- Adding/changing an MCP tool, its role gating, or its input/output schema.
- Generating `.mcp.json` (repo root, lead) or `.cowflow/mcp.json` (workspace, worker).
- Writing or fixing a runner adapter (Claude Code, Codex CLI, Aider, Gemini CLI, generic) or `cowflow runners list|check`.
- Implementing protocol injection into `context_file`, enforcement refusals, or agent-boundary checks.

## Source of truth
SCOPE.md wins on any conflict.
- §15 MCP (mandatory for LLM agents), §15.1 Tools by role, §15.2 Generated `.mcp.json`, §15.3 Runners and mixed-vendor team, §15.4 Headless permissions, §15.5 Enforcement
- §5.3 Modes, §5.4 Use with VS Code and lead mode
- §9.4 Lead, §9.7 Watchdog, §9.8 Resource leases, §9.9 Events
- §14 CLI (`# Setup`, `# MCP`, exit codes), §16 Go stack, §16.1 Layout
- §17 Security, §22 Configuration (`[runner]`, `[runner.*]`, `[scheduler.llm.*]`, `[watchdog]`, `[security]`)
- §23 Risks / Settled decisions #5 / Open questions #1, #4
- §24.13 Session and workspace, §24.14 Agent boundaries, §24.15 Budget, §24.16 Definition of Done
- §19 Phase 3, §20 criteria 4, 18, 19, 27, 28, 29

## Rules
- MCP is MUST for every LLM agent; transport is stdio; identity comes from `COWFLOW_SESSION_TOKEN` (§15).
- The agent NEVER chooses `workspace_id`; the server derives task/workspace/session from the token (§15).
- `cowflow mcp --role lead|worker|watchdog` exposes exactly the role's tool set (§14, §15.1):
  - Worker: base set — `cowflow_get_context`, `cowflow_announce_intent`, `cowflow_expand_scope`, `cowflow_find_collaborators`, `cowflow_send_request`, `cowflow_reply_request`, `cowflow_deliver_request`, `cowflow_publish_change`, `cowflow_list_changes`, `cowflow_request_sync`, `cowflow_report_status`, `cowflow_report_result`, `cowflow_resource_check`, `cowflow_resource_acquire`, `cowflow_resource_release`, `cowflow_resource_announce`, `cowflow_escalate`.
  - Lead adds: `cowflow_create_task`, `cowflow_update_task`, `cowflow_run`, `cowflow_pause_task`, `cowflow_create_integration`, `cowflow_validate`, `cowflow_promote`, `cowflow_wait_events`, `cowflow_reassign`, `cowflow_prioritize`, `cowflow_approve_promotion`, `cowflow_resource_revoke`, `cowflow_status`.
  - Watchdog: `cowflow_subscribe_events`, `cowflow_read_metrics`, `cowflow_send_request`, `cowflow_escalate`; read-only + requests; NEVER publishes changes (§5.4, §9.7).
- `cowflow_wait_events(filter, after_cursor, timeout_seconds ≤ 300)` MUST return events plus a new cursor and NEVER block beyond the timeout (§15.1, §23).
- Lead token: issued by `cowflow init` to the local user, project-wide scope, not per session (§15.1).
- `cowflow_promote` with `promotion_mode = "manual"` MUST return `pending_human_approval` and NOT promote (§15.1).
- `cowflow init` MUST write `.mcp.json` at the repo root with `"args": ["mcp", "--role", "lead", "--project", "."]` (§5.4, §15.2).
- Workers get `.cowflow/mcp.json` in their workspace with `--role worker` and `COWFLOW_SESSION_TOKEN` in `env` (§15.2).
- Lead dispatches and returns; it does not edit worker code — it opens a cross-request or creates a task (§5.4).
- Enforcement is server-side and model-independent: refuse `publish` without intent, `integration` without change, `publish` with a violated invariant (§2.2 #3, §15.5, §20 #4).
- Runners are declarative `[runner.<name>]` with `provider`, `cmd`, `mcp_config_flag`, `permission_flag`, `context_file`, `prompt_mode`; commands are arrays, NEVER a shell string (§15.3, §7, §17).
- Runner substitutes only `{mcp_config}`, `{workspace}`, `{prompt}`, `{task_id}` (and `{cmd}` for generic) (§15.3).
- `[runner].default` applies unless `cowflow task create --runner <name>`; `[runner].lead` is used when `lead = agent:<task>` (§15.3, §22).
- With `inject_protocol = true` the runner injects the protocol into the vendor's `context_file`, including acceptance criteria, the three waiting options (blocked+event, declared stub, escalate) and `cowflow exec --resource` usage when `build_cmd`/`test_cmd` exist (§9.3, §9.8, §15.5, §22, §24.7).
- Scheduler dispatches per provider (`[scheduler.llm.<provider>]`); a provider at its limit MUST NOT block others (§15.3, §20 #28).
- Budget: 80% → warning to agent and lead; 100% → session paused, `budget.exceeded`, workspace preserved; retries count against the same budget (§24.15, §20 #19).
- Headless `permission_flag` grants free command execution: agent code is untrusted; `blocked_patterns` and `check_on_publish` invariants are the first line; `provider = "container"` with restricted network is the recommendation for client/third-party code (§15.4).
- Agent boundaries (§24.14) MUST be enforced for workers regardless of runner: no `git push`, `git tag`, branch deletion, touching `refs/cowflow/*`; no editing another task's `contracts/*` without an accepted `confirm_contract`; no editing `.cowflow/`, `config.toml`, `.mcp.json`, generated `AGENTS.md`/`CLAUDE.md`; deps outside shared cache or root `go.mod`/`package.json` without `touches_deps: true` → warning to lead; NEVER call `cowflow` with another session's token. Lead may do these via CLI, audited.
- Session protocol injected for workers (§24.13): start `get_context` → read contracts → `announce_intent` → `resource_check`; publish at each compilable milestone (≥ every 45 min); end `report_result` (criteria answered) + `report_status(completed|failed)`; missing `report_result` → `incomplete` in review.
- Never-commit paths (`node_modules/`, `.cowflow/`, `*.log`, build artifacts, `.env*`) are rejected by publish; `init` maintains `.gitignore` (§24.13).
- MCP SDK: official MCP Go SDK, fallback `mark3labs/mcp-go`, behind an internal adapter (§16, §23 risk "Go MCP ecosystem changes"). Final SDK choice is OPEN (§23 Open question 1).
- Daemonless: `cowflow mcp` MUST work directly on SQLite when no daemon is running (§3.1, §5.2, §5.3).

## Implementation guide
Packages (§16.1): `internal/mcp/`, `internal/runner/`, `internal/config/` (runner TOML), `internal/collab/` + `internal/api/` (enforcement lives behind the API/domain, not in MCP handlers), `internal/scheduler/` (per-provider dispatch), `internal/cli/` (`init`, `mcp`, `runners`).

Sketch (names from spec; shapes are Guidance):
```go
// internal/mcp
type Role string // "lead" | "worker" | "watchdog"

// Backend hides daemon vs daemonless (§5.3). Implemented by an HTTP client to /v1 and by a direct store adapter.
type Backend interface {
    GetContext(ctx context.Context, sess SessionToken) (*Context, error)
    AnnounceIntent(ctx context.Context, sess SessionToken, in IntentInput) (*Intent, error)
    PublishChange(ctx context.Context, sess SessionToken, in PublishInput) (*Change, error)
    WaitEvents(ctx context.Context, sess SessionToken, filter EventFilter, afterCursor string, timeout time.Duration) ([]Event, string, error)
    // ... one method per tool in §15 / §15.1
}

// SDK adapter (§23): the only file importing the MCP SDK.
type ToolServer interface {
    AddTool(name string, schema json.RawMessage, h ToolHandler)
    ServeStdio(ctx context.Context) error
}

// internal/runner
type Runner struct { // mirrors [runner.<name>]
    Name, Provider, ContextFile, PromptMode string // prompt_mode: arg | stdin | file (spec also shows "message" for aider)
    Cmd, MCPConfigFlag, PermissionFlag []string
}
type Placeholders struct{ MCPConfig, Workspace, Prompt, TaskID, Cmd string }
```
Steps:
1. `internal/config`: parse `[runner]` and `[runner.<name>]` (BurntSushi/toml); validate `cmd` non-empty, known `prompt_mode`, `provider` maps to a `[scheduler.llm.<provider>]` or the global `[scheduler.llm]`.
2. `internal/mcp`: register tools per `Role` from a single static table; a tool not in the role table is not registered (not just rejected). Guidance: also re-check role in the backend so a forged call is refused.
3. Resolve session from `COWFLOW_SESSION_TOKEN` at startup; lead role uses the init-issued lead token. All tool inputs omit `workspace_id`.
4. `cowflow_wait_events`: clamp `timeout_seconds` to 300; daemon → SSE/outbox subscription; daemonless → poll SQLite with backoff; always return cursor.
5. `cowflow init`: write `.mcp.json` (§15.2), lead token, `.gitignore` entries. Runner spawn: write `.cowflow/mcp.json` with `--role worker` + token in `env`; for Gemini write `.gemini/settings.json` (§15.3 comment).
6. Runner spawn: build argv = `cmd` + `mcp_config_flag` + `permission_flag` + prompt per `prompt_mode`; substitute placeholders per element (never join into a string); set `cmd.Dir` to the workspace; env adds `COWFLOW_SESSION_TOKEN`; run under the platform process tree (Job Object / `Setpgid`, §7).
7. Protocol injection: render protocol (§24.13, §24.14, §9.3, acceptance list) into `context_file` inside the workspace; that generated file is itself forbidden for the worker to edit (§24.14).
8. Aider (`mcp_config_flag = []`) participates via `cowflow --json` through shell (§15.3); same token, same enforcement.
9. `cowflow runners list|check`: detect installed CLIs and versions; flag incompatible versions (§14, §23).
10. Enforcement in domain/API: intent required before publish; invariants with `check_on_publish`; change required for integration; boundary checks on manifest paths and refs; refusals map to exit code `4` (refused by policy) for CLI (§14).

## Pitfalls
- Putting enforcement only in MCP handlers: Aider/CLI path bypasses it. Enforce in the API/domain (§15.5).
- Accepting `workspace_id`/`session_id` from tool input — breaks scoping (§15, §17).
- `wait_events` holding the lead's extension session for hours (§5.4, §23).
- Concatenating placeholders into a shell string, or invoking `cmd.exe /c` (§7, §17).
- Assuming the Codex `mcp_config_flag` passes role/token — it only sets `command=cowflow`; verify how args/env reach the server (ambiguity, see Done).
- Leaking the lead token into worker workspaces or `.mcp.json` committed content; `.mcp.json` `env` is `{}` in the spec.
- Hard-wiring one MCP SDK across the codebase; keep it behind the adapter (§23).
- One provider's rate limit stalling the whole queue (§15.3).

## Tests required
- Role table: each role registers exactly its tools; watchdog cannot publish; worker cannot call lead tools (§15.1).
- Token scoping: worker token cannot read/act on another task/workspace; another session's token refused (§24.14, §17).
- Enforcement (§20 #4): publish without intent refused; integration without change refused; publish with violated `check_on_publish` invariant refused with diagnostic.
- Boundaries: manifest touching `.cowflow/`, `.mcp.json`, `config.toml`, generated context file, another task's `contracts/*` refused; `touches_deps` warning emitted.
- `cowflow_wait_events` returns by timeout with cursor; timeout > 300 clamped; daemon and daemonless (fake clock from `test/harness`).
- `cowflow_promote` in manual mode returns `pending_human_approval` and no ref moves.
- `.mcp.json` golden-file test; recognized by Claude Code extension and Codex CLI (§20 #29, manual/e2e).
- Runner argv building: table tests for every `[runner.*]` in §15.3 and all `prompt_mode`s; placeholders substituted per element; no shell.
- Per-provider dispatch: limit 1 on one vendor while the other continues (§20 #28); budget 80%/100% behavior (§20 #19).
- Fuzz MCP tool input decoding (JSON payloads) (§18.1).
- E2E (Phase 3 gate): lead via MCP only creates tasks, `run`, `wait_events`, integration (§20 #27); Claude + Codex workers complete a real dependency cross-request (§20 #18, #28).
- All with `-race`; concurrency with 20+ simulated MCP sessions (§20 #15).

## Done checklist
- [ ] Tools registered per role exactly as §15/§15.1; names match spec.
- [ ] No tool accepts `workspace_id`; identity only from token.
- [ ] Enforcement lives in API/domain and covers the MCP, CLI and `--json` paths.
- [ ] §24.14 boundaries enforced and audited; lead CLI overrides audited.
- [ ] `.mcp.json` and `.cowflow/mcp.json` generated as §15.2; lead token never in worker env.
- [ ] Runner argv built from arrays; placeholders per element; process tree kill wired.
- [ ] Protocol injected into `context_file` when `inject_protocol = true`.
- [ ] Per-provider limits and budgets honored; `budget.exceeded` emitted.
- [ ] MCP SDK isolated behind the adapter; SDK choice left open (§23 #1).
- [ ] Works daemonless and with daemon.
- [ ] Tests above pass with `go test -race ./...` on Windows, Linux, macOS; `CGO_ENABLED=0` build (Go 1.26+ per go.mod and SCOPE.md §16).
