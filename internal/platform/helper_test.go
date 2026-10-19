package platform

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// Helper-process mode: when the test binary runs with treeHelperEnv set, it
// acts as one level of a process tree instead of running tests.
const (
	// treeHelperEnv holds the remaining depth of the tree (>= 1).
	treeHelperEnv = "COWFLOW_TREE_HELPER"
	// treeHelperGracefulEnv, when "1", makes the helper exit 0 on
	// CTRL_BREAK (Windows) or SIGTERM (Unix).
	treeHelperGracefulEnv = "COWFLOW_TREE_HELPER_GRACEFUL"
	// treeHelperSleep bounds how long a helper lives if nobody kills it.
	treeHelperSleep = 10 * time.Minute
)

func TestMain(m *testing.M) {
	if depth := os.Getenv(treeHelperEnv); depth != "" {
		os.Exit(runTreeHelper(depth))
	}
	os.Exit(m.Run())
}

// runTreeHelper prints "PID <n>" for itself, spawns the next level with a
// plain exec.Command (descendants must be caught by inheritance, not by
// StartTree), then sleeps.
func runTreeHelper(depthEnv string) int {
	depth, err := strconv.Atoi(depthEnv)
	if err != nil || depth < 1 {
		fmt.Fprintf(os.Stderr, "tree helper: bad depth %q\n", depthEnv)
		return 2
	}

	// Install the handler before announcing the PID, so a test that has read
	// the PID can rely on it. On Windows, os.Interrupt covers CTRL_BREAK.
	stop := make(chan os.Signal, 1)
	if os.Getenv(treeHelperGracefulEnv) == "1" {
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	}

	fmt.Printf("PID %d\n", os.Getpid())

	if depth > 1 {
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "tree helper: %v\n", err)
			return 2
		}
		child := exec.Command(exe) //nolint:gosec // G204: re-executes the test binary itself
		child.Env = append(os.Environ(), fmt.Sprintf("%s=%d", treeHelperEnv, depth-1))
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "tree helper: start child: %v\n", err)
			return 2
		}
	}

	select {
	case <-stop:
		return 0
	case <-time.After(treeHelperSleep):
		return 0
	}
}
