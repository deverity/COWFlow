//go:build windows

package platform

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

// watchPID opens a SYNCHRONIZE handle on pid now (so a reused PID cannot fool
// the check later) and returns a function that reports whether the process
// exits within the given timeout. If the process is already gone, OpenProcess
// fails and the function always reports true.
func watchPID(t *testing.T, pid int) func(time.Duration) bool {
	t.Helper()
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid)) //nolint:gosec // G115: a Windows PID is a DWORD
	if err != nil {
		return func(time.Duration) bool { return true }
	}
	t.Cleanup(func() { _ = windows.CloseHandle(h) })
	return func(timeout time.Duration) bool {
		ev, err := windows.WaitForSingleObject(h, uint32(timeout.Milliseconds())) //nolint:gosec // G115: test timeouts are small
		return err == nil && ev == windows.WAIT_OBJECT_0
	}
}

// Closing the job without Cancel kills every member (KILL_ON_JOB_CLOSE).
func TestProcessTree_KillOnClose(t *testing.T) {
	tree, pids := startHelperTree(t, 3, false)
	require.Len(t, pids, 3)
	watches := make([]func(time.Duration) bool, len(pids))
	for i, pid := range pids {
		watches[i] = watchPID(t, pid)
	}

	wt, ok := tree.(*winTree)
	require.True(t, ok)
	wt.mu.Lock()
	wt.closeLocked()
	wt.mu.Unlock()

	for i, gone := range watches {
		assert.True(t, gone(goneTimeout), "process %d (depth %d) survived closing the job", pids[i], 3-i)
	}
	assert.Error(t, tree.Wait())
}
