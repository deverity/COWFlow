package workspace

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/deverity/cowflow/internal/domain"
	"github.com/deverity/cowflow/internal/platform"
)

// protectedBranches are never a workspace's branch (SCOPE.md §24.2).
var protectedBranches = []string{"main", "develop"}

// maxLastError bounds the error text stored in workspaces.last_error.
const maxLastError = 2000

// plan is a validated create request, ready for a provider.
type plan struct {
	root, path string // canonical [workspace].root and <root>/<slug>
	baseSHA    string
	sparse     SparseSpec
	sel        Selection
}

// Create makes the workspace of req.Task (SCOPE.md §6.1, §18) and returns it
// in state ready with the provider selection, whose Warning the CLI prints.
//
// The order is fixed: validate the branch and base (no filesystem access);
// pass the Dev Drive mount gate before anything is written; confine the path
// and refuse an existing path or branch; select the provider (an explicit
// cow_clone without block clone fails here, before any row exists); insert
// the row as creating with its port slot; run the provider and the
// .env.cowflow writer; mark the row ready. Any failure after the insert is
// compensated on a detached context: the confined directory and worktree are
// removed, the branch this operation created is deleted, and the row
// becomes failed, which frees its port slot. No row is ever left ready or
// creating by a failed create.
//
// When the mount gate fails, the row is inserted as pending_mount, the
// workspace.pending_mount event is emitted, and the error wraps
// ErrPendingMount; nothing is created on disk.
//
// Create is idempotent by req.OpID: a ready workspace is returned unchanged
// and a pending_mount one is resumed. An OpID whose workspace is creating,
// failed or gone is an error; a new attempt needs a new OpID.
func (m *Manager) Create(ctx context.Context, req CreateRequest) (*Workspace, Selection, error) {
	start := m.clock.Now()
	if err := m.checkRequest(req); err != nil {
		return nil, Selection{}, err
	}
	row, found, err := m.findOp(ctx, req)
	if err != nil {
		return nil, Selection{}, err
	}
	if found && row.State != domain.WorkspacePendingMount {
		return m.existingOp(row)
	}
	t := req.Task
	if err := m.checkBranchName(ctx, t.Branch); err != nil {
		return nil, Selection{}, err
	}
	baseSHA, err := m.resolveBase(ctx, req)
	if err != nil {
		return nil, Selection{}, err
	}

	// The mount gate comes before every filesystem access below: on a
	// detached Dev Drive the mount point may be a plain NTFS folder where a
	// MkdirAll would quietly succeed (SCOPE.md §6.4, §18).
	reason, err := m.mountGate(ctx)
	if err != nil {
		return nil, Selection{}, err
	}
	if reason != "" {
		return nil, Selection{}, m.pendingMount(ctx, req, baseSHA, row, found, reason)
	}

	if found {
		return m.resume(ctx, req, row, baseSHA, start)
	}
	p, err := m.plan(ctx, req, baseSHA)
	if err != nil {
		return nil, Selection{}, err
	}
	id, err := m.ids.New(domain.KindWorkspace)
	if err != nil {
		return nil, Selection{}, fmt.Errorf("create workspace: new id: %w", err)
	}
	row, err = m.store.InsertWorkspace(ctx, m.record(req, id, p), m.cfg.Workspace.PortRangePerWorkspace, m.cfg.Workspace.PortRangeStart)
	if err != nil {
		return nil, Selection{}, fmt.Errorf("create workspace: insert: %w", err)
	}
	if row.ID != id {
		// A concurrent call with the same OpID inserted first.
		return m.existingOp(row)
	}
	return m.build(ctx, req, row, p, start)
}

// checkRequest rejects an incomplete request before anything is read.
func (m *Manager) checkRequest(req CreateRequest) error {
	if err := m.ready(); err != nil {
		return err
	}
	if m.cfg.Workspace.Root == "" {
		return ErrRootNotSet
	}
	t := req.Task
	switch {
	case req.OpID == "", req.ProjectID == "", t.ID == "", t.Branch == "":
		return errors.New("create workspace: op_id, project_id, task id and task branch are required")
	case t.ProjectID != "" && t.ProjectID != req.ProjectID:
		return fmt.Errorf("create workspace: task %s belongs to project %s, not %s", t.ID, t.ProjectID, req.ProjectID)
	case req.RepoDir == "" || !filepath.IsAbs(req.RepoDir):
		return fmt.Errorf("create workspace: repository dir %q is not absolute", req.RepoDir)
	}
	return domain.ValidateSlug(t.Slug)
}

// findOp looks for the row an earlier call with req.OpID inserted.
func (m *Manager) findOp(ctx context.Context, req CreateRequest) (domain.WorkspaceRecord, bool, error) {
	rows, err := m.store.ListWorkspaces(ctx, domain.WorkspaceFilter{ProjectID: req.ProjectID, TaskID: req.Task.ID})
	if err != nil {
		return domain.WorkspaceRecord{}, false, fmt.Errorf("create workspace: look up op %s: %w", req.OpID, err)
	}
	for _, r := range rows {
		if r.OpID == req.OpID {
			return r, true, nil
		}
	}
	return domain.WorkspaceRecord{}, false, nil
}

// existingOp answers a retry whose row is not pending_mount.
func (m *Manager) existingOp(r domain.WorkspaceRecord) (*Workspace, Selection, error) {
	switch r.State {
	case domain.WorkspaceReady:
		w := recordToWorkspace(r)
		return &w, Selection{Provider: m.providerByName(r.Provider), Caps: w.Caps}, nil
	case domain.WorkspaceFailed:
		return nil, Selection{}, fmt.Errorf("create workspace: op %s already failed (workspace %s: %s); retry with a new op id",
			r.OpID, r.ID, r.LastError)
	case domain.WorkspacePendingMount:
		return nil, Selection{}, fmt.Errorf("workspace %s: %w", r.ID, ErrPendingMount)
	default:
		return nil, Selection{}, fmt.Errorf("create workspace: op %s belongs to workspace %s, which is %s",
			r.OpID, r.ID, r.State)
	}
}

// checkBranchName refuses main/develop (ErrProtectedBranch) and any name
// git check-ref-format --branch rejects (SCOPE.md §8.1, §17, §24.2).
func (m *Manager) checkBranchName(ctx context.Context, branch string) error {
	if slices.Contains(protectedBranches, branch) {
		return fmt.Errorf("create workspace on %q: %w", branch, ErrProtectedBranch)
	}
	if err := m.git.CheckBranchName(ctx, branch); err != nil {
		return fmt.Errorf("create workspace: branch %q: %w", branch, err)
	}
	return nil
}

// resolveBase resolves the task's base to a full commit SHA. A stored
// BaseSHA must resolve to itself.
func (m *Manager) resolveBase(ctx context.Context, req CreateRequest) (string, error) {
	rev := cmp.Or(req.Task.BaseSHA, req.Task.Base)
	if rev == "" {
		return "", fmt.Errorf("create workspace: task %s has no base", req.Task.ID)
	}
	sha, err := m.git.ResolveCommit(ctx, req.RepoDir, rev)
	if err != nil {
		return "", fmt.Errorf("create workspace: base %q: %w", rev, err)
	}
	if req.Task.BaseSHA != "" && sha != req.Task.BaseSHA {
		return "", fmt.Errorf("create workspace: base %q resolves to %s", req.Task.BaseSHA, sha)
	}
	return sha, nil
}

// mountGate checks, when [devdrive].enabled, that the configured VHDX is
// attached and mounted and that [workspace].root lies on its volume, by
// volume ID: a folder mount point on NTFS exists even when the Dev Drive is
// detached, so the existence of a path proves nothing. It returns a
// non-empty reason when the gate is closed and an error only when the check
// itself failed. It reads; it never writes.
func (m *Manager) mountGate(ctx context.Context) (string, error) {
	dd := m.cfg.DevDrive
	if !dd.Enabled {
		return "", nil
	}
	st, err := m.dev.Status(ctx, platform.DevDriveSpec{VHDXPath: dd.Path, Mount: dd.Mount})
	if err != nil {
		return "", fmt.Errorf("create workspace: dev drive status: %w", err)
	}
	switch {
	case !st.Exists:
		return fmt.Sprintf("dev drive %s does not exist", dd.Path), nil
	case !st.Attached:
		return fmt.Sprintf("dev drive %s is not attached", dd.Path), nil
	case st.Volume == nil:
		return fmt.Sprintf("dev drive %s is not mounted at %s", dd.Path, dd.Mount), nil
	}
	mount := cmp.Or(st.Volume.Path, st.Mount, dd.Mount)
	dv, reason, err := m.volume(ctx, mount, "dev drive mount")
	if reason != "" || err != nil {
		return reason, err
	}
	root := m.cfg.Workspace.Root
	rv, reason, err := m.volume(ctx, root, "workspace root")
	if reason != "" || err != nil {
		return reason, err
	}
	if dv.ID == "" || rv.ID != dv.ID {
		return fmt.Sprintf("workspace root %s is on volume %q, not on the dev drive volume %q mounted at %s",
			root, rv.ID, dv.ID, mount), nil
	}
	return "", nil
}

// volume probes the volume of path for the mount gate. A path that does not
// exist or is not mounted closes the gate (reason); other errors fail it.
func (m *Manager) volume(ctx context.Context, path, what string) (platform.Volume, string, error) {
	v, err := m.vols.VolumeOf(ctx, path)
	switch {
	case err == nil:
		return v, "", nil
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, platform.ErrNotMounted):
		return platform.Volume{}, fmt.Sprintf("%s %s is not available: %v", what, path, err), nil
	}
	return platform.Volume{}, "", fmt.Errorf("create workspace: volume of %s %s: %w", what, path, err)
}

// pendingMount records a create that waits for the Dev Drive: a new row in
// state pending_mount (no directory) and the workspace.pending_mount event.
// A resumed row that still waits is left as it is.
func (m *Manager) pendingMount(ctx context.Context, req CreateRequest, baseSHA string,
	row domain.WorkspaceRecord, found bool, reason string) error {
	m.log.Warn("workspace pending mount", "project_id", req.ProjectID, "task_id", req.Task.ID,
		"op_id", req.OpID, "reason", reason)
	if found {
		return fmt.Errorf("workspace %s: %s: %w", row.ID, reason, ErrPendingMount)
	}
	path, err := lexicalPath(m.cfg.Workspace.Root, req.Task.Slug)
	if err != nil {
		return err
	}
	id, err := m.ids.New(domain.KindWorkspace)
	if err != nil {
		return fmt.Errorf("create workspace: new id: %w", err)
	}
	rec := domain.WorkspaceRecord{
		ID: id, ProjectID: req.ProjectID, TaskID: req.Task.ID, SessionID: req.SessionID,
		// The provider is selected only once the volume is there: until
		// then the row records what was requested.
		Provider: cmp.Or(req.Provider, m.cfg.Workspace.Provider),
		Path:     path, Branch: req.Task.Branch, BaseSHA: baseSHA,
		SparsePaths: m.sparse(req.Task).Paths,
		State:       domain.WorkspacePendingMount, OpID: req.OpID,
	}
	got, err := m.store.InsertWorkspace(ctx, rec, m.cfg.Workspace.PortRangePerWorkspace, m.cfg.Workspace.PortRangeStart)
	if err != nil {
		return fmt.Errorf("create workspace: insert pending_mount: %w", err)
	}
	if got.ID != id {
		_, _, err := m.existingOp(got)
		return err
	}
	ev := domain.WorkspaceEvent{
		Type: domain.EventWorkspacePendingMount, ProjectID: got.ProjectID, WorkspaceID: got.ID, TaskID: got.TaskID,
		Payload: map[string]string{
			"op_id": req.OpID, "reason": reason, "root": m.cfg.Workspace.Root,
			"vhdx": m.cfg.DevDrive.Path, "mount": m.cfg.DevDrive.Mount,
		},
	}
	pending := fmt.Errorf("workspace %s: %s: %w", got.ID, reason, ErrPendingMount)
	if err := m.events.Emit(ev); err != nil {
		return fmt.Errorf("%w (emit %s: %w)", pending, ev.Type, err)
	}
	return pending
}

// resume continues a pending_mount row whose volume is now there. The row
// becomes creating first, recording what the manager finally selected, so
// every later failure is compensated into failed and frees the port slot.
// The selection is made before the transition: a row requested with auto
// records the provider, copy-on-write kind, filesystem and canonical path
// the create will deliver.
func (m *Manager) resume(ctx context.Context, req CreateRequest, row domain.WorkspaceRecord,
	baseSHA string, start time.Time) (*Workspace, Selection, error) {
	p, err := m.plan(ctx, req, baseSHA)
	if err == nil {
		err = m.resumable(row, req, p)
	}
	if err != nil {
		return nil, Selection{}, fmt.Errorf("resume workspace %s: %w", row.ID, err)
	}
	change := domain.StateChange{
		Provider: p.sel.Provider.Name(), CowKind: p.sel.Caps.CopyOnWrite.String(),
		Filesystem: p.sel.Caps.Filesystem, Path: p.path,
	}
	if err := m.store.SetWorkspaceState(ctx, row.ID, domain.WorkspaceCreating, change); err != nil {
		return nil, Selection{}, fmt.Errorf("resume workspace %s: %w", row.ID, err)
	}
	row.Provider, row.CowKind, row.Filesystem, row.Path = change.Provider, change.CowKind, change.Filesystem, change.Path
	m.log.Info("workspace resumed after mount", "workspace_id", row.ID, "op_id", req.OpID,
		"provider", change.Provider, "cow_kind", change.CowKind, "filesystem", change.Filesystem)
	return m.build(ctx, req, row, p, start)
}

// resumable checks that the request still describes the pending_mount row:
// the same branch at the same base. The provider, kind, filesystem and path
// are not compared; the selection made now replaces them (U-PLAT-13 retry).
func (m *Manager) resumable(row domain.WorkspaceRecord, req CreateRequest, p plan) error {
	if row.Branch != req.Task.Branch || row.BaseSHA != p.baseSHA {
		return fmt.Errorf("the row records branch %q at %s, the request %q at %s",
			row.Branch, row.BaseSHA, req.Task.Branch, p.baseSHA)
	}
	return nil
}

// plan confines the path, refuses an existing path or branch and selects
// the provider. It reads the filesystem and git but writes nothing.
func (m *Manager) plan(ctx context.Context, req CreateRequest, baseSHA string) (plan, error) {
	t := req.Task
	root, path, err := workspacePath(m.cfg.Workspace.Root, t.Slug)
	if err != nil {
		return plan{}, err
	}
	exists, err := m.git.BranchExists(ctx, req.RepoDir, t.Branch)
	if err != nil {
		return plan{}, fmt.Errorf("create workspace: branch %q: %w", t.Branch, err)
	}
	if exists {
		return plan{}, fmt.Errorf("%w: %q", ErrBranchExists, t.Branch)
	}
	sel, err := m.Select(ctx, req.Provider)
	if err != nil {
		return plan{}, err
	}
	return plan{root: root, path: path, baseSHA: baseSHA, sparse: m.sparse(t), sel: sel}, nil
}

// sparse applies sparse checkout only when [workspace].sparse_by_intent,
// the task's sparse flag and its paths all ask for it (§6.6, §23 decision
// 49).
func (m *Manager) sparse(t domain.Task) SparseSpec {
	if !m.cfg.Workspace.SparseByIntent || !t.Sparse || len(t.Paths) == 0 {
		return SparseSpec{}
	}
	return SparseSpec{Enabled: true, Paths: slices.Clone(t.Paths)}
}

// record is the creating row of a planned workspace.
func (m *Manager) record(req CreateRequest, id string, p plan) domain.WorkspaceRecord {
	return domain.WorkspaceRecord{
		ID: id, ProjectID: req.ProjectID, TaskID: req.Task.ID, SessionID: req.SessionID,
		Provider: p.sel.Provider.Name(), CowKind: p.sel.Caps.CopyOnWrite.String(), Filesystem: p.sel.Caps.Filesystem,
		Path: p.path, Branch: req.Task.Branch, BaseSHA: p.baseSHA, SparsePaths: p.sparse.Paths,
		State: domain.WorkspaceCreating, OpID: req.OpID,
	}
}

// build runs the provider and the .env.cowflow writer for a creating row and
// marks it ready. Every failure is compensated before it is returned.
func (m *Manager) build(ctx context.Context, req CreateRequest, row domain.WorkspaceRecord, p plan,
	start time.Time) (w *Workspace, sel Selection, err error) {
	touched := false
	defer func() {
		if err != nil {
			err = m.compensate(ctx, req, row, p, touched, err)
			w, sel = nil, Selection{}
		}
	}()
	spec := WorkspaceSpec{
		ID: row.ID, OpID: req.OpID,
		ProjectID: req.ProjectID, TaskID: req.Task.ID, SessionID: req.SessionID,
		RepoDir: req.RepoDir, Path: p.path, Branch: req.Task.Branch, BaseSHA: p.baseSHA,
		Sparse: SparseSpec{Enabled: p.sparse.Enabled, Paths: slices.Clone(p.sparse.Paths)},
		Ports:  PortRange{Start: row.PortStart, End: row.PortEnd},
	}
	name := p.sel.Provider.Name()
	touched = true // from here on the provider may have written the path or the branch
	w, err = p.sel.Provider.Create(ctx, spec)
	if err != nil {
		return nil, Selection{}, fmt.Errorf("create workspace %s with %s: %w", row.ID, name, err)
	}
	if w == nil {
		return nil, Selection{}, fmt.Errorf("create workspace %s with %s: provider returned no workspace", row.ID, name)
	}
	w.ID, w.ProjectID, w.TaskID, w.SessionID = row.ID, req.ProjectID, req.Task.ID, req.SessionID
	w.Provider, w.Path, w.Branch, w.BaseSHA = name, p.path, req.Task.Branch, p.baseSHA
	w.Ports, w.CreatedAt = spec.Ports, row.CreatedAt
	if m.env != nil {
		if err = m.env.Write(ctx, w); err != nil {
			return nil, Selection{}, fmt.Errorf("create workspace %s: write .env.cowflow: %w", row.ID, err)
		}
	}
	// No event for ready: §9.9 defines none.
	if err = m.store.SetWorkspaceState(ctx, row.ID, domain.WorkspaceReady, domain.StateChange{}); err != nil {
		return nil, Selection{}, fmt.Errorf("create workspace %s: mark ready: %w", row.ID, err)
	}
	w.State = domain.WorkspaceReady
	m.log.Info("workspace created",
		"project_id", req.ProjectID, "task_id", req.Task.ID, "session_id", req.SessionID,
		"workspace_id", row.ID, "provider", name, "cow_kind", p.sel.Caps.CopyOnWrite.String(),
		"filesystem", p.sel.Caps.Filesystem, "duration_ms", m.clock.Now().Sub(start).Milliseconds())
	return w, p.sel, nil
}

// compensate undoes a failed create on a context detached from ctx's
// cancellation: when touched, it removes the worktree and the confined
// directory and deletes the task branch if it still points at the base;
// then it marks the row failed with cause as last_error. It returns cause,
// with any compensation failure attached.
func (m *Manager) compensate(ctx context.Context, req CreateRequest, row domain.WorkspaceRecord, p plan,
	touched bool, cause error) error {
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), m.cleanupTimeout())
	defer cancel()
	var errs []error
	if touched {
		if err := m.removeTree(cctx, req.RepoDir, p.root, p.path); err != nil {
			errs = append(errs, err)
		}
		if err := m.deleteBranch(cctx, req.RepoDir, req.Task.Branch, p.baseSHA); err != nil {
			errs = append(errs, err)
		}
	}
	last := cause.Error()
	if len(last) > maxLastError {
		last = last[:maxLastError]
	}
	if err := m.store.SetWorkspaceState(cctx, row.ID, domain.WorkspaceFailed, domain.StateChange{LastError: last}); err != nil {
		errs = append(errs, fmt.Errorf("mark failed: %w", err))
	}
	m.log.Error("workspace create failed", "project_id", req.ProjectID, "task_id", req.Task.ID,
		"workspace_id", row.ID, "op_id", req.OpID, "error", cause, "compensation_errors", len(errs))
	if len(errs) == 0 {
		return cause
	}
	return fmt.Errorf("%w (compensation: %w)", cause, errors.Join(errs...))
}

// removeTree removes the worktree registration and the directory at path,
// then prunes stale worktree metadata. It never removes a path that fails
// confinement under root, or that resolves anywhere but itself.
func (m *Manager) removeTree(ctx context.Context, repo, root, path string) error {
	if root == "" || path == "" {
		return nil // never planned: nothing was created
	}
	var errs []error
	wts, err := m.git.WorktreeList(ctx, repo)
	if err != nil {
		errs = append(errs, fmt.Errorf("list worktrees: %w", err))
	}
	for _, wt := range wts {
		if samePath(wt.Path, path) {
			if err := m.git.WorktreeRemove(ctx, repo, path, true); err != nil {
				errs = append(errs, fmt.Errorf("remove worktree %s: %w", path, err))
			}
			break
		}
	}
	switch c, err := platform.Confine(root, path); {
	case err != nil:
		errs = append(errs, fmt.Errorf("refusing to remove %s: %w", path, err))
	case !samePath(c, path) || samePath(c, root):
		errs = append(errs, fmt.Errorf("refusing to remove %s: it resolves to %s", path, c))
	default:
		if err := os.RemoveAll(path); err != nil {
			errs = append(errs, fmt.Errorf("remove %s: %w", path, err))
		}
	}
	if err := m.git.WorktreePrune(ctx, repo); err != nil {
		errs = append(errs, fmt.Errorf("prune worktrees: %w", err))
	}
	return errors.Join(errs...)
}

// deleteBranch deletes branch when it exists and still points at sha. Plan
// refused an existing branch, so a branch found here was created by this
// operation; one that moved is left alone and reported.
func (m *Manager) deleteBranch(ctx context.Context, repo, branch, sha string) error {
	exists, err := m.git.BranchExists(ctx, repo, branch)
	if err != nil {
		return fmt.Errorf("branch %q: %w", branch, err)
	}
	if !exists {
		return nil
	}
	if err := m.git.DeleteBranchIfAt(ctx, repo, branch, sha); err != nil {
		return fmt.Errorf("delete branch %q: %w", branch, err)
	}
	return nil
}
