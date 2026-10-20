package git

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// RunOpts carries the per-call additions a Runner may accept beyond argv.
type RunOpts struct {
	// Env is appended to the runner's base environment. Only
	// GIT_INDEX_FILE=<absolute path> is allowed; anything else is rejected
	// so a caller cannot point git at another repository or config.
	Env []string
	// Stdin, when non-nil, becomes the subprocess's standard input.
	Stdin io.Reader
}

// OptsRunner is a Runner that also accepts RunOpts. *ExecRunner implements
// it; decorators that only implement Runner make the operations that need
// RunOpts fail with ErrOptsUnsupported instead of silently dropping them.
type OptsRunner interface {
	Runner
	RunWith(ctx context.Context, dir string, o RunOpts, args ...string) ([]byte, error)
}

// callerEnvKeys are the only variables a caller may add per call.
var callerEnvKeys = []string{"GIT_INDEX_FILE"}

// validateCallerEnv accepts only callerEnvKeys with an absolute,
// NUL-free path value.
func validateCallerEnv(env []string) error {
	for _, kv := range env {
		key, val, ok := strings.Cut(kv, "=")
		allowed := false
		for _, k := range callerEnvKeys {
			if envKeyEqual(key, k) {
				allowed = true
			}
		}
		if !ok || !allowed {
			return fmt.Errorf("git: environment variable %q is not allowed per call", truncate(key, 80))
		}
		if !filepath.IsAbs(val) || strings.ContainsRune(val, 0) {
			return fmt.Errorf("git: %s must be an absolute path, got %q", key, truncate(val, 200))
		}
	}
	return nil
}

// indexEnv returns the per-call environment selecting indexFile, or nil when
// indexFile is empty (use the repository's own index).
func indexEnv(indexFile string) []string {
	if indexFile == "" {
		return nil
	}
	return []string{"GIT_INDEX_FILE=" + indexFile}
}
