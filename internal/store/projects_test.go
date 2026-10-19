package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/domain"
)

type fakeClock struct{ t time.Time }

func (c fakeClock) Now() time.Time                        { return c.t }
func (c fakeClock) After(time.Duration) <-chan time.Time  { return nil }
func (c fakeClock) NewTicker(time.Duration) domain.Ticker { return nil }

func TestProjects_InsertAndByRoot(t *testing.T) {
	ctx := context.Background()
	s := openTest(t, dbPath(t), newTestIDs("a"))
	s.clock = fakeClock{t: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}

	var got domain.Project
	require.NoError(t, s.WithTx(ctx, "", func(tx *Tx) error {
		var err error
		got, err = tx.InsertProject("demo", "/repo")
		return err
	}))
	found, err := s.ProjectByRoot(ctx, "/repo")
	require.NoError(t, err)
	require.Equal(t, got, found)
	require.Equal(t, "demo", found.Name)
	require.True(t, found.CreatedAt.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)))
}

func TestProjects_NotFound(t *testing.T) {
	s := openTest(t, dbPath(t), newTestIDs("a"))
	_, err := s.ProjectByRoot(context.Background(), "/nope")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestProjects_UniqueRoot(t *testing.T) {
	ctx := context.Background()
	s := openTest(t, dbPath(t), newTestIDs("a"))
	require.NoError(t, s.WithTx(ctx, "", func(tx *Tx) error {
		_, err := tx.InsertProject("a", "/repo")
		return err
	}))
	err := s.WithTx(ctx, "", func(tx *Tx) error {
		_, err := tx.InsertProject("b", "/repo")
		return err
	})
	require.ErrorIs(t, err, ErrConflict)
	require.Equal(t, 1, countProjects(t, s))
}
