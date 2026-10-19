// Command cowflow is the COWFlow CLI: a local runtime that coordinates
// AI agent teams working in parallel on the same Git repository.
package main

import (
	"fmt"
	"io"
	"os"
)

// Set at build time via -ldflags "-X main.version=... -X main.commit=... -X main.date=...".
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes the CLI and returns the process exit code
// (SCOPE.md §14: 0 ok, 1 usage, 2 state, 3 conflict, 4 refused by policy).
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 1
	}

	switch args[0] {
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "cowflow %s (commit %s, built %s)\n", version, commit, date)
		return 0
	case "help", "--help", "-h":
		usage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "cowflow: unknown command %q\n\n", args[0])
		usage(stderr)
		return 1
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `Usage: cowflow <command>

Commands:
  version   Print version information
  help      Show this help
`)
}
