# Sprint 6 — ProjFS + hardening

> **Phase:** 6 of [SCOPE.md §19](../SCOPE.md) (wk. 13–15) · **Length:** 3 weeks (15 working days) · **Exit criterion (§19 "Output"):** functional `projfs` workspace with materialization metrics; all failures in the §18 table covered by tests.

---

## 1. Goal

This is the last sprint of v1. It does two things.

**First, ProjFS.** We add the experimental `projfs` workspace provider (§6.5). A `projfs` workspace starts as an *empty* directory. Windows' Projected File System asks the COWFlow daemon "what is in this folder?" and "give me the bytes of this file" only when a program actually looks. The daemon answers from the Git objects of the workspace's `base_sha`. Files the agent never opens never reach the disk. The provider is opt-in (never picked by `provider = "auto"`), runs only while the daemon runs (§5.3), and must never serve a blob outside the workspace's authorized `base_sha` (§17). We must *prove* the benefit with numbers: creation in < 1 s, and < 20% of a full worktree materialized when the agent touches < 10% of the repo (§20.22).

**Second, hardening.** Every failure row in the §18 table gets an automated test in the hermetic harness (§18.1, §20.26). We add the scenarios that were not covered yet (disk full, stuck handle, unmounted VHDX, ProjFS callback timeout, daemon restart with ProjFS roots), real-process crash and stress runs, fuzzing of manifest parsers and API payloads, a system-wide security review (§17), and the platform limitation documents `docs/windows.md` and `docs/projfs.md` (§20.14).

At the end of this sprint every §20 acceptance criterion has been demonstrated by some sprint, so v1 is feature-complete (see §9).

---

## 2. Exit criteria

Each box is ticked in the sprint-closing PR with a link to the evidence.

- [ ] **Functional `projfs` workspace** (§19 Phase 6 output, §6.5) — created, used by an agent (read, edit, `git status`, commit, `change publish`), inspected and destroyed on Windows with `Client-ProjFS` enabled. *Verified by:* `TestProjFSLifecycle` (S6-T09) on the ProjFS CI job; demo step 3.
- [ ] **Materialization metrics** (§19 Phase 6 output, §21) — reported per workspace by `workspace inspect`, `/v1/status` and `/metrics`. *Verified by:* S6-T13/T14/T15 tests; demo step 4.
- [ ] **AC 22** (§20.22, §6.5) — `projfs` workspace created in < 1 s; with the agent touching < 10% of the repo, < 20% of the bytes of a full worktree are materialized. *Verified by:* `TestAC22ProjFS` (S6-T16) prints `create_ms`, `touched_pct`, `materialized_pct` and fails above the thresholds; demo step 5.
- [ ] **AC 26** (§20.26, §18.1) — every row of the §18 table has an automated harness test, and all pass. *Verified by:* `TestFailureTableCoverage` (S6-T18) finds a `// §18:<row>` test for all 11 rows; `go test -race -run '^TestFailure' ./test/...`; demo step 6.
- [ ] **Stress, crash, disk-full, stuck-handle, unmounted-VHDX scenarios** (§19 Phase 6) pass. *Verified by:* S6-T21…T25 suites green (stress/crash also 3 nights in a row); demo step 7.
- [ ] **Fuzzing** (§18.1, §19 Phase 6) — targets for manifest parsers and API payloads exist and pass their seed corpus. *Verified by:* `go test ./test/fuzz/...` in CI; nightly `-fuzz` job (S6-T27); demo step 8.
- [ ] **AC 14** (§20.14) — core and tests green on Windows, Linux and macOS; limitations documented in `docs/windows.md` and `docs/projfs.md`. *Verified by:* CI run on `develop` with 3 green OS jobs + ProjFS job (S6-T31); both docs merged (S6-T30); demo step 9.
- [ ] **Security hardening** (§17, `S6-E7`) — whole-system review signed off by `AG-SEC`; no open high/critical finding. *Verified by:* review issue closed; S6-T29 guards green.
- [ ] **Open questions stay open** (SCOPE.md §23 Open questions 5, 6) — benchmark data for Open question 5 is attached for the maintainer; no code hard-codes a choice. *Verified by:* S6-T16 comparison table in the closing PR; `AG-LEAD` review of S6-T12.

---

## 3. Prerequisites

### 3.1 Earlier epics that must be merged

| Epic | What Sprint 6 uses from it |
|---|---|
| `S0-E4` | Git wrapper (`-C`, `GIT_TERMINAL_PROMPT=0`, timeouts) — extended by S6-T05 |
| `S0-E5` | Path canonicalization/confinement, long paths, Job Objects |
| `S0-E6` | Dev Drive / VHDX layer and its interface (simulated VHDX for row 8) |
| `S0-E7` | `cowflow doctor`, including the `Client-ProjFS` check of §7 |
| `S0-E8` | Basic hermetic harness (fake clock, temp Git repo, Git fault injection) and the 3-platform CI |
| `S1-E1` | `WorkspaceProvider` / `Capabilities` / `CowKind` (`Virtual` is used for the first time here) |
| `S1-E2`, `S1-E4` | `git_worktree` and `cow_clone` providers (comparison baseline for AC 22) |
| `S1-E5` | Sparse patterns `<paths> + <always>` — reused by `scope_only` |
| `S1-E6` | Dirty detection, rescue, `destroy --force`, `pending_cleanup` |
| `S1-E8` | `workspace list\|inspect\|destroy` CLI |
| `S1-E9` | Benchmark fixture repository (≥ 1 GB) and benchmark helpers |
| `S2-E2`, `S2-E3`, `S2-E5`, `S2-E7` | Outbox/cursor, intents with `paths`, change manifest parser, sync/abort |
| `S3-E1`, `S3-E2` | MCP (`cowflow_expand_scope`), scoped tokens |
| `S4-E1`…`S4-E4`, `S4-E7`, `S4-E8`, `S4-E9`, `S4-E11` | Daemon lifecycle, API, SSE, heartbeats/`session.lost`, budget pause, reconciliation, service, load test |
| `S5-E6`, `S5-E9` | Stale/revalidation, post-promotion auto-revert |

`AG-LEAD` checks on day 1 that each epic is merged on `develop`. A missing epic blocks only the tasks that list it in *Depends on*.

### 3.2 Machine setup for ProjFS

ProjFS is a Windows optional feature (Windows 10 1809+ / Windows 11). It must be enabled once, from an **elevated** PowerShell:

```powershell
Enable-WindowsOptionalFeature -Online -FeatureName Client-ProjFS -NoRestart
Get-WindowsOptionalFeature -Online -FeatureName Client-ProjFS | Select-Object FeatureName, State, RestartNeeded
# FeatureName : Client-ProjFS
# State       : Enabled
# RestartNeeded : False      # if True, reboot before running ProjFS tests
```

Developers on Linux/macOS still build and test everything: the ProjFS code compiles on all OSes (stubs return `ErrUnsupported`) and the pure logic runs against the simulated ProjFS host.

### 3.3 Open items that affect this sprint (not decided here)

`AG-LEAD` raises these with the maintainer on day 1. Until a decision is recorded in SCOPE.md, the listed tasks work behind an interface/flag and do not choose.

| Item | Source | Blocks / constrains |
|---|---|---|
| **SCOPE.md §23 Open question 5** — ProjFS: enumerate outside the scope as *empty* vs *lazy* | SCOPE.md §23 | S6-T12 implements both behind `ScopePolicy`; S6-T16 produces the benchmark. The *choice* stays with the maintainer. |
| **SCOPE.md §23 Open question 6** — golden tree per `base_sha` vs per branch | SCOPE.md §23 | Not blocking. `projfs` uses no golden tree; its tree index is keyed by `base_sha` because §17 requires the authorized `base_sha`. S6-T06 must not change golden keying. |
| **C1** — Go 1.23+ in spec vs 1.25 in `go.mod` | SPEC-GAPS | None (use `go.mod`). Fuzzing APIs exist in both. |
| **C7** — golden tree location | SPEC-GAPS | Not blocking (no golden for `projfs`); `docs/windows.md` (S6-T30) must not state a location as settled. |
| **C12** — §24.13 never-commit list vs `blocked_patterns` | SPEC-GAPS | S6-T28 reports both lists as-is; does not merge them. |
| **C14** — Defender exclusion "suggest" vs required | SPEC-GAPS | S6-T30 documents the *suggestion* only (§6.4); no code changes Defender. |
| **`U-DATA-5`** — workspace states beyond `ready`, `pending_mount`, `pending_cleanup` | SPEC-GAPS | S6-T09, S6-T21: assert "never `ready`" + compensation, not a specific failure-state name, unless an earlier sprint recorded one. |
| **`U-PLAT-2`** — rescue-before-destroy format and location | SPEC-GAPS | S6-T09 destroy reuses whatever `S1-E6` implemented; if `S1-E6` left it open, the dirty-destroy path of `projfs` is blocked too. |
| **`U-PLAT-5`** — auto-attach of the VHDX at boot: scheduled task vs service | SPEC-GAPS | S6-T23 real-machine variant only; the simulated test is not blocked. |
| **`U-PLAT-8`** — hermetic harness "Git repo on tmpfs" on Windows/macOS | SPEC-GAPS | S6-T17/T18 keep the `S0-E8` fallback; do not change it. |
| **`U-API-2`** — how CLI/MCP detect daemon vs daemonless | SPEC-GAPS | S6-T10: `projfs` needs the daemon (§5.3); reuse the `S4-E1` detection. If still open, the daemonless refusal message is blocked (the daemon path is not). |
| **`U-API-3`** — `/metrics` format | SPEC-GAPS | S6-T14 follows the format `S4-E2` shipped; metric names only. |
| **`U-API-4`** — HTTP status ↔ CLI exit code mapping | SPEC-GAPS | S6-T27 asserts "never 5xx on malformed input", not exact codes. |
| **`U-DATA-3`** — event cursor = ULID? | SPEC-GAPS | S6-T18 (row 3), S6-T25 assert "no gap/no duplicate per consumer" through the store API, not cursor internals. |
| **`U-DATA-4`** — concrete `cross_request.*` event names | SPEC-GAPS | Row 10 assertions use the names `S5-E9` emits. |
| **`U-SCHED-4`** — budget actions other than `pause` | SPEC-GAPS | Row 11 tests only `on_budget_exceeded = "pause"`. |

Gaps found while writing this plan (already in SPEC-GAPS.md; **not decided**):

| Item | Source | Blocks / constrains |
|---|---|---|
| **`U-PLAT-18`** — ProjFS callback timeout value (no config key exists) | SPEC-GAPS | S6-T04, S6-T19: `CallbackTimeout` stays a Guidance constant. |
| **`U-PLAT-19`** — whether projected content applies `.gitattributes` filters (eol, LFS) | SPEC-GAPS | S6-T30 documents content served without filters as a limitation. |
| **`U-PLAT-20`** — meaning of "lazy" in SCOPE.md §23 Open question 5 | SPEC-GAPS | S6-T12 `lazy-outside` is a proposed interpretation, benchmark only. |
| **`U-PLAT-21`** — exact denominator of "bytes of a full worktree" in §20.22 | SPEC-GAPS | S6-T13, S6-T16: `TreeBytes()` (sum of blob sizes at `base_sha`) until decided. |

---

## 4. Concepts you need

**ProjFS virtualization root.** A normal directory that a *provider* process (our daemon) registers with `PrjMarkDirectoryAsPlaceholder` and then serves with `PrjStartVirtualizing`. Programs see a full directory tree, but the contents come from the provider on demand. If the provider is not running, access to not-yet-materialized items fails (it does not hang forever). The root must be on NTFS or ReFS (§6.5). <https://learn.microsoft.com/windows/win32/projfs/projected-file-system>

**Placeholders, hydration, full files.** Each item in a root has an on-disk state. *Virtual*: exists only in the provider's answer. *Placeholder*: the OS wrote metadata (name, size, times) after a lookup — almost no bytes. *Hydrated placeholder*: the content was read once, so ProjFS asked `GetFileData` and cached the bytes on disk. *Dirty placeholder*: metadata changed. *Full file*: the program wrote to it; it is now a normal file that ProjFS no longer manages. *Tombstone*: a deleted projected item. `PrjGetOnDiskFileState` returns these flags. "Materialized bytes" = bytes of hydrated placeholders + full files that came from the base tree. <https://learn.microsoft.com/windows/win32/projfs/file-state>

**Callbacks.** ProjFS calls the provider through a `PRJ_CALLBACKS` table: `StartDirectoryEnumeration` / `GetDirectoryEnumeration` / `EndDirectoryEnumeration` (list a folder, filling a buffer with `PrjFillDirEntryBuffer`), `GetPlaceholderInfo` (metadata for one path, answered with `PrjWritePlaceholderInfo`), `GetFileData` (bytes, answered with `PrjWriteFileData`), optional `Notification` (create/rename/delete/modify) and `CancelCommand`. A callback may return `ERROR_IO_PENDING` and finish later with `PrjCompleteCommand` — this is how we implement timeouts. <https://learn.microsoft.com/windows/win32/projfs/projfs-programming-guide>

**ProjFS from Go without cgo.** `CGO_ENABLED=0` (§16) means no C headers. We load `ProjectedFSLib.dll` with `windows.NewLazySystemDLL` and call procs with `proc.Call`. Callbacks are Go functions turned into C function pointers with `windows.NewCallback`. Constraints: (1) each argument must be pointer-sized and there is one pointer-sized result (HRESULT as `uintptr`); (2) the runtime can create only a limited number of callbacks per process ("at least 1024") and never frees them — so we create the table **once per process**, never per workspace; (3) callbacks arrive on ProjFS thread-pool threads, many at once, so handlers must be goroutine-safe, must not panic (a panic kills the daemon and freezes every agent's I/O), and must not block long; (4) we must not hand Go pointers to the OS as context — we pass an integer key into a registry; (5) the `Client-ProjFS` feature must be enabled or the DLL procs fail. <https://pkg.go.dev/golang.org/x/sys/windows#NewCallback>

**fsmonitor / `core.fsmonitor`.** `git status` normally `lstat`s every file in the index. In a projected root that is slow and can force hydration. `core.fsmonitor=true` (Git ≥ 2.37 on Windows; we require ≥ 2.40) starts Git's built-in file-system monitor daemon, which tells `git status` which paths changed since the last query so Git can skip the rest. §6.5 says v1 sets `core.fsmonitor=true` on `projfs` workspaces (config `[workspace.projfs] fsmonitor = true`). The monitor is a background process per worktree; it must be stopped before destroy or its handles block deletion. <https://git-scm.com/docs/git-fsmonitor--daemon>

**`scope_only`.** `[workspace.projfs] scope_only = true` makes enumeration outside the intent scope (`paths` + the sparse `always` set) return empty (§6.5). Tools that walk the whole tree (`go build ./...`, indexers, Defender) then cannot hydrate everything. SCOPE.md §23 Open question 5 asks whether "outside the scope" should be *empty* or *lazy*; we implement both behind an interface and only measure.

**Materialization metrics.** The ratio `materialized bytes / bytes of a full worktree of base_sha`. The denominator is computed from `git ls-tree -r -l <base_sha>` (sum of blob sizes). The numerator comes from two independent sources that must agree within a tolerance: a live counter of bytes served by `GetFileData`, and an on-disk scan with `PrjGetOnDiskFileState`. §21 lists "materialized bytes (projfs)" in `/v1/status` and `/metrics`.

**Fault injection.** Making a dependency fail on purpose, deterministically: the harness wraps Git, the filesystem, the clock, the VHDX layer and the ProjFS host with decorators that return an exit code, a timeout, truncated output, `ENOSPC`, a sharing violation or a hang when a rule matches. Production code depends on interfaces, so tests swap in the faulty version (§18.1).

**Fuzzing with `go test -fuzz`.** Native Go fuzzing mutates inputs to find panics and broken invariants. A target is `func FuzzX(f *testing.F)` with `f.Add(seed...)` and `f.Fuzz(func(t *testing.T, in []byte) {...})`. A plain `go test` runs only the seeds (fast, in CI); `go test -run=^$ -fuzz=^FuzzX$ -fuzztime=10m ./pkg` explores. Crashers are saved in `testdata/fuzz/FuzzX/` and become permanent regression seeds. Only one target per package per `-fuzz` run. <https://go.dev/doc/security/fuzz/>

**Stress testing.** Running many concurrent actors (20+ simulated sessions, §20.15) for a long time while faults fire randomly, then checking global invariants: no lost or duplicated events, no partial `ready`, no leaked processes or leases. Stress tests sit behind a build tag so normal `go test` stays fast. Use a seeded PRNG and print the seed so a failure can be replayed.

**Stuck handles.** On Windows a file opened without `FILE_SHARE_DELETE` cannot be deleted; `RemoveAll` fails with `ERROR_SHARING_VIOLATION` or `ERROR_ACCESS_DENIED`. Editors, antivirus, the Git fsmonitor daemon or a leftover build process cause it. §7/§18 require `pending_cleanup` with retry and diagnostics, never a crash and never a silent leak. <https://learn.microsoft.com/windows/win32/fileio/creating-and-opening-files>

**Disk-full simulation.** Real disk-full is slow and dangerous to provoke on a developer machine. The harness `FS` decorator returns `ENOSPC` (Windows: `ERROR_DISK_FULL`) after N bytes or on a matching path. The rule we test: explicit failure, never a partial `ready`, and compensation removes what was created (§18). One optional platform test uses a tiny real volume.

---

## 5. Agents and lanes

### 5.1 Who does what

| Agent | Tasks |
|---|---|
| `AG-LEAD` | S6-T01 (contracts), reviews, S6-T32 (demo with `AG-QA`) |
| `AG-PLAT` | S6-T02, S6-T03, S6-T04, S6-T30 |
| `AG-GIT` | S6-T05 (cross-request: persistent `cat-file` lives in `internal/git`) |
| `AG-STORE` | S6-T08 (cross-request: migration) |
| `AG-WS` | S6-T06, S6-T07, S6-T09, S6-T11, S6-T12, S6-T13 |
| `AG-API` | S6-T10, S6-T14 |
| `AG-CLI` | S6-T15 |
| `AG-QA` | S6-T16…S6-T27, S6-T29, S6-T31, S6-T32 |
| `AG-SEC` | S6-T28; reviewer of every `Security: yes` task |

Guidance: `AG-QA` carries the largest load. `AG-LEAD` may staff the role with two agents, shown below as lanes QA-a (harness and §18 rows) and QA-b (fuzz, stress, CI, benchmark). They remain one role for ownership and review.

### 5.2 Lane diagram (working days)

```text
Day        01  02  03  04  05  06  07  08  09  10  11  12  13  14  15
AG-LEAD    T01 ─── reviews / SPEC-GAPS follow-up ───────────────── T32 T32
AG-PLAT    T02 T02 T03 T03 T04 T04 T04 T04 T30 T30 T30 T30 T30 T30 ·
AG-GIT     T05 T05 T05  ·   ·   ·   ·   ·   ·   ·   ·   ·   ·   ·   ·
AG-STORE    ·  T08 T08  ·   ·   ·   ·   ·   ·   ·   ·   ·   ·   ·   ·
AG-WS      T06 T06 T06 T07 T07 T07 T09 T09 T09 T11 T12 T13 fix fix fix
                                                   T12
AG-API      ·   ·   ·   ·   ·   ·   ·   ·   ·  T10 T10 T14 T14  ·   ·
AG-CLI      ·   ·   ·   ·   ·   ·   ·   ·   ·   ·   ·   ·  T15  ·   ·
QA-a       T17 T17 T17 T18 T18 T21 T19 T19 T23 T22 T20 T20 T24 T32 T32
QA-b       T26 T26 T27 T27 T27 T31 T25 T25 T29 T29 T16 T16 T16 T16 T32
AG-SEC      ·   ·  T28 ──────────── rolling review ─────────── T28  ·
```

`T16` on days 11–12 builds the benchmark against the simulated host; days 13–14 are real measurements on a ProjFS-enabled Windows machine.

### 5.3 Critical path

`T01 → T02 → T03 → T04 → T09 (real host, day 9) → T12 → T13 → T16 (real measurement) → T32`. It has no slack: any slip in the spike or the host binding moves the AC 22 measurement. Mitigation: `T06`, `T07`, `T09`, `T12`, `T13` and `T16` are written against the simulated host first (S6-T01 fake, S6-T17 simulator), so only real-host integration sits on the path.

---

## 6. Contracts

Frozen on day 1 by S6-T01. Consumers code against these and use the fakes until the real implementation lands. Signatures are illustrative Go 1.25; names in backticks from SCOPE.md are fixed, other names are Guidance agreed here.

### 6.1 ProjFS host ↔ projection (`AG-PLAT` → `AG-WS`)

Package `internal/workspace/projfs` (§16.1: host, callbacks, blobstore). `contract.go` has no build tag. `AG-PLAT` implements `Host` in `host_windows.go` (+ `host_other.go` returning `ErrUnsupported`). `AG-WS` implements `Projection`. The host translates OS callbacks into `Projection` calls; the projection never sees a Windows type.

```go
package projfs

// RelPath is slash-separated, relative to the virtualization root,
// already validated by CleanRel (no "..", no ":", no NUL, not absolute).
type RelPath string

type ObjectID string     // git object id, lowercase hex
type InstanceID [16]byte // virtualization instance GUID, persisted per root
type EnumID [16]byte     // enumeration session GUID from ProjFS

type PlaceholderInfo struct {
	Path     RelPath
	IsDir    bool
	Size     int64
	ModTime  time.Time // Guidance: commit time of base_sha, deterministic
	ObjectID ObjectID  // blob id; empty for directories
}

type DirEntry struct {
	Name    string
	IsDir   bool
	Size    int64
	ModTime time.Time
}

// DirFiller wraps PrjFillDirEntryBuffer. Add returns false when the buffer is full.
type DirFiller interface{ Add(e DirEntry) bool }

// DataWriter wraps PrjAllocateAlignedBuffer + PrjWriteFileData.
type DataWriter interface{ WriteAt(p []byte, off uint64) error }

type NotificationKind uint8

const (
	NotifyNewFile NotificationKind = iota + 1
	NotifyOverwritten
	NotifyModifiedOnClose
	NotifyDeleted
	NotifyRenamed
)

type Notification struct {
	Kind  NotificationKind
	Path  RelPath
	Dest  RelPath // renames only
	IsDir bool
}

// Projection is the pure-Go side. Implemented by AG-WS. Must be goroutine-safe.
type Projection interface {
	StartEnumeration(ctx context.Context, id EnumID, dir RelPath) error
	GetEnumeration(ctx context.Context, id EnumID, dir RelPath, pattern string, restart bool, fill DirFiller) error
	EndEnumeration(ctx context.Context, id EnumID) error
	GetPlaceholderInfo(ctx context.Context, p RelPath) (PlaceholderInfo, error)
	GetFileData(ctx context.Context, p RelPath, want ObjectID, off uint64, n uint32, w DataWriter) error
	Notify(ctx context.Context, n Notification) error
}

// Errors the projection returns; the host maps them to HRESULTs.
var (
	ErrNotFound    = errors.New("projfs: not found")             // ERROR_FILE_NOT_FOUND
	ErrOutsideBase = errors.New("projfs: object outside base_sha") // ERROR_ACCESS_DENIED
	ErrTimeout     = errors.New("projfs: callback timeout")      // ERROR_TIMEOUT
	ErrUnsupported = errors.New("projfs: unsupported on this platform")
	ErrUnavailable = errors.New("projfs: Client-ProjFS not available")
)

// Host is the OS side. Implemented by AG-PLAT (Windows), simulated by AG-QA.
type Host interface {
	Available(ctx context.Context) error // ErrUnsupported | ErrUnavailable | nil
	MarkRoot(ctx context.Context, root string, id InstanceID) error
	Start(ctx context.Context, root string, id InstanceID, p Projection, o HostOptions) (Session, error)
}

type HostOptions struct {
	CallbackTimeout   time.Duration // Guidance constant until SPEC-GAPS U-PLAT-18 is decided (see §3.3)
	PoolThreads       uint32
	ConcurrentThreads uint32
	OnTimeout         func(op string, p RelPath) // provider schedules a virtualization restart
}

type OnDiskState uint8 // bit flags mirroring PRJ_FILE_STATE

const (
	StatePlaceholder OnDiskState = 1 << iota
	StateHydrated
	StateDirty
	StateFull
	StateTombstone
)

type Session interface {
	Stop(ctx context.Context) error                    // PrjStopVirtualizing; idempotent
	ClearNegativePathCache(ctx context.Context) (uint32, error)
	FileState(ctx context.Context, p RelPath) (OnDiskState, error) // PrjGetOnDiskFileState
	Counters() HostCounters
}

type HostCounters struct {
	BytesServed, PlaceholdersWritten, CallbackTimeouts, CallbackErrors uint64
}
```

### 6.2 Backing store at `base_sha` (`AG-GIT` → `AG-WS`)

```go
// internal/git (AG-GIT): persistent `git cat-file --batch`, one per repository.
type CatFile interface {
	// Read returns the object only if its type is "blob". Never accepts a rev expression,
	// only a full object id (validated hex).
	ReadBlob(ctx context.Context, oid string) ([]byte, error)
	Close() error
}
func (g *Git) OpenCatFile(ctx context.Context, repoDir string) (CatFile, error)

type TreeEntry struct {
	Path string // slash-separated
	Mode string // "100644", "100755", "120000", "160000", "040000"
	OID  string
	Size int64  // -1 for trees and gitlinks
}
// LsTree runs `git ls-tree -r -t -l -z <commit>` and parses it.
func (g *Git) LsTree(ctx context.Context, repoDir, commit string) ([]TreeEntry, error)
```

```go
// internal/workspace/projfs (AG-WS): the only path from a callback to bytes.
type BlobStore interface {
	BaseSHA() string
	Lookup(p RelPath) (TreeEntry, bool) // case-insensitive, from the base_sha tree index
	List(dir RelPath) []TreeEntry       // direct children, sorted with PrjFileNameCompare order
	// ReadBlob resolves p in the base_sha tree; returns ErrOutsideBase if want != entry.OID.
	ReadBlob(ctx context.Context, p RelPath, want ObjectID) ([]byte, error)
	TreeBytes() int64 // denominator of the materialization ratio
}
```

Rule (§17): a blob is read **only** through `Lookup` on the tree index of the workspace's `base_sha`. The object id stored in a placeholder is a check, never an input to `cat-file`.

### 6.3 Scope policy (§6.5 `scope_only`, SCOPE.md §23 Open question 5 stays open)

```go
// internal/workspace/projfs (AG-WS)
type ScopePolicy interface {
	Name() string // "full" | "empty-outside" | "lazy-outside"
	// Listed: does this entry appear when its parent directory is enumerated?
	Listed(p RelPath, isDir bool) bool
	// Resolvable: does a direct lookup by exact path succeed?
	Resolvable(p RelPath, isDir bool) bool
	Update(paths []string) // new intent scope (expand_scope)
}

func NewScopePolicy(scopeOnly bool, variant OutOfScopeVariant, scope Scope) ScopePolicy

type OutOfScopeVariant uint8

const (
	OutOfScopeEmpty OutOfScopeVariant = iota // §6.5 text: "returns empty"
	OutOfScopeLazy                           // SCOPE.md §23 Open question 5 candidate, benchmark only
)
```

- `scope_only = false` → `full` (everything listed and resolvable).
- `scope_only = true` → `empty-outside` (outside: not listed, not resolvable). This is what §6.5 literally says.
- `lazy-outside` (outside: not listed, but resolvable by exact path) exists only as a code path selectable from tests/benchmarks via `projfs.Options.OutOfScope`. **No config key is added** (AGENTS.md: no config keys the spec does not list). SCOPE.md does not define "lazy"; this interpretation is proposed for the benchmark and the maintainer confirms it when deciding SCOPE.md §23 Open question 5.

### 6.4 Provider registration behind the flag (`AG-WS` → `AG-CLI`, `AG-API`)

```go
// internal/workspace/registry.go (AG-WS)
type Deps struct {
	Git      *git.Git
	Store    WorkspaceStore
	Platform platform.FSCaps
	ProjFS   projfs.Host // nil on non-Windows or when not wired
	Clock    Clock
	Hooks    *Hooks      // fault-injection seam, see 6.6
}

// NewRegistry registers cow_clone, git_worktree (and container if built), and
// projfs ONLY when cfg.ProjFS.Enabled && runtime.GOOS == "windows" && deps.ProjFS != nil.
func NewRegistry(ctx context.Context, cfg config.Workspace, deps Deps) (*Registry, error)

// Select: "auto" → cow_clone → git_worktree. Never projfs (§6.3, §23 decision 10).
// "projfs" when not registered → ErrProviderDisabled with a message naming
// `[workspace.projfs] enabled`.
func (r *Registry) Select(ctx context.Context, requested string, root string) (WorkspaceProvider, error)

var ErrProviderDisabled = errors.New("workspace: provider not enabled")
var ErrRequiresDaemon   = errors.New("workspace: provider requires the daemon") // §5.3
```

### 6.5 Metrics (`AG-WS` → `AG-API`, `AG-CLI`, `AG-QA`)

```go
// internal/workspace (AG-WS)
type MaterializationStats struct {
	WorkspaceID      string
	BaseSHA          string
	TreeFiles        int64
	TreeBytes        int64 // "bytes of a full worktree": sum of blob sizes at base_sha
	PlaceholderFiles int64
	HydratedFiles    int64
	HydratedBytes    int64
	FullFiles        int64 // base files converted to full by a write
	FullBytes        int64
	NewFileBytes     int64 // files the agent created; reported, not in the ratio
	BytesServed      uint64 // live counter from GetFileData
	CallbackTimeouts uint64
	Restarts         uint64 // virtualization restarts
	MeasuredAt       time.Time
	Mode             MeasureMode
}

func (s MaterializationStats) Ratio() float64 // (HydratedBytes+FullBytes)/TreeBytes

type MeasureMode uint8

const (
	MeasureLive MeasureMode = iota // counters only, O(1), used by /metrics
	MeasureScan                    // walks the root with FileState, used by inspect/bench
)

type MaterializationReporter interface {
	Materialization(ctx context.Context, workspaceID string, m MeasureMode) (MaterializationStats, error)
}
```

`WorkspaceStatus` (from `S1-E1`) gains an optional `Materialization *MaterializationStats` field, set only for `projfs`.

### 6.6 Harness fault-injection hooks per §18 row (`AG-QA` ↔ owners)

```go
// test/harness (AG-QA)
type Row string

const (
	RowAgentCrash        Row = "agent-crash"
	RowDaemonRestart     Row = "daemon-restart"
	RowLostEvent         Row = "lost-event"
	RowSyncConflict      Row = "sync-conflict"
	RowTargetAdvances    Row = "target-advances"
	RowDiskFull          Row = "disk-full-provider-fails"
	RowStuckHandle       Row = "stuck-handle"
	RowVHDXNotMounted    Row = "vhdx-not-mounted"
	RowProjFSTimeout     Row = "projfs-callback-timeout"
	RowPostPromotionFail Row = "post-promotion-fails"
	RowLLMBudget         Row = "llm-budget"
)

type Faults struct {
	Git    *GitFaults        // S0-E8: exit code, timeout, truncated output
	FS     *FSFaults         // ENOSPC after N bytes; sharing violation on remove
	Crash  *CrashPoints      // named crash points, e.g. "outbox.after_write_before_delivery"
	VHDX   *SimVHDX          // S0-E6 interface: Mounted(false), AttachErr
	ProjFS *projfssim.Host   // in-memory callbacks, latency/hang per op and path
	Proc   *ProcFaults       // kill a runner/agent process tree
	LLM    *LLMUsageInjector // push usage to 100% of a budget
	Clock  *FakeClock
}

func (f *FSFaults) DiskFullAfter(bytes int64, match string)
func (f *FSFaults) LockForDelete(path string) (release func()) // stuck handle
func (c *CrashPoints) Arm(name string)                       // next Hit(name) aborts the actor
func (h *projfssim.Host) Delay(op string, match string, d time.Duration)
func (h *projfssim.Host) Hang(op string, match string) (release func())
```

Production seam (owned by each package, agreed here): components receive a `*workspace.Hooks` / `*store.Hooks` struct with a `Hit(ctx, name string) error` method. It is a no-op when nil. No global mutable state (`go-conventions`).

Crash points used this sprint (names fixed here, Guidance): `workspace.after_tree_before_ready`, `workspace.projfs.after_mark_before_start`, `outbox.after_write_before_delivery`, `sync.after_merge_before_record`, `destroy.after_rescue_before_remove`.

### 6.7 Fakes and simulators

| Interface | Provider | Fake for early consumers | Full simulator |
|---|---|---|---|
| `projfs.Host` | `AG-PLAT` (S6-T04) | `projfs/fakehost_test.go` (S6-T01): map-backed, synchronous | `test/harness/projfssim` (S6-T17): drives callbacks like the OS, keeps on-disk states in memory, injects latency/hangs |
| `git.CatFile`, `git.LsTree` | `AG-GIT` (S6-T05) | `projfs/fakecat_test.go` (S6-T01): map oid→bytes | real Git in harness repos |
| `projfs.Projection` | `AG-WS` (S6-T07) | — | used by both hosts |
| `MaterializationReporter` | `AG-WS` (S6-T13) | static struct (S6-T01) for `AG-API`/`AG-CLI` | — |
| Harness hooks | `AG-QA` (S6-T17) | no-op `Hooks` | — |

---

## 7. Tasks

### 7.1 §18 coverage matrix

The §18 table has 11 rows. Rows already demonstrated by an earlier epic are audited (and tagged) in S6-T18; rows that lack coverage get their own task.

| # | §18 row | Expected behavior | Earlier coverage | Sprint 6 task |
|---|---|---|---|---|
| 1 | Agent crashes | `session.lost`, workspace/logs preserved, retry | `S4-E4` | audit in S6-T18 (+ real kill in S6-T24) |
| 2 | Daemon restarts | reconcile SQLite × processes × worktrees × **ProjFS roots**; idempotent `op_id`; resume outbox | `S4-E8` (no ProjFS roots) | **S6-T20** |
| 3 | Lost event | cursor + redelivery + dedup | `S2-E2`, `S4-E3`, `S4-E8` | audit in S6-T18 |
| 4 | Sync conflict | nothing overwritten; `conflict` with files; `abort` restores | `S2-E7` | audit in S6-T18 |
| 5 | Target advances | `stale` → revalidates | `S5-E6` | audit in S6-T18 |
| 6 | Disk full / provider fails | explicit failure; never partial `ready`; compensation | partial (`S1-E4`) | **S6-T21** |
| 7 | Stuck handle | `pending_cleanup` with retry | partial (`S1-E6`) | **S6-T22** |
| 8 | VHDX not mounted | `pending_mount`; auto-attach; never silently NTFS | partial (`S0-E6`) | **S6-T23** |
| 9 | ProjFS callback timeout | error to the agent, not a hang; daemon restarts virtualization | none | **S6-T19** |
| 10 | Post-promotion fails | automatic revert + requests + lead | `S5-E9` | audit in S6-T18 |
| 11 | LLM budget | pause + lead | `S4-E7` | audit in S6-T18 |

If the S6-T18 audit finds a row 1/3/4/5/10/11 test missing or not in the harness, `AG-LEAD` adds a task `S6-T33+` to this document in the same PR.

---

### S6-T01 — Freeze Sprint 6 contracts and fakes

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-LEAD` | `AG-PLAT`, `AG-WS`, `AG-QA`, `AG-SEC` | `S6-E1` | `S1-E1`, `S0-E8` | `chore/sprint6-contracts` | yes |

**Skills:** `cow-workspaces`, `windows-platform`, `testing-harness`, `go-conventions`, `gitflow`   **Spec:** §6.2, §6.5, §17, §18.1
**Files:** `internal/workspace/projfs/contract.go`, `internal/workspace/projfs/fakehost_test.go`, `internal/workspace/projfs/fakecat_test.go`, `internal/workspace/registry.go` (signatures only), `internal/workspace/materialization.go` (types only), `test/harness/rows.go`

**Why / what** — Six roles meet at ProjFS and the harness. If each invents its own types, day 10 is spent reconciling them. This PR puts the §6 contracts in code (types and interfaces only, bodies `panic("not implemented")` are **not** allowed — return `ErrUnsupported`) so every lane compiles from day 2.

**Steps**
1. `git switch develop && git pull --ff-only && git switch -c chore/sprint6-contracts`.
2. Copy §6.1–§6.6 of this document into the listed files. Package doc comment in `contract.go` states the §17 rule.
3. Write the fakes: `fakeHost` calls the `Projection` synchronously from test code; `fakeCat` is `map[string][]byte`.
4. Add `test/harness/rows.go` with the `Row` constants and `AllRows() []Row` (11 entries, §18 order).
5. Cross-compile: `for os in windows linux darwin; do GOOS=$os CGO_ENABLED=0 go vet ./...; done`.
6. Record the open items of §3.3 as comments on the kickoff issue; ask the maintainer about `U-PLAT-18`…`U-PLAT-21`.
7. `git commit -S -s -m "chore(workspace): add sprint 6 projfs and harness contracts"`; PR to `develop`.

**Tests** — `go test ./internal/workspace/projfs/...` compiles and runs an empty fake-host test; `go vet` on 3 GOOS.

**Acceptance**
- [ ] All §6 types exist with the names above; no Windows import in `contract.go`.
- [ ] Builds with `CGO_ENABLED=0` for windows, linux, darwin.
- [ ] Every owner in §5.1 approved the PR.

**Pitfalls** — Adding fields "we might need": every field is a promise. Putting `windows.GUID` in the contract (breaks Linux/macOS builds; use `[16]byte`).

---

### S6-T02 — ProjFS feasibility spike (time-boxed, 2 days)

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-PLAT` | `AG-LEAD`, `AG-WS` | `S6-E1` | — | `docs/projfs-spike-notes` | no |

**Skills:** `windows-platform`, `cow-workspaces`   **Spec:** §6.5, §16, SCOPE.md §23 Open question 5
**Files:** `docs/projfs.md` (draft section "Spike findings"); throw-away code in the scratch area, **not** merged

**Why / what** — Several facts decide the design and are not in the spec: the order of `git worktree add` vs `PrjMarkDirectoryAsPlaceholder`; whether `git status` hydrates; whether Git's built-in fsmonitor works in a projected root; how a root is deleted. A two-day prototype answers them before we write production code.

**Steps**
1. Enable the feature (§3.2). Write a ~200-line Go program: `NewLazySystemDLL("ProjectedFSLib.dll")`, `PrjMarkDirectoryAsPlaceholder`, `PrjStartVirtualizing` with three `NewCallback` handlers serving a fixed in-memory tree.
2. Answer and record, with commands and timings:
   1. Can `git worktree add --no-checkout -b spike <dir> <sha>` run **before** marking `<dir>` as the root, leaving `<dir>\.git` as a full file that ProjFS ignores?
   2. Does `git read-tree <sha>` (index only, no `-u`) followed by `git status` hydrate in-scope files? How many bytes are served?
   3. With `core.fsmonitor=true`, does `git fsmonitor--daemon` start in a projected root? Second `git status` time and bytes served.
   4. Does `git sparse-checkout set` mark out-of-scope entries `skip-worktree` without writing files when those paths are not listed?
   5. Destroy order: `git fsmonitor--daemon stop` → `PrjStopVirtualizing` → `git worktree remove --force` → `os.RemoveAll`. Which step fails, with which error?
   6. Symlinks (mode `120000`) and gitlinks (`160000`) — what does each placeholder option produce?
   7. What does a reading process see when a `GetFileData` completes with `ERROR_TIMEOUT` after `ERROR_IO_PENDING`?
3. Write the answers in `docs/projfs.md` under "Spike findings" with Windows build number and Git version.
4. Open a PR with the doc only; delete the prototype.

**Tests** — none (spike). Findings must be reproducible from the doc.

**Acceptance**
- [ ] All seven questions answered with evidence.
- [ ] `AG-WS` confirms the S6-T09 create/destroy sequence from the findings.

**Pitfalls** — Letting the spike grow into the product. Forgetting `-NoRestart` reboot needs. Testing in a folder under OneDrive or on a network drive (not supported by ProjFS or fsmonitor).

---

### S6-T03 — `ProjectedFSLib.dll` binding (procs, structs, HRESULTs)

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-PLAT` | `AG-LEAD`, `AG-WS`, `AG-SEC` | `S6-E1` | S6-T01, S6-T02 | `feat/projfs-dll-binding` | yes |

**Skills:** `windows-platform`, `go-conventions`, `security-review`   **Spec:** §6.5, §16
**Files:** `internal/workspace/projfs/prjlib_windows.go`, `internal/workspace/projfs/prjlib_windows_test.go`, `internal/workspace/projfs/hresult.go`

**Why / what** — A thin, typed layer over the DLL, so the host (S6-T04) never calls `proc.Call` with raw numbers. No logic here.

**Steps**
1. Declare procs lazily:
   ```go
   //go:build windows
   var (
   	modPrj                        = windows.NewLazySystemDLL("ProjectedFSLib.dll")
   	procPrjStartVirtualizing      = modPrj.NewProc("PrjStartVirtualizing")
   	procPrjStopVirtualizing       = modPrj.NewProc("PrjStopVirtualizing")
   	procPrjMarkDirectoryAsPlaceholder = modPrj.NewProc("PrjMarkDirectoryAsPlaceholder")
   	procPrjWritePlaceholderInfo   = modPrj.NewProc("PrjWritePlaceholderInfo")
   	procPrjFillDirEntryBuffer     = modPrj.NewProc("PrjFillDirEntryBuffer")
   	procPrjWriteFileData          = modPrj.NewProc("PrjWriteFileData")
   	procPrjAllocateAlignedBuffer  = modPrj.NewProc("PrjAllocateAlignedBuffer")
   	procPrjFreeAlignedBuffer      = modPrj.NewProc("PrjFreeAlignedBuffer")
   	procPrjCompleteCommand        = modPrj.NewProc("PrjCompleteCommand")
   	procPrjClearNegativePathCache = modPrj.NewProc("PrjClearNegativePathCache")
   	procPrjGetOnDiskFileState     = modPrj.NewProc("PrjGetOnDiskFileState")
   	procPrjFileNameMatch          = modPrj.NewProc("PrjFileNameMatch")
   	procPrjFileNameCompare        = modPrj.NewProc("PrjFileNameCompare")
   )

   func loadErr() error {
   	if err := modPrj.Load(); err != nil {
   		return fmt.Errorf("%w: %v", ErrUnavailable, err)
   	}
   	return procPrjStartVirtualizing.Find()
   }
   ```
2. Mirror the structs with exact layout (Windows is LLP64; pointers 8 bytes on amd64/arm64): `prjCallbacks` (8 function pointers), `prjCallbackData`, `prjPlaceholderVersionInfo` (`ProviderID [128]byte`, `ContentID [128]byte`), `prjFileBasicInfo`, `prjPlaceholderInfo`, `prjStartVirtualizingOptions`, `prjNotificationMapping`.
   ```go
   type prjCallbackData struct {
   	Size                           uint32
   	Flags                          uint32
   	NamespaceVirtualizationContext uintptr
   	CommandID                      int32
   	FileID                         windows.GUID
   	DataStreamID                   windows.GUID
   	FilePathName                   *uint16
   	VersionInfo                    *prjPlaceholderVersionInfo
   	TriggeringProcessID            uint32
   	TriggeringProcessImageFileName *uint16
   	InstanceContext                uintptr
   }
   ```
3. `hresult.go` (no build tag): `hrFromWin32(code uint32) uintptr` = `0x80070000 | code&0xFFFF`; constants `sOK`, `eFail` (0x80004005), `hrIOPending` (997), `hrInsufficientBuffer` (122), `hrFileNotFound` (2), `hrAccessDenied` (5), `hrTimeout` (1460); `hrFromErr(error) uintptr` mapping §6.1 errors.
4. Typed wrappers: `prjMarkRoot(root string, id *windows.GUID) error`, `prjWritePlaceholder(ctx uintptr, rel string, info *prjPlaceholderInfo) error`, etc. Every wrapper converts paths with `windows.UTF16PtrFromString` and checks the returned HRESULT (`int32(r) < 0` → error).

**Tests** — `unsafe.Sizeof`/`Offsetof` table for every struct against values computed from the SDK header layout and written in the test with a comment (`// from projectedfslib.h, x64 and arm64`); HRESULT mapping table; `loadErr` returns `ErrUnavailable` when the DLL is absent (run on a CI image without the feature or skip with reason).

**Acceptance**
- [ ] All offsets/sizes asserted; tests run on `windows/amd64` (and `arm64` if a runner exists, else documented).
- [ ] No exported Windows types; package still builds on Linux/macOS.

**Pitfalls** — `BOOLEAN` is 1 byte but `PRJ_FILE_BASIC_INFO` pads to 8 before `FileSize`. `proc.Call` returns `uintptr`; HRESULT is a signed 32-bit value — compare `int32(r) < 0`, not `r != 0`. Ignoring the third `err` result of `Call` is correct only after checking HRESULT (it holds `GetLastError`, often stale).

---

### S6-T04 — ProjFS host: lifecycle, callback trampolines, timeouts

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-PLAT` | `AG-LEAD`, `AG-WS`, `AG-SEC` | `S6-E1` | S6-T03 | `feat/projfs-host` | yes |

**Skills:** `windows-platform`, `go-conventions`, `security-review`   **Spec:** §6.5, §17, §18
**Files:** `internal/workspace/projfs/host_windows.go`, `internal/workspace/projfs/trampoline_windows.go`, `internal/workspace/projfs/host_other.go`, `internal/workspace/projfs/host_windows_test.go`

**Why / what** — Implements `Host`/`Session` (§6.1). This is where the cgo-less constraints live: one callback table per process, a registry instead of Go pointers, recovery from panics, and asynchronous completion so a slow backing store produces an error, not a frozen agent (§18 row 9).

**Steps**
1. **Callback table, once per process.** `windows.NewCallback` allocations are never freed and limited in number:
   ```go
   var cbOnce sync.Once
   var cbTable prjCallbacks

   func callbacks() *prjCallbacks {
   	cbOnce.Do(func() {
   		cbTable = prjCallbacks{
   			StartDirectoryEnumeration: windows.NewCallback(tStartEnum),
   			EndDirectoryEnumeration:   windows.NewCallback(tEndEnum),
   			GetDirectoryEnumeration:   windows.NewCallback(tGetEnum),
   			GetPlaceholderInfo:        windows.NewCallback(tGetPlaceholder),
   			GetFileData:               windows.NewCallback(tGetFileData),
   			Notification:              windows.NewCallback(tNotify),
   			CancelCommand:             windows.NewCallback(tCancel),
   		}
   	})
   	return &cbTable
   }
   ```
2. **Registry.** `InstanceContext` is an integer key, never a Go pointer. This is the one sanctioned package-level state (document it in a comment; `AG-LEAD` approves):
   ```go
   type registry struct {
   	mu   sync.RWMutex
   	next uintptr
   	m    map[uintptr]*session
   }
   ```
3. **Trampolines.** Every one: `defer recover()` → `eFail`; look up the session; convert `FilePathName` with `windows.UTF16PtrToString`, then `CleanRel` (reject `..`, `:`, NUL, drive letters, leading separators; convert `\` to `/`). Unknown session → `hrFromWin32(ERROR_FILE_SYSTEM_VIRTUALIZATION_UNAVAILABLE)`.
4. **Synchronous ops.** Enumeration and placeholder info are answered from the in-memory tree index (S6-T06) synchronously: they do no I/O. Enumeration: keep per-`EnumID` cursor state; on `restart` reset; fill with `PrjFillDirEntryBuffer`; if the **first** entry does not fit return `hrInsufficientBuffer`, otherwise stop and resume next call. Match `pattern` with `PrjFileNameMatch`.
5. **Asynchronous `GetFileData` with deadline.**
   ```go
   func (s *session) getFileData(d *prjCallbackData, off uint64, n uint32) uintptr {
   	rel, err := relFromWire(d.FilePathName)
   	if err != nil {
   		return hrAccessDenied
   	}
   	want := contentIDFrom(d.VersionInfo) // copy now: d is only valid during the callback
   	cmd, stream := d.CommandID, d.DataStreamID
   	if !s.sem.TryAcquire() { // bounded concurrency = ConcurrentThreads
   		return hrFromWin32(windows.ERROR_BUSY)
   	}
   	ctx, cancel := context.WithTimeout(s.ctx, s.opts.CallbackTimeout)
   	s.pending.Store(cmd, cancel) // CancelCommand cancels it
   	go func() {
   		defer s.sem.Release()
   		defer cancel()
   		err := s.proj.GetFileData(ctx, rel, want, off, n, s.writer(stream))
   		if errors.Is(err, context.DeadlineExceeded) {
   			err = ErrTimeout
   			s.counters.timeouts.Add(1)
   			s.opts.OnTimeout("GetFileData", rel)
   		}
   		s.complete(cmd, hrFromErr(err)) // sync.Once per command: exactly one PrjCompleteCommand
   	}()
   	return hrIOPending
   }
   ```
6. **Writer.** `PrjAllocateAlignedBuffer` (C memory) → copy Go bytes into `unsafe.Slice` → `PrjWriteFileData` → `PrjFreeAlignedBuffer`. Chunk at 1 MiB (Guidance). Count `BytesServed`.
7. **Lifecycle.** `MarkRoot`: verify the volume is NTFS or ReFS via `GetVolumeInformation` (§6.5), then `PrjMarkDirectoryAsPlaceholder(root, nil, nil, &guid)`. `Start`: `PrjStartVirtualizing` with options (pool/concurrent threads, notification mappings; suppress notifications under `.git`). `Stop`: cancel all pending commands, complete them with an error, then `PrjStopVirtualizing`; idempotent.
8. `host_other.go` (`//go:build !windows`): `Available` returns `ErrUnsupported`; other methods too.

**Tests** — Windows, skip with reason if `Available` fails: start a root with a tiny projection; read a file (bytes match); enumerate 5 000 entries (buffer refill path); `GetFileData` hangs → reader gets an error in ≤ `CallbackTimeout + 1 s` and `OnTimeout` fired; panic inside projection → reader gets an error and the daemon survives; `Stop` with pending commands returns. `-race` on all.

**Acceptance**
- [ ] Callback table created exactly once per process (test calls `Start` 50 times).
- [ ] No Go pointer stored in OS memory (`go vet` clean; code review by `AG-SEC`).
- [ ] Every callback path has `recover` and returns an HRESULT.
- [ ] Hung backing store never blocks a reader beyond the deadline.

**Pitfalls** — Reading `d.FilePathName` inside the goroutine (dangling after return). Completing a command twice. Unbounded goroutines when a tool reads 50 000 files at once. Calling `PrjStopVirtualizing` from inside a callback (deadlock). Context deadlines here use real time, so timeout behavior is tested on every OS through the simulator with the fake clock (S6-T17/T19); the Windows test uses a short real deadline only.

---

### S6-T05 — Persistent `git cat-file --batch` and `ls-tree` parser

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-GIT` | `AG-LEAD`, `AG-STORE`, `AG-SEC` | `S6-E1` | S6-T01, `S0-E4` | `feat/git-catfile-batch` | yes |

**Skills:** `git-changes-sync`, `security-review`, `go-conventions`   **Spec:** §6.5, §8.1, §17
**Files:** `internal/git/catfile.go`, `internal/git/catfile_test.go`, `internal/git/lstree.go`, `internal/git/lstree_test.go`

**Why / what** — §6.5 names the backing store: a persistent `git cat-file --batch` per repository. Git calls belong to `internal/git` (ownership rule 1), so `AG-WS` asks for it here.

**Steps**
1. Start the process through the `S0-E4` exec path: `exec.CommandContext(ctx, "git", "-C", repo, "cat-file", "--batch")`, env with `GIT_TERMINAL_PROMPT=0`, scrubbed `GIT_*` variables, no shell.
2. Protocol: write `<oid>\n`; read header `<oid> <type> <size>\n`, then exactly `size` bytes and a trailing `\n`; `<oid> missing\n` → `ErrNotFound`. Validate `oid` with a hex regexp (40 or 64 chars) **before** writing — never accept `HEAD:path` or any rev syntax.
3. Serialize requests with a mutex (Guidance: one process per repo plus a small pool if S6-T16 shows contention). Respect `ctx`: if the deadline passes mid-read, kill the process (via the `S0-E5` process-tree helper), return `context.DeadlineExceeded`, and restart lazily on the next call — the stream is now unsynchronized.
4. Reject non-`blob` types with an error; cap object size read by the header value (never `io.ReadAll` an unbounded stream).
5. `LsTree`: `git -C <repo> ls-tree -r -t -l -z <commit>`; parse NUL-separated records `<mode> SP <type> SP <oid> SP+ <size>\t<path>`; validate the commit id as hex first.

**Tests** — harness repo: read 1 000 blobs; missing oid; non-blob oid refused; truncated output (Git fault injection) → error and restart; timeout → process killed and next call works; `ls-tree` with spaces, unicode and newline-in-name paths. Fuzz the header and `ls-tree` record parsers (seeds in `testdata/fuzz/`).

**Acceptance**
- [ ] No rev expressions reach `cat-file`; only validated object ids.
- [ ] Timeout leaves no orphan `git` process (checked on Windows and Unix).
- [ ] Fuzz seeds pass under `go test`.

**Pitfalls** — Reading size+1 bytes and forgetting the trailing LF desyncs every later read. `bufio.Reader` buffering across a kill/restart. Inheriting `GIT_DIR` from the agent's environment.

---

### S6-T06 — Tree index and authorized `BlobStore` with LRU cache

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-WS` | `AG-LEAD`, `AG-PLAT`, `AG-SEC` | `S6-E1` | S6-T01 (S6-T05 for the real Git path) | `feat/projfs-blobstore` | yes |

**Skills:** `cow-workspaces`, `security-review`   **Spec:** §6.5, §17, §22 `blob_cache_mb`, SCOPE.md §23 Open question 6
**Files:** `internal/workspace/projfs/treeindex.go`, `internal/workspace/projfs/blobstore.go`, `internal/workspace/projfs/lru.go`, tests beside them

**Why / what** — The §17 guarantee lives here: callbacks can only reach bytes through a tree index built from the workspace's `base_sha`.

**Steps**
1. `BuildTreeIndex(ctx, git, repo, baseSHA)`: call `LsTree`; build a map keyed by **case-folded** path (`strings.ToLower` is not enough for all Unicode; Guidance: use `PrjFileNameCompare` semantics on Windows and `strings.EqualFold` in the pure layer) plus a per-directory sorted child list. Detect case collisions: record them in the index and log `projfs.case_collision` with the paths (behavior is a documented limitation, S6-T30).
2. Share one immutable index per `(repo, base_sha)` across workspaces (refcounted). This is not a golden tree and does not answer SCOPE.md §23 Open question 6.
3. `ReadBlob(ctx, p, want)`: `e, ok := Lookup(p)`; `!ok` → `ErrNotFound`; `e.Mode` is a tree/gitlink → `ErrNotFound`; `want != "" && want != e.OID` → `ErrOutsideBase`; otherwise read `e.OID` (never `want`) from the LRU, else from `CatFile`.
4. LRU: one per repository, byte-bounded by `blob_cache_mb`; blobs larger than 1/8 of the budget bypass the cache (Guidance).
5. `TreeBytes()` = sum of `Size` for blob entries (denominator for §6.5).

**Tests** — `want` from another commit → `ErrOutsideBase`; path not in tree → `ErrNotFound`; `../x`, `a/../../b`, `C:/x`, `a:stream` rejected before lookup; case-insensitive lookup; LRU eviction by bytes; index shared and freed by refcount; `-race` with 64 concurrent readers.

**Acceptance**
- [ ] There is no code path from a callback to `CatFile` that skips `Lookup`.
- [ ] `AG-SEC` approved with a note in the PR.

**Pitfalls** — Caching by path instead of oid (two bases share a path with different content). Building the index on every `Create` (kills the < 1 s target on large repos).

---

### S6-T07 — Projection callbacks (pure Go)

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-WS` | `AG-LEAD`, `AG-PLAT`, `AG-SEC` | `S6-E1` | S6-T06 | `feat/projfs-projection` | yes |

**Skills:** `cow-workspaces`, `windows-platform`, `security-review`   **Spec:** §6.5, §17, §18
**Files:** `internal/workspace/projfs/projection.go`, `internal/workspace/projfs/projection_test.go`

**Why / what** — Implements `Projection` on top of `BlobStore` and `ScopePolicy`. Pure Go, so it is tested on every OS against the fake host (and later the simulator).

**Steps**
1. `GetPlaceholderInfo`: `CleanRel`; refuse `.git` and any path segment equal to `.git` case-insensitively; `policy.Resolvable` false → `ErrNotFound`; return size and a deterministic `ModTime`.
2. Enumeration: `List(dir)` filtered by `policy.Listed`; filter by `pattern` (pure-Go wildcard matcher with the same semantics as `PrjFileNameMatch`, Guidance); store cursor per `EnumID` in a mutex-protected map; delete on `EndEnumeration`.
3. `GetFileData`: `ReadBlob(ctx, p, want)`; write `[off, off+n)` (or the whole blob, Guidance) through `DataWriter`; honor `ctx`.
4. `Notify`: update a concurrent `touched` set and counters used by S6-T13 (new file, overwritten, modified-on-close, deleted, renamed).
5. Gitlinks (`160000`) project as empty directories; symlinks follow the S6-T02 finding and are listed in S6-T30 limitations.

**Tests** — table tests through the fake host: list root, list nested, pattern `*.go`, restart scan, placeholder for in/out of scope, `.git`, `.GIT`, gitlink; `GetFileData` ranges; `ErrOutsideBase` propagated; ctx cancel stops the read.

**Acceptance**
- [ ] Coverage ≥ 85% for `projection.go` (Guidance).
- [ ] `-race` clean with concurrent enumerations sharing a directory.

**Pitfalls** — Returning entries unsorted (ProjFS requires `PrjFileNameCompare` order). Treating enumeration state as per-directory instead of per-`EnumID`.

---

### S6-T08 — Store: persist the ProjFS instance id per workspace

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-STORE` | `AG-LEAD`, `AG-GIT` | `S6-E1` | S6-T01 | `feat/store-projfs-instance-id` | no |

**Skills:** `store-and-events`   **Spec:** §13, §18 (row 2)
**Files:** `internal/store/migrations/NNNN_workspace_projfs_instance.sql`, `internal/store/workspaces.go`, tests

**Why / what** — Restarting virtualization (after a timeout or a daemon restart) must reuse the GUID passed to `PrjMarkDirectoryAsPlaceholder`. Reconciliation needs it in SQLite (§18 row 2). §13 does not list columns of `workspaces`, so a nullable column is an implementation detail.

**Steps**
1. New embedded migration: `ALTER TABLE workspaces ADD COLUMN projfs_instance_id TEXT NULL;` (never edit an applied migration).
2. Store methods: `SetProjFSInstance(ctx, tx, workspaceID, id)` in the same tx as the workspace row; `ListProjFSWorkspaces(ctx, projectID)`.
3. Commit: `feat(store): persist projfs instance id per workspace`.

**Tests** — migration from empty and from the previous version; round-trip; `-race`.

**Acceptance**
- [ ] Migration idempotent on re-open; old rows read with `NULL`.

**Pitfalls** — Storing the GUID as a blob with platform byte order; use the canonical string form.

---

### S6-T09 — `projfs` `WorkspaceProvider` and registration behind the flag

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-WS` | `AG-LEAD`, `AG-PLAT`, `AG-SEC` | `S6-E1` | S6-T02, S6-T04, S6-T07, S6-T08, `S1-E6` | `feat/workspace-projfs-provider` | yes |

**Skills:** `cow-workspaces`, `windows-platform`, `security-review`   **Spec:** §5.3, §6.1–§6.3, §6.5, §17, §18, §22 `[workspace.projfs]`, §23 decision 10
**Files:** `internal/workspace/projfs.go`, `internal/workspace/registry.go`, `internal/workspace/projfs_test.go`, `test/integration/projfs/lifecycle_windows_test.go`

**Why / what** — Wires everything into the §6.2 interface so the rest of COWFlow sees "just another provider", reporting `CowKind = Virtual` honestly (§2.2 principle 10).

**Steps**
1. `Capabilities(ctx, root)`: `CopyOnWrite: Virtual`, `SparseSupported: true`, `RequiresDaemon: true`, `ProcessIsolation: false`, `Filesystem` from `S0` fscaps; return an error if the root volume is not NTFS/ReFS or `Host.Available` fails.
2. Registration (§6.4 contract): only when `[workspace.projfs] enabled = true`, on Windows, with a host. `Select("auto")` never returns it. Requesting `projfs` while disabled → `ErrProviderDisabled` naming the config key.
3. `Create(ctx, spec)` — sequence confirmed by S6-T02 (Guidance until then):
   1. Validate branch with `check-ref-format`; refuse `main`/`develop`; confine the path under `workspace.root`.
   2. Get-or-build the tree index for `spec.BaseSHA` (S6-T06).
   3. `git worktree add --no-checkout -b <branch> <path> <base_sha>`; `git read-tree <base_sha>` (index only); per-worktree config `core.autocrlf=false`, `core.longpaths=true`.
   4. If the scope policy is `empty-outside`, apply the `S1-E5` sparse patterns so out-of-scope entries are `skip-worktree` (otherwise `git status` reports them deleted).
   5. New `InstanceID`; `MarkRoot`; `hooks.Hit("workspace.projfs.after_mark_before_start")`; `Start`.
   6. fsmonitor config (S6-T11). Write `.env.cowflow` as other providers do.
   7. Persist row + `projfs_instance_id` + event in one tx; only then `ready`.
   8. Any error: stop virtualization, `git worktree remove --force`, remove dir, delete the branch if created here, release ports — compensation per §18; never a partial `ready`.
4. `Inspect`: `S1-E1` status + `Materialization` (S6-T13).
5. `Destroy(ctx, id, opts)`: dirty detection and rescue from `S1-E6` **first** (they read only full/dirty files, which are on disk); then the S6-T02 order (stop fsmonitor → stop virtualization → `git worktree remove` → remove dir). Sharing violation → `pending_cleanup` + `workspace.pending_cleanup` event.
6. `OnTimeout` handler: debounce per workspace, then `Stop` + `Start` with the same `InstanceID` and restart `CatFile`; count `Restarts`.
7. `Reconcile(ctx)` hook used by the daemon (S6-T10): for each stored `projfs` workspace, root exists and carries the ProjFS reparse point → restart virtualization; root missing → report through the `S4-E8` reconciliation outcome; roots on disk without a row → diagnostics only (never delete).

**Tests** — pure (fake host): create/inspect/destroy, compensation at each step via hooks, `auto` never picks projfs, disabled flag error. Windows integration (skip without feature): `TestProjFSLifecycle` = create → read a file → edit → `git status` shows exactly that file → commit → `change publish` → destroy; isolation (§20.2) with two `projfs` workspaces editing the same path.

**Acceptance**
- [ ] `projfs` only via explicit request + `enabled = true`.
- [ ] `inspect` reports `Virtual` and the real filesystem (§20.13).
- [ ] Compensation leaves no dir, worktree registration, branch or row.
- [ ] Destroy never removes unpreserved changes without `--force` (§20.12).

**Pitfalls** — Running `git worktree add` into an already-projected directory (Git sees projected entries and refuses). Forgetting that `projfs` workspaces vanish from the agent's view when the daemon stops — this is expected (§5.3), not a bug to "fix" by hydrating.

---

### S6-T10 — Daemon wiring: ProjFS host lifecycle and daemonless refusal

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-API` | `AG-LEAD`, `AG-MCP`, `AG-SEC` | `S6-E1` | S6-T09, `S4-E1`, `S4-E8`, `S4-E9` | `feat/daemon-projfs-lifecycle` | yes |

**Skills:** `local-api-daemon`, `windows-platform`   **Spec:** §5.3, §6.5, §7 (service), §18
**Files:** `internal/daemon/projfs.go`, `internal/daemon/lifecycle.go`, `internal/daemon/service_windows.go`, tests

**Why / what** — §5.3: the ProjFS host is a daemon capability. The daemon must own the host, start virtualization for existing roots after reconciliation, and stop it cleanly on shutdown and on service stop.

**Steps**
1. Daemon start order: VHDX attach (existing) → open store → reconcile (`S4-E8`, now calling the `projfs` provider's `Reconcile`) → resume outbox → serve.
2. Shutdown and Windows service Stop/Shutdown: drain API → stop all `projfs` sessions (`PrjStopVirtualizing`) → close `CatFile` processes. Bound the whole step with a deadline and log what did not stop.
3. Daemonless path: a `projfs` create request from CLI/MCP without a daemon returns `ErrRequiresDaemon`, mapped to exit code `2` (state) with a message to start the daemon (Guidance). Detection uses the `S4-E1` mechanism (`U-API-2`, §3.3).
4. Log fields `workspace_id`, `project_id` on every ProjFS lifecycle line.

**Tests** — start daemon with two stored `projfs` workspaces (simulated host) → both sessions started; stop → both stopped; service stop path on Windows; daemonless create refused with exit code 2.

**Acceptance**
- [ ] No ProjFS session survives daemon shutdown.
- [ ] Reconciliation runs before the API accepts requests.

**Pitfalls** — Stopping virtualization before draining requests that are creating a workspace (half-created root). Holding a DB transaction while starting virtualization.

---

### S6-T11 — fsmonitor on `projfs` workspaces

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-WS` | `AG-LEAD`, `AG-PLAT` | `S6-E2` | S6-T09 | `feat/projfs-fsmonitor` | no |

**Skills:** `cow-workspaces`   **Spec:** §6.5, §22 `fsmonitor`
**Files:** `internal/workspace/projfs.go`, `internal/workspace/fsmonitor.go`, tests

**Why / what** — Without fsmonitor, `git status` `lstat`s everything and may hydrate. §6.5 sets `core.fsmonitor=true` on `projfs` workspaces; `[workspace.projfs] fsmonitor` controls it.

**Steps**
1. When `fsmonitor = true`: `git -C <ws> config --worktree core.fsmonitor true` (enable `extensions.worktreeConfig` first if `S1` did not). Guidance: also `core.untrackedCache true` if S6-T02 showed a gain; record it in `docs/projfs.md`.
2. Run one `git status` at the end of `Create` (warms the monitor) only if S6-T02 showed it does not hydrate out-of-scope content; otherwise skip.
3. Destroy: `git -C <ws> fsmonitor--daemon stop` before stopping virtualization; ignore "not running".
4. When `fsmonitor = false`: do not set it; log a warning once per workspace that `git status` may be slow (§6.5).

**Tests** — Windows integration: after create, `git config core.fsmonitor` is `true`; second `git status` serves 0 extra bytes (host counter); destroy leaves no `fsmonitor--daemon` process. Pure: config off → key unset.

**Acceptance**
- [ ] Measured: `git status` time and bytes served, with and without fsmonitor, written in the PR.

**Pitfalls** — Leaving the monitor running → stuck handle on destroy (row 7). Setting the key globally instead of per worktree.

---

### S6-T12 — `scope_only` enumeration and live scope expansion (SCOPE.md §23 Open question 5 behind interface)

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-WS` | `AG-LEAD`, `AG-PLAT`, `AG-SEC` | `S6-E2` | S6-T07, S6-T09, `S1-E5`, `S3-E1` | `feat/projfs-scope-only` | yes |

**Skills:** `cow-workspaces`, `security-review`   **Spec:** §6.5, §6.6, §15 `cowflow_expand_scope`, §22 `scope_only`, SCOPE.md §23 Open question 5
**Files:** `internal/workspace/projfs/scope.go`, `internal/workspace/projfs.go`, `internal/workspace/projfs/scope_test.go`

**Why / what** — Tools that walk everything (`go build ./...`, the indexer, Defender) defeat ProjFS (§6.5 known risks). `scope_only` hides what is outside the intent scope. Whether "outside" is *empty* or *lazy* is SCOPE.md §23 Open question 5: both variants exist, the config only selects what §6.5 describes.

**Steps**
1. Scope = intent/task `paths` + the `always` set produced by the `S1-E5` function (do not re-implement it; the content of `always` beyond "root, configs" is SPEC-GAPS `U-PLAT-3`).
2. Implement `full`, `empty-outside`, `lazy-outside` per §6.3. Ancestors of in-scope paths are always listed (otherwise `src/` hides `src/auth`).
3. Mapping: `scope_only = false` → `full`; `true` → `empty-outside`. `lazy-outside` only through `projfs.Options.OutOfScope` used by tests/benchmarks. Add a code comment: `// SCOPE.md §23 Open question 5 is open: do not change the default without a maintainer decision.`
4. Expansion: the `S1-E5` "apply scope" hook (called by `cowflow_expand_scope` / `POST /v1/intents/{id}/expand-scope`) gets a `projfs` implementation: `policy.Update(paths)` → update sparse patterns (if used) → `Session.ClearNegativePathCache` so previously "not found" paths are asked again.
5. A task with `sparse = false` uses `full` regardless of `scope_only` (§6.6, Guidance: keeps one meaning of "needs everything").

**Tests** — table: listed/resolvable for in, out, ancestor, `always` entries under each variant; expansion makes a previously missing path appear without restarting virtualization (simulated host checks the negative-cache clear); `git status` clean after create with `empty-outside` (no phantom deletions).

**Acceptance**
- [ ] Default behavior equals §6.5 text; `lazy-outside` not reachable from config.
- [ ] No path outside scope served under `empty-outside` (assert host counters).

**Pitfalls** — Hiding ancestors. Forgetting the negative path cache (expanded paths stay "not found" until reboot). Treating scope as security: it is a performance feature; §17 confinement is the `base_sha` rule (S6-T06).

---

### S6-T13 — Materialization accounting

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-WS` | `AG-LEAD`, `AG-QA`, `AG-PLAT` | `S6-E3` | S6-T09 | `feat/projfs-materialization-metrics` | no |

**Skills:** `cow-workspaces`, `testing-harness`   **Spec:** §6.5 (acceptance metric), §20.22, §21
**Files:** `internal/workspace/materialization.go`, `internal/workspace/projfs.go`, tests

**Why / what** — AC 22 is a number; this task produces it, from two independent sources so a bug in one is caught by the other.

**Steps**
1. `MeasureLive`: read `Session.Counters()` and the projection's notification counters. O(1).
2. `MeasureScan`: walk only the on-disk entries of the root (`os.ReadDir` on directories that exist; do **not** trigger enumeration of virtual directories — skip entries whose `FileState` is placeholder-only directory), call `FileState`, classify: hydrated → `HydratedBytes += size`; full and in tree → `FullBytes`; full and not in tree → `NewFileBytes`.
3. `TreeBytes` from the tree index. `Ratio()` = `(HydratedBytes+FullBytes)/TreeBytes`.
4. Cross-check in tests: `|BytesServed − (HydratedBytes+FullBytes from base)| ≤ 5%` (Guidance tolerance; files partially read and then rewritten explain small drift).
5. `Inspect` fills `WorkspaceStatus.Materialization` with `MeasureScan`.

**Tests** — simulated host: read 3 files, write 1, create 1 → exact numbers; Windows integration: same with real states; scan does not increase `BytesServed`.

**Acceptance**
- [ ] Scan is read-only: running it twice gives the same numbers and serves 0 bytes.

**Pitfalls** — Walking with `filepath.WalkDir` from the root: it enumerates virtual directories and *creates* placeholders, polluting the measurement. Counting new files in the ratio.

---

### S6-T14 — Expose materialization in `/v1/status` and `/metrics`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-API` | `AG-LEAD`, `AG-CLI`, `AG-SEC` | `S6-E3` | S6-T13 (fake from S6-T01 until then), `S4-E2` | `feat/api-projfs-metrics` | yes |

**Skills:** `local-api-daemon`, `security-review`   **Spec:** §12.5, §21
**Files:** `internal/api/status.go`, `internal/api/metrics.go`, tests

**Why / what** — §21 lists "materialized bytes (projfs)" in `/v1/status` and `/metrics`.

**Steps**
1. `/v1/status`: per `projfs` workspace add `materialized_bytes`, `tree_bytes`, `ratio`, `callback_timeouts`, `restarts` (from `MeasureLive`).
2. `/metrics`: same values in the format `S4-E2` chose (`U-API-3`, §3.3); labels `workspace_id`, `project_id` only — no paths.
3. Scope check as for other status data; `/metrics` access rules unchanged.

**Tests** — handler tests with a static `MaterializationReporter`; no `projfs` workspaces → no series; unauthorized → refused.

**Acceptance**
- [ ] Values match `MeasureLive` exactly; no file paths or branch names in labels.

**Pitfalls** — Calling `MeasureScan` from `/metrics` (walks disks on every scrape).

---

### S6-T15 — `workspace inspect` shows materialization

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-CLI` | `AG-LEAD`, `AG-API` | `S6-E3` | S6-T13, `S1-E8` | `feat/cli-inspect-materialization` | no |

**Skills:** `cli-and-config`   **Spec:** §14, §20.13, §21
**Files:** `internal/cli/workspace_inspect.go`, golden test files

**Why / what** — Humans and the demo read materialization from `cowflow workspace inspect <id>` (and `--json`).

**Steps**
1. Human output: `Materialized: 41.2 MiB of 1.02 GiB (3.9%) — hydrated 312 files, full 4, new 2` only when present.
2. `--json`: `"materialization": {...}` with snake_case fields from §6.5 contract.
3. Golden-file tests for both outputs.

**Tests** — golden tests; absent block for non-`projfs` workspaces.

**Acceptance**
- [ ] `--json` stable and documented in the command help.

**Pitfalls** — Printing the ratio from rounded MiB values instead of bytes.

---

### S6-T16 — AC 22 benchmark and SCOPE.md §23 Open question 5 comparison data

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD`, `AG-WS`, `AG-SEC` | `S6-E3` | S6-T11, S6-T12, S6-T13, `S1-E9` | `test/projfs-ac22-benchmark` | no |

**Skills:** `testing-harness`, `cow-workspaces`   **Spec:** §6.5, §20.22, SCOPE.md §23 Open question 5
**Files:** `test/integration/projfs/ac22_windows_test.go`, `test/integration/projfs/agentsim.go`, `test/integration/projfs/q5_windows_test.go`

**Why / what** — Proves AC 22 and gives the maintainer data for SCOPE.md §23 Open question 5 without deciding it.

**Steps**
1. Fixture: the `S1-E9` ≥ 1 GB repository (same commit for every run). Pick a touch set: directories whose blob bytes total 5–8% **and** file count < 10% of the tree; print both percentages.
2. Agent simulation (`agentsim.go`): open and read every file in the touch set; modify 10 of them; create 2 new files; `git status`; `git add`; `git commit`. No other access.
3. `TestAC22ProjFS`: for `scope_only = true` (scope = touch set) and `false`:
   - `create_ms` = wall time of `Provider.Create` until `ready`; run cold (first workspace for the `base_sha`) and warm (second), 5 repetitions each, report median and max.
   - run `agentsim`; `MeasureScan`; print `touched_pct`, `materialized_pct`, `bytes_served`.
   - fail if `max(create_ms) ≥ 1000` or `materialized_pct ≥ 20` while `touched_pct < 10`.
   - baseline: same steps on a `git_worktree` workspace; print its disk usage.
4. `TestQ5ScopeVariants` (report only, never fails on numbers): for `empty-outside` and `lazy-outside`, run `agentsim` + `go build ./...` (if the fixture is a Go module) + a full directory walk (simulating an indexer); print bytes served and time per variant. Output a Markdown table to stdout for the PR.
5. Run on a ProjFS-enabled Windows 11 machine on a Dev Drive and on NTFS; attach outputs to the sprint-closing PR.

**Tests** — this task *is* the test. It skips with a reason when ProjFS is unavailable.

**Acceptance**
- [ ] AC 22 thresholds met on the reference machine (cold and warm reported separately).
- [ ] Open question 5 table produced; no default changed.

**Pitfalls** — Redefining the metric to make it pass (e.g. warm-only timing): if only warm passes, report it and escalate to `AG-LEAD`/maintainer. Defender scanning the root during the run — record whether the exclusion was set.

---

### S6-T17 — Simulated ProjFS and fault-hook registry in the harness

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD`, `AG-SEC`, `AG-WS` | `S6-E4` | S6-T01, `S0-E8` | `test/harness-projfs-sim` | no |

**Skills:** `testing-harness`, `windows-platform`   **Spec:** §18.1
**Files:** `test/harness/projfssim/host.go`, `test/harness/projfssim/host_test.go`, `test/harness/faults.go`, `test/harness/fs.go`, `test/harness/crash.go`

**Why / what** — §18.1 requires "simulated ProjFS (in-memory callbacks)", "simulated disk full" and crash points. All Sprint 6 failure tests use them, on every OS.

**Steps**
1. `projfssim.Host` implements `projfs.Host`. It keeps an in-memory map of on-disk states (virtual → placeholder → hydrated → full/tombstone) and exposes `Open(path)`, `Read(path)`, `Write(path)`, `List(dir)`, `Delete(path)` that drive the projection exactly as the OS would (placeholder info before data; enumeration in pages of N entries to exercise refill).
2. Fault controls: `Delay(op, glob, d)`, `Hang(op, glob)`, `FailNext(op, err)`; timeouts use the fake clock so tests never sleep.
3. `FSFaults` decorator over the `S0-E8` FS seam: `DiskFullAfter(bytes, glob)` returns `ENOSPC`/`ERROR_DISK_FULL`; `LockForDelete(path)` returns a sharing violation from `Remove*` until released.
4. `CrashPoints`: `Arm(name)` makes the next `Hooks.Hit(name)` return `ErrCrashed`; the actor under test aborts like a killed process (no deferred cleanup that a real crash would skip — run cleanup only in `t.Cleanup`).
5. `Faults` aggregate (§6.6) attached to `harness.New(t, opts...)`.

**Tests** — self-tests: simulator state machine; delay honored with fake clock; `ENOSPC` exactly after N bytes; lock released → remove succeeds.

**Acceptance**
- [ ] `S6-T07` projection tests pass against the simulator unchanged.
- [ ] No `time.Sleep` in the harness.

**Pitfalls** — Making the simulator smarter than ProjFS (e.g. never asking placeholder info before data) hides real bugs. Keep it strict.

---

### S6-T18 — §18 coverage matrix, traceability gate, audit of covered rows

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD`, `AG-SEC` | `S6-E4` | S6-T17 | `test/failure-table-coverage` | no |

**Skills:** `testing-harness`   **Spec:** §18, §18.1, §20.26
**Files:** `test/harness/coverage_test.go`, `test/integration/failures/*_test.go` (rows 1, 3, 4, 5, 10, 11 if moved/added)

**Why / what** — AC 26 needs an automatic answer to "is every row covered?". We tag tests and make a test that fails when a row has no tagged test.

**Steps**
1. Convention: a failure test is named `TestFailure_<Row>_<case>` and carries a comment `// §18:<row-id>` with ids from `test/harness/rows.go`.
2. `TestFailureTableCoverage`: parse all `_test.go` files of the module with `go/parser` (comments), collect tags, compare with `AllRows()`; fail listing missing rows; print the matrix.
3. Audit rows **1, 3, 4, 5, 10, 11** (expected from `S4-E4`, `S2-E2`/`S4-E3`/`S4-E8`, `S2-E7`, `S5-E6`, `S5-E9`, `S4-E7`). For each: find the test, check it uses the harness (fake clock, no real time/network/LLM), asserts the **event** too (`session.lost`, redelivery + dedup on two consumers, `conflict` + files + `abort` restores bytes, `integration.stale` → revalidation, `integration.reverted` + requests + lead notified, `budget.exceeded` + pause), and tag it. Small fixes here; bigger gaps → new task (see §7.1).
4. Add the coverage test to the default `go test ./...` run.

**Tests** — the coverage test itself; a unit test of the tag parser.

**Acceptance**
- [ ] Matrix printed with a test per row; rows 2, 6, 7, 8, 9 pass once S6-T19…T23 merge.
- [ ] Audit notes for rows 1/3/4/5/10/11 in the PR description.

**Pitfalls** — Counting a test that only asserts "no error". Tags on skipped tests: the coverage test must also check the tagged test is not unconditionally skipped on all three OSes.

---

### S6-T19 — Row 9: ProjFS callback timeout → error, not hang; virtualization restarts

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD`, `AG-WS`, `AG-SEC` | `S6-E4` | S6-T07, S6-T09 (pure parts), S6-T17 | `test/failure-projfs-timeout` | no |

**Skills:** `testing-harness`, `cow-workspaces`   **Spec:** §18 row 9, §6.5 risks
**Files:** `test/integration/failures/projfs_timeout_test.go`, `test/integration/projfs/timeout_windows_test.go`

**Why / what** — A daemon hang must never freeze the agent (§6.5, §23 risks).

**Steps**
1. Simulated (all OSes): `Hang("GetFileData", "src/**")`; agent reads `src/a.go` → error within `CallbackTimeout` (fake clock advanced); `OnTimeout` fired; provider performs `Stop`+`Start` with the same `InstanceID`; next read of `src/a.go` (hang released) succeeds; `Restarts == 1`; other workspaces unaffected.
2. Also hang the `CatFile` (Git fault: timeout) instead of the host → same outcome and `cat-file` restarted.
3. Windows real: `Delay` via a projection wrapper; `Get-Content` from a child process returns an error, process exits, no zombie.
4. Tag `// §18:projfs-callback-timeout`.

**Tests** — above; `-race`.

**Acceptance**
- [ ] No test waits on real time; the Windows variant bounds itself with a context deadline.

**Pitfalls** — Asserting only the error and not the restart.

---

### S6-T20 — Row 2: daemon restart reconciles SQLite × processes × worktrees × ProjFS roots

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD`, `AG-SEC`, `AG-API` | `S6-E4` | S6-T09, S6-T10, S6-T17, `S4-E8` | `test/failure-daemon-restart-projfs` | no |

**Skills:** `testing-harness`, `store-and-events`, `local-api-daemon`   **Spec:** §18 row 2, §17
**Files:** `test/integration/failures/daemon_restart_test.go`

**Why / what** — `S4-E8` covered SQLite × processes × worktrees. ProjFS roots, idempotent `op_id` during workspace create, and outbox resumption are tested together here.

**Steps**
1. Setup: 2 `git_worktree` + 2 `projfs` (simulated) workspaces, 1 running simulated session, pending outbox events.
2. Crash at `outbox.after_write_before_delivery`; restart on the same SQLite: events redelivered once per consumer after dedup; both ProjFS sessions restarted with stored `InstanceID`s.
3. Crash at `workspace.projfs.after_mark_before_start` during a create, then retry the same `op_id`: exactly one workspace, `ready`, no duplicate branch.
4. Delete one `projfs` root on disk while "down": reconciliation reports it (no panic, no recreate on another path).
5. Tag `// §18:daemon-restart`.

**Tests** — above, `-race`, fake clock.

**Acceptance**
- [ ] Reconciliation decisions are read from the filesystem/Git, not only SQLite (§17).

**Pitfalls** — Reusing the in-memory daemon object across "restart" (hides state that a real restart loses). Build a new one from disk.

---

### S6-T21 — Row 6: disk full / provider fails → explicit failure, never partial `ready`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD`, `AG-SEC`, `AG-WS` | `S6-E5` | S6-T17 | `test/failure-disk-full` | no |

**Skills:** `testing-harness`, `cow-workspaces`, `store-and-events`   **Spec:** §18 row 6, §6.4
**Files:** `test/integration/failures/disk_full_test.go`, `test/integration/failures/disk_full_windows_test.go`

**Why / what** — Proves §18's "never partial `ready`; compensation" for every provider and for the store.

**Steps**
1. For each provider (`git_worktree`, `cow_clone` where capable, `projfs` simulated): `DiskFullAfter(N, workspace.root/**)` for N in {0, mid-create, just-before-ready}; assert: create returns an error with a stable code, no row in state `ready`, no directory, no worktree registration, no branch created by this call, ports released, an event/log line explains the failure.
2. Provider failure: Git fault `exit 128` on `worktree add`; `MarkRoot` error (simulated) → same assertions.
3. Store: `ENOSPC` on SQLite WAL write during publish → explicit error, no ref without a row (or reconciled by `S4-E8` on restart).
4. `projfs` runtime: `ENOSPC` while serving `GetFileData` → reader error, daemon healthy.
5. Optional Windows platform test (admin, skip otherwise): a 64 MiB VHDX from the `S0-E6` layer as `workspace.root`; create workspaces until full.
6. Tag `// §18:disk-full-provider-fails`.

**Tests** — above.

**Acceptance**
- [ ] Every assertion in step 1 checked for every provider variant.

**Pitfalls** — Asserting a specific failure-state name (unspecified, `U-DATA-5`). Leaving the fake `ENOSPC` armed for `t.Cleanup` so cleanup fails.

---

### S6-T22 — Row 7: stuck handle → `pending_cleanup` with retry

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD`, `AG-SEC`, `AG-WS` | `S6-E5` | S6-T17, `S1-E6` | `test/failure-stuck-handle` | no |

**Skills:** `testing-harness`, `windows-platform`   **Spec:** §7 (handles), §18 row 7, §9.9
**Files:** `test/integration/failures/stuck_handle_test.go`, `test/integration/failures/stuck_handle_windows_test.go`

**Why / what** — On Windows, an open handle blocks delete. The workspace must go to `pending_cleanup`, emit `workspace.pending_cleanup`, retry with backoff and finish when the handle closes.

**Steps**
1. Simulated (all OSes): `LockForDelete(<ws>/src/a.go)`; `destroy --force` → state `pending_cleanup`, event emitted once, diagnostics name the path; advance fake clock → retries happen with backoff; release → next retry removes everything; event/state final.
2. Windows real: open a file in the workspace from a child process with `windows.CreateFile` and share mode without `FILE_SHARE_DELETE`; destroy → `pending_cleanup`; kill child → cleanup completes.
3. Variant: Git `fsmonitor--daemon` left running in a `projfs` workspace (S6-T11 regression) → must not reach `pending_cleanup` because destroy stops it first.
4. Tag `// §18:stuck-handle`.

**Tests** — above.

**Acceptance**
- [ ] Dirty-workspace rescue still happens before any removal attempt (§17).

**Pitfalls** — Holding the handle in the test process itself and then failing to close it on test failure (use `t.Cleanup`).

---

### S6-T23 — Row 8: VHDX not mounted → `pending_mount`, auto-attach, never NTFS

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD`, `AG-SEC`, `AG-PLAT` | `S6-E5` | S6-T17, `S0-E6` | `test/failure-vhdx-unmounted` | no |

**Skills:** `testing-harness`, `windows-platform`   **Spec:** §6.4 failures, §18 row 8, §23 risks
**Files:** `test/integration/failures/vhdx_unmounted_test.go`, `test/integration/failures/vhdx_unmounted_windows_test.go`

**Why / what** — The worst silent failure would be creating workspaces on `C:` (NTFS, no COW) after a reboot. The spec forbids it.

**Steps**
1. Simulated: `SimVHDX.Mounted(false)`; `task create` → workspace `pending_mount`, event `workspace.pending_mount`; auto-attach attempted (assert call); `FSFaults` records **zero** writes outside the VHDX mount path; `Mounted(true)` + fake clock retry → `ready`.
2. Attach fails permanently → stays `pending_mount`, retries bounded, still no NTFS writes.
3. Daemon start with VHDX unmounted: daemon waits/reports, does not create state on NTFS (§7 auto-attach before the daemon).
4. Windows real (admin, skip otherwise): `cowflow devdrive detach`, create a task, check state and that `C:\` has no new workspace dir; `cowflow devdrive attach` → `ready`. The boot-time mechanism is not tested (`U-PLAT-5`).
5. Tag `// §18:vhdx-not-mounted`.

**Tests** — above.

**Acceptance**
- [ ] The "zero writes outside mount" assertion exists in every variant.

**Pitfalls** — Drive letter reused by another volume after detach: assert by volume identity (from `S0-E6` status), not by letter.

---

### S6-T24 — Crash scenarios with real processes

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD`, `AG-SEC`, `AG-API` | `S6-E5` | S6-T20, `S4-E1` | `test/crash-real-process` | no |

**Skills:** `testing-harness`, `local-api-daemon`   **Spec:** §18 rows 1–2, §20.8
**Files:** `test/integration/crash/crash_test.go`, `test/integration/crash/helper_main_test.go`

**Why / what** — In-process crash points cannot reproduce a torn SQLite WAL or a killed Git child. Here the daemon and an agent run as real child processes and are killed hard.

**Steps**
1. Build `cowflow` once in `TestMain` (`go build -o $TMP/cowflow ./cmd/cowflow`, `CGO_ENABLED=0`).
2. Start the daemon on a temp state dir; drive 5 simulated sessions (generic runner with a scripted fake agent) doing intent → publish → sync.
3. Kill the daemon at random moments (seeded; print the seed): Windows `TerminateProcess`, Unix `SIGKILL`. Restart. Repeat 20 times.
4. After each restart assert: every committed event delivered (each consumer sees no gap and no duplicate after dedup); every ref `refs/cowflow/changes/*` has a row (or reconciliation fixed it); no workspace `ready` without a tree; `op_id` retries are no-ops.
5. Kill an agent process tree (§18 row 1) → `session.lost`, workspace and logs preserved, retry scheduled. Tag `// §18:agent-crash` (real-process variant).

**Tests** — above; skipped under `-short`.

**Acceptance**
- [ ] 20 kill/restart cycles green on Windows and Linux CI; macOS at least 5 (Guidance: runtime budget).

**Pitfalls** — Port reuse between restarts (`127.0.0.1:0` + discovery file per `S4-E1`). Orphaned children on Windows if the test kills only the parent — use the Job Object helper.

---

### S6-T25 — Stress: 20+ sessions under chaos

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD`, `AG-SEC` | `S6-E5` | S6-T17, `S4-E11` | `test/stress-chaos` | no |

**Skills:** `testing-harness`, `scheduler-and-leases`, `store-and-events`   **Spec:** §18, §20.15, §20.30
**Files:** `test/integration/stress/stress_test.go` (`//go:build stress`)

**Why / what** — `S4-E11` proved load without faults. Here faults fire randomly while 24 sessions work, to find races between recovery paths.

**Steps**
1. Reuse the `S4-E11` simulated sessions; 24 sessions, 30 simulated minutes (fake clock), seeded PRNG.
2. Random faults every few simulated seconds: Git exit/timeout/truncation, `ENOSPC` bursts, stuck handles on destroy, lost SSE connections, `projfs` (simulated) delays, lease holders dying.
3. Invariants checked continuously and at the end: no event lost or duplicated per consumer (§20.15); leases ≤ slots and FIFO grants (§20.30); no partial `ready`; no goroutine leak (`runtime.NumGoroutine` back to baseline ± small); no `SQLITE_BUSY` surfaced.
4. Run: `go test -tags stress -race -run TestStress ./test/integration/stress -timeout 30m`.

**Tests** — this is the test; nightly in CI (S6-T31).

**Acceptance**
- [ ] 3 consecutive green nightly runs before sprint end.

**Pitfalls** — Non-reproducible failures: always log the seed and fault schedule.

---

### S6-T26 — Fuzz manifest and Git-output parsers

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD`, `AG-SEC`, `AG-GIT` | `S6-E6` | `S2-E5`, `S0-E4` | `test/fuzz-manifest-parsers` | yes |

**Skills:** `testing-harness`, `security-review`, `git-changes-sync`   **Spec:** §8.2, §18.1
**Files:** `test/fuzz/git/manifest_fuzz_test.go`, `test/fuzz/git/testdata/fuzz/...`

**Why / what** — §18.1: "Fuzzing of manifest parsers". The manifest is built from `git diff-tree -r --numstat` + `--raw` output and stored as JSON (§8.2); both parsers read data an agent can influence (file names).

**Steps**
1. Targets against exported entry points of `internal/git` (ask `AG-GIT` to export a `Parse…` function if one is unexported — ownership rule 1):
   ```go
   func FuzzParseDiffTree(f *testing.F) {
   	f.Add([]byte(":100644 100644 aaaa bbbb M\x00src/a.go\x00"), []byte("3\t1\tsrc/a.go\x00"))
   	f.Fuzz(func(t *testing.T, raw, numstat []byte) {
   		m, err := git.ParseManifest(raw, numstat)
   		if err != nil {
   			return
   		}
   		for _, fl := range m.Files {
   			require.False(t, strings.HasPrefix(fl.Path, "/") || strings.Contains(fl.Path, "\x00"))
   			require.GreaterOrEqual(t, fl.Add, 0)
   		}
   		require.Equal(t, len(m.Files), m.Stat.Files)
   	})
   }
   ```
2. `FuzzManifestJSON`: decode → encode → decode is stable; unknown fields rejected; huge numbers rejected.
3. Seeds: real outputs from the harness repo incl. renames, binary (`-\t-`), unicode, quoted paths.
4. Add the ref-name sanitizer (`check-ref-format` front) and `Confine` path function as extra targets (`security-review` tests required).

**Tests** — seeds run in `go test ./test/fuzz/...`; each target fuzzed ≥ 10 min locally before merge, crashers committed as seeds.

**Acceptance**
- [ ] No panic; invariants above hold; crashers fixed by the owner and kept as seeds.

**Pitfalls** — Fuzzing a copy of the parser instead of the production one.

---

### S6-T27 — Fuzz API payloads and MCP inputs; nightly fuzz job

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD`, `AG-SEC`, `AG-API` | `S6-E6` | `S4-E2`, `S3-E1` | `test/fuzz-api-payloads` | yes |

**Skills:** `testing-harness`, `local-api-daemon`, `security-review`   **Spec:** §12 (payload ≤ 1 MiB), §18.1
**Files:** `test/fuzz/api/api_fuzz_test.go`, `test/fuzz/mcp/mcp_fuzz_test.go`, `.github/workflows/fuzz.yml`

**Why / what** — Every `/v1` JSON body is attacker-influenced (agents craft them). Fuzzing through the real router also exercises auth and size limits.

**Steps**
1. One server per fuzz process (temp store, worker token, lead token), built through the production `Routes()`.
2. Single target over all §12 mutating endpoints:
   ```go
   var endpoints = []struct{ method, path string }{
   	{"POST", "/v1/tasks"}, {"PATCH", "/v1/tasks/{id}"}, {"POST", "/v1/workspaces"},
   	{"POST", "/v1/intents"}, {"POST", "/v1/intents/{id}/expand-scope"},
   	{"POST", "/v1/integration-groups"}, {"POST", "/v1/events/ack"}, /* … all of §12 … */
   }

   func FuzzAPIPayloads(f *testing.F) {
   	srv := newFuzzServer(f)
   	f.Add(uint8(0), []byte(`{"title":"x"}`))
   	f.Fuzz(func(t *testing.T, i uint8, body []byte) {
   		ep := endpoints[int(i)%len(endpoints)]
   		req := httptest.NewRequest(ep.method, srv.fill(ep.path), bytes.NewReader(body))
   		req.Header.Set("Authorization", "Bearer "+srv.workerToken)
   		rec := httptest.NewRecorder()
   		srv.h.ServeHTTP(rec, req)
   		require.Less(t, rec.Code, 500, "malformed input must not cause 5xx")
   		require.True(t, json.Valid(rec.Body.Bytes()))
   		require.NotContains(t, rec.Body.String(), srv.workerToken)
   	})
   }
   ```
3. Separate deterministic test: body of 1 MiB + 1 byte rejected.
4. `FuzzMCPToolInput`: decode each tool's input schema struct from bytes; no `workspace_id` accepted from input (§15).
5. `fuzz.yml` (nightly, `workflow_dispatch`): matrix of targets, `go test -run=^$ -fuzz=^<Target>$ -fuzztime=10m <pkg>`, uploads `testdata/fuzz` crashers as artifacts. Pin action SHAs like `ci.yml`.

**Tests** — seeds in CI; nightly run.

**Acceptance**
- [ ] Every §12 endpoint with a body is in the table (test compares with the router).
- [ ] Nightly job green 3 nights before sprint end.

**Pitfalls** — Asserting exact status codes (mapping is SPEC-GAPS `U-API-4`). Sharing a SQLite file across parallel fuzz workers without WAL/busy timeout.

---

### S6-T28 — System-wide security review (§17)

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-SEC` | `AG-LEAD` | `S6-E7` | all `S0`–`S5` epics; S6-T06, S6-T09 for ProjFS | `docs/security-review-v1` | yes |

**Skills:** `security-review`, all area skills as needed   **Spec:** §6.8, §7, §8.1, §12, §15, §15.4, §17, §24.13, §24.14
**Files:** `docs/architecture.md` (threat-model section), review issue with findings; **no code** (findings go to owners)

**Why / what** — Before v1 is called feature-complete, one reviewer walks the whole system with the `security-review` checklist, not just one PR at a time.

**Steps**
1. Refresh the threat model (assets, actors, trust boundaries) incl. the new ProjFS boundary: OS callbacks → projection → `base_sha` tree index → `cat-file`.
2. Walk each package against the PR review checklist: token scope and constant-time compare; loopback bind and `Host` check; 1 MiB limit; argv-only subprocesses; `-C` + `GIT_TERMINAL_PROMPT=0` + `GIT_*` scrubbing; `check-ref-format` before every ref; `Confine` on every path; `blocked_patterns` and §24.13 paths on the manifest (report C12 as-is); §24.14 boundaries server-side; audit rows; destroy rescue; ProjFS `base_sha` rule and timeouts; logs free of tokens.
3. Try to break it: worker token on another workspace; path with `..`/junction/ADS/8.3 name; branch starting with `-`; ProjFS read of a path from another commit; `.git` through the projection; oversized and deeply nested JSON.
4. File one issue per finding with severity, owner role and a failing test idea. High/critical must be fixed in this sprint.
5. Sign off in the sprint-closing PR when all high findings are closed.

**Tests** — findings become tests in owners' PRs and S6-T29.

**Acceptance**
- [ ] Checklist completed for every package in §16.1.
- [ ] Zero open high/critical findings at sprint end.

**Pitfalls** — Fixing code yourself (`AG-SEC` owns no package). Deciding C12 or SCOPE.md §23 Open questions 3/4 in a finding: report, do not decide.

---

### S6-T29 — Security regression guards

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD`, `AG-SEC` | `S6-E7` | S6-T28 (first findings) | `test/security-guards` | yes |

**Skills:** `security-review`, `testing-harness`   **Spec:** §7, §12, §17
**Files:** `test/security/static_test.go`, `test/security/runtime_test.go`

**Why / what** — `AG-SEC` owns no package (README §1); `AG-QA` encodes the review's invariants as tests so they cannot regress.

**Steps**
1. Static (`go/ast` over the module): no `exec.Command`/`CommandContext` whose first arg is `sh`, `bash`, `cmd`, `cmd.exe`, `powershell` with `-c`/`/c`/`-Command` built from a non-constant (the `S0-E6` fixed PowerShell script is allow-listed by file); every `exec.Command*` call in `internal/` lives in an allow-listed wrapper file.
2. Runtime: git env scrubbing (`GIT_DIR` set in the test env does not leak into wrapper calls); tokens never appear in slog output of a full scenario (capture handler); API listener address is loopback; ProjFS read of an object from another commit refused through the simulated host.
3. Add one test per high finding from S6-T28 once fixed.

**Tests** — run in default `go test ./...`.

**Acceptance**
- [ ] Guards fail when a deliberate violation is introduced (proved in the PR with a reverted commit).

**Pitfalls** — Over-broad static rules that force `//nolint` everywhere.

---

### S6-T30 — Platform limitation docs: `docs/projfs.md` and `docs/windows.md`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-PLAT` | `AG-LEAD`, `AG-WS`, `AG-SEC` | `S6-E8` | S6-T02, S6-T09, S6-T16 (numbers) | `docs/platform-limitations` | no |

**Skills:** `windows-platform`, `cow-workspaces`   **Spec:** §6.4, §6.5, §7, §16.1 (`docs/windows`, `docs/projfs`), §20.14, §23
**Files:** `docs/projfs.md`, `docs/windows.md`, `CHANGELOG.md`

**Why / what** — AC 14 requires the limitations in these two files. Users must know what `projfs` and Windows do *not* do before they turn them on.

**Steps**
1. `docs/projfs.md`: status (experimental, never auto, `[workspace.projfs]` keys); requirements (`Client-ProjFS` + exact command, Windows 10 1809+/11, NTFS/ReFS root, daemon running); how it works (diagram: callbacks → projection → `base_sha` tree index → `cat-file` + LRU); `scope_only` and the open SCOPE.md §23 Open question 5 with the S6-T16 table; fsmonitor behavior; materialization metrics and how to read them; limitations (tools that enumerate everything, Defender/indexer, symlinks/gitlinks per S6-T02, case collisions, content served without `.gitattributes` filters unless decided, workspace unavailable while the daemon is down, callback timeout value); troubleshooting; spike findings.
2. `docs/windows.md`: Dev Drive/VHDX provisioning and auto-attach (mechanism left open per SPEC-GAPS `U-PLAT-5`); NTFS fallback with warning; `pending_mount`; long paths and `\\?\`; line endings; Job Objects and `CTRL_BREAK` (service caveat); stuck handles and `pending_cleanup`; Defender exclusion as a *suggestion* (C14) with the exact `Add-MpPreference -ExclusionPath` command for the user; ProjFS pointer.
3. Every statement cites a SCOPE section; Guidance is labeled; open items say "open".
4. `CHANGELOG.md` `[Unreleased]`: experimental `projfs` provider, metrics, docs.

**Tests** — markdown links checked by the existing docs lint if any; review by `AG-WS` for accuracy.

**Acceptance**
- [ ] Both files exist, are linked from `README.md` docs section, and state every known limitation found in this sprint.

**Pitfalls** — Presenting Guidance or a benchmark preference as a decision.

---

### S6-T31 — CI evidence on three platforms (AC 14) and nightly jobs

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD`, `AG-SEC` | `S6-E8` | `S0-E8` | `ci/projfs-and-nightly` | no |

**Skills:** `testing-harness`, `gitflow`   **Spec:** §3.1 (CI), §20.14, §18.1
**Files:** `.github/workflows/ci.yml`, `.github/workflows/nightly.yml`

**Why / what** — AC 14 is proved by CI. ProjFS tests need the optional feature on the Windows runner; stress and crash suites are too slow for every PR.

**Steps**
1. Keep the ubuntu/windows/macos `go test -race -cover ./...` matrix. Add a cross-compile step: `for os in windows linux darwin; do for arch in amd64 arm64; do GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build ./...; done; done`.
2. Add job `projfs` (windows-latest): `Enable-WindowsOptionalFeature -Online -FeatureName Client-ProjFS -NoRestart`; if `RestartNeeded` is true, the ProjFS tests skip with a printed reason (Guidance: then run them on a self-hosted Windows 11 runner, recorded in the demo PR).
3. `nightly.yml`: stress (S6-T25), crash (S6-T24, without `-short`), fuzz (S6-T27 matrix). Pin action SHAs.
4. Keep job names stable (they are required status checks in `docs/GITFLOW.md`).

**Tests** — the workflows themselves; one manual dispatch each.

**Acceptance**
- [ ] Latest `develop` run: 3 OS jobs green, `projfs` job green or skipped with reason plus self-hosted evidence.

**Pitfalls** — Using unpinned actions. Letting ProjFS tests "pass" by skipping silently: skips must print why and the demo must show a real run.

---

### S6-T32 — Sprint demo, evidence, changelog and retro

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` + `AG-LEAD` | `AG-SEC`, all owners | `S6-E3`, `S6-E4`, `S6-E8` (closing) | all S6 tasks | `docs/sprint6-evidence` | no |

**Skills:** `testing-harness`, `gitflow`   **Spec:** §19 Phase 6, §20
**Files:** `CHANGELOG.md`, `docs/SPEC-GAPS.md` (gaps found during the sprint), sprint-closing PR body with evidence

**Why / what** — Runs §8 on a clean machine, records evidence, closes v1 feature work.

**Steps**
1. Fresh Windows 11 machine with Dev Drive and `Client-ProjFS`; fresh Linux and macOS for the platform-neutral steps.
2. Run §8 verbatim; paste outputs (timings, percentages, coverage matrix, CI links) in the PR.
3. Re-run the acceptance tests of all earlier sprints (`go test -race ./test/...` plus the opt-in e2e jobs) to confirm §9.2.
4. Add to `docs/SPEC-GAPS.md` any gap found during the sprint — as gaps, undecided (the planning gaps `U-PLAT-18`…`U-PLAT-21` are already there).
5. Retro: move learnings into `docs/skill/windows-platform`, `cow-workspaces`, `testing-harness`.

**Tests** — the demo.

**Acceptance**
- [ ] Every §2 checkbox ticked with linked evidence.

**Pitfalls** — Running the demo on a developer machine with Defender exclusions and warm caches without saying so.

---

## 8. Demo script

Run on Windows 11 (Dev Drive provisioned by `init`, `Client-ProjFS` enabled), PowerShell, from the repository root. Numbers shown are **examples**; the pass condition is the threshold printed by the test.

```powershell
# 1. Preconditions
Get-WindowsOptionalFeature -Online -FeatureName Client-ProjFS | Select-Object FeatureName, State
#   FeatureName   State
#   Client-ProjFS Enabled
cowflow doctor
#   ... ProjFS: Client-ProjFS enabled ............ ok
#   ... Filesystem X: ReFS (Dev Drive) .......... ok (CowKind Block)

# 2. Enable the experimental provider (config file location as fixed by S0-E2)
#    [workspace]
#    provider = "projfs"
#    [workspace.projfs]
#    enabled = true
#    scope_only = true
#    fsmonitor = true
cowflow daemon start
cowflow daemon status
#   running (pid 12345) on 127.0.0.1:8787

# 3. Functional projfs workspace
Measure-Command { cowflow task create demo --base develop --title "ProjFS demo" --paths src/mod07 } |
  Select-Object TotalMilliseconds
#   TotalMilliseconds : 640          # CLI round-trip; the provider's own time is in step 5
cowflow workspace list --json | ConvertFrom-Json | Where-Object provider -eq "projfs"
$ws = (cowflow workspace list --json | ConvertFrom-Json | Where-Object provider -eq "projfs").path
Get-Content "$ws\src\mod07\a.go" -TotalCount 3          # bytes served on demand
Add-Content "$ws\src\mod07\a.go" "// demo edit"
git -C $ws status --short
#    M src/mod07/a.go
git -C $ws config core.fsmonitor
#   true
Get-ChildItem $ws\src                                      # scope_only: only the scope and its ancestors
git -C $ws commit -am "feat: demo edit" -q
cowflow change publish --task demo

# 4. Materialization metrics
cowflow workspace inspect <workspace-id>
#   Provider:      projfs (CowKind Virtual, filesystem refs)
#   Materialized:  6.3 MiB of 1.02 GiB (0.6%) — hydrated 41 files, full 1, new 0
$token = "<lead token issued by init (storage location per S3-E3)>"
curl.exe -s -H "Authorization: Bearer $token" http://127.0.0.1:8787/v1/status |
  ConvertFrom-Json | Select-Object -ExpandProperty workspaces | Where-Object provider -eq projfs
curl.exe -s -H "Authorization: Bearer $token" http://127.0.0.1:8787/metrics |
  Select-String "materialized"                             # metric names as fixed by S6-T14

# 5. AC 22 measurement (fails above thresholds)
go test -count=1 -run TestAC22ProjFS -v ./test/integration/projfs
#   provider=projfs scope_only=true  create_ms cold max=780 warm max=210 (limit 1000)  PASS
#   touched_pct=7.4 (files 6.1%)  materialized_pct=9.8 (limit 20)  bytes_served=105 MB  PASS
#   provider=projfs scope_only=false create_ms cold max=790 warm max=220               PASS
#   touched_pct=7.4  materialized_pct=12.1                                              PASS
#   baseline git_worktree disk=1.02 GiB
go test -count=1 -run TestQ5ScopeVariants -v ./test/integration/projfs
#   | variant       | bytes served after go build + walk | time  |
#   | empty-outside | ...                                | ...   |
#   | lazy-outside  | ...                                | ...   |   (data for SCOPE.md §23 Open question 5; no decision)

# 6. Every §18 row covered and passing
go test -race -count=1 -run '^TestFailure' ./test/...
#   ok  github.com/deverity/cowflow/test/integration/failures  ...
go test -count=1 -run TestFailureTableCoverage -v ./test/harness
#   agent-crash               TestFailure_AgentCrash_SessionLost, TestFailure_AgentCrash_RealKill
#   daemon-restart            TestFailure_DaemonRestart_ProjFSRoots, ...
#   lost-event                ...
#   sync-conflict             ...
#   target-advances           ...
#   disk-full-provider-fails  ...
#   stuck-handle              ...
#   vhdx-not-mounted          ...
#   projfs-callback-timeout   ...
#   post-promotion-fails      ...
#   llm-budget                ...
#   11/11 rows covered
#   --- PASS: TestFailureTableCoverage

# 7. Stress and crash suites
go test -tags stress -race -count=1 -run TestStress -timeout 30m ./test/integration/stress
#   seed=1739... sessions=24 faults=812 events_checked=... lost=0 dup=0 partial_ready=0  PASS
go test -count=1 -run TestCrash -timeout 20m ./test/integration/crash
#   20/20 kill-restart cycles consistent  PASS

# 8. Fuzzing (seeds in CI; short live run here)
go test ./test/fuzz/...
go test -run='^$' -fuzz='^FuzzParseDiffTree$' -fuzztime=60s ./test/fuzz/git
go test -run='^$' -fuzz='^FuzzAPIPayloads$'  -fuzztime=60s ./test/fuzz/api
#   fuzz: elapsed: 1m0s, execs: ..., new interesting: ...  PASS

# 9. AC 14: three platforms + docs
gh run list --workflow ci.yml --branch develop --limit 1
gh run view <run-id>          # jobs lint, test (ubuntu, windows, macos), projfs: all "success"
foreach ($os in "windows","linux","darwin") { $env:GOOS=$os; $env:CGO_ENABLED="0"; go build ./... }
Remove-Item Env:GOOS
Test-Path docs\windows.md, docs\projfs.md
#   True
#   True

# 10. Clean up
cowflow workspace destroy <workspace-id>
cowflow daemon stop
```

On Linux and macOS, run steps 6–8 and confirm `go test -race ./...` is green (ProjFS tests report `SKIP: projfs: unsupported on this platform`).

---

## 9. Definition of Done

### 9.1 Sprint 6

- [ ] Every task's acceptance boxes ticked; PRs squash-merged to `develop`, commits signed (`-S`) and signed off (`-s`), no AI attribution.
- [ ] `go build ./...`, `go test -race -cover ./...`, `golangci-lint run`, `go mod tidy` (no diff), `govulncheck ./...` green on 3 OSes.
- [ ] `projfs` never selected by `auto`; requires `[workspace.projfs] enabled = true`.
- [ ] No code path serves a blob outside the workspace's `base_sha` (S6-T06 test + S6-T28 sign-off).
- [ ] SCOPE.md §23 Open questions 5 and 6 still listed as open; Open question 5 data attached.
- [ ] `docs/projfs.md`, `docs/windows.md`, `CHANGELOG.md` updated.
- [ ] Gaps found during the sprint added to `docs/SPEC-GAPS.md`; learnings moved to skills.
- [ ] §8 demo run on a clean machine with evidence in the closing PR.

### 9.2 v1 feature-complete checklist (README §3)

Each §20 criterion and the sprint whose demo shows it. Before tagging any release, `AG-LEAD` re-runs these tests (S6-T32 step 3).

| Done | §20 | Criterion (short) | Demonstrated in |
|---|---|---|---|
| [ ] | 1 | 20 workspaces with independent branches | Sprint 1 |
| [ ] | 2 | Same file edited in two workspaces without interference | Sprint 1 |
| [ ] | 3 | Changes attributable by task/session | Sprint 2 |
| [ ] | 4 | Publish refused without intent / with violated invariant | Sprint 2 |
| [ ] | 4 | Integration refused without change | Sprint 5 |
| [ ] | 5 | Change consumed via merge, conflict recorded | Sprint 2 |
| [ ] | 6 | Complete cross-request with linked change | Sprint 2 |
| [ ] | 7 | `blocked` agent unblocked by an event | Sprint 2 |
| [ ] | 8 | State survives restart; outbox redelivers | Sprint 4 |
| [ ] | 9 | Published / validated / promoted distinct | Sprint 5 |
| [ ] | 10 | Integration group, diff-only gates first, manual promotion | Sprint 5 |
| [ ] | 11 | Target advanced → revalidation | Sprint 5 |
| [ ] | 12 | Never destroys unpreserved changes without `--force` | Sprint 1 |
| [ ] | 13 | `inspect` reports actual filesystem and `CowKind` | Sprint 1 |
| [ ] | 14 | Core and tests on 3 platforms; `docs/windows.md`, `docs/projfs.md` | **Sprint 6** |
| [ ] | 15 | 20+ simulated sessions without event duplication/loss | Sprint 4 |
| [ ] | 16 | Cancellation on Windows kills the tree | Sprint 0 |
| [ ] | 17 | Shared cache cuts second-workspace setup ≥ 50% | Sprint 1 |
| [ ] | 18 | Two LLM agents with a real dependency finish a feature | Sprint 3 |
| [ ] | 19 | Exceeded budget pauses and notifies | Sprint 4 |
| [ ] | 20 | Changed contract notifies dependents, requires lead | Sprint 2 |
| [ ] | 21 | Dev Drive provisioned by `init` | Sprint 0 |
| [ ] | 21 | `cow_clone` ≥ 1 GB repo < 2 s, incremental disk < 1% | Sprint 1 |
| [ ] | 22 | ProjFS < 1 s; materialization < 20% when touching < 10% | **Sprint 6** |
| [ ] | 23 | `who --path` from the index without Git (mock) | Sprint 2 |
| [ ] | 24 | Post-promotion auto mode reverts and opens requests | Sprint 5 |
| [ ] | 25 | Watchdog detects injected anomaly, opens cross-request | Sprint 4 |
| [ ] | 26 | Harness covers every §18 failure | **Sprint 6** |
| [ ] | 27 | Lead in VS Code drives tasks/run/events/integration via MCP only | Sprint 3 |
| [ ] | 28 | Mixed team; per-provider limit respected | Sprint 3 |
| [ ] | 29 | `init` generates a valid `.mcp.json` | Sprint 3 |
| [ ] | 30 | Resource leases: slots, queue order, dead-session expiry | Sprint 4 |
| [ ] | 31 | Branch pattern refused; no direct commits to `main`/`develop` | Sprint 3 |
| [ ] | 31 | Hotfix to `main` creates integration group for `develop` | Sprint 5 |

---

## 10. Risks and mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| `git status` hydrates every in-scope file (index built by `read-tree` has no stat data) | AC 22 ratio rises | S6-T02 measures it; `scope_only` + sparse `skip-worktree` bound it to the scope; fsmonitor (S6-T11); AC 22 run includes `git status` so the number is honest |
| Git's built-in fsmonitor does not work in a projected root | slow `git status` | S6-T02 checks on day 1–2; if it fails, document in `docs/projfs.md` and escalate — do not swap in another mechanism silently |
| `windows.NewCallback` limit or a callback panic kills the daemon | every agent's I/O freezes | one callback table per process; `recover` in every trampoline; tests start 50 sessions (S6-T04) |
| Daemon or `cat-file` hang | agent I/O frozen (§23 risk) | async `GetFileData` with deadline, `PrjCompleteCommand` error, virtualization + `cat-file` restart (S6-T04, S6-T19) |
| Tools, Defender or indexer enumerate everything | gain disappears (§6.5) | `scope_only`; Defender exclusion suggested (C14); caches outside the workspace (§6.7); Open question 5 data |
| Blob served from outside `base_sha` | §17 violation | single `Lookup` gate, oid from tree index only, `AG-SEC` review, guard test (S6-T06, S6-T29) |
| `Client-ProjFS` cannot be enabled on hosted CI without reboot | AC 22 / ProjFS tests not run in CI | skip with reason in hosted CI; self-hosted Windows 11 run recorded in the demo PR (S6-T31) |
| Critical path has no slack (spike → binding → host) | AC 22 measurement late | all consumers code against the fake/simulator first; real host only at integration |
| `AG-QA` overloaded (16 tasks) | sprint slips | two `AG-QA` lanes (Guidance); owners review tests of their areas quickly; stress/fuzz nightly, not per PR |
| Hidden gaps in rows "covered" by earlier sprints | AC 26 fails at the end | audit in S6-T18 on days 4–5, early enough to add tasks |
| Flaky crash/stress tests | false alarms, ignored failures | fake clock, seeded PRNG printed on failure, nightly not per PR, 3 green nights required |
| Pressure to decide SCOPE.md §23 Open question 5 from benchmark numbers | spec violated | default stays the §6.5 text; data goes to the maintainer; comment in code |
| Case-colliding paths, symlinks, `.gitattributes` filters in projected content | wrong file contents for some repos | detected and logged; documented limitations; `.gitattributes` filters are SPEC-GAPS `U-PLAT-19` |
| Destroy blocked by projected state or fsmonitor handles | `pending_cleanup` piles up | destroy order from S6-T02; S6-T22 variant; diagnostics name the path |
