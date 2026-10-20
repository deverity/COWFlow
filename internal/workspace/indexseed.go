package workspace

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// IndexSeeder gives a freshly cloned cow_clone worktree its own index, so
// that git status is clean without hashing every file (sprint-1.md §4.7).
// wt is the worktree, g the golden tree its files were cloned from and
// baseSHA the commit both hold.
type IndexSeeder interface {
	Seed(ctx context.Context, wt string, g Golden, baseSHA string) error
}

// goldenIndexSeeder is the production seeder (SCOPE.md §23 decision 39):
// the golden index is block-cloned to the worktree's index path, then
// core.checkStat=minimal and core.trustCtime=false are set per worktree.
// Cloned files keep the golden's size and mtime but get their own inode
// and ctime; with those two settings Git's stat check compares only size
// and whole-second mtime, so the index is clean without rehashing. The
// index is cloned, never copied, so it adds no incremental disk.
type goldenIndexSeeder struct {
	git    GitOps
	cloner Cloner
}

var _ IndexSeeder = goldenIndexSeeder{}

// Seed clones g.IndexFile to the worktree's index and sets the stat
// settings. An index already at the path, which `worktree add --no-checkout`
// does not write, belongs to the worktree this create just added and is
// replaced.
func (s goldenIndexSeeder) Seed(ctx context.Context, wt string, g Golden, _ string) error {
	idx, err := s.git.GitPath(ctx, wt, "index")
	if err != nil {
		return err
	}
	if err := os.Remove(idx); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("replace worktree index: %w", err)
	}
	if err := s.cloner.CloneFile(ctx, g.IndexFile, idx); err != nil {
		return fmt.Errorf("clone golden index: %w", err)
	}
	for _, kv := range [][2]string{{"core.checkStat", "minimal"}, {"core.trustCtime", "false"}} {
		if err := s.git.ConfigSetWorktree(ctx, wt, kv[0], kv[1]); err != nil {
			return fmt.Errorf("set %s: %w", kv[0], err)
		}
	}
	return nil
}
