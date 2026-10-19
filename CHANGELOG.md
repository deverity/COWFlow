# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `platform.AutoAttacher` seam with `NewAutoAttacher`: until the auto-attach
  mechanism is decided (SPEC-GAPS U-PLAT-5), `Enable` returns
  `ErrAutoAttachUndecided` on Windows and `ErrUnsupported` elsewhere.
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

- CI now runs on push and pull requests to `master` and `develop`: lint with a
  six-target cross-build, tests on Linux, Windows and macOS, govulncheck,
  gitleaks and DCO.
- Usage errors exit `1` (was `2`); `cowflow version` is replaced by
  `cowflow --version` until SPEC-GAPS U-API-13 is decided.

[Unreleased]: https://github.com/deverity/COWFlow/commits/develop
