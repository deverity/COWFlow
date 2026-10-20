package git

import (
	"fmt"
	"io"
	"os"
	"testing"
	"time"
)

// helperEnv selects a helper mode when the test binary is used as a fake git
// executable (ExecOptions.GitPath = the test binary).
const helperEnv = "COWFLOW_GIT_HELPER"

// TestMain lets the test binary act as a fake git:
//
//	COWFLOW_GIT_HELPER=env   print the environment, one variable per line
//	COWFLOW_GIT_HELPER=sleep sleep far longer than any test timeout
//	COWFLOW_GIT_HELPER=stdin copy stdin to stdout
func TestMain(m *testing.M) {
	switch os.Getenv(helperEnv) {
	case "":
		os.Exit(m.Run())
	case "env":
		for _, kv := range os.Environ() {
			fmt.Println(kv)
		}
		os.Exit(0)
	case "stdin":
		if _, err := io.Copy(os.Stdout, os.Stdin); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	case "sleep":
		time.Sleep(time.Minute)
		os.Exit(0)
	default:
		fmt.Fprintln(os.Stderr, "unknown helper mode")
		os.Exit(2)
	}
}

// helperRunner returns an ExecRunner whose "git" is this test binary in the
// given helper mode.
func helperRunner(t *testing.T, mode string, timeout time.Duration) *ExecRunner {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	r, err := NewExecRunner(ExecOptions{
		GitPath:  self,
		Timeout:  timeout,
		ExtraEnv: []string{helperEnv + "=" + mode},
	})
	if err != nil {
		t.Fatalf("NewExecRunner: %v", err)
	}
	return r
}
