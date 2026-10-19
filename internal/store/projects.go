package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"modernc.org/sqlite"

	"github.com/deverity/cowflow/internal/domain"
)

// Extended SQLite result codes for constraint violations.
const (
	sqliteConstraintPrimaryKey = 1555
	sqliteConstraintUnique     = 2067
)

func isUniqueViolation(err error) bool {
	var se *sqlite.Error
	if !errors.As(err, &se) {
		return false
	}
	c := se.Code()
	return c == sqliteConstraintUnique || c == sqliteConstraintPrimaryKey
}

// InsertProject registers a project. repoRoot must already be canonical
// (platform.Canonical). A duplicate repoRoot returns ErrConflict.
func (t *Tx) InsertProject(name, repoRoot string) (domain.Project, error) {
	id, err := t.s.ids.New(domain.KindProject)
	if err != nil {
		return domain.Project{}, fmt.Errorf("store: new project id: %w", err)
	}
	now := t.s.clock.Now().UTC()
	_, err = t.tx.Exec(
		"INSERT INTO projects (id, name, repo_root, created_at) VALUES (?, ?, ?, ?)",
		id, name, repoRoot, now.Format(time.RFC3339Nano))
	if err != nil {
		if isUniqueViolation(err) {
			return domain.Project{}, fmt.Errorf("%w: project for %q already exists", ErrConflict, repoRoot)
		}
		return domain.Project{}, fmt.Errorf("store: insert project: %w", err)
	}
	return domain.Project{ID: id, Name: name, RepoRoot: repoRoot, CreatedAt: now}, nil
}

// ProjectByRoot returns the project registered for root, or ErrNotFound.
func (s *Store) ProjectByRoot(ctx context.Context, root string) (domain.Project, error) {
	var p domain.Project
	var created string
	err := s.db.QueryRowContext(ctx,
		"SELECT id, name, repo_root, created_at FROM projects WHERE repo_root = ?", root).
		Scan(&p.ID, &p.Name, &p.RepoRoot, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Project{}, fmt.Errorf("%w: project for %q", ErrNotFound, root)
	}
	if err != nil {
		return domain.Project{}, fmt.Errorf("store: project by root: %w", err)
	}
	if p.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return domain.Project{}, fmt.Errorf("store: parse created_at %q: %w", created, err)
	}
	return p, nil
}
