// Command cowflow is the COWFlow CLI: a local runtime that coordinates
// AI agent teams working in parallel on the same Git repository.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"os/user"
	"time"

	"github.com/deverity/cowflow/internal/cli"
	"github.com/deverity/cowflow/internal/config"
	"github.com/deverity/cowflow/internal/git"
	"github.com/deverity/cowflow/internal/platform"
)

// brokenGit is the git.Runner used when the git executable cannot be found,
// so that commands such as doctor can report it instead of failing to start.
type brokenGit struct{ err error }

func (b brokenGit) Run(context.Context, string, ...string) ([]byte, error) { return nil, b.err }

func newDeps() cli.Deps {
	host := platform.NewHost()
	// Each call is bounded by the built-in scheduler.command_timeout_seconds
	// (SPEC-GAPS U-API-11).
	gitTimeout := time.Duration(config.Default().Scheduler.CommandTimeoutSeconds) * time.Second
	var runner git.Runner
	if r, err := git.NewExecRunner(git.ExecOptions{Timeout: gitTimeout}); err != nil {
		runner = brokenGit{err: err}
	} else {
		runner = r
	}
	return cli.Deps{
		Version:    version,
		Commit:     commit,
		Date:       date,
		ConfigPath: config.Locate,
		Host:       host,
		DevDrive:   platform.NewDevDrive(host),
		Git:        runner,
		Elevate:    elevate,

		NewAutoAttacher: platform.NewAutoAttacherFor,
		CurrentUser:     currentUser,
		// DefenderExclusions stays nil until internal/platform provides the
		// Get-MpPreference probe; doctor skips defender.exclusion meanwhile.
	}
}

// currentUser returns the user running cowflow; on Windows os/user reports
// it as DOMAIN\user without cgo.
func currentUser() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("current user: %w", err)
	}
	return u.Username, nil
}

// Set at build time via -ldflags "-X main.version=... -X main.commit=... -X main.date=...".
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := cli.Execute(ctx, os.Args[1:],
		cli.IOStreams{In: os.Stdin, Out: os.Stdout, Err: os.Stderr},
		newDeps())
	stop()
	os.Exit(code)
}

// elevate relaunches this executable with args through UAC, in the same
// working directory. os.Executable is used, not os.Args[0], which may be
// relative to a directory the elevated process does not start in.
func elevate(ctx context.Context, args []string) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return -1, fmt.Errorf("locate cowflow executable: %w", err)
	}
	return platform.RunElevated(ctx, exe, args, "")
}
