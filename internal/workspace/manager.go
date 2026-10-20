package workspace

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/deverity/cowflow/internal/config"
	"github.com/deverity/cowflow/internal/domain"
	"github.com/deverity/cowflow/internal/platform"
	"github.com/deverity/cowflow/internal/store"
)

// Selection is the provider chosen for a create request.
type Selection struct {
	Provider WorkspaceProvider
	Caps     Capabilities
	Warning  string // non-empty when auto fell back (§1.2 "explicit warning")
}

// CreateRequest asks the Manager for a workspace for one task.
type CreateRequest struct {
	OpID      string      // idempotency key from the caller (§18); a retry returns the same workspace
	ProjectID string      // project the task belongs to
	RepoDir   string      // canonical main repository dir
	SessionID string      // empty until Sprint 3
	Provider  string      // requested provider: "auto", "cow_clone" or "git_worktree"; empty: [workspace].provider
	Task      domain.Task // stored task: slug, branch, base SHA, paths and sparse flag
}

// ListFilter selects workspaces for List. Empty fields match everything.
type ListFilter struct {
	ProjectID string
	TaskID    string
	States    []domain.WorkspaceState // empty: every state
}

// EnvWriter writes the .env.cowflow of a created workspace (SCOPE.md §6.1,
// S1-T15). Create calls it after the provider and before the row becomes
// ready; an error is a create failure with compensation.
type EnvWriter interface {
	Write(ctx context.Context, w *Workspace) error
}

// EventSink receives the workspace events the Manager emits itself: today
// only workspace.pending_mount, whose row is inserted without a state change.
// *store.Store's sink and the default (log at info) both fit.
//
// PENDING U-DATA-1: until the events/outbox tables exist (S2-E2) the event is
// not atomic with the insert.
type EventSink interface {
	Emit(ev domain.WorkspaceEvent) error
}

// DevDrives reports the observed state of the configured Dev Drive (the
// S0-E6 device layer). platform.DevDrive satisfies it; tests use the
// simulated VHDX of test/harness.
type DevDrives interface {
	Status(ctx context.Context, spec platform.DevDriveSpec) (platform.DevDriveStatus, error)
}

// Deps are the Manager's collaborators. Config, Store, Git and Worktree are
// required; every other field has a production default.
type Deps struct {
	Config   *config.Config
	Store    Store
	Git      GitOps
	Volumes  Volumes           // default PlatformVolumes
	DevDrive DevDrives         // default platform.NewDevDrive(platform.NewHost())
	Cow      WorkspaceProvider // cow_clone; nil: see CowClone
	// CowClone, when Cow is nil, builds the cow_clone provider over the OS
	// block-clone primitive and the Manager's golden cache. False with a nil
	// Cow means cow_clone is not built in.
	CowClone bool
	Worktree WorkspaceProvider // git_worktree
	Env      EnvWriter         // nil: no .env.cowflow yet (S1-T15)
	Events   EventSink         // default: log at info
	IDs      domain.IDGen      // default: ULIDs from Clock
	Clock    domain.Clock      // default domain.SystemClock
	Logger   *slog.Logger      // default slog.Default()
}

// Manager is the entry point the CLI uses to create, inspect, list and
// destroy workspaces (sprint-1.md §6.1). It owns the rules that must never
// break: auto is cow_clone then git_worktree and never projfs; no write
// before the Dev Drive mount gate; never a partial ready; never on
// main/develop; the path always confined under [workspace].root.
//
// A Manager is safe for concurrent use; concurrent creates for one OpID are
// serialized by the store's op_id uniqueness.
type Manager struct {
	cfg    config.Config
	store  Store
	git    GitOps
	vols   Volumes
	dev    DevDrives
	cow    WorkspaceProvider
	wt     WorkspaceProvider
	env    EnvWriter
	events EventSink
	ids    domain.IDGen
	clock  domain.Clock
	log    *slog.Logger
}

// managerAPI is the Manager method set agreed in sprint-1.md §6.1 (the
// golden tree accessor arrives with S1-T09). workspacetest.FakeManager has
// the same set.
type managerAPI interface {
	Select(ctx context.Context, requested string) (Selection, error)
	Create(ctx context.Context, req CreateRequest) (*Workspace, Selection, error)
	Inspect(ctx context.Context, id string) (*WorkspaceStatus, error)
	Destroy(ctx context.Context, id string, opts DestroyOptions) error
	List(ctx context.Context, f ListFilter) ([]Workspace, error)
	ExpandScope(ctx context.Context, id string, add []string) error
}

var _ managerAPI = (*Manager)(nil)

// NewManager returns a Manager over d, filling the defaults of the optional
// dependencies. A missing required dependency is reported by the first
// method that needs it, never by a panic.
func NewManager(d Deps) *Manager {
	m := &Manager{
		store: d.Store, git: d.Git, vols: d.Volumes, dev: d.DevDrive,
		cow: d.Cow, wt: d.Worktree, env: d.Env, events: d.Events,
		ids: d.IDs, clock: d.Clock, log: d.Logger,
	}
	if d.Config != nil {
		m.cfg = *d.Config
	}
	if m.vols == nil {
		m.vols = PlatformVolumes{}
	}
	if m.dev == nil {
		m.dev = platform.NewDevDrive(platform.NewHost())
	}
	if m.clock == nil {
		m.clock = domain.SystemClock{}
	}
	if m.ids == nil {
		m.ids = domain.NewULIDGen(m.clock)
	}
	if m.log == nil {
		m.log = slog.Default()
	}
	if m.events == nil {
		m.events = logEvents{log: m.log}
	}
	if m.cow == nil && d.CowClone {
		m.cow = NewCowCloneProvider(CowCloneDeps{
			Config: &m.cfg, Git: m.git, Golden: m.Golden(), Volumes: m.vols, Logger: m.log,
		}).WithDestroyer(NewDestroyer(DestroyerDeps{
			Config: &m.cfg, Store: m.store, Git: m.git, Volumes: m.vols, Clock: m.clock, Logger: m.log,
		}))
	}
	return m
}

// logEvents is the default EventSink (PENDING U-DATA-1).
type logEvents struct{ log *slog.Logger }

func (s logEvents) Emit(ev domain.WorkspaceEvent) error {
	s.log.Info("event", "type", ev.Type, "project_id", ev.ProjectID,
		"workspace_id", ev.WorkspaceID, "task_id", ev.TaskID, "payload", ev.Payload)
	return nil
}

// errNotConfigured is wrapped when a required dependency is missing.
var errNotConfigured = errors.New("workspace: manager not configured")

func (m *Manager) ready() error {
	switch {
	case m.store == nil:
		return fmt.Errorf("%w: no store", errNotConfigured)
	case m.git == nil:
		return fmt.Errorf("%w: no git", errNotConfigured)
	}
	return nil
}

// providerByName returns the built-in provider called name, or nil.
func (m *Manager) providerByName(name string) WorkspaceProvider {
	for _, p := range []WorkspaceProvider{m.cow, m.wt} {
		if p != nil && p.Name() == name {
			return p
		}
	}
	return nil
}

// Inspect returns the live status of workspace id. A workspace that is
// failed, destroyed or still pending a mount is reported from its row with a
// warning and without probing the filesystem (SCOPE.md §6.1); any other is
// inspected by its provider.
func (m *Manager) Inspect(ctx context.Context, id string) (*WorkspaceStatus, error) {
	r, err := m.get(ctx, id)
	if err != nil {
		return nil, err
	}
	switch r.State {
	case domain.WorkspaceFailed, domain.WorkspaceDestroyed, domain.WorkspacePendingMount:
		w := recordToWorkspace(r)
		warn := fmt.Sprintf("workspace is %s: filesystem not probed", r.State)
		if r.LastError != "" {
			warn += ": " + r.LastError
		}
		st := &WorkspaceStatus{Workspace: w, CowKind: w.Caps.CopyOnWrite, Warnings: []string{warn}}
		if m.cfg.Daemon.StateDir != "" {
			st.LogsDir = LogsDir(m.cfg.Daemon.StateDir, id)
		}
		return st, nil
	}
	p := m.providerByName(r.Provider)
	if p == nil {
		return nil, fmt.Errorf("inspect workspace %s: provider %q: %w", id, r.Provider, ErrUnsupportedProvider)
	}
	return p.Inspect(ctx, id)
}

// Destroy destroys workspace id through its provider. Destroying a
// destroyed workspace is a no-op; a pending_mount one is [ErrPendingMount].
// Dirty checks, rescue and pending_cleanup are the shared [Destroyer]'s,
// which every provider delegates to (S1-T14).
func (m *Manager) Destroy(ctx context.Context, id string, opts DestroyOptions) error {
	r, err := m.get(ctx, id)
	if err != nil {
		return err
	}
	switch r.State {
	case domain.WorkspaceDestroyed:
		return nil
	case domain.WorkspaceReady, domain.WorkspacePendingCleanup:
	case domain.WorkspacePendingMount:
		return fmt.Errorf("destroy workspace %s: %w", id, ErrPendingMount)
	default:
		return fmt.Errorf("destroy workspace %s: workspace is %s", id, r.State)
	}
	p := m.providerByName(r.Provider)
	if p == nil {
		return fmt.Errorf("destroy workspace %s: provider %q: %w", id, r.Provider, ErrUnsupportedProvider)
	}
	return p.Destroy(ctx, id, opts)
}

// List returns the stored workspaces matching f, oldest first.
func (m *Manager) List(ctx context.Context, f ListFilter) ([]Workspace, error) {
	if err := m.ready(); err != nil {
		return nil, err
	}
	rows, err := m.store.ListWorkspaces(ctx, domain.WorkspaceFilter{
		ProjectID: f.ProjectID, TaskID: f.TaskID, States: slices.Clone(f.States),
	})
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	out := make([]Workspace, 0, len(rows))
	for _, r := range rows {
		out = append(out, recordToWorkspace(r))
	}
	return out, nil
}

// ExpandScope adds directories to a workspace's sparse scope (SCOPE.md
// §6.6). The paths are validated first and nothing is touched when one is
// refused. A workspace that is not sparse already has every file, so the
// call is a no-op. The provider then materializes the new scope: cow_clone
// clones the new files from the golden tree while HEAD is still the base
// commit, otherwise git writes them. The MCP tool and the intent update that
// call this land in later sprints.
//
// PENDING: the workspace row's sparse_paths is not updated, because the
// store has no operation for it (AG-STORE); the scope in force is read back
// from git, so the next call stays correct.
func (m *Manager) ExpandScope(ctx context.Context, id string, add []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := SparseSet(add)
	if err != nil {
		return fmt.Errorf("expand scope of workspace %s: %w", id, err)
	}
	r, err := m.get(ctx, id)
	if err != nil {
		return err
	}
	if r.State != domain.WorkspaceReady {
		return fmt.Errorf("expand scope of workspace %s: state is %s, not ready", id, r.State)
	}
	if len(r.SparsePaths) == 0 || len(add) == 0 {
		return nil
	}
	p := m.providerByName(r.Provider)
	ex, ok := p.(scopeExpander)
	if !ok {
		return fmt.Errorf("expand scope of workspace %s: provider %q: %w", id, r.Provider, ErrUnsupportedProvider)
	}
	if err := ex.expandScope(ctx, recordToWorkspace(r), add); err != nil {
		return fmt.Errorf("expand scope of workspace %s: %w", id, err)
	}
	return nil
}

// get reads the row of workspace id, mapping a missing row to ErrNotFound.
func (m *Manager) get(ctx context.Context, id string) (domain.WorkspaceRecord, error) {
	if err := m.ready(); err != nil {
		return domain.WorkspaceRecord{}, err
	}
	r, err := m.store.GetWorkspace(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return domain.WorkspaceRecord{}, fmt.Errorf("workspace %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return domain.WorkspaceRecord{}, fmt.Errorf("workspace %s: %w", id, err)
	}
	return r, nil
}

// recordToWorkspace converts a stored row. Capabilities carry only what the
// row records (copy-on-write kind and filesystem), never more.
func recordToWorkspace(r domain.WorkspaceRecord) Workspace {
	return Workspace{
		ID: r.ID, ProjectID: r.ProjectID, TaskID: r.TaskID, SessionID: r.SessionID,
		Provider: r.Provider, Path: r.Path, Branch: r.Branch, BaseSHA: r.BaseSHA,
		Caps:      Capabilities{CopyOnWrite: parseCowKind(r.CowKind), Filesystem: r.Filesystem},
		Sparse:    SparseSpec{Enabled: len(r.SparsePaths) > 0, Paths: slices.Clone(r.SparsePaths)},
		Ports:     PortRange{Start: r.PortStart, End: r.PortEnd},
		State:     r.State,
		CreatedAt: r.CreatedAt,
	}
}

// parseCowKind is the inverse of CowKind.String for stored rows; anything
// unknown is CowNone, so a row never claims copy-on-write it did not record.
func parseCowKind(s string) CowKind {
	switch s {
	case CowBlock.String():
		return CowBlock
	case CowVirtual.String():
		return CowVirtual
	}
	return CowNone
}

// cleanupTimeout bounds compensation, which runs on a context detached from
// the caller's cancellation.
func (m *Manager) cleanupTimeout() time.Duration {
	if s := m.cfg.Scheduler.CommandTimeoutSeconds; s > 0 {
		return time.Duration(s) * time.Second
	}
	return 2 * time.Minute
}
