package workspacetest

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/deverity/cowflow/internal/domain"
	"github.com/deverity/cowflow/internal/workspace"
)

// FakeManager has the method set of the workspace Manager (sprint-1.md
// §6.1) and keeps its workspaces in a FakeProvider, so the CLI can be
// developed and tested before the real Manager lands (S1-T03).
//
// Select("auto") returns Provider with Warning (set Warning to simulate the
// no-COW fallback); "cow_clone" and "git_worktree" return Provider without a
// warning; "projfs" and "container" fail with
// workspace.ErrUnsupportedProvider. Create is idempotent by OpID and
// allocates port ranges PortStart + n*PortsPerWorkspace (defaults 40000 and
// 10), inclusive at both ends. SelectErr, CreateErr and ExpandErr, when set,
// are returned by the matching method.
type FakeManager struct {
	Provider          *FakeProvider // created on first use when nil
	Warning           string
	IDs               domain.IDGen
	PortStart         int
	PortsPerWorkspace int

	SelectErr, CreateErr, ExpandErr error

	mu       sync.Mutex
	requests []workspace.CreateRequest
	byOp     map[string]string // op_id -> workspace ID
	order    []string          // workspace IDs, oldest first
}

func (m *FakeManager) provider() *FakeProvider {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Provider == nil {
		m.Provider = &FakeProvider{}
	}
	return m.Provider
}

// Requests returns a copy of the requests passed to Create, in order.
func (m *FakeManager) Requests() []workspace.CreateRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.requests)
}

// Select resolves the requested provider name.
func (m *FakeManager) Select(ctx context.Context, requested string) (workspace.Selection, error) {
	if err := ctx.Err(); err != nil {
		return workspace.Selection{}, err
	}
	if m.SelectErr != nil {
		return workspace.Selection{}, m.SelectErr
	}
	p := m.provider()
	switch requested {
	case "auto", "cow_clone", "git_worktree":
	case "projfs", "container":
		return workspace.Selection{}, fmt.Errorf("provider %q not available in this build: %w", requested, workspace.ErrUnsupportedProvider)
	default:
		return workspace.Selection{}, fmt.Errorf("fake manager: unknown provider %q", requested)
	}
	caps, err := p.Capabilities(ctx, "")
	if err != nil {
		return workspace.Selection{}, err
	}
	sel := workspace.Selection{Provider: p, Caps: caps}
	if requested == "auto" {
		sel.Warning = m.Warning
	}
	return sel, nil
}

// Create makes a ready workspace for req.Task through Provider. A repeated
// OpID returns the workspace created first.
func (m *FakeManager) Create(ctx context.Context, req workspace.CreateRequest) (*workspace.Workspace, workspace.Selection, error) {
	m.mu.Lock()
	m.requests = append(m.requests, req)
	m.mu.Unlock()
	if m.CreateErr != nil {
		return nil, workspace.Selection{}, m.CreateErr
	}
	sel, err := m.Select(ctx, req.Provider)
	if err != nil {
		return nil, workspace.Selection{}, err
	}
	p := m.provider()

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.byOp == nil {
		m.byOp = map[string]string{}
	}
	id, seen := m.byOp[req.OpID]
	if !seen {
		if m.IDs == nil {
			m.IDs = domain.NewULIDGen(domain.SystemClock{})
		}
		if id, err = m.IDs.New(domain.KindWorkspace); err != nil {
			return nil, workspace.Selection{}, fmt.Errorf("fake manager: new workspace id: %w", err)
		}
	}
	per := cmp.Or(m.PortsPerWorkspace, 10)
	start := cmp.Or(m.PortStart, 40000) + len(m.order)*per
	spec := workspace.WorkspaceSpec{
		ID: id, OpID: req.OpID,
		ProjectID: req.ProjectID, TaskID: req.Task.ID, SessionID: req.SessionID,
		RepoDir: req.RepoDir,
		Path:    req.Task.Slug, // the real Manager uses <workspace.root>/<slug>
		Branch:  req.Task.Branch, BaseSHA: req.Task.BaseSHA,
		Sparse: workspace.SparseSpec{
			Enabled: req.Task.Sparse && len(req.Task.Paths) > 0,
			Paths:   slices.Clone(req.Task.Paths),
		},
		Ports: workspace.PortRange{Start: start, End: start + per - 1},
	}
	w, err := p.Create(ctx, spec)
	if err != nil {
		return nil, workspace.Selection{}, err
	}
	if !seen {
		m.byOp[req.OpID] = id
		m.order = append(m.order, id)
	}
	return w, sel, nil
}

// Inspect returns Provider's view of the workspace.
func (m *FakeManager) Inspect(ctx context.Context, id string) (*workspace.WorkspaceStatus, error) {
	return m.provider().Inspect(ctx, id)
}

// Destroy destroys the workspace through Provider.
func (m *FakeManager) Destroy(ctx context.Context, id string, opts workspace.DestroyOptions) error {
	return m.provider().Destroy(ctx, id, opts)
}

// List returns the workspaces matching f, oldest first.
func (m *FakeManager) List(ctx context.Context, f workspace.ListFilter) ([]workspace.Workspace, error) {
	p := m.provider()
	m.mu.Lock()
	ids := slices.Clone(m.order)
	m.mu.Unlock()
	var out []workspace.Workspace
	for _, id := range ids {
		st, err := p.Inspect(ctx, id)
		if err != nil {
			return nil, err
		}
		w := st.Workspace
		if (f.ProjectID == "" || w.ProjectID == f.ProjectID) &&
			(f.TaskID == "" || w.TaskID == f.TaskID) &&
			(len(f.States) == 0 || slices.Contains(f.States, w.State)) {
			out = append(out, w)
		}
	}
	return out, nil
}

// ExpandScope adds directories to the workspace's sparse scope.
func (m *FakeManager) ExpandScope(ctx context.Context, id string, add []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.ExpandErr != nil {
		return m.ExpandErr
	}
	return m.provider().addSparse(id, add)
}
