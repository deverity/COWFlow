package git

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// callRecorder is a fake OptsRunner that records each call and answers from
// a per-subcommand table.
type callRecorder struct {
	calls []recordedCall
	out   map[string]string // first arg after global options → stdout
	err   map[string]error
}

type recordedCall struct {
	dir  string
	args []string
	env  []string
}

func (f *callRecorder) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return f.RunWith(ctx, dir, RunOpts{}, args...)
}

func (f *callRecorder) RunWith(_ context.Context, dir string, o RunOpts, args ...string) ([]byte, error) {
	f.calls = append(f.calls, recordedCall{dir: dir, args: args, env: o.Env})
	key := subcommand(args)
	return []byte(f.out[key]), f.err[key]
}

// subcommand returns the first argument that is not a global option.
func subcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-c":
			i++
		case strings.HasPrefix(args[i], "--work-tree="):
		default:
			return args[i]
		}
	}
	return ""
}

// plainRunner hides RunWith.
type plainRunner struct{ r Runner }

func (p plainRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return p.r.Run(ctx, dir, args...)
}

func absDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func TestClientCommands(t *testing.T) {
	ctx := context.Background()
	repo, wt := absDir(t), absDir(t)
	idx := filepath.Join(absDir(t), "golden.index")
	f := &callRecorder{out: map[string]string{
		"rev-parse": sha1a + "\n",
		"rev-list":  sha1b + "\n",
		"worktree":  "worktree " + wt + "\x00HEAD " + sha1a + "\x00branch refs/heads/b\x00\x00",
	}}
	c := NewClient(f)

	_, err := c.ResolveCommit(ctx, repo, "develop")
	require.NoError(t, err)
	_, err = c.BranchExists(ctx, repo, "cowflow/p/t")
	require.NoError(t, err)
	require.NoError(t, c.DeleteBranchIfAt(ctx, repo, "cowflow/p/t", sha1a))
	require.NoError(t, c.WorktreeAdd(ctx, repo, wt, WorktreeAddOptions{NewBranch: "cowflow/p/t", Commitish: sha1a, NoCheckout: true}))
	require.NoError(t, c.WorktreeAdd(ctx, repo, wt, WorktreeAddOptions{Commitish: "feature/x"}))
	require.NoError(t, c.WorktreeRemove(ctx, repo, wt, true))
	require.NoError(t, c.WorktreeRemove(ctx, repo, wt, false))
	require.NoError(t, c.WorktreePrune(ctx, repo))
	_, err = c.WorktreeList(ctx, repo)
	require.NoError(t, err)
	_, err = c.Head(ctx, wt)
	require.NoError(t, err)
	require.NoError(t, c.ExportTree(ctx, repo, sha1a, wt, idx))
	require.NoError(t, c.RefreshIndex(ctx, repo, wt, idx))
	require.NoError(t, c.RefreshIndex(ctx, repo, wt, ""))
	require.NoError(t, c.ReadTree(ctx, wt, "-mu", "HEAD"))
	_, err = c.Status(ctx, wt)
	require.NoError(t, err)
	_, err = c.Unpublished(ctx, wt, sha1a, "refs/cowflow/changes")
	require.NoError(t, err)

	want := []recordedCall{
		{repo, []string{"rev-parse", "--verify", "--end-of-options", "develop^{commit}"}, nil},
		{repo, []string{"show-ref", "--verify", "--quiet", "refs/heads/cowflow/p/t"}, nil},
		{repo, []string{"update-ref", "-d", "refs/heads/cowflow/p/t", sha1a}, nil},
		{repo, []string{"worktree", "add", "--no-checkout", "-b", "cowflow/p/t", wt, sha1a}, nil},
		{repo, []string{"worktree", "add", wt, "feature/x"}, nil},
		{repo, []string{"worktree", "remove", "--force", wt}, nil},
		{repo, []string{"worktree", "remove", wt}, nil},
		{repo, []string{"worktree", "prune"}, nil},
		{repo, []string{"worktree", "list", "--porcelain", "-z"}, nil},
		{wt, []string{"rev-parse", "--verify", "HEAD"}, nil},
		{repo, []string{"read-tree", "--index-output=" + idx, sha1a}, nil},
		{repo, []string{"-c", "core.autocrlf=false", "-c", "core.longpaths=true", "--work-tree=" + wt,
			"checkout-index", "--all", "--force", "-u"}, []string{"GIT_INDEX_FILE=" + idx}},
		{repo, []string{"--work-tree=" + wt, "update-index", "-q", "--refresh"}, []string{"GIT_INDEX_FILE=" + idx}},
		{repo, []string{"--work-tree=" + wt, "update-index", "-q", "--refresh"}, nil},
		{wt, []string{"read-tree", "-mu", "HEAD"}, nil},
		{wt, []string{"status", "--porcelain=v2", "-z", "--untracked-files=all"}, nil},
		{"", []string{"check-ref-format", "refs/cowflow/changes"}, nil},
		{wt, []string{"rev-list", "HEAD", "--not", sha1a, "--glob=refs/cowflow/changes/*"}, nil},
	}
	assert.Equal(t, want, f.calls)
}

func TestClientRejectsBeforeGit(t *testing.T) {
	ctx := context.Background()
	repo := absDir(t)
	f := &callRecorder{}
	c := NewClient(f)

	checks := map[string]error{
		"relative repo":         c.WorktreePrune(ctx, "rel/repo"),
		"option-like repo":      c.WorktreePrune(ctx, "-C"),
		"nul in path":           c.WorktreeRemove(ctx, repo, repo+"\x00x", false),
		"relative wt path":      c.WorktreeAdd(ctx, repo, "ws", WorktreeAddOptions{Commitish: sha1a}),
		"option commitish":      c.WorktreeAdd(ctx, repo, repo, WorktreeAddOptions{Commitish: "--detach"}),
		"empty commitish":       c.WorktreeAdd(ctx, repo, repo, WorktreeAddOptions{}),
		"option new branch":     c.WorktreeAdd(ctx, repo, repo, WorktreeAddOptions{NewBranch: "-f", Commitish: sha1a}),
		"short sha delete":      c.DeleteBranchIfAt(ctx, repo, "b", "abc123"),
		"upper sha export":      c.ExportTree(ctx, repo, strings.Repeat("ABCDEF0123", 4), repo, filepath.Join(repo, "i")),
		"relative export dst":   c.ExportTree(ctx, repo, sha1a, "dst", filepath.Join(repo, "i")),
		"relative index":        c.ExportTree(ctx, repo, sha1a, repo, "index"),
		"relative refresh idx":  c.RefreshIndex(ctx, repo, repo, "idx"),
		"read-tree bad arg":     c.ReadTree(ctx, repo, "--reset", "HEAD"),
		"read-tree ref name":    c.ReadTree(ctx, repo, "-mu", "develop"),
		"read-tree no tree":     c.ReadTree(ctx, repo, "-mu"),
		"config value option":   c.ConfigSetWorktree(ctx, repo, "core.autocrlf", "--global"),
		"config bad key":        c.ConfigSetWorktree(ctx, repo, "core autocrlf", "false"),
		"git path parent":       errOnly(c.GitPath(ctx, repo, "../config")),
		"git path absolute":     errOnly(c.GitPath(ctx, repo, repo)),
		"git path option":       errOnly(c.GitPath(ctx, repo, "--git-dir")),
		"resolve empty":         errOnly(c.ResolveCommit(ctx, repo, "")),
		"branch exists refs":    errOnly(c.BranchExists(ctx, repo, "refs/heads/x")),
		"branch exists option":  errOnly(c.BranchExists(ctx, repo, "-x")),
		"unpublished bad base":  errOnly(c.Unpublished(ctx, repo, "HEAD~1", "refs/cowflow/changes")),
		"unpublished glob ns":   errOnly(c.Unpublished(ctx, repo, sha1a, "refs/*")),
		"unpublished slash ns":  errOnly(c.Unpublished(ctx, repo, sha1a, "refs/cowflow/")),
		"unpublished not refs/": errOnly(c.Unpublished(ctx, repo, sha1a, "cowflow/changes")),
		"status relative":       errOnly(c.Status(ctx, ".")),
		"head relative":         errOnly(c.Head(ctx, "")),
	}
	for name, err := range checks {
		assert.Error(t, err, name)
		assert.True(t, errors.Is(err, ErrInvalidArgument) || errors.Is(err, ErrInvalidRefName),
			"%s: got %v", name, err)
	}
	assert.Empty(t, f.calls, "rejected arguments must not reach git")
}

func errOnly[T any](_ T, err error) error { return err }

func TestClientOutputValidation(t *testing.T) {
	ctx := context.Background()
	dir := absDir(t)
	for name, out := range map[string]string{
		"no newline":  sha1a,
		"short":       "abc\n",
		"two lines":   sha1a + "\n" + sha1b + "\n",
		"empty":       "",
		"upper":       strings.Repeat("ABCDEF0123", 4) + "\n",
		"with spaces": " " + sha1a + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			c := NewClient(&callRecorder{out: map[string]string{"rev-parse": out}})
			_, err := c.Head(ctx, dir)
			require.ErrorIs(t, err, ErrUnexpectedOutput)
			_, err = c.ResolveCommit(ctx, dir, "HEAD")
			require.ErrorIs(t, err, ErrUnexpectedOutput)
		})
	}
}

func TestClientNeedsOptsRunnerForIndexEnv(t *testing.T) {
	ctx := context.Background()
	dir := absDir(t)
	inner := &callRecorder{}
	c := NewClient(plainRunner{inner})

	err := c.ExportTree(ctx, dir, sha1a, dir, filepath.Join(dir, "i"))
	require.ErrorIs(t, err, ErrOptsUnsupported)
	require.ErrorIs(t, c.RefreshIndex(ctx, dir, dir, filepath.Join(dir, "i")), ErrOptsUnsupported)
	// Without an index file, no options are needed.
	require.NoError(t, c.RefreshIndex(ctx, dir, dir, ""))
}

func TestGitPathResolvesRelative(t *testing.T) {
	ctx := context.Background()
	wt := absDir(t)
	c := NewClient(&callRecorder{out: map[string]string{"rev-parse": ".git/index\n"}})
	p, err := c.GitPath(ctx, wt, "index")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(wt, ".git", "index"), p)

	abs := filepath.Join(absDir(t), "worktrees", "w", "index")
	c = NewClient(&callRecorder{out: map[string]string{"rev-parse": filepath.ToSlash(abs) + "\n"}})
	p, err = c.GitPath(ctx, wt, "index")
	require.NoError(t, err)
	assert.Equal(t, abs, p)

	for _, out := range []string{"", "\n", ".git/index"} {
		c = NewClient(&callRecorder{out: map[string]string{"rev-parse": out}})
		_, err = c.GitPath(ctx, wt, "index")
		require.ErrorIs(t, err, ErrUnexpectedOutput, "%q", out)
	}
}

func TestBranchExistsExitCodes(t *testing.T) {
	ctx := context.Background()
	dir := absDir(t)
	c := NewClient(&callRecorder{err: map[string]error{"show-ref": &Error{ExitCode: 1}}})
	ok, err := c.BranchExists(ctx, dir, "x")
	require.NoError(t, err)
	assert.False(t, ok)

	c = NewClient(&callRecorder{err: map[string]error{"show-ref": &Error{ExitCode: 128}}})
	_, err = c.BranchExists(ctx, dir, "x")
	var ge *Error
	require.ErrorAs(t, err, &ge)
	assert.Equal(t, 128, ge.ExitCode)
}

func TestClientCheckBranchNameRejectsNormalized(t *testing.T) {
	r := realRunner(t)
	c := NewClient(r)
	require.NoError(t, c.CheckBranchName(context.Background(), "cowflow/p/t"))
	require.ErrorIs(t, c.CheckBranchName(context.Background(), "a//b"), ErrInvalidRefName)
	require.ErrorIs(t, c.CheckBranchName(context.Background(), "a..b"), ErrInvalidRefName)
}

func TestValidateCallerEnv(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "index")
	require.NoError(t, validateCallerEnv(nil))
	require.NoError(t, validateCallerEnv([]string{"GIT_INDEX_FILE=" + abs}))
	require.NoError(t, validateCallerEnv([]string{"GIT_AUTHOR_NAME=A B", "GIT_COMMITTER_EMAIL=a@b"}))
	for _, env := range [][]string{
		{"GIT_DIR=" + abs},
		{"GIT_WORK_TREE=" + abs},
		{"GIT_COMMON_DIR=" + abs},
		{"GIT_INDEX_FILE=relative"},
		{"GIT_INDEX_FILE"},
		{"GIT_INDEX_FILE=" + abs + "\x00"},
		{"PATH=/bin"},
		{"GIT_AUTHOR_DATE=2020-01-01"},
		{"GIT_AUTHOR_NAME="},
		{"GIT_COMMITTER_EMAIL=a\nb"},
		{"GIT_AUTHOR_EMAIL=a\x00b"},
	} {
		assert.Error(t, validateCallerEnv(env), "%q", env)
	}
}

func TestRunWithEnvAndStdin(t *testing.T) {
	idx := filepath.Join(t.TempDir(), "index")
	t.Setenv("GIT_INDEX_FILE", filepath.Join(t.TempDir(), "inherited"))
	r := helperRunner(t, "env", testTimeout)

	out, err := r.RunWith(context.Background(), "", RunOpts{Env: []string{"GIT_INDEX_FILE=" + idx}}, "x")
	require.NoError(t, err)
	var got []string
	for _, line := range strings.Split(string(out), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimRight(line, "\r"), "GIT_INDEX_FILE="); ok {
			got = append(got, v)
		}
	}
	assert.Equal(t, []string{idx}, got, "only the caller's GIT_INDEX_FILE")

	_, err = r.RunWith(context.Background(), "", RunOpts{Env: []string{"GIT_DIR=" + idx}}, "x")
	require.Error(t, err)

	r = helperRunner(t, "stdin", testTimeout)
	out, err = r.RunWith(context.Background(), "", RunOpts{Stdin: strings.NewReader("a\nb\n")}, "x")
	require.NoError(t, err)
	assert.Equal(t, "a\nb\n", string(out))
}
