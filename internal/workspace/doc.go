// Package workspace creates, inspects and destroys COWFlow workspaces
// (SCOPE.md §6).
//
// A workspace is an isolated working copy of a project repository with its
// own path, branch, HEAD, index, files, logs, .env.cowflow and port range
// (§6.1). Backends implement [WorkspaceProvider] (§6.2): cow_clone (block
// copy-on-write from a golden tree), git_worktree (the universal fallback),
// and later projfs and container. Each provider reports in [Capabilities]
// exactly what it delivers, never more (§2.2 principle 10).
//
// The package declares the interfaces it consumes, so its adapters stay
// swappable and testable: [GitOps] (satisfied by *git.Client), [Store]
// (satisfied by *store.Store), [Volumes] (backed by internal/platform) and
// [Cloner] (the per-OS block-clone primitive). Package workspacetest holds
// in-memory fakes of all of them for tests.
package workspace
