package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/domain"
)

func TestDeleteTask(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	// No workspaces: deleted.
	empty := f.addTask(t, f.project.ID, "empty")
	require.NoError(t, f.s.DeleteTask(ctx, f.project.ID, empty.ID))
	_, err := f.s.GetTaskBySlug(ctx, f.project.ID, "empty")
	require.ErrorIs(t, err, ErrNotFound)

	// Only failed rows: deleted together with them.
	w := f.insert(t)
	f.setState(t, w.ID, domain.WorkspaceFailed)
	require.NoError(t, f.s.DeleteTask(ctx, f.project.ID, f.task.ID))
	_, err = f.s.GetWorkspace(ctx, w.ID)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestDeleteTaskRefusedWithNonFailedWorkspace(t *testing.T) {
	ctx := context.Background()
	for _, to := range []domain.WorkspaceState{domain.WorkspaceCreating, domain.WorkspaceReady, domain.WorkspaceDestroyed} {
		f := newFixture(t)
		failed := f.insert(t)
		f.setState(t, failed.ID, domain.WorkspaceFailed)
		w := f.insert(t)
		if to != domain.WorkspaceCreating {
			f.setState(t, w.ID, domain.WorkspaceReady)
		}
		if to == domain.WorkspaceDestroyed {
			f.setState(t, w.ID, domain.WorkspaceDestroyed)
		}
		err := f.s.DeleteTask(ctx, f.project.ID, f.task.ID)
		require.ErrorIs(t, err, ErrTaskHasWorkspaces, to)
		// Nothing was removed, not even the failed row.
		_, err = f.s.GetTaskBySlug(ctx, f.project.ID, "login")
		require.NoError(t, err)
		_, err = f.s.GetWorkspace(ctx, failed.ID)
		require.NoError(t, err)
	}
}

func TestDeleteTaskNotFoundAndWrongProject(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	require.ErrorIs(t, f.s.DeleteTask(ctx, f.project.ID, "tsk_missing"), ErrNotFound)
	other := f.addProject(t, "other", "/other")
	require.ErrorIs(t, f.s.DeleteTask(ctx, other.ID, f.task.ID), ErrNotFound)
	_, err := f.s.GetTaskBySlug(ctx, f.project.ID, "login")
	require.NoError(t, err)
}

func TestUpdateWorkspaceSparsePaths(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	w := f.insert(t)

	// Not ready yet: refused.
	err := f.s.UpdateWorkspaceSparsePaths(ctx, w.ID, []string{"a"})
	require.ErrorIs(t, err, ErrInvalidTransition)

	f.setState(t, w.ID, domain.WorkspaceReady)
	f.clock.Advance(time.Minute)
	require.NoError(t, f.s.UpdateWorkspaceSparsePaths(ctx, w.ID, []string{"contracts", "svc/api"}))
	got, err := f.s.GetWorkspace(ctx, w.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"contracts", "svc/api"}, got.SparsePaths)
	require.True(t, got.UpdatedAt.After(w.UpdatedAt))

	require.NoError(t, f.s.UpdateWorkspaceSparsePaths(ctx, w.ID, nil))
	got, err = f.s.GetWorkspace(ctx, w.ID)
	require.NoError(t, err)
	require.Equal(t, []string{}, got.SparsePaths)

	require.ErrorIs(t, f.s.UpdateWorkspaceSparsePaths(ctx, "wks_missing", nil), ErrNotFound)
}

func TestListGoldenIncludesRowID(t *testing.T) {
	f := newFixture(t)
	f.readyGolden(t, "sha1")
	list, err := f.s.ListGolden(context.Background(), f.project.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.NotEmpty(t, list[0].ID)
}
