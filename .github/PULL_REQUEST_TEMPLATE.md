## What and why

<!-- What does this change do, and why is it needed? Link the issue: "Closes #123". -->

## How it was tested

<!-- Commands run, platforms tested (Windows / Linux / macOS), new tests added. -->

## Checklist

- [ ] Targets `develop` (or `master` only for `release/*` / `hotfix/*`)
- [ ] PR title is a Conventional Commit (`feat(scope): ...`) — it becomes the squash commit
- [ ] Every commit is signed (`-S`) and signed off (`-s`, DCO)
- [ ] `go test -race ./...` and `golangci-lint run` pass locally
- [ ] Tests added or updated for new behavior
- [ ] `CHANGELOG.md` updated under `[Unreleased]` (if user-visible)
- [ ] Docs updated (if behavior or configuration changed)
- [ ] No secrets, credentials, or local paths committed
