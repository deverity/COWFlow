//go:build linux || darwin

package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCloneFileRefusesSymlink(t *testing.T) {
	dir := cowTestDir(t)
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	dst := filepath.Join(dir, "dst")
	writeRandom(t, target, 100)
	require.NoError(t, os.Symlink(target, link))

	err := cloneFile(context.Background(), link, dst)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrCloneUnsupported, "a symlink is refused, not reported as an unsupported volume")
	assert.NoFileExists(t, dst)
}

func TestCloneFileRefusesDirectoryAsPlainError(t *testing.T) {
	dir := cowTestDir(t)
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.Mkdir(sub, 0o700))
	dst := filepath.Join(dir, "dst")

	err := cloneFile(context.Background(), sub, dst)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrCloneUnsupported)
	assert.NoFileExists(t, dst)
}

func TestCloneFileMissingSource(t *testing.T) {
	dir := cowTestDir(t)
	dst := filepath.Join(dir, "dst")

	err := cloneFile(context.Background(), filepath.Join(dir, "missing"), dst)
	require.ErrorIs(t, err, os.ErrNotExist)
	assert.NoFileExists(t, dst)
}

func TestCloneFileFailureKeepsExistingDestination(t *testing.T) {
	dir := cowTestDir(t)
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	writeRandom(t, src, 100)
	require.NoError(t, os.WriteFile(dst, []byte("keep"), 0o600))

	err := cloneFile(context.Background(), src, dst)
	require.Error(t, err, "an existing destination is refused whether or not the volume clones")
	got, rerr := os.ReadFile(dst) //nolint:gosec // G304: test temp path
	require.NoError(t, rerr)
	assert.Equal(t, "keep", string(got), "a failed clone never removes or overwrites an existing destination")
}

// TestCloneFileXFSWithoutReflink needs a directory on an XFS image made with
// mkfs.xfs -m reflink=0 (the cow-linux CI job mounts one).
func TestCloneFileXFSWithoutReflink(t *testing.T) {
	root := os.Getenv("COWFLOW_TEST_XFS_NOREFLINK_DIR")
	if root == "" {
		t.Skip("COWFLOW_TEST_XFS_NOREFLINK_DIR not set")
	}
	dir, err := os.MkdirTemp(root, "cowflow-clone-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) }) //nolint:gosec // G703: test temp dir; best effort, the root is the caller's
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	writeRandom(t, src, 4096)

	err = cloneFile(context.Background(), src, dst)
	require.ErrorIs(t, err, ErrCloneUnsupported)
	assert.NoFileExists(t, dst)
}
