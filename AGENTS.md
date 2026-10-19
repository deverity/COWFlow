# AGENTS.md

Instructions for AI coding agents (Claude Code, Codex, Aider, Gemini CLI, …)
working **on the COWFlow repository itself**. Humans: see
[CONTRIBUTING.md](CONTRIBUTING.md).

## 1. Ground truth

| Topic | Source | Rule |
|---|---|---|
| What to build | [docs/SCOPE.md](docs/SCOPE.md) | The spec wins. Do not add features, providers, or config keys it does not list. |
| How code moves | [docs/GITFLOW.md](docs/GITFLOW.md) | Mandatory for every change. |
| How to do X | [docs/skill/](docs/skill/README.md) | Load the matching skill **before** writing code in its area. |

If the spec is ambiguous, listed under §23 "Open questions", or listed in
[docs/SPEC-GAPS.md](docs/SPEC-GAPS.md), **stop and ask** the maintainer.
Never resolve an open question silently in code.

## 2. Language

Everything in the repository is **English**: code, identifiers, comments,
docs, commit messages, PR titles and bodies, issues, log messages, CLI output.
(You may talk to the maintainer in their language; the repo stays English.)

## 3. Git workflow (summary of docs/GITFLOW.md)

- **Never commit or push to `master` or `develop`.** Always work on a branch.
- Branch from `develop`: `feat/<slug>`, `fix/<slug>`, `docs/<slug>`,
  `chore/<slug>`, `refactor/<slug>`, `test/<slug>`, `ci/<slug>`.
  `release/x.y.z` and `hotfix/x.y.z` are created only when the maintainer asks.
- Commits: Conventional Commits, `git commit -S -s` (signed + DCO sign-off).
  Summary ≤ 72 chars, imperative, no trailing period; body says *why*.
- One logical change per commit; each commit builds and passes tests.
- **No AI attribution.** Never add `Co-Authored-By:` trailers, "Generated
  with …" lines, or any other AI/tool attribution to commits, PR titles, or
  PR bodies. The human author and their `Signed-off-by:` are the only
  attribution.
- Open a PR against `develop`. Do not merge it yourself unless the maintainer
  explicitly tells you to.
- **Never:** force-push shared branches, rewrite published history, create,
  move, or delete tags, use `--no-verify`, or bypass signing.
- Never commit: secrets, `.env*`, `*.pem`, keys, `.cowflow/`, `dist/`,
  binaries, logs, machine-specific paths.

> **Do not confuse two sets of conventions.** `docs/GITFLOW.md` governs *this
> repository* (`master`, `feat/`, `fix/`). `docs/SCOPE.md` §24 describes the
> conventions the COWFlow *product* enforces on the projects it manages
> (`main`, `feature/`, `bugfix/`). When implementing §24, follow §24; when
> committing to this repo, follow GITFLOW.md.

## 4. Build, test, lint

```bash
go build ./...
go test -race -cover ./...
golangci-lint run          # v2, config in .golangci.yml
go mod tidy                # must leave go.mod/go.sum unchanged in CI
govulncheck ./...
```

Before declaring a task done, all of the above pass locally. CI runs them on
Windows, Linux, and macOS (`.github/workflows/ci.yml`).

## 5. Architecture constraints (from SCOPE.md §16)

- Go version from `go.mod`; **`CGO_ENABLED=0`** — Windows DLLs via
  `golang.org/x/sys/windows` (`LazyDLL`), never cgo.
- Stack is fixed: cobra, `net/http` + manual SSE, `modernc.org/sqlite`,
  BurntSushi/toml, `log/slog`, oklog/ulid, official MCP Go SDK
  (fallback `mark3labs/mcp-go`), testify, goreleaser. Adding any other
  dependency requires the maintainer's approval in the PR.
- Layout follows §16.1: `cmd/cowflow`, `internal/{cli,daemon,api,domain,store,
  git,workspace,platform,runner,scheduler,collab,integration,mcp,config}`,
  `test/{harness,integration,fixtures}`. Everything is under `internal/`.
- Git is driven through the `git` CLI (`os/exec`, argument slices). Never
  build shell strings.
- Platform code uses file suffixes (`_windows.go`, `_linux.go`, `_darwin.go`)
  behind a shared interface; Windows is the primary platform, not an afterthought.

## 6. Implementation phases (SCOPE.md §19)

Work lands in phase order unless the maintainer says otherwise:

| Phase | Focus | Skills |
|---|---|---|
| 0 Foundation | module, cobra, TOML, SQLite + migrations, Git wrapper, Windows platform layer, VHDX provisioning, `doctor`, CI, basic harness | `go-conventions`, `cli-and-config`, `store-and-events`, `git-changes-sync`, `windows-platform`, `testing-harness` |
| 1 COW workspaces | `cow_clone`, golden trees, `git_worktree` fallback, sparse, rescue, shared cache | `cow-workspaces`, `windows-platform` |
| 2 Collaboration plane (daemonless) | intents, requests, changes + manifest, derived index, sync, outbox, contracts, invariants | `collaboration-protocol`, `git-changes-sync`, `store-and-events` |
| 3 MCP + runners | roles, `.mcp.json`, tokens, runners, enforcement, budgets | `mcp-and-runners`, `security-review` |
| 4 Daemon, scheduler, SSE | API, heartbeat, limits, SSE, reconciliation, service, watchdog | `local-api-daemon`, `scheduler-and-leases` |
| 5 Integration | engine, gates, stale, promotion, post-promotion | `integration-engine` |
| 6 ProjFS + hardening | `projfs`, stress, crash, disk-full, fuzzing | `cow-workspaces`, `windows-platform`, `testing-harness` |

## 7. Skills

Skills live in [docs/skill/](docs/skill/README.md), one folder per skill with
a `SKILL.md`. Read the index, then load every skill whose area your change
touches. Always load `gitflow` and `go-conventions`; load `security-review`
for anything touching tokens, paths, processes, git invocation, or the API.

## 8. Definition of done (for changes to this repo)

- [ ] Change matches SCOPE.md; no unrequested features.
- [ ] Relevant skills loaded and their checklists satisfied.
- [ ] Tests added; `go test -race ./...` green; hermetic harness used for
      failure paths (§18.1).
- [ ] `golangci-lint run` and `go vet ./...` clean; `go mod tidy` is a no-op.
- [ ] Docs and `CHANGELOG.md` (`[Unreleased]`) updated if user-visible.
- [ ] Commits are Conventional, signed, and signed off; branch targets `develop`.
