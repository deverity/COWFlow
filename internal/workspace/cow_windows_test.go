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
	"time"
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
	var sp fileSetSparseBuffer
	assert.Equal(t, uintptr(1), unsafe.Sizeof(sp))
	var fsi fileStandardInfo
	assert.Equal(t, uintptr(24), unsafe.Sizeof(fsi))
	assert.Equal(t, uintptr(8), unsafe.Offsetof(fsi.EndOfFile))
	assert.Equal(t, uintptr(16), unsafe.Offsetof(fsi.NumberOfLinks))
	assert.Equal(t, uintptr(21), unsafe.Offsetof(fsi.Directory))
}

// A clone of a non-sparse file is sparse: the attribute keeps the size
// change from allocating and is kept, since clearing it can allocate. The
// source stays as it was and the content matches.
func TestCloneFileNonSparseSource(t *testing.T) {
	dir := cowTestDir(t)
	src := filepath.Join(dir, "plain.src")
	dst := filepath.Join(dir, "plain.dst")
	want := writeRandom(t, src, 2*clusterOf(t, dir)+5)
	require.Zero(t, attributes(t, src)&windows.FILE_ATTRIBUTE_SPARSE_FILE)

	cloneOrSkip(t, src, dst)
	assert.Zero(t, attributes(t, src)&windows.FILE_ATTRIBUTE_SPARSE_FILE, "the source stays non-sparse")
	assert.NotZero(t, attributes(t, dst)&windows.FILE_ATTRIBUTE_SPARSE_FILE, "the clone is sparse")
	got, err := os.ReadFile(dst) //nolint:gosec // G304: test temp path
	require.NoError(t, err)
	assert.True(t, bytes.Equal(want, got), "clone content differs from the source")
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
//
// ReFS publishes free space from allocator state it persists at checkpoints,
// so GetDiskFreeSpaceEx can lag a write by seconds even after the file is
// flushed. Sampling right after writing the source therefore charged the
// source's own 1 GiB to the clone. Every sample is now taken on a settled
// volume (volume flushed, readings stable), and a control step requires the
// 1 GiB source write itself to show up first: a measurement that cannot see
// 1 GiB of data cannot prove that a clone costs none, so the test fails.
//
// The test logs the free space after every step of the clone (through
// cloneTrace), after the handles close, and while it waits for the volume
// to settle, together with the cluster size, the number of block-clone
// calls and the allocation sizes, so one failing run shows which step
// charged the space. A charge that ReFS hands back in the background is
// logged; the bound applies to the reading once the volume has settled,
// which a byte copy never reaches.
func TestCloneFileFreeSpace(t *testing.T) {
	if os.Getenv("COWFLOW_TEST_REFS_DIR") == "" {
		t.Skip("COWFLOW_TEST_REFS_DIR not set")
	}
	if testing.Short() {
		t.Skip("writes 1 GiB")
	}
	const size = int64(1 << 30)
	const bound = int64(1 << 20)
	dir := cowTestDir(t)
	v, err := platform.VolumeOf(context.Background(), dir)
	require.NoError(t, err)
	if v.Filesystem != "refs" {
		t.Skipf("%s is on %s, not ReFS", dir, v.Filesystem)
	}
	t.Logf("volume %s: filesystem %s, cluster %d bytes", v.MountPath, v.Filesystem, v.ClusterSize)
	src := filepath.Join(dir, "1gib.src")
	dst := filepath.Join(dir, "1gib.dst")

	base := settledFreeBytes(t, dir, 0)
	logFree(t, dir, "baseline")
	if base < 2*uint64(size) {
		t.Skipf("%s has %d bytes free; the test needs 2 GiB", v.MountPath, base)
	}
	start := time.Now()
	writePattern(t, src, size)
	afterSrc := waitFreeAtMost(t, dir, base-uint64(size)*15/16, time.Minute)
	lag := time.Since(start)
	srcUsed := freeDelta(base, afterSrc)
	t.Logf("source written: free %d, used %d, visible after %s; %s",
		afterSrc, srcUsed, lag, pathAllocation(t, src))
	require.GreaterOrEqual(t, srcUsed, size*15/16,
		"control: writing 1 GiB lowered free space on %s by only %d bytes within a minute, "+
			"so free space there cannot measure the clone", v.MountPath, srcUsed)
	ranges, err := cloneExtents(size, int64(clusterOf(t, dir)))
	require.NoError(t, err)
	t.Logf("the clone takes %d FSCTL_DUPLICATE_EXTENTS_TO_FILE call(s)", len(ranges))

	prev := afterSrc
	trace := func(phase string, h windows.Handle) {
		free := settledFreeBytes(t, dir, 0)
		t.Logf("after %s: free %d, used %d by this step, %d since the source; %s",
			phase, free, freeDelta(prev, free), freeDelta(afterSrc, free), handleAllocation(h))
		prev = free
	}
	cloneTrace.Store(&trace)
	cloneOrSkip(t, src, dst)
	cloneTrace.Store(nil)
	closed := settledFreeBytes(t, dir, 0)
	t.Logf("after close: free %d, used %d since the source; %s",
		closed, freeDelta(afterSrc, closed), pathAllocation(t, dst))
	logFree(t, dir, "after close")

	// Wait longer than the source write took to become visible, so a clone
	// that did allocate data cannot hide behind the same lag; then give a
	// charge ReFS releases in the background up to two minutes to go, and
	// log each reading.
	wait := 2*lag + time.Second
	after := settledFreeBytes(t, dir, wait)
	t.Logf("after settling %s: free %d, used %d since the source", wait, after, freeDelta(afterSrc, after))
	for deadline := time.Now().Add(2 * time.Minute); freeDelta(afterSrc, after) >= bound && time.Now().Before(deadline); {
		after = settledFreeBytes(t, dir, 5*time.Second)
		t.Logf("still settling: free %d, used %d since the source", after, freeDelta(afterSrc, after))
	}
	logFree(t, dir, "settled")
	used := freeDelta(afterSrc, after)
	assert.Less(t, used, bound,
		"a 1 GiB clone used %d bytes (the source write used %d, visible after %s)", used, srcUsed, lag)

	// Diagnostic only: what clearing the sparse attribute of the clone costs,
	// the step cloneFile no longer takes.
	f, err := os.OpenFile(dst, os.O_RDWR, 0) //nolint:gosec // G304: test temp path
	require.NoError(t, err)
	unset := fileSetSparseBuffer{SetSparse: 0}
	err = ioctl(windows.Handle(f.Fd()), windows.FSCTL_SET_SPARSE,
		unsafe.Pointer(&unset), uint32(unsafe.Sizeof(unset)), nil, 0) //nolint:gosec // G103: fixed-size ioctl input buffer
	require.NoError(t, f.Close())
	if err != nil {
		t.Logf("diagnostic: clearing the sparse attribute of the clone failed: %v", err)
		return
	}
	cleared := settledFreeBytes(t, dir, wait)
	t.Logf("diagnostic: clearing the sparse attribute of the clone used %d bytes; %s",
		freeDelta(after, cleared), pathAllocation(t, dst))
}

// fileSetSparseBuffer mirrors FILE_SET_SPARSE_BUFFER, the optional input of
// FSCTL_SET_SPARSE: one BOOLEAN; no input means "set". Layout: 1 byte.
type fileSetSparseBuffer struct {
	SetSparse byte
}

// fileStandardInfo mirrors FILE_STANDARD_INFO: two LARGE_INTEGERs, a DWORD
// and two BOOLEANs, padded to 24 bytes.
type fileStandardInfo struct {
	AllocationSize int64
	EndOfFile      int64
	NumberOfLinks  uint32
	DeletePending  byte
	Directory      byte
}

// handleAllocation describes the size and allocation size of the open file
// h, or why they could not be read.
func handleAllocation(h windows.Handle) string {
	var fsi fileStandardInfo
	if err := windows.GetFileInformationByHandleEx(h, windows.FileStandardInfo,
		(*byte)(unsafe.Pointer(&fsi)), uint32(unsafe.Sizeof(fsi))); err != nil { //nolint:gosec // G103: fixed-size output buffer
		return fmt.Sprintf("file standard info: %v", err)
	}
	return fmt.Sprintf("size %d, allocation size %d", fsi.EndOfFile, fsi.AllocationSize)
}

// x/sys/windows does not wrap GetCompressedFileSizeW.
var procGetCompressedFileSizeW = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetCompressedFileSizeW")

// pathAllocation describes the file at path like handleAllocation, adding
// its attributes and GetCompressedFileSize, which counts only the allocated
// ranges of a sparse file.
func pathAllocation(t testing.TB, path string) string {
	t.Helper()
	f, err := os.Open(path) //nolint:gosec // G304: test temp path
	require.NoError(t, err)
	defer func() { _ = f.Close() }() // read-only handle
	desc := fmt.Sprintf("%s: %s, attributes %#x",
		filepath.Base(path), handleAllocation(windows.Handle(f.Fd())), attributes(t, path))
	if procGetCompressedFileSizeW.Find() != nil {
		return desc
	}
	p16, err := windows.UTF16PtrFromString(platform.LongPath(path))
	require.NoError(t, err)
	var high uint32
	low, _, callErr := procGetCompressedFileSizeW.Call(
		uintptr(unsafe.Pointer(p16)),   //nolint:gosec // G103: in parameter of a syscall
		uintptr(unsafe.Pointer(&high)), //nolint:gosec // G103: out parameter of a syscall
	)
	const invalidFileSize = 0xFFFFFFFF // INVALID_FILE_SIZE, which x/sys/windows does not define
	lo := uint32(low)                  //nolint:gosec // G115: the call returns a DWORD
	if lo == invalidFileSize && !errors.Is(callErr, windows.ERROR_SUCCESS) {
		return fmt.Sprintf("%s, compressed size: %v", desc, callErr)
	}
	return fmt.Sprintf("%s, compressed size %d", desc, uint64(high)<<32|uint64(lo))
}

// logFree logs both free-space counters of the volume holding dir: the
// bytes available to the caller, which the test measures, and the total
// free bytes, which differ when quotas or reservations apply.
func logFree(t testing.TB, dir, phase string) {
	t.Helper()
	p16, err := windows.UTF16PtrFromString(platform.LongPath(dir))
	require.NoError(t, err)
	var avail, total, free uint64
	require.NoError(t, windows.GetDiskFreeSpaceEx(p16, &avail, &total, &free))
	t.Logf("%s: %d bytes available to the caller, %d free of %d", phase, avail, free, total)
}

// freeDelta returns how many bytes free space dropped from before to after;
// it is negative when free space grew.
func freeDelta(before, after uint64) int64 {
	return int64(before) - int64(after) //nolint:gosec // G115: free bytes of one volume fit in int64
}

// settledFreeBytes waits minWait, flushes the volume holding dir and returns
// its free bytes once three readings 200 ms apart agree, or the last reading
// after 30 s.
func settledFreeBytes(t testing.TB, dir string, minWait time.Duration) uint64 {
	t.Helper()
	time.Sleep(minWait)
	flushVolume(t, dir)
	deadline := time.Now().Add(30 * time.Second)
	last, err := platform.FreeBytes(dir)
	require.NoError(t, err)
	for same := 0; same < 2 && time.Now().Before(deadline); {
		time.Sleep(200 * time.Millisecond)
		cur, err := platform.FreeBytes(dir)
		require.NoError(t, err)
		if cur == last {
			same++
		} else {
			same, last = 0, cur
		}
	}
	return last
}

// waitFreeAtMost returns the settled free bytes of the volume holding dir
// once they are at most limit, or the last settled reading after timeout.
func waitFreeAtMost(t testing.TB, dir string, limit uint64, timeout time.Duration) uint64 {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		free := settledFreeBytes(t, dir, 0)
		if free <= limit || time.Now().After(deadline) {
			return free
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// flushVolume flushes the volume holding dir, which makes ReFS write a
// checkpoint and publish its allocator state. Opening a volume handle needs
// administrator rights; without them the readings settle on their own, and
// the test only logs that the flush was skipped.
func flushVolume(t testing.TB, dir string) {
	t.Helper()
	p16, err := windows.UTF16PtrFromString(platform.LongPath(dir))
	require.NoError(t, err)
	const n = uint32(windows.MAX_PATH + 1)
	root := make([]uint16, n)
	require.NoError(t, windows.GetVolumePathName(p16, &root[0], n))
	name := make([]uint16, n)
	require.NoError(t, windows.GetVolumeNameForVolumeMountPoint(&root[0], &name[0], n))
	// \\?\Volume{GUID}\ names the root directory; without the trailing
	// backslash it names the volume device.
	dev, err := windows.UTF16PtrFromString(strings.TrimSuffix(windows.UTF16ToString(name), `\`))
	require.NoError(t, err)
	h, err := windows.CreateFile(dev, windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Logf("volume flush skipped: %v", err)
		return
	}
	defer func() { _ = windows.CloseHandle(h) }() // the flush result is already reported
	if err := windows.FlushFileBuffers(h); err != nil {
		t.Logf("volume flush failed: %v", err)
	}
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
