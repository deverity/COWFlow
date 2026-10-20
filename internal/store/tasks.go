package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/deverity/cowflow/internal/domain"
)

// S2-E1 extends tasks.

// CreateTask inserts a minimal task row in state "created". The branch must
// already be computed (domain.BranchNamer) and is stored verbatim. A
// duplicate (project, slug) returns ErrConflict. ID and CreatedAt are filled
// in when empty.
func (s *Store) CreateTask(ctx context.Context, t domain.Task) (domain.Task, error) {
	if err := domain.ValidateSlug(t.Slug); err != nil {
		return domain.Task{}, err
	}
	if t.ProjectID == "" || t.Base == "" || t.BaseSHA == "" || t.Branch == "" {
		return domain.Task{}, errors.New("store: create task: project, base, base_sha and branch are required")
	}
	paths, err := marshalStrings(t.Paths)
	if err != nil {
		return domain.Task{}, err
	}
	err = s.WithTx(ctx, "", func(tx *Tx) error {
		if t.ID == "" {
			id, err := s.ids.New(domain.KindTask)
			if err != nil {
				return fmt.Errorf("store: new task id: %w", err)
			}
			t.ID = id
		}
		t.State = domain.TaskStateCreated
		t.CreatedAt = s.clock.Now().UTC()
		_, err := tx.tx.ExecContext(ctx,
			`INSERT INTO tasks (id, project_id, slug, title, kind, base, base_sha, branch, paths, sparse, state, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			t.ID, t.ProjectID, t.Slug, t.Title, t.Kind, t.Base, t.BaseSHA, t.Branch, paths,
			boolToInt(t.Sparse), t.State, formatTime(t.CreatedAt))
		if err != nil {
			if isUniqueViolation(err) {
				return fmt.Errorf("%w: task %q already exists in project %s", ErrConflict, t.Slug, t.ProjectID)
			}
			return fmt.Errorf("store: insert task: %w", err)
		}
		return nil
	})
	if err != nil {
		return domain.Task{}, err
	}
	return t, nil
}

// GetTaskBySlug returns the task with the slug in the project, or ErrNotFound.
func (s *Store) GetTaskBySlug(ctx context.Context, projectID, slug string) (domain.Task, error) {
	var t domain.Task
	var paths, created string
	var sparse int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, project_id, slug, title, kind, base, base_sha, branch, paths, sparse, state, created_at
		 FROM tasks WHERE project_id = ? AND slug = ?`, projectID, slug).
		Scan(&t.ID, &t.ProjectID, &t.Slug, &t.Title, &t.Kind, &t.Base, &t.BaseSHA, &t.Branch,
			&paths, &sparse, &t.State, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Task{}, fmt.Errorf("%w: task %q", ErrNotFound, slug)
	}
	if err != nil {
		return domain.Task{}, fmt.Errorf("store: get task: %w", err)
	}
	t.Sparse = sparse != 0
	if t.Paths, err = unmarshalStrings(paths); err != nil {
		return domain.Task{}, err
	}
	if t.CreatedAt, err = parseTime(created); err != nil {
		return domain.Task{}, err
	}
	return t, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// marshalStrings encodes a string slice as a JSON array; nil becomes "[]".
func marshalStrings(v []string) (string, error) {
	if v == nil {
		v = []string{}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("store: encode json: %w", err)
	}
	return string(b), nil
}

func unmarshalStrings(s string) ([]string, error) {
	var v []string
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, fmt.Errorf("store: decode json %q: %w", s, err)
	}
	if v == nil {
		v = []string{}
	}
	return v, nil
}
