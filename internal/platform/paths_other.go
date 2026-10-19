//go:build !windows

package platform

import (
	"path/filepath"
	"strings"
)

// resolve canonicalizes an absolute, clean path by evaluating the symbolic
// links of its deepest existing ancestor.
func resolve(p string) (string, error) {
	return resolveExisting(p, filepath.EvalSymlinks)
}

// samePath compares canonical paths byte for byte. This is stricter than
// the usual case-insensitivity of APFS, which is the safe direction.
func samePath(a, b string) bool { return a == b }

// hasPathPrefix is a byte-exact prefix test on canonical paths.
func hasPathPrefix(p, prefix string) bool { return strings.HasPrefix(p, prefix) }

// LongPath returns p unchanged: the \\?\ prefix exists only on Windows
// (SCOPE.md §7).
func LongPath(p string) string { return p }
