//go:build !windows

package platform

import "context"

// RunElevated is Windows-only (UAC); elsewhere it returns ErrUnsupported.
func RunElevated(context.Context, string, []string, string) (int, error) {
	return -1, ErrUnsupported
}
