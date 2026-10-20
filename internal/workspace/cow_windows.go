//go:build windows

package workspace

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// duplicateExtentsData mirrors DUPLICATE_EXTENTS_DATA, the input of
// FSCTL_DUPLICATE_EXTENTS_TO_FILE. x/sys/windows defines the control code
// but not this struct. Layout: 32 bytes, offsets 0/8/16/24.
type duplicateExtentsData struct {
	FileHandle       windows.Handle
	SourceFileOffset int64
	TargetFileOffset int64
	ByteCount        int64
}

// getIntegrityBuffer mirrors FSCTL_GET_INTEGRITY_INFORMATION_BUFFER
// (16 bytes).
type getIntegrityBuffer struct {
	ChecksumAlgorithm        uint16
	Reserved                 uint16
	Flags                    uint32
	ChecksumChunkSizeInBytes uint32
	ClusterSizeInBytes       uint32
}

// setIntegrityBuffer mirrors FSCTL_SET_INTEGRITY_INFORMATION_BUFFER
// (8 bytes).
type setIntegrityBuffer struct {
	ChecksumAlgorithm uint16
	Reserved          uint16
	Flags             uint32
}

// cloneFile clones src into the new file dst with
// FSCTL_DUPLICATE_EXTENTS_TO_FILE (ReFS / Dev Drive block cloning): dst
// shares src's clusters until either is written. It follows the frozen
// contract of [Cloner] and never copies bytes; a volume or file the IOCTL
// refuses yields an error wrapping ErrCloneUnsupported.
//
// The files are opened through package os, which adds the \\?\ prefix for
// long paths; every later call works on the handles.
func cloneFile(ctx context.Context, src, dst string) (err error) {
	if err = ctx.Err(); err != nil {
		return err
	}
	src, dst = filepath.Clean(src), filepath.Clean(dst)
	fi, err := os.Lstat(src) //nolint:gosec // G703: the caller confines src to the golden tree
	if err != nil {
		return fmt.Errorf("clone %s: %w", src, err)
	}
	if !fi.Mode().IsRegular() { // never follow a symlink or junction
		return fmt.Errorf("clone %s: not a regular file", src)
	}
	s, err := os.Open(src) //nolint:gosec // G703: the caller confines src to the golden tree
	if err != nil {
		return fmt.Errorf("clone %s: %w", src, err)
	}
	defer func() { _ = s.Close() }() // read-only handle: a close failure loses nothing

	d, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o666) //nolint:gosec // G304: the caller confines dst to the workspace
	if err != nil {
		return fmt.Errorf("clone %s: %w", src, err) // dst existed or could not be created: nothing of ours to remove
	}
	// Assign, never shadow, err below: the deferred cleanup reads it.
	defer func() {
		if err == nil {
			return
		}
		if rerr := os.Remove(dst); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) { //nolint:gosec // G703: the caller confines dst to the workspace
			err = errors.Join(err, rerr) // contract rule 5 could not be kept: say so
		}
	}()
	err = duplicateFile(ctx, src, s, d)
	if cerr := d.Close(); err == nil && cerr != nil {
		err = fmt.Errorf("clone %s: %w", src, cerr)
	}
	if err != nil {
		return err
	}
	// Windows maps the permission bits to the read-only attribute. It is set
	// last, once the handle is closed, so it never blocks the clone itself.
	if perm := fi.Mode().Perm(); perm&0o200 == 0 {
		if err = os.Chmod(dst, perm); err != nil { //nolint:gosec // G703: the caller confines dst to the workspace
			return fmt.Errorf("clone %s: %w", src, err)
		}
	}
	return nil
}

// duplicateFile makes the empty file d a block clone of s: it matches the
// sparse and integrity-stream settings (the IOCTL fails with
// ERROR_INVALID_PARAMETER when they differ), sets the size, duplicates the
// extents in cluster-aligned chunks below 4 GiB, and copies the timestamps.
// A zero-byte file only gets its timestamps.
func duplicateFile(ctx context.Context, src string, s, d *os.File) error {
	sh, dh := windows.Handle(s.Fd()), windows.Handle(d.Fd()) // os.File owns both handles
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(sh, &info); err != nil {
		return fmt.Errorf("clone %s: file information: %w", src, err)
	}
	if info.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0 {
		return fmt.Errorf("clone %s: not a regular file", src)
	}
	size := int64(info.FileSizeHigh)<<32 | int64(info.FileSizeLow)
	if size > 0 {
		if err := cloneData(ctx, src, sh, dh, &info, size, d); err != nil {
			return err
		}
	}
	if err := windows.SetFileTime(dh, &info.CreationTime, &info.LastAccessTime, &info.LastWriteTime); err != nil {
		return fmt.Errorf("clone %s: set file time: %w", src, err)
	}
	return nil
}

// cloneData matches the sparse and integrity settings, sets the size and
// duplicates the extents of a non-empty file, checking ctx between chunks.
func cloneData(ctx context.Context, src string, sh, dh windows.Handle,
	info *windows.ByHandleFileInformation, size int64, d *os.File) error {
	if info.FileAttributes&windows.FILE_ATTRIBUTE_SPARSE_FILE != 0 {
		if err := ioctl(dh, windows.FSCTL_SET_SPARSE, nil, 0, nil, 0); err != nil {
			return mapCloneErr(src, "set sparse", err)
		}
	}
	cluster, err := copyIntegrity(sh, dh)
	if err != nil {
		return mapCloneErr(src, "integrity information", err)
	}
	if cluster == 0 {
		return fmt.Errorf("clone %s: %w: volume reports no cluster size", src, ErrCloneUnsupported)
	}
	extents, err := cloneExtents(size, int64(cluster))
	if err != nil {
		return fmt.Errorf("clone %s: %w", src, err)
	}
	if err := d.Truncate(size); err != nil {
		return fmt.Errorf("clone %s: %w", src, err)
	}
	for _, e := range extents {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("clone %s: %w", src, err)
		}
		in := duplicateExtentsData{
			FileHandle:       sh,
			SourceFileOffset: e.Offset,
			TargetFileOffset: e.Offset,
			ByteCount:        e.Length,
		}
		if err := ioctl(dh, windows.FSCTL_DUPLICATE_EXTENTS_TO_FILE,
			unsafe.Pointer(&in), uint32(unsafe.Sizeof(in)), nil, 0); err != nil { //nolint:gosec // G103: fixed-size ioctl input buffer
			return mapCloneErr(src, "duplicate extents", err)
		}
	}
	return nil
}

// copyIntegrity gives dst the integrity-stream settings of src and returns
// the volume cluster size, which FSCTL_GET_INTEGRITY_INFORMATION reports.
// dst must still be empty: integrity settings only change on empty files.
func copyIntegrity(sh, dh windows.Handle) (uint32, error) {
	var get getIntegrityBuffer
	if err := ioctl(sh, windows.FSCTL_GET_INTEGRITY_INFORMATION, nil, 0,
		unsafe.Pointer(&get), uint32(unsafe.Sizeof(get))); err != nil { //nolint:gosec // G103: fixed-size ioctl output buffer
		return 0, err
	}
	set := setIntegrityBuffer{ChecksumAlgorithm: get.ChecksumAlgorithm, Flags: get.Flags}
	if err := ioctl(dh, windows.FSCTL_SET_INTEGRITY_INFORMATION,
		unsafe.Pointer(&set), uint32(unsafe.Sizeof(set)), nil, 0); err != nil { //nolint:gosec // G103: fixed-size ioctl input buffer
		return 0, err
	}
	return get.ClusterSizeInBytes, nil
}

// ioctl issues a synchronous DeviceIoControl with fixed-size buffers.
func ioctl(h windows.Handle, code uint32, in unsafe.Pointer, inLen uint32, out unsafe.Pointer, outLen uint32) error {
	var returned uint32
	return windows.DeviceIoControl(h, code, (*byte)(in), inLen, (*byte)(out), outLen, &returned, nil)
}

// mapCloneErr wraps an error of a block-clone IOCTL. The codes a volume,
// filesystem or file layout without block cloning returns (NTFS, another
// volume, mismatched attributes) wrap ErrCloneUnsupported as well; every
// other error passes through with context.
func mapCloneErr(src, op string, err error) error {
	switch {
	case errors.Is(err, windows.ERROR_NOT_SUPPORTED),
		errors.Is(err, windows.ERROR_INVALID_FUNCTION),
		errors.Is(err, windows.ERROR_NOT_SAME_DEVICE),
		errors.Is(err, windows.ERROR_INVALID_PARAMETER):
		return fmt.Errorf("clone %s: %s: %w: %w", src, op, ErrCloneUnsupported, err)
	default:
		return fmt.Errorf("clone %s: %s: %w", src, op, err)
	}
}
