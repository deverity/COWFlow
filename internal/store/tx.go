package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Tx is a write transaction opened by Store.WithTx.
type Tx struct {
	tx *sql.Tx
	s  *Store
}

// begin starts a write transaction. SQLite's busy handler is not fair, so
// under heavy writer contention (notably on slow Windows runners) one BEGIN
// IMMEDIATE can wait out busy_timeout and still get SQLITE_BUSY. Callers
// should not see that as a failure under normal concurrent use, so retry for
// up to another busy_timeout (bounded, and cut short by ctx).
func (s *Store) begin(ctx context.Context) (*sql.Tx, error) {
	deadline := time.Now().Add(s.busy)
	for {
		sqlTx, err := s.db.BeginTx(ctx, nil)
		if err == nil || !isBusy(err) || !time.Now().Before(deadline) {
			return sqlTx, err
		}
		t := time.NewTimer(pingRetry)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, errors.Join(err, ctx.Err())
		case <-t.C:
		}
	}
}

// WithTx runs fn in a BEGIN IMMEDIATE transaction (the DSN sets
// _txlock=immediate) and commits when fn returns nil; any error rolls back
// and is returned unchanged. Keep fn short: never hold the transaction across
// Git subprocess calls or network waits.
func (s *Store) WithTx(ctx context.Context, opID string, fn func(tx *Tx) error) (err error) {
	_ = opID // stored from S2-E2; PENDING U-DATA-1 (Sprint 2: where op_id is stored)
	sqlTx, err := s.begin(ctx)
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
