package harness_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/test/harness"
)

func gitOut(t *testing.T, r *harness.Repo, args ...string) string {
	t.Helper()
	out, err := r.Runner.Run(context.Background(), r.Dir, args...)
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}

func TestRepo_Basics(t *testing.T) {
	r := harness.NewRepo(t)

	assert.Equal(t, "develop", gitOut(t, r, "symbolic-ref", "--short", "HEAD"))
	assert.Equal(t, "false", gitOut(t, r, "config", "core.autocrlf"))
	attrs, err := os.ReadFile(filepath.Join(r.Dir, ".gitattributes"))
	require.NoError(t, err)
	assert.Equal(t, "* text=auto eol=lf\n", string(attrs))

	sha := r.Commit(t, "feat: add file", map[string]string{"a/b/c.txt": "hi\n"})
	assert.Equal(t, sha, gitOut(t, r, "rev-parse", "HEAD"))
	assert.Equal(t, "hi", gitOut(t, r, "show", "HEAD:a/b/c.txt"))
	assert.Equal(t, "feat: add file", gitOut(t, r, "log", "-1", "--format=%s"))
}

func TestRepo_OnTmpfsWhenAvailable(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("tmpfs location is Linux-only (SPEC-GAPS U-PLAT-8)")
	}
	if fi, err := os.Stat("/dev/shm"); err != nil || !fi.IsDir() {
		t.Skip("/dev/shm not available")
	}
	r := harness.NewRepo(t)
	if !strings.HasPrefix(r.Dir, "/dev/shm/") {
		t.Skipf("/dev/shm not writable; fell back to %s", r.Dir)
	}
}

func TestRepo_Options(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "repo")
	r := harness.NewRepo(t, harness.WithDir(dir), harness.WithBranch("main"), harness.WithAttributes(""))

	assert.Equal(t, dir, r.Dir)
	assert.Equal(t, "main", gitOut(t, r, "symbolic-ref", "--short", "HEAD"))
	assert.NoFileExists(t, filepath.Join(dir, ".gitattributes"))
	_, err := r.Runner.Run(context.Background(), dir, "rev-parse", "--verify", "HEAD")
	assert.Error(t, err, "no initial commit without attributes")
	assert.NotEmpty(t, r.Commit(t, "chore: empty", nil))
}

func TestRepo_RejectsEscapingPaths(t *testing.T) {
	r := harness.NewRepo(t)
	ft := &fatalCatcher{TB: t}
	assert.Panics(t, func() { r.Commit(ft, "x", map[string]string{"../evil": "x"}) })
}

// fatalCatcher turns Fatalf into a panic so a test can assert it.
type fatalCatcher struct{ testing.TB }

func (fatalCatcher) Fatalf(string, ...any) { panic("fatal") }

func TestRepo_Deterministic(t *testing.T) {
	build := func() (first, second string) {
		r := harness.NewRepo(t)
		first = r.Commit(t, "feat: one", map[string]string{"x.txt": "1\n", "d/y.txt": "2\n"})
		second = r.Commit(t, "feat: two", map[string]string{"x.txt": "3\n"})
		return first, second
	}
	a1, a2 := build()
	b1, b2 := build()
	assert.Equal(t, a1, b1)
	assert.Equal(t, a2, b2)
	assert.NotEqual(t, a1, a2)
}

func TestRepo_IgnoresUserConfig(t *testing.T) {
	evil := filepath.Join(t.TempDir(), "evil.gitconfig")
	require.NoError(t, os.WriteFile(evil, []byte("[user]\n\tname = evil\n\temail = evil@example.com\n[core]\n\tautocrlf = true\n"), 0o600))
	t.Setenv("GIT_CONFIG_GLOBAL", evil)
	t.Setenv("GIT_AUTHOR_NAME", "evil")
	fakeHome := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(fakeHome, ".gitconfig"), []byte("[user]\n\tname = evil\n"), 0o600))
	t.Setenv("HOME", fakeHome)
	t.Setenv("USERPROFILE", fakeHome)

	r := harness.NewRepo(t)
	r.Commit(t, "feat: x", map[string]string{"x.txt": "x\n"})

	assert.Equal(t, "COWFlow Harness <harness@cowflow.invalid>",
		gitOut(t, r, "log", "-1", "--format=%an <%ae>"))
	assert.Equal(t, "COWFlow Harness <harness@cowflow.invalid>",
		gitOut(t, r, "log", "-1", "--format=%cn <%ce>"))
	assert.Equal(t, "false", gitOut(t, r, "config", "core.autocrlf"))
}
