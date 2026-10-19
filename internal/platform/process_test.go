package platform

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	pidReadTimeout = 30 * time.Second
	goneTimeout    = 5 * time.Second
)

// startHelperTree starts a helper tree of the given depth through StartTree
// and returns it with the PIDs of its members, root first.
func startHelperTree(t *testing.T, depth int, graceful bool) (Tree, []int) {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)

	cmd := exec.Command(exe) //nolint:gosec // G204: re-executes the test binary itself
	cmd.Env = append(os.Environ(), fmt.Sprintf("%s=%d", treeHelperEnv, depth))
	if graceful {
		cmd.Env = append(cmd.Env, treeHelperGracefulEnv+"=1")
	}
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)

	tree, err := StartTree(cmd)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = tree.Cancel(0)
		_ = tree.Wait()
	})

	pids, err := readPIDs(stdout, depth, pidReadTimeout)
	require.NoError(t, err)
	assert.Equal(t, tree.Pid(), pids[0], "the root prints its PID first")
	return tree, pids
}

// readPIDs reads n "PID <n>" lines from r, failing after timeout.
func readPIDs(r io.Reader, n int, timeout time.Duration) ([]int, error) {
	type result struct {
		pids []int
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		var pids []int
		sc := bufio.NewScanner(r)
		for len(pids) < n && sc.Scan() {
			s, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "PID ")
			if !ok {
				continue
			}
			pid, err := strconv.Atoi(s)
			if err != nil {
				ch <- result{err: fmt.Errorf("parse %q: %w", sc.Text(), err)}
				return
			}
			pids = append(pids, pid)
		}
		if len(pids) < n {
			ch <- result{err: fmt.Errorf("read %d of %d PIDs: %w", len(pids), n, sc.Err())}
			return
		}
		ch <- result{pids: pids}
	}()
	select {
	case res := <-ch:
		return res.pids, res.err
	case <-time.After(timeout):
		return nil, fmt.Errorf("timed out reading %d PIDs", n)
	}
}

// AC 16: canceling kills the whole tree, grandchildren included.
func TestProcessTree_CancelKillsTree(t *testing.T) {
	tree, pids := startHelperTree(t, 3, false)
	require.Len(t, pids, 3)

	// Watch by handle (Windows) before canceling, to avoid PID reuse.
	watches := make([]func(time.Duration) bool, len(pids))
	for i, pid := range pids {
		watches[i] = watchPID(t, pid)
	}

	require.NoError(t, tree.Cancel(500*time.Millisecond))
	for i, gone := range watches {
		assert.True(t, gone(goneTimeout), "process %d (depth %d) still alive after Cancel", pids[i], 3-i)
	}
	assert.Error(t, tree.Wait(), "the root was killed, not exited cleanly")
}

// A process that stops on CTRL_BREAK / SIGTERM lets Cancel return before the
// grace deadline.
func TestProcessTree_GracefulExit(t *testing.T) {
	tree, pids := startHelperTree(t, 1, true)
	gone := watchPID(t, pids[0])

	const grace = 30 * time.Second
	start := time.Now()
	require.NoError(t, tree.Cancel(grace))
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 10*time.Second, "Cancel waited for the deadline instead of the polite stop")
	require.NoError(t, tree.Wait(), "the helper exits 0 on a polite stop")
	assert.True(t, gone(goneTimeout))
}

func TestProcessTree_WaitIsRepeatable(t *testing.T) {
	tree, _ := startHelperTree(t, 1, true)
	require.NoError(t, tree.Cancel(10*time.Second))
	err1 := tree.Wait()
	err2 := tree.Wait()
	assert.Equal(t, err1, err2)
	// Cancel after Wait is a no-op, not an error.
	assert.NoError(t, tree.Cancel(0))
}

func TestProcessTree_NaturalExit(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)
	// Without the helper env and with a filter matching nothing, the test
	// binary runs no test and exits 0.
	cmd := exec.Command(exe, "-test.run=^$") //nolint:gosec // G204: re-executes the test binary itself
	tree, err := StartTree(cmd)
	require.NoError(t, err)
	assert.Positive(t, tree.Pid())
	require.NoError(t, tree.Wait())
	assert.NoError(t, tree.Cancel(0))
}

func TestStartTree_Errors(t *testing.T) {
	t.Run("nil command", func(t *testing.T) {
		tree, err := StartTree(nil)
		require.ErrorIs(t, err, errNilCmd)
		assert.Nil(t, tree)
	})
	t.Run("missing executable", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "does-not-exist")
		tree, err := StartTree(exec.Command(missing)) //nolint:gosec // G204: fixed test path
		require.Error(t, err)
		assert.Nil(t, tree)
	})
}
