package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/deverity/cowflow/internal/domain"
)

const workspaceCols = `id, project_id, task_id, COALESCE(session_id, ''), provider, cow_kind, filesystem,
	path, branch, base_sha, sparse_paths, port_slot, port_start, port_end, state, last_error,
	op_id, created_at, updated_at`

// liveSlotsQuery lists the port slots in use by a project. Its state filter
// must stay identical to the WHERE clause of the workspaces_live_slot index
// in migration 0002.
const liveSlotsQuery = `SELECT port_slot FROM workspaces
	WHERE project_id = ? AND state NOT IN ('destroyed','failed') ORDER BY port_slot`

// maxPort is the highest TCP port a slot may reach.
const maxPort = 65535

type rowScanner interface{ Scan(dest ...any) error }

func scanWorkspace(sc rowScanner) (domain.WorkspaceRecord, error) {
	var r domain.WorkspaceRecord
	var sparse, state, created, updated string
	err := sc.Scan(&r.ID, &r.ProjectID, &r.TaskID, &r.SessionID, &r.Provider, &r.CowKind,
		&r.Filesystem, &r.Path, &r.Branch, &r.BaseSHA, &sparse, &r.PortSlot, &r.PortStart,
		&r.PortEnd, &state, &r.LastError, &r.OpID, &created, &updated)
	if err != nil {
		return domain.WorkspaceRecord{}, err
	}
	r.State = domain.WorkspaceState(state)
	if r.SparsePaths, err = unmarshalStrings(sparse); err != nil {
		return domain.WorkspaceRecord{}, err
	}
	if r.CreatedAt, err = parseTime(created); err != nil {
		return domain.WorkspaceRecord{}, err
	}
	if r.UpdatedAt, err = parseTime(updated); err != nil {
		return domain.WorkspaceRecord{}, err
	}
	return r, nil
}

// InsertWorkspace inserts a workspace row and allocates the lowest free port
// slot of the project. The initial state is creating (the default) or
// pending_mount (U-DATA-5). It is idempotent by OpID: when a row with the same
// OpID exists it is returned unchanged. Port slot n spans perWS ports from
// start+n*perWS; PortEnd is inclusive (U-PLAT-14). ID, CreatedAt and
// UpdatedAt are filled in by the store.
func (s *Store) InsertWorkspace(ctx context.Context, r domain.WorkspaceRecord, perWS, start int) (domain.WorkspaceRecord, error) {
	switch {
	case r.OpID == "" || r.ProjectID == "" || r.TaskID == "":
		return domain.WorkspaceRecord{}, errors.New("store: insert workspace: op_id, project_id and task_id are required")
	case perWS < 1 || start < 0:
		return domain.WorkspaceRecord{}, fmt.Errorf("store: insert workspace: invalid port range (per workspace %d, start %d)", perWS, start)
	}
	if r.State == "" {
		r.State = domain.WorkspaceCreating
	}
	if r.State != domain.WorkspaceCreating && r.State != domain.WorkspacePendingMount {
		return domain.WorkspaceRecord{}, fmt.Errorf("%w: a new workspace cannot start in state %q", ErrInvalidTransition, r.State)
	}
	sparse, err := marshalStrings(r.SparsePaths)
	if err != nil {
		return domain.WorkspaceRecord{}, err
	}
	var out domain.WorkspaceRecord
	err = s.WithTx(ctx, r.OpID, func(tx *Tx) error {
		existing, err := scanWorkspace(tx.tx.QueryRowContext(ctx,
			"SELECT "+workspaceCols+" FROM workspaces WHERE op_id = ?", r.OpID))
		if err == nil {
			out = existing
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("store: look up workspace by op_id: %w", err)
		}
		slot, err := lowestFreeSlot(ctx, tx, r.ProjectID)
		if err != nil {
			return err
		}
		r.PortSlot = slot
		r.PortStart = start + slot*perWS
		r.PortEnd = r.PortStart + perWS - 1
		if r.PortEnd > maxPort {
			return fmt.Errorf("%w: slot %d would end at port %d", ErrPortRangeExhausted, slot, r.PortEnd)
		}
		if r.ID == "" {
			if r.ID, err = s.ids.New(domain.KindWorkspace); err != nil {
				return fmt.Errorf("store: new workspace id: %w", err)
			}
		}
		now := s.clock.Now().UTC()
		r.CreatedAt, r.UpdatedAt = now, now
		r.LastError = ""
		var session any // NULL when no agent session owns the workspace
		if r.SessionID != "" {
			session = r.SessionID
		}
		_, err = tx.tx.ExecContext(ctx,
			`INSERT INTO workspaces (id, project_id, task_id, session_id, provider, cow_kind, filesystem,
			   path, branch, base_sha, sparse_paths, port_slot, port_start, port_end, state, last_error,
			   op_id, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.ID, r.ProjectID, r.TaskID, session, r.Provider, r.CowKind, r.Filesystem,
			r.Path, r.Branch, r.BaseSHA, sparse, r.PortSlot, r.PortStart, r.PortEnd, string(r.State),
			r.LastError, r.OpID, formatTime(now), formatTime(now))
		if err != nil {
			if isUniqueViolation(err) {
				return fmt.Errorf("%w: workspace insert: %w", ErrConflict, err)
			}
			return fmt.Errorf("store: insert workspace: %w", err)
		}
		if r.SparsePaths == nil {
			r.SparsePaths = []string{}
		}
		out = r
		return nil
	})
	if err != nil {
		return domain.WorkspaceRecord{}, err
	}
	return out, nil
}

// lowestFreeSlot returns the smallest port slot not held by a live workspace.
func lowestFreeSlot(ctx context.Context, tx *Tx, projectID string) (int, error) {
	rows, err := tx.tx.QueryContext(ctx, liveSlotsQuery, projectID)
	if err != nil {
		return 0, fmt.Errorf("store: read live port slots: %w", err)
	}
	defer func() { _ = rows.Close() }() // the rows error below is what matters
	next := 0
	for rows.Next() {
		var slot int
		if err := rows.Scan(&slot); err != nil {
			return 0, fmt.Errorf("store: scan port slot: %w", err)
		}
		if slot != next {
			break
		}
		next++
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("store: read live port slots: %w", err)
	}
	return next, nil
}

// allowedTransitions is the U-DATA-5 transition table. destroyed -> destroyed
// is handled separately as an idempotent no-op; pending_cleanup ->
// pending_cleanup lets a resumed destroy that gets stuck again record its new
// diagnostics.
var allowedTransitions = map[domain.WorkspaceState][]domain.WorkspaceState{
	domain.WorkspacePendingMount:   {domain.WorkspaceCreating},
	domain.WorkspaceCreating:       {domain.WorkspaceReady, domain.WorkspaceFailed},
	domain.WorkspaceReady:          {domain.WorkspacePendingCleanup, domain.WorkspaceDestroyed},
	domain.WorkspacePendingCleanup: {domain.WorkspacePendingCleanup, domain.WorkspaceDestroyed},
}

func transitionAllowed(from, to domain.WorkspaceState) bool {
	for _, t := range allowedTransitions[from] {
		if t == to {
			return true
		}
	}
	return false
}

// auditDetail is the JSON stored in audit_log.detail_json for a state change.
type auditDetail struct {
	ProjectID string `json:"project_id"`
	TaskID    string `json:"task_id"`
	From      string `json:"from"`
	To        string `json:"to"`
	Force     bool   `json:"force"`
	Dirty     string `json:"dirty,omitempty"`
	RescueRef string `json:"rescue_ref,omitempty"`
}

// SetWorkspaceState moves a workspace to state "to" (U-DATA-5 transitions)
// and, in the same transaction, records c.LastError, the audit_log row when
// c.Audit is set and the event when c.Event is set. On the pending_mount ->
// creating transition the non-empty c.Provider, c.CowKind, c.Filesystem and
// c.Path replace the stored values, so the row records what the manager
// finally selected; on any other transition setting one is refused with
// ErrInvalidTransition. Moving a destroyed
// workspace to destroyed again is a no-op that writes nothing.
func (s *Store) SetWorkspaceState(ctx context.Context, id string, to domain.WorkspaceState, c domain.StateChange) error {
	if !to.Valid() {
		return fmt.Errorf("store: set workspace state: unknown state %q", to)
	}
	if c.Audit != nil && c.Audit.Action == "" {
		return errors.New("store: set workspace state: audit action is required")
	}
	if c.Event != nil && c.Event.Type != domain.EventWorkspacePendingCleanup &&
		c.Event.Type != domain.EventWorkspacePendingMount {
		return fmt.Errorf("store: set workspace state: event type %q is not a workspace event", c.Event.Type)
	}
	selection := c.Provider != "" || c.CowKind != "" || c.Filesystem != "" || c.Path != ""
	return s.WithTx(ctx, "", func(tx *Tx) error {
		cur, err := scanWorkspace(tx.tx.QueryRowContext(ctx,
			"SELECT "+workspaceCols+" FROM workspaces WHERE id = ?", id))
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: workspace %q", ErrNotFound, id)
		}
		if err != nil {
			return fmt.Errorf("store: read workspace: %w", err)
		}
		if cur.State == domain.WorkspaceDestroyed && to == domain.WorkspaceDestroyed {
			return nil
		}
		if !transitionAllowed(cur.State, to) {
			return fmt.Errorf("%w: workspace %s: %s -> %s", ErrInvalidTransition, id, cur.State, to)
		}
		retry := cur.State == domain.WorkspacePendingMount && to == domain.WorkspaceCreating
		if selection && !retry {
			return fmt.Errorf("%w: workspace %s: provider selection is only recorded on pending_mount -> creating, not %s -> %s",
				ErrInvalidTransition, id, cur.State, to)
		}
		now := s.clock.Now().UTC()
		// An empty selection field keeps the stored value.
		if _, err := tx.tx.ExecContext(ctx,
			`UPDATE workspaces SET state = ?, last_error = ?, updated_at = ?,
			   provider = COALESCE(NULLIF(?, ''), provider), cow_kind = COALESCE(NULLIF(?, ''), cow_kind),
			   filesystem = COALESCE(NULLIF(?, ''), filesystem), path = COALESCE(NULLIF(?, ''), path)
			 WHERE id = ?`,
			string(to), c.LastError, formatTime(now), c.Provider, c.CowKind, c.Filesystem, c.Path, id); err != nil {
			return fmt.Errorf("store: update workspace state: %w", err)
		}
		if c.Audit != nil {
			if err := s.insertAudit(ctx, tx, now, cur, to, *c.Audit); err != nil {
				return err
			}
		}
		if c.Event != nil {
			ev := *c.Event
			if ev.ProjectID == "" {
				ev.ProjectID = cur.ProjectID
			}
			if ev.WorkspaceID == "" {
				ev.WorkspaceID = cur.ID
			}
			if ev.TaskID == "" {
				ev.TaskID = cur.TaskID
			}
			if err := s.sink.Emit(ev); err != nil {
				return fmt.Errorf("store: emit %s: %w", ev.Type, err)
			}
		}
		return nil
	})
}

func (s *Store) insertAudit(ctx context.Context, tx *Tx, now time.Time, cur domain.WorkspaceRecord, to domain.WorkspaceState, a domain.StateAudit) error {
	id, err := s.ids.New(domain.KindAuditEntry)
	if err != nil {
		return fmt.Errorf("store: new audit id: %w", err)
	}
	detail, err := json.Marshal(auditDetail{
		ProjectID: cur.ProjectID, TaskID: cur.TaskID, From: string(cur.State), To: string(to),
		Force: a.Force, Dirty: a.Dirty, RescueRef: a.RescueRef,
	})
	if err != nil {
		return fmt.Errorf("store: encode audit detail: %w", err)
	}
	if _, err := tx.tx.ExecContext(ctx,
		`INSERT INTO audit_log (id, at, actor_kind, actor_id, action, subject_id, detail_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, formatTime(now), a.ActorKind, a.ActorID, a.Action, cur.ID, string(detail)); err != nil {
		return fmt.Errorf("store: insert audit row: %w", err)
	}
	return nil
}

// UpdateWorkspaceSparsePaths replaces the sparse paths of a ready workspace
// (ExpandScope). The caller passes the already normalized, complete set; nil
// is stored as an empty list. A missing workspace is ErrNotFound; a workspace
// in any other state is ErrInvalidTransition and keeps its stored paths.
func (s *Store) UpdateWorkspaceSparsePaths(ctx context.Context, workspaceID string, paths []string) error {
	enc, err := marshalStrings(paths)
	if err != nil {
		return err
	}
	return s.WithTx(ctx, "", func(tx *Tx) error {
		var state string
		err := tx.tx.QueryRowContext(ctx, "SELECT state FROM workspaces WHERE id = ?", workspaceID).Scan(&state)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: workspace %q", ErrNotFound, workspaceID)
		}
		if err != nil {
			return fmt.Errorf("store: read workspace: %w", err)
		}
		if domain.WorkspaceState(state) != domain.WorkspaceReady {
			return fmt.Errorf("%w: workspace %s is %s, sparse paths change only when ready", ErrInvalidTransition, workspaceID, state)
		}
		if _, err := tx.tx.ExecContext(ctx,
			"UPDATE workspaces SET sparse_paths = ?, updated_at = ? WHERE id = ?",
			enc, formatTime(s.clock.Now().UTC()), workspaceID); err != nil {
			return fmt.Errorf("store: update sparse paths: %w", err)
		}
		return nil
	})
}

// GetWorkspace returns the workspace with the ID, or ErrNotFound.
func (s *Store) GetWorkspace(ctx context.Context, id string) (domain.WorkspaceRecord, error) {
	r, err := scanWorkspace(s.db.QueryRowContext(ctx,
		"SELECT "+workspaceCols+" FROM workspaces WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WorkspaceRecord{}, fmt.Errorf("%w: workspace %q", ErrNotFound, id)
	}
	if err != nil {
		return domain.WorkspaceRecord{}, fmt.Errorf("store: get workspace: %w", err)
	}
	return r, nil
}

// ListWorkspaces returns the workspaces matching f, oldest first.
func (s *Store) ListWorkspaces(ctx context.Context, f domain.WorkspaceFilter) ([]domain.WorkspaceRecord, error) {
	states := make([]string, len(f.States))
	for i, st := range f.States {
		states[i] = string(st)
	}
	statesJSON, err := marshalStrings(states)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+workspaceCols+` FROM workspaces
		 WHERE (?1 = '' OR project_id = ?1) AND (?2 = '' OR task_id = ?2)
		   AND (?3 = '[]' OR state IN (SELECT value FROM json_each(?3)))
		 ORDER BY created_at, id`, f.ProjectID, f.TaskID, statesJSON)
	if err != nil {
		return nil, fmt.Errorf("store: list workspaces: %w", err)
	}
	defer func() { _ = rows.Close() }() // the rows error below is what matters
	var out []domain.WorkspaceRecord
	for rows.Next() {
		r, err := scanWorkspace(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan workspace: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list workspaces: %w", err)
	}
	return out, nil
}
