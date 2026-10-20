package workspace

import (
	"context"
	"fmt"
)

// cowflowOwned reports whether a repo-relative, slash-separated path from
// `git status` is a file COWFlow itself writes into a workspace. Such files
// are never unpreserved work (SCOPE.md §23 decision 47): today only the
// root-level [EnvFileName]. Git-ignored files never reach this check, as
// Status does not list them.
func cowflowOwned(path string) bool { return path == EnvFileName }

// Dirty reads Git now and reports what destroying ws would lose: uncommitted
// paths (COWFlow-owned files excluded) and commits not reachable from the
// shared ref namespace (SCOPE.md §17, §20.12). It never caches, because the
// agent may be writing while it runs, and it fails closed: any Git error is
// returned and never read as clean.
//
// The namespace comes only from [git].shared_ref_namespace: a typo would
// mark every commit unpublished.
func (in *Inspector) Dirty(ctx context.Context, ws Workspace) (DirtyReport, error) {
	if in == nil || in.git == nil {
		return DirtyReport{}, fmt.Errorf("dirty check %s: %w: no git", ws.ID, errNotConfigured)
	}
	entries, err := in.git.Status(ctx, ws.Path)
	if err != nil {
		return DirtyReport{}, fmt.Errorf("dirty check %s: %w", ws.ID, err)
	}
	var r DirtyReport
	for _, e := range entries {
		if cowflowOwned(e.Path) {
			continue
		}
		r.Uncommitted = append(r.Uncommitted, e.Path)
	}
	unpub, err := in.git.Unpublished(ctx, ws.Path, ws.BaseSHA, in.namespace)
	if err != nil {
		return DirtyReport{}, fmt.Errorf("dirty check %s: %w", ws.ID, err)
	}
	r.Unpublished = unpub
	return r, nil
}
