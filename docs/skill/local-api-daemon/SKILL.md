---
name: local-api-daemon
description: Use when building or changing internal/api or internal/daemon — the loopback `/v1` HTTP API, per-session scoped tokens, endpoint handlers, SSE/polling event delivery, `/v1/status` and `/metrics`, daemon start/stop/service install, startup reconciliation, timeouts/cancellation, or slog JSON logging and audit.
---
# Local API and Daemon (internal/api, internal/daemon)

## When to use
- Adding/changing any endpoint in §12.1–§12.5, request validation, auth/scope checks.
- Implementing `GET /v1/events/stream` (SSE), `GET /v1/events` (polling), `POST /v1/events/ack`.
- Daemon lifecycle: `cowflow daemon start|stop|status|install`, Windows service / systemd / launchd, reconciliation on restart.
- Session timeouts, cancellation, process-tree kill, logging fields, audit entries, metrics.

## Source of truth
SCOPE.md wins on any conflict.
- §3.1 Included (daemon on demand or service; loopback API with per-session token; slog, audit, `/metrics`, timeouts, cancellation)
- §5.1–§5.3 Architecture, Diagram, Modes; §5.4 lead rules
- §7 Windows (service via `x/sys/windows/svc`, VHDX auto-attach before daemon, Job Objects, process groups)
- §9.8 Resource leases (heartbeat renews leases), §9.9 Events
- §12 Local API (§12.1–§12.5)
- §15 MCP (session token, lead token, enforcement §15.5, `pending_human_approval`)
- §16/§16.1 Go stack/layout; §17 Security; §18 Resilience/§18.1 harness; §19 Phase 4; §20; §21 Observability; §22 `[daemon]`, `[collaboration]`, `[scheduler]`; §23

## Rules
- API MUST be loopback-only, JSON, versioned under `/v1` (§12). NEVER bind a non-loopback address; default `[daemon] bind = "127.0.0.1:8787"` (§22).
- Every request MUST carry a per-session Bearer token scoped to project/task/workspace; authorization is scope-based (§12, §17).
- The lead token is issued by `cowflow init` to the local user, scope = entire project (§15). Worker tokens are per session (`COWFLOW_SESSION_TOKEN`) (§15, §15.2).
- The agent does not choose `workspace_id`; the server derives it from the token scope (§15). A worker using another session's token MUST be refused (§24.14).
- MUST validate input, cap payload at 1 MiB, confine paths (`Clean + EvalSymlinks + prefix`, §7), sanitize names via `check-ref-format` (§12.5, §17).
- Enforcement is server-side and model-independent: refuse `publish` without intent, integration without change, `publish` with a violated invariant (§2.2 #3, §15.5, §20.4).
- `promote` with `promotion_mode = "manual"` MUST return `pending_human_approval` and not promote (§15).
- Endpoints MUST match §12 exactly (paths, methods). Groups: projects/tasks/workspaces/sessions (§12.1); presence/intents/contracts/index (§12.2); cross-requests (§12.3); changes/sync (§12.4); integration/events/status (§12.5).
- Event delivery: SSE in daemon mode, polling with `after=<cursor>` always; cursor + ACK, at-least-once (§9.9, §23 decision 3). SSE is hand-written on `net/http` (§16).
- `/v1/status` JSON and `/metrics` MUST expose §21 data (agents/tasks by state, provider timings, `CowKind`/disk per workspace, projfs materialized bytes, golden cache, pending events, requests open/overdue/escalated, changes by state, sync conflicts by cause, gate durations, reverts, retries/cancellations/orphans, tokens/cost per session/task/day). No web dashboard (§3.2).
- Logs MUST be `log/slog` JSON with `project_id`, `task_id`, `session_id`, `workspace_id`, `request_id`, `change_id`, `group_id` when known (§3.1, §21); level from `[daemon] log_level`.
- MUST audit publish/sync/destroy/promote/revert/reassign (§17), lead overrides (§24.14), `--force` base and `--no-contract` (§24.2, §24.7).
- Daemon-only capabilities: scheduler, runner, heartbeat, SSE, integration engine, watchdog, reconciliation, ProjFS host (§5.3). `projfs` workspaces exist only while the daemon runs (§5.3).
- On start the daemon MUST reconcile SQLite × processes × worktrees × ProjFS roots, honor idempotent `op_id`, and resume the outbox (§18); reconciliation reads actual Git and filesystem (§17).
- Windows service via `x/sys/windows/svc`; VHDX auto-attach MUST happen before the daemon; VHDX not mounted → `pending_mount`, never fall back silently to NTFS (§7, §18).
- Timeouts: `session_timeout_seconds`, `command_timeout_seconds` (§11, §22). Cancellation (`POST /v1/sessions/{id}/cancel`) MUST kill the process tree: Windows Job Object `KILL_ON_JOB_CLOSE`, `CTRL_BREAK` → deadline → `TerminateJobObject`; Unix `Setpgid` + `SIGTERM` + `SIGKILL` (§7, §20.16).
- Agent crash → `session.lost`, workspace/logs preserved, retry (§18). Heartbeat (`POST /v1/presence/heartbeat`) renews presence (TTL `presence_ttl_seconds`) and active leases (§9.8, §22).
- NEVER build shell strings; commands are arrays (§7, §17). Remote credentials belong to the user only (§17).
- Open (§23): named pipes as transport are v1.1 — do not implement now; multi-machine hub is out of scope (§3.2).

## Implementation guide
Files (§16.1): `internal/api/` (router, handlers per §12 group, auth, sse, errors), `internal/daemon/` (lifecycle, reconcile, service), `internal/platform/` (`process_*` tree kill, service helpers), `docs/api`.

Guidance (sketch; names illustrative):
```go
type Server struct{ store *store.Store; auth Authenticator; log *slog.Logger; cfg config.Daemon }
type Scope struct{ ProjectID, TaskID, WorkspaceID, SessionID string; Role string } // lead|worker|watchdog
type Authenticator interface{ Authenticate(r *http.Request) (Scope, error) }
func (s *Server) Routes() http.Handler
type Daemon struct{ /* api, scheduler, runner, integration, watchdog */ }
func (d *Daemon) Run(ctx context.Context) error // reconcile → resume outbox → serve
```
Steps:
1. Router: Go 1.22+ `http.ServeMux` method patterns, e.g. `mux.HandleFunc("POST /v1/cross-requests/{id}/accept", h)`; read with `r.PathValue("id")`.
2. Middleware chain (Guidance): request logging (slog with IDs) → `http.MaxBytesReader(w, r.Body, 1<<20)` → auth (Bearer → `Scope`) → scope check → handler. Guidance: compare token hashes with `crypto/subtle.ConstantTimeCompare`; store only hashes. Guidance: reject requests whose `Host` is not loopback (DNS-rebinding defense).
3. Decode with `json.Decoder` + `DisallowUnknownFields` (Guidance); all mutations accept/derive an `op_id` and run in one store tx with their event.
4. Errors: Guidance — JSON body with a stable machine code distinguishing state / conflict / policy-refused so the CLI maps them to exit codes `2`/`3`/`4` (§14). HTTP status mapping is not specified in SCOPE.md; document it in `docs/api`.
5. SSE (`GET /v1/events/stream?project_id=...`): set `Content-Type: text/event-stream`, write `id: <cursor>`, `event: <type>`, `data: <json>`; flush via `http.ResponseController(w).Flush()`; resume from `Last-Event-ID` (Guidance); periodic comment keep-alive (Guidance); exit on `r.Context().Done()`.
6. Server timeouts (Guidance): set `ReadHeaderTimeout`/`IdleTimeout`; do not set a global `WriteTimeout` that kills SSE — use `ResponseController.SetWriteDeadline` per handler.
7. Daemon start: load config → (Windows) ensure VHDX attached → open store → reconcile → start outbox drain, scheduler, heartbeat sweeper (expire presence/leases, emit `session.lost`/`resource.expired`) → `ListenAndServe`. Shutdown: `srv.Shutdown(ctx)`, stop ProjFS virtualization (`PrjStopVirtualizing`, §6.5), cancel sessions per policy.
8. Service: Windows `svc.Run` handler; Linux systemd unit; macOS launchd plist (§3.1). Exact unit/plist contents not specified.
9. `/metrics`: exposition format is not specified in SCOPE.md (no metrics library in §16); Guidance — Prometheus text format via stdlib, decision recorded in `docs/decisions/`.

## Pitfalls
- Holding a DB transaction while streaming SSE or waiting on a subprocess.
- SSE without flush or with proxy-style buffering → clients see nothing; also leaking goroutines when clients disconnect.
- Trusting client-sent `workspace_id`/`task_id` instead of token scope.
- `exec.CommandContext` alone kills only the parent; children survive. Use Job Objects/process groups (§7).
- Daemon startup reconciliation must be idempotent; it can run after any crash point.
- `log_level` and per-request IDs: never log tokens or `.env`/`blocked_patterns` content.
- Unspecified: how "on demand" startup is triggered and how CLI/MCP detect a running daemon — do not invent silently; record the decision.
- `request_id` in §21 is ambiguous (cross-request ID vs HTTP request ID).

## Tests required
- Table-driven handler tests for every §12 endpoint: method, path, scope allowed/denied, 1 MiB limit, invalid input.
- Token scope: worker cannot act on another task/workspace or with another session's token; lead token has project scope.
- Enforcement (§20.4): publish without intent, integration without change, publish with violated invariant → refused.
- Manual promotion returns `pending_human_approval` (§15).
- SSE: events in order, resume from cursor, disconnect cleanup, no dup/loss with 20+ simulated sessions (§20.15); polling equivalence.
- `blocked` agent unblocked by an event (§20.7).
- §18.1 harness: daemon restart reconciles and outbox redelivers (§20.8); crash between outbox write and delivery; agent crash → `session.lost`; VHDX not mounted → `pending_mount`; ProjFS callback timeout → error, not hang.
- Cancellation kills the whole tree on Windows (§20.16) and Unix.
- Budget exceeded pauses and notifies (§20.19) via `budget.exceeded`.
- Fuzz API payload decoding (§18.1).
- Phase 4 output: 20 simulated + 5 real sessions under limits (§19).
- `-race` on all; CI on Windows, Linux, macOS (§3.1).

## Done checklist
- [ ] All §12 endpoints routed with exact methods/paths; loopback-only bind enforced.
- [ ] Bearer token → scope; scope authorization on every handler; workspace derived from token.
- [ ] 1 MiB cap, input validation, path confinement, ref-name sanitization.
- [ ] Server-side enforcement rules return policy refusals.
- [ ] SSE + polling + ACK working with cursor resume.
- [ ] `/v1/status` and `/metrics` cover §21.
- [ ] slog JSON with §21 IDs; audit rows for §17 actions.
- [ ] Daemon reconcile-on-start, outbox resume, graceful shutdown.
- [ ] Service install on Windows (svc), systemd, launchd; VHDX auto-attach before daemon.
- [ ] Timeouts and process-tree cancellation tested on all platforms.
