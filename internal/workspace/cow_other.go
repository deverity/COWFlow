//go:build !windows && !linux && !darwin

package workspace

import (
	"context"
	"fmt"
	"runtime"
)

// cloneFile has no block-clone primitive on this OS and never copies bytes:
// it reports ErrCloneUnsupported, so auto selection falls back to
// git_worktree. Windows, Linux (FICLONE) and macOS (clonefile) have their own
// cow_<goos>.go; this file covers the remaining systems.
func cloneFile(_ context.Context, _, _ string) error {
	return fmt.Errorf("%w: %s", ErrCloneUnsupported, runtime.GOOS)
}
