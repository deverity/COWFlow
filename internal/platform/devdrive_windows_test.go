package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

// TestVolumeDiskExtentsLayout checks the VOLUME_DISK_EXTENTS mirror with one
// DISK_EXTENT against winioctl.h on 64-bit Windows.
func TestVolumeDiskExtentsLayout(t *testing.T) {
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skipf("layout is specified for amd64 and arm64, not %s", runtime.GOARCH)
	}
	var e volumeDiskExtents
	require.EqualValues(t, 32, unsafe.Sizeof(e))
	require.EqualValues(t, 8, unsafe.Offsetof(e.DiskNumber))
	require.EqualValues(t, 16, unsafe.Offsetof(e.StartingOffset))
	require.EqualValues(t, 24, unsafe.Offsetof(e.ExtentLength))
}

func TestLetterFree(t *testing.T) {
	free, err := letterFree('C')
	require.NoError(t, err)
	require.False(t, free, "the system drive is in use")

	_, err = letterFree('1')
	require.Error(t, err)
}

func TestVolumeDisk_FreeLetter(t *testing.T) {
	for l := byte('Z'); l >= 'D'; l-- {
		free, err := letterFree(l)
		require.NoError(t, err)
		if !free {
			continue
		}
		_, mounted, err := volumeDisk(context.Background(), string(l)+":")
		require.NoError(t, err)
		require.False(t, mounted, "free letter %c reported mounted", l)
		return
	}
	t.Skip("no free drive letter")
}

func TestVolumeDisk_RejectsOtherForms(t *testing.T) {
	for _, m := range []string{"C:", `X:\`, `C:\cf\mnt`, ""} {
		_, _, err := volumeDisk(context.Background(), m)
		require.Error(t, err, m)
	}
}

// TestProvision_Real provisions a real 50 GiB dynamic Dev Drive. It needs
// an elevated Windows 11 22H2+ machine and COWFLOW_TEST_DEVDRIVE=1, and is
// run manually; its output goes into the PR as evidence.
func TestProvision_Real(t *testing.T) {
	if os.Getenv("COWFLOW_TEST_DEVDRIVE") != "1" {
		t.Skip("set COWFLOW_TEST_DEVDRIVE=1 on an elevated Windows 11 22H2+ machine")
	}
	ctx := context.Background()
	h := NewHost()
	info, err := h.OS(ctx)
	require.NoError(t, err)
	if !info.Elevated || info.Build < minDevDriveBuild {
		t.Skipf("needs an elevated Windows 11 22H2+ (build %d, elevated %v)", info.Build, info.Elevated)
	}

	var letter byte
	for l := byte('Z'); l >= 'D'; l-- {
		if free, err := letterFree(l); err == nil && free {
			letter = l
			break
		}
	}
	if letter == 0 {
		t.Skip("no free drive letter")
	}
	spec := DevDriveSpec{
		VHDXPath:  filepath.Join(t.TempDir(), "cowflow-test.vhdx"),
		SizeBytes: minDevDriveSize,
		Mount:     fmt.Sprintf("%c:", letter),
	}
	dd := NewDevDrive(h)
	t.Cleanup(func() {
		if err := dd.Detach(context.Background(), spec.VHDXPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Logf("cleanup detach: %v", err)
		}
		if err := os.Remove(spec.VHDXPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Logf("cleanup remove: %v", err)
		}
	})

	st, err := dd.Provision(ctx, spec)
	require.NoError(t, err)
	t.Logf("provisioned: %+v volume=%+v", st, *st.Volume)
	require.True(t, st.Exists)
	require.True(t, st.Attached)
	require.Equal(t, "ReFS", st.Volume.Filesystem)
	require.True(t, st.Volume.BlockClone)
	require.True(t, st.Volume.DevDrive)

	require.NoError(t, dd.Detach(ctx, spec.VHDXPath))
	st, err = dd.Status(ctx, spec)
	require.NoError(t, err)
	t.Logf("after detach: %+v", st)
	require.False(t, st.Attached)
	require.Nil(t, st.Volume)
}
