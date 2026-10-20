package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mkdir(t *testing.T, parts ...string) string {
	t.Helper()
	p := filepath.Join(parts...)
	require.NoError(t, os.MkdirAll(p, 0o750))
	return p
}

func touch(t *testing.T, parts ...string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(parts...), nil, 0o600))
}

func TestLocateFrom_ExistingConfigWins(t *testing.T) {
	root := t.TempDir()
	mkdir(t, root, ".git")
	mkdir(t, root, "sub", ".git") // nested repo root is closer, but the config rule wins
	mkdir(t, root, ".cowflow")
	touch(t, root, ".cowflow", "config.toml")
	deep := mkdir(t, root, "sub", "a", "b")

	got, err := LocateFrom(deep)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, ".cowflow", "config.toml"), got)
}

func TestLocateFrom_NearestConfigWins(t *testing.T) {
	root := t.TempDir()
	mkdir(t, root, ".cowflow")
	touch(t, root, ".cowflow", "config.toml")
	inner := mkdir(t, root, "inner")
	mkdir(t, inner, ".cowflow")
	touch(t, inner, ".cowflow", "config.toml")

	got, err := LocateFrom(mkdir(t, inner, "x"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(inner, ".cowflow", "config.toml"), got)
}

func TestLocateFrom_GitRootWhenNoConfig(t *testing.T) {
	root := t.TempDir()
	mkdir(t, root, ".git")
	got, err := LocateFrom(mkdir(t, root, "pkg", "x"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, ".cowflow", "config.toml"), got)
}

func TestLocateFrom_GitFileCounts(t *testing.T) {
	root := t.TempDir()
	touch(t, root, ".git") // a worktree or submodule has a .git file
	got, err := LocateFrom(mkdir(t, root, "pkg"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, ".cowflow", "config.toml"), got)
}

func TestLocateFrom_FallsBackToStartDir(t *testing.T) {
	// A temp dir has no .git or .cowflow above it on a normal machine; the
	// fallback is checked only when no ancestor has either.
	start := mkdir(t, t.TempDir(), "plain")
	got, err := LocateFrom(start)
	require.NoError(t, err)
	if filepath.Dir(filepath.Dir(got)) != start {
		t.Skip("an ancestor of the temp dir is a repository or project")
	}
	assert.Equal(t, filepath.Join(start, ".cowflow", "config.toml"), got)
}

func TestLocateFrom_DirectoryNamedConfigIsNotAConfig(t *testing.T) {
	root := t.TempDir()
	mkdir(t, root, ".git")
	mkdir(t, root, ".cowflow", "config.toml")
	got, err := LocateFrom(root)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, ".cowflow", "config.toml"), got)
}

func TestLocate_UsesWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	mkdir(t, root, ".git")
	sub := mkdir(t, root, "sub")
	t.Chdir(sub)
	got, err := Locate()
	require.NoError(t, err)
	wantRoot, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)
	gotRoot, err := filepath.EvalSymlinks(filepath.Dir(filepath.Dir(got)))
	require.NoError(t, err)
	assert.Equal(t, wantRoot, gotRoot)
	assert.Equal(t, filepath.Join(".cowflow", "config.toml"), filepath.Join(filepath.Base(filepath.Dir(got)), filepath.Base(got)))
}

func TestStatePath(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), ".cowflow", "config.toml")
	dir := filepath.Dir(cfgPath)

	c := Default()
	assert.Equal(t, filepath.Join(dir, "state"), StatePath(cfgPath, c))
	assert.Equal(t, filepath.Join(dir, "state", "cowflow.db"), DBPath(cfgPath, c))

	c.Daemon.StateDir = "data"
	assert.Equal(t, filepath.Join(dir, "data"), StatePath(cfgPath, c))

	abs := filepath.Join(t.TempDir(), "abs-state")
	c.Daemon.StateDir = abs
	assert.Equal(t, abs, StatePath(cfgPath, c))
	assert.Equal(t, filepath.Join(abs, "cowflow.db"), DBPath(cfgPath, c))
}
