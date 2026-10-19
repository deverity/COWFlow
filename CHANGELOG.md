# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Product scope specification (`docs/SCOPE.md`).
- Git workflow and governance (`docs/GITFLOW.md`, `CONTRIBUTING.md`, `SECURITY.md`, `CODE_OF_CONDUCT.md`).
- Go module `github.com/deverity/cowflow` with a minimal `cowflow` CLI.
- Cobra-based `cowflow` root command with a persistent `--json` flag and the
  §14 exit codes (`1` usage, `2` state, `3` conflict, `4` policy); errors go
  to stderr, as a JSON document in `--json` mode.
- Full SCOPE.md §14 command tree with its arguments and flags; commands
  are stubs that report "not implemented yet" until later sprints.
- CI: lint, tests on Windows/Linux/macOS, `govulncheck`, `gitleaks`, DCO check.
- Release pipeline: GoReleaser with checksums and build provenance attestation.
- `internal/platform`: `Host`/`DevDrive` contracts, OS info and filesystem
  capability detection (`ReFS`/`NTFS` block refcounting, btrfs, APFS).
- AI agent guide (`AGENTS.md`, `CLAUDE.md`) and implementation skills (`docs/skill/`).

### Changed

- Usage errors exit `1` (was `2`); `cowflow version` is replaced by
  `cowflow --version` until SPEC-GAPS U-API-13 is decided.

[Unreleased]: https://github.com/deverity/COWFlow/commits/develop
