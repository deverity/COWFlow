# Sprint 0 — Foundation

> **Phase:** [SCOPE.md §19](../SCOPE.md) Phase 0 · **Length:** 2 weeks (10 working days) · **Exit criterion:** `cowflow init --devdrive` creates and mounts a Dev Drive; `cowflow doctor` reports ReFS/COW correctly.

---

## 1. Goal

At the end of this sprint COWFlow is a real Go program instead of a stub. It has:

- a **cobra command tree** with every command from §14 (most of them stubs), a global `--json` flag and the §14 exit codes;
- a **`config.toml` loader** that knows every §22 section, applies the spec's defaults, rejects typos, and ships `config.example.toml`;
- a **SQLite store** (`modernc.org/sqlite`, no cgo) in WAL mode with embedded, versioned migrations and a first base schema;
- a **Git wrapper** that runs `git` safely (argv arrays, `-C`, `GIT_TERMINAL_PROMPT=0`, timeout), checks `git >= 2.40` and validates names with `git check-ref-format`;
- a **Windows platform layer**: Job Objects that kill a whole process tree, long-path handling, and path canonicalization/confinement (with Unix equivalents so everything builds on all three OSes);
- **Dev Drive provisioning**: `cowflow init --devdrive 50GB` creates a dynamic VHDX, formats it as ReFS with the Dev Drive flag and mounts it; `cowflow devdrive create|attach|detach|status` manage it;
- **`cowflow doctor`**, which tells the truth about the machine: Git version, Dev Drive, ReFS vs NTFS, block-clone (COW) support, `core.longpaths`, `core.autocrlf`, `.gitattributes`;
- a **basic hermetic test harness** (fake clock, throw-away Git repos, Git fault injection, simulated VHDX) and **CI re-enabled** on Windows, Linux and macOS.

Why it matters: every later sprint stands on these pieces. Sprint 1's `cow_clone` provider needs the Dev Drive and the volume capability probe; Sprint 2's publish/sync needs the Git wrapper, the store and `check-ref-format`; Sprint 3–4's runners and scheduler need process-tree kill; every sprint needs the CLI skeleton, config, harness and green CI. If the foundation lies (for example, reports COW on NTFS), principle 10 of §2.2 ("Explicit capability… never claims what it does not deliver") is broken from day one.

---

## 2. Exit criteria

Phase 0 output (§19) plus the §20 criteria that [README §3](README.md#3-acceptance-criteria--sprint) assigns to Sprint 0 (16 and the "Dev Drive provisioned by `init`" part of 21).

- [ ] **`cowflow init --devdrive <size>` creates and mounts a Dev Drive** (§19 Phase 0 output, §6.4, §20.21 first half). *Verified:* demo steps 7–9 on a clean Windows 11 machine: `Get-Volume` shows `ReFS` on the chosen letter, `fsutil devdrv query` reports a developer volume, `cowflow devdrive status --json` reports it mounted.
- [ ] **`cowflow doctor` reports ReFS/COW correctly** (§19 Phase 0 output, §7, §2.2 p10). *Verified:* demo steps 6 and 10 (NTFS flagged as "no COW" before, ReFS + block clone after); unit tests `TestDoctor_*` with simulated volumes (S0-T20).
- [ ] **§20.16 — Cancellation on Windows kills the tree.** *Verified:* `TestProcessTree_CancelKillsTree` (3-level tree, no survivors) green on `test (windows-latest)` in CI and in demo step 13; Unix variant green on Linux/macOS.
- [ ] **Go module + cobra** (§16): `go build ./...` with `CGO_ENABLED=0` for windows/linux/darwin × amd64/arm64. *Verified:* CI `lint` job cross-build step (S0-T23).
- [ ] **TOML config**: `config.example.toml` loads and validates; unknown keys rejected. *Verified:* `TestLoad_ExampleConfig`, `TestLoad_UnknownKeyFails` (S0-T03…T05).
- [ ] **SQLite + migrations** (§13): WAL + `busy_timeout`, embedded migrations, idempotent re-open, 20 concurrent writers without `SQLITE_BUSY`. *Verified:* `go test -race ./internal/store/...` (S0-T07, S0-T08).
- [ ] **Git wrapper** (§8.1, §17, §3.1): argv-only exec, `git >= 2.40` check, `check-ref-format`. *Verified:* `go test -race ./internal/git/...` incl. `FuzzCheckRefFormat` seeds (S0-T09…T11).
- [ ] **Windows platform layer** (§7): Job Objects, long paths, canonicalization. *Verified:* `go test ./internal/platform/...` on all three OSes (S0-T12…T14).
- [ ] **CI on 3 platforms** (§19, §20.14 partial): `ci.yml` triggers re-enabled, all jobs green on `develop`, required status checks configured. *Verified:* GitHub Actions run link in the sprint-closing PR (S0-T23, S0-T24).
- [ ] **Basic harness** (§18.1 subset): fake clock, hermetic Git repo, Git subprocess fault injection, simulated VHDX. *Verified:* `go test ./test/harness/...` and its use in S0-T18/S0-T20 tests.

---

## 3. Prerequisites

### 3.1 Tools (Windows 11 developer machine)

Run in **PowerShell**. Commands that need admin are marked.

```powershell
# Go (version must satisfy go.mod: go 1.26.0; the toolchain line selects the patch)
winget install -e --id GoLang.Go
go version                       # expect: go version go1.26.x windows/amd64

# Git >= 2.40 (the only runtime dependency of cowflow, §3.1)
winget install -e --id Git.Git
git --version                    # expect: git version 2.4x.y.windows.z  (x >= 0, major.minor >= 2.40)

# GitHub CLI (PRs, branch protection in S0-T24)
winget install -e --id GitHub.cli
gh auth login

# golangci-lint v2 — same version as CI (.github/workflows/ci.yml)
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
golangci-lint version            # expect: golangci-lint has version v2.15.0 ...

# govulncheck — same version as CI
go install golang.org/x/vuln/cmd/govulncheck@v1.9.0   # needs Go 1.26

# C toolchain: ONLY for `go test -race` (the race detector needs cgo on Windows).
# The cowflow binary itself is always built with CGO_ENABLED=0.
winget install -e --id BrechtSanders.WinLibs.POSIX.UCRT   # or MSYS2 + mingw-w64-ucrt-x86_64-gcc
gcc --version
```

Make sure `%USERPROFILE%\go\bin` is on `PATH` (for `golangci-lint` and `govulncheck`).

Repository setup (signing and DCO are mandatory, see [GITFLOW.md §6](../GITFLOW.md#6-one-time-setup)):

```powershell
git clone https://github.com/deverity/COWFlow.git
cd COWFlow
git switch develop
git config commit.gpgsign true            # plus user.signingkey / gpg.format per GITFLOW.md §6
go build ./... ; go test ./...
```

**Dev Drive / VHDX work (AG-PLAT, AG-CLI on S0-T15…T18, and the demo):**

- Windows 11 **22H2 or later** (§6.4). Check: `(Get-CimInstance Win32_OperatingSystem).BuildNumber` → `22621` or higher. Dev Drive itself needs a cumulative update of 22H2 from late 2023 or later (see [Dev Drive docs](https://learn.microsoft.com/en-us/windows/dev-drive/)); keep the machine updated.
- **Local administrator rights.** Creating/attaching a VHDX and formatting a volume require elevation. Use an elevated terminal ("Run as administrator") for the real-device tests; `cowflow` itself elevates once (S0-T18).
- At least ~1 GB free on `C:` for the dynamic VHDX during tests (it grows on demand; the declared size is a maximum).
- Optional, for the `New-VHD` fallback path only: Hyper-V PowerShell module (`Get-Command New-VHD`). Not available on Windows Home; do not install it just for this sprint — the primary path is `virtdisk.dll`.

Linux/macOS contributors need only Go, Git ≥ 2.40, golangci-lint and a C compiler for `-race` (on macOS: `brew install git` if `git --version` shows Apple Git < 2.40).

### 3.2 Earlier epics

None. Sprint 0 starts from the bootstrap repository (`go.mod` with `module github.com/deverity/cowflow`, `go 1.25.0`; a stdlib-only `cmd/cowflow/main.go`; CI workflows disabled).

### 3.3 Spec gaps that touch this sprint

`AG-LEAD` resolves these with the maintainer on **Day 1 (kickoff)**, records each decision in SCOPE.md §23 and removes the item from [SPEC-GAPS.md](../SPEC-GAPS.md) in the same PR. **This document does not decide them.** Until a decision lands, the affected task codes behind the named seam and does not merge the blocked part.

| Gap | What is undecided | Tasks affected | What is blocked until decided |
|---|---|---|---|
| ~~`C1`~~ | Resolved: Go 1.26+ (`go 1.26.0` plus a `toolchain` line), SCOPE.md §23 decision 1 | S0-T01, S0-T23 | Nothing. |
| `C3` | `promotion_mode` values `manual\|auto` vs also `pr` | S0-T04 | The enum check for `integration.promotion_mode`. |
| `C4` | Sync strategy `squash-merge` vs `--strategy squash` | S0-T02, S0-T04 | Accepted values for `git.default_sync_strategy` and the help text of `sync --strategy`. |
| `C5` | `max_concurrent_per_target` vs `max_integrations_per_target` | S0-T03 | Which key name(s) the schema accepts. |
| `C6` | Invariant example import path `cowflow/cmd` | S0-T05 | The `[[invariants]]` example in `config.example.toml`. |
| `C7` | Golden trees at `<root>/golden/<sha>` vs `workspace.golden_root` | S0-T03, S0-T18 | Whether `workspace.golden_root` exists in the schema and whether `init --devdrive` writes it. |
| `C9` | Per-priority SLA vs `escalation_after_minutes` | S0-T03 | Keys under `[collaboration]`. |
| `C10` | Aider `prompt_mode = "message"` | S0-T04 | The `prompt_mode` enum check. |
| `C11` | Two `[scheduler.llm.anthropic]` examples | S0-T05 | That block in `config.example.toml`. |
| `C12` | §24.13 never-commit list vs `blocked_patterns` | S0-T04 | Default value of `security.blocked_patterns`. |
| `C13` | `gradle_home`, `pip_cache` missing from `[cache]` example | S0-T03, S0-T18 | Keys of `[cache]`; which `cache.*` keys `init --devdrive` writes. |
| `C14` | Defender exclusion: suggest vs required | S0-T18, S0-T20 | Whether `init` only prints the exclusion command or must configure it; severity of the `doctor` Defender check (the check is not merged before the decision). |
| `U-API-1` | SQLite file name/location under `state_dir`; `config.toml` location; lead token storage | S0-T05, S0-T08, S0-T18, S0-T20 | `config.Locate()` and the store path resolver. Code takes explicit paths (injected through `cli.Deps`) until decided. |
| `U-DATA-2` | Prefixes for prefixed ULIDs | S0-T06, S0-T08 | The prefix table in `internal/domain/ids.go`. |
| `U-DATA-1` | Columns of `events`, `outbox`, `event_deliveries`; where `op_id` is stored | S0-T08 | Nothing in S0: `WithTx` takes an `opID` parameter but does not persist it yet (Sprint 2, `S2-E2`). |
| `U-PLAT-5` | Auto-attach of the VHDX at boot: scheduled task vs service (§6.4 step 5) | S0-T17 | The whole real implementation of auto-attach. Interface + fake only. |
| `U-PLAT-6` | `CTRL_BREAK` cancellation when running as a Windows service (no console) | S0-T13 | Nothing in S0 (no service until `S4-E9`); the deadline → `TerminateJobObject` path covers it. |
| `U-PLAT-8` | Hermetic harness "Git repo on tmpfs" on Windows/macOS | S0-T21 | Default repo location on Windows/macOS (an option with `t.TempDir()` until decided). |
| `U-PLAT-4` | Same-volume requirement between `golden_root` and `workspace.root` | S0-T18 | Nothing in S0 beyond C7; note for Sprint 1. |
| `U-API-11` | Which §22 values are defaults vs examples | S0-T04, S0-T18 | Defaults beyond the ones the spec states explicitly; the Dev Drive sub-directory layout written by `init` (`X:/ws`, `X:/cache/...`). |
| `U-API-6`, `U-API-10` | Default role of `cowflow mcp` without `--role`; `init --provider` omits `container` | S0-T02 | Only the stub's flag default and help text. |
| `U-API-12` | Exit codes for cases §14 does not classify: `doctor` with failing checks, a not-yet-implemented command stub, an unclassified internal error, UAC elevation declined | S0-T01, S0-T02, S0-T18, S0-T20 | The `exit*` variables (`exitDoctorFailed`, `exitNotImplemented`, UAC declined); `AG-LEAD` sets the values at kickoff. |
| `U-PLAT-9` | Flags of `devdrive create\|attach\|detach\|status`; defaults of `init --devdrive`'s `--path` and `--letter`; form of the "mount point under `C:\cf\`" (§6.4 step 4). Working proposal to confirm: the `devdrive` subcommands read `[devdrive]` (`path`, `mount`, `size_gb`) from config. | S0-T16, S0-T18 | Flag defaults (both flags required until decided); the `C:\cf\` mount form; `devdrive create` flags. |
| `U-PLAT-10` | Where §6.4 step 5 "registers it in the project state" lives: `[devdrive]` in `config.toml`, a SQLite row, or both | S0-T08, S0-T18 | Dev Drive columns in S0-T08; the registration step of S0-T18. |
| `U-API-13` | The bootstrap `version` subcommand is not in §14. Keep it, or keep only `--version`? | S0-T01, S0-T02 | The `version` subcommand (only `--version` until decided). |

---

## 4. Concepts you need

**Cobra command trees.** Cobra models a CLI as a tree of `*cobra.Command` values: `cowflow` is the root, `devdrive` is a child, `devdrive status` a grandchild. Each command declares `Use`, `Args` (validators such as `cobra.ExactArgs(1)`), flags, and a `RunE` function that returns an `error`. *Persistent* flags (like `--json`) declared on the root are inherited by every descendant. Cobra also auto-adds `help` and `completion` commands; §14 does not list `completion`, so we disable it. Docs: <https://pkg.go.dev/github.com/spf13/cobra>, <https://cobra.dev>.

**Exit codes and `--json`.** Scripts and agents read the process exit code, not the text. §14 fixes `0` ok, `1` usage, `2` state, `3` conflict, `4` refused by policy. We carry the code inside a typed error (`*cli.ExitError`) and translate it once, in `main`. `--json` means "exactly one JSON document on stdout"; human text and diagnostics go elsewhere so a JSON consumer never has to strip noise. Docs: <https://pkg.go.dev/os#Exit>, <https://pkg.go.dev/errors#As>.

**TOML decoding with BurntSushi/toml.** `toml.DecodeFile(path, &cfg)` fills a Go struct using `toml:"key"` tags and returns `toml.MetaData`. `md.Undecoded()` lists keys present in the file that matched no struct field — we turn those into errors so a typo like `max_agent = 20` fails loudly. `toml.Primitive` delays decoding of a sub-tree, which we need for tables that mix scalars with named sub-tables (`[runner]` + `[runner.codex]`). Docs: <https://pkg.go.dev/github.com/BurntSushi/toml>.

**SQLite WAL, `busy_timeout`, `BEGIN IMMEDIATE`.** In WAL (write-ahead log) mode readers never block the writer and the writer never blocks readers; there is still only **one writer at a time**. `busy_timeout` makes a connection wait (instead of failing with `SQLITE_BUSY`) when another process holds the write lock. A normal (`DEFERRED`) transaction starts as a reader and tries to upgrade on its first write; under concurrency that upgrade can fail immediately, ignoring `busy_timeout`. `BEGIN IMMEDIATE` takes the write lock up front, so it waits politely. COWFlow's daemonless mode (§5.3) has many processes writing one file, so this matters. Docs: <https://www.sqlite.org/wal.html>, <https://www.sqlite.org/lang_transaction.html>, <https://www.sqlite.org/pragma.html#pragma_busy_timeout>.

**Embedded migrations.** A migration is a numbered SQL file (`0001_base.sql`) that moves the schema forward one step. `//go:embed migrations/*.sql` compiles them into the binary so there is nothing to ship next to it (§3.1 single binary, §13 "migrations via `embed`"). We store the applied version in SQLite's `PRAGMA user_version`. Applied migrations are never edited — you add a new one. Docs: <https://pkg.go.dev/embed>, <https://www.sqlite.org/pragma.html#pragma_user_version>.

**`modernc.org/sqlite` and `CGO_ENABLED=0`.** `modernc.org/sqlite` is SQLite translated to pure Go, so the binary needs no C compiler and cross-compiles from any OS (§16, §23 decision 2). The driver name is `"sqlite"`; pragmas are passed in the DSN as `_pragma=name(value)` so every pooled connection gets them. Docs: <https://pkg.go.dev/modernc.org/sqlite>.

**Prefixed ULIDs.** A ULID is a 128-bit ID whose first 48 bits are a millisecond timestamp, so IDs sort roughly by creation time; it prints as 26 Crockford base32 characters. "Prefixed" means a short type tag in front (like `prj_…`) so a human can tell a project ID from a task ID — the actual prefixes are an open gap (`U-DATA-2`). `oklog/ulid/v2` generates them; a *monotonic* entropy source guarantees strictly increasing IDs within the same millisecond but is not safe for concurrent use without a mutex. Docs: <https://github.com/oklog/ulid>, <https://github.com/ulid/spec>.

**Running subprocesses safely (`os/exec`).** `exec.CommandContext(ctx, "git", "-C", dir, "status")` passes each argument as a separate string straight to the OS — no shell parses it, so a branch called `x; rm -rf /` is just a weird name, not a command. Never build a string and hand it to `cmd.exe /c` or `sh -c` (§7, §17). `ctx` with a deadline kills the process on timeout; `cmd.WaitDelay` bounds how long we wait for its pipes afterwards. `exec.LookPath` refuses to run a `git.exe` found in the current directory (`exec.ErrDot`). Docs: <https://pkg.go.dev/os/exec>.

**Git invocation rules (`-C`, `GIT_TERMINAL_PROMPT=0`).** `git -C <dir>` runs Git as if started in `<dir>`, so we never `chdir` the whole process. `GIT_TERMINAL_PROMPT=0` makes Git fail instead of waiting for a username/password on a terminal nobody is watching (§8.1). Environment variables like `GIT_DIR` inherited from the caller can silently point Git at another repository; the wrapper removes them. Docs: <https://git-scm.com/docs/git#_environment_variables>.

**`git check-ref-format`.** Git has precise rules for ref names (no `..`, no ASCII control characters, no `~^:?*[\`, cannot end with `.lock`, etc.). Instead of re-implementing them we ask Git: `git check-ref-format --branch <name>` exits 0 for a valid branch name and prints its normalized form. §17 requires this for every name that becomes a branch or ref. We also reject names beginning with `-` ourselves, because Git would read them as options. Docs: <https://git-scm.com/docs/git-check-ref-format>.

**Windows Job Objects.** A Job Object groups processes so the OS can manage them as one unit. With `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, closing the last handle to the job kills every process in it, and `TerminateJobObject` kills them all immediately — including grandchildren, which plain `Process.Kill` misses. A child must be put in the job *before* it can spawn anything, so we start it suspended, assign it, then resume it. Cancellation (§7) is `CTRL_BREAK` → wait a deadline → `TerminateJobObject`. Docs: <https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects>, <https://learn.microsoft.com/en-us/windows/console/generateconsolectrlevent>.

**Unix process groups.** On Linux/macOS, `SysProcAttr{Setpgid: true}` puts the child in a new process group whose ID equals its PID; its descendants inherit the group. `kill(-pgid, SIGTERM)` signals the whole group; after the deadline `SIGKILL` finishes the job (§7). Docs: <https://man7.org/linux/man-pages/man2/setpgid.2.html>, <https://pkg.go.dev/syscall#SysProcAttr>.

**Long paths and `\\?\`.** Classic Win32 APIs limit paths to 260 characters (`MAX_PATH`). Prefixing an absolute path with `\\?\` (or `\\?\UNC\server\share\…`) tells Windows to skip normalization and allow ~32,767 characters. Go's `os` package adds the prefix automatically, but direct `x/sys/windows` calls do not — so §7 says "`\\?\` above 240 chars". For Git, `core.longpaths=true` enables the same in Git for Windows. Docs: <https://learn.microsoft.com/en-us/windows/win32/fileio/maximum-file-path-limitation>.

**Path canonicalization and confinement.** To decide whether a path is "inside" a root, compare canonical forms: absolute → `filepath.Clean` → resolve symlinks (`filepath.EvalSymlinks`) → check that it equals the root or starts with `root + separator` (§7, §17). On Windows the comparison is case-insensitive. Since Go 1.23, `EvalSymlinks` on Windows no longer resolves mount points/junctions, so a junction inside a workspace could escape the root unless we resolve the final path with the OS (`GetFinalPathNameByHandle`). Go 1.24+ also offers `os.Root` for traversal-resistant file access. Docs: <https://pkg.go.dev/path/filepath#EvalSymlinks>, <https://pkg.go.dev/os#Root>, <https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-getfinalpathnamebyhandlew>.

**VHDX and `virtdisk.dll`.** A VHDX is a virtual hard-disk file. A *dynamic* VHDX starts small and grows as data is written, up to its declared size. `virtdisk.dll` exposes `CreateVirtualDisk`, `OpenVirtualDisk`, `AttachVirtualDisk` (make the OS see it as a disk), `DetachVirtualDisk` and `GetVirtualDiskPhysicalPath` (→ `\\.\PhysicalDriveN`). With `ATTACH_VIRTUAL_DISK_FLAG_PERMANENT_LIFETIME` the disk stays attached after our handle closes, but **not** across reboot — hence auto-attach. Docs: <https://learn.microsoft.com/en-us/windows/win32/api/virtdisk/>.

**Dev Drive, ReFS and block clone.** Dev Drive is a Windows 11 volume type built on ReFS and tuned for developer workloads (Defender scans it in a lighter "performance mode"). ReFS supports **block cloning**: `FSCTL_DUPLICATE_EXTENTS_TO_FILE` makes a new file share the same disk clusters as the old one; data is copied only when one side writes (copy-on-write). That is what makes Sprint 1's `cow_clone` workspaces cost metadata, not data (§6.4). A volume advertises it with the `FILE_SUPPORTS_BLOCK_REFCOUNTING` flag. NTFS has no block clone. Dev Drive requires at least 50 GB. Docs: <https://learn.microsoft.com/en-us/windows/dev-drive/>, <https://learn.microsoft.com/en-us/windows-server/storage/refs/block-cloning>.

**Calling Windows DLLs without cgo (`LazyDLL`).** `windows.NewLazySystemDLL("virtdisk.dll")` loads the DLL from `System32` only (never from the current directory) the first time it is used. `proc.Find()` tells you whether the function exists on this Windows version before you call it. `proc.Call(args...)` takes `uintptr` arguments; structs must mirror the C layout exactly, including padding. Docs: <https://pkg.go.dev/golang.org/x/sys/windows#LazyDLL>.

**UAC elevation.** Even an administrator runs programs with a filtered token until they elevate. `windows.GetCurrentProcessToken().IsElevated()` tells you which one you have. To elevate, a program asks the shell to start a copy of itself with the `runas` verb (`ShellExecuteEx`), which shows the UAC prompt; the elevated copy is a separate process with its own console. Docs: <https://learn.microsoft.com/en-us/windows/win32/api/shellapi/nf-shellapi-shellexecuteexw>.

**PowerShell storage cmdlets.** Partitioning and formatting are easiest through `Initialize-Disk`, `New-Partition`, `Format-Volume -DevDrive` (§6.4 names `Format-Volume -DevDrive` and `New-VHD`). We run `powershell.exe -NoProfile -NonInteractive -EncodedCommand <base64>` with a fixed script; user-supplied values are validated and embedded only base64-encoded, never interpolated as code. Docs: <https://learn.microsoft.com/en-us/powershell/module/storage/format-volume>, <https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_powershell_exe>.

**Build constraints by file name.** `process_windows.go` compiles only on Windows, `process_linux.go` only on Linux; `//go:build unix` covers Linux and macOS together. Every OS-specific function needs a counterpart (or a `!windows` stub returning `ErrUnsupported`) so `GOOS=darwin go vet ./...` still passes. Docs: <https://pkg.go.dev/cmd/go#hdr-Build_constraints>.

**Hermetic tests.** A hermetic test gives the same result on any machine, any time: no real clock (`time.Now`), no network, no developer `~/.gitconfig`, no shared files. COWFlow's harness (§18.1) provides a fake clock, throw-away Git repos with an empty global config, and fault injection (make `git` "fail with exit 128" or "time out") so failure paths are testable. Go 1.25's `testing/synctest` can complement the fake clock for goroutine-heavy code later. Docs: <https://go.dev/doc/tutorial/add-a-test>, <https://pkg.go.dev/testing/synctest>.

**The race detector and fuzzing.** `go test -race` instruments memory accesses and fails a test when two goroutines touch the same data without synchronization; on Windows it needs cgo and gcc (tests only — the shipped binary stays `CGO_ENABLED=0`). Native fuzzing (`func FuzzX(f *testing.F)`) generates inputs to find crashes in parsers and validators; `go test` runs only the seed corpus, `go test -fuzz=FuzzX` explores. Docs: <https://go.dev/doc/articles/race_detector>, <https://go.dev/doc/security/fuzz/>.

**CI matrix and required status checks.** `ci.yml` runs the `test` job once per OS (`ubuntu-latest`, `windows-latest`, `macos-latest`); each run appears as a status check named `test (<os>)`. Branch protection can require named checks to pass before merge — but requiring a check that never runs blocks every PR forever, which is why GITFLOW.md §5 says to re-enable triggers first. Docs: <https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/about-protected-branches#require-status-checks-before-merging>.

---

## 5. Agents and lanes

| Agent | Tasks | Notes |
|---|---|---|
| `AG-LEAD` | kickoff (gaps, contracts freeze), all reviews/merges, S0-T24 (with maintainer), S0-T25 (with `AG-QA`) | Unblocks S0-T17 and the gap-dependent parts. |
| `AG-CLI` | S0-T01, S0-T02, S0-T03, S0-T04, S0-T05, S0-T18, S0-T20 | Owns `cmd/cowflow`, `internal/cli`, `internal/config`, `config.example.toml`. |
| `AG-STORE` | S0-T06, S0-T07, S0-T08 | Owns `internal/domain`, `internal/store`. Free from Day 6: reviews, designs Sprint 1 migrations. |
| `AG-GIT` | S0-T09, S0-T10, S0-T11 | Owns `internal/git`. Free from Day 6: reviews, prepares `S1-E2`/`S1-E5`. |
| `AG-PLAT` | S0-T12, S0-T13, S0-T14, S0-T15, S0-T16, S0-T17, S0-T19 | Owns `internal/platform`. **On the critical path; most loaded role.** |
| `AG-QA` | S0-T21, S0-T22, S0-T23, S0-T24, S0-T25 | Owns `test/`, CI workflows. |
| `AG-SEC` | reviews S0-T09, S0-T10, S0-T11, S0-T12, S0-T13, S0-T15, S0-T16, S0-T17, S0-T18, S0-T19, S0-T23, S0-T24 (and the `bind` rule of S0-T04) | Owns no package. |
| `AG-API`, `AG-MCP` | pod-peer reviews of `AG-CLI` PRs that touch shared types (`cli.Deps`, `config.Config`) | No tasks. |
| `AG-WS` | pod-peer reviews of `AG-PLAT` PRs; may pair on S0-T13 (ownership stays with `AG-PLAT`) | No tasks; prepares Sprint 1. |

### Lane diagram (10 working days)

```text
Day        D1      D2      D3      D4      D5      D6      D7      D8      D9      D10
AG-LEAD   [KICK]  ── contracts frozen ── reviews & merges every day ──────── [T24]   [T25 demo]
AG-CLI    [T01]   [T02]   [T03 ─────────] [T04]  [T05]  [T18 ───────────────] [T20 ──────]
AG-STORE  [T06]   [T07 ─────────] [T08 ─────────]  (reviews; S1 migration design)
AG-GIT    [T09 ─────────] [T10]   [T11 ─────────]  (reviews; S1 prep)
AG-PLAT   [T12|T14 ─────] [T15 ─────────] [T16 ─────────] [T13 ─────────] [T19]  [T17*]
AG-QA             [T21 ─────────] [T23]  [T22 ─────────]   ............   [T24]   [T25]
AG-SEC    ...... reviews as PRs arrive (security: yes tasks) ......................

Dependencies (A ──▶ B = B needs A merged; consumers may start earlier on fakes):
  T01 ──▶ T02 ──▶ T18, T20          T03 ──▶ T04 ──▶ T05 ──▶ T18, T20
  T06 ──▶ T07 ──▶ T08               T06, T09 ──▶ T21 ──▶ T23 ──▶ T24
  T09 ──▶ T10 ──▶ T20               T09 ──▶ T11
  T14 ──▶ T15 ──▶ T16 ──▶ T18 ──▶ T25        T12 ──▶ T16
  T14 ──▶ T19 ──▶ T20 ──▶ T25       T14 ──▶ T22 ──▶ T18, T20
  T16 + gap decision ──▶ T17 (*blocked until `U-PLAT-5` auto-attach is decided)
  T13 ──▶ T25 (AC 16 evidence)
```

**Critical path:** `S0-T14 → S0-T15 → S0-T16 → S0-T18 → S0-T20 → S0-T25` (≈ 9.5 days). Any slip in the virtdisk bindings (S0-T15) or the format/mount step (S0-T16) moves the sprint end. Mitigations: `AG-CLI` builds S0-T18 and S0-T20 against the simulated VHDX from S0-T22 from Day 5, so only the final wiring waits for S0-T16; S0-T13 (process tree) is scheduled after the critical items but is self-contained, and `AG-WS` may pair on it.

---

## 6. Contracts

Frozen at kickoff (Day 1). The provider lands the contract file as the **first commit of its first task** (S0-T03, S0-T06, S0-T09, S0-T14) so consumers can compile against it by Day 2–3. Names below are Guidance unless they quote the spec; changing a signature after the freeze needs `AG-LEAD` approval and a note in the affected tasks.

### 6.1 Clock and IDs — `internal/domain`

Provider: `AG-STORE` (S0-T06). Consumers: `AG-STORE` (store), `AG-QA` (harness fake clock), later every service. Fake: `harness.Clock` (S0-T21).

```go
package domain

// Clock is injected everywhere time matters so the harness fake clock works (§18.1).
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
	NewTicker(d time.Duration) Ticker
}

type Ticker interface {
	C() <-chan time.Time
	Stop()
}

// Kind identifies the entity an ID belongs to; its prefix is pending
// SPEC-GAPS U-DATA-2 (prefixes for prefixed ULIDs).
type Kind string

const KindProject Kind = "project"

// IDGen mints prefixed ULIDs (§13). Safe for concurrent use.
type IDGen interface {
	New(kind Kind) (string, error)
}
```

### 6.2 Git runner — `internal/git`

Provider: `AG-GIT` (S0-T09…T11). Consumers: `AG-CLI` (`doctor`), `AG-QA` (fault injection), Sprint 1+ (`AG-WS`, `AG-GIT`). Fakes: `harness.FaultyRunner`, `harness.RecordingRunner` (S0-T21).

```go
package git

// Runner is the single entry point for every git subprocess (§8.1).
// dir == "" means "no -C"; otherwise "-C dir" is prepended.
type Runner interface {
	Run(ctx context.Context, dir string, args ...string) ([]byte, error)
}

// Error is returned when git exits non-zero or times out.
type Error struct {
	Args     []string // without "-C dir"
	ExitCode int      // -1 when killed by timeout/cancel
	Stderr   string   // truncated, never contains env values
	Err      error    // underlying *exec.ExitError or context error
}

var (
	ErrNotFound       = errors.New("git: executable not found")
	ErrVersionTooOld  = errors.New("git: version older than 2.40")
	ErrInvalidRefName = errors.New("git: invalid ref name")
)

type Version struct{ Major, Minor, Patch int; Raw string }

func NewExecRunner(opts ExecOptions) (*ExecRunner, error) // implements Runner
func CheckVersion(ctx context.Context, r Runner) (Version, error)
func CheckBranchName(ctx context.Context, r Runner, name string) (string, error) // normalized name
func CheckRefName(ctx context.Context, r Runner, ref string) error
func ConfigGet(ctx context.Context, r Runner, dir, key string) (value string, found bool, err error)
```

### 6.3 Paths and process trees — `internal/platform`

Provider: `AG-PLAT` (S0-T12, S0-T13). Consumers: `AG-CLI` (`--path` of `init --devdrive`), `AG-PLAT` (VHDX), Sprint 1+ (`AG-WS`, `AG-MCP`, `AG-SCHED`). Pure functions — no fake needed; tests use real temp dirs.

```go
package platform

var ErrOutsideRoot = errors.New("platform: path outside root")

func Canonical(p string) (string, error)          // Abs → Clean → resolve links → (Windows) final path
func Confine(root, p string) (string, error)      // Canonical(p) must be root or below it
func LongPath(p string) string                    // Windows: \\?\ when len > 240; identity elsewhere

// Tree is a started process and all of its descendants.
type Tree interface {
	Pid() int
	Wait() error
	// Cancel asks politely (CTRL_BREAK / SIGTERM), waits up to grace, then kills the whole tree.
	Cancel(grace time.Duration) error
}

func StartTree(cmd *exec.Cmd) (Tree, error)
```

### 6.4 OS, volumes and Dev Drive — `internal/platform`

Provider: `AG-PLAT` (S0-T14 contract; S0-T15/T16/T19 implementation). Consumers: `AG-CLI` (`init --devdrive`, `devdrive *`, `doctor`), Sprint 1 `AG-WS` (capabilities). Fakes: `harness.SimDevDrive`, `harness.FakeHost` (S0-T22).

```go
package platform

var (
	ErrUnsupported    = errors.New("platform: not supported on this OS")
	ErrNotElevated    = errors.New("platform: administrator rights required")
	ErrOSTooOld       = errors.New("platform: Windows 11 22H2 or later required")
	ErrNotMounted     = errors.New("platform: volume not mounted")
	ErrAlreadyExists  = errors.New("platform: virtual disk already exists")
)

type OSInfo struct {
	GOOS     string
	Build    uint32 // Windows build number (e.g. 22631); 0 elsewhere
	Elevated bool
}

type VolumeInfo struct {
	Path       string // volume root, e.g. `X:\` or "/"
	Filesystem string // as reported by the OS: "ReFS", "NTFS", "btrfs", "xfs", "apfs", ...
	BlockClone bool   // FS advertises block cloning (Windows: FILE_SUPPORTS_BLOCK_REFCOUNTING)
	DevDrive   bool   // Windows only: volume is a Dev Drive
}

// Host answers questions about the machine. doctor depends only on this.
type Host interface {
	OS(ctx context.Context) (OSInfo, error)
	Volume(ctx context.Context, path string) (VolumeInfo, error)
	ProjFSAvailable(ctx context.Context) (bool, error)
}

type DevDriveSpec struct {
	VHDXPath  string // canonical absolute path, e.g. C:\cf\cowflow.vhdx
	SizeBytes uint64 // >= 50 GiB (Dev Drive minimum)
	Mount     string // "X:" or a directory under C:\cf\ (U-PLAT-9)
}

type DevDriveStatus struct {
	VHDXPath string
	Exists   bool
	Attached bool
	Mount    string
	Volume   *VolumeInfo // nil when not mounted
}

// DevDrive is the "simulated VHDX (interface)" seam of §18.1.
type DevDrive interface {
	Provision(ctx context.Context, spec DevDriveSpec) (DevDriveStatus, error) // §6.4 steps 2–4
	Attach(ctx context.Context, spec DevDriveSpec) (DevDriveStatus, error)
	Detach(ctx context.Context, vhdxPath string) error
	Status(ctx context.Context, spec DevDriveSpec) (DevDriveStatus, error)
}

// AutoAttacher registers attach-at-boot (§6.4 step 5). Mechanism pending SPEC-GAPS U-PLAT-5.
type AutoAttacher interface {
	Enable(ctx context.Context, spec DevDriveSpec) error
	Disable(ctx context.Context, vhdxPath string) error
}

func NewHost() Host
func NewDevDrive(h Host) DevDrive
```

### 6.5 Config — `internal/config`

Provider: `AG-CLI` (S0-T03…T05). Consumers: `AG-CLI` (`init`, `doctor`), `AG-STORE` (`daemon.state_dir`), later all. Fake: none — tests build a `*config.Config` with `config.Default()` and set fields.

```go
package config

func Default() *Config                                  // spec-stated defaults only (§22, §6.3, §9.4, §23)
func Load(path string) (*Config, error)                 // decode → reject unknown keys → defaults → Validate
func (c *Config) Validate() error                       // errors.Join of every problem
func Save(path string, c *Config) error                 // atomic write (temp file + rename)
func Locate() (string, error)                           // BLOCKED: U-API-1 (config.toml location)
```

### 6.6 Store — `internal/store`

Provider: `AG-STORE` (S0-T07, S0-T08). Consumer: `AG-CLI` (registration in S0-T18 once `U-PLAT-10` is decided), Sprint 1+. Fake: none — SQLite is pure Go; tests open a real store in `t.TempDir()`.

```go
package store

type Options struct {
	BusyTimeout time.Duration // Guidance default 5s
	Clock       domain.Clock
	IDs         domain.IDGen
}

func Open(ctx context.Context, path string, opt Options) (*Store, error) // opens + migrates
func (s *Store) Close() error
func (s *Store) SchemaVersion(ctx context.Context) (int, error)
// WithTx runs fn in a BEGIN IMMEDIATE transaction. opID is accepted now and
// persisted from Sprint 2 (S2-E2) once U-DATA-1 (op_id storage) is decided.
func (s *Store) WithTx(ctx context.Context, opID string, fn func(tx *Tx) error) error
```

### 6.7 CLI wiring — `internal/cli`

Provider: `AG-CLI` (S0-T01). Consumers: everyone who returns errors to the CLI. Other packages never import `internal/cli`; they return sentinel errors and `internal/cli` maps them.

```go
package cli

const (
	ExitOK       = 0 // §14
	ExitUsage    = 1
	ExitState    = 2
	ExitConflict = 3
	ExitPolicy   = 4
)

type ExitError struct {
	Code int
	Err  error
}

type IOStreams struct {
	In       io.Reader
	Out, Err io.Writer
}

// Deps is everything a command needs; tests inject fakes here.
type Deps struct {
	Version, Commit, Date string
	ConfigPath func() (string, error) // until U-API-1 (location) is decided, tests inject it
	Host       platform.Host
	DevDrive   platform.DevDrive
	Git        git.Runner
	Elevate    func(ctx context.Context, args []string) (exitCode int, err error)
}

func Execute(ctx context.Context, args []string, ios IOStreams, deps Deps) int
```

### 6.8 `doctor` result shape

Provider: `AG-CLI` (S0-T20). Consumers: humans, scripts, later MCP. Guidance (spec gives no schema):

```go
type CheckStatus string // "ok" | "warn" | "fail" | "skip"

type CheckResult struct {
	Name   string      `json:"name"`   // e.g. "git.version", "fs.cow"
	Status CheckStatus `json:"status"`
	Detail string      `json:"detail"`
	Fix    string      `json:"fix,omitempty"` // exact command to run
}
```

---

## 7. Tasks

### How every task starts and ends

Every task below assumes these steps; they are not repeated.

```powershell
# start
git switch develop; git pull --ff-only
git switch -c <branch from the task table>

# before every commit
gofmt -l . ; go vet ./... ; go build ./... ; go test -race ./...
# before opening the PR
golangci-lint run ; go mod tidy ; git diff --exit-code go.mod go.sum ; govulncheck ./...
$env:GOOS="linux"; go vet ./... ; $env:GOOS="darwin"; go vet ./... ; Remove-Item Env:GOOS

# commit (Conventional Commit, signed, signed off; no AI attribution trailers)
git add <paths>                      # never blindly `git add -A`; review `git diff --staged`
git commit -S -s -m "<type>(<scope>): <summary>"
git push -u origin <branch>
gh pr create --base develop --fill   # PR title = the Conventional Commit
```

Add a `CHANGELOG.md` `[Unreleased]` entry for anything user-visible. If two PRs both add dependencies, the second rebases and re-runs `go mod tidy` (go.sum conflicts are expected this sprint).

---

### S0-T01 — Cobra root, exit codes and `--json`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-CLI` | `AG-LEAD`, `AG-API` | `S0-E1` | — | `feat/cli-cobra-root` | no |

**Skills:** `cli-and-config`, `go-conventions`, `gitflow`   **Spec:** §14, §16, §16.1
**Files:** `cmd/cowflow/main.go`, `internal/cli/root.go`, `internal/cli/exit.go`, `internal/cli/output.go`, `internal/cli/doc.go`, `internal/cli/root_test.go`, `internal/cli/exit_test.go`, `go.mod`, `go.sum`

**Why / what** — Replace the stdlib stub with a cobra root that every later command hangs from. Fix the plumbing that every command shares: how errors become exit codes (§14: `0` ok, `1` usage, `2` state, `3` conflict, `4` policy), how `--json` works, and how `main` stays thin (`go-conventions`: "thin: build deps, call internal/cli, os.Exit").

**Steps**
1. Add dependencies: `go get github.com/spf13/cobra@latest github.com/stretchr/testify@latest`.
2. `internal/cli/exit.go`: the constants and `ExitError` from Contracts §6.7, plus helpers:
   ```go
   func (e *ExitError) Error() string { return e.Err.Error() }
   func (e *ExitError) Unwrap() error { return e.Err }

   func usageErr(err error) error { return &ExitError{Code: ExitUsage, Err: err} }

   // exitUnclassified is the code for errors §14 does not classify.
   // PENDING U-API-12 — AG-LEAD sets the value at kickoff.
   var exitUnclassified = ExitState

   func codeOf(err error, parsed bool) int {
   	if err == nil {
   		return ExitOK
   	}
   	var ee *ExitError
   	if errors.As(err, &ee) {
   		return ee.Code
   	}
   	if !parsed { // cobra failed before any RunE ran: bad command, flag or args
   		return ExitUsage
   	}
   	return exitUnclassified
   }
   ```
3. `internal/cli/root.go`:
   ```go
   type runState struct{ parsed, json bool }

   func NewRootCmd(ios IOStreams, deps Deps, st *runState) *cobra.Command {
   	root := &cobra.Command{
   		Use:           "cowflow",
   		Short:         "Local runtime for teams of AI agents on one Git repository",
   		Version:       fmt.Sprintf("%s (commit %s, built %s)", deps.Version, deps.Commit, deps.Date),
   		SilenceUsage:  true,
   		SilenceErrors: true,
   		PersistentPreRunE: func(*cobra.Command, []string) error {
   			st.parsed = true // flags and args were valid
   			return nil
   		},
   	}
   	root.CompletionOptions.DisableDefaultCmd = true // `completion` is not in §14
   	root.PersistentFlags().BoolVar(&st.json, "json", false, "print one JSON document on stdout")
   	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageErr(err) })
   	root.SetIn(ios.In)
   	root.SetOut(ios.Out)
   	root.SetErr(ios.Err)
   	return root
   }

   func Execute(ctx context.Context, args []string, ios IOStreams, deps Deps) int {
   	st := &runState{}
   	root := NewRootCmd(ios, deps, st)
   	addCommands(root, ios, deps, st) // S0-T02
   	root.SetArgs(args)
   	err := root.ExecuteContext(ctx)
   	code := codeOf(err, st.parsed)
   	if err != nil {
   		renderError(ios, st.json, code, err)
   	}
   	return code
   }
   ```
   Note: cobra runs `Args` validation *before* `PersistentPreRunE`, so wrong arity is still classified as usage.
4. `internal/cli/output.go`: `func printResult(ios IOStreams, asJSON bool, v any, human func(io.Writer) error) error` — JSON mode: `json.NewEncoder(ios.Out)` with `SetIndent("", "  ")`, exactly one document; human mode: call `human(ios.Out)`. `renderError` writes to **stderr**: human text `cowflow: <message>`, or in JSON mode `{"error":{"message":"...","exit_code":N}}` (Guidance: the error JSON shape is not in the spec; keep it minimal and document it in the PR).
5. `cmd/cowflow/main.go`:
   ```go
   func main() {
   	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
   	code := cli.Execute(ctx, os.Args[1:],
   		cli.IOStreams{In: os.Stdin, Out: os.Stdout, Err: os.Stderr},
   		cli.Deps{Version: version, Commit: commit, Date: date})
   	stop()
   	os.Exit(code)
   }
   ```
   Keep the `version`, `commit`, `date` variables — `.goreleaser.yaml` sets them with `-X main.version=...`.
6. `version` subcommand: cobra's `Version` field gives `cowflow --version`. Whether the bootstrap `version` subcommand survives is `U-API-13`; until decided keep `--version` only and say so in the PR.
7. Package doc in `internal/cli/doc.go` (`// Package cli implements the cowflow command tree (SCOPE.md §14).`).

**Tests** — `internal/cli/exit_test.go`, `root_test.go` (table-driven, `t.Run`, testify):
- `TestExecute_NoArgsPrintsHelp` — `[]` → exit 0, stdout contains `Usage:`.
- `TestExecute_UnknownCommandIsUsage` — `["nope"]` → exit `1`.
- `TestExecute_UnknownFlagIsUsage` — `["--nope"]` → exit `1`.
- `TestExecute_Version` — `["--version"]` → exit 0, stdout contains `dev`.
- `TestCodeOf` — `ExitError{Code: 3}` wrapped twice with `fmt.Errorf("...: %w")` → `3`; nil → `0`.
- `TestRenderError_JSON` — with `--json`, stderr parses as JSON and has `exit_code`.
Run: `go test -race ./internal/cli/... ./cmd/...`.

**Acceptance**
- [ ] `go run ./cmd/cowflow --help` shows cobra help; no `completion` command.
- [ ] Usage errors exit `1`; typed errors keep their code through wrapping.
- [ ] `--json` is a persistent flag on the root.
- [ ] `main.go` only wires dependencies and calls `os.Exit`; no `log.Fatal`; no `fmt.Print` diagnostics outside the output helpers.
- [ ] `go mod tidy` is a no-op; only cobra and testify added.

**Pitfalls**
- Forgetting `SilenceUsage`/`SilenceErrors` → cobra prints usage on *every* error and prints errors twice.
- Cobra returns unknown-command errors as plain `error`s; without the `parsed` flag you cannot tell them from runtime failures.
- Writing errors to stdout in `--json` mode breaks consumers that parse stdout.
- Calling `os.Exit` inside `internal/cli` makes it untestable — return codes instead.

---

### S0-T02 — Full §14 command tree as stubs

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-CLI` | `AG-LEAD`, `AG-MCP` | `S0-E1` | S0-T01 | `feat/cli-command-tree` | no |

**Skills:** `cli-and-config`, `go-conventions`, `gitflow`   **Spec:** §14; §6.4 (`--devdrive`, `--path`, `--letter`); §24.2 (`--kind`, `--force`); §24.7 (`--no-contract`); §24.10 (`sync --reason`); §20.31 (`init --conventions`)
**Files:** `internal/cli/commands.go`, one file per group (`init.go`, `mcp.go`, `doctor.go`, `daemon.go`, `devdrive.go`, `task.go`, `run.go`, `runners.go`, `team.go`, `request.go`, `change.go`, `sync.go`, `integration.go`, `resource.go`, `exec.go`, `index.go`, `workspace.go`, `golden.go`, `logs.go`, `events.go`, `budget.go`), `internal/cli/stub.go`, `internal/cli/tree_test.go`

**Why / what** — Later sprints fill in behavior; the *shape* of the CLI must exist now so docs, tests and agents can rely on it, and so nobody invents a command that §14 does not list. Each command exists with its exact name, arguments and flags; `RunE` returns "not implemented" except `init --devdrive`, `devdrive *` and `doctor`, which S0-T18/S0-T20 implement.

**Steps**
1. `internal/cli/stub.go`:
   ```go
   var errNotImplemented = errors.New("not implemented yet")

   // exitNotImplemented: PENDING U-API-12.
   var exitNotImplemented = exitUnclassified

   func stub(use, short string, args cobra.PositionalArgs) *cobra.Command {
   	return &cobra.Command{Use: use, Short: short, Args: args,
   		RunE: func(*cobra.Command, []string) error {
   			return &ExitError{Code: exitNotImplemented, Err: errNotImplemented}
   		}}
   }
   ```
2. Build the tree exactly as §14 (and the `cli-and-config` rule list): `init`, `mcp`, `doctor`, `daemon start|stop|status|install`, `devdrive create|attach|detach|status`, `task create|list|show|pause|resume`, `run`, `runners list|check`, `status`, `agents`, `intents`, `who`, `open`, `request send|list|accept|reject|close|comment|reassign|deliver`, `change publish|list|show|manifest|discard`, `sync` (+ `sync status|abort`), `integration create|validate|promote|cancel|show|open`, `resources`, `resource acquire|release|revoke|announce`, `exec`, `index rebuild`, `workspace list|inspect|destroy`, `golden list|gc`, `logs`, `events`, `budget`.
3. Flags (names exactly as the spec): `init --devdrive <size> --path --letter --provider --conventions`; `mcp --role --project`; `task create <slug> --base --title --contract --paths --depends --kind --watch --runner --force --no-contract`; `run --max-agents --runner`; `status --watch`; `intents --path`; `who --path`; `request send <to> --kind --title --expect`; `request list --open --to`; `request deliver <id> --change`; `change publish --task`; `sync <change-id> --into --strategy --reason`; `integration create <tasks...> --target`; `integration show <group-id> --diff`; `resource <verb> <name> --wait --ttl`; `exec --resource <name> -- <cmd...>`; `workspace destroy <id> --force`; `logs --task --follow`; `events --follow --since`.
4. Gap-sensitive help text: `mcp --role` has **no default** (`U-API-6`, default `mcp` role); `--provider` lists `auto|cow_clone|projfs|git_worktree` exactly as §14 (`U-API-10`, `init --provider` omits `container`); `sync --strategy` lists the §14 spelling `merge|cherry-pick|squash` with a `PENDING C4` comment. Stubs do not validate enums.
5. `exec`: `Args: cobra.MinimumNArgs(1)` and cobra's `--` handling (`cmd.ArgsLenAtDash()`), so `cowflow exec --resource compile -- go build ./...` parses.
6. `sync` has positional usage *and* subcommands: `sync` gets `Args: cobra.ExactArgs(1)`; children `status`/`abort` get `ExactArgs(1)`.

**Tests** — `internal/cli/tree_test.go`:
- `TestCommandTree_MatchesSpec` — walk the tree recursively and compare the sorted full command paths with a golden slice copied from §14. A missing or extra command fails.
- `TestCommandTree_Flags` — table of `{path, flag}` pairs from step 3; `cmd.Flags().Lookup(flag)` is non-nil for each.
- `TestCommandTree_JSONEverywhere` — every leaf sees the inherited `--json`.
- `TestStubs_ExitCode` — `["task", "list"]` → `exitNotImplemented`; `["task", "show"]` (missing id) → `1`.
- `TestExec_DashDash` — `["exec", "--resource", "compile", "--", "go", "build", "./..."]` parses.
Run: `go test -race ./internal/cli/...`.

**Acceptance**
- [ ] Every §14 command and every flag in step 3 exists; nothing else (except cobra's `help`).
- [ ] Bad arity → exit `1`; stub → `exitNotImplemented`.
- [ ] Gap notes (C4, `U-API-6` for `mcp --role`, `U-API-10` for `--provider`) are present with their SPEC-GAPS reference.

**Pitfalls**
- Adding "helpful" commands or aliases (`ls`, `rm`) — §14 is closed; the spec wins.
- Setting a default for `--role` silently decides an open gap (`U-API-6`).
- `cobra.ArbitraryArgs` everywhere hides usage errors that must exit `1`.

---

### S0-T03 — Config schema and strict decoding

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-CLI` | `AG-LEAD`, `AG-API` | `S0-E2` | — | `feat/config-schema` | no |

**Skills:** `cli-and-config`, `go-conventions`, `gitflow`   **Spec:** §22, §6.7, §11, §15.3, §9.8, §10.3, §24.2
**Files:** `internal/config/doc.go`, `internal/config/config.go`, `internal/config/decode.go`, `internal/config/decode_test.go`, `internal/config/testdata/*.toml`

**Why / what** — One Go struct tree that mirrors every §22 section, decoded with BurntSushi/toml, where unknown keys are errors. This is the contract (§6.5) everything else reads; get it right before defaults and validation.

**Steps**
1. `go get github.com/BurntSushi/toml@latest`.
2. First commit: the contract file (`Default`, `Load`, `Validate`, `Save`, `Locate` signatures returning "not implemented" errors) so consumers compile.
3. Structs for every section: `[daemon]`, `[project]`, `[devdrive]`, `[workspace]`, `[workspace.projfs]`, `[cache]`, `[git]`, `[scheduler]`, `[scheduler.llm]`, `[scheduler.llm.<provider>]`, `[collaboration]`, `[integration]`, `[integration.post_promotion]`, `[[gates]]`, `[[invariants]]`, `[[resources]]`, `[conventions]`, `[runner]`, `[runner.<name>]`, `[watchdog]`, `[security]`. Excerpt:
   ```go
   type Config struct {
   	Daemon        Daemon        `toml:"daemon"`
   	Project       Project       `toml:"project"`
   	DevDrive      DevDrive      `toml:"devdrive"`
   	Workspace     Workspace     `toml:"workspace"`
   	Cache         Cache         `toml:"cache"`
   	Git           Git           `toml:"git"`
   	Scheduler     Scheduler     `toml:"-"` // decoded manually: [scheduler.llm] mixes scalars and tables
   	Collaboration Collaboration `toml:"collaboration"`
   	Integration   Integration   `toml:"integration"`
   	Gates         []Gate        `toml:"gates"`
   	Invariants    []Invariant   `toml:"invariants"`
   	Resources     []Resource    `toml:"resources"`
   	Conventions   Conventions   `toml:"conventions"`
   	Runner        Runner        `toml:"-"` // decoded manually: [runner] mixes scalars and tables
   	Watchdog      Watchdog      `toml:"watchdog"`
   	Security      Security      `toml:"security"`
   }

   type DevDrive struct {
   	Enabled    bool   `toml:"enabled"`
   	Path       string `toml:"path"`
   	Mount      string `toml:"mount"`
   	SizeGB     int    `toml:"size_gb"`
   	AutoAttach bool   `toml:"auto_attach"`
   }

   type Gate struct {
   	Name           string   `toml:"name"`
   	Cmd            []string `toml:"cmd"` // argv array, never a shell string (§7)
   	TimeoutSeconds int      `toml:"timeout_seconds"`
   	Required       bool     `toml:"required"`
   }

   type LLMBudget struct {
   	MaxConcurrentRequests int     `toml:"max_concurrent_requests"`
   	TokensPerHourBudget   int64   `toml:"tokens_per_hour_budget"`
   	CostPerDayBudgetUSD   float64 `toml:"cost_per_day_budget_usd"`
   	OnBudgetExceeded      string  `toml:"on_budget_exceeded"`
   }

   type SchedulerLLM struct {
   	LLMBudget                      // scalars of [scheduler.llm]
   	Providers map[string]LLMBudget // [scheduler.llm.<provider>]
   }

   type RunnerDef struct {
   	Provider       string   `toml:"provider"`
   	Cmd            []string `toml:"cmd"`
   	MCPConfigFlag  []string `toml:"mcp_config_flag"`
   	PermissionFlag []string `toml:"permission_flag"`
   	ContextFile    string   `toml:"context_file"`
   	PromptMode     string   `toml:"prompt_mode"`
   }

   type Runner struct {
   	Default        string
   	InjectProtocol bool
   	Lead           string
   	Defs           map[string]RunnerDef // [runner.<name>]
   }
   ```
4. Mixed tables (`decode.go`): decode the file into a `raw` struct where `Scheduler` is a struct whose `LLM` field is `map[string]toml.Primitive`, and `Runner` is `map[string]toml.Primitive`. For each key, `md.Type("runner", key)` returns `"Hash"` for a sub-table (→ `md.PrimitiveDecode` into `RunnerDef`) or a scalar type (→ the known scalars `default`, `inject_protocol`, `lead`). Same for `scheduler.llm` (scalars `max_concurrent_requests`, `tokens_per_hour_budget`, `cost_per_day_budget_usd`, `on_budget_exceeded`). Unknown scalars are errors.
5. Unknown keys: after decoding, `if u := md.Undecoded(); len(u) > 0` → one error per key, e.g. `config: unknown key "scheduler.max_agent"`. A test must confirm that keys decoded through `PrimitiveDecode` are *not* reported as undecoded.
6. Gaps: `[cache]` has exactly the §22 keys with a `// PENDING C13` comment for `gradle_home`/`pip_cache`; `workspace.golden_root` is present (it is in §22) with `// PENDING C7`; `[collaboration] escalation_after_minutes` per §22 with `// PENDING C9`; the per-target integration key is `max_integrations_per_target` (§11/§22) with `// PENDING C5`. Do not add alternative spellings before the decision.

**Tests** — `decode_test.go` with fixtures in `testdata/`:
- `TestDecode_FullSpecExample` — `testdata/spec22.toml` (verbatim §22) decodes; spot-check values (`Scheduler.LLM.Providers["openai"].MaxConcurrentRequests == 6`, `Runner.Defs["codex"].Cmd[0] == "codex"`).
- `TestDecode_RunnersFromSection15_3` — `testdata/runners.toml` (verbatim §15.3 runner blocks) decodes (validation of `prompt_mode` is S0-T04's job).
- `TestDecode_UnknownKeyFails` — typo in `[scheduler]` and in `[runner.codex]`, both reported with their dotted path.
- `TestDecode_MixedTables` — `[runner]` scalars and `[runner.claude-code]` sub-table both land.
- `FuzzDecode` — seeds from `testdata/`; must never panic.
Run: `go test -race ./internal/config/...`.

**Acceptance**
- [ ] Every §22 section and key has a struct field with the exact TOML name.
- [ ] Unknown keys anywhere (including inside `[runner.<name>]`) fail with the dotted key path.
- [ ] Gap-dependent keys carry `PENDING <id>` comments; nothing decided.

**Pitfalls**
- BurntSushi has no "inline remaining keys" tag; use `Primitive` for mixed tables.
- Decoding `cmd` as `string` instead of `[]string` would allow shell strings (§7, §17).
- Runner names contain `-` (`claude-code`): they are map keys, not struct fields.

---

### S0-T04 — Config defaults and validation

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-CLI` | `AG-LEAD`, `AG-API`, `AG-SEC` | `S0-E2` | S0-T03 | `feat/config-validation` | no |

**Skills:** `cli-and-config`, `security-review`, `go-conventions`, `gitflow`   **Spec:** §22, §6.3, §8.4, §9.4, §9.6, §9.8, §10.3, §12, §15.3, §17, §23 decisions 6, 7, 10
**Files:** `internal/config/defaults.go`, `internal/config/validate.go`, `internal/config/validate_test.go`

**Why / what** — A config that parses can still be dangerous or nonsensical (`bind = "0.0.0.0:8787"`, a gate with an empty `cmd`). `Validate` collects *all* problems with `errors.Join` so the user fixes them in one pass. `Default()` applies only defaults the spec states. (`AG-SEC` reviews the `bind` rule even though the task is not security-critical as a whole.)

**Steps**
1. `Default()` sets exactly the spec-stated defaults: `workspace.provider = "auto"`, `project.lead = "human"`, `project.default_base = "develop"`, `git.default_sync_strategy = "merge"`, `integration.promotion_mode = "manual"`, `workspace.projfs.enabled = false`, `watchdog.enabled = false`, `integration.post_promotion.enabled = false`. Every other §22 value waits for SPEC-GAPS `U-API-11` (§22 defaults vs examples): leave the zero value and a `// PENDING` comment.
2. `Load` = decode into a struct pre-filled by `Default()` (so unset keys keep defaults) → `Validate()`.
3. `Validate()` rules, each appending to a `[]error`:
   - `daemon.bind`: `net.SplitHostPort`; host is a loopback IP (`net.ParseIP(h).IsLoopback()`) or `localhost` (§12, §17).
   - `workspace.provider` ∈ `auto|cow_clone|projfs|git_worktree|container`; `projfs` requires `workspace.projfs.enabled = true` (Guidance, §1.2 "behind a flag").
   - `project.lead` is `human` or `agent:<task-id>` (§9.4).
   - `project.branching` ∈ `gitflow|trunk` (§10.3); `conventions.preset` ∈ `gitflow|trunk|none` (§22).
   - `integration.promotion_mode`: **PENDING C3** — the rule is written as a table entry but disabled; its test row calls `t.Skip("PENDING C3")`.
   - `git.default_sync_strategy`: **PENDING C4** (same approach).
   - `resources[].slots > 0`, `queue` ∈ `fifo|priority`; names unique within `gates`, `invariants`, `resources`.
   - `gates[].cmd` and `runner.<name>.cmd` are non-empty arrays; `invariants[].kind` ∈ `pattern|cmd` (§9.6).
   - `runner.default` and `runner.lead` name a defined `[runner.<name>]`.
   - `runner.<name>.prompt_mode` ∈ `arg|stdin|file`: **PENDING C10** for `message`.
   - `scheduler.retry_backoff_seconds` non-empty.
   - `security.blocked_patterns`: each pattern compiles (`path.Match(p, "")` must not return `path.ErrBadPattern`); the default list is **PENDING C12**.
4. Return `fmt.Errorf("config %s: %w", path, errors.Join(errs...))`. Guidance: `internal/cli` maps config errors to exit `1` (the user must fix input); raise it in the PR if reviewers disagree (`U-API-12`).

**Tests** — `validate_test.go`, one table row per rule (`name`, `mutate func(*Config)`, `wantErrContains`):
- `TestValidate_Defaults` — `Default()` plus a minimal runner definition passes.
- `TestValidate_NonLoopbackBind` — `0.0.0.0:8787`, `192.168.0.2:1`, `127.evil.example:1` fail; `127.0.0.1:8787`, `[::1]:8787`, `localhost:8787` pass.
- `TestValidate_AllErrorsReported` — three broken fields → the error mentions all three.
- `TestValidate_PendingGaps` — rows for C3/C4/C10/C12 skip with their ID.

**Acceptance**
- [ ] Only spec-stated defaults are set; every other default is marked pending.
- [ ] Non-loopback `bind` is rejected.
- [ ] All validation errors are reported together.
- [ ] No gap decided in code; each pending rule names its ID.

**Pitfalls**
- `strings.HasPrefix(bind, "127.")` accepts `127.evil.example` and misses `[::1]`.
- Treating §22 example values as defaults silently decides an open gap (`U-API-11`).

---

### S0-T05 — `config.example.toml`, save and locate

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-CLI` | `AG-LEAD` | `S0-E2` | S0-T04 | `feat/config-example` | no |

**Skills:** `cli-and-config`, `gitflow`   **Spec:** §16.1 (`config.example.toml` at repo root), §22, §6.4 step 6
**Files:** `config.example.toml`, `internal/config/save.go`, `internal/config/locate.go`, `internal/config/example_test.go`, `internal/config/save_test.go`

**Why / what** — Users copy `config.example.toml`; a test guarantees it always loads and validates. `init --devdrive` must write `[devdrive]`, `workspace.root` and `cache.*` (§6.4 step 6), so we need a safe `Save`.

**Steps**
1. `config.example.toml`: mirror §22 verbatim, with short comments. For **C6** (invariant import path) and **C11** (`[scheduler.llm.anthropic]` contents) copy §22's text and add `# PENDING C6` / `# PENDING C11`; adjust after the decision.
2. `Save(path, c)`: encode with `toml.NewEncoder(f)` into a temp file in the **same directory** (`os.CreateTemp(filepath.Dir(path), ".config-*.toml")`), `f.Sync()`, close, copy the previous file to `path + ".bak"`, then `os.Rename(tmp, path)`. Guidance: comments in a hand-edited file are lost on rewrite; `init` prints that a `.bak` was kept.
3. `Locate()`: returns an error naming SPEC-GAPS `U-API-1` (`config.toml` location) until it is decided. `cli.Deps.ConfigPath` lets tests and the demo inject the path.

**Tests**
- `TestExampleConfig_LoadsAndValidates` — `Load(filepath.Join("..", "..", "config.example.toml"))` succeeds (rows pending a gap are skipped in S0-T04).
- `TestSave_RoundTrip` — `Save` then `Load` gives an equal struct (`require.Equal`).
- `TestSave_KeepsBackup` and `TestSave_FailureLeavesOriginal` — inject a failing writer through an unexported hook.

**Acceptance**
- [ ] `config.example.toml` at the repo root, validated by a test on all three OSes.
- [ ] `Save` is atomic and keeps `.bak`.
- [ ] `Locate` does not guess a location.

**Pitfalls**
- Windows: `os.Rename` fails if the target is open with a sharing lock (some editors); return a clear error.
- `.gitattributes` forces LF; do not let an editor rewrite the example with CRLF.

---

### S0-T06 — Domain clock and prefixed ULID generator

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-STORE` | `AG-LEAD`, `AG-GIT` | `S0-E3` | — | `feat/domain-clock-ids` | no |

**Skills:** `store-and-events`, `go-conventions`, `gitflow`   **Spec:** §13 ("Prefixed ULID IDs"), §16 (oklog/ulid), §18.1 (fake clock)
**Files:** `internal/domain/doc.go`, `internal/domain/clock.go`, `internal/domain/ids.go`, `internal/domain/ids_test.go`

**Why / what** — Two tiny seams used everywhere: a `Clock` (so tests never wait for real time) and an `IDGen` (prefixed ULIDs, §13). `internal/domain` imports nothing internal.

**Steps**
1. `go get github.com/oklog/ulid/v2@latest`.
2. `clock.go`: the interfaces of Contracts §6.1 plus `SystemClock` (wrapping `time.Now`, `time.After`, `time.NewTicker`). This is the only place in domain/service code allowed to call `time.Now()`.
3. `ids.go`:
   ```go
   // prefixes: PENDING SPEC-GAPS U-DATA-2. Filled after kickoff.
   var prefixes = map[Kind]string{}

   type ULIDGen struct {
   	mu      sync.Mutex
   	clock   Clock
   	entropy *ulid.MonotonicEntropy
   }

   func NewULIDGen(c Clock) *ULIDGen {
   	return &ULIDGen{clock: c, entropy: ulid.Monotonic(rand.Reader, 0)} // crypto/rand
   }

   func (g *ULIDGen) New(kind Kind) (string, error) {
   	p, ok := prefixes[kind]
   	if !ok {
   		return "", fmt.Errorf("ids: no prefix for kind %q", kind)
   	}
   	g.mu.Lock()
   	defer g.mu.Unlock()
   	id, err := ulid.New(ulid.Timestamp(g.clock.Now()), g.entropy)
   	if err != nil {
   		return "", fmt.Errorf("ids: %w", err)
   	}
   	return p + id.String(), nil
   }
   ```
   Until the decision, tests register a test-only prefix through an unexported helper in `export_test.go`.

**Tests**
- `TestULIDGen_Monotonic` — 10,000 IDs with a frozen clock are strictly increasing.
- `TestULIDGen_Concurrent` — 20 goroutines × 1,000 IDs, no duplicates, `-race` clean.
- `TestULIDGen_UnknownKind` — returns an error.

**Acceptance**
- [ ] `internal/domain` has no internal imports and no I/O.
- [ ] Generator is safe for concurrent use and uses `crypto/rand`.
- [ ] Prefix table empty and marked pending until `U-DATA-2` is decided.

**Pitfalls**
- `ulid.Monotonic` is **not** goroutine-safe — hence the mutex.
- `ulid.Make()` uses its own clock and entropy, bypassing the injected clock.

---

### S0-T07 — SQLite open and embedded migration runner

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-STORE` | `AG-LEAD`, `AG-GIT` | `S0-E3` | S0-T06 | `feat/store-sqlite-migrations` | no |

**Skills:** `store-and-events`, `go-conventions`, `gitflow`   **Spec:** §3.1, §13 ("SQLite WAL, `busy_timeout`, migrations via `embed`"), §16, §23 decision 2
**Files:** `internal/store/doc.go`, `internal/store/sqlite.go`, `internal/store/migrate.go`, `internal/store/migrations/.gitkeep`, `internal/store/migrate_test.go`

**Why / what** — Opening SQLite correctly is where most concurrency bugs are born. Every connection must get WAL, `busy_timeout`, foreign keys and immediate transactions; migrations must be embedded, ordered, and safe if two processes start at once.

**Steps**
1. `go get modernc.org/sqlite@latest`.
2. `sqlite.go`:
   ```go
   import _ "modernc.org/sqlite" // registers driver "sqlite"

   func dsn(path string, busy time.Duration) string {
   	q := url.Values{}
   	q.Add("_pragma", "journal_mode(WAL)")
   	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busy.Milliseconds()))
   	q.Add("_pragma", "foreign_keys(1)")
   	q.Add("_pragma", "synchronous(NORMAL)") // Guidance: the usual pairing with WAL
   	q.Set("_txlock", "immediate")
   	return path + "?" + q.Encode()
   }

   func Open(ctx context.Context, path string, opt Options) (*Store, error) {
   	if strings.ContainsRune(path, '?') {
   		return nil, fmt.Errorf("store: path %q must not contain '?'", path)
   	}
   	if opt.BusyTimeout == 0 {
   		opt.BusyTimeout = 5 * time.Second // Guidance
   	}
   	db, err := sql.Open("sqlite", dsn(path, opt.BusyTimeout))
   	if err != nil {
   		return nil, fmt.Errorf("store: open %s: %w", path, err)
   	}
   	if err := db.PingContext(ctx); err != nil {
   		_ = db.Close() // the ping error is the one that matters
   		return nil, fmt.Errorf("store: ping %s: %w", path, err)
   	}
   	s := &Store{db: db, clock: opt.Clock, ids: opt.IDs}
   	if err := s.migrate(ctx); err != nil {
   		_ = db.Close() // the migration error is the one that matters
   		return nil, err
   	}
   	return s, nil
   }
   ```
3. `migrate.go`:
   ```go
   //go:embed migrations
   var embedded embed.FS

   var migrationFS fs.FS = embedded // tests swap in fstest.MapFS

   type migration struct {
   	version int
   	name    string
   	sql     string
   }

   // loadMigrations parses "NNNN_name.sql", sorts by NNNN, and requires 1..N with no gaps.
   func loadMigrations(fsys fs.FS) ([]migration, error)

   func (s *Store) migrate(ctx context.Context) error {
   	ms, err := loadMigrations(migrationFS)
   	if err != nil {
   		return err
   	}
   	for {
   		applied, err := s.applyNext(ctx, ms) // one BEGIN IMMEDIATE tx per migration
   		if err != nil {
   			return err
   		}
   		if !applied {
   			return nil
   		}
   	}
   }
   ```
   `applyNext` begins the transaction, **reads `PRAGMA user_version` inside it**, returns `false` if up to date, fails with `ErrSchemaTooNew` if `user_version > len(ms)` (a newer binary wrote this file), else executes the next file plus `PRAGMA user_version = N` in the same transaction and commits.
4. Ship no tables yet; S0-T08 adds `0001_base.sql`.

**Tests** — `migrate_test.go`, each test on `filepath.Join(t.TempDir(), "s.db")`:
- `TestOpen_PragmasOnEveryConnection` — `db.SetMaxOpenConns(4)`; hold 4 connections (`db.Conn`) and query `PRAGMA journal_mode`, `busy_timeout`, `foreign_keys` on each → `wal`, `5000`, `1`.
- `TestMigrate_FromEmpty`, `TestMigrate_Idempotent` — open twice; `user_version` stable.
- `TestMigrate_ConcurrentOpeners` — 10 goroutines `Open` the same new file at once; all succeed; each migration applied once (a counting test migration inserts one row).
- `TestMigrate_SchemaTooNew` — `user_version = 99` → `ErrSchemaTooNew`.
- `TestMigrate_GapInNumbering` — MapFS with `0001`, `0003` → error.
Run: `$env:CGO_ENABLED=0; go test ./internal/store/...` and `go test -race ./internal/store/...`.

**Acceptance**
- [ ] Only `modernc.org/sqlite`; builds with `CGO_ENABLED=0`.
- [ ] WAL, `busy_timeout`, `foreign_keys`, immediate transactions on every pooled connection.
- [ ] Migrations embedded, ordered, gap-checked, applied once even with concurrent openers.

**Pitfalls**
- Setting pragmas with one `db.Exec` after `Open` affects only one pooled connection.
- Reading `user_version` *before* `BEGIN IMMEDIATE` races with another process migrating.
- Never back up just the `.db` file — the `-wal` and `-shm` files belong to it.

---

### S0-T08 — Base schema and transaction helper

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-STORE` | `AG-LEAD`, `AG-GIT` | `S0-E3` | S0-T07 | `feat/store-base-schema` | no |

**Skills:** `store-and-events`, `go-conventions`, `gitflow`   **Spec:** §13, §2.2 p6, §5.3 (daemonless writers), §18
**Files:** `internal/store/migrations/0001_base.sql`, `internal/store/tx.go`, `internal/store/projects.go`, `internal/domain/project.go`, `internal/store/tx_test.go`, `internal/store/projects_test.go`

**Why / what** — Give later sprints a working transaction helper and the first §13 table. Scope is deliberately small: the other §13 tables arrive as new numbered migrations with the epics that use them (Sprint 1 asks `AG-STORE` for `workspaces` and `golden_trees`; `S2-E1`/`S2-E2` add tasks, intents, events, outbox, …). `AG-STORE` owns all of them.

**Steps**
1. `0001_base.sql` (Guidance: §13 gives no columns for `projects`; keep it minimal and list the columns in the PR):
   ```sql
   CREATE TABLE projects (
     id         TEXT PRIMARY KEY,         -- prefixed ULID (prefix pending U-DATA-2)
     name       TEXT NOT NULL,
     repo_root  TEXT NOT NULL UNIQUE,     -- canonical path (platform.Canonical)
     created_at TEXT NOT NULL             -- RFC 3339 UTC from domain.Clock
   ) STRICT;
   ```
   No Dev Drive columns until `U-PLAT-10` is decided.
2. `tx.go`:
   ```go
   type Tx struct {
   	tx *sql.Tx
   	s  *Store
   }

   func (s *Store) WithTx(ctx context.Context, opID string, fn func(*Tx) error) (err error) {
   	_ = opID // stored from S2-E2; SPEC-GAPS U-DATA-1 (where op_id is stored)
   	sqlTx, err := s.db.BeginTx(ctx, nil) // _txlock=immediate → BEGIN IMMEDIATE
   	if err != nil {
   		return fmt.Errorf("store: begin: %w", err)
   	}
   	defer func() {
   		if err != nil {
   			_ = sqlTx.Rollback() // the original error is returned
   		}
   	}()
   	if err = fn(&Tx{tx: sqlTx, s: s}); err != nil {
   		return err
   	}
   	return sqlTx.Commit()
   }
   ```
3. `projects.go`: `func (t *Tx) InsertProject(name, repoRoot string) (domain.Project, error)` and `func (s *Store) ProjectByRoot(ctx context.Context, root string) (domain.Project, error)` returning `ErrNotFound`; unique violation → `ErrConflict` (so the CLI can map it to exit `3`).

**Tests**
- `TestWithTx_RollbackOnError` — fn inserts then returns an error → no row.
- `TestWithTx_ConcurrentWriters` — 20 goroutines, **each with its own `Store` on the same file** (simulates daemonless processes), 50 inserts each → 1,000 rows, no `SQLITE_BUSY`, `-race` clean.
- `TestProjects_UniqueRoot` — duplicate `repo_root` → `ErrConflict`.
- Guidance: `TestWithTx_MultiProcess` re-executes the test binary (`os.Args[0] -test.run=TestHelperWriter`) 5 times for a true multi-process check; skipped under `-short`.

**Acceptance**
- [ ] `0001_base.sql` creates `projects` only; columns documented in the PR.
- [ ] `WithTx` uses immediate transactions and rolls back on error.
- [ ] 20 concurrent writers pass under `-race`.

**Pitfalls**
- Holding a transaction while running `git` or waiting on a channel blocks every other process.
- `STRICT` tables reject wrong types — that is the point; do not drop `STRICT` to silence a test.

---

### S0-T09 — Safe Git runner

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-GIT` | `AG-LEAD`, `AG-STORE`, `AG-SEC` | `S0-E4` | — | `feat/git-runner` | yes |

**Skills:** `git-changes-sync`, `security-review`, `go-conventions`, `gitflow`   **Spec:** §8.1 ("Git via `exec.Command` with `-C`, `GIT_TERMINAL_PROMPT=0`, timeout"), §7, §17, §3.1
**Files:** `internal/git/doc.go`, `internal/git/command.go`, `internal/git/errors.go`, `internal/git/command_test.go`, `internal/git/helper_test.go`

**Why / what** — Every Git call in COWFlow goes through one function. That single choke point is where we guarantee no shell, no prompts, a timeout and a clean environment — and where the harness injects faults (§18.1).

**Steps**
1. First commit: the contract file (Contracts §6.2): `Runner`, `Error`, sentinels.
2. `command.go`:
   ```go
   type ExecOptions struct {
   	GitPath  string        // "" → exec.LookPath("git")
   	Timeout  time.Duration // per call; Guidance: from scheduler.command_timeout_seconds
   	ExtraEnv []string      // harness: GIT_CONFIG_GLOBAL=..., fixed author/committer
   }

   type ExecRunner struct {
   	path    string
   	timeout time.Duration
   	env     []string
   }

   // Inherited variables that would point git at another repository or config.
   var strippedEnv = []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE",
   	"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE",
   	"GIT_CEILING_DIRECTORIES", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT"}

   func (r *ExecRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
   	ctx, cancel := context.WithTimeout(ctx, r.timeout)
   	defer cancel()
   	argv := args
   	if dir != "" {
   		argv = append([]string{"-C", dir}, args...)
   	}
   	cmd := exec.CommandContext(ctx, r.path, argv...)
   	cmd.Env = r.env
   	cmd.WaitDelay = 2 * time.Second
   	var out, errb bytes.Buffer
   	cmd.Stdout, cmd.Stderr = &out, &errb
   	if err := cmd.Run(); err != nil {
   		ge := &Error{Args: args, ExitCode: -1, Stderr: truncate(errb.String(), 4096), Err: err}
   		var ee *exec.ExitError
   		if errors.As(err, &ee) && ctx.Err() == nil {
   			ge.ExitCode = ee.ExitCode()
   		} else if ctx.Err() != nil {
   			ge.Err = errors.Join(err, ctx.Err())
   		}
   		return out.Bytes(), ge
   	}
   	return out.Bytes(), nil
   }
   ```
   `r.env` = `os.Environ()` minus `strippedEnv`, plus `GIT_TERMINAL_PROMPT=0`, `LC_ALL=C` (Guidance: stable, parseable output), `GCM_INTERACTIVE=never` (Guidance: no Git Credential Manager pop-ups on Windows), plus `ExtraEnv`.
3. `exec.LookPath` failures (including `exec.ErrDot`) → `ErrNotFound`.
4. `Error.Error()` prints args, exit code and truncated stderr — never env values.

**Tests** — `command_test.go` (real `git` on PATH; repos created with `git init` in `t.TempDir()`):
- `TestRun_UsesDashC` — `Run(ctx, repo, "rev-parse", "--show-toplevel")` while the test's working directory is elsewhere → repo path.
- `TestRun_Env` — `GitPath` = the test binary in helper mode (`helper_test.go`, `COWFLOW_GIT_HELPER=env`) which prints its environment: contains `GIT_TERMINAL_PROMPT=0`, does not contain `GIT_DIR` even though the test set it with `t.Setenv`.
- `TestRun_Timeout` — helper mode that sleeps; `Timeout: 100ms` → `*Error` with `ExitCode == -1` and `errors.Is(err, context.DeadlineExceeded)`.
- `TestRun_ExitCode` — `git rev-parse --verify nope` → `ExitCode == 128`.
- `TestNoShellInvocations` — Guidance: scan `internal/**/*.go` and fail on `"cmd", "/c"`, `"cmd.exe", "/c"` or `"sh", "-c"` pairs.

**Acceptance**
- [ ] Single entry point; argv arrays only; `-C`, `GIT_TERMINAL_PROMPT=0`, timeout always applied.
- [ ] Inherited `GIT_*` redirection variables removed.
- [ ] Errors carry exit code and truncated stderr; never env values.
- [ ] `AG-SEC` approved.

**Pitfalls**
- No `WaitDelay`: a grandchild holding stdout open makes `Run` hang after the timeout.
- `ExitCode()` is `-1` when the process was killed; use `ctx.Err()` to classify timeouts.
- Parsing localized Git output — always `LC_ALL=C`.

---

### S0-T10 — Git version gate and config probes

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-GIT` | `AG-LEAD`, `AG-SEC` | `S0-E4` | S0-T09 | `feat/git-version-check` | yes |

**Skills:** `git-changes-sync`, `security-review`, `gitflow`   **Spec:** §3.1 ("depends only on `git >= 2.40`"), §7 (`core.longpaths`, `core.autocrlf`, `.gitattributes`)
**Files:** `internal/git/version.go`, `internal/git/config.go`, `internal/git/version_test.go`, `internal/git/config_test.go`

**Why / what** — `doctor` must say whether Git is new enough and whether the repository has the §7 settings. These helpers return facts; `doctor` (S0-T20) decides how to present them.

**Steps**
1. `version.go`:
   ```go
   var versionRE = regexp.MustCompile(`^git version (\d+)\.(\d+)(?:\.(\d+))?`)

   // ParseVersion handles "2.47.1.windows.2" and "2.39.5 (Apple Git-154)".
   func ParseVersion(out string) (Version, error)

   func CheckVersion(ctx context.Context, r Runner) (Version, error) {
   	out, err := r.Run(ctx, "", "version")
   	if err != nil {
   		return Version{}, fmt.Errorf("git version: %w", err)
   	}
   	v, err := ParseVersion(strings.TrimSpace(string(out)))
   	if err != nil {
   		return Version{}, err
   	}
   	if v.Major < 2 || (v.Major == 2 && v.Minor < 40) {
   		return v, fmt.Errorf("%w: found %s", ErrVersionTooOld, v.Raw)
   	}
   	return v, nil
   }
   ```
2. `config.go`: `ConfigGet(ctx, r, dir, key)` runs `git config --get <key>`; exit code `1` means "not set" → `found=false, err=nil`; any other failure is an error. Validate `key` against `^[a-z0-9]+(\.[a-zA-Z0-9-]+)+$` (Guidance) before calling.

**Tests**
- `TestParseVersion` — table: `git version 2.40.0`, `2.47.1.windows.2`, `2.39.5 (Apple Git-154)`, `2.40`, garbage.
- `TestCheckVersion_TooOld` — fake `Runner` returning `git version 2.39.5` → `ErrVersionTooOld`.
- `TestConfigGet` — real temp repo: unset → `found=false`; after `git config core.longpaths true` → `"true"`.

**Acceptance**
- [ ] `>= 2.40` enforced with a typed error that includes the found version.
- [ ] "Not set" is distinguished from "git failed".

**Pitfalls**
- macOS machines may have Apple Git 2.39 first on `PATH`; tests must not depend on the machine's Git version.
- `git config --get` exits `1` for a missing key — not an error.

---

### S0-T11 — Ref name validation with `check-ref-format`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-GIT` | `AG-LEAD`, `AG-STORE`, `AG-SEC` | `S0-E4` | S0-T09 | `feat/git-check-ref-format` | yes |

**Skills:** `git-changes-sync`, `security-review`, `gitflow`   **Spec:** §17 ("sanitized names (`check-ref-format`)"), §8.1, §24.2
**Files:** `internal/git/refs.go`, `internal/git/refs_test.go`, `internal/git/testdata/fuzz/FuzzCheckBranchName/*`

**Why / what** — Task slugs and branch names come from users and agents. Every name that becomes a branch or ref is checked by Git itself before use. This task only validates; the branch *pattern* (`cowflow/<project>/<task-slug>` vs `<kind>/<slug>`, gap `C2`) belongs to later sprints and is not decided here.

**Steps**
1. `refs.go`:
   ```go
   func CheckBranchName(ctx context.Context, r Runner, name string) (string, error) {
   	if name == "" || strings.HasPrefix(name, "-") || strings.Contains(name, "@{") {
   		return "", fmt.Errorf("%w: %q", ErrInvalidRefName, name)
   	}
   	out, err := r.Run(ctx, "", "check-ref-format", "--branch", name)
   	if err != nil {
   		var ge *Error
   		if errors.As(err, &ge) && ge.ExitCode > 0 {
   			return "", fmt.Errorf("%w: %q", ErrInvalidRefName, name)
   		}
   		return "", err // git missing or timed out: not the caller's fault
   	}
   	return strings.TrimSpace(string(out)), nil
   }

   // CheckRefName validates a full ref such as refs/cowflow/changes/<id>.
   func CheckRefName(ctx context.Context, r Runner, ref string) error
   ```
   `@{` is rejected up front because `--branch` expands `@{-1}` against a repository. `CheckRefName` requires the `refs/` prefix and runs `git check-ref-format <ref>`.

**Tests**
- `TestCheckBranchName` — valid: `feat/x`, `cowflow/p/auth`; invalid: `-x`, `a..b`, `a.lock`, `a b`, `a~1`, `a^`, `a:b`, `a\b`, `@{-1}`, `x/`, `.x`, `""`.
- `TestCheckRefName` — `refs/cowflow/changes/01HZX…` valid; `cowflow/changes/x` invalid.
- `FuzzCheckBranchName` — properties: never panics; a successful result never starts with `-`. Seeds in `testdata/fuzz/`.

**Acceptance**
- [ ] Every rejection wraps `ErrInvalidRefName`; infrastructure failures do not.
- [ ] Names starting with `-` never reach Git.
- [ ] Fuzz seeds run in plain `go test`.

**Pitfalls**
- Treating "git not found" as "invalid name" hides real failures.
- Re-implementing Git's rules in Go — they change between versions; ask Git.

---

### S0-T12 — Path canonicalization, confinement and long paths

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-PLAT` | `AG-LEAD`, `AG-WS`, `AG-SEC` | `S0-E5` | — | `feat/platform-paths` | yes |

**Skills:** `windows-platform`, `security-review`, `go-conventions`, `gitflow`   **Spec:** §7 ("`\\?\` above 240 chars; short root; canonicalization `Clean + EvalSymlinks + prefix`"), §17 ("confined paths")
**Files:** `internal/platform/doc.go`, `internal/platform/paths.go`, `internal/platform/paths_windows.go`, `internal/platform/paths_other.go`, `internal/platform/paths_test.go`, `internal/platform/paths_windows_test.go`

**Why / what** — Every path that arrives from a user, agent or config must be proven to stay under its allowed root before COWFlow writes or deletes anything. This is the most reused security primitive in the project.

**Steps**
1. `go get golang.org/x/sys@latest` (if not already added by S0-T14).
2. `paths.go`:
   ```go
   func Canonical(p string) (string, error) {
   	abs, err := filepath.Abs(p)
   	if err != nil {
   		return "", fmt.Errorf("canonical %q: %w", p, err)
   	}
   	return resolve(filepath.Clean(abs)) // OS-specific
   }

   // resolveExisting resolves the deepest existing ancestor and re-appends the
   // remaining elements, so not-yet-existing files can still be confined.
   func resolveExisting(p string, eval func(string) (string, error)) (string, error)

   func Confine(root, p string) (string, error) {
   	r, err := Canonical(root)
   	if err != nil {
   		return "", err
   	}
   	c, err := Canonical(p)
   	if err != nil {
   		return "", err
   	}
   	if !within(r, c) {
   		return "", fmt.Errorf("%w: %q not under %q", ErrOutsideRoot, p, root)
   	}
   	return c, nil
   }

   func within(root, p string) bool {
   	if samePath(root, p) {
   		return true
   	}
   	prefix := root
   	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
   		prefix += string(filepath.Separator)
   	}
   	return hasPathPrefix(p, prefix) // case-insensitive on Windows
   }
   ```
3. `paths_windows.go`: `resolve` = `resolveExisting(p, finalPath)`, where `finalPath` opens the path with `windows.CreateFile(..., windows.FILE_FLAG_BACKUP_SEMANTICS, ...)` and calls `windows.GetFinalPathNameByHandle(h, buf, n, windows.VOLUME_NAME_DOS)`, then strips `\\?\` and maps `\\?\UNC\` back to `\\`. This resolves symlinks, **junctions** and 8.3 short names. `samePath`/`hasPathPrefix` use `strings.EqualFold`. `LongPath(p)`: if `len(p) > 240` and not already prefixed → `\\?\` + p, or `\\?\UNC\` + `p[2:]` for `\\server\share\…`.
4. `paths_other.go` (`//go:build !windows`): `resolve` = `resolveExisting(p, filepath.EvalSymlinks)`; byte-exact prefix compare (Guidance: stricter than APFS's usual case-insensitivity, which is safe); `LongPath` returns `p`.

**Tests**
- `TestConfine` (all OSes) — inside; equal to root; `..` escape; absolute path outside; sibling prefix collision (`ws/a` vs `ws/ab`); not-yet-existing file inside; symlink pointing outside (`t.Skip` with reason when creating symlinks needs privileges).
- `TestConfine_Windows` — case difference (`C:\WS\a` vs `c:\ws\a`); junction escaping the root (create it in the test with `powershell.exe -NoProfile -NonInteractive -Command New-Item -ItemType Junction -Path <p> -Target <t>` passed as argv, test-only); `\\?\` input; 8.3 short name (`windows.GetShortPathName`).
- `TestLongPath` — 239 and 241 chars, UNC, already-prefixed.
- `FuzzConfine` — property: on success the result equals root or starts with root + separator.

**Acceptance**
- [ ] Junctions and symlinks cannot escape the root on Windows.
- [ ] Prefix check respects separator boundaries; case-insensitive on Windows.
- [ ] Non-existing leaf paths can be confined.
- [ ] Builds and tests pass on all three OSes.

**Pitfalls**
- Since Go 1.23, `filepath.EvalSymlinks` on Windows no longer resolves mount points/junctions — relying on it alone is a confinement bypass.
- `strings.HasPrefix(p, root)` without the trailing separator.
- `GetFinalPathNameByHandle` returns `\\?\C:\...` — normalize before comparing.
- Alternate data streams (`file.txt:stream`): Guidance — reject `:` in path elements after the volume.

---

### S0-T13 — Process trees: Job Objects and process groups (AC 16)

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-PLAT` | `AG-LEAD`, `AG-WS`, `AG-SEC` | `S0-E5` | — | `feat/platform-process-tree` | yes |

**Skills:** `windows-platform`, `security-review`, `testing-harness`, `go-conventions`, `gitflow`   **Spec:** §7 ("Job Object with `KILL_ON_JOB_CLOSE`; `CTRL_BREAK` → deadline → `TerminateJobObject`. Unix: `Setpgid` + `SIGTERM` + `SIGKILL`"), §3.1, §20.16
**Files:** `internal/platform/process.go`, `internal/platform/process_windows.go`, `internal/platform/process_unix.go`, `internal/platform/process_test.go`, `internal/platform/helper_test.go`

**Why / what** — Agents, builds and tests spawn trees of processes. Cancelling must kill the whole tree, or orphaned `node`/`go` processes keep files locked and burn CPU. This task delivers acceptance criterion 16.

**Steps**
1. `process.go`: the `Tree` interface and `StartTree(cmd)` (Contracts §6.3). The caller builds `cmd` with `exec.Command(name, args...)` (argv only); `StartTree` sets `SysProcAttr` and starts it.
2. `process_windows.go`:
   ```go
   var (
   	ntdll               = windows.NewLazySystemDLL("ntdll.dll")
   	procNtResumeProcess = ntdll.NewProc("NtResumeProcess")
   )

   const processSuspendResume = 0x0800 // PROCESS_SUSPEND_RESUME

   type winTree struct {
   	cmd  *exec.Cmd
   	job  windows.Handle
   	proc windows.Handle
   	done chan struct{}
   	err  error
   }

   func StartTree(cmd *exec.Cmd) (Tree, error) {
   	job, err := windows.CreateJobObject(nil, nil)
   	if err != nil {
   		return nil, fmt.Errorf("create job: %w", err)
   	}
   	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
   	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
   	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
   		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
   		_ = windows.CloseHandle(job)
   		return nil, fmt.Errorf("configure job: %w", err)
   	}
   	cmd.SysProcAttr = &syscall.SysProcAttr{
   		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_SUSPENDED,
   	}
   	if err := cmd.Start(); err != nil {
   		_ = windows.CloseHandle(job)
   		return nil, err
   	}
   	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|processSuspendResume,
   		false, uint32(cmd.Process.Pid))
   	if err == nil {
   		err = windows.AssignProcessToJobObject(job, h)
   	}
   	if err == nil {
   		if st, _, _ := procNtResumeProcess.Call(uintptr(h)); st != 0 { // NTSTATUS
   			err = fmt.Errorf("NtResumeProcess: 0x%x", st)
   		}
   	}
   	if err != nil {
   		_ = cmd.Process.Kill() // still suspended; nothing else ran
   		_ = windows.CloseHandle(job)
   		return nil, fmt.Errorf("start tree: %w", err)
   	}
   	t := &winTree{cmd: cmd, job: job, proc: h, done: make(chan struct{})}
   	go func() { t.err = cmd.Wait(); close(t.done) }()
   	return t, nil
   }

   func (t *winTree) Cancel(grace time.Duration) error {
   	// Best effort: fails without a shared console (CI, services) — the deadline path covers it.
   	_ = windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(t.cmd.Process.Pid))
   	timer := time.NewTimer(grace)
   	defer timer.Stop()
   	select {
   	case <-t.done:
   	case <-timer.C:
   	}
   	// Kill every remaining member, including grandchildren that outlived the root.
   	return windows.TerminateJobObject(t.job, 1)
   }
   ```
   `Wait()` waits on `done`, then closes `proc` and `job` (closing the job also kills stragglers via `KILL_ON_JOB_CLOSE`).
3. `process_unix.go` (`//go:build unix`): `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}`; `Cancel`: `unix.Kill(-pgid, unix.SIGTERM)` → wait `grace` → `unix.Kill(-pgid, unix.SIGKILL)`; ignore `ESRCH`.
4. Guidance: a real timer inside `Cancel` is acceptable (bounded OS-level wait); callers inject `grace`.

**Tests** — `helper_test.go` + `process_test.go`:
- Helper mode: `TestMain` checks `COWFLOW_TREE_HELPER=<depth>`; the helper spawns itself with `depth-1` using **plain `exec.Command`** (grandchildren must be caught by inheritance, not by our code), prints `PID <n>` lines to stdout, then sleeps 10 minutes.
- `TestProcessTree_CancelKillsTree` (**AC 16**) — start depth 3 via `StartTree`, read 3 PIDs, `Cancel(500 * time.Millisecond)`, then assert every PID is gone within 5 s: Windows — `windows.OpenProcess(windows.SYNCHRONIZE, …)` then `WaitForSingleObject(h, 5000) == WAIT_OBJECT_0` (or `OpenProcess` fails because the process no longer exists); Unix — `unix.Kill(pid, 0)` returns `ESRCH`.
- `TestProcessTree_GracefulExit` — helper that exits on `CTRL_BREAK`/`SIGTERM` → `Cancel` returns before the grace deadline.
- `TestProcessTree_KillOnClose` (Windows) — close the job without `Cancel` → all members gone.
Run: `go test -race -run ProcessTree -count=1 -v ./internal/platform/`.

**Acceptance**
- [ ] AC 16 test green on `windows-latest`; Unix variant green on Linux and macOS.
- [ ] The child starts suspended and is in the job before it runs any code.
- [ ] No orphans after `Cancel`; all handles closed.
- [ ] `AG-SEC` approved (argv only, no shell).

**Pitfalls**
- Assigning the job *after* the child runs lets fast children escape — hence `CREATE_SUSPENDED`.
- `GenerateConsoleCtrlEvent` needs `CREATE_NEW_PROCESS_GROUP` and a shared console; never rely on it alone (SPEC-GAPS `U-PLAT-6`, `CTRL_BREAK` under a Windows service).
- PID reuse: check liveness promptly, by handle on Windows.
- On Unix, a grandchild that calls `setsid` leaves the group; document it as a known limit in the PR.

---

### S0-T14 — Platform contracts, OS info and filesystem capabilities

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-PLAT` | `AG-LEAD`, `AG-WS` | `S0-E5` | — | `feat/platform-fscaps` | no |

**Skills:** `windows-platform`, `go-conventions`, `gitflow`   **Spec:** §2.2 p10, §6.3, §6.4, §7 ("Dev Drive present and mounted; ReFS confirmed; NTFS flagged as no COW"), §16.1 (`fscaps_*`)
**Files:** `internal/platform/host.go` (contract: Contracts §6.4), `internal/platform/osinfo_windows.go`, `internal/platform/osinfo_other.go`, `internal/platform/fscaps_windows.go`, `internal/platform/fscaps_linux.go`, `internal/platform/fscaps_darwin.go`, `internal/platform/fscaps_other.go`, `internal/platform/fscaps_test.go`

**Why / what** — `doctor` and Sprint 1's `cow_clone` both need a truthful answer to "what filesystem is this path on, and can it block-clone?". This task lands the whole §6.4 contract file first (so `AG-CLI` and `AG-QA` can code against it on Day 2) and implements `OS()` and `Volume()` on all three OSes.

**Steps**
1. First commit: `host.go` with every type, interface and sentinel from Contracts §6.4, plus `NewHost()` and a `NewDevDrive()` returning `ErrUnsupported` until S0-T16.
2. `osinfo_windows.go`: `Build` from `windows.RtlGetVersion().BuildNumber`; `Elevated` from `windows.GetCurrentProcessToken().IsElevated()`. `osinfo_other.go`: `Build = 0`, `Elevated = os.Geteuid() == 0` (Guidance: informational only).
3. `fscaps_windows.go`:
   ```go
   const fileSupportsBlockRefcounting = 0x08000000 // FILE_SUPPORTS_BLOCK_REFCOUNTING

   func (h *host) Volume(ctx context.Context, path string) (VolumeInfo, error) {
   	p16, err := windows.UTF16PtrFromString(LongPath(path))
   	if err != nil {
   		return VolumeInfo{}, err
   	}
   	root := make([]uint16, windows.MAX_PATH+1)
   	if err := windows.GetVolumePathName(p16, &root[0], uint32(len(root))); err != nil {
   		return VolumeInfo{}, fmt.Errorf("volume path of %q: %w", path, err)
   	}
   	var flags uint32
   	fsName := make([]uint16, windows.MAX_PATH+1)
   	if err := windows.GetVolumeInformation(&root[0], nil, 0, nil, nil, &flags,
   		&fsName[0], uint32(len(fsName))); err != nil {
   		return VolumeInfo{}, fmt.Errorf("volume information: %w", err)
   	}
   	return VolumeInfo{
   		Path:       windows.UTF16ToString(root),
   		Filesystem: windows.UTF16ToString(fsName),        // "ReFS", "NTFS", ...
   		BlockClone: flags&fileSupportsBlockRefcounting != 0,
   		DevDrive:   false,                                // filled by S0-T19
   	}, nil
   }
   ```
4. `fscaps_linux.go`: `unix.Statfs`; map `Type` to `btrfs` (`unix.BTRFS_SUPER_MAGIC`), `xfs` (`unix.XFS_SUPER_MAGIC`), `ext4`, … and set `BlockClone` for btrfs only; XFS reflink support cannot be read from `statfs` — report `BlockClone: false` with Filesystem `xfs` and leave confirmation to the probe clone of `S1-E3` (Guidance). `fscaps_darwin.go`: `unix.Statfs` → `Fstypename` (`apfs` → `BlockClone: true`). `fscaps_other.go`: `ErrUnsupported`.
5. Never claim more than the OS reports (§2.2 p10). Sprint 1 (`S1-E3`) adds a real probe clone to confirm.

**Tests**
- `TestVolume_TempDir` (all OSes) — returns a non-empty `Filesystem` for `t.TempDir()`.
- `TestVolume_WindowsSystemDrive` — `C:\` → `Filesystem == "NTFS"` on GitHub runners and typical laptops; `BlockClone == false`.
- `TestVolume_ReFS` — only when `COWFLOW_TEST_REFS_PATH` is set (e.g. `X:\`): `Filesystem == "ReFS"`, `BlockClone == true`; otherwise `t.Skip("set COWFLOW_TEST_REFS_PATH to a ReFS volume")`.
- `TestOSInfo` — Windows: `Build > 0`.
- `GOOS=linux|darwin go vet ./internal/platform/` passes.

**Acceptance**
- [ ] Contract file merged by end of Day 2.
- [ ] `Volume` reports the OS's filesystem name and the block-refcounting flag, nothing invented.
- [ ] Compiles on all three OSes; unsupported OSes return `ErrUnsupported`.

**Pitfalls**
- `GetVolumeInformation` needs the **volume root with trailing backslash**, not an arbitrary directory — hence `GetVolumePathName` first.
- Mounted folders (`C:\cf\mnt`) are separate volumes; `GetVolumePathName` handles that, `filepath.VolumeName` does not.
- Do not infer COW from the filesystem name alone on Linux (XFS without `reflink=1`).

---

### S0-T15 — `virtdisk.dll` bindings and elevation helper

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-PLAT` | `AG-LEAD`, `AG-WS`, `AG-SEC` | `S0-E6` | S0-T14 | `feat/platform-vhdx-virtdisk` | yes |

**Skills:** `windows-platform`, `security-review`, `go-conventions`, `gitflow`   **Spec:** §6.4 steps 1–3 ("creates a dynamic VHDX via `virtdisk.dll` (`CreateVirtualDisk`)", "Attaches (`AttachVirtualDisk`)", "elevates once"), §16 (`x/sys/windows`, `virtdisk.dll`), §23 decision 1 (`CGO_ENABLED=0`)
**Files:** `internal/platform/virtdisk_windows.go`, `internal/platform/virtdisk_windows_test.go`, `internal/platform/elevate_windows.go`, `internal/platform/elevate_other.go`, `internal/platform/elevate_windows_test.go`

**Why / what** — Thin, exact Go mirrors of the Win32 virtual-disk API, plus the "run myself elevated once" helper. No orchestration here (S0-T16 does that); this task is about getting struct layouts and return codes right, which is where most bugs hide.

**Steps**
1. Lazy procs:
   ```go
   var (
   	modVirtDisk                    = windows.NewLazySystemDLL("virtdisk.dll")
   	procCreateVirtualDisk          = modVirtDisk.NewProc("CreateVirtualDisk")
   	procOpenVirtualDisk            = modVirtDisk.NewProc("OpenVirtualDisk")
   	procAttachVirtualDisk          = modVirtDisk.NewProc("AttachVirtualDisk")
   	procDetachVirtualDisk          = modVirtDisk.NewProc("DetachVirtualDisk")
   	procGetVirtualDiskPhysicalPath = modVirtDisk.NewProc("GetVirtualDiskPhysicalPath")
   )

   // errVirtDiskUnavailable lets S0-T16 fall back to PowerShell New-VHD.
   var errVirtDiskUnavailable = errors.New("virtdisk.dll procedures unavailable")

   func virtdiskAvailable() error {
   	for _, p := range []*windows.LazyProc{procCreateVirtualDisk, procOpenVirtualDisk,
   		procAttachVirtualDisk, procDetachVirtualDisk, procGetVirtualDiskPhysicalPath} {
   		if err := p.Find(); err != nil {
   			return fmt.Errorf("%w: %v", errVirtDiskUnavailable, err)
   		}
   	}
   	return nil
   }
   ```
2. Structs — keep the C union shape (a `Version` field followed by a nested struct) so Go inserts the same padding as C:
   ```go
   type virtualStorageType struct {
   	DeviceID uint32
   	VendorID windows.GUID
   }

   const virtualStorageTypeDeviceVHDX = 3

   // VIRTUAL_STORAGE_TYPE_VENDOR_MICROSOFT {EC984AEC-A0F9-47E9-901F-71415A66345B}
   var vendorMicrosoft = windows.GUID{Data1: 0xec984aec, Data2: 0xa0f9, Data3: 0x47e9,
   	Data4: [8]byte{0x90, 0x1f, 0x71, 0x41, 0x5a, 0x66, 0x34, 0x5b}}

   type createVirtualDiskParameters struct {
   	Version uint32 // CREATE_VIRTUAL_DISK_VERSION_2 = 2
   	V2      createVirtualDiskV2
   }

   type createVirtualDiskV2 struct {
   	UniqueID                  windows.GUID
   	MaximumSize               uint64
   	BlockSizeInBytes          uint32
   	SectorSizeInBytes         uint32
   	PhysicalSectorSizeInBytes uint32
   	ParentPath                *uint16
   	SourcePath                *uint16
   	OpenFlags                 uint32
   	ParentVirtualStorageType  virtualStorageType
   	SourceVirtualStorageType  virtualStorageType
   	ResiliencyGUID            windows.GUID
   }

   type attachVirtualDiskParameters struct {
   	Version  uint32 // ATTACH_VIRTUAL_DISK_VERSION_1 = 1
   	Reserved uint32
   }

   const (
   	attachFlagPermanentLifetime = 0x4 // ATTACH_VIRTUAL_DISK_FLAG_PERMANENT_LIFETIME
   	virtualDiskAccessNone       = 0
   )
   ```
3. Wrappers return Win32 error codes from the **return value**, not `GetLastError`:
   ```go
   func createVHDX(path string, size uint64) (windows.Handle, error) {
   	p, err := windows.UTF16PtrFromString(LongPath(path))
   	if err != nil {
   		return 0, err
   	}
   	st := virtualStorageType{DeviceID: virtualStorageTypeDeviceVHDX, VendorID: vendorMicrosoft}
   	params := createVirtualDiskParameters{Version: 2, V2: createVirtualDiskV2{MaximumSize: size}}
   	var h windows.Handle
   	r, _, _ := procCreateVirtualDisk.Call(
   		uintptr(unsafe.Pointer(&st)), uintptr(unsafe.Pointer(p)),
   		virtualDiskAccessNone, 0, // no security descriptor
   		0, 0,                     // CREATE_VIRTUAL_DISK_FLAG_NONE → dynamic; provider flags
   		uintptr(unsafe.Pointer(&params)), 0, // no OVERLAPPED
   		uintptr(unsafe.Pointer(&h)))
   	if r != 0 {
   		return 0, fmt.Errorf("CreateVirtualDisk: %w", windows.Errno(r))
   	}
   	return h, nil
   }
   ```
   Same pattern for `openVHDX(path)`, `attach(h)` (permanent lifetime, no drive letter yet), `detach(h)`, `physicalPath(h)` (→ `\\.\PhysicalDriveN`; parse `N` with `strconv.Atoi` and fail on anything else).
4. `elevate_windows.go` — `RunElevated(ctx, exe string, args []string, dir string) (int, error)` via `shell32.dll` `ShellExecuteExW` with `SEE_MASK_NOCLOSEPROCESS`, verb `runas`, `lpParameters` built with `syscall.EscapeArg` for each argument (the standard Windows argv quoting; no shell involved), then `WaitForSingleObject` (respecting `ctx`) and `GetExitCodeProcess`. `ERROR_CANCELLED` (UAC declined) → `ErrNotElevated`. `elevate_other.go` → `ErrUnsupported`.

**Tests**
- `TestStructLayout` (Windows, amd64 and arm64) — `unsafe.Sizeof`/`Offsetof` against the SDK values: `createVirtualDiskParameters` V2 offset `8` (padding after `Version`), `MaximumSize` offset `16` within V2, total size matches `sizeof(CREATE_VIRTUAL_DISK_PARAMETERS)` from the Windows SDK (record the number in a comment with its source).
- `TestVirtdiskAvailable` — no error on Windows 10+.
- `TestCreateAttachDetach_Real` — gated: requires admin **and** `COWFLOW_TEST_VHDX=1`; creates a 3 GB dynamic VHDX in `t.TempDir()`, attaches, reads the physical path, detaches, deletes. Skips with a reason otherwise.
- `TestEscapeArgs` — arguments with spaces, quotes and trailing backslashes round-trip through `windows.CommandLineToArgv`.

**Acceptance**
- [ ] Struct layouts verified by tests on amd64 (and arm64 via `GOARCH=arm64 go vet` + CI if available).
- [ ] All procs checked with `Find()`; missing procs yield `errVirtDiskUnavailable`.
- [ ] Return codes read from `r1`; errors wrap `windows.Errno`.
- [ ] Elevation never loops and never builds a `cmd.exe` string.
- [ ] `AG-SEC` approved.

**Pitfalls**
- Flattening the union (putting `UniqueID` right after `Version`) loses 4 bytes of padding — `CreateVirtualDisk` then fails with `ERROR_INVALID_PARAMETER` or worse. Reference layout to *read*, not import: `github.com/Microsoft/go-winio/vhd`.
- The `err` returned by `LazyProc.Call` is `GetLastError` and is often stale; virtdisk returns the error code directly.
- With version-2 create parameters the access mask must be `VIRTUAL_DISK_ACCESS_NONE`.
- Permanent-lifetime attach survives our process but **not** a reboot.

---

### S0-T16 — Dev Drive provisioning: partition, format, mount, fallback

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-PLAT` | `AG-LEAD`, `AG-WS`, `AG-SEC` | `S0-E6` | S0-T12, S0-T15 | `feat/platform-devdrive-provision` | yes |

**Skills:** `windows-platform`, `security-review`, `go-conventions`, `gitflow`   **Spec:** §6.4 steps 1–4 and "Failures", §18 ("Disk full / provider fails → explicit failure; … compensation"; "VHDX not mounted → … never silently falls back to NTFS"), §7 ("never `cmd.exe` with a string")
**Files:** `internal/platform/devdrive.go`, `internal/platform/devdrive_windows.go`, `internal/platform/devdrive_other.go`, `internal/platform/psscript_windows.go`, `internal/platform/devdrive_windows_test.go`, `internal/platform/psscript_windows_test.go`

**Why / what** — Implements `platform.DevDrive` (Contracts §6.4): the sequence that turns "nothing" into "an ReFS Dev Drive mounted at `X:`", with compensation if any step fails. The CLI (S0-T18) only calls `Provision`/`Attach`/`Detach`/`Status`.

**Steps**
1. `Provision(ctx, spec)` order:
   1. `OS()` → `Build < 22621` → `ErrOSTooOld`; not elevated → `ErrNotElevated` (the CLI elevates, S0-T18).
   2. Validate: `spec.SizeBytes >= 50 << 30` (Dev Drive minimum; Guidance from Microsoft docs); `spec.VHDXPath` passes `Canonical`, ends in `.vhdx`, does not exist (`ErrAlreadyExists`); `spec.Mount` is `^[D-Z]:$` and the letter is free (`windows.GetLogicalDrives` bitmask) or an empty directory under `C:\cf\` (form pending `U-PLAT-9`).
   3. `os.MkdirAll(filepath.Dir(VHDXPath))`; `virtdiskAvailable()` → `createVHDX` → `attach`; on `errVirtDiskUnavailable` use the `New-VHD` fallback script (step 3).
   4. `physicalPath` → disk number `N`.
   5. Run the format script (step 2) with `N` and the mount target.
   6. `Status()`; require `Volume.Filesystem == "ReFS"`; otherwise fail (never report success on NTFS).
   7. On any failure after the file was created: **compensate** — detach if attached, delete the VHDX this call created, and return an error naming the failed step. Never leave a half-provisioned disk that `Status` would call ready.
2. `psscript_windows.go` — fixed script, values passed only as validated literals or base64:
   ```go
   const formatScript = `
   $ErrorActionPreference = 'Stop'
   $n = [int]'{{.Disk}}'
   $mount = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('{{.MountB64}}'))
   Initialize-Disk -Number $n -PartitionStyle GPT
   if ($mount -match '^[D-Z]:$') {
     $p = New-Partition -DiskNumber $n -UseMaximumSize -DriveLetter $mount[0]
     Format-Volume -DriveLetter $mount[0] -DevDrive -Confirm:$false | Out-Null
   } else {
     $p = New-Partition -DiskNumber $n -UseMaximumSize
     Format-Volume -Partition $p -DevDrive -Confirm:$false | Out-Null
     Add-PartitionAccessPath -DiskNumber $n -PartitionNumber $p.PartitionNumber -AccessPath $mount
   }
   `

   func runPowerShell(ctx context.Context, script string) error {
   	enc := utf16.Encode([]rune(script))
   	b := make([]byte, 2*len(enc))
   	for i, u := range enc {
   		binary.LittleEndian.PutUint16(b[2*i:], u)
   	}
   	cmd := exec.CommandContext(ctx, `powershell.exe`, "-NoProfile", "-NonInteractive",
   		"-ExecutionPolicy", "Bypass", "-EncodedCommand", base64.StdEncoding.EncodeToString(b))
   	var stderr bytes.Buffer
   	cmd.Stderr = &stderr
   	if err := cmd.Run(); err != nil {
   		return fmt.Errorf("powershell: %w: %s", err, truncate(stderr.String(), 2048))
   	}
   	return nil
   }
   ```
   Render with `text/template` from a struct whose `Disk` is an `int` and `MountB64` is base64 of the already-validated mount — no user text ever appears unencoded in the script. Resolve `powershell.exe` from `%SystemRoot%\System32\WindowsPowerShell\v1.0\` (Guidance), not from `PATH`.
3. Fallback script for create+attach when virtdisk is unavailable (§6.4 "falls back to PowerShell `New-VHD`"): `New-VHD -Path <b64-decoded> -SizeBytes <int> -Dynamic | Out-Null; (Mount-VHD -Path <…> -Passthru | Get-Disk).Number` — parse the single integer from stdout.
4. `Attach(ctx, spec)`: `openVHDX` → `attach` (permanent) → if the volume does not come back at `spec.Mount` (Windows usually remembers the letter), assign it with a fixed `Set-Partition`/`Add-PartitionAccessPath` script; return `Status`. `Detach`: `openVHDX` → `detach`. `Status`: file exists? attached (`GetVirtualDiskPhysicalPath` succeeds on an opened handle)? `Volume(spec.Mount)` when mounted.
5. Defender exclusion is **not** done here (C14); S0-T18 prints the suggestion.
6. `devdrive_other.go`: every method returns `ErrUnsupported`.

**Tests**
- `TestFormatScript_Golden` — rendered script for `Disk: 3, Mount: "X:"` equals a golden file; a mount containing `'; Remove-Item C:\ -Recurse #` appears only base64-encoded.
- `TestProvision_Validation` — table over a fake `Host`: build 19045 → `ErrOSTooOld`; not elevated → `ErrNotElevated`; 10 GB → size error; letter in use → error; existing file → `ErrAlreadyExists`.
- `TestProvision_CompensatesOnFormatFailure` — inject a failing `runPowerShell` (package-level hook) → VHDX file removed, detach called.
- `TestProvision_Real` — gated by admin + Windows 11 + `COWFLOW_TEST_DEVDRIVE=1`: provisions 50 GB dynamic at a free letter, asserts `ReFS` and `BlockClone`, then detaches and deletes. Run manually on a developer machine; record output in the PR.

**Acceptance**
- [ ] §6.4 steps 1–4 implemented; virtdisk first, `New-VHD` fallback.
- [ ] No user-provided text is interpolated into PowerShell source.
- [ ] Any failure after creation leaves no attached disk and no orphan VHDX created by this call.
- [ ] Success is reported only after `Volume` confirms `ReFS`.
- [ ] `TestProvision_Real` evidence (output) attached to the PR.
- [ ] `AG-SEC` approved.

**Pitfalls**
- `Format-Volume -DevDrive` fails on Windows builds without Dev Drive support and on volumes < 50 GB — surface PowerShell's error text, do not parse it (it is localized).
- Windows may auto-assign a letter on attach; check before assigning to avoid "letter in use".
- `New-VHD` needs the Hyper-V PowerShell module (not on Windows Home) — the fallback can itself be unavailable; report both causes.
- Shell Hardware Detection may pop up "format this disk?" dialogs between attach and format; they are harmless but confuse demos.

---

### S0-T17 — VHDX auto-attach at boot (seam only; blocked)

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-PLAT` | `AG-LEAD`, `AG-WS`, `AG-SEC` | `S0-E6` | S0-T16; decision on `U-PLAT-5` (VHDX auto-attach at boot) | `feat/platform-devdrive-autoattach` | yes |

**Skills:** `windows-platform`, `security-review`, `gitflow`   **Spec:** §6.4 step 5 ("configures auto-attach at boot (scheduled task or service)"), §7 ("VHDX auto-attach before the daemon"), §18 ("VHDX not mounted → `pending_mount`; auto-attach")
**Files:** `internal/platform/autoattach.go`, `internal/platform/autoattach_windows.go` (only after the decision), `internal/platform/autoattach_other.go`, `internal/platform/autoattach_test.go`

**Why / what** — A permanently attached VHDX still disappears on reboot. §6.4 requires auto-attach but leaves the mechanism open. This task ships the `AutoAttacher` seam (Contracts §6.4) and a no-op implementation that returns a clear "not configured" error, so S0-T18 can call it and report honestly. The real mechanism waits for the decision.

**Steps**
1. Before the decision: `autoattach.go` with `NewAutoAttacher() AutoAttacher` returning an implementation whose `Enable` returns `ErrAutoAttachUndecided` (exported sentinel, message names SPEC-GAPS `U-PLAT-5`). S0-T18 reports it as a warning: "auto-attach not configured; run `cowflow devdrive attach` after reboot".
2. After the decision (`AG-LEAD` updates this task): implement the chosen mechanism in `autoattach_windows.go` (argv-only invocations; the stored command is the absolute path of `cowflow.exe` plus `devdrive attach`); `Disable` removes it; both idempotent.
3. `autoattach_other.go`: `ErrUnsupported`.

**Tests**
- `TestAutoAttacher_Undecided` — `Enable` returns `ErrAutoAttachUndecided`.
- After the decision: idempotency (`Enable` twice), `Disable` after `Enable`, real registration gated by admin + `COWFLOW_TEST_DEVDRIVE=1`.

**Acceptance**
- [ ] Seam merged; no mechanism chosen in code before the decision.
- [ ] `init --devdrive` output tells the user auto-attach is pending (S0-T18).
- [ ] After the decision: implementation merged with `AG-SEC` approval, or the task is carried to Sprint 1 with `AG-LEAD`'s note.

**Pitfalls**
- Registering anything that runs as SYSTEM with a user-writable binary path is a privilege-escalation risk — flag it to `AG-SEC` when the decision lands.
- Do not make the exit criterion depend on this task: §19 Phase 0 output requires "creates and mounts", not auto-attach.

---

### S0-T18 — `cowflow init --devdrive` and `cowflow devdrive create|attach|detach|status`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-CLI` | `AG-LEAD`, `AG-API`, `AG-SEC` | `S0-E6` | S0-T02, S0-T05, S0-T16 (fake from S0-T22 until merged), S0-T22 | `feat/cli-init-devdrive` | yes |

**Skills:** `cli-and-config`, `windows-platform`, `security-review`, `gitflow`   **Spec:** §6.4 (all 7 steps), §14 (`init [--devdrive 50GB]`, `devdrive create|attach|detach|status`), §22 `[devdrive]`, §1.2 ("NTFS falls back to worktree with an explicit warning"), §23 risks ("Dev Drive unavailable (Win10, no admin) → explicit worktree fallback")
**Files:** `internal/cli/init.go`, `internal/cli/devdrive.go`, `internal/cli/size.go`, `internal/cli/devdrive_test.go`, `internal/cli/size_test.go`, `CHANGELOG.md`

**Why / what** — The user-facing half of the sprint's main output. `init --devdrive` drives `platform.DevDrive`, elevates once, writes config, and reports exactly what happened. In this sprint `init` without `--devdrive` stays a stub (`.mcp.json`, lead token, `--provider`, `--conventions` belong to `S3-E3`).

**Steps**
1. `size.go`: `parseSize("50GB") (uint64, error)` — Guidance: accept `^\d+\s*(GB|TB)$` case-insensitive with binary multiples (as PowerShell's `50GB` literal), reject everything else with a usage error (exit `1`).
2. `init.go` flow for `--devdrive <size> [--path] [--letter]`:
   1. Build `DevDriveSpec`: `--path` canonicalized with `platform.Canonical`; `--letter X` → `Mount: "X:"` (validate `^[D-Za-z]$`). Defaults when flags are omitted are **`U-PLAT-9`** — until decided, both flags are required and the error says so.
   2. `deps.Host.OS()`; non-Windows → `platform.ErrUnsupported` with the message "Dev Drive requires Windows 11; COWFlow will use git_worktree" (the explicit fallback, §1.2/§23). Build too old → same explicit message.
   3. Not elevated → `deps.Elevate(ctx, os.Args[1:])` (S0-T15 `RunElevated`, same working directory). Print `Requesting administrator rights (one UAC prompt)…` first. UAC declined → error (exit code per `U-API-12`). The elevated child performs steps 4–7; the parent then runs `Status` and prints the result. The elevated child never elevates again (it is already elevated; if not, it fails).
   4. `deps.DevDrive.Provision(ctx, spec)`.
   5. Register (§6.4 step 5): write `[devdrive]` (`enabled = true`, `path`, `mount`, `size_gb`, `auto_attach`) to the config file — location and whether SQLite also records it are **`U-PLAT-10`** / **`U-API-1`** (config location); until decided the path comes from `deps.ConfigPath()`. Call `AutoAttacher.Enable`; on `ErrAutoAttachUndecided` print a warning (S0-T17).
   6. Write `workspace.root` and `cache.*` inside the volume (§6.4 step 6). The sub-directory layout (e.g. §22's `X:/ws`, `X:/cache/pnpm`, …) is **pending** **`U-API-11`** (§22 defaults vs examples), **C7** (`golden_root`) and **C13** (extra cache keys): implement as one table `devdriveLayout(mount) map[string]string` filled after the decision; create the directories.
   7. Suggest the Defender exclusion (§6.4 step 7): print `Suggested (run as administrator): Add-MpPreference -ExclusionPath 'X:\'` — printing only; whether `init` must configure it is **C14**.
   8. Output: human summary or `--json` `{"devdrive": {...DevDriveStatus...}, "config_written": "...", "warnings": [...]}`.
3. `devdrive.go`: `create` = provision from `[devdrive]` config values (proposal under `U-PLAT-9`), `attach` = `DevDrive.Attach`, `detach` = `DevDrive.Detach`, `status` = `DevDrive.Status` (no elevation needed for `status`). All support `--json`.
4. Error mapping: `ErrAlreadyExists` → exit `3` (conflict); `ErrNotElevated`/`ErrOSTooOld`/`ErrUnsupported` → per `U-API-12`; validation → `1`.
5. Never touch NTFS paths as a silent fallback: if provisioning fails, config is **not** written (§6.4 "never silently creates on NTFS").

**Tests** — `devdrive_test.go` with `harness.SimDevDrive` and `harness.FakeHost` (S0-T22), config path in `t.TempDir()`:
- `TestInitDevDrive_HappyPath` — sim provisions; config contains `[devdrive] enabled = true` and the mount; output mentions the Defender suggestion and the auto-attach warning.
- `TestInitDevDrive_ElevatesOnce` — `FakeHost{Elevated: false}` → `Elevate` called exactly once with the original args; elevated child path never calls it.
- `TestInitDevDrive_ProvisionFails_NoConfigWritten` — sim fails at `format` → config file unchanged, non-zero exit.
- `TestInitDevDrive_NotWindows` and `_OldBuild` — explicit fallback message, no provisioning call.
- `TestInitDevDrive_AlreadyExists` — exit `3`.
- `TestDevDriveStatus_JSON` — valid JSON with `attached`, `mount`, `volume.filesystem`.
- `TestParseSize` — `50GB`, `1tb`, `50 GB` ok; `50`, `50MB`, `-1GB`, `GB` fail.

**Acceptance**
- [ ] `cowflow init --devdrive 50GB --path C:\cf\cowflow.vhdx --letter X` provisions and mounts a Dev Drive on Windows 11 (demo step 7).
- [ ] Exactly one UAC prompt; no elevation loop.
- [ ] Config written only after a successful provision; previous config kept as `.bak`.
- [ ] Defender exclusion suggested, not applied; auto-attach status reported honestly.
- [ ] Every gap-dependent default is a clearly marked placeholder, not a decision.
- [ ] `CHANGELOG.md` `[Unreleased]` updated. `AG-SEC` approved.

**Pitfalls**
- The elevated child has its own console window that closes on exit — the parent must print the final status itself.
- Passing `os.Args[0]` relative to the parent's working directory to the elevated process: use `os.Executable()`.
- Writing config before provisioning finishes leaves a config that points at a disk that does not exist.

---

### S0-T19 — Doctor probes: Dev Drive state and ProjFS availability

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-PLAT` | `AG-LEAD`, `AG-WS`, `AG-SEC` | `S0-E7` | S0-T14 | `feat/platform-doctor-probes` | yes |

**Skills:** `windows-platform`, `security-review`, `gitflow`   **Spec:** §7 ("Dev Drive present and mounted"; "ProjFS: feature enabled when `projfs` is configured"), §6.5 (`Client-ProjFS`, `ProjectedFSLib.dll`)
**Files:** `internal/platform/devvolume_windows.go`, `internal/platform/projfs_probe_windows.go`, `internal/platform/projfs_probe_other.go`, `internal/platform/devvolume_windows_test.go`

**Why / what** — Two facts `doctor` needs that `Volume()` does not give yet: "is this ReFS volume actually a Dev Drive?" and "is ProjFS installed?". Both must work **without admin** (`doctor` runs as a normal user).

**Steps**
1. Dev Drive flag: open the volume root (`X:\`) with `windows.CreateFile(..., 0, FILE_SHARE_READ|FILE_SHARE_WRITE, nil, OPEN_EXISTING, FILE_FLAG_BACKUP_SEMANTICS, 0)` and call `DeviceIoControl` with `FSCTL_QUERY_PERSISTENT_VOLUME_STATE` (`0x0009023C`), input/output `FILE_FS_PERSISTENT_VOLUME_INFORMATION{VolumeFlags, FlagMask, Version: 1, Reserved}` with `FlagMask = PERSISTENT_VOLUME_STATE_DEV_VOLUME` (`0x00002000`). Verify both constants against the Windows SDK `winioctl.h` before merging and cite the SDK version in a comment. Set `VolumeInfo.DevDrive` in `Volume()`.
2. ProjFS: `windows.NewLazySystemDLL("ProjectedFSLib.dll").Load()` succeeds ⇔ the `Client-ProjFS` feature is enabled (the DLL ships with the feature). Guidance: this avoids `Get-WindowsOptionalFeature`, which needs admin. `projfs_probe_other.go` → `false, nil`.
3. Defender exclusion probe: **not implemented** until C14 is decided (reading `ExclusionPath` needs admin anyway).

**Tests**
- `TestDevVolume_SystemDrive` — `C:\` → `DevDrive == false`.
- `TestDevVolume_DevDrive` — gated by `COWFLOW_TEST_REFS_PATH` pointing at a Dev Drive → `true`.
- `TestProjFSAvailable` — returns without error on Windows; value logged, not asserted (machines differ).
- `TestPersistentVolumeInfoLayout` — `unsafe.Sizeof == 16`.

**Acceptance**
- [ ] Both probes work as a non-admin user.
- [ ] Constants cited from the SDK.
- [ ] No Defender probe before C14.

**Pitfalls**
- Opening `\\.\X:` (the raw volume) needs admin; opening the root directory with backup semantics does not.
- A ReFS volume is not necessarily a Dev Drive; report both facts separately.

---

### S0-T20 — `cowflow doctor`

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-CLI` | `AG-LEAD`, `AG-API`, `AG-PLAT` | `S0-E7` | S0-T02, S0-T05, S0-T10, S0-T19, S0-T22 | `feat/cli-doctor` | no |

**Skills:** `cli-and-config`, `windows-platform`, `gitflow`   **Spec:** §7 (all bullets "Verified by `cowflow doctor`"), §3.1 (`git >= 2.40`), §6.4 ("No Dev Drive: `doctor` explains and offers to create one"), §2.2 p10, §23 risks
**Files:** `internal/cli/doctor.go`, `internal/cli/doctor_checks.go`, `internal/cli/doctor_test.go`, `CHANGELOG.md`

**Why / what** — The machine's honest self-report and the second half of the sprint exit criterion: "`doctor` reports ReFS/COW correctly". It composes facts from `platform.Host` and `internal/git`; it never changes anything.

**Steps**
1. Checks (`CheckResult`, Contracts §6.8). Names and statuses are Guidance; each has a `fix` command when one exists:

   | Check | Source | Rule |
   |---|---|---|
   | `git.version` | `git.CheckVersion` | fail if missing or < 2.40 (§3.1) |
   | `os.version` | `Host.OS` | Windows build < 22621 → warn "Dev Drive unavailable; git_worktree fallback" (§23 risk) |
   | `devdrive.mounted` | `DevDrive.Status` with `[devdrive]` config | configured but not mounted → fail, fix `cowflow devdrive attach` (never suggest NTFS, §6.4/§18); not configured → warn, fix `cowflow init --devdrive 50GB` ("offers to create one", §6.4) |
   | `fs.cow` | `Host.Volume(workspace.root or cwd)` | `ReFS` + `BlockClone` (+ `DevDrive`) → ok "copy-on-write: block clone"; `NTFS` → warn "NTFS: no COW (git_worktree fallback)" (§7, §1.2); other → report as-is |
   | `projfs.feature` | `Host.ProjFSAvailable` | only when `workspace.provider = "projfs"` or `workspace.projfs.enabled = true` (§7) |
   | `git.longpaths` | `git.ConfigGet(core.longpaths)` | Windows only; not `true` → warn, fix `git config core.longpaths true` |
   | `git.autocrlf` | `git.ConfigGet(core.autocrlf)` | not `false` → warn, fix `git config core.autocrlf false` |
   | `git.gitattributes` | read `<repo>/.gitattributes` | when `git.require_gitattributes`: a line `* text=auto eol=lf` must exist |
   | `defender.exclusion` | — | **not added until C14 is decided** |

2. Repository detection: `git rev-parse --show-toplevel` from the working directory; outside a repo the `git.*` repo checks are `skip` with detail "not a Git repository".
3. Config: `deps.ConfigPath()`; missing config → use `config.Default()` and add a `warn` check `config.found` (Guidance).
4. Output: human table (`CHECK  STATUS  DETAIL` + indented `fix:` lines) or `--json` `{"checks": [...], "ok": bool}`.
5. Exit code when any check is `fail`: **`U-API-12`** — use the `exitDoctorFailed` variable set at kickoff; `warn` never fails the command.
6. Each check runs with a per-check timeout (Guidance: 10 s) and independently — one failing probe must not hide the others.

**Tests** — `doctor_test.go`, all hermetic via `harness.FakeHost`, `harness.SimDevDrive`, `harness.NewRepo`:
- `TestDoctor_NTFSNoCOW` — volume `NTFS`, no block clone → `fs.cow` = warn, detail contains `no COW`.
- `TestDoctor_ReFSDevDrive` — `ReFS`, block clone, Dev Drive, mounted → `fs.cow` ok, `devdrive.mounted` ok.
- `TestDoctor_ConfiguredButUnmounted` — fail with fix `cowflow devdrive attach`; detail never suggests NTFS.
- `TestDoctor_NoDevDriveOffersInit` — fix contains `cowflow init --devdrive`.
- `TestDoctor_GitTooOld` — `FaultyRunner` answers `git version 2.39.0` → fail.
- `TestDoctor_RepoSettings` — longpaths/autocrlf/gitattributes ok and missing variants.
- `TestDoctor_ProjFSOnlyWhenConfigured` — check absent unless configured.
- `TestDoctor_JSON` — schema round-trip; `ok == false` when any fail.

**Acceptance**
- [ ] Every §7 bullet that applies in Phase 0 has a check; plus `git >= 2.40`.
- [ ] ReFS with block clone reported as COW; NTFS reported as no COW; nothing claimed beyond what the probes report.
- [ ] No check modifies the system.
- [ ] Defender check absent until C14; exit code on failure taken from the `U-API-12` decision.
- [ ] `CHANGELOG.md` updated.

**Pitfalls**
- Probing `cwd` instead of `workspace.root` reports the wrong volume once a Dev Drive exists.
- Treating "not a repo" as a failure makes `doctor` useless before `init`.
- Printing the Defender `fix` as if required decides C14.

---

### S0-T21 — Hermetic harness: fake clock, test repos, Git fault injection

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD`, `AG-SEC` | `S0-E8` | S0-T06, S0-T09 | `test/harness-basic` | no |

**Skills:** `testing-harness`, `go-conventions`, `gitflow`   **Spec:** §18.1 ("fake clock, Git repository on tmpfs, fault injection into the Git subprocess (exit code, timeout, truncated output)"), §16 (testify, `-race`)
**Files:** `test/harness/doc.go`, `test/harness/clock.go`, `test/harness/repo.go`, `test/harness/gitfault.go`, `test/harness/clock_test.go`, `test/harness/repo_test.go`, `test/harness/gitfault_test.go`

**Why / what** — The first slice of the §18.1 harness, enough for Sprint 0 tests and ready for Sprint 1–2. The remaining §18.1 pieces (disk full, crash between outbox write and delivery, simulated ProjFS) land with the epics that need them (`S2-E2`, `S6-E4`, `S6-E5`).

**Steps**
1. `clock.go` — `harness.Clock` implements `domain.Clock`:
   ```go
   type Clock struct {
   	mu      sync.Mutex
   	now     time.Time
   	waiters []waiter // fire when now >= at
   }

   func NewClock(start time.Time) *Clock
   func (c *Clock) Now() time.Time
   func (c *Clock) After(d time.Duration) <-chan time.Time
   func (c *Clock) NewTicker(d time.Duration) domain.Ticker
   func (c *Clock) Advance(d time.Duration) // fires due waiters in order, outside the lock
   ```
2. `repo.go` — `NewRepo(t testing.TB, opts ...RepoOption) *Repo` with `Dir`, `Env []string`, `Runner git.Runner`, `Commit(t, msg string, files map[string]string) string`:
   - Location: Linux → a directory under `/dev/shm` when writable, else `t.TempDir()`; Windows/macOS → `t.TempDir()` (SPEC-GAPS `U-PLAT-8`, Git repo on tmpfs on Windows/macOS — `WithDir` option lets the decision plug in).
   - Hermetic env: `GIT_CONFIG_GLOBAL=<empty file in t.TempDir()>`, `GIT_CONFIG_NOSYSTEM=1`, `GIT_AUTHOR_NAME/EMAIL`, `GIT_COMMITTER_NAME/EMAIL`, `GIT_AUTHOR_DATE`/`GIT_COMMITTER_DATE` fixed (deterministic SHAs), `HOME`/`USERPROFILE` pointed at the temp dir.
   - `git init -b develop` (Guidance: matches `project.default_base` default) then `.gitattributes` with `* text=auto eol=lf` and `core.autocrlf=false` unless options say otherwise.
3. `gitfault.go`:
   ```go
   type GitFault struct {
   	Match      []string // argv prefix, e.g. {"check-ref-format"}
   	ExitCode   int      // > 0: fail with this code
   	Timeout    bool     // block until ctx is done, then return ctx error
   	TruncateAt int      // > 0: cut stdout to this many bytes
   	Stdout     string   // canned output instead of running git
   }

   type FaultyRunner struct {
   	Inner  git.Runner
   	mu     sync.Mutex
   	Faults []GitFault
   	Calls  [][]string // recorded argv (RecordingRunner behavior, for "no Git invoked" tests later)
   }

   func (f *FaultyRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error)
   ```
   Faults return a `*git.Error` shaped exactly like the real runner's, so production code cannot tell the difference.

**Tests**
- `TestClock_AdvanceFiresInOrder`, `TestClock_Ticker`, `TestClock_Concurrent` (`-race`).
- `TestRepo_Deterministic` — two repos with the same commits have identical SHAs.
- `TestRepo_IgnoresUserConfig` — set `t.Setenv("GIT_CONFIG_GLOBAL", <file with user.name=evil>)` before `NewRepo` → commits authored by the harness identity.
- `TestFaultyRunner_ExitCode`, `_Timeout`, `_Truncate`, `_PassThrough`, `_RecordsCalls`.

**Acceptance**
- [ ] No test using the harness touches the developer's Git config, network or wall clock.
- [ ] Fault injection covers exit code, timeout and truncated output (§18.1).
- [ ] Works on all three OSes.

**Pitfalls**
- `GIT_CONFIG_NOSYSTEM` alone does not stop `~/.gitconfig`; set `GIT_CONFIG_GLOBAL` too.
- Firing clock waiters while holding the mutex deadlocks callers that call `Now()` in the handler.
- Windows file locks: close everything before `t.TempDir()` cleanup or the test fails on cleanup.

---

### S0-T22 — Harness: simulated VHDX and fake host

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD`, `AG-PLAT` | `S0-E8` | S0-T14 (contract file) | `test/harness-simulated-vhdx` | no |

**Skills:** `testing-harness`, `windows-platform`, `gitflow`   **Spec:** §18.1 ("simulated VHDX (interface)"), §6.4 "Failures", §18 ("VHDX not mounted → `pending_mount`; … never silently falls back to NTFS")
**Files:** `test/harness/vhdx.go`, `test/harness/host.go`, `test/harness/vhdx_test.go`

**Why / what** — `init --devdrive` and `doctor` must be testable on every OS and in CI, where no Windows 11 admin machine exists. The simulation implements the real interfaces from Contracts §6.4 and can fail at any step.

**Steps**
1. `host.go`:
   ```go
   type FakeHost struct {
   	mu      sync.Mutex
   	OSInfo  platform.OSInfo
   	Volumes map[string]platform.VolumeInfo // key: mount or path prefix
   	ProjFS  bool
   }

   func (h *FakeHost) OS(context.Context) (platform.OSInfo, error)
   func (h *FakeHost) Volume(ctx context.Context, path string) (platform.VolumeInfo, error) // longest-prefix match
   func (h *FakeHost) ProjFSAvailable(context.Context) (bool, error)
   ```
2. `vhdx.go`:
   ```go
   type SimStep string

   const (
   	StepCreate SimStep = "create"
   	StepAttach SimStep = "attach"
   	StepFormat SimStep = "format"
   	StepMount  SimStep = "mount"
   )

   type SimDevDrive struct {
   	Host   *FakeHost
   	FailAt SimStep // inject a failure at this step
   	disks  map[string]*simDisk
   	Calls  []string
   }
   ```
   `Provision` walks the steps, honors `FailAt` with compensation (removes the sim disk), and on success registers `VolumeInfo{Filesystem: "ReFS", BlockClone: true, DevDrive: true}` in `Host.Volumes[mount]`. `Detach` removes the volume (simulates reboot/unmount); `Attach` restores it. `SimulateReboot()` detaches every disk.
3. A compile-time check: `var _ platform.DevDrive = (*SimDevDrive)(nil)` and `var _ platform.Host = (*FakeHost)(nil)`.

**Tests**
- `TestSimDevDrive_ProvisionThenStatus`, `_FailAtFormatCompensates`, `_RebootUnmounts` (status `Attached == false`, `Volume == nil` — never an NTFS volume in its place).

**Acceptance**
- [ ] Implements the exact platform interfaces; breaks the build if the contract changes.
- [ ] Every provisioning step can be failed on demand.
- [ ] Used by S0-T18 and S0-T20 tests.

**Pitfalls**
- A simulation that "helpfully" returns an NTFS volume after unmount would hide the §18 rule.
- Keep it deterministic: no goroutines, no real time.

---

### S0-T23 — Re-enable CI on three platforms

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` | `AG-LEAD`, `AG-SEC` | `S0-E8` | S0-T01, S0-T21 | `ci/enable-workflows` | yes |

**Skills:** `testing-harness`, `gitflow`, `security-review`   **Spec:** §3.1 ("CI on all three platforms"), §19 Phase 0, §20.14 (partial), [GITFLOW.md §5](../GITFLOW.md#5-repository-layout-for-governance)
**Files:** `.github/workflows/ci.yml`

**Why / what** — CI was disabled while the repo had no code (`workflow_dispatch` only). Now that real packages and tests exist, every PR must be checked on Windows, Linux and macOS. `release.yml` stays disabled until the first release (not this sprint).

**Steps**
1. In `ci.yml`, uncomment the triggers and remove the "Disabled until there is code" comment:
   ```yaml
   on:
     workflow_dispatch:
     push:
       branches: [master, develop]
     pull_request:
       branches: [master, develop]
   ```
2. Keep job names unchanged (`lint`, `test (<os>)`, `govulncheck`, `gitleaks`, `dco`) — they are the future required checks.
3. Add to the `lint` job, after `go vet`, a cross-build step (no new job, so no new check name):
   ```yaml
         - name: cross-build (CGO_ENABLED=0)
           run: |
             for os in windows linux darwin; do
               for arch in amd64 arm64; do
                 echo "== $os/$arch"
                 CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build ./...
                 GOOS=$os GOARCH=$arch go vet ./...
               done
             done
   ```
4. In the `test` job add `-count=1` to `go test -race -cover ./...` (no cached results for platform tests). The Windows runner already has a C toolchain for `-race`.
5. Keep every action pinned by commit SHA (GITFLOW.md §4); no new third-party actions.
6. Open the PR; confirm all jobs run on the PR itself and are green. After merge, confirm the `push` run on `develop` is green and record the run URL in the PR.

**Tests** — the workflow run is the test: all of `lint`, `test (ubuntu-latest)`, `test (windows-latest)`, `test (macos-latest)`, `govulncheck`, `gitleaks`, `dco` green on the PR and on `develop`.

**Acceptance**
- [ ] Triggers re-enabled for `push` and `pull_request` on `master`/`develop`; `release.yml` untouched.
- [ ] Cross-build of 6 targets with `CGO_ENABLED=0` passes.
- [ ] AC 16 test (`TestProcessTree_CancelKillsTree`) runs on `windows-latest`.
- [ ] Green run on `develop` linked in the PR. `AG-SEC` approved (pins, permissions `contents: read`).

**Pitfalls**
- Merging this before tests exist makes every docs PR wait for a slow matrix — that is why it depends on S0-T01 and S0-T21.
- Adding required status checks in this PR — do not; that is S0-T24, after a green run exists.
- `macos-latest` may have an older Git first on `PATH`; tests that need ≥ 2.40 must skip with a reason, not fail.

---

### S0-T24 — Configure required status checks

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` (doc) · maintainer applies settings | `AG-LEAD`, `AG-SEC` | `S0-E8` | S0-T23 merged and a green `push` run on `develop` | `docs/gitflow-ci-enabled` | yes |

**Skills:** `gitflow`, `testing-harness`   **Spec:** [GITFLOW.md §4–§6](../GITFLOW.md#4-rules-that-make-the-flow-auditable)
**Files:** `docs/GITFLOW.md` (remove the "Current state: workflows disabled" note; record the solo-maintainer choice)

**Why / what** — Branch protection must require the CI checks so nothing unverified reaches `develop`/`master` — but only once the checks actually run (GITFLOW.md §5). This needs repository admin rights, so the maintainer runs it; `AG-QA` prepares and documents.

**Steps**
1. Verify the check names from the latest `develop` run: `gh run list --branch develop --workflow ci.yml --limit 1` then `gh run view <id> --json jobs --jq '.jobs[].name'` → expect `lint`, `test (ubuntu-latest)`, `test (windows-latest)`, `test (macos-latest)`, `govulncheck`, `gitleaks` (`dco` runs only on PRs: confirm its name on a PR run).
2. Maintainer runs the branch-protection script from GITFLOW.md §6 for `master` and `develop` exactly as written (contexts list must match step 1). Solo maintainer: choose approval count `0` or add a second maintainer, and write the choice into GITFLOW.md §4 as that document asks.
3. Verify: `gh api repos/{owner}/{repo}/branches/develop/protection --jq '.required_status_checks.contexts'`.
4. Docs PR: update GITFLOW.md §5 to say CI is enabled (keep the note that `release.yml` stays manual until the first release).

**Tests** — open a throw-away PR with a failing test commit (then close it): merge button blocked until checks pass.

**Acceptance**
- [ ] Required checks configured on `master` and `develop`, names verified against a real run.
- [ ] GITFLOW.md updated in the same sprint.

**Pitfalls**
- A typo in a context name blocks every PR forever — copy names from the API, not from memory.
- Requiring `dco` on `push` events: it only runs on `pull_request`; that is fine for branch protection (PRs only).

---

### S0-T25 — Sprint demo, evidence and changelog

| Owner | Reviewers | Epic | Depends on | Branch | Security |
|---|---|---|---|---|---|
| `AG-QA` (demo run with `AG-LEAD`) | `AG-LEAD`, `AG-SEC` | `S0-E8` | all other S0 tasks (S0-T17 may be carried over) | `docs/sprint-0-demo` | no |

**Skills:** `testing-harness`, `gitflow`   **Spec:** §19 Phase 0 output, §20.16, §20.21 (first half), [README §4](README.md#4-how-a-sprint-runs)
**Files:** `CHANGELOG.md`, `docs/sprints/sprint-0.md` (only the evidence links in §8, if the team chooses to record them here), skill files touched by the retro

**Why / what** — Prove every exit criterion on a clean machine and leave evidence that the next sprint can trust.

**Steps**
1. Run §8 (Demo script) on a clean Windows 11 22H2+ machine with admin rights; save the console transcript (`Start-Transcript`).
2. Attach to the PR: transcript, `cowflow doctor --json` before/after, `go test` output for AC 16, the CI run URL.
3. `CHANGELOG.md` `[Unreleased]`: consolidate entries (CLI tree, config, store, Git wrapper, platform layer, Dev Drive, doctor, CI).
4. Retro (README §4 step 5): move learnings into skills; e.g. correct the `cli-and-config` pitfall that says the bootstrap `main.go` returns `2` for usage (it returns `1`); add every new gap that was not decided to `SPEC-GAPS.md`.
5. Detach and delete the demo VHDX (§8 cleanup).

**Tests** — the demo itself.

**Acceptance**
- [ ] Every checkbox in §2 ticked with linked evidence.
- [ ] `CHANGELOG.md` updated; retro changes merged.

**Pitfalls**
- Running the demo on a developer's daily machine (existing `X:`, existing config) hides first-run bugs — use a clean VM or a fresh user profile.

---

## 8. Demo script

Clean Windows 11 22H2+ machine (or VM), Defender on, user is a local administrator but the terminal is **not** elevated. The steps assume the kickoff decisions on config location (`U-API-1`; `<config.toml>` below) and on `U-API-12`/`U-PLAT-9`; outputs are illustrative (wording may differ, facts must match).

```powershell
# 1. Prerequisites (§3.1), then:
git clone https://github.com/deverity/COWFlow.git C:\src\COWFlow; cd C:\src\COWFlow; git switch develop

# 2. Build a static binary
$env:CGO_ENABLED = "0"; go build -o bin\cowflow.exe ./cmd/cowflow; Remove-Item Env:CGO_ENABLED
.\bin\cowflow.exe --version
#   cowflow dev (commit none, built unknown)

# 3. Command tree (§14) and exit codes
.\bin\cowflow.exe --help                       # lists init, mcp, doctor, daemon, devdrive, task, run, ... (no "completion")
.\bin\cowflow.exe nope; $LASTEXITCODE          # cowflow: unknown command "nope" ... → 1
.\bin\cowflow.exe task show; $LASTEXITCODE     # accepts 1 arg(s), received 0 → 1
.\bin\cowflow.exe task list; $LASTEXITCODE     # cowflow: not implemented yet → <U-API-12 value>

# 4. A managed test repository
mkdir C:\demo\repo; cd C:\demo\repo; git init -b develop
Set-Content .gitattributes "* text=auto eol=lf"; git config core.autocrlf false; git config core.longpaths true

# 5. Config validation
Copy-Item C:\src\COWFlow\config.example.toml <config.toml>
# (introduce a typo, e.g. max_agent = 20, run any command that loads config → exit 1 and "unknown key scheduler.max_agent"; then revert)

# 6. doctor BEFORE the Dev Drive
C:\src\COWFlow\bin\cowflow.exe doctor
#   CHECK              STATUS  DETAIL
#   git.version        ok      git 2.4x.y.windows.z (>= 2.40)
#   os.version         ok      Windows build 226xx
#   devdrive.mounted   warn    no Dev Drive configured
#                              fix: cowflow init --devdrive 50GB
#   fs.cow             warn    C:\ is NTFS: no copy-on-write (git_worktree fallback)
#   git.longpaths      ok      core.longpaths=true
#   git.autocrlf       ok      core.autocrlf=false
#   git.gitattributes  ok      * text=auto eol=lf
C:\src\COWFlow\bin\cowflow.exe doctor --json | ConvertFrom-Json | Select-Object -ExpandProperty checks | Format-Table

# 7. Provision the Dev Drive (one UAC prompt)
C:\src\COWFlow\bin\cowflow.exe init --devdrive 50GB --path C:\cf\cowflow.vhdx --letter X
#   Requesting administrator rights (one UAC prompt)...
#   Dev Drive ready: X:\ (ReFS, Dev Drive, block clone) backed by C:\cf\cowflow.vhdx (dynamic, max 50 GB)
#   Config updated: <config.toml> (previous kept as <config.toml>.bak)
#   warning: auto-attach at boot not configured yet; after a reboot run: cowflow devdrive attach
#   Suggested (run as administrator): Add-MpPreference -ExclusionPath 'X:\'
$LASTEXITCODE                                   # 0

# 8. Independent verification with Windows tools
Get-Volume -DriveLetter X | Format-List FileSystem, Size, HealthStatus   # FileSystem : ReFS
fsutil devdrv query X:                          # reports X: as a (trusted) developer volume
(Get-Item C:\cf\cowflow.vhdx).Length / 1GB      # far below 50 (dynamic)

# 9. Status
C:\src\COWFlow\bin\cowflow.exe devdrive status --json
#   {"vhdx_path":"C:\\cf\\cowflow.vhdx","exists":true,"attached":true,"mount":"X:",
#    "volume":{"path":"X:\\","filesystem":"ReFS","block_clone":true,"dev_drive":true}}

# 10. doctor AFTER: ReFS/COW reported correctly
C:\src\COWFlow\bin\cowflow.exe doctor
#   devdrive.mounted   ok      X: mounted (C:\cf\cowflow.vhdx)
#   fs.cow             ok      X:\ is ReFS Dev Drive: copy-on-write via block clone

# 11. Unmounted VHDX is never replaced by NTFS
C:\src\COWFlow\bin\cowflow.exe devdrive detach  # one UAC prompt
C:\src\COWFlow\bin\cowflow.exe doctor           # devdrive.mounted  fail  configured Dev Drive X: is not mounted
                                                #                   fix: cowflow devdrive attach
$LASTEXITCODE                                   # <U-API-12 value for doctor failure>
C:\src\COWFlow\bin\cowflow.exe devdrive attach  # X: back; doctor → ok again

# 12. Re-running init is refused, not destructive
C:\src\COWFlow\bin\cowflow.exe init --devdrive 50GB --path C:\cf\cowflow.vhdx --letter X; $LASTEXITCODE   # already exists → 3

# 13. AC 16 — cancellation kills the whole tree
cd C:\src\COWFlow
go test -run ProcessTree -count=1 -v ./internal/platform/
#   --- PASS: TestProcessTree_CancelKillsTree (…s)
#   --- PASS: TestProcessTree_GracefulExit (…s)
#   --- PASS: TestProcessTree_KillOnClose (…s)

# 14. Full local gate (same as CI)
go test -race -count=1 ./... ; golangci-lint run ; govulncheck ./...
gh run list --branch develop --workflow ci.yml --limit 1     # completed  success

# 15. Cleanup (elevated)
C:\src\COWFlow\bin\cowflow.exe devdrive detach; Remove-Item C:\cf\cowflow.vhdx
```

Exit criteria mapping: steps 7–9 → "init creates and mounts a Dev Drive" and §20.21 (first half); steps 6, 10, 11 → "doctor reports ReFS/COW correctly"; step 13 → §20.16; step 14 → CI on 3 platforms; steps 3, 5 → CLI skeleton and config. Store, Git wrapper and harness are covered by step 14's test run.

---

## 9. Definition of Done

- [ ] All tasks S0-T01…S0-T25 merged to `develop` via squash, each with a Conventional Commit title, signed and signed-off commits, and the reviews required by README §1 (S0-T17 may be carried to Sprint 1 only with `AG-LEAD`'s written note if its gap is still open).
- [ ] Every §2 exit criterion ticked with evidence (transcript, CI run URL) in the sprint-closing PR.
- [ ] `go build ./...` with `CGO_ENABLED=0` for windows/linux/darwin × amd64/arm64; `go test -race ./...` green on all three CI platforms.
- [ ] `golangci-lint run`, `go vet ./...` clean; `go mod tidy` is a no-op; `govulncheck ./...` clean.
- [ ] Only fixed-stack dependencies in `go.mod` (cobra, BurntSushi/toml, modernc.org/sqlite, oklog/ulid, golang.org/x/sys, testify, plus their transitive requirements).
- [ ] No shell-string subprocesses anywhere (`TestNoShellInvocations` green); every Git call through `git.Runner`.
- [ ] Every OS-specific file has a counterpart or `ErrUnsupported` stub.
- [ ] No SPEC-GAPS item decided in code; each pending point carries `PENDING <id>` and the kickoff decisions are recorded in SCOPE.md §23 with SPEC-GAPS.md updated; `U-API-12`, `U-API-13`, `U-PLAT-9`, `U-PLAT-10` decided or still open in SPEC-GAPS.md.
- [ ] CI triggers re-enabled; required status checks configured; GITFLOW.md §5 updated.
- [ ] `CHANGELOG.md` `[Unreleased]` describes the user-visible changes.
- [ ] Retro done: learnings moved into `docs/skill/`.

---

## 10. Risks and mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| `AG-PLAT` overloaded (7 tasks, owns the critical path) | Sprint exit slips | Critical items first (T14 → T15 → T16); CLI builds on the simulated VHDX from Day 5; `AG-WS` pairs on S0-T13; S0-T17 is allowed to carry over. |
| Kickoff gaps not decided on Day 1 (`U-API-1` config location, `U-DATA-2` ID prefixes, `U-API-12`, `U-API-13`, `U-PLAT-9`, `U-PLAT-10`) | Tasks merge with placeholders; demo cannot show final exit codes or paths | Every gap is behind a seam (`Deps.ConfigPath`, prefix table, `exit*` variables); `AG-LEAD` tracks them daily; demo notes the placeholder values. |
| No Windows 11 + admin machine in CI (GitHub runners are Windows Server) | Dev Drive code only tested manually | Simulated VHDX for all logic; gated real-device tests (`COWFLOW_TEST_DEVDRIVE=1`) run by `AG-PLAT` and in the demo; output attached to PRs. |
| Wrong struct layout in virtdisk/ioctl bindings | Crashes or `ERROR_INVALID_PARAMETER` on real machines | Layout tests with `unsafe.Offsetof`; constants cited from the Windows SDK; read go-winio's layouts for reference (not as a dependency). |
| PowerShell storage cmdlets unavailable or localized errors | Provisioning fails opaquely | Fixed script, `-EncodedCommand`, surface raw stderr, never parse it; virtdisk first so PowerShell only partitions/formats. |
| UAC: elevated child's output lost; elevation loops | Confusing UX, possible infinite prompts | Parent prints final `Status`; child never elevates; UAC decline mapped to an error. |
| `SQLITE_BUSY` under concurrent daemonless writers | Flaky store, data loss in later sprints | WAL + `busy_timeout` + `BEGIN IMMEDIATE` on every connection; 20-writer test under `-race`. |
| Junction escape after Go 1.23 `EvalSymlinks` change | Path-confinement bypass | `GetFinalPathNameByHandle` on Windows; junction test; `AG-SEC` review. |
| `-race` needs cgo/gcc on Windows dev machines | Developers skip race tests | Prerequisite install step (§3.1); CI runs `-race` on all three OSes regardless. |
| `GenerateConsoleCtrlEvent` fails without a console (CI, services) | AC 16 flaky if relying on graceful stop | Always follow with `TerminateJobObject` after the deadline; the test asserts tree death, not graceful exit. |
| macOS Apple Git < 2.40 on runners/laptops | Git tests fail on macOS | Tests needing ≥ 2.40 skip with a reason; prerequisites say `brew install git`. |
| Required status check names mistyped | Every PR blocked | Copy names from `gh run view --json jobs`; throw-away PR to verify (S0-T24). |
| go.mod/go.sum conflicts between parallel PRs adding dependencies | Merge friction | Rebase and `go mod tidy`; dependencies added by the first task of each owner (Days 1–3). |
| Defender / indexer slow down VHDX tests | Slow, flaky timings | Suggest the exclusion (printing only, C14); no timing assertions in Sprint 0 (performance targets start in Sprint 1). |
