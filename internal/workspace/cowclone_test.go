package workspace_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

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

// --- fixtures ---

// ccRealDir returns a fresh directory on the block-cloning volume named by
// COWFLOW_TEST_COW_DIR or COWFLOW_TEST_REFS_DIR, and false when neither is
// set.
func ccRealDir(t testing.TB) (string, bool) {
	t.Helper()
	for _, env := range []string{"COWFLOW_TEST_COW_DIR", "COWFLOW_TEST_REFS_DIR"} {
		if root := os.Getenv(env); root != "" {
			dir, err := os.MkdirTemp(root, "cowflow-cowclone-")
			require.NoError(t, err, "%s=%s", env, root)
			t.Cleanup(func() { _ = os.RemoveAll(dir) }) //nolint:gosec // G703: test temp dir; best effort
			dir, err = filepath.EvalSymlinks(dir)
			require.NoError(t, err)
			return dir, true
		}
	}
	return "", false
}

// ccFixture is a real repository, a real golden cache and the cow_clone
// provider. By default the files are cloned by FakeCloner on a FakeVolumes
// btrfs volume; with onRealFS they are block-cloned by the OS primitive
// on the volume of COWFLOW_TEST_COW_DIR / COWFLOW_TEST_REFS_DIR.
type ccFixture struct {
	base, wsRoot string
	repo         *harness.Repo
	client       *git.Client
	cfg          *config.Config
	store        *workspacetest.MemStore
	vols         workspace.Volumes
	cloner       workspace.Cloner
	prov         *workspace.CowCloneProvider
	sha          string
}

// ccFiles is the default tree: nested directories, an empty-ish file and an
// executable.
func ccFiles() map[string]string {
	return map[string]string{
		"a.txt":               "a\n",
		"README.md":           "# demo\n",
		"cmd/app/main.go":     "package main\n\nfunc main() {}\n",
		"internal/x/x.go":     "package x\n",
		"contracts/api.yaml":  "openapi: 3.1.0\n",
		"docs/deep/a/b/c.txt": strings.Repeat("c", 70000),
	}
}

func newCCFixture(t testing.TB, onRealFS bool, cloner workspace.Cloner, files map[string]string) *ccFixture {
	t.Helper()
	var base string
	var vols workspace.Volumes
	if onRealFS {
		dir, ok := ccRealDir(t)
		if !ok {
			t.Skip("set COWFLOW_TEST_COW_DIR or COWFLOW_TEST_REFS_DIR to a directory on a block-cloning volume")
		}
		base, vols, cloner = dir, workspace.PlatformVolumes{}, nil
	} else {
		var err error
		base, err = filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		vols = workspacetest.NewFakeVolumes(platform.Volume{Filesystem: "btrfs", ID: "vol", MountPath: base})
		if cloner == nil {
			cloner = &workspacetest.FakeCloner{}
		}
	}
	f := &ccFixture{
		base:   base,
		wsRoot: filepath.Join(base, "ws"),
		// read-tree --index-output needs the golden root and the
		// repository's git directory on one filesystem.
		repo:  harness.NewRepo(t, harness.WithDir(filepath.Join(base, "repo"))),
		cfg:   config.Default(),
		store: workspacetest.NewMemStore(domain.SystemClock{}),
		vols:  vols,
	}
	require.NoError(t, os.Mkdir(f.wsRoot, 0o750))
	f.cfg.Workspace.Root = f.wsRoot
	f.cfg.Workspace.Provider = workspace.ProviderCowClone
	f.client = git.NewClient(f.repo.Runner)
	if files == nil {
		files = ccFiles()
	}
	f.sha = f.repo.Commit(t, "feat: content", files)
	f.cloner = cloner
	f.prov = f.provider(f.client)
	return f
}

func (f *ccFixture) provider(g workspace.GitOps) *workspace.CowCloneProvider {
	log := slog.New(slog.DiscardHandler)
	golden := workspace.NewGoldenCache(workspace.GoldenDeps{
		Config: f.cfg, Store: f.store, Git: g, Volumes: f.vols, Logger: log,
	})
	return workspace.NewCowCloneProvider(workspace.CowCloneDeps{
		Config: f.cfg, Git: g, Golden: golden, Volumes: f.vols, Cloner: f.cloner, Logger: log,
	})
}

func (f *ccFixture) spec(slug string) workspace.WorkspaceSpec {
	return workspace.WorkspaceSpec{
		ID: "wks_" + slug, OpID: "op-" + slug, ProjectID: "prj_1", TaskID: "tsk_" + slug,
		RepoDir: f.repo.Dir, Path: filepath.Join(f.wsRoot, slug), Branch: "feature/" + slug, BaseSHA: f.sha,
	}
}

func (f *ccFixture) git(t testing.TB, dir string, args ...string) string {
	t.Helper()
	out, err := f.repo.Runner.Run(context.Background(), dir, args...)
	require.NoError(t, err, "git %v", args)
	return string(out)
}

func (f *ccFixture) status(t testing.TB, ws string) string {
	t.Helper()
	return f.git(t, ws, "status", "--porcelain", "--untracked-files=all")
}

func (f *ccFixture) goldenDir() string { return filepath.Join(f.wsRoot, "golden", f.sha) }

// assertGone checks that a failed create left no directory and no worktree
// registration behind.
func (f *ccFixture) assertGone(t testing.TB, slug string) {
	t.Helper()
	path := filepath.Join(f.wsRoot, slug)
	assert.NoDirExists(t, path)
	assert.NotContains(t, f.git(t, f.repo.Dir, "worktree", "list", "--porcelain"), path)
	assert.NoDirExists(t, filepath.Join(f.repo.Dir, ".git", "worktrees", slug), "no admin entry")
}

// ccTree maps every regular file and symlink under dir, except the
// top-level .git file, to its content (or link target) and mode.
func ccTree(t testing.TB, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	require.NoError(t, filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if rel == ".git" {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		var body []byte
		if d.Type()&fs.ModeSymlink != 0 {
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			body = []byte("link:" + target)
		} else if body, err = os.ReadFile(p); err != nil { //nolint:gosec // G304: test tree
			return err
		}
		out[filepath.ToSlash(rel)] = fi.Mode().String() + " " + string(body)
		return nil
	}))
	return out
}

// --- Capabilities ---

// scriptCloner counts its calls and returns err from each (nil: success
// without creating anything).
type scriptCloner struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (c *scriptCloner) CloneFile(_ context.Context, _, _ string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return c.err
}

func (c *scriptCloner) Calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// fsRoot is the root of the file system holding t.TempDir(): "/" on Unix,
// the drive root ("C:\") on Windows, where "/" cleans to `\` and contains
// no drive-letter path. A fake volume mounted there holds every temp dir.
var fsRoot = filepath.VolumeName(os.TempDir()) + string(filepath.Separator)

func capsProvider(t *testing.T, vols workspace.Volumes, cl workspace.Cloner) (*workspace.CowCloneProvider, string) {
	t.Helper()
	root := t.TempDir()
	cfg := config.Default()
	cfg.Workspace.Root = root
	return workspace.NewCowCloneProvider(workspace.CowCloneDeps{
		Config: cfg, Git: &workspacetest.FakeGit{}, Golden: stubGolden{}, Volumes: vols, Cloner: cl,
		Logger: slog.New(slog.DiscardHandler),
	}), root
}

// stubGolden is a GoldenSource handing out one prepared golden tree.
type stubGolden struct {
	g   workspace.Golden
	err error
}

func (s stubGolden) Acquire(context.Context, string, string, string) (workspace.Golden, func(), error) {
	if s.err != nil {
		return workspace.Golden{}, nil, s.err
	}
	return s.g, func() {}, nil
}

func TestCowCloneCapabilitiesBlockAfterProbe(t *testing.T) {
	cl := &workspacetest.FakeCloner{}
	vols := workspacetest.NewFakeVolumes(platform.Volume{Filesystem: "refs", ID: "v", MountPath: fsRoot})
	p, root := capsProvider(t, vols, cl)

	caps, err := p.Capabilities(context.Background(), root)
	require.NoError(t, err)
	assert.Equal(t, workspace.Capabilities{CopyOnWrite: workspace.CowBlock, SparseSupported: true, Filesystem: "refs"}, caps)
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	assert.Empty(t, entries, "both probe files removed")

	again, err := p.Capabilities(context.Background(), root)
	require.NoError(t, err)
	assert.Equal(t, caps, again)
	assert.Equal(t, 1, cl.Calls(), "the probe result is cached per root")
}

func TestCowCloneCapabilitiesNoneWhenCloneUnsupported(t *testing.T) {
	cl := &scriptCloner{err: fmt.Errorf("probe: %w", workspace.ErrCloneUnsupported)}
	vols := workspacetest.NewFakeVolumes(platform.Volume{Filesystem: "ntfs", ID: "v", MountPath: fsRoot})
	p, root := capsProvider(t, vols, cl)

	caps, err := p.Capabilities(context.Background(), root)
	require.NoError(t, err)
	assert.Equal(t, workspace.CowNone, caps.CopyOnWrite)
	assert.Equal(t, "ntfs", caps.Filesystem)
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	assert.Empty(t, entries, "probe file removed")
}

func TestCowCloneCapabilitiesNoneOnOtherVolume(t *testing.T) {
	cl := &scriptCloner{}
	root := t.TempDir()
	golden := t.TempDir()
	vols := workspacetest.NewFakeVolumes(platform.Volume{Filesystem: "refs", ID: "v1", MountPath: root})
	vols.Set(platform.Volume{Filesystem: "refs", ID: "v2", MountPath: golden})
	cfg := config.Default()
	cfg.Workspace.Root = root
	cfg.Workspace.GoldenRoot = golden
	p := workspace.NewCowCloneProvider(workspace.CowCloneDeps{
		Config: cfg, Git: &workspacetest.FakeGit{}, Golden: stubGolden{}, Volumes: vols, Cloner: cl,
		Logger: slog.New(slog.DiscardHandler),
	})

	caps, err := p.Capabilities(context.Background(), root)
	require.NoError(t, err)
	assert.Equal(t, workspace.CowNone, caps.CopyOnWrite, "U-PLAT-4: different volumes, no COW")
	assert.Zero(t, cl.Calls(), "no probe across volumes")
}

func TestCowCloneCapabilitiesProbeErrorNotCached(t *testing.T) {
	cl := &scriptCloner{err: syscall.EIO}
	vols := workspacetest.NewFakeVolumes(platform.Volume{Filesystem: "refs", ID: "v", MountPath: fsRoot})
	p, root := capsProvider(t, vols, cl)

	_, err := p.Capabilities(context.Background(), root)
	require.ErrorIs(t, err, syscall.EIO)
	_, err = p.Capabilities(context.Background(), root)
	require.Error(t, err)
	assert.Equal(t, 2, cl.Calls(), "a failed probe is retried")
}

func TestCowCloneCreateWithoutBlockClone(t *testing.T) {
	g := &workspacetest.FakeGit{}
	root := t.TempDir()
	cfg := config.Default()
	cfg.Workspace.Root = root
	p := workspace.NewCowCloneProvider(workspace.CowCloneDeps{
		Config: cfg, Git: g, Golden: stubGolden{err: errors.New("must not be acquired")},
		Volumes: workspacetest.NewFakeVolumes(platform.Volume{Filesystem: "ext4", ID: "v", MountPath: fsRoot}),
		Cloner:  &scriptCloner{err: workspace.ErrCloneUnsupported},
		Logger:  slog.New(slog.DiscardHandler),
	})
	_, err := p.Create(context.Background(), workspace.WorkspaceSpec{
		ID: "wks_1", ProjectID: "prj_1", RepoDir: root, Path: filepath.Join(root, "a"),
		Branch: "feature/a", BaseSHA: baseSHA,
	})
	require.ErrorIs(t, err, workspace.ErrCloneUnsupported)
	assert.Zero(t, g.Count("WorktreeAdd"), "nothing written")
	assert.NoDirExists(t, filepath.Join(root, "a"))
}

func TestCowCloneNameAndMissingCollaborators(t *testing.T) {
	p := workspace.NewCowCloneProvider(workspace.CowCloneDeps{})
	assert.Equal(t, workspace.ProviderCowClone, p.Name())
	_, err := p.Inspect(context.Background(), "wks_1")
	require.Error(t, err, "no inspector: an error, not a panic")
	_, err = p.Destroy(context.Background(), "wks_1", workspace.DestroyOptions{})
	require.Error(t, err, "no destroyer: an error, not a panic")
	_, err = p.Create(context.Background(), workspace.WorkspaceSpec{})
	require.Error(t, err, "missing dependencies are reported, not a panic")
}

// --- Create over real Git ---

func TestCowCloneMatchesGolden(t *testing.T) {
	for _, onRealFS := range []bool{false, true} {
		t.Run(fmt.Sprintf("real=%v", onRealFS), func(t *testing.T) {
			f := newCCFixture(t, onRealFS, nil, nil)
			w, err := f.prov.Create(context.Background(), f.spec("feat-a"))
			require.NoError(t, err)

			assert.Equal(t, workspace.ProviderCowClone, w.Provider)
			assert.Equal(t, workspace.CowBlock, w.Caps.CopyOnWrite)
			assert.Equal(t, ccTree(t, f.goldenDir()), ccTree(t, w.Path), "every file byte-identical, with its mode")
			assert.Empty(t, f.status(t, w.Path), "git status is clean right after create")
			assert.Equal(t, f.sha, strings.TrimSpace(f.git(t, w.Path, "rev-parse", "HEAD")))
			assert.Equal(t, "feature/feat-a", strings.TrimSpace(f.git(t, w.Path, "branch", "--show-current")))
			assert.Equal(t, "minimal\n", f.git(t, w.Path, "config", "--worktree", "core.checkStat"))
			assert.Equal(t, "false\n", f.git(t, w.Path, "config", "--worktree", "core.trustCtime"))
			assert.Equal(t, "false\n", f.git(t, w.Path, "config", "--worktree", "core.autocrlf"))
			fi, err := os.Lstat(filepath.Join(w.Path, ".git"))
			require.NoError(t, err)
			assert.True(t, fi.Mode().IsRegular(), "a linked worktree: .git is a file, never a copy of a repository")
		})
	}
}

func TestCowCloneIsolation(t *testing.T) {
	f := newCCFixture(t, false, nil, nil)
	a, err := f.prov.Create(context.Background(), f.spec("ws-a"))
	require.NoError(t, err)
	b, err := f.prov.Create(context.Background(), f.spec("ws-b"))
	require.NoError(t, err)
	goldenBefore := ccTree(t, f.goldenDir())
	bBefore := ccTree(t, b.Path)

	require.NoError(t, os.WriteFile(filepath.Join(a.Path, "a.txt"), []byte("changed in A\n"), 0o600))

	assert.Equal(t, goldenBefore, ccTree(t, f.goldenDir()), "golden unchanged")
	assert.Equal(t, bBefore, ccTree(t, b.Path), "workspace B unchanged")
	assert.Empty(t, f.status(t, b.Path))
	assert.Equal(t, " M a.txt\n", f.status(t, a.Path))
}

func TestCowCloneDiskFullCompensates(t *testing.T) {
	files := map[string]string{}
	for i := range 150 {
		files[fmt.Sprintf("pkg%02d/f%03d.txt", i%10, i)] = fmt.Sprintf("file %d\n", i)
	}
	cl := &workspacetest.FakeCloner{FailAfter: 100, Err: syscall.ENOSPC}
	f := newCCFixture(t, false, cl, files)
	m := workspace.NewManager(workspace.Deps{
		Config: f.cfg, Store: f.store, Git: f.client, Volumes: f.vols, DevDrive: &harness.SimDevDrive{},
		Cow: f.prov, Worktree: workspace.NewWorktreeProvider(f.client, f.vols),
		Logger: slog.New(slog.DiscardHandler),
	})
	req := workspace.CreateRequest{
		OpID: "op-full", ProjectID: "prj_1", RepoDir: f.repo.Dir, Provider: workspace.ProviderCowClone,
		Task: domain.Task{
			ID: "tsk_full", ProjectID: "prj_1", Slug: "full", Branch: "feature/full",
			Base: "develop", BaseSHA: f.sha, State: domain.TaskStateCreated,
		},
	}

	_, _, err := m.Create(context.Background(), req)
	require.ErrorIs(t, err, syscall.ENOSPC)
	rows, lerr := f.store.ListWorkspaces(context.Background(), domain.WorkspaceFilter{})
	require.NoError(t, lerr)
	require.Len(t, rows, 1)
	assert.Equal(t, domain.WorkspaceFailed, rows[0].State, "never a partial ready")
	f.assertGone(t, "full")
	assert.Empty(t, f.git(t, f.repo.Dir, "branch", "--list", "feature/full"), "branch deleted")
	assert.FileExists(t, filepath.Join(f.goldenDir(), "pkg00", "f000.txt"), "the golden tree stays")
}

// cancelCloner cancels the create after n successful clones.
type cancelCloner struct {
	inner  workspace.Cloner
	n      int
	cancel context.CancelFunc
	mu     sync.Mutex
	done   int
}

func (c *cancelCloner) CloneFile(ctx context.Context, src, dst string) error {
	c.mu.Lock()
	c.done++
	if c.done == c.n {
		c.cancel()
	}
	c.mu.Unlock()
	return c.inner.CloneFile(ctx, src, dst)
}

func TestCowCloneCancelMidWalk(t *testing.T) {
	files := map[string]string{}
	for i := range 300 {
		files[fmt.Sprintf("d%02d/f%03d.txt", i%20, i)] = fmt.Sprintf("file %d\n", i)
	}
	f := newCCFixture(t, false, nil, files)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Build the golden and probe first, so the count starts at the walk.
	_, err := f.prov.Create(context.Background(), f.spec("warm"))
	require.NoError(t, err)
	f.cloner = &cancelCloner{inner: &workspacetest.FakeCloner{}, n: 50, cancel: cancel}
	prov := f.provider(f.client)
	before := runtime.NumGoroutine()

	_, err = prov.Create(ctx, f.spec("canceled"))
	require.ErrorIs(t, err, context.Canceled)
	f.assertGone(t, "canceled")
	// Polled here, not with assert.Eventually, whose own goroutine would
	// count.
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	assert.LessOrEqual(t, runtime.NumGoroutine(), before, "no clone worker leaks")
}

func TestCowCloneSymlinkReplicated(t *testing.T) {
	if runtime.GOOS == "windows" {
		probe := filepath.Join(t.TempDir(), "l")
		if err := os.Symlink("x", probe); err != nil {
			t.Skipf("symlinks need Developer Mode or admin rights on Windows: %v", err)
		}
	}
	f := newCCFixture(t, false, nil, nil)
	if runtime.GOOS == "windows" {
		// git init on Windows records core.symlinks=false, so the golden
		// checkout would write the link as a plain file holding its target
		// and there would be no symlink to replicate. The probe above
		// showed this machine can create symlinks.
		f.git(t, f.repo.Dir, "config", "core.symlinks", "true")
	}
	require.NoError(t, os.Symlink("cmd/app/main.go", filepath.Join(f.repo.Dir, "link-to-main")))
	f.sha = f.repo.Commit(t, "feat: add a symlink", nil)

	w, err := f.prov.Create(context.Background(), f.spec("links"))
	require.NoError(t, err)
	fi, err := os.Lstat(filepath.Join(w.Path, "link-to-main"))
	require.NoError(t, err)
	require.NotZero(t, fi.Mode()&fs.ModeSymlink, "replicated as a symlink, not followed: %v", fi.Mode())
	target, err := os.Readlink(filepath.Join(w.Path, "link-to-main"))
	require.NoError(t, err)
	// Windows stores link targets with backslashes.
	assert.Equal(t, "cmd/app/main.go", filepath.ToSlash(target), "replicated as a symlink, not followed")
	assert.Empty(t, f.status(t, w.Path))
}

func TestIndexSeedersAgree(t *testing.T) {
	f := newCCFixture(t, false, nil, nil)
	golden, err := f.prov.Create(context.Background(), f.spec("seed-golden"))
	require.NoError(t, err)
	readTree := f.provider(f.client)
	workspace.SetIndexSeeder(readTree, workspace.ReadTreeSeeder(f.client))
	rt, err := readTree.Create(context.Background(), f.spec("seed-readtree"))
	require.NoError(t, err)

	assert.Empty(t, f.status(t, golden.Path), "option B (production): clean")
	assert.Empty(t, f.status(t, rt.Path), "option A (test-only): clean")
	assert.Equal(t, f.git(t, rt.Path, "ls-files", "-s"), f.git(t, golden.Path, "ls-files", "-s"),
		"both seeders index the same entries")
}

// --- failure paths over FakeGit ---

// fakeGolden writes a golden tree of files into dir with an index file
// beside it.
func fakeGolden(t *testing.T, dir string, files map[string]string) workspace.Golden {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	}
	require.NoError(t, os.WriteFile(dir+".index", []byte("DIRC"), 0o600))
	return workspace.Golden{ProjectID: "prj_1", BaseSHA: baseSHA, Dir: dir, IndexFile: dir + ".index", FileCount: len(files)}
}

type fakeCC struct {
	root string
	git  *workspacetest.FakeGit
	prov *workspace.CowCloneProvider
	spec workspace.WorkspaceSpec
}

func newFakeCC(t *testing.T, mutate func(g *workspace.Golden)) *fakeCC {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "ws")
	require.NoError(t, os.Mkdir(root, 0o750))
	g := fakeGolden(t, filepath.Join(root, "golden", baseSHA), map[string]string{"a.txt": "a", "d/b.txt": "b"})
	if mutate != nil {
		mutate(&g)
	}
	fg := &workspacetest.FakeGit{}
	fg.Hook = func(c workspacetest.Call) error {
		if c.Method == "WorktreeAdd" { // as git does: the directory and its .git
			return os.MkdirAll(filepath.Join(c.Args[1].(string), ".git"), 0o750)
		}
		return nil
	}
	cfg := config.Default()
	cfg.Workspace.Root = root
	vols := workspacetest.NewFakeVolumes(platform.Volume{Filesystem: "apfs", ID: "v", MountPath: base})
	return &fakeCC{
		root: root, git: fg,
		prov: workspace.NewCowCloneProvider(workspace.CowCloneDeps{
			Config: cfg, Git: fg, Golden: stubGolden{g: g}, Volumes: vols, Cloner: &workspacetest.FakeCloner{},
			Logger: slog.New(slog.DiscardHandler),
		}),
		spec: workspace.WorkspaceSpec{
			ID: "wks_1", ProjectID: "prj_1", RepoDir: base, Path: filepath.Join(root, "a"),
			Branch: "feature/a", BaseSHA: baseSHA,
		},
	}
}

func TestCowCloneFakeGitSequence(t *testing.T) {
	f := newFakeCC(t, nil)
	w, err := f.prov.Create(context.Background(), f.spec)
	require.NoError(t, err)
	assert.Equal(t, workspace.CowBlock, w.Caps.CopyOnWrite)
	idx, err := os.ReadFile(filepath.Join(f.spec.Path, ".git", "index"))
	require.NoError(t, err)
	assert.Equal(t, []byte("DIRC"), idx, "the golden index is cloned into the worktree")

	var add git.WorktreeAddOptions
	var cfg []string
	for _, c := range f.git.Calls() {
		switch c.Method {
		case "WorktreeAdd":
			add = c.Args[2].(git.WorktreeAddOptions)
		case "ConfigSetWorktree":
			cfg = append(cfg, c.Args[1].(string)+"="+c.Args[2].(string))
		}
	}
	assert.Equal(t, git.WorktreeAddOptions{NewBranch: "feature/a", Commitish: baseSHA, NoCheckout: true}, add)
	want := []string{"core.autocrlf=false"}
	if runtime.GOOS == "windows" {
		want = append(want, "core.longpaths=true")
	}
	want = append(want, "core.checkStat=minimal", "core.trustCtime=false")
	assert.Equal(t, want, cfg)
}

func TestCowCloneFileCountMismatchFails(t *testing.T) {
	f := newFakeCC(t, func(g *workspace.Golden) { g.FileCount++ })
	_, err := f.prov.Create(context.Background(), f.spec)
	require.ErrorContains(t, err, "cloned 2 files")
	assert.NoDirExists(t, f.spec.Path)
	assert.Equal(t, 1, f.git.Count("WorktreeRemove"), "worktree registration removed")
}

func TestCowCloneHeadMismatchFails(t *testing.T) {
	f := newFakeCC(t, nil)
	f.git.Heads = map[string]string{f.spec.Path: strings.Repeat("f", 40)}
	_, err := f.prov.Create(context.Background(), f.spec)
	require.ErrorContains(t, err, "HEAD is")
	assert.NoDirExists(t, f.spec.Path)
}

func TestCowCloneRefusesGitDirInGolden(t *testing.T) {
	f := newFakeCC(t, func(g *workspace.Golden) {
		require.NoError(t, os.MkdirAll(filepath.Join(g.Dir, ".git"), 0o750))
	})
	_, err := f.prov.Create(context.Background(), f.spec)
	require.ErrorContains(t, err, ".git")
	assert.NoDirExists(t, f.spec.Path)
}

func TestCowCloneExistingPathUntouched(t *testing.T) {
	f := newFakeCC(t, nil)
	require.NoError(t, os.Mkdir(f.spec.Path, 0o750))
	marker := filepath.Join(f.spec.Path, "keep")
	require.NoError(t, os.WriteFile(marker, nil, 0o600))
	_, err := f.prov.Create(context.Background(), f.spec)
	require.ErrorIs(t, err, workspace.ErrPathExists)
	assert.FileExists(t, marker)
	assert.Zero(t, f.git.Count("WorktreeAdd"))
}

func TestCowCloneGoldenErrorWritesNothing(t *testing.T) {
	f := newFakeCC(t, nil)
	cfg := config.Default()
	cfg.Workspace.Root = f.root
	p := workspace.NewCowCloneProvider(workspace.CowCloneDeps{
		Config: cfg, Git: f.git, Golden: stubGolden{err: syscall.ENOSPC},
		Volumes: workspacetest.NewFakeVolumes(platform.Volume{Filesystem: "apfs", ID: "v", MountPath: fsRoot}),
		Cloner:  &workspacetest.FakeCloner{}, Logger: slog.New(slog.DiscardHandler),
	})
	_, err := p.Create(context.Background(), f.spec)
	require.ErrorIs(t, err, syscall.ENOSPC)
	assert.Zero(t, f.git.Count("WorktreeAdd"))
	assert.NoDirExists(t, f.spec.Path)
}

// --- Manager wiring ---

func TestManagerBuildsCowCloneWithOSCloner(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	cfg := config.Default()
	cfg.Workspace.Root = base
	vols := workspacetest.NewFakeVolumes(platform.Volume{Filesystem: "btrfs", ID: "v", MountPath: base})
	m := workspace.NewManager(workspace.Deps{
		Config: cfg, Store: workspacetest.NewMemStore(domain.SystemClock{}), Git: &workspacetest.FakeGit{},
		Volumes: vols, CowClone: true, Logger: slog.New(slog.DiscardHandler),
	})
	sel, err := m.Select(context.Background(), workspace.ProviderCowClone)
	if err != nil {
		// The temp volume cannot block clone: the real provider probed it.
		require.ErrorIs(t, err, workspace.ErrCloneUnsupported)
		assert.NotContains(t, err.Error(), "not available in this build")
		return
	}
	assert.Equal(t, workspace.ProviderCowClone, sel.Provider.Name())
	assert.Equal(t, workspace.CowBlock, sel.Caps.CopyOnWrite)
	entries, err := os.ReadDir(base)
	require.NoError(t, err)
	assert.Empty(t, entries, "probe files removed")
}
