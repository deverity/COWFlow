package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/deverity/cowflow/internal/domain"
	"github.com/deverity/cowflow/internal/platform"
)

// LogsDir is the per-workspace log directory,
// <daemon.state_dir>/logs/<workspace_id>/ (SCOPE.md §6.1, §23 decision 42).
// destroy never removes it. workspaceID is a store-minted wks_ ID.
func LogsDir(stateDir, workspaceID string) string {
	return filepath.Join(stateDir, "logs", workspaceID)
}

// lexicalPath is <root>/<slug> without touching the filesystem. It is what a
// pending_mount row records, since the volume holding root is not there to
// resolve against.
func lexicalPath(root, slug string) (string, error) {
	if err := domain.ValidateSlug(slug); err != nil {
		return "", err
	}
	return filepath.Join(filepath.Clean(root), slug), nil
}

// workspacePath returns the canonical root and the canonical workspace path
// <root>/<slug> (SCOPE.md §6.1). The slug must pass the C2 grammar, the path
// must stay under root after links are resolved (platform.Confine), and
// nothing may exist there yet: an existing path in any state is
// ErrPathExists. It reads the filesystem but never writes it.
func workspacePath(root, slug string) (croot, path string, err error) {
	if err := domain.ValidateSlug(slug); err != nil {
		return "", "", err
	}
	croot, err = platform.Canonical(root)
	if err != nil {
		return "", "", fmt.Errorf("workspace root: %w", err)
	}
	p := filepath.Join(croot, slug)
	c, err := platform.Confine(croot, p)
	if err != nil {
		return "", "", fmt.Errorf("workspace path: %w", err)
	}
	switch _, err := os.Lstat(p); {
	case err == nil:
		return "", "", fmt.Errorf("%w: %s", ErrPathExists, p)
	case !errors.Is(err, fs.ErrNotExist):
		return "", "", fmt.Errorf("workspace path: %w", err)
	}
	if !samePath(c, p) || samePath(c, croot) {
		// Unreachable after the checks above; kept so a resolution surprise
		// can never hand a provider a path other than <root>/<slug>.
		return "", "", fmt.Errorf("%w: %q resolves to %q", platform.ErrOutsideRoot, p, c)
	}
	return croot, p, nil
}

// samePath compares two clean OS paths, ignoring case on Windows.
func samePath(a, b string) bool {
	a, b = filepath.Clean(filepath.FromSlash(a)), filepath.Clean(filepath.FromSlash(b))
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
