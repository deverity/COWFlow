# Sprint 1 — COW workspaces

> **Phase:** 1 — COW workspaces ([SCOPE.md §19](../SCOPE.md), wk. 3–4) · **Length:** 2 weeks (10 working days) · **Exit criterion:** 20 COW workspaces created in < 2 s each on a test repo; isolated editing verified; incremental disk measured.

## 1. Goal

At the end of this sprint, `cowflow task create` gives every task its own **workspace**. A workspace is a directory with its own branch, its own Git `HEAD` and index, and its own files (§6.1). On a copy-on-write filesystem (ReFS Dev Drive on Windows, btrfs or XFS on Linux, APFS on macOS) the workspace appears in under two seconds, because the files share disk blocks with a cached "golden" copy of the base commit instead of being copied. On any other filesystem COWFlow falls back to `git worktree` and prints a warning. It never claims copy-on-write it does not deliver (§2.2 principle 10).

Users can list, inspect, open and destroy workspaces. `inspect` shows the real filesystem and the `CowKind`. `destroy` refuses to delete work that is not preserved unless the user passes `--force`. Workspaces can be **sparse**: only the paths a task declares are materialized (§6.6). Dependency caches (Go, pnpm, cargo, …) are shared through `.env.cowflow`, so the second workspace sets up much faster than the first (§6.7).

We prove all of this with a benchmark on a repository of at least 1 GB.

Out of scope this sprint: intents, publish and sync (Sprint 2); MCP, runners and `cowflow_expand_scope` wiring (Sprint 3); daemon, reconciliation and background retries (Sprint 4); integration candidates (Sprint 5); `projfs` (Sprint 6). The `container` backend is not in any Sprint 1 epic.

## 2. Exit criteria

Every box is ticked in the PR that closes the sprint. That PR also carries the evidence: benchmark JSON files and the demo transcript (§8).

- [ ] **§19 Phase 1 output: 20 COW workspaces in < 2 s each** on the benchmark repo.
  *Verified by* `TestPhase1_TwentyCowWorkspaces` (S1-T21) on a Windows 11 Dev Drive, Linux btrfs, Linux XFS (`reflink=1`) and macOS APFS, plus demo step 4.
- [ ] **§19 Phase 1 output: isolated editing verified.**
  *Verified by* `TestAC02_SameFileTwoWorkspaces` (S1-T20) and demo step 5. The golden tree checksum must be unchanged afterwards.
- [ ] **§19 Phase 1 output: incremental disk measured.**
  *Verified by* S1-T21: it records the volume free-space delta for each workspace. Numbers appear in the result JSON and in demo step 4.
- [ ] **§20.1 — 20 workspaces with independent branches.**
  *Verified by* `TestAC01_TwentyIndependentBranches` (S1-T20): 20 distinct branches, and a commit in one workspace moves no other `HEAD`.
- [ ] **§20.2 — Same file edited in two workspaces without interference.**
  *Verified by* `TestAC02_SameFileTwoWorkspaces` (S1-T20) and demo step 5.
- [ ] **§20.12 — Never destroys unpreserved changes without `--force`.**
  *Verified by* `TestAC12_DestroyRefusesUnpreserved` (S1-T20), covering modified, staged, untracked and committed-but-unpublished work, plus demo step 6. The `--force` rescue path depends on `U-PLAT-2` (§3.3).
- [ ] **§20.13 — `inspect` reports the actual filesystem and `CowKind`.**
  *Verified by* `TestAC13_InspectReportsFilesystemAndCowKind` (S1-T20) on every CI platform, and demo step 7, which compares ReFS → `refs`/`block`/`cow_clone` with NTFS → `ntfs`/`none`/`git_worktree`.
- [ ] **§20.17 — Shared cache reduces second-workspace setup by ≥ 50%.**
  *Verified by* `TestAC17_SharedCacheSecondSetup` (S1-T21) and demo step 9.
- [ ] **§20.21 — Dev Drive provisioned by `init`; `cow_clone` creates a workspace from a ≥ 1 GB repo in < 2 s with incremental disk < 1%.**
  The Dev Drive part was built in `S0-E6` and is re-run in demo step 1. The clone part is *verified by* `TestAC21_CowCloneLargeRepo` (S1-T21) on Windows 11 + Dev Drive.
- [ ] **Phase 1 scope delivered (§19):** `cow_clone` on Windows/Linux/macOS, golden trees, `git_worktree` fallback, sparse by paths, dirty detection, rescue (or rescue explicitly blocked, see §3.3), shared cache, `open`.
  *Verified by* the §9 Definition of Done.

## 3. Prerequisites

### 3.1 Sprint 0 epics that must be merged

On day 1, `AG-LEAD` confirms that each of these epics is merged into `develop`. If one is missing, every task that depends on it codes against a fake. The gap is recorded in the kickoff notes.

| Epic | What Sprint 1 uses from it |
|---|---|
| `S0-E1` | cobra command stubs for `task create`, `workspace list\|inspect\|destroy`, `open`, `golden list\|gc`; global `--json`; the exit-code type (`0` ok, `1` usage, `2` state, `3` conflict, `4` policy) |
| `S0-E2` | config structs for `[workspace]`, `[cache]`, `[devdrive]`, `[scheduler]` (`golden_cache_gb`, `workspace_disk_budget_gb`, `command_timeout_seconds`), `[git]` (`shared_ref_namespace`), `[conventions]`, `[project]` (`default_base`) |
| `S0-E3` | SQLite store (WAL, `busy_timeout`, `BEGIN IMMEDIATE`), embedded migrations, base schema for `projects`, `tasks`, `workspaces`, `golden_trees`, `audit_log` (and `events`/`outbox` if present), prefixed ULID generator, clock seam |
| `S0-E4` | single Git runner (`-C`, `GIT_TERMINAL_PROMPT=0`, timeout), `git >= 2.40` gate, `check-ref-format` helper, injectable `GitRunner` |
| `S0-E5` | path canonicalization and confinement (`Clean + EvalSymlinks + prefix`), long-path handling |
| `S0-E6` | Dev Drive provisioning, `devdrive create\|attach\|detach\|status`, the simulated VHDX interface |
| `S0-E7` | `doctor`; the first filesystem probe (ReFS/NTFS, Dev Drive state), extended in S1-T06 |
| `S0-E8` | harness: fake clock, temporary Git repo, Git subprocess fault injection, disk-full FS decorator, simulated VHDX; CI on Windows/Linux/macOS |

### 3.2 Tools and hardware

| Who | What | Why |
|---|---|---|
| `AG-PLAT`, `AG-WS`, `AG-QA` | Windows 11 22H2+ (build ≥ 22621), local admin, ≥ 80 GB free for a 60 GB Dev Drive, plus a plain NTFS folder | block clone, `pending_mount`, AC 13 ReFS vs NTFS, AC 21 numbers |
| `AG-PLAT`, `AG-QA` | Linux VM or host (e.g. Ubuntu 24.04) with `btrfs-progs`, `xfsprogs`, loop devices and `sudo`; the default ext4 for negative tests | `FICLONE` on btrfs and XFS; ext4 → `git_worktree` |
| `AG-PLAT`, `AG-QA` | macOS 13+ on APFS (the default). Optional: a dedicated APFS volume (`diskutil apfs addVolume`) for clean disk measurements | `clonefile` |
| everyone | `git >= 2.40`, Go 1.25 (from `go.mod`), `golangci-lint` v2, `govulncheck`; `CGO_ENABLED=0` | §3.1, §16 |
| `AG-QA` | CI: hosted `ubuntu-latest` and `macos-latest`; hosted `windows-latest` for functional ReFS tests; **Guidance:** a self-hosted Windows 11 runner (label `win11-devdrive`) for the official AC 21 numbers | hosted Windows Server images may not support the Dev Drive flag |
| `AG-QA` | about 10 GB of scratch space for the benchmark repo (S1-T19) | §20.21 needs a ≥ 1 GB repo |

### 3.3 SPEC-GAPS that affect this sprint

IDs are the stable ones from [SPEC-GAPS.md](../SPEC-GAPS.md). **Nothing below is decided here.** On day 1, `AG-LEAD` takes each item to the maintainer. The listed tasks code behind the named interface or function, so a decision changes one place.

| Ref | Gap | Blocks | How to keep working (not a decision) |
|---|---|---|---|
| `C1` | Go 1.23+ in spec vs 1.25 in `go.mod` | nothing | use the `go.mod` version |
| `C2` | Task branch name: `cowflow/<project>/<task-slug>` vs `<kind>/<slug>` | S1-T02 (`domain.BranchNamer`), S1-T16 | all code receives a branch from `BranchNamer`; tests use a fake namer |
| `C7` | Golden location: `<root>/golden/<sha>` vs `workspace.golden_root` | S1-T09 (`goldenDir`) | one function `goldenDir(cfg, sha)`; tests inject a temp root |
| `C13` | `gradle_home` and `pip_cache` missing from the `[cache]` example | S1-T15 (two of six keys) | implement the four keys in §22 `[cache]`; the other two wait until `AG-CLI` can add the config keys |
| `C14` | Defender exclusion: "suggest" vs "required" | nothing (it affects benchmark conditions) | never change Defender; S1-T21 records the exclusion state in its result JSON |
| `U-PLAT-1` | How a `cow_clone` tree gets its own `HEAD` and index | S1-T10 step 5 | `IndexSeeder` interface with options A and B (S1-T10); if undecided by day 3, implement both and let S1-T21 measure them |
| `U-PLAT-2` | Rescue-before-destroy format and location | S1-T14 step 4 (`--force` on a dirty workspace) | `Rescuer` interface. Until a decision, `--force` on a dirty workspace is **refused** (`ErrRescueUndecided`), so no data is ever lost |
| `U-PLAT-3` | Sparse `always` set beyond "root, configs" | S1-T12 (`AlwaysSet`) | `AlwaysSet()` returns only the spec-named items: root files, `contracts`, `go.mod` |
| `U-PLAT-4` | Same-volume requirement between `golden_root` and `workspace.root` | S1-T06, S1-T10 (`Capabilities`) | when the two are on different volumes, `Capabilities` reports `CowNone`, so `auto` applies the existing no-COW rule from §6.3 (worktree + warning). `AG-LEAD` confirms |
| `U-PLAT-8` | Harness "Git repo on tmpfs" on Windows/macOS | S1-T20 | reuse the `S0-E8` approach (`t.TempDir()` where no tmpfs exists) |
| `U-DATA-1` | Columns of `events`/`outbox` | S1-T03, S1-T14 (emitting `workspace.pending_mount\|pending_cleanup`) | `EventSink` interface; a recording fake if `S0-E3` has no events table; `S2-E2` wires the real outbox |
| `U-DATA-2` | Prefixes for prefixed ULIDs | S1-T02 (workspace/task IDs) | use the `S0-E3` generator; if still open, `AG-LEAD` escalates on day 1 |
| `U-DATA-5` | Workspace states beyond `ready`, `pending_mount`, `pending_cleanup` | S1-T02, S1-T03, S1-T14 | constants in `internal/domain`. Proposed names `creating`, `failed`, `destroyed` go to the maintainer |
| `U-API-1` | SQLite and `config.toml` location | demo step 7 (editing config) | inherit the `S0` choice |
| `U-API-11` | Which §22 values are defaults | S1-T03, S1-T15 (`port_range_start`, `port_range_per_workspace`, `golden_cache_gb`) | read whatever `S0-E2` loads; never hardcode |
| SCOPE.md §23 Open question 6 | Golden tree per `base_sha` vs per branch ("Phase 1 benchmark") | nothing | implement per `base_sha` (§6.4); S1-T21 `TestOQ6_GoldenPerBaseSHA` produces the data |

**Gaps found while writing this plan** (already in SPEC-GAPS.md; not decided here either):

| ID | Question | Tasks waiting on it |
|---|---|---|
| `U-API-12` | Exit code when `destroy` is refused for unpreserved changes: `2` (state) or `4` (refused by policy)? | S1-T14, S1-T17 |
| `U-API-14` | How does a task declare `sparse = false` (§6.6) from the CLI? §14 has no flag | S1-T12, S1-T16 |
| `U-PLAT-11` | Workspace directory name (§5.4 shows `X:/ws/auth`, i.e. the task slug) and where per-workspace logs live (§6.1) | S1-T03 |
| `U-PLAT-12` | Benchmark definitions: which timing counts for "< 2 s each" (in-process create vs CLI wall time; sequential vs concurrent), and what "setup" means in §20.17 | S1-T21 |
| `U-PLAT-13` | What "in use" means for golden GC, and the states of `golden_trees` rows | S1-T02, S1-T09 |
| `U-PLAT-14` | Variable names in `.env.cowflow` for `port_range` and workspace identity | S1-T15 |
| `U-PLAT-15` | Explicit `provider = "cow_clone"` on a volume without COW: fail, or fall back with a warning? | S1-T03 |
| `U-PLAT-16` | Do git-ignored files count as "unpreserved changes"? | S1-T13 |

## 4. Concepts you need

### 4.1 Copy-on-write (COW)
With copy-on-write, a "copy" of a file does not duplicate its bytes. The new file points at the same disk blocks as the original. A block is copied only when one of the two files writes to it, and only that block. So copying a 1 GB tree costs metadata (file records, block maps) instead of 1 GB of data, and it takes milliseconds per file instead of seconds per gigabyte. The isolation rule still holds: after the clone the two files are independent, and writing one never changes the other (§6.1).
Docs: <https://learn.microsoft.com/en-us/windows-server/storage/refs/block-cloning>

### 4.2 Reflink, block clone, `FICLONE`, `clonefile`
Each OS exposes COW cloning with its own call. All three are listed in §6.3.
- **Windows:** `FSCTL_DUPLICATE_EXTENTS_TO_FILE` via `DeviceIoControl` on ReFS. The ranges must be cluster-aligned, each call must cover less than 4 GiB, and both files must be on the same volume.
- **Linux:** the `FICLONE` ioctl (`unix.IoctlFileClone`), often called a "reflink". It works on btrfs, and on XFS formatted with `reflink=1`.
- **macOS:** `clonefile(2)` (`unix.Clonefile`) on APFS. It creates the destination file itself.
When the filesystem cannot clone, all three return an error. **We never fall back to a byte copy while claiming `Block`.**
Docs: <https://learn.microsoft.com/en-us/windows/win32/api/winioctl/ni-winioctl-fsctl_duplicate_extents_to_file> · <https://man7.org/linux/man-pages/man2/ioctl_ficlone.2.html> · `man 2 clonefile` on macOS · <https://pkg.go.dev/golang.org/x/sys/unix#IoctlFileClone>

### 4.3 ReFS vs NTFS, and Dev Drive
NTFS is the default Windows filesystem and **cannot** block-clone. ReFS can. A **Dev Drive** is a ReFS volume tuned for developer workloads: Defender scans it in a lighter "performance mode" and it uses fewer filter drivers. COWFlow creates one as a VHDX file (`cowflow init --devdrive`, `S0-E6`). A volume reports cloning support through `GetVolumeInformation` with the flag `FILE_SUPPORTS_BLOCK_REFCOUNTING` (`0x08000000`, which is not defined in `x/sys/windows`). If the Dev Drive is configured but not mounted, the workspace becomes `pending_mount`. COWFlow never quietly writes to NTFS instead (§6.4, §18).
Docs: <https://learn.microsoft.com/en-us/windows/dev-drive/> · <https://learn.microsoft.com/en-us/windows-server/storage/refs/refs-overview>

### 4.4 `git worktree`
A single repository can have several working trees. `git worktree add -b <branch> <path> <commit>` creates a directory with its own `HEAD`, index and files. All worktrees share one object database, and the per-worktree data lives in `.git/worktrees/<name>/`. This is the universal fallback provider, `git_worktree` (§6.3). Its cost is a full checkout: every byte is written. Per-worktree config (`git config --worktree`) needs `extensions.worktreeConfig=true`, which is how §7's "`core.autocrlf=false` per worktree" is implemented.
Docs: <https://git-scm.com/docs/git-worktree> · <https://git-scm.com/docs/git-config#Documentation/git-config.txt---worktree>

### 4.5 Golden trees
To clone files you need files to clone from. A **golden tree** is a fully materialized checkout of one commit (`base_sha`), kept read-only under the golden root (§6.4; exact location is `C7`). The first workspace for a `base_sha` pays for the checkout. Every later workspace for that `base_sha` clones it. Goldens are immutable: a write to a golden would silently corrupt every future workspace. They are garbage-collected by LRU within `[scheduler].golden_cache_gb` (§11).
Docs: §6.4 in [SCOPE.md](../SCOPE.md)

### 4.6 Sparse checkout
Sparse checkout tells Git to materialize only some paths. Index entries outside the patterns get the *skip-worktree* bit, and their files are not on disk. In **cone mode** (Git's default) the patterns are directories. Cone mode always includes the files at the repository root and the files directly inside each parent directory of a listed directory. §6.6 applies `git sparse-checkout set <paths> + <always>` when a task declares paths.
Docs: <https://git-scm.com/docs/git-sparse-checkout>

### 4.7 Git index and `HEAD` per workspace
`HEAD` says which commit or branch the workspace is on. The **index** (the staging area) lists every tracked path with its blob ID and a snapshot of the file's `stat` data (size, mtime, …). `git status` compares the stat data first and re-reads (hashes) a file only when the stat data differs. That detail decides whether `cow_clone` can be fast. If the index has no matching stat data, the first `git status` hashes every byte of the repo. How a cloned tree gets its own index is `U-PLAT-1`.
Docs: <https://git-scm.com/docs/index-format> · <https://git-scm.com/docs/git-update-index> (see `core.checkStat`, `--refresh`) · <https://git-scm.com/docs/git-checkout-index>

### 4.8 Dirty detection
A workspace is "dirty" when it holds work that exists nowhere else:
- uncommitted changes (modified, staged, deleted, renamed or untracked files), read from `git status --porcelain=v2 -z --untracked-files=all`;
- commits on `HEAD` that are not reachable from `base_sha` or from any `refs/cowflow/changes/*` retention ref (`git rev-list HEAD --not <base> --glob=<ns>/*`).
If detection itself fails (Git error, truncated output), the workspace is treated as dirty. In other words, detection **fails closed**.
Docs: <https://git-scm.com/docs/git-status#_porcelain_format_version_2> · <https://git-scm.com/docs/git-rev-list>

### 4.9 Rescue
§17 requires "rescue of uncommitted changes before destroy". Even with `--force`, COWFlow first saves the uncommitted work somewhere outside the workspace and only then deletes the directory. The format and location are not decided (`U-PLAT-2`). Until they are, `--force` on a dirty workspace is refused. Clean workspaces are destroyed normally.
Docs: §17 in [SCOPE.md](../SCOPE.md); <https://git-scm.com/docs/git-stash#Documentation/git-stash.txt-create> (one candidate mechanism)

### 4.10 Dependency caches
Package managers keep a download cache and often a build cache. If 20 workspaces each download the same dependencies, setup is slow and the disk fills up ("20 × node_modules", §23). §6.7 shares the *download* caches between workspaces. A *build* cache that is not safe for concurrent use gets a subdirectory per workspace; the spec names `cargo_target`. The workspace learns the locations from `.env.cowflow` through each tool's standard environment variable (`GOMODCACHE`, `GOCACHE`, `CARGO_TARGET_DIR`, …).
Docs: <https://go.dev/ref/mod#module-cache> · <https://pkg.go.dev/cmd/go#hdr-Build_and_test_caching> · <https://pnpm.io/npmrc#store-dir> · <https://doc.rust-lang.org/cargo/reference/environment-variables.html>

## 5. Agents and lanes

| Agent | Tasks this sprint | Notes |
|---|---|---|
| `AG-LEAD` | kickoff, contract freeze, gap escalation, reviews, demo | reviews every PR |
| `AG-WS` | S1-T01, S1-T03, S1-T05, S1-T09, S1-T10, S1-T12, S1-T13, S1-T14, S1-T15 | the bottleneck. `AG-PLAT` pairs on S1-T10 (review and debugging, no commits to `AG-WS` files) |
| `AG-PLAT` | S1-T06, S1-T07, S1-T08 | owns `internal/platform` and the OS-specific `internal/workspace/cow_<goos>.go` files |
| `AG-GIT` | S1-T04, S1-T11 | `internal/git` |
| `AG-STORE` | S1-T02 | cross-request from `AG-WS` (ownership rule 1); `internal/store`, `internal/domain` |
| `AG-CLI` | S1-T16, S1-T17, S1-T18 | codes against a fake `Manager` until the real one lands |
| `AG-QA` | S1-T19, S1-T20, S1-T21 | `test/`, CI workflows, benchmarks |
| `AG-SEC` | reviewer for every task marked `Security: yes` | owns no package |

```text
Day         D1     D2     D3     D4     D5     D6     D7     D8     D9     D10
AG-LEAD   [kickoff+contracts][..... reviews, gap follow-up with maintainer .....][demo]
AG-WS     [T01  ][---- T03 ----][T05  ][T13+15][T09  ][---- T10 ----][T12+14][fixes]
AG-PLAT   [---- T06 ----][---------- T07 ----------][---- T08 ----][pair T10, CI   ]
AG-GIT    [--------- T04 ---------][---- T11 ----][support T05/T09/T12, reviews   ]
AG-STORE  [--------- T02 ---------][reviews, S2-E1 preparation                     ]
AG-CLI           [--------- T17 ---------][--------- T16 ---------][-- T18 --][demo]
AG-QA     [--------- T19 ---------][------------- T20 -------------][---- T21 ----]
AG-SEC     reviews: T02 T03 T04 T05 T07 T08 T09 T10 T11 T12 T13 T14 T15 T16 T17 T18
```

Dependencies (an arrow means "must be merged before the arrowhead task can merge"; the later task may start earlier against fakes):

```text
T01 ──> T02, T03, T07, T17 (fakes)
T02 ──> T03, T09, T16
T03 ──> T05, T09, T15, T16
T04 ──> T05, T09, T11, T13
T05 ──> T10, T12, T13, T16
T06 ──> T07 ──> T08 ──> T10
T09 ──> T10, T18
T10 ──> T12
T11 ──> T12
T13 ──> T14 ──> T17
T05, T10, T13, T14, T16, T17, T19 ──> T20
T10, T12, T15, T16, T19 ──> T21
```

**Critical path:** S1-T01 → S1-T03 → S1-T09 (needs S1-T04 `ExportTree`) → S1-T10 (needs S1-T07/S1-T08) → S1-T12 → S1-T21 → demo. A second chain must also finish in time: S1-T06 → S1-T07 → S1-T10. If S1-T07 slips past D6, `AG-LEAD` runs Linux/macOS `cow_clone` first so S1-T10 can merge, and the Windows numbers move to D9–D10.

## 6. Contracts

`AG-LEAD` freezes these on day 1. A change after day 1 needs a PR reviewed by both sides and by `AG-LEAD`. Identifiers in §6.2 of SCOPE.md are verbatim. Everything else is **Guidance** (conventional Go naming), agreed for this sprint.

Following `go-conventions`, interfaces are declared **where they are consumed** (`internal/workspace`) and implemented by the provider's package.

### 6.1 `WorkspaceProvider`, capabilities and `CowKind` — owner `AG-WS`

Provider: `AG-WS` (`internal/workspace/provider.go`, `errors.go`). Consumers: `AG-CLI` (S1-T16–T18), `AG-QA` (S1-T20–T21); later `AG-API`, `AG-MCP`, `AG-INT`.

```go
package workspace

// §6.2 verbatim — do not rename or add fields.
type WorkspaceProvider interface {
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
	Filesystem       string // "refs", "ntfs", "btrfs", "apfs", ...
	RequiresDaemon   bool
}

type CowKind uint8

const (
	CowNone    CowKind = iota // git_worktree (and container, later)
	CowBlock                  // cow_clone
	CowVirtual                // projfs (Sprint 6)
)

func (k CowKind) String() string             // "none" | "block" | "virtual"
func (k CowKind) MarshalText() ([]byte, error) // --json uses the String form

// Guidance: field set agreed for Sprint 1 (§6.1 lists what a workspace owns).
type WorkspaceSpec struct {
	ID, OpID                     string // ID: prefixed ULID minted by Manager; OpID: idempotency key (§18)
	ProjectID, TaskID, SessionID string // SessionID empty until Sprint 3
	RepoDir                      string // canonical main repository dir
	Path                         string // confined under [workspace].root (S0-E5)
	Branch                       string // from domain.BranchNamer; check-ref-format'ed; never main/develop
	BaseSHA                      string // full commit id from GitOps.ResolveCommit
	Sparse                       SparseSpec
	Ports                        PortRange
}
type SparseSpec struct {
	Enabled bool
	Paths   []string // slash-separated, repo-relative directories
}
type PortRange struct{ Start, End int } // half-open [Start, End)

type Workspace struct {
	ID, ProjectID, TaskID, SessionID string
	Provider, Path, Branch, BaseSHA  string
	Caps                             Capabilities
	Sparse                           SparseSpec
	Ports                            PortRange
	State                            domain.WorkspaceState
	CreatedAt                        time.Time
}
type WorkspaceStatus struct {
	Workspace
	Filesystem string  // probed NOW from Path, not copied from the row (§20.13)
	CowKind    CowKind // what this provider delivers
	Head       string
	Dirty      DirtyReport
	Warnings   []string // e.g. filesystem differs from the one recorded at create
}
type DirtyReport struct {
	Uncommitted []string // porcelain v2 paths, minus COWFlow-owned files (.env.cowflow)
	Unpublished []string // commit SHAs (see §4.8)
}
func (d DirtyReport) Clean() bool { return len(d.Uncommitted) == 0 && len(d.Unpublished) == 0 }

type DestroyOptions struct{ Force bool }

var (
	ErrNotFound         = errors.New("workspace: not found")
	ErrPendingMount     = errors.New("workspace: volume not mounted")   // state pending_mount
	ErrPendingCleanup   = errors.New("workspace: cleanup pending")      // state pending_cleanup
	ErrProtectedBranch  = errors.New("workspace: protected branch")     // main/develop (§24.2)
	ErrCloneUnsupported = errors.New("workspace: block clone unsupported")
	ErrRescueUndecided  = errors.New("workspace: rescue not available") // U-PLAT-2
	ErrUnsupportedProvider = errors.New("workspace: provider not available") // projfs, container
)
type UnpreservedError struct{ Report DirtyReport } // destroy without --force (§20.12)
```

`Manager` is the entry point the CLI uses. It is owned by `AG-WS` and lives in `internal/workspace/manager.go`:

```go
type Selection struct {
	Provider WorkspaceProvider
	Caps     Capabilities
	Warning  string // non-empty when auto fell back (§1.2 "explicit warning")
}
func NewManager(d Deps) *Manager
func (m *Manager) Select(ctx context.Context, requested string) (Selection, error) // "auto"|"cow_clone"|"git_worktree"
func (m *Manager) Create(ctx context.Context, req CreateRequest) (*Workspace, Selection, error)
func (m *Manager) Inspect(ctx context.Context, id string) (*WorkspaceStatus, error)
func (m *Manager) Destroy(ctx context.Context, id string, opts DestroyOptions) error
func (m *Manager) List(ctx context.Context, f ListFilter) ([]Workspace, error)
func (m *Manager) ExpandScope(ctx context.Context, id string, add []string) error // MCP wiring: Sprint 3
func (m *Manager) Golden() *GoldenCache                                         // list/gc for S1-T18
```

### 6.2 Clone primitive — between `AG-PLAT` and `AG-WS`

Provider: `AG-PLAT` writes `internal/workspace/cow_windows.go`, `cow_linux.go`, `cow_darwin.go` and `cow_other.go` (§16.1 layout; README ownership of OS-specific primitives). Consumer: `AG-WS` uses it through the `Cloner` interface in `clone.go`.

```go
// internal/workspace/clone.go (AG-WS) — the seam
type Cloner interface {
	CloneFile(ctx context.Context, src, dst string) error
}

// internal/workspace/cow_<goos>.go (AG-PLAT) — one per OS, same signature
//
// Contract (frozen):
//  1. dst MUST NOT exist; cloneFile creates it exclusively (O_EXCL) and never overwrites.
//  2. Caller guarantees src and dst are on the same volume.
//  3. On success dst has src's bytes, size, permission bits and modification time
//     (mtime matters: the index stat check depends on it, §4.7).
//  4. NEVER falls back to a byte copy. Unsupported FS/volume/flags -> error wrapping
//     ErrCloneUnsupported (errors.Is must work).
//  5. On any error dst is removed: no partial file is left behind.
//  6. ctx is checked between chunks; a single ioctl is not interruptible.
func cloneFile(ctx context.Context, src, dst string) error
```

Until S1-T08 lands, `cow_other.go` carries `//go:build !windows` and returns `ErrCloneUnsupported`, so every OS compiles. S1-T08 narrows the tag to `!windows && !linux && !darwin`. The production adapter `type osCloner struct{}` (calling `cloneFile`) is added by `AG-WS` in S1-T10.

### 6.3 Filesystem and volume information — owner `AG-PLAT`

Provider: `AG-PLAT` (`internal/platform/fscaps*.go`, `freespace_*.go`), extending the `S0-E7` probe. Consumers: `AG-WS` (`Capabilities`, `Inspect`), `AG-QA` (disk measurement).

```go
package platform

type Volume struct {
	Filesystem       string // lowercase: "refs", "ntfs", "btrfs", "xfs", "ext4", "apfs", "tmpfs", ...
	ID               string // stable per volume: serial number (Windows), st_dev/fsid (Unix)
	MountPath        string // volume root ("X:\\", "/mnt/cf")
	DevDrive         bool
	BlockRefcounting bool   // a hint only; the probe clone in S1-T10 is authoritative
	ClusterSize      uint32 // 0 when unknown
}

func VolumeOf(ctx context.Context, path string) (Volume, error)
func SameVolume(ctx context.Context, a, b string) (bool, error)
func FreeBytes(path string) (uint64, error) // bytes available to the caller
```

### 6.4 Git operations `AG-WS` needs from `AG-GIT`

Provider: `AG-GIT` (`*git.Client` in `internal/git`, S1-T04 and S1-T11). Consumer: `AG-WS` through a consumer-side interface.

Every call goes through the `S0-E4` runner (`-C`, `GIT_TERMINAL_PROMPT=0`, timeout, argv only). Inherited `GIT_DIR`, `GIT_WORK_TREE` and `GIT_INDEX_FILE` are stripped from the environment (security pitfall).

```go
// internal/workspace/gitops.go (AG-WS declares; *git.Client satisfies)
type GitOps interface {
	ResolveCommit(ctx context.Context, repo, rev string) (sha string, err error)
	CheckBranchName(ctx context.Context, name string) error // S0-E4
	BranchExists(ctx context.Context, repo, name string) (bool, error)
	DeleteBranchIfAt(ctx context.Context, repo, name, sha string) error // compensation only

	WorktreeAdd(ctx context.Context, repo, path string, o git.WorktreeAddOptions) error
	WorktreeRemove(ctx context.Context, repo, path string, force bool) error
	WorktreePrune(ctx context.Context, repo string) error
	WorktreeList(ctx context.Context, repo string) ([]git.Worktree, error)
	GitPath(ctx context.Context, worktree, name string) (string, error) // e.g. "index"
	ConfigSetWorktree(ctx context.Context, worktree, key, value string) error
	Head(ctx context.Context, worktree string) (string, error)

	ExportTree(ctx context.Context, repo, sha, dstDir, indexFile string) error // golden build
	RefreshIndex(ctx context.Context, repo, workTree, indexFile string) error
	ReadTree(ctx context.Context, worktree string, args ...string) error // e.g. "-mu", "HEAD"

	SparseSet(ctx context.Context, worktree string, dirs []string) error // cone mode, via stdin
	SparseDisable(ctx context.Context, worktree string) error

	Status(ctx context.Context, worktree string) ([]git.StatusEntry, error)
	Unpublished(ctx context.Context, worktree, baseSHA, refNamespace string) ([]string, error)
}

// internal/git (AG-GIT)
type WorktreeAddOptions struct {
	NewBranch  string // -b <name>; empty = check out an existing branch named in Commitish
	Commitish  string // base SHA (hex) or existing branch
	NoCheckout bool
}
type Worktree struct {
	Path, Head, Branch string
	Locked, Prunable   bool
}
type StatusEntry struct {
	Kind     StatusKind // Changed | Renamed | Unmerged | Untracked
	XY       string     // two-letter porcelain code
	Path     string
	OrigPath string // renames/copies only
}
```

### 6.5 Store operations `AG-WS` needs from `AG-STORE`

Provider: `AG-STORE` (`*store.Store`, S1-T02). Consumers: `AG-WS`, and `AG-CLI` for task rows. Rules: every method is one short `BEGIN IMMEDIATE` transaction, and no transaction is ever held across a Git or clone call (store skill).

```go
// internal/workspace/store.go (AG-WS declares)
type Store interface {
	// Inserts state=creating (U-DATA-5), allocates the lowest free port slot, idempotent by OpID.
	InsertWorkspace(ctx context.Context, r domain.WorkspaceRecord, perWS, start int) (domain.WorkspaceRecord, error)
	// State change + optional event (U-DATA-1) + optional audit row, same transaction (§13, §17).
	SetWorkspaceState(ctx context.Context, id string, to domain.WorkspaceState, c domain.StateChange) error
	GetWorkspace(ctx context.Context, id string) (domain.WorkspaceRecord, error)
	ListWorkspaces(ctx context.Context, f domain.WorkspaceFilter) ([]domain.WorkspaceRecord, error)

	ClaimGolden(ctx context.Context, projectID, sha, opID string, staleAfter time.Duration) (domain.GoldenClaim, error)
	FinishGolden(ctx context.Context, projectID, sha string, sizeBytes int64, files int) error
	FailGolden(ctx context.Context, projectID, sha, reason string) error
	UseGolden(ctx context.Context, projectID, sha string, delta int) error // in_use ±1, last_used_at
	ListGolden(ctx context.Context, projectID string) ([]domain.GoldenTree, error)
	MarkGoldenDeleting(ctx context.Context, projectID, sha string) (bool, error) // false if in use
	DeleteGolden(ctx context.Context, projectID, sha string) error

	CreateTask(ctx context.Context, t domain.Task) (domain.Task, error) // minimal; S2-E1 extends
	GetTaskBySlug(ctx context.Context, projectID, slug string) (domain.Task, error)
}

// internal/domain (AG-STORE)
type GoldenClaim struct {
	Action GoldenAction // Build | Ready | Wait
	Tree   GoldenTree
}
type BranchNamer interface { // C2 — one implementation per decision
	TaskBranch(project, kind, slug string) (string, error)
}
```

### 6.6 Config consumed (from `S0-E2`, owner `AG-CLI`)

The keys read this sprint are:
- `[workspace]`: `provider`, `root`, `golden_root`, `sparse_by_intent`, `port_range_per_workspace`, `port_range_start`, `keep_failed_workspaces`, `keep_failed_hours`
- `[cache]`: `pnpm_store`, `go_modcache`, `go_cache`, `cargo_target` (`gradle_home` and `pip_cache` are blocked by `C13`)
- `[devdrive]`: `enabled`, `mount`
- `[scheduler]`: `golden_cache_gb`, `workspace_disk_budget_gb`, `command_timeout_seconds`
- `[git]`: `shared_ref_namespace`
- `[project]`: `default_base`
- `[conventions]`: `preset`

No new config key is added this sprint. If one is missing from `S0-E2`, open a cross-request to `AG-CLI`.

### 6.7 Fakes (so consumers can start on day 1)

| Fake | Package | Owner | Used by |
|---|---|---|---|
| `FakeProvider`, `FakeManager` | `internal/workspace/workspacetest` | `AG-WS` (S1-T01) | `AG-CLI`, `AG-QA` |
| `FakeCloner` (byte copy plus fault injection: error after N files, `ENOSPC`) — **test only** | `internal/workspace/workspacetest` | `AG-WS` (S1-T01) | S1-T03, S1-T10, S1-T20 |
| `FakeGit` (records calls, scripted results) | `internal/workspace/workspacetest` | `AG-WS` (S1-T01) | S1-T03, S1-T13 |
| `MemStore` (in-memory `Store`) | `internal/workspace/workspacetest` | `AG-WS` (S1-T01) | S1-T03, S1-T09 |
| `FakeVolumes` (scripted `Volume`, mount state) | `internal/workspace/workspacetest` | `AG-WS` (S1-T01) | S1-T03, S1-T13 |
| simulated VHDX, fake clock, Git faults, disk full | `test/harness` | `AG-QA` (`S0-E8`) | everyone |

## 7. Tasks

Conventions for every task:
- Branch from an up-to-date `develop`.
- Commit with `git commit -S -s -m "<type>(<scope>): <summary>"`. Never add AI or tool attribution.
- Open the PR against `develop`.
- Before pushing, run:
  ```bash
  go build ./... && go test -race ./... && golangci-lint run && go mod tidy && git diff --exit-code go.mod go.sum
  GOOS=linux go vet ./... && GOOS=darwin go vet ./... && GOOS=windows go vet ./...
  ```
- Update `CHANGELOG.md` `[Unreleased]` when the change is user-visible.

---

### S1-T01 — Workspace provider contract, `CowKind`, sentinel errors and fakes

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-WS` | `AG-LEAD`, `AG-PLAT` | `S1-E1` | `S0-E3` | `feat/workspace-provider-contract` | no |

**Skills:** `cow-workspaces`, `go-conventions`, `gitflow`   **Spec:** §2.2 (9, 10), §6.1, §6.2, §6.3, §20.13
**Files:** `internal/workspace/doc.go`, `provider.go`, `errors.go`, `clone.go` (interface only), `gitops.go`, `store.go`, `volumes.go`, `manager.go` (types only), `internal/workspace/workspacetest/*.go`, `internal/workspace/provider_test.go`

**Why / what** — Six roles meet at `internal/workspace`. If the types of §6 exist on day 1, all of them can start in parallel against fakes. This task writes no behavior. It writes the contract, the errors, and fakes good enough for the CLI and the tests to use.

**Steps**
1. Create the branch:
   ```bash
   git switch develop && git pull --ff-only && git switch -c feat/workspace-provider-contract
   ```
2. Write `provider.go` with the §6.1 code of this document. Copy `WorkspaceProvider` and `Capabilities` **verbatim** from SCOPE.md §6.2.
3. Implement `CowKind.String()` and `MarshalText()`. Unknown values print `"unknown(<n>)"` and never panic.
4. Write `errors.go` with the sentinels and `UnpreservedError`. Its `Error()` lists at most 10 paths and then `"… and N more"`.
5. Write the consumer-side interfaces from §6.2–§6.5 into `clone.go`, `gitops.go`, `store.go` and `volumes.go`:
   ```go
   type Volumes interface { // satisfied by a thin adapter over internal/platform
   	VolumeOf(ctx context.Context, path string) (platform.Volume, error)
   	SameVolume(ctx context.Context, a, b string) (bool, error)
   }
   ```
6. Write the fakes in `workspacetest` (§6.7). `FakeCloner` copies bytes with `io.Copy` and preserves mtime. It accepts `FailAfter int` and `Err error` (default `syscall.ENOSPC`). Add a doc comment saying it exists for tests only.
7. Add compile-time checks: `var _ workspace.WorkspaceProvider = (*workspacetest.FakeProvider)(nil)`, and the same for every fake.
8. Commit: `feat(workspace): add WorkspaceProvider contract and test fakes`.

**Tests** — `TestCowKindString` (table: 0, 1, 2, 99), `TestCowKindMarshalJSON`, `TestUnpreservedErrorMessageTruncates`. Run `go test -race ./internal/workspace/...` and `GOOS=linux|darwin|windows go vet ./internal/workspace/...`.

**Acceptance**
- [ ] `WorkspaceProvider` and `Capabilities` match §6.2 character for character (the reviewer diffs them).
- [ ] Every interface of §6.2–§6.5 exists, and every fake satisfies it at compile time.
- [ ] The package compiles on all three OSes and imports nothing from `internal/cli`.

**Pitfalls**
- Do not add fields to `Capabilities`. Extra information such as a fallback warning goes in `Selection` or `WorkspaceStatus`.
- The byte-copying `FakeCloner` must never be reachable from production code. Keep it in `workspacetest`.
- Do not reference `cloneFile` yet: `AG-PLAT` owns it (S1-T07). Referencing it now breaks the build.

---

### S1-T02 — Store: workspaces, golden trees, minimal tasks, port slots

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-STORE` | `AG-LEAD`, `AG-GIT`, `AG-WS`, `AG-SEC` | `S1-E1` (cross-request from `AG-WS`) | `S0-E3`, S1-T01 | `feat/store-workspaces-golden` | yes |

**Skills:** `store-and-events`, `go-conventions`, `security-review`, `gitflow`   **Spec:** §6.1, §6.4, §11, §13, §17, §18, §22, §24.2
**Files:** `internal/store/migrations/00NN_workspaces_golden.sql`, `internal/store/workspaces.go`, `internal/store/golden.go`, `internal/store/tasks.go`, `internal/domain/workspace.go`, `internal/domain/task.go`, `internal/domain/branch.go`, matching `_test.go` files

**Why / what** — Workspaces and goldens are created by several CLI processes at the same time (daemonless mode, §5.3). Port slots and "who builds the golden" must therefore be decided inside SQLite transactions, not in memory. Destroy must also be audited in the same transaction as the state change (§17).

**Steps**
1. Find out what `S0-E3` already created. Write a throwaway test that prints `SELECT name, sql FROM sqlite_master`. Add only what is missing; never edit an applied migration.
2. Migration (**Guidance** — §13 names the tables but not their columns):
   ```sql
   -- workspaces: one row per workspace (§6.1)
   CREATE TABLE IF NOT EXISTS workspaces (
     id TEXT PRIMARY KEY, project_id TEXT NOT NULL, task_id TEXT NOT NULL, session_id TEXT,
     provider TEXT NOT NULL, cow_kind TEXT NOT NULL, filesystem TEXT NOT NULL,
     path TEXT NOT NULL, branch TEXT NOT NULL, base_sha TEXT NOT NULL,
     sparse_paths TEXT NOT NULL DEFAULT '[]',          -- JSON array
     port_slot INTEGER NOT NULL, port_start INTEGER NOT NULL, port_end INTEGER NOT NULL,
     state TEXT NOT NULL, last_error TEXT NOT NULL DEFAULT '',
     op_id TEXT NOT NULL UNIQUE,
     created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
   CREATE UNIQUE INDEX IF NOT EXISTS workspaces_live_slot
     ON workspaces(project_id, port_slot) WHERE state NOT IN ('destroyed','failed'); -- U-DATA-5
   CREATE TABLE IF NOT EXISTS golden_trees (
     project_id TEXT NOT NULL, base_sha TEXT NOT NULL, path TEXT NOT NULL,
     state TEXT NOT NULL,                                 -- U-PLAT-13: building|ready|failed|deleting (proposed)
     size_bytes INTEGER NOT NULL DEFAULT 0, file_count INTEGER NOT NULL DEFAULT 0,
     in_use INTEGER NOT NULL DEFAULT 0, builder_op_id TEXT NOT NULL,
     created_at TEXT NOT NULL, last_used_at TEXT NOT NULL, updated_at TEXT NOT NULL,
     PRIMARY KEY (project_id, base_sha));
   ```
3. `InsertWorkspace`: inside `BEGIN IMMEDIATE`, return the existing row if `op_id` is already present. Otherwise read the live slots of the project, pick the lowest free `n`, and set `port_start = port_range_start + n*port_range_per_workspace` and `port_end = port_start + port_range_per_workspace`. Then insert with state `creating`.
4. `SetWorkspaceState`: update `state`, `last_error`, `updated_at`. In the **same** transaction, write the `audit_log` row when `c.Audit != nil` (destroy, `--force`) and the event when `c.Event != nil` (only `workspace.pending_cleanup` and `workspace.pending_mount` exist in §9.9). If `S0-E3` has no events table, delegate to an injected `EventSink` (`U-DATA-1`).
5. `ClaimGolden(projectID, sha, opID, staleAfter)`:
   - no row → insert `building` → return `Build`;
   - `ready` → `in_use = in_use + 1`, `last_used_at = now` → return `Ready`;
   - `building` and `updated_at` older than `staleAfter` → take it over (new `builder_op_id`) → return `Build`;
   - otherwise → return `Wait`.
6. `MarkGoldenDeleting` succeeds only when `in_use = 0` and the state is `ready`. That is the check that keeps GC from racing a clone.
7. Minimal `tasks` support: `CreateTask` and `GetTaskBySlug`, with columns `id, project_id, slug, title, kind, base, base_sha, branch, paths(JSON), sparse, state, created_at`. Leave a `// S2-E1 extends` comment and add nothing more.
8. `internal/domain/branch.go`: declare `BranchNamer` and the `WorkspaceState` constants. Spec-stated: `WorkspaceReady = "ready"`, `WorkspacePendingMount = "pending_mount"`, `WorkspacePendingCleanup = "pending_cleanup"`. Proposed (`U-DATA-5`): `creating`, `failed`, `destroyed`. **Do not** implement a `BranchNamer` until `C2` is decided; tests use a fake.
9. Inject the clock and the ID generator from `S0-E3`; never call `time.Now()` directly.
10. Commit: `feat(store): add workspace, golden tree and port slot storage`.

**Tests** — in `internal/store`:
- `TestInsertWorkspaceLowestFreeSlot`: slots 0 and 2 live, 1 destroyed → the new row gets 1.
- `TestInsertWorkspaceIdempotentByOpID`.
- `TestInsertWorkspaceConcurrent20`: 20 goroutines, each with its own `*sql.DB` on the same file. Result: unique slots, no `SQLITE_BUSY` surfaced.
- `TestSetStateAuditSameTx`: inject a failure after the state update; neither row persists.
- `TestClaimGoldenSingleBuilder`: 20 concurrent claims → exactly one `Build`.
- `TestClaimGoldenStaleTakeover` (fake clock).
- `TestMarkGoldenDeletingRefusesInUse`.
- `TestMigrationsFromEmptyAndFromS0`.

**Acceptance**
- [ ] All tests pass with `-race`, and the store tests run against a real `modernc.org/sqlite` file.
- [ ] No transaction spans a call outside SQLite.
- [ ] The `audit_log` row for state changes that carry an audit is atomic with the change.

**Pitfalls**
- A deferred transaction that upgrades to a write under concurrency fails with `SQLITE_BUSY` immediately. Use `BEGIN IMMEDIATE`.
- A partial unique index needs the exact same `WHERE` clause that the slot query uses. Otherwise slots leak or collide.
- Store times in UTC (RFC 3339 with nanoseconds) so LRU ordering is a plain string sort.

---

### S1-T03 — Workspace manager: provider selection and create orchestration

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-WS` | `AG-LEAD`, `AG-PLAT`, `AG-SEC` | `S1-E1` | S1-T01, S1-T02 (fake OK), `S0-E5`, `S0-E6` | `feat/workspace-manager` | yes |

**Skills:** `cow-workspaces`, `windows-platform`, `security-review`, `go-conventions`   **Spec:** §1.2, §2.2 (1, 10), §6.1, §6.3, §6.4, §7, §17, §18, §21, §23 (decision 10), §24.2
**Files:** `internal/workspace/manager.go`, `select.go`, `create.go`, `paths.go`, `manager_test.go`, `select_test.go`

**Why / what** — The `Manager` is the single place that turns "make me a workspace" into a safe sequence of steps. It holds the rules that must never break:
- `auto` means `cow_clone` → `git_worktree`, and `projfs` is never chosen;
- never silent NTFS when the Dev Drive is unmounted;
- never a partial `ready`;
- never on `main`/`develop`;
- the path is always confined under the root.

**Steps**
1. `Select(ctx, requested)`:
   ```go
   switch requested {
   case "auto":
   	caps, err := m.cow.Capabilities(ctx, m.cfg.Workspace.Root)
   	if err == nil && caps.CopyOnWrite == CowBlock {
   		return Selection{Provider: m.cow, Caps: caps}, nil
   	}
   	wcaps, werr := m.wt.Capabilities(ctx, m.cfg.Workspace.Root)
   	return Selection{Provider: m.wt, Caps: wcaps,
   		Warning: fmt.Sprintf("no copy-on-write on %q (%s): using git_worktree", m.cfg.Workspace.Root, wcaps.Filesystem)}, werr
   case "cow_clone", "git_worktree":
   	// explicit: honour it or fail; never switch silently (U-PLAT-15 pending)
   case "projfs", "container":
   	return Selection{}, fmt.Errorf("provider %q not available in this build: %w", requested, ErrUnsupportedProvider)
   }
   ```
   Log the fallback with `slog.Warn` and also return it, so the CLI prints it on stderr (§1.2 "explicit warning").
2. **`pending_mount` gate, before any filesystem write.** If `[devdrive].enabled`:
   - ask the `S0-E6` device layer whether the configured VHDX is attached and mounted;
   - check that `VolumeOf(root).ID` is the registered Dev Drive's volume ID.
   If either check fails, insert the row with state `pending_mount`, emit `workspace.pending_mount`, and return `ErrPendingMount`. **Never** call `MkdirAll` before this gate: a mount-point folder under `C:\cf\` exists on NTFS when the VHDX is detached, and `MkdirAll` would quietly succeed there.
3. Validate the request:
   - branch: `CheckBranchName`, and refuse `main`/`develop` with `ErrProtectedBranch`;
   - path: `workspacePath(root, task)` (U-PLAT-11; §5.4 example `<root>/<task-slug>`), then `platform.Confine(root, p)` from `S0-E5`; refuse when the result equals `root` or already exists;
   - base: `ResolveCommit` → a full SHA.
4. `InsertWorkspace` (state `creating`, ports allocated) → `provider.Create(ctx, spec)` → `EnvWriter.Write` (S1-T15) → `SetWorkspaceState(ready)`. Emit **no** event for `ready`: §9.9 has none, and we do not invent events.
5. Compensation on any error after the insert. Do it in a `defer` that checks a named `err`:
   1. the provider's cleanup removes the confined directory (`WorktreeRemove --force`, then `os.RemoveAll`, then `WorktreePrune`);
   2. `DeleteBranchIfAt(branch, baseSHA)` runs only if this operation created the branch;
   3. `SetWorkspaceState(failed, last_error)`.
   The port slot is freed because `failed` is excluded from the live index.
6. Idempotency: `CreateRequest.OpID` comes from the caller. A retry with the same `OpID` returns the `ready` row unchanged, or resumes when the row is `pending_mount`.
7. Logs and metrics (§21): one `slog.Info("workspace created", …)` with `project_id`, `task_id`, `workspace_id`, `provider`, `cow_kind`, `filesystem` and `duration_ms`.
8. Commit: `feat(workspace): add manager with provider selection and safe create`.

**Tests**
- `TestSelectAutoMatrix`, a table over `FakeVolumes` + `FakeCloner`: refs+clone ok → `cow_clone`; ntfs → `git_worktree` + warning; btrfs, apfs → `cow_clone`; xfs with the probe failing → `git_worktree`; ext4 → `git_worktree`; cross-volume (`U-PLAT-4`) → `git_worktree` + warning.
- `TestSelectNeverProjfs`.
- `TestCreatePendingMountNeverWritesNTFS`: the simulated VHDX is detached; assert no directory was created anywhere under the mount point and the row is `pending_mount`.
- `TestCreateRefusesProtectedBranch` for `main` and `develop`.
- `TestCreateRejectsPathEscape` (`..`, absolute slug, symlink root).
- `TestCreateCompensatesOnProviderError`: `FakeCloner.FailAfter = 5`; assert no `ready` row, the directory is gone, the slot is reusable, and the branch is deleted.
- `TestCreateIdempotentOpID`.

**Acceptance**
- [ ] No code path creates a directory before the `pending_mount` gate.
- [ ] On every injected failure, no row is left in `ready` or `creating`.
- [ ] `projfs` and `container` are never selected.

**Pitfalls**
- Checking `os.Stat("X:\\")` is not enough when the Dev Drive is mounted under a folder. Compare volume IDs.
- Compensation must use a fresh context (`context.WithoutCancel(ctx)` plus a timeout). A cancelled `ctx` would otherwise skip the cleanup.
- Never `RemoveAll` a path that fails confinement, even during compensation.

---

### S1-T04 — Git: worktree, export, index and status operations

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-GIT` | `AG-LEAD`, `AG-STORE`, `AG-WS`, `AG-SEC` | `S1-E2` | `S0-E4` | `feat/git-worktree-ops` | yes |

**Skills:** `git-changes-sync`, `security-review`, `go-conventions`   **Spec:** §3.1, §6.1, §7, §8.1, §17, §18.1
**Files:** `internal/git/worktree.go`, `export.go`, `index.go`, `status.go`, `config.go`, `branch.go`, `env.go`, `*_test.go`, `internal/git/testdata/fuzz/`

**Why / what** — Both providers, the golden cache and dirty detection drive Git. This task gives `AG-WS` the §6.4 functions, each one a safe argv call through the `S0-E4` runner. The runner gains optional extra environment variables (`GIT_INDEX_FILE`) and stdin.

**Steps**
1. Extend the runner (**Guidance**): `RunOpts{Env []string; Stdin io.Reader}`.
   - The base environment is `os.Environ()` **minus** every `GIT_DIR`, `GIT_WORK_TREE`, `GIT_INDEX_FILE`, `GIT_OBJECT_DIRECTORY` and `GIT_COMMON_DIR`, plus `GIT_TERMINAL_PROMPT=0` and `LC_ALL=C`.
   - Callers may add only `GIT_INDEX_FILE`.
2. Implement each function with these exact commands. `<repo>`/`<wt>` are absolute paths and `<sha>` is 40 or 64 hex characters, so neither can be read as an option. Assert this in code.

   | Function | Command |
   |---|---|
   | `ResolveCommit` | `git -C <repo> rev-parse --verify --end-of-options <rev>^{commit}` |
   | `BranchExists` | `git -C <repo> show-ref --verify --quiet refs/heads/<name>` (exit 1 = no) |
   | `DeleteBranchIfAt` | `git -C <repo> update-ref -d refs/heads/<name> <sha>` (atomic compare) |
   | `WorktreeAdd` | `git -C <repo> worktree add [--no-checkout] [-b <new>] <path> <commitish>` |
   | `WorktreeRemove` | `git -C <repo> worktree remove [--force] <path>` |
   | `WorktreePrune` | `git -C <repo> worktree prune` |
   | `WorktreeList` | `git -C <repo> worktree list --porcelain -z` |
   | `GitPath` | `git -C <wt> rev-parse --git-path <name>` (resolve a relative result against `<wt>`) |
   | `ConfigSetWorktree` | once: `git -C <wt> config extensions.worktreeConfig true`; then `git -C <wt> config --worktree <key> <value>` |
   | `Head` | `git -C <wt> rev-parse --verify HEAD` |
   | `ExportTree` | `git -C <repo> read-tree --index-output=<indexFile> <sha>`, then `GIT_INDEX_FILE=<indexFile> git -C <repo> -c core.autocrlf=false -c core.longpaths=true --work-tree=<dst> checkout-index --all --force -u` |
   | `RefreshIndex` | `[GIT_INDEX_FILE=<idx>] git -C <repo> --work-tree=<wt> update-index -q --refresh` |
   | `ReadTree` | `git -C <wt> read-tree <args...>` (args from a fixed allow-list: `-mu`, `HEAD`, a hex SHA) |
   | `Status` | `git -C <wt> status --porcelain=v2 -z --untracked-files=all` |
   | `Unpublished` | `git -C <wt> rev-list HEAD --not <base> --glob=<ns>/*` (`<ns>` = `[git].shared_ref_namespace`) |
3. Parse `status --porcelain=v2 -z` records: `1 XY sub mH mI mW hH hI path`, `2 XY … path\0origPath`, `u XY …`, `? path`. Ignored entries (`!`) never appear because we do not pass `--ignored`.
4. Parse `worktree list --porcelain -z`: blocks of `worktree <p>`, `HEAD <sha>`, `branch <ref>` or `detached`, `locked [reason]`, `prunable [reason]`.
5. Every error is typed (`*git.ExitError{Code, Stderr, Args}`). Truncated or unparsable output returns an error and never a partial result.
6. Commit: `feat(git): add worktree, export, index and status operations`.

**Tests** — run with the real `git` in the harness repo:
- `TestWorktreeAddNoCheckout`, `TestWorktreeAddNewBranchFailsIfExists`.
- `TestExportTreeMatchesCheckout`: byte-compare against a normal `git worktree add` checkout.
- `TestExportTreeIndexIsClean`: `git status` with `GIT_INDEX_FILE` and `--work-tree` shows no changes.
- `TestStatusParse` (table: spaces, newline in a file name, unicode, rename, unmerged, untracked directory).
- `FuzzParseStatusV2`, `FuzzParseWorktreeList`.
- `TestUnpublishedIgnoresChangeRefs`: commit, then `git update-ref refs/cowflow/changes/x HEAD` → empty result.
- `TestEnvStripsInheritedGitVars`: run with `t.Setenv("GIT_DIR", …)`; assert it has no effect.
- `TestStatusTruncatedOutputFails`, using the `S0-E8` `GitFault{TruncateAt: 10}`.

**Acceptance**
- [ ] Every command is argv-only through the `S0-E4` runner, and `rg 'sh -c|cmd /c|cmd.exe' internal/git` finds nothing.
- [ ] The fuzz seeds pass in CI, and the parsers never return partial data.
- [ ] `ExportTree` output is byte-identical to a checkout with `core.autocrlf=false`.

**Pitfalls**
- `-c` options must come **before** the subcommand, and `--work-tree` must be absolute.
- Turning on `extensions.worktreeConfig` writes the repo's shared `.git/config`. That is expected (§7 per-worktree config), but read the git-config notes on `core.bare`/`core.worktree` first.
- `checkout-index` runs smudge filters. A Git LFS repo needs `git-lfs` installed, so surface the error and do not hide it.
- Never parse porcelain output without `-z`.

---

### S1-T05 — `git_worktree` provider

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-WS` | `AG-LEAD`, `AG-PLAT`, `AG-SEC` | `S1-E2` | S1-T03, S1-T04 | `feat/workspace-git-worktree-provider` | yes |

**Skills:** `cow-workspaces`, `git-changes-sync`, `security-review`   **Spec:** §6.1, §6.2, §6.3, §7, §8.1
**Files:** `internal/workspace/worktree.go`, `internal/workspace/worktree_test.go`

**Why / what** — This is the universal fallback, available everywhere and never COW. It is also the baseline that S1-T21 compares `cow_clone` against.

**Steps**
1. Write `Capabilities(ctx, root)`, which returns `{CopyOnWrite: CowNone, SparseSupported: true, ProcessIsolation: false, ResourceLimits: false, Filesystem: vol.Filesystem, RequiresDaemon: false}`, where `vol` comes from `Volumes.VolumeOf(root)`.
2. Write `Create`:
   1. `WorktreeAdd(repo, spec.Path, {NewBranch: spec.Branch, Commitish: spec.BaseSHA, NoCheckout: spec.Sparse.Enabled})`. The sparse path is finished in S1-T12.
   2. `ConfigSetWorktree(core.autocrlf=false)`; on Windows also `core.longpaths=true` (§7).
   3. Return a `*Workspace` with `Provider: "git_worktree"` and the `Caps`.
3. Write `Inspect`, which delegates to the shared `inspect.go` (S1-T13), and `Destroy`, which delegates to the shared `destroy.go` (S1-T14). Until those tasks land, both return `errors.ErrUnsupported`.
4. Write the provider's compensation helper, which the `Manager` uses: `WorktreeRemove(force)`, then the confined `RemoveAll`, then `WorktreePrune`.
5. Commit: `feat(workspace): add git_worktree provider`.

**Tests**
- `TestWorktreeCreateIndependentBranch`: two workspaces, two branches, different `HEAD` after a commit in one.
- `TestWorktreeCapabilitiesNone`.
- `TestWorktreeAutocrlfFalse`: `git config --worktree --get core.autocrlf` → `false`.
- `TestWorktreeCreateFailureCompensates`: `GitFault` on `worktree add` exits 128.

**Acceptance**
- [ ] `inspect` on a `git_worktree` workspace never reports `block`, even on ReFS.
- [ ] A failed create leaves no directory and no `.git/worktrees/<name>` entry.

**Pitfalls**
- `git worktree add -b` fails if the branch already exists. Return a conflict error (CLI exit `3`) instead of reusing the branch silently.
- The worktree admin directory is named after the path's base name. Two workspaces with the same base name in different roots get suffixed names, so always use `WorktreeList` and never assume the name.

---

### S1-T06 — Platform: volume information, same-volume check, free space

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-PLAT` | `AG-LEAD`, `AG-WS` | `S1-E3` | `S0-E5`, `S0-E7` | `feat/platform-volume-info` | no |

**Skills:** `windows-platform`, `go-conventions`   **Spec:** §2.2 (10), §6.3, §6.4, §7, §20.13, §20.21
**Files:** `internal/platform/fscaps.go`, `fscaps_windows.go`, `fscaps_linux.go`, `fscaps_darwin.go`, `fscaps_other.go`, `freespace_windows.go`, `freespace_unix.go`, `*_test.go`

**Why / what** — `inspect` must report the real filesystem (§20.13). Block clone needs both trees on one volume (`U-PLAT-4`). The benchmark needs the volume's free bytes to measure incremental disk. The `S0-E7` probe answers "ReFS or not". This task adds volume identity and free space.

**Steps**
1. Windows (`fscaps_windows.go`):
   ```go
   func VolumeOf(ctx context.Context, path string) (Volume, error) {
   	p, _ := windows.UTF16PtrFromString(path)
   	var root [windows.MAX_PATH + 1]uint16
   	if err := windows.GetVolumePathName(p, &root[0], uint32(len(root))); err != nil { return Volume{}, err }
   	var serial, maxComp, flags uint32
   	var fsName [windows.MAX_PATH + 1]uint16
   	if err := windows.GetVolumeInformation(&root[0], nil, 0, &serial, &maxComp, &flags, &fsName[0], uint32(len(fsName))); err != nil { … }
   	const fileSupportsBlockRefcounting = 0x08000000 // not defined in x/sys/windows
   	return Volume{Filesystem: strings.ToLower(windows.UTF16ToString(fsName[:])),
   		ID: fmt.Sprintf("%08x", serial), MountPath: windows.UTF16ToString(root[:]),
   		BlockRefcounting: flags&fileSupportsBlockRefcounting != 0,
   		DevDrive: devDriveState(root), // reuse S0-E7
   		ClusterSize: clusterSize(root)}, nil
   }
   ```
   `clusterSize` calls `GetDiskFreeSpaceW` through `windows.NewLazySystemDLL("kernel32.dll")`, because `x/sys/windows` does not wrap it. Call `Find()` first and return 0 on failure.
2. Linux (`fscaps_linux.go`): call `unix.Statfs`. Map `int64(st.Type)` against `unix.BTRFS_SUPER_MAGIC`, `unix.XFS_SUPER_MAGIC`, `unix.EXT4_SUPER_MAGIC`, `unix.TMPFS_MAGIC` and `unix.OVERLAYFS_SUPER_MAGIC`. The volume ID is `unix.Stat_t.Dev` of the mount root. Set `BlockRefcounting` true only for btrfs and xfs; it is a hint, and the probe clone decides.
3. macOS (`fscaps_darwin.go`): call `unix.Statfs`. `Fstypename` is a fixed byte array, so trim at the first `0`. The volume ID is `Fsid`.
4. `FreeBytes`: on Windows, `windows.GetDiskFreeSpaceEx` (the caller-available value). On Unix, `st.Bavail * uint64(st.Bsize)`.
5. `SameVolume(a, b)`: compare `VolumeOf(a).ID` with `VolumeOf(b).ID`. For a path that does not exist yet, walk up to the nearest existing ancestor first.
6. Commit: `feat(platform): report volume identity, filesystem and free space`.

**Tests**
- On all OSes: `TestVolumeOfTempDir` (non-empty filesystem name), `TestSameVolumeSameDir`, `TestFreeBytesPositive`.
- Gated, skipping with a reason when the environment variable is unset: `TestVolumeOfReFS` with `COWFLOW_TEST_REFS_DIR` → `refs` + `BlockRefcounting`; `TestVolumeOfNTFS` with `COWFLOW_TEST_NTFS_DIR` → `ntfs`, no refcounting; `TestVolumeOfBtrfs` with `COWFLOW_TEST_COW_DIR`.

**Acceptance**
- [ ] Compiles for `windows`, `linux` and `darwin` on amd64 and arm64, and `fscaps_other.go` returns `Filesystem: "unknown"`.
- [ ] Filesystem names are lowercase and stable (they appear in `--json`).

**Pitfalls**
- On some Linux architectures `Statfs_t.Type` is not `int64`. Convert before comparing.
- btrfs subvolumes have different `st_dev` values while sharing one filesystem, so `SameVolume` may say "no" where a clone would work. Treat that as conservative and let the probe clone in S1-T10 decide.
- `GetVolumePathName` on a mount-point folder returns the folder, not `C:\`. That is correct; do not "fix" it.

---

### S1-T07 — Windows block clone (`FSCTL_DUPLICATE_EXTENTS_TO_FILE`)

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-PLAT` | `AG-LEAD`, `AG-WS`, `AG-SEC` | `S1-E3` | S1-T01, S1-T06 | `feat/block-clone-windows` | yes |

**Skills:** `windows-platform`, `cow-workspaces`, `security-review`   **Spec:** §6.3, §6.4, §7, §16, §20.21
**Files:** `internal/workspace/cow_windows.go`, `cow_other.go` (temporary `//go:build !windows`), `cow_windows_test.go`, `cow_conformance_test.go` (shared conformance suite for `cloneFile`, owned by `AG-PLAT`)

**Why / what** — This is the primitive that makes a 1 GB workspace cost metadata instead of data. It must be exact: aligned, same volume, sparse and integrity attributes matching. It must also be honest: unsupported means an error, never a silent copy.

**Steps**
1. Declare the input struct (`x/sys/windows` defines the IOCTL code but not this struct):
   ```go
   //go:build windows
   type duplicateExtentsData struct { // DUPLICATE_EXTENTS_DATA
   	FileHandle       windows.Handle
   	SourceFileOffset int64
   	TargetFileOffset int64
   	ByteCount        int64
   }
   ```
2. Implement `cloneFile`:
   ```go
   func cloneFile(ctx context.Context, src, dst string) (err error) {
   	s, err := os.Open(src) // os handles \\?\ for long paths; raw CreateFile would not
   	if err != nil { return err }
   	defer s.Close()
   	var info windows.ByHandleFileInformation
   	if err = windows.GetFileInformationByHandle(windows.Handle(s.Fd()), &info); err != nil { return err }
   	size := int64(info.FileSizeHigh)<<32 | int64(info.FileSizeLow)
   	d, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o666)
   	if err != nil { return err }
   	defer func() {
   		cerr := d.Close()
   		if err == nil { err = cerr }
   		if err != nil { _ = os.Remove(dst) } // contract rule 5; error already being returned
   	}()
   	dh := windows.Handle(d.Fd())
   	if info.FileAttributes&windows.FILE_ATTRIBUTE_SPARSE_FILE != 0 {
   		if err = ioctl(dh, windows.FSCTL_SET_SPARSE, nil, 0); err != nil { return err }
   	}
   	cluster, err := copyIntegrity(windows.Handle(s.Fd()), dh) // FSCTL_GET/SET_INTEGRITY_INFORMATION; returns ClusterSizeInBytes
   	if err != nil { return mapCloneErr(err) }
   	if err = d.Truncate(size); err != nil { return err }
   	const maxChunk = int64(4 << 30) // each call must cover < 4 GiB
   	step := (maxChunk - int64(cluster)) / int64(cluster) * int64(cluster)
   	for off := int64(0); off < size; off += step {
   		if err = ctx.Err(); err != nil { return err }
   		n := min(step, roundUp(size-off, int64(cluster)))
   		in := duplicateExtentsData{FileHandle: windows.Handle(s.Fd()), SourceFileOffset: off, TargetFileOffset: off, ByteCount: n}
   		var ret uint32
   		if err = windows.DeviceIoControl(dh, windows.FSCTL_DUPLICATE_EXTENTS_TO_FILE,
   			(*byte)(unsafe.Pointer(&in)), uint32(unsafe.Sizeof(in)), nil, 0, &ret, nil); err != nil {
   			return mapCloneErr(err)
   		}
   	}
   	return windows.SetFileTime(dh, &info.CreationTime, &info.LastAccessTime, &info.LastWriteTime)
   }
   ```
3. `mapCloneErr` wraps `ErrCloneUnsupported` for `ERROR_NOT_SUPPORTED`, `ERROR_INVALID_FUNCTION`, `ERROR_NOT_SAME_DEVICE` and `ERROR_INVALID_PARAMETER` when they come from the IOCTL. Every other error passes through with context: `fmt.Errorf("clone %s: %w", src, err)`.
4. Zero-byte files skip the IOCTL. They only get created and have their times set.
5. Write `cow_other.go` (`//go:build !windows`) returning `fmt.Errorf("%w: %s", ErrCloneUnsupported, runtime.GOOS)`.
6. Commit: `feat(workspace): add Windows block clone primitive`.

**Tests**
- `cow_conformance_test.go` runs on every OS, against the directory in `COWFLOW_TEST_COW_DIR`, `COWFLOW_TEST_REFS_DIR` or `t.TempDir()`, and skips with a reason when cloning is unsupported:
  - `TestCloneFileContentEqual`: sizes 0, 1, cluster−1, cluster, cluster+1, and 10 MiB random;
  - `TestCloneFileIsolation`: write to dst; src is unchanged;
  - `TestCloneFileRefusesExisting`, `TestCloneFilePreservesMtime`.
- Windows only:
  - `TestCloneFileSparse`: a sparse source with a hole;
  - `TestCloneFileNTFSUnsupported` with `COWFLOW_TEST_NTFS_DIR`: `errors.Is(err, ErrCloneUnsupported)` and dst is absent;
  - `TestCloneFileCrossVolume`;
  - `TestCloneFileLongPath` (> 240 characters);
  - `TestDuplicateExtentsDataLayout`: `unsafe.Sizeof == 32` and offsets 0/8/16/24;
  - `TestCloneFileOver4GiB` (only when `COWFLOW_TEST_LARGE=1`);
  - `BenchmarkCloneFileSmall` (4 KiB files).

**Acceptance**
- [ ] On ReFS, a cloned 1 GiB file lowers free space by less than 1 MiB (measured in the test with `platform.FreeBytes`).
- [ ] On NTFS, the error is `ErrCloneUnsupported` and no destination file remains.
- [ ] No code path copies bytes.

**Pitfalls**
- Mismatched sparse or integrity-stream settings make the IOCTL fail with `ERROR_INVALID_PARAMETER`. Copy both before cloning.
- `os.File.Fd()` is fine on Windows. Never close the handle yourself: `os.File` owns it.
- ReFS limits how many times one cluster can be shared (see the block-cloning doc). Surface the error, and never retry with a copy.
- Defender and other filter drivers can make each `CreateFile` slow. Record this for S1-T21; do not change Defender settings (`C14`).

---

### S1-T08 — Linux `FICLONE` and macOS `clonefile`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-PLAT` | `AG-LEAD`, `AG-WS`, `AG-SEC` | `S1-E3` | S1-T07 | `feat/clone-linux-darwin` | yes |

**Skills:** `windows-platform` (block clone section), `cow-workspaces`, `security-review`   **Spec:** §6.3, §16, §20.14
**Files:** `internal/workspace/cow_linux.go`, `cow_darwin.go`, `cow_other.go` (narrow the tag to `!windows && !linux && !darwin`), `cow_unix_test.go`

**Why / what** — `cow_clone` must work on all three platforms (§19 Phase 1). The same conformance suite from S1-T07 proves equal behavior.

**Steps**
1. Linux:
   ```go
   //go:build linux
   func cloneFile(ctx context.Context, src, dst string) (err error) {
   	s, err := os.Open(src)
   	if err != nil { return err }
   	defer s.Close()
   	fi, err := s.Stat()
   	if err != nil { return err }
   	d, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fi.Mode().Perm())
   	if err != nil { return err }
   	defer func() { cerr := d.Close(); if err == nil { err = cerr }; if err != nil { _ = os.Remove(dst) } }()
   	if err = unix.IoctlFileClone(int(d.Fd()), int(s.Fd())); err != nil {
   		if errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.EXDEV) || errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOTTY) {
   			return fmt.Errorf("%w: %v", ErrCloneUnsupported, err)
   		}
   		return fmt.Errorf("clone %s: %w", src, err)
   	}
   	return os.Chtimes(dst, fi.ModTime(), fi.ModTime())
   }
   ```
2. macOS: `unix.Clonefile(src, dst, unix.CLONE_NOFOLLOW)` creates `dst` itself. Map `ENOTSUP` and `EXDEV` to `ErrCloneUnsupported`, and `EEXIST` to an existing-file error. Call `os.Chtimes` afterwards to guarantee mtime.
3. Narrow `cow_other.go`'s build tag.
4. Commit: `feat(workspace): add FICLONE and clonefile primitives`.

**Tests** — the S1-T07 conformance suite on:
- the Linux btrfs and XFS loop mounts from S1-T19 (`COWFLOW_TEST_COW_DIR`);
- the ext4 `t.TempDir()`, which expects `ErrCloneUnsupported`;
- the macOS runner's APFS temp dir, which expects success.
Add `TestCloneFileXFSWithoutReflink`: an image made with `mkfs.xfs -m reflink=0` must give unsupported.

**Acceptance**
- [ ] The conformance suite is green on btrfs, XFS (`reflink=1`) and APFS, and reports "unsupported" on ext4 and XFS (`reflink=0`).
- [ ] `CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build ./...` succeeds.

**Pitfalls**
- `clonefile` also clones whole directories. Do not use that on the golden root: sparse filtering and the per-file contract must stay identical across OSes (**Guidance**).
- Linux kernels before 5.18 refuse to reflink across mount points even within one filesystem. The probe clone in S1-T10 catches that.
- `os.File.Fd()` puts the file into blocking mode. That is fine for regular files.

---

### S1-T09 — Golden trees: build once per `base_sha`, LRU GC

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-WS` | `AG-LEAD`, `AG-PLAT`, `AG-SEC` | `S1-E4` | S1-T02, S1-T03, S1-T04 | `feat/workspace-golden-trees` | yes |

**Skills:** `cow-workspaces`, `windows-platform`, `security-review`   **Spec:** §6.4 (Golden trees), §11, §13, §18, §21, §22 (`golden_root`, `golden_cache_gb`), §23 (Open question 6)
**Files:** `internal/workspace/golden.go`, `internal/workspace/golden_test.go`

**Why / what** — Clones need a source. A golden tree is the materialized `base_sha` that every `cow_clone` workspace clones from. Golden trees must have three properties:
- **built once**, even when 20 processes ask for the same SHA at once;
- **never half-visible**: a crash mid-build must not leave a tree that later clones would use;
- **immutable** once ready.

**Steps**
1. `goldenDir(cfg, sha) string`: the only place that knows the location (`C7`). Until `C7` is decided, it reads `[workspace].golden_root` and appends `sha`. The index lives **beside** the tree, at `<goldenDir>.index`, never inside it; otherwise every workspace would receive a copy.
2. `Acquire(ctx, projectID, repo, sha) (g Golden, release func(), err error)`:
   ```go
   for {
   	claim, err := c.store.ClaimGolden(ctx, projectID, sha, opID, c.staleAfter) // staleAfter = command_timeout_seconds
   	switch claim.Action {
   	case domain.GoldenReady:
   		return golden(claim.Tree), func() { _ = c.store.UseGolden(context.WithoutCancel(ctx), projectID, sha, -1) }, nil
   	case domain.GoldenBuild:
   		if err := c.build(ctx, projectID, repo, sha, opID); err != nil { return Golden{}, nil, err }
   		continue // next loop sees Ready and takes an in_use reference
   	case domain.GoldenWait:
   		select { case <-ctx.Done(): return Golden{}, nil, ctx.Err(); case <-c.clock.After(backoff.Next()): } // 100ms → 1s
   	}
   }
   ```
3. `build`:
   1. Create `tmp := goldenDir + ".tmp-" + opID` on the **same volume**. Assert this with `Volumes.SameVolume(tmp, cfg.Workspace.Root)` (`U-PLAT-4`).
   2. `ExportTree(repo, sha, tmp, tmp+".index")`.
   3. Wait at least 1 s on the injected clock, then run `RefreshIndex(repo, tmp, tmp+".index")`. This clears "racily clean" entries (§4.7).
   4. Walk the tree to count files and bytes.
   5. `os.Rename` the index first, then the tree.
   6. `FinishGolden(size, files)`.
   On any error: `os.RemoveAll(tmp)` (confined under the golden root), `FailGolden(reason)`, return the error.
4. `GC(ctx, budgetBytes)`, with the budget from `[scheduler].golden_cache_gb`:
   1. `ListGolden`, then sort by `last_used_at` ascending.
   2. While the total is above the budget, for each golden: `MarkGoldenDeleting` (false → skip, it is in use; `U-PLAT-13`), then rename to `<dir>.deleting-<ulid>`, then `RemoveAll`, then `DeleteGolden`.
   3. Rename first so that no reader ever sees a half-deleted golden.
   4. If removal fails (Windows handles), keep the row in `deleting` and retry on the next GC.
5. Logs and metrics (§21): golden cache hit or miss, `golden_build_ms`, `size_bytes`, `file_count`.
6. Commit: `feat(workspace): add golden tree cache with atomic build and LRU GC`.

**Tests**
- `TestGoldenBuildOncePerSHA`: 20 goroutines with separate store connections → exactly one `ExportTree` call (count it with `FakeGit`), and everyone gets the same path.
- `TestGoldenHalfBuiltNeverVisible`: a `GitFault` makes `checkout-index` exit 1 → no final directory, the row is `failed`, and the temp directory is removed.
- `TestGoldenStaleBuilderTakeover` (fake clock past `command_timeout_seconds`).
- `TestGoldenIndexClean`: real Git; `git status` with `GIT_INDEX_FILE=<golden>.index --work-tree=<golden>` is empty.
- `TestGoldenGCLRUAndBudget`, `TestGoldenGCSkipsInUse`.
- `TestGoldenImmutableAfterWorkspaces`: hash every golden file, create two workspaces, edit files in both, hash again → equal.

**Acceptance**
- [ ] A concurrent `Acquire` builds each SHA exactly once and never returns a partial tree.
- [ ] GC never deletes a golden with `in_use > 0` or in `building`.
- [ ] No code writes into a `ready` golden.

**Pitfalls**
- Never mark golden files read-only to "protect" them. `cloneFile` copies attributes on some OSes, and every workspace would inherit read-only files. Protection comes from the code and from `TestGoldenImmutableAfterWorkspaces`.
- On Windows and macOS, case-colliding paths in the repo (`A.txt` and `a.txt`) overwrite each other during export, and the golden silently differs from the commit. `RefreshIndex` + `git diff-files --quiet` must succeed, or the build fails with an explicit error.
- `os.Rename` on Windows fails while any handle is open inside `tmp` (antivirus, indexer). Retry a few times with a short backoff before failing.

---

### S1-T10 — `cow_clone` provider

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-WS` | `AG-LEAD`, `AG-PLAT`, `AG-SEC` | `S1-E4` | S1-T05, S1-T07, S1-T08, S1-T09 | `feat/workspace-cow-clone` | yes |

**Skills:** `cow-workspaces`, `windows-platform`, `git-changes-sync`, `security-review`   **Spec:** §2.2 (10, 14), §6.1, §6.2, §6.3, §6.4 (Clone), §17, §18, §20.1, §20.2, §20.21
**Files:** `internal/workspace/cowclone.go`, `clone.go` (add `osCloner`), `indexseed.go`, `cowclone_test.go`, `cowclone_bench_test.go`

**Why / what** — This is the heart of the sprint. The provider:
1. registers a Git worktree without checking out any files;
2. clones every golden file into the worktree in parallel;
3. gives the workspace its own index;
4. reports `CowBlock` only when a real probe clone succeeded.

**Steps**
1. `Capabilities(ctx, root)`:
   - get `vol := VolumeOf(root)`;
   - require `SameVolume(golden_root, root)`; otherwise return `CowNone` (`U-PLAT-4`);
   - probe: write 64 KiB of random bytes to `root/.cowflow-probe-<ulid>`, `CloneFile` it, compare the bytes, then delete both files;
   - success → `{CopyOnWrite: CowBlock, SparseSupported: true, Filesystem: vol.Filesystem}`; `ErrCloneUnsupported` → `CowNone`.
   Cache the result per root for the life of the process (**Guidance**).
2. `Create(ctx, spec)`:
   1. `g, release, err := golden.Acquire(...)`, then `defer release()`.
   2. `WorktreeAdd(repo, spec.Path, {NewBranch: spec.Branch, Commitish: spec.BaseSHA, NoCheckout: true})`. The directory now holds only the `.git` file.
   3. `ConfigSetWorktree`: `core.autocrlf=false`, plus `core.longpaths=true` on Windows.
3. Parallel clone walk (**Guidance**: one walker goroutine, `N = 16` workers, tuned by S1-T21):
   ```go
   jobs := make(chan [2]string, 1024)
   var wg sync.WaitGroup
   errc := make(chan error, 1)
   ctx, cancel := context.WithCancel(ctx); defer cancel()
   for range workers {
   	wg.Add(1)
   	go func() {
   		defer wg.Done()
   		for j := range jobs {
   			if err := p.cloner.CloneFile(ctx, j[0], j[1]); err != nil {
   				select { case errc <- err: default: }
   				cancel()
   			}
   		}
   	}()
   }
   walkErr := filepath.WalkDir(g.Dir, func(src string, d fs.DirEntry, err error) error {
   	if err != nil { return err }
   	rel, _ := filepath.Rel(g.Dir, src)
   	if !p.sparse.Match(rel, d.IsDir()) { if d.IsDir() { return filepath.SkipDir }; return nil } // S1-T12
   	dst := filepath.Join(spec.Path, rel)
   	switch {
   	case d.IsDir():             return os.MkdirAll(dst, 0o755)
   	case d.Type()&fs.ModeSymlink != 0: t, err := os.Readlink(src); if err != nil { return err }; return os.Symlink(t, dst)
   	default:
   		select { case jobs <- [2]string{src, dst}: return nil; case <-ctx.Done(): return ctx.Err() }
   	}
   })
   close(jobs); wg.Wait()
   ```
   Collect `walkErr` and the first error from `errc`. Any error triggers the `Manager`'s compensation.
4. Count the cloned files and compare with `golden.file_count` (minus the sparse-excluded files). A mismatch is an error.
5. **Seed the index through `IndexSeeder` (`U-PLAT-1`, not decided here):**
   ```go
   type IndexSeeder interface {
   	Seed(ctx context.Context, wt string, g Golden, baseSHA string) error
   }
   ```
   - **Option A — `readTreeSeeder`:** `ReadTree(wt, baseSHA)`, then `RefreshIndex(repo, wt, "")`. Git builds a fresh index and hashes every file once. It is simple and needs no extra config, but its cost grows with repo size and probably breaks < 2 s on 1 GB.
   - **Option B — `goldenIndexSeeder`:** clone `<golden>.index` to `GitPath(wt, "index")` with the same `Cloner`, then `ConfigSetWorktree(core.checkStat=minimal)` and `ConfigSetWorktree(core.trustCtime=false)`. Cloned files keep the golden's mtime and size, so Git's stat check passes without hashing. It is fast, but it changes two per-worktree Git settings.
   Implement the option the maintainer picks. If no decision exists by day 3, implement both behind the interface (selected by an unexported field set only in tests and benchmarks) and let S1-T21 report the numbers.
6. Verify cheaply in production: `Head(wt) == spec.BaseSHA` and the file count matches. The full `git status` emptiness check runs in tests and in S1-T21. Whether production should also run it is `U-PLAT-12` data.
7. Return `&Workspace{Provider: "cow_clone", Caps: caps, …}`.
8. Add `type osCloner struct{}` calling `cloneFile` (S1-T07/T08), and wire it in `NewManager`.
9. Commit: `feat(workspace): add cow_clone provider with parallel block clone`.

**Tests** — the real-FS tests are gated on `COWFLOW_TEST_COW_DIR` or `COWFLOW_TEST_REFS_DIR`; everything else uses `FakeCloner`:
- `TestCowCloneMatchesGolden`: byte-compare every file; `git status --porcelain` is empty; `HEAD` equals the base.
- `TestCowCloneIsolation`: edit `a.txt` in workspace A → B and the golden are unchanged.
- `TestCowCloneDiskFullCompensates`: `FakeCloner{FailAfter: 100, Err: syscall.ENOSPC}` → no `ready` row and no directory.
- `TestCowCloneCancelMidWalk`: cancel after 50 files → compensation, no goroutine leak (`-race`; count goroutines before and after).
- `TestCowCloneSymlinkReplicated`: Unix always; Windows only with Developer Mode, else skip with a reason.
- `TestIndexSeedersAgree`: both options give a clean status on the same tree.
- `BenchmarkCowCloneCreate` on the S1-T19 `small` profile.

**Acceptance**
- [ ] On ReFS, btrfs, XFS and APFS, `inspect` reports `cow_kind: block` and a clean status right after creation.
- [ ] No byte-copy path exists in production code (reviewer check: `rg "io.Copy" internal/workspace --glob '!*_test.go' --glob '!workspacetest/**'` is empty).
- [ ] Every failure path leaves no directory, no worktree entry and no `ready` row.

**Pitfalls**
- Never copy the golden's `.git` (the golden has none, by design). Sharing an index or `HEAD` between workspaces violates §6.1.
- Never "emulate" COW with hardlinks or symlinks into the golden: a write would change the golden and every other workspace.
- Do not import `golang.org/x/sync/errgroup`: it is not in the fixed stack (§16). Use `sync.WaitGroup` plus an error channel, as shown.
- With option B, the index file itself counts toward incremental disk unless it is cloned too. Clone it; do not copy it.

---

### S1-T11 — Git sparse-checkout operations

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-GIT` | `AG-LEAD`, `AG-STORE`, `AG-WS`, `AG-SEC` | `S1-E5` | S1-T04 | `feat/git-sparse-checkout` | yes |

**Skills:** `git-changes-sync`, `security-review`   **Spec:** §2.2 (14), §6.6, §7, §17
**Files:** `internal/git/sparse.go`, `internal/git/sparse_test.go`

**Why / what** — §6.6 runs `git sparse-checkout set <paths> + <always>`. Paths come from the user (`task create --paths`), so they must be validated and passed safely. A list can also be long enough to overflow the Windows command-line limit.

**Steps**
1. `SparseSet(ctx, wt, dirs)` runs `git -C <wt> sparse-checkout set --cone --stdin`, with the directories newline-separated on stdin.
   - **Guidance:** cone mode is Git's default and recommended mode, and it includes root files automatically (§6.6 "root").
   - Passing directories on stdin avoids both argv-length limits and option injection.
2. `ValidateSparseDir(p string) (string, error)`:
   - convert `\` to `/` and trim a trailing `/`;
   - reject empty strings, absolute paths, drive letters, `..` segments, a leading `-`, glob characters `*?[` (cone mode takes literal directories) and control characters.
3. `SparseDisable(ctx, wt)` runs `git -C <wt> sparse-checkout disable`. `SparseList(ctx, wt)` runs `git -C <wt> sparse-checkout list`.
4. Expose `PopulateSparse(ctx, wt)` = `ReadTree(wt, "-mu", "HEAD")`. It brings the index and the working tree to `HEAD` while honoring the sparse patterns (the `git_worktree` path in S1-T12).
5. Commit: `feat(git): add cone-mode sparse checkout operations`.

**Tests**
- `TestSparseSetMaterializesOnlyCone`: root files plus the listed directories; `git ls-files -t` shows `S` (skip-worktree) for everything else.
- `TestValidateSparseDir` (table, including Windows forms such as `src\auth`, `C:\x` and `..\x`).
- `TestSparseSetFileArgumentRejected`: a file path in `--paths` gives a clear error naming the path.
- `TestSparseDisableRestoresAll`.
- `TestSparseSetLargeList`: 5,000 directories through stdin.
- `TestSparseSetGitFault` (exit code, timeout).

**Acceptance**
- [ ] No user-supplied path ever reaches argv.
- [ ] Validation runs before any Git call, and invalid input means exit `1` (usage) at the CLI.

**Pitfalls**
- Cone mode also includes files that sit *directly* in each parent directory of a listed directory, not just root files. S1-T12's matcher must mirror this exactly.
- `git sparse-checkout set` turns on `extensions.worktreeConfig` by itself. That is consistent with S1-T04.

---

### S1-T12 — Sparse checkout by task paths in both providers

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-WS` | `AG-LEAD`, `AG-PLAT`, `AG-SEC` | `S1-E5` | S1-T05, S1-T10, S1-T11 | `feat/workspace-sparse-paths` | yes |

**Skills:** `cow-workspaces`, `git-changes-sync`, `security-review`   **Spec:** §2.2 (14), §6.6, §14 (`task create --paths`), §19 (Phase 1 "sparse by paths")
**Files:** `internal/workspace/sparse.go`, `sparse_test.go`; edits to `worktree.go`, `cowclone.go`, `manager.go` (`ExpandScope`)

**Why / what** — "Materialize only what is read" (§2.2 principle 14). Intents arrive in Sprint 2. This sprint takes the scope from `task create --paths` (§14) and applies it in both providers. `cow_clone` must then clone exactly the files Git would check out, no more and no fewer.

**Steps**
1. `SparseSet(paths) []string` = normalized task paths ∪ `AlwaysSet()`.
   - `AlwaysSet()` returns only spec-named items: `contracts` and the root (root files and `go.mod` are implicit in cone mode).
   - "configs" waits for `U-PLAT-3`.
2. Enable sparse only when `cfg.Workspace.SparseByIntent && task.Sparse && len(paths) > 0`. `task.Sparse` defaults to true; how a task sets it to false from the CLI is `U-API-14`.
3. Write `coneMatcher.Match(rel string, isDir bool) bool`, mirroring Git's cone rules:
   - a root-level file → true;
   - a directory that is a cone directory, a descendant of one, or an ancestor of one → true;
   - a file whose parent is a cone directory or a descendant of one → true;
   - a file whose parent is an *ancestor* of a cone directory (the "files directly in parents" rule) → true;
   - everything else → false.
4. `git_worktree` order: `WorktreeAdd(NoCheckout)` → `SparseSet` → `PopulateSparse`.
5. `cow_clone` order:
   1. clone only matching files (S1-T10 step 3 calls `Match`);
   2. seed the index;
   3. `SparseSet`, which marks skip-worktree, and since unmatched files are already absent there is nothing to delete;
   4. verify that status is clean in tests.
6. `Manager.ExpandScope(ctx, id, add)`:
   1. validate the new paths;
   2. compute the new set;
   3. for `cow_clone` with `HEAD == base_sha`, clone the newly matched files from the golden (re-`Acquire` it) **before** `SparseSet`, so Git finds them up to date instead of writing full copies;
   4. if `HEAD != base_sha`, let Git materialize them (correctness first);
   5. `SparseSet`.
   The `cowflow_expand_scope` MCP tool and the intent update land in later sprints.
7. Commit: `feat(workspace): apply sparse checkout from task paths`.

**Tests**
- `TestConeMatcherAgreesWithGit`: on the S1-T19 `small` repo with 20 random path sets, compare `Match` with `git ls-files -t` on a sparse `git_worktree` (`H` = materialized). They must agree on every path.
- `TestAlwaysSetIncluded`.
- `TestSparseOffWithoutPaths`, `TestSparseOffByConfig`.
- `TestCowCloneSparseStatusClean`.
- `TestWorktreeSparseOnlyMaterializesScope`.
- `TestExpandScopeAddsFiles` (both providers).
- `TestExpandScopeAfterCommitUsesGit`.

**Acceptance**
- [ ] For both providers, the set of files on disk equals the set Git reports as not skip-worktree.
- [ ] `git status` is clean right after creating a sparse workspace.

**Pitfalls**
- If the matcher includes a file that Git excludes, `sparse-checkout set` deletes it, and you wasted a clone. If the matcher misses a file that Git includes, Git writes a full copy and incremental disk grows. `TestConeMatcherAgreesWithGit` is the guard.
- Path comparison: use `/` internally and convert with `filepath.ToSlash` and `FromSlash` at the edges. On Windows and macOS, case-insensitivity is the filesystem's business; do not lowercase paths yourself.

---

### S1-T13 — Dirty detection and `Inspect`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-WS` | `AG-LEAD`, `AG-PLAT`, `AG-SEC` | `S1-E6` | S1-T04, S1-T05 | `feat/workspace-dirty-detection` | yes |

**Skills:** `cow-workspaces`, `git-changes-sync`, `security-review`   **Spec:** §2.2 (10), §6.1, §17, §20.12, §20.13, §21
**Files:** `internal/workspace/dirty.go`, `inspect.go`, `dirty_test.go`, `inspect_test.go`

**Why / what** — Two features rest on knowing a workspace's real state: refusing unsafe destroys (§20.12) and reporting the truth (§20.13). Both must read Git and the filesystem *now*, not trust the database (§17 "reconciliation reads the actual Git and filesystem").

**Steps**
1. `Dirty(ctx, ws) (DirtyReport, error)`:
   ```go
   entries, err := m.git.Status(ctx, ws.Path)
   if err != nil { return DirtyReport{}, fmt.Errorf("dirty check %s: %w", ws.ID, err) } // fail closed
   var r DirtyReport
   for _, e := range entries {
   	if cowflowOwned(e.Path) { continue } // ".env.cowflow" (S1-T15)
   	r.Uncommitted = append(r.Uncommitted, e.Path)
   }
   r.Unpublished, err = m.git.Unpublished(ctx, ws.Path, ws.BaseSHA, m.cfg.Git.SharedRefNamespace)
   return r, err
   ```
   Ignored files are not counted (`U-PLAT-16`); revisit once that is decided.
2. `Inspect(ctx, id)`:
   - load the row;
   - `vol := VolumeOf(ws.Path)` **now**: `Filesystem = vol.Filesystem`;
   - `CowKind` is what the provider delivered (`cow_clone` → `block`, `git_worktree` → `none`);
   - `Head`, `Dirty`;
   - add a warning when `vol.Filesystem` differs from the row's recorded filesystem, or when the path is missing.
   An unmounted volume gives the state `pending_mount` and the error `ErrPendingMount`, never a crash.
3. Suggested `--json` shape for S1-T17 (**Guidance**):
   ```json
   {"id":"…","task_id":"…","provider":"cow_clone","cow_kind":"block","filesystem":"refs",
    "path":"X:\\ws\\auth","branch":"…","base_sha":"…","head":"…","state":"ready",
    "ports":{"start":40000,"end":40010},"sparse":{"enabled":true,"paths":["src/auth","contracts"]},
    "dirty":{"uncommitted":[],"unpublished":[]},"warnings":[]}
   ```
4. Commit: `feat(workspace): add dirty detection and inspect`.

**Tests**
- `TestDirtyCleanAfterCreate` (both providers).
- `TestDirtyUncommittedKinds` (table: modified, staged, deleted, renamed, untracked in a new directory, a name with a space, newline or unicode).
- `TestDirtyUnpublishedCommit`, `TestDirtyPublishedCommitIsClean` (`git update-ref refs/cowflow/changes/x HEAD`).
- `TestDirtyIgnoresEnvCowflow`.
- `TestDirtyGitFailureFailsClosed` (`GitFault` exit 128, timeout, truncated output).
- `TestInspectReportsActualFilesystem` (`FakeVolumes` changes between create and inspect → new value plus a warning).
- `TestInspectPendingMount`.

**Acceptance**
- [ ] `Dirty` never reports clean when Git failed.
- [ ] `inspect` reads the filesystem at call time.

**Pitfalls**
- `rev-list --glob` with a namespace that has no refs is fine. A **typo** in the namespace silently marks every commit unpublished, so read it only from config.
- Do not cache dirty results: the agent may be writing while you check.

---

### S1-T14 — Destroy: `--force`, rescue, `pending_cleanup`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-WS` | `AG-LEAD`, `AG-PLAT`, `AG-SEC` | `S1-E6` | S1-T02, S1-T13 | `feat/workspace-destroy-rescue` | yes |

**Skills:** `cow-workspaces`, `windows-platform`, `security-review`, `testing-harness`   **Spec:** §7 (Handles), §9.9, §11, §17, §18 (Stuck handle), §20.12
**Files:** `internal/workspace/destroy.go`, `rescue.go`, `destroy_test.go`

**Why / what** — Destroy is the only operation in this sprint that can lose someone's work. The rules (§17, §20.12):
- refuse without `--force` when anything is unpreserved;
- with `--force`, rescue first;
- audit it;
- survive Windows file locks by moving to `pending_cleanup` instead of half-deleting.

**Steps**
1. Load the row. If the state is the proposed `destroyed` → return nil (idempotent). If the state is `pending_cleanup` → skip to step 5 (resume).
2. `r, err := Dirty(...)`. If `err != nil` → refuse: fail closed, even with `--force`.
3. If `!r.Clean() && !opts.Force` → `return &UnpreservedError{Report: r}`. The CLI maps it to the `U-API-12` exit code.
4. If `!r.Clean() && opts.Force` → `loc, err := rescuer.Rescue(ctx, ws, r)`. `U-PLAT-2` is undecided, so the default `Rescuer` returns `ErrRescueUndecided` and destroy stops there. Options to bring to the maintainer (do **not** pick one):
   - (a) `git stash create`, with the resulting commit kept reachable by a ref (this needs a ref-namespace decision; the skill forbids inventing one);
   - (b) a `git diff --binary HEAD` patch plus an archive of untracked files under `[daemon].state_dir`;
   - (c) a commit on a rescue branch.
   Unpublished *commits* are not lost by destroy, because the task branch is kept (step 7).
5. `SetWorkspaceState(…, Audit{Action: "destroy", Force: opts.Force, Dirty: summary, RescueLocation: loc})`. The audit row is in the same transaction (§17).
6. Remove:
   1. `p, err := platform.Confine(root, ws.Path)`; require `p != root`;
   2. `WorktreeRemove(repo, p, true)`;
   3. if the directory still exists → `os.RemoveAll(p)`;
   4. `WorktreePrune`;
   5. remove the per-workspace `cargo_target/<id>` directory (S1-T15), confined to `cargo_target`.
7. Do **not** delete the task branch (no rule allows it, and it holds commits).
8. Handle errors. Classify `ERROR_SHARING_VIOLATION` (32), `ERROR_ACCESS_DENIED` (5), `ERROR_LOCK_VIOLATION` (33) and `ERROR_DIR_NOT_EMPTY` (145) on Windows, and `EBUSY` on Unix, as "stuck handle":
   1. set the state to `pending_cleanup`, emit `workspace.pending_cleanup` with diagnostics (first failing path, error code, hint: "close editors/terminals using this folder");
   2. retry on the injected clock (**Guidance**: 250 ms, 1 s, 4 s);
   3. then return `ErrPendingCleanup` (CLI exit `2`).
   Running `cowflow workspace destroy <id>` again resumes. Background retries arrive with the daemon in Sprint 4.
9. On success: the proposed state `destroyed`; the port slot is freed by the partial index.
10. Commit: `feat(workspace): add safe destroy with rescue hook and pending_cleanup`.

**Tests**
- `TestDestroyRefusesWithoutForce` (table: modified, staged, untracked, unpublished commit): `errors.As(err, *UnpreservedError)`, and the files are still present.
- `TestDestroyCleanSucceeds`, `TestDestroyIdempotent`.
- `TestDestroyForceRescuesBeforeRemoval`: the fake `Rescuer` records the call order, and rescue happens before any `RemoveAll`.
- `TestDestroyForceRescueUndecidedRefuses`.
- `TestDestroyGitFailureRefusesEvenWithForce`.
- `TestDestroyStuckHandlePendingCleanup`: on Windows, hold a file open without `FILE_SHARE_DELETE`; on Unix, use a fake remover returning `EBUSY`; the fake clock drives the retries; assert the event and the state.
- `TestDestroyWritesAudit`.
- `TestDestroyNeverEscapesRoot`: a symlink or junction inside the workspace points outside, and the target survives.
- `TestDestroyKeepsBranch`.

**Acceptance**
- [ ] No code path deletes files before the dirty check, the rescue, and the audit.
- [ ] `--force` on a dirty workspace never deletes anything while `U-PLAT-2` is open.
- [ ] A stuck handle ends in `pending_cleanup` with diagnostics, never in a half-deleted `ready`.

**Pitfalls**
- `os.RemoveAll` must not follow links out of the workspace. Go does not descend into symlinks, and since Go 1.23 Windows junctions are not reported as directories either. Keep the test anyway.
- Do not "fix" a stuck handle by killing processes: that is not in the spec. Report the diagnostics and retry.
- Rescue output must not end up inside `workspace.root`, where a later destroy could delete it.

---

### S1-T15 — Shared dependency cache and `.env.cowflow`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-WS` | `AG-LEAD`, `AG-PLAT`, `AG-SEC` | `S1-E7` | S1-T03 | `feat/workspace-shared-cache` | yes |

**Skills:** `cow-workspaces`, `security-review`   **Spec:** §6.1, §6.4 (step 6), §6.7, §20.17, §22 (`[cache]`, `port_range_*`), §23 (risk "20 × node_modules"), §24.13
**Files:** `internal/workspace/envfile.go`, `cache.go`, `envfile_test.go`, `internal/workspace/testdata/envfile.golden`

**Why / what** — Each workspace receives `.env.cowflow` (§6.1) with its port range and the shared cache locations (§6.7). Tools then reuse downloads instead of fetching them 20 times. This file is how §20.17's "≥ 50% faster second setup" happens.

**Steps**
1. Map `[cache]` keys to standard tool variables (**Guidance**: each tool's documented variable):

   | `[cache]` key | Variable written | Sharing |
   |---|---|---|
   | `go_modcache` | `GOMODCACHE=<path>` | shared (download cache) |
   | `go_cache` | `GOCACHE=<path>` | shared (Go's build cache is safe for concurrent use) |
   | `pnpm_store` | `npm_config_store_dir=<path>` | shared (download cache) |
   | `cargo_target` | `CARGO_TARGET_DIR=<path>/<workspace_id>` | **per workspace** (§6.7) |
   | `gradle_home` | `GRADLE_USER_HOME` | blocked by `C13` |
   | `pip_cache` | `PIP_CACHE_DIR` | blocked by `C13` |
2. Port and identity variables (`U-PLAT-14`, names to be confirmed): proposed `COWFLOW_WORKSPACE_ID`, `COWFLOW_TASK_ID`, `COWFLOW_PORT_RANGE_START`, `COWFLOW_PORT_RANGE_END`.
3. `EnvWriter.Write(ctx, ws)`:
   1. build sorted `KEY=VALUE` lines;
   2. reject values containing `\n`, `\r` or NUL;
   3. require cache paths to be absolute and `filepath.Clean`ed;
   4. `MkdirAll` each cache directory (the per-workspace cargo directory too);
   5. write `.env.cowflow.tmp`, then `os.Rename` it into place (atomic).
4. If `[devdrive].enabled` and a cache path is not on the Dev Drive volume (`SameVolume`), log a warning ("They live on the Dev Drive", §6.7). Never fail on this.
5. Register `.env.cowflow` in `cowflowOwned()` so that dirty detection ignores it (S1-T13). It is also covered by the `.env*` entry that `init` writes to `.gitignore` (§24.13).
6. Commit: `feat(workspace): write .env.cowflow with ports and shared caches`.

**Tests**
- `TestEnvFileGolden`: compare with `testdata/envfile.golden`, using forward slashes normalized per OS.
- `TestEnvFileAtomic`: a fault between write and rename leaves no `.env.cowflow`.
- `TestCargoTargetPerWorkspace`, `TestSharedCachesIdenticalAcrossWorkspaces`.
- `TestEnvRejectsNewlineInValue`, `TestCacheOutsideDevDriveWarns`.

**Acceptance**
- [ ] Two workspaces share `GOMODCACHE`, `GOCACHE` and `npm_config_store_dir`, and have different `CARGO_TARGET_DIR` values.
- [ ] `.env.cowflow` never makes a workspace "dirty".

**Pitfalls**
- Do not put caches inside the workspace: they would be cloned, counted as dirty, and deleted on destroy.
- Config values are trusted (the user wrote them), but they still go through `Clean` and the newline check, because the file is parsed by other tools.
- The runner that *loads* `.env.cowflow` arrives in Sprint 3. Here, the benchmark (S1-T21) and `open` read it.

---

### S1-T16 — CLI: `task create`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-CLI` | `AG-LEAD`, `AG-API`, `AG-WS`, `AG-SEC` | `S1-E8` | S1-T02, S1-T03, S1-T05 | `feat/cli-task-create` | yes |

**Skills:** `cli-and-config`, `cow-workspaces`, `security-review`   **Spec:** §5.3 (daemonless), §6.3, §6.6, §8.1, §14, §17, §24.2
**Files:** `internal/cli/task.go`, `internal/cli/task_test.go`, `internal/cli/testdata/task_create*.golden`

**Why / what** — `task create` is how a human creates a workspace this sprint, and it is the command the demo times. It runs daemonless: the CLI opens SQLite and calls `workspace.Manager` directly (§5.3).

**Steps**
1. Implement these flags: `<slug>`, `--base`, `--title`, `--paths` (repeatable, also comma-separated), and, under the gitflow preset, `--kind` and `--force` (`--force` is audited, §24.2).
   - `--contract`, `--depends`, `--runner` and `--watch` belong to later epics. They stay as the `S0-E1` stubs and return exit `1` with `"not available yet"`. Never accept them and silently ignore them.
2. Flow:
   1. load config (`S0-E2`) and open the store (`S0-E3`);
   2. resolve the project from the current repo;
   3. base = `--base`, or `[project].default_base`;
   4. branch = `BranchNamer.TaskBranch(project, kind, slug)` (`C2`);
   5. `CheckBranchName`;
   6. `CreateTask` with `op_id = ulid`;
   7. `Manager.Create`;
   8. print the result.
3. Exit codes (§14), with **Guidance** on the mapping:

   | Case | Exit |
   |---|---|
   | invalid slug, bad `--paths` | `1` |
   | `ErrPendingMount`, project not initialized, provider unavailable | `2` |
   | slug or branch already exists | `3` |
   | protected branch (`main`/`develop`), non-conforming gitflow name | `4` |
4. Human output (stdout), with any fallback warning on stderr:
   ```text
   task auth created
     workspace  ws_01JB…  X:\ws\auth
     provider   cow_clone (block, refs)
     branch     <per C2>   base develop @ 3f2c9e1
     sparse     src/auth, contracts
     ports      40000-40009
     created in 0.84s
   ```
5. `--json` (one document on stdout):
   ```json
   {"task":{"id":"…","slug":"auth"},"workspace":{"id":"…","path":"…","provider":"cow_clone",
    "cow_kind":"block","filesystem":"refs","branch":"…","base_sha":"…","ports":{"start":40000,"end":40010}},
    "warnings":[],"duration_ms":842}
   ```
6. Commit: `feat(cli): implement task create with workspace creation`.

**Tests**
- `TestTaskCreateJSONGolden`, `TestTaskCreateHumanGolden`.
- `TestTaskCreateExitCodes` (the table above).
- `TestTaskCreateDaemonless`: no daemon process, and the rows exist in SQLite.
- `TestTaskCreateFallbackWarningOnStderr`.
- `TestTaskCreatePendingMountExit2`.
- `TestTaskCreateUnimplementedFlagsExit1`.

**Acceptance**
- [ ] With `--json`, stdout carries exactly one JSON document, and diagnostics go to stderr.
- [ ] No business logic lives in `internal/cli` (the reviewer checks that it only calls `Manager` and `Store`).

**Pitfalls**
- The slug becomes part of a branch **and** of a path. `check-ref-format` validates the branch; path confinement (S1-T03) validates the path. Both are needed.
- Do not print the fallback warning on stdout in `--json` mode: it would break the JSON parsers in scripts.

---

### S1-T17 — CLI: `workspace list|inspect|destroy`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-CLI` | `AG-LEAD`, `AG-API`, `AG-WS`, `AG-SEC` | `S1-E8` | S1-T01 (fakes); S1-T13, S1-T14 to merge | `feat/cli-workspace-commands` | yes |

**Skills:** `cli-and-config`, `cow-workspaces`, `security-review`   **Spec:** §14, §17, §20.12, §20.13
**Files:** `internal/cli/workspace.go`, `internal/cli/workspace_test.go`, `internal/cli/testdata/workspace_*.golden`

**Why / what** — These commands are how a human proves §20.12 and §20.13.

**Steps**
1. `workspace list [--json]` shows the columns `ID TASK PROVIDER COW FS STATE BRANCH PATH`. Destroyed workspaces are hidden. **Guidance:** no extra flags beyond §14.
2. `workspace inspect <id> [--json]` shows the S1-T13 shape. Warnings go to stderr in human mode.
3. `workspace destroy <id> [--force]`:
   - `UnpreservedError` → print the files and commits, plus the hint `use --force to rescue and destroy`, then exit with the `U-API-12` code;
   - `ErrRescueUndecided` → exit `2` with `"rescue is not available yet; nothing was deleted"`;
   - `ErrPendingCleanup` → exit `2` with the diagnostics.
4. The audit actor for daemonless CLI calls is the OS user (**Guidance**).
5. Develop against `workspacetest.FakeManager`. Switch to the real `Manager` once S1-T13 and S1-T14 merge.
6. Commit: `feat(cli): add workspace list, inspect and destroy`.

**Tests**
- `TestWorkspaceListGolden`.
- `TestWorkspaceInspectJSONHasFilesystemAndCowKind`.
- `TestWorkspaceDestroyRefusesWithoutForce`: exit code per `U-API-12`, and the message lists the files.
- `TestWorkspaceDestroyPendingCleanupExit2`.
- `TestWorkspaceDestroyUnknownIDExit2`.

**Acceptance**
- [ ] The refusal output names every unpreserved file (truncated after 10, with a count).
- [ ] `--json` output is stable (golden files).

**Pitfalls**
- Never ask "are you sure?" interactively: agents and CI run this headless. `--force` is the only override.

---

### S1-T18 — CLI: `open <task>` and `golden list|gc`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-CLI` | `AG-LEAD`, `AG-API`, `AG-WS`, `AG-SEC` | `S1-E8` | S1-T03, S1-T09 | `feat/cli-open-golden` | yes |

**Skills:** `cli-and-config`, `cow-workspaces`, `security-review`   **Spec:** §5.4 (Practical limits), §6.4 (Golden trees), §7 (Shell), §11, §14, §17
**Files:** `internal/cli/open.go`, `internal/cli/golden.go`, `*_test.go`

**Why / what** — `open` puts a worker's workspace in front of the human, in VS Code (§5.4). `golden list|gc` makes the golden cache visible and controllable (§14).

**Steps**
1. `open <task>`:
   1. find the task's most recent `ready` workspace;
   2. with `--json`, print `{"path": "…"}` and stop;
   3. otherwise `code, err := exec.LookPath("code")`. If it is not found, print the path and the hint `install the VS Code 'code' command` and exit `0`;
   4. otherwise run `exec.CommandContext(ctx, code, path)`, an argv array with no shell.
   **Guidance:** `code` is the standard VS Code launcher.
2. Windows: `code` resolves to `code.cmd`, a batch file, which Windows runs through `cmd.exe`. §7 forbids building shell strings, so before launching:
   - require the path to match `^[A-Za-z]:\\[A-Za-z0-9_.\\-]+$` (COWFlow generates it from the root and a sanitized slug);
   - otherwise refuse with exit `4` and print the path for the user to open by hand.
   `AG-SEC` must review this branch.
3. `golden list [--json]` shows `SHA SIZE FILES STATE IN_USE LAST_USED`. The `--json` form also carries `path` and `size_bytes`, which the demo uses.
4. `golden gc [--json]` calls `GoldenCache.GC(ctx, golden_cache_gb)` and prints the removed SHAs, the bytes freed, and the goldens skipped because they are in use. No new flags (§14).
5. Commit: `feat(cli): add open and golden list/gc commands`.

**Tests**
- `TestOpenPrintsPathWhenNoEditor`.
- `TestOpenArgvNoShell`: a fake exec seam records the argv.
- `TestOpenRejectsUnsafePathForCmdShim`.
- `TestGoldenListJSONGolden`.
- `TestGoldenGCRespectsBudgetAndInUse`.

**Acceptance**
- [ ] No code path builds a command string. Only argv arrays are used.
- [ ] `golden gc` never removes a golden that is in use.

**Pitfalls**
- Launching an editor must not block the CLI. Use `cmd.Start()` and release the process; do not wait for it.
- §5.4 warns against opening more than 2–3 worktrees per VS Code window. `open` opens one per call; do not add multi-open.

---

### S1-T19 — Benchmark repository generator and COW filesystems in CI

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD`, `AG-SEC` | `S1-E9` | `S0-E8` | `test/bench-repo-generator` | no |

**Skills:** `testing-harness`, `go-conventions`   **Spec:** §18.1, §19 (Phase 1 "on a test repo"), §20 (last line: "numeric targets after benchmarking with a representative repository"), §20.21
**Files:** `test/fixtures/benchrepo/gen.go`, `test/fixtures/benchrepo/cmd/genrepo/main.go`, `test/fixtures/benchrepo/gen_test.go`, `test/fixtures/goproxy/gen.go`, `.github/workflows/ci.yml` (new jobs)

**Why / what** — §20.21 needs a repo of at least 1 GB. Public repos are a poor default: they are big downloads, they change over time, and some contain paths that Windows cannot create (for example the Linux kernel's `aux.c`). A deterministic generator gives every machine the same repo, offline.

**Steps**
1. `benchrepo.Generate(ctx, dir string, p Profile, seed int64) (Report, error)` writes a `git fast-import` stream (fast, one process, no per-file `git add`) to `git -C <dir> fast-import`:
   - two commits on `main`, then `develop` branched from it, plus a third commit on `develop` that changes 1% of the files (for `TestOQ6_GoldenPerBaseSHA`);
   - `.gitattributes` with `* text=auto eol=lf` (§7), a root `go.mod`, a `contracts/` directory, and `src/<area>/<pkg>/...` directories (so sparse has structure);
   - Windows-safe names only: no `CON`, `PRN`, `AUX`, `NUL`, `COM1`–`9`, `LPT1`–`9`, no trailing dot or space, no `:*?"<>|`, no case collisions; repo-relative paths ≤ 180 characters;
   - symlinks only with `--symlinks` (Unix tests).
2. Profiles:

   | Profile | Bytes | Files | Use |
   |---|---|---|---|
   | `small` | ~50 MiB | ~2,000 | unit and CI tests |
   | `large` | ~1.2 GiB | ~6,000 (50 KiB–5 MiB) | AC 21, file-size bound |
   | `monorepo` | ~1.2 GiB | ~60,000 (log-normal, median ~8 KiB) | AC 21 / Phase 1, file-count bound |
3. `genrepo --profile monorepo --seed 1 --out <dir>` prints a JSON `Report{bytes, files, head_tree, branches}`. `genrepo --verify <repo>` sums `git ls-tree -r -l HEAD` for any repo, so a real repo can be checked against "≥ 1 GB" too (environment variable `COWFLOW_BENCH_REPO`).
4. `goproxy.Generate(dir, n)` builds a file-based `GOPROXY` (`<module>/@v/list`, `.info`, `.mod`, `.zip` via `archive/zip`) with N synthetic modules and a consumer module that imports them all. It is the hermetic fixture for AC 17. Use `GOPROXY=file:///…`, `GOSUMDB=off`, `GOFLAGS=-mod=mod`, `GOTOOLCHAIN=local`.
5. CI jobs, so the S1-T07, S1-T08 and S1-T20 tests run on real COW filesystems:
   ```yaml
   cow-linux:
     runs-on: ubuntu-latest
     strategy: { matrix: { fs: [btrfs, xfs] } }
     steps:
       - uses: actions/checkout@v4
       - uses: actions/setup-go@v5
         with: { go-version-file: go.mod }
       - name: Create ${{ matrix.fs }} loop filesystem
         run: |
           sudo apt-get update && sudo apt-get install -y btrfs-progs xfsprogs
           truncate -s 8G "$RUNNER_TEMP/cow.img"
           if [ "${{ matrix.fs }}" = btrfs ]; then mkfs.btrfs -q "$RUNNER_TEMP/cow.img"; else mkfs.xfs -q -m reflink=1 "$RUNNER_TEMP/cow.img"; fi
           sudo mkdir -p /mnt/cow && sudo mount -o loop "$RUNNER_TEMP/cow.img" /mnt/cow && sudo chown "$USER" /mnt/cow
       - run: CGO_ENABLED=0 COWFLOW_TEST_COW_DIR=/mnt/cow go test -race ./internal/workspace/... ./test/integration/...
   ```
   - macOS: the runner's temp directory is APFS, so set `COWFLOW_TEST_COW_DIR=$RUNNER_TEMP/cow`.
   - Windows (hosted, admin): create a ReFS VHDX with the `S0-E6` code (`go run ./cmd/cowflow devdrive create …`). If the image lacks Dev Drive support, format plain ReFS in the workflow (test-only). Set `COWFLOW_TEST_REFS_DIR`, and set `COWFLOW_TEST_NTFS_DIR=$RUNNER_TEMP`.
   - **Guidance:** official AC 21 numbers come from the self-hosted `win11-devdrive` runner (S1-T21).
6. Commit: `test(fixtures): add deterministic benchmark repo generator and COW CI jobs`.

**Tests**
- `TestGenerateDeterministic`: same seed → same `head_tree`.
- `TestGenerateSmallProfile` (size within ±5%).
- `TestGenerateWindowsSafeNames`.
- `TestGoproxyBuildsOffline`: `go build` of the consumer with the network disabled through env.

**Acceptance**
- [ ] `genrepo --profile monorepo` produces ≥ 1 GiB of checkout in under 10 minutes on a laptop.
- [ ] The CI jobs run the conformance suites on btrfs, XFS, APFS and ReFS, and skip with a clear reason where a filesystem is unavailable.

**Pitfalls**
- Random content compresses badly, which is exactly what we want: Git packs stay realistic and the checkout really is ≥ 1 GB. Do not use zero-filled files; some filesystems store them sparsely and fake the numbers.
- Never fetch from the network in tests (`testing-harness`).

---

### S1-T20 — Acceptance tests (AC 1, 2, 12, 13) and harness failure scenarios

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD`, `AG-SEC`, `AG-WS` | `S1-E9` | S1-T05, S1-T10, S1-T13, S1-T14, S1-T16, S1-T17, S1-T19 | `test/workspace-acceptance` | no |

**Skills:** `testing-harness`, `cow-workspaces`   **Spec:** §18 (rows: disk full / provider fails, stuck handle, VHDX not mounted), §18.1, §20.1, §20.2, §20.12, §20.13
**Files:** `test/integration/workspace_ac_test.go`, `test/integration/workspace_faults_test.go`, `test/harness/` (helpers only)

**Why / what** — The unit tests prove the parts. These tests prove the §20 criteria end to end, through the CLI entry point, on every CI filesystem. Every test name and comment carries its criterion (`// AC#2`).

**Steps**
1. Helper `runCLI(t, args ...string) (stdout, stderr string, code int)`. It builds `cli.NewRootCmd` with buffers, against a harness repo (S1-T19 `small`) and a fresh SQLite file per test.
2. Run each AC test twice, as subtests `provider=git_worktree` (always) and `provider=cow_clone` (only when `COWFLOW_TEST_COW_DIR` or `COWFLOW_TEST_REFS_DIR` is set; otherwise skip with a reason).
3. Tests:
   - `TestAC01_TwentyIndependentBranches`: 20× `task create t01..t20` → 20 distinct branches; `git worktree list --porcelain` shows 20 extra entries; one commit in `t05` changes no other workspace's `HEAD`.
   - `TestAC02_SameFileTwoWorkspaces`: append different text to the same file in `t01` and `t02`; each file holds only its own text; `git status` in each lists only that file; the golden checksum and the main checkout are unchanged.
   - `TestAC12_DestroyRefusesUnpreserved`: a table of modified, staged, untracked and unpublished-commit cases; `workspace destroy` exits with the `U-API-12` code and every file is still on disk; a clean workspace exits `0` and its directory is gone.
   - `TestAC13_InspectReportsFilesystemAndCowKind`: `workspace inspect --json` reports `filesystem` equal to `platform.VolumeOf(path).Filesystem` and `cow_kind` equal to `block` (cow_clone) or `none` (git_worktree). On Windows CI, also with `COWFLOW_TEST_NTFS_DIR` → `ntfs`/`none`.
4. Fault scenarios (partial §18 coverage; the full table is `S6-E4`):
   - `TestFault_DiskFullDuringCloneNoPartialReady` (`FakeCloner{FailAfter, ENOSPC}`, or the harness disk-full FS);
   - `TestFault_GitWorktreeAddTimeout` (`GitFault{Timeout: true}`);
   - `TestFault_TruncatedStatusFailsClosed`: destroy is refused;
   - `TestFault_VHDXNotMountedPendingMount` (simulated VHDX from `S0-E8`): exit `2`, state `pending_mount`, and **no** directory under the mount point;
   - `TestFault_StuckHandlePendingCleanup`: exit `2`, state `pending_cleanup`, the event is recorded, and a retry after release succeeds.
5. Commit: `test(integration): cover workspace acceptance criteria and failures`.

**Tests** — this task *is* tests. Run `go test -race ./test/integration/... -run 'TestAC|TestFault' -v` locally on your OS, then confirm the CI matrix is green.

**Acceptance**
- [ ] AC 1, 2, 12 and 13 pass on Windows, Linux and macOS CI, including at least one real COW filesystem per OS.
- [ ] Every fault test asserts the absence of partial state (rows, directories, worktree entries), not just an error.

**Pitfalls**
- Do not share one repo or one SQLite file between `t.Parallel()` tests.
- On Windows, close every file handle the test opened before cleanup, or `t.TempDir()` removal fails the test.

---

### S1-T21 — Benchmarks: 20 workspaces < 2 s, incremental disk < 1%, shared cache ≥ 50%

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD`, `AG-SEC`, `AG-PLAT`, `AG-WS` | `S1-E9` | S1-T10, S1-T12, S1-T15, S1-T16, S1-T19 | `test/workspace-benchmarks` | no |

**Skills:** `testing-harness`, `cow-workspaces`, `windows-platform`   **Spec:** §19 (Phase 1 output), §20.17, §20.21, §21, §23 (Open question 6)
**Files:** `test/integration/bench/bench_test.go` (`//go:build cowflowbench`), `test/integration/bench/result.go`, `.github/workflows/bench.yml` (`workflow_dispatch`, self-hosted `win11-devdrive`, plus the Linux btrfs/XFS and macOS jobs)

**Why / what** — The sprint's exit criterion is a measurement. This task builds the instrument: timed creation, a disk-delta method that works per OS, a hermetic cache experiment, and machine-readable results attached to the closing PR.

**Steps**
1. Gate on the build tag `cowflowbench` and the environment variables `COWFLOW_BENCH_ROOT` (a directory on the COW volume), `COWFLOW_BENCH_REPO` (optional, otherwise the generator is used) and `COWFLOW_BENCH_OUT` (results directory).
2. Measure disk with `platform.FreeBytes(root)` before and after each create:
   - Linux: run `unix.Syncfs` on a root directory fd first, because btrfs updates free space lazily;
   - macOS: use a dedicated APFS volume, and turn off Spotlight on it with `mdutil -i off <volume>`;
   - Windows: ReFS free space is immediate.
   `incremental_pct = Δfree / golden_bytes × 100`. Take the median of 3 runs.
3. `TestPhase1_TwentyCowWorkspaces`:
   1. `task create warm` builds the golden; record `golden_build_ms` separately;
   2. create 20 workspaces sequentially, recording per create `create_ms` (in-process `Manager.Create`), `cli_ms` (wall time of `cowflow task create` as a child process, second run) and `dfree_bytes`;
   3. assert `max ≤ 2000 ms` on the timing chosen for `U-PLAT-12`, and `incremental_pct < 1`;
   4. also run 20 creates **concurrently** and report the numbers without asserting (`U-PLAT-12`).
4. `TestAC21_CowCloneLargeRepo`: profiles `large` and `monorepo` (≥ 1 GiB), one create after the golden is warm, < 2 s and < 1%. Record `files` and `bytes` so per-file metadata cost is visible.
5. `TestAC17_SharedCacheSecondSetup`, using the S1-T19 GOPROXY fixture (offline). Under `U-PLAT-12`, "setup" means workspace create + `go mod download` + `go build ./...`.
   - Scenario **isolated**: each workspace gets a fresh, private `GOMODCACHE`/`GOCACHE`. Measure workspace 2.
   - Scenario **shared**: `.env.cowflow` values; workspace 1 warms the caches. Measure workspace 2.
   - Assert `shared_ws2 ≤ 0.5 × isolated_ws2`.
6. `TestOQ6_GoldenPerBaseSHA` (data only, no assertion): create from `develop` and from `develop~1` (1% of files differ). Report the second golden's build time and disk. This feeds SCOPE.md §23 Open question 6.
7. If `U-PLAT-1` is still open, run `TestPhase1_…` once per `IndexSeeder` and report both.
8. Result JSON per test: `{goos, filesystem, dev_drive, defender_excluded (from env COWFLOW_BENCH_DEFENDER_EXCLUDED, C14), git_version, cpu, ram_gb, repo_profile, bytes, files, golden_build_ms, creates:[{create_ms, cli_ms, dfree_bytes}], p50_ms, p95_ms, max_ms, incremental_pct, seeder, commit}`.
9. Commit: `test(bench): measure workspace creation time, disk and cache gain`.

**Tests** — run locally:
```bash
COWFLOW_BENCH_ROOT=/mnt/cow/bench COWFLOW_BENCH_OUT=$PWD/bench-results \
  go test -tags cowflowbench -run 'TestPhase1_|TestAC21_|TestAC17_|TestOQ6_' -v -timeout 90m ./test/integration/bench/
```

**Acceptance**
- [ ] Result JSON exists for Windows Dev Drive, Linux btrfs, Linux XFS and macOS APFS, and is attached to the sprint-closing PR.
- [ ] Every assertion passes on the reference machines, or `AG-LEAD` has reported the gap to the maintainer with the data (§20: "additional numeric targets after benchmarking").

**Pitfalls**
- The first create after a reboot reads cold metadata. Warm up once, and report the cold number separately.
- Defender real-time scanning and the Windows Search indexer skew Windows timings. Record the state, and never change Defender settings (`C14`).
- Per-file metadata on ReFS and APFS is not free. On the `monorepo` profile it may approach the 1% budget, so report bytes per file and let the data speak (risk R2).
- `bench-results/` must not be committed. Upload it as a CI artifact and attach it to the PR.

## 8. Demo script

Run on a clean Windows 11 22H2+ machine with admin rights, from the repo root, with the `develop` that contains all Sprint 1 PRs. Record the transcript and the `bench-results/*.json` files in the sprint-closing PR.

```powershell
# 0. Build and check the toolchain
git --version                       # >= 2.40
go version                          # go1.25.x
$env:CGO_ENABLED = "0"
go build -o C:\cf\bin\cowflow.exe ./cmd/cowflow
$env:PATH = "C:\cf\bin;$env:PATH"

# 1. Benchmark repo + Dev Drive (§20.21, Dev Drive part — built in S0-E6)
go run ./test/fixtures/benchrepo/cmd/genrepo --profile monorepo --seed 1 --out C:\cf\repos\bench
#   -> {"bytes":12xxxxxxxx,"files":6xxxx,"branches":["main","develop"],...}
Set-Location C:\cf\repos\bench
cowflow init --devdrive 60GB --letter X
cowflow doctor                      # expect: Dev Drive X: mounted, ReFS, block clone supported
```

```powershell
# 2. Warm the golden tree for develop (paid once)
Measure-Command { cowflow task create warm --base develop --title "warm golden" } | Select-Object TotalSeconds
cowflow golden list                 # one row: <sha>  1.2 GB  6xxxx files  ready  in_use 0
$goldenBytes = (cowflow golden list --json | ConvertFrom-Json)[0].size_bytes

# 3. Capability check
cowflow workspace inspect (cowflow workspace list --json | ConvertFrom-Json)[0].id --json
#   -> "provider":"cow_clone","cow_kind":"block","filesystem":"refs"
```

```powershell
# 4. Phase 1 output: 20 COW workspaces, each < 2 s, incremental disk < 1%
New-Item -ItemType Directory -Force C:\cf\out | Out-Null
$before = (Get-Volume -DriveLetter X).SizeRemaining
1..20 | ForEach-Object {
  $n = "t{0:D2}" -f $_
  $s = Measure-Command { cowflow task create $n --base develop --title "bench $n" --json | Out-File "C:\cf\out\$n.json" }
  "{0}  {1:N2}s" -f $n, $s.TotalSeconds          # expect every line < 2.00s
}
$after = (Get-Volume -DriveLetter X).SizeRemaining
"incremental per workspace: {0:N3}%" -f ((($before - $after) / 20) / $goldenBytes * 100)   # expect < 1%

$env:COWFLOW_BENCH_ROOT = "X:\bench"; $env:COWFLOW_BENCH_OUT = "C:\cf\out\bench"
go test -tags cowflowbench -run 'TestPhase1_|TestAC21_|TestAC17_|TestOQ6_' -v -timeout 90m ./test/integration/bench/
#   -> PASS lines with p50/p95/max ms and incremental_pct; JSON files in C:\cf\out\bench
```

```powershell
# 5. Isolated editing (§20.2)
$A = Get-Content C:\cf\out\t01.json | ConvertFrom-Json
$B = Get-Content C:\cf\out\t02.json | ConvertFrom-Json
$f = "go.mod"
$golden = (cowflow golden list --json | ConvertFrom-Json)[0].path              # location per C7
$goldenHash = (Get-FileHash "$golden\$f").Hash
Add-Content "$($A.workspace.path)\$f" "// edited in A"
Add-Content "$($B.workspace.path)\$f" "// edited in B"
git -C $A.workspace.path status --short                                        # ->  M go.mod
git -C $B.workspace.path status --short                                        # ->  M go.mod
Select-String "edited in A" "$($B.workspace.path)\$f"                          # -> no output
(Get-FileHash "$golden\$f").Hash -eq $goldenHash                               # -> True (golden untouched)
```

```powershell
# 6. Never destroy unpreserved work without --force (§20.12)
cowflow workspace destroy $A.workspace.id; $LASTEXITCODE
#   -> refused: 1 uncommitted file (go.mod); use --force to rescue and destroy   exit = U-API-12 code
git -C $A.workspace.path commit -qam "wip"; cowflow workspace destroy $A.workspace.id; $LASTEXITCODE
#   -> refused: 1 unpublished commit                                              exit = U-API-12 code
$C = Get-Content C:\cf\out\t03.json | ConvertFrom-Json
cowflow workspace destroy $C.workspace.id; $LASTEXITCODE                         # -> destroyed, exit 0
cowflow workspace destroy $B.workspace.id --force; $LASTEXITCODE
#   -> if U-PLAT-2 decided: "rescued to <location>", destroyed, exit 0
#   -> otherwise: "rescue is not available yet; nothing was deleted", exit 2
```

```powershell
# 7. inspect reports the real filesystem and CowKind (§20.13): ReFS vs NTFS
cowflow workspace inspect ((Get-Content C:\cf\out\t04.json | ConvertFrom-Json).workspace.id) --json
#   -> "provider":"cow_clone","cow_kind":"block","filesystem":"refs"
git clone -q C:\cf\repos\bench C:\cf\repos\bench-ntfs; Set-Location C:\cf\repos\bench-ntfs
cowflow init                         # no --devdrive; then set [workspace] root/golden_root to C:/cf/ws-ntfs, C:/cf/golden-ntfs (config location per U-API-1)
cowflow task create n01 --base develop --title "ntfs"
#   stderr -> warning: no copy-on-write on "C:/cf/ws-ntfs" (ntfs): using git_worktree
cowflow workspace inspect (cowflow workspace list --json | ConvertFrom-Json)[0].id --json
#   -> "provider":"git_worktree","cow_kind":"none","filesystem":"ntfs"
Set-Location C:\cf\repos\bench
```

```powershell
# 8. Unmounted Dev Drive -> pending_mount, never NTFS (§6.4, §18)
cowflow devdrive detach
cowflow task create t99 --base develop --title "unmounted"; $LASTEXITCODE    # -> pending_mount, exit 2
Test-Path X:\ws\t99                                                          # -> False (and nothing on C:)
cowflow devdrive attach
cowflow task create t99 --base develop --title "unmounted"; $LASTEXITCODE    # -> created, exit 0

# 9. Shared cache (§20.17) — read from the bench output of step 4
Get-Content C:\cf\out\bench\TestAC17_SharedCacheSecondSetup-windows-refs.json
#   -> "isolated_ws2_ms": <a>, "shared_ws2_ms": <b>, with b <= 0.5 * a

# 10. open + golden
cowflow open t04                     # VS Code opens X:\ws\t04
cowflow golden gc                    # -> nothing removed while in budget; in-use goldens skipped
```

**Linux notes (btrfs or XFS):**
```bash
truncate -s 20G /var/tmp/cf.img && mkfs.btrfs -q /var/tmp/cf.img      # or: mkfs.xfs -q -m reflink=1 /var/tmp/cf.img
sudo mkdir -p /mnt/cf && sudo mount -o loop /var/tmp/cf.img /mnt/cf && sudo chown "$USER" /mnt/cf
```
Set `[devdrive] enabled = false`, `[workspace] root = "/mnt/cf/ws"` and `golden_root = "/mnt/cf/golden"`. Run steps 1–7 and 9–10 with `bash` equivalents (`time cowflow task create …`, `df -B1 --output=avail /mnt/cf` after `sync`). The expected values are `filesystem: btrfs|xfs` and `cow_kind: block`. Negative check: a root on ext4 → `git_worktree` + warning.

**macOS notes (APFS):** use a dedicated APFS volume (`diskutil apfs addVolume disk1 APFS cowflow`) and turn off Spotlight on it (`sudo mdutil -i off /Volumes/cowflow`). Expect `filesystem: apfs` and `cow_kind: block`. Measure free space with `df -k /Volumes/cowflow`.

## 9. Definition of Done

- [ ] Every task S1-T01…S1-T21 is merged into `develop` through a reviewed PR (`AG-LEAD`, plus the pod peer and `AG-SEC` where listed), with signed, signed-off Conventional Commits and no AI attribution.
- [ ] Every §2 exit-criterion box is ticked, with evidence (transcript plus `bench-results/*.json`) in the sprint-closing PR.
- [ ] `go build ./...`, `go test -race -cover ./...`, `golangci-lint run`, `go vet ./...` and `govulncheck ./...` are clean, and `go mod tidy` is a no-op. No new dependency was added.
- [ ] `CGO_ENABLED=0` builds pass for windows, linux and darwin on amd64 and arm64.
- [ ] CI is green on Windows, Linux and macOS, including the COW filesystem jobs (ReFS, btrfs, XFS, APFS).
- [ ] `WorkspaceProvider` and `Capabilities` match SCOPE.md §6.2 exactly. `auto` = `cow_clone` → `git_worktree`, and `projfs` is never automatic.
- [ ] No partial `ready` on any injected failure; `pending_mount` never writes to NTFS; `pending_cleanup` carries diagnostics.
- [ ] `destroy` never deletes unpreserved work without `--force`, and `--force` never deletes without a rescue. While `U-PLAT-2` is open, `--force` on a dirty workspace is refused.
- [ ] Every Git call is argv-only through the `S0-E4` runner. Paths are confined. User paths go to Git through stdin, not argv.
- [ ] Each SPEC-GAPS item in §3.3 is either decided by the maintainer (recorded in SCOPE.md §23 and removed from SPEC-GAPS.md in the same PR) or still behind its interface. The gaps found while planning (`U-API-12`, `U-API-14`, `U-PLAT-11`…`U-PLAT-16`) are likewise decided or still open in SPEC-GAPS.md.
- [ ] `CHANGELOG.md` `[Unreleased]` lists the new commands (`task create`, `workspace list|inspect|destroy`, `open`, `golden list|gc`) and the `cow_clone`/`git_worktree` providers.
- [ ] Retro: learnings are moved into `docs/skill/cow-workspaces` and `docs/skill/windows-platform` (for example the measured per-file costs and the chosen `IndexSeeder`).

## 10. Risks and mitigations

| # | Risk | Impact | Mitigation |
|---|---|---|---|
| R1 | A many-small-files repo misses < 2 s: ~60k files is ~33 µs per file on Windows, where `CreateFile` plus filters can dominate | §19 exit criterion | parallel workers (S1-T10), tuned by S1-T21; Dev Drive filter-light mode; sparse checkout; report `large` vs `monorepo` separately so the maintainer sees the curve |
| R2 | Per-file metadata on ReFS/APFS pushes incremental disk past 1% on `monorepo` | §20.21 | measure bytes per file; clone (not copy) the index; report against both profiles; escalate with data (§20 last line) instead of tuning the test |
| R3 | `U-PLAT-1` stays undecided, and option A (re-hash) is too slow | §19, §20.21 | `IndexSeeder` with both options; S1-T21 numbers support the decision |
| R4 | Rescue (`U-PLAT-2`) is undecided by the demo | §20.12 `--force` path incomplete | refusal-only behavior is safe and still satisfies §20.12; the demo shows the explicit refusal |
| R5 | Hosted Windows CI cannot create a Dev Drive | no automated Windows COW coverage | plain-ReFS VHDX for functional tests; self-hosted `win11-devdrive` runner for numbers |
| R6 | `AG-WS` overload (9 tasks) | critical-path slip | `AG-PLAT` pairs on S1-T10; S1-T13 and S1-T15 are small; `AG-LEAD` re-sequences on D6 if S1-T09 is late |
| R7 | Golden built twice or half-visible under concurrent daemonless CLIs | corrupted workspaces | SQLite claim (S1-T02), temp-dir + rename (S1-T09), `TestGoldenBuildOncePerSHA` |
| R8 | Silent fallback to NTFS when the VHDX is detached (mount folder exists on C:) | data on the wrong volume; §18 violation | volume-ID gate before any write (S1-T03); `TestFault_VHDXNotMountedPendingMount` |
| R9 | Windows file locks (editor, antivirus, indexer) block destroy or golden rename | stuck cleanups | `pending_cleanup` with retries and diagnostics; rename retries; documented hint |
| R10 | Cone-mode matcher diverges from Git | wasted clones or full copies, dirty status | `TestConeMatcherAgreesWithGit` on random path sets |
| R11 | `extensions.worktreeConfig` changes the user's `.git/config` | surprises for users with exotic configs | expected per §7; note in the PR and CHANGELOG; test on a repo with `core.bare=false` only |
| R12 | Case-colliding or Windows-reserved paths in real repos break golden export | golden differs from the commit | generator avoids them; golden build verifies with `diff-files` and fails explicitly |
| R13 | `C2` (branch naming) unresolved | `task create` cannot ship its final branch format | `BranchNamer` interface; the demo uses the decided namer; escalated on D1 |
| R14 | `code.cmd` on Windows routes through `cmd.exe` | command injection via the path | strict path allow-list and `AG-SEC` review (S1-T18); otherwise print the path |
