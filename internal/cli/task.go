package cli

import "github.com/spf13/cobra"

func newTaskCmd() *cobra.Command {
	return group("task", "Manage tasks",
		newTaskCreateCmd(),
		stub("list", "List tasks", cobra.NoArgs),
		stub("show <id>", "Show a task", cobra.ExactArgs(1)),
		stub("pause <id>", "Pause a task", cobra.ExactArgs(1)),
		stub("resume <id>", "Resume a task", cobra.ExactArgs(1)),
	)
}

func newTaskCreateCmd() *cobra.Command {
	cmd := stub("create <slug>", "Create a task", cobra.ExactArgs(1))
	f := cmd.Flags()
	f.String("base", "", "base branch")
	f.String("title", "", "task title")
	f.String("contract", "", "contract file the task implements")
	f.StringSlice("paths", nil, "paths the task may change")
	f.StringSlice("depends", nil, "tasks this task depends on")
	f.String("kind", "", "task kind (e.g. feature|bugfix|chore|hotfix|release|watchdog)")
	f.String("watch", "", "what a watchdog task watches")
	f.String("runner", "", "runner for the task")
	f.Bool("force", false, "allow a non-default --base (audited)")
	f.Bool("no-contract", false, "run without the dependency's contract (audited)")
	return cmd
}
