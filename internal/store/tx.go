package store

import (
	"context"
	"database/sql"
	"fmt"
)

// Tx is a write transaction opened by Store.WithTx.
type Tx struct {
	tx *sql.Tx
	s  *Store
}

// WithTx runs fn in a BEGIN IMMEDIATE transaction (the DSN sets
// _txlock=immediate) and commits when fn returns nil; any error rolls back
// and is returned unchanged. Keep fn short: never hold the transaction across
// Git subprocess calls or network waits.
func (s *Store) WithTx(ctx context.Context, opID string, fn func(tx *Tx) error) (err error) {
	_ = opID // stored from S2-E2; PENDING U-DATA-1 (Sprint 2: where op_id is stored)
	sqlTx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = sqlTx.Rollback() // the original error is returned
		}
	}()
	if err = fn(&Tx{tx: sqlTx, s: s}); err != nil {
		return err
	}
	if err = sqlTx.Commit(); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}
