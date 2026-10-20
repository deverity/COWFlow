package cli

import "github.com/spf13/cobra"

func newSyncCmd() *cobra.Command {
	cmd := stub("sync <change-id>", "Sync a change into a workspace", cobra.ExactArgs(1))
	f := cmd.Flags()
	f.String("into", "", "target workspace id")
	// One spelling everywhere: squash (SPEC-GAPS C4).
	f.String("strategy", "", "sync strategy: merge|cherry-pick|squash")
	f.String("reason", "", "why the sync is needed (recorded)")
	cmd.AddCommand(
		stub("status <sync-id>", "Show a sync attempt", cobra.ExactArgs(1)),
		stub("abort <sync-id>", "Abort a sync attempt", cobra.ExactArgs(1)),
	)
	return cmd
}
