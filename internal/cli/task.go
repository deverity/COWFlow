package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	slashpath "path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/deverity/cowflow/internal/config"
	"github.com/deverity/cowflow/internal/domain"
	"github.com/deverity/cowflow/internal/platform"
	"github.com/deverity/cowflow/internal/store"
	"github.com/deverity/cowflow/internal/workspace"
)

func newTaskCmd(ios IOStreams, deps Deps, st *runState) *cobra.Command {
	return group("task", "Manage tasks",
		newTaskCreateCmd(ios, deps, st),
		stub("list", "List tasks", cobra.NoArgs),
		stub("show <id>", "Show a task", cobra.ExactArgs(1)),
		stub("pause <id>", "Pause a task", cobra.ExactArgs(1)),
		stub("resume <id>", "Resume a task", cobra.ExactArgs(1)),
	)
}

// TaskStore is the part of the SQLite store that task create uses.
// *store.Store implements it.
type TaskStore interface {
	ProjectByRoot(ctx context.Context, root string) (domain.Project, error)
	CreateTask(ctx context.Context, t domain.Task) (domain.Task, error)
	GetTaskBySlug(ctx context.Context, projectID, slug string) (domain.Task, error)
}

// TaskWorkspaces creates the workspace of a task. *workspace.Manager
// implements it.
type TaskWorkspaces interface {
	Create(ctx context.Context, req workspace.CreateRequest) (*workspace.Workspace, workspace.Selection, error)
	List(ctx context.Context, f workspace.ListFilter) ([]workspace.Workspace, error)
}

// TaskRepo is the part of the repository client that task create uses to
// check its input before any row is written. The git client implements it.
type TaskRepo interface {
	ResolveCommit(ctx context.Context, repo, rev string) (string, error)
	CheckBranchName(ctx context.Context, name string) error
	BranchExists(ctx context.Context, repo, name string) (bool, error)
}

// TaskBackend is what task create needs to run daemonless (SCOPE.md §5.3):
// the opened store, the workspace manager over it and the repository client.
// Close releases the store; it may be nil.
type TaskBackend struct {
	Store      TaskStore
	Workspaces TaskWorkspaces
	Repo       TaskRepo
	// Tasks, Lister and Golden serve `open` and `golden`; task create does
	// not use them. *store.Store, *workspace.Manager and the Manager's
	// *workspace.GoldenCache implement them.
	Tasks  TaskLookup
	Lister WorkspaceLister
	Golden GoldenCache
	Close  func() error
}

// unavailableFlags are accepted by the command tree but belong to later
// epics. Using one is an error, never silently ignored.
var unavailableFlags = []string{"contract", "depends", "runner", "watch", "no-contract"}

// Policy of the gitflow preset (SCOPE.md §24.2).
const (
	defaultKind = "feature"
	hotfixKind  = "hotfix"
	mainBase    = "main"
	developBase = "develop"
)

type taskCreateFlags struct {
	base, title, kind string
	paths             []string
	sparse, force     bool
}

func newTaskCreateCmd(ios IOStreams, deps Deps, st *runState) *cobra.Command {
	var fl taskCreateFlags
	cmd := &cobra.Command{
		Use:   "create <slug>",
		Short: "Create a task and its workspace",
		Long: "Create a task and its isolated workspace from the base branch.\n\n" +
			"The slug becomes the workspace directory name and part of the task branch. " +
			"Under the gitflow preset --kind (feature, bugfix, chore, hotfix or release; default feature) " +
			"picks the branch <kind>/<slug>; a --base other than the kind's own needs --force, " +
			"which is audited.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, name := range unavailableFlags {
				if cmd.Flags().Changed(name) {
					return usageErr(fmt.Errorf("--%s is not available yet", name))
				}
			}
			return runTaskCreate(cmd.Context(), ios, deps, st, args[0], fl)
		},
	}
	f := cmd.Flags()
	f.StringVar(&fl.base, "base", "", "base branch (default [project].default_base)")
	f.StringVar(&fl.title, "title", "", "task title")
	f.String("contract", "", "contract file the task implements (not available yet)")
	f.StringSliceVar(&fl.paths, "paths", nil, "repo-relative directories the task works in (repeatable or comma-separated)")
	f.StringSlice("depends", nil, "tasks this task depends on (not available yet)")
	f.StringVar(&fl.kind, "kind", "", "branch kind under the gitflow preset: feature|bugfix|chore|hotfix|release (default feature)")
	f.String("watch", "", "what a watchdog task watches (not available yet)")
	f.String("runner", "", "runner for the task (not available yet)")
	f.BoolVar(&fl.force, "force", false, "allow a --base other than the kind's own under gitflow (audited)")
	f.Bool("no-contract", false, "run without the dependency's contract (not available yet)")
	f.BoolVar(&fl.sparse, "sparse", true, "check out only the --paths and the contracts directory")
	return cmd
}

// taskDoc is the --json document of task create.
type taskDoc struct {
	Task       taskDocTask      `json:"task"`
	Workspace  taskDocWorkspace `json:"workspace"`
	Warnings   []string         `json:"warnings"`
	DurationMS int64            `json:"duration_ms"`
}

type taskDocTask struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
}

type taskDocWorkspace struct {
	ID         string            `json:"id"`
	Path       string            `json:"path"`
	Provider   string            `json:"provider"`
	CowKind    workspace.CowKind `json:"cow_kind"`
	Filesystem string            `json:"filesystem"`
	Branch     string            `json:"branch"`
	BaseSHA    string            `json:"base_sha"`
	Ports      taskDocPorts      `json:"ports"`
}

// taskDocPorts is the inclusive port range of the workspace (SCOPE.md §23
// decision 45).
type taskDocPorts struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

func runTaskCreate(ctx context.Context, ios IOStreams, deps Deps, st *runState, slug string, fl taskCreateFlags) error {
	started := now(deps)
	if err := domain.ValidateSlug(slug); err != nil {
		return usageErr(err)
	}
	paths, err := normalizePaths(fl.paths)
	if err != nil {
		return usageErr(err)
	}
	cfgPath, err := deps.ConfigPath()
	if err != nil {
		return &ExitError{Code: ExitState, Err: err}
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return configErr(err)
	}
	if deps.OpenTasks == nil {
		return &ExitError{Code: exitUnsupported, Err: errors.New("task create is not available in this build")}
	}
	be, err := deps.OpenTasks(ctx, cfg, cfgPath)
	if err != nil {
		return &ExitError{Code: ExitState, Err: fmt.Errorf("open store: %w", err)}
	}
	if be.Close != nil {
		defer func() { _ = be.Close() }() // a close failure cannot change the outcome
	}

	// The config sits at <repo>/.cowflow/config.toml (SPEC-GAPS U-API-1).
	repoDir, err := platform.Canonical(filepath.Dir(filepath.Dir(cfgPath)))
	if err != nil {
		return &ExitError{Code: ExitState, Err: err}
	}
	proj, err := be.Store.ProjectByRoot(ctx, repoDir)
	if errors.Is(err, store.ErrNotFound) {
		return &ExitError{Code: ExitState, Err: fmt.Errorf("project %s is not initialized: run cowflow init", repoDir)}
	}
	if err != nil {
		return &ExitError{Code: ExitState, Err: err}
	}

	var warnings []string
	base, rule := baseFor(cfg, fl)
	if rule.violated {
		if !fl.force {
			return &ExitError{Code: ExitPolicy, Err: fmt.Errorf(
				"base %q is not the base of a %s branch (%q); use --force to override (audited)",
				base, rule.kind, rule.want)}
		}
		// PENDING: the --force override is audited (SCOPE.md §24.2), but the
		// store has no general audit entry point yet; until it has one the
		// override is reported to the caller only.
		warnings = append(warnings, fmt.Sprintf("--force: base %q instead of %q for a %s branch", base, rule.want, rule.kind))
	}

	namer, err := domain.NewBranchNamer(cfg.Conventions.Preset)
	if err != nil {
		return &ExitError{Code: ExitUsage, Err: err}
	}
	branch, err := namer.TaskBranch(proj.Name, fl.kind, slug)
	if err != nil {
		return branchNameErr(err)
	}
	if err := be.Repo.CheckBranchName(ctx, branch); err != nil {
		return usageErr(fmt.Errorf("branch %q: %w", branch, err))
	}
	baseSHA, err := be.Repo.ResolveCommit(ctx, repoDir, base)
	if err != nil {
		return usageErr(fmt.Errorf("base %q: %w", base, err))
	}
	exists, err := be.Repo.BranchExists(ctx, repoDir, branch)
	if err != nil {
		return &ExitError{Code: exitUnclassified, Err: fmt.Errorf("branch %q: %w", branch, err)}
	}
	if exists {
		return &ExitError{Code: ExitConflict, Err: fmt.Errorf("branch %q already exists", branch)}
	}

	opID, err := domain.NewULIDGen(domain.SystemClock{}).New(domain.KindOperation)
	if err != nil {
		return &ExitError{Code: exitUnclassified, Err: err}
	}
	task, err := createOrReuseTask(ctx, be, domain.Task{
		ProjectID: proj.ID, Slug: slug, Title: fl.title, Kind: taskKind(cfg, fl.kind),
		Base: base, BaseSHA: baseSHA, Branch: branch, Paths: paths, Sparse: fl.sparse,
	})
	if err != nil {
		return taskCreateErr(err)
	}
	ws, sel, err := be.Workspaces.Create(ctx, workspace.CreateRequest{
		OpID: opID, ProjectID: proj.ID, RepoDir: repoDir, Task: task,
	})
	if err != nil {
		return workspaceErr(err)
	}
	if sel.Warning != "" {
		warnings = append(warnings, sel.Warning)
	}
	for _, w := range warnings {
		// stderr only, so the --json document on stdout stays parseable.
		_, _ = fmt.Fprintf(ios.Err, "warning: %s\n", w)
	}
	elapsed := now(deps).Sub(started)
	doc := taskDoc{
		Task: taskDocTask{ID: task.ID, Slug: task.Slug},
		Workspace: taskDocWorkspace{
			ID: ws.ID, Path: ws.Path, Provider: ws.Provider, CowKind: ws.Caps.CopyOnWrite,
			Filesystem: ws.Caps.Filesystem, Branch: ws.Branch, BaseSHA: ws.BaseSHA,
			Ports: taskDocPorts{Start: ws.Ports.Start, End: ws.Ports.End},
		},
		Warnings:   append([]string{}, warnings...),
		DurationMS: elapsed.Milliseconds(),
	}
	return printResult(ios, st.json, doc, func(w io.Writer) error {
		return writeTaskHuman(w, task, ws, elapsed)
	})
}

// createOrReuseTask inserts t, or returns the task row an earlier failed
// `task create` left behind for the same slug.
//
// The task row is written before Manager.Create, which needs its ID; when
// the Manager then fails (pending mount, no block clone, a Git error) the
// row stays and would turn every retry into a slug conflict. The store has
// no way to delete a task (PENDING for AG-STORE: DeleteTask), so the retry is
// made safe here instead: a row in state created whose workspaces are all
// failed, and whose fields equal what this call would insert, is the
// leftover of a failed attempt and is reused. Anything else is a real
// conflict: a live or destroyed workspace, or different options.
func createOrReuseTask(ctx context.Context, be TaskBackend, t domain.Task) (domain.Task, error) {
	created, err := be.Store.CreateTask(ctx, t)
	if !errors.Is(err, store.ErrConflict) {
		return created, err
	}
	old, getErr := be.Store.GetTaskBySlug(ctx, t.ProjectID, t.Slug)
	if getErr != nil || old.State != domain.TaskStateCreated {
		return domain.Task{}, err // report the original conflict
	}
	ws, listErr := be.Workspaces.List(ctx, workspace.ListFilter{ProjectID: t.ProjectID, TaskID: old.ID})
	if listErr != nil {
		return domain.Task{}, fmt.Errorf("check the workspaces of task %q: %w", t.Slug, listErr)
	}
	for _, w := range ws {
		if w.State != domain.WorkspaceFailed {
			return domain.Task{}, err
		}
	}
	if !sameTaskRequest(old, t) {
		return domain.Task{}, fmt.Errorf("%w: task %q was left by an earlier failed create with other options; repeat that command line",
			store.ErrConflict, t.Slug)
	}
	return old, nil
}

// sameTaskRequest reports whether a stored task was created from the same
// request as t.
func sameTaskRequest(old, t domain.Task) bool {
	return old.Title == t.Title && old.Kind == t.Kind && old.Base == t.Base &&
		old.BaseSHA == t.BaseSHA && old.Branch == t.Branch && old.Sparse == t.Sparse &&
		slices.Equal(old.Paths, t.Paths)
}

func now(deps Deps) time.Time {
	if deps.Now != nil {
		return deps.Now()
	}
	return time.Now()
}

func writeTaskHuman(w io.Writer, t domain.Task, ws *workspace.Workspace, elapsed time.Duration) error {
	sparse := "off"
	if ws.Sparse.Enabled {
		sparse = strings.Join(ws.Sparse.Paths, ", ")
	}
	lines := []string{
		fmt.Sprintf("task %s created", t.Slug),
		fmt.Sprintf("  workspace  %s  %s", ws.ID, ws.Path),
		fmt.Sprintf("  provider   %s (%s, %s)", ws.Provider, ws.Caps.CopyOnWrite, ws.Caps.Filesystem),
		fmt.Sprintf("  branch     %s   base %s @ %s", ws.Branch, t.Base, shortSHA(ws.BaseSHA)),
		fmt.Sprintf("  sparse     %s", sparse),
		fmt.Sprintf("  ports      %d-%d", ws.Ports.Start, ws.Ports.End),
		fmt.Sprintf("  created in %.2fs", elapsed.Seconds()),
	}
	_, err := io.WriteString(w, strings.Join(lines, "\n")+"\n")
	return err
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// baseRule reports an explicit --base that departs from the gitflow rule.
type baseRule struct {
	violated   bool
	kind, want string
}

// baseFor returns the base ref and, under the gitflow preset, whether an
// explicit --base departs from the kind's own base (SCOPE.md §24.2: hotfix
// off main, everything else off develop). Without --base,
// [project].default_base applies, except that a gitflow hotfix starts from
// main.
func baseFor(cfg *config.Config, fl taskCreateFlags) (string, baseRule) {
	gitflow := cfg.Conventions.Preset == domain.PresetGitflow
	kind := fl.kind
	if kind == "" {
		kind = defaultKind
	}
	want := developBase
	if kind == hotfixKind {
		want = mainBase
	}
	switch {
	case fl.base != "":
		return fl.base, baseRule{violated: gitflow && fl.base != want, kind: kind, want: want}
	case gitflow && kind == hotfixKind:
		return want, baseRule{}
	}
	return cfg.Project.DefaultBase, baseRule{}
}

// taskKind is the kind stored with the task: only the gitflow preset has
// kinds, and its default is feature.
func taskKind(cfg *config.Config, kind string) string {
	if cfg.Conventions.Preset != domain.PresetGitflow {
		return ""
	}
	if kind == "" {
		return defaultKind
	}
	return kind
}

// normalizePaths turns --paths values into slash-separated, repo-relative,
// duplicate-free directories. Anything that could leave the repository is a
// usage error.
func normalizePaths(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	for _, raw := range in {
		p := strings.ReplaceAll(strings.TrimSpace(raw), `\`, "/")
		switch {
		case p == "":
			return nil, errors.New("--paths: empty path")
		case strings.HasPrefix(p, "/") || (len(p) > 1 && p[1] == ':'):
			return nil, fmt.Errorf("--paths: %q must be relative to the repository", raw)
		case strings.ContainsFunc(p, func(r rune) bool { return r < 0x20 || r == 0x7f }):
			return nil, fmt.Errorf("--paths: %q contains a control character", raw)
		}
		c := slashpath.Clean(p)
		if c == "." || c == ".." || strings.HasPrefix(c, "../") {
			return nil, fmt.Errorf("--paths: %q must name a directory inside the repository", raw)
		}
		if !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	return out, nil
}

// branchNameErr maps BranchNamer errors (SPEC-GAPS C2): a bad slug, project
// name or kind is usage (1); a name outside the gitflow preset is policy (4).
func branchNameErr(err error) error {
	if errors.Is(err, domain.ErrNonConformingBranch) {
		return &ExitError{Code: ExitPolicy, Err: err}
	}
	return &ExitError{Code: ExitUsage, Err: err}
}

func taskCreateErr(err error) error {
	switch {
	case errors.Is(err, store.ErrConflict):
		return &ExitError{Code: ExitConflict, Err: err}
	case errors.Is(err, domain.ErrInvalidSlug):
		return &ExitError{Code: ExitUsage, Err: err}
	}
	return &ExitError{Code: exitUnclassified, Err: err}
}

// workspaceErr maps the workspace sentinels to the §14 exit codes.
func workspaceErr(err error) error {
	code := exitUnclassified
	switch {
	case errors.Is(err, domain.ErrInvalidSlug):
		code = ExitUsage
	case errors.Is(err, workspace.ErrProtectedBranch):
		code = ExitPolicy
	case errors.Is(err, workspace.ErrPathExists), errors.Is(err, workspace.ErrBranchExists):
		code = ExitConflict
	case errors.Is(err, workspace.ErrRootNotSet), errors.Is(err, workspace.ErrPendingMount),
		errors.Is(err, workspace.ErrCloneUnsupported), errors.Is(err, workspace.ErrUnsupportedProvider),
		errors.Is(err, fs.ErrNotExist):
		code = ExitState
	}
	return &ExitError{Code: code, Err: err}
}
