//go:build windows

package platform

import (
	"context"

	"golang.org/x/sys/windows"
)

// ProjFSAvailable reports whether the Client-ProjFS optional feature is
// enabled. ProjectedFSLib.dll ships with the feature, so loading it from
// System32 succeeds exactly when the feature is on. This needs no
// administrator rights, unlike Get-WindowsOptionalFeature. NewLazySystemDLL
// restricts the search to System32, so a planted DLL is never loaded.
func (h *host) ProjFSAvailable(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return windows.NewLazySystemDLL("ProjectedFSLib.dll").Load() == nil, nil
}
