package git

import (
	"context"
	"fmt"
)

// ExportTree materializes commit sha into dstDir and writes a matching index
// to indexFile, without touching the repository's own worktree or index. It
// is how a golden tree is built:
//
//	git -C <repo> read-tree --index-output=<indexFile> <sha>
//	GIT_INDEX_FILE=<indexFile> git -C <repo> -c core.autocrlf=false \
//	    -c core.longpaths=true --work-tree=<dst> checkout-index --all --force -u
//
// -u records the stat data of the written files, so the index is clean
// against dstDir. checkout-index runs smudge filters: a Git LFS repository
// without git-lfs fails here, and the error is returned as is. The Runner
// must implement OptsRunner.
//
// dstDir must already exist; ExportTree creates no directory of its own.
// read-tree --index-output locks the repository's own index while it runs
// and renames its lock file onto indexFile, so indexFile must be on the same
// filesystem as the repository's git directory (git-read-tree(1)); otherwise
// git fails with "unable to write new index file".
func (c *Client) ExportTree(ctx context.Context, repo, sha, dstDir, indexFile string) error {
	if err := checkAbs("repo", repo); err != nil {
		return err
	}
	if err := checkSHA("sha", sha); err != nil {
		return err
	}
	if err := checkAbs("destination", dstDir); err != nil {
		return err
	}
	if err := checkAbs("index file", indexFile); err != nil {
		return err
	}
	if _, err := c.run(ctx, repo, "read-tree", "--index-output="+indexFile, sha); err != nil {
		return fmt.Errorf("export %s: %w", sha, err)
	}
	// -c and --work-tree are global options: they must precede the
	// subcommand.
	if _, err := c.runWith(ctx, repo, RunOpts{Env: indexEnv(indexFile)},
		"-c", "core.autocrlf=false", "-c", "core.longpaths=true",
		"--work-tree="+dstDir, "checkout-index", "--all", "--force", "-u"); err != nil {
		return fmt.Errorf("export %s to %s: %w", sha, dstDir, err)
	}
	return nil
}
