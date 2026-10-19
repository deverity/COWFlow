//go:build windows

package platform

import (
	"context"
	"fmt"

	"golang.org/x/sys/windows"
)

// bufLen is the UTF-16 buffer length for volume names.
const bufLen = windows.MAX_PATH + 1

// fileSupportsBlockRefcounting is FILE_SUPPORTS_BLOCK_REFCOUNTING.
const fileSupportsBlockRefcounting = 0x08000000

// Volume reports the filesystem name and block-refcounting flag of the
// volume holding path. GetVolumePathName resolves mounted folders to their
// own volume root, which GetVolumeInformation requires.
func (h *host) Volume(ctx context.Context, path string) (VolumeInfo, error) {
	if err := ctx.Err(); err != nil {
		return VolumeInfo{}, err
	}
	// PENDING S0-T12: wrap path in LongPath once it lands.
	p16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return VolumeInfo{}, err
	}
	root := make([]uint16, bufLen)
	if err := windows.GetVolumePathName(p16, &root[0], bufLen); err != nil {
		return VolumeInfo{}, fmt.Errorf("volume path of %q: %w", path, err)
	}
	var flags uint32
	fsName := make([]uint16, bufLen)
	if err := windows.GetVolumeInformation(&root[0], nil, 0, nil, nil, &flags,
		&fsName[0], bufLen); err != nil {
		return VolumeInfo{}, fmt.Errorf("volume information: %w", err)
	}
	return VolumeInfo{
		Path:       windows.UTF16ToString(root),
		Filesystem: windows.UTF16ToString(fsName), // "ReFS", "NTFS", ...
		BlockClone: flags&fileSupportsBlockRefcounting != 0,
		DevDrive:   false, // PENDING S0-T19: Dev Drive detection
	}, nil
}
