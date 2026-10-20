package store

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/domain"
)

const goldenStale = 10 * time.Minute

func (f *fixture) claim(t *testing.T, sha, op string) domain.GoldenClaim {
	t.Helper()
	c, err := f.s.ClaimGolden(context.Background(), f.project.ID, sha, op, goldenStale)
	require.NoError(t, err)
	return c
}

func (f *fixture) readyGolden(t *testing.T, sha string) {
	t.Helper()
	require.Equal(t, domain.GoldenActionBuild, f.claim(t, sha, "b-"+sha).Action)
	require.NoError(t, f.s.FinishGolden(context.Background(), f.project.ID, sha, 100, 5))
}

func TestClaimGoldenLifecycle(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	c := f.claim(t, "sha1", "op-a")
	require.Equal(t, domain.GoldenActionBuild, c.Action)
	require.Equal(t, domain.GoldenBuilding, c.Tree.State)
	require.NotEmpty(t, c.Tree.ID)
	require.Equal(t, "op-a", c.Tree.BuilderOpID)

	require.Equal(t, domain.GoldenActionWait, f.claim(t, "sha1", "op-b").Action, "someone else is building")
	require.Equal(t, domain.GoldenActionBuild, f.claim(t, "sha1", "op-a").Action, "the builder resumes")

	f.clock.Advance(time.Minute)
	require.NoError(t, f.s.FinishGolden(ctx, f.project.ID, "sha1", 4096, 12))
	c = f.claim(t, "sha1", "op-b")
	require.Equal(t, domain.GoldenActionReady, c.Action)
	require.Equal(t, domain.GoldenReady, c.Tree.State)
	require.Equal(t, int64(4096), c.Tree.SizeBytes)
	require.Equal(t, 12, c.Tree.FileCount)
	require.Equal(t, 1, c.Tree.InUse)
	require.True(t, c.Tree.LastUsedAt.After(c.Tree.CreatedAt))

	require.NoError(t, f.s.UseGolden(ctx, f.project.ID, "sha1", 1))
	require.NoError(t, f.s.UseGolden(ctx, f.project.ID, "sha1", -1))
	list, err := f.s.ListGolden(ctx, f.project.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, 1, list[0].InUse)
}

func TestClaimGoldenSingleBuilder(t *testing.T) {
	ctx := context.Background()
	path := dbPath(t)
	first := openTest(t, path, newTestIDs("g0"))
	var project domain.Project
	require.NoError(t, first.WithTx(ctx, "", func(tx *Tx) error {
		var err error
		project, err = tx.InsertProject("demo", "/repo")
		return err
	}))
	const n = 20
	stores := make([]*Store, n)
	for i := range n {
		stores[i] = openTest(t, path, newTestIDs(fmt.Sprintf("g%d", i+1)))
	}
	var wg sync.WaitGroup
	actions := make([]domain.GoldenAction, n)
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := stores[i].ClaimGolden(ctx, project.ID, "deadbeef", fmt.Sprintf("op-%d", i), goldenStale)
			actions[i], errs[i] = c.Action, err
		}()
	}
	wg.Wait()
	builders, waiters := 0, 0
	for i := range n {
		require.NoError(t, errs[i])
		switch actions[i] {
		case domain.GoldenActionBuild:
			builders++
		case domain.GoldenActionWait:
			waiters++
		}
	}
	require.Equal(t, 1, builders, "exactly one Build")
	require.Equal(t, n-1, waiters)
}

func TestClaimGoldenStaleTakeover(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	require.Equal(t, domain.GoldenActionBuild, f.claim(t, "sha1", "op-a").Action)

	f.clock.Advance(goldenStale - time.Second)
	require.Equal(t, domain.GoldenActionWait, f.claim(t, "sha1", "op-b").Action, "not stale yet")

	f.clock.Advance(time.Second) // exactly staleAfter old: stale
	c := f.claim(t, "sha1", "op-b")
	require.Equal(t, domain.GoldenActionBuild, c.Action)
	require.Equal(t, "op-b", c.Tree.BuilderOpID)

	// The takeover refreshed updated_at, so a third process waits.
	require.Equal(t, domain.GoldenActionWait, f.claim(t, "sha1", "op-c").Action)
	require.NoError(t, f.s.FinishGolden(ctx, f.project.ID, "sha1", 1, 1))
}

func TestClaimGoldenRejectsBadInput(t *testing.T) {
	f := newFixture(t)
	_, err := f.s.ClaimGolden(context.Background(), f.project.ID, "sha", "op", 0)
	require.Error(t, err)
	_, err = f.s.ClaimGolden(context.Background(), f.project.ID, "sha", "", goldenStale)
	require.Error(t, err)
}

func TestFailGoldenThenRetry(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.claim(t, "sha1", "op-a")
	require.NoError(t, f.s.FailGolden(ctx, f.project.ID, "sha1", "ENOSPC"))
	list, err := f.s.ListGolden(ctx, f.project.ID)
	require.NoError(t, err)
	require.Equal(t, domain.GoldenFailed, list[0].State)
	require.Equal(t, "ENOSPC", list[0].LastError)

	require.ErrorIs(t, f.s.FinishGolden(ctx, f.project.ID, "sha1", 1, 1), ErrInvalidTransition)
	require.ErrorIs(t, f.s.UseGolden(ctx, f.project.ID, "sha1", 1), ErrInvalidTransition)

	c := f.claim(t, "sha1", "op-b")
	require.Equal(t, domain.GoldenActionBuild, c.Action)
	require.Empty(t, c.Tree.LastError)
	require.Equal(t, "op-b", c.Tree.BuilderOpID)
}

func TestGoldenMissingRow(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	require.ErrorIs(t, f.s.FinishGolden(ctx, f.project.ID, "nope", 1, 1), ErrNotFound)
	require.ErrorIs(t, f.s.FailGolden(ctx, f.project.ID, "nope", "x"), ErrNotFound)
	require.ErrorIs(t, f.s.UseGolden(ctx, f.project.ID, "nope", 1), ErrNotFound)
	_, err := f.s.MarkGoldenDeleting(ctx, f.project.ID, "nope")
	require.ErrorIs(t, err, ErrNotFound)
	require.NoError(t, f.s.DeleteGolden(ctx, f.project.ID, "nope"), "delete is idempotent")
	require.Error(t, f.s.UseGolden(ctx, f.project.ID, "nope", 2))
}

func TestMarkGoldenDeletingRefusesInUse(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.readyGolden(t, "sha1")
	require.NoError(t, f.s.UseGolden(ctx, f.project.ID, "sha1", 1))

	ok, err := f.s.MarkGoldenDeleting(ctx, f.project.ID, "sha1")
	require.NoError(t, err)
	require.False(t, ok, "in use")

	require.NoError(t, f.s.UseGolden(ctx, f.project.ID, "sha1", -1))
	ok, err = f.s.MarkGoldenDeleting(ctx, f.project.ID, "sha1")
	require.NoError(t, err)
	require.True(t, ok)

	// Deleting trees are not claimable; the caller waits.
	require.Equal(t, domain.GoldenActionWait, f.claim(t, "sha1", "op-z").Action)
	require.ErrorIs(t, f.s.UseGolden(ctx, f.project.ID, "sha1", 1), ErrInvalidTransition)

	ok, err = f.s.MarkGoldenDeleting(ctx, f.project.ID, "sha1")
	require.NoError(t, err)
	require.False(t, ok, "already deleting")

	require.NoError(t, f.s.DeleteGolden(ctx, f.project.ID, "sha1"))
	list, err := f.s.ListGolden(ctx, f.project.ID)
	require.NoError(t, err)
	require.Empty(t, list)
	require.Equal(t, domain.GoldenActionBuild, f.claim(t, "sha1", "op-n").Action, "row gone: rebuild")
}

func TestMarkGoldenDeletingRefusesNonReady(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.claim(t, "sha1", "op-a")
	ok, err := f.s.MarkGoldenDeleting(ctx, f.project.ID, "sha1")
	require.NoError(t, err)
	require.False(t, ok, "a building tree is not deletable")
}

func TestDeleteGoldenStates(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.readyGolden(t, "ready")
	require.ErrorIs(t, f.s.DeleteGolden(ctx, f.project.ID, "ready"), ErrInvalidTransition)
	f.claim(t, "bad", "op-b")
	require.NoError(t, f.s.FailGolden(ctx, f.project.ID, "bad", "x"))
	require.NoError(t, f.s.DeleteGolden(ctx, f.project.ID, "bad"), "gc removes failed rows")
}

func TestUseGoldenNeverGoesNegative(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.readyGolden(t, "sha1")
	require.NoError(t, f.s.UseGolden(ctx, f.project.ID, "sha1", -1))
	list, err := f.s.ListGolden(ctx, f.project.ID)
	require.NoError(t, err)
	require.Equal(t, 0, list[0].InUse)
}

func TestListGoldenLeastRecentlyUsedFirst(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	for _, sha := range []string{"b", "a", "c"} {
		f.readyGolden(t, sha)
		f.clock.Advance(time.Second)
	}
	f.clock.Advance(time.Second)
	require.NoError(t, f.s.UseGolden(ctx, f.project.ID, "b", 1)) // b becomes the most recent
	list, err := f.s.ListGolden(ctx, f.project.ID)
	require.NoError(t, err)
	var got []string
	for _, g := range list {
		got = append(got, g.BaseSHA)
	}
	require.Equal(t, []string{"a", "c", "b"}, got)

	other, err := f.s.ListGolden(ctx, "prj_other")
	require.NoError(t, err)
	require.Empty(t, other)
}
