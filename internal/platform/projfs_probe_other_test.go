//go:build !windows

package platform

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProjFSAvailable_NotWindows(t *testing.T) {
	ok, err := NewHost().ProjFSAvailable(context.Background())
	require.NoError(t, err)
	require.False(t, ok)
}

func TestProjFSAvailable_CanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewHost().ProjFSAvailable(ctx)
	require.ErrorIs(t, err, context.Canceled)
}
