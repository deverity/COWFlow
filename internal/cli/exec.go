package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

func newExecCmd() *cobra.Command {
	cmd := stub("exec --resource <name> -- <cmd...>", "Run a command while holding a resource lease", execArgs)
	cmd.Flags().String("resource", "", "resource to hold")
	return cmd
}

// execArgs requires a command after "--", so its own flags are never
// mistaken for cowflow flags.
func execArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 || cmd.ArgsLenAtDash() != 0 {
		return errors.New(`exec needs a command after "--"`)
	}
	return nil
}
