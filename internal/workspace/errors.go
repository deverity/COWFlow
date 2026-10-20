package workspace

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Sentinel errors callers branch on with errors.Is. The CLI maps them to
// exit codes (SCOPE.md §14).
var (
	ErrNotFound            = errors.New("workspace: not found")
	ErrPendingMount        = errors.New("workspace: volume not mounted")      // state pending_mount
	ErrPendingCleanup      = errors.New("workspace: cleanup pending")         // state pending_cleanup
	ErrProtectedBranch     = errors.New("workspace: protected branch")        // main/develop (§24.2)
	ErrCloneUnsupported    = errors.New("workspace: block clone unsupported") // no block clone on this volume
	ErrUnsupportedProvider = errors.New("workspace: provider not available")  // projfs, container

	// ErrRootNotSet: [workspace].root is empty (exit 2, SCOPE.md §22).
	ErrRootNotSet = errors.New("workspace: workspace.root is not set (set workspace.root or run cowflow init --devdrive)")
	// ErrPathExists: <workspace.root>/<slug> exists in some state (exit 3, §6.1).
	ErrPathExists = errors.New("workspace: workspace path exists: destroy it first")
	// ErrBranchExists: the task branch exists before its workspace (exit 3, §14).
	ErrBranchExists = errors.New("workspace: branch exists")
)

// maxListed is how many paths or commits an UnpreservedError message names
// before it summarizes the rest.
const maxListed = 10

// UnpreservedError refuses a destroy without --force because the workspace
// holds changes that would be lost (SCOPE.md §17, §20.12). Report carries
// the full lists; Error names at most ten entries of each.
type UnpreservedError struct{ Report DirtyReport }

// Error lists the uncommitted paths and unpublished commits, quoted, at most
// ten of each, followed by "… and N more".
func (e *UnpreservedError) Error() string {
	var b strings.Builder
	b.WriteString("workspace: unpreserved changes")
	writeList(&b, "uncommitted file", e.Report.Uncommitted)
	writeList(&b, "unpublished commit", e.Report.Unpublished)
	return b.String()
}

func writeList(b *strings.Builder, noun string, items []string) {
	if len(items) == 0 {
		return
	}
	plural := "s"
	if len(items) == 1 {
		plural = ""
	}
	fmt.Fprintf(b, "; %d %s%s: ", len(items), noun, plural)
	shown := items
	if len(shown) > maxListed {
		shown = shown[:maxListed]
	}
	for i, it := range shown {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(strconv.Quote(it)) // a path may hold a comma, space or newline
	}
	if rest := len(items) - len(shown); rest > 0 {
		fmt.Fprintf(b, " … and %d more", rest)
	}
}
