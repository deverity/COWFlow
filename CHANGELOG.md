# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `test/integration`: end-to-end tests of acceptance criteria 1, 2, 12 and 13
  through the in-process CLI entry point (`task create`, `workspace
  inspect|destroy|list`) on the generated `small` repository, as
  `provider=git_worktree` and, on a copy-on-write volume
  (`COWFLOW_TEST_COW_DIR` or `COWFLOW_TEST_REFS_DIR`), `provider=cow_clone`;
  on Windows `COWFLOW_TEST_NTFS_DIR` adds the NTFS `ntfs`/`none` check. Fault
  tests cover disk full during a clone, a `git worktree add` timeout, a
  truncated `git status`/`rev-list` on destroy, a detached Dev Drive
  (`pending_mount`) and a stuck handle (`pending_cleanup`), and assert that no
  partial row, directory, worktree entry or branch is left behind.
- `test/harness`: `HermeticGit` (the hermetic Git environment and runner of
  `NewRepo`, for repositories made by other means), `ScratchDir` (tmpfs on
  Linux, else a temp directory), `FaultyRunner.Clear` and `FaultyOptsRunner`
  (a `FaultyRunner` with per-call options), so faults also reach the index,
  export, snapshot and sparse plumbing.
- `internal/git`: snapshot plumbing for the workspace rescue (`AddAll`,
  `WriteTree`, `CommitTree`, `UpdateRef`), so `destroy --force` on a dirty
  workspace can save its changes. The commit identity is set per call and
  wins over inherited `GIT_AUTHOR_*` / `GIT_COMMITTER_*`.
- `internal/store`: `DeleteTask` removes a task and its failed workspace rows
  in one transaction, and refuses with `ErrTaskHasWorkspaces` when any other
  workspace row exists. `UpdateWorkspaceSparsePaths` stores the widened scope
  of a ready workspace (for `ExpandScope`).
- `cowflow workspace list|inspect|destroy`: list the project's workspaces
  (destroyed ones are hidden), inspect one as it is now (filesystem, HEAD,
  dirty state, `logs_dir`) and destroy it. A destroy with unpreserved changes
  names the files and commits, truncated after ten, and exits 4; `--force`
  rescues them to `refs/cowflow/rescue/<id>` first. A rescue failure,
  `pending_cleanup` or `pending_mount` exits 2. There is no prompt.
- `test/integration/bench`: Sprint 1 workspace benchmarks and the `bench`
  workflow (manual; self-hosted Windows Dev Drive, Linux btrfs and XFS, macOS
  APFS). They measure in-process `Manager.Create` over 20 sequential creates
  (median of 3 runs, budget 2 s), the incremental disk per workspace over the
  golden size (budget 1% for a golden tree of at least 1 GiB), the shared
  Go cache gain (budget: shared at most half of isolated) and the golden tree
  cost per base commit. Each test writes a result JSON to
  `COWFLOW_BENCH_OUT`, including the filesystem, the Defender exclusion state
  (`COWFLOW_BENCH_DEFENDER_EXCLUDED`) and `cowflow task create` wall time.
  Without a copy-on-write volume (`COWFLOW_BENCH_ROOT`,
  `COWFLOW_TEST_COW_DIR` or `COWFLOW_TEST_REFS_DIR`) they skip.
- `internal/workspace`: sparse checkout from the task paths in both providers.
  The cone set is the normalized `--paths` plus `contracts`. `git_worktree`
  adds the worktree without checkout, sets the cone and populates it;
  `cow_clone` clones only the files the cone mode of Git would check out,
  seeds the index, then sets the cone, so `git status` is clean and nothing is
  written twice. `Manager.ExpandScope` widens the scope of a ready workspace;
  while HEAD is the base commit, `cow_clone` clones the new files from the
  golden tree first.
- `internal/workspace`: safe workspace destroy, shared by `git_worktree` and
  `cow_clone`. It refuses with `UnpreservedError` when the workspace holds
  uncommitted files or unpublished commits and `--force` is not given, and
  refuses even with `--force` when the dirty check itself fails. With
  `--force` it first rescues the changes as a snapshot commit (tracked,
  staged and untracked files; git-ignored files and `.env.cowflow` are not
  preserved) at `refs/cowflow/rescue/<workspace_id>`, without touching the
  worktree or the task branch; a rescue failure (`ErrRescueFailed`) stops the
  destroy before anything is deleted. The destroy is audited (with
  `rescue_ref`) before any deletion, then the worktree, its directory, its
  admin entry and its per-workspace cargo target directory are removed; the
  task branch and the logs are kept. A removal blocked by an open handle
  moves the workspace to `pending_cleanup`, emits `workspace.pending_cleanup`
  with the path, error code and a hint, retries after 250 ms, 1 s and 4 s,
  then returns `ErrPendingCleanup`; destroying again resumes. The workspace
  path must be a direct child of `[workspace].root` that resolves to itself.
- `cowflow open <task>` opens the task's most recent ready workspace in VS Code
  with the `code` command, started detached from an argument array (no shell).
  `--json` prints `{"path": ...}` and starts nothing. Without `code` on PATH it
  prints the path and a hint, exit 0. On Windows, where `code` is a batch shim
  run through `cmd.exe`, a path outside `^[A-Za-z]:\\[A-Za-z0-9_.\\-]+$` is
  refused with exit 4 and printed for opening by hand.
- `cowflow golden list` shows each golden tree (SHA, size, files, state, in-use
  count, last use); `--json` adds `path` and `size_bytes`. `cowflow golden gc`
  removes least recently used trees down to `[scheduler].golden_cache_gb` and
  reports what it removed, the bytes freed and the trees skipped because they
  are in use.
- `internal/workspace`: dirty detection and the shared inspect. `Dirty` reads
  Git now and lists uncommitted paths (ignored files and `.env.cowflow` are not
  counted) and commits not under the shared ref namespace; any Git failure is
  an error, never "clean". `inspect` probes the filesystem at call time, adds
  warnings for a changed filesystem or a missing path, reports `logs_dir`, and
  gives `pending_mount` with `ErrPendingMount` when the volume is not mounted.
- `cowflow task create <slug>`: creates a task and its workspace without a
  daemon. It opens the SQLite store, names the branch by the conventions
  preset (`<kind>/<slug>` under gitflow, `cowflow/<project>/<slug>` otherwise),
  and calls the workspace manager. Flags: `--base`, `--title`, `--paths`
  (repeatable or comma-separated), `--sparse` (default true), and under gitflow
  `--kind` and `--force` (a `--base` other than the kind's own needs it). The
  result prints as text or as one `--json` document; the provider fallback
  warning goes to stderr. Exit codes follow SCOPE.md section 14. `--contract`,
  `--depends`, `--runner`, `--watch` and `--no-contract` fail with exit 1 until
  they are implemented.
- `cowflow init` without flags now creates `.cowflow/config.toml` with the
  defaults when it is missing (an existing file is never overwritten), creates
  the SQLite store and registers the project; running it again changes
  nothing. `init --devdrive` registers the project too. `--provider` and
  `--conventions` still exit 2 until `.mcp.json` and the lead token land.
- `cowflow task create` builds the `cow_clone` provider and gives
  `git_worktree` the shared inspector. A create that fails after the task row
  was written no longer blocks the retry: the same command line reuses the
  leftover task, while a live workspace or different options stay a conflict.
- `internal/store`: `SetWorkspaceState` records the provider, COW kind,
  filesystem and path the manager finally selected when a `pending_mount`
  workspace moves to `creating`, in the same transaction as the audit row; it
  refuses them on any other transition. `ResetStaleGoldenInUse` zeroes the
  `in_use` count of golden trees not used since a cutoff, for `golden gc`.
- `internal/workspace`: a `pending_mount` workspace requested with provider
  `auto` now resumes and records the final selection; `golden gc` resets a
  stale `in_use` count with a warning instead of only reporting it.
- `internal/workspace`: the `git_worktree` provider, the universal fallback.
  It adds a worktree on a new task branch at the base commit (no checkout for
  a sparse workspace), sets `core.autocrlf=false` per worktree (plus
  `core.longpaths=true` on Windows), reports no copy-on-write on any
  filesystem, and removes the worktree, directory and admin entry when a
  create fails. An existing branch is `ErrBranchExists`; an existing path is
  never touched. Inspect and destroy follow in later tasks.
- `internal/workspace`: the `cow_clone` provider. It reports block
  copy-on-write only when the golden root and `workspace.root` share a volume
  and a 64 KiB probe clone under the root succeeds (cached per root);
  otherwise `auto` falls back to `git_worktree`. A workspace is
  `git worktree add --no-checkout` on the task branch, then every golden file
  block-cloned by 16 parallel workers (directories created, symlinks
  replicated, no byte copy), then the golden index block-cloned as the
  worktree's index with `core.checkStat=minimal` and `core.trustCtime=false`,
  so `git status` is clean without rehashing. The file count and `HEAD` are
  checked; any failure (disk full, cancellation) removes the worktree,
  directory and admin entry. `Deps.CowClone` builds it in `NewManager` over
  the OS block-clone primitive and the manager's golden cache.
- `internal/workspace` (Linux, macOS): the block-clone primitive for `cow_clone`
  on the other two platforms. Linux clones with the `FICLONE` ioctl (btrfs,
  XFS with `reflink=1`), macOS with `clonefile(2)` (APFS); both refuse
  symlinks and directories, keep the permission bits and modification time,
  never copy bytes, and report `ErrCloneUnsupported` (ext4, XFS without
  reflink, another volume) without leaving a destination file.
- `workspace.GoldenCache` (`Manager.Golden()`): golden trees per `base_sha`
  at `<golden_root>/<sha>` (empty `golden_root`: `<workspace.root>/golden`),
  with the index beside the tree at `<dir>.index`. `Acquire` builds each SHA
  once across processes (store claim, stale takeover after
  `command_timeout_seconds`), in `<dir>.tmp-<op_id>` on the workspace volume,
  refreshes the index after a 1 s racy-clean wait, fails explicitly when the
  export lost paths (case collisions), and renames into place, so a partial
  tree is never visible. `GC` deletes ready trees least recently used first
  down to a byte budget, never one in use or building, renames before
  removal, retries `deleting` rows, and removes failed rows and leftover
  temp paths. `List` serves `golden list`.
- `internal/workspace`: `EnvFileWriter` writes `.env.cowflow` into each new
  workspace (atomic temp file and rename): sorted `KEY=VALUE` lines with the
  project, task, workspace and inclusive port range, plus the shared
  `GOMODCACHE`, `GOCACHE`, `npm_config_store_dir`, `GRADLE_USER_HOME` and
  `PIP_CACHE_DIR`, and a per-workspace `CARGO_TARGET_DIR`. Cache directories
  are created outside the workspace, values with a newline or NUL are
  rejected, and a cache off the Dev Drive volume only logs a warning.
- `internal/workspace` (Windows): the block-clone primitive for `cow_clone`.
  It clones a file with `FSCTL_DUPLICATE_EXTENTS_TO_FILE` in cluster-aligned
  chunks below 4 GiB, after matching the sparse and integrity-stream settings,
  and keeps the size, modification time and read-only bit. It never copies
  bytes: NTFS, a second volume or a refused layout returns
  `ErrCloneUnsupported` and leaves no destination file. Other OSes report
  `ErrCloneUnsupported` until their primitive lands.
- `internal/workspace` contract: the `WorkspaceProvider` interface and
  `Capabilities` of SCOPE.md §6.2, `CowKind` (`none|block|virtual` in
  `--json`), workspace spec/status types with an inclusive port range, the
  sentinel errors and `UnpreservedError` (names at most ten paths), and the
  consumer-side `GitOps`, `Store`, `Volumes` and `Cloner` seams.
  `workspacetest` adds test-only fakes: `FakeProvider`, `FakeManager`,
  `FakeCloner` (byte copy with fault injection), `FakeGit`, `MemStore` and
  `FakeVolumes`.
- `workspace.Manager`: provider selection (`auto` takes `cow_clone` only with
  block clone, otherwise `git_worktree` with a logged and returned warning;
  an explicit `cow_clone` without block clone fails with
  `ErrCloneUnsupported`; `projfs` and `container` are never selected) and
  safe create: refuses `main`/`develop`, invalid or existing branches and
  paths, confines `<workspace.root>/<slug>`, checks the Dev Drive mount gate
  by volume ID before any write (a failure records a `pending_mount` row and
  event, nothing on disk), and compensates every failure after the insert
  (worktree and directory removed, the created branch deleted, row `failed`)
  so no workspace is left partially `ready`. Create is idempotent by `op_id`.
  New sentinels `ErrRootNotSet`, `ErrPathExists`, `ErrBranchExists`;
  `workspace.LogsDir` names `<state_dir>/logs/<workspace_id>`.
- Store: tasks (minimal), workspaces, golden trees and `audit_log` tables
  (migration 0002). Workspace inserts allocate the lowest free port slot and
  are idempotent by `op_id`; state changes follow the settled workspace state
  machine and write their audit row in the same transaction; golden tree claims
  elect a single builder across processes, with stale takeover and in-use
  protected deletion. `domain` gains the workspace, golden tree and task types
  and the branch namer (`feature/<slug>` under the gitflow preset,
  `cowflow/<project>/<slug>` otherwise) with slug validation.
- `git.Client`: worktree add/remove/prune/list, commit and branch lookup,
  compare-and-delete of a branch, golden-tree export into a separate index,
  index refresh, `read-tree`, per-worktree config, `status --porcelain=v2 -z`
  and unpublished-commit listing, each a single argv call through the git
  runner. The runner now takes a per-call `GIT_INDEX_FILE` and stdin, and also
  strips an inherited `GIT_COMMON_DIR`.
- `git.Client` cone-mode sparse checkout: `SparseSet` (directories on stdin,
  never argv; a path that is a file at `HEAD` is refused by name),
  `SparseDisable`, `SparseList`, `PopulateSparse` (`read-tree -mu HEAD`), and
  `git.ValidateSparseDir`, which normalizes `\` to `/` and rejects empty,
  absolute, drive-letter, `..`, leading-`-`, glob and control-character paths.
- Volume identity and free space (`platform.VolumeOf`, `SameVolume`,
  `FreeBytes`): lowercase filesystem name, volume ID, mount path, Dev Drive
  state, block-refcounting hint and cluster size. `SameVolume` and
  `FreeBytes` also accept paths that do not exist yet.
- Test fixtures: `test/fixtures/benchrepo` and the `genrepo` command generate
  deterministic benchmark repositories offline (`small`, `large`, `monorepo`
  profiles; Windows-safe paths; `--verify` reports the size of any repo), and
  `test/fixtures/goproxy` builds a file-based `GOPROXY` for hermetic dependency
  tests. CI gains `cow-linux` (btrfs, XFS), `cow-macos` and `cow-windows` jobs
  that run the filesystem-dependent tests on real copy-on-write volumes.
- Dev Drive auto-attach (`platform.NewAutoAttacher`, `NewAutoAttacherFor`):
  on Windows `Enable` registers the Task Scheduler task
  `\COWFlow\devdrive-attach-<hash of the VHDX path>`, which runs
  `cowflow devdrive attach` from the project root at logon of the user who
  ran init, with highest privileges; `Disable` removes it. Both are
  idempotent. Other systems report `ErrUnsupported`.
- Config: `config.toml` lives at `<project root>/.cowflow/config.toml`
  (`config.Locate` walks up to the nearest config, else the repository root).
  `[collaboration.escalation_after_minutes]` is now a `high`/`normal`/`low`
  table (defaults 15/60/0). Every SCOPE.md section 22 scalar is now a built-in
  default, including `security.blocked_patterns`; empty `daemon.bind`,
  `project.branching`, `conventions.preset`, resource `queue` and
  `scheduler.retry_backoff_seconds` are rejected, as are
  `integration.promotion_mode = "pr"` and `prompt_mode = "message"`.
- `cowflow init --devdrive <size> [--path <file.vhdx>] [--letter <X>]`
  provisions an ReFS Dev Drive VHDX on Windows 11 22H2+, asking for
  administrator rights once (one UAC prompt), and records it under
  `[devdrive]` in the config file only after provisioning succeeds (the
  previous file is kept as `.bak`). `--path` defaults to
  `C:\cf\cowflow.vhdx` and `--letter` to `X`; an existing file or a taken
  letter exits `3` and names the flag to pass. It writes `workspace.root`
  (`X:/ws`), `workspace.golden_root` (`X:/golden`) and the six `cache.*`
  keys (`X:/cache/...`) and creates those directories; `daemon.state_dir` is
  left to its derived default. It registers Dev Drive auto-attach (a Task
  Scheduler task that runs `cowflow devdrive attach` from the project root at
  logon of the user who ran init) and sets `devdrive.auto_attach` only when
  that succeeded; otherwise it warns and names `cowflow devdrive attach` as
  the fallback. The non-elevated process reports the elevated one's real
  outcome. It prints, without applying it, the suggested Defender
  exclusion command. Other systems get an explicit "COWFlow will use
  git_worktree" message. A config file that does not parse or validate exits
  `1`; a missing one exits `2`.
- `cowflow devdrive create|attach|detach|status` manage the Dev Drive
  described by `[devdrive]` in the config file; `status` needs no
  administrator rights. All support `--json`.
- `cowflow doctor`: read-only report of the machine and repository against the
  SCOPE.md §7 requirements (Dev Drive mounted, ReFS block clone vs NTFS
  without COW, ProjFS when configured, `core.longpaths`, `core.autocrlf`,
  `.gitattributes`) plus `git >= 2.40`, with a fix command per finding. Prints
  a table or, with `--json`, `{"checks": [...], "ok": bool}`; exits non-zero
  only when a check fails. `defender.exclusion` reports `ok` when the Dev
  Drive is excluded from Defender scans, `warn` with the `Add-MpPreference`
  command when it is not, and `skip` when the exclusions cannot be read (not
  elevated, Defender not active); it never fails.
- Git version gate (`git >= 2.40`, typed `ErrVersionTooOld`) and a `ConfigGet`
  probe that distinguishes an unset key from a git failure (`internal/git`).
- Platform doctor probes: Dev Drive detection in `Host.Volume` and ProjFS
  availability (`Host.ProjFSAvailable`), both working without administrator
  rights.
- Dev Drive provisioning on Windows (`platform.NewDevDrive`): creates a
  dynamic VHDX through `virtdisk.dll` (PowerShell `New-VHD` fallback),
  attaches it, partitions and formats it as an ReFS Dev Drive at a drive
  letter, and reports success only once the volume is confirmed ReFS. A
  failed provisioning detaches the disk and deletes the VHDX it created.
  `Attach`, `Detach` and `Status` manage an existing Dev Drive.
- Product scope specification (`docs/SCOPE.md`).
- Git workflow and governance (`docs/GITFLOW.md`, `CONTRIBUTING.md`, `SECURITY.md`, `CODE_OF_CONDUCT.md`).
- Go module `github.com/deverity/cowflow` with a minimal `cowflow` CLI.
- Cobra-based `cowflow` root command with a persistent `--json` flag and the
  §14 exit codes (`1` usage, `2` state, `3` conflict, `4` policy); errors go
  to stderr, as a JSON document in `--json` mode.
- Full SCOPE.md §14 command tree with its arguments and flags; commands
  are stubs that report "not implemented yet" until later sprints.
- Configuration schema and strict TOML decoding (`internal/config`): every
  SCOPE.md §22 section is decoded and unknown keys are rejected with their
  dotted path.
- SQLite store (`internal/store`): WAL, busy timeout, foreign keys and
  immediate transactions on every pooled connection, plus an embedded,
  gap-checked migration runner that is safe with concurrent openers.
- Base schema migration `0001_base.sql` (`projects` table), the `WithTx`
  immediate-transaction helper and project insert/lookup in `internal/store`.
- Configuration defaults and validation (`internal/config`): `Default()` sets
  only the spec-stated defaults, `Load` applies them before decoding and
  validates afterwards, and `Validate` reports every problem at once
  (including rejecting a non-loopback `daemon.bind`).
- Process-tree control in `internal/platform`: commands start inside a
  Windows Job Object (`KILL_ON_JOB_CLOSE`, created suspended) or a Unix
  process group, and cancellation stops the whole tree (polite stop, grace
  period, then kill).
- CI: lint, tests on Windows/Linux/macOS, `govulncheck`, `gitleaks`, DCO check.
- Release pipeline: GoReleaser with checksums and build provenance attestation.
- `internal/platform`: `Host`/`DevDrive` contracts, OS info and filesystem
  capability detection (`ReFS`/`NTFS` block refcounting, btrfs, APFS).
- Test harness (`test/harness`): `FakeHost` and `SimDevDrive`, a deterministic
  simulated VHDX Dev Drive that can fail at any provisioning step and drops
  its volume on detach or reboot.
- Hermetic test harness (`test/harness`): a fake clock, throw-away Git
  repositories isolated from the developer's Git config, and Git
  fault injection (exit code, timeout, truncated output).
- `internal/platform` (Windows): `virtdisk.dll` bindings to create, open,
  attach and detach a dynamic VHDX, and `RunElevated`, which relaunches a
  command once through the UAC prompt without any shell.
- AI agent guide (`AGENTS.md`, `CLAUDE.md`) and implementation skills (`docs/skill/`).
- `config.example.toml` at the repository root (tested to always load and
  validate), and `config.Save`, an atomic rewrite that keeps the previous
  file as `.bak`. `config.Locate` reports the undecided location instead of
  guessing one.

### Fixed

- `internal/store`: `WithTx` retries `BEGIN IMMEDIATE` on `SQLITE_BUSY` for up
  to another busy timeout, so heavy writer contention no longer surfaces
  `database is locked` to callers (seen on slow Windows CI runners).
- `internal/platform`: `Canonical` no longer fails with a spurious `dangling
  link` error when a path component is created concurrently; the component is
  re-resolved a bounded number of times, and real dangling links are still
  refused.
- `cowflow workspace destroy` of a `git_worktree` workspace no longer fails
  with `destroy has no store` (exit 2): the provider now gets the shared
  Destroyer.
- `cowflow workspace destroy` prints `rescued to <ref> (restore with: git
  cherry-pick -n <ref>)` from the actual rescue result, also when the destroy
  ends in `pending_cleanup` (on stderr, exit 2), and records the OS user as the
  audit actor.
- `cowflow task create` removes the leftover task row of an earlier failed
  create, so a retry (also with other options) is no longer a slug conflict;
  a workspace waiting for its volume (`pending_mount`) is resumed by repeating
  the same command.
- `cowflow golden list` shows the golden tree ID (`ID` column, `id` in
  `--json`).
- `internal/workspace`: a failed `git_worktree` or `cow_clone` create now
  cleans up when the workspace path is not in canonical form (a Windows 8.3
  short name such as `C:\Users\RUNNER~1`, or a symlinked parent). It used to
  refuse to remove the directory and leave the worktree registered.
- `internal/workspace` (Windows): a ReFS / Dev Drive block clone no longer
  allocates the destination's full size before sharing the source's clusters.
  Growing a non-sparse file allocated its clusters, which ReFS freed only
  later, so cloning briefly cost the file's size in free space and could fail
  on a nearly full volume. The destination is grown as a sparse file and the
  attribute is cleared again for a non-sparse source.

### Changed

- Go 1.26 is now required (`go 1.26.0`, built with the `go1.26.9`
  toolchain); Go 1.25.0 had a standard-library vulnerability reported by
  govulncheck. `golang.org/x/sys` is updated to v0.49.0.
- CI now runs on push and pull requests to `master` and `develop`: lint with a
  six-target cross-build, tests on Linux, Windows and macOS, govulncheck,
  gitleaks and DCO.
- Usage errors exit `1` (was `2`); `cowflow version` is replaced by
  `cowflow --version`, and `cowflow --version --json` prints
  `{"version", "commit", "date"}`. There is no `version` subcommand.
- Exit codes follow one rule: `1` the command line or config file is wrong,
  `2` machine or project state prevents the command (including a failing
  doctor check, a declined UAC prompt, an unsupported OS and a command that
  is not implemented yet), `3` it collides with something that exists, `4` a
  COWFlow rule refused it.
- `cowflow mcp --project <dir>` locates `config.toml` from `<dir>`; `--role`
  has no fixed default (the session token's role, else `lead`).

[Unreleased]: https://github.com/deverity/COWFlow/commits/develop
