package cli

import "github.com/spf13/cobra"

func newMCPCmd() *cobra.Command {
	cmd := stub("mcp", "Run the MCP server over stdio", cobra.NoArgs)
	f := cmd.Flags()
	// No default role: PENDING SPEC-GAPS U-API-6.
	f.String("role", "", "tool set to expose: lead|worker|watchdog")
	f.String("project", "", "project directory")
	return cmd
}
