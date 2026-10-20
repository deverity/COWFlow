# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `git.Client`: worktree add/remove/prune/list, commit and branch lookup,
  compare-and-delete of a branch, golden-tree export into a separate index,
  index refresh, `read-tree`, per-worktree config, `status --porcelain=v2 -z`
  and unpublished-commit listing, each a single argv call through the git
  runner. The runner now takes a per-call `GIT_INDEX_FILE` and stdin, and also
  strips an inherited `GIT_COMMON_DIR`.
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
