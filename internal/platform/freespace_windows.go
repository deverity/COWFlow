//go:build windows

package platform

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// freeBytes returns the free bytes available to the caller (quota aware), as
// reported by GetDiskFreeSpaceEx.
func freeBytes(path string) (uint64, error) {
	p16, err := windows.UTF16PtrFromString(LongPath(path))
	if err != nil {
		return 0, err
	}
	var avail, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(p16, &avail, &total, &free); err != nil {
		return 0, fmt.Errorf("free space of %q: %w", path, err)
	}
	return avail, nil
}
