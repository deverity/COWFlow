package workspace

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/deverity/cowflow/internal/git"
)

// alwaysSet is the sparse set every sparse workspace carries besides the task
// paths (SCOPE.md §23 decision 41, U-PLAT-3). Root files, go.mod included,
// are implicit in cone mode.
var alwaysSet = []string{"contracts"}

// AlwaysSet returns the directories every sparse workspace materializes
// whatever the task paths: exactly "contracts". The slice is a copy.
func AlwaysSet() []string { return slices.Clone(alwaysSet) }

// SparseSet returns the cone-mode directories of a sparse workspace: the
// task paths, normalized as `git sparse-checkout` takes them, united with
// [AlwaysSet], sorted and without duplicates. A path git would refuse (see
// git.ValidateSparseDir) is an error and nothing is returned.
func SparseSet(paths []string) ([]string, error) {
	set := make([]string, 0, len(paths)+len(alwaysSet))
	for _, p := range paths {
		n, err := git.ValidateSparseDir(p)
		if err != nil {
			return nil, fmt.Errorf("sparse set: %w", err)
		}
		set = append(set, n)
	}
	set = append(set, alwaysSet...)
	slices.Sort(set)
	return slices.Compact(set), nil
}

// coneMatcher decides which repository paths git's cone mode materializes
// for a set of directories (SCOPE.md §6.6). It mirrors git exactly, because
// cow_clone clones only matching files before `sparse-checkout set` runs: a
// file it includes that git excludes is deleted again, a file it misses that
// git includes is written as a full copy. TestConeMatcherAgreesWithGit is
// the guard.
//
// Paths are slash-separated and relative to the repository root.
type coneMatcher struct {
	cone      map[string]struct{} // listed directories: everything below is included
	ancestors map[string]struct{} // proper ancestors of a listed directory: files directly inside are included
}

// newConeMatcher returns the matcher of dirs, which must already be
// normalized (see [SparseSet]).
func newConeMatcher(dirs []string) *coneMatcher {
	m := &coneMatcher{cone: map[string]struct{}{}, ancestors: map[string]struct{}{}}
	for _, d := range dirs {
		m.cone[d] = struct{}{}
		for p := path.Dir(d); p != "."; p = path.Dir(p) {
			m.ancestors[p] = struct{}{}
		}
	}
	return m
}

// Match reports whether the file (isDir false) or directory rel is
// materialized:
//   - the root, and files directly in it, always;
//   - a directory that is a cone directory, below one, or above one;
//   - a file whose parent is a cone directory, below one, or above one.
//
// Everything else is not. A false for a directory means its whole subtree
// can be skipped.
func (m *coneMatcher) Match(rel string, isDir bool) bool {
	rel = strings.Trim(rel, "/")
	if rel == "" || rel == "." {
		return true
	}
	dir := rel
	if !isDir {
		dir = path.Dir(rel)
		if dir == "." {
			return true
		}
	}
	if _, ok := m.ancestors[dir]; ok {
		return true
	}
	for p := dir; p != "."; p = path.Dir(p) {
		if _, ok := m.cone[p]; ok {
			return true
		}
	}
	return false
}

// applySparse restricts the worktree wt to the cone set of paths: git
// `sparse-checkout set`, then (populate) `read-tree -mu HEAD` to bring a
// --no-checkout worktree's index and files to HEAD under the new patterns.
func applySparse(ctx context.Context, g GitOps, wt string, paths []string, populate bool) error {
	set, err := SparseSet(paths)
	if err != nil {
		return err
	}
	if err := g.SparseSet(ctx, wt, set); err != nil {
		return err
	}
	if populate {
		if err := g.ReadTree(ctx, wt, "-mu", "HEAD"); err != nil {
			return fmt.Errorf("populate sparse worktree: %w", err)
		}
	}
	return nil
}

// scopeExpander is implemented by providers that can widen a workspace's
// sparse scope (Manager.ExpandScope).
type scopeExpander interface {
	expandScope(ctx context.Context, w Workspace, add []string) error
}
