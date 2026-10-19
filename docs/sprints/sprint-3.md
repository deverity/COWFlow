# Sprint 3 — MCP + runners

> **Phase:** 3 ([SCOPE.md §19](../SCOPE.md)) · **Length:** 2 weeks (10 working days) · **Exit criterion:** the lead in the VS Code extension creates two tasks; two workers with a real dependency (auth → api), **one Claude Code and one Codex**, finish the feature without a human editing code. *"The test that decides the product."*

---

## 1. Goal

Until now COWFlow has been a tool that humans drive from a terminal. This sprint
makes it a tool that **LLM agents** drive.

By the end of the sprint:

- `cowflow mcp --role lead|worker|watchdog` is an MCP server. An agent talks to
  COWFlow through tools such as `cowflow_announce_intent` or
  `cowflow_publish_change`; it never touches SQLite or Git refs itself.
- `cowflow init` writes `.mcp.json` at the repo root, so the Claude Code
  extension in VS Code sees COWFlow as soon as the folder is opened. That
  extension session is the **lead**.
- The lead calls `cowflow_create_task` twice and `cowflow_run` once. COWFlow
  starts one headless agent per task (`claude -p …` for `auth`, `codex exec …`
  for `api`) inside each task's COW workspace. Each worker gets its own scoped
  token and a generated protocol file (`CLAUDE.md` / `AGENTS.md`).
- The rules are enforced by the **server**, not by the model's goodwill:
  publish without an intent, publish of `.mcp.json`, publish from `develop`, a
  worker acting on another task — all refused, whatever the model says.
- Usage per provider (Anthropic, OpenAI) is recorded, and a provider at its
  concurrency limit does not stop the other provider's workers.

There is still **no daemon** (that is Sprint 4). Everything runs daemonless on
the SQLite store from `S0-E3`/`S2-E1`, plus whatever process hosts the runners
(see gap `C15` in §3).

---

## 2. Exit criteria

- [ ] **§19 Phase 3 output** — lead in VS Code creates two tasks; Claude worker (`auth`) and Codex worker (`api`, `depends auth`) finish the feature with no human editing code.
  *Verified by:* demo script (§8) on a clean Windows 11 machine; evidence (`cowflow events --json` dump, `git log --all --oneline` of the fixture, worker logs, recording of the VS Code session) attached to the sprint-closing PR (`S3-T24`).
- [ ] **§20.18** — two LLM agents with a real dependency complete a feature end-to-end.
  *Verified by:* the same run — `api` sends `expose_interface` to `auth`, `auth` delivers a change, `api` syncs it, both `cowflow_report_result` with acceptance answered, fixture tests pass in the `api` workspace.
- [ ] **§20.27** — lead in the VS Code extension creates tasks, triggers `run`, follows with `wait_events`, and creates an integration group **via MCP only**, without the CLI.
  *Verified by:* demo steps 5–8; the lead prompt forbids shell use; the extension's tool-call log shows only `cowflow_*` tools; audit rows show the lead acting via MCP. The integration-group step depends on `C16` (§3.3).
- [ ] **§20.28** — Claude worker and Codex worker complete a cross-request with each other; the scheduler respects the per-provider limit (limit 1 on one vendor while the other continues).
  *Verified by:* hermetic `TestE2E_ProviderLimit` (`S3-T23`, fake agents, `-race`) **and** demo step 10 with `[scheduler.llm.openai] max_concurrent_requests = 1`.
- [ ] **§20.29** — `init` generates a valid `.mcp.json` recognized by the Claude Code extension and by the Codex CLI.
  *Verified by:* golden-file test (`S3-T10`); manual — the extension shows the `cowflow` server connected; Codex half per the `U-API-7` findings in `S3-T19`.
- [ ] **§20.31 (sprint 3 part)** — with `init --conventions gitflow`, a branch outside the pattern is refused in `task create`; a direct commit to `main`/`develop` by an agent is refused.
  *Verified by:* tests in `S3-T11` and `S3-T20` tagged `// AC#31`; demo step 11. The hotfix → `develop` group part of §20.31 is Sprint 5 (`S5-E7`).
- [ ] **Sprint hygiene** — all Sprint 3 PRs merged to `develop`; CI green on Windows/Linux/macOS; `CHANGELOG.md` `[Unreleased]` updated.
  *Verified by:* `AG-LEAD` on the sprint-closing PR.

---

## 3. Prerequisites

### 3.1 Earlier epics that must be merged

| Epic | Used for |
|---|---|
| `S0-E1`, `S0-E2` | cobra tree (`mcp`, `runners`, `run` stubs), exit codes, `--json`; `config.toml` parsing incl. `[runner]`, `[runner.<name>]`, `[scheduler.llm]`, `[scheduler.llm.<provider>]`, `[conventions]`, `[security]` |
| `S0-E3` | SQLite store, embedded migrations (new columns this sprint) |
| `S0-E4` | Git wrapper (`check-ref-format`, `-C`, `GIT_TERMINAL_PROMPT=0`) |
| `S0-E5` | Process-tree spawn/kill (Job Objects / process groups) — every runner process goes through it |
| `S0-E8` | Hermetic harness: fake clock, tmp Git repo, fault injection |
| `S1-E1`, `S1-E2`, `S1-E4`, `S1-E5` | Workspaces the runners execute in; sparse by intent paths |
| `S1-E8` | `task create` (extended with conventions in `S3-T11`), `open` |
| `S2-E1` … `S2-E11` | Tasks/sessions/intents/requests/changes, outbox + cursor polling, index, sync, contracts, `pattern` invariants, CLI parity. The MCP tools are thin adapters over these services |

### 3.2 Tools and accounts

| Item | Who | Notes |
|---|---|---|
| Go 1.26, `git >= 2.40`, `golangci-lint` v2 | everyone | as in Sprint 0 |
| Node.js LTS (for vendor CLIs) | `AG-MCP`, `AG-QA`, `AG-LEAD` | Claude Code and Codex CLI are distributed via npm |
| Claude Code CLI + VS Code extension | `AG-MCP`, `AG-QA`, `AG-LEAD` | `npm install -g @anthropic-ai/claude-code`; extension "Claude Code" from the VS Code Marketplace. Docs: <https://docs.anthropic.com/en/docs/claude-code> |
| Codex CLI | `AG-MCP`, `AG-QA`, `AG-LEAD` | `npm install -g @openai/codex`. Docs: <https://github.com/openai/codex> |
| Anthropic and OpenAI credentials | `AG-QA`, `AG-LEAD` | API keys in **environment variables only** (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`) or the vendor's own `login` flow. Never in `config.toml`, `.mcp.json`, `.env`, test fixtures, CI logs, or any committed file. Real-agent CI jobs use repository secrets and are opt-in |
| Spending cap on both vendor accounts | `AG-LEAD` | Set a hard monthly limit in each vendor console before the first real run (§10 risk "cost") |
| Windows 11 with Dev Drive (optional for dev, required for the demo) | `AG-QA` | `cowflow init --devdrive` from `S0-E6`; worktree fallback is fine for unit work |

Verify the CLIs before day 2:

```powershell
claude --version
claude -p --help      # must list --mcp-config and --dangerously-skip-permissions
codex --version
codex exec --help     # must list --config and --full-auto
```

If a flag is missing, **do not** edit the spec's `[runner.*]` defaults in code —
report the version to `AG-LEAD` (§23 risk "Vendor CLI changes flags").

### 3.3 Spec gaps affecting this sprint (not decided here)

IDs are the stable ones from [SPEC-GAPS.md](../SPEC-GAPS.md). `C15`, `C16`,
`C17`, `U-API-18`, `U-SCHED-8` and `U-GIT-10` were found while planning this
sprint.

| Ref | Gap (source) | Blocks | While blocked |
|---|---|---|---|
| SCOPE.md §23 Open question 1 | Final MCP SDK (official Go SDK vs `mark3labs/mcp-go`) | `S3-T01` concrete adapter (critical path) | Everything else codes against `ToolServer` + in-memory fake |
| ~~`C1`~~ | Resolved: Go 1.26+ (SCOPE.md §23 decision 1) | nothing (use `go.mod`) | — |
| `C2` | Task branch `cowflow/<project>/<task-slug>` vs `<kind>/<slug>` under gitflow | `S3-T11` (which namer applies outside the gitflow preset) | Implement both behind `BranchNamer`; selection waits |
| `C3` | `promotion_mode` `manual\|auto` vs also `pr` | `S3-T11` (`init --conventions gitflow` must not write `pr`); `S3-T04` `cowflow_promote` accepts only `manual` behavior | Validate only `manual\|auto`; `pr` returns a validation error naming `C3` |
| `C8` | Contract edit needs *open* vs *accepted* `confirm_contract` | `S3-T20` foreign-contract boundary rule | Rule implemented with the predicate injected; merge waits |
| `C10` | Aider `prompt_mode = "message"` | `S3-T12` validation of `prompt_mode` | Aider is out of this sprint; validator rejects unknown modes except where `C10` is decided |
| `C11` | Two different `[scheduler.llm.anthropic]` examples | `S3-T22` default values | Use only values present in the user's `config.toml`; no baked-in provider defaults |
| `C12` | §24.13 never-commit list vs `[security] blocked_patterns` | `S3-T20` | Check both lists; how they combine (union vs override) waits |
| `U-API-6` | "Default role of `cowflow mcp` without `--role`" (§14, §15) | `S3-T06` | `--role` required; missing role → exit `1` until decided |
| `U-API-1` | "Lead token storage" + `config.toml`/SQLite location (§5, §15, §22) | `S3-T09`, `S3-T10` | `LeadTokenStore` interface + test fake; on-disk location waits |
| `U-API-7` | "Codex MCP config passes no role/token" (§15.3) — `mcp_servers.cowflow.command=cowflow` runs bare `cowflow` | `S3-T19`, §20.28/§20.29 Codex half | Codex adapter is config-driven; the fix is expected to be a config change, decided by the maintainer |
| `U-API-7` | "Gemini `.gemini/settings.json` path and writer; generic runner has no permission/MCP flags" (§15.3) | `S3-T14` generic runner MCP wiring | Generic runner gets `.cowflow/mcp.json` written but no flag injected |
| `U-API-8` | "How session tokens are validated in daemonless mode" (§5.3, §15) | `S3-T08`, `S3-T09` | Validator behind interface; proposal (hash lookup in SQLite) awaits confirmation |
| `U-API-9` | "Watchdog tool set: worker base + extras, or read-only + requests only" (§5.4, §15.1) | `S3-T05` watchdog registration | Registry entry for watchdog is a single table row; filled after decision |
| `U-SCHED-4` | "How usage is measured per runner"; "whether 80%/100% applies to provider budgets"; "budget actions other than `pause`" (§11, §24.15) | `S3-T22`, `S3-T18`, `S3-T19` usage parsing | Ledger records whatever adapters report, tagged with `source`; thresholds are Sprint 4 (`S4-E7`) |
| `U-API-2` | "How CLI/MCP detect daemon vs daemonless" (§5.3) | nothing this sprint (always daemonless) | `Backend` interface keeps the Sprint 4 HTTP backend pluggable |
| `U-SCHED-1` | "Whether leases … work daemonless" (§5.3, §9.8) | `S3-T03` `cowflow_resource_*` behavior | Tools registered; backend returns `unavailable` until `S4-E6` |
| `U-DATA-3` | "Whether the event cursor is the ULID" (§9.9) | `S3-T05` `cowflow_wait_events` | Use the cursor type exported by `S2-E2` verbatim; never parse it |
| `U-DATA-4` | "Concrete `cross_request.*` event names" (§9.9) | `S3-T05` filters, `S3-T17` protocol text | Use names exported by `S2-E4` constants; never hard-code strings |
| `U-API-4` | "HTTP status ↔ CLI exit code mapping" (§12, §14) — the same question applies to MCP errors | `S3-T02` error envelope | Guidance codes in `S3-T02`; AG-LEAD confirms |
| `U-SCHED-5` | "`depends` depth: edges or nodes" (§24.7) | `S3-T04` `cowflow_create_task` (validation is reused from the task service) | Delegate to the existing validator; no new rule in MCP |
| `C15` | **Runner host before the daemon exists.** §5.3 lists "runner" as a daemon capability; §19 puts runners in Phase 3 and the daemon in Phase 4 | `S3-T16` | `runner.Host` interface; the two options are (a) `cowflow run` / the MCP process hosts workers in the foreground, (b) pull the on-demand start of `S4-E1` forward. Day-1 decision |
| `U-API-18` | **Protocol injection into a tracked `context_file`.** If the repo already tracks `CLAUDE.md`/`AGENTS.md`, writing the protocol dirties a tracked file, and publish of it is forbidden (§24.14) | `S3-T17` tracked-file case | Untracked case proceeds (`info/exclude`); tracked case refuses with a clear error until decided |
| `C16` | **§20.27 needs `cowflow_create_integration` this sprint, but the integration-group model is `S5-E1` (`AG-INT`)** | `S3-T04` integration tools; exit criterion §20.27 | Either `AG-INT` lands a minimal `Create` slice (record + members + "refused without change") in Sprint 3, or §20.27 is re-mapped to Sprint 5 in the README. Day-1 decision |
| `U-SCHED-8` | **Unit of `max_concurrent_requests`.** COWFlow cannot see a vendor CLI's HTTP requests; it can only count running sessions | `S3-T22` | `ProviderGate` counts admissions; what one admission means awaits decision (Guidance: one running session = one slot) |
| `U-GIT-10` | **Detection of worker `git push` / `git tag` / branch deletion** (§24.14). The worker has a shell; the spec does not say how these are prevented or detected | `S3-T20` refs part | Publish-time checks only; ref snapshot detection proposed, not built |
| `C17` | `cowflow_wait_events` signature `(filter, after_cursor, timeout_seconds)` (§15.1) vs `until=tasks_completed` in the §5.4 flow | `S3-T05` | Schema follows §15.1; `until` support waits |

**Day-1 kickoff (`AG-LEAD` with the maintainer) must resolve at least:** SCOPE.md §23 Open question 1,
`U-API-1`, `U-API-8`, `C15`, `C16`. They sit on the critical path (§5).

---

## 4. Concepts you need

**MCP (Model Context Protocol).** A JSON-RPC 2.0 protocol between an LLM
client (Claude Code, Codex) and a server that offers **tools**. The client calls
`initialize`, then `tools/list` to learn tool names and their JSON Schemas, then
`tools/call` with arguments; the server answers with content blocks (we return
one JSON text block) and an `isError` flag. Our transport is **stdio**: the
client starts `cowflow mcp …` as a child process and talks over its
stdin/stdout. Consequence: **nothing but protocol may ever be written to
stdout** — logs go to stderr or a file. Spec: <https://modelcontextprotocol.io/>;
SCOPE §15.

**Roles: lead, worker, watchdog.** One binary, three tool sets (§5.4, §15.1).
The *lead* orchestrates (create tasks, run, wait, integrate) and never edits
worker code. A *worker* is a headless agent in one workspace doing one task. A
*watchdog* observes and opens requests, never publishes. A tool not in the
role's set is **not registered at all**, so the model cannot even see it.

**`.mcp.json`.** A file at the repo root that tells the Claude Code extension
which MCP servers to start for this folder (§15.2). `cowflow init` writes it
with `["mcp", "--role", "lead", "--project", "."]` and `"env": {}` — it holds no
secret and is safe to commit. Workers get a different file,
`<workspace>/.cowflow/mcp.json`, with `--role worker` and
`COWFLOW_SESSION_TOKEN` in `env`; it is git-ignored and never committed.

**Runners and headless agent CLIs.** A runner is a declarative recipe,
`[runner.<name>]` in `config.toml` (§15.3): `cmd`, `mcp_config_flag`,
`permission_flag`, `context_file`, `prompt_mode`, `provider`. COWFlow builds an
**argument slice** from it (never a shell string), substitutes `{mcp_config}`,
`{workspace}`, `{prompt}`, `{task_id}` per element, and starts the process in the
workspace under a killable process tree. "Headless" means the agent runs
without asking a human for permission (`--dangerously-skip-permissions`,
`--full-auto`) — so its code is untrusted (§15.4, §17).

**Protocol injection.** Each vendor reads a different instruction file:
`CLAUDE.md` for Claude Code, `AGENTS.md` for Codex. With
`[runner] inject_protocol = true`, the runner renders the COWFlow protocol
(session start/checkpoint/end §24.13, the three waiting options §9.3,
boundaries §24.14, acceptance criteria §24.7, contracts) into that file in the
workspace before starting the agent. Injection *teaches*; it does not
*enforce*. SCOPE §15.5.

**Server-side enforcement.** Principle 3 (§2.2): "the server refuses; the model
does not choose". Every rule lives in the service layer (`internal/collab`,
task service, publish path) that CLI, MCP and the future API all call. An MCP
handler only decodes input, resolves identity, calls the service and encodes
the result. If a rule were only in the MCP handler, an agent running
`cowflow --json` from its shell would bypass it.

**Scoped tokens.** Identity comes from a secret, not from arguments (§12, §15,
§17). At spawn, COWFlow issues a session token whose **scope** is
`{project, task, workspace, session, role}`. The MCP server reads it from
`COWFLOW_SESSION_TOKEN`, resolves the scope once, and passes it to every
service call. Tools never accept `workspace_id` or `session_id`. The lead token
is project-wide and issued by `init` to the local user (§15.1). Store only a
hash; compare in constant time; never log the secret.

**Per-provider budgets and rate limits.** One subscription account saturates
with 3–6 agents (§5.4). `[scheduler.llm.<provider>]` gives each vendor its own
`max_concurrent_requests` and budgets, so when OpenAI is at its limit the
Anthropic workers keep going (§15.3, §20.28). This sprint builds the admission
gate and the usage ledger; the 80% warning / 100% pause enforcement is
`S4-E7` (§24.15).

---

## 5. Agents and lanes

| Agent | Tasks |
|---|---|
| `AG-LEAD` | Day-1 decisions (§3.3), contract freeze (§6), all reviews, `S3-T24` (with `AG-QA`) |
| `AG-MCP` (server lane) | `S3-T01`, `S3-T08`, `S3-T02`, `S3-T09`, `S3-T03`, `S3-T05`, `S3-T04`, `S3-T21` |
| `AG-MCP` (runner lane) | `S3-T12`, `S3-T13`, `S3-T14`, `S3-T17`, `S3-T18`, `S3-T19`, `S3-T16` |
| `AG-CLI` | `S3-T06`, `S3-T10`, `S3-T11`, `S3-T15` |
| `AG-STORE` | `S3-T07` |
| `AG-COLLAB` | `S3-T20` |
| `AG-SCHED` | `S3-T22` |
| `AG-QA` | `S3-T23`, `S3-T24` |
| `AG-SEC` | Mandatory reviewer of every task marked `Security: yes` |

Guidance: `AG-MCP` owns 15 tasks; staff it with two people/agents (server lane,
runner lane) acting as one role. Ownership of `internal/mcp` and
`internal/runner` stays with `AG-MCP`; the two lanes review each other.

```text
Day          1     2     3     4     5     6     7     8     9     10
AG-LEAD    [kick][------------------ reviews -------------------][demo ]
MCP server [T01-----][T08--][T02][T09][T03-----------][T05][T04--------][T21-----]
MCP runner [T12-----][T13-------][T14--][T17------][T18--][T19--][T16-------]
AG-CLI           [T06-----][T10-----][T11------------][T15--]
AG-STORE   [T07-----]
AG-COLLAB        [T20-----------------------------]
AG-SCHED         [T22-----------------------------]
AG-QA            [T23 fixture + fake agent ------------------------][T24 real--]
AG-SEC           reviews: T07 T08 T09 T10 T12 T13 T14 T16 T17 T18 T19 T20 T21
```

"Depends on" in a task means the *interface* in §6 is frozen; per README
ownership rule 2 the consumer codes against fakes until the provider's PR
merges, and only the final integration needs the real implementation.

**Critical path:** day-1 SDK decision (SCOPE.md §23 Open question 1) → `S3-T01` → `S3-T02` →
`S3-T03` → `S3-T04` (needs `S3-T16`, which needs `C15`) → `S3-T23` → `S3-T24`.
The runner lane (`S3-T12` → `S3-T13` → `S3-T17` → `S3-T18`/`S3-T19` →
`S3-T16`) has about one day of slack. Any slip on `S3-T01` moves the demo.

---

## 6. Contracts

Frozen by `AG-LEAD` on day 1. Changing a signature after day 1 needs a comment
on this section's PR and a note to every consumer. Shapes are Guidance unless a
name is quoted from the spec; spec names (tools, config keys, roles) are fixed.

### 6.1 MCP SDK adapter — provider `AG-MCP` (`S3-T01`), consumers: rest of `internal/mcp`

```go
// internal/mcp/server.go — no SDK import here.
package mcp

type ToolDef struct {
	Name        string          // exact §15/§15.1 name, e.g. "cowflow_get_context"
	Description string
	InputSchema json.RawMessage // JSON Schema object; additionalProperties:false
}

// ToolResult is encoded as ONE text content block containing JSON.
type ToolResult struct {
	Body    any
	IsError bool
}

type ToolHandler func(ctx context.Context, args json.RawMessage) (ToolResult, error)

// ToolServer is the only seam to the MCP SDK (§23 risk "Go MCP ecosystem changes").
type ToolServer interface {
	AddTool(def ToolDef, h ToolHandler) error
	Serve(ctx context.Context, in io.Reader, out io.Writer) error // stdio in prod
}

type ServerFactory func(name, version string) ToolServer
```

Fake: `mcptest.NewServer()` — in-memory `ToolServer` with `Call(name, args)`
for unit tests; plus `mcptest.Pipe()` that runs a real `ToolServer` over
`io.Pipe` and speaks raw JSON-RPC, used by the conformance suite.

### 6.2 Tool registry per role — provider `AG-MCP` (`S3-T02`)

```go
type Role string

const (
	RoleLead     Role = "lead"
	RoleWorker   Role = "worker"
	RoleWatchdog Role = "watchdog"
)

// ToolsFor returns the exact tool names a role exposes (§15, §15.1).
// Static table; tests compare it against the spec lists.
func ToolsFor(r Role) []string

type Registry struct{ /* name -> ToolDef + handler factory */ }
func (r *Registry) Mount(s ToolServer, role Role, deps Deps, scope token.Scope) error

// Error envelope (Guidance, pending U-API-4): stable code + message + diagnostics.
type ToolError struct {
	Code        string       `json:"code"` // usage|invalid_state|conflict|refused_by_policy|unauthorized|unavailable|internal
	Message     string       `json:"message"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}
```

### 6.3 Backend services consumed by MCP tools — providers `AG-COLLAB`, `AG-STORE`, `AG-GIT`, `AG-SCHED`, `AG-INT`

Defined **in `internal/mcp`** (consumer side), satisfied by the Sprint 2
services. Every method takes the resolved `token.Scope`; none takes a
workspace or session ID from the agent.

```go
type Deps struct {
	Tasks       TaskService        // S1-E8/S2-E1 + S3-T11 conventions
	Intents     IntentService      // S2-E3
	Requests    RequestService     // S2-E4
	Changes     ChangeService      // S2-E5 (+ S3-T20 enforcement inside)
	Sync        SyncService        // S2-E7
	Index       IndexService       // S2-E6
	Events      EventService       // S2-E2
	Leases      LeaseService       // S4-E6; until then returns ErrUnavailable (U-SCHED-1)
	Runs        RunService         // S3-T16
	Integration IntegrationService // C16
	Usage       scheduler.UsageLedger
	Clock       Clock
}

type IntentService interface {
	Announce(ctx context.Context, sc token.Scope, opID string, in IntentInput) (Intent, error)
	ExpandScope(ctx context.Context, sc token.Scope, opID string, paths []string) (Intent, error)
}

type ChangeService interface {
	Publish(ctx context.Context, sc token.Scope, opID string) (Change, []collab.Diagnostic, error)
	List(ctx context.Context, sc token.Scope, f ChangeFilter) ([]Change, error)
}

type EventService interface {
	// Read returns events strictly after cursor (cursor type from S2-E2, opaque here).
	Read(ctx context.Context, sc token.Scope, f EventFilter, after Cursor, limit int) ([]Event, Cursor, error)
}
// TaskService, RequestService, SyncService, IndexService, LeaseService,
// RunService, IntentService: one method per tool in §15/§15.1, same pattern.
```

Fakes: `internal/mcp/mcptest/fakes.go` — map-backed implementations recording
calls, used until the real service is wired.

### 6.4 Tokens — provider `AG-MCP` (`S3-T08`, security), consumers: `internal/mcp`, `internal/runner`, `internal/cli` (`init`), Sprint 4 `internal/api`

```go
// internal/mcp/token
package token

type Role string // "lead" | "worker" | "watchdog"

type Scope struct {
	ProjectID   string
	TaskID      string // empty for lead
	WorkspaceID string // empty for lead and watchdog
	SessionID   string // empty for lead (project-wide token, §15.1)
	Role        Role
}

// Secret never prints: String() and LogValue() return "[REDACTED]".
type Secret string

type Issuer interface {
	IssueSession(ctx context.Context, opID string, sc Scope) (Secret, error)
	IssueLead(ctx context.Context, opID, projectID string) (Secret, error)
	Revoke(ctx context.Context, opID, sessionID string) error
}

type Validator interface {
	Validate(ctx context.Context, s Secret) (Scope, error) // ErrInvalidToken, ErrRevoked
}

type LeadTokenStore interface { // location blocked by U-API-1
	Load(ctx context.Context, projectID string) (Secret, error)
	Save(ctx context.Context, projectID string, s Secret) error
}

// Authorize is deny-by-default (§17).
func Authorize(sc Scope, a Action, t Target) error // ErrForbidden
```

Fakes: `tokentest.NewMemory()` implements `Issuer`, `Validator`, `LeadTokenStore`.

### 6.5 Runner — provider `AG-MCP` (`S3-T12`/`S3-T13`), consumers: `RunService` (`S3-T16`), `internal/cli` (`runners`, `run`)

```go
// internal/runner
type PromptMode string // "arg" | "stdin" | "file" (§15.3; "message" pending C10)

type Spec struct { // mirrors [runner.<name>]
	Name, Provider, ContextFile string
	Cmd, MCPConfigFlag, PermissionFlag []string
	PromptMode PromptMode
}

type Invocation struct {
	Spec          Spec
	TaskID        string
	Workspace     string        // confined absolute path
	Prompt        string
	MCPConfigPath string        // <workspace>/.cowflow/mcp.json
	Token         token.Secret  // only ever placed in env / mcp.json env
	Protocol      ProtocolInput // rendered into ContextFile (S3-T17)
}

type Adapter interface {
	Name() string
	Check(ctx context.Context) (VersionInfo, error)     // `runners check`
	Prepare(ctx context.Context, inv Invocation) error  // context_file + .cowflow/mcp.json
	Argv(inv Invocation) (argv []string, stdin io.Reader, err error)
	ParseUsage(stdout []byte) (scheduler.Usage, bool)   // U-SCHED-4: false when unknown
}

type Handle interface {
	Wait() (ExitInfo, error)
	Cancel(ctx context.Context) error // kills the whole tree (S0-E5)
}

type Supervisor interface {
	Start(ctx context.Context, inv Invocation, a Adapter) (Handle, error)
}

// Host decides where supervised processes live (C15).
type Host interface {
	Dispatch(ctx context.Context, req RunRequest) (RunResult, error)
}
```

Fakes: `runnertest.FakeAgent` — a tiny Go program built in `TestMain` that
acts as an "agent CLI": it reads its MCP config, starts `cowflow mcp`, and plays
a scripted list of tool calls (used by `S3-T23`).

### 6.6 Enforcement hooks — provider `AG-COLLAB` (`S3-T20`, security), consumers: change publish path (`AG-GIT`), task service, `internal/mcp`

```go
// internal/collab/enforce.go
type Severity string // "refuse" | "warn"

type Diagnostic struct {
	Rule     string   `json:"rule"`     // e.g. "boundary.mcp_json", "protected_branch"
	Path     string   `json:"path,omitempty"`
	Message  string   `json:"message"`
	Severity Severity `json:"severity"`
}

type PublishCheck struct {
	Scope      token.Scope
	HeadBranch string          // branch HEAD points to in the workspace
	Manifest   domain.Manifest // §8.2, already computed
	Intent     *domain.Intent  // nil = no intent
	Generated  []string        // generated files of this workspace (context_file, .cowflow/*)
}

type Enforcer interface {
	// Returns all diagnostics; err wraps ErrRefusedByPolicy if any is "refuse".
	CheckPublish(ctx context.Context, in PublishCheck) ([]Diagnostic, error)
}

var ErrRefusedByPolicy = errors.New("refused by policy") // exit code 4
```

### 6.7 Budget accounting — provider `AG-SCHED` (`S3-T22`), consumers: `RunService`, runner adapters, Sprint 4 `S4-E7`

```go
// internal/scheduler/budget.go
type Usage struct {
	Provider, SessionID, TaskID string
	InputTokens, OutputTokens   int64
	CostUSD                     float64
	Source                      string // "claude-code:json", "codex:jsonl", "unknown" (U-SCHED-4)
	At                          time.Time
}

type UsageLedger interface {
	Record(ctx context.Context, opID string, u Usage) error
	Totals(ctx context.Context, q UsageQuery) (UsageTotals, error) // by provider|task|session, window
}

type Admission struct {
	Admitted bool
	Reason   string // e.g. "provider openai at max_concurrent_requests=1"
}

type ProviderGate interface {
	TryAdmit(ctx context.Context, provider, sessionID string) (Admission, error)
	Release(ctx context.Context, provider, sessionID string) error
}
```

Fakes: `schedtest.Gate` (counts in memory), `schedtest.Ledger`.

---

## 7. Tasks

### S3-T01 — MCP SDK adapter behind `ToolServer`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-MCP` | `AG-LEAD`, `AG-CLI` | `S3-E1` | SCOPE.md §23 Open question 1 decision | `feat/mcp-sdk-adapter` | no |

**Skills:** `mcp-and-runners`, `go-conventions`, `testing-harness`, `gitflow`   **Spec:** §15, §16, §23 (risk "Go MCP ecosystem changes", Open question 1)
**Files:** `internal/mcp/server.go`, `internal/mcp/sdkadapter.go` (only SDK import), `internal/mcp/mcptest/server.go`, `internal/mcp/conformance_test.go`, `go.mod`

**Why / what** — The Go MCP ecosystem moves fast; the spec keeps the SDK
swappable. All of `internal/mcp` talks to `ToolServer` (§6.1); exactly one file
imports the SDK the maintainer chose on day 1. A conformance suite proves any
adapter behaves the same, so a future swap is one file plus a green suite.

**Steps**
1. `git switch develop && git pull --ff-only && git switch -c feat/mcp-sdk-adapter`.
2. Write `server.go` with the §6.1 types. No SDK import.
3. Write `mcptest/server.go`: in-memory `ToolServer` storing defs and handlers; `Call(ctx, name, json)` invokes the handler; `Tools()` lists names.
4. Write `sdkadapter.go` for the decided SDK: `New(name, version string) ToolServer`. `AddTool` converts `ToolDef.InputSchema` to the SDK's schema type and wraps the handler so that a returned `ToolResult` becomes one text content block (`json.Marshal(res.Body)`) with `isError = res.IsError`, and a Go `error` becomes `isError: true` with `{"code":"internal","message":"internal error"}` (never the raw error text — it may contain paths).
5. `Serve` runs the SDK's stdio transport on the given reader/writer and returns when `ctx` is cancelled or stdin closes.
6. `go get <sdk>@<pinned version>`; `go mod tidy`. Record the version in the PR body.
7. Conformance suite (`conformance_test.go`), parameterised by `ServerFactory`, drives raw JSON-RPC over `io.Pipe`:
   ```go
   send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`)
   send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
   send(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
   send(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"x":1}}}`)
   ```
   Guidance: use the protocol version the chosen SDK reports as latest.
8. Commit: `git commit -S -s -m "feat(mcp): add sdk adapter behind tool server interface"`; PR to `develop`.

**Tests** — conformance suite against the real adapter and against `mcptest`
(where applicable): `initialize` handshake, `tools/list` returns exactly the
registered names and schemas, `tools/call` success, `isError` path, unknown tool
→ JSON-RPC error, stdin EOF ends `Serve`, ctx cancel ends `Serve`, nothing but
JSON-RPC lines on stdout. `go test -race ./internal/mcp/...`.

**Acceptance**
- [ ] `grep -rl "<sdk module path>" internal/` lists only `sdkadapter.go`.
- [ ] Conformance suite green on 3 OSes; `CGO_ENABLED=0 go build ./...` passes.
- [ ] `go mod tidy` is a no-op; SDK version pinned.

**Pitfalls** — Writing a log line to stdout breaks the protocol (the client
sees garbage and disconnects): configure `slog` to stderr in `Serve`'s caller.
Leaking SDK types into other files "just this once" defeats the adapter.
Choosing the SDK yourself if the decision is late — escalate instead.

---

### S3-T02 — Tool catalogue, role registry and error envelope

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-MCP` | `AG-LEAD`, `AG-CLI` | `S3-E1` | `S3-T01` (interface only) | `feat/mcp-role-registry` | no |

**Skills:** `mcp-and-runners`, `collaboration-protocol`, `go-conventions`   **Spec:** §15, §15.1, §14 (exit codes), §24.11
**Files:** `internal/mcp/registry.go`, `internal/mcp/tools_*.go` (defs only), `internal/mcp/schemas/*.json` (embedded), `internal/mcp/errors.go`, tests

**Why / what** — One static table decides which tools each role sees. If the
table is wrong, a worker could call `cowflow_promote`. Every tool also needs a
strict JSON Schema so models get good hints and bad input is rejected before
any service runs.

**Steps**
1. Encode the three lists from §15/§15.1 exactly (copy names from SCOPE.md, do not retype). Worker = the base set of §15. Lead = base set + §15.1 "Additional tools" (Guidance: §15.1 says *additional*; confirm on day 1 together with `U-API-9`). Watchdog row stays empty until `U-API-9` is decided (`S3-T05`).
2. One `ToolDef` per tool. Schemas use `"additionalProperties": false`. Example:
   ```json
   {
     "type": "object", "additionalProperties": false,
     "required": ["goal", "paths"],
     "properties": {
       "goal":      {"type": "string", "minLength": 1, "maxLength": 2000},
       "paths":     {"type": "array", "items": {"type": "string"}, "minItems": 1},
       "contracts": {"type": "array", "items": {"type": "string"}},
       "stub":      {"type": "boolean"},
       "touches_deps": {"type": "boolean"}
     }
   }
   ```
   No schema may contain `workspace_id`, `session_id` or `token` (§15).
3. `Mount(s, role, deps, scope)`: iterate `ToolsFor(role)` and `AddTool` only those — unlisted tools are never registered.
4. `errors.go`: map sentinel errors with `errors.Is` to the envelope codes in §6.2 (Guidance pending `U-API-4`): `collab.ErrRefusedByPolicy` → `refused_by_policy` (CLI exit `4`), invalid state → `invalid_state` (`2`), conflict → `conflict` (`3`), bad input → `usage` (`1`), `token.ErrForbidden` → `unauthorized`, `ErrUnavailable` → `unavailable`. Unknown → `internal` with a generic message; the full error goes to the stderr log with the `op_id`.
5. Input decoding helper: `json.Decoder` with `DisallowUnknownFields`, size cap 1 MiB (mirrors §12).

**Tests** — `TestToolsFor_MatchesSpec` (golden lists), `TestNoIdentityFields`
(walk every schema), `TestMount_UnlistedNotRegistered` (worker mount has no
`cowflow_promote`), error mapping table test, fuzz `FuzzDecodeToolInput` with
seeds in `testdata/fuzz/`.

**Acceptance**
- [ ] Lists equal §15/§15.1 character-for-character.
- [ ] No schema exposes identity fields; unknown fields rejected.
- [ ] Error envelope never contains file paths or token material.

**Pitfalls** — Returning `"workspace_id"` in *outputs* is fine (for display);
accepting it as *input* is not. Do not implement business rules here — only
shape validation.

---

### S3-T03 — Worker tools on the daemonless backend

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-MCP` | `AG-LEAD`, `AG-COLLAB` | `S3-E1` | `S3-T02`, `S3-T09`, `S2-E*` | `feat/mcp-worker-tools` | no |

**Skills:** `mcp-and-runners`, `collaboration-protocol`, `store-and-events`   **Spec:** §15, §5.3, §9.2–§9.3, §24.9–§24.13
**Files:** `internal/mcp/backend.go` (§6.3 interfaces), `internal/mcp/worker_tools.go`, `internal/mcp/mcptest/fakes.go`, tests

**Why / what** — Each worker tool is a thin adapter: decode → call the Sprint 2
service with the token's scope → encode. Rules stay in the services so CLI and
MCP behave identically (§15.5).

**Steps**
1. Write the `Deps` and service interfaces of §6.3 next to their consumer.
2. Implement handlers for every worker tool. `cowflow_get_context` returns task, workspace, `base_sha`, contracts, invariants, requests addressed to me, available changes (§15) — all derived from `scope`.
3. Generate an `op_id` per call (ULID) and pass it to mutating services so retries are idempotent (§18).
4. `cowflow_publish_change` returns `{change, diagnostics}`; on refusal, `isError: true` with `code: refused_by_policy` and the diagnostics list, so the model can read *why*.
5. `cowflow_resource_check|acquire|release|announce`: wired to `LeaseService`; until `S4-E6` the injected implementation returns `ErrUnavailable` (`U-SCHED-1`). Do not fake slots.
6. Example output of `cowflow_announce_intent`:
   ```json
   {"intent":{"id":"int_01J…","task_id":"tsk_01J…","goal":"Implement validateJWT","paths":["src/auth"],"state":"active"},
    "overlaps":[]}
   ```
7. Wire the real daemonless backend: a constructor `NewStoreBackend(store, git, clock)` that builds the Sprint 2 services; `cowflow mcp` uses it (`U-API-2`: always daemonless this sprint).

**Tests** — one table test per tool against fakes (input → service call with
the scope, output shape); integration test with the real store and a tmp repo:
intent → publish → list changes; publish without intent returns
`refused_by_policy` (proves enforcement comes from the service, §20.4).

**Acceptance**
- [ ] Every worker tool from §15 implemented; no handler contains a business rule.
- [ ] All mutations carry an `op_id`; repeating a call with the same `op_id` is a no-op.
- [ ] Resource tools return `unavailable` (not success) until `S4-E6`.

**Pitfalls** — Reading the workspace path from the process CWD instead of the
scope; swallowing diagnostics into a generic error (the model then retries
blindly and burns budget).

---

### S3-T04 — Lead tools

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-MCP` | `AG-LEAD`, `AG-CLI` | `S3-E1` | `S3-T03`, `S3-T05`, `S3-T16`, `S3-T11`, `C16` | `feat/mcp-lead-tools` | no |

**Skills:** `mcp-and-runners`, `collaboration-protocol`, `scheduler-and-leases`   **Spec:** §5.4, §15.1, §24.2, §24.7, §24.12
**Files:** `internal/mcp/lead_tools.go`, tests

**Why / what** — The lead in VS Code must do everything through MCP (§20.27):
create tasks, run, follow, integrate. The lead dispatches and returns; no tool
may hold the extension for long.

**Steps**
1. `cowflow_create_task`: schema mirrors `task create` flags (§14, §24.2, §24.7): `slug`, `title`, `base`, `kind`, `contracts`, `paths`, `depends`, `runner`, `acceptance`, `priority`, `budget_tokens`, `no_contract`, `force`. Calls the same `TaskService.Create` as the CLI, so branch pattern, base rules, depth/cycle checks (`U-SCHED-5`) and audit apply.
   ```json
   {"slug":"api","title":"Orders endpoint","kind":"feature","depends":["auth"],
    "contracts":["contracts/auth-api.md"],"paths":["src/api"],"runner":"codex",
    "acceptance":["go test ./src/api/... passes","POST /orders returns 201"]}
   ```
2. `cowflow_update_task`, `cowflow_pause_task`, `cowflow_prioritize`, `cowflow_reassign`, `cowflow_status`: delegate to the task/request services.
3. `cowflow_run({max_agents, runner?})`: calls `RunService.Run` (`S3-T16`) and returns immediately with the sessions started and those queued (e.g. by the provider gate). It never waits for workers to finish.
4. `cowflow_create_integration`, `cowflow_validate`, `cowflow_promote`, `cowflow_approve_promotion`: delegate to `IntegrationService` (`C16`). `cowflow_promote` with `promotion_mode = "manual"` returns `{"status":"pending_human_approval"}` and moves no ref (§15.1). `pr` mode → validation error citing `C3`. Until the integration service exists, these return `unavailable`.
5. `cowflow_resource_revoke`: `LeaseService` (`unavailable` until `S4-E6`).
6. Every lead tool checks `scope.Role == "lead"` through `token.Authorize` (defence in depth — registration already filters).

**Tests** — table tests on fakes; `TestPromote_ManualReturnsPending` asserts
no ref moved (fake git records zero calls); `TestRun_ReturnsWithoutWaiting`
with a fake host that blocks forever; `TestCreateTask_SameRulesAsCLI` (bad
branch name refused with exit-4 class code).

**Acceptance**
- [ ] All §15.1 lead tools registered and implemented or explicitly `unavailable` with the blocking epic in the message.
- [ ] `cowflow_run` returns in < 2 s with a fake host.
- [ ] `cowflow_promote` never promotes in `manual` mode.

**Pitfalls** — Implementing a "run and wait" convenience tool: forbidden by
§5.4. Creating tasks with a different validator than the CLI.

---

### S3-T05 — `cowflow_wait_events` and watchdog tools

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-MCP` | `AG-LEAD`, `AG-COLLAB` | `S3-E1` | `S3-T03`, `S2-E2`, `U-API-9`, `U-DATA-3`, `C17` | `feat/mcp-wait-events` | no |

**Skills:** `mcp-and-runners`, `store-and-events`, `testing-harness`   **Spec:** §5.4, §9.7, §9.9, §15.1, §23 (risk "Lead stuck waiting")
**Files:** `internal/mcp/events_tools.go`, `internal/mcp/watchdog_tools.go`, tests

**Why / what** — The lead follows progress by calling
`cowflow_wait_events(filter, after_cursor, timeout_seconds ≤ 300)`. It must
return events and a new cursor and **never block beyond the timeout**. Without a
daemon there is no SSE, so it polls SQLite.

**Steps**
1. Clamp: `timeout = min(max(timeout_seconds, 0), 300)`; values > 300 are clamped, not rejected (skill rule).
2. Loop: `Events.Read(scope, filter, after, 100)`; if any → return them with the new cursor; else wait on `clock.After(backoff)` (Guidance: 250 ms doubling to 2 s) until the deadline; on deadline return `{"events":[],"cursor":"<same>","timed_out":true}`.
3. Honor `ctx.Done()` (the client may cancel the call).
4. Filters: event types (use constants from `S2-E2`/`S2-E4`, `U-DATA-4`), task IDs. Treat the cursor as opaque (`U-DATA-3`). `until` (`C17`) is not supported until decided.
5. Watchdog: `cowflow_subscribe_events` and `cowflow_read_metrics` are registered only after `U-API-9` is decided. `cowflow_read_metrics` reads the in-store counters available daemonless; `/metrics` arrives with the daemon. Guidance: the watchdog *session* only runs in daemon mode (§5.3), so the Sprint 3 deliverable is the registration and handlers with fakes.

**Tests** — fake clock: returns early when an event arrives at t=3 s; returns
`timed_out` at exactly the clamped timeout; `timeout_seconds = 900` clamped to
300; ctx cancel returns promptly; 20 concurrent waiters each see every event once
(`-race`).

**Acceptance**
- [ ] Never blocks past `min(timeout_seconds, 300)` (fake-clock proof).
- [ ] Always returns a cursor, even on timeout.
- [ ] No `time.Sleep`/`time.Now` in the handler.

**Pitfalls** — Using a ULID comparison yourself (`U-DATA-3` says that can skip
events); polling with a tight loop that pins a CPU core per lead.

---

### S3-T06 — `cowflow mcp` command

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-CLI` | `AG-LEAD`, `AG-MCP` | `S3-E1` | `S3-T02`, `S3-T08` (fakes until `S3-T09`), `U-API-6` | `feat/cli-mcp-command` | no |

**Skills:** `cli-and-config`, `mcp-and-runners`   **Spec:** §14 (`# Setup`, `# MCP`), §15, §15.2
**Files:** `internal/cli/mcp.go`, tests

**Why / what** — The process the extension and the workers start. It must set
up logging away from stdout, resolve identity, mount the role's tools and serve
stdio.

**Steps**
1. Flags: `--role lead|worker|watchdog`, `--project <path>` (§15.2). `--role` is required until `U-API-6` is decided (missing → exit `1`, message names the gap).
2. Build `slog` with a JSON handler on **stderr** (Guidance: plus a file under the state dir once `U-API-1` fixes its location).
3. Resolve identity: worker/watchdog → `COWFLOW_SESSION_TOKEN` via `token.Validator`; lead → `LeadTokenStore.Load(project)` then validate (`S3-T09`). Mismatch between `--role` and token role → exit `4`.
4. Build `Deps` (`S3-T03`), `Registry.Mount`, `Serve(ctx, os.Stdin, os.Stdout)`; cancel on SIGINT/stdin EOF.
5. `--json` has no meaning here; accept it silently (global flag) and document.

**Tests** — run the command in-process with pipes: handshake, `tools/list`
equals `ToolsFor(role)`; missing token → exit `4` and nothing written to
stdout; worker token with `--role lead` → exit `4`.

**Acceptance**
- [ ] Stdout carries only JSON-RPC; all logs on stderr.
- [ ] Role/token mismatch refused before any tool is registered.

**Pitfalls** — cobra printing usage/errors to stdout: set `cmd.SetOut(os.Stderr)`
for this command.

---

### S3-T07 — Store: token and usage columns

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-STORE` | `AG-LEAD`, `AG-GIT`, `AG-SEC` | `S3-E2` | `S0-E3`, `S2-E1` | `feat/store-session-tokens` | yes |

**Skills:** `store-and-events`, `security-review`   **Spec:** §13 (`agent_sessions`, `budgets`), §12, §17, §24.15
**Files:** `internal/store/migrations/00NN_session_tokens.sql`, `internal/store/sessions.go`, `internal/store/budgets.go`, tests

**Why / what** — Tokens and usage need persistence. §13 already lists
`agent_sessions` and `budgets`; columns are unspecified (`U-DATA-9`), so this task adds them
to existing tables instead of inventing new ones.

**Steps**
1. Migration (Guidance on column names):
   ```sql
   ALTER TABLE agent_sessions ADD COLUMN role TEXT NOT NULL DEFAULT 'worker';
   ALTER TABLE agent_sessions ADD COLUMN runner TEXT;
   ALTER TABLE agent_sessions ADD COLUMN provider TEXT;
   ALTER TABLE agent_sessions ADD COLUMN token_hash BLOB;      -- SHA-256 of the secret
   ALTER TABLE agent_sessions ADD COLUMN token_revoked_at TEXT;
   CREATE UNIQUE INDEX agent_sessions_token_hash ON agent_sessions(token_hash);
   -- budgets: one row per usage record (Guidance)
   -- provider, session_id, task_id, input_tokens, output_tokens, cost_usd, source, recorded_at, op_id UNIQUE
   ```
2. Lead token hash: where it lives depends on `U-API-1`; provide `PutLeadTokenHash(projectID, hash)` on `projects` only after the decision.
3. Repository functions: `InsertSessionToken`, `FindSessionByTokenHash`, `RevokeSessionToken`, `InsertUsage` (idempotent by `op_id`), `SumUsage(filter)`.
4. State + event in one transaction where an event is emitted (e.g. `session` revoked).

**Tests** — migration up on an empty DB and on a Sprint 2 DB fixture; unique
hash; idempotent `InsertUsage`; `-race` concurrent inserts.

**Acceptance**
- [ ] Only hashes stored; no column can hold a raw secret.
- [ ] Migration is forward-only and embedded.

**Pitfalls** — Storing the raw token "for debugging"; forgetting the unique
index (lookup becomes a table scan with 20 sessions polling).

---

### S3-T08 — Token issuer, validator and `Authorize`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-MCP` | `AG-LEAD`, `AG-CLI`, `AG-SEC` | `S3-E2` | `S3-T07`, `U-API-8` | `feat/mcp-scoped-tokens` | yes |

**Skills:** `security-review`, `mcp-and-runners`, `go-conventions`   **Spec:** §12, §15, §15.1, §17, §24.14
**Files:** `internal/mcp/token/{token.go,issuer.go,validator.go,authorize.go}`, `internal/mcp/token/tokentest/`, tests

**Why / what** — Identity must come from a secret the agent cannot forge or
borrow. The scope decides what a call may touch.

**Steps**
1. Generate: 32 bytes from `crypto/rand`, base64url, Guidance prefix `cfs_` (session) / `cfl_` (lead) so leaked tokens are recognisable in secret scanners.
2. Hash with SHA-256; store via `S3-T07`. Validation: hash the presented secret, lookup, then compare with `subtle.ConstantTimeCompare`; revoked → `ErrRevoked`. This hash-lookup design is the proposal for `U-API-8`; merge only after `AG-LEAD` confirms.
3. `Secret` implements `fmt.Stringer` and `slog.LogValuer` returning `[REDACTED]`.
4. `Authorize(scope, action, target)`: deny by default. Rules: a worker may act only on its own `TaskID`/`WorkspaceID`/`SessionID`; watchdog may read events and open/comment requests, never publish (§9.7); lead may act on any target in its `ProjectID`; nothing crosses projects.
   ```go
   func Authorize(sc Scope, a Action, t Target) error {
       if sc.ProjectID == "" || sc.ProjectID != t.ProjectID { return ErrForbidden }
       switch sc.Role {
       case RoleLead:
           return nil
       case RoleWorker:
           if t.TaskID != "" && t.TaskID != sc.TaskID { return ErrForbidden }
           if a == ActionPublish && t.WorkspaceID != sc.WorkspaceID { return ErrForbidden }
           return allowWorker(a)
       case RoleWatchdog:
           return allowWatchdog(a) // never ActionPublish
       }
       return ErrForbidden
   }
   ```
5. Session token lifetime: Guidance — valid until the session ends or is revoked; expiry beyond that is unspecified (`U-API-22`), do not invent a TTL.

**Tests** — table tests for `Authorize` (every role × action × own/foreign
target); validator: valid, unknown, revoked, wrong project; `FuzzValidate`;
log capture test proving the secret never appears in `slog` output.

**Acceptance**
- [ ] Deny-by-default proven by a test that adds a new `Action` and expects `ErrForbidden`.
- [ ] Constant-time comparison; no secret in logs, errors or events.
- [ ] `U-API-8` decision recorded in the PR.

**Pitfalls** — `==` on secrets; returning "token belongs to task X" in an error
(information leak); accepting a token from a CLI flag (it would show in process
listings).

---

### S3-T09 — Token plumbing: MCP startup, lead token store, revocation

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-MCP` | `AG-LEAD`, `AG-CLI`, `AG-SEC` | `S3-E2` | `S3-T08`, `U-API-1` | `feat/mcp-token-plumbing` | yes |

**Skills:** `security-review`, `mcp-and-runners`   **Spec:** §15, §15.1, §15.2, §24.14
**Files:** `internal/mcp/token/leadstore.go`, `internal/mcp/identity.go`, tests

**Why / what** — The lead's `.mcp.json` has `"env": {}` (§15.2), so the lead
server must find the lead token on its own; workers get theirs through env.
Sessions that end must lose their power.

**Steps**
1. `identity.Resolve(ctx, role, project, env) (Scope, error)`: worker/watchdog read `COWFLOW_SESSION_TOKEN` only; lead uses `LeadTokenStore`.
2. `LeadTokenStore` file implementation **after `U-API-1`**: outside the repository, user-only permissions (Guidance: `0600` on Unix; on Windows, rely on the user profile ACL). Until then ship only the interface and `tokentest` memory store.
3. On worker session end (normal exit, crash, cancel): `Issuer.Revoke(sessionID)` is called by the supervisor (`S3-T13`). Validator refuses revoked tokens; tool calls after revocation return `unauthorized`.
4. Clear `COWFLOW_SESSION_TOKEN` from the environment copy passed to any child process the MCP server itself spawns (none today; guard for the future).

**Tests** — lead resolution with memory store; worker without env → error;
revoked token → `unauthorized` on next call; a worker token from task A cannot
call `cowflow_get_context` for task B (§24.14 "another session's token").

**Acceptance**
- [ ] Lead token never read from env or flags; never written into any workspace.
- [ ] Revocation effective on the next call (no caching of valid scopes beyond one call).

**Pitfalls** — Caching the resolved scope for the whole process life and
skipping re-validation, so revocation never bites. Guidance: re-validate per
call; it is one indexed lookup.

---

### S3-T10 — `init` writes `.mcp.json`, `.gitignore`, lead token

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-CLI` | `AG-LEAD`, `AG-MCP`, `AG-SEC` | `S3-E3` | `S3-T08`, `S3-T09`, `U-API-1` | `feat/cli-init-mcp-json` | yes |

**Skills:** `cli-and-config`, `mcp-and-runners`, `security-review`   **Spec:** §5.4, §14 (`init … # generates .mcp.json and the lead token`), §15.2, §24.13
**Files:** `internal/cli/init.go`, `internal/cli/initfiles.go`, `internal/cli/testdata/mcp.json.golden`, tests

**Why / what** — Without `.mcp.json` the extension does not see COWFlow
(§1.3). `init` also issues the project-wide lead token and makes sure
never-commit paths are ignored.

**Steps**
1. Write exactly (§15.2):
   ```json
   {
     "mcpServers": {
       "cowflow": {
         "command": "cowflow",
         "args": ["mcp", "--role", "lead", "--project", "."],
         "env": {}
       }
     }
   }
   ```
2. If `.mcp.json` exists: parse, replace only `mcpServers.cowflow`, keep other servers and key order where possible (Guidance), write atomically (temp file + rename). Invalid existing JSON → exit `2` with a message; never overwrite blindly.
3. `.gitignore`: append missing entries `node_modules/`, `.cowflow/`, `*.log`, `.env*` (§24.13) plus build-artifact patterns the project declares (Guidance: none by default); no duplicates; idempotent.
4. Lead token: `Issuer.IssueLead` then `LeadTokenStore.Save` (`U-API-1`). Print only "lead token issued" — never the secret.
5. Re-running `init` must not silently replace a working lead token. Guidance: re-run is a no-op for the token. §14 defines no rotation flag — do not add one.
6. Tell the user (stderr) to commit `.mcp.json` and `.gitignore` through their normal branch/PR flow.

**Tests** — golden file byte-for-byte; merge with an existing file holding
another server; idempotent second run (no diff); `.gitignore` merge; secret
absent from stdout/stderr/`.mcp.json`.

**Acceptance**
- [ ] `.mcp.json` matches §15.2 exactly; `env` is `{}`.
- [ ] Manual: the Claude Code extension lists `cowflow` after reopening the folder (§20.29, evidence in `S3-T24`).

**Pitfalls** — Writing an absolute path or the token into `.mcp.json` (it is
committed). Windows: write with `\n` line endings consistently (the repo uses
`eol=lf`).

---

### S3-T11 — `init --conventions gitflow` and branch rules in `task create`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-CLI` | `AG-LEAD`, `AG-MCP` | `S3-E3` | `S1-E8`, `S0-E4`, `C2`, `C3` | `feat/cli-conventions-gitflow` | no |

**Skills:** `cli-and-config`, `git-changes-sync`, `gitflow` (do not confuse with §24)   **Spec:** §24.1, §24.2, §22 `[conventions]`, §20.31
**Files:** `internal/config/conventions.go`, `internal/cli/init.go`, `internal/cli/task.go`, tests

**Why / what** — §20.31: a branch outside the pattern is refused in
`task create`. The product's GitFlow (`main`, `develop`, `feature/…`) is
§24 — not this repository's GITFLOW.md.

**Steps**
1. `init --conventions gitflow` writes the `[conventions]` table (§22): `preset = "gitflow"`, `enforce_branch_names = true`, `enforce_protected_branches = true`, `enforce_commit_format = true`, `tag_format = "v{major}.{minor}.{patch}"`. It does **not** write `promotion_mode = "pr"` (`C3`).
2. `internal/config/conventions.go` — pure functions, no I/O:
   ```go
   type Kind string // feature|bugfix|chore|hotfix|release (§24.2)
   type BranchNamer interface{ Branch(kind Kind, slug string) (string, error) }
   func (p Preset) BaseFor(k Kind) string // feature/bugfix/chore/release → develop; hotfix → main
   func (p Preset) Protected() []string   // ["main", "develop"]
   ```
   Implement the §24.2 namer (`<kind>/<slug>`) and the §8.1 namer (`cowflow/<project>/<task-slug>`); which one applies outside `gitflow` waits for `C2`.
3. `task create --kind <kind>`: name = namer output; validate with `git check-ref-format --branch` (`S0-E4`); refuse a name not matching `^(feature|bugfix|chore|hotfix|release)/` under gitflow → exit `4`.
4. Base rule: `--base` different from `BaseFor(kind)` → exit `4` unless `--force`; with `--force`, write an `audit_log` row (§24.2).
5. Refuse creating a task whose branch would be `main` or `develop`.
6. The same validation runs for `cowflow_create_task` (it calls the same service — verify in `S3-T04`).

**Tests** — table: kinds × slugs (valid, uppercase, spaces, leading `-`, `..`)
→ expected branch or exit `4`; base mismatch with/without `--force` + audit row;
`init --conventions gitflow` writes the table and is idempotent.

**Acceptance**
- [ ] §20.31 "branch outside the pattern is refused in `task create`" covered by a test tagged `// AC#31`.
- [ ] `--force` overrides are audited.

**Pitfalls** — Building the branch with `fmt.Sprintf` and skipping
`check-ref-format`; applying the gitflow namer when `preset = "none"`.

---

### S3-T12 — Runner spec, argv builder, placeholders, prompt modes

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-MCP` | `AG-LEAD`, `AG-CLI`, `AG-SEC` | `S3-E4` | `S0-E2`, `C10` | `feat/runner-argv-builder` | yes |

**Skills:** `mcp-and-runners`, `security-review`, `go-conventions`   **Spec:** §15.3, §22 `[runner]`, §7, §17
**Files:** `internal/runner/spec.go`, `internal/runner/argv.go`, tests

**Why / what** — The single most security-sensitive line of the runner: turning
config + task text into a process command line. The prompt is attacker-
influenced (task descriptions, repo content), so it must only ever be **one
argv element** or stdin/file content.

**Steps**
1. `SpecFromConfig(cfg.Runner[name]) (Spec, error)`: `cmd` non-empty; `prompt_mode` ∈ `arg|stdin|file` (`message` → error citing `C10`); `provider` must have a `[scheduler.llm.<provider>]` table or fall back to `[scheduler.llm]`. If `S0-E2` lacks a key, ask `AG-CLI` (they own `internal/config`).
2. `Argv(spec, ph Placeholders) ([]string, io.Reader, error)`:
   ```go
   argv := slices.Clone(spec.Cmd)
   argv = append(argv, subst(spec.MCPConfigFlag, ph)...)
   argv = append(argv, subst(spec.PermissionFlag, ph)...)
   switch spec.PromptMode {
   case "arg":   argv = append(argv, ph.Prompt)          // one element, never split
   case "stdin": stdin = strings.NewReader(ph.Prompt)
   case "file":  // write prompt to <workspace>/.cowflow/prompt.md; substitute path
   }
   ```
   `subst` replaces `{mcp_config}`, `{workspace}`, `{prompt}`, `{task_id}` (and `{cmd}` for generic) **inside each element**; never joins, never splits on spaces.
3. Reject a placeholder in `cmd[0]` other than `{cmd}`; unknown `{…}` placeholder → error.
4. Resolve `cmd[0]` with `exec.LookPath`; on Windows let Go resolve `.exe`/`.cmd` shims (npm installs `claude.cmd`/`codex.cmd`). Guidance: `.cmd` shims are run by `cmd.exe` internally — Go ≥ 1.22 escapes arguments for batch files and refuses unsafe ones; keep Go ≥ 1.26 and add a test with a prompt containing `&|^%"`.

**Tests** — table for every `[runner.*]` in §15.3/§22 × each `prompt_mode`;
prompt with spaces, quotes, newlines, `$(…)`, `%PATH%`, `&& del` stays one
element; `FuzzArgv` asserting `len(argv)` is independent of prompt content.

**Acceptance**
- [ ] No code path builds a command string; `grep -rn '"sh", "-c"\|cmd.exe' internal/runner` empty.
- [ ] Spec defaults from §22 produce the expected argv in golden tests.

**Pitfalls** — `strings.Fields(prompt)`; substituting `{prompt}` into the
middle of a flag value like `--message={prompt}` (still one element — fine —
but document it).

---

### S3-T13 — Process supervision for runners

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-MCP` | `AG-LEAD`, `AG-CLI`, `AG-SEC` | `S3-E4` | `S3-T12`, `S0-E5`, `S3-T09` | `feat/runner-supervisor` | yes |

**Skills:** `mcp-and-runners`, `windows-platform`, `security-review`   **Spec:** §7, §11 (`session_timeout_seconds`), §15.4, §17, §18 (agent crashes)
**Files:** `internal/runner/supervisor.go`, `internal/runner/env.go`, tests

**Why / what** — Starts the agent CLI in its workspace, captures logs, enforces
the session timeout, kills the whole tree on cancel, and records how it ended.

**Steps**
1. `Start(ctx, inv, adapter)`: `adapter.Prepare` → `adapter.Argv` → spawn through the `S0-E5` process-tree API with `Dir = inv.Workspace` (confined path).
2. Environment (`env.go`): copy the host env; remove every `COWFLOW_*` and `GIT_DIR`, `GIT_WORK_TREE`, `GIT_INDEX_FILE`, `GIT_OBJECT_DIRECTORY` (Guidance; prevents leaking the host's repo/identity); add `COWFLOW_SESSION_TOKEN=<secret>`; add `GIT_TERMINAL_PROMPT=0`. Vendor keys (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`) pass through from the host env untouched — never written anywhere.
3. Logs: stdout and stderr to `<state>/logs/<task>/<session>.log` (location per `U-API-1`; Guidance: inside `.cowflow/` of the workspace until decided). Keep the last stdout bytes in memory (Guidance: 1 MiB cap) for `ParseUsage`.
4. Timeout: `session_timeout_seconds` via `context.WithTimeout`; on expiry → tree kill.
5. On exit: presence `completed`/`failed` per exit code; record usage via `UsageLedger` if `ParseUsage` succeeds; `Issuer.Revoke(session)`; release the provider gate (`S3-T22`). Always in a `defer`, including panics and cancellation.
6. Never log the env.

**Tests** — fake agent binary: success, non-zero exit, hang past timeout
(fake clock + real kill), spawn of grandchildren then cancel (Windows tree kill
test, `S0-E5` helper); env test proving `COWFLOW_SESSION_TOKEN` of the host is
replaced and `GIT_DIR` removed; revoke called on every path.

**Acceptance**
- [ ] Cancel kills grandchildren on Windows (§20.16 style check).
- [ ] Token revoked and gate released on every exit path.

**Pitfalls** — Waiting on the process before draining stdout pipes (deadlock
when output > pipe buffer); setting `cmd.Env = append(os.Environ(), …)` and
ending up with two `COWFLOW_SESSION_TOKEN` entries.

---

### S3-T14 — Generic runner and version checks

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-MCP` | `AG-LEAD`, `AG-CLI`, `AG-SEC` | `S3-E4` | `S3-T12`, `U-API-7` | `feat/runner-generic` | yes |

**Skills:** `mcp-and-runners`, `security-review`   **Spec:** §3.1 ("Generic runner + Claude Code adapter"), §15.3 `[runner.generic]`, §14 `runners list|check`, §23 risk "Vendor CLI changes flags"
**Files:** `internal/runner/generic.go`, `internal/runner/check.go`, tests

**Why / what** — The generic runner lets any headless agent participate; it is
also the base the vendor adapters embed. `Check` powers `cowflow runners check`.

**Steps**
1. `Generic` implements `Adapter` from the `Spec` alone: `Prepare` writes `.cowflow/mcp.json` (worker form, §15.2) and the protocol (`S3-T17`); since `[runner.generic]` has no `mcp_config_flag`/`permission_flag` (`U-API-7`), nothing is injected into argv.
2. `{cmd}` substitution: the generic `cmd = ["{cmd}"]` is replaced by an **argv array** supplied by config/task, never a string to split. Guidance: where that array comes from is not specified; until decided, the generic runner refuses to start if no array is configured.
3. `Check(ctx)`: run `cmd[0] --version` with a 10 s timeout (Guidance), parse the first semver-looking token; return `{name, path, version, ok, detail}`. Version compatibility tables per vendor live in the adapters (`S3-T18`/`S3-T19`).

**Tests** — fake binaries printing versions; missing binary → `ok=false` with
a fix hint; `{cmd}` array substitution table.

**Acceptance**
- [ ] Generic runner runs the fake agent end-to-end in a test.
- [ ] `Check` never hangs (timeout test).

**Pitfalls** — Accepting a free-form `cmd` string from a task description
(arbitrary command execution by whoever writes tasks).

---

### S3-T15 — CLI: `runners list|check`, `run`, `task create --runner`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-CLI` | `AG-LEAD`, `AG-MCP`, `AG-SEC` | `S3-E4` | `S3-T14`, `S3-T16` | `feat/cli-runners` | yes |

**Skills:** `cli-and-config`, `mcp-and-runners`   **Spec:** §14 (`run --max-agents [--runner]`, `runners list|check`, `task create --runner`), §15.3
**Files:** `internal/cli/runners.go`, `internal/cli/run.go`, `internal/cli/task.go`, tests

**Why / what** — The human-lead path (§5.4 "the lead can be a pure human: same
steps via CLI") and the diagnostic for vendor CLI drift.

**Steps**
1. `cowflow runners list [--json]`: every `[runner.<name>]` with provider, `cmd[0]`, `context_file`, and which one is `[runner].default`.
2. `cowflow runners check [--json]`: `Adapter.Check` for each; exit `0` when all used runners are ok, else `2` (Guidance).
   ```json
   [{"name":"claude-code","provider":"anthropic","path":"C:\\Users\\me\\AppData\\Roaming\\npm\\claude.cmd","version":"<x.y.z>","ok":true},
    {"name":"codex","provider":"openai","path":"","ok":false,"detail":"codex not found in PATH","fix":"npm install -g @openai/codex"}]
   ```
3. `task create --runner <name>`: unknown runner → exit `1`. Stored on the task; default `[runner].default`.
4. `cowflow run --max-agents N [--runner name]`: calls `RunService.Run` (`S3-T16`); prints started/queued sessions.

**Tests** — command-tree test includes the new flags; `--json` shape; exit
codes.

**Acceptance**
- [ ] `runners check` output is safe to paste in an issue (no env, no tokens).

**Pitfalls** — Printing the full PATH or env in `detail`.

---

### S3-T16 — Run dispatch (`cowflow_run` / `cowflow run`) and runner host

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-MCP` | `AG-LEAD`, `AG-CLI`, `AG-SCHED`, `AG-SEC` | `S3-E4` | `S3-T13`, `S3-T17`, `S3-T22`, `S1-E*`, `C15` | `feat/runner-run-dispatch` | yes |

**Skills:** `mcp-and-runners`, `scheduler-and-leases`, `cow-workspaces`   **Spec:** §5.3, §5.4 (typical flow), §11, §15.3, §24.7
**Files:** `internal/runner/run.go`, `internal/runner/host_*.go`, tests

**Why / what** — `RunService.Run` turns "run" into: for each runnable task,
ensure a workspace, open a session, issue a token, ask the provider gate, start
the adapter. Where the processes live is `C15`.

**Steps**
1. Select tasks in state ready (§5.4 flow starts `auth` and `api` together; `depends` does not delay start — the dependency is resolved by cross-request). Respect `--max-agents` and `[scheduler] max_agents`.
2. Per task: `workspace` provider ensures the workspace (`S1-E*`, sparse by intent paths once announced); create `agent_sessions` row (role worker, runner, provider); `IssueSession`; `ProviderGate.TryAdmit(provider)`; if not admitted → leave queued with the reason (this is what makes §20.28 observable); else `Supervisor.Start`.
3. Implement `Host` for the decided `C15` option only. Both options call the same `Run` logic; only the process lifetime differs.
4. Return `RunResult{Started: […], Queued: [{task, reason}]}` immediately.
5. When a session ends and the gate frees a slot, start the next queued task for that provider (Guidance: the host re-runs selection on every session exit).

**Tests** — fakes for workspace/issuer/gate/supervisor: two providers, openai
limit 1, three tasks (2 codex, 1 claude) → claude and one codex start, one codex
queued with reason; when the codex session ends, the queued one starts.

**Acceptance**
- [ ] `C15` decision recorded in the PR; no other host code merged.
- [ ] A saturated provider never blocks another provider's tasks (`// AC#28`).

**Pitfalls** — Holding a SQLite transaction while spawning a process; starting
a session before the gate admits it.

---

### S3-T17 — Protocol injection and worker MCP config

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-MCP` | `AG-LEAD`, `AG-COLLAB`, `AG-SEC` | `S3-E5` | `S3-T12`, `U-API-18`, `U-DATA-4` | `feat/runner-protocol-injection` | yes |

**Skills:** `mcp-and-runners`, `collaboration-protocol`, `security-review`   **Spec:** §9.3, §15.2, §15.5, §22 `inject_protocol`, §24.7, §24.9, §24.11, §24.13, §24.14, §24.16
**Files:** `internal/runner/protocol.go`, `internal/runner/protocol.md.tmpl` (embedded), `internal/runner/mcpconfig.go`, tests

**Why / what** — Each vendor reads its own context file. Injection gives every
model the same rules in the place it looks; enforcement (`S3-T20`) catches the
rest.

**Steps**
1. Template (`text/template`, embedded) with sections, in this order:
   - Identity: task slug, title, branch, base, runner — never the token.
   - Session protocol (§24.13): `cowflow_get_context` → read contracts → `cowflow_announce_intent` → (resources when available); publish each compilable milestone, ≥ every 45 min; end with `cowflow_report_result` answering each acceptance item, then `cowflow_report_status`.
   - Waiting options (§9.3): `blocked` + event, declared stub (`stub: true`), escalate (`cowflow_escalate`).
   - Cross-requests (§24.11): `expected_output` required, `accept` with `eta_minutes`, `reject` with reason, `deliver` with `change_id`.
   - Boundaries (§24.14) verbatim list.
   - Acceptance criteria (§24.7) and contracts of the task.
   - Resource section (§9.8, `cowflow exec --resource`) only when `LeaseService` is available (not this sprint).
2. Write `<workspace>/<context_file>` only when `[runner] inject_protocol = true`. If the path is **untracked**: write it and add it to the exclude file found with `git -C <ws> rev-parse --git-path info/exclude`. If **tracked** (`git ls-files --error-unmatch`): refuse to start the session with a clear error citing `U-API-18`.
3. Write `<workspace>/.cowflow/mcp.json`:
   ```json
   {"mcpServers":{"cowflow":{"command":"cowflow",
     "args":["mcp","--role","worker","--project","<repo root>"],
     "env":{"COWFLOW_SESSION_TOKEN":"<secret>"}}}}
   ```
   `--project` value: Guidance pending `U-API-1` (where state is found). File mode user-only (Guidance). `.cowflow/` is git-ignored (`S3-T10`).
4. Register both generated paths in `PublishCheck.Generated` so publish refuses them (`S3-T20`).

**Tests** — golden render for the fixture task; untracked vs tracked
context file; exclude entry added once; `mcp.json` contains the token and the
lead token is never present (§security-review test "lead token not present in
worker `.cowflow/mcp.json`").

**Acceptance**
- [ ] Protocol text uses the exact tool names; a test greps the rendered text for every worker tool referenced.
- [ ] No generated file is ever committed by the fixture e2e (`S3-T23` asserts).

**Pitfalls** — Writing the protocol into the *main repo* instead of the
workspace; forgetting that the context file must stay unchanged by the worker
(§24.14).

---

### S3-T18 — Claude Code runner adapter

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-MCP` | `AG-LEAD`, `AG-CLI`, `AG-SEC` | `S3-E5` | `S3-T14`, `S3-T17`, `U-SCHED-4` | `feat/runner-claude-code` | yes |

**Skills:** `mcp-and-runners`, `security-review`   **Spec:** §15.3 `[runner.claude-code]`, §15.4, §23 (settled decision 5: Claude Code as reference)
**Files:** `internal/runner/claudecode.go`, tests, `test/fixtures/runners/claude/*.json` (recorded outputs, scrubbed)

**Why / what** — The reference adapter (§23 decision 5). Argv from §22:
`claude -p --mcp-config <ws>/.cowflow/mcp.json --dangerously-skip-permissions "<prompt>"`.

**Steps**
1. Embed `Generic`; `Name() = "claude-code"`; `{mcp_config}` = `inv.MCPConfigPath`.
2. `Check`: `claude --version`; verify `claude -p --help` mentions `--mcp-config` and `--dangerously-skip-permissions`; missing → `ok=false`, "incompatible version" (§23).
3. Usage (`U-SCHED-4`): Guidance — Claude Code can print a final JSON result with usage when an output-format flag is set; that flag is **not** in the §22 `cmd`. Do not add it to defaults. `ParseUsage` tries to parse the last JSON object in stdout and returns `false` otherwise; a project may add the flag to its own `[runner.claude-code] cmd` after `U-SCHED-4` is decided.
4. Prompt (`{prompt}`): "You are the COWFlow worker for task `<slug>`: `<title>`. Follow `CLAUDE.md` in this directory. Use only the `cowflow_*` tools for coordination." (Guidance wording).
5. Record a real run's stdout (with keys/paths scrubbed) as a fixture for `ParseUsage` tests.

**Tests** — argv golden; `Check` against fake `claude` binaries (good/bad help
text); `ParseUsage` on recorded fixture and on garbage.

**Acceptance**
- [ ] Manual smoke: `cowflow run` with only the `auth` task makes Claude call `cowflow_get_context` and `cowflow_announce_intent` (log excerpt in PR).

**Pitfalls** — Committing a recorded fixture that contains an API key, a
session ID or a local username. Scrub, then grep for `sk-`.

---

### S3-T19 — Codex runner adapter

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-MCP` | `AG-LEAD`, `AG-CLI`, `AG-SEC` | `S3-E5` | `S3-T14`, `S3-T17`, `U-API-7`, `U-SCHED-4` | `feat/runner-codex` | yes |

**Skills:** `mcp-and-runners`, `security-review`   **Spec:** §15.3 `[runner.codex]`, §15.4, §20.28, §20.29
**Files:** `internal/runner/codex.go`, tests, `test/fixtures/runners/codex/*`

**Why / what** — The second vendor that makes the team "mixed". Spec argv:
`codex exec --config mcp_servers.cowflow.command=cowflow --full-auto "<prompt>"`.
**Known gap `U-API-7`:** that flag starts bare `cowflow` with no `mcp --role worker`
and says nothing about the token. Codex must still reach `cowflow mcp --role
worker` with `COWFLOW_SESSION_TOKEN`.

**Steps**
1. Embed `Generic`; `Name() = "codex"`; `context_file = AGENTS.md`.
2. Implement argv exactly from config — the adapter adds nothing the config does not say. The maintainer's `U-API-7` decision is expected to be expressed in `[runner.codex] mcp_config_flag`; if it needs code (e.g. writing a Codex config file in the workspace), that lands as a follow-up PR after the decision.
3. Verify with the installed Codex version and document in the PR: how Codex passes environment to MCP servers it spawns (whether `COWFLOW_SESSION_TOKEN` from the parent env reaches `cowflow mcp`), and whether Codex reads a project `.mcp.json` at all (§20.29). Report findings to `AG-LEAD`; do not decide.
4. `Check`: `codex --version`; `codex exec --help` lists `--config` and `--full-auto`.
5. Usage (`U-SCHED-4`): same approach as `S3-T18` (`ParseUsage` best effort, `false` when unknown).

**Tests** — argv golden for the §22 config and for a candidate `U-API-7` config
given as test data; fake `codex` binary for `Check`.

**Acceptance**
- [ ] With the `U-API-7` decision applied in config, a manual smoke shows Codex calling `cowflow_get_context` as a worker (log excerpt in PR).
- [ ] Findings on env propagation and `.mcp.json` discovery attached to the PR.

**Pitfalls** — Hard-coding a Codex config file format from memory; vendor
behaviour changes between versions — record the version you tested.

---

### S3-T20 — Server-side enforcement: boundaries, protected branches, never-commit

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-COLLAB` | `AG-LEAD`, `AG-SCHED`, `AG-SEC` | `S3-E6` | `S2-E5`, `S2-E9`, `S3-T11`, `C8`, `C12`, `U-GIT-10` | `feat/collab-publish-enforcement` | yes |

**Skills:** `collaboration-protocol`, `security-review`, `git-changes-sync`   **Spec:** §2.2 principle 3, §15.4, §15.5, §17, §24.2, §24.8, §24.13, §24.14, §20.4, §20.31
**Files:** `internal/collab/enforce.go`, `internal/collab/boundaries.go`, tests; one-line call site in the publish path (`internal/git`, request to `AG-GIT`)

**Why / what** — Headless agents run with full permissions; the publish path is
the gate between "what an agent did in its sandbox" and "what the team sees".
Every rule here works for MCP, CLI and the future API because it runs inside
the service.

**Steps**
1. Implement `Enforcer.CheckPublish` (§6.6). Rules (each a `Diagnostic` with a stable `Rule`):
   - `intent.required` — no intent → refuse (already `S2-E3`; keep one implementation, call it).
   - `protected_branch` — `HeadBranch` ∈ `Preset.Protected()` (`main`, `develop`) → refuse (§24.2, `// AC#31`).
   - `boundary.cowflow_dir`, `boundary.mcp_json`, `boundary.config_toml`, `boundary.generated_context` — manifest touches `.cowflow/`, `.mcp.json`, `config.toml`, or any path in `Generated` → refuse (§24.14).
   - `boundary.foreign_contract` — manifest touches `contracts/*` owned by another task without the required `confirm_contract` request → refuse; the "open vs accepted" predicate is injected (`C8`).
   - `never_commit` — `node_modules/`, `.cowflow/`, `*.log`, `.env*` (§24.13) and `[security] blocked_patterns` (§22) → refuse; how the two lists combine waits for `C12` (check both meanwhile).
   - `deps.undeclared` — root `go.mod`/`package.json` changed without `touches_deps: true` → **warn** to lead (§24.14).
   - `scope.exceeded` — files outside intent paths → **warn** (§24.9).
2. Match paths on the **manifest** (§8.2), including rename targets, case-insensitively on Windows.
3. Call site: the publish service calls `CheckPublish` after computing the manifest and **before** creating the retention ref; on refuse, emit `invariant.violated`-style diagnostics per existing `S2-E9` conventions and return `ErrRefusedByPolicy` (exit `4`).
4. Refs (`U-GIT-10`): `git push`, `git tag`, branch deletion, `refs/cowflow/*` changes are not detectable at publish. Leave a `// TODO(U-GIT-10)` and an issue; do not invent a mechanism.

**Tests** — table test per rule (refuse/warn); renamed `.env` → refused;
`Config.TOML` on Windows → refused; publish from `develop` HEAD in a harness repo
→ exit `4` (`// AC#31`); same results via CLI `change publish` and MCP
`cowflow_publish_change` (parity).

**Acceptance**
- [ ] Every rule has a stable `Rule` string used in diagnostics.
- [ ] Refusal happens before any ref is written (assert refs unchanged).
- [ ] CLI and MCP produce identical diagnostics.

**Pitfalls** — Checking the working tree instead of the manifest; matching
`.envoy/config` as `.env*` (match on path segments, not substrings).

---

### S3-T21 — Authorization on every tool and front-end parity

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-MCP` | `AG-LEAD`, `AG-COLLAB`, `AG-SEC` | `S3-E6` | `S3-T04`, `S3-T05`, `S3-T08`, `S3-T20` | `test/mcp-authz-parity` | yes |

**Skills:** `security-review`, `mcp-and-runners`, `testing-harness`   **Spec:** §15, §15.5, §17, §24.14
**Files:** `internal/mcp/authz.go`, `internal/mcp/authz_test.go`, `test/integration/parity_test.go`

**Why / what** — Proves the enforcement story end to end: identity is from the
token only, each tool authorizes against the scope, and CLI/MCP cannot diverge.

**Steps**
1. Wrap every handler with `withAuthz(action, targetFn)` that calls `token.Authorize` before the service.
2. A test enumerates `ToolsFor(role)` for each role and fails if a handler is registered without `withAuthz`.
3. Cross-task tests: worker A calls `cowflow_deliver_request` / `cowflow_reply_request` on a request addressed to B → `unauthorized`; crafted args carrying `workspace_id` or `task_id` of B (e.g. on `cowflow_report_status`) → `usage` (rejected by the schema, `S3-T02`); `cowflow_request_sync` always targets A's own workspace.
4. Watchdog scope calling publish (if registered by `U-API-9`) → `unauthorized`.
5. Parity: for publish without intent, publish with `.mcp.json`, publish from `develop`, run the CLI with `--json` and the MCP tool on the same harness repo; compare `code`/exit class and diagnostics.

**Tests** — as above, `-race`, 20 concurrent sessions hammering
`cowflow_get_context` with mixed tokens (no cross-leak).

**Acceptance**
- [ ] Zero tools without authorization wrapper (test-enforced).
- [ ] Parity test green on 3 OSes.

**Pitfalls** — Authorizing on IDs from tool arguments rather than on the
target looked up in the store.

---

### S3-T22 — Per-provider gate and usage ledger

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-SCHED` | `AG-LEAD`, `AG-COLLAB` | `S3-E7` | `S3-T07`, `S0-E2`, `C11`, `U-SCHED-4`, `U-SCHED-8` | `feat/scheduler-provider-budget` | no |

**Skills:** `scheduler-and-leases`, `store-and-events`, `testing-harness`   **Spec:** §5.4 (practical limits), §11, §15.3, §21, §24.15, §20.28
**Files:** `internal/scheduler/provider.go`, `internal/scheduler/budget.go`, `internal/scheduler/schedtest/`, tests

**Why / what** — Two pieces Sprint 4 builds on: (1) a gate that admits sessions
per provider so a saturated vendor does not stop the others; (2) a ledger of
usage per provider/task/session. Enforcement of 80%/100% is `S4-E7`.

**Steps**
1. Read `[scheduler.llm]` and each `[scheduler.llm.<provider>]` from config (values only from the user's file; no hard-coded provider defaults — `C11`). A runner's `provider` without its own table uses `[scheduler.llm]`.
2. `ProviderGate` daemonless: the count lives in SQLite (`agent_sessions` with `provider` and a running state), computed in the same transaction that marks a session running, so two hosts cannot over-admit. What one admission means is `U-SCHED-8` (Guidance: one running session = one `max_concurrent_requests` slot).
   ```go
   func (g *gate) TryAdmit(ctx context.Context, provider, sess string) (Admission, error) {
       return g.store.Tx(ctx, func(tx store.Tx) (Admission, error) {
           n, err := tx.CountRunningSessions(provider)
           if err != nil { return Admission{}, err }
           if lim := g.limit(provider); lim > 0 && n >= lim {
               return Admission{Reason: fmt.Sprintf("provider %s at max_concurrent_requests=%d", provider, lim)}, nil
           }
           return Admission{Admitted: true}, tx.MarkSessionRunning(sess)
       })
   }
   ```
3. `UsageLedger.Record` (idempotent by `op_id`) and `Totals` by provider/task/session over a window (hour, day). Rows tagged with `Source` (`U-SCHED-4`).
4. Expose totals for observability (§21 "tokens and cost per session/task/day"); the `cowflow budget` command is Sprint 4.
5. Do **not** pause sessions or emit `budget.exceeded` this sprint (`S4-E7`).

**Tests** — fake clock windows; concurrent `TryAdmit` from 20 goroutines with
limit 3 → exactly 3 admitted (`-race`); two providers independent (`// AC#28`);
ledger idempotency.

**Acceptance**
- [ ] Limit 1 on `openai` never blocks `anthropic` admissions.
- [ ] No over-admission under concurrency.

**Pitfalls** — Counting in memory (two `cowflow run` processes would each think
they have the full budget); treating `max_concurrent_requests = 0` as "zero
allowed" — absent/zero semantics are unspecified (`U-SCHED-12`); keep "no limit" only when the
key is absent and ask `AG-LEAD` about `0`.

---

### S3-T23 — Hermetic end-to-end with fake agents

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD`, `AG-SEC` | `S3-E8` | all of `S3-T01`…`S3-T22` (fakes until then) | `test/e2e-mcp-fake-agents` | no |

**Skills:** `testing-harness`, `mcp-and-runners`   **Spec:** §5.4 flow, §18.1, §20.18, §20.27, §20.28, §20.31
**Files:** `test/fixtures/e2e-auth-api/` (repo template), `test/integration/e2e_mcp_test.go`, `test/harness/fakeagent/main.go`

**Why / what** — Real LLMs are nondeterministic and cost money; CI needs a
deterministic proof that the plumbing works. A scripted fake agent drives the
real `cowflow mcp` server over real stdio, exactly like Claude/Codex would.

**Steps**
1. Fixture repo (built by the test, not committed as a nested `.git`): Go module with `src/auth` (empty `validateJWT` stub), `src/api` (orders handler needing `validateJWT`), tests that fail until implemented, `contracts/auth-api.md` with `Status: agreed` and `Owner: task auth`, `.gitignore` per `S3-T10`, branches `main` and `develop`.
2. `fakeagent`: reads the MCP config path from argv (like `--mcp-config`), starts `cowflow mcp` from it, and executes a script file (`FAKEAGENT_SCRIPT`) of tool calls with simple variables, e.g.:
   ```json
   [{"tool":"cowflow_get_context"},
    {"tool":"cowflow_announce_intent","args":{"goal":"orders endpoint","paths":["src/api"],"contracts":["contracts/auth-api.md"]}},
    {"tool":"cowflow_send_request","args":{"to":"auth","kind":"expose_interface","title":"Expose validateJWT","expected_output":"commit"},"save":"req"},
    {"tool":"cowflow_report_status","args":{"state":"blocked","reason":"waiting for validateJWT"}},
    {"wait_event":"<delivered event constant>","request":"$req"},
    {"tool":"cowflow_request_sync","args":{"change_id":"$delivered.change_id"}},
    {"shell":["git","apply","api.patch"]},
    {"shell":["git","commit","-am","feat(api): add orders endpoint"]},
    {"tool":"cowflow_publish_change"},
    {"tool":"cowflow_report_result","args":{"acceptance":[{"item":"go test ./src/api/... passes","ok":true}]}}]
   ```
   Note: the worker's tool for waiting is the event mechanism available to workers; if none exists for workers this sprint, the fake polls `cowflow_get_context` (record as finding for `AG-LEAD`).
3. Configure `[runner.fake-claude]` and `[runner.fake-codex]` with `provider = "anthropic"`/`"openai"` and `cmd` pointing at the built fake.
4. Lead side: an in-test MCP client with the lead token calls `cowflow_create_task` ×2, `cowflow_run`, `cowflow_wait_events` until both `report_result`, then `cowflow_create_integration` (if `C16` landed).
5. Assertions: request lifecycle `open → accepted → delivered → closed`; two changes published; `api` workspace contains auth's change via merge; fixture tests pass in the `api` workspace; `develop` and `main` unchanged; no generated file in any manifest.
6. Extra cases: `TestE2E_ProviderLimit` (limit 1 on openai, two fake-codex tasks + one fake-claude) and `TestE2E_GitflowRefusals` (bad branch name; publish from `develop`) — `// AC#28`, `// AC#31`.

**Tests** — this task *is* the tests; runs in normal `go test -race ./test/...`
on all 3 OSes; target < 60 s.

**Acceptance**
- [ ] Deterministic: 20 consecutive runs green locally (`-count=20`).
- [ ] No network, no real LLM, no global git config (harness env).

**Pitfalls** — Fake agent writing to stdout of the MCP pipe; Windows `.exe`
suffix for the fake binary path; leaving workspaces behind on failure (use
`t.Cleanup`).

---

### S3-T24 — Real-agent end-to-end, runbook and evidence

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` + `AG-LEAD` | `AG-LEAD`, `AG-SEC` | `S3-E8` | `S3-T23`, `S3-T18`, `S3-T19` with `U-API-7` resolved, `C16` | `test/e2e-real-agents` | no |

**Skills:** `testing-harness`, `mcp-and-runners`, `security-review`   **Spec:** §19 Phase 3, §20.18, §20.27, §20.28, §20.29, §20.31, §15.4
**Files:** `test/e2e/real/README.md` (runbook), `test/e2e/real/run_test.go` (build tag `e2e_real`, opt-in), `CHANGELOG.md`

**Why / what** — The test that decides the product: real Claude Code and real
Codex, real dependency, no human editing code. It cannot run in normal CI
(cost, nondeterminism, credentials); it runs on demand and its evidence closes
the sprint.

**Steps**
1. Runbook = §8 of this document, kept in sync.
2. `run_test.go` (`//go:build e2e_real`) automates §8 steps 3, 6–9 with the lead side driven by an MCP client (for repeatability); the VS Code session (§20.27) is done manually once and recorded. Skips unless `COWFLOW_E2E_REAL=1` and both vendor credentials are present in env.
3. Cost guard: Guidance — run once per candidate build; cap `session_timeout_seconds` to 1800 for the fixture; set vendor-console spend limits beforehand.
4. Collect evidence into the sprint-closing PR: CLI versions (`claude --version`, `codex --version`), `cowflow runners check --json`, `cowflow events --json` dump, `git log --all --oneline --graph`, worker logs (scrubbed), screenshot/recording of the extension tool calls, time and approximate cost per worker.
5. Retry policy for flakiness: up to 3 attempts per candidate; record every attempt's outcome (failures are data — §10).
6. Update `CHANGELOG.md` `[Unreleased]`.

**Tests** — `go test -tags e2e_real ./test/e2e/real/... -run TestRealMixedTeam -timeout 60m` with env set.

**Acceptance**
- [ ] At least one fully green run with both vendors; evidence attached.
- [ ] Logs scrubbed: `grep -E "sk-|cfs_|cfl_"` over the evidence is empty.

**Pitfalls** — Running against your real project: use the fixture only — the
agents run with `--dangerously-skip-permissions`/`--full-auto` (§15.4). Pasting
API keys into the runbook or shell history files that get committed.

---

## 8. Demo script

Run on a clean Windows 11 machine with Dev Drive, by `AG-QA` with `AG-LEAD`
watching. PowerShell. Expected outputs are abbreviated.

**0. One-time setup (per machine; never commit any of this)**

```powershell
npm install -g @anthropic-ai/claude-code @openai/codex
claude --version; codex --version
# Credentials: session env vars or the vendors' own login flows.
$env:ANTHROPIC_API_KEY = "<paste>"      # current shell only
$env:OPENAI_API_KEY    = "<paste>"
code --install-extension anthropic.claude-code   # or install "Claude Code" from the Marketplace
go install ./cmd/cowflow                         # from the COWFlow checkout at the sprint tag candidate
```

**1. Create the fixture project**

```powershell
# Dev Drive: if X: does not exist yet, provision it first with the S0-E6 runbook (`cowflow init --devdrive 50GB`).
Copy-Item -Recurse test\fixtures\e2e-auth-api X:\demo\shop   # Guidance: fixture exported by S3-T23
cd X:\demo\shop
git init -b main; git add .; git commit -S -s -m "chore: initial fixture"; git switch -c develop
cowflow init --conventions gitflow
Get-Content .mcp.json                  # expect §15.2 content, "env": {}
git status --short                     # expect no changes (fixture already has .gitignore and .mcp.json)
cowflow runners check --json           # expect claude-code ok, codex ok
```

**2. Configure runners** — `config.toml` (location per `U-API-1`) contains the
`[runner.claude-code]` and `[runner.codex]` blocks from §22 (Codex block with the
`U-API-7` decision applied) and:

```toml
[scheduler.llm.anthropic]
max_concurrent_requests = 5
[scheduler.llm.openai]
max_concurrent_requests = 6
```

**3. Open VS Code** — `code .`. The extension asks to approve the project MCP
server `cowflow`: approve. In the Claude Code panel run `/mcp`: expect `cowflow`
connected with the lead tools (`cowflow_create_task`, `cowflow_run`,
`cowflow_wait_events`, …).

**4. Observe** — second terminal: `cowflow events --follow`; third:
`cowflow status --watch` (whatever `S2-E10` provides daemonless).

**5. Lead prompt** (paste in the extension):

> You are the COWFlow lead. Use only `cowflow_*` MCP tools; do not run shell
> commands and do not edit files. Create task `auth` (kind feature, contract
> `contracts/auth-api.md`, paths `src/auth`, runner `claude-code`, acceptance
> "go test ./src/auth/... passes"). Create task `api` (kind feature, depends
> `auth`, contract `contracts/auth-api.md`, paths `src/api`, runner `codex`,
> acceptance "go test ./src/api/... passes", "POST /orders returns 201"). Then
> call `cowflow_run` with max_agents 2, and follow with `cowflow_wait_events`
> (timeout 300) until both tasks report results. Report what happened.

Expected tool results (abbreviated):

```json
{"task":{"slug":"auth","branch":"feature/auth","base":"develop","runner":"claude-code"}}
{"task":{"slug":"api","branch":"feature/api","base":"develop","runner":"codex","depends":["auth"]}}
{"started":[{"task":"auth","provider":"anthropic"},{"task":"api","provider":"openai"}],"queued":[]}
{"events":[…],"cursor":"…","timed_out":false}
```

**6. Watch the collaboration** — in the events terminal, expect in order (event
names per `S2-E*` constants): presence `starting`/`working` for both;
`intent.created` ×2; a cross-request from `api` to `auth` (`expose_interface`,
`expected_output = commit`); `api` presence `blocked`; `auth` accepts;
`change.published` (auth); request delivered with the change; `sync.*` into
`api`; `change.published` (api); request closed; both presence `completed`.

**7. Verify results**

```powershell
cowflow request list --json            # one request, state closed, linked change
cowflow change list --json             # two published changes
git log --all --oneline --graph        # develop and main unchanged; commits only on feature/auth, feature/api, refs/cowflow/changes/*
cowflow open api                       # inspect; then in that workspace:
go test ./...                          # expect PASS
```

No human edited any file under `src/` (the VS Code window only ran the lead).

**8. Integration (§20.27, requires `C16`)** — ask the lead: "Create an
integration group with auth and api targeting develop." Expect
`cowflow_create_integration` result with a group ID. Asking it to promote returns
`pending_human_approval` (no ref moves).

**9. `.mcp.json` recognition (§20.29)** — evidence from step 3 for the
extension; Codex per the `U-API-7` findings in `S3-T19`.

**10. Per-provider limit (§20.28)** — set `[scheduler.llm.openai]
max_concurrent_requests = 1`; ask the lead to create `api-docs` (runner `codex`,
paths `docs/api`) and `auth-tests` (runner `claude-code`, paths `src/auth`),
plus a second codex task `api-errors`, then `cowflow_run`. Expect `queued`
containing one codex task with reason `provider openai at
max_concurrent_requests=1` while the claude task starts. Cancel afterwards with
`cowflow task pause`.

**11. GitFlow refusals (§20.31)**

```powershell
cowflow task create Bad_Name --kind feature --title x ; $LASTEXITCODE   # expect 4
cowflow task create fix1 --kind feature --base main --title x ; $LASTEXITCODE  # expect 4 (no --force)
git -C <auth workspace> switch --ignore-other-worktrees develop
cowflow change publish --task auth --json ; $LASTEXITCODE              # expect 4, rule "protected_branch"
```

**12. Collect evidence** per `S3-T24` step 4.

---

## 9. Definition of Done

- [ ] Every task's Acceptance boxes ticked; each PR reviewed by `AG-LEAD`, the pod peer, and `AG-SEC` where `Security: yes`.
- [ ] Exit criteria in §2 verified with evidence attached to the sprint-closing PR.
- [ ] `go build ./...`, `go test -race -cover ./...`, `golangci-lint run`, `go vet ./...`, `govulncheck ./...` green; `go mod tidy` no-op; CI green on Windows, Linux, macOS; `CGO_ENABLED=0`.
- [ ] The MCP SDK is imported by exactly one file.
- [ ] No tool accepts `workspace_id`/`session_id`; all enforcement lives in services (parity test green).
- [ ] No secret (API key, session or lead token) in any committed file, log fixture, event or error; evidence grep clean.
- [ ] Every gap in §3.3 is either decided (recorded in SCOPE.md §23 and removed from SPEC-GAPS.md in the same PR) or still open and visibly blocking; the gaps found while planning (`C15`–`C17`, `U-API-18`, `U-SCHED-8`, `U-GIT-10`) are likewise decided or still open.
- [ ] `CHANGELOG.md` `[Unreleased]` lists MCP server, `.mcp.json` generation, runners, enforcement, provider gate.
- [ ] Commits Conventional, signed and signed off (`git commit -S -s`), no AI attribution; branches `<type>/<kebab-slug>` from `develop`.
- [ ] Learnings moved into `docs/skill/mcp-and-runners/SKILL.md` (vendor CLI versions tested, flags verified).

---

## 10. Risks and mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| LLM nondeterminism: the real run fails for reasons unrelated to COWFlow (model forgets `announce_intent`, loops, misreads the contract) | Exit criterion looks red while the plumbing is fine | Hermetic fake-agent e2e (`S3-T23`) proves the plumbing in CI; real run allows 3 attempts and records all; server refusals carry diagnostics the model can act on; protocol injection + fixture with clear acceptance criteria |
| Cost of real runs (two vendors, retries) | Budget overrun | Vendor-console spend caps; fixture is tiny; `session_timeout_seconds` capped for the demo; usage ledger (`S3-T22`) reports cost per run; real e2e opt-in only |
| Vendor CLI changes flags/behaviour (Claude Code, Codex) | Adapters break between runs | Declarative `[runner.<name>]` (§23); `runners check` verifies help text; versions recorded in evidence; adapters add nothing beyond config |
| MCP SDK decision late or SDK API churn (SCOPE.md §23 Open question 1) | Critical path slips | Day-1 decision; `ToolServer` adapter + conformance suite; everything else uses the fake |
| Codex cannot receive role/token with the spec's flag (`U-API-7`) | §20.28/§20.29 Codex half blocked | Raised day 1; adapter is config-driven so the fix is config; findings documented in `S3-T19` |
| Runner host undecided (`C15`) | No way to start workers | Day-1 decision; `Host` interface keeps either option to one small file |
| Integration group not available (`C16`) | §20.27 incomplete | Day-1 decision: minimal slice from `AG-INT` or re-map §20.27 to Sprint 5 |
| Headless workers with full permissions damage the host or leak secrets (§15.4) | Security incident | Demo only on the fixture; env scrubbing (`S3-T13`); publish enforcement (`S3-T20`); container provider recommended for third-party code; keys in env only |
| Token leak via logs, recorded fixtures or `.cowflow/mcp.json` | Another process acts as a worker | `Secret` redaction, hash-only storage, revocation at session end, `.cowflow/` ignored, evidence grep |
| Account rate limits throttle both workers | Slow or failed demo | Per-provider gate; API keys instead of shared subscription logins; small fixture |
| Protocol file collides with a tracked `CLAUDE.md`/`AGENTS.md` (`U-API-18`) | Worker dirties a tracked file and publish is refused | Fixture has none tracked; tracked case refused with a clear error until decided |
| Windows specifics: npm `.cmd` shims, long paths, process trees | Spawn failures or orphans | Go batch-argument escaping test (`S3-T12`); `S0-E5` tree kill; long-path handling from Sprint 0 |
