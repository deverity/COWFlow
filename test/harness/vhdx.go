package harness

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"

	"github.com/deverity/cowflow/internal/platform"
)

// ErrInjected is wrapped by every failure SimDevDrive injects through FailAt.
var ErrInjected = errors.New("harness: injected failure")

// SimStep names a provisioning step that can be failed on demand.
type SimStep string

// Provisioning steps of SCOPE.md §6.4, in execution order.
const (
	StepCreate SimStep = "create"
	StepAttach SimStep = "attach"
	StepFormat SimStep = "format"
	StepMount  SimStep = "mount"
)

var provisionSteps = []SimStep{StepCreate, StepAttach, StepFormat, StepMount}

type simDisk struct {
	spec     platform.DevDriveSpec
	attached bool
}

// SimDevDrive is an in-memory platform.DevDrive. It registers a ReFS Dev
// Drive volume in Host.Volumes while the disk is attached and removes it on
// detach: it never leaves any other volume (for example NTFS) in its place
// (SCOPE.md §18 "VHDX not mounted").
//
// Host may be nil; a private FakeHost is then created on first use.
// Calls records each step in order, for example "provision:create". The
// type is not safe for concurrent use.
type SimDevDrive struct {
	Host   *FakeHost
	FailAt SimStep // inject a failure at this step; empty means never
	disks  map[string]*simDisk
	Calls  []string
}

var _ platform.DevDrive = (*SimDevDrive)(nil)

// Provision creates, attaches, formats and mounts the disk. When FailAt
// names one of those steps it stops there, undoes everything it did
// (compensation) and returns an error wrapping ErrInjected.
func (d *SimDevDrive) Provision(ctx context.Context, spec platform.DevDriveSpec) (platform.DevDriveStatus, error) {
	if err := ctx.Err(); err != nil {
		return platform.DevDriveStatus{}, err
	}
	if _, ok := d.disks[spec.VHDXPath]; ok {
		return platform.DevDriveStatus{}, platform.ErrAlreadyExists
	}
	var disk *simDisk
	for _, step := range provisionSteps {
		d.Calls = append(d.Calls, "provision:"+string(step))
		if d.FailAt == step {
			d.compensate(spec)
			return platform.DevDriveStatus{}, fmt.Errorf("provision %s: %w", step, ErrInjected)
		}
		switch step {
		case StepCreate:
			disk = &simDisk{spec: spec}
			d.putDisk(disk)
		case StepAttach:
			disk.attached = true
		case StepMount:
			d.mount(disk)
		}
	}
	return d.status(spec.VHDXPath, spec), nil
}

// Attach restores the volume of an existing, detached disk. FailAt
// StepAttach or StepMount fails it and leaves the disk detached.
func (d *SimDevDrive) Attach(ctx context.Context, spec platform.DevDriveSpec) (platform.DevDriveStatus, error) {
	if err := ctx.Err(); err != nil {
		return platform.DevDriveStatus{}, err
	}
	disk, ok := d.disks[spec.VHDXPath]
	if !ok {
		return platform.DevDriveStatus{}, fmt.Errorf("attach %s: %w", spec.VHDXPath, fs.ErrNotExist)
	}
	for _, step := range []SimStep{StepAttach, StepMount} {
		d.Calls = append(d.Calls, "attach:"+string(step))
		if d.FailAt == step {
			d.unmount(disk)
			disk.attached = false
			return platform.DevDriveStatus{}, fmt.Errorf("attach %s: %w", step, ErrInjected)
		}
		if step == StepAttach {
			disk.attached = true
		} else {
			d.mount(disk)
		}
	}
	return d.status(spec.VHDXPath, disk.spec), nil
}

// Detach removes the volume of the disk, as an unmount or reboot would.
// Detaching an already detached disk is a no-op.
func (d *SimDevDrive) Detach(ctx context.Context, vhdxPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.Calls = append(d.Calls, "detach")
	disk, ok := d.disks[vhdxPath]
	if !ok {
		return fmt.Errorf("detach %s: %w", vhdxPath, fs.ErrNotExist)
	}
	d.unmount(disk)
	disk.attached = false
	return nil
}

// Status reports the simulated state of the disk named by spec.VHDXPath.
// Volume is nil unless the disk is attached and mounted.
func (d *SimDevDrive) Status(ctx context.Context, spec platform.DevDriveSpec) (platform.DevDriveStatus, error) {
	if err := ctx.Err(); err != nil {
		return platform.DevDriveStatus{}, err
	}
	return d.status(spec.VHDXPath, spec), nil
}

// SimulateReboot detaches every disk, the way a reboot drops a VHDX that
// has no auto-attach.
func (d *SimDevDrive) SimulateReboot() {
	d.Calls = append(d.Calls, "reboot")
	paths := make([]string, 0, len(d.disks))
	for p := range d.disks {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		disk := d.disks[p]
		d.unmount(disk)
		disk.attached = false
	}
}

func (d *SimDevDrive) host() *FakeHost {
	if d.Host == nil {
		d.Host = &FakeHost{}
	}
	return d.Host
}

func (d *SimDevDrive) putDisk(disk *simDisk) {
	if d.disks == nil {
		d.disks = make(map[string]*simDisk)
	}
	d.disks[disk.spec.VHDXPath] = disk
}

func (d *SimDevDrive) compensate(spec platform.DevDriveSpec) {
	if disk, ok := d.disks[spec.VHDXPath]; ok {
		d.unmount(disk)
		delete(d.disks, spec.VHDXPath)
	}
}

func (d *SimDevDrive) mount(disk *simDisk) {
	d.host().setVolume(disk.spec.Mount, platform.VolumeInfo{
		Path:       disk.spec.Mount,
		Filesystem: "ReFS",
		BlockClone: true,
		DevDrive:   true,
	})
}

func (d *SimDevDrive) unmount(disk *simDisk) {
	d.host().removeVolume(disk.spec.Mount)
}

func (d *SimDevDrive) status(vhdxPath string, spec platform.DevDriveSpec) platform.DevDriveStatus {
	st := platform.DevDriveStatus{VHDXPath: vhdxPath, Mount: spec.Mount}
	disk, ok := d.disks[vhdxPath]
	if !ok {
		return st
	}
	st.Exists = true
	st.Mount = disk.spec.Mount
	st.Attached = disk.attached
	if disk.attached {
		if v, ok := d.host().volume(disk.spec.Mount); ok {
			st.Volume = &v
		}
	}
	return st
}
