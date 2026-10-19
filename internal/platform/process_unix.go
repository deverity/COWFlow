//go:build unix

package platform

import (
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// unixTree is a process group led by the root process (SCOPE.md §7: Setpgid
// + SIGTERM + SIGKILL).
//
// Known limit: a descendant that calls setsid(2) or setpgid(2) leaves the
// group and is not reached by Cancel.
type unixTree struct {
	cmd  *exec.Cmd
	pgid int
	done chan struct{}
	err  error

	cancelMu sync.Mutex // serializes Cancel calls
}

func startTree(cmd *exec.Cmd) (Tree, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start tree: %w", err)
	}
	// With Setpgid and Pgid == 0 the child's group ID is its own PID.
	t := &unixTree{cmd: cmd, pgid: cmd.Process.Pid, done: make(chan struct{})}
	go func() { t.err = cmd.Wait(); close(t.done) }()
	return t, nil
}

func (t *unixTree) Pid() int { return t.cmd.Process.Pid }

func (t *unixTree) Wait() error {
	<-t.done
	return t.err
}

func (t *unixTree) Cancel(grace time.Duration) error {
	t.cancelMu.Lock()
	defer t.cancelMu.Unlock()

	if err := signalGroup(t.pgid, unix.SIGTERM); err != nil {
		return fmt.Errorf("cancel tree: SIGTERM: %w", err)
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-t.done:
	case <-timer.C:
	}
	// Kill every remaining member, including grandchildren that outlived the root.
	if err := signalGroup(t.pgid, unix.SIGKILL); err != nil {
		return fmt.Errorf("cancel tree: SIGKILL: %w", err)
	}
	return nil
}

// signalGroup sends sig to every member of process group pgid. An empty or
// vanished group (ESRCH) is not an error: there is nothing left to stop.
func signalGroup(pgid int, sig unix.Signal) error {
	err := unix.Kill(-pgid, sig)
	if err == nil || errors.Is(err, unix.ESRCH) {
		return nil
	}
	return err
}
