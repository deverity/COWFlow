package workspace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// RescueRefPrefix is the namespace of rescue snapshots (SCOPE.md §17, §23
// decision 40). One ref per workspace; a later rescue of the same workspace
// overwrites it. There is no retention in v1, and the Sprint 2 retention GC
// (SPEC-GAPS U-GIT-4) must leave this namespace alone.
const RescueRefPrefix = "refs/cowflow/rescue/"

// RescueRef is the ref a rescue of workspace id is stored at.
func RescueRef(workspaceID string) string { return RescueRefPrefix + workspaceID }

// ErrRescueFailed is wrapped by every rescue failure. The destroy stops
// before anything is deleted (SCOPE.md §17; exit 2, §14).
var ErrRescueFailed = errors.New("workspace: rescue failed")

// Rescuer saves the unpreserved changes of a workspace outside it before a
// destroy --force removes it, and returns where they went (a ref). It must
// not modify the worktree, its index or the task branch.
type Rescuer interface {
	Rescue(ctx context.Context, ws Workspace, r DirtyReport) (ref string, err error)
}

// Rescue identity and message (SCOPE.md §23 decision 40).
const (
	rescueUserName  = "COWFlow"
	rescueUserEmail = "cowflow@localhost"
)

// SnapshotGit is the git plumbing a snapshot rescue needs. Every call is one
// argv invocation in the worktree; indexFile, when non-empty, is passed as
// GIT_INDEX_FILE and never touches the worktree's own index.
//
// It is declared apart from [GitOps] so that *git.Client keeps satisfying
// GitOps until AG-GIT adds these methods; until then the default rescuer
// fails every rescue and destroy --force on a dirty workspace deletes
// nothing.
type SnapshotGit interface {
	// GitPath is GitOps.GitPath; the rescuer reads the index location.
	GitPath(ctx context.Context, worktree, name string) (string, error)
	// AddAll stages tracked, staged and untracked files into indexFile, with
	// `GIT_INDEX_FILE=<indexFile> git -C <wt> add -A -- ':(exclude,top,literal)<p>'...`
	// for each repo-relative, slash-separated path in exclude. Ignored files
	// are never added (SCOPE.md §23 decision 47).
	AddAll(ctx context.Context, worktree, indexFile string, exclude []string) error
	// WriteTree writes indexFile as a tree, with
	// `GIT_INDEX_FILE=<indexFile> git -C <wt> write-tree`, and returns its id.
	WriteTree(ctx context.Context, worktree, indexFile string) (string, error)
	// CommitTree creates a commit of tree with parent and message, with
	// `git -C <wt> -c user.name=<name> -c user.email=<email> commit-tree <tree> -p <parent> -F -`,
	// the message on stdin, and returns its id.
	CommitTree(ctx context.Context, worktree, tree, parent, name, email, message string) (string, error)
	// UpdateRef points ref at sha, overwriting it, with
	// `git -C <wt> update-ref <ref> <sha>`. ref passes check-ref-format.
	UpdateRef(ctx context.Context, worktree, ref, sha string) error
}

// SnapshotRescuer is the production [Rescuer] (SCOPE.md §23 decision 40): a
// snapshot commit of tracked, staged and untracked files (ignored files and
// .env.cowflow excluded) whose parent is HEAD, stored at
// refs/cowflow/rescue/<workspace_id>. The snapshot is staged into a
// temporary copy of the worktree's index, next to it in the worktree's git
// directory, so the worktree, its index and the task branch are untouched,
// and paths outside a sparse checkout keep their HEAD content.
type SnapshotRescuer struct {
	git  SnapshotGit
	head func(ctx context.Context, worktree string) (string, error)
}

var _ Rescuer = (*SnapshotRescuer)(nil)

// NewSnapshotRescuer returns a SnapshotRescuer over g, reading HEAD with
// head (GitOps.Head).
func NewSnapshotRescuer(g SnapshotGit, head func(ctx context.Context, worktree string) (string, error)) *SnapshotRescuer {
	return &SnapshotRescuer{git: g, head: head}
}

// Rescue writes the snapshot and returns its ref. Any failure wraps
// [ErrRescueFailed]; the temporary index is always removed.
func (s *SnapshotRescuer) Rescue(ctx context.Context, ws Workspace, _ DirtyReport) (_ string, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("%w: workspace %s: %w", ErrRescueFailed, ws.ID, err)
		}
	}()
	if s == nil || s.git == nil || s.head == nil {
		return "", fmt.Errorf("%w: no git", errNotConfigured)
	}
	if !validRescueID(ws.ID) {
		return "", fmt.Errorf("workspace id %q cannot name a rescue ref", ws.ID)
	}
	head, err := s.head(ctx, ws.Path)
	if err != nil {
		return "", fmt.Errorf("head: %w", err)
	}
	idx, err := s.git.GitPath(ctx, ws.Path, "index")
	if err != nil {
		return "", fmt.Errorf("index path: %w", err)
	}
	tmp, err := copyIndex(idx)
	if err != nil {
		return "", err
	}
	defer func() {
		if rerr := os.Remove(tmp); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("remove temporary index: %w", rerr))
		}
	}()
	if err := s.git.AddAll(ctx, ws.Path, tmp, []string{EnvFileName}); err != nil {
		return "", fmt.Errorf("stage snapshot: %w", err)
	}
	tree, err := s.git.WriteTree(ctx, ws.Path, tmp)
	if err != nil {
		return "", fmt.Errorf("write tree: %w", err)
	}
	sha, err := s.git.CommitTree(ctx, ws.Path, tree, head, rescueUserName, rescueUserEmail, rescueMessage(ws))
	if err != nil {
		return "", fmt.Errorf("commit snapshot: %w", err)
	}
	ref := RescueRef(ws.ID)
	if err := s.git.UpdateRef(ctx, ws.Path, ref, sha); err != nil {
		return "", fmt.Errorf("store %s: %w", ref, err)
	}
	return ref, nil
}

// rescueMessage is the snapshot commit message with its trailers (SCOPE.md
// §23 decision 40). The task slug is the last element of the workspace path
// (<workspace.root>/<slug>, §23 decision 42).
func rescueMessage(ws Workspace) string {
	slug := filepath.Base(ws.Path)
	return fmt.Sprintf("cowflow rescue: workspace %s (task %s) before destroy --force\n\n"+
		"Cowflow-Workspace: %s\nCowflow-Task: %s\n", ws.ID, slug, ws.ID, ws.TaskID)
}

// validRescueID accepts IDs that are one plain ref component; a store-minted
// wks_ ULID always is. check-ref-format still runs in UpdateRef.
func validRescueID(id string) bool {
	return id != "" && id != "." && id != ".." && !strings.ContainsAny(id, "/\\ ~^:?*[\x00\x7f") &&
		!strings.HasPrefix(id, ".") && !strings.HasSuffix(id, ".lock") && !strings.Contains(id, "..")
}

// copyIndex copies the index at idx to a new temporary file in the same
// directory and returns its path. A missing index yields the path of a file
// that does not exist yet (git creates an index there), because git rejects
// an empty one.
func copyIndex(idx string) (string, error) {
	dir := filepath.Dir(idx)
	f, err := os.CreateTemp(dir, "cowflow-rescue-index-*")
	if err != nil {
		return "", fmt.Errorf("temporary index: %w", err)
	}
	tmp := f.Name()
	src, err := os.Open(idx) //nolint:gosec // idx comes from git rev-parse --git-path
	if errors.Is(err, fs.ErrNotExist) {
		return tmp, errors.Join(f.Close(), os.Remove(tmp))
	}
	if err == nil {
		_, err = io.Copy(f, src)
		err = errors.Join(err, src.Close())
	}
	if err = errors.Join(err, f.Close()); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("copy index: %w", err)
	}
	return tmp, nil
}
