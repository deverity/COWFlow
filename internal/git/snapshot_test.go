package git_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/git"
)

// tempIndex copies the repository's index to a fresh file next to it.
func tempIndex(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, ".git", "index")) //nolint:gosec // test path
	require.NoError(t, err)
	p := filepath.Join(dir, ".git", "snap-index")
	require.NoError(t, os.WriteFile(p, data, 0o600)) //nolint:gosec // test path
	return p
}

func TestSnapshotPlumbing(t *testing.T) {
	ctx := context.Background()
	r, c, _ := newRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(r.Dir, ".gitignore"), []byte("ignored.txt\n"), 0o600))
	gitIn(t, r, r.Dir, "add", ".gitignore")
	gitIn(t, r, r.Dir, "commit", "-q", "-m", "chore: ignore")
	head := strings.TrimSpace(gitIn(t, r, r.Dir, "rev-parse", "HEAD"))

	// "*.go" is excluded literally: it drops the file of that name only.
	hostile := []string{"-rf.txt", "a b.txt", "unié.txt", "[x].txt", "--force"}
	if runtime.GOOS != "windows" {
		// NTFS and ReFS refuse '*' and ':' in file names, so these two
		// cannot exist there. The literal "*.go" exclude still runs and
		// must keep src/main.go.
		hostile = append(hostile, "*.go", ":(glob)x")
	}
	files := append([]string{"new.txt", "ignored.txt", ".env.cowflow", "dir with space/skip me"}, hostile...)
	for _, f := range files {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(r.Dir, f)), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(r.Dir, f), []byte("x\n"), 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(r.Dir, "README.md"), []byte("changed\n"), 0o600))

	idxBefore, err := os.ReadFile(filepath.Join(r.Dir, ".git", "index"))
	require.NoError(t, err)
	idx := tempIndex(t, r.Dir)

	require.NoError(t, c.AddAll(ctx, r.Dir, idx, []string{".env.cowflow", "dir with space/skip me", "*.go"}))
	tree, err := c.WriteTree(ctx, r.Dir, idx)
	require.NoError(t, err)

	// Inherited identity must not win over the rescue identity.
	t.Setenv("GIT_AUTHOR_NAME", "Inherited")
	t.Setenv("GIT_COMMITTER_EMAIL", "inherited@example.com")
	runner, err := git.NewExecRunner(git.ExecOptions{Timeout: 30e9})
	require.NoError(t, err)
	ic := git.NewClient(runner)
	sha, err := ic.CommitTree(ctx, r.Dir, tree, head, "COWFlow", "cowflow@localhost", "msg line\n\nTrailer: v\n")
	require.NoError(t, err)
	require.True(t, git.IsSHA(sha))

	ref := "refs/cowflow/rescue/wks_01"
	require.NoError(t, c.UpdateRef(ctx, r.Dir, ref, sha))
	assert.Equal(t, sha, strings.TrimSpace(gitIn(t, r, r.Dir, "rev-parse", ref)))
	assert.Equal(t, "COWFlow <cowflow@localhost> COWFlow <cowflow@localhost>",
		strings.TrimSpace(gitIn(t, r, r.Dir, "log", "-1", "--format=%an <%ae> %cn <%ce>", ref)))
	assert.Equal(t, "msg line\n\nTrailer: v", strings.TrimSpace(gitIn(t, r, r.Dir, "log", "-1", "--format=%B", ref)))
	assert.Equal(t, head, strings.TrimSpace(gitIn(t, r, r.Dir, "rev-parse", ref+"^")))

	listed := strings.Split(strings.Trim(gitIn(t, r, r.Dir, "ls-tree", "-r", "--name-only", "-z", ref), "\x00"), "\x00")
	added := slices.DeleteFunc(slices.Clone(hostile), func(f string) bool { return f == "*.go" })
	for _, want := range append([]string{"new.txt", "README.md", "src/main.go"}, added...) {
		assert.Contains(t, listed, want)
	}
	for _, not := range []string{"ignored.txt", ".env.cowflow", "dir with space/skip me", "*.go"} {
		assert.NotContains(t, listed, not)
	}
	assert.Equal(t, "changed\n", gitIn(t, r, r.Dir, "show", ref+":README.md"))

	// The worktree's own index and branch are untouched.
	idxAfter, err := os.ReadFile(filepath.Join(r.Dir, ".git", "index"))
	require.NoError(t, err)
	assert.Equal(t, idxBefore, idxAfter)
	assert.Equal(t, head, strings.TrimSpace(gitIn(t, r, r.Dir, "rev-parse", "HEAD")))

	// Overwrites an existing ref.
	require.NoError(t, c.UpdateRef(ctx, r.Dir, ref, head))
	assert.Equal(t, head, strings.TrimSpace(gitIn(t, r, r.Dir, "rev-parse", ref)))
}

func TestSnapshotRejectsBadArguments(t *testing.T) {
	ctx := context.Background()
	r, c, head := newRepo(t)
	idx := filepath.Join(r.Dir, ".git", "x-index")
	for name, err := range map[string]error{
		"add relative worktree": c.AddAll(ctx, "rel", idx, nil),
		"add relative index":    c.AddAll(ctx, r.Dir, "idx", nil),
		"add empty exclude":     c.AddAll(ctx, r.Dir, idx, []string{""}),
		"add parent exclude":    c.AddAll(ctx, r.Dir, idx, []string{"a/../../b"}),
		"add absolute exclude":  c.AddAll(ctx, r.Dir, idx, []string{"/etc/passwd"}),
		"add magic exclude":     c.AddAll(ctx, r.Dir, idx, []string{":(top)x"}),
		"add nul exclude":       c.AddAll(ctx, r.Dir, idx, []string{"a\x00b"}),
		"update ref bad sha":    c.UpdateRef(ctx, r.Dir, "refs/x/y", "HEAD"),
		"update ref option":     c.UpdateRef(ctx, r.Dir, "--delete", head),
		"update ref not refs":   c.UpdateRef(ctx, r.Dir, "heads/x", head),
		"update ref bad name":   c.UpdateRef(ctx, r.Dir, "refs/a..b", head),
		"update ref space":      c.UpdateRef(ctx, r.Dir, "refs/a b", head),
	} {
		require.Error(t, err, name)
	}
	_, err := c.WriteTree(ctx, r.Dir, "idx")
	require.Error(t, err)
	for name, a := range map[string][5]string{
		"bad tree":     {"HEAD", head, "n", "e@x", "m"},
		"bad parent":   {head, "-x", "n", "e@x", "m"},
		"empty name":   {head, head, "", "e@x", "m"},
		"newline mail": {head, head, "n", "e\n@x", "m"},
		"nul message":  {head, head, "n", "e@x", "m\x00"},
	} {
		_, err := c.CommitTree(ctx, r.Dir, a[0], a[1], a[2], a[3], a[4])
		require.Error(t, err, name)
	}
	_, err = c.CommitTree(ctx, "rel", head, head, "n", "e@x", "m")
	require.Error(t, err)
}

func TestCommitTreeNeedsOptsRunner(t *testing.T) {
	r, _, head := newRepo(t)
	c := git.NewClient(plainRunner{r.Runner})
	_, err := c.CommitTree(context.Background(), r.Dir, head, head, "n", "e@x", "m")
	require.ErrorIs(t, err, git.ErrOptsUnsupported)
}

// plainRunner hides RunWith.
type plainRunner struct{ git.Runner }
