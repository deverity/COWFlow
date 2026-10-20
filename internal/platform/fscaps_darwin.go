//go:build darwin

package platform

import (
	"context"
	"fmt"
	"strings"

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

// volumeOf fills a [Volume] from statfs(2). The ID is the filesystem ID.
func volumeOf(_ context.Context, path string) (Volume, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return Volume{}, fmt.Errorf("statfs %q: %w", path, err)
	}
	name := strings.ToLower(unix.ByteSliceToString(st.Fstypename[:]))
	return Volume{
		Filesystem:       name,
		ID:               fmt.Sprintf("%08x%08x", uint32(st.Fsid.Val[0]), uint32(st.Fsid.Val[1])), //nolint:gosec // G115: bit reinterpretation
		MountPath:        unix.ByteSliceToString(st.Mntonname[:]),
		BlockRefcounting: name == "apfs",
	}, nil
}
