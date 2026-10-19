# Sprint 7 — Web dashboard (post-v1)

> **Post-v1** · 2 weeks (10 working days) · Exit: `cowflow dashboard` opens a read-only live view of agents, tasks, intents, requests, leases, budgets, and integrations, updated over SSE.

## 1. Goal

v1 lets a human watch a COWFlow run through `cowflow status --watch`, `cowflow events --follow`,
and the `/v1/status` JSON. That works, but with 20 agents, a dozen cross-requests, three
`compile` slots, and an integration group being validated at once, it becomes hard to see
what is happening.

This sprint adds a **read-only web page** served by the daemon on loopback. The human runs
`cowflow dashboard`, a browser tab opens, and they can see, without refreshing:

- who is working (sessions, runner, vendor, presence state) and on which task;
- what each agent intends to touch, and where intents overlap;
- which cross-requests are waiting, for whom, and how close they are to their SLA;
- who holds the shared slots, who is queued, and how much LLM budget is used;
- which integration groups exist and what their gates say;
- a live feed of events, with filters.

The dashboard **only reads**. It cannot promote, accept, pause, or revoke anything. The
maintainer has already fixed the stack (SCOPE §23 "Settled decisions" #13): plain HTML, CSS,
and vanilla JavaScript ES modules, no Node, no npm, no bundler, no framework, embedded in
the Go binary with `go:embed` in `internal/web`, served by the daemon on loopback, live
updates through the browser `EventSource` API on the existing SSE stream. v1 scope does not
change (§3.2).

## 2. Exit criteria

Each criterion must be checked in the PR that closes the sprint, with evidence (test
output, screenshots, or the demo log).

- [ ] **D1 — One command.** With the daemon stopped, `cowflow dashboard` starts it (S4-E1
      on-demand start) and opens an authenticated dashboard tab in ≤ 5 s on the
      reference machine; `--no-open` prints the URL instead.
      *Verified by:* demo §8 steps 2–3; CLI unit test with a fake API (S7-T19).
- [ ] **D2 — All views.** Overview, Intents, Cross-requests, Resources & budgets,
      Integrations, and Events render real data from a running daemon and fixture data
      from the dev server, with empty and error states.
      *Verified by:* manual browser checklist (S7-T22); demo §8 step 5.
- [ ] **D3 — Read-only.** No route reachable with the dashboard cookie changes state.
      Every route in the API route table is tested: a cookie-only request to a
      non-allowlisted route, or with a method other than `GET`/`HEAD`, gets `403`.
      *Verified by:* route-sweep test (S7-T07, S7-T20).
- [ ] **D4 — Browser auth.** The one-time token is single-use, expires (Proposed 60 s),
      never appears in a query string, server log, `Referer`, or browser history entry; the
      session cookie is `HttpOnly`, `SameSite=Strict`, `Path=/`, without `Domain`, and
      expires.
      *Verified by:* `httptest` + fake clock + slog capture tests (S7-T05, S7-T20);
      manual history check (S7-T22).
- [ ] **D5 — Loopback hardening.** A wrong `Host` gets `421`; a cookie-authenticated or
      exchange request from a foreign `Origin` gets `403`; no response carries any
      `Access-Control-*` header; `OPTIONS` gets `405`.
      *Verified by:* table tests (S7-T04, S7-T20).
- [ ] **D6 — Headers.** Every `/ui/` response carries the agreed CSP,
      `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`,
      `X-Frame-Options: DENY`; JSON responses carry `Cache-Control: no-store`.
      *Verified by:* header test over every embedded asset (S7-T06, S7-T20).
- [ ] **D7 — Assets.** All UI files are embedded; no external URL, no inline `<script>`,
      no inline event handler, no `style=` attribute, no `innerHTML`/`eval`; `.js` files are
      served as `text/javascript` on Windows, Linux, and macOS.
      *Verified by:* asset lint tests in CI on the 3 OSes (S7-T06).
- [ ] **D8 — No secrets exposed.** Snapshot and stream responses contain no token or
      token hash, environment variable, absolute filesystem path, file content, prompt,
      gate log output, or comment body.
      *Verified by:* canary redaction test (S7-T03, S7-T20).
- [ ] **D9 — Live and gap-free.** New events reach the page in ≤ 1 s p95 (Proposed
      target); after a dropped connection the stream resumes from `Last-Event-ID` with no
      missing and no duplicated events.
      *Verified by:* SSE resume tests (S7-T16), load test (S7-T21), dev server
      `-drop-every` check (S7-T22).
- [ ] **D10 — Load.** 25 simulated sessions for 10 min with 3 dashboard streams open: no
      event gaps/duplicates, snapshot p95 ≤ 200 ms (Proposed target), daemon goroutines and
      memory back to baseline after the streams close; the browser tab has no long task
      > 100 ms in steady state.
      *Verified by:* `TestDashboardLoad` (S7-T21) + DevTools Performance check (S7-T22).
- [ ] **D11 — Daemon down.** If the daemon stops while the page is open, a status banner
      says so within 5 s and the page retries; if the daemon cannot start,
      `cowflow dashboard` explains that the dashboard is a daemon-only feature (§5.3) and
      exits with code `2`.
      *Verified by:* demo §8 steps 7–9; CLI test (S7-T19).
- [ ] **D12 — Usable.** Keyboard-only navigation works, states are not conveyed by color
      alone, dark/light follows `prefers-color-scheme`, and the page works with no network
      other than loopback (no CDN).
      *Verified by:* manual checklist (S7-T22).
- [ ] **D13 — v1 unchanged.** All v1 tests pass; Bearer clients (CLI, MCP) see no
      behavior change on existing `/v1` routes.
      *Verified by:* CI green on Windows, Linux, macOS; S7-T04/S7-T16 regression tests.
- [ ] **D14 — Documented.** `docs/dashboard.md` (usage, screenshots, security model) and a
      `CHANGELOG.md` `[Unreleased]` entry are merged.
      *Verified by:* S7-T24 review.

## 3. Prerequisites

### 3.1 v1 epics this sprint builds on

v1 must be released before kickoff (Sprints 0–6 merged). The dashboard consumes:

| Epic | What the dashboard needs from it |
|---|---|
| `S0-E1`, `S0-E2` | cobra command tree, `--json`, exit codes; `[daemon] bind` |
| `S0-E8` | CI on 3 platforms (asset tests must run on Windows) |
| `S2-E1`, `S2-E2` | tables for tasks, sessions, intents, requests; event store, cursor, outbox |
| `S2-E3`, `S2-E4` | presence states, intents, `intent.overlap`; cross-request states and SLA escalation |
| `S3-E2` | lead token (used by `cowflow dashboard` to ask for a dashboard token) |
| `S3-E5` | runner name and provider recorded per session |
| `S3-E7`, `S4-E7` | per-task and per-provider budget accounting, 80% / 100% levels |
| `S4-E1` | daemon lifecycle and on-demand start helper |
| `S4-E2` | loopback HTTP API, Bearer auth middleware, `Scope`, error format, API client |
| `S4-E3` | `GET /v1/events/stream`, cursor, keep-alive, `Last-Event-ID` |
| `S4-E5`, `S4-E6` | scheduler queue, resource slots, leases, queue entries |
| `S4-E11` | the 20+ session load driver (reused by S7-T21) |
| `S5-E1`, `S5-E3`, `S5-E4`, `S5-E6`, `S5-E8` | integration groups, gate results, stale, `GET /v1/integration-groups/{id}/review` |
| `S6-E4`, `S6-E7` | §18 harness coverage; system security review baseline |

**Kickoff check (`AG-LEAD`):** confirm each read the views need exists as a Go method
(list tasks/sessions/intents/requests/leases/queue/budgets/groups by project). Anything
missing becomes a cross-request to the owning role (`AG-STORE`, `AG-COLLAB`, `AG-SCHED`,
`AG-INT`) on day 1, not a surprise on day 6.

### 3.2 Browsers to test

Proposed: the two latest stable versions of:

| OS | Browsers |
|---|---|
| Windows 11 (primary) | Microsoft Edge, Firefox |
| macOS | Safari, Chrome |
| Linux | Firefox, Chromium |

All support ES modules, `EventSource`, `SameSite=Strict`, CSP Level 2,
`prefers-color-scheme`, and `structuredClone` without polyfills.

### 3.3 Decisions needed at kickoff

SCOPE.md does not cover these. Each is a **proposal** for the maintainer; nothing is
settled until `AG-LEAD` records the answer (in this document's Contracts section and, if
it changes SCOPE, in §23 "Settled decisions").

1. **Proposed: browser auth flow.** `cowflow dashboard` gets a one-time token from the
   daemon and opens `http://127.0.0.1:<port>/ui/#t=<token>`. The page reads the fragment,
   removes it with `history.replaceState`, and swaps it for a session cookie via
   `POST /v1/dashboard/session`. Why the fragment: browsers never send it to the server,
   so it never reaches logs or `Referer`. Alternative: a one-shot
   `GET /ui/login?code=` that redirects (the token is then in a request line and history).
2. **Proposed: one-time token.** 32 random bytes, base64url, TTL 60 s, single use,
   issued by `POST /v1/dashboard/tokens` to a caller holding the **lead token**; scope
   `dashboard:read` for the lead token's project.
3. **Proposed: session cookie.** Name `cowflow_dash_<port>`, `HttpOnly`, `SameSite=Strict`,
   `Path=/`, no `Domain`, no `Secure` (plain HTTP on loopback), absolute lifetime 8 h,
   no sliding renewal.
4. **Proposed: session storage.** In memory in the daemon; a daemon restart signs every
   browser out (the user runs `cowflow dashboard` again). Alternative: persist sessions in
   SQLite (a new table, needs a §13 change).
5. **Proposed: `dashboard` scope role.** A new `Scope.Role = "dashboard"` that is only
   accepted on an explicit allowlist of `GET` routes; denied everywhere else by default.
6. **Proposed: URL layout.** Assets under `/ui/`; `GET /` redirects to `/ui/`; all data
   stays under `/v1/`.
7. **Proposed: new endpoints.** `POST /v1/dashboard/tokens`, `POST /v1/dashboard/session`,
   `GET /v1/dashboard/session`, `GET /v1/projects/{id}/dashboard`; plus an `after=<cursor>`
   query parameter on `GET /v1/events/stream` (used when no `Last-Event-ID` is sent).
8. **Proposed: loopback guard.** `Host` must be `<bind-ip>:<port>` or `localhost:<port>`,
   else `421 Misdirected Request`; cookie-authenticated non-`GET` requests and the
   exchange endpoint require an allowed `Origin`; `Sec-Fetch-Site`, when present, must be
   `same-origin` or `none` for cookie requests; no CORS; `OPTIONS` → `405`. Applied to the
   whole daemon listener.
9. **Proposed: header set** (exact values in §6.6), including
   `Content-Security-Policy: default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'`.
10. **Proposed: dashboard SSE variant.** For dashboard-scope streams the server omits the
    `event:` line (type stays in `data.type`) so `EventSource.onmessage` receives every
    event, sends `retry: 3000`, and **never ACKs** (no `event_deliveries` row). Bearer
    streams are unchanged.
11. **Proposed: redaction rules.** The read model never contains tokens or hashes, env,
    absolute filesystem paths, file contents or diffs, prompts, gate/command output, or
    cross-request comment bodies (count only).
12. **Proposed: snapshot caps.** Active items uncapped (bounded by `max_agents`); terminal
    cross-requests last 50; terminal integration groups last 20; recent events last 200;
    a `truncated` map says what was cut.
13. **Proposed: refresh strategy.** The client patches state directly only for
    `agent.presence_changed`; every other event appends to the feed and marks state dirty,
    and a snapshot refetch runs at most once every 2 s; rendering at most every 250 ms.
14. **Proposed: limits.** At most 32 live dashboard sessions, 4 streams per session, 16
    dashboard streams in total; beyond that `429`.
15. **Proposed: one project per dashboard.** The project of the directory where
    `cowflow dashboard` runs. Multi-project is future work.
16. **Proposed: no new config keys.** The dashboard uses `[daemon] bind`; no `[dashboard]`
    section in `config.toml`.
17. **Proposed: CLI surface.** `cowflow dashboard [--no-open]`; global `--json` prints
    `{"url": ..., "expires_at": ...}`. Exit codes per §14 (`2` when the daemon is not
    available).
18. **Proposed: server-computed timers and levels.** The read model carries
    `sla_due_at`/`escalated` for cross-requests and `level` (`ok|warn|exceeded|n/a`) for
    budgets; the UI never re-implements SLA or budget rules.
19. **Proposed: UI dev server.** `internal/web/devserver` (a `package main` run with
    `go run`, never imported by `cmd/cowflow`) serves assets from disk, fixture JSON, and a
    fake SSE stream.
20. **Proposed: performance targets.** Event-to-screen ≤ 1 s p95; snapshot ≤ 200 ms p95 with
    25 sessions; no browser long task > 100 ms in steady state.
21. **Proposed: screenshots.** PNG under `docs/images/dashboard/`, ≤ 200 KB each, taken from
    fixture data only (never a real project).

### 3.4 SPEC-GAPS that affect this sprint

Do **not** decide these in Sprint 7 code. If still open at kickoff, the affected task
waits or uses the stated neutral behavior.

| Gap ID | Topic (SPEC-GAPS.md) | Effect on this sprint | Neutral behavior until decided |
|---|---|---|---|
| `U-API-1` | Lead token storage (§5, §15, §22) | `cowflow dashboard` must read the lead token to call `POST /v1/dashboard/tokens` | Use whatever S3-E2 implemented; S7-T19 blocked if S3-E2 left it open |
| `U-API-2` | Daemon detection / on-demand start (§5.3) | `cowflow dashboard` must detect or start the daemon | Reuse the S4-E1 helper as is |
| `U-DATA-3` | Event cursor vs ULID (§9.9) | `Last-Event-ID` and `after` must be the same cursor S2-E2 chose | UI treats the cursor as an opaque string |
| `U-DATA-1` | Columns of `events`/`outbox`/`event_deliveries` (§13) | The non-ACK dashboard consumer must not write delivery rows | No writes from dashboard streams |
| `U-DATA-4` | `cross_request.*` event names; `scope.exceeded` (§9.9, §24.9) | Feed filters and labels | Filter by prefix (`cross_request.`), show raw type |
| `U-DATA-5` | States of integration groups, workspaces, `resource_leases.state` (§6, §9.8, §10) | Badges and grouping | Render unknown states verbatim with a neutral style |
| `C9` | Cross-request escalation (§9.4, §22, §24.11) | SLA timers on the requests board | Server sends `sla_due_at` per the decided rule; `null` → no timer |
| `U-SCHED-4` | 80%/100% for provider budgets; usage measurement (§11, §24.15) | Budget levels | Server sends `level: "n/a"`; UI shows raw usage only |
| `C5` | Per-target integration limit name (§10.3, §11) | Label of the `integrate:<target>` slot | Show the resource name the scheduler reports |
| `U-API-3` | `/metrics` format (§21) | None — the dashboard does **not** read `/metrics` | — |
| `U-API-4` | HTTP status ↔ exit code (§12, §14) | Exit code of `cowflow dashboard` on API errors | Reuse the S4-E2 mapping |
| `U-DATA-2` | Prefixes for prefixed ULIDs (§13) | IDs shown in the UI | Never parse IDs; display as opaque strings |

## 4. Concepts you need

**`go:embed`.** A `//go:embed static` comment above a `var x embed.FS` makes the Go
compiler copy the `static/` directory into the binary. No files are needed at runtime, so
the single-binary rule (§3.1) holds. Files whose names start with `.` or `_` are skipped
unless you write `all:static`. Use `fs.Sub(x, "static")` to strip the directory prefix.
Docs: <https://pkg.go.dev/embed>.

**Serving static files with `net/http`.** `http.FileServerFS` serves an `fs.FS`, but it also
lists directories and redirects `/index.html` to `./`. We want neither, so we build a small
map of `path → bytes` at startup and serve with `http.ServeContent`, which handles `HEAD`,
`Range`, and `If-None-Match` (ETag) for us. Docs:
<https://pkg.go.dev/net/http#ServeContent>.

**SSE and `EventSource`.** Server-Sent Events is a long-lived HTTP response with
`Content-Type: text/event-stream`. The server writes blocks like `id: …`, `event: …`,
`data: …` followed by a blank line and flushes. In the browser,
`new EventSource(url)` opens it, calls `onmessage` for blocks without an `event:` line, and
calls listeners registered with `addEventListener(name)` for named ones. It reconnects by
itself after network errors. Docs:
<https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events/Using_server-sent_events>.

**`Last-Event-ID`.** When `EventSource` reconnects, it sends the last `id:` it saw in the
`Last-Event-ID` request header, so the server can resume right after it. It does this
only for automatic reconnects of the **same** `EventSource` object; if our code creates a
new object, it must pass the cursor itself (we use `?after=`). A non-200 response (e.g.
`401`) stops reconnection for good. Spec:
<https://html.spec.whatwg.org/multipage/server-sent-events.html#the-last-event-id-header>.

**Cookies: `HttpOnly`, `SameSite`.** A cookie set by the server is sent back
automatically on same-site requests, including `EventSource` (which cannot set an
`Authorization` header — this is why we need a cookie at all). `HttpOnly` hides it from
JavaScript, so an XSS bug cannot read it. `SameSite=Strict` means the browser does not
send it on requests started by another site. Cookies are scoped by host, **not by port**.
Docs: <https://developer.mozilla.org/en-US/docs/Web/HTTP/Cookies>.

**CSRF vs read-only.** Cross-Site Request Forgery is another site making your browser
send an authenticated request that *changes* something. A read-only session greatly
reduces the impact, but only if the server really refuses every write when the cookie is
the credential — so we allowlist `GET` routes for the cookie and also check `Origin`.
Docs: <https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html>.

**DNS rebinding.** An attacker's page at `evil.example` changes its DNS record to
`127.0.0.1`; the browser then thinks requests to our daemon are same-origin with the
attacker's page. The request still carries `Host: evil.example:8787`, so rejecting any
`Host` that is not ours blocks it. Docs: <https://en.wikipedia.org/wiki/DNS_rebinding>.

**Content Security Policy (CSP).** A response header that tells the browser which
sources may load scripts, styles, images, and connections. `script-src 'self'` with no
`'unsafe-inline'` means injected `<script>` tags or `onclick="…"` attributes do not run,
even if an attacker gets text into the page. Docs:
<https://developer.mozilla.org/en-US/docs/Web/HTTP/CSP>.

**ES modules without a bundler.** `<script type="module" src="app.js">` loads `app.js`,
which can `import { x } from './lib/dom.js'`. The browser fetches each file itself; no
build step is needed. Modules are deferred and strict by default. The server **must**
send a JavaScript MIME type or the browser refuses to run them. Docs:
<https://developer.mozilla.org/en-US/docs/Web/JavaScript/Guide/Modules>.

**Client-side state reducer.** All UI state lives in one object. Every change is an
*action* (`{type: 'event', ev}`) passed to a pure function
`reduce(state, action) → newState` that never touches the DOM. Rendering reads the state
and redraws. This keeps the logic small and testable in a browser self-test page without
Node. Docs (the same idea as `Array.prototype.reduce`):
<https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Array/reduce>.

**Render throttling with `requestAnimationFrame`.** With 20+ sessions, events can arrive
in bursts of dozens per second. Instead of redrawing on each event, we set a flag and ask
the browser to call us once before the next frame; many events become one render. Browsers
pause rAF in hidden tabs, which saves CPU for free. Docs:
<https://developer.mozilla.org/en-US/docs/Web/API/Window/requestAnimationFrame>.

## 5. Agents and lanes

| Agent | Tasks |
|---|---|
| `AG-LEAD` | kickoff decisions (§3.3), contract freeze, reviews, demo, sprint close |
| `AG-API` | S7-T01, S7-T03, S7-T04, S7-T05, S7-T07, S7-T16 |
| `AG-STORE` | S7-T02 |
| `AG-WEB` | S7-T06, S7-T08, S7-T09, S7-T10, S7-T11, S7-T12, S7-T13, S7-T14, S7-T15, S7-T17, S7-T24 |
| `AG-PLAT` | S7-T18 (requested by `AG-CLI`) |
| `AG-CLI` | S7-T19 |
| `AG-QA` | S7-T20, S7-T21, S7-T22 |
| `AG-SEC` | S7-T23; mandatory reviewer of every `Security: yes` task |

```text
Day        1      2      3      4      5      6      7      8      9      10
AG-LEAD   KICK   ·······················reviews························ DEMO  CLOSE
AG-API    T01    T04    T05────T05    T07    T03────T03    T16────T16   fixes
AG-STORE         T02────T02
AG-WEB           T06    T08    T09────T10    T11    T12    T13/T14 T15  T17────T17 T24
                                                         (T17 starts on devserver day 7)
AG-PLAT          T18
AG-CLI                         T19────T19 (fake API until T05 lands)
AG-QA                   T20 (grows with each merge) ─────────── T21────T21  T22
AG-SEC    review T04/T05/T07 as they open ─────────────────────────── T23
```

**Critical path:** S7-T01 (contract) → S7-T04 (loopback guard) → S7-T05 (token + cookie)
→ S7-T07 (allowlist + mount) → S7-T16 (dashboard SSE) → S7-T17 (live layer on the real
daemon) → S7-T21 (load test) → demo. S7-T02 → S7-T03 runs in parallel and must land
before S7-T17 switches from the dev server to the real daemon (day 8).

`AG-WEB` has the most tasks; the views (S7-T10 … S7-T15) are small and follow one
pattern. If a second person fills `AG-WEB`, split the views between them after S7-T09.

## 6. Contracts

Frozen by `AG-LEAD` at the end of day 1. Changing them later needs both sides to agree in
the PR. Names below that are not in SCOPE.md are **Proposed** (see §3.3).

### 6.1 Read model (owner `AG-API`, file `internal/api/dashboard_types.go`)

The view types are separate from domain types **on purpose**: only fields listed here can
ever reach the browser (allowlist by construction, decision 11).

```go
// DashboardSnapshot is the body of GET /v1/projects/{id}/dashboard.
type DashboardSnapshot struct {
	SchemaVersion int             `json:"schema_version"` // 1
	ProjectID     string          `json:"project_id"`
	GeneratedAt   time.Time       `json:"generated_at"`
	Cursor        string          `json:"cursor"` // last event included; open the stream with after=Cursor
	Status        StatusResponse  `json:"status"` // the S4 /v1/status type, unchanged
	Sessions      []SessionView   `json:"sessions"`
	Tasks         []TaskView      `json:"tasks"`
	Intents       []IntentView    `json:"intents"`
	Overlaps      []OverlapView   `json:"overlaps"`
	CrossRequests []CrossReqView  `json:"cross_requests"`
	Resources     []ResourceView  `json:"resources"`
	Budgets       BudgetsView     `json:"budgets"`
	Integrations  []IntegrationView `json:"integration_groups"`
	RecentEvents  []EventView     `json:"recent_events"` // newest first, cap 200
	Truncated     map[string]bool `json:"truncated,omitempty"`
}

type SessionView struct {
	ID            string     `json:"id"`
	TaskID        string     `json:"task_id,omitempty"` // empty for the lead
	Role          string     `json:"role"`              // lead | worker | watchdog
	Runner        string     `json:"runner"`            // e.g. claude-code, codex
	Provider      string     `json:"provider"`          // e.g. anthropic, openai
	Presence      string     `json:"presence"`          // §9.1 states
	BlockedReason string     `json:"blocked_reason,omitempty"`
	WorkspaceID   string     `json:"workspace_id,omitempty"`
	CowKind       string     `json:"cow_kind,omitempty"`
	StartedAt     time.Time  `json:"started_at"`
	LastHeartbeat *time.Time `json:"last_heartbeat_at,omitempty"`
}

type TaskView struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Kind      string    `json:"kind,omitempty"` // e.g. watchdog
	State     string    `json:"state"`          // opaque string
	Priority  string    `json:"priority,omitempty"`
	Runner    string    `json:"runner"`
	DependsOn []string  `json:"depends_on,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	Budget    *BudgetUse `json:"budget,omitempty"`
}

type IntentView struct {
	ID        string    `json:"id"`
	TaskID    string    `json:"task_id"`
	SessionID string    `json:"session_id"`
	Goal      string    `json:"goal"`  // untrusted text: render with textContent
	Paths     []string  `json:"paths"` // repo-relative scope
	State     string    `json:"state"`
	Stub      bool      `json:"stub"`
	BaseSHA   string    `json:"base_sha"`
	UpdatedAt time.Time `json:"updated_at"`
}

type OverlapView struct {
	PathPrefix string    `json:"path_prefix"`
	IntentIDs  []string  `json:"intent_ids"`
	TaskIDs    []string  `json:"task_ids"`
	DetectedAt time.Time `json:"detected_at"`
}

type CrossReqView struct {
	ID             string     `json:"id"`
	FromTask       string     `json:"from_task"`
	ToTask         string     `json:"to_task"`
	Kind           string     `json:"kind"`            // §9.3 kinds
	ExpectedOutput string     `json:"expected_output"` // commit|interface|test|contract|answer
	Title          string     `json:"title"`           // untrusted text
	Priority       string     `json:"priority"`        // high|normal|low
	State          string     `json:"state"`           // open|accepted|delivered|closed|rejected
	CreatedAt      time.Time  `json:"created_at"`
	ETAMinutes     *int       `json:"eta_minutes,omitempty"`
	SLADueAt       *time.Time `json:"sla_due_at,omitempty"` // server-computed (decision 18, gap C9)
	Escalated      bool       `json:"escalated"`
	CommentCount   int        `json:"comment_count"`
	ChangeID       string     `json:"change_id,omitempty"`
}

type ResourceView struct {
	Name     string           `json:"name"`  // e.g. compile, test, integrate:develop
	Slots    int              `json:"slots"`
	Free     int              `json:"free"`
	Queue    string           `json:"queue"` // fifo|priority
	Holders  []LeaseView      `json:"holders"`
	Waiting  []QueueEntryView `json:"waiting"`
	Announce []AnnounceView   `json:"announced,omitempty"`
}
type LeaseView struct {
	ID         string    `json:"id"`
	SessionID  string    `json:"session_id"`
	TaskID     string    `json:"task_id"`
	AcquiredAt time.Time `json:"acquired_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	State      string    `json:"state"` // opaque (gap U-DATA-5)
}
type QueueEntryView struct {
	SessionID  string    `json:"session_id"`
	TaskID     string    `json:"task_id"`
	Priority   string    `json:"priority"`
	Position   int       `json:"position"`
	EnqueuedAt time.Time `json:"enqueued_at"`
}
type AnnounceView struct {
	TaskID     string `json:"task_id"`
	ETASeconds int    `json:"eta_seconds"`
}

type BudgetsView struct {
	Providers []ProviderBudgetView `json:"providers"`
}
type ProviderBudgetView struct {
	Provider             string   `json:"provider"`
	InFlight             int      `json:"in_flight"`
	MaxConcurrent        int      `json:"max_concurrent_requests"`
	TokensThisHour       int64    `json:"tokens_this_hour"`
	TokensPerHourBudget  int64    `json:"tokens_per_hour_budget,omitempty"`
	CostTodayUSD         float64  `json:"cost_today_usd"`
	CostPerDayBudgetUSD  float64  `json:"cost_per_day_budget_usd,omitempty"`
	Level                string   `json:"level"` // ok|warn|exceeded|n/a
	Paused               bool     `json:"paused"`
}
type BudgetUse struct {
	TokensUsed   int64  `json:"tokens_used"`
	BudgetTokens int64  `json:"budget_tokens,omitempty"` // absent: inherits from provider
	Level        string `json:"level"`                   // ok|warn|exceeded|n/a
}

type IntegrationView struct {
	ID           string     `json:"id"`
	Target       string     `json:"target"`
	State        string     `json:"state"` // opaque (gap U-DATA-5)
	TaskIDs      []string   `json:"task_ids"`
	TargetSHA    string     `json:"target_sha,omitempty"`
	CandidateSHA string     `json:"candidate_sha,omitempty"`
	PromotedSHA  string     `json:"promoted_sha,omitempty"`
	StaleCount   int        `json:"stale_count"`
	Gates        []GateView `json:"gates"`
	UpdatedAt    time.Time  `json:"updated_at"`
}
type GateView struct {
	Name       string `json:"name"`  // build, test, lint, invariant name, ...
	Stage      string `json:"stage"` // diff_only | workspace | post_promotion
	Required   bool   `json:"required"`
	Status     string `json:"status"` // pending|running|passed|failed|skipped
	DurationMS int64  `json:"duration_ms,omitempty"`
}

// EventView is one feed row and the SSE data payload consumed by the UI (§6.3).
type EventView struct {
	ID         string          `json:"id"`     // cursor
	Type       string          `json:"type"`   // §9.9 name
	OccurredAt time.Time       `json:"occurred_at"`
	TaskID     string          `json:"task_id,omitempty"`
	SessionID  string          `json:"session_id,omitempty"`
	SubjectID  string          `json:"subject_id,omitempty"` // intent/request/change/group/lease id
	Payload    json.RawMessage `json:"payload,omitempty"`    // redacted, ≤ 4 KiB
}
```

Example (shortened):

```json
{
  "schema_version": 1,
  "project_id": "prj_01JC9W6K2T",
  "generated_at": "2026-11-02T14:03:11Z",
  "cursor": "000000000000184a",
  "status": { "agents_by_state": { "working": 4, "blocked": 1 } },
  "sessions": [
    { "id": "ses_01JC9", "task_id": "tsk_api", "role": "worker", "runner": "codex",
      "provider": "openai", "presence": "blocked",
      "blocked_reason": "waiting for validateJWT", "workspace_id": "ws_api",
      "cow_kind": "block", "started_at": "2026-11-02T13:40:00Z",
      "last_heartbeat_at": "2026-11-02T14:03:02Z" }
  ],
  "cross_requests": [
    { "id": "req_01JCA", "from_task": "tsk_api", "to_task": "tsk_auth",
      "kind": "expose_interface", "expected_output": "commit",
      "title": "Expose validateJWT", "priority": "high", "state": "open",
      "created_at": "2026-11-02T13:55:00Z", "sla_due_at": "2026-11-02T14:10:00Z",
      "escalated": false, "comment_count": 1 }
  ],
  "resources": [
    { "name": "compile", "slots": 3, "free": 0, "queue": "fifo",
      "holders": [ { "id": "lea_1", "session_id": "ses_01JC9", "task_id": "tsk_api",
        "acquired_at": "2026-11-02T14:01:00Z", "expires_at": "2026-11-02T14:11:00Z",
        "state": "active" } ],
      "waiting": [ { "session_id": "ses_01JCB", "task_id": "tsk_ui",
        "priority": "normal", "position": 1, "enqueued_at": "2026-11-02T14:02:30Z" } ] }
  ],
  "budgets": { "providers": [ { "provider": "anthropic", "in_flight": 3,
    "max_concurrent_requests": 5, "tokens_this_hour": 1640000,
    "tokens_per_hour_budget": 2000000, "cost_today_usd": 41.2,
    "cost_per_day_budget_usd": 80, "level": "warn", "paused": false } ] },
  "recent_events": [],
  "truncated": { "cross_requests": false }
}
```

### 6.2 Endpoints

| Method | Path | Auth | Response | Status |
|---|---|---|---|---|
| `GET` | `/` | none | `302` → `/ui/` | Proposed |
| `GET` | `/ui/` and `/ui/{file}` | none (static, no data) | embedded asset | Proposed |
| `POST` | `/v1/dashboard/tokens` | Bearer, role `lead` | `{"token","expires_at","url"}` | Proposed |
| `POST` | `/v1/dashboard/session` | one-time token in JSON body; allowed `Origin` required | `Set-Cookie` + `{"project_id","expires_at"}` | Proposed |
| `GET` | `/v1/dashboard/session` | cookie | `{"project_id","expires_at"}` or `401` | Proposed |
| `GET` | `/v1/projects/{id}/dashboard` | cookie (same project) or Bearer `lead` | `DashboardSnapshot` | Proposed |
| `GET` | `/v1/status` | Bearer or cookie | S4 status JSON | existing (§12.5), cookie added |
| `GET` | `/v1/events/stream?project_id=…&after=…` | Bearer or cookie | `text/event-stream` | existing (§12.5), cookie + `after` added |
| `GET` | `/v1/integration-groups/{id}/review` | Bearer or cookie | S5-E8 review JSON (no diff lines) | existing (§12.5), cookie added |

Every other route: cookie-only requests → `403 {"code":"forbidden"}`. All JSON responses:
`Cache-Control: no-store`. Error body is the S4-E2 error format.

### 6.3 SSE envelope consumed by the UI

Proposed for dashboard-scope streams (decision 10):

```text
retry: 3000

id: 000000000000184b
data: {"id":"000000000000184b","type":"intent.overlap","occurred_at":"2026-11-02T14:03:12Z","task_id":"tsk_api","subject_id":"int_01JCC","payload":{"path_prefix":"src/auth/"}}

: ping

```

The UI relies only on `id`, `type`, `occurred_at`, and the optional `task_id`,
`session_id`, `subject_id`. It ignores unknown fields and treats unknown types as
"refresh needed". If S4-E3's real envelope differs, `normalize()` in
`internal/web/static/lib/live.js` is the single adapter.

### 6.4 `internal/web` handler interface (owner `AG-WEB`, consumer `AG-API`)

```go
package web

// Options configures the dashboard asset handler.
type Options struct {
	Prefix string       // "/ui/"
	Logger *slog.Logger // never logs query strings or cookies
}

// Handler serves the embedded dashboard assets under opts.Prefix.
// It fails at construction if an embedded file has no known content type.
func Handler(opts Options) (http.Handler, error)

// SetSecurityHeaders writes the dashboard header set (§6.6) on h.
func SetSecurityHeaders(h http.Header)

// CSP is the exact Content-Security-Policy value.
const CSP = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
```

Mounted by `internal/api` (S7-T07): `mux.Handle("GET /ui/", webHandler)` behind the
loopback guard. `internal/web` imports nothing from `internal/api` (no cycle).

### 6.5 Platform helper (owner `AG-PLAT`, consumer `AG-CLI`)

```go
// OpenURL opens u in the user's default browser. u must be http://127.0.0.1:<port>/...
// or http://localhost:<port>/...; anything else returns ErrURLNotAllowed.
// Returns ErrNoBrowser when no opener is available (headless Linux).
func OpenURL(ctx context.Context, u string) error
```

### 6.6 Header set (decision 9)

| Header | Value | Where |
|---|---|---|
| `Content-Security-Policy` | `web.CSP` | every `/ui/` response |
| `X-Content-Type-Options` | `nosniff` | every response |
| `Referrer-Policy` | `no-referrer` | every response |
| `X-Frame-Options` | `DENY` | every `/ui/` response |
| `Cross-Origin-Opener-Policy` | `same-origin` | every `/ui/` response |
| `Cross-Origin-Resource-Policy` | `same-origin` | every response |
| `Cache-Control` | `no-cache` + `ETag` (assets); `no-store` (JSON, SSE) | per type |
| `Access-Control-*` | never sent | — |

### 6.7 Fakes

- **Fixtures** (owner `AG-API`, S7-T01): `internal/api/testdata/dashboard/`:
  `snapshot.json` (5 sessions, every view non-empty), `snapshot-empty.json`,
  `snapshot-busy.json` (25 sessions, queued slots, a failed gate, an overdue request),
  `events.ndjson` (one `EventView` per line, all §9.9 families). A contract test decodes
  each with `DisallowUnknownFields` into the Go types, so fixtures cannot drift.
- **Dev server** (owner `AG-WEB`, S7-T08): `go run ./internal/web/devserver` serves
  `internal/web/static` from disk, the fixtures above, and a fake stream that replays
  `events.ndjson` with increasing ids, honors `Last-Event-ID`/`after`, and can drop
  connections or play "daemon down".
- **Fake API for the CLI** (owner `AG-CLI`, S7-T19): an `httptest.Server` implementing
  `POST /v1/dashboard/tokens`.

## 7. Tasks

Every task: branch from `develop`, one PR, Conventional Commits signed and signed off
(`git commit -S -s`), **no AI attribution** (no `Co-Authored-By`, no "Generated with").
Always load `go-conventions` and `gitflow`.

### S7-T01 — Freeze the dashboard read-model contract and fixtures

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-API` | `AG-LEAD`, `AG-WEB`, `AG-SEC` | `S7-E1` | kickoff decisions | `feat/api-dashboard-contract` | yes (defines exposed data) |

**Skills:** `local-api-daemon`, `security-review`  **Spec:** §9.1–§9.9, §10, §11, §12.5, §13, §21, §24.11, §24.15
**Files:** `internal/api/dashboard_types.go`, `internal/api/dashboard_contract_test.go`, `internal/api/testdata/dashboard/{snapshot,snapshot-empty,snapshot-busy}.json`, `internal/api/testdata/dashboard/events.ndjson`

**Why / what** — UI and API work in parallel from day 2. They need one shared, frozen
description of the JSON. The fixtures are also what the dev server serves, so the UI is
built against exactly what the daemon will return.

**Steps**
1. Copy the types from §6.1 into `dashboard_types.go` with doc comments; reuse the S4
   `StatusResponse` type, do not redefine it.
2. Add `const DashboardSchemaVersion = 1`.
3. Write the three snapshot fixtures by hand; use fake IDs, repo-relative paths, and
   timestamps in 2026; every enum value from §9.1/§9.3 appears at least once in
   `snapshot-busy.json`.
4. Write `events.ndjson` with ≥ 40 lines covering `agent.presence_changed`,
   `intent.created|updated|overlap`, `cross_request.*`, `change.published`, `sync.*`,
   `resource.acquired|released|granted|queued|expired`, `integration.*`,
   `budget.exceeded`, `session.lost`.
5. Contract test:
   ```go
   func TestDashboardFixturesMatchContract(t *testing.T) {
   	for _, name := range []string{"snapshot.json", "snapshot-empty.json", "snapshot-busy.json"} {
   		t.Run(name, func(t *testing.T) {
   			b, err := os.ReadFile(filepath.Join("testdata", "dashboard", name))
   			require.NoError(t, err)
   			dec := json.NewDecoder(bytes.NewReader(b))
   			dec.DisallowUnknownFields()
   			var s DashboardSnapshot
   			require.NoError(t, dec.Decode(&s))
   			assert.Equal(t, DashboardSchemaVersion, s.SchemaVersion)
   		})
   	}
   }
   ```
6. Add a redaction guard: a test that reflects over every field of `DashboardSnapshot`
   (recursively) and fails if a field name matches `(?i)token|secret|env|password|path$`
   except the allowlisted `Paths`/`PathPrefix`.

**Tests** — `go test -race ./internal/api -run Dashboard`.
**Acceptance**
- [ ] Types match §6.1; `AG-WEB` and `AG-SEC` approved the PR.
- [ ] Fixtures decode strictly; reflection redaction guard passes.
**Pitfalls**
- Embedding a domain struct "to save time" leaks every future field to the browser.
- `omitempty` on `bool`/`int` hides `false`/`0`; the UI needs `escalated: false`.
- Absolute workspace paths in fixtures teach the UI to expect them; decision 11 forbids them.

### S7-T02 — Store: consistent read view and head cursor

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-STORE` | `AG-LEAD`, `AG-GIT`, `AG-API` | `S7-E1` | S7-T01 | `feat/store-read-view` | no |

**Skills:** `store-and-events`  **Spec:** §9.9, §13, §18
**Files:** `internal/store/readview.go`, `internal/store/readview_test.go`

**Why / what** — The snapshot and its `cursor` must describe the same instant; otherwise
the client either misses events (cursor too new) or double-applies them (cursor too old,
harmless but noisy). SQLite WAL gives every read transaction a stable snapshot; we expose
that as a read view that never blocks writers.

**Steps**
1. Add `func (s *Store) ReadView(ctx context.Context, fn func(*ReadTx) error) error`.
   `ReadTx` wraps a `*sql.Tx` and offers only `SELECT` helpers.
2. Guidance: if the store DSN uses `_txlock=immediate`, a normal `BeginTx` takes a write
   lock. Open a second pool for reads (`mode=ro`, `_txlock=deferred`, same WAL pragmas) and
   run `ReadView` on it.
3. `func (r *ReadTx) HeadCursor(ctx context.Context, projectID string) (string, error)`
   returns the newest cursor in the S2-E2 cursor order (gap U-DATA-3 — use the order
   S2-E2 chose, do not invent one).
4. Expose any missing list query the snapshot needs (by project): sessions with
   runner/provider, tasks, active intents, overlaps from `path_index`, cross-requests by
   state with cap, leases + queue, integration groups with gate results, recent events.
   Reuse existing queries where they exist.

**Tests** — read view sees a stable snapshot while a concurrent writer commits;
writer is not blocked (measure < 100 ms with `busy_timeout` 5 s); `HeadCursor` equals the
cursor of the last event visible in the same view; `-race`.
**Acceptance**
- [ ] `ReadView` is read-only (a write inside returns an error).
- [ ] A long `ReadView` does not cause `SQLITE_BUSY` for writers.
**Pitfalls**
- Running some queries on `s.db` instead of the tx breaks the snapshot.
- WAL snapshots start at the first `SELECT`, not at `BEGIN`; that is fine as long as all
  reads use the same tx.
- Holding a read tx open for seconds keeps the WAL from checkpointing; keep views short.

### S7-T03 — Snapshot endpoint `GET /v1/projects/{id}/dashboard`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-API` | `AG-LEAD`, `AG-WEB`, `AG-SEC` | `S7-E1` | S7-T01, S7-T02, S7-T07 | `feat/api-dashboard-snapshot` | yes (exposed data) |

**Skills:** `local-api-daemon`, `security-review`, `store-and-events`  **Spec:** §12, §12.5, §17, §21
**Files:** `internal/api/dashboard_snapshot.go`, `internal/api/dashboard_snapshot_test.go`

**Why / what** — One request gives the page everything it needs to draw all views, plus
the cursor to start the stream. v1 has no list endpoints for tasks, sessions, resources,
budgets, or groups, and adding many small endpoints means many round trips and many
consistency problems.

**Steps**
1. Register `GET /v1/projects/{id}/dashboard` in the route table with `dashboard: true`
   (S7-T07). Scope check: dashboard role → `scope.ProjectID == r.PathValue("id")`;
   Bearer → role `lead` only.
2. Build inside one `ReadView`:
   ```go
   func (s *Server) buildSnapshot(ctx context.Context, projectID string) (DashboardSnapshot, error) {
   	var snap DashboardSnapshot
   	err := s.store.ReadView(ctx, func(r *store.ReadTx) error {
   		cur, err := r.HeadCursor(ctx, projectID)
   		if err != nil { return fmt.Errorf("head cursor: %w", err) }
   		snap.Cursor = cur
   		// sessions, tasks, intents, overlaps, requests, leases, groups, events ...
   		return nil
   	})
   	snap.SchemaVersion, snap.ProjectID, snap.GeneratedAt = DashboardSchemaVersion, projectID, s.clock.Now().UTC()
   	return snap, err
   }
   ```
3. Map rows to view types with explicit constructors (`toSessionView(row)`), never
   `json.Marshal(row)`.
4. Compute `SLADueAt`/`Escalated` with the collab package's SLA function (gap C9) and
   budget `Level` with the scheduler's function (gap U-SCHED-4); if the function does not
   exist or the gap is open, set `nil` / `"n/a"`.
5. Apply caps (decision 12) and fill `Truncated`.
6. Event payloads: copy only an allowlist of payload keys per event family (ids, states,
   paths, names, counts); drop the payload if it exceeds 4 KiB.
7. `Cache-Control: no-store`; JSON via the S4 response helper.

**Tests** — golden test against a harness project seeded like `snapshot.json`;
**canary test**: seed a session with env `COWFLOW_SESSION_TOKEN=CANARY-TOK`, a workspace at
an absolute path containing `CANARY-PATH`, a comment body `CANARY-COMMENT`, a gate output
`CANARY-LOG`, then assert the response bytes contain no `CANARY`; cross-project → `403`;
worker Bearer → `403`; caps; 25 sessions p95 ≤ 200 ms (`testing.B`).
**Acceptance**
- [ ] Response decodes into `DashboardSnapshot` with `DisallowUnknownFields`.
- [ ] Canary test passes; `AG-SEC` approved.
- [ ] No new write path; handler does only reads.
**Pitfalls**
- Calling Git (e.g. to resolve SHAs) from this handler; everything comes from SQLite.
- Building the cursor outside the read view (race with new events).
- Returning `null` for empty lists; initialize slices so the UI gets `[]`.

### S7-T04 — Loopback guard and baseline headers

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-API` | `AG-LEAD`, `AG-WEB`, `AG-SEC` | `S7-E2` | kickoff decision 8 | `feat/api-loopback-guard` | yes |

**Skills:** `local-api-daemon`, `security-review`  **Spec:** §12, §17, §22 `[daemon] bind`
**Files:** `internal/api/guard.go`, `internal/api/guard_test.go`, `internal/api/server.go` (wire)

**Why / what** — Once a browser talks to the daemon, any web page the user visits can
try to talk to it too. The Host check blocks DNS rebinding; the Origin check blocks
cross-site requests; no CORS means other origins can never read responses.

**Steps**
1. Derive allowed hosts from the **actual** listener address (works with port `0` in tests):
   ```go
   func allowedHosts(addr net.Addr) map[string]bool {
   	tcp := addr.(*net.TCPAddr)
   	port := strconv.Itoa(tcp.Port)
   	return map[string]bool{
   		net.JoinHostPort(tcp.IP.String(), port): true, // 127.0.0.1:8787 or [::1]:8787
   		"localhost:" + port:                      true,
   	}
   }
   ```
2. Middleware applied to the whole mux:
   ```go
   func (g *guard) wrap(next http.Handler) http.Handler {
   	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
   		h := w.Header()
   		h.Set("X-Content-Type-Options", "nosniff")
   		h.Set("Referrer-Policy", "no-referrer")
   		h.Set("Cross-Origin-Resource-Policy", "same-origin")
   		if !g.hosts[strings.ToLower(r.Host)] {
   			writeError(w, http.StatusMisdirectedRequest, "bad_host", "host not allowed")
   			return
   		}
   		if r.Method == http.MethodOptions {
   			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "no CORS")
   			return
   		}
   		if o := r.Header.Get("Origin"); o != "" && !g.originAllowed(o) {
   			writeError(w, http.StatusForbidden, "bad_origin", "origin not allowed")
   			return
   		}
   		next.ServeHTTP(w, r)
   	})
   }
   ```
3. `originAllowed`: parse with `url.Parse`; require scheme `http`, empty path, host in
   `g.hosts`; reject `null`.
4. Export `requireSameOrigin(r) bool` (Origin present **and** allowed) for S7-T05/S7-T07;
   also reject cookie requests whose `Sec-Fetch-Site` is present and not `same-origin`/`none`.
5. Never write any `Access-Control-*` header anywhere; add a test that greps responses.

**Tests** — table: hosts `127.0.0.1:P` ok, `localhost:P` ok, `LOCALHOST:P` ok,
`evil.example:P` 421, `127.0.0.1:P+1` 421, `localhost.:P` 421, empty 421; origins
`http://127.0.0.1:P` ok, `http://evil.example` 403, `null` 403, `https://127.0.0.1:P` 403;
`OPTIONS` 405; regression: existing S4 Bearer tests unchanged (Go client sends no Origin).
`FuzzHostHeader` seeds the table.
**Acceptance**
- [ ] All `/v1` and `/ui` responses pass through the guard.
- [ ] No `Access-Control-*` header in any test response.
**Pitfalls**
- Using the configured `bind` string instead of the real listener port (breaks with `:0`).
- Allowing a missing `Host` (HTTP/1.0) — reject it.
- Checking `r.URL.Host` (empty for server requests) instead of `r.Host`.

### S7-T05 — One-time dashboard token and read-only session cookie

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-API` | `AG-LEAD`, `AG-CLI`, `AG-SEC` | `S7-E2` | S7-T04, kickoff decisions 1–4, 14 | `feat/api-dashboard-session` | yes |

**Skills:** `local-api-daemon`, `security-review`  **Spec:** §12, §15 (lead token), §17
**Files:** `internal/api/dashauth.go`, `internal/api/dashauth_test.go`, `internal/api/client/dashboard.go` (client method for the CLI)

**Why / what** — `EventSource` cannot send `Authorization`, and putting the lead token in
the browser would give a web page full project power. Instead the CLI trades the lead
token for a short, single-use token, and the browser trades that for a read-only cookie.

**Steps**
1. Secrets: `crypto/rand` 32 bytes → `base64.RawURLEncoding`; store only
   `sha256.Sum256` of the string as map key.
2. `POST /v1/dashboard/tokens`: Bearer required, `scope.Role == "lead"` (else `403`); body
   `{}` ≤ 1 KiB; store `oneTime{projectID, expiresAt: now+60s}`; respond
   `{"token": t, "expires_at": …, "url": "http://<listener>/ui/#t=" + t}`.
3. `POST /v1/dashboard/session`: `requireSameOrigin(r)` else `403`; `Content-Type` must be
   `application/json` (a cross-site form cannot send it without a preflight); body
   `{"token": "..."}` ≤ 1 KiB, `DisallowUnknownFields`. Under the mutex: look up the hash,
   check `expiresAt`, **delete it** (single use), create a session:
   ```go
   http.SetCookie(w, &http.Cookie{
   	Name:     "cowflow_dash_" + port,
   	Value:    sid,
   	Path:     "/",
   	HttpOnly: true,
   	SameSite: http.SameSiteStrictMode,
   	MaxAge:   int(sessionTTL / time.Second), // 8 h, Proposed
   	// Secure: false on purpose — plain HTTP on loopback; a Secure cookie may be dropped.
   })
   ```
   Any failure → `401 {"code":"unauthorized"}` (same body for unknown/expired/used).
4. `GET /v1/dashboard/session`: cookie → `{project_id, expires_at}`, else `401`.
5. Session store: in memory, `sync.Mutex`, injected clock, lazy expiry on lookup plus a
   sweep every minute owned by the daemon's errgroup; cap 32 sessions (evict oldest).
6. Cookie authenticator for S7-T07: `func (a *dashAuth) FromCookie(r *http.Request) (Scope, bool)`
   returning `Scope{ProjectID: s.projectID, Role: "dashboard"}`.
7. Client: `func (c *Client) CreateDashboardToken(ctx context.Context) (DashboardToken, error)`.
8. Logging: log `dashboard.token_issued` / `dashboard.session_created` with project id
   only; never the token, cookie, or request body.

**Tests** — fake clock: token valid at 59 s, `401` at 61 s; second use `401`; worker
token `403`; exchange without Origin `403`, foreign Origin `403`, `text/plain` `415`;
cookie attributes parsed from `Set-Cookie`; session expires at 8 h; slog captured into a
buffer and asserted free of the token and session id; `FuzzDashboardSessionBody`.
**Acceptance**
- [ ] Single-use and TTL proven with the fake clock.
- [ ] Token/cookie never appear in logs or error bodies.
- [ ] `AG-SEC` approved.
**Pitfalls**
- `__Host-` cookie prefix requires `Secure`; do not use it on HTTP.
- Comparing secrets with `==` on raw strings; hash first (constant-time compare if you
  ever compare raw values).
- Cookies are not port-scoped: include the port in the name so two daemons do not clash.
- Returning different errors for "expired" and "unknown" helps an attacker probe.

### S7-T06 — `internal/web`: embedded assets, serving, and security headers

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-WEB` | `AG-LEAD`, `AG-API`, `AG-SEC` | `S7-E3` | §6.4 contract | `feat/web-embedded-assets` | yes (headers) |

**Skills:** `local-api-daemon`, `security-review`  **Spec:** §3.1 (single binary), §17, §23 #13
**Files:** `internal/web/doc.go`, `internal/web/web.go`, `internal/web/web_test.go`, `internal/web/assets_test.go`, `internal/web/static/index.html` (placeholder), `internal/web/static/favicon.svg`

**Why / what** — The UI ships inside the binary. Serving must be boring and strict:
known files only, exact content types, the full header set.

**Steps**
1. Embed and index at construction:
   ```go
   //go:embed static
   var staticFS embed.FS

   var contentTypes = map[string]string{
   	".html": "text/html; charset=utf-8",
   	".js":   "text/javascript; charset=utf-8",
   	".css":  "text/css; charset=utf-8",
   	".svg":  "image/svg+xml",
   }

   type asset struct{ body []byte; ctype, etag string }

   func Handler(opts Options) (http.Handler, error) {
   	sub, err := fs.Sub(staticFS, "static")
   	if err != nil { return nil, fmt.Errorf("web: sub fs: %w", err) }
   	assets := map[string]asset{}
   	err = fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
   		if err != nil || d.IsDir() { return err }
   		ct, ok := contentTypes[path.Ext(p)]
   		if !ok { return fmt.Errorf("web: no content type for %s", p) }
   		b, err := fs.ReadFile(sub, p)
   		if err != nil { return err }
   		sum := sha256.Sum256(b)
   		assets[p] = asset{b, ct, `"` + hex.EncodeToString(sum[:8]) + `"`}
   		return nil
   	})
   	if err != nil { return nil, err }
   	strip := strings.TrimSuffix(opts.Prefix, "/")
   	return http.StripPrefix(strip, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
   		SetSecurityHeaders(w.Header())
   		name := strings.TrimPrefix(r.URL.Path, "/")
   		if name == "" { name = "index.html" }
   		a, ok := assets[name]
   		if !ok { http.NotFound(w, r); return }
   		w.Header().Set("Content-Type", a.ctype)
   		w.Header().Set("Cache-Control", "no-cache")
   		w.Header().Set("ETag", a.etag)
   		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(a.body))
   	})), nil
   }
   ```
2. `SetSecurityHeaders` writes the §6.6 `/ui/` set, including `CSP`.
3. `assets_test.go` (the "linter without Node"):
   - every `<script>` tag in HTML has `type="module"` and `src=`; no `on[a-z]+=`; no `style=`;
   - no `http://` or `https://` in any asset except the SVG `xmlns`;
   - JS bans: `innerHTML`, `outerHTML`, `insertAdjacentHTML`, `document.write`, `eval(`,
     `new Function`, `setTimeout("`, `localStorage` (state stays in memory);
   - every relative `src`/`href` in HTML and every `import … from './x.js'` in JS resolves to
     an embedded file (regex `from\s+['"](\.{1,2}/[^'"]+)['"]` + `path.Join`);
   - total embedded size ≤ 300 KiB (Guidance: keeps the UI small).
4. `web_test.go`: every asset → `200`, exact `Content-Type`, all headers, `ETag`;
   `If-None-Match` → `304`; `/ui/nope.js` → `404`; `/ui/../go.mod` → `404`; `HEAD` works.

**Tests** — `go test -race ./internal/web/...` on all three OSes in CI.
**Acceptance**
- [ ] No `mime.TypeByExtension` use; unknown extension fails `Handler`.
- [ ] Asset lint passes; CI green on Windows.
**Pitfalls**
- On Windows, `mime.TypeByExtension(".js")` can return `text/plain` from the registry;
  with `nosniff` the browser then refuses every module. Use the fixed map.
- `http.FileServer` lists directories and redirects `index.html`; we do not use it.
- Files named `_x.js` or `.x.js` are silently not embedded without `all:`; do not use such names.

### S7-T07 — Mount `/ui/`, root redirect, cookie-auth route allowlist

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-API` | `AG-LEAD`, `AG-WEB`, `AG-SEC` | `S7-E2` | S7-T04, S7-T05, S7-T06 | `feat/api-dashboard-routes` | yes |

**Skills:** `local-api-daemon`, `security-review`  **Spec:** §12, §17
**Files:** `internal/api/routes.go`, `internal/api/authz.go`, `internal/api/routes_test.go`

**Why / what** — The read-only promise must be enforced by the server, not by the UI
"not having buttons". A single route table marks which routes accept the dashboard
cookie; everything else refuses it.

**Steps**
1. Route table:
   ```go
   type route struct {
   	pattern   string // "GET /v1/status"
   	handler   http.HandlerFunc
   	dashboard bool   // may be called with the dashboard cookie
   }
   func (s *Server) register(mux *http.ServeMux, rt route) {
   	mux.Handle(rt.pattern, s.authn(rt, rt.handler))
   }
   ```
2. `authn(rt, h)`: if `Authorization` is present → S4 Bearer path (unchanged). Else if
   `rt.dashboard` and method is `GET`/`HEAD` → `dashAuth.FromCookie`; else `401`. A cookie
   on a non-dashboard route → `403 forbidden`.
3. Mark `dashboard: true` only for the six routes in §6.2. Project match: the `id` path
   value or `project_id` query must equal the session project.
4. Mount `mux.Handle("GET /ui/", web)` and `mux.HandleFunc("GET /{$}", redirectToUI)`.
5. Expose `func (s *Server) Routes() []route` for tests.

**Tests** — route sweep:
```go
for _, rt := range srv.Routes() {
	method, path := splitPattern(rt.pattern) // fill {id} with a fixture id
	req := httptest.NewRequest(method, path, nil)
	req.Host = hostOK
	req.AddCookie(validDashCookie)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if !rt.dashboard || method != http.MethodGet {
		assert.Equal(t, http.StatusForbidden, rec.Code, rt.pattern)
	}
}
```
Plus: cookie + other project → `403`; Bearer behavior unchanged on every route.
**Acceptance**
- [ ] Every non-`GET` route refuses the cookie (D3).
- [ ] Adding a route without deciding `dashboard` is impossible (field is explicit, zero = no).
**Pitfalls**
- Authorizing in a middleware that wraps the whole mux: `r.Pattern` is empty there. Wrap
  per route as above.
- Accepting both Bearer and cookie on one request; Bearer wins, cookie ignored.

### S7-T08 — Dev server and fixtures for UI development

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-WEB` | `AG-LEAD`, `AG-API` | `S7-E4` | S7-T01 | `feat/web-devserver` | no |

**Skills:** `local-api-daemon`, `testing-harness`  **Spec:** §12.5, §9.9
**Files:** `internal/web/devserver/main.go`, `internal/web/devtest/selftest.html`, `internal/web/devtest/selftest.js`

**Why / what** — `AG-WEB` must build views from day 3, before the real endpoints exist.
The dev server also lets us test reconnects and "daemon down" on demand.

**Steps**
1. Flags: `-addr 127.0.0.1:8790`, `-static internal/web/static`,
   `-fixtures internal/api/testdata/dashboard`, `-snapshot snapshot.json`,
   `-rate 5` (events/s), `-drop-every 0s`, `-down` (all `/v1` return connection close).
   Refuse a non-loopback `-addr`.
2. Serve static from disk (`http.FileServerFS(os.DirFS(dir))` is fine here) with
   `web.SetSecurityHeaders` and the same content-type map, so CSP bugs show up in dev.
3. `POST/GET /v1/dashboard/session` always succeed; `GET /v1/projects/{id}/dashboard`
   returns the fixture with `cursor` = id of the last replayed event.
4. Stream: read `events.ndjson`; assign ids `fmt.Sprintf("%016x", n)`; start after
   `Last-Event-ID` or `after`; write unnamed events (§6.3); `: ping` every 15 s; close the
   response every `-drop-every`.
5. Serve `devtest/` at `/devtest/` (not embedded in the binary) with a self-test page that
   imports `../ui/lib/state.js` and asserts reducer behavior (S7-T17).

**Tests** — `go vet ./internal/web/...`; manual: `go run ./internal/web/devserver` and
open `http://127.0.0.1:8790/ui/`.
**Acceptance**
- [ ] `cmd/cowflow` does not import `internal/web/devserver` (`go list -deps` check in a test).
- [ ] Drop and down modes work.
**Pitfalls**
- Binding `:8790` (all interfaces) instead of `127.0.0.1:8790`.
- Drifting from the contract: always serve the S7-T01 fixtures, never private copies.

### S7-T09 — App shell: HTML, styles, DOM helpers, auth bootstrap, router

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-WEB` | `AG-LEAD`, `AG-API`, `AG-SEC` | `S7-E4` | S7-T06, S7-T08 | `feat/web-app-shell` | yes (token handling, DOM) |

**Skills:** `security-review`  **Spec:** §5.3, §17
**Files:** `internal/web/static/index.html`, `styles.css`, `app.js`, `lib/dom.js`, `lib/auth.js`, `lib/api.js`, `lib/state.js`, `lib/format.js`

**Why / what** — Every view shares the layout, the safe DOM builder, the state store,
and the sign-in. Getting these right once makes the views simple and safe.

**Steps**
1. `index.html`:
   ```html
   <!doctype html>
   <html lang="en">
   <head>
     <meta charset="utf-8">
     <meta name="viewport" content="width=device-width, initial-scale=1">
     <meta name="color-scheme" content="light dark">
     <meta name="referrer" content="no-referrer">
     <title>COWFlow dashboard</title>
     <link rel="icon" href="favicon.svg" type="image/svg+xml">
     <link rel="stylesheet" href="styles.css">
     <script type="module" src="app.js"></script>
   </head>
   <body>
     <header class="topbar">
       <h1>COWFlow</h1>
       <span id="project" class="muted"></span>
       <p id="conn" class="conn" role="status" aria-live="polite">Connecting…</p>
     </header>
     <nav aria-label="Views"><ul>
       <li><a href="#/overview">Overview</a></li>
       <li><a href="#/intents">Intents</a></li>
       <li><a href="#/requests">Cross-requests</a></li>
       <li><a href="#/resources">Resources &amp; budgets</a></li>
       <li><a href="#/integrations">Integrations</a></li>
       <li><a href="#/events">Events</a></li>
     </ul></nav>
     <main id="main" tabindex="-1"></main>
     <noscript>The dashboard needs JavaScript.</noscript>
   </body>
   </html>
   ```
2. `lib/dom.js` — the only way to create elements:
   ```js
   const ATTRS = new Set(['class', 'id', 'href', 'title', 'role', 'scope', 'colspan',
     'datetime', 'tabindex', 'aria-label', 'aria-current', 'aria-sort', 'aria-pressed',
     'data-due', 'data-filter', 'type', 'value', 'checked', 'for', 'name']);
   export function h(tag, attrs = {}, ...children) {
     const el = document.createElement(tag);
     for (const [k, v] of Object.entries(attrs)) {
       if (v == null || v === false) continue;
       if (!ATTRS.has(k)) throw new Error(`attribute not allowed: ${k}`);
       if (k === 'href' && !String(v).startsWith('#')) throw new Error('only #hrefs');
       el.setAttribute(k, v === true ? '' : String(v));
     }
     for (const c of children.flat()) {
       if (c == null || c === false) continue;
       el.append(c instanceof Node ? c : document.createTextNode(String(c)));
     }
     return el;
   }
   ```
3. `lib/auth.js`:
   ```js
   export async function bootstrap() {
     const m = /^#t=([A-Za-z0-9_-]{40,64})$/.exec(location.hash);
     if (m) {
       history.replaceState(null, '', location.pathname + '#/overview'); // drop the token first
       const r = await fetch('/v1/dashboard/session', {
         method: 'POST', cache: 'no-store',
         headers: { 'Content-Type': 'application/json' },
         body: JSON.stringify({ token: m[1] }),
       });
       if (!r.ok) return { state: 'signed_out' };
     }
     const who = await fetch('/v1/dashboard/session', { cache: 'no-store' });
     return who.ok ? { state: 'signed_in', ...(await who.json()) } : { state: 'signed_out' };
   }
   ```
   Signed out → main shows: "Not signed in. Run `cowflow dashboard` in the project to open
   a new session." (no form, no token input).
4. `lib/state.js`: `createStore(reducer, initial)` with `getState`, `dispatch`,
   `subscribe`; `reduce` is pure (S7-T17 fills event handling).
5. `app.js`: bootstrap → `api.snapshot(projectId)` → `dispatch({type:'snapshot'})` →
   router. Router listens to `hashchange`, accepts only `#/<view>`, ignores other hashes,
   sets `aria-current="page"` on the active link, moves focus to `#main` on navigation,
   calls `views[name].render(state, main)` inside `scheduleRender()`.
6. `styles.css`: custom properties for colors on `:root`, overridden in
   `@media (prefers-color-scheme: dark)`; state badges use text + color; visible
   `:focus-visible` outline; `@media (prefers-reduced-motion: reduce)` disables transitions;
   system font stack (no web fonts).
7. `lib/format.js`: `Intl.DateTimeFormat`, `Intl.RelativeTimeFormat`, durations, percent.

**Tests** — asset lint (S7-T06) passes; manual: sign-in via dev server; token gone
from the address bar and from the Back-button history.
**Acceptance**
- [ ] No string-to-HTML anywhere; all text via `h()`/text nodes.
- [ ] Token removed from the URL before any `await`.
**Pitfalls**
- A "Skip to content" link with `href="#main"` would be read by a naive router as a route.
- `fetch` to a relative URL from `/ui/` resolves under `/ui/`; always use absolute `/v1/...`.
- Setting `el.setAttribute('style', …)` is blocked by CSP; use classes (or `el.style.x`).

### S7-T10 — Overview view (sessions, agents, tasks)

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-WEB` | `AG-LEAD`, `AG-API` | `S7-E4` | S7-T09 | `feat/web-view-overview` | no |

**Skills:** —  **Spec:** §9.1, §15.3, §21
**Files:** `internal/web/static/views/overview.js`, `styles.css`

**Why / what** — The first screen answers "who is doing what right now".

**Steps**
1. Export `{ id: 'overview', title: 'Overview', render(state, root) }`.
2. Counters row from `snap.status` (agents by state, tasks by state, open/overdue
   requests): `<dl>` of label/value pairs.
3. Sessions table: Task, Role, Runner, Vendor (provider), Presence (badge),
   Blocked reason, Workspace (`cow_kind`), Last heartbeat (relative), Started. Sort:
   `blocked` first, then `working`, then the rest; stable by task id.
4. Tasks table: Title, State, Priority, Runner, Depends on, Budget (bar with
   `level`), Updated.
5. Empty state: "No sessions yet. Start work with `cowflow run`."
6. A presence older than `presence_ttl_seconds` is not inferred client-side; show what the
   server says.

**Tests** — manual with `snapshot.json`, `snapshot-empty.json`, `snapshot-busy.json`.
**Acceptance**
- [ ] Tables use `<caption>`, `<th scope="col">`; badges have text, not color only.
- [ ] Renders 25 sessions without visible jank.
**Pitfalls**
- Hard-coding the §9.1 list and dropping unknown states; render unknown verbatim.

### S7-T11 — Intents and overlaps view

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-WEB` | `AG-LEAD`, `AG-API` | `S7-E4` | S7-T09 | `feat/web-view-intents` | no |

**Skills:** —  **Spec:** §9.2, §8.3, §24.9
**Files:** `internal/web/static/views/intents.js`, `styles.css`

**Why / what** — Overlaps are the early warning of semantic conflicts (§9.2). They do not
block anything, so a human has to notice them.

**Steps**
1. Overlaps panel first (if any): path prefix + the tasks involved + detected time.
2. Intents table: Task, Goal, Paths (first 3 + "+N more" toggle with `aria-expanded` on a
   `<button>`), Stub (badge), Base SHA (first 10 chars, full in `title`), Updated.
3. Path filter: `<input type="search">` filters intents whose `paths` start with the text
   (client-side, no server call). Highlight rows that are part of an overlap.

**Tests** — manual with `snapshot-busy.json` (contains overlaps and a stub).
**Acceptance**
- [ ] Goal text with `<script>` in the fixture is displayed as text.
- [ ] Filter is keyboard accessible and labeled.
**Pitfalls**
- Treating overlap as an error color; it is a warning.

### S7-T12 — Cross-requests board with SLA timers

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-WEB` | `AG-LEAD`, `AG-API` | `S7-E4` | S7-T09 | `feat/web-view-requests` | no |

**Skills:** —  **Spec:** §9.3, §9.4, §24.11
**Files:** `internal/web/static/views/requests.js`, `lib/timers.js`, `styles.css`

**Why / what** — Requests block agents. The board shows which ones wait, on whom, and how
close they are to escalation.

**Steps**
1. Columns by state: `open`, `accepted`, `delivered`, then a collapsed "Closed / rejected"
   section (with the `truncated` note when applicable).
2. Card: title, kind, expected output, from → to task, priority badge, ETA (if
   accepted), comment count, change id (if delivered), escalated badge.
3. SLA timer: `<time data-due="…">` filled by `lib/timers.js`:
   ```js
   export function startTimers(root) {
     const tick = () => {
       if (document.hidden) return;
       const now = Date.now();
       for (const el of root.querySelectorAll('[data-due]')) {
         const left = Date.parse(el.dataset.due) - now;
         el.textContent = left >= 0 ? `due in ${fmtDuration(left)}` : `overdue ${fmtDuration(-left)}`;
         el.classList.toggle('overdue', left < 0);
       }
     };
     tick();
     return setInterval(tick, 1000);
   }
   ```
   No `sla_due_at` → no timer (low priority, or gap C9 open).
4. Below 768 px, columns stack vertically.

**Tests** — manual with an overdue request in `snapshot-busy.json`.
**Acceptance**
- [ ] Timer updates text only, not a full re-render.
- [ ] Interval cleared when leaving the view.
**Pitfalls**
- Computing SLA from priority in JS (re-implements a rule with an open gap).

### S7-T13 — Resources, leases, and budgets view

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-WEB` | `AG-LEAD`, `AG-API` | `S7-E4` | S7-T09 | `feat/web-view-resources` | no |

**Skills:** —  **Spec:** §9.8, §11, §24.15
**Files:** `internal/web/static/views/resources.js`, `styles.css`

**Why / what** — Slots and budgets explain why agents wait; this view makes the queues
and spending visible.

**Steps**
1. One card per resource: `free/slots` meter (`<meter min=0 max=slots value=used>` with a
   text label), queue mode, holders table (task, session, acquired, expires in), waiting
   list in position order, announcements ("api plans to compile in ~2 min").
2. Lease about to expire (< 60 s) gets a text marker "expiring".
3. Budgets table per provider: in flight / max concurrent, tokens this hour / budget,
   cost today / budget, level badge (`ok`, `warn` = 80%, `exceeded` = 100%, `n/a`), paused.
4. When `level` is `n/a`, show raw numbers and no warning styling.

**Tests** — manual with `snapshot-busy.json` (full `compile`, a queue of 3, a provider at `warn`).
**Acceptance**
- [ ] Percentages shown as text next to every bar/meter.
**Pitfalls**
- Dividing by a zero/absent budget → show "no budget", not `Infinity%`.

### S7-T14 — Integration groups view with gate results

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-WEB` | `AG-LEAD`, `AG-API`, `AG-SEC` | `S7-E4` | S7-T09 | `feat/web-view-integrations` | yes (loads review data) |

**Skills:** `security-review`  **Spec:** §10, §10.1, §10.2, §24.12
**Files:** `internal/web/static/views/integrations.js`, `styles.css`

**Why / what** — Shows each group's progress through diff-only gates, workspace gates,
stale revalidation, and promotion — the "review by intent" story of §9.4 at a glance.

**Steps**
1. List groups: target, state, tasks, stale count, updated; active first.
2. Gate strip per group, ordered by stage (`diff_only` → `workspace` → `post_promotion`):
   name, required/optional, status text + icon, duration.
3. "Details" button loads `GET /v1/integration-groups/{id}/review` on demand and shows per
   task the intent, manifest stat (files, +/−), touched contracts, invariant results. Never
   requests diff lines.
4. SHAs shortened to 10 chars.

**Tests** — manual with fixtures; a failed required gate is obvious without color.
**Acceptance**
- [ ] Review content rendered only via `h()`; no diff lines fetched.
**Pitfalls**
- Polling `/review` for every group on every event; fetch only on click.

### S7-T15 — Live event feed with filters

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-WEB` | `AG-LEAD`, `AG-API` | `S7-E4` | S7-T09 | `feat/web-view-events` | no |

**Skills:** —  **Spec:** §9.9
**Files:** `internal/web/static/views/events.js`, `styles.css`

**Why / what** — The raw truth, newest first, for when a view does not explain something.

**Steps**
1. Rows: time, type, task, session, subject id; newest on top; cap 500 rows (ring buffer
   in state).
2. Filters: family checkboxes (`agent.`, `intent.`, `cross_request.`, `change.`, `sync.`,
   `resource.`, `integration.`, `contract.`, `invariant.`, `session.`, `workspace.`,
   `budget.`, other), task filter, "pause" toggle (`aria-pressed`) that freezes the list
   while events keep being stored.
3. Incremental render: prepend only new rows since the last render (track the newest id
   rendered); full rebuild only when filters change.
4. No `aria-live` on the feed (too chatty for screen readers).

**Tests** — manual with dev server `-rate 50`.
**Acceptance**
- [ ] 50 events/s for 1 min with no long task > 100 ms.
**Pitfalls**
- Rebuilding 500 rows per event.

### S7-T16 — SSE stream for dashboard sessions

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-API` | `AG-LEAD`, `AG-WEB`, `AG-SEC` | `S7-E5` | S7-T07, kickoff decisions 10, 14 | `feat/api-dashboard-sse` | yes |

**Skills:** `local-api-daemon`, `store-and-events`, `security-review`  **Spec:** §9.9, §12.5, §18 ("Lost event")
**Files:** `internal/api/sse.go`, `internal/api/sse_dashboard_test.go`

**Why / what** — Reuse the S4-E3 stream; add what a browser needs without changing
behavior for Bearer clients.

**Steps**
1. Resume cursor: `Last-Event-ID` header first, else `after` query, else head. Validate
   (no CR/LF, ≤ 128 bytes).
2. For dashboard scope: write unnamed events (no `event:` line), `retry: 3000` first,
   payload filtered as in S7-T03 step 6, **no ACK and no `event_deliveries` writes**.
   ```go
   func writeEvent(w io.Writer, ev EventView, named bool) error {
   	data, err := json.Marshal(ev) // never contains raw newlines
   	if err != nil { return err }
   	if _, err := fmt.Fprintf(w, "id: %s\n", ev.ID); err != nil { return err }
   	if named {
   		if _, err := fmt.Fprintf(w, "event: %s\n", ev.Type); err != nil { return err }
   	}
   	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
   	return err
   }
   ```
3. Caps: per-session and total counters; over the cap → `429` before streaming.
4. Session expiry: on each write or keep-alive, re-check the session; expired → return
   (the browser reconnects and gets `401`).
5. Shutdown: streams watch a daemon context cancelled from `srv.RegisterOnShutdown`, so
   `Shutdown` does not wait forever for open streams.
6. Per-write deadline via `http.NewResponseController(w).SetWriteDeadline` (no global
   `WriteTimeout`).

**Tests** — resume with `Last-Event-ID` and with `after`, header wins over query;
disconnect mid-stream and reconnect: union of received ids = all ids, no duplicates;
dashboard stream writes no `event_deliveries` row; Bearer stream output byte-identical to
before (golden); `429` at the cap; expired session ends the stream; goroutine count back to
baseline after 100 connect/disconnect cycles (`runtime.NumGoroutine` with retry).
**Acceptance**
- [ ] D9 resume test passes; D13 Bearer golden unchanged.
**Pitfalls**
- Holding a DB transaction while waiting for new events.
- Forgetting to flush after each event (the browser sees nothing).
- Browsers allow ~6 HTTP/1.1 connections per host across tabs; each tab holds one stream.

### S7-T17 — Client live layer: EventSource, reducer, throttling, connection states

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-WEB` | `AG-LEAD`, `AG-API` | `S7-E5` | S7-T09, S7-T08 (dev), S7-T16 + S7-T03 (real) | `feat/web-live-updates` | no |

**Skills:** `local-api-daemon`  **Spec:** §5.3, §9.9, §18
**Files:** `internal/web/static/lib/live.js`, `lib/state.js`, `lib/render.js`, `internal/web/devtest/selftest.js`

**Why / what** — Turns a static page into a live one that survives drops, restarts, and
bursts from 20+ sessions.

**Steps**
1. Connection:
   ```js
   export function connect(store, projectId) {
     let es, lastId = store.getState().cursor, backoff = 1000;
     const seen = new RecentIds(2000); // bounded dedup
     const open = () => {
       const q = new URLSearchParams({ project_id: projectId, after: lastId });
       es = new EventSource(`/v1/events/stream?${q}`);
       es.onopen = () => { backoff = 1000; store.dispatch({ type: 'conn', conn: 'live' }); };
       es.onmessage = (m) => {
         const ev = normalize(JSON.parse(m.data), m.lastEventId);
         if (seen.has(ev.id)) return;
         seen.add(ev.id); lastId = ev.id;
         store.dispatch({ type: 'event', ev });
       };
       es.onerror = async () => {
         if (es.readyState === EventSource.CONNECTING) {
           store.dispatch({ type: 'conn', conn: 'reconnecting' }); return; // browser retries with Last-Event-ID
         }
         es.close();
         const probe = await fetch('/v1/dashboard/session', { cache: 'no-store' }).catch(() => null);
         if (probe && probe.status === 401) { store.dispatch({ type: 'conn', conn: 'signed_out' }); return; }
         store.dispatch({ type: 'conn', conn: probe ? 'reconnecting' : 'daemon_down' });
         setTimeout(open, backoff); backoff = Math.min(backoff * 2, 30000);
       };
     };
     open();
   }
   ```
2. Reducer (pure): `snapshot` replaces entity sections and keeps the feed; `event` appends
   to the feed (cap 500), patches `sessions[].presence` for `agent.presence_changed`, and
   sets `dirty = true` for every other type; `conn` sets the banner state.
3. Refetch: when `dirty`, schedule `api.snapshot()` no sooner than 2 s after the previous
   one; never more than one in flight; skip while `document.hidden`, run once on
   `visibilitychange` to visible.
4. Render throttling in `lib/render.js`:
   ```js
   let queued = false, last = 0;
   export function scheduleRender(fn) {
     if (queued) return;
     queued = true;
     const run = () => requestAnimationFrame(() => { queued = false; last = performance.now(); fn(); });
     const wait = 250 - (performance.now() - last);
     wait > 0 ? setTimeout(run, wait) : run();
   }
   ```
5. Banner texts (in `#conn`, `role="status"`): Live · Reconnecting… · "Daemon not running.
   The dashboard is a daemon-only feature; start it with `cowflow daemon start`." ·
   "Session ended. Run `cowflow dashboard` to sign in again."
6. Self-test page (`/devtest/` on the dev server): reducer cases — snapshot then events,
   duplicate id ignored, unknown type marks dirty, feed cap, presence patch.

**Tests** — self-test page green in the 3 browser engines; dev server `-drop-every 20s`
for 5 min: no duplicate or missing rows (compare against `events.ndjson` order); `-down`
shows the daemon banner ≤ 5 s.
**Acceptance**
- [ ] Reconnect after a drop resumes from the last id (D9).
- [ ] One render per ≥ 250 ms under 50 events/s.
**Pitfalls**
- `new EventSource` on manual reopen does not send `Last-Event-ID`; pass `after`.
- `EventSource` gives no HTTP status; probe the session endpoint to tell `401` from down.
- Using `addEventListener(type)` when the server sends unnamed events (nothing arrives).

### S7-T18 — `platform.OpenURL`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-PLAT` | `AG-LEAD`, `AG-WS`, `AG-CLI`, `AG-SEC` | `S7-E6` | §6.5 contract | `feat/platform-open-url` | yes (process spawn) |

**Skills:** `windows-platform`, `security-review`  **Spec:** §7, §17
**Files:** `internal/platform/openurl.go`, `openurl_windows.go`, `openurl_linux.go`, `openurl_darwin.go`, `openurl_test.go`

**Why / what** — Opening a browser is OS-specific and spawns a process, so it belongs in
the platform layer (ownership rule 1). `AG-CLI` requests it; `AG-PLAT` owns it.

**Steps**
1. Validate first: `url.Parse`; scheme `http`; hostname `127.0.0.1`, `::1`, or
   `localhost`; else `ErrURLNotAllowed`.
2. argv only, never a shell string:
   - Windows: `rundll32.exe url.dll,FileProtocolHandler <url>` (not `cmd /c start`, which
     would interpret `&` and `^`);
   - macOS: `open <url>`;
   - Linux: `xdg-open <url>`; if not found in `PATH` or `DISPLAY`/`WAYLAND_DISPLAY` unset →
     `ErrNoBrowser`.
3. `exec.CommandContext(ctx, name, args...)`; `Start()` and reap in a goroutine; do not
   wait for the browser to exit.
4. A test seam: `var startCmd = func(c *exec.Cmd) error { return c.Start() }`.

**Tests** — table for allowed/refused URLs; argv captured via the seam per OS (build
tags); no `cmd`/`sh` in argv.
**Acceptance**
- [ ] Non-loopback URL refused; no shell anywhere.
**Pitfalls**
- The URL (with the one-time token) is visible in the process list for a moment; this is
  why the token is single-use with a 60 s TTL.

### S7-T19 — CLI `cowflow dashboard`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-CLI` | `AG-LEAD`, `AG-API`, `AG-SEC` | `S7-E6` | S7-T05 (client), S7-T18; gaps U-API-1, U-API-2 | `feat/cli-dashboard` | yes (handles lead token) |

**Skills:** `cli-and-config`, `security-review`  **Spec:** §5.3, §14, §15
**Files:** `internal/cli/dashboard.go`, `internal/cli/dashboard_test.go`, `docs/cli.md` (if it exists)

**Why / what** — One command a human remembers. It hides daemon start, token exchange, and
browser launch.

**Steps**
1. Command:
   ```go
   func newDashboardCmd(d *Deps) *cobra.Command {
   	var noOpen bool
   	cmd := &cobra.Command{
   		Use:   "dashboard",
   		Short: "Open the read-only web dashboard for this project",
   		Args:  cobra.NoArgs,
   		RunE: func(cmd *cobra.Command, _ []string) error {
   			ctx := cmd.Context()
   			client, err := d.EnsureDaemon(ctx) // S4-E1 helper
   			if err != nil {
   				return stateError("the dashboard needs the daemon (daemon-only feature): %v; "+
   					"start it with `cowflow daemon start`", err) // exit code 2
   			}
   			tok, err := client.CreateDashboardToken(ctx)
   			if err != nil { return fmt.Errorf("create dashboard token: %w", err) }
   			out := cmd.OutOrStdout()
   			if d.JSON { return writeJSON(out, map[string]any{"url": tok.URL, "expires_at": tok.ExpiresAt}) }
   			if noOpen || d.OpenURL(ctx, tok.URL) != nil {
   				fmt.Fprintf(out, "Open this URL in your browser (single use, expires %s):\n%s\n",
   					tok.ExpiresAt.Local().Format(time.Kitchen), tok.URL)
   				return nil
   			}
   			fmt.Fprintln(out, "Dashboard opened in your browser.")
   			return nil
   		},
   	}
   	cmd.Flags().BoolVar(&noOpen, "no-open", false, "print the URL instead of opening a browser")
   	return cmd
   }
   ```
2. The lead token is read the way S3-E2 stores it; never printed, never logged.
3. Register the command in the root tree; update help text.

**Tests** — fake API (`httptest`): happy path calls `OpenURL` with the URL; `--no-open`
prints it; `--json` shape; `OpenURL` error → URL printed, exit 0; `EnsureDaemon` error →
exit code `2` and message mentions `cowflow daemon start`; lead token missing → state error.
**Acceptance**
- [ ] D1 and the CLI half of D11 covered.
- [ ] The lead token never reaches stdout, stderr, or logs.
**Pitfalls**
- Starting a daemonless fallback "to be helpful": §5.3 — daemon-only features are not faked.

### S7-T20 — Handler, auth, header, and redaction test suite

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD`, `AG-SEC`, `AG-API` | `S7-E7` | grows with S7-T03…T07, S7-T16 | `test/dashboard-security-suite` | yes |

**Skills:** `testing-harness`, `security-review`  **Spec:** §12, §17, §18.1
**Files:** `test/integration/dashboard_security_test.go`, `test/integration/dashboard_fuzz_test.go`

**Why / what** — Unit tests live with each package; this suite runs the **real daemon
wiring** end to end so a refactor cannot silently drop a check.

**Steps**
1. Start the daemon in-process with the harness (fake clock, temp state dir,
   `bind = 127.0.0.1:0`); get a lead token from the harness.
2. Flow test: issue token → exchange with correct Origin → cookie → snapshot → stream.
3. Matrix tests reusing S7-T04/T05/T07 cases through the real listener (bad Host, bad
   Origin, `OPTIONS`, reuse, expiry, worker token, route sweep).
4. Headers: fetch every embedded asset path (list from `web` test helper) and assert §6.6.
5. Redaction: canary seeding from S7-T03 plus a canary in an event payload; scan
   snapshot, stream (first 100 events), and the captured slog output for `CANARY` and the
   raw token/session id.
6. Fuzz: `FuzzDashboardSessionBody`, `FuzzHostHeader`, `FuzzLastEventID` with seeds in
   `testdata/fuzz/`.

**Tests** — `go test -race ./test/integration -run Dashboard`; fuzz seeds in CI.
**Acceptance**
- [ ] D3–D8 covered by at least one test each, named `// D<n>` in a comment.
**Pitfalls**
- Using `httptest.NewRequest` only: it bypasses the listener and the Host guard setup;
  include real-socket tests too.

### S7-T21 — Load: 20+ sessions with the dashboard open

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD`, `AG-SEC`, `AG-API` | `S7-E7` | S7-T16, S7-T03; S4-E11 driver | `test/dashboard-load` | no |

**Skills:** `testing-harness`, `local-api-daemon`  **Spec:** §20.15, §21
**Files:** `test/integration/dashboard_load_test.go`, `test/harness/cmd/simsessions/main.go` (only if S4-E11 has no runnable entry point)

**Why / what** — The dashboard must not slow the system it observes, and must keep up.

**Steps**
1. Reuse the S4-E11 driver: 25 simulated sessions doing intent → acquire `compile` →
   publish → request → sync, for 10 min (fake clock where possible; real time for latency).
2. Open 3 dashboard SSE clients (cookie) + 1 Bearer client. Each records `(id, recv time)`.
3. Every 2 s per client, fetch the snapshot (simulates the refetch policy).
4. Assert: each dashboard client received exactly the Bearer client's id sequence (no gap,
   no dup); event-to-receive p95 ≤ 1 s; snapshot p95 ≤ 200 ms; after closing clients,
   goroutines and heap return within 10% of baseline; zero `event_deliveries` rows for
   dashboard clients.
5. Build tag `load` so the default `go test ./...` stays fast; CI runs it nightly
   (Guidance).
6. If S4-E11 has no runnable command, add `simsessions` (`-project`, `-sessions`,
   `-duration`) as a thin wrapper, used by the demo.

**Tests** — `go test -race -tags load -run TestDashboardLoad -timeout 20m ./test/integration`.
**Acceptance**
- [ ] D10 numbers recorded in the PR.
**Pitfalls**
- Measuring latency with the fake clock (always zero).

### S7-T22 — Manual browser checklist and accessibility pass

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD`, `AG-WEB`, `AG-SEC` | `S7-E7` | S7-T10…T17, S7-T19 | `test/dashboard-manual-checklist` | yes (verifies token/history) |

**Skills:** `testing-harness`  **Spec:** §17
**Files:** `test/manual/dashboard-checklist.md`

**Why / what** — Without Node there is no headless JS test runner; a short, repeatable
human checklist covers what Go tests cannot see.

**Steps** — write the checklist and run it on each browser in §3.2; record results
(browser, version, OS, pass/fail, notes) in the PR:
1. Sign-in: the address bar never shows `#t=` after load; Back does not return to it;
   reloading keeps the session; DevTools → Application → Cookies shows `HttpOnly`,
   `SameSite=Strict`.
2. Console: zero errors and zero CSP violations on every view.
3. Network: only requests to `127.0.0.1:<port>`; works with Wi-Fi/Ethernet disabled.
4. Each view with `snapshot.json`, `snapshot-empty.json`, `snapshot-busy.json` on the dev server.
5. Live: dev server `-drop-every 20s` for 5 min → no duplicate/missing feed rows;
   `-down` → daemon banner ≤ 5 s; back up → live again.
6. Keyboard only: Tab reaches every control in order, visible focus, Enter activates,
   view change moves focus to main.
7. Screen reader smoke test (Narrator / VoiceOver / Orca): view names, table headers,
   connection status announced.
8. Dark and light (`prefers-color-scheme` via OS or DevTools emulation); 200% zoom; width
   375 px.
9. Performance: DevTools Performance 60 s with dev server `-rate 50` → no long task > 100 ms.
10. Self-test page `/devtest/` all green.

**Acceptance**
- [ ] Checklist passes on all §3.2 browsers; failures filed as issues and fixed or
      accepted by `AG-LEAD`.
**Pitfalls**
- Testing only in the browser you develop with.

### S7-T23 — Security review of the dashboard

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-SEC` | `AG-LEAD`, `AG-API`, `AG-WEB` | `S7-E7` | S7-T03…T07, S7-T16, S7-T19 | `docs/dashboard-threat-model` | yes |

**Skills:** `security-review`  **Spec:** §12, §17, §24.14
**Files:** threat-model section text for `docs/dashboard.md` (delivered to `AG-WEB` for S7-T24); findings as issues

**Why / what** — A browser is a new client type with new attackers: any web page the
user visits, and any text an agent writes.

**Steps** — review the merged code and record findings for each threat:
1. Malicious website: CSRF (cookie allowlist, Origin, `SameSite=Strict`), DNS rebinding
   (Host), cross-origin reads (no CORS, CORP), clickjacking (`frame-ancestors`).
2. Malicious agent text (intent goals, request titles, task titles, branch names): stored
   XSS → `h()` only, CSP without inline, asset lint bans.
3. Token leakage: argv (S7-T18), history (`replaceState`), `Referer`
   (`no-referrer`), logs (S7-T05/T20).
4. Other local processes: cookies are shared across ports of `127.0.0.1`; a server on
   another loopback port that the user's browser visits receives the cookie. Assess: the
   session is read-only, project-scoped, and ≤ 8 h; document the residual risk.
5. Data exposure: snapshot/stream fields vs decision 11.
6. Denial of service: stream and session caps, snapshot cost.

**Acceptance**
- [ ] Every `Security: yes` PR in this sprint has an `AG-SEC` approval.
- [ ] No open high/critical finding at sprint close.
**Pitfalls**
- Reviewing only the UI or only the API; the controls are split across both.

### S7-T24 — `docs/dashboard.md`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-WEB` | `AG-LEAD`, `AG-CLI`, `AG-SEC` | `S7-E8` | S7-T19, S7-T22, S7-T23 | `docs/dashboard` | no |

**Skills:** —  **Spec:** §3.2, §23 #13
**Files:** `docs/dashboard.md`, `docs/images/dashboard/*.png`, `CHANGELOG.md`, `README.md` (docs table row)

**Why / what** — Users need to know how to open it, what each view means, and what it
does and does not protect.

**Steps**
1. Sections: What it is (read-only, daemon-only, one project) · Open it
   (`cowflow dashboard`, `--no-open`, `--json`) · Views (one paragraph + screenshot each) ·
   Live updates and reconnects · Security model (from S7-T23) · Troubleshooting (daemon
   down, session ended, port in use, headless Linux) · Browser support · Limitations.
2. Screenshots from the dev server with fixtures (decision 21).
3. `CHANGELOG.md` `[Unreleased]` → `### Added`: "Read-only web dashboard (`cowflow dashboard`)".

**Acceptance**
- [ ] Every command in the doc was run while writing it.
**Pitfalls**
- Screenshots of a real repository leak names and paths.

## 8. Demo script

Run on a clean Windows 11 machine (primary), then spot-check on macOS/Linux. Commands in
PowerShell. The demo project is a repository already set up with `cowflow init` (S3-E3).

```powershell
# 1. Build and check the starting state
go build -o "$env:TEMP\cf\cowflow.exe" ./cmd/cowflow
$env:PATH = "$env:TEMP\cf;$env:PATH"
cd C:\demo\shop
cowflow daemon stop          # ignore "not running"
cowflow daemon status        # expect: not running

# 2. One command starts the daemon and opens the browser (D1)
cowflow dashboard
#   expect: "Dashboard opened in your browser." in ≤ 5 s;
#   the tab shows /ui/#/overview (no #t= in the address bar), banner "Live".

# 3. URL-only and JSON modes
cowflow dashboard --no-open  # prints http://127.0.0.1:8787/ui/#t=...
cowflow dashboard --json     # {"url": "...", "expires_at": "..."}

# 4. Generate load: 25 simulated sessions for 15 minutes (S4-E11 driver / S7-T21)
go run ./test/harness/cmd/simsessions -project C:\demo\shop -sessions 25 -duration 15m
```

5. **Walk through the views** while the load runs:
   - *Overview:* 25 sessions with runner and vendor; some `blocked` with a reason at the
     top; task states and budget bars.
   - *Intents:* overlap panel lists shared path prefixes; filter by `src/auth` narrows rows.
   - *Cross-requests:* cards move `open → accepted → delivered`; `high` cards show a
     countdown; an overdue one shows "overdue" and the escalated badge.
   - *Resources & budgets:* `compile` at `0/3` free with a FIFO queue; positions advance as
     leases are released; a provider reaches `warn`.
   - *Integrations:* create a group from another terminal
     (`cowflow integration create auth api --target develop`, then
     `cowflow integration validate <group-id>`); the gate strip goes diff-only → workspace;
     "Details" shows the review without diff lines.
   - *Events:* rows stream in; untick `resource.` and the rows disappear; "pause" freezes.

```powershell
# 6. Read-only proof (D3): the cookie cannot write
#    In the browser DevTools console on the dashboard tab:
#    await fetch('/v1/tasks/x/pause', {method:'POST'}).then(r => r.status)   // expect 403

# 7. Daemon down while open (D11)
cowflow daemon stop
#   expect within 5 s: banner "Daemon not running. The dashboard is a daemon-only feature; …"
#   the last data stays visible; retries back off up to 30 s.

# 8. Restart
cowflow daemon start
#   expect (Proposed decision 4, in-memory sessions): banner "Session ended. Run
#   `cowflow dashboard` to sign in again."  (With SQLite sessions it would resume live.)
cowflow dashboard            # new tab, live again, snapshot is current

# 9. Daemon cannot start → clear message, exit code 2
$l = [System.Net.Sockets.TcpListener]::new([ipaddress]'127.0.0.1', 8787); $l.Start()
cowflow daemon stop
cowflow dashboard; $LASTEXITCODE   # expect message mentioning `cowflow daemon start`, then 2
$l.Stop()

# 10. Gap-free resume (D9), automated evidence
go test -race ./internal/api -run TestDashboardStreamResume -v
go test -race -tags load -run TestDashboardLoad -timeout 20m ./test/integration
```

Record in the closing PR: step timings, screenshots of each view under load, the console
showing zero CSP errors, and the S7-T21 numbers.

## 9. Definition of Done

- [ ] D1–D14 checked with evidence in the closing PR.
- [ ] Every task's acceptance boxes ticked; every `Security: yes` PR approved by `AG-SEC`.
- [ ] `go build ./...`, `go test -race -cover ./...`, `golangci-lint run`, `go vet ./...`,
      `go mod tidy` (no diff), `govulncheck ./...` pass; CI green on Windows, Linux, macOS.
- [ ] No new Go dependency; no JS/CSS library; no file fetched from a CDN.
- [ ] No new write path on the API; route sweep test green.
- [ ] Kickoff decisions recorded (this document §3.3 updated from "Proposed" to the
      answers); any SCOPE change recorded in §23 "Settled decisions" in the same PR;
      SPEC-GAPS untouched unless the maintainer decided them.
- [ ] `docs/dashboard.md`, `CHANGELOG.md` `[Unreleased]`, and README docs table updated.
- [ ] Commits Conventional, signed (`-S`), signed off (`-s`), no AI attribution; all PRs
      target `develop`.
- [ ] Retro: learnings moved into `docs/skill/` (e.g. a new section in `local-api-daemon`
      for cookie auth and the dashboard SSE variant); new gaps added to `SPEC-GAPS.md`.

## 10. Future work

Explicitly **out of this sprint**:

- **Actions for the lead:** approve promotion (`pending_human_approval`), accept/reject
  or reassign cross-requests, pause/resume tasks, revoke leases, cancel sessions. Needs a
  write-capable scope, CSRF tokens on top of Origin checks, confirmation UX, and audit rows
  (§17, §24.14).
- **Multi-project view:** a project switcher and one session covering several projects.
- **Remote access via the Hub:** viewing a daemon from another machine needs TLS and real
  user authentication; depends on the multi-machine hub (§3.2, §23 #8).
- **History and charts:** trends from `/metrics` once its format is decided (U-API-3).
- **Diff viewer and log tail:** need careful secret handling (`blocked_patterns`).
- **Desktop notifications** for escalations and failed gates.

## 11. Risks and mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Stored XSS from agent-written text (goals, titles) | Script runs in the dashboard origin | `h()` with text nodes only; CSP without inline; asset lint bans `innerHTML`/`eval` (S7-T06, S7-T09) |
| Cookie sent to other servers on other `127.0.0.1` ports | Read-only session leak to a local process | Read-only, project-scoped, 8 h TTL, port-suffixed name; documented residual risk; `AG-SEC` review (S7-T23) |
| DNS rebinding / CSRF from a website | Data read or actions from a foreign page | Host allowlist, Origin + `Sec-Fetch-Site`, `SameSite=Strict`, GET-only cookie allowlist (S7-T04, S7-T07) |
| One-time token leaks (argv, history, logs) | Session hijack within 60 s | Fragment, `replaceState`, single use, 60 s TTL, log capture tests |
| Dashboard load slows the daemon with 20+ sessions | Agents wait longer | Throttled refetch, stream/session caps, no ACK rows, load test (S7-T21) |
| Event cursor undecided (U-DATA-3, §9.9) | Missed events after reconnect | Uses S2-E2's cursor as-is; resume tests; refetch on dirty |
| JS rots without automated tests | Regressions slip in | Small pure reducer, self-test page, Go asset linter, manual checklist each release |
| Windows MIME registry maps `.js` wrongly | Blank page on some machines | Fixed content-type map; CI asset tests on Windows |
| Scope creep into actions | Read-only promise broken | Route sweep test; actions listed only as future work |
| Unknown/undecided states (groups, leases) | Broken badges | Opaque state strings with neutral fallback style |
| Open SSE streams block daemon shutdown | `daemon stop` hangs | Shutdown context via `RegisterOnShutdown` (S7-T16) |
| Browser connection limit (~6 per host) with many tabs | Later tabs hang | One stream per tab; documented in `docs/dashboard.md` |
| Kickoff decisions slip | Critical path blocked | Decisions listed in §3.3 with defaults; `AG-LEAD` resolves on day 1 |
