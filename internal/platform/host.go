// Package platform hides OS-specific behavior (volumes, Dev Drive, paths,
// process trees) behind small interfaces so the rest of COWFlow and the
// hermetic test harness never touch the OS directly.
package platform

import (
	"context"
	"errors"
)

// Sentinel errors returned by platform operations.
var (
	ErrUnsupported   = errors.New("platform: not supported on this OS")
	ErrNotElevated   = errors.New("platform: administrator rights required")
	ErrOSTooOld      = errors.New("platform: Windows 11 22H2 or later required")
	ErrNotMounted    = errors.New("platform: volume not mounted")
	ErrAlreadyExists = errors.New("platform: virtual disk already exists")
)

// OSInfo describes the host operating system.
type OSInfo struct {
	GOOS     string
	Build    uint32 // Windows build number (e.g. 22631); 0 elsewhere
	Elevated bool
}

// VolumeInfo describes the volume a path lives on.
type VolumeInfo struct {
	Path       string // volume root, e.g. `X:\` or "/"
	Filesystem string // as reported by the OS: "ReFS", "NTFS", "btrfs", "xfs", "apfs", ...
	BlockClone bool   // FS advertises block cloning (Windows: FILE_SUPPORTS_BLOCK_REFCOUNTING)
	DevDrive   bool   // Windows only: volume is a Dev Drive
}

// Host answers questions about the machine. doctor depends only on this.
type Host interface {
	OS(ctx context.Context) (OSInfo, error)
	Volume(ctx context.Context, path string) (VolumeInfo, error)
	ProjFSAvailable(ctx context.Context) (bool, error)
}

// DevDriveSpec describes the VHDX-backed Dev Drive to manage.
type DevDriveSpec struct {
	VHDXPath  string // canonical absolute path, e.g. C:\cf\cowflow.vhdx
	SizeBytes uint64 // >= 50 GiB (Dev Drive minimum)
	Mount     string // "X:" or a directory under C:\cf\ (U-PLAT-9)
}

// DevDriveStatus reports the state of a Dev Drive.
type DevDriveStatus struct {
	VHDXPath string
	Exists   bool
	Attached bool
	Mount    string
	Volume   *VolumeInfo // nil when not mounted
}

// DevDrive is the "simulated VHDX (interface)" seam of SCOPE.md §18.1.
type DevDrive interface {
	Provision(ctx context.Context, spec DevDriveSpec) (DevDriveStatus, error) // §6.4 steps 2-4
	Attach(ctx context.Context, spec DevDriveSpec) (DevDriveStatus, error)
	Detach(ctx context.Context, vhdxPath string) error
	Status(ctx context.Context, spec DevDriveSpec) (DevDriveStatus, error)
}

// AutoAttacher registers attach-at-boot (§6.4 step 5).
// Mechanism pending SPEC-GAPS U-PLAT-5.
type AutoAttacher interface {
	Enable(ctx context.Context, spec DevDriveSpec) error
	Disable(ctx context.Context, vhdxPath string) error
}

// host is the real Host. OS and Volume are implemented per OS in
// osinfo_*.go and fscaps_*.go.
type host struct{}

// NewHost returns the Host backed by the real operating system.
func NewHost() Host { return &host{} }

// ProjFSAvailable reports whether ProjFS can be used.
// PENDING S0-T19: the probe lands with the doctor platform checks.
func (h *host) ProjFSAvailable(context.Context) (bool, error) {
	return false, ErrUnsupported
}

// devDrive is the real DevDrive; it is implemented by S0-T15/S0-T16.
type devDrive struct{ host Host }

// NewDevDrive returns a DevDrive whose operations report ErrUnsupported
// until S0-T16 provides the Windows implementation.
func NewDevDrive(h Host) DevDrive { return &devDrive{host: h} }

func (d *devDrive) Provision(context.Context, DevDriveSpec) (DevDriveStatus, error) {
	return DevDriveStatus{}, ErrUnsupported
}

func (d *devDrive) Attach(context.Context, DevDriveSpec) (DevDriveStatus, error) {
	return DevDriveStatus{}, ErrUnsupported
}

func (d *devDrive) Detach(context.Context, string) error { return ErrUnsupported }

func (d *devDrive) Status(context.Context, DevDriveSpec) (DevDriveStatus, error) {
	return DevDriveStatus{}, ErrUnsupported
}

// OS and Volume are placeholders until the per-OS implementations land.
func (h *host) OS(context.Context) (OSInfo, error) { return OSInfo{}, ErrUnsupported }

func (h *host) Volume(context.Context, string) (VolumeInfo, error) {
	return VolumeInfo{}, ErrUnsupported
}
