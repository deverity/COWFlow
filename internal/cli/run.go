package cli

import "github.com/spf13/cobra"

func newRunCmd() *cobra.Command {
	cmd := stub("run", "Run agent sessions for ready tasks", cobra.NoArgs)
	f := cmd.Flags()
	f.Int("max-agents", 0, "maximum concurrent agent sessions")
	f.String("runner", "", "runner to use")
	return cmd
}
