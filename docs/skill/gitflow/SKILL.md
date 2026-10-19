---
name: gitflow
description: Use for every change to the COWFlow repository — creating branches, writing commits, opening PRs, cutting releases or hotfixes, tagging. Load before running any git write command.
---
# Git workflow for the COWFlow repository

## When to use

Always. Any task that ends in a commit, branch, PR, tag, or release.

## Source of truth

- `docs/GITFLOW.md` (full rules, setup, audit checklist) — wins on conflict.
- `CONTRIBUTING.md` (commit format, DCO).
- Not to be confused with `docs/SCOPE.md` §24, which describes conventions the
  product enforces on *managed* projects (`main`, `feature/`, `bugfix/`).

## Rules

- NEVER commit, merge, or push directly to `master` or `develop`.
- NEVER force-push a shared branch, rewrite published history, or create /
  move / delete tags. Tags are created by the maintainer only.
- NEVER use `--no-verify`, `--no-gpg-sign`, or `-c commit.gpgsign=false`.
- MUST branch from an up-to-date `develop` (or `master` for a hotfix the
  maintainer requested).
- MUST name branches `<type>/<kebab-slug>`:

  | Type | Use |
  |---|---|
  | `feat/` | new behavior |
  | `fix/` | bug fix (non-urgent) |
  | `docs/` | documentation only |
  | `chore/` | tooling, deps, housekeeping |
  | `refactor/` | no behavior change |
  | `test/` | tests only |
  | `ci/` | workflows |
  | `release/x.y.z` | maintainer-initiated release |
  | `hotfix/x.y.z` | maintainer-initiated production fix, from `master` |

- MUST write Conventional Commits: `<type>(<scope>): <summary>`
  - types: `feat`, `fix`, `docs`, `chore`, `refactor`, `test`, `perf`, `build`, `ci`
  - scope = package or area: `workspace`, `git`, `store`, `api`, `mcp`, `cli`, …
  - summary ≤ 72 chars, imperative, lowercase start, no trailing period
  - body explains *why*; breaking change → `!` and `BREAKING CHANGE:` footer
- MUST sign and sign off: `git commit -S -s`.
- NEVER add `Co-Authored-By:` trailers or "Generated with …" lines (AI/tool
  attribution) to commits or PRs.
- MUST keep commits atomic: each builds and passes `go test ./...`.
- MUST open PRs against `develop` (only `release/*`/`hotfix/*` target `master`).
- Merge strategy is fixed: squash into `develop`; merge commit into `master`
  and for merge-back `master → develop`.

## Implementation guide

### Feature / fix

```bash
git switch develop && git pull --ff-only
git switch -c feat/<slug>
# work in small steps
git add <paths>                        # never `git add -A` blindly; review the diff
git commit -S -s -m "feat(<scope>): <summary>"
git push -u origin feat/<slug>
gh pr create --base develop --fill     # title = Conventional Commit
```

Keep the branch current: `git rebase develop` before review; `git merge
develop` after review has started (do not force-push reviewed commits).

### Release (maintainer asks for it)

1. `git switch -c release/x.y.z develop`
2. Move `## [Unreleased]` entries in `CHANGELOG.md` to `## [x.y.z] - YYYY-MM-DD`;
   add the compare link.
3. `chore(release): x.y.z` commit → PR to `master` → merge commit.
4. Maintainer tags `vx.y.z` (signed) on `master`; `release.yml` publishes.
5. Merge-back PR `master → develop` (merge commit).

### Hotfix (maintainer asks for it)

`hotfix/x.y.z` from `master` → fix + changelog → PR to `master` → tag →
merge-back PR to `develop` (and into any open `release/*`).

### Versioning

- SemVer; `v0.y.z` until the API is stable.
- A published tag is permanent (Go module proxy caches it). Broken release →
  new patch + `retract` directive in `go.mod`, never a moved tag.

## Pitfalls

- Squash-merging a release into `master` makes `develop` and `master` diverge.
- Forgetting the merge-back after a release/hotfix — the #1 GitFlow failure.
- Committing generated files, `dist/`, or `.cowflow/` state.
- PR title not Conventional → bad squash commit on `develop`.
- Commit missing `Signed-off-by:` → `dco` check fails; fix with
  `git rebase --signoff develop` (on your own unpublished-to-others branch).

## Tests required

- Before every commit: `go build ./...`, `go test -race ./...`.
- Before opening a PR: `golangci-lint run`, `go mod tidy` (no diff).

## Done checklist

- [ ] Branch name follows `<type>/<slug>` and starts from `develop`
- [ ] Every commit is Conventional, signed (`-S`), signed off (`-s`)
- [ ] `git diff --staged` reviewed; no secrets, binaries, or local paths
- [ ] CI-equivalent checks pass locally
- [ ] `CHANGELOG.md` `[Unreleased]` updated for user-visible changes
- [ ] PR targets `develop`, template filled, title is a Conventional Commit
