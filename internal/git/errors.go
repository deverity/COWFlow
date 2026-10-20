package git

import (
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors callers branch on with errors.Is.
var (
	// ErrNotFound means the git executable could not be located.
	ErrNotFound = errors.New("git: executable not found")
	// ErrVersionTooOld means git is older than the required 2.40 (§3.1).
	ErrVersionTooOld = errors.New("git: version older than 2.40")
	// ErrInvalidRefName means a branch or ref name was rejected by
	// git check-ref-format (§17).
	ErrInvalidRefName = errors.New("git: invalid ref name")
	// ErrInvalidArgument means an argument was refused before it reached
	// git: a relative path, a non-hex SHA, an option-like value, a NUL byte.
	ErrInvalidArgument = errors.New("git: invalid argument")
	// ErrUnexpectedOutput means git's output was truncated or unparsable.
	// Parsers never return partial results with it.
	ErrUnexpectedOutput = errors.New("git: unexpected output")
	// ErrOptsUnsupported means an operation needs per-call environment or
	// stdin and the Runner does not implement OptsRunner.
	ErrOptsUnsupported = errors.New("git: runner does not support per-call options")
	// ErrWorktreeConfigUnsafe means extensions.worktreeConfig was not turned
	// on because core.bare=true or core.worktree is set in the shared
	// config; git-config(1) requires moving them to config.worktree first.
	ErrWorktreeConfigUnsafe = errors.New("git: cannot enable extensions.worktreeConfig with core.bare or core.worktree in the shared config")
)

// Error is returned when git exits non-zero or times out.
type Error struct {
	Args     []string // without "-C dir"
	ExitCode int      // -1 when killed by timeout/cancel
	Stderr   string   // truncated, never contains env values
	Err      error    // underlying *exec.ExitError or context error
}

// Error reports the arguments, the exit code and the truncated stderr. It
// never includes environment values.
func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("git")
	if len(e.Args) > 0 {
		b.WriteByte(' ')
		b.WriteString(strings.Join(e.Args, " "))
	}
	fmt.Fprintf(&b, ": exit code %d", e.ExitCode)
	if s := strings.TrimSpace(e.Stderr); s != "" {
		b.WriteString(": ")
		b.WriteString(s)
	} else if e.Err != nil {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

// Unwrap returns the underlying error so errors.Is and errors.As can match
// context.DeadlineExceeded, context.Canceled or *exec.ExitError.
func (e *Error) Unwrap() error { return e.Err }
