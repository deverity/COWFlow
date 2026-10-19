package cli

import "github.com/spf13/cobra"

// Flags of the devdrive subcommands: PENDING SPEC-GAPS U-PLAT-9.
func newDevDriveCmd() *cobra.Command {
	return group("devdrive", "Manage the Dev Drive VHDX",
		stub("create", "Create the Dev Drive VHDX", cobra.NoArgs),
		stub("attach", "Attach the Dev Drive VHDX", cobra.NoArgs),
		stub("detach", "Detach the Dev Drive VHDX", cobra.NoArgs),
		stub("status", "Show Dev Drive status", cobra.NoArgs),
	)
}
