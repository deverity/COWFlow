package workspace

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/deverity/cowflow/internal/config"
	"github.com/deverity/cowflow/internal/domain"
	"github.com/deverity/cowflow/internal/platform"
	"github.com/deverity/cowflow/internal/store"
)

// destroyRetryDelays are the waits between removal attempts once a handle
// is stuck (sprint-1.md S1-T14 guidance). Background retries arrive with
// the daemon (Sprint 4); until then a later destroy resumes.
var destroyRetryDelays = []time.Duration{250 * time.Millisecond, time.Second, 4 * time.Second}

// stuckHint is the advice a pending_cleanup diagnostic carries. COWFlow never
// kills the processes holding the handles (SCOPE.md §18).
const stuckHint = "close editors/terminals using this folder"

// DestroyerDeps are the shared destroy's collaborators. Config, Store and Git
// are required; every other field has a production default.
type DestroyerDeps struct {
	Config *config.Config // [workspace].root, [cache].cargo_target, [git].shared_ref_namespace
	Store  Store
	Git    GitOps
	// Inspector runs the dirty check; default NewInspector(Store, Git,
	// Volumes, Config).
	Inspector *Inspector
	Volumes   Volumes // default PlatformVolumes
	// Rescuer saves unpreserved changes before destroy --force; default a
	// [SnapshotRescuer] when Git also implements [SnapshotGit], otherwise one
	// that fails every rescue, so nothing dirty is ever deleted.
	Rescuer   Rescuer
	RemoveAll func(path string) error // default os.RemoveAll; tests inject stuck handles
	Clock     domain.Clock            // default domain.SystemClock; drives the retries
	Logger    *slog.Logger            // default slog.Default()
}

// Destroyer is the destroy logic every provider delegates to (SCOPE.md §17,
// §18, §20.12; sprint-1.md S1-T14). In order, and never otherwise:
//
//  1. the path is confined to <workspace.root>/<slug>;
//  2. the dirty state is read from Git now; a Git failure refuses, even
//     with --force;
//  3. unpreserved changes refuse without --force ([UnpreservedError]); with
//     it they are rescued first, and a rescue failure refuses
//     ([ErrRescueFailed]);
//  4. the destroy is audited in the same transaction that moves the row to
//     pending_cleanup, so a crash from here on resumes instead of leaving a
//     half-deleted ready workspace;
//  5. the worktree, its directory, its admin entry and its cargo target
//     directory are removed; the task branch and the logs are kept;
//  6. the row becomes destroyed, which frees the port slot.
//
// A removal blocked by an open handle records diagnostics and the event
// workspace.pending_cleanup, retries on the clock and then returns
// [ErrPendingCleanup]; destroying again resumes at step 4.
type Destroyer struct {
	cfg       config.Config
	store     Store
	git       GitOps
	insp      *Inspector
	rescuer   Rescuer
	removeAll func(string) error
	clock     domain.Clock
	log       *slog.Logger
	goos      string
}

// NewDestroyer returns a Destroyer over d, filling the defaults. A missing
// required dependency is reported by Destroy.
func NewDestroyer(d DestroyerDeps) *Destroyer {
	x := &Destroyer{
		store: d.Store, git: d.Git, insp: d.Inspector, rescuer: d.Rescuer,
		removeAll: d.RemoveAll, clock: d.Clock, log: d.Logger, goos: runtime.GOOS,
	}
	if d.Config != nil {
		x.cfg = *d.Config
	}
	if x.insp == nil {
		x.insp = NewInspector(d.Store, d.Git, d.Volumes, d.Config)
	}
	if x.rescuer == nil {
		x.rescuer = defaultRescuer(d.Git)
	}
	if x.removeAll == nil {
		x.removeAll = os.RemoveAll
	}
	if x.clock == nil {
		x.clock = domain.SystemClock{}
	}
	if x.log == nil {
		x.log = slog.Default()
	}
	return x
}

// defaultRescuer is a SnapshotRescuer when g has the snapshot plumbing.
func defaultRescuer(g GitOps) Rescuer {
	if sg, ok := g.(SnapshotGit); ok {
		return NewSnapshotRescuer(sg, g.Head)
	}
	return noRescuer{}
}

// noRescuer fails every rescue: the git client lacks [SnapshotGit].
type noRescuer struct{}

func (noRescuer) Rescue(_ context.Context, ws Workspace, _ DirtyReport) (string, error) {
	return "", fmt.Errorf("%w: workspace %s: the git client has no snapshot operations", ErrRescueFailed, ws.ID)
}

func (d *Destroyer) ready() error {
	switch {
	case d == nil || d.store == nil:
		return fmt.Errorf("%w: destroy has no store", errNotConfigured)
	case d.git == nil:
		return fmt.Errorf("%w: destroy has no git", errNotConfigured)
	}
	return nil
}

// Destroy destroys workspace id, which provider created; see [Destroyer].
// A destroyed workspace is a no-op.
func (d *Destroyer) Destroy(ctx context.Context, provider, id string, opts DestroyOptions) error {
	if err := d.ready(); err != nil {
		return fmt.Errorf("destroy workspace %s: %w", id, err)
	}
	r, err := d.store.GetWorkspace(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("workspace %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("workspace %s: %w", id, err)
	}
	if r.Provider != provider {
		return fmt.Errorf("destroy workspace %s: provider %q is not %q: %w", id, r.Provider, provider, ErrUnsupportedProvider)
	}
	resume := false
	switch r.State {
	case domain.WorkspaceDestroyed:
		return nil
	case domain.WorkspaceReady:
	case domain.WorkspacePendingCleanup:
		resume = true // decided and audited before; finish the removal
	case domain.WorkspacePendingMount:
		return fmt.Errorf("destroy workspace %s: %w", id, ErrPendingMount)
	default:
		return fmt.Errorf("destroy workspace %s: workspace is %s", id, r.State)
	}
	ws := recordToWorkspace(r)

	// Probe first: an unmounted volume is pending_mount, not a bad path.
	vol, missing, err := d.insp.probe(ctx, ws.Path)
	if err != nil {
		return fmt.Errorf("destroy workspace %s: refusing: %w", id, err)
	}
	root, path, err := confineWorkspace(d.cfg.Workspace.Root, ws.Path)
	if err != nil {
		return fmt.Errorf("destroy workspace %s: refusing: %w", id, err)
	}
	if missing && r.Filesystem != "" && vol.Filesystem != r.Filesystem {
		// The path may be on a volume that is not mounted right now: marking
		// it destroyed would orphan whatever comes back on remount.
		return fmt.Errorf("destroy workspace %s: refusing: path %s is missing and its volume is %q, recorded %q: %w",
			id, path, vol.Filesystem, r.Filesystem, ErrPendingMount)
	}

	audit := domain.StateAudit{Action: "destroy", Force: opts.Force}
	switch {
	case resume:
		audit.Dirty = "resumed after pending_cleanup"
	case missing:
		audit.Dirty = "workspace path missing"
	default:
		rep, err := d.insp.Dirty(ctx, ws)
		if err != nil {
			return fmt.Errorf("destroy workspace %s: refusing: %w", id, err) // fail closed, even with --force
		}
		if !rep.Clean() {
			if !opts.Force {
				return &UnpreservedError{Report: rep}
			}
			ref, err := d.rescuer.Rescue(ctx, ws, rep)
			if err != nil {
				if !errors.Is(err, ErrRescueFailed) {
					err = fmt.Errorf("%w: %w", ErrRescueFailed, err)
				}
				return fmt.Errorf("destroy workspace %s: %w", id, err)
			}
			audit.RescueRef = ref
			audit.Dirty = (&UnpreservedError{Report: rep}).Error()
		}
	}

	repo, err := d.repoOf(ctx, path)
	if err != nil {
		if !resume && !missing {
			return fmt.Errorf("destroy workspace %s: refusing: %w", id, err)
		}
		// The worktree is already gone or broken: the directory is still
		// removed, but its admin entry stays until `git worktree prune`.
		d.log.Warn("cannot locate the repository of a workspace; its worktree entry stays until git worktree prune",
			"workspace_id", id, "path", path, "error", err)
		repo = ""
	}

	// Audit before any deletion, in the transaction of the state change.
	if err := d.store.SetWorkspaceState(ctx, id, domain.WorkspacePendingCleanup, domain.StateChange{Audit: &audit}); err != nil {
		return fmt.Errorf("destroy workspace %s: audit: %w", id, err)
	}
	if err := d.removeWithRetry(ctx, ws, root, path, repo); err != nil {
		return fmt.Errorf("destroy workspace %s: %w", id, err)
	}
	if err := d.store.SetWorkspaceState(ctx, id, domain.WorkspaceDestroyed, domain.StateChange{}); err != nil {
		return fmt.Errorf("destroy workspace %s: mark destroyed: %w", id, err)
	}
	d.log.Info("workspace destroyed", "project_id", ws.ProjectID, "task_id", ws.TaskID,
		"workspace_id", id, "provider", provider, "force", opts.Force, "rescue_ref", audit.RescueRef)
	return nil
}

// removeWithRetry removes the workspace; a stuck handle records
// pending_cleanup diagnostics once, then retries after each delay.
func (d *Destroyer) removeWithRetry(ctx context.Context, ws Workspace, root, path, repo string) error {
	err := d.remove(ctx, ws, root, path, repo)
	diag, stuck := d.stuck(err)
	if !stuck {
		return err
	}
	if serr := d.store.SetWorkspaceState(ctx, ws.ID, domain.WorkspacePendingCleanup, domain.StateChange{
		LastError: diag.String(),
		Event: &domain.WorkspaceEvent{Type: domain.EventWorkspacePendingCleanup, Payload: map[string]string{
			"path": diag.path, "code": diag.code, "hint": stuckHint, "error": diag.err.Error(),
		}},
	}); serr != nil {
		return errors.Join(fmt.Errorf("%w: %s", ErrPendingCleanup, diag), fmt.Errorf("record pending_cleanup: %w", serr))
	}
	d.log.Warn("workspace removal blocked by an open handle", "workspace_id", ws.ID,
		"path", diag.path, "code", diag.code, "hint", stuckHint)
	for _, delay := range destroyRetryDelays {
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: %s: %w", ErrPendingCleanup, diag, ctx.Err())
		case <-d.clock.After(delay):
		}
		err = d.remove(ctx, ws, root, path, repo)
		if err == nil {
			return nil
		}
		next, ok := d.stuck(err)
		if !ok {
			return err
		}
		diag = next
	}
	return fmt.Errorf("%w: %s (run destroy again to resume)", ErrPendingCleanup, diag)
}

// remove deletes the worktree registration, the confined directory, the
// stale admin entry and the cargo target directory. It is idempotent, so a
// retry or a resumed destroy runs it again from the start. A failed
// `worktree remove` is not final: the directory removal that follows
// reports the real cause, and prune drops the entry.
func (d *Destroyer) remove(ctx context.Context, ws Workspace, root, path, repo string) error {
	// Re-check confinement on every attempt: the tree may change between
	// retries.
	if _, p, err := confineWorkspace(root, path); err != nil || !samePath(p, path) {
		return fmt.Errorf("refusing to remove %s: %w", path, errors.Join(err, platform.ErrOutsideRoot))
	}
	present, err := exists(path)
	if err != nil {
		return err
	}
	if present && repo != "" {
		if err := d.git.WorktreeRemove(ctx, repo, path, true); err != nil {
			d.log.Debug("git worktree remove failed; removing the directory", "path", path, "error", err)
		}
	}
	if present, err = exists(path); err != nil {
		return err
	}
	if present {
		// os.RemoveAll never follows a link or a junction out of the tree.
		if err := d.removeAll(path); err != nil {
			return err
		}
	}
	if repo != "" {
		if err := d.git.WorktreePrune(ctx, repo); err != nil {
			return fmt.Errorf("prune worktrees: %w", err)
		}
	}
	return d.removeCargoTarget(ws.ID)
}

// removeCargoTarget removes <cache.cargo_target>/<workspace_id> (SCOPE.md
// §6.7), confined to cargo_target. An unset key removes nothing.
func (d *Destroyer) removeCargoTarget(id string) error {
	if d.cfg.Cache.CargoTarget == "" {
		return nil
	}
	es, err := cacheEntries(config.Cache{CargoTarget: d.cfg.Cache.CargoTarget}, id)
	if err != nil {
		return err
	}
	for _, e := range es {
		base := filepath.Dir(e.dir)
		c, err := platform.Confine(base, e.dir)
		if err != nil {
			return fmt.Errorf("refusing to remove %s: %w", e.dir, err)
		}
		if !samePath(filepath.Dir(c), mustCanonical(base)) {
			return fmt.Errorf("refusing to remove %s: it resolves to %s: %w", e.dir, c, platform.ErrOutsideRoot)
		}
		if err := d.removeAll(e.dir); err != nil {
			return err
		}
	}
	return nil
}

// repoOf returns the repository's common git directory, from which the
// worktree commands run (`git -C <dir> worktree remove|prune` works there).
// The workspace row does not record the repository.
func (d *Destroyer) repoOf(ctx context.Context, path string) (string, error) {
	cfgPath, err := d.git.GitPath(ctx, path, "config") // a common path: <common dir>/config
	if err != nil {
		return "", fmt.Errorf("locate repository: %w", err)
	}
	return filepath.Dir(cfgPath), nil
}

// stuckDiag describes a removal blocked by an open handle.
type stuckDiag struct {
	path, code string
	err        error
}

func (s stuckDiag) String() string {
	return fmt.Sprintf("%s is in use (%s): %s", s.path, s.code, stuckHint)
}

// stuck classifies err as a stuck handle: ERROR_SHARING_VIOLATION (32),
// ERROR_ACCESS_DENIED (5), ERROR_LOCK_VIOLATION (33) and
// ERROR_DIR_NOT_EMPTY (145) on Windows, EBUSY elsewhere.
func (d *Destroyer) stuck(err error) (stuckDiag, bool) {
	if err == nil {
		return stuckDiag{}, false
	}
	code, ok := stuckCode(d.goos, err)
	if !ok {
		return stuckDiag{}, false
	}
	diag := stuckDiag{code: code, err: err}
	var pe *fs.PathError
	if errors.As(err, &pe) {
		diag.path = pe.Path
	}
	return diag, true
}

// Windows error codes of a stuck handle. syscall.Errno compares by number,
// so the classification is testable on every OS.
const (
	winErrorAccessDenied     syscall.Errno = 5
	winErrorSharingViolation syscall.Errno = 32
	winErrorLockViolation    syscall.Errno = 33
	winErrorDirNotEmpty      syscall.Errno = 145
)

func stuckCode(goos string, err error) (string, bool) {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return "", false
	}
	if goos == "windows" {
		switch errno {
		case winErrorSharingViolation:
			return "ERROR_SHARING_VIOLATION", true
		case winErrorAccessDenied:
			return "ERROR_ACCESS_DENIED", true
		case winErrorLockViolation:
			return "ERROR_LOCK_VIOLATION", true
		case winErrorDirNotEmpty:
			return "ERROR_DIR_NOT_EMPTY", true
		}
		return "", false
	}
	if errno == syscall.EBUSY {
		return "EBUSY", true
	}
	return "", false
}

// confineWorkspace returns the canonical root and path of a workspace and
// refuses anything but a direct child of root that resolves to itself: no
// link as the last element, nothing outside root, never root itself.
func confineWorkspace(root, path string) (croot, p string, err error) {
	if root == "" {
		return "", "", ErrRootNotSet
	}
	croot, err = platform.Canonical(root)
	if err != nil {
		return "", "", fmt.Errorf("workspace root: %w", err)
	}
	parent, err := platform.Canonical(filepath.Dir(path))
	if err != nil {
		return "", "", fmt.Errorf("workspace path: %w", err)
	}
	lexical := filepath.Join(parent, filepath.Base(path))
	c, err := platform.Confine(croot, lexical)
	if err != nil {
		return "", "", fmt.Errorf("workspace path: %w", err)
	}
	if !samePath(c, lexical) || samePath(c, croot) || !samePath(filepath.Dir(c), croot) {
		return "", "", fmt.Errorf("%w: workspace path %q resolves to %q, not a directory directly under %q",
			platform.ErrOutsideRoot, path, c, croot)
	}
	return croot, c, nil
}

// mustCanonical is platform.Canonical, or the cleaned path when it fails;
// the comparison it feeds then fails closed.
func mustCanonical(p string) string {
	if c, err := platform.Canonical(p); err == nil {
		return c
	}
	return filepath.Clean(p)
}

// exists reports whether path exists, without following a final link.
func exists(path string) (bool, error) {
	switch _, err := os.Lstat(path); {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, err
	}
}
