package platform

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// These tests drive the OS-independent §6.4 orchestration with fake
// primitives, so they run on every OS.

// ddHost is a fake Host for the Dev Drive orchestration.
type ddHost struct {
	info   OSInfo
	volume VolumeInfo
	volErr error
}

func (h *ddHost) OS(context.Context) (OSInfo, error) { return h.info, nil }

func (h *ddHost) Volume(_ context.Context, path string) (VolumeInfo, error) {
	if h.volErr != nil {
		return VolumeInfo{}, h.volErr
	}
	v := h.volume
	v.Path = path
	return v, nil
}

func (h *ddHost) ProjFSAvailable(context.Context) (bool, error) { return false, nil }

// ddFake is a fake disk backend plus the other primitives. Disk number 7 is
// the VHDX; "attached" files are tracked by path.
type ddFake struct {
	calls []string

	attached map[string]bool
	mounted  map[string]int // mount -> disk number holding it
	used     map[byte]bool  // drive letters in use

	failCreate      error
	leaveFileOnFail bool
	failAttach      error
	failFormat      error
	failAssign      error
	formatMounts    bool // a successful format mounts disk 7 at the mount
	detachNeedsLive bool // detach fails when its context is done
}

const fakeDisk = 7

func newDDFake() *ddFake {
	return &ddFake{
		attached:     map[string]bool{},
		mounted:      map[string]int{},
		used:         map[byte]bool{'C': true},
		formatMounts: true,
	}
}

func (f *ddFake) ops() devDriveOps {
	return devDriveOps{
		backend: func() diskBackend { return f },
		format: func(_ context.Context, n int, mount string) error {
			f.calls = append(f.calls, fmt.Sprintf("format %d %s", n, mount))
			if f.failFormat != nil {
				return f.failFormat
			}
			if f.formatMounts {
				f.mounted[mount] = n
			}
			return nil
		},
		assignMount: func(_ context.Context, n int, mount string) error {
			f.calls = append(f.calls, fmt.Sprintf("assign %d %s", n, mount))
			if f.failAssign != nil {
				return f.failAssign
			}
			f.mounted[mount] = n
			return nil
		},
		letterFree: func(letter byte) (bool, error) { return !f.used[letter], nil },
		volumeDisk: func(_ context.Context, mount string) (int, bool, error) {
			n, ok := f.mounted[mount]
			return n, ok, nil
		},
	}
}

func (f *ddFake) create(_ context.Context, path string, _ uint64) error {
	f.calls = append(f.calls, "create")
	if f.failCreate != nil {
		if f.leaveFileOnFail {
			if err := os.WriteFile(path, []byte("partial"), 0o600); err != nil {
				return err
			}
		}
		return f.failCreate
	}
	return os.WriteFile(path, []byte("vhdx"), 0o600)
}

func (f *ddFake) attach(_ context.Context, path string) (int, error) {
	f.calls = append(f.calls, "attach")
	if f.failAttach != nil {
		return 0, f.failAttach
	}
	f.attached[path] = true
	return fakeDisk, nil
}

func (f *ddFake) detach(ctx context.Context, path string) error {
	f.calls = append(f.calls, "detach")
	if f.detachNeedsLive {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	delete(f.attached, path)
	for m, n := range f.mounted {
		if n == fakeDisk {
			delete(f.mounted, m)
		}
	}
	return nil
}

func (f *ddFake) disk(_ context.Context, path string) (int, bool, error) {
	if f.attached[path] {
		return fakeDisk, true, nil
	}
	return 0, false, nil
}

func newTestDevDrive(t *testing.T) (*devDrive, *ddFake, *ddHost) {
	t.Helper()
	f := newDDFake()
	h := &ddHost{
		info:   OSInfo{GOOS: "windows", Build: 22631, Elevated: true},
		volume: VolumeInfo{Filesystem: "ReFS", BlockClone: true, DevDrive: true},
	}
	return &devDrive{host: h, ops: f.ops()}, f, h
}

func validSpec(t *testing.T) DevDriveSpec {
	t.Helper()
	p, err := Canonical(filepath.Join(t.TempDir(), "cf", "cowflow.vhdx"))
	require.NoError(t, err)
	return DevDriveSpec{VHDXPath: p, SizeBytes: 50 << 30, Mount: "X:"}
}

func TestProvision_Validation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, h *ddHost, f *ddFake, s *DevDriveSpec)
		want   error
	}{
		{"old build", func(_ *testing.T, h *ddHost, _ *ddFake, _ *DevDriveSpec) { h.info.Build = 19045 }, ErrOSTooOld},
		{"not elevated", func(_ *testing.T, h *ddHost, _ *ddFake, _ *DevDriveSpec) { h.info.Elevated = false }, ErrNotElevated},
		{"10 GB", func(_ *testing.T, _ *ddHost, _ *ddFake, s *DevDriveSpec) { s.SizeBytes = 10 << 30 }, errInvalidSpec},
		{"just below 50 GiB", func(_ *testing.T, _ *ddHost, _ *ddFake, s *DevDriveSpec) { s.SizeBytes = 50<<30 - 1 }, errInvalidSpec},
		{"letter in use", func(_ *testing.T, _ *ddHost, f *ddFake, _ *DevDriveSpec) { f.used['X'] = true }, errInvalidSpec},
		{"existing file", func(t *testing.T, _ *ddHost, _ *ddFake, s *DevDriveSpec) {
			require.NoError(t, os.MkdirAll(filepath.Dir(s.VHDXPath), 0o750))
			require.NoError(t, os.WriteFile(s.VHDXPath, nil, 0o600))
		}, ErrAlreadyExists},
		{"relative path", func(_ *testing.T, _ *ddHost, _ *ddFake, s *DevDriveSpec) { s.VHDXPath = "cowflow.vhdx" }, errInvalidSpec},
		{"empty path", func(_ *testing.T, _ *ddHost, _ *ddFake, s *DevDriveSpec) { s.VHDXPath = "" }, errInvalidSpec},
		{"not .vhdx", func(_ *testing.T, _ *ddHost, _ *ddFake, s *DevDriveSpec) {
			s.VHDXPath = filepath.Join(filepath.Dir(s.VHDXPath), "cowflow.vhd")
		}, errInvalidSpec},
		{"lower-case letter", func(_ *testing.T, _ *ddHost, _ *ddFake, s *DevDriveSpec) { s.Mount = "x:" }, errInvalidSpec},
		{"letter C", func(_ *testing.T, _ *ddHost, _ *ddFake, s *DevDriveSpec) { s.Mount = "C:" }, errInvalidSpec},
		{"letter with slash", func(_ *testing.T, _ *ddHost, _ *ddFake, s *DevDriveSpec) { s.Mount = `X:\` }, errInvalidSpec},
		{"directory mount is open question 8", func(_ *testing.T, _ *ddHost, _ *ddFake, s *DevDriveSpec) { s.Mount = `C:\cf\mnt` }, errInvalidSpec},
		{"injection in mount", func(_ *testing.T, _ *ddHost, _ *ddFake, s *DevDriveSpec) {
			s.Mount = `X:'; Remove-Item C:\ -Recurse #`
		}, errInvalidSpec},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, f, h := newTestDevDrive(t)
			spec := validSpec(t)
			tt.mutate(t, h, f, &spec)
			_, err := d.Provision(context.Background(), spec)
			require.ErrorIs(t, err, tt.want)
			require.NotContains(t, f.calls, "create", "nothing may be created when validation fails")
		})
	}
}

func TestProvision_Success(t *testing.T) {
	d, f, _ := newTestDevDrive(t)
	spec := validSpec(t)

	st, err := d.Provision(context.Background(), spec)
	require.NoError(t, err)
	require.Equal(t, []string{"create", "attach", "format 7 X:"}, f.calls)
	require.True(t, st.Exists)
	require.True(t, st.Attached)
	require.Equal(t, "X:", st.Mount)
	require.NotNil(t, st.Volume)
	require.Equal(t, "ReFS", st.Volume.Filesystem)
	require.Equal(t, `X:\`, st.Volume.Path)
	require.FileExists(t, spec.VHDXPath)
}

// assertCompensated checks that no attached disk and no VHDX are left.
func assertCompensated(t *testing.T, f *ddFake, spec DevDriveSpec) {
	t.Helper()
	require.NoFileExists(t, spec.VHDXPath)
	require.Empty(t, f.attached)
	require.Empty(t, f.mounted)
}

func TestProvision_CompensatesOnFormatFailure(t *testing.T) {
	d, f, _ := newTestDevDrive(t)
	cause := errors.New("Format-Volume: the volume is too small")
	f.failFormat = cause
	spec := validSpec(t)

	_, err := d.Provision(context.Background(), spec)
	require.ErrorIs(t, err, cause)
	require.ErrorContains(t, err, "partition and format")
	require.Contains(t, f.calls, "detach")
	assertCompensated(t, f, spec)
}

func TestProvision_CompensatesOnAttachFailure(t *testing.T) {
	d, f, _ := newTestDevDrive(t)
	cause := errors.New("AttachVirtualDisk: access denied")
	f.failAttach = cause
	spec := validSpec(t)

	_, err := d.Provision(context.Background(), spec)
	require.ErrorIs(t, err, cause)
	require.ErrorContains(t, err, "attach")
	require.NotContains(t, f.calls, "detach", "a disk that never attached is not detached")
	assertCompensated(t, f, spec)
}

func TestProvision_CompensatesPartialCreate(t *testing.T) {
	d, f, _ := newTestDevDrive(t)
	cause := errors.New("CreateVirtualDisk: disk full")
	f.failCreate = cause
	f.leaveFileOnFail = true
	spec := validSpec(t)

	_, err := d.Provision(context.Background(), spec)
	require.ErrorIs(t, err, cause)
	require.ErrorContains(t, err, "create")
	assertCompensated(t, f, spec)
}

func TestProvision_NeverSucceedsOnNTFS(t *testing.T) {
	d, f, h := newTestDevDrive(t)
	h.volume = VolumeInfo{Filesystem: "NTFS"}
	spec := validSpec(t)

	_, err := d.Provision(context.Background(), spec)
	require.ErrorContains(t, err, `"NTFS", not ReFS`)
	require.ErrorContains(t, err, "verify")
	assertCompensated(t, f, spec)
}

func TestProvision_NotMountedAfterFormat(t *testing.T) {
	d, f, _ := newTestDevDrive(t)
	f.formatMounts = false
	spec := validSpec(t)

	_, err := d.Provision(context.Background(), spec)
	require.ErrorIs(t, err, ErrNotMounted)
	assertCompensated(t, f, spec)
}

func TestProvision_VolumeOfAnotherDiskIsNotOurs(t *testing.T) {
	d, f, _ := newTestDevDrive(t)
	f.formatMounts = false
	spec := validSpec(t)
	f.mounted["X:"] = 2 // another disk took the letter after validation

	_, err := d.Provision(context.Background(), spec)
	require.ErrorIs(t, err, ErrNotMounted)
	require.NoFileExists(t, spec.VHDXPath)
	require.Empty(t, f.attached)
}

func TestProvision_CompensatesAfterCancel(t *testing.T) {
	d, f, _ := newTestDevDrive(t)
	f.detachNeedsLive = true
	ctx, cancel := context.WithCancel(context.Background())
	d.ops.format = func(context.Context, int, string) error {
		cancel()
		return context.Canceled
	}
	spec := validSpec(t)

	_, err := d.Provision(ctx, spec)
	require.ErrorIs(t, err, context.Canceled)
	require.Contains(t, f.calls, "detach")
	require.NotContains(t, err.Error(), "compensation failed")
	require.NoFileExists(t, spec.VHDXPath)
	require.Empty(t, f.attached)
}

func TestProvision_ReportsCompensationFailure(t *testing.T) {
	d, f, _ := newTestDevDrive(t)
	cause := errors.New("format failed")
	f.failFormat = cause
	detachErr := errors.New("detach failed")
	b := &failingDetach{ddFake: f, err: detachErr}
	d.ops.backend = func() diskBackend { return b }
	spec := validSpec(t)

	_, err := d.Provision(context.Background(), spec)
	require.ErrorIs(t, err, cause)
	require.ErrorIs(t, err, detachErr)
	require.ErrorContains(t, err, "compensation failed")
}

type failingDetach struct {
	*ddFake
	err error
}

func (b *failingDetach) detach(context.Context, string) error { return b.err }

func TestAttach(t *testing.T) {
	provisioned := func(t *testing.T) (*devDrive, *ddFake, DevDriveSpec) {
		t.Helper()
		d, f, _ := newTestDevDrive(t)
		spec := validSpec(t)
		_, err := d.Provision(context.Background(), spec)
		require.NoError(t, err)
		require.NoError(t, d.Detach(context.Background(), spec.VHDXPath))
		f.calls = nil
		return d, f, spec
	}

	t.Run("letter restored by Windows", func(t *testing.T) {
		d, f, spec := provisioned(t)
		d.ops.backend = func() diskBackend { return &restoringAttach{ddFake: f, mount: spec.Mount} }
		st, err := d.Attach(context.Background(), spec)
		require.NoError(t, err)
		require.Equal(t, []string{"attach"}, f.calls)
		require.NotNil(t, st.Volume)
	})
	t.Run("letter assigned", func(t *testing.T) {
		d, f, spec := provisioned(t)
		st, err := d.Attach(context.Background(), spec)
		require.NoError(t, err)
		require.Equal(t, []string{"attach", "assign 7 X:"}, f.calls)
		require.True(t, st.Attached)
		require.NotNil(t, st.Volume)
	})
	t.Run("already attached is idempotent", func(t *testing.T) {
		d, f, spec := provisioned(t)
		_, err := d.Attach(context.Background(), spec)
		require.NoError(t, err)
		f.calls = nil
		_, err = d.Attach(context.Background(), spec)
		require.NoError(t, err)
		require.Empty(t, f.calls)
	})
	t.Run("letter in use", func(t *testing.T) {
		d, f, spec := provisioned(t)
		f.used['X'] = true
		_, err := d.Attach(context.Background(), spec)
		require.ErrorIs(t, err, errInvalidSpec)
		require.NotContains(t, f.calls, "assign 7 X:")
	})
	t.Run("another volume at the letter", func(t *testing.T) {
		d, f, spec := provisioned(t)
		f.mounted["X:"] = 2
		_, err := d.Attach(context.Background(), spec)
		require.ErrorContains(t, err, "holds another volume")
	})
	t.Run("missing file", func(t *testing.T) {
		d, _, _ := newTestDevDrive(t)
		_, err := d.Attach(context.Background(), validSpec(t))
		require.ErrorIs(t, err, fs.ErrNotExist)
	})
	t.Run("not elevated", func(t *testing.T) {
		d, _, h := newTestDevDrive(t)
		h.info.Elevated = false
		_, err := d.Attach(context.Background(), validSpec(t))
		require.ErrorIs(t, err, ErrNotElevated)
	})
}

// restoringAttach simulates Windows remembering the drive letter.
type restoringAttach struct {
	*ddFake
	mount string
}

func (b *restoringAttach) attach(ctx context.Context, path string) (int, error) {
	n, err := b.ddFake.attach(ctx, path)
	if err == nil {
		b.mounted[b.mount] = n
	}
	return n, err
}

func TestDetach(t *testing.T) {
	d, f, _ := newTestDevDrive(t)
	spec := validSpec(t)

	require.ErrorIs(t, d.Detach(context.Background(), spec.VHDXPath), fs.ErrNotExist)

	_, err := d.Provision(context.Background(), spec)
	require.NoError(t, err)
	f.calls = nil
	require.NoError(t, d.Detach(context.Background(), spec.VHDXPath))
	require.Equal(t, []string{"detach"}, f.calls)
	require.Empty(t, f.attached)

	f.calls = nil
	require.NoError(t, d.Detach(context.Background(), spec.VHDXPath), "detached disk is a no-op")
	require.Empty(t, f.calls)
}

func TestStatus(t *testing.T) {
	d, f, _ := newTestDevDrive(t)
	spec := validSpec(t)

	st, err := d.Status(context.Background(), spec)
	require.NoError(t, err)
	require.Equal(t, DevDriveStatus{VHDXPath: spec.VHDXPath, Mount: "X:"}, st)

	_, err = d.Provision(context.Background(), spec)
	require.NoError(t, err)
	require.NoError(t, d.Detach(context.Background(), spec.VHDXPath))
	st, err = d.Status(context.Background(), spec)
	require.NoError(t, err)
	require.True(t, st.Exists)
	require.False(t, st.Attached)
	require.Nil(t, st.Volume)

	// Attached, but the letter belongs to another disk's volume.
	f.attached[spec.VHDXPath] = true
	f.mounted["X:"] = 2
	st, err = d.Status(context.Background(), spec)
	require.NoError(t, err)
	require.True(t, st.Attached)
	require.Nil(t, st.Volume)

	_, err = d.Status(context.Background(), DevDriveSpec{VHDXPath: "relative.vhdx", Mount: "X:"})
	require.ErrorIs(t, err, errInvalidSpec)
}

// TestCheckMountForm_NamesOpenQuestion: v1 mounts on a drive letter only,
// and the rejection of any other form points at SCOPE.md §23 open question 8.
func TestCheckMountForm_NamesOpenQuestion(t *testing.T) {
	require.NoError(t, checkMountForm("X:"))
	err := checkMountForm(`C:\cf\mnt`)
	require.ErrorIs(t, err, errInvalidSpec)
	require.ErrorContains(t, err, "§23 open question 8")
	require.NotContains(t, err.Error(), "PENDING")
}
