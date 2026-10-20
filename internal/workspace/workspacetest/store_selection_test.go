package workspacetest_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/domain"
	"github.com/deverity/cowflow/internal/store"
	"github.com/deverity/cowflow/internal/workspace/workspacetest"
	"github.com/deverity/cowflow/test/harness"
)

func TestMemStoreRecordsSelectionOnRetryOnly(t *testing.T) {
	ctx := context.Background()
	s := workspacetest.NewMemStore(harness.NewClock(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)))
	r := newRecord("op_sel")
	r.State, r.Provider, r.CowKind, r.Filesystem, r.Path = domain.WorkspacePendingMount, "auto", "none", "ntfs", "/lexical"
	w, err := s.InsertWorkspace(ctx, r, 10, 40000)
	require.NoError(t, err)

	require.NoError(t, s.SetWorkspaceState(ctx, w.ID, domain.WorkspaceCreating, domain.StateChange{
		Provider: "cow_clone", CowKind: "block", Path: "/canonical",
	}))
	got, err := s.GetWorkspace(ctx, w.ID)
	require.NoError(t, err)
	assert.Equal(t, "cow_clone", got.Provider)
	assert.Equal(t, "block", got.CowKind)
	assert.Equal(t, "/canonical", got.Path)
	assert.Equal(t, "ntfs", got.Filesystem, "an empty field keeps the stored value")

	err = s.SetWorkspaceState(ctx, w.ID, domain.WorkspaceReady, domain.StateChange{Provider: "other"})
	require.ErrorIs(t, err, store.ErrInvalidTransition)
	got, err = s.GetWorkspace(ctx, w.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.WorkspaceCreating, got.State)
	assert.Equal(t, "cow_clone", got.Provider)
}

func TestMemStoreResetStaleGoldenInUse(t *testing.T) {
	ctx := context.Background()
	clock := harness.NewClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	s := workspacetest.NewMemStore(clock)
	const sha2 = "1123456789abcdef0123456789abcdef01234567"
	for _, sha := range []string{sha1, sha2} {
		_, err := s.ClaimGolden(ctx, "prj_1", sha, "op_1", time.Hour)
		require.NoError(t, err)
		require.NoError(t, s.FinishGolden(ctx, "prj_1", sha, 1, 1))
		_, err = s.ClaimGolden(ctx, "prj_1", sha, "op_2", time.Hour) // in_use 1
		require.NoError(t, err)
		clock.Advance(time.Hour)
	}
	cutoff := clock.Now().Add(-90 * time.Minute)
	got, err := s.ResetStaleGoldenInUse(ctx, "prj_1", cutoff)
	require.NoError(t, err)
	assert.Equal(t, []string{sha1}, got)
	again, err := s.ResetStaleGoldenInUse(ctx, "prj_1", cutoff)
	require.NoError(t, err)
	assert.Empty(t, again)
	list, err := s.ListGolden(ctx, "prj_1")
	require.NoError(t, err)
	inUse := map[string]int{}
	for _, g := range list {
		inUse[g.BaseSHA] = g.InUse
	}
	assert.Equal(t, map[string]int{sha1: 0, sha2: 1}, inUse)
}
