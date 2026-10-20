package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/deverity/cowflow/internal/config"
	"github.com/deverity/cowflow/internal/platform"
	"github.com/deverity/cowflow/internal/store"
)

// projectView is the registered project in the --json output of init.
type projectView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	RepoRoot string `json:"repo_root"`
	Created  bool   `json:"created"` // false: the project was already registered
}

// initProjectResult is the --json output of a plain `init`.
type initProjectResult struct {
	Project       projectView `json:"project"`
	Config        string      `json:"config"`
	ConfigCreated bool        `json:"config_created"`
	Database      string      `json:"database"`
	Warnings      []string    `json:"warnings"`
}

// runInitProject is a plain `cowflow init`: it creates
// <repo>/.cowflow/config.toml with the defaults when it is missing (an
// existing file is never touched), creates the store at config.DBPath and
// registers the project. It is idempotent. .mcp.json, the lead token and
// .gitignore are S3-E3.
func runInitProject(ctx context.Context, ios IOStreams, deps Deps, st *runState) error {
	if deps.ConfigPath == nil {
		return &ExitError{Code: exitUnsupported, Err: errors.New("init is not available in this build")}
	}
	cfgPath, err := deps.ConfigPath()
	if err != nil {
		return &ExitError{Code: ExitState, Err: fmt.Errorf("config location: %w", err)}
	}
	created, err := ensureConfig(cfgPath)
	if err != nil {
		return err
	}
	proj, err := registerProject(ctx, cfgPath)
	if err != nil {
		return err
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return configErr(err)
	}
	res := initProjectResult{
		Project: proj, Config: cfgPath, ConfigCreated: created,
		Database: config.DBPath(cfgPath, cfg), Warnings: []string{},
	}
	return printResult(ios, st.json, res, func(w io.Writer) error {
		verb := "already registered"
		if proj.Created {
			verb = "registered"
		}
		cfgVerb := "kept"
		if created {
			cfgVerb = "created"
		}
		_, err := fmt.Fprintf(w, "project %s %s\n  id        %s\n  root      %s\n  config    %s (%s)\n  database  %s\n",
			proj.Name, verb, proj.ID, proj.RepoRoot, cfgPath, cfgVerb, res.Database)
		return err
	})
}

// ensureConfig writes config.Default() to cfgPath unless the file exists. It
// reports whether it wrote the file. A directory without .git is refused:
// init would otherwise create .cowflow in an arbitrary directory.
func ensureConfig(cfgPath string) (bool, error) {
	switch _, err := os.Stat(cfgPath); {
	case err == nil:
		return false, nil
	case !errors.Is(err, fs.ErrNotExist):
		return false, &ExitError{Code: ExitState, Err: fmt.Errorf("config %s: %w", cfgPath, err)}
	}
	root := projectRoot(cfgPath)
	if _, err := os.Lstat(filepath.Join(root, ".git")); err != nil {
		return false, &ExitError{Code: ExitState, Err: fmt.Errorf(
			"%s is not a Git repository: run cowflow init inside one", root)}
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o750); err != nil {
		return false, &ExitError{Code: ExitState, Err: fmt.Errorf("config directory: %w", err)}
	}
	if err := config.Save(cfgPath, config.Default()); err != nil {
		return false, &ExitError{Code: ExitState, Err: fmt.Errorf("write config: %w", err)}
	}
	return true, nil
}

// registerProject creates the store at config.DBPath when missing and
// inserts the projects row of the repository that holds cfgPath unless it is
// already there. The project is named after the repository directory.
func registerProject(ctx context.Context, cfgPath string) (projectView, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return projectView{}, configErr(err)
	}
	repoDir, err := platform.Canonical(projectRoot(cfgPath))
	if err != nil {
		return projectView{}, &ExitError{Code: ExitState, Err: err}
	}
	// A name that cannot be a branch component (trunk preset) is reported by
	// task create (exit 1); the gitflow preset does not use the name.
	name := filepath.Base(repoDir)
	if err := os.MkdirAll(config.StatePath(cfgPath, cfg), 0o750); err != nil {
		return projectView{}, &ExitError{Code: ExitState, Err: fmt.Errorf("create state dir: %w", err)}
	}
	s, err := store.Open(ctx, config.DBPath(cfgPath, cfg), store.Options{})
	if err != nil {
		return projectView{}, &ExitError{Code: ExitState, Err: fmt.Errorf("open store: %w", err)}
	}
	defer func() { _ = s.Close() }() // a close failure cannot change the outcome

	p, err := s.ProjectByRoot(ctx, repoDir)
	if err == nil {
		return projectView{ID: p.ID, Name: p.Name, RepoRoot: p.RepoRoot}, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return projectView{}, &ExitError{Code: ExitState, Err: err}
	}
	err = s.WithTx(ctx, "", func(tx *store.Tx) (e error) {
		p, e = tx.InsertProject(name, repoDir)
		return e
	})
	created := true
	if errors.Is(err, store.ErrConflict) {
		// Another init registered it between the lookup and the insert.
		created = false
		if p, err = s.ProjectByRoot(ctx, repoDir); err != nil {
			return projectView{}, &ExitError{Code: ExitState, Err: err}
		}
	} else if err != nil {
		return projectView{}, &ExitError{Code: ExitState, Err: fmt.Errorf("register project: %w", err)}
	}
	return projectView{ID: p.ID, Name: p.Name, RepoRoot: p.RepoRoot, Created: created}, nil
}
