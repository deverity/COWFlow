package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

var errNotImplemented = errors.New("not implemented yet")

// stub returns a command whose behavior lands in a later sprint.
func stub(use, short string, args cobra.PositionalArgs) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  args,
		RunE: func(*cobra.Command, []string) error {
			return &ExitError{Code: exitNotImplemented, Err: errNotImplemented}
		},
	}
}

// group returns a parent command that only holds subcommands. It prints
// help when run alone and rejects unknown subcommands as usage errors.
func group(use, short string, children ...*cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(children...)
	return cmd
}
