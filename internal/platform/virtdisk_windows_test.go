//go:build windows

package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

// TestStructLayout checks the Go mirrors against the C layouts declared in
// the Windows SDK's virtdisk.h, computed with the natural x64/ARM64
// alignment (8-byte pointers and ULONGLONG, 4-byte GUID):
//
//   - VIRTUAL_STORAGE_TYPE: ULONG + GUID = 20 bytes.
//   - CREATE_VIRTUAL_DISK_PARAMETERS: Version (4) + 4 bytes padding, then the
//     union at offset 8. Version2 is 120 bytes (MaximumSize at 16, ParentPath
//     at 40 after 4 bytes padding, ResiliencyGuid at 100, 4 bytes tail
//     padding), so sizeof == 128 for the union of Version1 and Version2 as
//     declared up to the Windows 8.1 SDK. Windows 10 SDKs add Version3/4
//     members that enlarge the union; the API reads only the member selected
//     by Version, so passing the 128-byte Version2 form is valid.
//   - OPEN_VIRTUAL_DISK_PARAMETERS: Version (4) + Version2 (BOOL, BOOL,
//     GUID) at offset 4 = 28 bytes.
//   - ATTACH_VIRTUAL_DISK_PARAMETERS: Version (4) + Version1 Reserved (4) =
//     8 bytes.
func TestStructLayout(t *testing.T) {
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skipf("layouts are specified for amd64 and arm64, not %s", runtime.GOARCH)
	}

	var st virtualStorageType
	require.EqualValues(t, 20, unsafe.Sizeof(st))
	require.EqualValues(t, 4, unsafe.Offsetof(st.VendorID))

	var c createVirtualDiskParameters
	require.EqualValues(t, 8, unsafe.Offsetof(c.V2), "padding after Version")
	require.EqualValues(t, 128, unsafe.Sizeof(c))
	var v2 createVirtualDiskV2
	require.EqualValues(t, 120, unsafe.Sizeof(v2))
	require.EqualValues(t, 0, unsafe.Offsetof(v2.UniqueID))
	require.EqualValues(t, 16, unsafe.Offsetof(v2.MaximumSize))
	require.EqualValues(t, 24, unsafe.Offsetof(v2.BlockSizeInBytes))
	require.EqualValues(t, 28, unsafe.Offsetof(v2.SectorSizeInBytes))
	require.EqualValues(t, 32, unsafe.Offsetof(v2.PhysicalSectorSizeInBytes))
	require.EqualValues(t, 40, unsafe.Offsetof(v2.ParentPath))
	require.EqualValues(t, 48, unsafe.Offsetof(v2.SourcePath))
	require.EqualValues(t, 56, unsafe.Offsetof(v2.OpenFlags))
	require.EqualValues(t, 60, unsafe.Offsetof(v2.ParentVirtualStorageType))
	require.EqualValues(t, 80, unsafe.Offsetof(v2.SourceVirtualStorageType))
	require.EqualValues(t, 100, unsafe.Offsetof(v2.ResiliencyGUID))

	var o openVirtualDiskParameters
	require.EqualValues(t, 4, unsafe.Offsetof(o.V2))
	require.EqualValues(t, 28, unsafe.Sizeof(o))
	require.EqualValues(t, 8, unsafe.Offsetof(o.V2.ResiliencyGUID))

	var a attachVirtualDiskParameters
	require.EqualValues(t, 8, unsafe.Sizeof(a))
	require.EqualValues(t, 4, unsafe.Offsetof(a.Reserved))
}

func TestVendorMicrosoftGUID(t *testing.T) {
	require.Equal(t, "{EC984AEC-A0F9-47E9-901F-71415A66345B}", vendorMicrosoft.String())
}

func TestVirtdiskAvailable(t *testing.T) {
	// virtdisk.dll ships with every Windows 10+ edition.
	require.NoError(t, virtdiskAvailable())
}

func TestParsePhysicalDrive(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
	}{
		{`\\.\PhysicalDrive0`, 0},
		{`\\.\PhysicalDrive3`, 3},
		{`\\.\PhysicalDrive12`, 12},
	} {
		got, err := parsePhysicalDrive(tc.in)
		require.NoError(t, err, tc.in)
		require.Equal(t, tc.want, got, tc.in)
	}
	for _, bad := range []string{
		"",
		`\\.\PhysicalDrive`,
		`\\.\PhysicalDrive-1`,
		`\\.\PhysicalDrive+1`,
		`\\.\PhysicalDrive 1`,
		`\\.\PhysicalDrive1x`,
		`\\.\CdRom0`,
		`PhysicalDrive1`,
		`\\.\PhysicalDrive99999999999999999999999`,
	} {
		_, err := parsePhysicalDrive(bad)
		require.Error(t, err, bad)
	}
}

// TestCreateAttachDetach_Real exercises the real API. It needs an elevated
// process and COWFLOW_TEST_VHDX=1, because it attaches a disk to the host.
func TestCreateAttachDetach_Real(t *testing.T) {
	if os.Getenv("COWFLOW_TEST_VHDX") != "1" {
		t.Skip("set COWFLOW_TEST_VHDX=1 to create and attach a real VHDX")
	}
	if !windows.GetCurrentProcessToken().IsElevated() {
		t.Skip("attaching a VHDX requires an elevated (administrator) process")
	}
	require.NoError(t, virtdiskAvailable())

	path := filepath.Join(t.TempDir(), "test.vhdx")
	h, err := createVHDX(path, 3<<30)
	require.NoError(t, err)
	require.NoError(t, windows.CloseHandle(h))
	t.Cleanup(func() { _ = os.Remove(path) })

	_, err = createVHDX(path, 3<<30)
	require.Error(t, err, "creating over an existing file must fail")

	h, err = openVHDX(path)
	require.NoError(t, err)
	defer func() { _ = windows.CloseHandle(h) }()

	require.NoError(t, attach(h))
	attached := true
	defer func() {
		if attached {
			_ = detach(h)
		}
	}()

	n, err := physicalPath(h)
	require.NoError(t, err)
	require.GreaterOrEqual(t, n, 0)
	t.Logf("attached as disk %d", n)

	require.NoError(t, detach(h))
	attached = false
}
