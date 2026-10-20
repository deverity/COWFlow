// Package workspacetest provides in-memory fakes of the workspace contract
// for tests (sprint-1.md §6.7): FakeProvider and FakeManager for the CLI and
// acceptance tests, and FakeCloner, FakeGit, MemStore and FakeVolumes for the
// workspace package's own tests.
//
// Everything here exists for tests only. Production code must never import
// this package: FakeCloner in particular copies bytes, which the cow_clone
// contract forbids (SCOPE.md §6.1, sprint-1.md §6.2).
//
// Every fake is safe for concurrent use. Exported configuration fields must
// be set before the fake is shared between goroutines.
package workspacetest
