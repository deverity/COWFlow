package config

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSave_RoundTrip(t *testing.T) {
	want, err := Load(filepath.Join("testdata", "spec22.toml"))
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, Save(path, want))

	got, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestSave_KeepsBackup(t *testing.T) {
	c, err := Load(filepath.Join("testdata", "spec22.toml"))
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("# hand edited\n"), 0o600))
	require.NoError(t, Save(path, c))

	bak, err := os.ReadFile(path + ".bak") //nolint:gosec // G304: test temp path
	require.NoError(t, err)
	require.Equal(t, "# hand edited\n", string(bak))
}

func TestSave_FailureLeavesOriginal(t *testing.T) {
	orig := encodeConfig
	t.Cleanup(func() { encodeConfig = orig })
	encodeConfig = func(w io.Writer, _ *Config) error {
		_, _ = w.Write([]byte("partial"))
		return errors.New("disk full")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("original\n"), 0o600))

	err := Save(path, Default())
	require.ErrorContains(t, err, "disk full")

	got, rerr := os.ReadFile(path) //nolint:gosec // G304: test temp path
	require.NoError(t, rerr)
	require.Equal(t, "original\n", string(got))
	entries, rerr := os.ReadDir(dir)
	require.NoError(t, rerr)
	require.Len(t, entries, 1, "temporary file must be removed")
}

func TestLocate_DoesNotGuess(t *testing.T) {
	p, err := Locate()
	require.Empty(t, p)
	require.ErrorContains(t, err, "U-API-1")
}
