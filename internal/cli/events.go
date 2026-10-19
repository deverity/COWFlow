package cli

import "github.com/spf13/cobra"

func newEventsCmd() *cobra.Command {
	cmd := stub("events", "Show the event stream", cobra.NoArgs)
	cmd.Flags().Bool("follow", false, "keep streaming")
	cmd.Flags().String("since", "", "start after this cursor")
	return cmd
}
