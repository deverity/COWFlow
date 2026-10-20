package workspace

import (
	"context"

	"github.com/deverity/cowflow/internal/git"
)

// GitOps is the set of git operations the workspace providers use
// (sprint-1.md §6.4). *git.Client satisfies it; every call is one argv
// invocation through the git runner, never a shell string (SCOPE.md §7,
// §17).
type GitOps interface {
	ResolveCommit(ctx context.Context, repo, rev string) (sha string, err error)
	CheckBranchName(ctx context.Context, name string) error
	BranchExists(ctx context.Context, repo, name string) (bool, error)
	DeleteBranchIfAt(ctx context.Context, repo, name, sha string) error // compensation only

	WorktreeAdd(ctx context.Context, repo, path string, o git.WorktreeAddOptions) error
	WorktreeRemove(ctx context.Context, repo, path string, force bool) error
	WorktreePrune(ctx context.Context, repo string) error
	WorktreeList(ctx context.Context, repo string) ([]git.Worktree, error)
	GitPath(ctx context.Context, worktree, name string) (string, error) // e.g. "index"
	ConfigSetWorktree(ctx context.Context, worktree, key, value string) error
	Head(ctx context.Context, worktree string) (string, error)

	ExportTree(ctx context.Context, repo, sha, dstDir, indexFile string) error // golden build
	RefreshIndex(ctx context.Context, repo, workTree, indexFile string) error
	ReadTree(ctx context.Context, worktree string, args ...string) error // e.g. "-mu", "HEAD"

	SparseSet(ctx context.Context, worktree string, dirs []string) error // cone mode, via stdin
	SparseDisable(ctx context.Context, worktree string) error

	Status(ctx context.Context, worktree string) ([]git.StatusEntry, error)
	Unpublished(ctx context.Context, worktree, baseSHA, refNamespace string) ([]string, error)
}
