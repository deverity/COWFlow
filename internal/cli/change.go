package cli

import "github.com/spf13/cobra"

func newChangeCmd() *cobra.Command {
	publish := stub("publish", "Publish a task's work as a change", cobra.NoArgs)
	publish.Flags().String("task", "", "task whose workspace to publish")

	return group("change", "Manage published changes",
		publish,
		stub("list", "List changes", cobra.NoArgs),
		stub("show <id>", "Show a change", cobra.ExactArgs(1)),
		stub("manifest <id>", "Print a change manifest", cobra.ExactArgs(1)),
		stub("discard <id>", "Discard a change", cobra.ExactArgs(1)),
	)
}
