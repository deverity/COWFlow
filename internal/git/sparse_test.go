package git_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/git"
)

// sparseWorktree creates a worktree with a no-checkout add, like the
// git_worktree provider does.
func sparseWorktree(t *testing.T, c *git.Client, repo, sha string) string {
	t.Helper()
	ws := wsDir(t, "ws")
	require.NoError(t, c.WorktreeAdd(context.Background(), repo, ws,
		git.WorktreeAddOptions{NewBranch: "t/sparse", Commitish: sha, NoCheckout: true}))
	return ws
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func TestSparseSetMaterializesOnlyCone(t *testing.T) {
	ctx := context.Background()
	r, c, sha := newRepo(t)
	ws := sparseWorktree(t, c, r.Dir, sha)

	require.NoError(t, c.SparseSet(ctx, ws, []string{"src", "contracts"}))
	require.NoError(t, c.PopulateSparse(ctx, ws))

	assert.True(t, exists(filepath.Join(ws, "README.md")), "root files are included")
	assert.True(t, exists(filepath.Join(ws, "src", "main.go")))
	assert.True(t, exists(filepath.Join(ws, "contracts", "api.proto")))
	assert.False(t, exists(filepath.Join(ws, "deep")))
	assert.False(t, exists(filepath.Join(ws, "dir with space")))

	lines := strings.Split(strings.TrimSpace(gitIn(t, r, ws, "ls-files", "-t")), "\n")
	require.NotEmpty(t, lines)
	for _, l := range lines {
		_, name, _ := strings.Cut(l, " ")
		switch {
		case !strings.Contains(name, "/"), strings.HasPrefix(name, "src/"), strings.HasPrefix(name, "contracts/"):
			assert.True(t, strings.HasPrefix(l, "H "), l)
		default:
			assert.True(t, strings.HasPrefix(l, "S "), "skip-worktree expected: %s", l)
		}
	}

	dirs, err := c.SparseList(ctx, ws)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"src", "contracts"}, dirs)
}

func TestSparseSetUnusualNames(t *testing.T) {
	ctx := context.Background()
	r, c, sha := newRepo(t)
	ws := sparseWorktree(t, c, r.Dir, sha)

	require.NoError(t, c.SparseSet(ctx, ws, []string{`dir with space\`, "unicodé", "deep/a"}))
	require.NoError(t, c.PopulateSparse(ctx, ws))
	assert.True(t, exists(filepath.Join(ws, "dir with space", "a b")))
	assert.True(t, exists(filepath.Join(ws, "unicodé", "日本.txt")))
	assert.True(t, exists(filepath.Join(ws, "deep", "a", "b", "c", "d", "e.txt")))
	assert.False(t, exists(filepath.Join(ws, "src")))
}

func TestValidateSparseDir(t *testing.T) {
	ok := map[string]string{
		"src":            "src",
		"src/auth":       "src/auth",
		`src\auth`:       "src/auth",
		"src/auth/":      "src/auth",
		`src\auth\\`:     "src/auth",
		"dir with space": "dir with space",
		"unicodé/日本":     "unicodé/日本",
		"a.b/.hidden":    "a.b/.hidden",
		"a..b":           "a..b",
	}
	for in, want := range ok {
		got, err := git.ValidateSparseDir(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, in := range []string{
		"", " ", "/", "/etc", "//server/share", `\\server\share`, `C:\x`, "c:/x", "C:x", `..\x`, "../x",
		"a/../b", "..", "a/..", ".", "./a", "a/./b", "a//b", "-rf", "-", "--stdin", `"quoted"`,
		"a*", "src/*", "a?b", "a[b]", "a\nb", "a\x00b", "a\tb", "\x7fa",
	} {
		_, err := git.ValidateSparseDir(in)
		require.ErrorIs(t, err, git.ErrInvalidArgument, "%q", in)
	}
}

func TestSparseSetInvalidInputRunsNoGit(t *testing.T) {
	ctx := context.Background()
	rec := &recordingRunner{}
	c := git.NewClient(rec)
	err := c.SparseSet(ctx, t.TempDir(), []string{"ok", "../escape"})
	require.ErrorIs(t, err, git.ErrInvalidArgument)
	assert.Contains(t, err.Error(), "../escape")
	require.ErrorIs(t, c.SparseSet(ctx, "relative", nil), git.ErrInvalidArgument)
	assert.Empty(t, rec.calls, "git was not run")
}

func TestSparseSetDirsNeverReachArgv(t *testing.T) {
	rec := &recordingRunner{}
	c := git.NewClient(rec)
	require.NoError(t, c.SparseSet(context.Background(), t.TempDir(), []string{"a/b", `c\d`}))
	require.Len(t, rec.calls, 2)
	assert.Equal(t, []string{"cat-file", "--batch-check"}, rec.calls[0].args)
	assert.Equal(t, "HEAD:a/b\nHEAD:c/d\n", rec.calls[0].stdin)
	assert.Equal(t, []string{"sparse-checkout", "set", "--cone", "--stdin"}, rec.calls[1].args)
	assert.Equal(t, "a/b\nc/d\n", rec.calls[1].stdin)
}

type recordedCall struct {
	args  []string
	stdin string
}

// recordingRunner records every call's argv and stdin; it answers
// cat-file --batch-check with "missing" for each input line.
type recordingRunner struct {
	calls []recordedCall
}

func (r *recordingRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, recordedCall{args: args})
	return nil, nil
}

func (r *recordingRunner) RunWith(_ context.Context, _ string, o git.RunOpts, args ...string) ([]byte, error) {
	call := recordedCall{args: args}
	if o.Stdin != nil {
		b, err := io.ReadAll(o.Stdin)
		if err != nil {
			return nil, err
		}
		call.stdin = string(b)
	}
	r.calls = append(r.calls, call)
	if args[0] == "cat-file" {
		var out strings.Builder
		for l := range strings.SplitSeq(strings.TrimSuffix(call.stdin, "\n"), "\n") {
			out.WriteString(l + " missing\n")
		}
		return []byte(out.String()), nil
	}
	return nil, nil
}

func TestSparseSetFileArgumentRejected(t *testing.T) {
	ctx := context.Background()
	r, c, sha := newRepo(t)
	ws := sparseWorktree(t, c, r.Dir, sha)

	err := c.SparseSet(ctx, ws, []string{"README.md"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "README.md")
	require.ErrorIs(t, err, git.ErrInvalidArgument)
	_, err = c.SparseList(ctx, ws)
	require.Error(t, err, "sparse checkout was not turned on")
}

func TestSparseDisableRestoresAll(t *testing.T) {
	ctx := context.Background()
	r, c, sha := newRepo(t)
	ws := sparseWorktree(t, c, r.Dir, sha)

	require.NoError(t, c.SparseSet(ctx, ws, []string{"src"}))
	require.NoError(t, c.PopulateSparse(ctx, ws))
	require.False(t, exists(filepath.Join(ws, "deep")))

	require.NoError(t, c.SparseDisable(ctx, ws))
	assert.True(t, exists(filepath.Join(ws, "deep", "a", "b", "c", "d", "e.txt")))
	assert.True(t, exists(filepath.Join(ws, "contracts", "api.proto")))
	for _, l := range strings.Split(strings.TrimSpace(gitIn(t, r, ws, "ls-files", "-t")), "\n") {
		assert.True(t, strings.HasPrefix(l, "H "), l)
	}
	_, err := c.SparseList(ctx, ws)
	require.Error(t, err, "list fails once sparse checkout is off")
}

func TestSparseSetLargeList(t *testing.T) {
	ctx := context.Background()
	r, c, _ := newRepo(t)
	files := map[string]string{}
	var dirs []string
	for i := range 5000 {
		d := fmt.Sprintf("bulk/pkg%04d/with-a-fairly-long-directory-name-to-exceed-argv", i)
		files[d+"/f.txt"] = "x\n"
		dirs = append(dirs, d)
	}
	sha := commitIn(t, r, r.Dir, "feat: bulk", files)
	ws := sparseWorktree(t, c, r.Dir, sha)

	require.NoError(t, c.SparseSet(ctx, ws, dirs))
	require.NoError(t, c.PopulateSparse(ctx, ws))
	got, err := c.SparseList(ctx, ws)
	require.NoError(t, err)
	assert.Len(t, got, 5000)
	assert.True(t, exists(filepath.Join(ws, "bulk", "pkg4999", "with-a-fairly-long-directory-name-to-exceed-argv", "f.txt")))
	assert.False(t, exists(filepath.Join(ws, "src")))
}

func TestSparseSetGitFault(t *testing.T) {
	ctx := context.Background()
	r, c, _ := newRepo(t)

	// Exit code: the directory is not a repository, git exits 128.
	err := c.SparseSet(ctx, t.TempDir(), []string{"src"})
	var ge *git.Error
	require.ErrorAs(t, err, &ge)
	assert.NotZero(t, ge.ExitCode)
	assert.Contains(t, err.Error(), "sparse-checkout set")

	// Timeout: the call is killed before git can finish.
	short, err := git.NewExecRunner(git.ExecOptions{Timeout: time.Nanosecond})
	require.NoError(t, err)
	err = git.NewClient(short).SparseSet(ctx, r.Dir, []string{"src"})
	require.ErrorAs(t, err, &ge)
	assert.Equal(t, -1, ge.ExitCode)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestSparseOpsNeedAbsoluteWorktree(t *testing.T) {
	ctx := context.Background()
	c := git.NewClient(&recordingRunner{})
	require.ErrorIs(t, c.SparseDisable(ctx, "rel"), git.ErrInvalidArgument)
	_, err := c.SparseList(ctx, "rel")
	require.ErrorIs(t, err, git.ErrInvalidArgument)
	require.ErrorIs(t, c.PopulateSparse(ctx, "rel"), git.ErrInvalidArgument)
}
