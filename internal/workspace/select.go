package workspace

import (
	"cmp"
	"context"
	"errors"
	"fmt"
)

// Provider names as [workspace].provider and --provider spell them (SCOPE.md
// §6.3, §22).
const (
	ProviderAuto        = "auto"
	ProviderCowClone    = "cow_clone"
	ProviderGitWorktree = "git_worktree"
	ProviderProjFS      = "projfs"
	ProviderContainer   = "container"
)

// Select resolves a requested provider name for [workspace].root (SCOPE.md
// §6.3, §23 decisions 10, 31, 46). An empty name means [workspace].provider.
//
//   - "auto" takes cow_clone when its capabilities report block clone and
//     otherwise git_worktree, with a warning that is both logged and returned
//     so the CLI prints it (§1.2 "explicit warning"). projfs is never chosen.
//   - "cow_clone" is honored or fails with an error wrapping
//     ErrCloneUnsupported that names auto and git_worktree; it never falls
//     back.
//   - "git_worktree" is always honored.
//   - "projfs" and "container" fail with ErrUnsupportedProvider in this build.
//
// Select only probes; the Dev Drive mount gate is Create's, which runs it
// before selecting.
func (m *Manager) Select(ctx context.Context, requested string) (Selection, error) {
	root := m.cfg.Workspace.Root
	if root == "" {
		return Selection{}, ErrRootNotSet
	}
	switch name := cmp.Or(requested, m.cfg.Workspace.Provider); name {
	case ProviderAuto:
		return m.selectAuto(ctx, root)
	case ProviderCowClone:
		return m.selectCow(ctx, root)
	case ProviderGitWorktree:
		wt, err := m.worktree()
		if err != nil {
			return Selection{}, err
		}
		caps, err := wt.Capabilities(ctx, root)
		if err != nil {
			return Selection{}, fmt.Errorf("provider %s: capabilities of %q: %w", ProviderGitWorktree, root, err)
		}
		return Selection{Provider: wt, Caps: caps}, nil
	case ProviderProjFS, ProviderContainer:
		return Selection{}, fmt.Errorf("provider %q not available in this build: %w", name, ErrUnsupportedProvider)
	default:
		return Selection{}, fmt.Errorf("workspace: unknown provider %q (want auto, cow_clone or git_worktree)", name)
	}
}

func (m *Manager) selectAuto(ctx context.Context, root string) (Selection, error) {
	reason := "cow_clone is not available in this build"
	if m.cow != nil {
		caps, err := m.cow.Capabilities(ctx, root)
		switch {
		case err == nil && caps.CopyOnWrite == CowBlock:
			return Selection{Provider: m.cow, Caps: caps}, nil
		case err != nil:
			reason = err.Error()
		default:
			reason = fmt.Sprintf("cow_clone reports copy-on-write %s", caps.CopyOnWrite)
		}
	}
	if err := ctx.Err(); err != nil {
		return Selection{}, err
	}
	wt, err := m.worktree()
	if err != nil {
		return Selection{}, err
	}
	caps, err := wt.Capabilities(ctx, root)
	if err != nil {
		return Selection{}, fmt.Errorf("provider %s: capabilities of %q: %w", ProviderGitWorktree, root, err)
	}
	warning := fmt.Sprintf("no copy-on-write on %q (%s): using git_worktree", root, caps.Filesystem)
	m.log.Warn("workspace provider fallback", "requested", ProviderAuto, "provider", wt.Name(),
		"root", root, "filesystem", caps.Filesystem, "reason", reason)
	return Selection{Provider: wt, Caps: caps, Warning: warning}, nil
}

func (m *Manager) selectCow(ctx context.Context, root string) (Selection, error) {
	const hint = "use provider auto or git_worktree"
	if m.cow == nil {
		return Selection{}, fmt.Errorf("provider %s is not available in this build: %w; %s",
			ProviderCowClone, ErrCloneUnsupported, hint)
	}
	caps, err := m.cow.Capabilities(ctx, root)
	switch {
	case errors.Is(err, ErrCloneUnsupported):
		return Selection{}, fmt.Errorf("provider %s on %q: %w; %s", ProviderCowClone, root, err, hint)
	case err != nil:
		return Selection{}, fmt.Errorf("provider %s: capabilities of %q: %w", ProviderCowClone, root, err)
	case caps.CopyOnWrite != CowBlock:
		return Selection{}, fmt.Errorf("provider %s: no block clone on %q (%s): %w; %s",
			ProviderCowClone, root, caps.Filesystem, ErrCloneUnsupported, hint)
	}
	return Selection{Provider: m.cow, Caps: caps}, nil
}

func (m *Manager) worktree() (WorkspaceProvider, error) {
	if m.wt == nil {
		return nil, fmt.Errorf("provider %s: %w", ProviderGitWorktree, ErrUnsupportedProvider)
	}
	return m.wt, nil
}
