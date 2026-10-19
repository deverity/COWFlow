# Contributing to COWFlow

Thanks for your interest in COWFlow. This document explains how to propose
changes. Everything in this repository — code, comments, commits, PRs,
issues, and docs — is written in **English**.

## Before you start

- **Bugs and features:** open an issue first for anything non-trivial, so the
  design can be discussed before code is written.
- **Security issues:** do **not** open a public issue. Follow
  [SECURITY.md](SECURITY.md).
- **Product scope:** read [docs/SCOPE.md](docs/SCOPE.md). Changes that
  contradict a "Settled decision" need an issue first.
- Be respectful: [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) applies everywhere.

## Development setup

Requirements:

- Go (version in [go.mod](go.mod))
- Git ≥ 2.40
- [golangci-lint](https://golangci-lint.run) v2

```bash
git clone https://github.com/deverity/COWFlow.git
cd COWFlow
git switch develop

go build ./...
go test -race ./...
golangci-lint run
```

## Workflow

The full branching model is in [docs/GITFLOW.md](docs/GITFLOW.md). In short:

1. Branch from `develop`: `feat/<slug>`, `fix/<slug>`, `docs/<slug>`,
   `chore/<slug>`, `refactor/<slug>`, `test/<slug>`, or `ci/<slug>`.
2. Commit with **sign-off and signature**: `git commit -S -s`.
3. Open a PR against `develop`. Never target `master` directly — only
   `release/*` and `hotfix/*` branches do.
4. CI must be green and at least one maintainer must approve.
5. PRs into `develop` are squash-merged; the squash title must be a valid
   Conventional Commit.

## Commit messages

[Conventional Commits](https://www.conventionalcommits.org):

```
<type>(<scope>): <summary>

<body: why the change is needed>

Signed-off-by: Your Name <you@example.com>
```

- Types: `feat`, `fix`, `docs`, `chore`, `refactor`, `test`, `perf`, `build`, `ci`.
- Summary ≤ 72 characters, imperative mood, no trailing period.
- Breaking change: `feat(api)!: ...` and/or a `BREAKING CHANGE:` footer.
- One logical change per commit; every commit builds and passes tests.

## Developer Certificate of Origin (DCO)

COWFlow uses the [Developer Certificate of Origin 1.1](https://developercertificate.org)
instead of a CLA. By adding a `Signed-off-by` line to a commit, you certify
that you wrote the change or otherwise have the right to submit it under the
project's license.

```bash
git commit -s                     # adds the trailer
git rebase --signoff develop      # fixes earlier commits on your branch
```

The `dco` CI check rejects PRs with commits that lack the trailer.

## Signed commits

Branch protection requires verified signatures. Configure GPG or SSH signing
once (see [docs/GITFLOW.md § 6](docs/GITFLOW.md#6-one-time-setup)) and upload
the key to GitHub as a signing key.

## Code guidelines

- Format with `gofmt`/`goimports`; `golangci-lint run` must pass.
- Production code lives under `internal/` unless it is a deliberate public API.
- New behavior comes with tests. Concurrency-sensitive code must pass `-race`.
- No secrets, credentials, or machine-specific paths in commits.
- Update `CHANGELOG.md` (`## [Unreleased]`) for user-visible changes.
- Update docs when behavior or configuration changes.

## Pull request checklist

The [PR template](.github/PULL_REQUEST_TEMPLATE.md) lists what reviewers
expect. PRs that skip it may be closed without review.
