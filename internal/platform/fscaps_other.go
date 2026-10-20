//go:build !windows && !linux && !darwin

package platform

import "context"

// Volume is not supported on this OS.
func (h *host) Volume(context.Context, string) (VolumeInfo, error) {
	return VolumeInfo{}, ErrUnsupported
}

func volumeOf(context.Context, string) (Volume, error) {
	return Volume{Filesystem: "unknown"}, ErrUnsupported
}

func freeBytes(string) (uint64, error) { return 0, ErrUnsupported }
