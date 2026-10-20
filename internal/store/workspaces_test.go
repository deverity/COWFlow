package store

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/domain"
)

// stepClock is a settable clock safe for concurrent use.
type stepClock struct {
	mu sync.Mutex
	t  time.Time
}

func newStepClock() *stepClock {
	return &stepClock{t: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
}

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *stepClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func (c *stepClock) After(time.Duration) <-chan time.Time  { return nil }
func (c *stepClock) NewTicker(time.Duration) domain.Ticker { return nil }

// recordingSink records events; fail makes Emit return an error.
type recordingSink struct {
	mu   sync.Mutex
	got  []domain.WorkspaceEvent
	fail error
}

func (r *recordingSink) Emit(ev domain.WorkspaceEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail != nil {
		return r.fail
	}
	r.got = append(r.got, ev)
	return nil
}

type fixture struct {
	s       *Store
	clock   *stepClock
	sink    *recordingSink
	project domain.Project
	task    domain.Task
	n       int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{clock: newStepClock(), sink: &recordingSink{}}
	s, err := Open(context.Background(), dbPath(t), Options{Clock: f.clock, IDs: newTestIDs("f"), Events: f.sink})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	f.s = s
	f.project = f.addProject(t, "demo", "/repo")
	f.task = f.addTask(t, f.project.ID, "login")
	return f
}

func (f *fixture) addProject(t *testing.T, name, root string) domain.Project {
	t.Helper()
	var p domain.Project
	require.NoError(t, f.s.WithTx(context.Background(), "", func(tx *Tx) error {
		var err error
		p, err = tx.InsertProject(name, root)
		return err
	}))
	return p
}

func (f *fixture) addTask(t *testing.T, projectID, slug string) domain.Task {
	t.Helper()
	task, err := f.s.CreateTask(context.Background(), domain.Task{
		ProjectID: projectID, Slug: slug, Base: "develop", BaseSHA: "abc123",
		Branch: "feature/" + slug, Sparse: true,
	})
	require.NoError(t, err)
	return task
}

// ws returns a new workspace record with a unique op_id.
func (f *fixture) ws() domain.WorkspaceRecord {
	f.n++
	return domain.WorkspaceRecord{
		ProjectID: f.project.ID, TaskID: f.task.ID, Provider: "cow_clone", CowKind: "block",
		Filesystem: "refs", Path: fmt.Sprintf("/ws/%d", f.n), Branch: "feature/login",
		BaseSHA: "abc123", OpID: fmt.Sprintf("op-%d", f.n),
	}
}

func (f *fixture) insert(t *testing.T) domain.WorkspaceRecord {
	t.Helper()
	r, err := f.s.InsertWorkspace(context.Background(), f.ws(), 10, 40000)
	require.NoError(t, err)
	return r
}

func (f *fixture) setState(t *testing.T, id string, to domain.WorkspaceState) {
	t.Helper()
	require.NoError(t, f.s.SetWorkspaceState(context.Background(), id, to, domain.StateChange{}))
}

func TestInsertWorkspaceFillsRow(t *testing.T) {
	f := newFixture(t)
	req := f.ws()
	req.SparsePaths = []string{"services/api", "contracts"}
	req.SessionID = "ses_x"
	r, err := f.s.InsertWorkspace(context.Background(), req, 10, 40000)
	require.NoError(t, err)
	require.NotEmpty(t, r.ID)
	require.Equal(t, domain.WorkspaceCreating, r.State)
	require.Equal(t, 0, r.PortSlot)
	require.Equal(t, 40000, r.PortStart)
	require.Equal(t, 40009, r.PortEnd, "PortEnd is inclusive (U-PLAT-14)")
	got, err := f.s.GetWorkspace(context.Background(), r.ID)
	require.NoError(t, err)
	require.Equal(t, r, got)
	require.Equal(t, []string{"services/api", "contracts"}, got.SparsePaths)
	require.Equal(t, "ses_x", got.SessionID)
}

func TestInsertWorkspacePendingMount(t *testing.T) {
	f := newFixture(t)
	req := f.ws()
	req.State = domain.WorkspacePendingMount
	r, err := f.s.InsertWorkspace(context.Background(), req, 10, 40000)
	require.NoError(t, err)
	require.Equal(t, domain.WorkspacePendingMount, r.State)

	req = f.ws()
	req.State = domain.WorkspaceReady
	_, err = f.s.InsertWorkspace(context.Background(), req, 10, 40000)
	require.ErrorIs(t, err, ErrInvalidTransition)
}

func TestInsertWorkspaceLowestFreeSlot(t *testing.T) {
	f := newFixture(t)
	w0, w1, w2 := f.insert(t), f.insert(t), f.insert(t)
	require.Equal(t, []int{0, 1, 2}, []int{w0.PortSlot, w1.PortSlot, w2.PortSlot})

	// Slots 0 and 2 stay live; slot 1 is destroyed and must be reused.
	f.setState(t, w1.ID, domain.WorkspaceReady)
	f.setState(t, w1.ID, domain.WorkspaceDestroyed)
	got := f.insert(t)
	require.Equal(t, 1, got.PortSlot)
	require.Equal(t, 40010, got.PortStart)

	// A failed workspace releases its slot too (U-DATA-5).
	f.setState(t, w0.ID, domain.WorkspaceFailed)
	require.Equal(t, 0, f.insert(t).PortSlot)
	require.Equal(t, 3, f.insert(t).PortSlot)
}

func TestInsertWorkspaceSlotsArePerProject(t *testing.T) {
	f := newFixture(t)
	f.insert(t)
	other := f.addProject(t, "other", "/other")
	task := f.addTask(t, other.ID, "x")
	req := f.ws()
	req.ProjectID, req.TaskID = other.ID, task.ID
	r, err := f.s.InsertWorkspace(context.Background(), req, 10, 40000)
	require.NoError(t, err)
	require.Equal(t, 0, r.PortSlot)
}

func TestInsertWorkspaceIdempotentByOpID(t *testing.T) {
	f := newFixture(t)
	req := f.ws()
	first, err := f.s.InsertWorkspace(context.Background(), req, 10, 40000)
	require.NoError(t, err)
	f.clock.Advance(time.Hour)
	again, err := f.s.InsertWorkspace(context.Background(), req, 10, 40000)
	require.NoError(t, err)
	require.Equal(t, first, again)
	list, err := f.s.ListWorkspaces(context.Background(), domain.WorkspaceFilter{})
	require.NoError(t, err)
	require.Len(t, list, 1)
}

func TestInsertWorkspacePortRangeExhausted(t *testing.T) {
	f := newFixture(t)
	_, err := f.s.InsertWorkspace(context.Background(), f.ws(), 10, 65530)
	require.ErrorIs(t, err, ErrPortRangeExhausted)
}

func TestInsertWorkspaceRejectsBadInput(t *testing.T) {
	f := newFixture(t)
	req := f.ws()
	req.OpID = ""
	_, err := f.s.InsertWorkspace(context.Background(), req, 10, 40000)
	require.Error(t, err)
	_, err = f.s.InsertWorkspace(context.Background(), f.ws(), 0, 40000)
	require.Error(t, err)
}

func TestInsertWorkspaceConcurrent20(t *testing.T) {
	ctx := context.Background()
	path := dbPath(t)
	first := openTest(t, path, newTestIDs("s0"))
	var project domain.Project
	require.NoError(t, first.WithTx(ctx, "", func(tx *Tx) error {
		var err error
		project, err = tx.InsertProject("demo", "/repo")
		return err
	}))
	task, err := first.CreateTask(ctx, domain.Task{
		ProjectID: project.ID, Slug: "login", Base: "develop", BaseSHA: "abc", Branch: "feature/login",
	})
	require.NoError(t, err)

	const n = 20
	stores := make([]*Store, n) // each its own *sql.DB on the same file
	for i := range n {
		stores[i] = openTest(t, path, newTestIDs(fmt.Sprintf("s%d", i+1)))
	}
	var wg sync.WaitGroup
	slots := make([]int, n)
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := stores[i].InsertWorkspace(ctx, domain.WorkspaceRecord{
				ProjectID: project.ID, TaskID: task.ID, Provider: "git_worktree", CowKind: "none",
				Filesystem: "ntfs", Path: fmt.Sprintf("/ws/%d", i), Branch: "b", BaseSHA: "abc",
				OpID: fmt.Sprintf("op-%d", i),
			}, 10, 40000)
			slots[i], errs[i] = r.PortSlot, err
		}()
	}
	wg.Wait()
	seen := map[int]bool{}
	for i := range n {
		require.NoError(t, errs[i], "no SQLITE_BUSY may surface")
		require.False(t, seen[slots[i]], "slot %d allocated twice", slots[i])
		seen[slots[i]] = true
	}
	for slot := range n {
		require.True(t, seen[slot], "slots must be dense, missing %d", slot)
	}
}

func TestSetWorkspaceStateTransitions(t *testing.T) {
	ctx := context.Background()
	type step struct {
		to domain.WorkspaceState
		ok bool
	}
	cases := []struct {
		name  string
		route []domain.WorkspaceState // states to walk before the checked step
		step  step
	}{
		{"creating to ready", nil, step{domain.WorkspaceReady, true}},
		{"creating to failed", nil, step{domain.WorkspaceFailed, true}},
		{"creating to destroyed", nil, step{domain.WorkspaceDestroyed, false}},
		{"creating to pending_cleanup", nil, step{domain.WorkspacePendingCleanup, false}},
		{"ready to pending_cleanup", []domain.WorkspaceState{domain.WorkspaceReady}, step{domain.WorkspacePendingCleanup, true}},
		{"ready to destroyed", []domain.WorkspaceState{domain.WorkspaceReady}, step{domain.WorkspaceDestroyed, true}},
		{"ready to failed", []domain.WorkspaceState{domain.WorkspaceReady}, step{domain.WorkspaceFailed, false}},
		{"ready to creating", []domain.WorkspaceState{domain.WorkspaceReady}, step{domain.WorkspaceCreating, false}},
		{"pending_cleanup to destroyed", []domain.WorkspaceState{domain.WorkspaceReady, domain.WorkspacePendingCleanup}, step{domain.WorkspaceDestroyed, true}},
		{"pending_cleanup to ready", []domain.WorkspaceState{domain.WorkspaceReady, domain.WorkspacePendingCleanup}, step{domain.WorkspaceReady, false}},
		{"failed is terminal", []domain.WorkspaceState{domain.WorkspaceFailed}, step{domain.WorkspaceCreating, false}},
		{"destroyed is terminal", []domain.WorkspaceState{domain.WorkspaceReady, domain.WorkspaceDestroyed}, step{domain.WorkspaceReady, false}},
		{"destroyed is idempotent", []domain.WorkspaceState{domain.WorkspaceReady, domain.WorkspaceDestroyed}, step{domain.WorkspaceDestroyed, true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			r := f.insert(t)
			for _, st := range tc.route {
				f.setState(t, r.ID, st)
			}
			err := f.s.SetWorkspaceState(ctx, r.ID, tc.step.to, domain.StateChange{})
			if tc.step.ok {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrInvalidTransition)
			}
		})
	}
}

func TestSetWorkspaceStatePendingMountToCreating(t *testing.T) {
	f := newFixture(t)
	req := f.ws()
	req.State = domain.WorkspacePendingMount
	r, err := f.s.InsertWorkspace(context.Background(), req, 10, 40000)
	require.NoError(t, err)
	f.setState(t, r.ID, domain.WorkspaceCreating)
	got, err := f.s.GetWorkspace(context.Background(), r.ID)
	require.NoError(t, err)
	require.Equal(t, domain.WorkspaceCreating, got.State)
}

func TestSetWorkspaceStateRecordsErrorAndTime(t *testing.T) {
	f := newFixture(t)
	r := f.insert(t)
	f.clock.Advance(time.Minute)
	require.NoError(t, f.s.SetWorkspaceState(context.Background(), r.ID, domain.WorkspaceFailed,
		domain.StateChange{LastError: "clone failed: ENOSPC"}))
	got, err := f.s.GetWorkspace(context.Background(), r.ID)
	require.NoError(t, err)
	require.Equal(t, "clone failed: ENOSPC", got.LastError)
	require.Equal(t, r.UpdatedAt.Add(time.Minute), got.UpdatedAt)
	require.Equal(t, r.CreatedAt, got.CreatedAt)
}

func TestSetWorkspaceStateNotFound(t *testing.T) {
	f := newFixture(t)
	err := f.s.SetWorkspaceState(context.Background(), "wks_missing", domain.WorkspaceReady, domain.StateChange{})
	require.ErrorIs(t, err, ErrNotFound)
	_, err = f.s.GetWorkspace(context.Background(), "wks_missing")
	require.ErrorIs(t, err, ErrNotFound)
}

func (f *fixture) auditRows(t *testing.T) int {
	t.Helper()
	var n int
	require.NoError(t, f.s.db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM audit_log").Scan(&n))
	return n
}

func TestSetStateWritesAuditAndEvent(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	r := f.insert(t)
	f.setState(t, r.ID, domain.WorkspaceReady)
	require.NoError(t, f.s.SetWorkspaceState(ctx, r.ID, domain.WorkspacePendingCleanup, domain.StateChange{
		LastError: "sharing violation",
		Event: &domain.WorkspaceEvent{
			Type: domain.EventWorkspacePendingCleanup, Payload: map[string]string{"path": "a/b"},
		},
		Audit: &domain.StateAudit{Action: "destroy", ActorKind: "user", ActorID: "u1", Force: true,
			Dirty: "2 modified", RescueRef: "refs/cowflow/rescue/" + r.ID},
	}))
	require.Equal(t, 1, f.auditRows(t))
	var action, subject, detail string
	require.NoError(t, f.s.db.QueryRowContext(ctx,
		"SELECT action, subject_id, detail_json FROM audit_log").Scan(&action, &subject, &detail))
	require.Equal(t, "destroy", action)
	require.Equal(t, r.ID, subject)
	require.JSONEq(t, fmt.Sprintf(`{"project_id":%q,"task_id":%q,"from":"ready","to":"pending_cleanup",
		"force":true,"dirty":"2 modified","rescue_ref":"refs/cowflow/rescue/%s"}`,
		r.ProjectID, r.TaskID, r.ID), detail)
	require.Len(t, f.sink.got, 1)
	ev := f.sink.got[0]
	require.Equal(t, domain.EventWorkspacePendingCleanup, ev.Type)
	require.Equal(t, r.ID, ev.WorkspaceID)
	require.Equal(t, r.ProjectID, ev.ProjectID)
	require.Equal(t, r.TaskID, ev.TaskID)
}

func TestSetStateDestroyedNoopWritesNothing(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	r := f.insert(t)
	f.setState(t, r.ID, domain.WorkspaceReady)
	audit := domain.StateChange{Audit: &domain.StateAudit{Action: "destroy"}}
	require.NoError(t, f.s.SetWorkspaceState(ctx, r.ID, domain.WorkspaceDestroyed, audit))
	require.NoError(t, f.s.SetWorkspaceState(ctx, r.ID, domain.WorkspaceDestroyed, audit))
	require.Equal(t, 1, f.auditRows(t))
}

func TestSetStateAuditSameTx(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	r := f.insert(t)
	f.setState(t, r.ID, domain.WorkspaceReady)
	before, err := f.s.GetWorkspace(ctx, r.ID)
	require.NoError(t, err)

	// The failure comes after the state update and the audit insert.
	boom := errors.New("sink down")
	f.sink.fail = boom
	err = f.s.SetWorkspaceState(ctx, r.ID, domain.WorkspacePendingCleanup, domain.StateChange{
		LastError: "x",
		Event:     &domain.WorkspaceEvent{Type: domain.EventWorkspacePendingCleanup},
		Audit:     &domain.StateAudit{Action: "destroy", Force: true},
	})
	require.ErrorIs(t, err, boom)
	after, err := f.s.GetWorkspace(ctx, r.ID)
	require.NoError(t, err)
	require.Equal(t, before, after, "state, last_error and updated_at must be rolled back")
	require.Zero(t, f.auditRows(t), "the audit row must be rolled back")
}

func TestSetStateRejectsBadInput(t *testing.T) {
	f := newFixture(t)
	r := f.insert(t)
	ctx := context.Background()
	require.Error(t, f.s.SetWorkspaceState(ctx, r.ID, "bogus", domain.StateChange{}))
	require.Error(t, f.s.SetWorkspaceState(ctx, r.ID, domain.WorkspaceReady,
		domain.StateChange{Event: &domain.WorkspaceEvent{Type: "change.published"}}))
	require.Error(t, f.s.SetWorkspaceState(ctx, r.ID, domain.WorkspaceReady,
		domain.StateChange{Audit: &domain.StateAudit{}}))
	got, err := f.s.GetWorkspace(ctx, r.ID)
	require.NoError(t, err)
	require.Equal(t, domain.WorkspaceCreating, got.State)
}

func TestListWorkspacesFilters(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	a, b, c := f.insert(t), f.insert(t), f.insert(t)
	f.setState(t, b.ID, domain.WorkspaceReady)
	other := f.addTask(t, f.project.ID, "other")
	req := f.ws()
	req.TaskID = other.ID
	d, err := f.s.InsertWorkspace(ctx, req, 10, 40000)
	require.NoError(t, err)

	ids := func(f2 domain.WorkspaceFilter) []string {
		got, err := f.s.ListWorkspaces(ctx, f2)
		require.NoError(t, err)
		out := make([]string, len(got))
		for i, r := range got {
			out[i] = r.ID
		}
		return out
	}
	require.Equal(t, []string{a.ID, b.ID, c.ID, d.ID}, ids(domain.WorkspaceFilter{ProjectID: f.project.ID}))
	require.Equal(t, []string{d.ID}, ids(domain.WorkspaceFilter{TaskID: other.ID}))
	require.Equal(t, []string{b.ID}, ids(domain.WorkspaceFilter{States: []domain.WorkspaceState{domain.WorkspaceReady}}))
	require.ElementsMatch(t, []string{a.ID, c.ID, d.ID},
		ids(domain.WorkspaceFilter{States: []domain.WorkspaceState{domain.WorkspaceCreating, domain.WorkspaceFailed}}))
	require.Empty(t, ids(domain.WorkspaceFilter{ProjectID: "prj_none"}))
}

func TestLiveSlotIndexMatchesQuery(t *testing.T) {
	// The partial index and the slot query must use the same predicate.
	f := newFixture(t)
	var sqlText string
	require.NoError(t, f.s.db.QueryRowContext(context.Background(),
		"SELECT sql FROM sqlite_master WHERE name = 'workspaces_live_slot'").Scan(&sqlText))
	require.Contains(t, sqlText, "state NOT IN ('destroyed','failed')")
	require.Contains(t, liveSlotsQuery, "state NOT IN ('destroyed','failed')")
}

func TestMigrationsFromEmptyAndFromS0(t *testing.T) {
	ctx := context.Background()
	tables := func(s *Store) map[string]bool {
		rows, err := s.db.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'table'")
		require.NoError(t, err)
		defer func() { _ = rows.Close() }()
		out := map[string]bool{}
		for rows.Next() {
			var n string
			require.NoError(t, rows.Scan(&n))
			out[n] = true
		}
		require.NoError(t, rows.Err())
		return out
	}

	// From empty: every migration applies.
	empty := openTest(t, dbPath(t), newTestIDs("e"))
	v, err := empty.SchemaVersion(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, v)
	for _, name := range []string{"projects", "tasks", "workspaces", "golden_trees", "audit_log"} {
		require.True(t, tables(empty)[name], name)
	}

	// From S0: a database holding only 0001 and a project row upgrades in place.
	body, err := fs.ReadFile(migrationFS, "migrations/0001_base.sql")
	require.NoError(t, err)
	orig := migrationFS
	migrationFS = fstest.MapFS{"migrations/0001_base.sql": {Data: body}}
	p := dbPath(t)
	s0, err := Open(ctx, p, Options{IDs: newTestIDs("s0")})
	migrationFS = orig
	require.NoError(t, err)
	require.NoError(t, s0.WithTx(ctx, "", func(tx *Tx) error {
		_, err := tx.InsertProject("old", "/old")
		return err
	}))
	v, err = s0.SchemaVersion(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, v)
	require.NoError(t, s0.Close())

	up := openTest(t, p, newTestIDs("up"))
	v, err = up.SchemaVersion(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, v)
	proj, err := up.ProjectByRoot(ctx, "/old")
	require.NoError(t, err)
	require.Equal(t, "old", proj.Name)
	require.True(t, tables(up)["workspaces"])
}
