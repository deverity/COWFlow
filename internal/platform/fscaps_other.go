//go:build !windows && !linux && !darwin

package platform

import "context"

// Volume is not supported on this OS.
func (h *host) Volume(context.Context, string) (VolumeInfo, error) {
	return VolumeInfo{}, ErrUnsupported
}
