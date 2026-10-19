// Command cowflow is the COWFlow CLI: a local runtime that coordinates
// AI agent teams working in parallel on the same Git repository.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/deverity/cowflow/internal/cli"
	"github.com/deverity/cowflow/internal/config"
	"github.com/deverity/cowflow/internal/platform"
)

// Set at build time via -ldflags "-X main.version=... -X main.commit=... -X main.date=...".
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	host := platform.NewHost()
	code := cli.Execute(ctx, os.Args[1:],
		cli.IOStreams{In: os.Stdin, Out: os.Stdout, Err: os.Stderr},
		cli.Deps{
			Version: version, Commit: commit, Date: date,
			// PENDING SPEC-GAPS U-API-1: Locate reports that the
			// location is undecided instead of guessing one.
			ConfigPath: config.Locate,
			Host:       host,
			DevDrive:   platform.NewDevDrive(host),
			Elevate:    elevate,
		})
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
