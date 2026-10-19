---
name: windows-platform
description: Use when writing or reviewing OS-specific code in `internal/platform` or the `_windows/_linux/_darwin` files of `internal/workspace` — Dev Drive/VHDX ReFS provisioning, block clone (`FSCTL_DUPLICATE_EXTENTS_TO_FILE`, `FICLONE`, `clonefile`), ProjFS (`ProjectedFSLib.dll`), Job Objects and process-tree kill, the Windows service, long paths, Defender/indexing, and `cowflow doctor` platform checks.
---
# Windows-first Platform Layer

## When to use
- `cowflow init --devdrive`, `cowflow devdrive create|attach|detach|status`, `cowflow daemon install`, `cowflow doctor`.
- Any syscall/DLL binding, build-tagged file, path canonicalization, or process spawning/cancellation.
- Workspace-level behavior (selection, golden, sparse, lifecycle) is in `cow-workspaces`.

## Source of truth
`docs/SCOPE.md` wins on any conflict. Derived from:
- §2.2 Principles (8, 10) · §3.1 Included · §5.1 Planes (Platform Layer) · §5.3 Modes
- §6.3 Backends · §6.4 VHDX ReFS (Dev Drive) · §6.5 ProjFS · §6.7 Shared cache
- §7 Windows — platform requirements · §8.1 Git rules · §14 CLI (Setup)
- §16 Go stack · §16.1 Layout · §17 Security · §18 Resilience · §18.1 Hermetic harness
- §19 Phases 0, 4, 6 · §20 AC 13, 14, 16, 21, 22 · §22 `[devdrive]`, `[workspace.projfs]` · §23 Risks/Decisions/Open questions

## Rules
- `CGO_ENABLED=0`: all Windows APIs via `golang.org/x/sys/windows` or `windows.NewLazySystemDLL`; Unix via `golang.org/x/sys/unix` (§16, §23 decision 1).
- Windows 11 + Dev Drive VHDX ReFS is the baseline; `cow_clone` by default (§23 decision 9).
- `init --devdrive <size> [--path C:\cf\cowflow.vhdx] [--letter X]` MUST (§6.4): check Windows 11 22H2+ and admin (elevate once); create a dynamic VHDX via `virtdisk.dll` `CreateVirtualDisk` (fallback PowerShell `New-VHD`); `AttachVirtualDisk`; init GPT, partition, format ReFS with the Dev Drive flag (`Format-Volume -DevDrive`); mount at a letter or under `C:\cf\`; register in project state; configure auto-attach at boot (scheduled task or service); write `workspace.root`, `cache.*` inside the volume; suggest a Defender exclusion.
- No Dev Drive → `doctor` explains and offers to create one; Win10/no admin → explicit worktree fallback (§6.4, §23 risks).
- VHDX not mounted → `pending_mount`, auto-attach, retry; NEVER silently fall back to NTFS (§6.4, §18).
- `doctor` verifies (§7): Dev Drive present+mounted, ReFS confirmed, NTFS flagged no-COW; ProjFS feature (`Client-ProjFS`) enabled when `projfs` configured; `core.longpaths=true`; `.gitattributes` `* text=auto eol=lf` (`git.require_gitattributes`); `core.autocrlf=false` per worktree.
- Paths: `\\?\` above 240 chars; short root; canonicalize with `Clean + EvalSymlinks + prefix` check; paths confined (§7, §17).
- Processes (§7, §3.1): Windows — Job Object with `KILL_ON_JOB_CLOSE`; cancel = `CTRL_BREAK` → deadline → `TerminateJobObject`. Unix — `Setpgid` + `SIGTERM` + `SIGKILL`. Cancellation MUST kill the whole tree (AC 16).
- NEVER `cmd.exe` with a string; commands/gates are argv arrays; no concatenated shell (§7, §17). Git via `exec.Command` with `-C`, `GIT_TERMINAL_PROMPT=0`, timeout (§8.1).
- Handles: destroy failure → `pending_cleanup` with retry and diagnostics; Defender with volume exclusion (§7).
- Service: `cowflow daemon install` via `x/sys/windows/svc`; VHDX auto-attach happens before the daemon (§7). Unix: systemd / launchd (§3.1).
- ProjFS (§6.5): `PrjStartVirtualizing`, `PrjMarkDirectoryAsPlaceholder`, callbacks `GetDirectoryEnumeration`, `GetPlaceholderInfo`, `GetFileData`, `Notification`; backing store persistent `git cat-file --batch` + LRU (`blob_cache_mb`); callback timeouts; `PrjStopVirtualizing` on shutdown; reconciliation on startup; root on NTFS or ReFS; requires running daemon; never serve blobs outside the authorized `base_sha` (§17); callback timeout → error to the agent, not a hang (§18).
- Block clone mechanisms (§6.3): Windows `FSCTL_DUPLICATE_EXTENTS_TO_FILE` (ReFS/Dev Drive), Linux `FICLONE` (btrfs/XFS), macOS `clonefile` (APFS).
- Platform capability reporting must be truthful: actual filesystem + `CowKind` (§2.2 p10, AC 13).

## Implementation guide
Layout (§16.1): `internal/platform/` — `paths`, `process_*`, `vhdx_windows.go`, `fscaps_*`; `internal/workspace/` — `cow_windows.go`, `cow_linux.go`, `cow_darwin.go`, `projfs/` (host, callbacks, blobstore).
Guidance — file/tag conventions: rely on GOOS filename suffixes (`_windows.go`, `_linux.go`, `_darwin.go`) rather than explicit tags; add `//go:build !windows` stubs (e.g. `vhdx_other.go`, `projfs/host_other.go`) returning a sentinel `ErrUnsupported` so all packages compile on all 3 OSes; `process_windows.go` / `process_unix.go` (`//go:build unix`); `fscaps_windows.go` / `fscaps_linux.go` / `fscaps_darwin.go` (+ `fscaps_other.go` reporting `None`). The `container` backend's build-tag name is not specified by the spec.

Guidance — seams the harness needs (§18.1 "simulated VHDX (interface)", "simulated ProjFS (in-memory callbacks)"): a `VirtualDisk`-style interface (create/attach/detach/status/mount path) with a fake; ProjFS host split into pure callback logic (testable on any OS) and a thin `ProjectedFSLib.dll` binding.

**VHDX / Dev Drive** (`vhdx_windows.go`). Guidance:
- `virtdisk := windows.NewLazySystemDLL("virtdisk.dll")`; procs `CreateVirtualDisk`, `AttachVirtualDisk`, `DetachVirtualDisk`, `OpenVirtualDisk`, `GetVirtualDiskPhysicalPath`. Mirror `VIRTUAL_STORAGE_TYPE` (`DeviceId = VIRTUAL_STORAGE_TYPE_DEVICE_VHDX` (3), Microsoft vendor GUID) and versioned `CREATE_/ATTACH_VIRTUAL_DISK_PARAMETERS` structs with exact field alignment; check `Find()` before use and return a typed error so the PowerShell fallback can run.
- Attach with `ATTACH_VIRTUAL_DISK_FLAG_PERMANENT_LIFETIME` so the disk outlives the handle; it still detaches on reboot → auto-attach.
- Partition/format: `powershell.exe -NoProfile -NonInteractive -Command` with a fixed script and arguments passed as separate argv/escaped literals — never interpolate user input; spec names `New-VHD` and `Format-Volume -DevDrive`.
- Version gate: `windows.RtlGetVersion()` (build ≥ 22621 for 22H2). Admin: `windows.GetCurrentProcessToken().IsElevated()`; elevate once via `ShellExecute` `runas` of the same binary with an explicit subcommand.

**FS capabilities** (`fscaps_*`). Guidance: Windows `GetVolumeInformation` (FS name `ReFS`/`NTFS`, flag `FILE_SUPPORTS_BLOCK_REFCOUNTING`); Dev Drive state via `FSCTL_QUERY_PERSISTENT_VOLUME_STATE`. Linux `unix.Statfs` `Type` vs `BTRFS_SUPER_MAGIC`/`XFS_SUPER_MAGIC`, then confirm with a real probe clone (XFS needs `reflink=1`). macOS `unix.Statfs` `Fstypename == "apfs"`. Always confirm by cloning a probe file in the target root.

**Block clone** (`cow_<goos>.go`). Guidance:
- Windows: open source (`GENERIC_READ`) and create dest (`GENERIC_READ|GENERIC_WRITE`) with `windows.CreateFile` (prefix `\\?\` yourself — raw syscalls do not get `os`'s long-path fix-up); match sparse attr (`FSCTL_SET_SPARSE`) and integrity info (`FSCTL_GET/SET_INTEGRITY_INFORMATION`); set dest size (`SetFileInformationByHandle` EOF) first; `DeviceIoControl(dst, FSCTL_DUPLICATE_EXTENTS_TO_FILE /*0x98344*/, &DUPLICATE_EXTENTS_DATA{FileHandle: src, SourceFileOffset, TargetFileOffset, ByteCount})` in chunks < 4 GiB aligned to the volume cluster size (`GetDiskFreeSpace`), final chunk rounded up to a cluster; copy timestamps/attributes. Same volume only.
- Linux: `unix.IoctlFileClone(dstFd, srcFd)` (`FICLONE`); `EOPNOTSUPP`/`EXDEV`/`EINVAL` → unsupported sentinel.
- macOS: `unix.Clonefile(src, dst, unix.CLONE_NOFOLLOW)`; `ENOTSUP`/`EXDEV` → unsupported sentinel.

**ProjFS** (`internal/workspace/projfs`). Guidance: `NewLazySystemDLL("ProjectedFSLib.dll")`; also `PrjFillDirEntryBuffer`, `PrjWritePlaceholderInfo`, `PrjAllocateAlignedBuffer`/`PrjFreeAlignedBuffer`, `PrjWriteFileData`, `PrjCompleteCommand`. Build `PRJ_CALLBACKS` with `windows.NewCallback` once per process (callbacks are never freed and their number is limited); callbacks arrive on OS threads, so dispatch via an instance-context → Go-object registry, never pass Go pointers to C memory. Persist the virtualization instance GUID per root for `PrjMarkDirectoryAsPlaceholder` and restart. For timeouts, return `ERROR_IO_PENDING` and finish with `PrjCompleteCommand`, failing with an HRESULT at the deadline.

**Process trees** (`process_*`). Guidance:
- Windows: `windows.CreateJobObject`; `SetInformationJobObject(JobObjectExtendedLimitInformation)` with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`; spawn via `os/exec` with `SysProcAttr.CreationFlags = CREATE_NEW_PROCESS_GROUP | CREATE_SUSPENDED`, `AssignProcessToJobObject`, then resume (os/exec exposes no thread handle: enumerate threads via `CreateToolhelp32Snapshot` + `ResumeThread`, or `NtResumeProcess`) so no grandchild escapes the job. Cancel: `GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT, pid)` → wait deadline → `TerminateJobObject`; close the job handle last.
- Unix: `SysProcAttr{Setpgid: true}`; `unix.Kill(-pgid, SIGTERM)` → deadline → `SIGKILL`.

**Paths** (`paths`). Guidance: `Canonical(p)` = `filepath.Abs` → `filepath.Clean` → `filepath.EvalSymlinks` → prefix check against the root; on Windows compare case-insensitively and on path-separator boundaries; add `\\?\` (or `\\?\UNC\`) only when length > 240 and only for raw API calls.

**Service**. Guidance: `svc/mgr` to install (`mgr.Connect`, `CreateService`), `svc.Run` handler with Stop/Shutdown → graceful drain incl. `PrjStopVirtualizing`; `svc/eventlog` for start failures. How auto-attach is ordered before the daemon (scheduled task vs service) is left open by §6.4. Linux/macOS: generate a systemd unit / launchd plist that runs `cowflow daemon start` (§3.1).

**Defender / indexing**. The spec requires only a *suggestion* to exclude the volume (§6.4) and lists Defender/indexer hydration as a ProjFS risk (§6.5). Guidance: `doctor` prints the exact `Add-MpPreference -ExclusionPath` command for the user to run; optionally mark `workspace.root`/`golden_root` with `FILE_ATTRIBUTE_NOT_CONTENT_INDEXED` (reversible, local to COWFlow's own directories).

## Pitfalls
- Daemon running as a service has no console: `CTRL_BREAK` cannot reach children unless they share a console; rely on the deadline → `TerminateJobObject` path.
- Assigning a job after the child started lets fast-forking children escape; start suspended.
- `os` auto-prefixes long paths; `x/sys/windows` calls and ProjFS paths do not.
- Block clone fails across volumes, on NTFS, with misaligned offsets, or with mismatched sparse/integrity attributes — report as unsupported, never fall back to a silent full copy while claiming `Block`.
- VHDX attach is lost on reboot; auto-attach must run before the daemon starts or workspaces go `pending_mount`.
- Defender and the indexer hydrate ProjFS roots and slow clones; spec mandates only suggesting the Defender exclusion (do not change Defender settings silently).
- ProjFS daemon hang freezes agent I/O; every callback needs a bounded deadline.
- Format/partition via PowerShell: check exit codes and parse nothing locale-dependent.
- Line endings: `core.autocrlf=true` from the user's global config breaks clone/hash equality; set `false` per worktree.
- Spec header says Go 1.23+; repo `go.mod` targets Go 1.25 — use APIs available in the `go.mod` version.

## Tests required
- Unit (all OSes): path canonicalization/confinement (`..`, symlink escape, case, UNC, > 240 chars); `\\?\` prefixing; argv construction (no shell strings); struct sizes/offsets for `DUPLICATE_EXTENTS_DATA`, virtdisk and ProjFS structs (`unsafe.Sizeof`/`Offsetof` vs documented values on amd64/arm64).
- Platform-gated integration (skip with reason when capability absent): Windows ReFS block clone of files incl. > 4 GiB-chunked and unaligned tail, sparse files; Linux btrfs/XFS `FICLONE`; macOS APFS `clonefile`; isolation after clone (write to dest leaves source untouched).
- AC 16: cancellation on Windows kills a 3-level process tree (and `Setpgid` group on Unix); no orphans.
- AC 13: `doctor`/`inspect` report actual FS and `CowKind` (ReFS vs NTFS on same host).
- AC 21: `init --devdrive` provisions a Dev Drive; `cow_clone` of ≥ 1 GB repo < 2 s, incremental disk < 1% (admin Windows 11 runner). Phase 0 output: `doctor` reports ReFS/COW correctly.
- AC 22: ProjFS workspace < 1 s; materialization < 20% when touching < 10%.
- Hermetic harness (§18.1): simulated VHDX — not mounted → `pending_mount`, auto-attach, never NTFS; simulated ProjFS (in-memory callbacks) — callback timeout → error not hang, virtualization restart, blob outside `base_sha` refused; stuck handle → `pending_cleanup` retry; daemon restart reconciles ProjFS roots.
- AC 14: CI on Windows, Linux, macOS; limitations documented in `docs/windows.md` and `docs/projfs.md`.

## Done checklist
- [ ] Builds with `CGO_ENABLED=0` for `windows`, `linux`, `darwin` (amd64 + arm64)
- [ ] Every OS-specific file has a counterpart or a `!goos` stub
- [ ] DLL procs loaded lazily with `Find()` checks and typed errors
- [ ] `init --devdrive` steps 1–7 of §6.4 implemented, PowerShell fallback for `New-VHD`
- [ ] Auto-attach before daemon; `pending_mount` path tested
- [ ] Block clone per OS with unsupported sentinel; capability probe uses a real clone
- [ ] Job Object (suspended start) / process group kill; AC 16 passes
- [ ] Long paths, canonicalization, confinement; no `cmd.exe` strings
- [ ] `daemon install` via `x/sys/windows/svc`; graceful stop stops ProjFS
- [ ] `doctor` covers every §7 bullet
- [ ] Harness scenarios for VHDX/ProjFS/handles pass
