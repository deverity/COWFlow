package workspacetest

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/deverity/cowflow/internal/domain"
	"github.com/deverity/cowflow/internal/workspace"
)

// FakeProvider is an in-memory workspace.WorkspaceProvider. Create records
// the workspace as ready without touching the filesystem; Inspect reports
// it with the configured capabilities and any dirty state set with SetDirty;
// Destroy refuses unpreserved changes without Force, like the real
// providers (SCOPE.md §20.12).
//
// ProviderName defaults to "fake" and Clock to the system clock. CapsErr,
// CreateErr, InspectErr and DestroyErr, when set, are returned by the
// matching method, which then changes nothing.
type FakeProvider struct {
	ProviderName string
	Caps         workspace.Capabilities
	Clock        domain.Clock

	CapsErr, CreateErr, InspectErr, DestroyErr error

	mu    sync.Mutex
	calls []string
	ws    map[string]*workspace.WorkspaceStatus
}

// Name returns ProviderName, or "fake".
func (p *FakeProvider) Name() string { return cmp.Or(p.ProviderName, "fake") }

// Calls returns the names of the methods called so far, in order (Name
// excluded).
func (p *FakeProvider) Calls() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.calls)
}

func (p *FakeProvider) enter(ctx context.Context, method string) error {
	p.calls = append(p.calls, method)
	if p.ws == nil {
		p.ws = map[string]*workspace.WorkspaceStatus{}
	}
	return ctx.Err()
}

// Capabilities returns Caps, or CapsErr.
func (p *FakeProvider) Capabilities(ctx context.Context, _ string) (workspace.Capabilities, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.enter(ctx, "Capabilities"); err != nil {
		return workspace.Capabilities{}, err
	}
	if p.CapsErr != nil {
		return workspace.Capabilities{}, p.CapsErr
	}
	return p.Caps, nil
}

// Create records a ready workspace for spec. A repeated spec.ID returns the
// recorded workspace unchanged.
func (p *FakeProvider) Create(ctx context.Context, spec workspace.WorkspaceSpec) (*workspace.Workspace, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.enter(ctx, "Create"); err != nil {
		return nil, err
	}
	if p.CreateErr != nil {
		return nil, p.CreateErr
	}
	if spec.ID == "" {
		return nil, errors.New("fake provider: create: spec.ID is required")
	}
	if st, ok := p.ws[spec.ID]; ok {
		w := cloneWorkspace(st.Workspace)
		return &w, nil
	}
	clock := p.Clock
	if clock == nil {
		clock = domain.SystemClock{}
	}
	w := workspace.Workspace{
		ID: spec.ID, ProjectID: spec.ProjectID, TaskID: spec.TaskID, SessionID: spec.SessionID,
		Provider: p.Name(), Path: spec.Path, Branch: spec.Branch, BaseSHA: spec.BaseSHA,
		Caps:      p.Caps,
		Sparse:    workspace.SparseSpec{Enabled: spec.Sparse.Enabled, Paths: slices.Clone(spec.Sparse.Paths)},
		Ports:     spec.Ports,
		State:     domain.WorkspaceReady,
		CreatedAt: clock.Now().UTC(),
	}
	p.ws[w.ID] = &workspace.WorkspaceStatus{
		Workspace:  cloneWorkspace(w),
		Filesystem: p.Caps.Filesystem,
		CowKind:    p.Caps.CopyOnWrite,
		Head:       spec.BaseSHA,
	}
	return &w, nil
}

// Inspect returns the recorded workspace, or an error wrapping
// workspace.ErrNotFound.
func (p *FakeProvider) Inspect(ctx context.Context, id string) (*workspace.WorkspaceStatus, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.enter(ctx, "Inspect"); err != nil {
		return nil, err
	}
	if p.InspectErr != nil {
		return nil, p.InspectErr
	}
	st, ok := p.ws[id]
	if !ok {
		return nil, fmt.Errorf("fake provider: inspect %s: %w", id, workspace.ErrNotFound)
	}
	out := cloneStatus(*st)
	return &out, nil
}

// Destroy marks the workspace destroyed. It returns a
// *workspace.UnpreservedError when the workspace is dirty and opts.Force is
// false, and nil for a workspace already destroyed.
func (p *FakeProvider) Destroy(ctx context.Context, id string, opts workspace.DestroyOptions) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.enter(ctx, "Destroy"); err != nil {
		return err
	}
	if p.DestroyErr != nil {
		return p.DestroyErr
	}
	st, ok := p.ws[id]
	switch {
	case !ok:
		return fmt.Errorf("fake provider: destroy %s: %w", id, workspace.ErrNotFound)
	case st.State == domain.WorkspaceDestroyed:
		return nil
	case !st.Dirty.Clean() && !opts.Force:
		return &workspace.UnpreservedError{Report: cloneDirty(st.Dirty)}
	}
	st.State = domain.WorkspaceDestroyed
	return nil
}

// SetDirty sets the dirty state Inspect reports and Destroy checks.
func (p *FakeProvider) SetDirty(id string, d workspace.DirtyReport) error {
	return p.update(id, func(st *workspace.WorkspaceStatus) { st.Dirty = cloneDirty(d) })
}

// SetState overrides the recorded state, e.g. to pending_cleanup.
func (p *FakeProvider) SetState(id string, s domain.WorkspaceState) error {
	return p.update(id, func(st *workspace.WorkspaceStatus) { st.State = s })
}

// SetWarnings sets the warnings Inspect reports.
func (p *FakeProvider) SetWarnings(id string, warnings ...string) error {
	return p.update(id, func(st *workspace.WorkspaceStatus) { st.Warnings = slices.Clone(warnings) })
}

// addSparse appends paths to the recorded sparse scope.
func (p *FakeProvider) addSparse(id string, add []string) error {
	return p.update(id, func(st *workspace.WorkspaceStatus) {
		st.Sparse.Enabled = true
		for _, a := range add {
			if !slices.Contains(st.Sparse.Paths, a) {
				st.Sparse.Paths = append(st.Sparse.Paths, a)
			}
		}
	})
}

func (p *FakeProvider) update(id string, fn func(st *workspace.WorkspaceStatus)) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	st, ok := p.ws[id]
	if !ok {
		return fmt.Errorf("fake provider: %s: %w", id, workspace.ErrNotFound)
	}
	fn(st)
	return nil
}

func cloneWorkspace(w workspace.Workspace) workspace.Workspace {
	w.Sparse.Paths = slices.Clone(w.Sparse.Paths)
	return w
}

func cloneDirty(d workspace.DirtyReport) workspace.DirtyReport {
	return workspace.DirtyReport{
		Uncommitted: slices.Clone(d.Uncommitted),
		Unpublished: slices.Clone(d.Unpublished),
	}
}

func cloneStatus(st workspace.WorkspaceStatus) workspace.WorkspaceStatus {
	st.Workspace = cloneWorkspace(st.Workspace)
	st.Dirty = cloneDirty(st.Dirty)
	st.Warnings = slices.Clone(st.Warnings)
	return st
}
