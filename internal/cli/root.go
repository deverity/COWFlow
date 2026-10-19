package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

// Deps is everything a command needs; tests inject fakes here.
type Deps struct {
	Version, Commit, Date string
}

// runState records what the run learned while cobra parsed the command line.
type runState struct {
	parsed bool // flags and args were valid and a command started running
	json   bool // --json
}

// NewRootCmd builds the cowflow root command.
func NewRootCmd(ios IOStreams, deps Deps, st *runState) *cobra.Command {
	root := &cobra.Command{
		Use:           "cowflow",
		Short:         "Local runtime for teams of AI agents on one Git repository",
		Version:       fmt.Sprintf("%s (commit %s, built %s)", deps.Version, deps.Commit, deps.Date),
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			st.parsed = true
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	root.CompletionOptions.DisableDefaultCmd = true // `completion` is not in §14
	root.PersistentFlags().BoolVar(&st.json, "json", false, "print one JSON document on stdout")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageErr(err) })
	root.SetIn(ios.In)
	root.SetOut(ios.Out)
	root.SetErr(ios.Err)
	return root
}

// Execute runs the CLI with args and returns the process exit code.
func Execute(ctx context.Context, args []string, ios IOStreams, deps Deps) int {
	st := &runState{}
	root := NewRootCmd(ios, deps, st)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	code := codeOf(err, st.parsed)
	if err != nil {
		renderError(ios, st.json, code, err)
	}
	return code
}
