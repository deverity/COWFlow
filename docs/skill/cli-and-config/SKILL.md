---
name: cli-and-config
description: Use when adding or changing `cowflow` commands/flags in internal/cli (cobra), exit codes, `--json` output, `cowflow doctor`, `cowflow init` (incl. `--devdrive`, `--conventions gitflow`, `.mcp.json`/`.gitignore` generation), or the `config.toml` schema, defaults, validation, and `config.example.toml` in internal/config.
---
# CLI and Config (internal/cli, internal/config)

## When to use
- Creating the cobra command tree or any subcommand/flag from §14.
- Mapping errors to exit codes; `--json` output.
- Implementing `cowflow doctor` checks or `cowflow init` generation steps.
- Adding/validating a `config.toml` key; updating `config.example.toml`.

## Source of truth
SCOPE.md wins on any conflict.
- §1.2, §1.3 Changes (Dev Drive via `init`, `.mcp.json` via `init`, runners, resources, conventions)
- §3.1 (single binary, `git >= 2.40`), §5.3 Modes, §5.4 lead rules
- §6.2–§6.4, §6.7 (providers, `--devdrive` provisioning, cache keys); §7 Windows requirements (verified by `doctor`)
- §9.6 Invariants, §9.8 Resource leases (CLI, aliases), §11 Scheduler
- §14 CLI; §15.2 Generated `.mcp.json`; §15.3 Runners
- §16/§16.1 Go stack/layout; §17 Security; §20 items 12, 13, 21, 29, 31
- §22 Configuration; §23; §24.1–§24.2 GitFlow and enforcement; §24.7, §24.10, §24.13

## Rules
- MUST use cobra (§16) with the command tree exactly as §14: `init`, `mcp`, `doctor`, `daemon start|stop|status|install`, `devdrive create|attach|detach|status`, `task create|list|show|pause|resume`, `run`, `runners list|check`, `status`, `agents`, `intents`, `who`, `open`, `request send|list|accept|reject|close|comment|reassign|deliver`, `change publish|list|show|manifest|discard`, `sync` (+ `sync status|abort`), `integration create|validate|promote|cancel|show|open`, `resources`, `resource acquire|release|revoke|announce`, `exec`, `index rebuild`, `workspace list|inspect|destroy`, `golden list|gc`, `logs`, `events`, `budget`.
- Flags from §14:
  - setup: `init --devdrive <size> --provider auto|cow_clone|projfs|git_worktree`; `mcp --role lead|worker|watchdog` (+ `--project`, §15.2)
  - tasks: `task create <slug> --base --title --contract --paths --depends --kind --watch --runner`; `run --max-agents [--runner]`
  - team: `status --watch`; `intents --path`; `who --path`
  - requests: `request send <to> --kind --title --expect`; `request list --open --to`; `request deliver <id> --change`
  - changes: `change publish --task`; `sync <change-id> --into <workspace-id> --strategy merge|cherry-pick|squash`
  - integration: `integration create <tasks...> --target`; `integration show --diff`
  - resources: `resource ... --wait --ttl`; `exec --resource <name> -- <cmd...>`
  - diagnostics: `workspace destroy --force`; `logs --task --follow`; `events --follow --since <cursor>`
- Additional flags named elsewhere: `init --conventions gitflow` (§20.31, §24); `--devdrive <size> [--path] [--letter]` (§6.4); `task create --kind feature|bugfix|chore|hotfix|release` and `--force` for a non-default `--base`, audited (§24.2); `--no-contract`, audited (§24.7); `sync --reason` (§24.10).
- `--json` on every command (§14). Exit codes: `0` ok, `1` usage, `2` state, `3` conflict, `4` refused by policy (§14).
- CLI talks to the daemon API when present, else writes SQLite directly (daemonless) (§5.2, §5.3). Daemon-only features (§5.3) must not be faked in daemonless mode.
- NEVER build shell strings; `exec` and gates take argv arrays (§7, §17).
- `workspace destroy` NEVER destroys unpreserved changes without `--force`; rescue uncommitted changes first (§17, §20.12).
- `workspace inspect` reports actual filesystem and `CowKind` (§20.13).
- `init` MUST write `.mcp.json` at repo root exactly per §15.2 (`"command": "cowflow"`, `"args": ["mcp", "--role", "lead", "--project", "."]`) and issue the lead token (project scope) (§5.4, §14, §15).
- `init` MUST generate/update `.gitignore` with `node_modules/`, `.cowflow/`, `*.log`, build artifacts, `.env*` (§24.13).
- `init --devdrive` MUST follow §6.4 steps 1–7: Win11 22H2+ and admin (elevate once); dynamic VHDX via `virtdisk.dll` `CreateVirtualDisk` or `New-VHD` fallback; attach, GPT, partition, ReFS with Dev Drive flag (`Format-Volume -DevDrive`); mount; register + auto-attach at boot; write `workspace.root` and `cache.*` inside the volume; suggest Defender exclusion.
- NTFS → worktree fallback with explicit warning; NEVER silently use NTFS when a configured VHDX is unmounted (§1.2, §6.4, §18).
- `init --conventions gitflow` writes the `[conventions]` preset (`gitflow | trunk | none`) and enables §24.2 enforcement: branch `<kind>/<slug>`, base rules, refusals (§20.31).
- `doctor` MUST verify §7: Dev Drive present+mounted, ReFS confirmed, NTFS flagged as no COW; ProjFS feature when `projfs` configured; `core.longpaths=true`; `.gitattributes` `* text=auto eol=lf`; `core.autocrlf=false`; plus `git >= 2.40` (§3.1). No Dev Drive → explain and offer to create one (§6.4); Win10/no admin → guide to worktree fallback (§23).
- `provider = "auto"` chooses `cow_clone` → `git_worktree`; `projfs` is never chosen automatically (§6.3, §23 decision 10).
- Config MUST be TOML via BurntSushi/toml (§16, §23 decision 4) with the §22 sections: `[daemon]`, `[project]`, `[devdrive]`, `[workspace]`, `[workspace.projfs]`, `[cache]`, `[git]`, `[scheduler]`, `[scheduler.llm]`, `[scheduler.llm.<provider>]`, `[collaboration]`, `[integration]`, `[integration.post_promotion]`, `[[gates]]`, `[[invariants]]`, `[[resources]]`, `[conventions]`, `[runner]`, `[runner.<name>]`, `[watchdog]`, `[security]`.
- Keys and enums per spec: `workspace.provider` `auto|cow_clone|projfs|git_worktree|container`; `project.lead` `human|agent:<task-id>` (§9.4); `branching` `gitflow|trunk` (§10.3); `default_sync_strategy` `merge` default (§8.4); `promotion_mode` `manual|auto` (§10.3) and `pr` (§24.2); `resources.queue` `fifo|priority`; `invariants.kind` `pattern|cmd` with `deny`/`cmd`/`check_on_publish`; `prompt_mode` `arg|stdin|file` (§15.3); `[cache]` also `gradle_home`, `pip_cache` (§6.7).
- Spec-stated defaults: `provider = "auto"`, `lead = "human"`, `default_base = "develop"`, sync `merge`, `promotion_mode = "manual"`, `workspace.projfs.enabled = false`, `watchdog.enabled = false`, `post_promotion.enabled = false` (§6.3, §9.4, §22, §23 decisions 6–7, 10).
- `max_builds`, `max_tests`, `max_integrations_per_target` remain valid and alias resources `compile`, `test`, `integrate:<target>` (§9.8).
- Gates/invariant `cmd` and runner `cmd` are arrays; runner placeholders `{mcp_config}`, `{workspace}`, `{prompt}`, `{task_id}` (§15.3).
- `config.example.toml` lives at repo root (§16.1) and mirrors §22.

## Implementation guide
Files: `cmd/cowflow/main.go` (thin: build cobra root from `internal/cli`, `os.Exit(code)`); `internal/cli/root.go`, one file per group (`task.go`, `request.go`, `change.go`, `integration.go`, `resource.go`, `init.go`, `doctor.go`, `daemon.go`, `devdrive.go`, ...); `internal/cli/exit.go`; `internal/config/config.go`, `defaults.go`, `validate.go`; `config.example.toml`.

Guidance (sketch):
```go
type ExitError struct{ Code int; Err error }       // 1 usage, 2 state, 3 conflict, 4 policy
func NewRootCmd(io IOStreams, deps Deps) *cobra.Command
type Client interface{ /* same ops via API or direct store */ } // daemon vs daemonless
type Config struct{ Daemon Daemon `toml:"daemon"`; Project Project `toml:"project"`; /* ... §22 */ }
func Load(path string) (*Config, error)            // decode → apply defaults → validate
func (c *Config) Validate() error                  // errors.Join of all problems
```
Steps:
1. Root: `SilenceUsage`, `SilenceErrors`; persistent `--json`; `SetFlagErrorFunc` and `cobra.ExactArgs`/`RangeArgs` failures map to exit `1`.
2. Error → exit code via `errors.As(err, *ExitError)`; domain/API error codes map to `2`/`3`/`4`.
3. `--json`: single JSON document on stdout; human text otherwise; diagnostics to stderr.
4. Config decode: `md, err := toml.DecodeFile(path, &raw)`; Guidance — treat `md.Undecoded()` keys as validation errors (typo safety).
5. Mixed tables: `[scheduler.llm]` and `[runner]` hold scalars **and** named sub-tables. Guidance — decode those as `map[string]toml.Primitive`, extract known scalars (`default`, `inject_protocol`, `lead`; `max_concurrent_requests`, ...) then `md.PrimitiveDecode` the remaining entries into per-provider/per-runner structs.
6. Validate: enums above; `bind` is loopback; `retry_backoff_seconds` non-empty; non-empty argv arrays; resource `slots > 0`; unique gate/invariant/resource names; `[runner].default` references a defined runner; `projfs` provider requires `workspace.projfs.enabled` (Guidance, §1.2 "behind a flag").
7. `init`: Guidance — idempotent; merge into existing `.gitignore` without duplicates; if `.mcp.json` already exists, add/replace only the `mcpServers.cowflow` entry and keep other servers (Guidance).
8. `doctor`: each check returns `{name, status ok|warn|fail, detail, fix}`; `--json` prints the list; non-zero exit when any `fail` (Guidance; code not specified).

## Pitfalls
- `cmd/cowflow/main.go` currently returns `2` for usage; §14 requires `1`.
- `cowflow mcp` appears in §14 both with and without `--role`; role default unspecified.
- `--strategy squash` (§14) vs `squash-merge` (§8.4); `--provider` in §14 omits `container` while config allows it.
- `prompt_mode = "message"` in `[runner.aider]` is not in the listed `arg|stdin|file`.
- `max_concurrent_per_target` (§10.3) vs `max_integrations_per_target` (§11/§22).
- Location of `config.toml` and of the lead token on disk is not specified; do not hardcode silently — record a decision.
- §22 values are an example; only the defaults listed under Rules are stated as defaults. Guidance: use §22 values for the rest and mirror them in `config.example.toml`.
- Windows: `--devdrive` needs elevation once; never loop elevation; paths use `\\?\` above 240 chars (§7).

## Tests required
- Command-tree test: every §14 command/subcommand/flag exists (golden list); `--json` accepted everywhere.
- Exit codes `0/1/2/3/4` per case, including bad args → `1`.
- Config: `config.example.toml` parses and validates; unknown key fails; each enum/required/array rule; aliases `max_builds`/`max_tests`/`max_integrations_per_target`; mixed `[runner]`/`[scheduler.llm]` decoding.
- `init` on a temp Git repo: `.mcp.json` byte-for-byte per §15.2 and valid for Claude Code extension and Codex CLI (§20.29); `.gitignore` entries; idempotent re-run.
- `init --conventions gitflow` (§20.31): non-conforming branch refused in `task create`; agent commit to `main`/`develop` refused; hotfix promoted to `main` creates group for `develop`.
- `init --devdrive` via simulated VHDX interface (§18.1); real Dev Drive test on Windows CI where possible (§20.21).
- `doctor` with faked filesystem/Git probes: ReFS vs NTFS, unmounted VHDX, ProjFS feature, longpaths, autocrlf, `.gitattributes`, git version.
- `workspace destroy` without `--force` refuses with unpreserved changes (§20.12); `inspect` shows real FS and `CowKind` (§20.13).
- Daemonless vs daemon client paths produce the same `--json` output.

## Done checklist
- [ ] Cobra tree matches §14 exactly, plus flags from §6.4/§24.
- [ ] `--json` global; exit codes per §14 (usage fixed to `1`).
- [ ] Config structs cover all §22 sections + §6.7/§15.3 keys; defaults applied; validation with `errors.Join`; unknown keys rejected.
- [ ] `config.example.toml` at repo root, validated in tests.
- [ ] `init`: `.mcp.json`, lead token, `.gitignore`, `--devdrive`, `--conventions`.
- [ ] `doctor` covers §7 + git version with actionable fixes.
- [ ] Ambiguities above recorded in `docs/decisions/`, not silently resolved.
