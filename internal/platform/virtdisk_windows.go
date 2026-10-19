//go:build windows

package platform

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Thin mirrors of the Win32 virtual disk API (virtdisk.dll) used to create
// and attach the Dev Drive VHDX (SCOPE.md §6.4 steps 2-3). Orchestration,
// formatting and compensation live in the DevDrive implementation (S0-T16).
//
// Every virtdisk function returns its Win32 error code as the return value;
// the error from LazyProc.Call is GetLastError and is ignored because it is
// often stale.

var (
	modVirtDisk                    = windows.NewLazySystemDLL("virtdisk.dll")
	procCreateVirtualDisk          = modVirtDisk.NewProc("CreateVirtualDisk")
	procOpenVirtualDisk            = modVirtDisk.NewProc("OpenVirtualDisk")
	procAttachVirtualDisk          = modVirtDisk.NewProc("AttachVirtualDisk")
	procDetachVirtualDisk          = modVirtDisk.NewProc("DetachVirtualDisk")
	procGetVirtualDiskPhysicalPath = modVirtDisk.NewProc("GetVirtualDiskPhysicalPath")
)

// errVirtDiskUnavailable lets S0-T16 fall back to PowerShell New-VHD.
var errVirtDiskUnavailable = errors.New("virtdisk.dll procedures unavailable")

// virtdiskAvailable reports whether every virtdisk procedure this file uses
// can be resolved. The struct mirrors below match the C layout only on
// 64-bit Windows (amd64, arm64): on 32-bit targets Go aligns uint64 to 4
// bytes where C aligns it to 8, so the bindings are reported unavailable
// there instead of passing a misaligned struct to the API.
func virtdiskAvailable() error {
	if strconv.IntSize != 64 {
		return fmt.Errorf("%w: struct layouts are only verified on 64-bit Windows", errVirtDiskUnavailable)
	}
	for _, p := range []*windows.LazyProc{procCreateVirtualDisk, procOpenVirtualDisk,
		procAttachVirtualDisk, procDetachVirtualDisk, procGetVirtualDiskPhysicalPath} {
		if err := p.Find(); err != nil {
			return fmt.Errorf("%w: %w", errVirtDiskUnavailable, err)
		}
	}
	return nil
}

// virtualStorageType mirrors VIRTUAL_STORAGE_TYPE.
type virtualStorageType struct {
	DeviceID uint32
	VendorID windows.GUID
}

const virtualStorageTypeDeviceVHDX = 3 // VIRTUAL_STORAGE_TYPE_DEVICE_VHDX

// vendorMicrosoft is VIRTUAL_STORAGE_TYPE_VENDOR_MICROSOFT
// {EC984AEC-A0F9-47E9-901F-71415A66345B}.
var vendorMicrosoft = windows.GUID{Data1: 0xec984aec, Data2: 0xa0f9, Data3: 0x47e9,
	Data4: [8]byte{0x90, 0x1f, 0x71, 0x41, 0x5a, 0x66, 0x34, 0x5b}}

// createVirtualDiskParameters mirrors CREATE_VIRTUAL_DISK_PARAMETERS with
// Version = CREATE_VIRTUAL_DISK_VERSION_2. The C union shape (Version followed
// by a nested struct) is kept on purpose: V2 starts at offset 8 because of the
// padding C inserts after Version. Flattening the union loses that padding.
type createVirtualDiskParameters struct {
	Version uint32 // CREATE_VIRTUAL_DISK_VERSION_2 = 2
	V2      createVirtualDiskV2
}

// createVirtualDiskV2 mirrors the Version2 member of
// CREATE_VIRTUAL_DISK_PARAMETERS.
type createVirtualDiskV2 struct {
	UniqueID                  windows.GUID
	MaximumSize               uint64
	BlockSizeInBytes          uint32
	SectorSizeInBytes         uint32
	PhysicalSectorSizeInBytes uint32
	ParentPath                *uint16
	SourcePath                *uint16
	OpenFlags                 uint32
	ParentVirtualStorageType  virtualStorageType
	SourceVirtualStorageType  virtualStorageType
	ResiliencyGUID            windows.GUID
}

const createVirtualDiskVersion2 = 2

// openVirtualDiskParameters mirrors OPEN_VIRTUAL_DISK_PARAMETERS with
// Version = OPEN_VIRTUAL_DISK_VERSION_2 (required for VHDX opened with
// VIRTUAL_DISK_ACCESS_NONE).
type openVirtualDiskParameters struct {
	Version uint32 // OPEN_VIRTUAL_DISK_VERSION_2 = 2
	V2      openVirtualDiskV2
}

// openVirtualDiskV2 mirrors the Version2 member of
// OPEN_VIRTUAL_DISK_PARAMETERS.
type openVirtualDiskV2 struct {
	GetInfoOnly    int32 // BOOL
	ReadOnly       int32 // BOOL
	ResiliencyGUID windows.GUID
}

const openVirtualDiskVersion2 = 2

// attachVirtualDiskParameters mirrors ATTACH_VIRTUAL_DISK_PARAMETERS with
// Version = ATTACH_VIRTUAL_DISK_VERSION_1.
type attachVirtualDiskParameters struct {
	Version  uint32 // ATTACH_VIRTUAL_DISK_VERSION_1 = 1
	Reserved uint32
}

const (
	attachVirtualDiskVersion1   = 1
	attachFlagPermanentLifetime = 0x4 // ATTACH_VIRTUAL_DISK_FLAG_PERMANENT_LIFETIME
	virtualDiskAccessNone       = 0   // VIRTUAL_DISK_ACCESS_NONE (mandatory with version 2 parameters)
)

// physicalDrivePrefix is the form GetVirtualDiskPhysicalPath returns:
// \\.\PhysicalDriveN.
const physicalDrivePrefix = `\\.\PhysicalDrive`

// createVHDX creates a dynamic VHDX of the given maximum size at path and
// returns its handle. The caller closes the handle with windows.CloseHandle.
func createVHDX(path string, size uint64) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(LongPath(path))
	if err != nil {
		return 0, err
	}
	st := virtualStorageType{DeviceID: virtualStorageTypeDeviceVHDX, VendorID: vendorMicrosoft}
	params := createVirtualDiskParameters{Version: createVirtualDiskVersion2, V2: createVirtualDiskV2{MaximumSize: size}}
	var h windows.Handle
	r, _, _ := procCreateVirtualDisk.Call(
		uintptr(unsafe.Pointer(&st)), //nolint:gosec // G103: Win32 ABI requires a pointer to the struct
		uintptr(unsafe.Pointer(p)),   //nolint:gosec // G103: Win32 ABI requires a pointer to the path
		virtualDiskAccessNone,
		0,                                // no security descriptor
		0,                                // CREATE_VIRTUAL_DISK_FLAG_NONE: dynamic (not fully allocated)
		0,                                // no provider-specific flags
		uintptr(unsafe.Pointer(&params)), //nolint:gosec // G103: Win32 ABI requires a pointer to the struct
		0,                                // no OVERLAPPED: synchronous
		uintptr(unsafe.Pointer(&h)))      //nolint:gosec // G103: Win32 ABI requires an out pointer for the handle
	if r != 0 {
		return 0, fmt.Errorf("CreateVirtualDisk: %w", windows.Errno(r))
	}
	return h, nil
}

// openVHDX opens an existing VHDX at path. The caller closes the handle with
// windows.CloseHandle.
func openVHDX(path string) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(LongPath(path))
	if err != nil {
		return 0, err
	}
	st := virtualStorageType{DeviceID: virtualStorageTypeDeviceVHDX, VendorID: vendorMicrosoft}
	params := openVirtualDiskParameters{Version: openVirtualDiskVersion2}
	var h windows.Handle
	r, _, _ := procOpenVirtualDisk.Call(
		uintptr(unsafe.Pointer(&st)), //nolint:gosec // G103: Win32 ABI requires a pointer to the struct
		uintptr(unsafe.Pointer(p)),   //nolint:gosec // G103: Win32 ABI requires a pointer to the path
		virtualDiskAccessNone,
		0,                                // OPEN_VIRTUAL_DISK_FLAG_NONE
		uintptr(unsafe.Pointer(&params)), //nolint:gosec // G103: Win32 ABI requires a pointer to the struct
		uintptr(unsafe.Pointer(&h)))      //nolint:gosec // G103: Win32 ABI requires an out pointer for the handle
	if r != 0 {
		return 0, fmt.Errorf("OpenVirtualDisk: %w", windows.Errno(r))
	}
	return h, nil
}

// attach attaches the virtual disk with a permanent lifetime, so it stays
// attached after the handle is closed (but not across a reboot). attach
// assigns no drive letter itself: a freshly created disk has no volume yet,
// and letters are set by the format/mount step (S0-T16).
func attach(h windows.Handle) error {
	params := attachVirtualDiskParameters{Version: attachVirtualDiskVersion1}
	r, _, _ := procAttachVirtualDisk.Call(
		uintptr(h),
		0, // no security descriptor
		attachFlagPermanentLifetime,
		0,                                // no provider-specific flags
		uintptr(unsafe.Pointer(&params)), //nolint:gosec // G103: Win32 ABI requires a pointer to the struct
		0)                                // no OVERLAPPED: synchronous
	if r != 0 {
		return fmt.Errorf("AttachVirtualDisk: %w", windows.Errno(r))
	}
	return nil
}

// detach detaches the virtual disk, including a permanent-lifetime attach.
func detach(h windows.Handle) error {
	r, _, _ := procDetachVirtualDisk.Call(
		uintptr(h),
		0, // DETACH_VIRTUAL_DISK_FLAG_NONE
		0) // no provider-specific flags
	if r != 0 {
		return fmt.Errorf("DetachVirtualDisk: %w", windows.Errno(r))
	}
	return nil
}

// physicalPath returns the disk number N of an attached virtual disk, read
// from its physical path \\.\PhysicalDriveN.
func physicalPath(h windows.Handle) (int, error) {
	buf := make([]uint16, windows.MAX_PATH)
	for {
		size := uint32(2 * len(buf)) //nolint:gosec // G115: buffer length is small and bounded below
		r, _, _ := procGetVirtualDiskPhysicalPath.Call(
			uintptr(h),
			uintptr(unsafe.Pointer(&size)),   //nolint:gosec // G103: Win32 ABI requires an in/out size pointer
			uintptr(unsafe.Pointer(&buf[0]))) //nolint:gosec // G103: Win32 ABI requires a pointer to the buffer
		switch {
		case r == 0:
			return parsePhysicalDrive(windows.UTF16ToString(buf))
		case windows.Errno(r) == windows.ERROR_INSUFFICIENT_BUFFER && int(size/2) > len(buf) && size <= 1<<16:
			buf = make([]uint16, size/2)
		default:
			return 0, fmt.Errorf("GetVirtualDiskPhysicalPath: %w", windows.Errno(r))
		}
	}
}

// parsePhysicalDrive extracts N from \\.\PhysicalDriveN and fails on any
// other form (no sign, no spaces, decimal digits only).
func parsePhysicalDrive(s string) (int, error) {
	digits, ok := strings.CutPrefix(s, physicalDrivePrefix)
	if !ok || digits == "" || strings.TrimLeft(digits, "0123456789") != "" {
		return 0, fmt.Errorf("unexpected virtual disk physical path %q", s)
	}
	n, err := strconv.Atoi(digits)
	if err != nil {
		return 0, fmt.Errorf("unexpected virtual disk physical path %q: %w", s, err)
	}
	return n, nil
}
