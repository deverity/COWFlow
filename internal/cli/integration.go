package cli

import "github.com/spf13/cobra"

func newIntegrationCmd() *cobra.Command {
	create := stub("create <tasks...>", "Create an integration group", cobra.MinimumNArgs(1))
	create.Flags().String("target", "", "target branch")

	show := stub("show <group-id>", "Show an integration group", cobra.ExactArgs(1))
	show.Flags().Bool("diff", false, "include the diff")

	return group("integration", "Manage integration groups",
		create,
		stub("validate <group-id>", "Validate an integration group", cobra.ExactArgs(1)),
		stub("promote <group-id>", "Promote an integration group", cobra.ExactArgs(1)),
		stub("cancel <group-id>", "Cancel an integration group", cobra.ExactArgs(1)),
		show,
		stub("open <group-id>", "Open an integration candidate workspace", cobra.ExactArgs(1)),
	)
}
