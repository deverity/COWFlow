package workspace

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/platform"
)

// Shared conformance suite for the per-OS cloneFile primitive (sprint-1.md
// §6.2). It runs on every OS against the directory named by
// COWFLOW_TEST_COW_DIR or COWFLOW_TEST_REFS_DIR, or t.TempDir(), and skips
// with the reason when the volume cannot block clone.

// cowTestDir returns a fresh directory on the volume under test.
func cowTestDir(t testing.TB) string {
	t.Helper()
	for _, env := range []string{"COWFLOW_TEST_COW_DIR", "COWFLOW_TEST_REFS_DIR"} {
		if root := os.Getenv(env); root != "" {
			dir, err := os.MkdirTemp(root, "cowflow-clone-")
			require.NoError(t, err, "%s=%s", env, root)
			t.Cleanup(func() { _ = os.RemoveAll(dir) }) //nolint:gosec // G703: test temp dir; best effort, the root is the caller's
			return dir
		}
	}
	return t.TempDir()
}

// clusterOf returns the cluster size of the volume holding dir, or 4096 when
// the platform does not report one.
func clusterOf(t testing.TB, dir string) int {
	t.Helper()
	v, err := platform.VolumeOf(context.Background(), dir)
	require.NoError(t, err)
	if v.ClusterSize == 0 {
		return 4096
	}
	return int(v.ClusterSize)
}

// cloneOrSkip clones src to dst and skips the test when the volume cannot
// block clone. Even then no destination file may remain (contract rule 5).
// A ReFS volume named by COWFLOW_TEST_REFS_DIR has the capability, so there
// an unsupported clone fails the test instead of hiding a broken clone
// behind a skip.
func cloneOrSkip(t testing.TB, src, dst string) {
	t.Helper()
	err := cloneFile(context.Background(), src, dst)
	if errors.Is(err, ErrCloneUnsupported) {
		assert.NoFileExists(t, dst, "an unsupported clone leaves no destination")
		if onTestReFS(t, dst) {
			require.NoError(t, err, "COWFLOW_TEST_REFS_DIR is on ReFS, which block clones")
		}
		t.Skipf("block clone unsupported in %s: %v", filepath.Dir(dst), err)
	}
	require.NoError(t, err)
}

// onTestReFS reports whether path lies under COWFLOW_TEST_REFS_DIR on a
// volume that reports ReFS.
func onTestReFS(t testing.TB, path string) bool {
	t.Helper()
	root := os.Getenv("COWFLOW_TEST_REFS_DIR")
	if root == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	v, err := platform.VolumeOf(context.Background(), root)
	require.NoError(t, err)
	return v.Filesystem == "refs"
}

func writeRandom(t testing.TB, path string, n int) []byte {
	t.Helper()
	data := make([]byte, n)
	_, err := rand.Read(data)
	require.NoError(t, err)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304/G703: test temp path
	require.NoError(t, err)
	_, err = f.Write(data)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return data
}

func TestCloneFileContentEqual(t *testing.T) {
	dir := cowTestDir(t)
	cluster := clusterOf(t, dir)
	sizes := []struct {
		name string
		n    int
	}{
		{"empty", 0},
		{"one byte", 1},
		{"cluster minus one", cluster - 1},
		{"cluster", cluster},
		{"cluster plus one", cluster + 1},
		{"10 MiB random", 10 << 20},
	}
	for _, sz := range sizes {
		t.Run(sz.name, func(t *testing.T) {
			src := filepath.Join(dir, sz.name+".src")
			dst := filepath.Join(dir, sz.name+".dst")
			want := writeRandom(t, src, sz.n)
			cloneOrSkip(t, src, dst)
			got, err := os.ReadFile(dst) //nolint:gosec // G304: test temp path
			require.NoError(t, err)
			assert.Len(t, got, sz.n)
			assert.True(t, bytes.Equal(want, got), "clone content differs from the source")
		})
	}
}

func TestCloneFileIsolation(t *testing.T) {
	dir := cowTestDir(t)
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	want := writeRandom(t, src, 3*clusterOf(t, dir)+5)
	cloneOrSkip(t, src, dst)

	f, err := os.OpenFile(dst, os.O_RDWR, 0) //nolint:gosec // G304: test temp path
	require.NoError(t, err)
	_, err = f.WriteAt([]byte("changed in the clone"), 1)
	require.NoError(t, err)
	_, err = f.Seek(0, io.SeekEnd)
	require.NoError(t, err)
	_, err = f.WriteString("appended")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	got, err := os.ReadFile(src) //nolint:gosec // G304: test temp path
	require.NoError(t, err)
	assert.True(t, bytes.Equal(want, got), "a write to the clone changed the source")
}

func TestCloneFileRefusesExisting(t *testing.T) {
	dir := cowTestDir(t)
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	writeRandom(t, src, 100)
	require.NoError(t, os.WriteFile(dst, []byte("keep"), 0o600))

	err := cloneFile(context.Background(), src, dst)
	if errors.Is(err, ErrCloneUnsupported) {
		t.Skipf("block clone unsupported in %s: %v", dir, err)
	}
	require.ErrorIs(t, err, fs.ErrExist)
	got, rerr := os.ReadFile(dst) //nolint:gosec // G304: test temp path
	require.NoError(t, rerr)
	assert.Equal(t, "keep", string(got), "an existing destination is never overwritten")
}

func TestCloneFilePreservesMtime(t *testing.T) {
	dir := cowTestDir(t)
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	writeRandom(t, src, 2*clusterOf(t, dir)+1)
	mtime := time.Date(2021, 3, 4, 5, 6, 7, 0, time.UTC)
	require.NoError(t, os.Chtimes(src, mtime, mtime))
	cloneOrSkip(t, src, dst)

	fi, err := os.Stat(dst)
	require.NoError(t, err)
	assert.True(t, fi.ModTime().Equal(mtime), "mtime %v, want %v", fi.ModTime(), mtime)
}

func TestCloneFilePreservesMode(t *testing.T) {
	dir := cowTestDir(t)
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	writeRandom(t, src, 10)
	require.NoError(t, os.Chmod(src, 0o444)) //nolint:gosec // G302: read-only on purpose
	t.Cleanup(func() {                       // let the directory cleanup remove read-only files
		_ = os.Chmod(src, 0o600)
		_ = os.Chmod(dst, 0o600)
	})
	cloneOrSkip(t, src, dst)

	si, err := os.Stat(src)
	require.NoError(t, err)
	di, err := os.Stat(dst)
	require.NoError(t, err)
	assert.Equal(t, si.Mode().Perm(), di.Mode().Perm())
}

func TestCloneFileRefusesNonRegular(t *testing.T) {
	dir := cowTestDir(t)
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.Mkdir(sub, 0o700))
	dst := filepath.Join(dir, "dst")

	err := cloneFile(context.Background(), sub, dst)
	require.Error(t, err)
	assert.NoFileExists(t, dst)
}

func TestCloneFileCanceled(t *testing.T) {
	dir := cowTestDir(t)
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	writeRandom(t, src, 10)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := cloneFile(ctx, src, dst)
	if errors.Is(err, ErrCloneUnsupported) {
		t.Skipf("block clone unsupported in %s: %v", dir, err)
	}
	require.ErrorIs(t, err, context.Canceled)
	assert.NoFileExists(t, dst)
}
