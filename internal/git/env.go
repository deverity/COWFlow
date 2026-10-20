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
	// GIT_INDEX_FILE=<absolute path> and the single-line GIT_AUTHOR_* /
	// GIT_COMMITTER_* NAME and EMAIL variables are allowed; anything else is rejected
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

// callerEnvKeys are the only variables a caller may add per call, with the
// check each value must pass.
var callerEnvKeys = map[string]func(val string) error{
	"GIT_INDEX_FILE":      checkEnvPath,
	"GIT_AUTHOR_NAME":     checkEnvIdentity,
	"GIT_AUTHOR_EMAIL":    checkEnvIdentity,
	"GIT_COMMITTER_NAME":  checkEnvIdentity,
	"GIT_COMMITTER_EMAIL": checkEnvIdentity,
}

// checkEnvPath accepts an absolute, NUL-free path.
func checkEnvPath(val string) error {
	if !filepath.IsAbs(val) || strings.ContainsRune(val, 0) {
		return fmt.Errorf("must be an absolute path, got %q", truncate(val, 200))
	}
	return nil
}

// checkEnvIdentity accepts a non-empty single-line value without NUL.
func checkEnvIdentity(val string) error {
	if val == "" || strings.ContainsAny(val, "\x00\r\n") {
		return fmt.Errorf("must be a non-empty single line, got %q", truncate(val, 200))
	}
	return nil
}

// validateCallerEnv accepts only callerEnvKeys with a valid value. The
// identity variables let a commit-tree call replace an inherited
// GIT_AUTHOR_* / GIT_COMMITTER_*, which would override -c user.*.
func validateCallerEnv(env []string) error {
	for _, kv := range env {
		key, val, ok := strings.Cut(kv, "=")
		var check func(string) error
		for k, f := range callerEnvKeys {
			if envKeyEqual(key, k) {
				check = f
			}
		}
		if !ok || check == nil {
			return fmt.Errorf("git: environment variable %q is not allowed per call", truncate(key, 80))
		}
		if err := check(val); err != nil {
			return fmt.Errorf("git: %s %w", key, err)
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
