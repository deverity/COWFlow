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
- Configuration schema and strict TOML decoding (`internal/config`): every
  SCOPE.md §22 section is decoded and unknown keys are rejected with their
  dotted path.
- SQLite store (`internal/store`): WAL, busy timeout, foreign keys and
  immediate transactions on every pooled connection, plus an embedded,
  gap-checked migration runner that is safe with concurrent openers.
- Base schema migration `0001_base.sql` (`projects` table), the `WithTx`
  immediate-transaction helper and project insert/lookup in `internal/store`.
- CI: lint, tests on Windows/Linux/macOS, `govulncheck`, `gitleaks`, DCO check.
- Release pipeline: GoReleaser with checksums and build provenance attestation.
- AI agent guide (`AGENTS.md`, `CLAUDE.md`) and implementation skills (`docs/skill/`).

### Changed

- Usage errors exit `1` (was `2`); `cowflow version` is replaced by
  `cowflow --version` until SPEC-GAPS U-API-13 is decided.

[Unreleased]: https://github.com/deverity/COWFlow/commits/develop
