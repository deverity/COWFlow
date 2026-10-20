package workspace

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/deverity/cowflow/internal/config"
	"github.com/deverity/cowflow/internal/git"
)

// cow_clone tuning. None of these is a config key (SCOPE.md §22 has none).
const (
	cloneWorkers = 16       // parallel CloneFile calls per Create; S1-T21 tunes it
	cloneQueue   = 1024     // walker-to-worker buffer
	probeSize    = 64 << 10 // bytes of the capability probe file
	probePrefix  = ".cowflow-probe-"
)

// GoldenSource hands out ready golden trees with an in_use reference that
// release drops (SCOPE.md §6.4). *GoldenCache satisfies it.
type GoldenSource interface {
	Acquire(ctx context.Context, projectID, repo, sha string) (g Golden, release func(), err error)
}

var _ GoldenSource = (*GoldenCache)(nil)

// CowCloneDeps are the cow_clone provider's collaborators. Config, Git and
// Golden are required; every other field has a production default.
type CowCloneDeps struct {
	Config  *config.Config // [workspace].root and golden_root
	Git     GitOps
	Golden  GoldenSource // the golden tree cache (S1-T09)
	Volumes Volumes      // default PlatformVolumes
	Cloner  Cloner       // default: the OS block-clone primitive (cow_<goos>.go)
	Logger  *slog.Logger // default slog.Default()
}

// CowCloneProvider is the cow_clone backend (SCOPE.md §6.2, §6.3): a Git
// worktree registered without a checkout, whose files are block clones of
// the golden tree of base_sha and whose index is a block clone of the
// golden index (§23 decision 39). It never byte-copies a file: where block
// clone is unavailable it reports CowNone and Create fails.
//
// A CowCloneProvider is safe for concurrent use.
type CowCloneProvider struct {
	cfg    config.Config
	git    GitOps
	golden GoldenSource
	vols   Volumes
	cloner Cloner
	seeder IndexSeeder // goldenIndexSeeder; tests may swap it (option A)
	wt     *WorktreeProvider
	log    *slog.Logger

	mu   sync.Mutex
	caps map[string]Capabilities // per clean root, for the life of the process
}

var _ WorkspaceProvider = (*CowCloneProvider)(nil)

// NewCowCloneProvider returns the cow_clone provider over d, filling the
// defaults of the optional dependencies. A missing required dependency is
// reported by the first method that needs it.
func NewCowCloneProvider(d CowCloneDeps) *CowCloneProvider {
	p := &CowCloneProvider{
		git: d.Git, golden: d.Golden, vols: d.Volumes, cloner: d.Cloner, log: d.Logger,
		caps: map[string]Capabilities{},
	}
	if d.Config != nil {
		p.cfg = *d.Config
	}
	if p.vols == nil {
		p.vols = PlatformVolumes{}
	}
	if p.cloner == nil {
		p.cloner = osCloner{}
	}
	if p.log == nil {
		p.log = slog.Default()
	}
	p.seeder = goldenIndexSeeder{git: p.git, cloner: p.cloner}
	p.wt = &WorktreeProvider{git: p.git, vols: p.vols, goos: runtime.GOOS}
	return p
}

// Name returns "cow_clone".
func (*CowCloneProvider) Name() string { return ProviderCowClone }

func (p *CowCloneProvider) ready() error {
	switch {
	case p.git == nil:
		return fmt.Errorf("%w: cow_clone has no git", errNotConfigured)
	case p.golden == nil:
		return fmt.Errorf("%w: cow_clone has no golden cache", errNotConfigured)
	}
	return nil
}

// Capabilities reports what cow_clone delivers under root. Block clone is
// claimed only when the golden root and root share a volume (§23 decision
// 31) and a real probe clone in root succeeded: 64 KiB of random bytes are
// cloned and compared, then both files are removed. Otherwise CopyOnWrite is
// CowNone, so auto falls back to git_worktree. The answer is cached per root
// for the life of the process; a probe that failed for another reason is
// returned as an error and not cached.
func (p *CowCloneProvider) Capabilities(ctx context.Context, root string) (Capabilities, error) {
	key := filepath.Clean(root)
	p.mu.Lock()
	c, ok := p.caps[key]
	p.mu.Unlock()
	if ok {
		return c, nil
	}
	c, err := p.probeCaps(ctx, key)
	if err != nil {
		return Capabilities{}, err
	}
	p.mu.Lock()
	p.caps[key] = c
	p.mu.Unlock()
	return c, nil
}

func (p *CowCloneProvider) probeCaps(ctx context.Context, root string) (Capabilities, error) {
	vol, err := p.vols.VolumeOf(ctx, root)
	if err != nil {
		return Capabilities{}, fmt.Errorf("cow_clone capabilities: %w", err)
	}
	none := Capabilities{CopyOnWrite: CowNone, SparseSupported: true, Filesystem: vol.Filesystem}
	golden := p.cfg.Workspace.EffectiveGoldenRoot()
	if golden == "" {
		return Capabilities{}, ErrRootNotSet
	}
	same, err := p.vols.SameVolume(ctx, golden, root)
	if err != nil {
		return Capabilities{}, fmt.Errorf("cow_clone capabilities: compare golden and workspace volumes: %w", err)
	}
	if !same {
		p.log.Info("cow_clone unavailable: golden root on another volume", "root", root, "golden_root", golden)
		return none, nil
	}
	switch err := p.probe(ctx, root); {
	case err == nil:
		return Capabilities{CopyOnWrite: CowBlock, SparseSupported: true, Filesystem: vol.Filesystem}, nil
	case errors.Is(err, ErrCloneUnsupported):
		p.log.Info("cow_clone unavailable: probe clone unsupported", "root", root,
			"filesystem", vol.Filesystem, "err", err)
		return none, nil
	default:
		return Capabilities{}, fmt.Errorf("cow_clone capabilities: probe clone in %s: %w", root, err)
	}
}

// probe clones a random 64 KiB file in root through the Cloner and checks
// the clone holds the same bytes. Both files are always removed.
func (p *CowCloneProvider) probe(ctx context.Context, root string) (err error) {
	data := make([]byte, probeSize)
	if _, err := rand.Read(data); err != nil {
		return fmt.Errorf("random probe data: %w", err)
	}
	src := filepath.Join(root, probePrefix+ulid.Make().String())
	dst := src + ".clone"
	defer func() {
		for _, f := range []string{dst, src} {
			if rerr := os.Remove(f); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
				err = errors.Join(err, fmt.Errorf("remove probe file: %w", rerr))
			}
		}
	}()
	f, err := os.OpenFile(src, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: a fresh name directly under the workspace root
	if err != nil {
		return fmt.Errorf("write probe file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close() // the write error is what matters
		return fmt.Errorf("write probe file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("write probe file: %w", err)
	}
	if err := p.cloner.CloneFile(ctx, src, dst); err != nil {
		return err
	}
	got, err := os.ReadFile(dst) //nolint:gosec // G304: the probe clone just created under the workspace root
	if err != nil {
		return fmt.Errorf("read probe clone: %w", err)
	}
	if !bytes.Equal(got, data) {
		return errors.New("probe clone differs from its source")
	}
	return nil
}

// Create makes a cow_clone workspace at spec.Path (SCOPE.md §6.3, §6.4):
//
//  1. acquire the golden tree of spec.BaseSHA (built on first use) and hold
//     its in_use reference for the duration of the call;
//  2. git worktree add --no-checkout -b spec.Branch spec.Path spec.BaseSHA,
//     then the per-worktree config (core.autocrlf=false, core.longpaths=true
//     on Windows, §7);
//  3. clone every golden file into the worktree with 16 parallel workers,
//     creating directories and replicating symlinks; nothing is copied.
//     With spec.Sparse.Enabled only the files git's cone mode would check
//     out for the sparse set are cloned (§6.6);
//  4. check the number of files against the golden's file count (without
//     sparse; a sparse clone can only be checked not to exceed it);
//  5. seed the worktree's own index from the golden index (§23 decision
//     39);
//  6. with sparse, `sparse-checkout set` on that index (§23 decision 39):
//     it marks the unmatched entries skip-worktree, and as their files
//     were never cloned there is nothing to delete;
//  7. check that HEAD is spec.BaseSHA.
//
// spec.Path must not exist; an existing path is [ErrPathExists] and is never
// touched. An existing branch is [ErrBranchExists]. Without block clone
// under the workspace root it fails with [ErrCloneUnsupported] before
// anything is written. Any other failure removes the worktree, the directory
// and the admin entry on a detached context; the task branch is the
// Manager's to delete.
func (p *CowCloneProvider) Create(ctx context.Context, spec WorkspaceSpec) (_ *Workspace, err error) {
	start := time.Now()
	if err := p.ready(); err != nil {
		return nil, err
	}
	if _, serr := os.Lstat(spec.Path); serr == nil {
		return nil, fmt.Errorf("%w: %s", ErrPathExists, spec.Path)
	} else if !errors.Is(serr, fs.ErrNotExist) {
		return nil, fmt.Errorf("cow_clone create: %w", serr)
	}
	caps, err := p.Capabilities(ctx, filepath.Dir(spec.Path))
	if err != nil {
		return nil, err
	}
	if caps.CopyOnWrite != CowBlock {
		return nil, fmt.Errorf("cow_clone create under %s (%s): %w", filepath.Dir(spec.Path), caps.Filesystem, ErrCloneUnsupported)
	}
	g, release, err := p.golden.Acquire(ctx, spec.ProjectID, spec.RepoDir, spec.BaseSHA)
	if err != nil {
		return nil, fmt.Errorf("cow_clone create: %w", err)
	}
	defer release()

	defer func() {
		if err != nil {
			err = errors.Join(err, p.wt.compensate(ctx, spec.RepoDir, spec.Path))
		}
	}()
	err = p.git.WorktreeAdd(ctx, spec.RepoDir, spec.Path, git.WorktreeAddOptions{
		NewBranch: spec.Branch, Commitish: spec.BaseSHA, NoCheckout: true,
	})
	if err != nil {
		if branchExists(err) {
			return nil, fmt.Errorf("%w: %q: %w", ErrBranchExists, spec.Branch, err)
		}
		return nil, fmt.Errorf("cow_clone create: %w", err)
	}
	if err = p.wt.configure(ctx, spec.Path); err != nil {
		return nil, err
	}
	var keep func(rel string, isDir bool) bool
	var set []string
	if spec.Sparse.Enabled {
		if set, err = SparseSet(spec.Sparse.Paths); err != nil {
			return nil, fmt.Errorf("cow_clone create: %w", err)
		}
		keep = newConeMatcher(set).Match
	}
	files, err := p.cloneTree(ctx, g.Dir, spec.Path, keep)
	if err != nil {
		return nil, fmt.Errorf("cow_clone create: clone golden tree %s: %w", g.BaseSHA, err)
	}
	if keep == nil && files != g.FileCount || keep != nil && files > g.FileCount {
		return nil, fmt.Errorf("cow_clone create: cloned %d files, the golden tree %s has %d", files, g.BaseSHA, g.FileCount)
	}
	if err = p.seeder.Seed(ctx, spec.Path, g, spec.BaseSHA); err != nil {
		return nil, fmt.Errorf("cow_clone create: seed index: %w", err)
	}
	if keep != nil {
		if err = p.git.SparseSet(ctx, spec.Path, set); err != nil {
			return nil, fmt.Errorf("cow_clone create: sparse set: %w", err)
		}
	}
	head, err := p.git.Head(ctx, spec.Path)
	if err != nil {
		return nil, fmt.Errorf("cow_clone create: %w", err)
	}
	if head != spec.BaseSHA {
		return nil, fmt.Errorf("cow_clone create: HEAD is %s, want %s", head, spec.BaseSHA)
	}
	p.log.Info("cow_clone workspace cloned", "project_id", spec.ProjectID, "task_id", spec.TaskID,
		"workspace_id", spec.ID, "base_sha", spec.BaseSHA, "file_count", files,
		"filesystem", caps.Filesystem, "clone_ms", time.Since(start).Milliseconds())
	return &Workspace{
		ID: spec.ID, ProjectID: spec.ProjectID, TaskID: spec.TaskID, SessionID: spec.SessionID,
		Provider: ProviderCowClone, Path: spec.Path, Branch: spec.Branch, BaseSHA: spec.BaseSHA,
		Caps:   caps,
		Sparse: SparseSpec{Enabled: spec.Sparse.Enabled, Paths: append([]string(nil), spec.Sparse.Paths...)},
		Ports:  spec.Ports,
	}, nil
}

// cloneTree replicates the golden tree src into dst: one walker creates the
// directories and symlinks and feeds the regular files to cloneWorkers
// goroutines calling the Cloner. The first error cancels the rest. It
// returns the number of regular files and symlinks created, and waits for
// every worker before returning.
//
// A non-nil keep (slash-separated path relative to src, and whether it is a
// directory) selects what is replicated: a directory it rejects is skipped
// with its subtree, a file it rejects is not cloned.
func (p *CowCloneProvider) cloneTree(ctx context.Context, src, dst string,
	keep func(rel string, isDir bool) bool) (int, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan [2]string, cloneQueue)
	errc := make(chan error, 1)
	var wg sync.WaitGroup
	for range cloneWorkers {
		wg.Go(func() {
			for j := range jobs {
				if ctx.Err() != nil {
					continue // drain: the walk is being abandoned
				}
				if err := p.cloner.CloneFile(ctx, j[0], j[1]); err != nil {
					select {
					case errc <- err:
					default:
					}
					cancel()
				}
			}
		})
	}
	files := 0
	walkErr := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if isGitDirName(rel) {
			// A golden tree holds files only (§6.1): a .git here would share
			// a repository between workspaces.
			return fmt.Errorf("golden tree holds %s", path)
		}
		if keep != nil && !keep(filepath.ToSlash(rel), d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		switch t := d.Type(); {
		case t.IsDir():
			return os.MkdirAll(target, 0o750)
		case t&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			// target is <dst>/<rel> with rel from the golden walk; dst is the
			// worktree this create just added, which nothing else writes.
			if err := os.Symlink(link, target); err != nil { //nolint:gosec // G122: see above
				return err
			}
			files++
			return nil
		case t.IsRegular():
			select {
			case jobs <- [2]string{path, target}:
				files++
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		default:
			return fmt.Errorf("unexpected file type %v at %s", t, path)
		}
	})
	close(jobs)
	wg.Wait()
	select {
	case err := <-errc:
		return 0, err
	default:
	}
	if walkErr != nil {
		return 0, walkErr
	}
	return files, nil
}

// isGitDirName reports whether the first element of the relative path rel
// is .git, in any case.
func isGitDirName(rel string) bool {
	first, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
	return strings.EqualFold(first, ".git")
}

// Inspect is not implemented until the shared inspect (S1-T13) lands.
func (*CowCloneProvider) Inspect(context.Context, string) (*WorkspaceStatus, error) {
	return nil, errors.ErrUnsupported
}

// Destroy is not implemented until the shared destroy (S1-T14) lands.
func (*CowCloneProvider) Destroy(context.Context, string, DestroyOptions) error {
	return errors.ErrUnsupported
}

// expandScope widens the sparse scope of w by add. While HEAD is still the
// base commit, the newly matching files are cloned from the golden tree
// first, so `sparse-checkout set` finds them up to date and writes no full
// copies; after a commit the golden no longer describes the worktree and git
// materializes them (correctness first).
func (p *CowCloneProvider) expandScope(ctx context.Context, w Workspace, add []string) error {
	if err := p.ready(); err != nil {
		return err
	}
	cur, err := p.git.SparseList(ctx, w.Path)
	if err != nil {
		return fmt.Errorf("cow_clone expand scope: %w", err)
	}
	curSet, err := SparseSet(cur)
	if err != nil {
		return fmt.Errorf("cow_clone expand scope: %w", err)
	}
	newSet, err := SparseSet(append(slices.Clone(cur), add...))
	if err != nil {
		return fmt.Errorf("cow_clone expand scope: %w", err)
	}
	head, err := p.git.Head(ctx, w.Path)
	if err != nil {
		return fmt.Errorf("cow_clone expand scope: %w", err)
	}
	if head == w.BaseSHA {
		if err := p.cloneNewScope(ctx, w, curSet, newSet); err != nil {
			return err
		}
	}
	if err := p.git.SparseSet(ctx, w.Path, newSet); err != nil {
		return fmt.Errorf("cow_clone expand scope: sparse set: %w", err)
	}
	return nil
}

// cloneNewScope clones from the golden tree of w the files that newSet
// matches and curSet does not. The golden reference is held for the call.
func (p *CowCloneProvider) cloneNewScope(ctx context.Context, w Workspace, curSet, newSet []string) error {
	repo, err := p.repoDirOf(ctx, w.Path)
	if err != nil {
		return err
	}
	g, release, err := p.golden.Acquire(ctx, w.ProjectID, repo, w.BaseSHA)
	if err != nil {
		return fmt.Errorf("cow_clone expand scope: %w", err)
	}
	defer release()
	oldM, newM := newConeMatcher(curSet), newConeMatcher(newSet)
	_, err = p.cloneTree(ctx, g.Dir, w.Path, func(rel string, isDir bool) bool {
		return newM.Match(rel, isDir) && (isDir || !oldM.Match(rel, isDir))
	})
	if err != nil {
		return fmt.Errorf("cow_clone expand scope: clone from golden tree %s: %w", g.BaseSHA, err)
	}
	return nil
}

// repoDirOf returns the main repository of worktree wt, derived from the
// shared git config the worktree reads: <repo>/.git/config. The golden cache
// needs it only to rebuild a golden tree that was collected meanwhile.
func (p *CowCloneProvider) repoDirOf(ctx context.Context, wt string) (string, error) {
	cfg, err := p.git.GitPath(ctx, wt, "config")
	if err != nil {
		return "", fmt.Errorf("cow_clone expand scope: locate repository: %w", err)
	}
	common := filepath.Dir(cfg)
	if filepath.Base(common) == ".git" {
		return filepath.Dir(common), nil
	}
	return common, nil
}
