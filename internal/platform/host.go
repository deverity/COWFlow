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
	Mount     string // drive letter "D:" to "Z:"; no mount-point form in v1 (SCOPE.md §23 open question 8)
}

// DevDriveStatus reports the observed state of a Dev Drive. The platform
// layer records nothing: the Dev Drive is registered only in [devdrive] of
// config.toml (SCOPE.md §6.4 step 5; no SQLite row), and this state is
// probed live on every call.
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

// AutoAttacher registers attach-at-boot (§6.4 step 5): on Windows a Task
// Scheduler task that runs `cowflow devdrive attach` at logon. Enable and
// Disable are idempotent.
type AutoAttacher interface {
	Enable(ctx context.Context, spec DevDriveSpec) error
	Disable(ctx context.Context, vhdxPath string) error
}

// host is the real Host. OS, Volume and ProjFSAvailable are implemented per
// OS in osinfo_*.go, fscaps_*.go and projfs_probe_*.go.
type host struct{}

// NewHost returns the Host backed by the real operating system.
func NewHost() Host { return &host{} }

// NewDevDrive returns the DevDrive backed by the real operating system
// (devdrive.go, devdrive_windows.go). On every OS but Windows its
// operations report ErrUnsupported.
func NewDevDrive(h Host) DevDrive { return newDevDrive(h) }
