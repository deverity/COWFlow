//go:build darwin

package platform

import (
	"context"
	"fmt"

	"golang.org/x/sys/unix"
)

// Volume reports the filesystem of the volume holding path. APFS supports
// clonefile(2), so BlockClone is true only for it.
func (h *host) Volume(ctx context.Context, path string) (VolumeInfo, error) {
	if err := ctx.Err(); err != nil {
		return VolumeInfo{}, err
	}
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return VolumeInfo{}, fmt.Errorf("statfs %q: %w", path, err)
	}
	fs := unix.ByteSliceToString(st.Fstypename[:])
	return VolumeInfo{
		Path:       unix.ByteSliceToString(st.Mntonname[:]),
		Filesystem: fs,
		BlockClone: fs == "apfs",
	}, nil
}
