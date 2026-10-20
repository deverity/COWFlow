//go:build windows

package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"

	"github.com/deverity/cowflow/internal/platform"
)

func TestDuplicateExtentsDataLayout(t *testing.T) {
	var d duplicateExtentsData
	assert.Equal(t, uintptr(32), unsafe.Sizeof(d))
	assert.Equal(t, uintptr(0), unsafe.Offsetof(d.FileHandle))
	assert.Equal(t, uintptr(8), unsafe.Offsetof(d.SourceFileOffset))
	assert.Equal(t, uintptr(16), unsafe.Offsetof(d.TargetFileOffset))
	assert.Equal(t, uintptr(24), unsafe.Offsetof(d.ByteCount))
}

func TestIntegrityBufferLayout(t *testing.T) {
	var g getIntegrityBuffer
	assert.Equal(t, uintptr(16), unsafe.Sizeof(g))
	assert.Equal(t, uintptr(4), unsafe.Offsetof(g.Flags))
	assert.Equal(t, uintptr(12), unsafe.Offsetof(g.ClusterSizeInBytes))
	var s setIntegrityBuffer
	assert.Equal(t, uintptr(8), unsafe.Sizeof(s))
	assert.Equal(t, uintptr(4), unsafe.Offsetof(s.Flags))
}

func TestMapCloneErr(t *testing.T) {
	tests := []struct {
		err         error
		unsupported bool
	}{
		{windows.ERROR_NOT_SUPPORTED, true},
		{windows.ERROR_INVALID_FUNCTION, true},
		{windows.ERROR_NOT_SAME_DEVICE, true},
		{windows.ERROR_INVALID_PARAMETER, true},
		{windows.ERROR_DISK_FULL, false},
		{windows.ERROR_ACCESS_DENIED, false},
		{context.Canceled, false},
	}
	for _, tt := range tests {
		t.Run(tt.err.Error(), func(t *testing.T) {
			err := mapCloneErr(`X:\src`, "duplicate extents", tt.err)
			assert.Equal(t, tt.unsupported, errors.Is(err, ErrCloneUnsupported))
			require.ErrorIs(t, err, tt.err, "the OS error stays inspectable")
			assert.Contains(t, err.Error(), `clone X:\src: duplicate extents`)
		})
	}
}

// setSparse marks path as a sparse file.
func setSparse(t testing.TB, path string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR, 0) //nolint:gosec // G304: test temp path
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	require.NoError(t, ioctl(windows.Handle(f.Fd()), windows.FSCTL_SET_SPARSE, nil, 0, nil, 0))
}

func attributes(t testing.TB, path string) uint32 {
	t.Helper()
	p16, err := windows.UTF16PtrFromString(platform.LongPath(path))
	require.NoError(t, err)
	a, err := windows.GetFileAttributes(p16)
	require.NoError(t, err)
	return a
}

func TestCloneFileSparse(t *testing.T) {
	dir := cowTestDir(t)
	src := filepath.Join(dir, "sparse.src")
	dst := filepath.Join(dir, "sparse.dst")
	require.NoError(t, os.WriteFile(src, nil, 0o600))
	setSparse(t, src)
	f, err := os.OpenFile(src, os.O_RDWR, 0) //nolint:gosec // G304: test temp path
	require.NoError(t, err)
	_, err = f.WriteAt([]byte("head"), 0)
	require.NoError(t, err)
	_, err = f.WriteAt([]byte("tail"), 8<<20-4) // a hole of almost 8 MiB in between
	require.NoError(t, err)
	require.NoError(t, f.Close())

	cloneOrSkip(t, src, dst)
	assert.NotZero(t, attributes(t, dst)&windows.FILE_ATTRIBUTE_SPARSE_FILE, "the clone is sparse")
	want, err := os.ReadFile(src) //nolint:gosec // G304: test temp path
	require.NoError(t, err)
	got, err := os.ReadFile(dst) //nolint:gosec // G304: test temp path
	require.NoError(t, err)
	assert.True(t, bytes.Equal(want, got), "clone content differs from the source")
}

func TestCloneFileNTFSUnsupported(t *testing.T) {
	root := os.Getenv("COWFLOW_TEST_NTFS_DIR")
	if root == "" {
		t.Skip("COWFLOW_TEST_NTFS_DIR not set")
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

func TestCloneFileCrossVolume(t *testing.T) {
	if os.Getenv("COWFLOW_TEST_COW_DIR") == "" && os.Getenv("COWFLOW_TEST_REFS_DIR") == "" {
		t.Skip("neither COWFLOW_TEST_COW_DIR nor COWFLOW_TEST_REFS_DIR is set")
	}
	srcDir := cowTestDir(t)
	dstDir := t.TempDir()
	if root := os.Getenv("COWFLOW_TEST_NTFS_DIR"); root != "" {
		d, err := os.MkdirTemp(root, "cowflow-clone-")
		require.NoError(t, err)
		t.Cleanup(func() { _ = os.RemoveAll(d) }) //nolint:gosec // G703: test temp dir; best effort, the root is the caller's
		dstDir = d
	}
	same, err := platform.SameVolume(context.Background(), srcDir, dstDir)
	require.NoError(t, err)
	if same {
		t.Skipf("%s and %s are on one volume", srcDir, dstDir)
	}
	src := filepath.Join(srcDir, "src")
	dst := filepath.Join(dstDir, "dst")
	writeRandom(t, src, 4096)

	err = cloneFile(context.Background(), src, dst)
	require.ErrorIs(t, err, ErrCloneUnsupported)
	assert.NoFileExists(t, dst)
}

func TestCloneFileLongPath(t *testing.T) {
	dir := cowTestDir(t)
	deep := dir
	for i := 0; len(deep) <= 260; i++ {
		deep = filepath.Join(deep, fmt.Sprintf("%02d-%s", i, strings.Repeat("d", 40)))
	}
	require.NoError(t, os.MkdirAll(deep, 0o700))
	src := filepath.Join(deep, "src")
	dst := filepath.Join(deep, "dst")
	require.Greater(t, len(dst), 240)
	want := writeRandom(t, src, 3*clusterOf(t, dir)+1)

	cloneOrSkip(t, src, dst)
	got, err := os.ReadFile(dst) //nolint:gosec // G304: test temp path
	require.NoError(t, err)
	assert.True(t, bytes.Equal(want, got), "clone content differs from the source")
}

// TestCloneFileFreeSpace checks the acceptance criterion: cloning a 1 GiB
// file on ReFS lowers free space by less than 1 MiB.
func TestCloneFileFreeSpace(t *testing.T) {
	if os.Getenv("COWFLOW_TEST_REFS_DIR") == "" {
		t.Skip("COWFLOW_TEST_REFS_DIR not set")
	}
	if testing.Short() {
		t.Skip("writes 1 GiB")
	}
	dir := cowTestDir(t)
	src := filepath.Join(dir, "1gib.src")
	dst := filepath.Join(dir, "1gib.dst")
	writePattern(t, src, 1<<30)

	before, err := platform.FreeBytes(dir)
	require.NoError(t, err)
	cloneOrSkip(t, src, dst)
	after, err := platform.FreeBytes(dir)
	require.NoError(t, err)
	used := int64(before) - int64(after) //nolint:gosec // G115: free bytes of one volume fit in int64
	assert.Less(t, used, int64(1<<20), "a 1 GiB clone used %d bytes", used)
}

// writePattern writes n bytes of a repeating 1 MiB block and syncs.
func writePattern(t testing.TB, path string, n int64) {
	t.Helper()
	block := make([]byte, 1<<20)
	for i := range block {
		block[i] = byte(i*31 + 7)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: test temp path
	require.NoError(t, err)
	for off := int64(0); off < n; off += int64(len(block)) {
		_, err = f.Write(block[:min(int64(len(block)), n-off)])
		require.NoError(t, err)
	}
	require.NoError(t, f.Sync())
	require.NoError(t, f.Close())
}

func TestCloneFileOver4GiB(t *testing.T) {
	if os.Getenv("COWFLOW_TEST_LARGE") != "1" {
		t.Skip("COWFLOW_TEST_LARGE=1 not set")
	}
	dir := cowTestDir(t)
	cluster := int64(clusterOf(t, dir))
	size := int64(4<<30) + 3*cluster + 17 // two chunks and an unaligned tail
	src := filepath.Join(dir, "large.src")
	dst := filepath.Join(dir, "large.dst")
	f, err := os.OpenFile(src, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: test temp path
	require.NoError(t, err)
	require.NoError(t, f.Truncate(size))
	marks := map[int64]string{0: "first", 4<<30 - 2: "boundary", size - 4: "last"}
	for off, s := range marks {
		_, err = f.WriteAt([]byte(s), off)
		require.NoError(t, err)
	}
	require.NoError(t, f.Close())

	cloneOrSkip(t, src, dst)
	fi, err := os.Stat(dst)
	require.NoError(t, err)
	assert.Equal(t, size, fi.Size())
	g, err := os.Open(dst) //nolint:gosec // G304: test temp path
	require.NoError(t, err)
	defer func() { _ = g.Close() }() // read-only handle
	for off, s := range marks {
		buf := make([]byte, len(s))
		_, err = g.ReadAt(buf, off)
		require.NoError(t, err)
		assert.Equal(t, s, string(buf), "offset %d", off)
	}
}

func BenchmarkCloneFileSmall(b *testing.B) {
	dir := cowTestDir(b)
	src := filepath.Join(dir, "small.src")
	writeRandom(b, src, 4096)
	cloneOrSkip(b, src, filepath.Join(dir, "probe.dst"))
	ctx := context.Background()
	i := 0
	for b.Loop() {
		if err := cloneFile(ctx, src, filepath.Join(dir, fmt.Sprintf("%d.dst", i))); err != nil {
			b.Fatal(err)
		}
		i++
	}
}
