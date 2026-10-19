# Sprint 4 — Daemon, scheduler, SSE

> Phase 4 of [SCOPE.md §19](../SCOPE.md) · 2 weeks (10 working days) · Exit: 20 simulated + 5 real sessions run under limits; the watchdog opens a request when it detects an anomaly.

---

## 1. Goal

Until now COWFlow has worked **daemonless**: every CLI and MCP process
opens SQLite, does its job, and exits. That covers tasks, workspaces,
intents, requests, changes, manual sync, the index and polling (§5.3).
It cannot cover anything that needs a process that *stays alive*: dispatching
agents, noticing that one died, pushing events, expiring a lease when its TTL
runs out, pausing a session that spent its budget, or watching the whole system.

This sprint adds that process: the **daemon** (§3.1, §5.3). It:

- serves the loopback `/v1` HTTP API with per-session Bearer tokens (§12);
- streams events over SSE, with cursor, ACK and dedup (§9.9, §12.5);
- receives heartbeats and emits `session.lost` (§18);
- runs the **scheduler** with the §11 limits, per-provider admission, resource
  leases (§9.8) and LLM budget enforcement (§24.15);
- **reconciles** after a crash and redelivers the outbox (§18, §20.8);
- can be installed as a service: Windows svc, systemd, launchd (§3.1, §7);
- hosts the **watchdog** (§9.7).

Daemonless mode must keep working (§5.3). The daemon adds capabilities; it never
becomes a requirement for the daemonless feature set.

## 2. Exit criteria

- [ ] **§19 Phase 4 output (a):** 20 simulated + 5 real sessions run
  concurrently under limits (`max_agents`, per-provider
  `max_concurrent_requests`, `compile` slots). *Verified by* S4-T28
  (`TestPhase4Load20Simulated`) + S4-T30 (opt-in real run, evidence attached to
  the closing PR); demo §8 step 6.
- [ ] **§19 Phase 4 output (b):** watchdog opens a request upon detecting an
  anomaly. *Verified by* S4-T27 (`TestAC25WatchdogOpensRequest`); demo §8 step 7.
- [ ] **§20.8** State survives restart; outbox redelivers. *Verified by* S4-T29
  (`TestAC8CrashBetweenOutboxWriteAndDelivery`, `TestAC8StateSurvivesRestart`);
  demo §8 step 4 (kill/restart).
- [ ] **§20.15** 20+ simulated sessions without event duplication or loss.
  *Verified by* S4-T28 (`TestAC15NoEventDupOrLoss`): every consumer's cursor
  sequence is gap-free and duplicate-free after dedup by event ID.
- [ ] **§20.19** Exceeded budget pauses and notifies. *Verified by* S4-T29
  (`TestAC19BudgetPause`): 80% → warning, 100% → session paused,
  `budget.exceeded`, workspace preserved; demo §8 step 5.
- [ ] **§20.25** Watchdog detects an injected anomaly and opens a cross-request.
  *Verified by* S4-T27.
- [ ] **§20.30** `compile.slots = 3`, 6 workers → at most 3 run, others queued,
  `resource.granted` in order, a dead session's lease expires and frees the
  slot. *Verified by* S4-T28 (`TestAC30Leases`); demo §8 step 3.
- [ ] **Daemonless regression (§5.3):** the S2-E11 two-terminal scenario still
  passes with **no** daemon running. *Verified by* S2-E11 test in CI; demo §8 step 1.

All of the above run with `go test -race` on Windows, Linux and macOS, except
the opt-in real-LLM run (S4-T30).

## 3. Prerequisites

### 3.1 Earlier epics (must be merged on `develop` before day 1)

| Epic | What this sprint uses |
|---|---|
| `S0-E1`, `S0-E2` | cobra tree with `daemon`, `resources`, `resource`, `exec`, `budget`, `events` stubs; exit codes; `[daemon]`, `[scheduler]`, `[scheduler.llm*]`, `[[resources]]`, `[collaboration]`, `[watchdog]` parsing |
| `S0-E3` | SQLite store, WAL, `busy_timeout`, migrations, `WithTx` |
| `S0-E5` | Job Objects / process-group tree kill, path canonicalization |
| `S0-E6` | Dev Drive attach (needed before the daemon opens a `state_dir` on the VHDX) |
| `S0-E8` | hermetic harness: fake clock, crash points |
| `S1-E1`, `S1-E6` | `WorkspaceProvider`, dirty detection (reconciliation of worktrees) |
| `S2-E1`, `S2-E2` | tables, event store, transactional outbox, cursor polling, `op_id` |
| `S2-E3`, `S2-E4`, `S2-E8` | presence + TTL, intents, cross-requests (SLA fields), contracts |
| `S2-E5`, `S2-E6`, `S2-E7` | change publish, index, sync services exposed by the API |
| `S2-E10`, `S2-E11` | daemonless CLI client and the regression scenario |
| `S3-E1`, `S3-E2` | MCP server + role tool sets; per-session tokens, lead token |
| `S3-E4`, `S3-E5` | runner framework, Claude Code and Codex runners |
| `S3-E6` | server-side enforcement in the domain layer (reused by every handler) |
| `S3-E7` | per-provider LLM budget **accounting** (this sprint adds **enforcement**) |

### 3.2 SPEC-GAPS affecting this sprint

IDs are the stable ones from [SPEC-GAPS.md](../SPEC-GAPS.md). **None of these is decided here.** `AG-LEAD` takes
them to the maintainer at kickoff (day 1). Blocked work codes against the
interface in §6 and uses a fake.

| ID | Gap | Blocks |
|---|---|---|
| `U-API-1` | SQLite file name/location under `state_dir`; `config.toml` location; lead token storage | T01 (reuse whatever S0/S3 decided; otherwise blocked) |
| `U-API-2` | How the daemon starts on demand; how CLI/MCP detect daemon vs daemonless | T02 production `ModeResolver`, T03 on-demand start, T10 mode switch |
| `U-API-3` | `/metrics` format | T07 `/metrics` renderer (`/v1/status` is not blocked) |
| `U-API-4` | HTTP status ↔ CLI exit code mapping | T04 `statusFor(kind)` table; tests assert on `error.kind` until decided |
| `U-API-5` | `request_id` in logs: cross-request vs HTTP request | T04 log field naming |
| `U-API-8` | How session tokens are validated in daemonless mode | T02/T10 mode switch (token must work in both modes) |
| `U-API-9` | Watchdog tool set (worker base + extras vs read-only + requests) | T05 watchdog row of the authorization matrix, T26 |
| `U-API-11` | Which §22 values are defaults vs examples | T13/T17 defaults for keys absent from user config |
| `U-DATA-1` | Columns of `events`, `outbox`, `event_deliveries`; where `op_id` lives | T22 (reuse S2-E2's choice) |
| `U-DATA-3` | Whether the event cursor is the ULID | T08 SSE `id:` (cursor is opaque here; reuse S2-E2's choice) |
| `U-DATA-5` | Values of `resource_leases.state` | T16, T17 (code uses named constants; values set by decision) |
| `U-PLAT-5` | VHDX auto-attach at boot: scheduled task vs service | T25 ordering of attach before the service |
| `U-PLAT-6` | `CTRL_BREAK` cancellation when running as a Windows service | T14 graceful-cancel path under service (hard kill path is not blocked) |
| `U-SCHED-1` | Whether leases and SLA escalation work daemonless | T17 daemonless entry points, T12 escalation in daemonless mode |
| `U-SCHED-2` | `acquire(wait=true, timeout)` timing out while queued | T17 timeout branch, T19 `--wait` exit path |
| `U-SCHED-3` | Precedence between `max_builds`/`max_tests` and `[[resources]]` slots | T17 alias synthesis (only the "both equal / only one present" cases proceed) |
| `U-SCHED-4` | Budget actions other than `pause`; whether 80%/100% applies to provider hourly/daily budgets; how usage is measured per runner | T21 provider-level warnings; T29/T30 real usage source |
| `U-SCHED-5` | Tie-breaking within a priority | T17 `priority` queue ordering inside one class |
| `U-SCHED-7` | Kind/config of the `require_resource_exec` invariant | out of this sprint (not started) |
| `C5` | `max_concurrent_per_target` vs `max_integrations_per_target` | T17 `integrate:<target>` alias name source |
| `C9` | Per-priority SLA (15/60 min) vs `escalation_after_minutes = 30` | T12 escalation tick |
| `C11` | Two different `[scheduler.llm.anthropic]` examples | T13/T21 test fixtures use explicit values only |

**Gaps found while planning** (already in SPEC-GAPS.md; not decided here):

| ID | Gap | Blocks |
|---|---|---|
| `U-API-19` | Daemon process model: does `daemon start` detach or stay in the foreground; how `daemon stop` reaches the process; one daemon per `state_dir` or per machine; service name | T01 instance identity, T03 `stop`, T25 service name |
| `U-API-20` | §12 lists no `/v1` endpoints for resources (§9.8), budget, or `/v1/sessions` spawn; CLI `resource …`/`budget` in daemon mode have no route | T19/T20 daemon-mode transport (library path works, see T17) |
| `U-SCHED-9` | Channel for the 80% budget warning "to the agent and the lead": §9.9 has no warning event | T21 `BudgetNotifier` implementation |
| `U-API-21` | Allowed values of `task create --kind watchdog --watch <x>` (§14 shows `integration`; §9.7 lists integration, requests, disk) | T26 validation of `--watch` |
| `U-PLAT-17` | Windows service account (LocalSystem vs user) given per-user tokens, repos and VHDX attach | T25 `Install` on Windows |
| `U-SCHED-10` | Meaning of "session paused" for a headless LLM CLI (stop the process tree and keep the workspace vs suspend) | T21 pause action, T13 resume |

## 4. Concepts you need

**Daemons and services.** A daemon is a long-running background process. An OS
*service manager* starts it at boot, restarts it on failure and stops it at
shutdown. Windows uses the Service Control Manager (SCM): your program calls
`svc.Run` and answers Stop/Shutdown requests; a service has **no console**.
Linux uses systemd units (`ExecStart=`, `Restart=on-failure`); macOS uses
launchd property lists. COWFlow can also run the daemon on demand (§3.1).
Links: [Windows services](https://learn.microsoft.com/en-us/windows/win32/services/services),
[x/sys/windows/svc](https://pkg.go.dev/golang.org/x/sys/windows/svc),
[systemd.service](https://www.freedesktop.org/software/systemd/man/latest/systemd.service.html),
[launchd jobs](https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html).

**Loopback HTTP + Bearer tokens.** Binding to `127.0.0.1` means only processes
on this machine can connect, but *any* local process can, including a browser
page making requests to `127.0.0.1:8787`. So every request still carries
`Authorization: Bearer <token>`, and the token decides the scope
(project/task/workspace). We also check the `Host` header to blunt DNS
rebinding. Link: [RFC 6750](https://datatracker.ietf.org/doc/html/rfc6750),
[DNS rebinding](https://en.wikipedia.org/wiki/DNS_rebinding).

**Server-Sent Events (SSE).** A plain HTTP response that never ends, with
`Content-Type: text/event-stream`. The server writes blocks like
`id: 42\nevent: resource.granted\ndata: {...}\n\n` and flushes. A reconnecting
client sends `Last-Event-ID` so the server resumes after it. Lines starting
with `:` are comments (keep-alives). One direction only; the client ACKs with a
separate `POST /v1/events/ack`. Links:
[WHATWG spec](https://html.spec.whatwg.org/multipage/server-sent-events.html),
[MDN guide](https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events/Using_server-sent_events),
[`http.ResponseController`](https://pkg.go.dev/net/http#ResponseController).

**Heartbeats and TTLs.** A session proves it is alive by calling
`POST /v1/presence/heartbeat` every `heartbeat_interval_seconds` (20). If nothing
arrives for `presence_ttl_seconds` (90), the daemon declares it dead and emits
`session.lost`. TTL > several intervals so one late heartbeat is not a death.
Link: [Heartbeat pattern](https://martinfowler.com/articles/patterns-of-distributed-systems/heartbeat.html).

**Semaphores and leases.** A counting semaphore lets at most N holders in. A
*lease* is a semaphore slot with an expiry: if the holder dies, the slot comes
back by itself when the TTL ends. Renewal (here: via heartbeat) extends it.
COWFlow leases limit **resource consumption** (compiler, test DB), never file
access (§9.8). Links: [Lease pattern](https://martinfowler.com/articles/patterns-of-distributed-systems/lease.html),
[Semaphore](https://en.wikipedia.org/wiki/Semaphore_(programming)).

**Fair queues.** When all slots are taken, requesters wait in a queue. FIFO
serves in arrival order; a priority queue lets `high` jump ahead. Fairness
also means **no barging**: a newcomer must not grab a freed slot while others
wait. The grant to the head of the queue must happen in the *same transaction*
that frees the slot, or two processes can grant the same slot.

**Budgets.** Each LLM provider costs tokens and money. COWFlow records usage
(S3-E7) and now *acts* on it: 80% of a budget → warning; 100% → the session is
paused, `budget.exceeded` is emitted, the workspace is kept (§24.15). Retries
count against the same budget.

**Transactional outbox, at-least-once, dedup.** A state change and its event
are written in one SQLite transaction (S2-E2). A dispatcher later delivers
outbox rows. If the daemon crashes after the write and before delivery, the
row is still there and is delivered after restart: *at least once*. Consumers
therefore dedup by event ID. Link:
[Transactional outbox](https://microservices.io/patterns/data/transactional-outbox.html).

**Reconciliation.** After a crash, memory is gone; SQLite, Git, the filesystem
and the OS process table remain. The daemon compares what SQLite *believes*
with what *is* (is the process alive? does the worktree exist? does the ref
exist?) and repairs SQLite, never the other way round for code: Git is
canonical (§2.2 #6, #8). It must be idempotent: running it twice changes
nothing the second time. Link:
[Controller reconcile loop](https://kubernetes.io/docs/concepts/architecture/controller/).

**Watchdogs.** A watchdog is an observer: it reads events and metrics and
reacts when something looks wrong. In COWFlow it is a special agent session
(`kind = "watchdog"`) with no workspace that can only open requests, comment
and escalate (§9.7). Link:
[Monitoring distributed systems](https://sre.google/sre-book/monitoring-distributed-systems/).

**Load testing for correctness.** We do not measure throughput here; we check
that invariants hold under concurrency: no lost or duplicated events, never
more than N lease holders, limits respected. Use the fake clock, many
goroutines/processes, `-race`, and assertions over the recorded event log.
Link: [Go race detector](https://go.dev/doc/articles/race_detector).

## 5. Agents and lanes

| Agent | Tasks |
|---|---|
| `AG-LEAD` | kickoff, gaps (§3.2), contract freeze (§6), reviews, demo with `AG-QA` |
| `AG-API` | T01, T02, T04, T05, T06, T07, T08, T11 |
| `AG-CLI` | T03, T09, T15, T19 |
| `AG-MCP` | T10, T20 |
| `AG-STORE` | T16, T22, T23 |
| `AG-SCHED` | T13, T14, T17, T18, T21 |
| `AG-COLLAB` | T12, T26 |
| `AG-PLAT` | T25 |
| `AG-WS` | T24 |
| `AG-QA` | T27, T28, T29, T30 |
| `AG-SEC` | T31; reviewer on every `security: yes` task |
| `AG-GIT`, `AG-INT` | reviewers (T06, T24); `AG-INT` prepares Sprint 5 against the T17 lease API |

```text
Day        1      2      3      4      5      6      7      8      9      10
AG-LEAD  [KO+gaps][contracts frozen]------ reviews / merges ------[demo][close]
AG-API   [T01  ][T04  ][T05  ][T02  ][T06 ........ ][T08  ][T11  ][T07  ][fix ]
AG-STORE [T16 ......][T22 ............][T23 ........ ][support / review .....]
AG-SCHED [T17 (fake store) .][T18  ][T13 ........ ][T14  ][T21  ][fix .......]
AG-CLI          [T03 (fake client)][..][T19  ][T09  ][T15  ][demo support ..]
AG-MCP                        [T10 ............... ][T20  ][fix ............]
AG-COLLAB[T12 ......][T26 ........................ ][fix ...................]
AG-PLAT  [T25 lock ][T25 service ................. ][fix ...................]
AG-WS                         [T24 ....... ]
AG-QA    [T28 driver (fakes) ...........][T27  ][T29  ][T28 run][T30 ][demo]
AG-SEC   [reviews ................................ ][T31 ........ ][demo   ]
```

**Critical path:** T04 → T05 → T06 → T08 → T10 → T28 → T30 → demo, joined by
T16 → T17 → T18 → T13 → T14 → T28. Any slip in T05 (auth) or T17 (leases)
moves the demo.

## 6. Contracts

Frozen on day 1 by `AG-LEAD`. Names are illustrative Go; route paths, event
names and config keys are the spec's.

```go
// ---- internal/api — provider AG-API; consumers AG-CLI, AG-MCP, AG-QA ----

type Role string // "lead" | "worker" | "watchdog" (§15.1)

// Scope is derived from the token; never from the request body (§15).
type Scope struct {
	ProjectID, TaskID, WorkspaceID, SessionID string
	Role                                      Role
}

type Authenticator interface {
	Authenticate(r *http.Request) (Scope, error) // wraps S3-E2 token resolution
}

// ErrorKind is what the CLI maps to exit codes (§14). The HTTP status per
// kind is U-API-4: one function statusFor(kind) fills it after the decision.
type ErrorKind string

const (
	KindUsage    ErrorKind = "usage"    // exit 1
	KindState    ErrorKind = "state"    // exit 2
	KindConflict ErrorKind = "conflict" // exit 3
	KindPolicy   ErrorKind = "policy"   // exit 4
	KindAuth     ErrorKind = "auth"
	KindNotFound ErrorKind = "not_found"
	KindInternal ErrorKind = "internal"
)

type Error struct {
	Kind    ErrorKind `json:"kind"`
	Code    string    `json:"code"`    // stable machine code, e.g. "intent_required"
	Message string    `json:"message"` // never contains paths or tokens
}

// Services: handlers are thin adapters over the SAME services the daemonless
// CLI/MCP already call (S2-E10, S3-E1), so enforcement (S3-E6) is shared.
type Services struct {
	Projects   ProjectService   // AG-STORE (S2-E1)            §12.1
	Tasks      TaskService      // AG-STORE; run/pause/resume → Scheduler
	Workspaces WorkspaceService // AG-WS (S1-E1)               §12.1
	Sessions   SessionService   // AG-SCHED (T14)              §12.1
	Presence   PresenceService  // AG-COLLAB (S2-E3, T12)      §12.2
	Intents    IntentService    // AG-COLLAB (S2-E3)           §12.2
	Contracts  ContractService  // AG-COLLAB (S2-E8)           §12.2
	Index      IndexService     // AG-STORE (S2-E6)            §12.2
	Requests   RequestService   // AG-COLLAB (S2-E4)           §12.3
	Changes    ChangeService    // AG-GIT (S2-E5)              §12.4
	Sync       SyncService      // AG-GIT (S2-E7)              §12.4
	Events     EventFeed        // AG-STORE (T22)              §12.5
	Status     StatusSource     // AG-API (T07)                §12.5
}
// §12.5 integration-group routes are registered by S5-E1, not here.

// ---- internal/api/client — provider AG-API; consumers AG-CLI, AG-MCP ----

type Mode string // "daemon" | "daemonless"

// ModeResolver hides U-API-2. Until decided only StaticResolver exists.
type ModeResolver interface {
	Resolve(ctx context.Context) (Mode, *url.URL, error)
}

type Client interface {
	Do(ctx context.Context, method, path string, in, out any) error // returns *api.Error
	Stream(ctx context.Context, projectID, lastEventID string,
		fn func(domain.Event) error) error // GET /v1/events/stream, reconnects
}

// ---- internal/store — provider AG-STORE; consumers AG-API, AG-SCHED, AG-COLLAB ----

type EventFeed interface {
	After(ctx context.Context, projectID, after string, limit int) ([]domain.Event, string, error)
	Subscribe(projectID string) (wake <-chan struct{}, cancel func()) // level-triggered, cap 1
	Ack(ctx context.Context, subscriber, projectID, cursor string) error
	LastAck(ctx context.Context, subscriber, projectID string) (string, error)
}

type OutboxHandler interface {
	Name() string
	Handle(ctx context.Context, ev domain.Event) error // must be idempotent
}

type Reconcilable interface {
	Name() string
	Order() int // lower runs first
	Reconcile(ctx context.Context, now time.Time) (ReconcileReport, error)
}

type Reconciler interface {
	Register(r Reconcilable)
	RunAll(ctx context.Context) ([]ReconcileReport, error)
}

// ---- internal/scheduler — provider AG-SCHED; consumers AG-API, AG-CLI, AG-MCP, AG-INT ----

type Scheduler interface {
	Run(ctx context.Context) error // dispatch loop, daemon only (§5.3)
	Start(ctx context.Context, opID, projectID string, opt RunOptions) error
	Pause(ctx context.Context, opID, taskID string, reason PauseReason) error
	Resume(ctx context.Context, opID, taskID string) error
	CancelSession(ctx context.Context, opID, sessionID string) error
}

type RunOptions struct {
	TaskIDs   []string
	MaxAgents int    // `cowflow run --max-agents`, capped by [scheduler] max_agents
	Runner    string // `--runner`
}

type PauseReason string // "lead" | "budget" (Guidance)

type LeaseManager interface {
	Check(ctx context.Context, resource string) (ResourceStatus, error) // {slots, free, queue, holders}
	Acquire(ctx context.Context, req AcquireRequest) (Grant, error)      // {lease_id, acquired} | {queued, position}
	Release(ctx context.Context, opID string, sc Scope, leaseID string) error
	Revoke(ctx context.Context, opID string, lead Scope, leaseID string) error
	Announce(ctx context.Context, opID string, sc Scope, resource string, eta time.Duration) error
	RenewSession(ctx context.Context, sessionID string, now time.Time) (int, error) // heartbeat
	ExpireDue(ctx context.Context, now time.Time) (int, error)                      // sweeper
}

type AcquireRequest struct {
	OpID     string
	Scope    Scope
	Resource string
	TTL      time.Duration // 0 → lease_ttl_seconds; `--ttl`
}

type Grant struct {
	LeaseID  string `json:"lease_id,omitempty"`
	Acquired bool   `json:"acquired"`
	Queued   bool   `json:"queued,omitempty"`
	Position int    `json:"position,omitempty"`
}

type BudgetEnforcer interface {
	OnUsage(ctx context.Context, u domain.Usage) error // called by S3-E7 accounting
	Status(ctx context.Context, projectID string) ([]BudgetStatus, error)
}

type BudgetNotifier interface { // channel is U-SCHED-9; fake in tests
	Warn(ctx context.Context, taskID, sessionID string, percent int) error
}

// ---- internal/collab — provider AG-COLLAB; consumer AG-API (liveness sweeper) ----

type PresenceService interface {
	Heartbeat(ctx context.Context, opID string, sc Scope, st domain.PresenceState, reason string) error
	ExpireStale(ctx context.Context, now time.Time) ([]string, error) // emits session.lost
}

type Escalator interface {
	EscalateOverdue(ctx context.Context, now time.Time) (int, error) // C9
}

// ---- internal/platform — provider AG-PLAT; consumers AG-API, AG-CLI ----

type InstanceLock interface{ Release() error }

func LockInstance(dir string) (InstanceLock, error) // ErrLocked when another daemon holds it

type ServiceSpec struct {
	Name, DisplayName, Exe string
	Args                   []string
}

type ServiceInstaller interface {
	Install(ctx context.Context, s ServiceSpec) error
	Status(ctx context.Context, name string) (ServiceStatus, error)
}

// RunAsService returns isService=false when not started by a service manager.
func RunAsService(name string, run func(ctx context.Context) error) (isService bool, err error)
```

**Fakes (land with the contract PR on day 1, in each provider's package under
`…/fake`):** `apifake.Client` (records calls, scripted errors),
`storefake.Feed` (in-memory events + wake channel), `schedfake.Leases`,
`schedfake.Scheduler`, `collabfake.Presence`, `platformfake.Lock`,
`platformfake.Installer`. All take the harness `Clock`.

## 7. Tasks

### S4-T01 — Daemon runtime skeleton

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-API` | `AG-LEAD`, `AG-CLI` | `S4-E1` | `S0-E2`, `S0-E3`, `S0-E6`; T25 lock (fake until merged) | `feat/daemon-runtime` | no |

**Skills:** `local-api-daemon`, `go-conventions`, `windows-platform`   **Spec:** §3.1, §5.3, §7, §18, §22 `[daemon]`
**Files:** `internal/daemon/daemon.go`, `internal/daemon/components.go`, `internal/daemon/daemon_test.go`

**Why / what** — One type owns the daemon's life: start order, the components
that only exist in daemon mode (§5.3), and a clean shutdown. Everything else
(API, sweeper, scheduler, outbox dispatcher, watchdog) plugs in as a component.

**Steps**
1. Define a component: `type Component interface{ Name() string; Run(ctx context.Context) error }`.
2. Implement start order (§7: VHDX before daemon; §18: reconcile before serving):
   ```go
   func (d *Daemon) Run(ctx context.Context) error {
       if err := d.plat.EnsureStateVolume(ctx, d.cfg.Daemon.StateDir); err != nil {
           return fmt.Errorf("state volume: %w", err) // never fall back to NTFS (§18)
       }
       lock, err := d.lockFn(d.cfg.Daemon.StateDir) // one daemon per state_dir (Guidance; U-API-19)
       if err != nil { return fmt.Errorf("daemon already running: %w", err) }
       defer lock.Release()
       st, err := d.openStore(ctx)
       if err != nil { return err }
       defer st.Close()
       if _, err := d.reconciler.RunAll(ctx); err != nil { return fmt.Errorf("reconcile: %w", err) }
       ctx, cancel := context.WithCancel(ctx)
       defer cancel()
       var wg sync.WaitGroup
       errs := make(chan error, len(d.components))
       for _, c := range d.components {
           wg.Go(func() { if err := c.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
               errs <- fmt.Errorf("%s: %w", c.Name(), err); cancel() } })
       }
       wg.Wait()
       close(errs)
       return errors.Join(drain(errs)...)
   }
   ```
3. Shutdown: cancel ctx → API `srv.Shutdown` with a 10 s grace (Guidance) → scheduler stops dispatching → wait.
4. slog JSON handler at `[daemon] log_level`; base attrs `project_id` when known.

**Tests** — fake components: one failing stops all and `Run` returns its error;
cancel returns nil; second `Run` on the same `state_dir` fails with `ErrLocked`;
reconcile runs before any component (order recorded by fakes).
`go test -race ./internal/daemon/...`

**Acceptance**
- [ ] Start order: volume → lock → store → reconcile → components.
- [ ] Every goroutine has an owner and exits on cancel; `-race` clean.
- [ ] No `golang.org/x/sync` (use `sync.WaitGroup.Go`, Go 1.25).

**Pitfalls** — Default `state_dir = "X:/state"` lives on the Dev Drive: if the
VHDX is not attached the store cannot open, so attach must come first. Do not
call `os.Exit` inside the package. Do not add a dependency for errgroup.

### S4-T02 — API client and mode resolver

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-API` | `AG-LEAD`, `AG-CLI`, `AG-MCP`, `AG-SEC` | `S4-E1` | T04 error envelope | `feat/api-client-mode-resolver` | yes |

**Skills:** `local-api-daemon`, `security-review`   **Spec:** §5.2, §5.3, §12, §14
**Files:** `internal/api/client/client.go`, `internal/api/client/stream.go`, `internal/api/client/mode.go`, tests

**Why / what** — CLI and MCP must call the daemon the same way, decode the
same error envelope, and map it to the same exit codes. Mode detection is
`U-API-2`, so it sits behind `ModeResolver`.

**Steps**
1. `Do`: JSON body ≤ 1 MiB, `Authorization: Bearer <token>`, decode `api.Error` on non-2xx into a Go error carrying `Kind`.
2. `ExitCode(err) int`: `usage→1`, `state→2`, `conflict→3`, `policy→4` (§14). Other kinds: leave to `U-API-4` decision; until then return `2` behind a single function marked `// TODO(U-API-4)`.
3. `Stream`: reconnect loop with `Last-Event-ID`; backoff on the fake clock; parse `id:`, `event:`, `data:` lines; ignore `:` comments.
4. `StaticResolver{Mode, URL}` for tests. Production resolver: **blocked by `U-API-2`**; write the interface and a doc comment listing the maintainer's options only.
5. Refuse non-loopback base URLs in the client too (defense in depth).

**Tests** — `httptest.Server` table tests: error kinds → exit codes; 1 MiB+ body
rejected client-side; stream parser with split chunks, comments, reconnect from
the last id; non-loopback URL refused.

**Acceptance**
- [ ] One client used by CLI and MCP; no duplicated HTTP code elsewhere.
- [ ] Token never logged (slog redaction test).
- [ ] Production `ModeResolver` not merged before the `U-API-2` decision.

**Pitfalls** — `bufio.Scanner` default 64 KiB token limit breaks on large
`data:` lines; set `Buffer(…, 1<<20)`. Don't treat EOF on a stream as success;
reconnect.

### S4-T03 — CLI `daemon start|stop|status|install`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-CLI` | `AG-LEAD`, `AG-API`, `AG-SEC` | `S4-E1` | T01, T02 (fakes ok), T25 for `install` | `feat/cli-daemon-commands` | yes |

**Skills:** `cli-and-config`, `local-api-daemon`   **Spec:** §3.1, §5.3, §14 (Setup), §22 `[daemon]`
**Files:** `internal/cli/daemon.go`, `internal/cli/daemon_test.go`, `internal/cli/client.go`

**Why / what** — The user-facing switch. `daemon start` runs `daemon.Run`;
`status` calls `GET /v1/status`; `install` calls AG-PLAT's installer. Every
other command picks daemon or daemonless through `ModeResolver`.

**Steps**
1. `daemon start`: build deps, call `platform.RunAsService` first; if not a service, call `daemon.Run(ctx)` with ctx cancelled on Ctrl+C. Detach vs foreground: **blocked by `U-API-19`** (implement foreground only).
2. `daemon status`: `GET /v1/status` with the lead token; `--json` prints the body plus `pid` (Guidance, used by the demo kill step); human mode prints mode, uptime, sessions by state. Daemon not reachable → exit `2` with "daemon not running" (Guidance).
3. `daemon stop`: **blocked by `U-API-19`**; command exists, returns a `usage`-free "not yet supported" error until decided.
4. `daemon install`: `ServiceInstaller.Install(ServiceSpec{Exe: os.Executable(), Args: ["daemon","start"]})`.
5. Daemon-only commands (`run`, `resources`, `exec`, `events --follow` via SSE, …) in daemonless mode return a `state` error naming the missing capability (§5.3); never fake them.

**Tests** — command tree golden list includes all four subcommands; `status`
against `httptest` returns JSON; daemon-only command with `StaticResolver{daemonless}` exits `2`.

**Acceptance**
- [ ] `cowflow daemon start` runs the daemon in the foreground and stops on Ctrl+C.
- [ ] `--json` on all four subcommands; exit codes per §14.
- [ ] No behavior chosen for `U-API-2`/`U-API-19` beyond what is written here.

**Pitfalls** — Windows: `RunAsService` must be checked before touching stdin/stdout.
Don't print the lead token in `status`.

### S4-T04 — HTTP server, loopback listener, middleware, error envelope

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-API` | `AG-LEAD`, `AG-SEC`, `AG-CLI` | `S4-E2` | T01 | `feat/api-server-middleware` | yes |

**Skills:** `local-api-daemon`, `security-review`, `go-conventions`   **Spec:** §12, §17, §21, §22 `[daemon] bind`
**Files:** `internal/api/server.go`, `internal/api/listen.go`, `internal/api/middleware.go`, `internal/api/errors.go`, tests

**Why / what** — The frame every handler runs inside: loopback only, size
limit, logging, auth hook, uniform errors. Get it right once.

**Steps**
1. Listener refuses non-loopback:
   ```go
   func ListenLoopback(bind string) (net.Listener, error) {
       host, port, err := net.SplitHostPort(bind)
       if err != nil { return nil, err }
       ip := net.ParseIP(host)
       if ip == nil || !ip.IsLoopback() { return nil, ErrNonLoopbackBind }
       return net.Listen("tcp", net.JoinHostPort(ip.String(), port))
   }
   ```
2. `http.Server{ReadHeaderTimeout: 10s, IdleTimeout: 120s}`; **no** global `WriteTimeout` (it kills SSE).
3. Middleware order: recover → request logging (slog: method, path, status, duration, `session_id`; field name for the HTTP request id waits on `U-API-5`) → Host check (loopback names + port only, Guidance) → `http.MaxBytesReader(w, r.Body, 1<<20)` → authenticate (T05) → route.
4. `writeError(w, r, err)`: map sentinel errors from services (`errors.Is`) to `ErrorKind`; `statusFor(kind)` is a single table marked `U-API-4`; until decided use one provisional status per kind recorded in the PR for the maintainer.
5. Strict decoding helper: `json.Decoder` + `DisallowUnknownFields` + reject trailing data.
6. Seed fuzz target `FuzzDecodeBody` (§18.1).

**Tests**
```bash
go test -race ./internal/api/... -run 'Listen|Middleware|Error'
```
`0.0.0.0:8787`, `[::]:8787`, `192.168.1.5:8787` refused; `127.0.0.1:0` and
`[::1]:0` accepted; 1 MiB + 1 byte → `usage`; `Host: evil.example` refused;
panic in handler → `internal` envelope, server alive.

**Acceptance**
- [ ] Non-loopback bind impossible, including via config.
- [ ] Every error response is the `api.Error` envelope.
- [ ] Tests assert `error.kind`, not HTTP status (until `U-API-4`).

**Pitfalls** — `localhost` may resolve to a non-loopback address on odd hosts;
require a literal IP in `bind` or resolve and check every address. Logging the
`Authorization` header. Reading the body before `MaxBytesReader`.

### S4-T05 — Token authentication and scope authorization

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-API` | `AG-LEAD`, `AG-SEC`, `AG-MCP` | `S4-E2` | T04, `S3-E2` | `feat/api-token-auth` | yes |

**Skills:** `local-api-daemon`, `security-review`, `mcp-and-runners`   **Spec:** §12, §15, §15.1, §17, §24.14
**Files:** `internal/api/auth.go`, `internal/api/authz.go`, `internal/api/routes.go` (permission column), tests

**Why / what** — The token is the identity. A worker may only touch its own
task/workspace/session; the lead token covers the project (§15.1).

**Steps**
1. `Authenticate`: `strings.CutPrefix(h, "Bearer ")`; resolve via the S3-E2 token store (hash lookup, `crypto/subtle.ConstantTimeCompare`, expiry/revocation as S3-E2 defined). Put `Scope` in the request context.
2. Route table with a required permission per route; registration panics in tests if a route has none (deny by default):
   ```go
   type route struct{ pattern string; perm Perm; h http.HandlerFunc }
   var table = []route{
       {"POST /v1/tasks", PermLead, h.createTask},
       {"POST /v1/presence/heartbeat", PermOwnSession, h.heartbeat},
       {"POST /v1/cross-requests/{id}/reassign", PermLead, h.reassignRequest},
       // …every §12.1–§12.5 route
   }
   ```
3. Authorization matrix mirrors the MCP role tool sets (§15, §15.1): worker routes = worker tools; lead = all; watchdog row **blocked by `U-API-9`** (start with read-only events/status + `POST /v1/cross-requests` + comments, flagged in code).
4. Resource-level checks: `{id}` in the path must belong to `Scope.ProjectID`, and to `Scope.TaskID` for worker-owned resources (workspace, session). Any `workspace_id`/`session_id` in a body is ignored or refused.

**Tests** — missing/invalid/expired token → `auth`; worker token on another
task's workspace → `policy`/forbidden kind; another session's token on
`POST /v1/sessions/{id}/cancel` refused (§24.14); lead token works project-wide;
table test that every registered route has a permission.

**Acceptance**
- [ ] Deny by default; no route without a permission.
- [ ] Scope only from token; body identity fields never trusted.
- [ ] Lead-only routes (`reassign`, `close` of others, `promote`) refuse worker tokens.

**Pitfalls** — Comparing tokens with `==`. Caching resolved tokens without
revocation. `curl` in PowerShell is an alias: use `curl.exe` in docs.

### S4-T06 — Route groups §12.1–§12.4 over the service layer

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-API` | `AG-LEAD`, `AG-SEC`, `AG-COLLAB`, `AG-GIT` | `S4-E2` | T05, `S2-E3`…`S2-E8`, `S3-E6` | `feat/api-route-groups` | yes |

**Skills:** `local-api-daemon`, `collaboration-protocol`, `security-review`   **Spec:** §12.1–§12.4, §15.5, §2.2 #3
**Files:** `internal/api/h_projects.go`, `h_tasks.go`, `h_workspaces.go`, `h_sessions.go`, `h_presence.go`, `h_intents.go`, `h_requests.go`, `h_changes.go`, `h_sync.go`, `internal/api/routes_test.go`

**Why / what** — Expose exactly the §12 endpoints. Handlers decode, call the
service the daemonless path already uses, encode. No business rules in handlers:
enforcement (no intent → no publish, invariants) stays in the domain (S3-E6).

**Steps**
1. Register every path/method of §12.1–§12.4 verbatim, e.g. `mux.HandleFunc("POST /v1/cross-requests/{id}/accept", …)`, read `r.PathValue("id")`.
2. Each mutation derives an `op_id` (Guidance: from an `Idempotency-Key`-style header if S2-E2 defined one, else server-generated) and passes it down.
3. `POST /v1/tasks/{id}/run|pause|resume` → `Scheduler` (T13); `POST /v1/sessions/{id}/cancel` → `Scheduler.CancelSession` (T14).
4. Example:
   ```bash
   curl.exe -s -X POST -H "Authorization: Bearer $env:COWFLOW_SESSION_TOKEN" `
     -d '{"goal":"JWT auth","paths":["src/auth"]}' http://127.0.0.1:8787/v1/intents
   ```

**Tests** — `routes_test.go` golden list compares registered patterns with the
§12.1–§12.4 list; per group: happy path, invalid JSON → `usage`, foreign scope
refused, enforcement refusals bubble up as `policy` (publish without intent →
`policy`, §20.4).

**Acceptance**
- [ ] All §12.1–§12.4 routes present with exact methods/paths; nothing extra.
- [ ] Same `--json` result via API and daemonless for one scenario per group.
- [ ] Mutations idempotent by `op_id` (replay returns the first result).

**Pitfalls** — Re-implementing a rule in the handler (bypassable by daemonless
path). Holding a DB transaction while calling Git. Trusting `?path=` without
confinement (§12 "confined paths").

### S4-T07 — `/v1/status` and `/metrics`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-API` | `AG-LEAD`, `AG-SCHED`, `AG-STORE` | `S4-E2` | T06, T13, T17, T21 | `feat/api-status-metrics` | no |

**Skills:** `local-api-daemon`   **Spec:** §3.2 (no dashboard), §9.7, §12.5, §21
**Files:** `internal/api/status.go`, `internal/api/metrics.go`, tests

**Why / what** — One snapshot struct feeds `/v1/status` (JSON), `/metrics`,
`cowflow status`, and the watchdog's `cowflow_read_metrics`.

**Steps**
1. `type Snapshot struct` with the §21 fields available this sprint: agents by state, tasks by state and duration, pending events, open/overdue/escalated requests, changes by state, sync conflicts by cause, retries/cancellations/orphans, tokens and cost per session/task/day, resource holders/queue. Workspace disk and `CowKind` from AG-WS; projfs/gate fields left zero until S5/S6.
2. `StatusSource` aggregates via read-only queries; no locks held across calls.
3. `GET /v1/status` → JSON snapshot.
4. `GET /metrics`: renderer **blocked by `U-API-3`**. Route registered; handler behind `type MetricsRenderer interface{ Render(io.Writer, Snapshot) error }` with no production impl until decided. No metrics library (§16).

**Tests** — snapshot from a seeded store equals expected; `/v1/status` with
lead token; worker scope sees only its project.

**Acceptance**
- [ ] `/v1/status` returns every §21 field that has data in Sprint 4.
- [ ] `/metrics` format not chosen in code before the decision.

**Pitfalls** — Expensive `COUNT(*)` per request under 20+ sessions: one query
per group, not per row. Leaking task titles to a watchdog outside its project.

### S4-T08 — SSE stream, polling and ACK

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-API` | `AG-LEAD`, `AG-STORE`, `AG-SEC` | `S4-E3` | T05, T22 | `feat/api-sse-events` | yes |

**Skills:** `local-api-daemon`, `store-and-events`   **Spec:** §9.9, §12.5, §18 "Lost event", §22 `event_delivery = "sse"`
**Files:** `internal/api/h_events.go`, `internal/api/sse.go`, tests

**Why / what** — Push events instead of polling: `GET /v1/events/stream`,
plus `GET /v1/events?after=<cursor>` (always available) and `POST /v1/events/ack`.
At-least-once: resume from `Last-Event-ID` or the last ACK; consumers dedup by ID.

**Steps**
1. Stream loop (pull-from-store, wake-on-notify; no per-client in-memory queue):
   ```go
   func (h *events) stream(w http.ResponseWriter, r *http.Request) {
       sc := ScopeFrom(r.Context())
       pid := r.URL.Query().Get("project_id")
       if err := h.authz.Project(sc, pid); err != nil { writeError(w, r, err); return }
       cur := r.Header.Get("Last-Event-ID")
       if cur == "" { cur, _ = h.feed.LastAck(r.Context(), sc.SessionID, pid) }
       rc := http.NewResponseController(w)
       w.Header().Set("Content-Type", "text/event-stream")
       w.Header().Set("Cache-Control", "no-cache")
       w.WriteHeader(http.StatusOK)
       wake, cancel := h.feed.Subscribe(pid)
       defer cancel()
       keep := h.clock.NewTicker(15 * time.Second) // Guidance
       defer keep.Stop()
       for {
           evs, next, err := h.feed.After(r.Context(), pid, cur, 256)
           if err != nil { return }
           for _, ev := range evs {
               if !visible(sc, ev) { continue } // S2-E2 recipient rules
               _ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
               if writeSSE(w, ev) != nil { return }
           }
           cur = next
           if rc.Flush() != nil { return }
           if len(evs) == 256 { continue }
           select {
           case <-r.Context().Done(): return
           case <-wake:
           case <-keep.C(): fmt.Fprint(w, ": keep-alive\n\n"); _ = rc.Flush()
           }
       }
   }
   ```
2. `writeSSE`: `id: <cursor>\nevent: <type>\ndata: <json.Marshal(ev)>\n\n` (cursor opaque, `U-DATA-3`).
3. `POST /v1/events/ack` body `{"project_id":"…","cursor":"…"}` (Guidance); subscriber = token's session; monotonic (an older cursor never moves the ACK back).
4. `GET /v1/events?project_id=…&after=…` returns `{"events":[…],"cursor":"…"}` using the same `visible` filter.

**Tests** — ordered delivery; reconnect with `Last-Event-ID` → no gap; no
`Last-Event-ID` → resume after last ACK; client disconnect frees the goroutine
(`goleak`-style check via `runtime.NumGoroutine` delta, no new dependency);
polling and SSE return identical sequences; worker of project A never sees B.
```bash
curl.exe -N -H "Authorization: Bearer $env:T" "http://127.0.0.1:8787/v1/events/stream?project_id=$env:P"
```

**Acceptance**
- [ ] SSE, polling and ACK behave identically w.r.t. order and visibility.
- [ ] No DB transaction held while writing to the socket.
- [ ] Slow client cannot grow daemon memory (pull model).

**Pitfalls** — `json.MarshalIndent` puts newlines in `data:` and breaks
framing. Forgetting `Flush`. A global `WriteTimeout`. Using the fake clock for
socket deadlines (deadlines are real time; logic uses the clock).

### S4-T09 — CLI `events --follow` and `status --watch` over SSE

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-CLI` | `AG-LEAD`, `AG-API` | `S4-E3` | T02, T08 | `feat/cli-events-follow` | no |

**Skills:** `cli-and-config`   **Spec:** §14 (Team, diagnostics), §9.9, §5.3
**Files:** `internal/cli/events.go`, `internal/cli/status.go`, tests

**Why / what** — `cowflow events [--follow] [--since <cursor>]` and
`cowflow status --watch` use SSE in daemon mode and keep polling daemonless (§5.3).

**Steps**
1. Daemon mode: `client.Stream(ctx, project, since, print)`; `--json` prints one JSON object per line (Guidance: NDJSON for a stream).
2. Daemonless: existing S2-E2 cursor polling with fake-clock backoff.
3. ACK after printing each batch (subscriber = the CLI's session/lead identity).
4. `status --watch`: redraw on wake from the stream; plain text, no dashboard (§3.2).

**Tests** — same event fixture printed identically in both modes; Ctrl+C
(context cancel) exits `0`.

**Acceptance**
- [ ] `events --follow` works in both modes with identical output.
- [ ] `--since` honored; ACK sent.

**Pitfalls** — Buffered stdout hides events; flush per line.

### S4-T10 — MCP daemon backend and event waiting

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-MCP` | `AG-LEAD`, `AG-API`, `AG-SEC` | `S4-E3` | T02, T08, `S3-E1` | `feat/mcp-daemon-backend` | yes |

**Skills:** `mcp-and-runners`, `local-api-daemon`, `security-review`   **Spec:** §5.2, §15, §15.1, §9.7
**Files:** `internal/mcp/backend_api.go`, `internal/mcp/wait_events.go`, tests

**Why / what** — S3-E1's `Backend` talks to SQLite. Add an implementation
over the API client so `cowflow mcp` uses the daemon when present (§5.2), and
implement `cowflow_wait_events` / `cowflow_subscribe_events` over SSE.

**Steps**
1. `apiBackend` implements every `Backend` method with `client.Do`; token from `COWFLOW_SESSION_TOKEN` (worker/watchdog) or the lead token.
2. `cowflow_wait_events(filter, after_cursor, timeout_seconds ≤ 300)`: clamp to 300; open stream from `after_cursor`; return on first matching batch or timeout with the new cursor; never block beyond the timeout.
3. Mode selection via `ModeResolver` (**`U-API-2`**, **`U-API-8`**): until decided, selected by test injection only.

**Tests** — table test: every tool gives the same result through both backends
(shared fixture); `wait_events` returns at timeout (fake clock) with cursor;
timeout 900 clamped to 300; watchdog role cannot reach worker-only tools.

**Acceptance**
- [ ] Both backends pass the same tool contract tests.
- [ ] No tool input accepts `workspace_id`.

**Pitfalls** — Leaving the SSE connection open after the tool returns.
Treating a reconnect as "no events".

### S4-T11 — Heartbeat endpoint and liveness sweeper

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-API` | `AG-LEAD`, `AG-COLLAB`, `AG-SCHED`, `AG-SEC` | `S4-E4` | T06, T12, T18 | `feat/api-heartbeat-sweeper` | yes |

**Skills:** `local-api-daemon`, `collaboration-protocol`, `scheduler-and-leases`   **Spec:** §9.1, §9.8, §18, §22 `[collaboration]`
**Files:** `internal/api/h_presence.go` (heartbeat), `internal/daemon/sweeper.go`, tests

**Why / what** — Liveness. A heartbeat renews presence **and** the session's
active leases (§9.8). A daemon component periodically expires stale
presence (→ `session.lost`), due leases (→ `resource.expired`), and runs
escalation.

**Steps**
1. `POST /v1/presence/heartbeat` (scope = own session); body `{"state":"working","reason":""}` (Guidance; payload shape is S2-E3's). Calls `Presence.Heartbeat` then `Leases.RenewSession`.
2. Sweeper component, ticking on the clock every `heartbeat_interval_seconds` (Guidance):
   ```go
   func (s *Sweeper) Run(ctx context.Context) error {
       t := s.clock.NewTicker(s.interval); defer t.Stop()
       for {
           select {
           case <-ctx.Done(): return ctx.Err()
           case <-t.C():
               now := s.clock.Now()
               lost, err := s.presence.ExpireStale(ctx, now)        // session.lost
               s.log(err, "presence"); s.sched.OnSessionsLost(ctx, lost) // retry policy (T14)
               _, err = s.leases.ExpireDue(ctx, now); s.log(err, "leases")
               _, err = s.escalator.EscalateOverdue(ctx, now); s.log(err, "escalation") // C9
           }
       }
   }
   ```
3. A failing step logs and continues; it must not stop the others.

**Tests** — fake clock: no heartbeat for 90 s → exactly one `session.lost`;
heartbeat at 80 s keeps it alive; heartbeat renews a lease's `expires_at`;
another session's token cannot heartbeat for you.

**Acceptance**
- [ ] `session.lost` emitted once per death (idempotent sweep).
- [ ] Lease renewal tied to heartbeat.

**Pitfalls** — Comparing against wall clock. Emitting `session.lost` for a
session that already reported `completed`.

### S4-T12 — Presence expiry and escalation hooks

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-COLLAB` | `AG-LEAD`, `AG-SCHED` | `S4-E4` | `S2-E3`, `S2-E4` | `feat/collab-presence-expiry` | no |

**Skills:** `collaboration-protocol`, `store-and-events`   **Spec:** §9.1, §9.3, §9.4, §18, §24.11
**Files:** `internal/collab/presence.go`, `internal/collab/escalation.go`, tests

**Why / what** — Provide `PresenceService.ExpireStale` and `Escalator` for the
sweeper. Escalation timing rule is `C9`; whether it runs daemonless is `U-SCHED-1`.

**Steps**
1. `ExpireStale(now)`: one tx — select sessions whose last heartbeat < `now - presence_ttl_seconds` and state not terminal; mark lost; emit `session.lost` (+ `agent.presence_changed`); return IDs.
2. `EscalateOverdue(now)`: reads persisted `due_at` (S2-E4). The rule computing `due_at` is **blocked by `C9`**; until decided, the escalator works only with `due_at` already stored and tests seed it directly.

**Tests** — fake clock TTL boundary (89 s alive, 91 s lost); idempotent second
call; escalation with seeded `due_at` escalates once.

**Acceptance**
- [ ] State + event in one transaction; `op_id` per sweep item.
- [ ] No escalation rule chosen for `C9`.

**Pitfalls** — Timers only in memory (lost on restart, §20.8). Use stored timestamps.

### S4-T13 — Dispatcher: limits, persistent queue, per-provider admission

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-SCHED` | `AG-LEAD`, `AG-COLLAB`, `AG-STORE` | `S4-E5` | T17, `S3-E4`, `S3-E7`, `S2-E1` | `feat/scheduler-dispatcher` | no |

**Skills:** `scheduler-and-leases`, `store-and-events`   **Spec:** §5.4 (limits), §11, §15.3, §20.28, §22 `[scheduler]`
**Files:** `internal/scheduler/scheduler.go`, `internal/scheduler/limits.go`, `internal/scheduler/dispatch_test.go`

**Why / what** — `cowflow run --max-agents N` / `POST /v1/tasks/{id}/run`
queue tasks; the dispatcher starts sessions while limits allow: `max_agents`,
the provider's `max_concurrent_requests` (as S3-E7 defined it), disk budget.
A saturated provider must not block others (§15.3).

**Steps**
1. Queue persisted in SQLite (Guidance: task state + `enqueued_at` on `tasks`; §13 lists no queue table — agree with `AG-STORE`). Survives restart (§11).
2. Admission per tick (and on wake from `resource.*`/`session.*` events):
   ```go
   for _, q := range s.queuedByProvider(ctx) { // one queue per provider
       for _, t := range q.Tasks {
           if s.running() >= s.maxAgents { return }
           if s.runningFor(q.Provider) >= s.limitFor(q.Provider) { break } // next provider
           if s.paused(t) || !s.budgetOK(t) || !s.diskOK() { continue }
           s.start(ctx, t) // T14
       }
   }
   ```
3. `Pause`/`Resume`: persisted state + event; paused tasks are skipped; `Resume` re-queues.
4. `workspace_disk_budget_gb`: admission refuses new workspaces over budget (usage from AG-WS); `golden_cache_gb` triggers AG-WS `golden gc` (S1-E8), never deleting unpreserved changes.
5. Defaults for absent keys: **`U-API-11`**; use only values present in config or S0-E2 defaults.

**Tests** — fake runner: `max_agents = 2`, 5 tasks → never > 2 running;
`[scheduler.llm.openai] max_concurrent_requests = 1` while anthropic continues
(§20.28 regression); queue survives store reopen; pause skips, resume re-queues.

**Acceptance**
- [ ] Limits never exceeded under `-race` with 20+ tasks.
- [ ] Per-provider queues; no head-of-line blocking across providers.

**Pitfalls** — Counting "running" from memory instead of SQLite. Starting a
session outside the transaction that marks it started (double start).

### S4-T14 — Session supervisor: spawn, timeouts, cancel, crash, retry

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-SCHED` | `AG-LEAD`, `AG-MCP`, `AG-PLAT`, `AG-SEC` | `S4-E5` | T13, `S0-E5`, `S3-E4` | `feat/scheduler-session-supervisor` | yes |

**Skills:** `scheduler-and-leases`, `windows-platform`, `security-review`   **Spec:** §7 (processes), §11, §18 "Agent crashes", §20.16
**Files:** `internal/scheduler/supervisor.go`, `internal/scheduler/retry.go`, tests

**Why / what** — Each session is a runner process tree. The supervisor
enforces `session_timeout_seconds`, handles `POST /v1/sessions/{id}/cancel`,
and on a crash emits `session.lost`, preserves workspace/logs, and retries
within `retry_limit` using `retry_backoff_seconds`.

**Steps**
1. Start via S3-E4 `Runner.Start` inside the platform tree (Job Object / process group, S0-E5); record pid + process start time (pid reuse) in `agent_sessions`.
2. Timeout: `context.WithTimeout(session_timeout_seconds)` on the fake clock → tree cancel.
3. Cancel: graceful signal → deadline → hard kill. Under a Windows service the graceful `CTRL_BREAK` path is **blocked by `U-PLAT-6`**; the hard kill (`TerminateJobObject`) path is not.
4. Exit handling: crash/non-zero exit without `report_result` → `session.lost`; retry `n` waits `retry_backoff_seconds[min(n, len-1)]`; after `retry_limit` → task failed. Retries count against the same budget (§24.15, via S3-E7 keys by task).
5. Every exit path releases the session's leases (`Leases.Release` for each) so no slot leaks.

**Tests** — fake runner crash → `session.lost`, workspace untouched, retry at
30 s then 120 s (fake clock), then failed; cancel kills a 3-level tree on
Windows and Unix (platform-gated, §20.16); leases released on every path.

**Acceptance**
- [ ] No orphan processes after cancel/timeout (platform tests).
- [ ] Retry/backoff exactly per config; budget shared across retries.

**Pitfalls** — `exec.CommandContext` kills only the parent. Retrying a
`completed` session. Building argv by string join (§17).

### S4-T15 — CLI `run`, `task pause|resume`, `budget`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-CLI` | `AG-LEAD`, `AG-SCHED` | `S4-E5` | T02, T06, T13, T21 | `feat/cli-run-pause-budget` | no |

**Skills:** `cli-and-config`, `scheduler-and-leases`   **Spec:** §11, §14 (Tasks, diagnostics), §24.15
**Files:** `internal/cli/run.go`, `internal/cli/task.go`, `internal/cli/budget.go`, tests

**Why / what** — Human/lead entry points for the scheduler and budget state.

**Steps**
1. `cowflow run --max-agents 20 [--runner claude-code]` → `POST /v1/tasks/{id}/run` per runnable task (or a lead-scoped call agreed with `AG-API`).
2. `cowflow task pause|resume <id>` → `POST /v1/tasks/{id}/pause|resume`.
3. `cowflow budget` (from S3-E7) gains per task/provider: used, limit, percent, state (`ok`/`warn`/`paused`); daemon mode via `/v1/status` fields (route gap `U-API-20`), daemonless reads the store.
4. All daemon-only actions in daemonless mode → exit `2` (T03 rule).

**Tests** — command golden list; `--json` shapes; exit codes against `apifake.Client`.

**Acceptance**
- [ ] `run`, `task pause|resume`, `budget` with `--json`.

**Pitfalls** — `--max-agents` above `[scheduler] max_agents`: cap, don't raise the config limit.

### S4-T16 — Store: resources, leases and queue tables

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-STORE` | `AG-LEAD`, `AG-GIT`, `AG-SCHED` | `S4-E6` | `S0-E3`, `S2-E2` | `feat/store-resource-tables` | no |

**Skills:** `store-and-events`   **Spec:** §9.8 (Table), §13
**Files:** `internal/store/migrations/00NN_resources.sql`, `internal/store/leases.go`, tests

**Why / what** — Persist leases and queue so they survive restart (§9.8).

**Steps**
1. Migration with the spec columns: `resources`, `resource_leases(id, resource, session_id, task_id, acquired_at, expires_at, released_at, state)`, `resource_queue(resource, session_id, priority, enqueued_at)`. Indexes on `(resource, state)` and `(resource, priority, enqueued_at)`.
2. `state` values: **`U-DATA-5`**. Use Go constants (`LeaseActive`, `LeaseReleased`, `LeaseExpired`, `LeaseRevoked` — Guidance names) whose string values are filled after the decision; no `CHECK` constraint until then.
3. Tx helpers: `CountActive`, `InsertLease`, `Enqueue` (returns position), `PopHead(mode)`, `DueLeases(now)`, `LeasesBySession`.

**Tests** — migration from empty; position computation; `PopHead` FIFO vs
priority; reopen keeps rows.

**Acceptance**
- [ ] Exact spec column names; prefixed ULID lease IDs.

**Pitfalls** — `PopHead` outside the releasing transaction.

### S4-T17 — LeaseManager

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-SCHED` | `AG-LEAD`, `AG-COLLAB`, `AG-INT`, `AG-SEC` | `S4-E6` | T16 (fake until merged) | `feat/scheduler-lease-manager` | yes |

**Skills:** `scheduler-and-leases`, `store-and-events`, `security-review`   **Spec:** §9.8, §24.5, §20.30, §22 `[[resources]]`
**Files:** `internal/scheduler/leases.go`, `internal/scheduler/resources.go`, `internal/scheduler/leases_test.go`

**Why / what** — Named semaphores with TTL, FIFO/priority queue, ownership
rules and `resource.*` events. A store-transactional library: the daemon, and
any process allowed by `U-SCHED-1`, call the same code; the transport for CLI
in daemon mode is `U-API-20`.

**Steps**
1. Resource specs from `[[resources]]`; synthesize `compile`/`test`/`integrate:<target>` from `max_builds`/`max_tests`/`max_integrations_per_target` when the resource is absent (§9.8). Both present with different slots → **`U-SCHED-3`**: validation error (Guidance from the skill; reversible). Target-limit key name: **`C5`**.
2. Acquire, one transaction, no barging:
   ```go
   err := m.store.WithTx(ctx, req.OpID, func(tx *store.Tx) error {
       spec, ok := m.specs[req.Resource]
       if !ok { return ErrUnknownResource }
       n, _ := tx.CountActive(req.Resource)
       if n < spec.Slots && tx.QueueLen(req.Resource) == 0 {
           l := m.newLease(req, spec)
           if err := tx.InsertLease(l); err != nil { return err }
           g = Grant{LeaseID: l.ID, Acquired: true}
           return tx.Emit(evResourceAcquired(l, spec.Slots-n-1))
       }
       pos, err := tx.Enqueue(req.Resource, req.Scope.SessionID, m.priorityOf(tx, req.Scope))
       if err != nil { return err }
       g = Grant{Queued: true, Position: pos}
       return tx.Emit(evResourceQueued(req, pos))
   })
   ```
3. Release/expire/revoke: same tx frees the slot, `PopHead`, inserts the head's lease, emits `resource.released|expired|revoked` then `resource.granted`.
4. Ownership: `Release` by a different session → `policy` error; `Revoke` requires lead scope (§9.8).
5. Priority mode: `priority = high` task or a `high` cross-request jumps ahead; order inside a class: **`U-SCHED-5`** (keep `enqueued_at` as a provisional, clearly-marked tie-break).
6. `wait=true`: callers wait on `resource.granted` via `wait_events`/event feed, never polling (§9.8). Timeout while queued: **`U-SCHED-2`** — return `{queued:true}` and leave the queue row untouched until decided.
7. `Announce`: emits `resource.announced` with `eta_seconds`; reserves nothing.

**Tests** — 6 goroutines on `compile.slots = 3` with `-race`: never > 3 active,
grants in FIFO order; priority jump; foreign release refused; revoke by lead
emits `resource.revoked`; alias mapping with legacy config only.

**Acceptance**
- [ ] Grants happen only inside the freeing transaction.
- [ ] All seven `resource.*` events emitted where §9.8 says.
- [ ] No polling loop anywhere in lease waiting.

**Pitfalls** — Granting to a newcomer while the queue is non-empty. Treating
leases as file locks. Deciding `U-SCHED-2` by deleting the queue row.

### S4-T18 — Lease TTL sweeper, heartbeat renewal, dead-session reconcile

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-SCHED` | `AG-LEAD`, `AG-STORE` | `S4-E6` | T17, T23 | `feat/scheduler-lease-expiry` | no |

**Skills:** `scheduler-and-leases`, `testing-harness`   **Spec:** §9.8 Rules, §24.5, §18
**Files:** `internal/scheduler/expiry.go`, `internal/scheduler/reconcile.go`, tests

**Why / what** — TTL expiry returns the slot (§24.5: no "one more minute");
heartbeat renews; on daemon start, leases of dead sessions expire.

**Steps**
1. `ExpireDue(now)`: leases active with `expires_at <= now` → expired + `resource.expired` + grant next (T17 path).
2. `RenewSession(sessionID, now)`: `expires_at = now + lease_ttl_seconds` for active leases of that session.
3. `Reconcilable` "leases" (Order after sessions): sessions not alive → their leases expired; queue rows of dead sessions removed (event: `resource.expired` for leases only; queue removal logged).

**Tests** — fake clock: TTL 600 s, no heartbeat → expired at 600 s and next
queued gets `resource.granted`; renewal at 500 s keeps it; restart with a dead
holder frees the slot (§20.30 last clause).

**Acceptance**
- [ ] Expiry and grant atomic; idempotent on repeat.

**Pitfalls** — Renewing an already-expired lease (expired is final).

### S4-T19 — CLI `resources`, `resource …`, `exec --resource`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-CLI` | `AG-LEAD`, `AG-SCHED`, `AG-SEC` | `S4-E6` | T17, T02 | `feat/cli-resources-exec` | yes |

**Skills:** `cli-and-config`, `scheduler-and-leases`, `security-review`   **Spec:** §9.8 (CLI, Enforcement), §14 (Resources), §11 `command_timeout_seconds`
**Files:** `internal/cli/resources.go`, `internal/cli/exec.go`, tests

**Why / what** — `cowflow resources`, `cowflow resource acquire|release|revoke|announce <name> [--wait] [--ttl 600]`,
and `cowflow exec --resource <name> -- <cmd...>`, which acquires, runs, and
releases even on failure, timeout, or cancel.

**Steps**
1. Transport in daemon mode: **`U-API-20`** (no §12 route). Until decided, call `LeaseManager` in-process against SQLite (WAL, `busy_timeout`) when `U-SCHED-1` allows; tests use `schedfake.Leases`.
2. `exec`:
   ```go
   g, err := leases.Acquire(ctx, AcquireRequest{OpID: op, Scope: sc, Resource: name})
   if g.Queued { g, err = waitGranted(ctx, feed, sc.SessionID, name) } // event-driven
   defer leases.Release(context.WithoutCancel(ctx), op+"-rel", sc, g.LeaseID)
   cctx, cancel := context.WithTimeout(ctx, cfg.Scheduler.CommandTimeout)
   defer cancel()
   code, err := platform.RunTree(cctx, argv) // argv after "--", no shell
   ```
3. Exit code of `exec` = the child's exit code on success path (Guidance); COWFlow errors use §14 codes.
4. `revoke` requires the lead token.

**Tests** — release on success, non-zero exit, timeout, Ctrl+C; argv passed
verbatim (spaces, quotes) with no shell; `resources --json` lists slots/holders/queue/age.

**Acceptance**
- [ ] `exec` never leaks a lease.
- [ ] No `cmd /c` / `sh -c` anywhere.

**Pitfalls** — `defer Release` with the cancelled ctx fails silently; use
`context.WithoutCancel`.

### S4-T20 — MCP resource tools

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-MCP` | `AG-LEAD`, `AG-SCHED`, `AG-SEC` | `S4-E6` | T17, T10 | `feat/mcp-resource-tools` | yes |

**Skills:** `mcp-and-runners`, `scheduler-and-leases`   **Spec:** §9.8 (Cycle), §15, §15.1
**Files:** `internal/mcp/tools_resources.go`, tests

**Why / what** — Wire `cowflow_resource_check`, `cowflow_resource_acquire`,
`cowflow_resource_release`, `cowflow_resource_announce` (worker) and
`cowflow_resource_revoke` (lead) to `LeaseManager`.

**Steps**
1. Results exactly as §9.8: check → `{slots, free, queue, holders}`; acquire → `{lease_id, acquired: true}` or `{queued: true, position}`.
2. `wait=true, timeout`: wait for `resource.granted` through `wait_events` (T10), clamp ≤ 300 s; timeout semantics **`U-SCHED-2`**.
3. Transport in daemon mode: **`U-API-20`** (same as T19).

**Tests** — role table (worker lacks revoke); acquire queued then granted via
an event; foreign release refused.

**Acceptance**
- [ ] Tool names and result fields match §9.8 verbatim.

**Pitfalls** — Polling `check` in a loop inside the tool.

### S4-T21 — LLM budget enforcement

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-SCHED` | `AG-LEAD`, `AG-COLLAB`, `AG-MCP` | `S4-E7` | `S3-E7`, T13, T14 | `feat/scheduler-budget-enforcement` | no |

**Skills:** `scheduler-and-leases`, `mcp-and-runners`   **Spec:** §9.7 (own budget), §11, §15.3, §18 "LLM budget", §20.19, §24.15, §22 `[scheduler.llm*]`, `[watchdog]`
**Files:** `internal/scheduler/budget.go`, `internal/scheduler/budget_test.go`

**Why / what** — Accounting exists (S3-E7). Now: 80% → warning to agent and
lead; 100% → session paused, `budget.exceeded`, workspace preserved; retries
count against the same budget; watchdog uses `[watchdog] budget_tokens_per_hour`.

**Steps**
1. `OnUsage(u)` after each S3-E7 record: compute percent of task `budget_tokens` (inherits from provider when absent, §24.15). Provider hourly/daily thresholds at 80%: **`U-SCHED-4`**; at 100% apply `on_budget_exceeded = "pause"` (§11). Other actions: **`U-SCHED-4`** (config validation accepts only `"pause"`).
2. Crossing 80% once → `BudgetNotifier.Warn` (channel **`U-SCHED-9`**; fake in tests). Never warn twice for the same threshold (persist the threshold crossing).
3. Crossing 100% → one tx: task paused (reason `budget`), emit `budget.exceeded`; then `Scheduler.Pause` → session action **`U-SCHED-10`** (Guidance to confirm: stop the tree gracefully, keep workspace, `Resume` starts a new session in the same workspace).
4. Lead sees `budget.exceeded` via events/`wait_events` (§18 "pause + lead").

**Tests** — fake usage: 79% nothing; 80% one warning; 100% pause +
`budget.exceeded` + workspace dir still present; retry after crash continues
counting; watchdog budget separate from task budgets.

**Acceptance**
- [ ] §20.19 harness test green (T29 drives it end to end).
- [ ] No usage-measurement method chosen for real runners (`U-SCHED-4`).

**Pitfalls** — Pausing in a goroutine after commit (event without state).
Cost float rounding: compare in micro-dollars (Guidance).

### S4-T22 — Outbox dispatcher and event feed

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-STORE` | `AG-LEAD`, `AG-GIT`, `AG-API` | `S4-E8` | `S2-E2` | `feat/store-outbox-dispatcher` | no |

**Skills:** `store-and-events`, `testing-harness`   **Spec:** §9.9, §18 "Daemon restarts", "Lost event", §20.8, §20.15
**Files:** `internal/store/feed.go`, `internal/store/dispatcher.go`, tests

**Why / what** — The daemon must (a) wake SSE streams when events commit,
including events written by **other processes** (daemonless CLIs keep writing
while the daemon runs), and (b) drain `outbox` to internal handlers (scheduler
wake, watchdog feed) and resume after restart.

**Steps**
1. `EventFeed.After/Ack/LastAck` over `events`/`event_deliveries` using S2-E2's cursor (`U-DATA-1`, `U-DATA-3` as decided there).
2. Change detection: Guidance — poll `PRAGMA data_version` every 100–250 ms on a dedicated connection; on change, signal all `Subscribe` channels (non-blocking send, cap 1).
3. Dispatcher: read undispatched `outbox` rows in order → call each `OutboxHandler` → mark dispatched per handler in one tx. Handler error → retry with backoff; never skip.
4. Crash point `outbox.after_write_before_delivery` (harness hook from S0-E8).

**Tests** — writer in a second `*sql.DB` wakes subscribers; crash after write →
restart → handler called exactly once more and dedups by event ID; 20 writers
× 100 events → reader sees each once, in cursor order.

**Acceptance**
- [ ] No event visible to `After` that is not committed; none lost after restart.

**Pitfalls** — Using the ULID as a cursor across processes (can skip events).
Blocking send on a subscriber channel.

### S4-T23 — Reconciler framework and store reconciliation

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-STORE` | `AG-LEAD`, `AG-GIT`, `AG-SCHED`, `AG-WS` | `S4-E8` | T22, `S2-E2` | `feat/store-reconciler` | no |

**Skills:** `store-and-events`, `local-api-daemon`   **Spec:** §2.2 #6 #8, §17, §18, §20.8
**Files:** `internal/store/reconcile.go`, `internal/store/reconcile_ops.go`, tests

**Why / what** — On start the daemon reconciles SQLite × processes ×
worktrees × ProjFS roots (§18). AG-STORE owns the ordered runner and the
store's own phase: pending `op_id` operations and outbox resume.

**Steps**
1. `Reconciler.RunAll`: run registered `Reconcilable`s by `Order()`; collect reports; log each with counts; expose the last report in `/v1/status`.
2. Order (Guidance): 10 store/pending ops → 20 Git refs (AG-GIT hook) → 30 workspaces (T24) → 40 sessions/processes (T14 hook) → 50 leases (T18) → 60 ProjFS roots (S6-E1, no-op now) → 70 outbox resume.
3. Pending ops: rows recorded before a side effect (S2-E2 pattern) are completed or compensated by asking the owning hook what actually exists (Git/filesystem wins).
4. Idempotence: running `RunAll` twice yields an empty second report.

**Tests** — harness: crash at each recorded crash point, restart, assert
convergent state; second run no-op; ref exists without row and row without ref
(per `store-and-events` tests).

**Acceptance**
- [ ] Reconciliation reads real Git/FS through hooks, never trusts SQLite for code.

**Pitfalls** — Holding a tx while hooks call Git. Making reconciliation depend
on the API being up (it runs before serving).

### S4-T24 — Workspace and Git reconcile hooks

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-WS` | `AG-LEAD`, `AG-PLAT`, `AG-GIT` | `S4-E8` | T23, `S1-E6`, `S2-E5` | `feat/workspace-reconcile-hooks` | no |

**Skills:** `cow-workspaces`, `store-and-events`   **Spec:** §6.4, §7 (Handles), §17, §18
**Files:** `internal/workspace/reconcile.go`, tests (Git ref listing via AG-GIT's existing `refs` functions)

**Why / what** — Compare `workspaces` rows with the filesystem: missing dir
→ state per S1 rules; dir without row → report, never delete; destroy that
failed → `pending_cleanup` retry; VHDX not mounted → `pending_mount`.

**Steps**
1. For each row: stat root (confined path); emit `workspace.pending_cleanup|pending_mount` as §18 says.
2. Never remove a directory with unpreserved changes (§20.12); rescue rules from S1-E6.
3. Git phase: AG-GIT exposes `ListChangeRefs` (S2-E5); the hook reports refs without rows and rows without refs to T23.

**Tests** — simulated VHDX unmounted → `pending_mount`, no NTFS fallback;
orphan dir left in place and reported; stuck handle → `pending_cleanup`.

**Acceptance**
- [ ] Idempotent; zero destructive actions without the S1-E6 rules.

**Pitfalls** — `RemoveAll` on an orphan directory.

### S4-T25 — Service install and instance lock

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-PLAT` | `AG-LEAD`, `AG-WS`, `AG-SEC` | `S4-E9` | `S0-E5`, `S0-E6` | `feat/platform-service-install` | yes |

**Skills:** `windows-platform`, `security-review`   **Spec:** §3.1, §6.4, §7 (Service), §14 (`daemon install`)
**Files:** `internal/platform/lock_windows.go`, `lock_unix.go`, `internal/platform/service.go`, `service_windows.go`, `service_linux.go`, `service_darwin.go`, `service_other.go`, tests

**Why / what** — `cowflow daemon install` registers the daemon with the OS
service manager (§3.1). The instance lock keeps two daemons off one `state_dir`.

**Steps**
1. Lock (day 1–2): Windows `LockFileEx` on `<state_dir>/daemon.lock` (Guidance name), Unix `unix.Flock(LOCK_EX|LOCK_NB)`; `ErrLocked` when held.
2. Windows: `mgr.Connect` → `CreateService(name, exe, mgr.Config{StartType: mgr.StartAutomatic}, "daemon", "start")`; `svc/eventlog` for start failures; `RunAsService` uses `svc.IsWindowsService()` + `svc.Run` handling Stop/Shutdown by cancelling ctx. Account: **`U-PLAT-17`**. Name: **`U-API-19`**. VHDX attach before the daemon: **`U-PLAT-5`** (the daemon's own `EnsureStateVolume` in T01 is the fallback inside the process).
3. Linux: write a systemd unit (Guidance: user unit `~/.config/systemd/user/`), `ExecStart=<exe> daemon start`, `Restart=on-failure`; enable via `systemctl --user enable --now` (argv, no shell).
4. macOS: launchd plist (Guidance: `~/Library/LaunchAgents/`), `ProgramArguments` array, `KeepAlive`; `launchctl bootstrap gui/<uid> <plist>`.
5. Do not add `uninstall` (not in §14).

**Tests** — lock contention across two processes; generated unit/plist golden
files; Windows install test gated on admin (`t.Skip` with reason).

**Acceptance**
- [ ] `CGO_ENABLED=0` builds for windows/linux/darwin; `_other` stub returns `ErrUnsupported`.
- [ ] Service path receives no console; stop is graceful via ctx.

**Pitfalls** — `exe` path with spaces in a unit file: quote per systemd rules.
The SCM's 30 s start timeout: report `StartPending` before reconciliation.

### S4-T26 — Watchdog runtime

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-COLLAB` | `AG-LEAD`, `AG-SCHED`, `AG-MCP`, `AG-SEC` | `S4-E10` | T13, T10, T07, `S3-E1` | `feat/collab-watchdog` | yes |

**Skills:** `collaboration-protocol`, `mcp-and-runners`, `security-review`   **Spec:** §5.4, §9.7, §14 (`task create … --kind watchdog --watch …`), §15.1, §22 `[watchdog]`
**Files:** `internal/collab/watchdog.go`, `internal/collab/watchdog_prompt.go`, tests

**Why / what** — A watchdog is a task with `kind = "watchdog"`: no code
workspace, subscribes to events and `/metrics`, focused prompt, can open
cross-requests, comment, escalate, never publishes, own budget (§9.7).

**Steps**
1. `task create guard --kind watchdog --watch integration --runner gemini`: no workspace provisioned; session token with role `watchdog`. `--watch` values: **`U-API-21`**.
2. `[watchdog] enabled = false` → the dispatcher does not start watchdog sessions (T13 hook).
3. Prompt rendering: "you only care about the health of X" with X from `--watch` (§9.7); tools = role `watchdog` (§15.1; extent **`U-API-9`**).
4. Server-side: watchdog token refused on publish/sync/workspace routes regardless of tool set (§9.7 "does not publish").

**Tests** — watchdog task has no workspace row; publish with watchdog token →
`policy`; budget tracked under `[watchdog] budget_tokens_per_hour`.

**Acceptance**
- [ ] Watchdog cannot publish via MCP, CLI or API.

**Pitfalls** — Provisioning a workspace "just in case".

### S4-T27 — Watchdog anomaly scenario (§20.25)

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD`, `AG-COLLAB` | `S4-E10` | T26, T08 | `test/watchdog-anomaly` | no |

**Skills:** `testing-harness`, `collaboration-protocol`   **Spec:** §9.7, §20.25
**Files:** `test/fixtures/scriptedwatchdog/main.go`, `test/integration/watchdog_test.go`

**Why / what** — Prove the loop without an LLM: a deterministic fixture
binary acts as the watchdog runner, reads events/status with its watchdog
token, and opens a cross-request when the injected anomaly appears.

**Steps**
1. Fixture: subscribe to events; on "requests watchdog" anomaly (open requests to one task above a threshold set by the test) call `POST /v1/cross-requests` (kind and `expected_output` per §9.3, e.g. `review`/`answer`).
2. Test: start daemon in-process, create watchdog task with the fixture as a `[runner.<name>]` (generic runner, §15.3), inject 10 open requests, advance fake clock.

**Tests** — `go test -race ./test/integration -run TestAC25WatchdogOpensRequest -v`

**Acceptance**
- [ ] A cross-request authored by the watchdog session exists and its `cross_request.*` event was delivered.

**Pitfalls** — Fixture sleeping on wall clock; drive it by events.

### S4-T28 — Simulated sessions driver and load tests (§20.15, §20.30)

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD`, `AG-API`, `AG-SCHED` | `S4-E11` | T08, T13, T17, T18, T19 | `test/load-20-sessions` | no |

**Skills:** `testing-harness`, `scheduler-and-leases`, `local-api-daemon`   **Spec:** §19 Phase 4, §20.15, §20.30
**Files:** `test/harness/simsession.go`, `test/fixtures/simworker/main.go`, `test/integration/load_test.go`, `test/integration/leases_test.go`

**Why / what** — The Phase 4 load: 20 simulated sessions, each a real
`simworker` process (or goroutine in the fast variant) with its own token,
doing heartbeat → intent → `exec --resource compile` → publish → report,
while N SSE consumers record everything.

**Steps**
1. `simworker`: reads `COWFLOW_SESSION_TOKEN`; flags `--hold 20s` for `exec` duration; emits deterministic `op_id`s.
2. `TestPhase4Load20Simulated`: `max_agents = 20`, `compile.slots = 3`; assert `/v1/status` sampled per event never shows > 20 running or > 3 `compile` holders.
3. `TestAC15NoEventDupOrLoss`: 3 consumers (SSE, polling, SSE with forced reconnects); after dedup by event ID each has exactly the committed set; raw cursor sequence strictly increasing per connection.
4. `TestAC30Leases`: 6 workers, `compile.slots = 3` → 3 acquired, 3 queued (positions 1–3), grants in queue order; kill one holder (no heartbeat) → `resource.expired` at TTL (fake clock) → next gets `resource.granted`.

**Tests**
```bash
go test -race ./test/integration -run 'TestPhase4Load20Simulated|TestAC15|TestAC30' -v -count=1
```

**Acceptance**
- [ ] Green on Windows, Linux, macOS CI.
- [ ] Timeline log written to `t.TempDir()/timeline.ndjson` for the demo evidence.

**Pitfalls** — Shared SQLite across `t.Parallel()` tests. Asserting order
across different resources (only per-resource order is defined).

### S4-T29 — Restart/redelivery and budget pause harness tests (§20.8, §20.19)

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD`, `AG-STORE`, `AG-SCHED` | `S4-E11` | T21, T22, T23 | `test/restart-redelivery-budget` | no |

**Skills:** `testing-harness`, `store-and-events`   **Spec:** §18, §18.1, §20.8, §20.19
**Files:** `test/integration/restart_test.go`, `test/integration/budget_test.go`

**Why / what** — §20.8 and §20.19 are failure behaviors: they are only proven
by forcing the failure (crash point, fake usage) in the hermetic harness, not by
waiting for it to happen in a real run.

**Steps**
1. `TestAC8CrashBetweenOutboxWriteAndDelivery`: enable crash point; publish a change; daemon goroutine aborts after commit; restart on same SQLite; consumer receives the event once after dedup.
2. `TestAC8StateSurvivesRestart`: tasks, queue, leases, presence, open requests identical before/after restart (minus reconciled dead sessions).
3. `TestAC19BudgetPause`: fake usage source to 80% → warning via `BudgetNotifier` fake; to 100% → task paused, `budget.exceeded`, workspace directory present, lead `wait_events` returns the event.

**Tests** — `go test -race ./test/integration -run 'TestAC8|TestAC19' -v -count=1`

**Acceptance**
- [ ] Both §20 criteria green on 3 OSes.

**Pitfalls** — Restarting with a fresh temp dir (that tests nothing).

### S4-T30 — Five real sessions and demo evidence

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD`, `AG-MCP`, `AG-SCHED` | `S4-E11` | T28, T29, `S3-E5` | `test/real-sessions-demo` | no |

**Skills:** `testing-harness`, `mcp-and-runners`   **Spec:** §19 Phase 4, §20.28
**Files:** `test/integration/real_sessions_test.go` (build tag `e2e`, Guidance), `test/fixtures/phase4/config.toml`

**Why / what** — The "+ 5 real" half of the Phase 4 output: 3 Claude Code +
2 Codex sessions on small tasks alongside the 20 simulated ones, under
`[scheduler.llm.anthropic] max_concurrent_requests = 2`. Opt-in (needs CLIs and
accounts), never in default CI.

**Steps**
1. Fixture config: `max_agents = 25`, explicit per-provider values (no reliance on `C11` examples).
2. Run, collect `events` NDJSON + `/v1/status` samples; assert anthropic concurrency ≤ 2 while codex continues.
3. Attach logs/timings to the sprint-closing PR (README §4.4).

**Tests** — `go test -tags e2e ./test/integration -run TestPhase4RealSessions -v -count=1`;
skips with a reason when `claude`/`codex` are missing (`cowflow runners check`).

**Acceptance**
- [ ] Evidence attached; no limit exceeded in the sampled timeline.

**Pitfalls** — Committing tokens or account details in evidence files.

### S4-T31 — Security review of daemon and API

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-SEC` | `AG-LEAD`, `AG-API` | `S4-E2` | T04–T11, T19, T25 | `docs/threat-model-local-api` | yes |

**Skills:** `security-review`, `local-api-daemon`   **Spec:** §12, §15.4, §17, §24.14
**Files:** `docs/api/security.md` (new trust boundary: local HTTP listener)

**Why / what** — The daemon opens the first network listener in COWFlow.
Review it end to end and document the boundary.

**Steps**
1. Run the `security-review` PR checklist over every Sprint 4 PR touching `internal/api`, `internal/daemon`, `internal/platform/service*`, `exec`.
2. Manual probes against a running daemon:
   ```bash
   curl.exe -s http://127.0.0.1:8787/v1/status                     # no token → auth error
   curl.exe -s -H "Host: evil.example" -H "Authorization: Bearer $env:T" http://127.0.0.1:8787/v1/status
   curl.exe -s -H "Authorization: Bearer $env:WORKER_A" http://127.0.0.1:8787/v1/workspaces/<B-workspace>
   ```
3. Grep test: no `sh -c`/`cmd /c`; no token in logs (run load test with `log_level = "debug"` and scan).
4. Document threat model and open items (`U-API-20`, `U-PLAT-17`, `U-API-9`).

**Tests** — the probes in step 2 become table tests in `internal/api` (owned
by `AG-API`, requested through review comments); the log scan runs in T28's
load test with `log_level = "debug"`.

**Acceptance**
- [ ] Findings filed as issues to owners; doc merged; no open high-severity finding at demo.

**Pitfalls** — Reviewing only handlers and missing the SSE and `exec` paths.

## 8. Demo script

Run on a clean machine (Windows 11 with Dev Drive for the primary run; repeat
steps 1–5 on Linux/macOS). PowerShell shown; outputs are illustrative shapes.
Steps that depend on `U-API-2`/`U-API-19` use whatever the kickoff decided.

**0. Setup**
```powershell
cowflow init --devdrive 50GB --conventions gitflow
cowflow doctor --json            # all checks ok
```
`config.toml` additions for the demo:
```toml
[scheduler]
max_agents = 25
[[resources]]
name = "compile"
slots = 3
lease_ttl_seconds = 60
queue = "fifo"
[scheduler.llm.anthropic]
max_concurrent_requests = 2
tokens_per_hour_budget = 50000
[watchdog]
enabled = true

[runner.simworker]                 # demo fixture from S4-T28 (Guidance)
provider     = "custom"
cmd          = ["go", "run", "./test/fixtures/simworker", "--hold", "30s"]
context_file = "AGENTS.md"
prompt_mode  = "stdin"
```

**1. Daemonless still works (§5.3)**
```powershell
go test ./test/integration -run TestPhase2TwoTerminals -v -count=1   # S2-E11, no daemon
```
Expected: `--- PASS` and no daemon process present.

**2. Start daemon, status, SSE**
```powershell
cowflow daemon start              # terminal A
cowflow daemon status --json      # terminal B
cowflow events --follow --json    # terminal C, keep open
```
Expected `status`: `{"mode":"daemon","sessions":{…},"pending_events":0,…}`;
terminal C prints one JSON event per line.

**3. Lease queueing — `compile.slots = 3`, 6 workers (§20.30)**
```powershell
1..6 | % { cowflow task create "w$_" --base develop --title "sim $_" --paths "sim/$_" --runner simworker }
cowflow run --max-agents 6
cowflow resources --json
```
Expected `resources`: `{"name":"compile","slots":3,"free":0,"queue":3,"holders":[3 entries]}`.
Terminal C shows 3× `resource.acquired`, 3× `resource.queued`; then, in order,
`resource.released` → `resource.granted` for w4, w5, w6. Kill one holder's
process tree (`Stop-Process -Id <pid> -Force`): after `lease_ttl_seconds`,
`resource.expired` then `resource.granted` to the next queued session.
Automated twin: `go test -race ./test/integration -run TestAC30Leases -v`.

**4. Daemon kill/restart → redelivery (§20.8)**
```powershell
cowflow change publish --task w1          # while terminal C is stopped (Ctrl+C)
Stop-Process -Id (cowflow daemon status --json | ConvertFrom-Json).pid -Force
cowflow daemon start                      # reconciliation report logged
cowflow events --follow --json            # resumes from last ACK
go test -race ./test/integration -run TestAC8 -v -count=1
```
Expected: `change.published` for w1 printed exactly once after restart; tasks,
queue and leases as before (dead sessions reconciled: `session.lost`); both
`TestAC8…` PASS.

**5. Budget pause (§20.19)**
```powershell
go test -race ./test/integration -run TestAC19BudgetPause -v -count=1
cowflow budget --json
```
Expected: warning at 80%; at 100% `budget.exceeded` in terminal C, task
`paused`, `cowflow workspace inspect <id>` still shows the workspace.

**6. 20 simulated + 5 real under limits (Phase 4 output)**
```powershell
go test -race ./test/integration -run 'TestPhase4Load20Simulated|TestAC15' -v -count=1
go test -tags e2e ./test/integration -run TestPhase4RealSessions -v -count=1   # opt-in
```
Expected: PASS; timeline shows ≤ 25 running, ≤ 3 `compile` holders, ≤ 2
anthropic sessions; no duplicate/missing event IDs per consumer.

**7. Watchdog opens a request on an anomaly (§20.25)**
```powershell
go test -race ./test/integration -run TestAC25WatchdogOpensRequest -v -count=1
cowflow request list --open --json
```
Expected: a request whose origin is the watchdog session, plus its
`cross_request.*` event in terminal C. Optional: same with
`cowflow task create guard --kind watchdog --watch requests --runner gemini` (value per `U-API-21`).

**8. Close** — `AG-QA` attaches NDJSON timelines, `status` samples and test
logs to the closing PR; `CHANGELOG.md` `[Unreleased]` updated.

## 9. Definition of Done

- [ ] All 31 tasks merged to `develop` via reviewed PRs (`AG-LEAD` + pod peer + `AG-SEC` where marked).
- [ ] Every exit criterion in §2 green on Windows, Linux, macOS (real-LLM run attached as evidence).
- [ ] `go build ./...`, `go test -race -cover ./...`, `golangci-lint run`, `go vet ./...`, `govulncheck ./...` clean; `go mod tidy` no-op; no new dependency.
- [ ] Every blocked item in §3.2 either decided (recorded in SCOPE.md §23 and removed from SPEC-GAPS.md in the same PR) or still behind its interface with no silent choice; the gaps found while planning (`U-API-19`…`U-API-21`, `U-SCHED-9`, `U-SCHED-10`, `U-PLAT-17`) are likewise decided or still behind their interfaces.
- [ ] Daemonless S2-E11 scenario still green.
- [ ] `docs/api/` documents routes, error envelope, SSE framing; `docs/api/security.md` merged.
- [ ] `CHANGELOG.md` `[Unreleased]` lists daemon, API, SSE, scheduler, leases, budget, service install, watchdog.
- [ ] Commits Conventional, `git commit -S -s`, no AI attribution; branches `<type>/<slug>` from `develop`.
- [ ] Retro: learnings moved into `local-api-daemon` and `scheduler-and-leases` skills.

## 10. Risks and mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| `U-API-2`/`U-API-19` not decided on day 1 | CLI/MCP cannot switch to daemon mode; demo blocked | `StaticResolver` lets all tests run; escalate to maintainer on day 1; demo steps 2–4 wait, steps 1, 5–7 run via tests |
| Double grant of a lease under concurrency | §20.30 fails, slots oversubscribed | grant only inside the freeing tx; `BEGIN IMMEDIATE`; 6-goroutine `-race` test in T17 |
| Events written by daemonless CLIs not seen by SSE | §20.15 loss | `data_version` change detection (T22); cross-process test |
| SSE goroutine/connection leaks | memory growth with 20+ sessions | pull model, ctx-bound loops, goroutine-count test (T08) |
| SQLite contention with 25 sessions + daemon | `SQLITE_BUSY`, timeouts | short immediate txs, `busy_timeout`, no tx across I/O; load test |
| Unix orphan process groups survive daemon crash (Job Objects do not) | ghost workers after restart | reconcile checks pid + start time and kills/marks lost (T14 hook in T23) |
| `CTRL_BREAK` unavailable under Windows service (`U-PLAT-6`) | non-graceful cancel | hard-kill path always works; graceful path waits for the decision |
| Budget usage from real CLIs unmeasurable (`U-SCHED-4`) | §20.19 only in harness | harness test with fake usage is the criterion; real measurement deferred to decision |
| State volume (Dev Drive) not attached at service start | daemon cannot open store | attach in T01 before store; `U-PLAT-5` for boot ordering; clear failure, never NTFS |
| Real-LLM run flaky or rate-limited | Phase 4 "+5 real" evidence missing | per-provider limits, small tasks, retry the opt-in run; evidence not tied to CI |
| Scope creep (dashboard, extra endpoints) | sprint overrun | §3.2 out of scope; new routes only after gap decisions (`U-API-20`) |
