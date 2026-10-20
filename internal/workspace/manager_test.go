package workspace_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/config"
	"github.com/deverity/cowflow/internal/domain"
	"github.com/deverity/cowflow/internal/git"
	"github.com/deverity/cowflow/internal/platform"
	"github.com/deverity/cowflow/internal/workspace"
	"github.com/deverity/cowflow/internal/workspace/workspacetest"
	"github.com/deverity/cowflow/test/harness"
)

const baseSHA = "0123456789abcdef0123456789abcdef01234567"

// testProvider stands in for the real providers (S1-T05, S1-T10): Create
// adds a worktree through FakeGit, creates the directory and clones every
// file of src through Cloner, so a FakeCloner fault fails it midway.
type testProvider struct {
	name   string
	caps   func(ctx context.Context, root string) (workspace.Capabilities, error)
	git    *workspacetest.FakeGit
	cloner *workspacetest.FakeCloner
	src    string
	// hook, when set, runs at the start of Create and may fail it.
	hook func(ctx context.Context, spec workspace.WorkspaceSpec) error

	mu    sync.Mutex
	specs []workspace.WorkspaceSpec
}

func (p *testProvider) Name() string { return p.name }

func (p *testProvider) Capabilities(ctx context.Context, root string) (workspace.Capabilities, error) {
	return p.caps(ctx, root)
}

func (p *testProvider) Create(ctx context.Context, spec workspace.WorkspaceSpec) (*workspace.Workspace, error) {
	p.mu.Lock()
	p.specs = append(p.specs, spec)
	p.mu.Unlock()
	if p.hook != nil {
		if err := p.hook(ctx, spec); err != nil {
			return nil, err
		}
	}
	if err := p.git.WorktreeAdd(ctx, spec.RepoDir, spec.Path, git.WorktreeAddOptions{
		NewBranch: spec.Branch, Commitish: spec.BaseSHA, NoCheckout: true,
	}); err != nil {
		return nil, err
	}
	if err := os.Mkdir(spec.Path, 0o750); err != nil {
		return nil, err
	}
	if p.cloner != nil && p.src != "" {
		entries, err := os.ReadDir(p.src)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if err := p.cloner.CloneFile(ctx, filepath.Join(p.src, e.Name()), filepath.Join(spec.Path, e.Name())); err != nil {
				return nil, err
			}
		}
	}
	caps, err := p.caps(ctx, spec.Path)
	if err != nil {
		return nil, err
	}
	return &workspace.Workspace{
		ID: spec.ID, Provider: p.name, Path: spec.Path, Branch: spec.Branch, BaseSHA: spec.BaseSHA,
		Caps: caps, Sparse: spec.Sparse, Ports: spec.Ports,
	}, nil
}

func (p *testProvider) Inspect(context.Context, string) (*workspace.WorkspaceStatus, error) {
	return nil, errors.ErrUnsupported
}

func (p *testProvider) Destroy(context.Context, string, workspace.DestroyOptions) error {
	return errors.ErrUnsupported
}

func (p *testProvider) Specs() []workspace.WorkspaceSpec {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.specs)
}

// volumeCaps reports the filesystem of root with the given copy-on-write
// kind, as a provider probing FakeVolumes would.
func volumeCaps(vols workspace.Volumes, kind workspace.CowKind) func(context.Context, string) (workspace.Capabilities, error) {
	return func(ctx context.Context, root string) (workspace.Capabilities, error) {
		v, err := vols.VolumeOf(ctx, root)
		if err != nil {
			return workspace.Capabilities{}, err
		}
		return workspace.Capabilities{CopyOnWrite: kind, SparseSupported: true, Filesystem: v.Filesystem}, nil
	}
}

// recordingSink is an EventSink that keeps every event.
type recordingSink struct {
	mu     sync.Mutex
	events []domain.WorkspaceEvent
	err    error
}

func (s *recordingSink) Emit(ev domain.WorkspaceEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
	return s.err
}

func (s *recordingSink) Events() []domain.WorkspaceEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.events)
}

// envWriterFunc adapts a function to workspace.EnvWriter.
type envWriterFunc func(ctx context.Context, w *workspace.Workspace) error

func (f envWriterFunc) Write(ctx context.Context, w *workspace.Workspace) error { return f(ctx, w) }

// fixture is a Manager over fakes, rooted in a temporary directory that
// FakeVolumes reports as one ext4 volume.
type fixture struct {
	base, root, repo string
	cfg              *config.Config
	store            *workspacetest.MemStore
	git              *workspacetest.FakeGit
	vols             *workspacetest.FakeVolumes
	dev              *harness.SimDevDrive
	events           *recordingSink
	wt               *testProvider
	cow              *testProvider // nil unless withCow
	env              workspace.EnvWriter
}

// realDir returns a fresh temporary directory with its links resolved
// (macOS puts t.TempDir under the /var -> /private/var link).
func realDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return d
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	base := realDir(t)
	f := &fixture{
		base:   base,
		root:   filepath.Join(base, "ws"),
		repo:   filepath.Join(base, "repo"),
		cfg:    config.Default(),
		store:  workspacetest.NewMemStore(domain.SystemClock{}),
		git:    &workspacetest.FakeGit{},
		vols:   workspacetest.NewFakeVolumes(platform.Volume{Filesystem: "ext4", ID: "vol-base", MountPath: base}),
		dev:    &harness.SimDevDrive{},
		events: &recordingSink{},
	}
	require.NoError(t, os.Mkdir(f.root, 0o750))
	require.NoError(t, os.Mkdir(f.repo, 0o750))
	f.cfg.Workspace.Root = f.root
	f.cfg.Workspace.Provider = workspace.ProviderAuto
	f.wt = &testProvider{name: workspace.ProviderGitWorktree, caps: volumeCaps(f.vols, workspace.CowNone), git: f.git}
	return f
}

func (f *fixture) withCow() *fixture {
	f.cow = &testProvider{name: workspace.ProviderCowClone, caps: volumeCaps(f.vols, workspace.CowBlock), git: f.git}
	return f
}

func (f *fixture) manager() *workspace.Manager {
	d := workspace.Deps{
		Config: f.cfg, Store: f.store, Git: f.git, Volumes: f.vols, DevDrive: f.dev,
		Worktree: f.wt, Env: f.env, Events: f.events,
		Logger: slog.New(slog.DiscardHandler),
	}
	if f.cow != nil {
		d.Cow = f.cow
	}
	return workspace.NewManager(d)
}

func (f *fixture) request(op, slug string) workspace.CreateRequest {
	return workspace.CreateRequest{
		OpID: op, ProjectID: "prj_1", RepoDir: f.repo,
		Task: domain.Task{
			ID: "tsk_" + slug, ProjectID: "prj_1", Slug: slug, Branch: "feature/" + slug,
			Base: "develop", BaseSHA: baseSHA, Sparse: true, State: domain.TaskStateCreated,
		},
	}
}

func (f *fixture) rows(t *testing.T) []domain.WorkspaceRecord {
	t.Helper()
	rows, err := f.store.ListWorkspaces(context.Background(), domain.WorkspaceFilter{})
	require.NoError(t, err)
	return rows
}

// assertNoLiveCreate checks the acceptance rule: after a failed create no
// row is ready or creating.
func (f *fixture) assertNoLiveCreate(t *testing.T) {
	t.Helper()
	for _, r := range f.rows(t) {
		assert.NotContains(t, []domain.WorkspaceState{domain.WorkspaceReady, domain.WorkspaceCreating}, r.State,
			"workspace %s", r.ID)
	}
}

func TestCreateReady(t *testing.T) {
	f := newFixture(t)
	f.cfg.Workspace.SparseByIntent = true
	m := f.manager()
	req := f.request("op-1", "feat-a")
	req.Task.Paths = []string{"svc/api"}
	req.SessionID = ""

	w, sel, err := m.Create(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, workspace.ProviderGitWorktree, sel.Provider.Name())
	assert.Contains(t, sel.Warning, "no copy-on-write")
	assert.Equal(t, domain.WorkspaceReady, w.State)
	assert.Equal(t, filepath.Join(f.root, "feat-a"), w.Path)
	assert.Equal(t, "feature/feat-a", w.Branch)
	assert.Equal(t, baseSHA, w.BaseSHA)
	assert.Equal(t, workspace.PortRange{Start: f.cfg.Workspace.PortRangeStart,
		End: f.cfg.Workspace.PortRangeStart + f.cfg.Workspace.PortRangePerWorkspace - 1}, w.Ports)
	assert.Equal(t, workspace.SparseSpec{Enabled: true, Paths: []string{"svc/api"}}, w.Sparse)

	rows := f.rows(t)
	require.Len(t, rows, 1)
	assert.Equal(t, w.ID, rows[0].ID)
	assert.Equal(t, domain.WorkspaceReady, rows[0].State)
	assert.Equal(t, "git_worktree", rows[0].Provider)
	assert.Equal(t, "none", rows[0].CowKind)
	assert.Equal(t, "ext4", rows[0].Filesystem)
	assert.Empty(t, f.store.Events(), "ready emits no event")
	assert.Empty(t, f.events.Events())

	specs := f.wt.Specs()
	require.Len(t, specs, 1)
	assert.Equal(t, w.ID, specs[0].ID)
	assert.Equal(t, "op-1", specs[0].OpID)
	assert.Equal(t, f.repo, specs[0].RepoDir)
}

func TestCreateSparseOnlyWhenAllAsk(t *testing.T) {
	tests := []struct {
		name           string
		byIntent, task bool
		paths          []string
		want           bool
	}{
		{"all", true, true, []string{"a"}, true},
		{"config off", false, true, []string{"a"}, false},
		{"task off", true, false, []string{"a"}, false},
		{"no paths", true, true, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.cfg.Workspace.SparseByIntent = tt.byIntent
			req := f.request("op", "s")
			req.Task.Sparse, req.Task.Paths = tt.task, tt.paths
			w, _, err := f.manager().Create(context.Background(), req)
			require.NoError(t, err)
			assert.Equal(t, tt.want, w.Sparse.Enabled)
			assert.Equal(t, tt.want, f.wt.Specs()[0].Sparse.Enabled)
		})
	}
}

func TestCreateRefusesProtectedBranch(t *testing.T) {
	for _, branch := range []string{"main", "develop"} {
		t.Run(branch, func(t *testing.T) {
			f := newFixture(t)
			req := f.request("op", "x")
			req.Task.Branch = branch
			_, _, err := f.manager().Create(context.Background(), req)
			require.ErrorIs(t, err, workspace.ErrProtectedBranch)
			assert.Empty(t, f.rows(t))
			assert.Empty(t, f.wt.Specs())
			assertEmptyDir(t, f.root)
		})
	}
}

func TestCreateRejectsBadBranchName(t *testing.T) {
	f := newFixture(t)
	f.git.Errs = map[string]error{"CheckBranchName": git.ErrInvalidRefName}
	_, _, err := f.manager().Create(context.Background(), f.request("op", "x"))
	require.ErrorIs(t, err, git.ErrInvalidRefName)
	assert.Empty(t, f.rows(t))
}

func TestCreateRejectsPathEscape(t *testing.T) {
	for _, slug := range []string{"..", "../escape", "a/../../x", "/etc", `C:\x`, "Upper"} {
		t.Run(slug, func(t *testing.T) {
			f := newFixture(t)
			_, _, err := f.manager().Create(context.Background(), f.request("op", slug))
			require.ErrorIs(t, err, domain.ErrInvalidSlug)
			assert.Empty(t, f.rows(t))
			assert.Empty(t, f.wt.Specs())
		})
	}

	t.Run("symlink out of root", func(t *testing.T) {
		f := newFixture(t)
		outside := filepath.Join(f.base, "outside")
		require.NoError(t, os.Mkdir(outside, 0o750))
		symlinkOrSkip(t, outside, filepath.Join(f.root, "evil"))

		_, _, err := f.manager().Create(context.Background(), f.request("op", "evil"))
		require.ErrorIs(t, err, platform.ErrOutsideRoot)
		assert.Empty(t, f.rows(t))
		assert.Empty(t, f.wt.Specs())
		assertEmptyDir(t, outside)
	})

	t.Run("symlink inside root", func(t *testing.T) {
		f := newFixture(t)
		require.NoError(t, os.Mkdir(filepath.Join(f.root, "target"), 0o750))
		symlinkOrSkip(t, filepath.Join(f.root, "target"), filepath.Join(f.root, "alias"))

		_, _, err := f.manager().Create(context.Background(), f.request("op", "alias"))
		require.ErrorIs(t, err, workspace.ErrPathExists)
		assert.Empty(t, f.rows(t))
	})

	t.Run("symlink root", func(t *testing.T) {
		// A root that is itself a link is canonicalized: the workspace is
		// created under the link's target and still confined.
		f := newFixture(t)
		link := filepath.Join(f.base, "root-link")
		symlinkOrSkip(t, f.root, link)
		f.cfg.Workspace.Root = link
		w, _, err := f.manager().Create(context.Background(), f.request("op", "via-link"))
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(f.root, "via-link"), w.Path)
	})
}

func TestCreateRefusesExistingPathAndBranch(t *testing.T) {
	t.Run("path", func(t *testing.T) {
		f := newFixture(t)
		require.NoError(t, os.Mkdir(filepath.Join(f.root, "taken"), 0o750))
		_, _, err := f.manager().Create(context.Background(), f.request("op", "taken"))
		require.ErrorIs(t, err, workspace.ErrPathExists)
		assert.Contains(t, err.Error(), "destroy it first")
		assert.Empty(t, f.rows(t))
	})
	t.Run("branch", func(t *testing.T) {
		f := newFixture(t)
		f.git.Branches = map[string]string{"feature/b": baseSHA}
		_, _, err := f.manager().Create(context.Background(), f.request("op", "b"))
		require.ErrorIs(t, err, workspace.ErrBranchExists)
		assert.Empty(t, f.rows(t))
		assert.Equal(t, baseSHA, f.git.Branches["feature/b"], "an existing branch is never touched")
	})
}

func TestCreateRequiresRoot(t *testing.T) {
	f := newFixture(t)
	f.cfg.Workspace.Root = ""
	_, _, err := f.manager().Create(context.Background(), f.request("op", "x"))
	require.ErrorIs(t, err, workspace.ErrRootNotSet)
}

func TestCreateRejectsBaseMismatch(t *testing.T) {
	f := newFixture(t)
	f.git.Commits = map[string]string{baseSHA: "fedcba9876543210fedcba9876543210fedcba98"}
	_, _, err := f.manager().Create(context.Background(), f.request("op", "x"))
	require.Error(t, err)
	assert.Empty(t, f.rows(t))
}

func TestCreateCompensatesOnProviderError(t *testing.T) {
	f := newFixture(t)
	f.wt.src = filepath.Join(f.base, "src")
	require.NoError(t, os.Mkdir(f.wt.src, 0o750))
	for i := range 10 {
		require.NoError(t, os.WriteFile(filepath.Join(f.wt.src, fmt.Sprintf("f%02d", i)), []byte("x"), 0o600))
	}
	f.wt.cloner = &workspacetest.FakeCloner{FailAfter: 5}
	m := f.manager()

	_, _, err := m.Create(context.Background(), f.request("op-1", "boom"))
	require.ErrorIs(t, err, syscall.ENOSPC)
	assert.Equal(t, 6, f.wt.cloner.Calls())

	rows := f.rows(t)
	require.Len(t, rows, 1)
	assert.Equal(t, domain.WorkspaceFailed, rows[0].State)
	assert.Contains(t, rows[0].LastError, "no space left")
	f.assertNoLiveCreate(t)
	assert.NoDirExists(t, filepath.Join(f.root, "boom"))
	assert.NotContains(t, f.git.Branches, "feature/boom", "the branch this create made is deleted")
	wts, err := f.git.WorktreeList(context.Background(), f.repo)
	require.NoError(t, err)
	assert.Empty(t, wts)
	assert.Positive(t, f.git.Count("WorktreePrune"))

	// The failed row freed its port slot.
	f.wt.cloner = &workspacetest.FakeCloner{}
	w, _, err := m.Create(context.Background(), f.request("op-2", "ok"))
	require.NoError(t, err)
	assert.Equal(t, f.cfg.Workspace.PortRangeStart, w.Ports.Start)

	// The same op id does not retry a failed create.
	_, _, err = m.Create(context.Background(), f.request("op-1", "boom"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "new op id")
}

func TestCreateCompensatesWhenContextCanceled(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.wt.hook = func(_ context.Context, spec workspace.WorkspaceSpec) error {
		// The provider got as far as the worktree and the directory.
		if err := f.git.WorktreeAdd(ctx, spec.RepoDir, spec.Path, git.WorktreeAddOptions{
			NewBranch: spec.Branch, Commitish: spec.BaseSHA,
		}); err != nil {
			return err
		}
		if err := os.Mkdir(spec.Path, 0o750); err != nil {
			return err
		}
		cancel()
		return ctx.Err()
	}
	_, _, err := f.manager().Create(ctx, f.request("op", "cancel"))
	require.ErrorIs(t, err, context.Canceled)
	f.assertNoLiveCreate(t)
	assert.Equal(t, domain.WorkspaceFailed, f.rows(t)[0].State)
	assert.NoDirExists(t, filepath.Join(f.root, "cancel"))
	assert.NotContains(t, f.git.Branches, "feature/cancel")
}

func TestCreateCompensatesOnEnvWriterError(t *testing.T) {
	f := newFixture(t)
	f.env = envWriterFunc(func(context.Context, *workspace.Workspace) error { return errors.New("disk full") })
	_, _, err := f.manager().Create(context.Background(), f.request("op", "env"))
	require.ErrorContains(t, err, "disk full")
	f.assertNoLiveCreate(t)
	assert.NoDirExists(t, filepath.Join(f.root, "env"))
}

// readyFailStore fails every move to ready.
type readyFailStore struct{ *workspacetest.MemStore }

func (s readyFailStore) SetWorkspaceState(ctx context.Context, id string, to domain.WorkspaceState, c domain.StateChange) error {
	if to == domain.WorkspaceReady {
		return errors.New("database is locked")
	}
	return s.MemStore.SetWorkspaceState(ctx, id, to, c)
}

func TestCreateCompensatesOnReadyError(t *testing.T) {
	f := newFixture(t)
	m := workspace.NewManager(workspace.Deps{
		Config: f.cfg, Store: readyFailStore{f.store}, Git: f.git, Volumes: f.vols, DevDrive: f.dev,
		Worktree: f.wt, Logger: slog.New(slog.DiscardHandler),
	})
	_, _, err := m.Create(context.Background(), f.request("op", "rdy"))
	require.ErrorContains(t, err, "database is locked")
	f.assertNoLiveCreate(t)
	assert.Equal(t, domain.WorkspaceFailed, f.rows(t)[0].State)
	assert.NoDirExists(t, filepath.Join(f.root, "rdy"))
	assert.NotContains(t, f.git.Branches, "feature/rdy")
}

func TestCreateReportsCompensationFailure(t *testing.T) {
	f := newFixture(t)
	f.env = envWriterFunc(func(context.Context, *workspace.Workspace) error { return errors.New("env failed") })
	f.git.Errs = map[string]error{"WorktreePrune": errors.New("prune failed")}
	_, _, err := f.manager().Create(context.Background(), f.request("op", "cf"))
	require.ErrorContains(t, err, "env failed")
	assert.ErrorContains(t, err, "prune failed")
	f.assertNoLiveCreate(t)
}

func TestCreateNeverLeavesPartialReady(t *testing.T) {
	// Every step after the insert fails once in turn; none leaves a ready or
	// creating row, a directory or a branch behind.
	steps := []string{"WorktreeAdd", "provider", "env"}
	for _, step := range steps {
		t.Run(step, func(t *testing.T) {
			f := newFixture(t)
			injected := errors.New("injected " + step)
			switch step {
			case "WorktreeAdd":
				f.git.Errs = map[string]error{"WorktreeAdd": injected}
			case "provider":
				f.wt.cloner = &workspacetest.FakeCloner{Err: injected}
				f.wt.src = filepath.Join(f.base, "src")
				require.NoError(t, os.Mkdir(f.wt.src, 0o750))
				require.NoError(t, os.WriteFile(filepath.Join(f.wt.src, "a"), nil, 0o600))
			case "env":
				f.env = envWriterFunc(func(context.Context, *workspace.Workspace) error { return injected })
			}
			_, _, err := f.manager().Create(context.Background(), f.request("op", "p"))
			require.ErrorIs(t, err, injected)
			f.assertNoLiveCreate(t)
			assert.NoDirExists(t, filepath.Join(f.root, "p"))
			assert.NotContains(t, f.git.Branches, "feature/p")
		})
	}
}

func TestCreateIdempotentOpID(t *testing.T) {
	f := newFixture(t)
	m := f.manager()
	req := f.request("op-same", "idem")
	w1, _, err := m.Create(context.Background(), req)
	require.NoError(t, err)
	w2, sel, err := m.Create(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, w1.ID, w2.ID)
	assert.Equal(t, w1.Path, w2.Path)
	assert.Equal(t, w1.Ports, w2.Ports)
	assert.Equal(t, domain.WorkspaceReady, w2.State)
	assert.Equal(t, workspace.ProviderGitWorktree, sel.Provider.Name())
	assert.Len(t, f.wt.Specs(), 1, "the provider runs once")
	assert.Len(t, f.rows(t), 1)
}

func TestCreatePendingMountNeverWritesNTFS(t *testing.T) {
	f := newFixture(t)
	// The Dev Drive is configured on a folder of the NTFS volume, then the
	// machine reboots without auto-attach: the folder is still there.
	mount := filepath.Join(f.base, "X")
	require.NoError(t, os.Mkdir(mount, 0o750))
	f.vols.Set(platform.Volume{Filesystem: "ntfs", ID: "vol-c", MountPath: f.base})
	spec := platform.DevDriveSpec{VHDXPath: filepath.Join(f.base, "cowflow.vhdx"), Mount: mount}
	_, err := f.dev.Provision(context.Background(), spec)
	require.NoError(t, err)
	f.dev.SimulateReboot()
	f.cfg.DevDrive.Enabled, f.cfg.DevDrive.Path, f.cfg.DevDrive.Mount = true, spec.VHDXPath, spec.Mount
	f.cfg.Workspace.Root = filepath.Join(mount, "ws")
	f = f.withCow()
	m := f.manager()

	_, _, err = m.Create(context.Background(), f.request("op-pm", "pm"))
	require.ErrorIs(t, err, workspace.ErrPendingMount)

	assertEmptyDir(t, mount)
	assert.Empty(t, f.wt.Specs())
	assert.Empty(t, f.cow.Specs())
	assert.Zero(t, f.git.Count("WorktreeAdd"))
	rows := f.rows(t)
	require.Len(t, rows, 1)
	assert.Equal(t, domain.WorkspacePendingMount, rows[0].State)
	assert.Equal(t, filepath.Join(mount, "ws", "pm"), rows[0].Path)
	evs := f.events.Events()
	require.Len(t, evs, 1)
	assert.Equal(t, domain.EventWorkspacePendingMount, evs[0].Type)
	assert.Equal(t, rows[0].ID, evs[0].WorkspaceID)
	assert.Equal(t, "tsk_pm", evs[0].TaskID)
	assert.Contains(t, evs[0].Payload["reason"], "not attached")

	// A retry while still unmounted changes nothing and emits nothing new.
	_, _, err = m.Create(context.Background(), f.request("op-pm", "pm"))
	require.ErrorIs(t, err, workspace.ErrPendingMount)
	assert.Len(t, f.rows(t), 1)
	assert.Len(t, f.events.Events(), 1)
	assertEmptyDir(t, mount)
}

func TestCreatePendingMountWhenRootOffDevDrive(t *testing.T) {
	f := newFixture(t)
	mount := filepath.Join(f.base, "X")
	require.NoError(t, os.Mkdir(mount, 0o750))
	f.vols.Set(platform.Volume{Filesystem: "ntfs", ID: "vol-c", MountPath: f.base})
	f.vols.Set(platform.Volume{Filesystem: "refs", ID: "vol-dev", MountPath: mount, DevDrive: true})
	spec := platform.DevDriveSpec{VHDXPath: filepath.Join(f.base, "cowflow.vhdx"), Mount: mount}
	_, err := f.dev.Provision(context.Background(), spec)
	require.NoError(t, err)
	f.cfg.DevDrive.Enabled, f.cfg.DevDrive.Path, f.cfg.DevDrive.Mount = true, spec.VHDXPath, spec.Mount
	// Mounted, but the root is a folder of the NTFS volume.
	f.cfg.Workspace.Root = f.root

	_, _, err = f.manager().Create(context.Background(), f.request("op", "off"))
	require.ErrorIs(t, err, workspace.ErrPendingMount)
	assert.Contains(t, err.Error(), "vol-c")
	assertEmptyDir(t, f.root)
	assert.Equal(t, domain.WorkspacePendingMount, f.rows(t)[0].State)
}

func TestCreateResumesPendingMount(t *testing.T) {
	f := newFixture(t)
	mount := filepath.Join(f.base, "X")
	require.NoError(t, os.Mkdir(mount, 0o750))
	root := filepath.Join(mount, "ws")
	require.NoError(t, os.Mkdir(root, 0o750))
	f.vols.Set(platform.Volume{Filesystem: "ntfs", ID: "vol-c", MountPath: f.base})
	spec := platform.DevDriveSpec{VHDXPath: filepath.Join(f.base, "cowflow.vhdx"), Mount: mount}
	_, err := f.dev.Provision(context.Background(), spec)
	require.NoError(t, err)
	f.dev.SimulateReboot()
	f.cfg.DevDrive.Enabled, f.cfg.DevDrive.Path, f.cfg.DevDrive.Mount = true, spec.VHDXPath, spec.Mount
	f.cfg.Workspace.Root = root
	f = f.withCow()
	m := f.manager()

	t.Run("explicit provider resumes under the same op id", func(t *testing.T) {
		req := f.request("op-r", "res")
		req.Provider = workspace.ProviderCowClone
		_, _, err := m.Create(context.Background(), req)
		require.ErrorIs(t, err, workspace.ErrPendingMount)
		pending := f.rows(t)[0]

		_, err = f.dev.Attach(context.Background(), spec)
		require.NoError(t, err)
		f.vols.Set(platform.Volume{Filesystem: "refs", ID: "vol-dev", MountPath: mount, DevDrive: true})

		w, sel, err := m.Create(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, pending.ID, w.ID)
		assert.Equal(t, pending.PortStart, w.Ports.Start)
		assert.Equal(t, workspace.ProviderCowClone, sel.Provider.Name())
		assert.Equal(t, workspace.CowBlock, sel.Caps.CopyOnWrite)
		assert.Equal(t, domain.WorkspaceReady, f.rows(t)[0].State)
		assert.Len(t, f.events.Events(), 1, "no second pending_mount event")
	})

	t.Run("auto cannot be recorded on resume yet", func(t *testing.T) {
		f.dev.SimulateReboot()
		req := f.request("op-auto", "auto")
		_, _, err := m.Create(context.Background(), req)
		require.ErrorIs(t, err, workspace.ErrPendingMount)

		_, err = f.dev.Attach(context.Background(), spec)
		require.NoError(t, err)
		_, _, err = m.Create(context.Background(), req)
		require.ErrorContains(t, err, "cannot record the selection")
		for _, r := range f.rows(t) {
			if r.OpID == "op-auto" {
				assert.Equal(t, domain.WorkspaceFailed, r.State)
			}
		}
		assert.NoDirExists(t, filepath.Join(root, "auto"))
	})
}

func TestCreateFailsWhenDevDriveStatusFails(t *testing.T) {
	f := newFixture(t)
	f.cfg.DevDrive.Enabled = true
	m := workspace.NewManager(workspace.Deps{
		Config: f.cfg, Store: f.store, Git: f.git, Volumes: f.vols, Worktree: f.wt,
		DevDrive: statusFunc(func() (platform.DevDriveStatus, error) { return platform.DevDriveStatus{}, platform.ErrUnsupported }),
		Logger:   slog.New(slog.DiscardHandler),
	})
	_, _, err := m.Create(context.Background(), f.request("op", "x"))
	require.ErrorIs(t, err, platform.ErrUnsupported)
	assert.Empty(t, f.rows(t))
	assertEmptyDir(t, f.root)
}

type statusFunc func() (platform.DevDriveStatus, error)

func (s statusFunc) Status(context.Context, platform.DevDriveSpec) (platform.DevDriveStatus, error) {
	return s()
}

func TestCreateExplicitCowWithoutCOWInsertsNoRow(t *testing.T) {
	f := newFixture(t).withCow()
	f.cow.caps = volumeCaps(f.vols, workspace.CowNone) // ext4: no block clone
	req := f.request("op", "cow")
	req.Provider = workspace.ProviderCowClone
	_, _, err := f.manager().Create(context.Background(), req)
	require.ErrorIs(t, err, workspace.ErrCloneUnsupported)
	assert.Empty(t, f.rows(t))
	assertEmptyDir(t, f.root)
}

func TestInspectFailedRowDoesNotProbe(t *testing.T) {
	f := newFixture(t)
	f.git.Errs = map[string]error{"WorktreeAdd": errors.New("boom")}
	m := f.manager()
	_, _, err := m.Create(context.Background(), f.request("op", "insp"))
	require.Error(t, err)
	id := f.rows(t)[0].ID

	st, err := m.Inspect(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, domain.WorkspaceFailed, st.State)
	assert.Empty(t, st.Filesystem, "not probed")
	require.Len(t, st.Warnings, 1)
	assert.Contains(t, st.Warnings[0], "boom")

	_, err = m.Inspect(context.Background(), "wks_missing")
	require.ErrorIs(t, err, workspace.ErrNotFound)
}

func TestListAndExpandScope(t *testing.T) {
	f := newFixture(t)
	m := f.manager()
	w, _, err := m.Create(context.Background(), f.request("op", "l"))
	require.NoError(t, err)
	ws, err := m.List(context.Background(), workspace.ListFilter{ProjectID: "prj_1"})
	require.NoError(t, err)
	require.Len(t, ws, 1)
	assert.Equal(t, w.ID, ws[0].ID)
	assert.Equal(t, domain.WorkspaceReady, ws[0].State)

	ws, err = m.List(context.Background(), workspace.ListFilter{States: []domain.WorkspaceState{domain.WorkspaceFailed}})
	require.NoError(t, err)
	assert.Empty(t, ws)

	require.ErrorIs(t, m.ExpandScope(context.Background(), w.ID, []string{"x"}), errors.ErrUnsupported)
}

func TestDestroyDelegatesToProvider(t *testing.T) {
	f := newFixture(t)
	m := f.manager()
	w, _, err := m.Create(context.Background(), f.request("op", "d"))
	require.NoError(t, err)
	require.ErrorIs(t, m.Destroy(context.Background(), w.ID, workspace.DestroyOptions{}), errors.ErrUnsupported)
	require.ErrorIs(t, m.Destroy(context.Background(), "wks_missing", workspace.DestroyOptions{}), workspace.ErrNotFound)
}

func TestManagerWithoutStoreFails(t *testing.T) {
	m := workspace.NewManager(workspace.Deps{Config: config.Default()})
	_, _, err := m.Create(context.Background(), workspace.CreateRequest{})
	require.Error(t, err)
	_, err = m.List(context.Background(), workspace.ListFilter{})
	require.Error(t, err)
}

func TestLogsDir(t *testing.T) {
	assert.Equal(t, filepath.Join("state", "logs", "wks_01"), workspace.LogsDir("state", "wks_01"))
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

func assertEmptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	assert.Empty(t, names, "nothing may be created under %s", dir)
}
