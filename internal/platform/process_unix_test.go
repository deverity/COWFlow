//go:build unix

package platform

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// watchPID returns a function that reports whether pid is gone within the
// given timeout: kill(pid, 0) fails with ESRCH, or (Linux) the process is a
// zombie that has already exited and only awaits reaping by its new parent.
func watchPID(t *testing.T, pid int) func(time.Duration) bool {
	t.Helper()
	return func(timeout time.Duration) bool {
		deadline := time.Now().Add(timeout)
		for {
			if pidGone(pid) {
				return true
			}
			if time.Now().After(deadline) {
				return false
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

func pidGone(pid int) bool {
	if err := unix.Kill(pid, 0); errors.Is(err, unix.ESRCH) {
		return true
	}
	return runtime.GOOS == "linux" && isZombie(pid)
}

// isZombie reads the state field of /proc/<pid>/stat ("pid (comm) S ...").
func isZombie(pid int) bool {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	i := bytes.LastIndexByte(b, ')')
	return i >= 0 && i+2 < len(b) && b[i+2] == 'Z'
}
