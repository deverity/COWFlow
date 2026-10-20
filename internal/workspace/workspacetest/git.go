package workspacetest

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sync"

	"github.com/deverity/cowflow/internal/git"
)

// ErrFakeGit is wrapped by the errors FakeGit makes up itself (an unknown
// revision, a branch that already exists).
var ErrFakeGit = errors.New("fake git")

// Call is one recorded FakeGit call: the method name and its arguments
// after ctx, in order.
type Call struct {
	Method string
	Args   []any
}

// FakeGit is a workspace.GitOps that records every call and returns scripted
// results. It keeps just enough state to be consistent: WorktreeAdd with a
// new branch creates the branch and lists the worktree, WorktreeRemove
// unlists it and DeleteBranchIfAt deletes the branch.
//
// For each call, in order: the call is recorded; Hook, when set, runs and a
// non-nil error is returned; Errs[method], when set, is returned; otherwise
// the scripted or default result is returned.
type FakeGit struct {
	Hook func(c Call) error
	Errs map[string]error // by method name, e.g. "ExportTree"

	Commits   map[string]string            // ResolveCommit: rev -> SHA; a full hex SHA resolves to itself
	Branches  map[string]string            // branch name -> tip SHA
	Heads     map[string]string            // Head: worktree -> SHA; unset: the SHA it was added at
	GitPaths  map[string]string            // GitPath: name -> path; unset: <worktree>/.git/<name>
	Statuses  map[string][]git.StatusEntry // Status: worktree -> entries
	Unpublish map[string][]string          // Unpublished: worktree -> commit SHAs

	mu        sync.Mutex
	calls     []Call
	worktrees []git.Worktree
}

// Calls returns a copy of the recorded calls, in order.
func (g *FakeGit) Calls() []Call {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.calls)
}

// Count reports how many times method was called.
func (g *FakeGit) Count(method string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, c := range g.calls {
		if c.Method == method {
			n++
		}
	}
	return n
}

// enter records the call and returns the injected error, if any. The caller
// holds no lock; on a nil result the lock is held and the caller must
// unlock it.
func (g *FakeGit) enter(ctx context.Context, method string, args ...any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c := Call{Method: method, Args: args}
	g.mu.Lock()
	g.calls = append(g.calls, c)
	g.mu.Unlock()
	if g.Hook != nil {
		if err := g.Hook(c); err != nil {
			return err
		}
	}
	if err := g.Errs[method]; err != nil {
		return err
	}
	g.mu.Lock()
	return nil
}

// ResolveCommit returns Commits[rev], or rev itself when it is a full SHA.
func (g *FakeGit) ResolveCommit(ctx context.Context, repo, rev string) (string, error) {
	if err := g.enter(ctx, "ResolveCommit", repo, rev); err != nil {
		return "", err
	}
	defer g.mu.Unlock()
	if sha, ok := g.Commits[rev]; ok {
		return sha, nil
	}
	if sha, ok := g.Branches[rev]; ok {
		return sha, nil
	}
	if git.IsSHA(rev) {
		return rev, nil
	}
	return "", fmt.Errorf("%w: unknown revision %q", ErrFakeGit, rev)
}

// CheckBranchName accepts every name unless an error is injected.
func (g *FakeGit) CheckBranchName(ctx context.Context, name string) error {
	if err := g.enter(ctx, "CheckBranchName", name); err != nil {
		return err
	}
	g.mu.Unlock()
	return nil
}

// BranchExists reports whether name is in Branches.
func (g *FakeGit) BranchExists(ctx context.Context, repo, name string) (bool, error) {
	if err := g.enter(ctx, "BranchExists", repo, name); err != nil {
		return false, err
	}
	defer g.mu.Unlock()
	_, ok := g.Branches[name]
	return ok, nil
}

// DeleteBranchIfAt deletes name when its tip is sha; otherwise it does
// nothing, like the compare-and-delete it fakes.
func (g *FakeGit) DeleteBranchIfAt(ctx context.Context, repo, name, sha string) error {
	if err := g.enter(ctx, "DeleteBranchIfAt", repo, name, sha); err != nil {
		return err
	}
	defer g.mu.Unlock()
	if g.Branches[name] == sha {
		delete(g.Branches, name)
	}
	return nil
}

// WorktreeAdd lists a worktree at path; with o.NewBranch it also creates
// that branch at o.Commitish and fails when the branch exists.
func (g *FakeGit) WorktreeAdd(ctx context.Context, repo, path string, o git.WorktreeAddOptions) error {
	if err := g.enter(ctx, "WorktreeAdd", repo, path, o); err != nil {
		return err
	}
	defer g.mu.Unlock()
	sha, branch := o.Commitish, o.NewBranch
	if branch == "" {
		if tip, ok := g.Branches[o.Commitish]; ok {
			sha, branch = tip, o.Commitish
		}
	} else {
		if _, ok := g.Branches[branch]; ok {
			return fmt.Errorf("%w: a branch named %q already exists", ErrFakeGit, branch)
		}
		if g.Branches == nil {
			g.Branches = map[string]string{}
		}
		g.Branches[branch] = sha
	}
	g.worktrees = append(g.worktrees, git.Worktree{Path: path, Head: sha, Branch: branch})
	return nil
}

// WorktreeRemove unlists the worktree at path.
func (g *FakeGit) WorktreeRemove(ctx context.Context, repo, path string, force bool) error {
	if err := g.enter(ctx, "WorktreeRemove", repo, path, force); err != nil {
		return err
	}
	defer g.mu.Unlock()
	g.worktrees = slices.DeleteFunc(g.worktrees, func(w git.Worktree) bool { return w.Path == path })
	return nil
}

// WorktreePrune records the call.
func (g *FakeGit) WorktreePrune(ctx context.Context, repo string) error {
	if err := g.enter(ctx, "WorktreePrune", repo); err != nil {
		return err
	}
	g.mu.Unlock()
	return nil
}

// WorktreeList returns the worktrees added and not removed.
func (g *FakeGit) WorktreeList(ctx context.Context, repo string) ([]git.Worktree, error) {
	if err := g.enter(ctx, "WorktreeList", repo); err != nil {
		return nil, err
	}
	defer g.mu.Unlock()
	return slices.Clone(g.worktrees), nil
}

// GitPath returns GitPaths[name], or <worktree>/.git/<name>.
func (g *FakeGit) GitPath(ctx context.Context, worktree, name string) (string, error) {
	if err := g.enter(ctx, "GitPath", worktree, name); err != nil {
		return "", err
	}
	defer g.mu.Unlock()
	if p, ok := g.GitPaths[name]; ok {
		return p, nil
	}
	return filepath.Join(worktree, ".git", filepath.FromSlash(name)), nil
}

// ConfigSetWorktree records the call.
func (g *FakeGit) ConfigSetWorktree(ctx context.Context, worktree, key, value string) error {
	if err := g.enter(ctx, "ConfigSetWorktree", worktree, key, value); err != nil {
		return err
	}
	g.mu.Unlock()
	return nil
}

// Head returns Heads[worktree], or the SHA the worktree was added at.
func (g *FakeGit) Head(ctx context.Context, worktree string) (string, error) {
	if err := g.enter(ctx, "Head", worktree); err != nil {
		return "", err
	}
	defer g.mu.Unlock()
	if sha, ok := g.Heads[worktree]; ok {
		return sha, nil
	}
	for _, w := range g.worktrees {
		if w.Path == worktree {
			return w.Head, nil
		}
	}
	return "", fmt.Errorf("%w: %s is not a worktree", ErrFakeGit, worktree)
}

// ExportTree records the call; use Hook to materialize files.
func (g *FakeGit) ExportTree(ctx context.Context, repo, sha, dstDir, indexFile string) error {
	if err := g.enter(ctx, "ExportTree", repo, sha, dstDir, indexFile); err != nil {
		return err
	}
	g.mu.Unlock()
	return nil
}

// RefreshIndex records the call.
func (g *FakeGit) RefreshIndex(ctx context.Context, repo, workTree, indexFile string) error {
	if err := g.enter(ctx, "RefreshIndex", repo, workTree, indexFile); err != nil {
		return err
	}
	g.mu.Unlock()
	return nil
}

// ReadTree records the call.
func (g *FakeGit) ReadTree(ctx context.Context, worktree string, args ...string) error {
	if err := g.enter(ctx, "ReadTree", worktree, slices.Clone(args)); err != nil {
		return err
	}
	g.mu.Unlock()
	return nil
}

// SparseSet records the call.
func (g *FakeGit) SparseSet(ctx context.Context, worktree string, dirs []string) error {
	if err := g.enter(ctx, "SparseSet", worktree, slices.Clone(dirs)); err != nil {
		return err
	}
	g.mu.Unlock()
	return nil
}

// SparseList records the call and returns nothing.
func (g *FakeGit) SparseList(ctx context.Context, worktree string) ([]string, error) {
	if err := g.enter(ctx, "SparseList", worktree); err != nil {
		return nil, err
	}
	g.mu.Unlock()
	return nil, nil
}

// SparseDisable records the call.
func (g *FakeGit) SparseDisable(ctx context.Context, worktree string) error {
	if err := g.enter(ctx, "SparseDisable", worktree); err != nil {
		return err
	}
	g.mu.Unlock()
	return nil
}

// Status returns Statuses[worktree].
func (g *FakeGit) Status(ctx context.Context, worktree string) ([]git.StatusEntry, error) {
	if err := g.enter(ctx, "Status", worktree); err != nil {
		return nil, err
	}
	defer g.mu.Unlock()
	return slices.Clone(g.Statuses[worktree]), nil
}

// Unpublished returns Unpublish[worktree].
func (g *FakeGit) Unpublished(ctx context.Context, worktree, baseSHA, refNamespace string) ([]string, error) {
	if err := g.enter(ctx, "Unpublished", worktree, baseSHA, refNamespace); err != nil {
		return nil, err
	}
	defer g.mu.Unlock()
	return slices.Clone(g.Unpublish[worktree]), nil
}
