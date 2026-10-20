package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/deverity/cowflow/internal/domain"
)

const goldenCols = `id, project_id, base_sha, state, size_bytes, file_count, in_use, builder_op_id,
	last_error, created_at, last_used_at, updated_at`

func scanGolden(sc rowScanner) (domain.GoldenTree, error) {
	var g domain.GoldenTree
	var state, created, used, updated string
	err := sc.Scan(&g.ID, &g.ProjectID, &g.BaseSHA, &state, &g.SizeBytes, &g.FileCount, &g.InUse,
		&g.BuilderOpID, &g.LastError, &created, &used, &updated)
	if err != nil {
		return domain.GoldenTree{}, err
	}
	g.State = domain.GoldenState(state)
	if g.CreatedAt, err = parseTime(created); err != nil {
		return domain.GoldenTree{}, err
	}
	if g.LastUsedAt, err = parseTime(used); err != nil {
		return domain.GoldenTree{}, err
	}
	if g.UpdatedAt, err = parseTime(updated); err != nil {
		return domain.GoldenTree{}, err
	}
	return g, nil
}

// goldenTx reads one golden row inside a transaction.
func goldenTx(ctx context.Context, tx *Tx, projectID, sha string) (domain.GoldenTree, error) {
	return scanGolden(tx.tx.QueryRowContext(ctx,
		"SELECT "+goldenCols+" FROM golden_trees WHERE project_id = ? AND base_sha = ?", projectID, sha))
}

// ClaimGolden decides, in one transaction, who builds the golden tree of
// (projectID, sha) (U-PLAT-13):
//
//   - no row: insert it as building and return Build;
//   - failed: take it over as building (a retry) and return Build;
//   - ready: take an in_use reference (the caller releases it with
//     UseGolden(-1)) and return Ready;
//   - building and the caller's opID is the builder: return Build again;
//   - building and updated_at older than staleAfter: take it over with the new
//     opID and return Build;
//   - anything else (building, or deleting): return Wait.
func (s *Store) ClaimGolden(ctx context.Context, projectID, sha, opID string, staleAfter time.Duration) (domain.GoldenClaim, error) {
	if projectID == "" || sha == "" || opID == "" {
		return domain.GoldenClaim{}, errors.New("store: claim golden: project, sha and op_id are required")
	}
	if staleAfter <= 0 {
		return domain.GoldenClaim{}, errors.New("store: claim golden: staleAfter must be positive")
	}
	var claim domain.GoldenClaim
	err := s.WithTx(ctx, opID, func(tx *Tx) error {
		now := s.clock.Now().UTC()
		g, err := goldenTx(ctx, tx, projectID, sha)
		if errors.Is(err, sql.ErrNoRows) {
			id, err := s.ids.New(domain.KindGoldenTree)
			if err != nil {
				return fmt.Errorf("store: new golden tree id: %w", err)
			}
			if _, err := tx.tx.ExecContext(ctx,
				`INSERT INTO golden_trees (id, project_id, base_sha, state, builder_op_id, created_at, last_used_at, updated_at)
				 VALUES (?, ?, ?, 'building', ?, ?, ?, ?)`,
				id, projectID, sha, opID, formatTime(now), formatTime(now), formatTime(now)); err != nil {
				return fmt.Errorf("store: insert golden tree: %w", err)
			}
			return s.finishClaim(ctx, tx, &claim, domain.GoldenActionBuild, projectID, sha)
		}
		if err != nil {
			return fmt.Errorf("store: read golden tree: %w", err)
		}
		switch {
		case g.State == domain.GoldenReady:
			if _, err := tx.tx.ExecContext(ctx,
				"UPDATE golden_trees SET in_use = in_use + 1, last_used_at = ?, updated_at = ? WHERE id = ?",
				formatTime(now), formatTime(now), g.ID); err != nil {
				return fmt.Errorf("store: use golden tree: %w", err)
			}
			return s.finishClaim(ctx, tx, &claim, domain.GoldenActionReady, projectID, sha)
		case g.State == domain.GoldenFailed,
			g.State == domain.GoldenBuilding && (g.BuilderOpID == opID || !g.UpdatedAt.Add(staleAfter).After(now)):
			if _, err := tx.tx.ExecContext(ctx,
				"UPDATE golden_trees SET state = 'building', builder_op_id = ?, last_error = '', updated_at = ? WHERE id = ?",
				opID, formatTime(now), g.ID); err != nil {
				return fmt.Errorf("store: take over golden tree: %w", err)
			}
			return s.finishClaim(ctx, tx, &claim, domain.GoldenActionBuild, projectID, sha)
		}
		claim = domain.GoldenClaim{Action: domain.GoldenActionWait, Tree: g}
		return nil
	})
	if err != nil {
		return domain.GoldenClaim{}, err
	}
	return claim, nil
}

func (s *Store) finishClaim(ctx context.Context, tx *Tx, claim *domain.GoldenClaim, a domain.GoldenAction, projectID, sha string) error {
	g, err := goldenTx(ctx, tx, projectID, sha)
	if err != nil {
		return fmt.Errorf("store: re-read golden tree: %w", err)
	}
	*claim = domain.GoldenClaim{Action: a, Tree: g}
	return nil
}

// updateGolden runs apply on a golden row inside one transaction, only when
// the row is in one of the allowed states. A missing row is ErrNotFound; a
// row in another state is ErrInvalidTransition.
func (s *Store) updateGolden(ctx context.Context, projectID, sha string, apply func(tx *Tx, g domain.GoldenTree) error, from ...domain.GoldenState) error {
	return s.WithTx(ctx, "", func(tx *Tx) error {
		g, err := goldenTx(ctx, tx, projectID, sha)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: golden tree %s", ErrNotFound, sha)
		}
		if err != nil {
			return fmt.Errorf("store: read golden tree: %w", err)
		}
		if !slices.Contains(from, g.State) {
			return fmt.Errorf("%w: golden tree %s is %s", ErrInvalidTransition, sha, g.State)
		}
		if err := apply(tx, g); err != nil {
			return fmt.Errorf("store: update golden tree: %w", err)
		}
		return nil
	})
}

// FinishGolden marks a building tree ready and records its size.
func (s *Store) FinishGolden(ctx context.Context, projectID, sha string, sizeBytes int64, files int) error {
	now := formatTime(s.clock.Now())
	return s.updateGolden(ctx, projectID, sha, func(tx *Tx, g domain.GoldenTree) error {
		_, err := tx.tx.ExecContext(ctx,
			"UPDATE golden_trees SET state = 'ready', size_bytes = ?, file_count = ?, last_error = '', updated_at = ? WHERE id = ?",
			sizeBytes, files, now, g.ID)
		return err
	}, domain.GoldenBuilding)
}

// FailGolden marks a building tree failed and keeps reason in last_error.
// `golden gc` removes failed rows; a later ClaimGolden retries them.
func (s *Store) FailGolden(ctx context.Context, projectID, sha, reason string) error {
	now := formatTime(s.clock.Now())
	return s.updateGolden(ctx, projectID, sha, func(tx *Tx, g domain.GoldenTree) error {
		_, err := tx.tx.ExecContext(ctx,
			"UPDATE golden_trees SET state = 'failed', last_error = ?, updated_at = ? WHERE id = ?",
			reason, now, g.ID)
		return err
	}, domain.GoldenBuilding)
}

// UseGolden adds delta (+1 or -1) to in_use and refreshes last_used_at. A
// reference can only be taken on a ready tree. A release never takes in_use
// below zero: gc may have reset a stale count first (U-PLAT-13).
func (s *Store) UseGolden(ctx context.Context, projectID, sha string, delta int) error {
	if delta != 1 && delta != -1 {
		return fmt.Errorf("store: use golden: delta must be +1 or -1, got %d", delta)
	}
	now := formatTime(s.clock.Now())
	from := []domain.GoldenState{domain.GoldenBuilding, domain.GoldenReady, domain.GoldenFailed, domain.GoldenDeleting}
	if delta > 0 {
		from = []domain.GoldenState{domain.GoldenReady}
	}
	return s.updateGolden(ctx, projectID, sha, func(tx *Tx, g domain.GoldenTree) error {
		_, err := tx.tx.ExecContext(ctx,
			"UPDATE golden_trees SET in_use = MAX(0, in_use + ?), last_used_at = ? WHERE id = ?",
			delta, now, g.ID)
		return err
	}, from...)
}

// ListGolden returns the project's golden trees, least recently used first.
func (s *Store) ListGolden(ctx context.Context, projectID string) ([]domain.GoldenTree, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+goldenCols+" FROM golden_trees WHERE project_id = ? ORDER BY last_used_at, base_sha", projectID)
	if err != nil {
		return nil, fmt.Errorf("store: list golden trees: %w", err)
	}
	defer func() { _ = rows.Close() }() // the rows error below is what matters
	var out []domain.GoldenTree
	for rows.Next() {
		g, err := scanGolden(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan golden tree: %w", err)
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list golden trees: %w", err)
	}
	return out, nil
}

// MarkGoldenDeleting moves a ready tree with in_use = 0 to deleting and
// reports true. It reports false, changing nothing, when the tree is in use
// or in any other state; this check keeps GC from racing a clone.
func (s *Store) MarkGoldenDeleting(ctx context.Context, projectID, sha string) (bool, error) {
	var marked bool
	err := s.WithTx(ctx, "", func(tx *Tx) error {
		g, err := goldenTx(ctx, tx, projectID, sha)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: golden tree %s", ErrNotFound, sha)
		}
		if err != nil {
			return fmt.Errorf("store: read golden tree: %w", err)
		}
		if g.State != domain.GoldenReady || g.InUse > 0 {
			return nil
		}
		if _, err := tx.tx.ExecContext(ctx,
			"UPDATE golden_trees SET state = 'deleting', updated_at = ? WHERE id = ?",
			formatTime(s.clock.Now()), g.ID); err != nil {
			return fmt.Errorf("store: mark golden tree deleting: %w", err)
		}
		marked = true
		return nil
	})
	return marked, err
}

// DeleteGolden removes the row of a tree that is deleting (its directory is
// gone) or failed (gc cleanup). A missing row is not an error, so GC retries
// are idempotent; a row in another state is ErrInvalidTransition.
func (s *Store) DeleteGolden(ctx context.Context, projectID, sha string) error {
	return s.WithTx(ctx, "", func(tx *Tx) error {
		g, err := goldenTx(ctx, tx, projectID, sha)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("store: read golden tree: %w", err)
		}
		if g.State != domain.GoldenDeleting && g.State != domain.GoldenFailed {
			return fmt.Errorf("%w: golden tree %s is %s", ErrInvalidTransition, sha, g.State)
		}
		if _, err := tx.tx.ExecContext(ctx, "DELETE FROM golden_trees WHERE id = ?", g.ID); err != nil {
			return fmt.Errorf("store: delete golden tree: %w", err)
		}
		return nil
	})
}
