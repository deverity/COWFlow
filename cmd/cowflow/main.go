// Command cowflow is the COWFlow CLI: a local runtime that coordinates
// AI agent teams working in parallel on the same Git repository.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/deverity/cowflow/internal/cli"
)

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
		cli.Deps{Version: version, Commit: commit, Date: date})
	stop()
	os.Exit(code)
}
