package platform

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ErrOutsideRoot reports that a path resolves to a location that is neither
// the allowed root nor below it (SCOPE.md §17 "confined paths").
var ErrOutsideRoot = errors.New("platform: path outside root")

// longPathThreshold is the length above which Windows paths get the \\?\
// prefix (SCOPE.md §7: "\\?\ above 240 chars").
const longPathThreshold = 240

// Canonical returns the canonical form of p: absolute, lexically clean, with
// every symbolic link (and, on Windows, junction and 8.3 short name) of its
// deepest existing ancestor resolved. Elements that do not exist yet are
// re-appended unresolved, so a file that is about to be created still has a
// canonical path.
//
// Lexical cleaning happens before link resolution, so "a/link/.." is "a"
// whatever link points to. Callers must therefore operate on the returned
// path, never on the original input.
func Canonical(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("canonical %q: %w", p, err)
	}
	c, err := resolve(filepath.Clean(abs)) // OS-specific
	if err != nil {
		return "", fmt.Errorf("canonical %q: %w", p, err)
	}
	return c, nil
}

// resolveExisting resolves the deepest existing ancestor and re-appends the
// remaining elements, so not-yet-existing files can still be confined.
//
// p must be absolute and clean. eval resolves an existing path and must
// report a missing one with an error matching fs.ErrNotExist. An element
// that exists but cannot be resolved (a dangling symlink or junction) is an
// error: re-appending it unresolved would let a later create follow the link
// out of the root.
func resolveExisting(p string, eval func(string) (string, error)) (string, error) {
	cur := p
	var rest []string // missing elements, deepest last
	for {
		resolved, err := eval(cur)
		if err == nil {
			for i := len(rest) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, rest[i])
			}
			return resolved, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("resolve %q: %w", cur, err)
		}
		if _, lerr := os.Lstat(cur); lerr == nil {
			// cur exists although eval said it does not: either a real
			// dangling link, or it was created between the two calls.
			// Re-resolve a bounded number of times; only a link that stays
			// unresolvable is refused.
			again, rerr := reEval(cur, eval)
			if rerr != nil {
				return "", rerr
			}
			if again == "" {
				continue // vanished again: re-examine this component
			}
			for i := len(rest) - 1; i >= 0; i-- {
				again = filepath.Join(again, rest[i])
			}
			return again, nil
		} else if !errors.Is(lerr, fs.ErrNotExist) {
			return "", fmt.Errorf("resolve %q: %w", cur, lerr)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", fmt.Errorf("resolve %q: no existing ancestor: %w", p, err)
		}
		rest = append(rest, filepath.Base(cur))
		cur = parent
	}
}

// resolveRetries bounds how often a component that appears or disappears
// concurrently is re-examined before it is treated as a dangling link.
const resolveRetries = 5

// reEval re-resolves cur, which Lstat reported as existing right after eval
// reported it missing. It returns the resolved path, "" when cur vanished
// again (the caller re-examines it), or an error for a link that stays
// unresolvable after resolveRetries attempts.
func reEval(cur string, eval func(string) (string, error)) (string, error) {
	var last error
	for range resolveRetries {
		resolved, err := eval(cur)
		if err == nil {
			return resolved, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("resolve %q: %w", cur, err)
		}
		last = err
		if _, lerr := os.Lstat(cur); errors.Is(lerr, fs.ErrNotExist) {
			return "", nil
		} else if lerr != nil {
			return "", fmt.Errorf("resolve %q: %w", cur, lerr)
		}
	}
	return "", fmt.Errorf("resolve %q: dangling link: %w", cur, last)
}

// Confine canonicalizes root and p and returns the canonical p when it is
// root itself or lies below it; otherwise the error wraps ErrOutsideRoot.
// The comparison respects separator boundaries ("ws/a" does not contain
// "ws/ab") and is case-insensitive on Windows.
func Confine(root, p string) (string, error) {
	r, err := Canonical(root)
	if err != nil {
		return "", err
	}
	c, err := Canonical(p)
	if err != nil {
		return "", err
	}
	if !within(r, c) {
		return "", fmt.Errorf("%w: %q not under %q", ErrOutsideRoot, p, root)
	}
	return c, nil
}

// within reports whether the canonical path p is root or below it.
func within(root, p string) bool {
	if samePath(root, p) {
		return true
	}
	prefix := root
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	return hasPathPrefix(p, prefix) // case-insensitive on Windows
}

// The helpers below operate on Windows path forms as plain strings. They do
// not depend on GOOS so that their logic is unit-tested on every platform;
// only the Windows build calls them outside tests.

// winLongPath adds the \\?\ prefix (\\?\UNC\ for \\server\share paths) to an
// absolute Windows path longer than longPathThreshold. Short, relative and
// already-prefixed paths are returned unchanged: \\?\ disables Win32 path
// normalization, so it is only valid on a full path.
func winLongPath(p string) string {
	switch {
	case len(p) <= longPathThreshold:
		return p
	case strings.HasPrefix(p, `\\?\`), strings.HasPrefix(p, `\\.\`):
		return p
	case strings.HasPrefix(p, `\\`):
		return `\\?\UNC\` + p[2:]
	case isWinDriveAbs(p):
		return `\\?\` + p
	default:
		return p
	}
}

// winStripLongPrefix turns \\?\C:\x into C:\x and \\?\UNC\server\share into
// \\server\share. Other \\?\ forms (for example \\?\Volume{guid}\ for a
// volume without a drive letter) have no DOS equivalent and are kept.
func winStripLongPrefix(p string) string {
	if rest, ok := strings.CutPrefix(p, `\\?\UNC\`); ok {
		return `\\` + rest
	}
	if rest, ok := strings.CutPrefix(p, `\\?\`); ok && isWinDriveAbs(rest) {
		return rest
	}
	return p
}

// isWinDriveAbs reports whether p starts with a drive letter, a colon and a
// separator ("C:\" or "C:/").
func isWinDriveAbs(p string) bool {
	if len(p) < 3 || p[1] != ':' || (p[2] != '\\' && p[2] != '/') {
		return false
	}
	c := p[0]
	return ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}

// hasPrefixFold is a case-insensitive strings.HasPrefix.
func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}
