package domain

import "time"

// WorkspaceState is the lifecycle state of a workspaces row (SCOPE.md §7,
// SPEC-GAPS U-DATA-5).
//
// Transitions: insert -> creating (mount gate passed) or pending_mount (row
// and event, no directory); pending_mount -> creating (retry, same op_id);
// creating -> ready; creating -> failed (terminal; retrying means a new
// op_id and a new row); ready -> pending_cleanup (stuck handle);
// ready or pending_cleanup -> destroyed (terminal, idempotent).
type WorkspaceState string

// Workspace states.
const (
	WorkspaceCreating       WorkspaceState = "creating"
	WorkspacePendingMount   WorkspaceState = "pending_mount"
	WorkspaceReady          WorkspaceState = "ready"
	WorkspaceFailed         WorkspaceState = "failed"
	WorkspacePendingCleanup WorkspaceState = "pending_cleanup"
	WorkspaceDestroyed      WorkspaceState = "destroyed"
)

// Valid reports whether s is one of the defined workspace states.
func (s WorkspaceState) Valid() bool {
	switch s {
	case WorkspaceCreating, WorkspacePendingMount, WorkspaceReady,
		WorkspaceFailed, WorkspacePendingCleanup, WorkspaceDestroyed:
		return true
	}
	return false
}

// Live reports whether a workspace in state s still holds its port slot.
func (s WorkspaceState) Live() bool {
	return s.Valid() && s != WorkspaceDestroyed && s != WorkspaceFailed
}

// WorkspaceRecord is one row of the workspaces table (SCOPE.md §6.1, §13).
// Provider and CowKind are the string forms of the workspace package enums so
// that domain stays dependency-free.
type WorkspaceRecord struct {
	ID          string
	ProjectID   string
	TaskID      string
	SessionID   string // empty when no agent session owns the workspace
	Provider    string
	CowKind     string
	Filesystem  string
	Path        string
	Branch      string
	BaseSHA     string
	SparsePaths []string
	PortSlot    int
	PortStart   int
	PortEnd     int // inclusive: PortStart + port_range_per_workspace - 1 (U-PLAT-14)
	State       WorkspaceState
	LastError   string
	OpID        string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// WorkspaceFilter selects workspaces for ListWorkspaces. Zero fields do not
// filter.
type WorkspaceFilter struct {
	ProjectID string
	TaskID    string
	States    []WorkspaceState
}

// Workspace event types that exist in SCOPE.md §9.9.
const (
	EventWorkspacePendingCleanup = "workspace.pending_cleanup"
	EventWorkspacePendingMount   = "workspace.pending_mount"
)

// WorkspaceEvent is an event emitted in the same transaction as a state
// change. Until the events table lands (PENDING U-DATA-1) it goes through an
// EventSink.
type WorkspaceEvent struct {
	Type        string // EventWorkspacePendingCleanup or EventWorkspacePendingMount
	ProjectID   string // filled by the store when empty
	WorkspaceID string // filled by the store when empty
	TaskID      string // filled by the store when empty
	Payload     map[string]string
}

// StateAudit is the audit_log row written with a state change (SCOPE.md §17):
// destroy and destroy --force.
type StateAudit struct {
	Action    string // e.g. "destroy"
	ActorKind string
	ActorID   string
	Force     bool
	Dirty     string // summary of the unpreserved changes, if any
	RescueRef string // refs/cowflow/rescue/<workspace_id>, if a rescue ran
}

// StateChange carries what SetWorkspaceState writes together with the new
// state, in one transaction.
//
// Provider, CowKind, Filesystem and Path record what the manager finally
// selected when a pending_mount row is retried. They are applied only on the
// pending_mount -> creating transition, where a non-empty field replaces the
// stored value and an empty one keeps it. On any other transition setting one
// is an error.
type StateChange struct {
	LastError  string // stored as last_error; empty clears it
	Provider   string // selected provider (pending_mount -> creating only)
	CowKind    string // selected COW kind (pending_mount -> creating only)
	Filesystem string // filesystem of the final location (pending_mount -> creating only)
	Path       string // canonicalized workspace path (pending_mount -> creating only)
	Event      *WorkspaceEvent
	Audit      *StateAudit
}

// GoldenState is the state of a golden_trees row (U-PLAT-13).
//
// Transitions: none -> building (ClaimGolden); building -> ready
// (FinishGolden); building -> failed (FailGolden); building -> building (stale
// takeover); ready -> deleting only when in_use = 0; deleting -> row deleted.
type GoldenState string

// Golden tree states.
const (
	GoldenBuilding GoldenState = "building"
	GoldenReady    GoldenState = "ready"
	GoldenFailed   GoldenState = "failed"
	GoldenDeleting GoldenState = "deleting"
)

// GoldenTree is one row of golden_trees. Its directory is
// <golden_root>/<BaseSHA> (SCOPE.md §23 decision 18) and is not stored.
type GoldenTree struct {
	ID          string // gld_<ULID>
	ProjectID   string
	BaseSHA     string
	State       GoldenState
	SizeBytes   int64
	FileCount   int
	InUse       int // active Acquire references; existing workspaces do not pin
	BuilderOpID string
	LastError   string
	CreatedAt   time.Time
	LastUsedAt  time.Time
	UpdatedAt   time.Time
}

// GoldenAction tells the caller of ClaimGolden what to do next.
type GoldenAction string

// Golden claim outcomes.
const (
	GoldenActionBuild GoldenAction = "build" // the caller builds the tree
	GoldenActionReady GoldenAction = "ready" // use it; the caller holds an in_use reference
	GoldenActionWait  GoldenAction = "wait"  // another process is building (or deleting) it
)

// GoldenClaim is the result of ClaimGolden.
type GoldenClaim struct {
	Action GoldenAction
	Tree   GoldenTree
}
