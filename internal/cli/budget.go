package cli

import "github.com/spf13/cobra"

func newBudgetCmd() *cobra.Command {
	return stub("budget", "Show LLM budget usage", cobra.NoArgs)
}
