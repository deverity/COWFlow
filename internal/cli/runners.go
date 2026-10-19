package cli

import "github.com/spf13/cobra"

func newRunnersCmd() *cobra.Command {
	return group("runners", "Inspect agent runners",
		stub("list", "List configured runners", cobra.NoArgs),
		stub("check", "Check installed runner CLIs and versions", cobra.NoArgs),
	)
}
