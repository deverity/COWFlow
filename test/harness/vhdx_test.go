package harness

import (
	"context"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/platform"
)

func testSpec() platform.DevDriveSpec {
	return platform.DevDriveSpec{
		VHDXPath:  `C:\cf\cowflow.vhdx`,
		SizeBytes: 50 << 30,
		Mount:     "X:",
	}
}

func TestSimDevDrive_ProvisionThenStatus(t *testing.T) {
	ctx := context.Background()
	host := &FakeHost{}
	sim := &SimDevDrive{Host: host}
	spec := testSpec()

	st, err := sim.Provision(ctx, spec)
	require.NoError(t, err)
	assert.True(t, st.Exists)
	assert.True(t, st.Attached)
	require.NotNil(t, st.Volume)
	assert.Equal(t, "ReFS", st.Volume.Filesystem)
	assert.True(t, st.Volume.BlockClone)
	assert.True(t, st.Volume.DevDrive)

	got, err := sim.Status(ctx, spec)
	require.NoError(t, err)
	assert.Equal(t, st, got)

	vol, err := host.Volume(ctx, `X:\repo\src`)
	require.NoError(t, err)
	assert.True(t, vol.DevDrive)

	_, err = sim.Provision(ctx, spec)
	assert.ErrorIs(t, err, platform.ErrAlreadyExists)
}

func TestSimDevDrive_FailAtEachStepCompensates(t *testing.T) {
	steps := []SimStep{StepCreate, StepAttach, StepFormat, StepMount}
	for _, step := range steps {
		t.Run(string(step), func(t *testing.T) {
			ctx := context.Background()
			host := &FakeHost{}
			sim := &SimDevDrive{Host: host, FailAt: step}
			spec := testSpec()

			_, err := sim.Provision(ctx, spec)
			require.ErrorIs(t, err, ErrInjected)

			st, err := sim.Status(ctx, spec)
			require.NoError(t, err)
			assert.False(t, st.Exists)
			assert.False(t, st.Attached)
			assert.Nil(t, st.Volume)
			assert.Empty(t, host.Volumes)

			// The failure leaves no residue: a clean retry works.
			sim.FailAt = ""
			_, err = sim.Provision(ctx, spec)
			require.NoError(t, err)
		})
	}
}

func TestSimDevDrive_FailAtFormatCompensates(t *testing.T) {
	ctx := context.Background()
	sim := &SimDevDrive{Host: &FakeHost{}, FailAt: StepFormat}

	_, err := sim.Provision(ctx, testSpec())
	require.ErrorIs(t, err, ErrInjected)
	assert.Equal(t,
		[]string{"provision:create", "provision:attach", "provision:format"},
		sim.Calls)

	st, err := sim.Status(ctx, testSpec())
	require.NoError(t, err)
	assert.False(t, st.Exists)
}

func TestSimDevDrive_RebootUnmounts(t *testing.T) {
	ctx := context.Background()
	host := &FakeHost{}
	sim := &SimDevDrive{Host: host}
	spec := testSpec()
	_, err := sim.Provision(ctx, spec)
	require.NoError(t, err)

	sim.SimulateReboot()

	st, err := sim.Status(ctx, spec)
	require.NoError(t, err)
	assert.True(t, st.Exists)
	assert.False(t, st.Attached)
	assert.Nil(t, st.Volume) // never an NTFS volume in its place

	_, err = host.Volume(ctx, `X:\repo`)
	assert.ErrorIs(t, err, platform.ErrNotMounted)

	st, err = sim.Attach(ctx, spec)
	require.NoError(t, err)
	assert.True(t, st.Attached)
	require.NotNil(t, st.Volume)
	assert.True(t, st.Volume.DevDrive)
}

func TestSimDevDrive_AttachFailureLeavesDetached(t *testing.T) {
	ctx := context.Background()
	sim := &SimDevDrive{Host: &FakeHost{}}
	spec := testSpec()
	_, err := sim.Provision(ctx, spec)
	require.NoError(t, err)
	require.NoError(t, sim.Detach(ctx, spec.VHDXPath))

	sim.FailAt = StepMount
	_, err = sim.Attach(ctx, spec)
	require.ErrorIs(t, err, ErrInjected)

	st, err := sim.Status(ctx, spec)
	require.NoError(t, err)
	assert.True(t, st.Exists)
	assert.False(t, st.Attached)
	assert.Nil(t, st.Volume)
}

func TestSimDevDrive_UnknownDisk(t *testing.T) {
	ctx := context.Background()
	sim := &SimDevDrive{}
	spec := testSpec()

	assert.ErrorIs(t, sim.Detach(ctx, spec.VHDXPath), fs.ErrNotExist)
	_, err := sim.Attach(ctx, spec)
	assert.ErrorIs(t, err, fs.ErrNotExist)
	st, err := sim.Status(ctx, spec)
	require.NoError(t, err)
	assert.False(t, st.Exists)
}

func TestSimDevDrive_NilHostAndCanceledContext(t *testing.T) {
	sim := &SimDevDrive{}
	_, err := sim.Provision(context.Background(), testSpec())
	require.NoError(t, err)
	require.NotNil(t, sim.Host)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = sim.Status(ctx, testSpec())
	assert.ErrorIs(t, err, context.Canceled)
}

func TestFakeHost_VolumeLongestPrefix(t *testing.T) {
	ctx := context.Background()
	h := &FakeHost{Volumes: map[string]platform.VolumeInfo{
		`C:\`:        {Path: `C:\`, Filesystem: "NTFS"},
		`C:\cf`:      {Path: `C:\cf`, Filesystem: "ReFS", DevDrive: true},
		`C:\cf\deep`: {Path: `C:\cf\deep`, Filesystem: "ReFS"},
	}}

	tests := []struct {
		path string
		want string
	}{
		{`C:\Users\me`, `C:\`},
		{`C:\cf`, `C:\cf`},
		{`c:/CF/proj/x`, `C:\cf`},
		{`C:\cf\deep\x`, `C:\cf\deep`},
		{`C:\cfx\proj`, `C:\`}, // whole path elements only
	}
	for _, tc := range tests {
		got, err := h.Volume(ctx, tc.path)
		require.NoError(t, err, tc.path)
		assert.Equal(t, tc.want, got.Path, tc.path)
	}

	_, err := h.Volume(ctx, `D:\data`)
	assert.ErrorIs(t, err, platform.ErrNotMounted)
}

func TestFakeHost_OSAndProjFS(t *testing.T) {
	ctx := context.Background()
	h := &FakeHost{OSInfo: platform.OSInfo{GOOS: "windows", Build: 22631, Elevated: true}, ProjFS: true}

	info, err := h.OS(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint32(22631), info.Build)

	ok, err := h.ProjFSAvailable(ctx)
	require.NoError(t, err)
	assert.True(t, ok)
}
