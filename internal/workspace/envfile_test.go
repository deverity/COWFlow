package workspace

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/config"
	"github.com/deverity/cowflow/internal/platform"
)

type envVolumes struct {
	same bool
	err  error
}

func (envVolumes) VolumeOf(context.Context, string) (platform.Volume, error) {
	return platform.Volume{}, nil
}

func (v envVolumes) SameVolume(context.Context, string, string) (bool, error) {
	return v.same, v.err
}

func envCfg(root string) *config.Config {
	return &config.Config{Cache: config.Cache{
		GoModCache:  filepath.Join(root, "gomod"),
		GoCache:     filepath.Join(root, "gocache"),
		PnpmStore:   filepath.Join(root, "pnpm"),
		CargoTarget: filepath.Join(root, "cargo"),
		GradleHome:  filepath.Join(root, "gradle"),
		PipCache:    filepath.Join(root, "pip"),
	}}
}

func envWS(t *testing.T, id string) *Workspace {
	t.Helper()
	p := filepath.Join(t.TempDir(), "ws")
	require.NoError(t, os.MkdirAll(p, 0o750))
	return &Workspace{
		ID: id, ProjectID: "prj_01", TaskID: "tsk_01", Path: p,
		Ports: PortRange{Start: 40000, End: 40009},
	}
}

func readEnv(t *testing.T, w *Workspace) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(w.Path, EnvFileName))
	require.NoError(t, err)
	return string(b)
}

func TestEnvFileGolden(t *testing.T) {
	root := t.TempDir()
	w := envWS(t, "wks_01")
	require.NoError(t, NewEnvFileWriter(envCfg(root), nil, nil).Write(t.Context(), w))

	got := strings.ReplaceAll(filepath.ToSlash(readEnv(t, w)), filepath.ToSlash(root), "<ROOT>")
	want, err := os.ReadFile(filepath.Join("testdata", "envfile.golden"))
	require.NoError(t, err)
	assert.Equal(t, string(want), got)
	for _, d := range []string{"gomod", "gocache", "pnpm", "cargo/wks_01", "gradle", "pip"} {
		assert.DirExists(t, filepath.Join(root, filepath.FromSlash(d)))
	}
}

func TestEnvFileAtomic(t *testing.T) {
	w := envWS(t, "wks_01")
	e := NewEnvFileWriter(envCfg(t.TempDir()), nil, nil)
	e.rename = func(string, string) error { return errors.New("injected fault") }
	err := e.Write(t.Context(), w)
	require.ErrorContains(t, err, "injected fault")
	assert.NoFileExists(t, filepath.Join(w.Path, EnvFileName))
	assert.NoFileExists(t, filepath.Join(w.Path, EnvFileName+".tmp"))
}

func TestEnvFileOverwritesExisting(t *testing.T) {
	w := envWS(t, "wks_01")
	require.NoError(t, os.WriteFile(filepath.Join(w.Path, EnvFileName), []byte("old"), 0o600))
	require.NoError(t, NewEnvFileWriter(envCfg(t.TempDir()), nil, nil).Write(t.Context(), w))
	assert.Contains(t, readEnv(t, w), "COWFLOW_WORKSPACE_ID=wks_01")
}

func TestCargoTargetPerWorkspace(t *testing.T) {
	root := t.TempDir()
	e := NewEnvFileWriter(envCfg(root), nil, nil)
	a, b := envWS(t, "wks_a"), envWS(t, "wks_b")
	require.NoError(t, e.Write(t.Context(), a))
	require.NoError(t, e.Write(t.Context(), b))
	assert.Contains(t, readEnv(t, a), "CARGO_TARGET_DIR="+filepath.Join(root, "cargo", "wks_a")+"\n")
	assert.Contains(t, readEnv(t, b), "CARGO_TARGET_DIR="+filepath.Join(root, "cargo", "wks_b")+"\n")
}

func TestSharedCachesIdenticalAcrossWorkspaces(t *testing.T) {
	root := t.TempDir()
	e := NewEnvFileWriter(envCfg(root), nil, nil)
	a, b := envWS(t, "wks_a"), envWS(t, "wks_b")
	require.NoError(t, e.Write(t.Context(), a))
	require.NoError(t, e.Write(t.Context(), b))
	for _, k := range []string{"GOMODCACHE", "GOCACHE", "npm_config_store_dir", "GRADLE_USER_HOME", "PIP_CACHE_DIR"} {
		assert.Equal(t, envLine(t, readEnv(t, a), k), envLine(t, readEnv(t, b), k), k)
	}
}

func envLine(t *testing.T, content, key string) string {
	t.Helper()
	for _, l := range strings.Split(content, "\n") {
		if strings.HasPrefix(l, key+"=") {
			return l
		}
	}
	t.Fatalf("%s not in %q", key, content)
	return ""
}

func TestEmptyCacheKeysOmitted(t *testing.T) {
	w := envWS(t, "wks_01")
	require.NoError(t, NewEnvFileWriter(&config.Config{}, nil, nil).Write(t.Context(), w))
	got := readEnv(t, w)
	assert.NotContains(t, got, "GOMODCACHE")
	assert.NotContains(t, got, "CARGO_TARGET_DIR")
	assert.Contains(t, got, "COWFLOW_PORT_RANGE_END=40009\n")
}

func TestEnvRejectsNewlineInValue(t *testing.T) {
	for _, bad := range []string{"a\nb", "a\rb", "a\x00b"} {
		w := envWS(t, "wks_01")
		cfg := envCfg(t.TempDir())
		cfg.Cache.GoCache = filepath.Join(t.TempDir(), "x"+bad)
		err := NewEnvFileWriter(cfg, nil, nil).Write(t.Context(), w)
		require.ErrorIs(t, err, ErrBadCachePath, "%q", bad)
		assert.NoFileExists(t, filepath.Join(w.Path, EnvFileName))
	}
	w := envWS(t, "wks_01")
	w.TaskID = "tsk\nINJECT=1"
	require.Error(t, NewEnvFileWriter(envCfg(t.TempDir()), nil, nil).Write(t.Context(), w))
	assert.NoFileExists(t, filepath.Join(w.Path, EnvFileName))
}

func TestEnvRejectsRelativeAndInWorkspaceCache(t *testing.T) {
	w := envWS(t, "wks_01")
	cfg := envCfg(t.TempDir())
	cfg.Cache.GoCache = "relative/cache"
	require.ErrorIs(t, NewEnvFileWriter(cfg, nil, nil).Write(t.Context(), w), ErrBadCachePath)

	cfg = envCfg(t.TempDir())
	cfg.Cache.GoCache = filepath.Join(w.Path, "cache")
	require.ErrorIs(t, NewEnvFileWriter(cfg, nil, nil).Write(t.Context(), w), ErrBadCachePath)
	assert.NoDirExists(t, filepath.Join(w.Path, "cache"))
}

func TestEnvCleansCachePath(t *testing.T) {
	root := t.TempDir()
	cfg := envCfg(root)
	cfg.Cache.GoCache = filepath.Join(root, "a", "..", "gocache") + string(filepath.Separator)
	w := envWS(t, "wks_01")
	require.NoError(t, NewEnvFileWriter(cfg, nil, nil).Write(t.Context(), w))
	assert.Contains(t, readEnv(t, w), "GOCACHE="+filepath.Join(root, "gocache")+"\n")
}

func TestEnvRejectsBadWorkspaceID(t *testing.T) {
	for _, id := range []string{"", "..", "a/b", `a\b`} {
		w := envWS(t, id)
		require.ErrorIs(t, NewEnvFileWriter(envCfg(t.TempDir()), nil, nil).Write(t.Context(), w), ErrBadCachePath, "%q", id)
	}
}

func TestCacheOutsideDevDriveWarns(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	cfg := envCfg(t.TempDir())
	cfg.DevDrive = config.DevDrive{Enabled: true, Mount: t.TempDir()}

	w := envWS(t, "wks_01")
	require.NoError(t, NewEnvFileWriter(cfg, envVolumes{same: false}, log).Write(t.Context(), w))
	assert.Contains(t, buf.String(), "not on the Dev Drive")
	assert.FileExists(t, filepath.Join(w.Path, EnvFileName), "a warning never fails the write")

	buf.Reset()
	require.NoError(t, NewEnvFileWriter(cfg, envVolumes{same: true}, log).Write(t.Context(), envWS(t, "wks_02")))
	assert.NotContains(t, buf.String(), "Dev Drive")

	buf.Reset()
	cfg.DevDrive.Enabled = false
	require.NoError(t, NewEnvFileWriter(cfg, envVolumes{same: false}, log).Write(t.Context(), envWS(t, "wks_03")))
	assert.Empty(t, buf.String(), "disabled Dev Drive: no warning")

	buf.Reset()
	cfg.DevDrive.Enabled = true
	require.NoError(t, NewEnvFileWriter(cfg, envVolumes{err: errors.New("probe")}, log).Write(t.Context(), envWS(t, "wks_04")))
	assert.Contains(t, buf.String(), "cannot tell")
}
