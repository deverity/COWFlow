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

// canonicalOrClean returns the canonical form of p, or p cleaned when it
// cannot be resolved. It is for comparisons only, never for removal.
func canonicalOrClean(p string) string {
	if c, err := platform.Canonical(filepath.FromSlash(p)); err == nil {
		return c
	}
	return filepath.Clean(filepath.FromSlash(p))
}

// removeOwnDir removes the directory at path only if it resolves to itself:
// its canonical parent joined with its own name. A link as the last
// element, or a path that resolves anywhere else, is refused. path need not
// be canonical (Windows 8.3 short names are fine); the removal acts on the
// canonical path.
func removeOwnDir(path string) error {
	path = filepath.Clean(path)
	parent, err := platform.Canonical(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("refusing to remove %s: %w", path, err)
	}
	own := filepath.Join(parent, filepath.Base(path))
	c, err := platform.Confine(parent, own)
	switch {
	case err != nil:
		return fmt.Errorf("refusing to remove %s: %w", path, err)
	case !samePath(c, own) || samePath(c, parent):
		return fmt.Errorf("refusing to remove %s: it resolves to %s", path, c)
	}
	if err := os.RemoveAll(c); err != nil {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
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
