package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/deverity/cowflow/internal/domain"
	"github.com/deverity/cowflow/internal/workspace"
)

// WorkspaceAdmin is what `workspace inspect|destroy` use of the workspace
// Manager. *workspace.Manager implements it.
type WorkspaceAdmin interface {
	Inspect(ctx context.Context, id string) (*workspace.WorkspaceStatus, error)
	Destroy(ctx context.Context, id string, opts workspace.DestroyOptions) error
}

// maxShown is how many unpreserved files or commits the refusal names before
// it summarizes the rest.
const maxShown = 10

// forceHint ends the refusal of a destroy with unpreserved changes.
const forceHint = "use --force to rescue and destroy"

func newWorkspaceCmd(ios IOStreams, deps Deps, st *runState) *cobra.Command {
	list := &cobra.Command{
		Use:   "list",
		Short: "List workspaces",
		Long:  "List the workspaces of the project. Destroyed workspaces are hidden.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWorkspaceList(cmd.Context(), ios, deps, st)
		},
	}
	inspect := &cobra.Command{
		Use:   "inspect <id>",
		Short: "Inspect a workspace",
		Long: "Show a workspace as it is now: the filesystem under its path, HEAD and the " +
			"uncommitted and unpublished changes are read at call time, not from the database.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkspaceInspect(cmd.Context(), ios, deps, st, args[0])
		},
	}
	var force bool
	destroy := &cobra.Command{
		Use:   "destroy <id>",
		Short: "Destroy a workspace",
		Long: "Destroy a workspace: its directory and worktree are removed, the task branch is kept.\n\n" +
			"A workspace with uncommitted files or unpublished commits is refused (exit 4). " +
			"With --force they are first saved to refs/cowflow/rescue/<id>. " +
			"Git-ignored files are not preserved: they are neither counted nor rescued. " +
			"There is no confirmation prompt: --force is the only override.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkspaceDestroy(cmd.Context(), ios, deps, st, args[0], force)
		},
	}
	destroy.Flags().BoolVar(&force, "force", false, "rescue unpreserved changes, then destroy")

	return group("workspace", "Manage workspaces", list, inspect, destroy)
}

// workspaceID validates the shape of a workspace ID before it reaches the
// store or a path join.
func workspaceID(id string) error {
	if k, err := domain.ParseID(id); err != nil || k != domain.KindWorkspace {
		return usageErr(fmt.Errorf("%q is not a workspace id (wks_...)", id))
	}
	return nil
}

// workspaceRowDoc is one entry of the workspace list --json array.
type workspaceRowDoc struct {
	ID         string `json:"id"`
	TaskID     string `json:"task_id"`
	Provider   string `json:"provider"`
	CowKind    string `json:"cow_kind"`
	Filesystem string `json:"filesystem"`
	State      string `json:"state"`
	Branch     string `json:"branch"`
	Path       string `json:"path"`
}

func runWorkspaceList(ctx context.Context, ios IOStreams, deps Deps, st *runState) error {
	o, err := openProject(ctx, deps, "workspace list")
	if err != nil {
		return err
	}
	defer o.close()
	if o.be.Lister == nil {
		return &ExitError{Code: exitUnsupported, Err: errors.New("workspace list is not available in this build")}
	}
	list, err := o.be.Lister.List(ctx, workspace.ListFilter{
		ProjectID: o.project.ID,
		States: []domain.WorkspaceState{ // destroyed is hidden
			domain.WorkspaceCreating, domain.WorkspacePendingMount, domain.WorkspaceReady,
			domain.WorkspaceFailed, domain.WorkspacePendingCleanup,
		},
	})
	if err != nil {
		return workspaceErr(err)
	}
	docs := make([]workspaceRowDoc, 0, len(list))
	for _, w := range list {
		if w.State == domain.WorkspaceDestroyed {
			continue
		}
		docs = append(docs, workspaceRowDoc{
			ID: w.ID, TaskID: w.TaskID, Provider: w.Provider, CowKind: w.Caps.CopyOnWrite.String(),
			Filesystem: w.Caps.Filesystem, State: string(w.State), Branch: w.Branch, Path: w.Path,
		})
	}
	return printResult(ios, st.json, docs, func(w io.Writer) error {
		if len(docs) == 0 {
			_, err := io.WriteString(w, "no workspaces\n")
			return err
		}
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "ID\tTASK\tPROVIDER\tCOW\tFS\tSTATE\tBRANCH\tPATH")
		for _, d := range docs {
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				d.ID, dash(d.TaskID), dash(d.Provider), d.CowKind, dash(d.Filesystem), d.State, dash(d.Branch), d.Path)
		}
		return tw.Flush()
	})
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// workspaceDoc is the --json document of workspace inspect: the S1-T13 shape
// plus logs_dir (SCOPE.md §23 decision 42).
type workspaceDoc struct {
	ID         string    `json:"id"`
	TaskID     string    `json:"task_id"`
	Provider   string    `json:"provider"`
	CowKind    string    `json:"cow_kind"`
	Filesystem string    `json:"filesystem"`
	Path       string    `json:"path"`
	Branch     string    `json:"branch"`
	BaseSHA    string    `json:"base_sha"`
	Head       string    `json:"head"`
	State      string    `json:"state"`
	Ports      portsDoc  `json:"ports"`
	Sparse     sparseDoc `json:"sparse"`
	Dirty      dirtyDoc  `json:"dirty"`
	LogsDir    string    `json:"logs_dir"`
	Warnings   []string  `json:"warnings"`
}

type portsDoc struct {
	Start int `json:"start"`
	End   int `json:"end"` // inclusive (SCOPE.md §23 decision 45)
}

type sparseDoc struct {
	Enabled bool     `json:"enabled"`
	Paths   []string `json:"paths"`
}

type dirtyDoc struct {
	Uncommitted []string `json:"uncommitted"`
	Unpublished []string `json:"unpublished"`
}

func newWorkspaceDoc(s *workspace.WorkspaceStatus) workspaceDoc {
	return workspaceDoc{
		ID: s.ID, TaskID: s.TaskID, Provider: s.Provider, CowKind: s.CowKind.String(),
		Filesystem: s.Filesystem, Path: s.Path, Branch: s.Branch, BaseSHA: s.BaseSHA, Head: s.Head,
		State:  string(s.State),
		Ports:  portsDoc{Start: s.Ports.Start, End: s.Ports.End},
		Sparse: sparseDoc{Enabled: s.Sparse.Enabled, Paths: nonNil(s.Sparse.Paths)},
		Dirty: dirtyDoc{
			Uncommitted: nonNil(s.Dirty.Uncommitted), Unpublished: nonNil(s.Dirty.Unpublished),
		},
		LogsDir:  s.LogsDir,
		Warnings: nonNil(s.Warnings),
	}
}

// admin opens the backend and returns its workspace administration seam.
func admin(ctx context.Context, deps Deps, command string) (*openedBackend, WorkspaceAdmin, error) {
	o, err := openProject(ctx, deps, command)
	if err != nil {
		return nil, nil, err
	}
	if o.be.Admin == nil {
		o.close()
		return nil, nil, &ExitError{Code: exitUnsupported, Err: fmt.Errorf("%s is not available in this build", command)}
	}
	return o, o.be.Admin, nil
}

func runWorkspaceInspect(ctx context.Context, ios IOStreams, deps Deps, st *runState, id string) error {
	if err := workspaceID(id); err != nil {
		return err
	}
	o, adm, err := admin(ctx, deps, "workspace inspect")
	if err != nil {
		return err
	}
	defer o.close()
	status, err := adm.Inspect(ctx, id)
	// A volume that is not mounted returns the status together with the
	// error: show what is known, then fail with exit 2.
	if status == nil {
		return workspaceErr(err)
	}
	doc := newWorkspaceDoc(status)
	if perr := printResult(ios, st.json, doc, func(w io.Writer) error { return writeWorkspace(w, doc) }); perr != nil {
		return perr
	}
	if !st.json {
		for _, w := range doc.Warnings {
			_, _ = fmt.Fprintf(ios.Err, "warning: %s\n", w)
		}
	}
	if err != nil {
		return workspaceErr(err)
	}
	return nil
}

func writeWorkspace(w io.Writer, d workspaceDoc) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	row := func(k, v string) { _, _ = fmt.Fprintf(tw, "%s:\t%s\n", k, dash(v)) }
	row("id", d.ID)
	row("task", d.TaskID)
	row("state", d.State)
	row("provider", d.Provider)
	row("cow", d.CowKind)
	row("filesystem", d.Filesystem)
	row("path", d.Path)
	row("branch", d.Branch)
	row("base", d.BaseSHA)
	row("head", d.Head)
	row("ports", fmt.Sprintf("%d-%d", d.Ports.Start, d.Ports.End))
	if d.Sparse.Enabled {
		row("sparse", strings.Join(d.Sparse.Paths, ", "))
	} else {
		row("sparse", "")
	}
	row("uncommitted", strconv.Itoa(len(d.Dirty.Uncommitted)))
	row("unpublished", strconv.Itoa(len(d.Dirty.Unpublished)))
	row("logs", d.LogsDir)
	return tw.Flush()
}

func runWorkspaceDestroy(ctx context.Context, ios IOStreams, deps Deps, st *runState, id string, force bool) error {
	if err := workspaceID(id); err != nil {
		return err
	}
	o, adm, err := admin(ctx, deps, "workspace destroy")
	if err != nil {
		return err
	}
	defer o.close()

	// With --force a dirty workspace is rescued by Destroy, which does not
	// report it; read the state first to tell the user where the work went.
	var willRescue bool
	if force {
		if s, ierr := adm.Inspect(ctx, id); ierr == nil && s != nil {
			willRescue = !s.Dirty.Clean()
		}
	}
	// PENDING: the audit actor of a daemonless call is the OS user; the
	// Destroyer records no actor and DestroyOptions has no field for one
	// (AG-WS).
	if err := adm.Destroy(ctx, id, workspace.DestroyOptions{Force: force}); err != nil {
		return destroyErr(id, err)
	}
	doc := destroyDoc{ID: id, Destroyed: true}
	if willRescue {
		doc.RescueRef = workspace.RescueRef(id)
	}
	return printResult(ios, st.json, doc, func(w io.Writer) error {
		if doc.RescueRef != "" {
			_, _ = fmt.Fprintf(w, "rescued to %[1]s (restore with: git cherry-pick -n %[1]s)\n", doc.RescueRef)
		}
		_, err := fmt.Fprintf(w, "destroyed workspace %s\n", id)
		return err
	})
}

// destroyDoc is the --json document of workspace destroy.
type destroyDoc struct {
	ID        string `json:"id"`
	Destroyed bool   `json:"destroyed"`
	RescueRef string `json:"rescue_ref,omitempty"`
}

// destroyErr maps a destroy failure to its exit code: unpreserved changes
// are a policy refusal (4); a rescue failure, pending_cleanup, pending_mount,
// an unknown workspace and anything unclassified are state (2).
func destroyErr(id string, err error) error {
	var ue *workspace.UnpreservedError
	if errors.As(err, &ue) {
		return &ExitError{Code: ExitPolicy, Err: errors.New(unpreservedMessage(id, ue.Report))}
	}
	return workspaceErr(err)
}

// unpreservedMessage names the files and commits a destroy would lose, at
// most maxShown of each, then the hint.
func unpreservedMessage(id string, r workspace.DirtyReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "workspace %s has unpreserved changes; nothing was deleted", id)
	list := func(noun string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n%d %s(s):", len(items), noun)
		for i, it := range items {
			if i == maxShown {
				fmt.Fprintf(&b, "\n  ... and %d more", len(items)-maxShown)
				break
			}
			b.WriteString("\n  " + strconv.Quote(it)) // a path may hold a space or a newline
		}
	}
	list("uncommitted file", r.Uncommitted)
	list("unpublished commit", r.Unpublished)
	b.WriteString("\n" + forceHint)
	return b.String()
}
