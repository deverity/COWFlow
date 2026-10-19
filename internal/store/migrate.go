package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
)

// all: keeps the .gitkeep placeholder embeddable while no migration exists.
//
//go:embed all:migrations
var embedded embed.FS

var migrationFS fs.FS = embedded // tests swap in fstest.MapFS

// ErrSchemaTooNew means the database was written by a newer binary.
var ErrSchemaTooNew = errors.New("store: database schema is newer than this binary")

type migration struct {
	version int
	name    string
	sql     string
}

var migrationName = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.sql$`)

// loadMigrations parses "NNNN_name.sql", sorts by NNNN, and requires 1..N with no gaps.
func loadMigrations(fsys fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, "migrations")
	if err != nil {
		return nil, fmt.Errorf("store: read migrations: %w", err)
	}
	var ms []migration
	for _, e := range entries {
		if e.IsDir() || e.Name() == ".gitkeep" {
			continue
		}
		m := migrationName.FindStringSubmatch(e.Name())
		if m == nil {
			return nil, fmt.Errorf("store: migration file %q does not match NNNN_name.sql", e.Name())
		}
		v, err := strconv.Atoi(m[1])
		if err != nil {
			return nil, fmt.Errorf("store: migration %q: %w", e.Name(), err)
		}
		body, err := fs.ReadFile(fsys, "migrations/"+e.Name())
		if err != nil {
			return nil, fmt.Errorf("store: read migration %q: %w", e.Name(), err)
		}
		ms = append(ms, migration{version: v, name: m[2], sql: string(body)})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].version < ms[j].version })
	for i, m := range ms {
		if m.version != i+1 {
			return nil, fmt.Errorf("store: migration numbering must be 1..N without gaps or duplicates: expected %04d, found %04d_%s", i+1, m.version, m.name)
		}
	}
	return ms, nil
}

func (s *Store) migrate(ctx context.Context) error {
	ms, err := loadMigrations(migrationFS)
	if err != nil {
		return err
	}
	for {
		applied, err := s.applyNext(ctx, ms) // one BEGIN IMMEDIATE tx per migration
		if err != nil {
			return err
		}
		if !applied {
			return nil
		}
	}
}

// applyNext applies at most one migration. It reads user_version inside the
// transaction so a concurrent process that migrated first is observed.
func (s *Store) applyNext(ctx context.Context, ms []migration) (applied bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil) // _txlock=immediate -> BEGIN IMMEDIATE
	if err != nil {
		return false, fmt.Errorf("store: begin migration: %w", err)
	}
	defer func() {
		if err != nil || !applied {
			_ = tx.Rollback() // the original error is returned
		}
	}()
	var cur int
	if err = tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&cur); err != nil {
		return false, fmt.Errorf("store: read user_version: %w", err)
	}
	if cur > len(ms) {
		err = fmt.Errorf("%w: database version %d, binary knows %d", ErrSchemaTooNew, cur, len(ms))
		return false, err
	}
	if cur == len(ms) {
		return false, nil
	}
	m := ms[cur]
	if err = execMigration(ctx, tx, m); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, fmt.Errorf("store: commit migration %04d_%s: %w", m.version, m.name, err)
	}
	return true, nil
}

func execMigration(ctx context.Context, tx *sql.Tx, m migration) error {
	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return fmt.Errorf("store: apply migration %04d_%s: %w", m.version, m.name, err)
	}
	// PRAGMA does not accept bound parameters; version is an int we parsed.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", m.version)); err != nil {
		return fmt.Errorf("store: set user_version %d: %w", m.version, err)
	}
	return nil
}
