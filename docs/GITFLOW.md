# Git Workflow

This document defines how code moves through the COWFlow repository. It is a
simplified GitFlow, hardened for a public repository: every rule below is
either enforced by GitHub settings or verifiable from the public history.

> **Language:** everything in this repository — code, comments, commit
> messages, PR titles/descriptions, issues, and docs — is written in **English**.

---

## 1. Flow overview

```
  feat/*   fix/*   docs/*   chore/*   refactor/*   test/*
     │       │        │        │          │         │
     └───────┴────────┴────────┴──────────┴─────────┘
                       │  PR + review + green CI  (squash merge)
                       ▼
 develop ─────●────────●────────●────────●───────────●───►  integration (protected)
               ╲        ▲        ╲        ▲
                ╲       │         ╲       │  merge-back PR (master → develop)
        release/x.y.z   │     hotfix/x.y.z│
                  ╲     │           ╲     │
                   ▼    │            ▼    │
 master ────────────●───┴─────────────●───┴──────────────►  production (protected)
                 tag vX.Y.Z        tag vX.Y.Z
```

**Golden rule:** nobody pushes directly to `master` or `develop` — maintainers
and admins included. Every change goes through a pull request.

---

## 2. Branches

| Branch | Created from | Merges into | Lifetime | Rule |
|---|---|---|---|---|
| `master` | — | — | permanent | Protected. Receives only `release/*` and `hotfix/*` via PR. Every merge is tagged. |
| `develop` | `master` | — | permanent | Protected. Continuous integration. PR + green CI required. |
| `feat/<slug>` | `develop` | `develop` | short | New feature. |
| `fix/<slug>` | `develop` | `develop` | short | Non-urgent bug fix. |
| `docs/<slug>`, `chore/<slug>`, `refactor/<slug>`, `test/<slug>`, `ci/<slug>` | `develop` | `develop` | short | Supporting work. |
| `release/x.y.z` | `develop` | `master` (then merge-back to `develop`) | short | Version freeze: only bug fixes, version bump, and changelog. |
| `hotfix/x.y.z` | `master` | `master` (then merge-back to `develop`) | short | Urgent production fix. |

`<slug>` is short kebab-case: `feat/cow-clone-refs`, `fix/lease-ttl-expiry`.

### Merge strategy

| PR | Strategy | Why |
|---|---|---|
| `feat/*`, `fix/*`, `docs/*`, … → `develop` | **Squash and merge** | One Conventional Commit per change on `develop`. |
| `release/*` → `master` | **Create a merge commit** | Keeps `develop` history as an ancestor of `master`. Squashing here makes the branches diverge permanently. |
| `hotfix/*` → `master` | **Create a merge commit** | Same reason. |
| merge-back `master` → `develop` | **Create a merge commit** | Brings release/hotfix commits back without rewriting anything. |

Because of the merge-back commits, **linear history is not required** on
`master`/`develop`. History integrity comes from blocking force-pushes and
deletions instead (section 4).

---

## 3. Day-to-day procedures

### Feature (or fix, docs, chore, …)

```bash
git switch develop && git pull --ff-only
git switch -c feat/cow-clone-refs

# ... small, atomic, signed commits ...
git commit -S -s -m "feat(workspace): add block-clone provider for ReFS"

git push -u origin feat/cow-clone-refs
gh pr create --base develop --fill
# After approval + green CI: "Squash and merge".
# The squash commit title must be a valid Conventional Commit.
```

Keep the branch current with `git rebase develop` (before review) or
`git merge develop` (during review, to avoid force-pushing over reviewed
commits). Delete the branch after merge.

### Release

```bash
git switch develop && git pull --ff-only
git switch -c release/1.4.0

# Bump version, finalize CHANGELOG.md, last fixes only.
git commit -S -s -m "chore(release): 1.4.0"
git push -u origin release/1.4.0
gh pr create --base master --title "release: 1.4.0" --fill
# After approval + green CI: "Create a merge commit".

# Tag the merge commit on master.
git switch master && git pull --ff-only
git tag -s v1.4.0 -m "v1.4.0"
git push origin v1.4.0           # triggers .github/workflows/release.yml

# Merge-back via PR (develop is protected).
gh pr create --base develop --head master --title "chore: merge-back v1.4.0 into develop"
# After CI: "Create a merge commit". Then delete release/1.4.0.
```

### Hotfix

```bash
git switch master && git pull --ff-only
git switch -c hotfix/1.4.1

# Fix + patch bump + CHANGELOG entry.
git commit -S -s -m "fix(engine): release slot when lease TTL expires"
git push -u origin hotfix/1.4.1
gh pr create --base master --title "hotfix: 1.4.1" --fill
# After approval + green CI: "Create a merge commit".

git switch master && git pull --ff-only
git tag -s v1.4.1 -m "v1.4.1"
git push origin v1.4.1

gh pr create --base develop --head master --title "chore: merge-back v1.4.1 into develop"
```

If a `release/*` branch is open when the hotfix lands, merge the hotfix into
it as well, or the next release will reintroduce the bug.

---

## 4. Rules that make the flow auditable

Without these controls, the workflow is decoration — and any reviewer of a
public repository will notice.

### Branch protection (`master` and `develop`)

- Require a pull request before merging.
- Require **1 approval** minimum, ideally from someone other than the author
  (`CODEOWNERS` routes reviews automatically).
- Dismiss stale approvals when new commits are pushed.
- Require status checks to pass and the branch to be up to date
  (lint, test, build, vulnerability scan, secret scan).
- Require conversation resolution.
- **Require signed commits.**
- Block force-pushes and branch deletion.
- **Include administrators** (`enforce_admins`) — no "I'm admin" shortcuts.

> **Solo maintainer note:** GitHub does not let you approve your own PR. While
> there is a single maintainer, either set the approval count to `0` (keeping
> every other check) or add a second maintainer. Document whichever you choose
> here; do not silently disable `enforce_admins`.

### Commits

- **Conventional Commits:** `<type>(<scope>): <summary>`
  - Types: `feat`, `fix`, `docs`, `chore`, `refactor`, `test`, `perf`, `build`, `ci`.
  - Summary ≤ 72 characters, imperative mood, no trailing period.
  - Body explains **why**, not what.
  - Breaking changes: `feat(api)!: ...` and/or a `BREAKING CHANGE:` footer.
- **Signed commits** (GPG, SSH, or Sigstore `gitsign`): `git commit -S`.
- **DCO sign-off** instead of a CLA: `git commit -s` adds
  `Signed-off-by: Name <email>`. CI rejects PRs with unsigned-off commits.
- Atomic commits; each one builds and passes `go test ./...` on its own.
- English only.

### Versioning and releases

- [Semantic Versioning](https://semver.org): `vMAJOR.MINOR.PATCH`. Before the
  first stable API, use `v0.y.z`.
- Tags are **annotated and signed**: `git tag -s vX.Y.Z`.
- **Tags are immutable.** The Go module proxy (`proxy.golang.org`) and checksum
  database (`sum.golang.org`) cache a version forever the first time anyone
  fetches it. Moving or recreating a tag breaks every downstream build with a
  checksum mismatch. A bad release is fixed with a new patch version, never
  `git tag -f`. Use `retract` in `go.mod` to mark a broken version.
- Going to `v2+` requires the `/v2` module path suffix (Go module rules).
- `CHANGELOG.md` follows [Keep a Changelog](https://keepachangelog.com) and
  has an entry for every tag.
- GitHub Release notes link to the diff (`v1.3.0...v1.4.0`).
- Published binaries ship with checksums, signatures, and SLSA provenance.

### Security and transparency

- `SECURITY.md` with a private disclosure channel (GitHub private
  vulnerability reporting) — never public issues for vulnerabilities.
- `CONTRIBUTING.md` points to this document.
- `CODEOWNERS`.
- Secret scanning **and push protection** enabled; `gitleaks` in CI.
- Dependabot for Go modules and GitHub Actions.
- GitHub Actions pinned by commit SHA, not by tag.
- Docs/site generated from `master` by CI, never edited by hand.

### Never do this in a public repository

- ❌ Force-push to `master`, `develop`, or any tag (rewrites auditable history).
- ❌ Push directly as a maintainer "because I'm admin".
- ❌ Move or delete a tag (`git tag -f`, `git push --delete`).
- ❌ Commit secrets. If it happens: **rotate the secret first**, then purge
  with `git filter-repo` — `git rm` leaves it in history.
- ❌ Squash a third-party PR without preserving authorship. GitHub keeps the
  author on squash; when merging manually, use `--author` or rebase-merge.

---

## 5. Repository layout for governance

```
/
├── .github/
│   ├── workflows/
│   │   ├── ci.yml            # lint, test (3 OS), govulncheck, gitleaks, dco
│   │   └── release.yml       # runs on push of tag v*: build, checksums, sign, provenance, GitHub Release
│   ├── CODEOWNERS
│   ├── PULL_REQUEST_TEMPLATE.md
│   ├── ISSUE_TEMPLATE/
│   └── dependabot.yml
├── docs/
│   └── GITFLOW.md            # this document
├── CHANGELOG.md
├── CONTRIBUTING.md
├── SECURITY.md
├── CODE_OF_CONDUCT.md
├── README.md
└── LICENSE
```

### CI checks for Go

| Job | Runs |
|---|---|
| `lint` | `go mod tidy` drift check, `go vet ./...`, `golangci-lint run` (incl. `gofmt`/`goimports`) |
| `test (<os>)` | `CGO_ENABLED=0 go build ./...` + `go test -race -cover ./...` on Windows, Linux, macOS |
| `govulncheck` | `govulncheck ./...` |
| `gitleaks` | `gitleaks git` over the full history |
| `dco` | every PR commit has a `Signed-off-by:` trailer |

The job names in `ci.yml` are the status check names required by branch
protection — keep them in sync.

---

## 6. One-time setup

### Local signing

```bash
# GPG
git config --global commit.gpgsign true
git config --global tag.gpgsign true
git config --global user.signingkey <fingerprint>

# or SSH signing
git config --global gpg.format ssh
git config --global user.signingkey ~/.ssh/id_ed25519.pub
```

Upload the same key to GitHub as a **signing key** and enable
**vigilant mode** so unsigned commits are visibly flagged.

### Branch protection (GitHub CLI)

Nested fields must be sent as JSON (`-f` cannot express them):

```bash
for branch in master develop; do
gh api -X PUT "repos/{owner}/{repo}/branches/$branch/protection" --input - <<'EOF'
{
  "required_status_checks": {
    "strict": true,
    "contexts": [
      "lint",
      "test (ubuntu-latest)", "test (windows-latest)", "test (macos-latest)",
      "govulncheck", "gitleaks", "dco"
    ]
  },
  "enforce_admins": true,
  "required_pull_request_reviews": {
    "required_approving_review_count": 1,
    "dismiss_stale_reviews": true,
    "require_code_owner_reviews": true
  },
  "restrictions": null,
  "required_linear_history": false,
  "allow_force_pushes": false,
  "allow_deletions": false,
  "required_conversation_resolution": true
}
EOF
gh api -X POST "repos/{owner}/{repo}/branches/$branch/protection/required_signatures"
done
```

### Tag protection (repository ruleset)

The legacy tag-protection endpoint is deprecated; use a ruleset:

```bash
gh api -X POST "repos/{owner}/{repo}/rulesets" --input - <<'EOF'
{
  "name": "immutable-release-tags",
  "target": "tag",
  "enforcement": "active",
  "conditions": { "ref_name": { "include": ["refs/tags/v*"], "exclude": [] } },
  "rules": [
    { "type": "deletion" },
    { "type": "update" },
    { "type": "non_fast_forward" }
  ]
}
EOF
```

### Repository settings

- Settings → General → Pull Requests: allow **squash** and **merge commits**,
  disable rebase merging; enable "Automatically delete head branches".
- Settings → Code security: enable secret scanning, push protection,
  Dependabot alerts/updates, and private vulnerability reporting.
- Default branch for PRs: `develop`.

---

## 7. Audit checklist

- [ ] `master` matches exactly what is published.
- [ ] Every `v*` tag points to a commit on `master` and has a valid signature.
- [ ] No force-push in the public history.
- [ ] `CHANGELOG.md` covers every tag.
- [ ] `SECURITY.md` is answered in a reasonable time.
- [ ] CI is green on `develop` **before** cutting a release.
- [ ] Merge-back `master → develop` done after every release and hotfix
      (otherwise `develop` diverges — the #1 GitFlow mistake).
- [ ] No secrets in history (`gitleaks` job in CI).
- [ ] `go.mod` has no `replace` directives pointing to local paths on `master`.
