package platform

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Dev Drive provisioning (SCOPE.md §6.4 steps 1-4). The orchestration in
// this file is OS-independent so that validation, step order and
// compensation are unit-tested on every OS with fake primitives; the real
// primitives (virtdisk.dll, PowerShell, volume IOCTLs) are wired in
// devdrive_windows.go. Other OSes get a DevDrive that reports ErrUnsupported
// (devdrive_other.go).

const (
	// minDevDriveBuild is Windows 11 22H2, the first build with Dev Drive.
	minDevDriveBuild = 22621

	// minDevDriveSize is the Dev Drive minimum size (Microsoft documents
	// 50 GB; 50 GiB is used so the formatted volume is never below it).
	minDevDriveSize = 50 << 30

	// compensateTimeout bounds the cleanup after a failed provisioning. It
	// runs on a context detached from the caller's cancellation, so a
	// canceled Provision still detaches and deletes what it created.
	compensateTimeout = 2 * time.Minute
)

// errInvalidSpec is wrapped by every DevDriveSpec validation failure.
var errInvalidSpec = errors.New("platform: invalid dev drive spec")

// letterMount is the accepted drive-letter mount form (SCOPE.md §6.4 step 4).
var letterMount = regexp.MustCompile(`^[D-Z]:$`)

// diskBackend creates, attaches and detaches the VHDX. The Windows build has
// two: virtdisk.dll (preferred) and the PowerShell Hyper-V cmdlets
// (fallback when virtdisk is unavailable, SCOPE.md §6.4 step 2).
type diskBackend interface {
	// create creates a dynamic VHDX of size bytes at path, not attached.
	create(ctx context.Context, path string, size uint64) error
	// attach attaches the VHDX at path for the lifetime of the boot and
	// returns its disk number.
	attach(ctx context.Context, path string) (int, error)
	// detach detaches the VHDX at path.
	detach(ctx context.Context, path string) error
	// disk reports whether the VHDX at path is attached and, if so, its
	// disk number.
	disk(ctx context.Context, path string) (n int, attached bool, err error)
}

// devDriveOps are the OS primitives the orchestration depends on. They are
// injected (not package-level variables) so tests can fail any step.
type devDriveOps struct {
	// backend returns the disk backend to use: virtdisk.dll when it is
	// available, otherwise the PowerShell fallback.
	backend func() diskBackend
	// format initializes disk n as GPT, creates one partition, formats it
	// as a Dev Drive and mounts it at mount.
	format func(ctx context.Context, n int, mount string) error
	// assignMount mounts the data partition of disk n at mount.
	assignMount func(ctx context.Context, n int, mount string) error
	// letterFree reports whether drive letter (e.g. 'X') is unused.
	letterFree func(letter byte) (bool, error)
	// volumeDisk reports whether a volume is mounted at mount and the disk
	// number holding it (-1 when the volume spans several disks).
	volumeDisk func(ctx context.Context, mount string) (n int, mounted bool, err error)
}

// devDrive is the real DevDrive: the §6.4 sequence over devDriveOps.
type devDrive struct {
	host Host
	ops  devDriveOps
}

// checkHost enforces SCOPE.md §6.4 step 1: Windows 11 22H2+ and an elevated
// process. Elevation itself is the CLI's job (RunElevated).
func (d *devDrive) checkHost(ctx context.Context) error {
	info, err := d.host.OS(ctx)
	if err != nil {
		return fmt.Errorf("platform: devdrive: os info: %w", err)
	}
	if info.Build < minDevDriveBuild {
		return fmt.Errorf("%w (build %d)", ErrOSTooOld, info.Build)
	}
	if !info.Elevated {
		return ErrNotElevated
	}
	return nil
}

// canonicalVHDX validates the form of a VHDX path and returns its canonical
// form: absolute, ending in .vhdx, with links of existing ancestors resolved.
func canonicalVHDX(p string) (string, error) {
	if p == "" || !filepath.IsAbs(p) {
		return "", fmt.Errorf("%w: vhdx path %q is not absolute", errInvalidSpec, p)
	}
	c, err := Canonical(p)
	if err != nil {
		return "", fmt.Errorf("%w: vhdx path: %w", errInvalidSpec, err)
	}
	if !strings.EqualFold(filepath.Ext(c), ".vhdx") {
		return "", fmt.Errorf("%w: vhdx path %q does not end in .vhdx", errInvalidSpec, p)
	}
	return c, nil
}

// checkMountForm accepts a drive letter D: to Z:.
func checkMountForm(mount string) error {
	if letterMount.MatchString(mount) {
		return nil
	}
	// PENDING U-PLAT-9: SCOPE.md §6.4 step 4 also allows "a mount point
	// under C:\cf\", whose form is undecided. Until it is, only drive
	// letters are accepted; the format and mount scripts already handle a
	// directory access path, so enabling it is a validation change only.
	return fmt.Errorf("%w: mount %q must be a drive letter D: to Z: "+
		"(mount points under C:\\cf\\ are pending SPEC-GAPS U-PLAT-9)", errInvalidSpec, mount)
}

// checkLetterFree fails when the drive letter of mount is in use.
func (d *devDrive) checkLetterFree(mount string) error {
	free, err := d.ops.letterFree(mount[0])
	if err != nil {
		return fmt.Errorf("platform: devdrive: drive letters: %w", err)
	}
	if !free {
		return fmt.Errorf("%w: drive letter %s is already in use", errInvalidSpec, mount)
	}
	return nil
}

// validateProvision checks spec for Provision (step 2) and returns it with
// the canonical VHDX path.
func (d *devDrive) validateProvision(spec DevDriveSpec) (DevDriveSpec, error) {
	if spec.SizeBytes < minDevDriveSize {
		return spec, fmt.Errorf("%w: size %d bytes is below the Dev Drive minimum of 50 GiB",
			errInvalidSpec, spec.SizeBytes)
	}
	p, err := canonicalVHDX(spec.VHDXPath)
	if err != nil {
		return spec, err
	}
	spec.VHDXPath = p
	switch _, err := os.Lstat(p); {
	case err == nil:
		return spec, fmt.Errorf("%w: %s", ErrAlreadyExists, p)
	case !errors.Is(err, fs.ErrNotExist):
		return spec, fmt.Errorf("platform: devdrive: %w", err)
	}
	if err := checkMountForm(spec.Mount); err != nil {
		return spec, err
	}
	if err := d.checkLetterFree(spec.Mount); err != nil {
		return spec, err
	}
	return spec, nil
}

// Provision turns "nothing" into an ReFS Dev Drive mounted at spec.Mount
// (SCOPE.md §6.4 steps 1-4): check the host, validate, create the dynamic
// VHDX, attach it, partition and format it as a Dev Drive, and confirm the
// result is ReFS. Any failure after the VHDX was created is compensated:
// the disk is detached and the VHDX this call created is deleted, so no
// half-provisioned disk is left behind.
func (d *devDrive) Provision(ctx context.Context, spec DevDriveSpec) (DevDriveStatus, error) {
	if err := d.checkHost(ctx); err != nil {
		return DevDriveStatus{}, err
	}
	spec, err := d.validateProvision(spec)
	if err != nil {
		return DevDriveStatus{}, err
	}
	if err := ctx.Err(); err != nil {
		return DevDriveStatus{}, err
	}
	if err := os.MkdirAll(filepath.Dir(spec.VHDXPath), 0o750); err != nil {
		return DevDriveStatus{}, fmt.Errorf("platform: devdrive: provision: create directory: %w", err)
	}

	b := d.ops.backend()
	if err := b.create(ctx, spec.VHDXPath, spec.SizeBytes); err != nil {
		// The file did not exist when validated; if it exists now, this
		// failed create left it behind.
		return DevDriveStatus{}, d.compensate(ctx, b, spec.VHDXPath, "create", err)
	}
	n, err := b.attach(ctx, spec.VHDXPath)
	if err != nil {
		return DevDriveStatus{}, d.compensate(ctx, b, spec.VHDXPath, "attach", err)
	}
	if err := d.ops.format(ctx, n, spec.Mount); err != nil {
		return DevDriveStatus{}, d.compensate(ctx, b, spec.VHDXPath, "partition and format", err)
	}
	st, err := d.status(ctx, b, spec)
	if err != nil {
		return DevDriveStatus{}, d.compensate(ctx, b, spec.VHDXPath, "verify", err)
	}
	switch {
	case st.Volume == nil:
		return DevDriveStatus{}, d.compensate(ctx, b, spec.VHDXPath, "verify",
			fmt.Errorf("%w at %s after format", ErrNotMounted, spec.Mount))
	case st.Volume.Filesystem != "ReFS":
		// Never report success on NTFS or anything else (SCOPE.md §18).
		return DevDriveStatus{}, d.compensate(ctx, b, spec.VHDXPath, "verify",
			fmt.Errorf("volume at %s is %q, not ReFS", spec.Mount, st.Volume.Filesystem))
	}
	return st, nil
}

// compensate undoes a failed provisioning: it detaches the disk if it is
// attached and deletes the VHDX at path, which Provision verified did not
// exist before it started. It returns an error naming the failed step and
// wrapping cause, plus any cleanup failure.
func (d *devDrive) compensate(ctx context.Context, b diskBackend, path, step string, cause error) error {
	failed := fmt.Errorf("platform: devdrive: provision: %s: %w", step, cause)

	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), compensateTimeout)
	defer cancel()
	if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		return failed // nothing was created
	}
	var cleanup []error
	if _, attached, err := b.disk(cctx, path); err != nil {
		cleanup = append(cleanup, fmt.Errorf("attached check: %w", err))
	} else if attached {
		if err := b.detach(cctx, path); err != nil {
			cleanup = append(cleanup, fmt.Errorf("detach: %w", err))
		}
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		cleanup = append(cleanup, fmt.Errorf("delete %s: %w", path, err))
	}
	if len(cleanup) == 0 {
		return failed
	}
	return fmt.Errorf("%w (compensation failed: %w)", failed, errors.Join(cleanup...))
}

// Attach attaches an existing Dev Drive VHDX and makes sure its volume is
// mounted at spec.Mount, assigning the letter when Windows did not restore
// it. Attaching an attached disk only checks and fixes the mount.
func (d *devDrive) Attach(ctx context.Context, spec DevDriveSpec) (DevDriveStatus, error) {
	if err := d.checkHost(ctx); err != nil {
		return DevDriveStatus{}, err
	}
	p, err := canonicalVHDX(spec.VHDXPath)
	if err != nil {
		return DevDriveStatus{}, err
	}
	spec.VHDXPath = p
	if err := checkMountForm(spec.Mount); err != nil {
		return DevDriveStatus{}, err
	}
	if _, err := os.Stat(p); err != nil {
		return DevDriveStatus{}, fmt.Errorf("platform: devdrive: attach: %w", err)
	}

	b := d.ops.backend()
	n, attached, err := b.disk(ctx, p)
	if err != nil {
		return DevDriveStatus{}, fmt.Errorf("platform: devdrive: attach: attached check: %w", err)
	}
	if !attached {
		if n, err = b.attach(ctx, p); err != nil {
			return DevDriveStatus{}, fmt.Errorf("platform: devdrive: attach: %w", err)
		}
	}
	vn, mounted, err := d.ops.volumeDisk(ctx, spec.Mount)
	if err != nil {
		return DevDriveStatus{}, fmt.Errorf("platform: devdrive: attach: volume at %s: %w", spec.Mount, err)
	}
	switch {
	case mounted && vn == n:
		// Windows restored the mount.
	case mounted:
		return DevDriveStatus{}, fmt.Errorf("platform: devdrive: attach: %s holds another volume", spec.Mount)
	default:
		// Windows may have given the volume another letter; that is fine,
		// but the target letter itself must be free.
		if err := d.checkLetterFree(spec.Mount); err != nil {
			return DevDriveStatus{}, fmt.Errorf("platform: devdrive: attach: %w", err)
		}
		if err := d.ops.assignMount(ctx, n, spec.Mount); err != nil {
			return DevDriveStatus{}, fmt.Errorf("platform: devdrive: attach: mount at %s: %w", spec.Mount, err)
		}
	}
	st, err := d.status(ctx, b, spec)
	if err != nil {
		return DevDriveStatus{}, fmt.Errorf("platform: devdrive: attach: %w", err)
	}
	if st.Volume == nil {
		return st, fmt.Errorf("platform: devdrive: attach: %w at %s", ErrNotMounted, spec.Mount)
	}
	return st, nil
}

// Detach detaches the Dev Drive VHDX at vhdxPath. A missing file is an
// error wrapping fs.ErrNotExist; detaching a detached disk is a no-op.
func (d *devDrive) Detach(ctx context.Context, vhdxPath string) error {
	if err := d.checkHost(ctx); err != nil {
		return err
	}
	p, err := canonicalVHDX(vhdxPath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(p); err != nil {
		return fmt.Errorf("platform: devdrive: detach: %w", err)
	}
	b := d.ops.backend()
	_, attached, err := b.disk(ctx, p)
	if err != nil {
		return fmt.Errorf("platform: devdrive: detach: attached check: %w", err)
	}
	if !attached {
		return nil
	}
	if err := b.detach(ctx, p); err != nil {
		return fmt.Errorf("platform: devdrive: detach: %w", err)
	}
	return nil
}

// Status reports whether the VHDX exists, whether it is attached, and the
// volume mounted at spec.Mount when that volume lives on this disk. It needs
// no administrator rights for the virtdisk backend.
func (d *devDrive) Status(ctx context.Context, spec DevDriveSpec) (DevDriveStatus, error) {
	p, err := canonicalVHDX(spec.VHDXPath)
	if err != nil {
		return DevDriveStatus{}, err
	}
	spec.VHDXPath = p
	return d.status(ctx, d.ops.backend(), spec)
}

// status implements Status with an already chosen backend and a canonical
// spec.VHDXPath.
func (d *devDrive) status(ctx context.Context, b diskBackend, spec DevDriveSpec) (DevDriveStatus, error) {
	st := DevDriveStatus{VHDXPath: spec.VHDXPath, Mount: spec.Mount}
	if err := ctx.Err(); err != nil {
		return st, err
	}
	fi, err := os.Stat(spec.VHDXPath)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return st, nil
	case err != nil:
		return st, fmt.Errorf("platform: devdrive: status: %w", err)
	case !fi.Mode().IsRegular():
		return st, fmt.Errorf("platform: devdrive: status: %s is not a regular file", spec.VHDXPath)
	}
	st.Exists = true

	n, attached, err := b.disk(ctx, spec.VHDXPath)
	if err != nil {
		return st, fmt.Errorf("platform: devdrive: status: attached check: %w", err)
	}
	if !attached {
		return st, nil
	}
	st.Attached = true
	if checkMountForm(spec.Mount) != nil {
		return st, nil // PENDING U-PLAT-9: directory mounts are not resolved yet
	}
	vn, mounted, err := d.ops.volumeDisk(ctx, spec.Mount)
	if err != nil {
		return st, fmt.Errorf("platform: devdrive: status: volume at %s: %w", spec.Mount, err)
	}
	if !mounted || vn != n {
		return st, nil // nothing, or another disk's volume, is at the mount
	}
	vi, err := d.host.Volume(ctx, spec.Mount+`\`)
	if err != nil {
		return st, fmt.Errorf("platform: devdrive: status: %w", err)
	}
	st.Volume = &vi
	return st, nil
}
