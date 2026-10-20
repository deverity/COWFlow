package workspacetest

import "github.com/deverity/cowflow/internal/workspace"

// Compile-time checks: every fake satisfies the interface it stands in for.
// FakeManager is checked against the Manager method set in manager_test.go
// until the real Manager type exists (S1-T03).
var (
	_ workspace.WorkspaceProvider = (*FakeProvider)(nil)
	_ workspace.Cloner            = (*FakeCloner)(nil)
	_ workspace.GitOps            = (*FakeGit)(nil)
	_ workspace.Store             = (*MemStore)(nil)
	_ workspace.Volumes           = (*FakeVolumes)(nil)
)
