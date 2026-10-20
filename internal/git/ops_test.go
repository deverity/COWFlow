package git_test

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/git"
	"github.com/deverity/cowflow/test/harness"
)

// gitIn runs git in dir through the harness runner and fails on error.
func gitIn(t *testing.T, r *harness.Repo, dir string, args ...string) string {
	t.Helper()
	out, err := r.Runner.Run(context.Background(), dir, args...)
	require.NoError(t, err, "git %v", args)
	return string(out)
}

// commitIn writes files in worktree dir and commits them.
func commitIn(t *testing.T, r *harness.Repo, dir, msg string, files map[string]string) string {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	}
	gitIn(t, r, dir, "add", "-A")
	gitIn(t, r, dir, "commit", "-q", "-m", msg)
	return strings.TrimSpace(gitIn(t, r, dir, "rev-parse", "HEAD"))
}

func newRepo(t *testing.T) (*harness.Repo, *git.Client, string) {
	t.Helper()
	r := harness.NewRepo(t)
	sha := r.Commit(t, "feat: seed", map[string]string{
		"README.md":           "hello\n",
		"src/main.go":         "package main\n",
		"dir with space/a b":  "spaces\n",
		"unicodé/日本.txt":      "unicode\n",
		"deep/a/b/c/d/e.txt":  "deep\n",
		"contracts/api.proto": "syntax = \"proto3\";\n",
	})
	return r, git.NewClient(r.Runner), sha
}

// sameFSIndex returns an index path on the repository's filesystem:
// read-tree --index-output renames a lock file from next to the repository's
// index, so the output must be on the same filesystem (git-read-tree(1)).
func sameFSIndex(t *testing.T, r *harness.Repo) string {
	t.Helper()
	dir, err := os.MkdirTemp(filepath.Join(r.Dir, ".git"), "golden-meta-*")
	require.NoError(t, err)
	return filepath.Join(dir, "golden.index")
}

func wsDir(t *testing.T, name string) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return filepath.Join(d, name)
}

func TestWorktreeAddNoCheckout(t *testing.T) {
	ctx := context.Background()
	r, c, sha := newRepo(t)
	ws := wsDir(t, "ws")

	require.NoError(t, c.WorktreeAdd(ctx, r.Dir, ws, git.WorktreeAddOptions{NewBranch: "cowflow/p/t", Commitish: sha, NoCheckout: true}))
	_, err := os.Stat(filepath.Join(ws, "README.md"))
	assert.ErrorIs(t, err, fs.ErrNotExist, "no checkout")

	head, err := c.Head(ctx, ws)
	require.NoError(t, err)
	assert.Equal(t, sha, head)

	ok, err := c.BranchExists(ctx, r.Dir, "cowflow/p/t")
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = c.BranchExists(ctx, r.Dir, "cowflow/p/other")
	require.NoError(t, err)
	assert.False(t, ok)

	list, err := c.WorktreeList(ctx, r.Dir)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "develop", list[0].Branch)
	assert.Equal(t, git.Worktree{Path: filepath.ToSlash(ws), Head: sha, Branch: "cowflow/p/t"},
		git.Worktree{Path: filepath.ToSlash(list[1].Path), Head: list[1].Head, Branch: list[1].Branch})

	// The index of a no-checkout worktree is empty; read-tree -mu HEAD
	// materializes it.
	idx, err := c.GitPath(ctx, ws, "index")
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(idx))
	require.NoError(t, c.ReadTree(ctx, ws, "-mu", "HEAD"))
	b, err := os.ReadFile(filepath.Join(ws, "README.md")) //nolint:gosec // G304: test temp path
	require.NoError(t, err)
	assert.Equal(t, "hello\n", string(b))
	_, err = os.Stat(idx)
	require.NoError(t, err, "GitPath names the worktree's real index")
	st, err := c.Status(ctx, ws)
	require.NoError(t, err)
	assert.Empty(t, st)
}

func TestWorktreeAddNewBranchFailsIfExists(t *testing.T) {
	ctx := context.Background()
	r, c, sha := newRepo(t)
	opts := git.WorktreeAddOptions{NewBranch: "cowflow/p/t", Commitish: sha}
	require.NoError(t, c.WorktreeAdd(ctx, r.Dir, wsDir(t, "a"), opts))

	err := c.WorktreeAdd(ctx, r.Dir, wsDir(t, "b"), opts)
	var ge *git.Error
	require.ErrorAs(t, err, &ge)
	assert.Positive(t, ge.ExitCode)
}

func TestWorktreeRemoveAndPrune(t *testing.T) {
	ctx := context.Background()
	r, c, sha := newRepo(t)
	a, b := wsDir(t, "a"), wsDir(t, "b")
	require.NoError(t, c.WorktreeAdd(ctx, r.Dir, a, git.WorktreeAddOptions{NewBranch: "t/a", Commitish: sha}))
	require.NoError(t, c.WorktreeAdd(ctx, r.Dir, b, git.WorktreeAddOptions{NewBranch: "t/b", Commitish: sha}))

	require.NoError(t, os.WriteFile(filepath.Join(a, "untracked"), []byte("x"), 0o600))
	require.Error(t, c.WorktreeRemove(ctx, r.Dir, a, false), "dirty worktree needs force")
	require.NoError(t, c.WorktreeRemove(ctx, r.Dir, a, true))
	_, err := os.Stat(a)
	assert.ErrorIs(t, err, fs.ErrNotExist)

	require.NoError(t, os.RemoveAll(b))
	list, err := c.WorktreeList(ctx, r.Dir)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.True(t, list[1].Prunable)

	require.NoError(t, c.WorktreePrune(ctx, r.Dir))
	list, err = c.WorktreeList(ctx, r.Dir)
	require.NoError(t, err)
	assert.Len(t, list, 1)
}

func TestResolveCommitAndDeleteBranchIfAt(t *testing.T) {
	ctx := context.Background()
	r, c, sha := newRepo(t)
	got, err := c.ResolveCommit(ctx, r.Dir, "develop")
	require.NoError(t, err)
	assert.Equal(t, sha, got)
	_, err = c.ResolveCommit(ctx, r.Dir, "--all")
	require.Error(t, err, "--end-of-options keeps revs from being options")

	second := r.Commit(t, "feat: second", map[string]string{"x": "y\n"})
	gitIn(t, r, r.Dir, "branch", "t/x", sha)
	require.Error(t, c.DeleteBranchIfAt(ctx, r.Dir, "t/x", second), "moved branch is kept")
	ok, err := c.BranchExists(ctx, r.Dir, "t/x")
	require.NoError(t, err)
	assert.True(t, ok)

	require.NoError(t, c.DeleteBranchIfAt(ctx, r.Dir, "t/x", sha))
	ok, err = c.BranchExists(ctx, r.Dir, "t/x")
	require.NoError(t, err)
	assert.False(t, ok)
}

// treeFiles returns every regular file under root (excluding .git) with its
// bytes.
func treeFiles(t *testing.T, root string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	require.NoError(t, filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p) //nolint:gosec // G304: test temp path
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = b
		return nil
	}))
	return files
}

func TestExportTreeMatchesCheckout(t *testing.T) {
	ctx := context.Background()
	r, c, _ := newRepo(t)
	sha := r.Commit(t, "feat: line endings", map[string]string{
		".gitattributes": "* text=auto eol=lf\nbinary.dat -text\n",
		"crlf.txt":       "one\r\ntwo\r\n",
		"binary.dat":     "a\r\n\x00b",
	})
	golden := wsDir(t, "golden")
	require.NoError(t, os.Mkdir(golden, 0o750))
	idx := sameFSIndex(t, r)

	require.NoError(t, c.ExportTree(ctx, r.Dir, sha, golden, idx))
	checkout := wsDir(t, "checkout")
	require.NoError(t, c.WorktreeAdd(ctx, r.Dir, checkout, git.WorktreeAddOptions{Commitish: sha}))

	want, got := treeFiles(t, checkout), treeFiles(t, golden)
	require.NotEmpty(t, want)
	assert.Equal(t, keys(want), keys(got))
	for name, b := range want {
		assert.True(t, bytes.Equal(b, got[name]), "%s differs", name)
	}
	assert.Equal(t, []byte("a\r\n\x00b"), got["binary.dat"])

	// The repository's own index and worktree are untouched.
	st, err := c.Status(ctx, r.Dir)
	require.NoError(t, err)
	assert.Empty(t, st)
}

func keys(m map[string][]byte) []string {
	k := make([]string, 0, len(m))
	for n := range m {
		k = append(k, n)
	}
	slices.Sort(k)
	return k
}

func TestExportTreeIndexIsClean(t *testing.T) {
	ctx := context.Background()
	r, c, sha := newRepo(t)
	golden := wsDir(t, "golden")
	require.NoError(t, os.Mkdir(golden, 0o750))
	idx := sameFSIndex(t, r)
	require.NoError(t, c.ExportTree(ctx, r.Dir, sha, golden, idx))

	or, ok := r.Runner.(git.OptsRunner)
	require.True(t, ok)
	status := func() string {
		out, err := or.RunWith(ctx, r.Dir, git.RunOpts{Env: []string{"GIT_INDEX_FILE=" + idx}},
			"--work-tree="+golden, "status", "--porcelain=v2", "-z", "--untracked-files=all")
		require.NoError(t, err)
		return string(out)
	}
	assert.Empty(t, status())

	// RefreshIndex against a copy keeps the index usable for another tree.
	require.NoError(t, c.RefreshIndex(ctx, r.Dir, golden, idx))
	assert.Empty(t, status())
	require.NoError(t, os.WriteFile(filepath.Join(golden, "README.md"), []byte("changed\n"), 0o600))
	require.NoError(t, c.RefreshIndex(ctx, r.Dir, golden, idx), "-q tolerates modified files")
	assert.Contains(t, status(), "README.md")
}

func TestStatusRealRepo(t *testing.T) {
	ctx := context.Background()
	r, c, _ := newRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(r.Dir, "README.md"), []byte("changed\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(r.Dir, "build", "out"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(r.Dir, "build", "out", "x.bin"), []byte("x"), 0o600))
	gitIn(t, r, r.Dir, "mv", "src/main.go", "src/renamed main.go")
	require.NoError(t, os.WriteFile(filepath.Join(r.Dir, "staged.txt"), []byte("s\n"), 0o600))
	gitIn(t, r, r.Dir, "add", "staged.txt")
	newline := "line\nbreak.txt"
	if runtime.GOOS != "windows" {
		require.NoError(t, os.WriteFile(filepath.Join(r.Dir, newline), []byte("n"), 0o600))
	}

	st, err := c.Status(ctx, r.Dir)
	require.NoError(t, err)
	byPath := map[string]git.StatusEntry{}
	for _, e := range st {
		byPath[e.Path] = e
	}
	assert.Equal(t, git.StatusEntry{Kind: git.StatusChanged, XY: ".M", Path: "README.md"}, byPath["README.md"])
	assert.Equal(t, git.StatusEntry{Kind: git.StatusChanged, XY: "A.", Path: "staged.txt"}, byPath["staged.txt"])
	assert.Equal(t, git.StatusEntry{Kind: git.StatusRenamed, XY: "R.", Path: "src/renamed main.go", OrigPath: "src/main.go"},
		byPath["src/renamed main.go"])
	assert.Equal(t, git.StatusEntry{Kind: git.StatusUntracked, XY: "??", Path: "build/out/x.bin"}, byPath["build/out/x.bin"])
	if runtime.GOOS != "windows" {
		assert.Equal(t, git.StatusUntracked, byPath[newline].Kind)
	}
}

func TestStatusTruncatedOutputFails(t *testing.T) {
	ctx := context.Background()
	r, _, _ := newRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(r.Dir, "README.md"), []byte("changed\n"), 0o600))
	f := &harness.FaultyRunner{Inner: r.Runner, Faults: []harness.GitFault{{Match: []string{"status"}, TruncateAt: 10}}}

	st, err := git.NewClient(f).Status(ctx, r.Dir)
	require.ErrorIs(t, err, git.ErrUnexpectedOutput)
	assert.Nil(t, st)
}

func TestWorktreeListTruncatedOutputFails(t *testing.T) {
	ctx := context.Background()
	r, _, _ := newRepo(t)
	f := &harness.FaultyRunner{Inner: r.Runner, Faults: []harness.GitFault{{Match: []string{"worktree", "list"}, TruncateAt: 10}}}

	list, err := git.NewClient(f).WorktreeList(ctx, r.Dir)
	require.ErrorIs(t, err, git.ErrUnexpectedOutput)
	assert.Nil(t, list)
}

func TestUnpublishedIgnoresChangeRefs(t *testing.T) {
	ctx := context.Background()
	r, c, base := newRepo(t)
	ws := wsDir(t, "ws")
	require.NoError(t, c.WorktreeAdd(ctx, r.Dir, ws, git.WorktreeAddOptions{NewBranch: "cowflow/p/t", Commitish: base}))

	got, err := c.Unpublished(ctx, ws, base, "refs/cowflow/changes")
	require.NoError(t, err)
	assert.Empty(t, got, "nothing committed yet")

	head := commitIn(t, r, ws, "feat: work", map[string]string{"work.txt": "w\n"})
	got, err = c.Unpublished(ctx, ws, base, "refs/cowflow/changes")
	require.NoError(t, err)
	assert.Equal(t, []string{head}, got)

	gitIn(t, r, ws, "update-ref", "refs/cowflow/changes/x", "HEAD")
	got, err = c.Unpublished(ctx, ws, base, "refs/cowflow/changes")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestConfigSetWorktree(t *testing.T) {
	ctx := context.Background()
	r, c, sha := newRepo(t)
	ws := wsDir(t, "ws")
	require.NoError(t, c.WorktreeAdd(ctx, r.Dir, ws, git.WorktreeAddOptions{NewBranch: "t/w", Commitish: sha}))

	for range 2 { // idempotent
		require.NoError(t, c.ConfigSetWorktree(ctx, ws, "core.autocrlf", "input"))
	}
	assert.Equal(t, "true\n", gitIn(t, r, r.Dir, "config", "--get", "extensions.worktreeConfig"))
	assert.Equal(t, "input\n", gitIn(t, r, ws, "config", "--worktree", "--get", "core.autocrlf"))
	assert.Equal(t, "false\n", gitIn(t, r, r.Dir, "config", "--get", "core.autocrlf"), "main worktree keeps its value")
}

func TestConfigSetWorktreeRefusesCoreWorktree(t *testing.T) {
	ctx := context.Background()
	r, c, _ := newRepo(t)
	gitIn(t, r, r.Dir, "config", "core.worktree", r.Dir)
	err := c.ConfigSetWorktree(ctx, r.Dir, "core.autocrlf", "false")
	require.ErrorIs(t, err, git.ErrWorktreeConfigUnsafe)
	_, err = r.Runner.Run(ctx, r.Dir, "config", "--get", "extensions.worktreeConfig")
	require.Error(t, err, "extension stays off")
}

func TestEnvStripsInheritedGitVars(t *testing.T) {
	ctx := context.Background()
	other := harness.NewRepo(t)
	otherSHA := other.Commit(t, "feat: other", map[string]string{"other": "o\n"})
	r, _, sha := newRepo(t)
	require.NotEqual(t, otherSHA, sha)

	t.Setenv("GIT_DIR", filepath.Join(other.Dir, ".git"))
	t.Setenv("GIT_WORK_TREE", other.Dir)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(other.Dir, ".git", "index"))
	t.Setenv("GIT_COMMON_DIR", filepath.Join(other.Dir, ".git"))
	runner, err := git.NewExecRunner(git.ExecOptions{Timeout: 30e9, ExtraEnv: r.Env})
	require.NoError(t, err)
	c := git.NewClient(runner)

	head, err := c.Head(ctx, r.Dir)
	require.NoError(t, err)
	assert.Equal(t, sha, head)
	st, err := c.Status(ctx, r.Dir)
	require.NoError(t, err)
	assert.Empty(t, st)
}
