package platform

import (
	"context"
	"errors"
	"os"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVolume_TempDir(t *testing.T) {
	vi, err := NewHost().Volume(context.Background(), t.TempDir())
	if errors.Is(err, ErrUnsupported) {
		t.Skip("unsupported OS")
	}
	require.NoError(t, err)
	require.NotEmpty(t, vi.Filesystem)
	require.NotEmpty(t, vi.Path)
}

func TestVolume_WindowsSystemDrive(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}
	vi, err := NewHost().Volume(context.Background(), `C:\`)
	require.NoError(t, err)
	require.Equal(t, "NTFS", vi.Filesystem)
	require.False(t, vi.BlockClone)
}

func TestVolume_ReFS(t *testing.T) {
	p := os.Getenv("COWFLOW_TEST_REFS_PATH")
	if p == "" {
		t.Skip("set COWFLOW_TEST_REFS_PATH to a ReFS volume")
	}
	vi, err := NewHost().Volume(context.Background(), p)
	require.NoError(t, err)
	require.Equal(t, "ReFS", vi.Filesystem)
	require.True(t, vi.BlockClone)
}

func TestVolume_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewHost().Volume(ctx, t.TempDir())
	if errors.Is(err, ErrUnsupported) {
		t.Skip("unsupported OS")
	}
	require.ErrorIs(t, err, context.Canceled)
}

func TestOSInfo(t *testing.T) {
	info, err := NewHost().OS(context.Background())
	require.NoError(t, err)
	require.Equal(t, runtime.GOOS, info.GOOS)
	if runtime.GOOS == "windows" {
		require.Positive(t, info.Build)
	} else {
		require.Zero(t, info.Build)
	}
}

func TestDevDrive_Unsupported(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has a real DevDrive (S0-T16)")
	}
	d := NewDevDrive(NewHost())
	_, err := d.Provision(context.Background(), DevDriveSpec{})
	require.ErrorIs(t, err, ErrUnsupported)
	require.ErrorIs(t, d.Detach(context.Background(), ""), ErrUnsupported)
}
