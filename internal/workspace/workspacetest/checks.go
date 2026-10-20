package workspacetest

import "github.com/deverity/cowflow/internal/workspace"

// Compile-time checks: every fake satisfies the interface it stands in for.
// FakeManager is checked against the Manager method set in fakes_test.go;
// *workspace.Manager is checked against the same set in manager.go.
var (
	_ workspace.WorkspaceProvider = (*FakeProvider)(nil)
	_ workspace.Cloner            = (*FakeCloner)(nil)
	_ workspace.GitOps            = (*FakeGit)(nil)
	_ workspace.Store             = (*MemStore)(nil)
	_ workspace.Volumes           = (*FakeVolumes)(nil)
)
