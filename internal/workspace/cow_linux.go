//go:build linux

package workspace

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// cloneFile clones src into the new file dst with the FICLONE ioctl (btrfs,
// XFS with reflink=1): dst shares src's extents until either is written. It
// follows the frozen contract of [Cloner] and never copies bytes; a
// filesystem or mount layout without reflink yields an error wrapping
// ErrCloneUnsupported and leaves no destination file.
func cloneFile(ctx context.Context, src, dst string) (err error) {
	if err = ctx.Err(); err != nil {
		return err
	}
	src, dst = filepath.Clean(src), filepath.Clean(dst)
	fi, err := os.Lstat(src) //nolint:gosec // G703: the caller confines src to the golden tree
	if err != nil {
		return fmt.Errorf("clone %s: %w", src, err)
	}
	if !fi.Mode().IsRegular() { // never follow a symlink, never clone a directory
		return fmt.Errorf("clone %s: not a regular file", src)
	}
	s, err := os.OpenFile(src, os.O_RDONLY|unix.O_NOFOLLOW, 0) //nolint:gosec // G304/G703: the caller confines src to the golden tree
	if err != nil {
		return fmt.Errorf("clone %s: %w", src, err)
	}
	defer func() { _ = s.Close() }() // read-only handle: a close failure loses nothing

	d, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, 0o600) //nolint:gosec // G304: the caller confines dst to the workspace
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
	err = unix.IoctlFileClone(int(d.Fd()), int(s.Fd()))
	if err != nil {
		_ = d.Close()
		return mapCloneErr(src, err)
	}
	if err = d.Close(); err != nil {
		return fmt.Errorf("clone %s: %w", src, err)
	}
	// The create mode is exact only after this: set it last, so a read-only
	// source never blocks the clone itself and the umask never applies.
	if err = os.Chmod(dst, fi.Mode().Perm()); err != nil { //nolint:gosec // G703: the caller confines dst to the workspace
		return fmt.Errorf("clone %s: %w", src, err)
	}
	if err = os.Chtimes(dst, fi.ModTime(), fi.ModTime()); err != nil {
		return fmt.Errorf("clone %s: %w", src, err)
	}
	return nil
}

// mapCloneErr wraps an error of the FICLONE ioctl. The codes a filesystem
// without reflink, another mount (kernels before 5.18 refuse even within one
// filesystem) or a file the filesystem cannot share returns wrap
// ErrCloneUnsupported as well; every other error passes through with context.
func mapCloneErr(src string, err error) error {
	switch {
	case errors.Is(err, unix.EOPNOTSUPP),
		errors.Is(err, unix.EXDEV),
		errors.Is(err, unix.EINVAL),
		errors.Is(err, unix.ENOTTY):
		return fmt.Errorf("clone %s: %w: %w", src, ErrCloneUnsupported, err)
	default:
		return fmt.Errorf("clone %s: %w", src, err)
	}
}
