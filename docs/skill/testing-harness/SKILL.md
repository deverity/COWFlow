---
name: testing-harness
description: Use when writing or reviewing tests for COWFlow — unit, integration, concurrency (20+ sessions), fuzzing, or the hermetic harness in test/harness (fake clock, tmpfs Git repo, Git subprocess fault injection, disk-full, crash between outbox write and delivery, simulated VHDX/ProjFS). Also load when mapping work to §20 acceptance criteria or changing CI.
---
# Testing Strategy and Hermetic Harness

## When to use
- Any PR adding behavior (every feature ships with tests; the `test` gate is `required`, §24.4).
- Building or extending `test/harness`, `test/integration`, `test/fixtures`.
- Adding a failure mode to §18, an acceptance criterion check from §20, or a fuzz target.
- Changing `.github/workflows/ci.yml` or test flags.

## Source of truth
SCOPE.md wins on any conflict.
- §3.1 Included (last bullet: unit, integration, 20+ sessions, harness, CI on 3 platforms)
- §16 Go stack (`testing` + testify, `-race`, hermetic harness), §16.1 Layout (`test/harness`, `test/integration`, `test/fixtures`)
- §18 Resilience (failure table), §18.1 Hermetic harness
- §19 Phases (Output of each phase; Phase 6 "all failures in table 18 covered by tests"; fuzzing)
- §20 Acceptance criteria (1–31)
- Context: §2.2 Principles, §8.1 Git rules, §9.9 Events, §13 Data model, §23 Risks and decisions

## Rules
- Tests use `testing` + testify and MUST pass under `-race` (§16).
- Build with `CGO_ENABLED=0` (§16, §23 decision 1). Go version per `go.mod` (1.26+, SCOPE.md §16).
- Core and tests MUST run on Windows, Linux and macOS in CI; platform limitations documented in `docs/windows.md` and `docs/projfs.md` (§3.1, §19 Phase 0, §20 #14).
- `test/harness` MUST provide (§18.1): fake clock; Git repository on tmpfs; fault injection into the Git subprocess (exit code, timeout, truncated output); simulated disk full; daemon crash between outbox write and delivery; simulated VHDX (interface); simulated ProjFS (in-memory callbacks).
- Every row of the §18 failure table MUST have an automated harness test (§18.1, §20 #26):
  agent crashes → `session.lost`, workspace/logs preserved, retry; daemon restarts → reconcile SQLite × processes × worktrees × ProjFS roots, idempotent `op_id`, outbox resumes; lost event → cursor + redelivery + dedup; sync conflict → nothing overwritten, `conflict` with files, `abort` restores; target advances → `stale` → revalidates; disk full / provider fails → explicit failure, never partial `ready`, compensation; stuck handle → `pending_cleanup` with retry; VHDX not mounted → `pending_mount`, auto-attach, NEVER silent NTFS fallback; ProjFS callback timeout → error to the agent, not a hang, virtualization restarts; post-promotion fails → automatic revert + requests + lead; LLM budget → pause + lead.
- Fuzz manifest parsers and API payloads (§18.1, §19 Phase 6). API payloads are ≤ 1 MiB (§12).
- Concurrency: 20+ simulated sessions without event duplication or loss (§20 #15).
- `who --path` MUST be verified to answer from the index without invoking Git, via a mock (§20 #23).
- Derived indexes are recomputable: `cowflow index rebuild` output MUST equal incremental state (§8.3, §13).
- State + event written in the same transaction; outbox is at-least-once with ULID, cursor + ACK (§9.9, §13) — tests assert both atomicity and dedup.
- NEVER rely on real time, real network, real LLMs or the developer's Git config in unit/harness tests. Guidance: real-LLM runs (§20 #18, #27, #28) are separate, opt-in e2e jobs.

## Implementation guide
Layout (§16.1): unit tests beside code in `internal/**` (`*_test.go`); harness in `test/harness/`; cross-package scenarios in `test/integration/`; repos, manifests, configs in `test/fixtures/`.

Seams the production code must expose so the harness can plug in (names Guidance unless quoted from spec):
```go
// test/harness consumes these; production code depends on interfaces, not concretes.
type Clock interface { Now() time.Time; After(d time.Duration) <-chan time.Time; NewTicker(d time.Duration) Ticker }

// Git runs via exec.Command with -C, GIT_TERMINAL_PROMPT=0, timeout (§8.1); wrap it so faults can be injected.
type GitRunner interface { Run(ctx context.Context, dir string, args ...string) ([]byte, error) }
type GitFault struct { Match []string; ExitCode int; Timeout bool; TruncateAt int }

type FS interface { /* create/write/rename used by workspace + store; harness returns ENOSPC */ }

// Spec interfaces simulated by the harness:
//   WorkspaceProvider (§6.2) with Capabilities{CopyOnWrite: Block|Virtual|None, RequiresDaemon, ...}
//   simulated VHDX: mount/attach state toggles -> pending_mount
//   simulated ProjFS: in-memory GetDirectoryEnumeration/GetPlaceholderInfo/GetFileData/Notification
type CrashPoint string // e.g. "outbox.after_write_before_delivery"
```
Steps:
1. Harness constructor (Guidance): `harness.New(t, opts...)` returns repo dir, SQLite path, fake clock, fault registry; all cleanup via `t.Cleanup`.
2. Git repo on tmpfs: Linux `/dev/shm` when available; Guidance: fall back to `t.TempDir()` on Windows/macOS (no tmpfs) and set `GIT_CONFIG_GLOBAL`/`GIT_CONFIG_SYSTEM` to empty files plus fixed author/committer env for determinism.
3. Git fault injection: `GitRunner` decorator matching argv prefixes; returns configured exit code, blocks until ctx deadline for timeout, or truncates stdout (exercises `git diff-tree -r --numstat` + `--raw` parsing, §8.2).
4. Disk full: `FS` decorator returning `ENOSPC` after N bytes; assert no workspace reaches `ready` and compensation removes partial state (§18).
5. Crash between outbox write and delivery: crash point hook that aborts the daemon goroutine after commit; restart daemon on same SQLite; assert redelivery and consumer dedup by ULID (§9.9, §20 #8).
6. Simulated VHDX: toggle unmounted → expect `workspace.pending_mount`, never NTFS path creation (§6.4, §18).
7. Simulated ProjFS: in-memory callbacks with injectable latency; callback timeout → error to agent; assert no blob served outside authorized `base_sha` (§17).
8. Concurrency: spawn 20+ simulated sessions (goroutines using MCP/API backends) doing intent → publish → sync → events; assert per-consumer event sequence has no gaps/duplicates after ACK (§20 #15); with `compile.slots = 3` and 6 requesters, at most 3 hold leases, FIFO `resource.granted`, dead session lease expires (§20 #30).
9. Fuzz: native Go fuzzing (`func FuzzXxx(f *testing.F)`) for change manifest parsing (§8.2), `git diff-tree` output parsing, TOML config, every `/v1` JSON request body, MCP tool inputs; seed corpus in `testdata/fuzz/`. Guidance: CI runs seeds only (`go test`); longer `-fuzz` runs are separate.
10. CI (`.github/workflows/ci.yml` already runs `CGO_ENABLED=0 go build` and `go test -race -cover ./...` on ubuntu/windows/macos): keep that matrix; platform-specific tests use build tags/filenames (`_windows.go`, `_linux.go`, `_darwin.go`) and `t.Skip` with a reason when a capability (ReFS, btrfs, APFS, ProjFS, admin) is absent.
11. Traceability: Guidance: name tests or add a comment `// AC#<n>` / `// §18:<row>` so each §20 criterion and §18 row maps to at least one test; keep a table in `docs/` only if the project asks for it.

Acceptance criteria → test level (Guidance on placement; criteria from §20):
- Harness/integration (hermetic): #1–#12, #15, #19, #20, #23–#26, #30, #31.
- CI matrix itself: #14.
- Platform-specific: #16 (Windows tree kill via Job Object), #13 (`inspect` real FS + `CowKind`), #21 (Dev Drive, ≥ 1 GB repo < 2 s, disk < 1%), #22 (ProjFS < 1 s, < 20% materialized), #17 (shared cache ≥ 50% faster second setup) — benchmarks, need real hardware/admin.
- E2E with real agents (opt-in): #18, #27, #28, #29.
- Phase outputs (§19) are milestone demos built on the same tests (e.g., Phase 1 "20 COW workspaces in < 2 s each").

## Pitfalls
- `time.Sleep` / wall clock in tests → flakes; use the fake clock (TTL, heartbeat, `escalation_after`, lease expiry, `wait_events` timeout).
- Using the developer's global Git config, hooks or credential helper; NEVER allow network (`GIT_TERMINAL_PROMPT=0`, §8.1).
- Asserting only the happy path of a fault: also assert the emitted event (`session.lost`, `workspace.pending_cleanup|pending_mount`, `budget.exceeded`, `integration.stale`, `integration.reverted`).
- Treating "no error" as success after disk full — check no partial `ready` row and no orphan dir/ref.
- Windows file locking: tests that destroy workspaces must close handles; stuck handle path → `pending_cleanup` (§7).
- Event dedup tested with one consumer only; test multiple consumers with independent cursors.
- `t.Parallel()` tests sharing one SQLite file or repo; each test gets its own harness instance.
- Mocking Git so heavily that `who --path` index tests no longer prove "no Git invoked" — assert the mock recorded zero calls (§20 #23).

## Tests required
- One harness test per §18 row (11 rows) — §20 #26.
- Outbox crash/redelivery/dedup — §20 #8, #15.
- Enforcement refusals — §20 #4; protected branch / naming / hotfix back-merge — §20 #31.
- Workspace isolation: same file edited in two workspaces, 20 independent branches — §20 #1, #2.
- Never destroy unpreserved changes without `--force` — §20 #12.
- Sync conflict recorded; `abort` restores — §20 #5, §18.
- Stale revalidation and diff-only gates before workspace — §20 #10, #11.
- Leases with fake clock — §20 #30. Budget pause — §20 #19. Contract change notifies + requires lead — §20 #20.
- Watchdog detects injected anomaly and opens a cross-request — §20 #25. Post-promotion auto-revert — §20 #24.
- Index answers without Git (mock) and `index rebuild` equivalence — §20 #23, §8.3.
- Fuzz targets: manifest parser, diff-tree parser, API payloads (≤ 1 MiB), MCP inputs.
- All suites green with `-race` on 3 OSes.

## Done checklist
- [ ] New behavior has unit tests; cross-component behavior has a `test/integration` scenario.
- [ ] Time, Git, FS, process and crash points are injectable; tests use the harness, not real time/network.
- [ ] Each touched §18 row and §20 criterion has a traceable test.
- [ ] Fuzz target added/updated for any new parser or API payload, with seed corpus.
- [ ] `go test -race ./...` passes locally; CI green on ubuntu, windows, macos; `CGO_ENABLED=0` build.
- [ ] Platform-only tests skip with an explicit reason; limitation documented in `docs/windows.md`/`docs/projfs.md` if applicable.
- [ ] No test depends on global Git config, network, or a real LLM (except opt-in e2e).
