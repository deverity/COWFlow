package workspacetest_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/domain"
	"github.com/deverity/cowflow/internal/git"
	"github.com/deverity/cowflow/internal/platform"
	"github.com/deverity/cowflow/internal/store"
	"github.com/deverity/cowflow/internal/workspace"
	"github.com/deverity/cowflow/internal/workspace/workspacetest"
	"github.com/deverity/cowflow/test/harness"
)

// managerAPI is the Manager method set agreed in sprint-1.md §6.1 (Golden
// excluded: GoldenCache lands with S1-T09). S1-T03 adds the same check for
// *workspace.Manager.
type managerAPI interface {
	Select(ctx context.Context, requested string) (workspace.Selection, error)
	Create(ctx context.Context, req workspace.CreateRequest) (*workspace.Workspace, workspace.Selection, error)
	Inspect(ctx context.Context, id string) (*workspace.WorkspaceStatus, error)
	Destroy(ctx context.Context, id string, opts workspace.DestroyOptions) error
	List(ctx context.Context, f workspace.ListFilter) ([]workspace.Workspace, error)
	ExpandScope(ctx context.Context, id string, add []string) error
}

var _ managerAPI = (*workspacetest.FakeManager)(nil)

const sha1 = "0123456789abcdef0123456789abcdef01234567"

func writeFile(t *testing.T, path, data string, mode os.FileMode, mtime time.Time) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(data), mode))
	require.NoError(t, os.Chmod(path, mode))
	require.NoError(t, os.Chtimes(path, mtime, mtime))
}

func TestFakeClonerCopiesBytesModeAndMtime(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "src"), filepath.Join(dir, "dst")
	mtime := time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)
	writeFile(t, src, "hello", 0o750, mtime)

	var c workspacetest.FakeCloner
	require.NoError(t, c.CloneFile(context.Background(), src, dst))

	b, err := os.ReadFile(dst) //nolint:gosec // G304: test temp path
	require.NoError(t, err)
	assert.Equal(t, "hello", string(b))
	fi, err := os.Stat(dst)
	require.NoError(t, err)
	assert.True(t, fi.ModTime().Equal(mtime), "mtime %v", fi.ModTime())
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o750), fi.Mode().Perm())
	}
	assert.Equal(t, 1, c.Calls())

	err = c.CloneFile(context.Background(), src, dst)
	require.ErrorIs(t, err, fs.ErrExist, "dst is never overwritten")
	b, err = os.ReadFile(dst) //nolint:gosec // G304: test temp path
	require.NoError(t, err)
	assert.Equal(t, "hello", string(b))
}

func TestFakeClonerFailAfter(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	writeFile(t, src, "x", 0o644, time.Now())
	c := &workspacetest.FakeCloner{FailAfter: 2}
	for i, name := range []string{"a", "b", "c", "d"} {
		dst := filepath.Join(dir, name)
		err := c.CloneFile(context.Background(), src, dst)
		if i < 2 {
			require.NoError(t, err)
			continue
		}
		require.ErrorIs(t, err, syscall.ENOSPC)
		assert.NoFileExists(t, dst)
	}

	boom := errors.New("boom")
	every := &workspacetest.FakeCloner{Err: boom}
	require.ErrorIs(t, every.CloneFile(context.Background(), src, filepath.Join(dir, "e")), boom)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, (&workspacetest.FakeCloner{}).CloneFile(ctx, src, filepath.Join(dir, "f")), context.Canceled)
	assert.NoFileExists(t, filepath.Join(dir, "f"))
}

func TestFakeVolumes(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "vol")
	sub := filepath.Join(root, "dev")
	v := workspacetest.NewFakeVolumes(
		platform.Volume{Filesystem: "ext4", ID: "1", MountPath: root},
		platform.Volume{Filesystem: "refs", ID: "2", MountPath: sub, DevDrive: true},
	)
	got, err := v.VolumeOf(ctx, filepath.Join(sub, "ws", "auth"))
	require.NoError(t, err)
	assert.Equal(t, "refs", got.Filesystem, "longest mount path wins")
	got, err = v.VolumeOf(ctx, root+"x")
	require.ErrorIs(t, err, fs.ErrNotExist, "a sibling with a common prefix is not under the mount")
	assert.Empty(t, got.Filesystem)

	same, err := v.SameVolume(ctx, filepath.Join(sub, "golden"), filepath.Join(sub, "ws"))
	require.NoError(t, err)
	assert.True(t, same)
	same, err = v.SameVolume(ctx, filepath.Join(root, "golden"), filepath.Join(sub, "ws"))
	require.NoError(t, err)
	assert.False(t, same)

	v.Unmount(sub)
	_, err = v.VolumeOf(ctx, filepath.Join(sub, "ws"))
	require.ErrorIs(t, err, fs.ErrNotExist)
	v.Mount(sub)
	v.Set(platform.Volume{Filesystem: "ntfs", ID: "2", MountPath: sub})
	got, err = v.VolumeOf(ctx, filepath.Join(sub, "ws"))
	require.NoError(t, err)
	assert.Equal(t, "ntfs", got.Filesystem)
}

func TestFakeGitRecordsAndScripts(t *testing.T) {
	ctx := context.Background()
	repo, wt := "/repo", "/ws/auth"
	boom := errors.New("export failed")
	g := &workspacetest.FakeGit{
		Errs:     map[string]error{"ExportTree": boom},
		Statuses: map[string][]git.StatusEntry{wt: {{Kind: git.StatusUntracked, XY: "??", Path: "new.txt"}}},
	}
	sha, err := g.ResolveCommit(ctx, repo, sha1)
	require.NoError(t, err)
	assert.Equal(t, sha1, sha)
	_, err = g.ResolveCommit(ctx, repo, "nope")
	require.ErrorIs(t, err, workspacetest.ErrFakeGit)

	require.NoError(t, g.WorktreeAdd(ctx, repo, wt, git.WorktreeAddOptions{NewBranch: "feature/auth", Commitish: sha1, NoCheckout: true}))
	require.ErrorIs(t, g.WorktreeAdd(ctx, repo, "/ws/b", git.WorktreeAddOptions{NewBranch: "feature/auth", Commitish: sha1}), workspacetest.ErrFakeGit)
	ok, err := g.BranchExists(ctx, repo, "feature/auth")
	require.NoError(t, err)
	assert.True(t, ok)
	head, err := g.Head(ctx, wt)
	require.NoError(t, err)
	assert.Equal(t, sha1, head)
	st, err := g.Status(ctx, wt)
	require.NoError(t, err)
	assert.Len(t, st, 1)
	require.ErrorIs(t, g.ExportTree(ctx, repo, sha1, "/g", "/g.index"), boom)

	require.NoError(t, g.DeleteBranchIfAt(ctx, repo, "feature/auth", "ffff"), "a moved branch is kept")
	ok, _ = g.BranchExists(ctx, repo, "feature/auth")
	assert.True(t, ok)
	require.NoError(t, g.DeleteBranchIfAt(ctx, repo, "feature/auth", sha1))
	ok, _ = g.BranchExists(ctx, repo, "feature/auth")
	assert.False(t, ok)

	require.NoError(t, g.WorktreeRemove(ctx, repo, wt, true))
	wts, err := g.WorktreeList(ctx, repo)
	require.NoError(t, err)
	assert.Empty(t, wts)

	assert.Equal(t, 2, g.Count("WorktreeAdd"))
	assert.Equal(t, 1, g.Count("ExportTree"))
	calls := g.Calls()
	require.NotEmpty(t, calls)
	assert.Equal(t, "ResolveCommit", calls[0].Method)
	assert.Equal(t, []any{repo, sha1}, calls[0].Args)
}

func TestFakeGitHookCountsConcurrentCalls(t *testing.T) {
	var mu sync.Mutex
	seen := 0
	g := &workspacetest.FakeGit{Hook: func(c workspacetest.Call) error {
		mu.Lock()
		defer mu.Unlock()
		if c.Method == "ExportTree" {
			seen++
		}
		return nil
	}}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { assert.NoError(t, g.ExportTree(context.Background(), "/r", sha1, "/g", "/g.index")) })
	}
	wg.Wait()
	assert.Equal(t, 20, seen)
	assert.Equal(t, 20, g.Count("ExportTree"))
}

func newRecord(op string) domain.WorkspaceRecord {
	return domain.WorkspaceRecord{OpID: op, ProjectID: "prj_1", TaskID: "tsk_1", Provider: "git_worktree"}
}

func TestMemStorePortSlotsAndIdempotency(t *testing.T) {
	ctx := context.Background()
	s := workspacetest.NewMemStore(harness.NewClock(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)))
	a, err := s.InsertWorkspace(ctx, newRecord("op_a"), 10, 40000)
	require.NoError(t, err)
	assert.Equal(t, domain.WorkspaceCreating, a.State)
	assert.Equal(t, 40000, a.PortStart)
	assert.Equal(t, 40009, a.PortEnd, "inclusive end")
	kind, err := domain.ParseID(a.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.KindWorkspace, kind)

	again, err := s.InsertWorkspace(ctx, newRecord("op_a"), 10, 40000)
	require.NoError(t, err)
	assert.Equal(t, a.ID, again.ID, "idempotent by op_id")

	b, err := s.InsertWorkspace(ctx, newRecord("op_b"), 10, 40000)
	require.NoError(t, err)
	assert.Equal(t, 1, b.PortSlot)

	require.NoError(t, s.SetWorkspaceState(ctx, a.ID, domain.WorkspaceFailed, domain.StateChange{LastError: "disk full"}))
	c, err := s.InsertWorkspace(ctx, newRecord("op_c"), 10, 40000)
	require.NoError(t, err)
	assert.Equal(t, 0, c.PortSlot, "a failed workspace frees its slot")

	_, err = s.InsertWorkspace(ctx, newRecord("op_d"), 10, 65530)
	require.ErrorIs(t, err, store.ErrPortRangeExhausted)

	r := newRecord("op_e")
	r.State = domain.WorkspaceReady
	_, err = s.InsertWorkspace(ctx, r, 10, 40000)
	require.ErrorIs(t, err, store.ErrInvalidTransition)
}

func TestMemStoreTransitionsEventsAudit(t *testing.T) {
	ctx := context.Background()
	s := &workspacetest.MemStore{}
	w, err := s.InsertWorkspace(ctx, newRecord("op_a"), 10, 40000)
	require.NoError(t, err)

	require.ErrorIs(t, s.SetWorkspaceState(ctx, w.ID, domain.WorkspacePendingCleanup, domain.StateChange{}), store.ErrInvalidTransition)
	require.NoError(t, s.SetWorkspaceState(ctx, w.ID, domain.WorkspaceReady, domain.StateChange{}))
	require.NoError(t, s.SetWorkspaceState(ctx, w.ID, domain.WorkspacePendingCleanup, domain.StateChange{
		Event: &domain.WorkspaceEvent{Type: domain.EventWorkspacePendingCleanup, Payload: map[string]string{"path": "x"}},
	}))
	require.NoError(t, s.SetWorkspaceState(ctx, w.ID, domain.WorkspaceDestroyed, domain.StateChange{
		Audit: &domain.StateAudit{Action: "destroy", Force: true},
	}))
	require.NoError(t, s.SetWorkspaceState(ctx, w.ID, domain.WorkspaceDestroyed, domain.StateChange{}), "idempotent")

	evs := s.Events()
	require.Len(t, evs, 1)
	assert.Equal(t, w.ID, evs[0].WorkspaceID)
	assert.Equal(t, "prj_1", evs[0].ProjectID)
	audits := s.Audits()
	require.Len(t, audits, 1)
	assert.Equal(t, domain.WorkspacePendingCleanup, audits[0].From)

	got, err := s.GetWorkspace(ctx, w.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.WorkspaceDestroyed, got.State)
	_, err = s.GetWorkspace(ctx, "wks_missing")
	require.ErrorIs(t, err, store.ErrNotFound)

	live, err := s.ListWorkspaces(ctx, domain.WorkspaceFilter{States: []domain.WorkspaceState{domain.WorkspaceReady}})
	require.NoError(t, err)
	assert.Empty(t, live)

	boom := errors.New("db down")
	s.Errs = map[string]error{"GetWorkspace": boom}
	_, err = s.GetWorkspace(ctx, w.ID)
	require.ErrorIs(t, err, boom)
}

func TestMemStoreGoldenClaims(t *testing.T) {
	ctx := context.Background()
	clock := harness.NewClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	s := workspacetest.NewMemStore(clock)
	c, err := s.ClaimGolden(ctx, "prj_1", sha1, "op_1", time.Minute)
	require.NoError(t, err)
	assert.Equal(t, domain.GoldenActionBuild, c.Action)
	c, err = s.ClaimGolden(ctx, "prj_1", sha1, "op_2", time.Minute)
	require.NoError(t, err)
	assert.Equal(t, domain.GoldenActionWait, c.Action)
	clock.Advance(2 * time.Minute)
	c, err = s.ClaimGolden(ctx, "prj_1", sha1, "op_2", time.Minute)
	require.NoError(t, err)
	assert.Equal(t, domain.GoldenActionBuild, c.Action, "stale takeover")
	assert.Equal(t, "op_2", c.Tree.BuilderOpID)

	require.NoError(t, s.FinishGolden(ctx, "prj_1", sha1, 1234, 7))
	c, err = s.ClaimGolden(ctx, "prj_1", sha1, "op_3", time.Minute)
	require.NoError(t, err)
	assert.Equal(t, domain.GoldenActionReady, c.Action)
	assert.Equal(t, 1, c.Tree.InUse)

	marked, err := s.MarkGoldenDeleting(ctx, "prj_1", sha1)
	require.NoError(t, err)
	assert.False(t, marked, "in use")
	require.NoError(t, s.UseGolden(ctx, "prj_1", sha1, -1))
	require.NoError(t, s.UseGolden(ctx, "prj_1", sha1, -1), "never below zero")
	marked, err = s.MarkGoldenDeleting(ctx, "prj_1", sha1)
	require.NoError(t, err)
	assert.True(t, marked)
	require.ErrorIs(t, s.UseGolden(ctx, "prj_1", sha1, 1), store.ErrInvalidTransition)
	require.NoError(t, s.DeleteGolden(ctx, "prj_1", sha1))
	require.NoError(t, s.DeleteGolden(ctx, "prj_1", sha1), "idempotent")
	list, err := s.ListGolden(ctx, "prj_1")
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestMemStoreTasks(t *testing.T) {
	ctx := context.Background()
	s := &workspacetest.MemStore{}
	task := domain.Task{ProjectID: "prj_1", Slug: "auth", Base: "develop", BaseSHA: sha1, Branch: "feature/auth"}
	got, err := s.CreateTask(ctx, task)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskStateCreated, got.State)
	assert.NotEmpty(t, got.ID)
	_, err = s.CreateTask(ctx, task)
	require.ErrorIs(t, err, store.ErrConflict)
	_, err = s.CreateTask(ctx, domain.Task{ProjectID: "prj_1", Slug: "Bad Slug", Base: "d", BaseSHA: sha1, Branch: "b"})
	require.ErrorIs(t, err, domain.ErrInvalidSlug)
	byslug, err := s.GetTaskBySlug(ctx, "prj_1", "auth")
	require.NoError(t, err)
	assert.Equal(t, got.ID, byslug.ID)
	_, err = s.GetTaskBySlug(ctx, "prj_1", "other")
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestFakeProviderDestroyRefusesDirty(t *testing.T) {
	ctx := context.Background()
	p := &workspacetest.FakeProvider{ProviderName: "cow_clone", Caps: workspace.Capabilities{CopyOnWrite: workspace.CowBlock, Filesystem: "refs"}}
	w, err := p.Create(ctx, workspace.WorkspaceSpec{ID: "wks_1", BaseSHA: sha1, Path: "/ws/auth"})
	require.NoError(t, err)
	assert.Equal(t, domain.WorkspaceReady, w.State)
	assert.Equal(t, "cow_clone", w.Provider)

	require.NoError(t, p.SetDirty("wks_1", workspace.DirtyReport{Uncommitted: []string{"a.go"}}))
	st, err := p.Inspect(ctx, "wks_1")
	require.NoError(t, err)
	assert.Equal(t, workspace.CowBlock, st.CowKind)
	assert.Equal(t, "refs", st.Filesystem)

	var ue *workspace.UnpreservedError
	_, err = p.Destroy(ctx, "wks_1", workspace.DestroyOptions{})
	require.ErrorAs(t, err, &ue)
	assert.Equal(t, []string{"a.go"}, ue.Report.Uncommitted)
	res, err := p.Destroy(ctx, "wks_1", workspace.DestroyOptions{Force: true, Actor: workspace.Actor{Kind: "user", ID: "ana"}})
	require.NoError(t, err)
	assert.Equal(t, "refs/cowflow/rescue/wks_1", res.RescueRef, "a forced dirty destroy reports the rescue ref")
	res, err = p.Destroy(ctx, "wks_1", workspace.DestroyOptions{})
	require.NoError(t, err, "idempotent")
	assert.Empty(t, res.RescueRef)
	_, err = p.Destroy(ctx, "wks_2", workspace.DestroyOptions{})
	require.ErrorIs(t, err, workspace.ErrNotFound)
	_, err = p.Inspect(ctx, "wks_2")
	require.ErrorIs(t, err, workspace.ErrNotFound)
	assert.Equal(t, []string{"Create", "Inspect", "Destroy", "Destroy", "Destroy", "Destroy", "Inspect"}, p.Calls())
}

func TestFakeManagerFlow(t *testing.T) {
	ctx := context.Background()
	m := &workspacetest.FakeManager{Warning: "no copy-on-write: using git_worktree"}

	sel, err := m.Select(ctx, "auto")
	require.NoError(t, err)
	assert.NotEmpty(t, sel.Warning)
	sel, err = m.Select(ctx, "git_worktree")
	require.NoError(t, err)
	assert.Empty(t, sel.Warning)
	for _, p := range []string{"projfs", "container"} {
		_, err = m.Select(ctx, p)
		require.ErrorIs(t, err, workspace.ErrUnsupportedProvider)
	}

	req := workspace.CreateRequest{
		OpID: "op_1", ProjectID: "prj_1", Provider: "auto",
		Task: domain.Task{ID: "tsk_1", Slug: "auth", Branch: "feature/auth", BaseSHA: sha1, Paths: []string{"src/auth"}, Sparse: true},
	}
	w, sel, err := m.Create(ctx, req)
	require.NoError(t, err)
	assert.NotEmpty(t, sel.Warning)
	assert.Equal(t, workspace.PortRange{Start: 40000, End: 40009}, w.Ports)
	assert.True(t, w.Sparse.Enabled)
	again, _, err := m.Create(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, w.ID, again.ID, "idempotent by op_id")

	req2 := req
	req2.OpID, req2.Task.ID, req2.Task.Slug = "op_2", "tsk_2", "billing"
	w2, _, err := m.Create(ctx, req2)
	require.NoError(t, err)
	assert.Equal(t, 40010, w2.Ports.Start)

	require.NoError(t, m.ExpandScope(ctx, w.ID, []string{"src/shared"}))
	st, err := m.Inspect(ctx, w.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"src/auth", "src/shared"}, st.Sparse.Paths)

	require.NoError(t, m.Provider.SetDirty(w.ID, workspace.DirtyReport{Uncommitted: []string{"x.go"}}))
	res, err := m.DestroyWithResult(ctx, w.ID, workspace.DestroyOptions{Force: true})
	require.NoError(t, err)
	assert.Equal(t, "refs/cowflow/rescue/"+w.ID, res.RescueRef)
	all, err := m.List(ctx, workspace.ListFilter{})
	require.NoError(t, err)
	assert.Len(t, all, 2)
	ready, err := m.List(ctx, workspace.ListFilter{States: []domain.WorkspaceState{domain.WorkspaceReady}})
	require.NoError(t, err)
	require.Len(t, ready, 1)
	assert.Equal(t, w2.ID, ready[0].ID)
	assert.Len(t, m.Requests(), 3)

	boom := errors.New("pending")
	m.CreateErr = boom
	_, _, err = m.Create(ctx, req)
	require.ErrorIs(t, err, boom)
}
