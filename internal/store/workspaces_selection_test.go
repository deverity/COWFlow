package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/domain"
)

func (f *fixture) pendingMount(t *testing.T) domain.WorkspaceRecord {
	t.Helper()
	req := f.ws()
	req.State = domain.WorkspacePendingMount
	req.Provider, req.CowKind, req.Filesystem = "git_worktree", "none", "ntfs"
	r, err := f.s.InsertWorkspace(context.Background(), req, 10, 40000)
	require.NoError(t, err)
	return r
}

func TestSetWorkspaceStateRecordsSelectionOnRetry(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	r := f.pendingMount(t)

	err := f.s.SetWorkspaceState(ctx, r.ID, domain.WorkspaceCreating, domain.StateChange{
		Provider: "cow_clone", CowKind: "block", Filesystem: "refs", Path: `D:\ws\canon`,
		Audit: &domain.StateAudit{Action: "workspace.select", ActorKind: "system"},
	})
	require.NoError(t, err)
	got, err := f.s.GetWorkspace(ctx, r.ID)
	require.NoError(t, err)
	require.Equal(t, domain.WorkspaceCreating, got.State)
	require.Equal(t, "cow_clone", got.Provider)
	require.Equal(t, "block", got.CowKind)
	require.Equal(t, "refs", got.Filesystem)
	require.Equal(t, `D:\ws\canon`, got.Path)
	var n int
	require.NoError(t, f.s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM audit_log WHERE subject_id = ? AND action = 'workspace.select'", r.ID).Scan(&n))
	require.Equal(t, 1, n, "the audit row is written with the selection")
}

func TestSetWorkspaceStateEmptySelectionKeepsRow(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	r := f.pendingMount(t)
	require.NoError(t, f.s.SetWorkspaceState(ctx, r.ID, domain.WorkspaceCreating, domain.StateChange{Provider: "cow_clone"}))
	got, err := f.s.GetWorkspace(ctx, r.ID)
	require.NoError(t, err)
	require.Equal(t, "cow_clone", got.Provider)
	require.Equal(t, r.CowKind, got.CowKind)
	require.Equal(t, r.Filesystem, got.Filesystem)
	require.Equal(t, r.Path, got.Path)
}

func TestSetWorkspaceStateRefusesSelectionElsewhere(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	r := f.insert(t) // creating
	before, err := f.s.GetWorkspace(ctx, r.ID)
	require.NoError(t, err)
	for _, c := range []domain.StateChange{
		{Provider: "x"}, {CowKind: "x"}, {Filesystem: "x"}, {Path: "x"},
	} {
		err = f.s.SetWorkspaceState(ctx, r.ID, domain.WorkspaceReady, c)
		require.ErrorIs(t, err, ErrInvalidTransition)
	}
	after, err := f.s.GetWorkspace(ctx, r.ID)
	require.NoError(t, err)
	require.Equal(t, before, after, "a refused change writes nothing")
}

func TestSetWorkspaceStateSelectionRejectedAuditWritesNothing(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	r := f.pendingMount(t)
	err := f.s.SetWorkspaceState(ctx, r.ID, domain.WorkspaceCreating, domain.StateChange{
		Provider: "cow_clone", Audit: &domain.StateAudit{},
	})
	require.Error(t, err)
	got, err := f.s.GetWorkspace(ctx, r.ID)
	require.NoError(t, err)
	require.Equal(t, domain.WorkspacePendingMount, got.State)
	require.Equal(t, r.Provider, got.Provider)
}
