package workspace

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/deverity/cowflow/internal/config"
)

// ErrBadCachePath reports a [cache] value that cannot be handed to a tool:
// relative, containing a newline or NUL, or inside the workspace.
var ErrBadCachePath = errors.New("workspace: invalid [cache] path")

// cacheEntry is one [cache] key mapped to the variable a tool documents for
// it (SCOPE.md §6.1, §6.7).
type cacheEntry struct {
	key      string // the [cache] key, for messages
	variable string // variable written to .env.cowflow
	dir      string // cleaned absolute directory to create, check and write
}

// cacheEntries maps the non-empty [cache] keys to variables. An empty key is
// omitted (SCOPE.md §6.1). Every path must be absolute; it is cleaned. The
// cargo target directory is per workspace, <cargo_target>/<workspace_id>
// (§6.7); the other five are shared download or build caches.
func cacheEntries(c config.Cache, workspaceID string) ([]cacheEntry, error) {
	type src struct{ key, variable, val string }
	srcs := []src{
		{"go_modcache", "GOMODCACHE", c.GoModCache},
		{"go_cache", "GOCACHE", c.GoCache},
		{"pnpm_store", "npm_config_store_dir", c.PnpmStore},
		{"cargo_target", "CARGO_TARGET_DIR", c.CargoTarget},
		{"gradle_home", "GRADLE_USER_HOME", c.GradleHome},
		{"pip_cache", "PIP_CACHE_DIR", c.PipCache},
	}
	var out []cacheEntry
	for _, s := range srcs {
		if s.val == "" {
			continue
		}
		if err := checkValue(s.val); err != nil {
			return nil, fmt.Errorf("%w: cache.%s: %w", ErrBadCachePath, s.key, err)
		}
		if !filepath.IsAbs(s.val) {
			return nil, fmt.Errorf("%w: cache.%s %q is not absolute", ErrBadCachePath, s.key, s.val)
		}
		dir := filepath.Clean(s.val)
		if s.key == "cargo_target" {
			if workspaceID == "" || workspaceID == "." || workspaceID == ".." ||
				strings.ContainsAny(workspaceID, `/\`) {
				return nil, fmt.Errorf("%w: workspace id %q cannot name a cargo target directory", ErrBadCachePath, workspaceID)
			}
			dir = filepath.Join(dir, workspaceID)
		}
		out = append(out, cacheEntry{key: s.key, variable: s.variable, dir: dir})
	}
	return out, nil
}

// checkValue rejects the characters that would split or corrupt a
// KEY=VALUE line (SCOPE.md §6.1).
func checkValue(v string) error {
	if strings.ContainsAny(v, "\n\r\x00") {
		return errors.New("value contains a newline, carriage return or NUL")
	}
	return nil
}

// within reports whether path is dir or lies under it, lexically.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// ensureCacheDirs rejects caches inside the workspace (they would be cloned,
// counted as dirty and deleted on destroy), creates every cache directory,
// and warns, never fails, when a cache is off the Dev Drive volume
// (SCOPE.md §6.7).
func ensureCacheDirs(ctx context.Context, es []cacheEntry, wsPath string, dd config.DevDrive, vols Volumes, log *slog.Logger) error {
	ws := filepath.Clean(wsPath)
	for _, e := range es {
		if within(ws, e.dir) {
			return fmt.Errorf("%w: cache.%s %q is inside the workspace", ErrBadCachePath, e.key, e.dir)
		}
	}
	for _, e := range es {
		if err := os.MkdirAll(e.dir, 0o750); err != nil {
			return fmt.Errorf("create cache directory for cache.%s: %w", e.key, err)
		}
	}
	if !dd.Enabled || dd.Mount == "" || vols == nil {
		return nil
	}
	for _, e := range es {
		same, err := vols.SameVolume(ctx, e.dir, dd.Mount)
		switch {
		case err != nil:
			log.Warn("cannot tell whether a cache is on the Dev Drive",
				"key", e.key, "path", e.dir, "mount", dd.Mount, "error", err)
		case !same:
			log.Warn("cache is not on the Dev Drive; caches should live there",
				"key", e.key, "path", e.dir, "mount", dd.Mount)
		}
	}
	return nil
}
