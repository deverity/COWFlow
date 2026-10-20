package workspace

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/deverity/cowflow/internal/git"
)

// WorktreeName is the provider name of the git_worktree backend.
const WorktreeName = "git_worktree"

// worktreeCleanupTimeout bounds the compensation of a failed Create, which
// runs on a context detached from the caller's cancellation.
const worktreeCleanupTimeout = 30 * time.Second

// WorktreeProvider is the git_worktree backend (SCOPE.md §6.2): a plain
// `git worktree add`. It is the universal fallback, available on every
// filesystem, and it is never copy-on-write.
type WorktreeProvider struct {
	git  GitOps
	vols Volumes
	goos string // runtime.GOOS; a field so the Windows config is testable anywhere
	insp *Inspector
	// destroyer is the shared destroy (S1-T14).
	destroyer *Destroyer
}

var _ WorkspaceProvider = (*WorktreeProvider)(nil)

// NewWorktreeProvider returns the git_worktree provider over g and v. A nil
// v means [PlatformVolumes].
func NewWorktreeProvider(g GitOps, v Volumes) *WorktreeProvider {
	if v == nil {
		v = PlatformVolumes{}
	}
	return &WorktreeProvider{git: g, vols: v, goos: runtime.GOOS}
}

// Name returns "git_worktree".
func (*WorktreeProvider) Name() string { return WorktreeName }

// Capabilities reports what git_worktree delivers under root: no
// copy-on-write, sparse checkout, no process isolation, no resource limits,
// no daemon, and the filesystem probed now from root. It never reports
// block on ReFS: this provider does not clone blocks.
func (p *WorktreeProvider) Capabilities(ctx context.Context, root string) (Capabilities, error) {
	vol, err := p.vols.VolumeOf(ctx, root)
	if err != nil {
		return Capabilities{}, fmt.Errorf("git_worktree capabilities: %w", err)
	}
	return Capabilities{
		CopyOnWrite:     CowNone,
		SparseSupported: true,
		Filesystem:      vol.Filesystem,
	}, nil
}

// Create adds a worktree at spec.Path on the new branch spec.Branch at
// spec.BaseSHA and sets the per-worktree config (SCOPE.md §7). With
// spec.Sparse.Enabled the checkout is skipped, then the cone set of
// spec.Sparse.Paths (plus [AlwaysSet]) is applied and the index and files
// are populated under it (SCOPE.md §6.6), so only that scope is on disk.
//
// spec.Path must not exist; an existing path is [ErrPathExists] and is never
// touched. An existing branch is [ErrBranchExists]. Any other failure removes
// what this call created (the worktree, the directory, the admin entry) on a
// detached context before returning; the task branch is the Manager's to
// delete.
func (p *WorktreeProvider) Create(ctx context.Context, spec WorkspaceSpec) (_ *Workspace, err error) {
	if _, serr := os.Lstat(spec.Path); serr == nil {
		return nil, fmt.Errorf("%w: %s", ErrPathExists, spec.Path)
	} else if !errors.Is(serr, fs.ErrNotExist) {
		return nil, fmt.Errorf("git_worktree create: %w", serr)
	}
	caps, err := p.Capabilities(ctx, filepath.Dir(spec.Path))
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, p.compensate(ctx, spec.RepoDir, spec.Path))
		}
	}()
	err = p.git.WorktreeAdd(ctx, spec.RepoDir, spec.Path, git.WorktreeAddOptions{
		NewBranch: spec.Branch, Commitish: spec.BaseSHA, NoCheckout: spec.Sparse.Enabled,
	})
	if err != nil {
		if branchExists(err) {
			return nil, fmt.Errorf("%w: %q: %w", ErrBranchExists, spec.Branch, err)
		}
		return nil, fmt.Errorf("git_worktree create: %w", err)
	}
	if err = p.configure(ctx, spec.Path); err != nil {
		return nil, err
	}
	if spec.Sparse.Enabled {
		if err = applySparse(ctx, p.git, spec.Path, spec.Sparse.Paths, true); err != nil {
			return nil, fmt.Errorf("git_worktree create: %w", err)
		}
	}
	return &Workspace{
		ID: spec.ID, ProjectID: spec.ProjectID, TaskID: spec.TaskID, SessionID: spec.SessionID,
		Provider: WorktreeName, Path: spec.Path, Branch: spec.Branch, BaseSHA: spec.BaseSHA,
		Caps:   caps,
		Sparse: SparseSpec{Enabled: spec.Sparse.Enabled, Paths: append([]string(nil), spec.Sparse.Paths...)},
		Ports:  spec.Ports,
	}, nil
}

// expandScope widens the sparse scope of w by add. Git writes the files that
// newly match, from the object store.
func (p *WorktreeProvider) expandScope(ctx context.Context, w Workspace, add []string) error {
	cur, err := p.git.SparseList(ctx, w.Path)
	if err != nil {
		return fmt.Errorf("git_worktree expand scope: %w", err)
	}
	if err := applySparse(ctx, p.git, w.Path, append(cur, add...), false); err != nil {
		return fmt.Errorf("git_worktree expand scope: %w", err)
	}
	return nil
}

// WithInspector sets the shared [Inspector] Inspect delegates to and returns
// p. Without one, Inspect fails with a configuration error.
func (p *WorktreeProvider) WithInspector(in *Inspector) *WorktreeProvider {
	p.insp = in
	return p
}

// Inspect reports the workspace through the shared [Inspector]; this
// provider never delivers copy-on-write, so the kind is always none.
func (p *WorktreeProvider) Inspect(ctx context.Context, id string) (*WorkspaceStatus, error) {
	return p.insp.Inspect(ctx, WorktreeName, CowNone, id)
}

// WithDestroyer sets the shared [Destroyer] Destroy delegates to and returns
// p. Without one, Destroy fails with a configuration error.
func (p *WorktreeProvider) WithDestroyer(d *Destroyer) *WorktreeProvider {
	p.destroyer = d
	return p
}

// Destroy destroys the workspace through the shared [Destroyer]: dirty
// check, rescue with --force, audit, removal, pending_cleanup on a stuck
// handle. The task branch is kept.
func (p *WorktreeProvider) Destroy(ctx context.Context, id string, opts DestroyOptions) (DestroyResult, error) {
	return p.destroyer.Destroy(ctx, WorktreeName, id, opts)
}

// configure sets the per-worktree config: line endings are left to
// .gitattributes (core.autocrlf=false), and on Windows long paths are
// enabled (SCOPE.md §7).
func (p *WorktreeProvider) configure(ctx context.Context, path string) error {
	cfg := [][2]string{{"core.autocrlf", "false"}}
	if p.goos == "windows" {
		cfg = append(cfg, [2]string{"core.longpaths", "true"})
	}
	for _, kv := range cfg {
		if err := p.git.ConfigSetWorktree(ctx, path, kv[0], kv[1]); err != nil {
			return fmt.Errorf("git_worktree create: set %s: %w", kv[0], err)
		}
	}
	return nil
}

// compensate removes the worktree registration, the confined directory and
// the stale admin entry of a failed Create, on a context detached from ctx.
// It only runs for a path that did not exist when Create began. The
// directory is removed only if it still resolves to itself under its parent.
//
// path need not be canonical: on Windows it may hold 8.3 short names (a
// %TEMP% of C:\Users\RUNNER~1) while git lists, and Confine returns, the
// long form. Every comparison is therefore made between canonical paths.
func (p *WorktreeProvider) compensate(ctx context.Context, repo, path string) error {
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), worktreeCleanupTimeout)
	defer cancel()
	var errs []error
	want := canonicalOrClean(path)
	wts, err := p.git.WorktreeList(cctx, repo)
	if err != nil {
		errs = append(errs, fmt.Errorf("list worktrees: %w", err))
	}
	for _, wt := range wts {
		if samePath(wt.Path, path) || samePath(canonicalOrClean(wt.Path), want) {
			if err := p.git.WorktreeRemove(cctx, repo, path, true); err != nil {
				errs = append(errs, fmt.Errorf("remove worktree %s: %w", path, err))
			}
			break
		}
	}
	if err := removeOwnDir(path); err != nil {
		errs = append(errs, err)
	}
	if err := p.git.WorktreePrune(cctx, repo); err != nil {
		errs = append(errs, fmt.Errorf("prune worktrees: %w", err))
	}
	if len(errs) > 0 {
		return fmt.Errorf("git_worktree compensation: %w", errors.Join(errs...))
	}
	return nil
}

// branchExists reports whether err is git refusing `worktree add -b` because
// the branch exists.
func branchExists(err error) bool {
	var ge *git.Error
	return errors.As(err, &ge) && strings.Contains(ge.Stderr, "already exists") &&
		strings.Contains(ge.Stderr, "branch")
}
