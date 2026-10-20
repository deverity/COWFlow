package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/domain"
)

func TestResetStaleGoldenInUse(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	other := f.addProject(t, "other", "/other")
	for _, sha := range []string{"old-b", "old-a", "fresh", "idle"} {
		f.readyGolden(t, sha)
		if sha != "idle" {
			require.NoError(t, f.s.UseGolden(ctx, f.project.ID, sha, 1))
		}
		if sha == "old-a" {
			f.clock.Advance(time.Hour) // old-b and old-a are now an hour behind the rest
		}
	}
	// A tree of another project must stay untouched.
	oc, err := f.s.ClaimGolden(ctx, other.ID, "old-a", "op-o", goldenStale)
	require.NoError(t, err)
	require.Equal(t, domain.GoldenActionBuild, oc.Action)

	cutoff := f.clock.Now().Add(-30 * time.Minute)
	got, err := f.s.ResetStaleGoldenInUse(ctx, f.project.ID, cutoff)
	require.NoError(t, err)
	require.Equal(t, []string{"old-a", "old-b"}, got, "idle (in_use 0) and fresh are not reported")

	list, err := f.s.ListGolden(ctx, f.project.ID)
	require.NoError(t, err)
	inUse := map[string]int{}
	for _, g := range list {
		inUse[g.BaseSHA] = g.InUse
	}
	require.Equal(t, map[string]int{"old-a": 0, "old-b": 0, "fresh": 1, "idle": 0}, inUse)

	again, err := f.s.ResetStaleGoldenInUse(ctx, f.project.ID, cutoff)
	require.NoError(t, err)
	require.Empty(t, again, "a second run changes nothing")

	marked, err := f.s.MarkGoldenDeleting(ctx, f.project.ID, "old-a")
	require.NoError(t, err)
	require.True(t, marked, "with the count reset, gc may mark the tree deleting")
}
