# Skills

Task-focused instructions for AI agents working on COWFlow. Each skill is a
folder with a `SKILL.md` (YAML front matter `name` + `description`, then
rules, implementation guide, pitfalls, required tests, and a done checklist).

**How to use:** read [AGENTS.md](../../AGENTS.md) first, then load every skill
whose area your change touches. `gitflow` and `go-conventions` apply to every
change. [docs/SCOPE.md](../SCOPE.md) wins over any skill on conflict — if you
find a conflict, fix the skill in the same PR.

| Skill | Load when | SCOPE.md |
|---|---|---|
| [gitflow](gitflow/SKILL.md) | any commit, branch, PR, release | GITFLOW.md |
| [go-conventions](go-conventions/SKILL.md) | any Go code | §16, §2.2 |
| [cli-and-config](cli-and-config/SKILL.md) | cobra commands, `init`, `doctor`, `config.toml` | §14, §22 |
| [store-and-events](store-and-events/SKILL.md) | SQLite, migrations, outbox, events, derived index | §9.9, §13, §8.3 |
| [git-changes-sync](git-changes-sync/SKILL.md) | git wrapper, publish, manifest, sync, refs | §8 |
| [windows-platform](windows-platform/SKILL.md) | Dev Drive/VHDX, block clone, ProjFS, Job Objects, paths | §6.4–6.5, §7 |
| [cow-workspaces](cow-workspaces/SKILL.md) | workspace providers, golden trees, sparse, cache | §6 |
| [collaboration-protocol](collaboration-protocol/SKILL.md) | presence, intents, cross-requests, contracts, invariants, watchdog | §9, §24.6–24.11 |
| [scheduler-and-leases](scheduler-and-leases/SKILL.md) | limits, budgets, resource leases | §9.8, §11, §24.15 |
| [local-api-daemon](local-api-daemon/SKILL.md) | daemon, HTTP API, tokens, SSE, metrics | §12, §5, §21 |
| [mcp-and-runners](mcp-and-runners/SKILL.md) | MCP server, roles, `.mcp.json`, runners | §15, §24.13–24.14 |
| [integration-engine](integration-engine/SKILL.md) | integration groups, gates, promotion, revert | §10, §24.12 |
| [testing-harness](testing-harness/SKILL.md) | test strategy, hermetic harness, fuzzing, acceptance | §18, §20 |
| [security-review](security-review/SKILL.md) | tokens, paths, processes, git calls, API, any PR review | §17, §24.14 |

## Using these with specific agents

- **Claude Code:** `CLAUDE.md` imports `AGENTS.md`, which points here. To make
  skills auto-discoverable as Claude Code skills, copy or link a folder into
  `.claude/skills/<name>/`.
- **Codex / Aider / Gemini CLI / others:** they read `AGENTS.md`; skills are
  loaded on demand by path.
