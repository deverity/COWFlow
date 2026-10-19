package cli

import "github.com/spf13/cobra"

// addCommands attaches the SCOPE.md §14 command tree to root. §14 is a
// closed list: do not add commands or aliases it does not name.
func addCommands(root *cobra.Command) {
	root.AddCommand(
		// Setup
		newInitCmd(),
		newMCPCmd(),
		newDoctorCmd(),
		newDaemonCmd(),
		newDevDriveCmd(),
		// Tasks
		newTaskCmd(),
		newRunCmd(),
		newRunnersCmd(),
		// Team
		newStatusCmd(),
		newAgentsCmd(),
		newIntentsCmd(),
		newWhoCmd(),
		newOpenCmd(),
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
		newWorkspaceCmd(),
		newGoldenCmd(),
		newLogsCmd(),
		newEventsCmd(),
		newBudgetCmd(),
	)
}
