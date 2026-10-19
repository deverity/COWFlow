package cli

import "github.com/spf13/cobra"

func newWorkspaceCmd() *cobra.Command {
	destroy := stub("destroy <id>", "Destroy a workspace", cobra.ExactArgs(1))
	destroy.Flags().Bool("force", false, "destroy even with unpreserved changes")

	return group("workspace", "Manage workspaces",
		stub("list", "List workspaces", cobra.NoArgs),
		stub("inspect <id>", "Inspect a workspace", cobra.ExactArgs(1)),
		destroy,
	)
}
