//go:build !windows

package workspace

import (
	"context"
	"fmt"
	"runtime"
)

// cloneFile has no block-clone primitive on this OS yet and never copies
// bytes: it reports ErrCloneUnsupported, so auto selection falls back to
// git_worktree. S1-T08 adds FICLONE (Linux) and clonefile (macOS) and narrows
// this file to the remaining systems.
func cloneFile(_ context.Context, _, _ string) error {
	return fmt.Errorf("%w: %s", ErrCloneUnsupported, runtime.GOOS)
}
