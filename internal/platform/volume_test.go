package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func volumeOrSkip(t *testing.T, path string) Volume {
	t.Helper()
	v, err := VolumeOf(context.Background(), path)
	if errors.Is(err, ErrUnsupported) {
		t.Skip("unsupported OS")
	}
	require.NoError(t, err)
	return v
}

func TestVolumeOfTempDir(t *testing.T) {
	v := volumeOrSkip(t, t.TempDir())
	require.NotEmpty(t, v.Filesystem)
	require.Equal(t, strings.ToLower(v.Filesystem), v.Filesystem)
	require.NotEmpty(t, v.ID)
	require.NotEmpty(t, v.MountPath)
}

func TestVolumeOfCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := VolumeOf(ctx, t.TempDir())
	require.ErrorIs(t, err, context.Canceled)
}

func TestVolumeOfMissingPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("GetVolumePathName succeeds for paths that do not exist")
	}
	volumeOrSkip(t, t.TempDir())
	_, err := VolumeOf(context.Background(), filepath.Join(t.TempDir(), "missing"))
	require.Error(t, err)
}

func TestSameVolumeSameDir(t *testing.T) {
	volumeOrSkip(t, t.TempDir())
	dir := t.TempDir()
	same, err := SameVolume(context.Background(), dir, dir)
	require.NoError(t, err)
	require.True(t, same)
}

func TestSameVolumeNonexistentPath(t *testing.T) {
	volumeOrSkip(t, t.TempDir())
	dir := t.TempDir()
	future := filepath.Join(dir, "a", "b", "c")
	same, err := SameVolume(context.Background(), dir, future)
	require.NoError(t, err)
	require.True(t, same)
}

func TestFreeBytesPositive(t *testing.T) {
	volumeOrSkip(t, t.TempDir())
	n, err := FreeBytes(t.TempDir())
	require.NoError(t, err)
	require.Positive(t, n)
}

func TestFreeBytesNonexistentPath(t *testing.T) {
	volumeOrSkip(t, t.TempDir())
	n, err := FreeBytes(filepath.Join(t.TempDir(), "not", "yet"))
	require.NoError(t, err)
	require.Positive(t, n)
}

func TestNearestExisting(t *testing.T) {
	dir := t.TempDir()
	got, err := nearestExisting(filepath.Join(dir, "x", "y"))
	require.NoError(t, err)
	require.Equal(t, dir, got)

	got, err = nearestExisting(dir)
	require.NoError(t, err)
	require.Equal(t, dir, got)
}

func TestVolumeOfReFS(t *testing.T) {
	dir := os.Getenv("COWFLOW_TEST_REFS_DIR")
	if dir == "" {
		t.Skip("set COWFLOW_TEST_REFS_DIR to a directory on a ReFS volume")
	}
	v := volumeOrSkip(t, dir)
	require.Equal(t, "refs", v.Filesystem)
	require.True(t, v.BlockRefcounting)
}

func TestVolumeOfNTFS(t *testing.T) {
	dir := os.Getenv("COWFLOW_TEST_NTFS_DIR")
	if dir == "" {
		t.Skip("set COWFLOW_TEST_NTFS_DIR to a directory on an NTFS volume")
	}
	v := volumeOrSkip(t, dir)
	require.Equal(t, "ntfs", v.Filesystem)
	require.False(t, v.BlockRefcounting)
}

func TestVolumeOfBtrfs(t *testing.T) {
	dir := os.Getenv("COWFLOW_TEST_COW_DIR")
	if dir == "" {
		t.Skip("set COWFLOW_TEST_COW_DIR to a directory on a block-cloning volume")
	}
	v := volumeOrSkip(t, dir)
	switch runtime.GOOS {
	case "linux":
		require.Contains(t, []string{"btrfs", "xfs"}, v.Filesystem)
	case "darwin":
		require.Equal(t, "apfs", v.Filesystem)
	default:
		require.Equal(t, "refs", v.Filesystem)
	}
	require.True(t, v.BlockRefcounting)
}
