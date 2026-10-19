package platform

import (
	"context"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAutoAttacher_Undecided(t *testing.T) {
	a := undecidedAutoAttacher{}
	err := a.Enable(context.Background(), DevDriveSpec{})
	require.ErrorIs(t, err, ErrAutoAttachUndecided)
	require.Contains(t, err.Error(), "U-PLAT-5")
	require.NoError(t, a.Disable(context.Background(), `X:\dev.vhdx`))
}

func TestAutoAttacher_Unsupported(t *testing.T) {
	a := unsupportedAutoAttacher{}
	require.ErrorIs(t, a.Enable(context.Background(), DevDriveSpec{}), ErrUnsupported)
	require.ErrorIs(t, a.Disable(context.Background(), "x"), ErrUnsupported)
}

func TestNewAutoAttacher_PerOS(t *testing.T) {
	err := NewAutoAttacher().Enable(context.Background(), DevDriveSpec{})
	if runtime.GOOS == "windows" {
		require.ErrorIs(t, err, ErrAutoAttachUndecided)
	} else {
		require.ErrorIs(t, err, ErrUnsupported)
	}
}
