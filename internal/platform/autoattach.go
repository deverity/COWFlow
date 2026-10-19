package platform

import (
	"context"
	"errors"
	"runtime"
)

// ErrAutoAttachUndecided is returned by AutoAttacher.Enable until the
// auto-attach mechanism (scheduled task or service, SCOPE.md §6.4 step 5) is
// decided. Callers report it as a warning: auto-attach is not configured and
// the user must run `cowflow devdrive attach` after a reboot.
var ErrAutoAttachUndecided = errors.New(
	"auto-attach mechanism is not decided (SPEC-GAPS U-PLAT-5); run `cowflow devdrive attach` after reboot")

// NewAutoAttacher returns the AutoAttacher for this OS. PENDING U-PLAT-5: on
// Windows it is a no-op whose Enable reports ErrAutoAttachUndecided; the real
// mechanism lands only after the maintainer decides. Other OSes report
// ErrUnsupported, because Dev Drive is Windows-only.
func NewAutoAttacher() AutoAttacher {
	if runtime.GOOS == "windows" {
		return undecidedAutoAttacher{}
	}
	return unsupportedAutoAttacher{}
}

// undecidedAutoAttacher is the PENDING U-PLAT-5 placeholder.
type undecidedAutoAttacher struct{}

func (undecidedAutoAttacher) Enable(context.Context, DevDriveSpec) error {
	return ErrAutoAttachUndecided
}

// Disable has nothing to remove: nothing was ever registered.
func (undecidedAutoAttacher) Disable(context.Context, string) error { return nil }

// unsupportedAutoAttacher is the AutoAttacher of every OS but Windows.
type unsupportedAutoAttacher struct{}

func (unsupportedAutoAttacher) Enable(context.Context, DevDriveSpec) error { return ErrUnsupported }

func (unsupportedAutoAttacher) Disable(context.Context, string) error { return ErrUnsupported }
