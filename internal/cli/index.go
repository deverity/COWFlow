package cli

import "github.com/spf13/cobra"

func newIndexCmd() *cobra.Command {
	return group("index", "Manage the derived index",
		stub("rebuild", "Rebuild the derived index from scratch", cobra.NoArgs),
	)
}
