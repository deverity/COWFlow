package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers driver "sqlite" (pure Go, no cgo)

	"github.com/deverity/cowflow/internal/domain"
)

// defaultBusyTimeout is the Guidance default for Options.BusyTimeout.
const defaultBusyTimeout = 5 * time.Second

// Options configures Open.
type Options struct {
	BusyTimeout time.Duration // Guidance default 5s
	Clock       domain.Clock
	IDs         domain.IDGen
}

// Store is the handle to the SQLite database.
type Store struct {
	db    *sql.DB
	clock domain.Clock
	ids   domain.IDGen
}

// dsn builds the driver DSN. Pragmas go in the DSN so that every pooled
// connection gets them; _txlock=immediate makes every transaction BEGIN
// IMMEDIATE so concurrent writers wait on busy_timeout instead of failing on
// a lock upgrade.
func dsn(path string, busy time.Duration) string {
	q := url.Values{}
	// busy_timeout first so the journal_mode switch itself can wait on a lock.
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busy.Milliseconds()))
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "synchronous(NORMAL)") // Guidance: the usual pairing with WAL
	q.Set("_txlock", "immediate")
	return path + "?" + q.Encode()
}

// Open opens (creating if needed) the database at path and applies all
// pending embedded migrations.
func Open(ctx context.Context, path string, opt Options) (*Store, error) {
	if strings.ContainsRune(path, '?') {
		return nil, fmt.Errorf("store: path %q must not contain '?'", path)
	}
	if opt.BusyTimeout == 0 {
		opt.BusyTimeout = defaultBusyTimeout
	}
	db, err := sql.Open("sqlite", dsn(path, opt.BusyTimeout))
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close() // the ping error is the one that matters
		return nil, fmt.Errorf("store: ping %s: %w", path, err)
	}
	if opt.Clock == nil {
		opt.Clock = domain.SystemClock{}
	}
	if opt.IDs == nil {
		opt.IDs = domain.NewULIDGen(opt.Clock)
	}
	s := &Store{db: db, clock: opt.Clock, ids: opt.IDs}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close() // the migration error is the one that matters
		return nil, err
	}
	return s, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// SchemaVersion returns the applied migration count (PRAGMA user_version).
func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var v int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); err != nil {
		return 0, fmt.Errorf("store: read schema version: %w", err)
	}
	return v, nil
}
