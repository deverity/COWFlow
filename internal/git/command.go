package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Runner is the single entry point for every git subprocess (§8.1).
// dir == "" means "no -C"; otherwise "-C dir" is prepended.
type Runner interface {
	Run(ctx context.Context, dir string, args ...string) ([]byte, error)
}

// ExecOptions configures an ExecRunner.
type ExecOptions struct {
	GitPath  string        // "" → exec.LookPath("git")
	Timeout  time.Duration // per call; must be > 0 (Guidance: from scheduler.command_timeout_seconds)
	ExtraEnv []string      // harness: GIT_CONFIG_GLOBAL=..., fixed author/committer
}

// ExecRunner runs the git executable as a subprocess. It is immutable after
// construction and safe for concurrent use.
type ExecRunner struct {
	path    string
	timeout time.Duration
	env     []string
}

var _ OptsRunner = (*ExecRunner)(nil)

// strippedEnv lists inherited variables that would point git at another
// repository or config.
var strippedEnv = []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE",
	"GIT_OBJECT_DIRECTORY", "GIT_COMMON_DIR", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE",
	"GIT_CEILING_DIRECTORIES", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT"}

// fixedEnv is always set on every call. GIT_TERMINAL_PROMPT=0 is required by
// §8.1; LC_ALL=C (Guidance) keeps output stable and parseable;
// GCM_INTERACTIVE=never (Guidance) prevents Git Credential Manager pop-ups.
var fixedEnv = []string{"GIT_TERMINAL_PROMPT=0", "LC_ALL=C", "GCM_INTERACTIVE=never"}

const (
	maxStderr = 4096
	waitDelay = 2 * time.Second
)

// NewExecRunner resolves the git executable and snapshots the environment
// (os.Environ() minus the redirection variables, plus the fixed variables and
// opts.ExtraEnv). Lookup failures, including exec.ErrDot, wrap ErrNotFound.
func NewExecRunner(opts ExecOptions) (*ExecRunner, error) {
	if opts.Timeout <= 0 {
		return nil, fmt.Errorf("git: timeout must be positive, got %s", opts.Timeout)
	}
	name := opts.GitPath
	if name == "" {
		name = "git"
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotFound, err)
	}
	return &ExecRunner{
		path:    path,
		timeout: opts.Timeout,
		env:     buildEnv(os.Environ(), opts.ExtraEnv),
	}, nil
}

// Run executes git with args, prepending "-C dir" when dir is non-empty. No
// shell is involved. Stdout is returned even on failure; failures are *Error.
func (r *ExecRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return r.RunWith(ctx, dir, RunOpts{}, args...)
}

// RunWith is Run with per-call extra environment and stdin (see RunOpts).
// An o.Env entry other than GIT_INDEX_FILE=<absolute path> is rejected before
// git starts.
func (r *ExecRunner) RunWith(ctx context.Context, dir string, o RunOpts, args ...string) ([]byte, error) {
	if err := validateCallerEnv(o.Env); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	argv := args
	if dir != "" {
		argv = append([]string{"-C", dir}, args...)
	}
	cmd := exec.CommandContext(ctx, r.path, argv...) //nolint:gosec // G204: fixed git executable, argv slice, no shell
	cmd.Env = r.env
	if len(o.Env) > 0 {
		cmd.Env = append(slices.Clip(r.env), o.Env...)
	}
	cmd.Stdin = o.Stdin
	// Without WaitDelay a grandchild holding stdout open makes Run hang
	// after the timeout.
	cmd.WaitDelay = waitDelay
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		ge := &Error{Args: slices.Clone(args), ExitCode: -1, Stderr: truncate(errb.String(), maxStderr), Err: err}
		var ee *exec.ExitError
		// ExitCode() is -1 when the process was killed; classify timeouts
		// and cancellation with ctx.Err().
		if errors.As(err, &ee) && ctx.Err() == nil {
			ge.ExitCode = ee.ExitCode()
		} else if ctx.Err() != nil {
			ge.Err = errors.Join(err, ctx.Err())
		}
		return out.Bytes(), ge
	}
	return out.Bytes(), nil
}

// buildEnv returns base minus strippedEnv and any keys COWFlow sets itself,
// followed by fixedEnv and extra.
func buildEnv(base, extra []string) []string {
	env := make([]string, 0, len(base)+len(fixedEnv)+len(extra))
	for _, kv := range base {
		key, _, _ := strings.Cut(kv, "=")
		if isStripped(key) {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, fixedEnv...)
	return append(env, extra...)
}

func isStripped(key string) bool {
	for _, list := range [][]string{strippedEnv, fixedEnv} {
		for _, s := range list {
			s, _, _ = strings.Cut(s, "=")
			if envKeyEqual(key, s) {
				return true
			}
		}
	}
	return false
}

// envKeyEqual compares environment keys; they are case-insensitive on Windows.
func envKeyEqual(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// truncate limits s to at most n bytes without splitting a UTF-8 sequence.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…[truncated]"
}
