package cli

import (
	"slices"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// specCommands is the SCOPE.md §14 command tree.
var specCommands = []string{
	"agents",
	"budget",
	"change", "change discard", "change list", "change manifest", "change publish", "change show",
	"daemon", "daemon install", "daemon start", "daemon status", "daemon stop",
	"devdrive", "devdrive attach", "devdrive create", "devdrive detach", "devdrive status",
	"doctor",
	"events",
	"exec",
	"golden", "golden gc", "golden list",
	"index", "index rebuild",
	"init",
	"integration", "integration cancel", "integration create", "integration open",
	"integration promote", "integration show", "integration validate",
	"intents",
	"logs",
	"mcp",
	"open",
	"request", "request accept", "request close", "request comment", "request deliver",
	"request list", "request reassign", "request reject", "request send",
	"resource", "resource acquire", "resource announce", "resource release", "resource revoke",
	"resources",
	"run",
	"runners", "runners check", "runners list",
	"status",
	"sync", "sync abort", "sync status",
	"task", "task create", "task list", "task pause", "task resume", "task show",
	"who",
	"workspace", "workspace destroy", "workspace inspect", "workspace list",
}

func newTree() *cobra.Command {
	st := &runState{}
	root := NewRootCmd(IOStreams{}, Deps{}, st)
	addCommands(root, IOStreams{}, Deps{}, st)
	return root
}

func walk(cmd *cobra.Command, fn func(*cobra.Command)) {
	for _, c := range cmd.Commands() {
		fn(c)
		walk(c, fn)
	}
}

// path returns the command path without the root name.
func path(c *cobra.Command) string {
	return c.CommandPath()[len("cowflow "):]
}

func TestCommandTree_MatchesSpec(t *testing.T) {
	var got []string
	walk(newTree(), func(c *cobra.Command) { got = append(got, path(c)) })
	slices.Sort(got)

	want := slices.Clone(specCommands)
	slices.Sort(want)
	assert.Equal(t, want, got)
}

func TestCommandTree_Flags(t *testing.T) {
	tests := []struct{ path, flag string }{
		{"init", "devdrive"}, {"init", "path"}, {"init", "letter"}, {"init", "provider"}, {"init", "conventions"},
		{"mcp", "role"}, {"mcp", "project"},
		{"task create", "base"}, {"task create", "title"}, {"task create", "contract"},
		{"task create", "paths"}, {"task create", "depends"}, {"task create", "kind"},
		{"task create", "watch"}, {"task create", "runner"}, {"task create", "force"},
		{"task create", "no-contract"},
		{"run", "max-agents"}, {"run", "runner"},
		{"status", "watch"},
		{"intents", "path"},
		{"who", "path"},
		{"request send", "kind"}, {"request send", "title"}, {"request send", "expect"},
		{"request list", "open"}, {"request list", "to"},
		{"request deliver", "change"},
		{"change publish", "task"},
		{"sync", "into"}, {"sync", "strategy"}, {"sync", "reason"},
		{"integration create", "target"},
		{"integration show", "diff"},
		{"resource acquire", "wait"}, {"resource acquire", "ttl"},
		{"resource release", "wait"}, {"resource release", "ttl"},
		{"resource revoke", "wait"}, {"resource revoke", "ttl"},
		{"resource announce", "wait"}, {"resource announce", "ttl"},
		{"exec", "resource"},
		{"workspace destroy", "force"},
		{"logs", "task"}, {"logs", "follow"},
		{"events", "follow"}, {"events", "since"},
	}
	cmds := map[string]*cobra.Command{}
	walk(newTree(), func(c *cobra.Command) { cmds[path(c)] = c })

	for _, tt := range tests {
		t.Run(tt.path+" --"+tt.flag, func(t *testing.T) {
			c, ok := cmds[tt.path]
			require.True(t, ok, "command %q missing", tt.path)
			assert.NotNil(t, c.Flags().Lookup(tt.flag))
		})
	}
}

func TestCommandTree_MCPRoleHasNoDefault(t *testing.T) {
	cmd, _, err := newTree().Find([]string{"mcp"})
	require.NoError(t, err)
	assert.Empty(t, cmd.Flags().Lookup("role").DefValue) // U-API-6
}

func TestCommandTree_JSONEverywhere(t *testing.T) {
	walk(newTree(), func(c *cobra.Command) {
		if !c.HasSubCommands() {
			assert.NotNil(t, c.InheritedFlags().Lookup("json"), path(c))
		}
	})
}

func TestStubs_ExitCode(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{name: "stub", args: []string{"task", "list"}, want: exitNotImplemented},
		{name: "stub with arg", args: []string{"task", "show", "t1"}, want: exitNotImplemented},
		{name: "missing id", args: []string{"task", "show"}, want: ExitUsage},
		{name: "extra arg", args: []string{"task", "list", "x"}, want: ExitUsage},
		{name: "group alone prints help", args: []string{"task"}, want: ExitOK},
		{name: "unknown subcommand", args: []string{"task", "nope"}, want: ExitUsage},
		{name: "sync positional", args: []string{"sync", "ch1", "--into", "ws1", "--strategy", "merge"}, want: exitNotImplemented},
		{name: "sync subcommand", args: []string{"sync", "status", "s1"}, want: exitNotImplemented},
		{name: "sync without id", args: []string{"sync"}, want: ExitUsage},
		{name: "integration create many", args: []string{"integration", "create", "auth", "api", "--target", "develop"}, want: exitNotImplemented},
		{name: "integration create none", args: []string{"integration", "create"}, want: ExitUsage},
		{name: "exec without dash", args: []string{"exec", "--resource", "compile", "go", "build"}, want: ExitUsage},
		{name: "exec without command", args: []string{"exec", "--resource", "compile", "--"}, want: ExitUsage},
		{name: "bad flag on stub", args: []string{"task", "list", "--nope"}, want: ExitUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := run(t, tt.args...)
			assert.Equal(t, tt.want, r.code, r.stderr)
		})
	}
}

func TestStubs_NotImplementedMessage(t *testing.T) {
	r := run(t, "status")
	assert.Contains(t, r.stderr, errNotImplemented.Error())
}

func TestExec_DashDash(t *testing.T) {
	var gotArgs []string
	root := newTree()
	cmd, _, err := root.Find([]string{"exec"})
	require.NoError(t, err)
	cmd.RunE = func(_ *cobra.Command, args []string) error {
		gotArgs = args
		return nil
	}
	root.SetArgs([]string{"exec", "--resource", "compile", "--", "go", "build", "./...", "--json"})
	require.NoError(t, root.Execute())
	assert.Equal(t, []string{"go", "build", "./...", "--json"}, gotArgs)
	assert.Equal(t, "compile", cmd.Flags().Lookup("resource").Value.String())
}
