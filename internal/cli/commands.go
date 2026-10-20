package cli

import "github.com/spf13/cobra"

// addCommands attaches the SCOPE.md §14 command tree to root. §14 is a
// closed list: do not add commands or aliases it does not name.
func addCommands(root *cobra.Command, ios IOStreams, deps Deps, st *runState) {
	root.AddCommand(
		// Setup
		newInitCmd(ios, deps, st),
		newMCPCmd(deps),
		newDoctorCmd(ios, deps, st),
		newDaemonCmd(),
		newDevDriveCmd(ios, deps, st),
		// Tasks
		newTaskCmd(ios, deps, st),
		newRunCmd(),
		newRunnersCmd(),
		// Team
		newStatusCmd(),
		newAgentsCmd(),
		newIntentsCmd(),
		newWhoCmd(),
		newOpenCmd(ios, deps, st),
		// Cross-requests
		newRequestCmd(),
		// Changes
		newChangeCmd(),
		newSyncCmd(),
		// Integration
		newIntegrationCmd(),
		// Resources
		newResourcesCmd(),
		newResourceCmd(),
		newExecCmd(),
		// Index and diagnostics
		newIndexCmd(),
		newWorkspaceCmd(ios, deps, st),
		newGoldenCmd(ios, deps, st),
		newLogsCmd(),
		newEventsCmd(),
		newBudgetCmd(),
	)
}
