package store

import (
	"context"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func swapMigrations(t *testing.T, files fstest.MapFS) {
	t.Helper()
	old := migrationFS
	migrationFS = files
	t.Cleanup(func() { migrationFS = old })
}

func dbPath(t *testing.T) string { return filepath.Join(t.TempDir(), "s.db") }

func countingFS() fstest.MapFS {
	return fstest.MapFS{
		"migrations/0001_count.sql": {Data: []byte("CREATE TABLE counter (n INTEGER); INSERT INTO counter VALUES (1);")},
		"migrations/0002_more.sql":  {Data: []byte("CREATE TABLE more (id INTEGER);")},
	}
}

func TestOpen_PragmasOnEveryConnection(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, dbPath(t), Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	s.db.SetMaxOpenConns(4)

	conns := make([]io.Closer, 0, 4)
	for range 4 {
		c, err := s.db.Conn(ctx)
		require.NoError(t, err)
		conns = append(conns, c)
		var jm string
		var bt, fk int
		require.NoError(t, c.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&jm))
		require.NoError(t, c.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&bt))
		require.NoError(t, c.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk))
		require.Equal(t, "wal", jm)
		require.Equal(t, 5000, bt)
		require.Equal(t, 1, fk)
	}
	for _, c := range conns {
		require.NoError(t, c.Close())
	}
}

func TestOpen_RejectsQuestionMark(t *testing.T) {
	_, err := Open(context.Background(), "x?y.db", Options{})
	require.Error(t, err)
}

func TestMigrate_FromEmpty(t *testing.T) {
	swapMigrations(t, countingFS())
	ctx := context.Background()
	s, err := Open(ctx, dbPath(t), Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	v, err := s.SchemaVersion(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, v)
}

func TestMigrate_EmbeddedLoads(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, dbPath(t), Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	v, err := s.SchemaVersion(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, v, 0)
}

func TestMigrate_Idempotent(t *testing.T) {
	swapMigrations(t, countingFS())
	ctx := context.Background()
	p := dbPath(t)
	for range 2 {
		s, err := Open(ctx, p, Options{})
		require.NoError(t, err)
		v, err := s.SchemaVersion(ctx)
		require.NoError(t, err)
		require.Equal(t, 2, v)
		var n int
		require.NoError(t, s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM counter").Scan(&n))
		require.Equal(t, 1, n)
		require.NoError(t, s.Close())
	}
}

func TestMigrate_ConcurrentOpeners(t *testing.T) {
	swapMigrations(t, countingFS())
	ctx := context.Background()
	p := dbPath(t)
	var wg sync.WaitGroup
	errs := make([]error, 10)
	stores := make([]*Store, 10)
	for i := range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stores[i], errs[i] = Open(ctx, p, Options{})
		}()
	}
	wg.Wait()
	for i := range 10 {
		require.NoError(t, errs[i])
	}
	var n int
	require.NoError(t, stores[0].db.QueryRowContext(ctx, "SELECT COUNT(*) FROM counter").Scan(&n))
	require.Equal(t, 1, n, "each migration must be applied exactly once")
	for _, s := range stores {
		require.NoError(t, s.Close())
	}
}

func TestMigrate_SchemaTooNew(t *testing.T) {
	swapMigrations(t, countingFS())
	ctx := context.Background()
	p := dbPath(t)
	s, err := Open(ctx, p, Options{})
	require.NoError(t, err)
	_, err = s.db.ExecContext(ctx, "PRAGMA user_version = 99")
	require.NoError(t, err)
	require.NoError(t, s.Close())

	_, err = Open(ctx, p, Options{})
	require.ErrorIs(t, err, ErrSchemaTooNew)
}

func TestMigrate_GapInNumbering(t *testing.T) {
	swapMigrations(t, fstest.MapFS{
		"migrations/0001_a.sql": {Data: []byte("SELECT 1;")},
		"migrations/0003_c.sql": {Data: []byte("SELECT 1;")},
	})
	_, err := Open(context.Background(), dbPath(t), Options{})
	require.Error(t, err)
}

func TestMigrate_BadNameAndFailedMigrationRollsBack(t *testing.T) {
	swapMigrations(t, fstest.MapFS{"migrations/oops.sql": {Data: []byte("SELECT 1;")}})
	_, err := Open(context.Background(), dbPath(t), Options{})
	require.Error(t, err)

	swapMigrations(t, fstest.MapFS{"migrations/0001_bad.sql": {Data: []byte("CREATE TABLE t (a); NOT SQL;")}})
	ctx := context.Background()
	p := dbPath(t)
	_, err = Open(ctx, p, Options{})
	require.Error(t, err)

	swapMigrations(t, fstest.MapFS{"migrations/0001_ok.sql": {Data: []byte("CREATE TABLE t (a);")}})
	s, err := Open(ctx, p, Options{})
	require.NoError(t, err, "failed migration must leave no partial table behind")
	require.NoError(t, s.Close())
}
