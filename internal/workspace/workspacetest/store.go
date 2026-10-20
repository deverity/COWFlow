package workspacetest

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/deverity/cowflow/internal/domain"
	"github.com/deverity/cowflow/internal/store"
)

// maxPort is the highest TCP port a port slot may reach.
const maxPort = 65535

// AuditRecord is one audit row MemStore recorded with a state change.
type AuditRecord struct {
	WorkspaceID string
	From, To    domain.WorkspaceState
	Audit       domain.StateAudit
}

// MemStore is an in-memory workspace.Store with the semantics of
// *store.Store: the same port slot allocation, op_id idempotency, U-DATA-5
// state transitions, golden tree claims (U-PLAT-13) and sentinel errors
// (store.ErrNotFound, store.ErrConflict, store.ErrInvalidTransition,
// store.ErrPortRangeExhausted). Events and audit rows are recorded instead of
// written; read them with Events and Audits.
//
// Clock and IDs default to the system clock and a ULID generator. Errs
// injects an error by method name (e.g. "SetWorkspaceState"); the method
// then changes nothing.
type MemStore struct {
	Clock domain.Clock
	IDs   domain.IDGen
	Errs  map[string]error

	mu         sync.Mutex
	workspaces map[string]domain.WorkspaceRecord
	golden     map[goldenKey]domain.GoldenTree
	tasks      map[taskKey]domain.Task
	events     []domain.WorkspaceEvent
	audits     []AuditRecord
}

type goldenKey struct{ project, sha string }

type taskKey struct{ project, slug string }

// NewMemStore returns an empty MemStore reading time from clock.
func NewMemStore(clock domain.Clock) *MemStore {
	return &MemStore{Clock: clock, IDs: domain.NewULIDGen(clock)}
}

// lock takes the lock, fills the defaults and returns the injected error
// for method, if any. The caller must unlock.
func (s *MemStore) lock(method string) error {
	s.mu.Lock()
	if s.Clock == nil {
		s.Clock = domain.SystemClock{}
	}
	if s.IDs == nil {
		s.IDs = domain.NewULIDGen(s.Clock)
	}
	if s.workspaces == nil {
		s.workspaces = map[string]domain.WorkspaceRecord{}
		s.golden = map[goldenKey]domain.GoldenTree{}
		s.tasks = map[taskKey]domain.Task{}
	}
	return s.Errs[method]
}

func (s *MemStore) now() time.Time { return s.Clock.Now().UTC() }

// Events returns a copy of the events emitted with state changes.
func (s *MemStore) Events() []domain.WorkspaceEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.events)
}

// Audits returns a copy of the audit rows written with state changes.
func (s *MemStore) Audits() []AuditRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.audits)
}

// InsertWorkspace inserts r in state creating (the default) or
// pending_mount with the lowest free port slot; a known OpID returns the
// existing row unchanged.
func (s *MemStore) InsertWorkspace(ctx context.Context, r domain.WorkspaceRecord, perWS, start int) (domain.WorkspaceRecord, error) {
	if err := ctx.Err(); err != nil {
		return domain.WorkspaceRecord{}, err
	}
	err := s.lock("InsertWorkspace")
	defer s.mu.Unlock()
	if err != nil {
		return domain.WorkspaceRecord{}, err
	}
	switch {
	case r.OpID == "" || r.ProjectID == "" || r.TaskID == "":
		return domain.WorkspaceRecord{}, errors.New("memstore: insert workspace: op_id, project_id and task_id are required")
	case perWS < 1 || start < 0:
		return domain.WorkspaceRecord{}, fmt.Errorf("memstore: insert workspace: invalid port range (per workspace %d, start %d)", perWS, start)
	}
	if r.State == "" {
		r.State = domain.WorkspaceCreating
	}
	if r.State != domain.WorkspaceCreating && r.State != domain.WorkspacePendingMount {
		return domain.WorkspaceRecord{}, fmt.Errorf("%w: a new workspace cannot start in state %q", store.ErrInvalidTransition, r.State)
	}
	for _, w := range s.workspaces {
		if w.OpID == r.OpID {
			return cloneRecord(w), nil
		}
	}
	used := map[int]bool{}
	for _, w := range s.workspaces {
		if w.ProjectID == r.ProjectID && w.State.Live() {
			used[w.PortSlot] = true
		}
	}
	slot := 0
	for used[slot] {
		slot++
	}
	r.PortSlot = slot
	r.PortStart = start + slot*perWS
	r.PortEnd = r.PortStart + perWS - 1
	if r.PortEnd > maxPort {
		return domain.WorkspaceRecord{}, fmt.Errorf("%w: slot %d would end at port %d", store.ErrPortRangeExhausted, slot, r.PortEnd)
	}
	if r.ID == "" {
		if r.ID, err = s.IDs.New(domain.KindWorkspace); err != nil {
			return domain.WorkspaceRecord{}, fmt.Errorf("memstore: new workspace id: %w", err)
		}
	}
	if _, dup := s.workspaces[r.ID]; dup {
		return domain.WorkspaceRecord{}, fmt.Errorf("%w: workspace %s exists", store.ErrConflict, r.ID)
	}
	now := s.now()
	r.CreatedAt, r.UpdatedAt = now, now
	r.LastError = ""
	r.SparsePaths = slices.Clone(r.SparsePaths)
	if r.SparsePaths == nil {
		r.SparsePaths = []string{}
	}
	s.workspaces[r.ID] = r
	return cloneRecord(r), nil
}

// allowedTransitions mirrors the U-DATA-5 table of *store.Store.
var allowedTransitions = map[domain.WorkspaceState][]domain.WorkspaceState{
	domain.WorkspacePendingMount:   {domain.WorkspaceCreating},
	domain.WorkspaceCreating:       {domain.WorkspaceReady, domain.WorkspaceFailed},
	domain.WorkspaceReady:          {domain.WorkspacePendingCleanup, domain.WorkspaceDestroyed},
	domain.WorkspacePendingCleanup: {domain.WorkspacePendingCleanup, domain.WorkspaceDestroyed},
}

// SetWorkspaceState moves workspace id to state to and records c's event
// and audit row. destroyed -> destroyed is a no-op.
func (s *MemStore) SetWorkspaceState(ctx context.Context, id string, to domain.WorkspaceState, c domain.StateChange) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := s.lock("SetWorkspaceState")
	defer s.mu.Unlock()
	if err != nil {
		return err
	}
	selection := c.Provider != "" || c.CowKind != "" || c.Filesystem != "" || c.Path != ""
	switch {
	case !to.Valid():
		return fmt.Errorf("memstore: set workspace state: unknown state %q", to)
	case c.Audit != nil && c.Audit.Action == "":
		return errors.New("memstore: set workspace state: audit action is required")
	case c.Event != nil && c.Event.Type != domain.EventWorkspacePendingCleanup &&
		c.Event.Type != domain.EventWorkspacePendingMount:
		return fmt.Errorf("memstore: set workspace state: event type %q is not a workspace event", c.Event.Type)
	}
	cur, ok := s.workspaces[id]
	if !ok {
		return fmt.Errorf("%w: workspace %q", store.ErrNotFound, id)
	}
	if cur.State == domain.WorkspaceDestroyed && to == domain.WorkspaceDestroyed {
		return nil
	}
	if !slices.Contains(allowedTransitions[cur.State], to) {
		return fmt.Errorf("%w: workspace %s: %s -> %s", store.ErrInvalidTransition, id, cur.State, to)
	}
	from := cur.State
	if selection && (from != domain.WorkspacePendingMount || to != domain.WorkspaceCreating) {
		return fmt.Errorf("%w: workspace %s: provider selection is only recorded on pending_mount -> creating, not %s -> %s",
			store.ErrInvalidTransition, id, from, to)
	}
	// An empty selection field keeps the stored value.
	cur.Provider = cmp.Or(c.Provider, cur.Provider)
	cur.CowKind = cmp.Or(c.CowKind, cur.CowKind)
	cur.Filesystem = cmp.Or(c.Filesystem, cur.Filesystem)
	cur.Path = cmp.Or(c.Path, cur.Path)
	cur.State, cur.LastError, cur.UpdatedAt = to, c.LastError, s.now()
	s.workspaces[id] = cur
	if c.Audit != nil {
		s.audits = append(s.audits, AuditRecord{WorkspaceID: id, From: from, To: to, Audit: *c.Audit})
	}
	if c.Event != nil {
		ev := *c.Event
		ev.ProjectID = cmp.Or(ev.ProjectID, cur.ProjectID)
		ev.WorkspaceID = cmp.Or(ev.WorkspaceID, cur.ID)
		ev.TaskID = cmp.Or(ev.TaskID, cur.TaskID)
		s.events = append(s.events, ev)
	}
	return nil
}

// GetWorkspace returns the workspace with the ID, or store.ErrNotFound.
func (s *MemStore) GetWorkspace(ctx context.Context, id string) (domain.WorkspaceRecord, error) {
	if err := ctx.Err(); err != nil {
		return domain.WorkspaceRecord{}, err
	}
	err := s.lock("GetWorkspace")
	defer s.mu.Unlock()
	if err != nil {
		return domain.WorkspaceRecord{}, err
	}
	r, ok := s.workspaces[id]
	if !ok {
		return domain.WorkspaceRecord{}, fmt.Errorf("%w: workspace %q", store.ErrNotFound, id)
	}
	return cloneRecord(r), nil
}

// ListWorkspaces returns the workspaces matching f, oldest first.
func (s *MemStore) ListWorkspaces(ctx context.Context, f domain.WorkspaceFilter) ([]domain.WorkspaceRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	err := s.lock("ListWorkspaces")
	defer s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	var out []domain.WorkspaceRecord
	for _, r := range s.workspaces {
		if (f.ProjectID == "" || r.ProjectID == f.ProjectID) &&
			(f.TaskID == "" || r.TaskID == f.TaskID) &&
			(len(f.States) == 0 || slices.Contains(f.States, r.State)) {
			out = append(out, cloneRecord(r))
		}
	}
	slices.SortFunc(out, func(a, b domain.WorkspaceRecord) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	})
	return out, nil
}

func cloneRecord(r domain.WorkspaceRecord) domain.WorkspaceRecord {
	r.SparsePaths = slices.Clone(r.SparsePaths)
	return r
}

// ClaimGolden decides who builds the golden tree of (projectID, sha), with
// the rules of *store.Store.ClaimGolden.
func (s *MemStore) ClaimGolden(ctx context.Context, projectID, sha, opID string, staleAfter time.Duration) (domain.GoldenClaim, error) {
	if err := ctx.Err(); err != nil {
		return domain.GoldenClaim{}, err
	}
	err := s.lock("ClaimGolden")
	defer s.mu.Unlock()
	if err != nil {
		return domain.GoldenClaim{}, err
	}
	if projectID == "" || sha == "" || opID == "" {
		return domain.GoldenClaim{}, errors.New("memstore: claim golden: project, sha and op_id are required")
	}
	if staleAfter <= 0 {
		return domain.GoldenClaim{}, errors.New("memstore: claim golden: staleAfter must be positive")
	}
	now := s.now()
	k := goldenKey{projectID, sha}
	g, ok := s.golden[k]
	switch {
	case !ok:
		id, err := s.IDs.New(domain.KindGoldenTree)
		if err != nil {
			return domain.GoldenClaim{}, fmt.Errorf("memstore: new golden tree id: %w", err)
		}
		g = domain.GoldenTree{
			ID: id, ProjectID: projectID, BaseSHA: sha, State: domain.GoldenBuilding,
			BuilderOpID: opID, CreatedAt: now, LastUsedAt: now, UpdatedAt: now,
		}
	case g.State == domain.GoldenReady:
		g.InUse++
		g.LastUsedAt, g.UpdatedAt = now, now
		s.golden[k] = g
		return domain.GoldenClaim{Action: domain.GoldenActionReady, Tree: g}, nil
	case g.State == domain.GoldenFailed,
		g.State == domain.GoldenBuilding && (g.BuilderOpID == opID || !g.UpdatedAt.Add(staleAfter).After(now)):
		g.State, g.BuilderOpID, g.LastError, g.UpdatedAt = domain.GoldenBuilding, opID, "", now
	default:
		return domain.GoldenClaim{Action: domain.GoldenActionWait, Tree: g}, nil
	}
	s.golden[k] = g
	return domain.GoldenClaim{Action: domain.GoldenActionBuild, Tree: g}, nil
}

// updateGolden applies fn to the golden row when it is in one of the from
// states: a missing row is store.ErrNotFound, another state
// store.ErrInvalidTransition.
func (s *MemStore) updateGolden(ctx context.Context, method, projectID, sha string, fn func(g *domain.GoldenTree), from ...domain.GoldenState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := s.lock(method)
	defer s.mu.Unlock()
	if err != nil {
		return err
	}
	k := goldenKey{projectID, sha}
	g, ok := s.golden[k]
	if !ok {
		return fmt.Errorf("%w: golden tree %s", store.ErrNotFound, sha)
	}
	if !slices.Contains(from, g.State) {
		return fmt.Errorf("%w: golden tree %s is %s", store.ErrInvalidTransition, sha, g.State)
	}
	fn(&g)
	s.golden[k] = g
	return nil
}

// FinishGolden marks a building tree ready and records its size.
func (s *MemStore) FinishGolden(ctx context.Context, projectID, sha string, sizeBytes int64, files int) error {
	return s.updateGolden(ctx, "FinishGolden", projectID, sha, func(g *domain.GoldenTree) {
		g.State, g.SizeBytes, g.FileCount, g.LastError, g.UpdatedAt = domain.GoldenReady, sizeBytes, files, "", s.now()
	}, domain.GoldenBuilding)
}

// FailGolden marks a building tree failed with reason.
func (s *MemStore) FailGolden(ctx context.Context, projectID, sha, reason string) error {
	return s.updateGolden(ctx, "FailGolden", projectID, sha, func(g *domain.GoldenTree) {
		g.State, g.LastError, g.UpdatedAt = domain.GoldenFailed, reason, s.now()
	}, domain.GoldenBuilding)
}

// UseGolden adds delta (+1 or -1) to in_use, never below zero; +1 needs a
// ready tree.
func (s *MemStore) UseGolden(ctx context.Context, projectID, sha string, delta int) error {
	if delta != 1 && delta != -1 {
		return fmt.Errorf("memstore: use golden: delta must be +1 or -1, got %d", delta)
	}
	from := []domain.GoldenState{domain.GoldenBuilding, domain.GoldenReady, domain.GoldenFailed, domain.GoldenDeleting}
	if delta > 0 {
		from = []domain.GoldenState{domain.GoldenReady}
	}
	return s.updateGolden(ctx, "UseGolden", projectID, sha, func(g *domain.GoldenTree) {
		g.InUse = max(0, g.InUse+delta)
		g.LastUsedAt = s.now()
	}, from...)
}

// ListGolden returns the project's golden trees, least recently used first.
func (s *MemStore) ListGolden(ctx context.Context, projectID string) ([]domain.GoldenTree, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	err := s.lock("ListGolden")
	defer s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	var out []domain.GoldenTree
	for k, g := range s.golden {
		if k.project == projectID {
			out = append(out, g)
		}
	}
	slices.SortFunc(out, func(a, b domain.GoldenTree) int {
		return cmp.Or(a.LastUsedAt.Compare(b.LastUsedAt), cmp.Compare(a.BaseSHA, b.BaseSHA))
	})
	return out, nil
}

// MarkGoldenDeleting moves a ready tree with in_use = 0 to deleting and
// reports true; otherwise it changes nothing and reports false.
func (s *MemStore) MarkGoldenDeleting(ctx context.Context, projectID, sha string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	err := s.lock("MarkGoldenDeleting")
	defer s.mu.Unlock()
	if err != nil {
		return false, err
	}
	k := goldenKey{projectID, sha}
	g, ok := s.golden[k]
	if !ok {
		return false, fmt.Errorf("%w: golden tree %s", store.ErrNotFound, sha)
	}
	if g.State != domain.GoldenReady || g.InUse > 0 {
		return false, nil
	}
	g.State, g.UpdatedAt = domain.GoldenDeleting, s.now()
	s.golden[k] = g
	return true, nil
}

// DeleteGolden removes a deleting or failed row; a missing row is not an
// error.
func (s *MemStore) DeleteGolden(ctx context.Context, projectID, sha string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := s.lock("DeleteGolden")
	defer s.mu.Unlock()
	if err != nil {
		return err
	}
	k := goldenKey{projectID, sha}
	g, ok := s.golden[k]
	if !ok {
		return nil
	}
	if g.State != domain.GoldenDeleting && g.State != domain.GoldenFailed {
		return fmt.Errorf("%w: golden tree %s is %s", store.ErrInvalidTransition, sha, g.State)
	}
	delete(s.golden, k)
	return nil
}

// ResetStaleGoldenInUse sets in_use to 0 on the project's trees with
// in_use > 0 and last_used_at before cutoff and returns their base SHAs in
// order.
func (s *MemStore) ResetStaleGoldenInUse(ctx context.Context, projectID string, cutoff time.Time) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	err := s.lock("ResetStaleGoldenInUse")
	defer s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	var shas []string
	for k, g := range s.golden {
		if k.project == projectID && g.InUse > 0 && g.LastUsedAt.Before(cutoff) {
			g.InUse, g.UpdatedAt = 0, s.now()
			s.golden[k] = g
			shas = append(shas, g.BaseSHA)
		}
	}
	slices.Sort(shas)
	return shas, nil
}

// CreateTask inserts a task in state created; a duplicate (project, slug)
// is store.ErrConflict.
func (s *MemStore) CreateTask(ctx context.Context, t domain.Task) (domain.Task, error) {
	if err := ctx.Err(); err != nil {
		return domain.Task{}, err
	}
	err := s.lock("CreateTask")
	defer s.mu.Unlock()
	if err != nil {
		return domain.Task{}, err
	}
	if err := domain.ValidateSlug(t.Slug); err != nil {
		return domain.Task{}, err
	}
	if t.ProjectID == "" || t.Base == "" || t.BaseSHA == "" || t.Branch == "" {
		return domain.Task{}, errors.New("memstore: create task: project, base, base_sha and branch are required")
	}
	k := taskKey{t.ProjectID, t.Slug}
	if _, dup := s.tasks[k]; dup {
		return domain.Task{}, fmt.Errorf("%w: task %q already exists in project %s", store.ErrConflict, t.Slug, t.ProjectID)
	}
	if t.ID == "" {
		if t.ID, err = s.IDs.New(domain.KindTask); err != nil {
			return domain.Task{}, fmt.Errorf("memstore: new task id: %w", err)
		}
	}
	t.State = domain.TaskStateCreated
	t.CreatedAt = s.now()
	t.Paths = slices.Clone(t.Paths)
	s.tasks[k] = t
	t.Paths = slices.Clone(t.Paths)
	return t, nil
}

// GetTaskBySlug returns the task with the slug, or store.ErrNotFound.
func (s *MemStore) GetTaskBySlug(ctx context.Context, projectID, slug string) (domain.Task, error) {
	if err := ctx.Err(); err != nil {
		return domain.Task{}, err
	}
	err := s.lock("GetTaskBySlug")
	defer s.mu.Unlock()
	if err != nil {
		return domain.Task{}, err
	}
	t, ok := s.tasks[taskKey{projectID, slug}]
	if !ok {
		return domain.Task{}, fmt.Errorf("%w: task %q", store.ErrNotFound, slug)
	}
	t.Paths = slices.Clone(t.Paths)
	return t, nil
}

// DeleteTask removes the task and its failed workspace rows. A task with a
// workspace row in any other state, destroyed included, is refused with
// store.ErrTaskHasWorkspaces and nothing changes; an unknown task of the
// project is store.ErrNotFound.
func (s *MemStore) DeleteTask(ctx context.Context, projectID, taskID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := s.lock("DeleteTask")
	defer s.mu.Unlock()
	if err != nil {
		return err
	}
	var key taskKey
	found := false
	for k, t := range s.tasks {
		if t.ID == taskID && t.ProjectID == projectID {
			key, found = k, true
			break
		}
	}
	if !found {
		return fmt.Errorf("%w: task %q", store.ErrNotFound, taskID)
	}
	blocking := 0
	for _, r := range s.workspaces {
		if r.TaskID == taskID && r.State != domain.WorkspaceFailed {
			blocking++
		}
	}
	if blocking > 0 {
		return fmt.Errorf("%w: task %s has %d workspace(s) that are not failed", store.ErrTaskHasWorkspaces, taskID, blocking)
	}
	for id, r := range s.workspaces {
		if r.TaskID == taskID {
			delete(s.workspaces, id)
		}
	}
	delete(s.tasks, key)
	return nil
}

// UpdateWorkspaceSparsePaths replaces the sparse paths of a ready workspace;
// nil is stored as an empty list. A missing workspace is store.ErrNotFound;
// any other state is store.ErrInvalidTransition and keeps the stored paths.
func (s *MemStore) UpdateWorkspaceSparsePaths(ctx context.Context, workspaceID string, paths []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := s.lock("UpdateWorkspaceSparsePaths")
	defer s.mu.Unlock()
	if err != nil {
		return err
	}
	r, ok := s.workspaces[workspaceID]
	if !ok {
		return fmt.Errorf("%w: workspace %q", store.ErrNotFound, workspaceID)
	}
	if r.State != domain.WorkspaceReady {
		return fmt.Errorf("%w: workspace %s is %s, sparse paths change only when ready", store.ErrInvalidTransition, workspaceID, r.State)
	}
	r.SparsePaths = slices.Clone(paths)
	if r.SparsePaths == nil {
		r.SparsePaths = []string{}
	}
	r.UpdatedAt = s.now()
	s.workspaces[workspaceID] = r
	return nil
}
