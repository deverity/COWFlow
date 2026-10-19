---
name: security-review
description: Use when reviewing or writing code that handles tokens, the loopback API, subprocess execution (git, gates, runners), names/paths from agents or config, workspace destroy, publish/sync/promote, ProjFS callbacks, or agent permissions. Also load for any PR review in COWFlow to run the security checklist.
---
# Security Threat Model and Review

## When to use
- Reviewing any PR touching `internal/api`, `internal/mcp`, `internal/runner`, `internal/git`, `internal/workspace` (incl. `projfs/`, `container/`), `internal/platform`, `internal/integration`, `internal/collab`, `internal/config`.
- Designing a new API endpoint, MCP tool, gate, invariant, or runner adapter.
- Handling a security report (see repo `SECURITY.md`).

## Source of truth
SCOPE.md wins on any conflict.
- §17 Security
- §24.14 Agent boundaries; §24.13 Session and workspace (never-commit paths); §24.4 Code and review (no secrets); §24.2 How COWFlow applies them (protected branches, tags, audit of `--force`)
- §7 Windows — platform requirements (paths, processes, shell)
- §6.1 Isolation contract, §6.5 ProjFS, §6.8 A workspace is not a container
- §8.1 Git rules
- §12 Local API (loopback, per-session Bearer token, payload ≤ 1 MiB, scope-based authorization, confined paths)
- §15 MCP, §15.1 lead token, §15.4 Headless permissions, §15.5 Enforcement
- §22 Configuration (`[daemon] bind`, `[security] blocked_patterns`), §23 Risks ("Headless worker with full permissions")

## Rules
- API binds loopback only (`[daemon] bind = "127.0.0.1:8787"`); NEVER a non-loopback address by default (§12, §17, §22).
- Every request carries a per-session Bearer token scoped to project/task/workspace; authorization is scope-based (§12, §17). The agent NEVER chooses `workspace_id`; it is derived from the token (§15).
- Workers MUST NOT call `cowflow` with another session's token (§24.14). Lead token scope = entire project, not per session (§15.1); it MUST NOT be placed in worker env/config.
- Input validation on all endpoints; payload ≤ 1 MiB; confined paths (§12).
- NEVER concatenate a shell command; NEVER `cmd.exe` with a string; gates, invariants and runners are argv arrays (§7, §17, §15.3).
- Git via `exec.Command` with `-C`, `GIT_TERMINAL_PROMPT=0`, timeout (§8.1).
- Names (task slugs, branches `cowflow/<project>/<task-slug>`, `<kind>/<slug>`, change refs) MUST be sanitized and validated with `git check-ref-format` (§8.1, §17, §24.2).
- Paths MUST be confined: `Clean + EvalSymlinks + prefix` against the allowed root; `\\?\` above 240 chars on Windows (§7, §12, §17).
- `[security] blocked_patterns` (default `.env`, `.env.*`, `*.pem`, `*.key`, `id_rsa*`) are rejected on publish; no secrets in commits (§17, §22, §24.4). Publish also rejects manifests with `node_modules/`, `.cowflow/`, `*.log`, build artifacts, `.env*` (§24.13).
- `blocked_patterns` and `check_on_publish` invariants are the first line of defense against headless workers (§15.4).
- Untrusted agent code (client repos, third-party code) → `provider = "container"` with restricted network; this is the recommendation, not an option (§6.8, §15.4, §17).
- Agent boundaries enforced server-side regardless of runner (§24.14, §15.5): no `git push`, `git tag`, branch deletion, touching `refs/cowflow/*`; no editing another task's `contracts/*` without an accepted `confirm_contract`; no editing `.cowflow/`, `config.toml`, `.mcp.json`, generated `AGENTS.md`/`CLAUDE.md`; dependency changes without `touches_deps: true` → warning to lead. Only the engine creates tags (§24.2).
- Protected branches: no workspace on `main`/`develop`; `change publish` refuses if `HEAD` is on them; promotion only via integration group (§24.2). Promotion = CAS on `target_sha` under a short lock (§8.1).
- Never rebase/amend a published commit (§8.1, §24.3).
- Audit trail MUST record publish, sync, destroy, promote, revert, reassign (§17), plus `--force` base overrides, `--no-contract`, and lead CLI actions that bypass §24.14 (§24.2, §24.7, §24.14). Audit rows go to `audit_log` (§13).
- Destroy MUST rescue uncommitted changes first; never destroy unpreserved changes without `--force` (§17, §20 #12).
- Reconciliation reads the actual Git and filesystem, not only SQLite (§17, §18).
- Remote credentials belong to the user only; workers never push (§17, §24.14). Refs are local until a Hub exists (§23 decision 8).
- ProjFS callbacks NEVER serve blobs outside the authorized `base_sha` (§17); callback timeouts return an error, not a hang (§18).

## Implementation guide
### Threat model
Assets: the user's repository and refs (esp. `main`/`develop`, `refs/cowflow/*`), remote credentials, secrets on disk, other workspaces, COWFlow state (SQLite, config, tokens), the host.
Actors and trust:
- Worker / watchdog agent: **untrusted code** with free command execution in its workspace via `permission_flag` (§15.4). May be prompt-injected by repo content.
- Lead: project-wide token issued by `cowflow init` to the local user (§15.1); trusted, but actions audited (§24.14).
- Human: trusted; approves promotion in `manual` mode.
- Other local processes/users: must not reach the API (loopback + token).
Key facts: a workspace is NOT a sandbox — it does not isolate network, ports, env, credentials or processes (§6.1, §6.8). Isolation guarantee is write isolation between cooperating parties only. Real isolation = `container` provider.

### Where each control lives (§16.1)
- `internal/api`: loopback listener, auth middleware (token → `Scope{ProjectID, TaskID, WorkspaceID, SessionID, Role}`), `http.MaxBytesReader` 1 MiB, strict JSON decoding.
- `internal/mcp`: no identity in tool inputs; token from `COWFLOW_SESSION_TOKEN`; delegates to the same backend so enforcement is shared.
- `internal/platform` (`paths`): `Confine(root, p string) (string, error)` = Clean → EvalSymlinks (of the deepest existing ancestor for new files) → prefix check on separator boundary, case-insensitive on Windows; long-path prefixing.
- `internal/git` (`command`, `refs`): single `Run(ctx, dir, args...)` entry; `check-ref-format` before any ref/branch creation; `--` before pathspecs.
- `internal/collab` (`invariants`) + `internal/git` (`publish`, `manifest`): `blocked_patterns`, never-commit paths, boundary paths and refs evaluated on the change manifest (§8.2) before the retention ref is created.
- `internal/workspace`: destroy → rescue → `pending_cleanup`; `container/` for untrusted code; `projfs/` blobstore keyed and checked by `base_sha`.
- `internal/store`: `audit_log` written in the same transaction as the state change (§13).
Sketch (Guidance; `Scope`/`Confine` are illustrative names):
```go
type Scope struct{ ProjectID, TaskID, WorkspaceID, SessionID string; Role string } // lead|worker|watchdog
func Authorize(s Scope, action Action, target Resource) error // deny by default
```
Guidance: compare tokens with `crypto/subtle.ConstantTimeCompare`; generate with `crypto/rand`; store only a hash in SQLite; never log tokens (slog redaction); set `Host` check against loopback names to blunt DNS rebinding; keep `gosec` enabled (`.golangci.yml`).

## Pitfalls
- `strings.HasPrefix(p, root)` without a trailing separator (`/ws/a` vs `/ws/ab`) or without resolving symlinks/junctions.
- EvalSymlinks on a not-yet-existing path fails → falling back to the unresolved path; resolve the existing parent instead.
- Windows: case-insensitive paths, 8.3 short names, `\\?\` and UNC forms, junctions, alternate data streams (`file:stream`).
- Branch/ref names beginning with `-` treated as git options; missing `--`.
- `GIT_*` env vars inherited from the agent or user leaking into COWFlow's git calls (e.g., `GIT_DIR`).
- Gate/invariant `cmd` assembled from task text; runner placeholders (`{prompt}`) joined into one string.
- Checking `blocked_patterns` only on file names in the working tree instead of the published manifest (renames, nested `.env`).
- Enforcement only in MCP handlers, bypassable via `cowflow --json` (Aider path) or direct API (§15.3, §15.5).
- Logging request bodies/env that contain tokens or `.env` content.
- Treating `git_worktree`/`cow_clone` as isolation for untrusted code (§6.8).
- Destroy path that `RemoveAll`s before rescue, or follows a symlink out of the workspace.

## Tests required
- Auth: missing/invalid/expired token → 401; worker token on another task/workspace → 403; another session's token refused (§24.14); lead token not present in worker `.cowflow/mcp.json`.
- Listener only on loopback; payload > 1 MiB rejected (§12).
- Path confinement table tests: `..`, absolute paths, symlinks/junctions escaping root, prefix-collision siblings, Windows case and `\\?\` forms; fuzz `Confine`.
- Ref names: fuzz slug → branch name; all invalid per `check-ref-format` rejected; names starting with `-` rejected.
- Publish rejects `blocked_patterns`, never-commit paths, boundary paths (`.cowflow/`, `.mcp.json`, `config.toml`, generated context files, foreign `contracts/*` without accepted `confirm_contract`), HEAD on `main`/`develop` (§20 #31).
- No subprocess built from a shell string (Guidance: lint/grep test that `exec.Command` is never called with `sh -c`/`cmd /c`).
- Destroy with dirty workspace without `--force` refused; rescue happens before deletion (§20 #12).
- Audit rows exist for publish, sync, destroy, promote, revert, reassign, `--force`, `--no-contract`.
- ProjFS simulated callbacks: request for an object not reachable from `base_sha` refused (harness, §18.1).
- Fuzz API payloads and manifest parsers (§18.1).

## Done checklist
- [ ] Threat model reviewed for the change; new trust boundary (if any) documented in `docs/`.
- [ ] All applicable PR review checklist items ticked.
- [ ] Open items left open, not decided here: restricted-network details for `container`, named-pipe transport (§23 Open question 3), minimum scope of the agent lead (§23 Open question 4).

### PR review checklist
- [ ] New endpoint/tool: token required, scope checked, deny-by-default, no `workspace_id`/session in input.
- [ ] Inputs validated; body limited to 1 MiB; unknown JSON fields rejected (Guidance).
- [ ] Every subprocess is an argv array via `exec.CommandContext`; no shell; timeout; Git has `-C` and `GIT_TERMINAL_PROMPT=0`.
- [ ] Every name that becomes a ref/branch passes `check-ref-format`; pathspecs after `--`.
- [ ] Every filesystem path from agent/config/API goes through Clean + EvalSymlinks + prefix confinement.
- [ ] Publish path checks `blocked_patterns`, never-commit paths, §24.14 boundary paths/refs, protected branches.
- [ ] No tag/push/remote operation reachable by a worker; tags only by the engine.
- [ ] State changes in publish/sync/destroy/promote/revert/reassign write `audit_log` in the same transaction.
- [ ] Destroy rescues uncommitted work; `--force` required for unpreserved changes.
- [ ] No tokens, credentials or `.env` content in logs, events, errors, or commits.
- [ ] ProjFS code serves only objects from the authorized `base_sha`; callbacks have timeouts.
- [ ] Untrusted-code scenario documented as requiring `provider = "container"`; no claim that a workspace is a sandbox.
- [ ] Enforcement lives in the API/domain layer (shared by CLI, MCP, `--json`), not in a single front end.
- [ ] Tests from "Tests required" added for the touched surface; `go test -race ./...` green; `gosec` clean or `//nolint:gosec // reason`.
