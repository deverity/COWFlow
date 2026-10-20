package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidRescueID(t *testing.T) {
	t.Parallel()
	for id, want := range map[string]bool{
		"wks_01HZX3": true, "": false, ".": false, "..": false, "a/b": false, `a\b`: false,
		"a b": false, "a..b": false, ".hidden": false, "x.lock": false, "a:b": false, "a~1": false,
	} {
		assert.Equal(t, want, validRescueID(id), "%q", id)
	}
}

func TestCopyIndex(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	idx := filepath.Join(dir, "index")
	require.NoError(t, os.WriteFile(idx, []byte("DIRC-data"), 0o600))
	tmp, err := copyIndex(idx)
	require.NoError(t, err)
	assert.Equal(t, dir, filepath.Dir(tmp), "next to the index, outside the workspace")
	b, err := os.ReadFile(tmp) //nolint:gosec // test path
	require.NoError(t, err)
	assert.Equal(t, "DIRC-data", string(b))

	// No index yet: a fresh path git creates, never an empty file.
	missing, err := copyIndex(filepath.Join(dir, "none"))
	require.NoError(t, err)
	assert.NoFileExists(t, missing)
}

func TestRescueMessage(t *testing.T) {
	t.Parallel()
	msg := rescueMessage(Workspace{ID: "wks_1", TaskID: "tsk_1", Path: filepath.Join("root", "auth")})
	assert.Equal(t, "cowflow rescue: workspace wks_1 (task auth) before destroy --force\n\n"+
		"Cowflow-Workspace: wks_1\nCowflow-Task: tsk_1\n", msg)
}
