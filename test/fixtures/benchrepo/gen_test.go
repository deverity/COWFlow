package benchrepo_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/git"
	"github.com/deverity/cowflow/test/fixtures/benchrepo"
)

// tiny keeps the tests that do not need the real profile fast.
var tiny = benchrepo.Profile{Name: "tiny", Files: 200, TargetSize: 1 << 20,
	MinSize: 256, MaxSize: 64 << 10, Median: 4 << 10, Sigma: 1.0}

func runner(t *testing.T) git.Runner {
	t.Helper()
	empty := filepath.Join(t.TempDir(), "gitconfig")
	r, err := git.NewExecRunner(git.ExecOptions{Timeout: 60e9,
		ExtraEnv: []string{"GIT_CONFIG_GLOBAL=" + empty, "GIT_CONFIG_NOSYSTEM=1"}})
	if err != nil {
		t.Skipf("git not available: %v", err)
	}
	return r
}

func gitOut(t *testing.T, r git.Runner, dir string, args ...string) string {
	t.Helper()
	out, err := r.Run(context.Background(), dir, args...)
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}

func generate(t *testing.T, p benchrepo.Profile, seed int64) (string, benchrepo.Report) {
	t.Helper()
	runner(t) // skips when git is missing
	dir := filepath.Join(t.TempDir(), "repo")
	rep, err := benchrepo.Generate(context.Background(), dir, p, seed)
	require.NoError(t, err)
	return dir, rep
}

func TestGenerateDeterministic(t *testing.T) {
	_, a := generate(t, tiny, 7)
	_, b := generate(t, tiny, 7)
	_, c := generate(t, tiny, 8)

	assert.Equal(t, a, b, "the same seed must give the same trees and commits")
	assert.NotEqual(t, a.HeadTree, c.HeadTree, "another seed must change the content")
	assert.Equal(t, a.Bytes, c.Bytes, "sizes are fixed by the profile, not the seed")
}

func TestGenerateSmallProfile(t *testing.T) {
	dir, rep := generate(t, benchrepo.Small, 1)
	r := runner(t)

	assert.InEpsilon(t, float64(50<<20), float64(rep.Bytes), 0.05, "size within 5%% of 50 MiB")
	assert.InDelta(t, 2000, rep.Files, 20)
	assert.Len(t, rep.HeadTree, 40)
	require.Contains(t, rep.Branches, "main")
	require.Contains(t, rep.Branches, "develop")

	// Two commits on main; develop adds a third that changes 1% of the files.
	assert.Equal(t, "2", gitOut(t, r, dir, "rev-list", "--count", "main"))
	assert.Equal(t, "3", gitOut(t, r, dir, "rev-list", "--count", "develop"))
	assert.Equal(t, rep.Branches["main"], gitOut(t, r, dir, "merge-base", "main", "develop"))
	changed := strings.Fields(gitOut(t, r, dir, "diff", "--name-only", "main", "develop"))
	assert.Len(t, changed, 20, "1%% of 2,000 files")

	// Structure and policy files.
	assert.Equal(t, "* text=auto eol=lf", gitOut(t, r, dir, "show", "main:.gitattributes"))
	assert.Contains(t, gitOut(t, r, dir, "show", "main:go.mod"), "module ")
	assert.NotEmpty(t, gitOut(t, r, dir, "ls-tree", "-r", "--name-only", "main", "contracts/"))
	assert.NotEmpty(t, gitOut(t, r, dir, "ls-tree", "--name-only", "main", "src/"))
	assert.Equal(t, "main", gitOut(t, r, dir, "symbolic-ref", "--short", "HEAD"))
	assert.Empty(t, gitOut(t, r, dir, "status", "--porcelain"), "main must be checked out and clean")
	assert.NotContains(t, gitOut(t, r, dir, "ls-tree", "-r", "main"), "120000", "no symlinks without Symlinks")

	// Inspect agrees with the report Generate returned.
	again, err := benchrepo.Inspect(context.Background(), dir)
	require.NoError(t, err)
	assert.Equal(t, rep, again)
}

func TestGenerateWindowsSafeNames(t *testing.T) {
	p := tiny
	p.Symlinks = true
	dir, _ := generate(t, p, 3)
	r := runner(t)

	out := gitOut(t, r, dir, "ls-tree", "-r", "--name-only", "-z", "develop")
	names := strings.Split(strings.Trim(out, "\x00"), "\x00")
	require.GreaterOrEqual(t, len(names), tiny.Files)
	seen := map[string]bool{}
	for _, n := range names {
		require.NoError(t, benchrepo.CheckPath(n), n)
		lower := strings.ToLower(n)
		assert.False(t, seen[lower], "case collision on %q", n)
		seen[lower] = true
	}
	assert.Contains(t, gitOut(t, r, dir, "ls-tree", "-r", "main"), "120000 blob", "--symlinks adds links")
}

func TestCheckPath(t *testing.T) {
	for _, ok := range []string{"src/auth/pkg0001/f000001.go", ".gitattributes", "contracts/api.v1.md", "a/b.c/d"} {
		assert.NoError(t, benchrepo.CheckPath(ok), ok)
	}
	long := strings.Repeat("a/", 91) + "b"
	for _, bad := range []string{
		"", "/abs", "a//b", "a/../b", "a\\b", "CON", "src/aux.c", "dir/nul.txt", "Com1", "x/LPT9.log",
		"trailing.", "dir /file", "dir/file ", "a:b", "a*b", "a?b", "a\"b", "a<b", "a>b", "a|b", "a\x01b", long,
	} {
		assert.Error(t, benchrepo.CheckPath(bad), "%q must be rejected", bad)
	}
	assert.NoError(t, benchrepo.CheckPath("console.txt"))
	assert.NoError(t, benchrepo.CheckPath("com10.txt"))
}

func TestGenerateRejectsBadInput(t *testing.T) {
	runner(t)
	ctx := context.Background()

	dir := t.TempDir()
	assert.NoError(t, writeFile(filepath.Join(dir, "x")))
	_, err := benchrepo.Generate(ctx, dir, tiny, 1)
	assert.ErrorContains(t, err, "not empty")

	bad := tiny
	bad.TargetSize = 1 // smaller than Files * MinSize
	_, err = benchrepo.Generate(ctx, filepath.Join(t.TempDir(), "r"), bad, 1)
	assert.ErrorContains(t, err, "outside")

	_, err = benchrepo.ProfileByName("nope")
	assert.Error(t, err)
	p, err := benchrepo.ProfileByName("monorepo")
	require.NoError(t, err)
	assert.Equal(t, benchrepo.Monorepo, p)
}

func TestGenerateHonorsCancellation(t *testing.T) {
	runner(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := benchrepo.Generate(ctx, filepath.Join(t.TempDir(), "r"), tiny, 1)
	assert.Error(t, err)
}

func TestExternalRepo(t *testing.T) {
	t.Setenv(benchrepo.EnvRepo, "")
	_, ok := benchrepo.ExternalRepo()
	assert.False(t, ok)
	t.Setenv(benchrepo.EnvRepo, "/some/repo")
	v, ok := benchrepo.ExternalRepo()
	assert.True(t, ok)
	assert.Equal(t, "/some/repo", v)
}
