---
name: cow-workspaces
description: Use when implementing or changing `internal/workspace` — the `WorkspaceProvider` interface, the `cow_clone`/`projfs`/`git_worktree`/`container` backends, golden trees, sparse checkout by intent, the shared cache, capability reporting, or workspace create/inspect/destroy/cleanup. Also load for `cowflow workspace|golden|open` commands and `/v1/workspaces` handlers.
---
# COW Workspaces

## When to use
- Adding/modifying a backend, `provider = "auto"` selection, or `Capabilities` probing.
- Golden tree cache (`golden_trees` table, `cowflow golden list|gc`), sparse (`cowflow_expand_scope`), shared cache env.
- Workspace lifecycle: create, `inspect`, `destroy [--force]`, rescue, `pending_cleanup`, `pending_mount`, failed/completed retention.
- For raw OS calls (block clone, VHDX, ProjFS DLL, Job Objects, long paths) also load `windows-platform`.

## Source of truth
`docs/SCOPE.md` wins on any conflict. Derived from:
- §2.2 Principles (1, 9, 10, 13, 14) · §4 Glossary · §5.3 Modes
- §6.1 Isolation contract · §6.2 Provider · §6.3 Backends · §6.4 VHDX ReFS (Dev Drive) · §6.5 ProjFS · §6.6 Sparse checkout by intent · §6.7 Shared cache · §6.8 A workspace is not a container
- §8.1 Git rules · §11 Scheduler · §12.1 API · §13 Data model · §14 CLI · §15.4 Headless permissions
- §16/§16.1 Go stack/Layout · §17 Security · §18/§18.1 Resilience/Hermetic harness · §19 Phases 1 and 6
- §20 Acceptance criteria 1, 2, 12, 13, 14, 17, 21, 22 · §21 Observability · §22 `[workspace]`, `[workspace.projfs]`, `[cache]`, `[scheduler]` · §23 Risks/Decisions/Open questions · §24.2, §24.12, §24.13

## Rules
- MUST give each workspace its own path, branch, `HEAD`/index/files, logs, `.env.cowflow`, `port_range`, link to project/task/session/`base_sha`, provider and capabilities (§6.1).
- A write in workspace A MUST never change B — including via golden trees, shared caches or clones (§6.1, AC 2).
- MUST implement backends behind `WorkspaceProvider`; providers are swappable (§2.2 p9, §6.2).
- `provider = "auto"` chooses `cow_clone` → `git_worktree`. `projfs` is NEVER chosen automatically in v1; it requires `--provider projfs` and `[workspace.projfs].enabled` (§6.3, §23 decision 10).
- NTFS / no COW MUST fall back to `git_worktree` with an explicit warning (§1.2, §23 risks).
- If the Dev Drive is configured but not mounted: state `workspace.pending_mount`, retry/auto-attach; NEVER silently create on NTFS (§6.4, §18).
- Capabilities MUST reflect what is actually delivered: physical COW (`Block`), `Virtual`, worktree (`None`), or container. NEVER claim what is not delivered (§2.2 p10). `inspect` reports the actual filesystem and `CowKind` (AC 13).
- `cow_clone`: walk the golden tree of `base_sha`; create dirs; replicate symlinks; clone each file with `FSCTL_DUPLICATE_EXTENTS_TO_FILE` / `FICLONE` / `clonefile` (§6.3, §6.4).
- Golden trees: one materialized tree per `base_sha` at `<root>/golden/<sha>` (config `workspace.golden_root`); first workspace pays the checkout, later ones clone; GC by LRU and disk budget (`scheduler.golden_cache_gb`) (§6.4, §11, §22).
- Sparse: when the intent declares `paths` and `sparse_by_intent = true`, `git_worktree` and `cow_clone` apply `git sparse-checkout set <paths> + <always>` (`always` = root, configs, contracts, `go.mod`). `cowflow_expand_scope` updates intent and sparse. Task `sparse = false` disables it (§6.6).
- Shared cache (`[cache]`): download caches shared; build caches without concurrency safety (e.g. `cargo_target`) get a subdirectory per workspace; caches live on the Dev Drive (§6.7).
- `projfs`: `RequiresDaemon = true`; workspaces exist only while the daemon runs (§5.3). Set `core.fsmonitor=true` (§6.5). Callbacks NEVER serve blobs outside the authorized `base_sha` (§17). Callback timeout → error to the agent, not a hang; daemon restarts virtualization (§18).
- `container`: optional, behind a build tag; bind/volume of the worktree; the only backend with process isolation. Untrusted/third-party code → `container` with restricted network (§6.3, §6.8, §15.4).
- A workspace is NOT a sandbox: no network/port/env/credential/process isolation unless `container` (§6.8).
- NEVER create a workspace on `main`/`develop` (§24.2). Branch per task (§8.1). Names sanitized with `check-ref-format`; paths confined (§17).
- Creation failure (disk full, provider error): explicit failure, NEVER a partial `ready`; run compensation (§18).
- NEVER destroy unpreserved changes without `--force`; rescue uncommitted changes before destroy; audit `destroy` (§17, AC 12). Scheduler cleans up only when there are no unpreserved changes (§11).
- Destroy blocked by handles → `pending_cleanup` with retry and diagnostics; emit `workspace.pending_cleanup` (§7, §9.9, §18).
- Retention: failed workspaces kept per `keep_failed_workspaces`/`keep_failed_hours`; `cleanup_completed` governs completed ones; a promoted task's workspace enters the cleanup policy (§22, §24.12). Agent crash → workspace/logs preserved (§18).
- State + event in the same SQLite transaction; prefixed ULID IDs (§13). Reconciliation on restart reads actual Git and filesystem (worktrees, ProjFS roots) (§17, §18).
- Git via `exec.Command` with `-C`, `GIT_TERMINAL_PROMPT=0`, timeout; never a shell string (§7, §8.1).
- Per worktree: `core.autocrlf=false`, `core.longpaths=true` (Windows) (§7).

## Implementation guide
Files (§16.1): `internal/workspace/provider.go`, `worktree.go`, `cowclone.go`, `golden.go`, `sparse.go`, `cow_windows.go`, `cow_linux.go`, `cow_darwin.go`, `projfs/` (host, callbacks, blobstore), `container/`. FS probing lives in `internal/platform/fscaps_*`; VHDX in `internal/platform/vhdx_windows.go`.

```go
type WorkspaceProvider interface { // §6.2, verbatim
    Name() string
    Capabilities(ctx context.Context, root string) (Capabilities, error)
    Create(ctx context.Context, spec WorkspaceSpec) (*Workspace, error)
    Inspect(ctx context.Context, id string) (*WorkspaceStatus, error)
    Destroy(ctx context.Context, id string, opts DestroyOptions) error
}
type Capabilities struct {
    CopyOnWrite      CowKind // None | Block | Virtual
    SparseSupported  bool
    ProcessIsolation bool
    ResourceLimits   bool
    Filesystem       string  // "refs", "ntfs", "btrfs", "apfs", ...
    RequiresDaemon   bool
}
```
Guidance: `CowKind` as `type CowKind uint8` with `String()`; `WorkspaceSpec` carries the §6.1 fields (project/task/session IDs, `base_sha`, branch, sparse paths, port range); `DestroyOptions{Force bool}`; the per-OS clone primitive behind an unexported `cloneFile(src, dst string) error` in `cow_<goos>.go`, returning a sentinel (e.g. `ErrCloneUnsupported`) on `EOPNOTSUPP`/`EXDEV`/unsupported volume.

Create (`cow_clone`), step by step:
1. Validate/sanitize task slug and branch (`check-ref-format`); refuse `main`/`develop`; confine target path under `workspace.root`.
2. Probe `Capabilities(ctx, workspace.root)`; if Dev Drive expected but unmounted → `pending_mount` (do not fall back).
3. `golden.go`: get-or-build `golden_root/<base_sha>`; serialize builds per SHA. Guidance: build into a temp dir on the same volume then `os.Rename` so a half-built golden is never visible; record in `golden_trees` with last-used time for LRU.
4. Walk golden (`filepath.WalkDir`), `MkdirAll` dirs, `os.Symlink`/`os.Readlink` for symlinks, `cloneFile` for regular files; with sparse, clone only paths matching `<paths> + <always>`.
5. Give the clone its own Git `HEAD`/index/branch. Guidance (spec does not prescribe the mechanism): register as a linked worktree (`git worktree add --no-checkout -b <branch> <path> <base_sha>`) before cloning files, then populate the index (e.g. `git read-tree`/`update-index --refresh`); verify `git status` is clean.
6. Apply `git sparse-checkout set` (§6.6), per-worktree config (`core.autocrlf=false`, `core.longpaths=true`; projfs: `core.fsmonitor=true`).
7. Write `.env.cowflow`: allocated `port_range` (`port_range_start` + n × `port_range_per_workspace`) and cache locations from `[cache]`. Guidance: map to the tools' standard env vars (`GOMODCACHE`, `GOCACHE`, `CARGO_TARGET_DIR=<cargo_target>/<workspace_id>`, ...).
8. Persist workspace row + event in one transaction; only then report `ready`. On any error: remove partial tree, release ports, emit failure (no partial `ready`).

`git_worktree`: `git worktree add -b <branch> <path> <base_sha>` then steps 6–8. `projfs`: see §6.5 — empty projected root, backing store = persistent `git cat-file --batch` per repo + LRU blob cache (`blob_cache_mb`), `scope_only` enumeration; DLL details in `windows-platform`. `container`: Guidance — reuse `git_worktree` for the tree and add container bind mount; keep Docker code behind its build tag so the default binary has no Docker dependency.

Destroy: detect dirty state (`git status --porcelain`, unpublished commits vs `refs/cowflow/changes/*`); refuse without `--force`; rescue uncommitted work first (mechanism not specified by spec — do not invent a ref namespace without a decision); `git worktree remove` + prune; on `ERROR_SHARING_VIOLATION`/access denied → `pending_cleanup`, retry with backoff, record diagnostics.

Metrics/logs (§21): creation/destruction time per provider, `CowKind` and disk per workspace, golden cache hit/size, materialized bytes (projfs); log `workspace_id`.

## Pitfalls
- Block clone requires source and destination on the same volume: `golden_root` and `workspace.root` must share the Dev Drive / filesystem; check in `Capabilities`, not after a failed clone.
- Treat golden trees as immutable; a modified golden silently corrupts every future clone of that `base_sha`.
- Never "emulate" COW with hardlinks or symlinks into the golden: a write through them changes other workspaces (§6.1).
- Copying `.git` from the golden shares an index/HEAD between workspaces — violates §6.1.
- Partial `FICLONE` support (XFS without `reflink=1`, overlayfs, tmpfs) must degrade to `git_worktree` at selection time, not per file mid-walk.
- `projfs` + `go build`/`npm install`/Defender/indexer hydrate everything (§6.5); keep caches outside the workspace.
- `pending_mount` is not an error to swallow: never retry onto `C:` or any NTFS path.
- Concurrent creates for the same `base_sha` must not build the golden twice or read a half-built one.
- Golden-per-`base_sha` vs per-branch is open (§23 Q6); ProjFS empty-vs-lazy enumeration outside scope is open (§23 Q5). Keep both behind the existing config/interfaces.

## Tests required
- Unit: auto selection matrix (ReFS/btrfs/XFS/APFS → `cow_clone`; NTFS/ext4 → `git_worktree` + warning; projfs never auto); sparse pattern set incl. `always`; port allocation; slug/branch sanitization; path confinement; `main`/`develop` refusal.
- Integration (real git ≥ 2.40, `-race`): AC 1 (20 workspaces, independent branches), AC 2 (same file edited in two workspaces), AC 12 (no destroy of unpreserved changes without `--force`), AC 13 (`inspect` reports actual FS and `CowKind`), AC 17 (shared cache cuts second-workspace setup ≥ 50%).
- Benchmarks on capable hosts: AC 21 (≥ 1 GB repo, `cow_clone` < 2 s, incremental disk < 1%), Phase 1 output (20 workspaces < 2 s each), AC 22 (projfs < 1 s, materialization < 20% when touching < 10%).
- Hermetic harness (§18.1): disk full / provider fails → no partial `ready` + compensation; stuck handle → `pending_cleanup` + retry; VHDX not mounted (simulated VHDX) → `pending_mount`, never NTFS; ProjFS callback timeout (in-memory callbacks) → error, virtualization restart; daemon restart reconciles SQLite × worktrees × ProjFS roots; agent crash preserves workspace/logs; Git subprocess faults (exit code, timeout, truncated output) during create.
- Golden GC: LRU order, `golden_cache_gb` budget, never deletes a golden in use.
- AC 14: suite runs on Windows, Linux, macOS CI; limitations documented in `docs/windows.md` and `docs/projfs.md`.

## Done checklist
- [ ] `WorkspaceProvider`/`Capabilities` match §6.2 names exactly
- [ ] `auto` = `cow_clone` → `git_worktree`; projfs only explicit
- [ ] Capabilities probed from the real filesystem; `inspect` shows FS + `CowKind`
- [ ] Golden trees per `base_sha`, atomic build, LRU GC, recorded in `golden_trees`
- [ ] Sparse by intent + `cowflow_expand_scope` + `sparse = false`
- [ ] `.env.cowflow` with `port_range` and `[cache]` paths; non-concurrent build caches per workspace
- [ ] No partial `ready`; compensation on failure
- [ ] `pending_mount` / `pending_cleanup` states + events, retries, diagnostics
- [ ] Dirty detection, rescue, `--force`, audit on destroy
- [ ] Reconciliation on daemon start
- [ ] Metrics and log fields per §21
- [ ] Harness scenarios and ACs above pass on 3 platforms with `-race`
