package cli

import "github.com/spf13/cobra"

func newStatusCmd() *cobra.Command {
	cmd := stub("status", "Show project status", cobra.NoArgs)
	cmd.Flags().Bool("watch", false, "keep refreshing")
	return cmd
}

func newAgentsCmd() *cobra.Command {
	return stub("agents", "List agent sessions", cobra.NoArgs)
}

func newIntentsCmd() *cobra.Command {
	cmd := stub("intents", "List active intents", cobra.NoArgs)
	cmd.Flags().String("path", "", "only intents covering this path")
	return cmd
}

func newWhoCmd() *cobra.Command {
	cmd := stub("who", "Show who is working on a path", cobra.NoArgs)
	cmd.Flags().String("path", "", "path to look up")
	return cmd
}
