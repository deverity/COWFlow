package cli

import "github.com/spf13/cobra"

func newResourcesCmd() *cobra.Command {
	return stub("resources", "List resources and leases", cobra.NoArgs)
}

func newResourceCmd() *cobra.Command {
	verb := func(use, short string) *cobra.Command {
		cmd := stub(use+" <name>", short, cobra.ExactArgs(1))
		cmd.Flags().Bool("wait", false, "wait in the queue for a slot")
		cmd.Flags().Int("ttl", 0, "lease time to live in seconds")
		return cmd
	}
	return group("resource", "Manage resource leases",
		verb("acquire", "Acquire a resource lease"),
		verb("release", "Release a resource lease"),
		verb("revoke", "Revoke a resource lease"),
		verb("announce", "Announce resource use"),
	)
}
