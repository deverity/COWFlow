package platform

import (
	"errors"
	"os/exec"
	"time"
)

// Tree is a started process and all of its descendants (SCOPE.md §7, AC 16).
//
// Windows: the root is created suspended, placed in a Job Object with
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, then resumed, so every descendant is a
// member of the job before any code runs. Unix: the root leads a new process
// group (Setpgid) that its descendants inherit.
//
// Callers must call Wait exactly as they would exec.Cmd.Wait; it releases the
// OS handles held for the tree. Wait and Cancel are safe for concurrent use.
type Tree interface {
	// Pid is the operating-system process ID of the root process.
	Pid() int
	// Wait blocks until the root process exits and returns its exit error,
	// like exec.Cmd.Wait. Later calls return the same error.
	Wait() error
	// Cancel asks politely (CTRL_BREAK / SIGTERM), waits up to grace, then kills the whole tree.
	// On Windows the wait is skipped when CTRL_BREAK cannot be delivered (no console).
	Cancel(grace time.Duration) error
}

// errNilCmd is returned by StartTree when cmd is nil.
var errNilCmd = errors.New("platform: nil command")

// StartTree starts cmd as the root of a process tree that Cancel can kill as
// a whole. The caller builds cmd with exec.Command(name, args...) — argv only,
// never a shell string (§7, §17) — and must not set cmd.SysProcAttr:
// StartTree owns it.
func StartTree(cmd *exec.Cmd) (Tree, error) {
	if cmd == nil {
		return nil, errNilCmd
	}
	return startTree(cmd)
}
