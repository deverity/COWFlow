package cli

import "github.com/spf13/cobra"

func newInitCmd() *cobra.Command {
	cmd := stub("init", "Initialize COWFlow in the current repository", cobra.NoArgs)
	f := cmd.Flags()
	f.String("devdrive", "", "provision a Dev Drive VHDX of this size (e.g. 50GB)")
	// Defaults of --path and --letter: PENDING SPEC-GAPS U-PLAT-9.
	f.String("path", "", "VHDX file path for --devdrive")
	f.String("letter", "", "drive letter for --devdrive")
	// §14 omits "container" here although config allows it: PENDING SPEC-GAPS U-API-10.
	f.String("provider", "", "workspace provider: auto|cow_clone|projfs|git_worktree")
	f.String("conventions", "", "apply a conventions preset (e.g. gitflow)")
	return cmd
}
