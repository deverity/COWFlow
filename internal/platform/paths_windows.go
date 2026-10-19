package platform

import (
	"fmt"
	"io/fs"
	"strings"

	"golang.org/x/sys/windows"
)

// volumeNameDOS is VOLUME_NAME_DOS (0x0) of GetFinalPathNameByHandleW: return
// the path with a drive letter (or \\?\UNC\). x/sys/windows does not define it.
const volumeNameDOS = 0x0

// resolve canonicalizes an absolute, clean path through the file system:
// GetFinalPathNameByHandle resolves symbolic links, junctions and 8.3 short
// names of the deepest existing ancestor.
func resolve(p string) (string, error) {
	return resolveExisting(p, finalPath)
}

// finalPath opens p (following reparse points) and returns its final DOS
// path without the \\?\ prefix. A missing path is reported as a
// *fs.PathError wrapping the Win32 error, which matches fs.ErrNotExist.
func finalPath(p string) (string, error) {
	name, err := windows.UTF16PtrFromString(winLongPath(p))
	if err != nil {
		return "", &fs.PathError{Op: "open", Path: p, Err: err}
	}
	// Zero access rights are enough to query the name; BACKUP_SEMANTICS is
	// required to open directories. Full sharing never blocks other users.
	h, err := windows.CreateFile(name, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", &fs.PathError{Op: "open", Path: p, Err: err}
	}
	defer func() {
		// Read-only query handle: a close failure loses nothing.
		_ = windows.CloseHandle(h)
	}()

	size := uint32(windows.MAX_PATH)
	for {
		buf := make([]uint16, size)
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], size, volumeNameDOS)
		if err != nil {
			return "", &fs.PathError{Op: "GetFinalPathNameByHandle", Path: p, Err: err}
		}
		if n < size {
			return winStripLongPrefix(windows.UTF16ToString(buf[:n])), nil
		}
		// The buffer was too small: n is the required size including the
		// terminating NUL.
		if n >= windows.MAX_LONG_PATH {
			return "", fmt.Errorf("final path of %q: %d characters exceeds the limit", p, n)
		}
		size = max(n, size+1)
	}
}

// samePath compares canonical paths case-insensitively, like NTFS and ReFS.
func samePath(a, b string) bool { return strings.EqualFold(a, b) }

// hasPathPrefix is a case-insensitive prefix test on canonical paths.
func hasPathPrefix(p, prefix string) bool { return hasPrefixFold(p, prefix) }

// LongPath returns p with the \\?\ prefix (\\?\UNC\ for \\server\share
// paths) when p is absolute and longer than 240 characters, and p unchanged
// otherwise (SCOPE.md §7). Use it only for raw Win32 calls: the os package
// already handles long paths itself.
func LongPath(p string) string { return winLongPath(p) }
