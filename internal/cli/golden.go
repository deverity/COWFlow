package cli

import "github.com/spf13/cobra"

func newGoldenCmd() *cobra.Command {
	return group("golden", "Manage golden trees",
		stub("list", "List golden trees", cobra.NoArgs),
		stub("gc", "Remove unused golden trees", cobra.NoArgs),
	)
}
