package workspacetest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/domain"
	"github.com/deverity/cowflow/internal/store"
	"github.com/deverity/cowflow/internal/workspace/workspacetest"
)

func TestMemStoreDeleteTask(t *testing.T) {
	ctx := context.Background()
	s := &workspacetest.MemStore{}
	task, err := s.CreateTask(ctx, domain.Task{ProjectID: "prj_1", Slug: "auth", Base: "develop", BaseSHA: sha1, Branch: "feature/auth"})
	require.NoError(t, err)

	require.ErrorIs(t, s.DeleteTask(ctx, "prj_1", "tsk_missing"), store.ErrNotFound)
	require.ErrorIs(t, s.DeleteTask(ctx, "prj_other", task.ID), store.ErrNotFound)

	r := newRecord("op_del")
	r.TaskID = task.ID
	w, err := s.InsertWorkspace(ctx, r, 10, 40000)
	require.NoError(t, err)

	// A creating workspace blocks the delete.
	err = s.DeleteTask(ctx, "prj_1", task.ID)
	require.ErrorIs(t, err, store.ErrTaskHasWorkspaces)
	_, err = s.GetTaskBySlug(ctx, "prj_1", "auth")
	require.NoError(t, err, "a refused delete changes nothing")

	// A destroyed row blocks it too.
	require.NoError(t, s.SetWorkspaceState(ctx, w.ID, domain.WorkspaceReady, domain.StateChange{}))
	require.NoError(t, s.SetWorkspaceState(ctx, w.ID, domain.WorkspacePendingCleanup, domain.StateChange{}))
	require.NoError(t, s.SetWorkspaceState(ctx, w.ID, domain.WorkspaceDestroyed, domain.StateChange{}))
	require.ErrorIs(t, s.DeleteTask(ctx, "prj_1", task.ID), store.ErrTaskHasWorkspaces)
}

func TestMemStoreDeleteTaskRemovesFailedRows(t *testing.T) {
	ctx := context.Background()
	s := &workspacetest.MemStore{}
	task, err := s.CreateTask(ctx, domain.Task{ProjectID: "prj_1", Slug: "auth", Base: "develop", BaseSHA: sha1, Branch: "feature/auth"})
	require.NoError(t, err)
	r := newRecord("op_failed")
	r.TaskID = task.ID
	w, err := s.InsertWorkspace(ctx, r, 10, 40000)
	require.NoError(t, err)
	require.NoError(t, s.SetWorkspaceState(ctx, w.ID, domain.WorkspaceFailed, domain.StateChange{LastError: "boom"}))

	require.NoError(t, s.DeleteTask(ctx, "prj_1", task.ID))
	_, err = s.GetTaskBySlug(ctx, "prj_1", "auth")
	require.ErrorIs(t, err, store.ErrNotFound)
	_, err = s.GetWorkspace(ctx, w.ID)
	require.ErrorIs(t, err, store.ErrNotFound)

	again, err := s.CreateTask(ctx, domain.Task{ProjectID: "prj_1", Slug: "auth", Base: "develop", BaseSHA: sha1, Branch: "feature/auth"})
	require.NoError(t, err, "the slug is free again")
	assert.NotEqual(t, task.ID, again.ID)
}

func TestMemStoreUpdateWorkspaceSparsePaths(t *testing.T) {
	ctx := context.Background()
	s := &workspacetest.MemStore{}
	w, err := s.InsertWorkspace(ctx, newRecord("op_sp"), 10, 40000)
	require.NoError(t, err)

	require.ErrorIs(t, s.UpdateWorkspaceSparsePaths(ctx, w.ID, []string{"a"}), store.ErrInvalidTransition, "creating")
	require.ErrorIs(t, s.UpdateWorkspaceSparsePaths(ctx, "wks_missing", nil), store.ErrNotFound)

	require.NoError(t, s.SetWorkspaceState(ctx, w.ID, domain.WorkspaceReady, domain.StateChange{}))
	paths := []string{"contracts", "svc/api"}
	require.NoError(t, s.UpdateWorkspaceSparsePaths(ctx, w.ID, paths))
	paths[0] = "mutated"
	got, err := s.GetWorkspace(ctx, w.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"contracts", "svc/api"}, got.SparsePaths, "the slice is copied")

	require.NoError(t, s.UpdateWorkspaceSparsePaths(ctx, w.ID, nil))
	got, err = s.GetWorkspace(ctx, w.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{}, got.SparsePaths)
}
