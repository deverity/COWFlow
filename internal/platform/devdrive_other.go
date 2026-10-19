//go:build !windows

package platform

import "context"

// newDevDrive returns a DevDrive that reports ErrUnsupported: Dev Drive is
// Windows-only (SCOPE.md §6.4).
func newDevDrive(Host) DevDrive { return unsupportedDevDrive{} }

// unsupportedDevDrive is the DevDrive of every OS but Windows.
type unsupportedDevDrive struct{}

func (unsupportedDevDrive) Provision(context.Context, DevDriveSpec) (DevDriveStatus, error) {
	return DevDriveStatus{}, ErrUnsupported
}

func (unsupportedDevDrive) Attach(context.Context, DevDriveSpec) (DevDriveStatus, error) {
	return DevDriveStatus{}, ErrUnsupported
}

func (unsupportedDevDrive) Detach(context.Context, string) error { return ErrUnsupported }

func (unsupportedDevDrive) Status(context.Context, DevDriveSpec) (DevDriveStatus, error) {
	return DevDriveStatus{}, ErrUnsupported
}
