//go:build windows

package platform

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Constants from the Windows SDK winioctl.h (10.0.22621.0):
//
//	FSCTL_QUERY_PERSISTENT_VOLUME_STATE =
//	    CTL_CODE(FILE_DEVICE_FILE_SYSTEM, 143, METHOD_BUFFERED, FILE_ANY_ACCESS)
//	    = 0x0009023C
//	PERSISTENT_VOLUME_STATE_DEV_VOLUME = 0x00002000
const (
	fsctlQueryPersistentVolumeState = 0x0009023C
	persistentVolumeStateDevVolume  = 0x00002000
)

// filePersistentVolumeInformation is FILE_FS_PERSISTENT_VOLUME_INFORMATION
// (winioctl.h): four ULONGs, 16 bytes.
type filePersistentVolumeInformation struct {
	VolumeFlags uint32
	FlagMask    uint32
	Version     uint32
	Reserved    uint32
}

// isDevDrive reports whether the volume rooted at root (e.g. `X:\`) is a
// Dev Drive. It opens the root directory with backup semantics, which needs
// no administrator rights (opening the raw volume `\\.\X:` would).
// Filesystems or Windows builds that do not know the FSCTL report false.
func isDevDrive(root string) (bool, error) {
	p16, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return false, err
	}
	h, err := windows.CreateFile(p16, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return false, err
	}
	defer func() { _ = windows.CloseHandle(h) }()

	info := filePersistentVolumeInformation{
		FlagMask: persistentVolumeStateDevVolume,
		Version:  1,
	}
	size := uint32(unsafe.Sizeof(info))
	var returned uint32
	// G103: info is a local fixed-size struct passed as the in/out buffer of
	// size bytes, as DeviceIoControl requires.
	buf := (*byte)(unsafe.Pointer(&info)) //nolint:gosec // G103: sized in/out ioctl buffer
	err = windows.DeviceIoControl(h, fsctlQueryPersistentVolumeState,
		buf, size, buf, size, &returned, nil)
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_FUNCTION) ||
			errors.Is(err, windows.ERROR_NOT_SUPPORTED) {
			return false, nil
		}
		return false, err
	}
	return info.VolumeFlags&persistentVolumeStateDevVolume != 0, nil
}
