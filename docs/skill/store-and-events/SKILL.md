---
name: store-and-events
description: Use when creating or changing internal/store — SQLite schema/migrations, transactions, the event store and transactional outbox, cursors/ACK/dedup, idempotent `op_id`, derived index tables (`path_index`, `contract_refs`, `change_ancestry`), daemonless multi-process writes, or crash reconciliation of SQLite against Git/filesystem.
---
# Store and Events (internal/store)

## When to use
- Adding/altering a table, migration, or query in `internal/store`.
- Emitting any event from §9.9, or reading events (polling, SSE feed, `wait_events`, `cowflow events`).
- Implementing `op_id` idempotency, `cowflow index rebuild`, `who --path`, overlap detection.
- Anything that must survive a crash/restart, or that runs in daemonless mode (CLI/MCP writing SQLite directly).

## Source of truth
SCOPE.md wins on any conflict.
- §2.2 Principles 6, 8 (Git canonical; SQLite = metadata/events/derived indexes; recovery by reconciliation)
- §3.1 Included (SQLite `modernc.org/sqlite`, no cgo, embedded migrations; daemonless mode; event store with outbox)
- §5.3 Modes (daemonless vs daemon capabilities)
- §8.2 Change manifest, §8.3 Derived index, §8.5 Change states
- §9.1 Presence, §9.3 Cross-requests (states), §9.8 Resource leases (tables, persistence), §9.9 Events
- §13 Data model
- §16 Go stack / §16.1 Layout; §17 Security (audit, reconciliation)
- §18 Resilience, §18.1 Hermetic harness; §20 items 3, 8, 9, 12, 15, 23, 26, 30; §21 Observability; §23 Risks/decisions

## Rules
- MUST use `modernc.org/sqlite` only; build with `CGO_ENABLED=0` (§3.1, §16, §23 decision 2). NEVER import `mattn/go-sqlite3`.
- MUST open SQLite in WAL mode with `busy_timeout` set; migrations MUST be embedded via `embed` (§13).
- MUST create the tables named in §13: `projects`, `tasks`, `task_dependencies`, `contracts`, `invariants`, `agent_sessions`, `workspaces`, `golden_trees`, `presence`, `intents`, `cross_requests`, `cross_request_comments`, `changes`, `change_files`, `sync_attempts`, `integration_groups`, `integration_group_members`, `integration_attempts`, `path_index`, `contract_refs`, `change_ancestry`, `resources`, `resource_leases`, `resource_queue`, `events`, `event_deliveries`, `outbox`, `budgets`, `audit_log`.
- MUST use the column sets the spec gives: `path_index(path_prefix, task_id, intent_id, change_id, kind, updated_at)`, `contract_refs(contract_path, task_id)`, `change_ancestry(change_id, parent_change_id)` (§8.3); `resource_leases(id, resource, session_id, task_id, acquired_at, expires_at, released_at, state)`, `resource_queue(resource, session_id, priority, enqueued_at)` (§9.8).
- IDs MUST be prefixed ULIDs (oklog/ulid) (§13, §16). Prefix strings are not specified in SCOPE.md.
- State change + its event MUST be written in the same transaction (§13, §9.9 "transactional outbox").
- Derived index rows MUST be updated in the same transaction as the publish/intent that causes them (§8.3).
- Derived indexes MUST be recomputable from changes and intents; `cowflow index rebuild` recomputes from scratch (§8.3, §13, §23).
- `cowflow who --path` and overlap detection MUST query only the index, never invoke Git (§8.3, §20.23).
- Overlap MUST emit `intent.overlap` to both sessions via the index; it MUST NOT block or reserve paths (§2.2 #2, §9.2).
- Event delivery MUST be at-least-once with cursor + ACK; consumers dedup; redelivery after loss (§9.9, §18 "Lost event").
- Event types MUST be exactly the §9.9 names: `agent.presence_changed`, `intent.created|updated|overlap`, `cross_request.*`, `change.published|superseded|discarded`, `sync.*`, `resource.acquired|released|granted|queued|expired|revoked|announced`, `integration.created|started|passed|failed|stale|promoted|reverted`, `contract.changed`, `invariant.violated`, `session.lost`, `workspace.pending_cleanup|pending_mount`, `budget.exceeded`.
- Mutations MUST be idempotent by `op_id` so a retried op after crash/restart is not applied twice (§18 "Daemon restarts", §23 "Git × SQLite diverge").
- Git is canonical: NEVER treat SQLite as the source of truth for code; reconciliation MUST read the actual Git refs and filesystem (§2.2 #6, §17).
- Store the change manifest per file in `change_files` (path, op, blob_before, blob_after, add, del) plus stat and `touches_contracts` (§8.2) so overlap/contract impact/index work without Git.
- Change states are `published → validated → promoted`, or `superseded` / `discarded`; keep them distinct (§8.5, §20.9).
- Resource queue MUST be persisted and survive restart; reconciliation expires leases of dead sessions (§9.8).
- MUST record audit rows for publish/sync/destroy/promote/revert/reassign (§17) in `audit_log`.
- Disk full / provider failure → explicit failure; NEVER leave a partial `ready` state; compensate (§18).
- Daemonless mode MUST support tasks, workspaces, intents, requests, changes, manual sync, index, polling — with no daemon process (§5.3, Phase 2 output §19).

## Implementation guide
Files (§16.1 `internal/store/  # sqlite, migrations/, outbox, index`):
- `internal/store/sqlite.go` — open/configure DB; `internal/store/migrations/NNNN_*.sql` + `migrate.go`.
- `internal/store/tx.go` — transaction helper; `internal/store/outbox.go` — event append/read/ACK; `internal/store/index.go` — derived index update/rebuild/query; `internal/store/ops.go` — `op_id` dedup; `internal/store/reconcile.go` hooks consumed by `internal/daemon`.
- Domain types live in `internal/domain`; store maps rows ⇄ domain structs.

Guidance (sketch; names other than spec tables/events are illustrative):
```go
type Store struct{ db *sql.DB; clock Clock; ids IDGen }
func Open(ctx context.Context, path string, opt Options) (*Store, error)
func (s *Store) WithTx(ctx context.Context, opID string, fn func(*Tx) error) error // BEGIN IMMEDIATE; op_id dedup; commit
func (t *Tx) Emit(ev domain.Event) error            // INSERT events + outbox in same tx
func (s *Store) EventsAfter(ctx context.Context, projectID, cursor string, limit int) ([]domain.Event, string, error)
func (s *Store) Ack(ctx context.Context, subscriber, cursor string) error // event_deliveries
func (t *Tx) IndexIntent(in domain.Intent) error    // path_index / contract_refs
func (t *Tx) IndexChange(c domain.Change, files []domain.ChangeFile) error // path_index / change_ancestry
func (s *Store) RebuildIndex(ctx context.Context, projectID string) error
func (s *Store) WhoPath(ctx context.Context, projectID, path string) ([]domain.PathHit, error)
```
Steps:
1. Guidance: DSN for modernc: `file:<state_dir>/...db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(<ms>)&_pragma=foreign_keys(1)&_txlock=immediate`. DB file name/location under `[daemon] state_dir` is not fixed by SCOPE.md.
2. Guidance: migrations via `//go:embed migrations/*.sql`, applied in order inside one tx each, version tracked with `PRAGMA user_version`; never edit an applied migration.
3. Guidance: every write uses `BEGIN IMMEDIATE` (`_txlock=immediate`) so concurrent daemonless writers wait on `busy_timeout` instead of failing on lock upgrade.
4. Event write = `INSERT INTO events` + `INSERT INTO outbox` in the caller's tx. The daemon drains `outbox` to SSE/watchdog and resumes it after restart (§18); `event_deliveries` tracks per-consumer cursor/ACK. Exact column layout of `events`/`outbox`/`event_deliveries` is not specified — document it in `docs/events` (§16.1 `docs/`).
5. Cursor: Guidance — ULIDs minted in different processes are not commit-ordered; derive the read cursor from an order assigned inside the write transaction (e.g., an integer sequence) and keep the ULID as event identity/dedup key. Whether the public cursor is the ULID itself is ambiguous in §9.9.
6. `op_id`: Guidance — UNIQUE column recorded in the same tx as the mutation; on conflict return the stored result. SCOPE.md lists no dedicated table for it; if one is added, record the decision in `docs/decisions/`.
7. Side effects outside SQLite (Git ref `refs/cowflow/changes/<id>`, worktree creation): Guidance — record a pending row keyed by `op_id` before the side effect, finalize after; reconciliation compares pending rows with actual Git/filesystem and completes or compensates.
8. `index rebuild`: delete derived rows for the project and recompute from `intents`, `changes`, `change_files` in one tx.
9. Inject a clock and ID generator so the harness fake clock (§18.1) controls TTLs (`presence_ttl_seconds`, `lease_ttl_seconds`) and ordering.

## Pitfalls
- Deferred tx + write upgrade under concurrency → immediate `SQLITE_BUSY` regardless of `busy_timeout`. Use immediate tx (Guidance).
- Up to 20+ MCP/CLI processes write the same file in daemonless mode; keep write tx short; never hold a tx across Git subprocess calls or network waits.
- `database/sql` pool: Guidance — set pragmas via DSN (applies per connection), not one-off `Exec`, or new pool connections miss them.
- Emitting an event after commit (or in a goroutine) breaks the outbox guarantee and loses events on crash.
- Consumers MUST tolerate duplicates (at-least-once); never assume exactly-once.
- Index drift: any code path that changes intents/changes without updating `path_index` in the same tx creates silent overlap misses.
- Calling Git from `who --path` violates §20.23 (tested by mock).
- Cleanup/destroy logic must not drop rows for unpreserved changes; destroy requires `--force` (§20.12).
- WAL files (`-wal`, `-shm`) belong to the DB; never copy only the main file for backups.
- Open: declared stub format (§23 open question 2) — do not fix a schema for `stub` beyond what intents need.

## Tests required
- Migrations apply from empty and are idempotent on re-open; `CGO_ENABLED=0 go test -race ./internal/store/...`.
- State + event atomicity: inject failure after state write → neither row persists.
- §18.1 harness: daemon crash between outbox write and delivery → redelivered after restart; no loss, dedup by ID (§20.8).
- 20+ simulated concurrent sessions (separate `*sql.DB`/processes on one file): no event duplication or loss as seen by a cursor reader (§20.15); no `SQLITE_BUSY` surfaced.
- `op_id` replay returns the original result and does not double-apply.
- Disk full simulated (§18.1) → explicit error, no partial `ready`.
- Reconciliation: Git ref exists without finalized row, and row without ref → reconciled per Git (§17, §23).
- `index rebuild` output equals incrementally maintained index; `who --path` with a Git mock asserting zero calls (§20.23).
- `intent.overlap` emitted to both sessions; no block.
- Lease persistence across restart; dead-session lease expires and frees slot, next queued gets `resource.granted` in order (§9.8, §20.30).
- Change states distinct and transitions enforced (§8.5, §20.9).
- Fuzz manifest parsing into `change_files` (§18.1).
- Phase 2: intent → request → publish → sync cycle across two processes without daemon (§19).

## Done checklist
- [ ] All §13 tables exist via embedded migrations; WAL + `busy_timeout` set on every connection.
- [ ] Prefixed ULID IDs; prefixes documented.
- [ ] Every state mutation emits its §9.9 event in the same tx.
- [ ] Cursor/ACK/redelivery implemented; consumers dedup.
- [ ] `op_id` idempotency on all mutating store operations.
- [ ] Derived index updated transactionally and rebuildable; `who --path` Git-free.
- [ ] Daemonless concurrency verified with 20+ writers.
- [ ] Reconciliation hooks compare SQLite with real Git/filesystem.
- [ ] Audit rows for publish/sync/destroy/promote/revert/reassign.
- [ ] Harness scenarios from §18 relevant to the store pass with `-race`.
