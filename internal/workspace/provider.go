package workspace

import (
	"context"
	"strconv"
	"time"

	"github.com/deverity/cowflow/internal/domain"
)

// WorkspaceProvider is one workspace backend (SCOPE.md §6.2, verbatim).
//
//nolint:revive // exported: the name is verbatim from SCOPE.md §6.2
type WorkspaceProvider interface {
	Name() string
	Capabilities(ctx context.Context, root string) (Capabilities, error)
	Create(ctx context.Context, spec WorkspaceSpec) (*Workspace, error)
	Inspect(ctx context.Context, id string) (*WorkspaceStatus, error)
	Destroy(ctx context.Context, id string, opts DestroyOptions) (DestroyResult, error)
}

// Capabilities is what a provider actually delivers under a root (SCOPE.md
// §6.2, verbatim). It never claims more than is delivered (§2.2 principle
// 10); extra information such as a fallback warning goes in [Selection] or
// [WorkspaceStatus], never here.
type Capabilities struct {
	CopyOnWrite      CowKind // None | Block | Virtual
	SparseSupported  bool
	ProcessIsolation bool
	ResourceLimits   bool
	Filesystem       string // "refs", "ntfs", "btrfs", "apfs", ...
	RequiresDaemon   bool
}

// CowKind is the kind of copy-on-write a provider delivers.
type CowKind uint8

// CowKind values.
const (
	CowNone    CowKind = iota // git_worktree (and container, later)
	CowBlock                  // cow_clone
	CowVirtual                // projfs (Sprint 6)
)

// String returns "none", "block" or "virtual", and "unknown(<n>)" for any
// other value.
func (k CowKind) String() string {
	switch k {
	case CowNone:
		return "none"
	case CowBlock:
		return "block"
	case CowVirtual:
		return "virtual"
	}
	return "unknown(" + strconv.Itoa(int(k)) + ")"
}

// MarshalText encodes the String form, so --json output carries "block"
// rather than a number. It never fails.
func (k CowKind) MarshalText() ([]byte, error) {
	return []byte(k.String()), nil
}

// WorkspaceSpec is everything a provider needs to create one workspace
// (SCOPE.md §6.1). The Manager fills and validates it; providers trust it.
//
//nolint:revive // exported: the name is part of the SCOPE.md §6.2 signature
type WorkspaceSpec struct {
	ID, OpID                     string // ID: prefixed ULID minted by Manager; OpID: idempotency key (§18)
	ProjectID, TaskID, SessionID string // SessionID empty until Sprint 3
	RepoDir                      string // canonical main repository dir
	Path                         string // <workspace.root>/<slug>, confined under the root (§6.1)
	Branch                       string // from domain.BranchNamer; check-ref-format'ed; never main/develop
	BaseSHA                      string // full commit id from GitOps.ResolveCommit
	Sparse                       SparseSpec
	Ports                        PortRange
}

// SparseSpec is the sparse-checkout scope of a workspace (SCOPE.md §6.6).
type SparseSpec struct {
	Enabled bool
	Paths   []string // slash-separated, repo-relative directories
}

// PortRange is the port range of a workspace. Both ends are inclusive,
// [Start, End], with End = Start + port_range_per_workspace - 1 (SCOPE.md
// §6.1, §23 decision 45).
type PortRange struct{ Start, End int }

// Workspace is a created workspace as a provider or the Manager reports it.
type Workspace struct {
	ID, ProjectID, TaskID, SessionID string
	Provider, Path, Branch, BaseSHA  string
	Caps                             Capabilities
	Sparse                           SparseSpec
	Ports                            PortRange
	State                            domain.WorkspaceState
	CreatedAt                        time.Time
}

// WorkspaceStatus is the live view of a workspace returned by Inspect. The
// filesystem, HEAD and dirty state are read at call time, never copied from
// the stored row (SCOPE.md §17, §20.13).
//
//nolint:revive // exported: the name is part of the SCOPE.md §6.2 signature
type WorkspaceStatus struct {
	Workspace
	Filesystem string  // probed now from Path, not copied from the row (§20.13)
	CowKind    CowKind // what this provider delivers
	Head       string
	Dirty      DirtyReport
	LogsDir    string   // <daemon.state_dir>/logs/<workspace_id> (§23 decision 42)
	Warnings   []string // e.g. filesystem differs from the one recorded at create
}

// DirtyReport lists the changes in a workspace that a destroy would lose
// (SCOPE.md §17, §20.12). Git-ignored files are never listed (§23 decision
// 47).
type DirtyReport struct {
	Uncommitted []string // porcelain v2 paths, minus COWFlow-owned files (.env.cowflow)
	Unpublished []string // commit SHAs not reachable from the shared ref namespace
}

// Clean reports whether nothing would be lost.
func (d DirtyReport) Clean() bool { return len(d.Uncommitted) == 0 && len(d.Unpublished) == 0 }

// DestroyOptions configures Destroy. With Force, unpreserved changes are
// rescued to refs/cowflow/rescue/<workspace_id> before removal (§23
// decision 40).
//
// Actor is who asked for the destroy; it fills the audit row. An empty Actor
// leaves the audit actor to the store's default.
type DestroyOptions struct {
	Force bool
	Actor Actor
}

// Actor identifies who performs an audited operation, e.g. kind "user" and
// the OS user name for a daemonless CLI call (SCOPE.md §17).
type Actor struct{ Kind, ID string }

// DestroyResult is what a destroy reports back. RescueRef is the ref that
// holds the rescued unpreserved changes (refs/cowflow/rescue/<workspace_id>)
// and is empty when nothing needed rescuing. It is also set when the destroy
// ends in [ErrPendingCleanup] after the rescue ran, so the caller can still
// tell the user where their work is.
type DestroyResult struct{ RescueRef string }
