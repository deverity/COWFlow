//go:build darwin

package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// cloneFile clones src into the new file dst with clonefile(2) (APFS): dst
// shares src's data until either is written. It follows the frozen contract
// of [Cloner] and never copies bytes; a filesystem or volume without clone
// support yields an error wrapping ErrCloneUnsupported.
//
// clonefile also clones whole directories. src is checked to be a regular
// file first, so the per-file contract stays identical across OSes. clonefile
// creates dst itself and creates nothing on failure, so an existing dst is
// never removed.
func cloneFile(ctx context.Context, src, dst string) error {
	if err := ctx.Err(); err != nil {
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
	if err := unix.Clonefile(src, dst, unix.CLONE_NOFOLLOW); err != nil {
		return mapCloneErr(src, err)
	}
	// clonefile keeps the mode and times; setting the times again guarantees
	// the mtime the index stat check depends on.
	if err := os.Chtimes(dst, fi.ModTime(), fi.ModTime()); err != nil {
		err = fmt.Errorf("clone %s: %w", src, err)
		if rerr := os.Remove(dst); rerr != nil { //nolint:gosec // G703: the caller confines dst to the workspace
			return errors.Join(err, rerr) // contract rule 5 could not be kept: say so
		}
		return err
	}
	return nil
}

// mapCloneErr wraps an error of clonefile(2). ENOTSUP (a filesystem without
// clone support) and EXDEV (another volume) wrap ErrCloneUnsupported as well;
// EEXIST matches fs.ErrExist through the errno; every other error passes
// through with context.
func mapCloneErr(src string, err error) error {
	switch {
	case errors.Is(err, unix.ENOTSUP), errors.Is(err, unix.EXDEV):
		return fmt.Errorf("clone %s: %w: %w", src, ErrCloneUnsupported, err)
	default:
		return fmt.Errorf("clone %s: %w", src, err)
	}
}
