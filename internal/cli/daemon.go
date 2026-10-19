package cli

import "github.com/spf13/cobra"

func newDaemonCmd() *cobra.Command {
	return group("daemon", "Manage the COWFlow daemon",
		stub("start", "Start the daemon", cobra.NoArgs),
		stub("stop", "Stop the daemon", cobra.NoArgs),
		stub("status", "Show daemon status", cobra.NoArgs),
		stub("install", "Install the daemon as an OS service", cobra.NoArgs),
	)
}
