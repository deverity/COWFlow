//go:build !windows

package platform

import (
	"context"
	"os"
	"runtime"
)

// OS reports the host OS. Build is always 0; Elevated is informational only.
func (h *host) OS(context.Context) (OSInfo, error) {
	return OSInfo{
		GOOS:     runtime.GOOS,
		Build:    0,
		Elevated: os.Geteuid() == 0,
	}, nil
}
