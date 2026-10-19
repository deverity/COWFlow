package cli

import "github.com/spf13/cobra"

func newLogsCmd() *cobra.Command {
	cmd := stub("logs", "Show a task's logs", cobra.NoArgs)
	cmd.Flags().String("task", "", "task whose logs to show")
	cmd.Flags().Bool("follow", false, "keep streaming")
	return cmd
}
