# Sprint plan

COWFlow v1 is delivered in **7 sprints (0–6)**, one per phase of
[SCOPE.md §19](../SCOPE.md), followed by **Sprint 7 (post-v1)** for the web
dashboard. Each sprint has its own document with goals,
concepts, tasks, and step-by-step instructions written so a junior developer
(or an AI agent) can pick up a task and finish it.

| Sprint | Phase | Length | Document | Exit criterion (§19 "Output") |
|---|---|---|---|---|
| 0 | Foundation | 2 wk | [sprint-0.md](sprint-0.md) | `init --devdrive` creates and mounts a Dev Drive; `doctor` reports ReFS/COW correctly |
| 1 | COW workspaces | 2 wk | [sprint-1.md](sprint-1.md) | 20 COW workspaces in < 2 s each; isolated editing verified; incremental disk measured |
| 2 | Collaboration plane (daemonless) | 2 wk | [sprint-2.md](sprint-2.md) | intent → request → publish → sync cycle in two terminals without a daemon; `who --path` via the index |
| 3 | MCP + runners | 2 wk | [sprint-3.md](sprint-3.md) | lead in VS Code creates two tasks; one Claude and one Codex worker finish a dependent feature with no human editing code |
| 4 | Daemon, scheduler, SSE | 2 wk | [sprint-4.md](sprint-4.md) | 20 simulated + 5 real sessions under limits; watchdog opens a request on an anomaly |
| 5 | Integration | 2 wk | [sprint-5.md](sprint-5.md) | two features validated and promoted with evidence; automatic revert in auto mode |
| 6 | ProjFS + hardening | 3 wk | [sprint-6.md](sprint-6.md) | functional `projfs` workspace with metrics; every failure in §18 covered by tests |
| 7 | **Post-v1:** web dashboard | 2 wk | [sprint-7.md](sprint-7.md) | `cowflow dashboard` opens a read-only live view of agents, tasks, intents, requests, leases, budgets, and integrations, updated over SSE |

Sprints 0–6 are v1. Sprint 7 starts only after v1 is released; it does not
change v1 scope (SCOPE.md §3.2 and §23 "Settled decisions" #13).

Before starting any task, read [AGENTS.md](../../AGENTS.md), the skills the
task lists ([docs/skill/](../skill/README.md)), and the SCOPE sections it cites.
[docs/SPEC-GAPS.md](../SPEC-GAPS.md) lists undecided points; a task blocked by
one says so and must wait for the maintainer's decision.

---

## 1. Agent roster

Work is split among **agent roles**. A role is a stable identifier for a set
of closely related functions; it can be filled by a human developer or an AI
agent. Roles are grouped into **pods** by how close their functions are —
roles in the same pod share types and change together often, so they sit
next to each other in the plan and review each other's work first.

| Pod | Agent ID | Role | Owns (packages / files) | Primary skills |
|---|---|---|---|---|
| **Command** | `AG-LEAD` | Lead / orchestrator | sprint planning, contracts between agents, reviews, merges, decisions on SPEC-GAPS (with the maintainer) | `gitflow`, all |
| **Interfaces** | `AG-CLI` | CLI & config | `cmd/cowflow`, `internal/cli`, `internal/config`, `config.example.toml` | `cli-and-config` |
| | `AG-API` | Daemon & local API | `internal/daemon`, `internal/api` | `local-api-daemon` |
| | `AG-MCP` | MCP & runners | `internal/mcp`, `internal/runner` | `mcp-and-runners` |
| | `AG-WEB` | Web dashboard (post-v1) | `internal/web` (embedded static UI: HTML, CSS, JavaScript) | `local-api-daemon`, `security-review` |
| **Data** | `AG-STORE` | Store, events & domain | `internal/store`, `internal/domain` | `store-and-events` |
| | `AG-GIT` | Git, changes & sync | `internal/git` | `git-changes-sync` |
| **Runtime** | `AG-PLAT` | OS platform layer | `internal/platform`, OS-specific clone/ProjFS primitives (`*_windows.go`, `*_linux.go`, `*_darwin.go`) | `windows-platform` |
| | `AG-WS` | Workspaces | `internal/workspace` (providers, golden trees, sparse, cache) | `cow-workspaces` |
| **Coordination** | `AG-COLLAB` | Collaboration protocol | `internal/collab` (presence, intents, requests, contracts, invariants, watchdog) | `collaboration-protocol` |
| | `AG-SCHED` | Scheduler & leases | `internal/scheduler` (limits, budgets, resource leases) | `scheduler-and-leases` |
| | `AG-INT` | Integration engine | `internal/integration` (groups, gates, promotion, revert) | `integration-engine` |
| **Assurance** | `AG-QA` | Quality & harness | `test/` (harness, integration, fixtures), CI workflows, benchmarks | `testing-harness` |
| | `AG-SEC` | Security reviewer | owns no package; mandatory reviewer for tokens, paths, processes, git calls, API | `security-review` |

Every role also follows `go-conventions` and `gitflow`.

### Ownership rules

1. **One owner per package.** Only the owning role changes a package's code.
   Need a change in someone else's package? Ask the owner (an issue or a
   comment on their task) — the same "cross-request" idea COWFlow itself
   implements.
2. **Interfaces are agreed first.** When two roles meet at an interface
   (e.g. `AG-WS` needs `AG-GIT`'s `Worktree` functions), the sprint document
   defines that interface in its *Contracts* section. Both sides code against
   it; the consumer may use a fake until the real one lands.
3. **Reviews:** every PR is reviewed by `AG-LEAD`; also by the other role in
   the same pod when the PR touches a shared type; also by `AG-SEC` when the
   task is marked `security: yes`.
4. **Branch per task:** `<type>/<slug>` from `develop`, as given in each task.

### Pod map

```text
                 ┌────────────── Command ──────────────┐
                 │               AG-LEAD               │
                 └──────────────────┬──────────────────┘
      ┌─────────────────┬───────────┴───────┬─────────────────────┐
  Interfaces           Data              Runtime            Coordination
  AG-CLI  AG-API      AG-STORE           AG-PLAT            AG-COLLAB
  AG-MCP  AG-WEB      AG-GIT             AG-WS              AG-SCHED  AG-INT
      └─────────────────┴───────────┬───────┴─────────────────────┘
                 ┌──────────────── Assurance ───────────────┐
                 │          AG-QA            AG-SEC         │
                 └──────────────────────────────────────────┘
```

---

## 2. Epics per sprint

Epic IDs are stable: later sprints reference them as dependencies
(e.g. "needs `S0-E3`"). Each sprint document breaks its epics into tasks
`S<n>-T<nn>`.

### Sprint 0 — Foundation

| Epic | Title | Owner |
|---|---|---|
| `S0-E1` | Cobra CLI skeleton: command tree stubs, `--json`, exit codes (§14) | `AG-CLI` |
| `S0-E2` | Config: `config.toml` schema, defaults, validation, `config.example.toml` (§22) | `AG-CLI` |
| `S0-E3` | Store: SQLite (`modernc.org/sqlite`), embedded migrations, base schema (§13) | `AG-STORE` |
| `S0-E4` | Git wrapper: safe exec, `git >= 2.40` check, `check-ref-format` (§8, §17) | `AG-GIT` |
| `S0-E5` | Windows platform layer: Job Objects / process tree kill, long paths, path canonicalization (§7) | `AG-PLAT` |
| `S0-E6` | Dev Drive: VHDX ReFS provisioning, `init --devdrive`, `devdrive create\|attach\|detach\|status` (§6.4) | `AG-PLAT` |
| `S0-E7` | `cowflow doctor` (§7, §14) | `AG-CLI` + `AG-PLAT` |
| `S0-E8` | Basic hermetic harness + re-enable CI on 3 platforms (§18.1) | `AG-QA` |

### Sprint 1 — COW workspaces

| Epic | Title | Owner |
|---|---|---|
| `S1-E1` | `WorkspaceProvider` interface + capability reporting (`CowKind`) (§6.2) | `AG-WS` |
| `S1-E2` | `git_worktree` provider (universal fallback) (§6.3) | `AG-WS` + `AG-GIT` |
| `S1-E3` | Clone primitives: block clone (Windows), `FICLONE` (Linux), `clonefile` (macOS) (§6.3–6.4) | `AG-PLAT` |
| `S1-E4` | `cow_clone` provider + golden trees (§6.3–6.4) | `AG-WS` |
| `S1-E5` | Sparse checkout by intent paths (§6.6) | `AG-GIT` + `AG-WS` |
| `S1-E6` | Dirty detection, rescue, destroy (`--force`) (§17, §20.12) | `AG-WS` |
| `S1-E7` | Shared dependency cache (§6.7) | `AG-WS` |
| `S1-E8` | CLI: `task create`, `workspace list\|inspect\|destroy`, `open`, `golden list\|gc` (§14) | `AG-CLI` |
| `S1-E9` | Benchmarks: 20 workspaces < 2 s, incremental disk, cache ≥ 50% (§20.1, .17, .21) | `AG-QA` |

### Sprint 2 — Collaboration plane (daemonless)

| Epic | Title | Owner |
|---|---|---|
| `S2-E1` | Domain model + tables: tasks, sessions, intents, requests, changes, contracts (§13) | `AG-STORE` |
| `S2-E2` | Event store, transactional outbox, cursor polling (§9.9) | `AG-STORE` |
| `S2-E3` | Presence + intents + overlap detection (§9.1–9.2, §24.9) | `AG-COLLAB` |
| `S2-E4` | Cross-requests lifecycle (§9.3, §24.11) | `AG-COLLAB` |
| `S2-E5` | Change publish, manifest, retention refs (§8.1–8.2) | `AG-GIT` |
| `S2-E6` | Derived index + `who --path` (§8.3, §20.23) | `AG-STORE` |
| `S2-E7` | Sync: merge / cherry-pick / squash, conflict, abort (§8.4) | `AG-GIT` |
| `S2-E8` | Contracts (§9.5, §24.8) | `AG-COLLAB` |
| `S2-E9` | `pattern` invariants at publish (§9.6) | `AG-COLLAB` |
| `S2-E10` | CLI for intents, requests, changes, sync, index (§14) | `AG-CLI` |
| `S2-E11` | Two-terminal end-to-end scenario (§19 Phase 2 output) | `AG-QA` |

### Sprint 3 — MCP + runners

| Epic | Title | Owner |
|---|---|---|
| `S3-E1` | MCP server with `lead` / `worker` / `watchdog` tool sets (§15, §15.1) | `AG-MCP` |
| `S3-E2` | Per-session scoped tokens (§12, §17) | `AG-MCP` (security: yes) |
| `S3-E3` | `.mcp.json` generation in `init`; `init --conventions gitflow` (§15.2, §24.2) | `AG-CLI` |
| `S3-E4` | Runner framework + generic runner (§15.3) | `AG-MCP` |
| `S3-E5` | Claude Code and Codex runners, per-vendor protocol injection, headless permissions (§15.3–15.4) | `AG-MCP` |
| `S3-E6` | Server-side protocol enforcement (§15.5, §2.2 principle 3) | `AG-COLLAB` + `AG-MCP` |
| `S3-E7` | Per-provider LLM budget accounting (§11, §24.15) | `AG-SCHED` |
| `S3-E8` | End-to-end: lead creates tasks; Claude + Codex workers finish a dependent feature (§20.18, .27, .28, .29) | `AG-QA` + `AG-LEAD` |

### Sprint 4 — Daemon, scheduler, SSE

| Epic | Title | Owner |
|---|---|---|
| `S4-E1` | Daemon lifecycle: `daemon start\|stop\|status`, on-demand start (§5, §14) | `AG-API` |
| `S4-E2` | Loopback HTTP API + token auth (§12) | `AG-API` (security: yes) |
| `S4-E3` | SSE streaming, ACK, dedup (§9.9, §12.5) | `AG-API` |
| `S4-E4` | Heartbeats, `session.lost` (§18) | `AG-API` |
| `S4-E5` | Scheduler limits and queueing (§11) | `AG-SCHED` |
| `S4-E6` | Resource leases: slots, queue, TTL, `exec --resource` (§9.8, §20.30) | `AG-SCHED` |
| `S4-E7` | LLM budget enforcement: 80% warn, 100% pause (§24.15, §20.19) | `AG-SCHED` |
| `S4-E8` | Reconciliation after restart; outbox redelivery (§18, §20.8) | `AG-STORE` |
| `S4-E9` | Service install: Windows svc / systemd / launchd (`daemon install`) (§3.1) | `AG-PLAT` |
| `S4-E10` | Watchdog (§9.7, §20.25) | `AG-COLLAB` |
| `S4-E11` | Load test: 20 simulated + 5 real sessions (§20.15) | `AG-QA` |

### Sprint 5 — Integration

| Epic | Title | Owner |
|---|---|---|
| `S5-E1` | Integration group model + CLI `integration create\|validate\|cancel\|show\|open` (§10, §14) | `AG-INT` + `AG-CLI` |
| `S5-E2` | Candidate workspace + deterministic application order (§10.1) | `AG-INT` + `AG-WS` |
| `S5-E3` | Diff-only gates (§10.1) | `AG-INT` |
| `S5-E4` | Workspace gates (build/test via resource leases) (§10.1, §9.8) | `AG-INT` |
| `S5-E5` | `cmd` invariants as gates (§9.6) | `AG-INT` |
| `S5-E6` | Stale detection and revalidation (§10.1, §20.11) | `AG-INT` |
| `S5-E7` | Manual promotion with compare-and-swap on `target_sha`; gitflow preset effects (§10, §24.2) | `AG-INT` + `AG-GIT` |
| `S5-E8` | Review by intent: `integration show` (§10, §1.2) | `AG-CLI` |
| `S5-E9` | Post-promotion with auto-revert (§10.2, §20.24) | `AG-INT` |
| `S5-E10` | Demo: two features validated and promoted with evidence (§20.9, .10) | `AG-QA` |

### Sprint 6 — ProjFS + hardening

| Epic | Title | Owner |
|---|---|---|
| `S6-E1` | ProjFS provider (experimental, behind flag) (§6.5) | `AG-PLAT` + `AG-WS` |
| `S6-E2` | fsmonitor integration and `scope_only` (§6.5) | `AG-WS` |
| `S6-E3` | Materialization metrics (§20.22) | `AG-WS` + `AG-QA` |
| `S6-E4` | Failure table §18 fully covered by harness tests (§20.26) | `AG-QA` |
| `S6-E5` | Stress / crash / disk-full / stuck-handle / unmounted-VHDX scenarios (§18) | `AG-QA` |
| `S6-E6` | Fuzzing: manifest parsers, API payloads (§18.1) | `AG-QA` |
| `S6-E7` | Security hardening review of the whole system (§17) | `AG-SEC` |
| `S6-E8` | Platform limitation docs: `docs/windows.md`, `docs/projfs.md` (§20.14) | `AG-PLAT` |

### Sprint 7 — Web dashboard (post-v1)

Read-only. Plain HTML + JavaScript (no Node, no build step) embedded with
`go:embed`, served by the daemon on loopback, live updates over the existing
SSE stream (§23 decision #13).

| Epic | Title | Owner |
|---|---|---|
| `S7-E1` | Read-only dashboard read model: aggregate `/v1/status` and the read endpoints the views need; no new write paths (§12) | `AG-API` |
| `S7-E2` | Browser authentication on loopback: one-time token → read-only session cookie; Host/Origin checks, CSP (§12, §17) | `AG-API` (security: yes) |
| `S7-E3` | `internal/web`: embedded static assets, serving, cache and security headers | `AG-WEB` |
| `S7-E4` | Views: overview (sessions, tasks), intents and overlaps, cross-requests board, resources/leases and budgets, integration groups, event feed | `AG-WEB` |
| `S7-E5` | Live updates: `EventSource` on the SSE stream, reconnect with cursor, client-side state, render throttling | `AG-WEB` + `AG-API` |
| `S7-E6` | CLI `cowflow dashboard` (start daemon if needed, open the browser) | `AG-CLI` |
| `S7-E7` | Tests: handler, auth, and header tests; 20+ session load with the dashboard open; manual browser checklist | `AG-QA` + `AG-SEC` |
| `S7-E8` | `docs/dashboard.md`: usage, screenshots, security model | `AG-WEB` |

---

## 3. Acceptance criteria → sprint

Each §20 criterion is demonstrated in exactly one sprint's demo (it may be
partially built earlier).

| Sprint | §20 criteria demonstrated |
|---|---|
| 0 | 16 (cancellation kills the tree), 21 (Dev Drive provisioned by `init`) |
| 1 | 1, 2, 12, 13, 17, 21 (`cow_clone` < 2 s, disk < 1%) |
| 2 | 3, 4 (publish refused without intent / with violated invariant), 5, 6, 7, 20, 23 |
| 3 | 18, 27, 28, 29, 31 (branch pattern, no direct commits) |
| 4 | 8, 15, 19, 25, 30 |
| 5 | 4 (integration refused without change), 9, 10, 11, 24, 31 (hotfix group) |
| 6 | 14, 22, 26 |
| 7 | none (post-v1; acceptance defined in [sprint-7.md](sprint-7.md)) |

---

## 4. How a sprint runs

1. **Day 1 — kickoff (`AG-LEAD`):** confirm prerequisites from earlier
   sprints are merged; resolve the SPEC-GAPS listed in the sprint document;
   freeze the *Contracts* section.
2. **Parallel lanes:** each role takes its tasks in the order of the sprint's
   lane diagram. Consumers code against agreed interfaces using fakes.
3. **Each task → one branch → one PR** to `develop`, following
   [GITFLOW.md](../GITFLOW.md). Small PRs; merge as soon as green and reviewed.
4. **Last days — demo (`AG-QA` + `AG-LEAD`):** run the sprint's demo script
   on a clean machine; record evidence (logs, timings) in the PR that closes
   the sprint; update `CHANGELOG.md`.
5. **Retro:** move learnings into skills (`docs/skill/`) and new gaps into
   `SPEC-GAPS.md`.

## 5. Task template

Every task in a sprint document follows this shape:

```markdown
### S<n>-T<nn> — <title>

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|

**Skills:** ...   **Spec:** §...
**Files:** paths to create or change

**Why / what** — plain-language context.
**Steps** — numbered, concrete, with commands and code sketches.
**Tests** — what to write and how to run it.
**Acceptance** — checkboxes that must be true to merge.
**Pitfalls** — mistakes a newcomer is likely to make.
```
