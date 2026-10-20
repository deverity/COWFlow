package workspace

import (
	"context"
	"time"

	"github.com/deverity/cowflow/internal/domain"
)

// Store is the persistence the workspace package uses (sprint-1.md §6.5).
// *store.Store satisfies it. Every method is one short transaction; callers
// never hold a transaction across a git or clone call.
type Store interface {
	// InsertWorkspace inserts the row in state creating or pending_mount,
	// allocates the lowest free port slot and is idempotent by OpID.
	InsertWorkspace(ctx context.Context, r domain.WorkspaceRecord, perWS, start int) (domain.WorkspaceRecord, error)
	// SetWorkspaceState changes the state and, in the same transaction,
	// records the optional event and audit row (SCOPE.md §13, §17).
	SetWorkspaceState(ctx context.Context, id string, to domain.WorkspaceState, c domain.StateChange) error
	GetWorkspace(ctx context.Context, id string) (domain.WorkspaceRecord, error)
	ListWorkspaces(ctx context.Context, f domain.WorkspaceFilter) ([]domain.WorkspaceRecord, error)

	ClaimGolden(ctx context.Context, projectID, sha, opID string, staleAfter time.Duration) (domain.GoldenClaim, error)
	FinishGolden(ctx context.Context, projectID, sha string, sizeBytes int64, files int) error
	FailGolden(ctx context.Context, projectID, sha, reason string) error
	UseGolden(ctx context.Context, projectID, sha string, delta int) error // in_use ±1, last_used_at
	ListGolden(ctx context.Context, projectID string) ([]domain.GoldenTree, error)
	MarkGoldenDeleting(ctx context.Context, projectID, sha string) (bool, error) // false if in use
	DeleteGolden(ctx context.Context, projectID, sha string) error
	// ResetStaleGoldenInUse sets in_use to 0 on trees last used before cutoff
	// and returns their base SHAs (U-PLAT-13).
	ResetStaleGoldenInUse(ctx context.Context, projectID string, cutoff time.Time) ([]string, error)

	CreateTask(ctx context.Context, t domain.Task) (domain.Task, error) // minimal; S2-E1 extends
	GetTaskBySlug(ctx context.Context, projectID, slug string) (domain.Task, error)
}
