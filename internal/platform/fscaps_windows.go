//go:build windows

package platform

import (
	"context"
	"fmt"
	"strings"
	"unsafe"

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
	vi := VolumeInfo{
		Path:       windows.UTF16ToString(root),
		Filesystem: windows.UTF16ToString(fsName), // "ReFS", "NTFS", ...
		BlockClone: flags&fileSupportsBlockRefcounting != 0,
	}
	// A Dev Drive is always ReFS; a ReFS volume is not necessarily a Dev
	// Drive, so the two facts are reported separately.
	if vi.Filesystem == "ReFS" {
		dev, err := isDevDrive(vi.Path)
		if err != nil {
			return VolumeInfo{}, fmt.Errorf("dev drive state of %q: %w", vi.Path, err)
		}
		vi.DevDrive = dev
	}
	return vi, nil
}

// volumeOf fills a [Volume] from GetVolumePathName and GetVolumeInformation.
// FILE_SUPPORTS_BLOCK_REFCOUNTING is only a hint that the volume can clone.
// GetVolumePathName on a mount-point folder returns that folder; that is
// intended.
func volumeOf(_ context.Context, path string) (Volume, error) {
	p16, err := windows.UTF16PtrFromString(LongPath(path))
	if err != nil {
		return Volume{}, err
	}
	root := make([]uint16, bufLen)
	if err := windows.GetVolumePathName(p16, &root[0], bufLen); err != nil {
		return Volume{}, fmt.Errorf("volume path of %q: %w", path, err)
	}
	var serial, flags uint32
	fsName := make([]uint16, bufLen)
	if err := windows.GetVolumeInformation(&root[0], nil, 0, &serial, nil, &flags,
		&fsName[0], bufLen); err != nil {
		return Volume{}, fmt.Errorf("volume information of %q: %w", path, err)
	}
	v := Volume{
		Filesystem:       strings.ToLower(windows.UTF16ToString(fsName)),
		ID:               fmt.Sprintf("%08x", serial),
		MountPath:        windows.UTF16ToString(root),
		BlockRefcounting: flags&fileSupportsBlockRefcounting != 0,
		ClusterSize:      clusterSize(&root[0]),
	}
	if v.Filesystem == "refs" {
		dev, err := isDevDrive(v.MountPath)
		if err != nil {
			return Volume{}, fmt.Errorf("dev drive state of %q: %w", v.MountPath, err)
		}
		v.DevDrive = dev
	}
	return v, nil
}

var procGetDiskFreeSpaceW = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetDiskFreeSpaceW")

// clusterSize returns sectors-per-cluster times bytes-per-sector of the
// volume rooted at root, or 0 when it cannot be determined.
// x/sys/windows does not wrap GetDiskFreeSpaceW.
func clusterSize(root *uint16) uint32 {
	if procGetDiskFreeSpaceW.Find() != nil {
		return 0
	}
	var sectors, bytesPerSector, freeClusters, totalClusters uint32
	r, _, _ := procGetDiskFreeSpaceW.Call(
		uintptr(unsafe.Pointer(root)),            //nolint:gosec // G103: out parameters of a syscall
		uintptr(unsafe.Pointer(&sectors)),        //nolint:gosec // G103: out parameters of a syscall
		uintptr(unsafe.Pointer(&bytesPerSector)), //nolint:gosec // G103: out parameters of a syscall
		uintptr(unsafe.Pointer(&freeClusters)),   //nolint:gosec // G103: out parameters of a syscall
		uintptr(unsafe.Pointer(&totalClusters)),  //nolint:gosec // G103: out parameters of a syscall
	)
	if r == 0 {
		return 0
	}
	return sectors * bytesPerSector
}
