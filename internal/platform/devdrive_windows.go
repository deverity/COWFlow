package platform

import (
	"context"
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// newDevDrive wires the §6.4 orchestration to the real Windows primitives.
func newDevDrive(h Host) DevDrive {
	return &devDrive{host: h, ops: windowsDevDriveOps(runPowerShell)}
}

// windowsDevDriveOps returns the Windows primitives, running scripts with
// run (runPowerShell outside tests).
func windowsDevDriveOps(run func(context.Context, string) (string, error)) devDriveOps {
	return devDriveOps{
		backend: func() diskBackend {
			if err := virtdiskAvailable(); err != nil {
				return psBackend{run: run, cause: err}
			}
			return virtdiskBackend{}
		},
		format: func(ctx context.Context, n int, mount string) error {
			s, err := renderFormatScript(n, mount)
			if err != nil {
				return err
			}
			_, err = run(ctx, s)
			return err
		},
		assignMount: func(ctx context.Context, n int, mount string) error {
			s, err := renderMountScript(n, mount)
			if err != nil {
				return err
			}
			_, err = run(ctx, s)
			return err
		},
		letterFree: letterFree,
		volumeDisk: volumeDisk,
	}
}

// letterFree reports whether drive letter is unused, from the
// GetLogicalDrives bitmask (bit 0 is A:).
func letterFree(letter byte) (bool, error) {
	if letter < 'A' || letter > 'Z' {
		return false, fmt.Errorf("invalid drive letter %q", letter)
	}
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return false, fmt.Errorf("GetLogicalDrives: %w", err)
	}
	return mask&(1<<(letter-'A')) == 0, nil
}

// ioctlVolumeGetVolumeDiskExtents is IOCTL_VOLUME_GET_VOLUME_DISK_EXTENTS
// (winioctl.h): CTL_CODE(IOCTL_VOLUME_BASE 'V', 0, METHOD_BUFFERED,
// FILE_ANY_ACCESS) = 0x00560000.
const ioctlVolumeGetVolumeDiskExtents = 0x00560000

// volumeDiskExtents mirrors VOLUME_DISK_EXTENTS with room for one
// DISK_EXTENT: NumberOfDiskExtents (4), padding (4), then DiskNumber (4),
// padding (4), StartingOffset (8), ExtentLength (8) = 32 bytes.
type volumeDiskExtents struct {
	NumberOfDiskExtents uint32
	_                   uint32
	DiskNumber          uint32
	_                   uint32
	StartingOffset      int64
	ExtentLength        int64
}

// volumeDisk reports whether a volume is mounted at the drive letter mount
// ("X:") and the number of the disk holding it, -1 when the volume spans
// several disks. Opening the volume with no access rights needs no
// administrator rights; FILE_ANY_ACCESS IOCTLs work on such a handle.
func volumeDisk(ctx context.Context, mount string) (int, bool, error) {
	if err := ctx.Err(); err != nil {
		return 0, false, err
	}
	if !letterMount.MatchString(mount) {
		return 0, false, fmt.Errorf("volume disk: unsupported mount %q", mount)
	}
	name, err := windows.UTF16PtrFromString(`\\.\` + mount)
	if err != nil {
		return 0, false, err
	}
	h, err := windows.CreateFile(name, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
		windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("open volume %s: %w", mount, err)
	}
	defer func() { _ = windows.CloseHandle(h) }() // query-only handle: a close failure loses nothing

	var ext volumeDiskExtents
	size := uint32(unsafe.Sizeof(ext))
	var returned uint32
	buf := (*byte)(unsafe.Pointer(&ext)) //nolint:gosec // G103: sized out buffer of the ioctl
	err = windows.DeviceIoControl(h, ioctlVolumeGetVolumeDiskExtents, nil, 0, buf, size, &returned, nil)
	switch {
	case errors.Is(err, windows.ERROR_MORE_DATA):
		return -1, true, nil // spans several disks: not a single VHDX volume
	case err != nil:
		return 0, false, fmt.Errorf("disk extents of %s: %w", mount, err)
	case ext.NumberOfDiskExtents != 1:
		return -1, true, nil
	}
	return int(ext.DiskNumber), true, nil
}

// virtdiskBackend drives the VHDX through virtdisk.dll (virtdisk_windows.go).
type virtdiskBackend struct{}

func (virtdiskBackend) create(ctx context.Context, path string, size uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	h, err := createVHDX(path, size)
	if err != nil {
		return err
	}
	return closeVHDX(h)
}

func (virtdiskBackend) attach(ctx context.Context, path string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	h, err := openVHDX(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = closeVHDX(h) }() // a permanent attach outlives the handle
	if err := attach(h); err != nil {
		return 0, err
	}
	return physicalPath(h)
}

func (virtdiskBackend) detach(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	h, err := openVHDX(path)
	if err != nil {
		return err
	}
	defer func() { _ = closeVHDX(h) }() // detach already happened or failed; the handle holds nothing
	return detach(h)
}

// disk reports the disk as attached exactly when GetVirtualDiskPhysicalPath
// succeeds on an opened handle.
func (virtdiskBackend) disk(ctx context.Context, path string) (int, bool, error) {
	if err := ctx.Err(); err != nil {
		return 0, false, err
	}
	h, err := openVHDX(path)
	if err != nil {
		return 0, false, err
	}
	defer func() { _ = closeVHDX(h) }() // query-only handle
	n, err := physicalPath(h)
	if err != nil {
		// A detached disk has no physical path: not attached, not an error.
		return 0, false, nil
	}
	return n, true, nil
}

// closeVHDX closes a virtual disk handle.
func closeVHDX(h windows.Handle) error {
	if err := windows.CloseHandle(h); err != nil {
		return fmt.Errorf("close virtual disk: %w", err)
	}
	return nil
}

// psBackend is the Hyper-V PowerShell fallback used when virtdisk.dll is
// unavailable. cause is why virtdisk was rejected; every error reports both
// causes, since the fallback can itself be unavailable (no Hyper-V module
// on Windows Home).
type psBackend struct {
	run   func(context.Context, string) (string, error)
	cause error
}

func (b psBackend) script(ctx context.Context, src, path string, size uint64) (string, error) {
	s, err := renderPathScript(src, path, size)
	if err != nil {
		return "", err
	}
	out, err := b.run(ctx, s)
	if err != nil {
		return "", fmt.Errorf("virtdisk unavailable (%w); PowerShell fallback failed: %w", b.cause, err)
	}
	return out, nil
}

func (b psBackend) create(ctx context.Context, path string, size uint64) error {
	_, err := b.script(ctx, newVHDScript, path, size)
	return err
}

func (b psBackend) attach(ctx context.Context, path string) (int, error) {
	out, err := b.script(ctx, mountVHDScript, path, 0)
	if err != nil {
		return 0, err
	}
	return parseScriptInt(out)
}

func (b psBackend) detach(ctx context.Context, path string) error {
	_, err := b.script(ctx, dismountVHDScript, path, 0)
	return err
}

func (b psBackend) disk(ctx context.Context, path string) (int, bool, error) {
	out, err := b.script(ctx, vhdDiskScript, path, 0)
	if err != nil {
		return 0, false, err
	}
	n, err := parseScriptInt(out)
	if err != nil {
		return 0, false, err
	}
	return n, n >= 0, nil
}
