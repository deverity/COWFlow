package workspace_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/git"
	"github.com/deverity/cowflow/internal/platform"
	"github.com/deverity/cowflow/internal/workspace"
	"github.com/deverity/cowflow/internal/workspace/workspacetest"
	"github.com/deverity/cowflow/test/harness"
)

// wtFixture is a real repository with a git_worktree provider over it.
type wtFixture struct {
	repo   *harness.Repo
	runner *harness.FaultyRunner
	client *git.Client
	prov   *workspace.WorktreeProvider
	vols   *workspacetest.FakeVolumes
	root   string
	base   string
}

func newWtFixture(t *testing.T, fs string) *wtFixture {
	t.Helper()
	repo := harness.NewRepo(t)
	base := repo.Commit(t, "feat: base", map[string]string{"a.txt": "a\n"})
	runner := &harness.FaultyRunner{Inner: repo.Runner}
	client := git.NewClient(runner)
	root := t.TempDir()
	vols := workspacetest.NewFakeVolumes(platform.Volume{Filesystem: fs, ID: "v1", MountPath: root})
	return &wtFixture{
		repo: repo, runner: runner, client: client, root: root, base: base, vols: vols,
		prov: workspace.NewWorktreeProvider(client, vols),
	}
}

func (f *wtFixture) spec(slug, branch string) workspace.WorkspaceSpec {
	return workspace.WorkspaceSpec{
		ID: "wks_" + slug, OpID: "op-" + slug, ProjectID: "prj_1", TaskID: "tsk_" + slug,
		RepoDir: f.repo.Dir, Path: filepath.Join(f.root, slug), Branch: branch, BaseSHA: f.base,
	}
}

func TestWorktreeCapabilitiesNone(t *testing.T) {
	t.Parallel()
	for _, fs := range []string{"refs", "ntfs", "ext4"} {
		f := newWtFixture(t, fs)
		caps, err := f.prov.Capabilities(context.Background(), f.root)
		require.NoError(t, err)
		assert.Equal(t, workspace.Capabilities{
			CopyOnWrite: workspace.CowNone, SparseSupported: true, Filesystem: fs,
		}, caps, fs)
	}
	assert.Equal(t, "git_worktree", newWtFixture(t, "ntfs").prov.Name())
}

func TestWorktreeCapabilitiesVolumeError(t *testing.T) {
	t.Parallel()
	f := newWtFixture(t, "ntfs")
	f.vols.Unmount(f.root)
	_, err := f.prov.Capabilities(context.Background(), f.root)
	require.Error(t, err)
}

func TestWorktreeCreateIndependentBranch(t *testing.T) {
	t.Parallel()
	f := newWtFixture(t, "ntfs")
	ctx := context.Background()
	w1, err := f.prov.Create(ctx, f.spec("one", "feature/one"))
	require.NoError(t, err)
	w2, err := f.prov.Create(ctx, f.spec("two", "feature/two"))
	require.NoError(t, err)

	assert.Equal(t, "git_worktree", w1.Provider)
	assert.Equal(t, workspace.CowNone, w1.Caps.CopyOnWrite)
	assert.Equal(t, "ntfs", w1.Caps.Filesystem)
	assert.Equal(t, f.base, w1.BaseSHA)
	assert.FileExists(t, filepath.Join(w1.Path, "a.txt"))

	require.NoError(t, os.WriteFile(filepath.Join(w1.Path, "b.txt"), []byte("b\n"), 0o600))
	_, err = f.runner.Run(ctx, w1.Path, "add", "b.txt")
	require.NoError(t, err)
	_, err = f.runner.Run(ctx, w1.Path, "-c", "user.name=t", "-c", "user.email=t@example.test", "commit", "-q", "-m", "feat: b")
	require.NoError(t, err)

	h1, err := f.client.Head(ctx, w1.Path)
	require.NoError(t, err)
	h2, err := f.client.Head(ctx, w2.Path)
	require.NoError(t, err)
	assert.NotEqual(t, h1, h2)
	assert.Equal(t, f.base, h2)
}

func TestWorktreeAutocrlfFalse(t *testing.T) {
	t.Parallel()
	f := newWtFixture(t, "ntfs")
	w, err := f.prov.Create(context.Background(), f.spec("crlf", "feature/crlf"))
	require.NoError(t, err)
	out, err := f.runner.Run(context.Background(), w.Path, "config", "--worktree", "--get", "core.autocrlf")
	require.NoError(t, err)
	assert.Equal(t, "false", strings.TrimSpace(string(out)))
}

func TestWorktreeSparseSkipsCheckout(t *testing.T) {
	t.Parallel()
	f := newWtFixture(t, "ntfs")
	// Sparse set needs per-call runner options, which the fault wrapper hides.
	client := git.NewClient(f.repo.Runner)
	f.prov = workspace.NewWorktreeProvider(client, f.vols)
	spec := f.spec("sp", "feature/sp")
	spec.Sparse = workspace.SparseSpec{Enabled: true, Paths: []string{"src"}}
	w, err := f.prov.Create(context.Background(), spec)
	require.NoError(t, err)
	assert.True(t, w.Sparse.Enabled)
	assert.FileExists(t, filepath.Join(w.Path, "a.txt"), "root files are part of every cone")
	dirs, err := client.SparseList(context.Background(), w.Path)
	require.NoError(t, err)
	assert.Equal(t, []string{"contracts", "src"}, dirs)
}

func TestWorktreeCreateFailureCompensates(t *testing.T) {
	t.Parallel()
	f := newWtFixture(t, "ntfs")
	spec := f.spec("boom", "feature/boom")
	// Fail the config step so the worktree exists when the failure comes.
	f.runner.Inject(harness.GitFault{Match: []string{"config"}, ExitCode: 128})
	_, err := f.prov.Create(context.Background(), spec)
	require.Error(t, err)
	assert.NoDirExists(t, spec.Path)
	assertNoAdminEntry(t, f, "boom")
}

// The workspace path reaches the provider in a non-canonical spelling, as a
// Windows %TEMP% with 8.3 short names (C:\Users\RUNNER~1) does: git lists the
// worktree, and Confine resolves the path, in the long form. Compensation
// must still find the registration and remove the directory. A symlinked
// parent stands in for the short name on every OS.
func TestWorktreeCreateFailureCompensatesNonCanonicalPath(t *testing.T) {
	t.Parallel()
	f := newWtFixture(t, "ntfs")
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(f.root, alias); err != nil {
		t.Skipf("cannot create symlinks here (needs a privilege on Windows): %v", err)
	}
	f.vols.Set(platform.Volume{Filesystem: "ntfs", ID: "v1", MountPath: alias})
	spec := f.spec("boom", "feature/boom")
	spec.Path = filepath.Join(alias, "boom")
	f.runner.Inject(harness.GitFault{Match: []string{"config"}, ExitCode: 128})
	_, err := f.prov.Create(context.Background(), spec)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "compensation", "compensation succeeded")
	assert.NoDirExists(t, filepath.Join(f.root, "boom"))
	assertNoAdminEntry(t, f, "boom")
}

func TestWorktreeCreateAddFailureLeavesNothing(t *testing.T) {
	t.Parallel()
	f := newWtFixture(t, "ntfs")
	spec := f.spec("boom", "feature/boom")
	f.runner.Inject(harness.GitFault{Match: []string{"worktree", "add"}, ExitCode: 128})
	_, err := f.prov.Create(context.Background(), spec)
	require.Error(t, err)
	assert.NoDirExists(t, spec.Path)
	assertNoAdminEntry(t, f, "boom")
}

func TestWorktreeCreateBranchExists(t *testing.T) {
	t.Parallel()
	f := newWtFixture(t, "ntfs")
	_, err := f.runner.Run(context.Background(), f.repo.Dir, "branch", "feature/dup", f.base)
	require.NoError(t, err)
	spec := f.spec("dup", "feature/dup")
	_, err = f.prov.Create(context.Background(), spec)
	require.ErrorIs(t, err, workspace.ErrBranchExists)
	assert.NoDirExists(t, spec.Path)
	// The pre-existing branch is untouched.
	ok, err := f.client.BranchExists(context.Background(), f.repo.Dir, "feature/dup")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestWorktreeCreateExistingPathUntouched(t *testing.T) {
	t.Parallel()
	f := newWtFixture(t, "ntfs")
	spec := f.spec("keep", "feature/keep")
	require.NoError(t, os.MkdirAll(spec.Path, 0o750))
	keep := filepath.Join(spec.Path, "precious.txt")
	require.NoError(t, os.WriteFile(keep, []byte("x"), 0o600))
	_, err := f.prov.Create(context.Background(), spec)
	require.ErrorIs(t, err, workspace.ErrPathExists)
	assert.FileExists(t, keep)
}

func TestWorktreeDestroyWithoutDestroyerFails(t *testing.T) {
	t.Parallel()
	p := workspace.NewWorktreeProvider(&workspacetest.FakeGit{}, nil)
	_, err := p.Destroy(context.Background(), "wks_x", workspace.DestroyOptions{})
	require.Error(t, err, "an error, not a panic")
}

// assertNoAdminEntry checks that the repository has no worktree besides the
// main one and no .git/worktrees/<name> directory for slug.
func assertNoAdminEntry(t *testing.T, f *wtFixture, slug string) {
	t.Helper()
	wts, err := f.client.WorktreeList(context.Background(), f.repo.Dir)
	require.NoError(t, err)
	assert.Len(t, wts, 1)
	_, err = os.Stat(filepath.Join(f.repo.Dir, ".git", "worktrees", slug))
	assert.True(t, os.IsNotExist(err), "admin entry remains: %v", err)
}
