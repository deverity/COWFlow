//go:build !windows

package platform

// newAutoAttacher returns an AutoAttacher that reports ErrUnsupported: Dev
// Drive auto-attach is a Windows Task Scheduler task (SCOPE.md §6.4).
func newAutoAttacher(AutoAttachTarget) AutoAttacher { return unsupportedAutoAttacher{} }
