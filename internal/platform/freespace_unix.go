//go:build linux || darwin

package platform

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// freeBytes returns the blocks available to an unprivileged caller times the
// filesystem block size.
func freeBytes(path string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, fmt.Errorf("statfs %q: %w", path, err)
	}
	// Bsize is int64, int32 or uint32 depending on the architecture.
	return st.Bavail * uint64(st.Bsize), nil //nolint:gosec,nolintlint // G115 on arches where Bsize is signed
}
