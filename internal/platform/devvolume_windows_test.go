//go:build windows

package platform

import (
	"context"
	"os"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func TestDevVolume_SystemDrive(t *testing.T) {
	vi, err := NewHost().Volume(context.Background(), `C:\`)
	require.NoError(t, err)
	require.False(t, vi.DevDrive)
}

func TestDevVolume_DevDrive(t *testing.T) {
	p := os.Getenv("COWFLOW_TEST_REFS_PATH")
	if p == "" {
		t.Skip("set COWFLOW_TEST_REFS_PATH to a Dev Drive")
	}
	vi, err := NewHost().Volume(context.Background(), p)
	require.NoError(t, err)
	require.True(t, vi.DevDrive)
}

func TestProjFSAvailable(t *testing.T) {
	ok, err := NewHost().ProjFSAvailable(context.Background())
	require.NoError(t, err)
	t.Logf("ProjFS available: %v", ok)
}

func TestPersistentVolumeInfoLayout(t *testing.T) {
	require.EqualValues(t, 16, unsafe.Sizeof(filePersistentVolumeInformation{}))
}
