//go:build windows

package platform

import (
	"context"
	"runtime"

	"golang.org/x/sys/windows"
)

// OS reports the Windows build number and whether the process is elevated.
func (h *host) OS(context.Context) (OSInfo, error) {
	return OSInfo{
		GOOS:     runtime.GOOS,
		Build:    windows.RtlGetVersion().BuildNumber,
		Elevated: windows.GetCurrentProcessToken().IsElevated(),
	}, nil
}
