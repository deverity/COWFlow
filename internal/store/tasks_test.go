package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/domain"
)

func TestCreateAndGetTask(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	in := domain.Task{
		ProjectID: f.project.ID, Slug: "fix-api", Title: "Fix the API", Kind: "bugfix",
		Base: "develop", BaseSHA: "0123", Branch: "bugfix/fix-api",
		Paths: []string{"services/api", "contracts"}, Sparse: false,
	}
	made, err := f.s.CreateTask(ctx, in)
	require.NoError(t, err)
	require.NotEmpty(t, made.ID)
	require.Equal(t, domain.TaskStateCreated, made.State)
	require.Equal(t, f.clock.Now(), made.CreatedAt)

	got, err := f.s.GetTaskBySlug(ctx, f.project.ID, "fix-api")
	require.NoError(t, err)
	require.Equal(t, made, got)
	require.False(t, got.Sparse)
	require.Equal(t, []string{"services/api", "contracts"}, got.Paths)
}

func TestCreateTaskDefaultsEmptyPaths(t *testing.T) {
	f := newFixture(t) // fixture task has nil paths and sparse = true
	got, err := f.s.GetTaskBySlug(context.Background(), f.project.ID, "login")
	require.NoError(t, err)
	require.Equal(t, []string{}, got.Paths)
	require.True(t, got.Sparse)
	require.Empty(t, got.Kind)
}

func TestCreateTaskDuplicateSlugConflicts(t *testing.T) {
	f := newFixture(t)
	_, err := f.s.CreateTask(context.Background(), domain.Task{
		ProjectID: f.project.ID, Slug: "login", Base: "develop", BaseSHA: "x", Branch: "bugfix/login",
	})
	require.ErrorIs(t, err, ErrConflict)

	// The same slug in another project is fine.
	other := f.addProject(t, "other", "/other")
	f.addTask(t, other.ID, "login")
}

func TestCreateTaskValidation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	base := domain.Task{ProjectID: f.project.ID, Slug: "ok", Base: "develop", BaseSHA: "x", Branch: "feature/ok"}

	bad := base
	bad.Slug = "Bad Slug"
	_, err := f.s.CreateTask(ctx, bad)
	require.ErrorIs(t, err, domain.ErrInvalidSlug)

	bad = base
	bad.Branch = ""
	_, err = f.s.CreateTask(ctx, bad)
	require.Error(t, err)

	bad = base
	bad.ProjectID = "prj_missing"
	_, err = f.s.CreateTask(ctx, bad)
	require.Error(t, err, "foreign key to projects")
}

func TestGetTaskBySlugNotFound(t *testing.T) {
	f := newFixture(t)
	_, err := f.s.GetTaskBySlug(context.Background(), f.project.ID, "nope")
	require.ErrorIs(t, err, ErrNotFound)
}
