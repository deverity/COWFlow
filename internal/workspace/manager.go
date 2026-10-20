package workspace

import "github.com/deverity/cowflow/internal/domain"

// The Manager is the entry point the CLI uses (sprint-1.md §6.1). Its type,
// constructor and methods land with S1-T03; until then consumers code against
// workspacetest.FakeManager, which has the agreed method set:
//
//	Select(ctx, requested string) (Selection, error) // "auto"|"cow_clone"|"git_worktree"
//	Create(ctx, req CreateRequest) (*Workspace, Selection, error)
//	Inspect(ctx, id string) (*WorkspaceStatus, error)
//	Destroy(ctx, id string, opts DestroyOptions) error
//	List(ctx, f ListFilter) ([]Workspace, error)
//	ExpandScope(ctx, id string, add []string) error // MCP wiring: Sprint 3

// Selection is the provider chosen for a create request.
type Selection struct {
	Provider WorkspaceProvider
	Caps     Capabilities
	Warning  string // non-empty when auto fell back (§1.2 "explicit warning")
}

// CreateRequest asks the Manager for a workspace for one task.
type CreateRequest struct {
	OpID      string      // idempotency key from the caller (§18); a retry returns the same workspace
	ProjectID string      // project the task belongs to
	RepoDir   string      // canonical main repository dir
	SessionID string      // empty until Sprint 3
	Provider  string      // requested provider: "auto", "cow_clone" or "git_worktree"
	Task      domain.Task // stored task: slug, branch, base SHA, paths and sparse flag
}

// ListFilter selects workspaces for List. Empty fields match everything.
type ListFilter struct {
	ProjectID string
	TaskID    string
	States    []domain.WorkspaceState // empty: every state
}
