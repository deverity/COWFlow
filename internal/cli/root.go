package cli

import (
	"context"
	"fmt"
	"io"
	"io/fs"

	"github.com/spf13/cobra"

	"github.com/deverity/cowflow/internal/git"
	"github.com/deverity/cowflow/internal/platform"
)

// Deps is everything a command needs; tests inject fakes here.
type Deps struct {
	Version, Commit, Date string

	// ConfigPath returns the path of config.toml: config.Locate in
	// production (<project root>/.cowflow/config.toml, SPEC-GAPS U-API-1);
	// tests inject a path.
	ConfigPath func() (string, error)
	Host       platform.Host
	DevDrive   platform.DevDrive
	Git        git.Runner
	// Elevate relaunches cowflow with args through UAC (one prompt) and
	// returns the elevated process's exit code.
	Elevate func(ctx context.Context, args []string) (exitCode int, err error)
	// MkdirAll creates the Dev Drive layout directories of init; nil means
	// os.MkdirAll. Tests inject a recorder because the layout lives on the
	// Dev Drive volume (X:/ws, X:/cache/...).
	MkdirAll func(path string, perm fs.FileMode) error
	// LetterInUse reports whether a drive letter ("X") is taken; nil means
	// a probe of its root directory.
	LetterInUse func(letter string) (bool, error)
	// NewAutoAttacher returns the Dev Drive auto-attach registrar for t
	// (platform.NewAutoAttacherFor in production); nil means auto-attach
	// is unavailable, so tests never register a real scheduled task.
	NewAutoAttacher func(t platform.AutoAttachTarget) platform.AutoAttacher
	// CurrentUser returns DOMAIN\user of the person running cowflow. The
	// non-elevated init hands it to its elevated child, which may run under
	// other credentials, so the auto-attach task triggers at the right
	// logon. nil: the child uses its own token's user.
	CurrentUser func() (string, error)
}

// runState records what the run learned while cobra parsed the command line.
type runState struct {
	parsed bool // flags and args were valid and a command started running
	json   bool // --json

	// args is the command line without the program name, for elevation.
	args []string
}

// NewRootCmd builds the cowflow root command.
func NewRootCmd(ios IOStreams, deps Deps, st *runState) *cobra.Command {
	var showVersion bool
	root := &cobra.Command{
		Use:           "cowflow",
		Short:         "Local runtime for teams of AI agents on one Git repository",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			st.parsed = true
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if showVersion {
				return printVersion(ios, deps, st)
			}
			return cmd.Help()
		},
	}
	root.CompletionOptions.DisableDefaultCmd = true // `completion` is not in §14
	root.PersistentFlags().BoolVar(&st.json, "json", false, "print one JSON document on stdout")
	// --version only: there is no version subcommand (SPEC-GAPS U-API-13).
	root.Flags().BoolVarP(&showVersion, "version", "v", false, "print the version and exit")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageErr(err) })
	root.SetIn(ios.In)
	root.SetOut(ios.Out)
	root.SetErr(ios.Err)
	return root
}

// versionInfo is the --json document of `cowflow --version`.
type versionInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

// printVersion prints the build information, as one JSON document with
// --json.
func printVersion(ios IOStreams, deps Deps, st *runState) error {
	v := versionInfo{Version: deps.Version, Commit: deps.Commit, Date: deps.Date}
	return printResult(ios, st.json, v, func(w io.Writer) error {
		_, err := fmt.Fprintf(w, "cowflow version %s (commit %s, built %s)\n", v.Version, v.Commit, v.Date)
		return err
	})
}

// Execute runs the CLI with args and returns the process exit code.
func Execute(ctx context.Context, args []string, ios IOStreams, deps Deps) int {
	st := &runState{args: args}
	root := NewRootCmd(ios, deps, st)
	addCommands(root, ios, deps, st)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	code := codeOf(err, st.parsed)
	if err != nil {
		renderError(ios, st.json, code, err)
	}
	return code
}
