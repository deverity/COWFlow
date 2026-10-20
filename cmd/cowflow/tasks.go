package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/deverity/cowflow/internal/cli"
	"github.com/deverity/cowflow/internal/config"
	"github.com/deverity/cowflow/internal/git"
	"github.com/deverity/cowflow/internal/store"
	"github.com/deverity/cowflow/internal/workspace"
)

// openTasks wires the daemonless task commands (SCOPE.md §5.3): the SQLite
// store at config.DBPath, the git client over runner and the workspace
// Manager with its providers.
func openTasks(runner git.Runner) func(context.Context, *config.Config, string) (cli.TaskBackend, error) {
	return func(ctx context.Context, cfg *config.Config, cfgPath string) (cli.TaskBackend, error) {
		stateDir := config.StatePath(cfgPath, cfg)
		if err := os.MkdirAll(stateDir, 0o750); err != nil {
			return cli.TaskBackend{}, fmt.Errorf("create state dir: %w", err)
		}
		st, err := store.Open(ctx, config.DBPath(cfgPath, cfg), store.Options{})
		if err != nil {
			return cli.TaskBackend{}, err
		}
		// Warnings and errors only: the Manager's info lines would mix into
		// the command output.
		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
		gc := git.NewClient(runner)
		vols := workspace.PlatformVolumes{}
		m := workspace.NewManager(workspace.Deps{
			Config:  cfg,
			Store:   st,
			Git:     gc,
			Volumes: vols,
			// Cow stays nil until the cow_clone provider (S1-T10) is on
			// develop; with it unset, auto selects git_worktree and an
			// explicit cow_clone fails with exit 2.
			Worktree: workspace.NewWorktreeProvider(gc, vols),
			Env:      workspace.NewEnvFileWriter(cfg, vols, logger),
			Logger:   logger,
		})
		return cli.TaskBackend{
			Store: st, Workspaces: m, Repo: gc,
			Tasks: st, Lister: m, Golden: m.Golden(),
			Close: st.Close,
		}, nil
	}
}
