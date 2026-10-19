---
name: go-conventions
description: Use whenever writing or reviewing Go code in COWFlow — package layout, dependencies, error handling, logging, concurrency, cross-platform files, and testing style.
---
# Go conventions for COWFlow

## When to use

Any change to `.go` files, `go.mod`, or package structure.

## Source of truth

- `docs/SCOPE.md` §16 "Go stack" and §16.1 "Layout" (wins on conflict).
- §2.2 "Principles", §17 "Security", §18 "Resilience".
- `.golangci.yml` (lint rules), `go.mod` (Go version).

## Rules

- MUST build with `CGO_ENABLED=0` (§16). Windows DLLs (`virtdisk.dll`,
  `ProjectedFSLib.dll`) via `golang.org/x/sys/windows.NewLazySystemDLL`.
- MUST use the fixed stack (§16): cobra (CLI), `net/http` + manual SSE,
  `modernc.org/sqlite`, `github.com/BurntSushi/toml`, `log/slog`,
  `github.com/oklog/ulid/v2`, official MCP Go SDK (fallback
  `github.com/mark3labs/mcp-go`), `golang.org/x/sys`, testify, goreleaser.
  Any other dependency needs maintainer approval.
- MUST place code per §16.1; everything outside `cmd/` lives in `internal/`.
- MUST invoke external programs with `exec.CommandContext(ctx, name, args...)`
  — never via a shell, never with concatenated command strings (§17).
- MUST accept `context.Context` as the first parameter of anything that does
  I/O, spawns processes, or may block; honor cancellation and timeouts.
- MUST make mutating operations idempotent by `op_id` where the spec says so
  (§18).
- MUST log with `log/slog` (JSON handler in the daemon); never `fmt.Print*`
  for diagnostics and never `log.Fatal` outside `main`.
- NEVER panic for expected errors; NEVER ignore errors (`_ =` needs a comment
  saying why).
- NEVER keep global mutable state; pass dependencies explicitly.

## Implementation guide

### Layout (§16.1)

```text
cmd/cowflow/main.go        thin: build deps, call internal/cli, os.Exit
internal/cli               cobra commands; no business logic
internal/domain            types, states, pure rules (no I/O)
internal/store             SQLite, migrations, outbox, index
internal/git               git CLI wrapper
internal/workspace         providers (+ projfs/, container/)
internal/platform          OS specifics: paths, process_*, vhdx_windows.go, fscaps_*
internal/{daemon,api,runner,scheduler,collab,integration,mcp,config}
test/{harness,integration,fixtures}
```

Dependency direction: `cli/api/mcp → services (collab, integration,
scheduler, workspace) → domain`; `store`, `git`, `platform` are adapters
behind interfaces defined by their consumers. `domain` imports nothing internal.

### Errors

- Wrap with context: `fmt.Errorf("publish change %s: %w", id, err)`.
- Sentinel errors for conditions callers branch on (`ErrStale`,
  `ErrConflict`, …); check with `errors.Is` / `errors.As` (`errorlint` enforces).
- Errors returned across the API/MCP boundary carry a stable machine code
  plus a human message; never leak internal paths or tokens.

### Cross-platform

- File suffixes: `cow_windows.go`, `cow_linux.go`, `cow_darwin.go`; a shared
  interface in the un-suffixed file; an explicit "unsupported" implementation
  for other OSes.
- Paths: `filepath` (never `path`) for OS paths; canonicalize with
  `filepath.Clean` + `filepath.EvalSymlinks` + prefix check (§7, §17).
- Never assume `/` separators, case sensitivity, or that open files can be
  deleted (Windows handles).

### Concurrency

- Every goroutine has an owner that waits for it (`errgroup`/`sync.WaitGroup`)
  and a cancellation path.
- Protect shared state with the narrowest primitive; prefer ownership by a
  single goroutine + channels for state machines.
- Target load is 20+ concurrent sessions: no unbounded goroutines or queues.

### IDs and time

- IDs: ULID (`oklog/ulid`), monotonic entropy per process.
- Time: inject a clock interface (`Now()`, timers) so the harness fake clock
  works (§18.1). Never call `time.Now()` directly in domain/service code.

### Style

- Small interfaces defined where they are consumed.
- Exported identifiers documented; package doc comment in one file per package.
- Table-driven tests with `t.Run`; `testify/require` for preconditions,
  `assert` for checks.

## Pitfalls

- Using `os.Exec`-style shell strings "just for Windows" — forbidden.
- `time.Now()` / `rand` in logic → untestable, flaky under the harness.
- Long-held SQLite transactions blocking other sessions (see `store-and-events`).
- Forgetting `-race` coverage on new concurrent code.
- Adding a dependency to save 20 lines.

## Tests required

- Unit tests next to the code (`*_test.go`), `-race` clean.
- Failure paths through the hermetic harness (`testing-harness` skill).
- Fuzz targets for every parser of external input (manifests, API payloads).

## Done checklist

- [ ] Code in the right package; no import cycles; `domain` stays pure
- [ ] `context.Context` threaded; cancellation tested where relevant
- [ ] Errors wrapped; sentinel errors for branchable conditions
- [ ] No shell strings; paths canonicalized and confined
- [ ] Clock/IDs injectable; no direct `time.Now()` in logic
- [ ] `gofmt`/`goimports`, `go vet`, `golangci-lint run` clean
- [ ] `go test -race ./...` green on your OS; platform files compile for all three (`GOOS=windows|linux|darwin go vet ./...`)
