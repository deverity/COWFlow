package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/deverity/cowflow/internal/config"
)

// newMCPCmd builds `cowflow mcp`. The server lands in Sprint 3; the config
// location is already resolved so --project behaves as it will then.
func newMCPCmd(deps Deps) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run the MCP server over stdio",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if _, err := projectConfigPath(deps, project); err != nil {
				return &ExitError{Code: ExitState, Err: fmt.Errorf("config location: %w", err)}
			}
			return &ExitError{Code: exitNotImplemented, Err: errNotImplemented}
		},
	}
	f := cmd.Flags()
	// No cobra default: the role is the one of COWFLOW_SESSION_TOKEN when it
	// is set, else lead; a conflicting --role is a usage error (SPEC-GAPS
	// U-API-6, enforced with the server in Sprint 3).
	f.String("role", "", "tool set to expose: lead|worker|watchdog (default: the session token's role, else lead)")
	f.StringVar(&project, "project", "", "project directory (config.toml is located from it)")
	return cmd
}

// projectConfigPath returns the config.toml path for --project dir: located
// from dir when it is set (SPEC-GAPS U-API-1), else from Deps.ConfigPath.
func projectConfigPath(deps Deps, dir string) (string, error) {
	if dir != "" {
		return config.LocateFrom(dir)
	}
	if deps.ConfigPath == nil {
		return config.Locate()
	}
	return deps.ConfigPath()
}
