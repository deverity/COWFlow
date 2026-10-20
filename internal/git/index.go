package git

import (
	"context"
	"fmt"
)

// RefreshIndex refreshes the stat data in an index against workTree, with
// `[GIT_INDEX_FILE=<idx>] git -C <repo> --work-tree=<wt> update-index -q --refresh`.
// An empty indexFile uses the repository's own index; a non-empty one needs
// an OptsRunner.
func (c *Client) RefreshIndex(ctx context.Context, repo, workTree, indexFile string) error {
	if err := checkAbs("repo", repo); err != nil {
		return err
	}
	if err := checkAbs("work tree", workTree); err != nil {
		return err
	}
	if indexFile != "" {
		if err := checkAbs("index file", indexFile); err != nil {
			return err
		}
	}
	if _, err := c.runWith(ctx, repo, RunOpts{Env: indexEnv(indexFile)},
		"--work-tree="+workTree, "update-index", "-q", "--refresh"); err != nil {
		return fmt.Errorf("refresh index for %s: %w", workTree, err)
	}
	return nil
}

// readTreeAllowed is the fixed allow-list of ReadTree arguments besides a
// hex SHA.
var readTreeAllowed = map[string]bool{"-mu": true, "HEAD": true}

// ReadTree runs `git -C <wt> read-tree <args...>`. Each argument must be
// "-mu", "HEAD" or a full hex SHA, and at least one tree must be named.
func (c *Client) ReadTree(ctx context.Context, worktree string, args ...string) error {
	if err := checkAbs("worktree", worktree); err != nil {
		return err
	}
	trees := 0
	for _, a := range args {
		switch {
		case a == "HEAD" || IsSHA(a):
			trees++
		case readTreeAllowed[a]:
		default:
			return fmt.Errorf("%w: read-tree argument %q is not allowed", ErrInvalidArgument, truncate(a, 80))
		}
	}
	if trees == 0 {
		return fmt.Errorf("%w: read-tree needs a tree", ErrInvalidArgument)
	}
	if _, err := c.run(ctx, worktree, append([]string{"read-tree"}, args...)...); err != nil {
		return fmt.Errorf("read-tree: %w", err)
	}
	return nil
}
