package cli

import "github.com/spf13/cobra"

func newRequestCmd() *cobra.Command {
	send := stub("send <to>", "Send a cross-request to a task", cobra.ExactArgs(1))
	send.Flags().String("kind", "", "request kind")
	send.Flags().String("title", "", "request title")
	send.Flags().String("expect", "", "expected output")

	list := stub("list", "List cross-requests", cobra.NoArgs)
	list.Flags().Bool("open", false, "only open requests")
	list.Flags().String("to", "", "only requests to this task")

	deliver := stub("deliver <id>", "Deliver a cross-request with a change", cobra.ExactArgs(1))
	deliver.Flags().String("change", "", "change that satisfies the request")

	return group("request", "Manage cross-requests",
		send,
		list,
		stub("accept <id>", "Accept a cross-request", cobra.ExactArgs(1)),
		stub("reject <id>", "Reject a cross-request", cobra.ExactArgs(1)),
		stub("close <id>", "Close a cross-request", cobra.ExactArgs(1)),
		stub("comment <id>", "Comment on a cross-request", cobra.ExactArgs(1)),
		stub("reassign <id>", "Reassign a cross-request", cobra.ExactArgs(1)),
		deliver,
	)
}
