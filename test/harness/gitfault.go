package harness

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/deverity/cowflow/internal/git"
)

// GitFault describes how one class of git invocations misbehaves. The first
// fault whose Match is a prefix of the call's argv (without "-C dir") wins;
// an empty Match matches every call.
type GitFault struct {
	Match      []string // argv prefix, e.g. {"check-ref-format"}
	ExitCode   int      // > 0: fail with this code
	Timeout    bool     // block until ctx is done, then return ctx error
	TruncateAt int      // > 0: cut stdout to this many bytes
	Stdout     string   // canned output instead of running git
}

// FaultyRunner is a git.Runner decorator that injects GitFaults and records
// every call. Calls that match no fault pass through to Inner. Set Faults
// before sharing the runner, or use Inject while it is in use.
type FaultyRunner struct {
	Inner  git.Runner
	mu     sync.Mutex
	Faults []GitFault
	Calls  [][]string // recorded argv (without "-C dir"), one entry per Run
}

var _ git.Runner = (*FaultyRunner)(nil)

// Inject appends a fault; it is safe to call while Run is in use.
func (f *FaultyRunner) Inject(fault GitFault) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Faults = append(f.Faults, fault)
}

// Recorded returns a copy of the recorded argv lists; it is safe to call
// while Run is in use.
func (f *FaultyRunner) Recorded() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]string, len(f.Calls))
	for i, c := range f.Calls {
		out[i] = slices.Clone(c)
	}
	return out
}

// Run records the call, then applies the first matching fault, or delegates
// to Inner. Injected failures are *git.Error values shaped like the real
// runner's: ExitCode -1 plus the context error for timeouts, the real exit
// code otherwise.
func (f *FaultyRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	argv := slices.Clone(args)
	f.mu.Lock()
	f.Calls = append(f.Calls, argv)
	var fault *GitFault
	for i := range f.Faults {
		if hasPrefix(argv, f.Faults[i].Match) {
			ft := f.Faults[i]
			fault = &ft
			break
		}
	}
	f.mu.Unlock()

	if fault == nil {
		return f.delegate(ctx, dir, argv)
	}
	if fault.Timeout {
		<-ctx.Done()
		return nil, &git.Error{
			Args:     slices.Clone(argv),
			ExitCode: -1,
			Err:      errors.Join(errors.New("signal: killed"), ctx.Err()),
		}
	}
	if fault.ExitCode > 0 {
		return []byte(fault.Stdout), &git.Error{
			Args:     slices.Clone(argv),
			ExitCode: fault.ExitCode,
			Err:      fmt.Errorf("exit status %d", fault.ExitCode),
		}
	}
	var out []byte
	if fault.Stdout != "" {
		out = []byte(fault.Stdout)
	} else {
		var err error
		if out, err = f.delegate(ctx, dir, argv); err != nil {
			return out, err
		}
	}
	if fault.TruncateAt > 0 && len(out) > fault.TruncateAt {
		out = out[:fault.TruncateAt]
	}
	return out, nil
}

func (f *FaultyRunner) delegate(ctx context.Context, dir string, argv []string) ([]byte, error) {
	if f.Inner == nil {
		return nil, fmt.Errorf("harness: FaultyRunner has no Inner runner for git %v", argv)
	}
	return f.Inner.Run(ctx, dir, argv...)
}

// hasPrefix reports whether argv starts with prefix (an empty prefix always
// matches).
func hasPrefix(argv, prefix []string) bool {
	return len(prefix) <= len(argv) && slices.Equal(argv[:len(prefix)], prefix)
}
