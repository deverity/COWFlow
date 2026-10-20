package goproxy_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/test/fixtures/goproxy"
)

func TestGoproxyBuildsOffline(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("go command not available: %v", err)
	}
	f, err := goproxy.Generate(t.TempDir(), 5)
	require.NoError(t, err)
	require.Len(t, f.Modules, 5)

	modCache := t.TempDir()
	env := append(os.Environ(), f.Env()...)
	// The module cache is read-only by default, which breaks t.TempDir cleanup.
	env = append(env, "GOFLAGS=-mod=mod -modcacherw", "GOMODCACHE="+modCache, "GOPATH="+t.TempDir())

	run := func(args ...string) string {
		cmd := exec.Command(goBin, args...) //nolint:gosec // G204: the go binary found on PATH and fixed arguments
		cmd.Dir = f.ConsumerDir
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
		return string(out)
	}
	run("build", "-o", filepath.Join(t.TempDir(), "consumer"), ".")
	assert.Equal(t, "15", strings.TrimSpace(run("run", ".")), "1+2+3+4+5 from the five modules")

	// Everything came from the proxy: the cache holds each module once.
	for _, m := range f.Modules {
		assert.DirExists(t, filepath.Join(modCache, filepath.FromSlash(m)+"@v1.0.0"))
	}
}

func TestGenerateIsReproducible(t *testing.T) {
	a, err := goproxy.Generate(t.TempDir(), 3)
	require.NoError(t, err)
	b, err := goproxy.Generate(t.TempDir(), 3)
	require.NoError(t, err)

	rel := filepath.FromSlash(a.Modules[1]) + "/@v/v1.0.0.zip"
	za, err := os.ReadFile(filepath.Join(a.ProxyDir, rel)) //nolint:gosec // G304: path built from the test's own temp dir
	require.NoError(t, err)
	zb, err := os.ReadFile(filepath.Join(b.ProxyDir, rel)) //nolint:gosec // G304: path built from the test's own temp dir
	require.NoError(t, err)
	assert.Equal(t, za, zb)
}

func TestGenerateRejectsBadInput(t *testing.T) {
	_, err := goproxy.Generate(t.TempDir(), 0)
	assert.Error(t, err)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "x"), []byte("x"), 0o600))
	_, err = goproxy.Generate(dir, 1)
	assert.ErrorContains(t, err, "not empty")
}

func TestEnvIsOffline(t *testing.T) {
	f, err := goproxy.Generate(t.TempDir(), 1)
	require.NoError(t, err)
	env := strings.Join(f.Env(), "\n")
	assert.Contains(t, env, "GOPROXY=file:///")
	assert.NotContains(t, env, "direct")
	for _, want := range []string{"GOSUMDB=off", "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local"} {
		assert.Contains(t, env, want)
	}
}
