package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ResolveCommit resolves rev to a full commit SHA with
// `git -C <repo> rev-parse --verify --end-of-options <rev>^{commit}`.
// --end-of-options keeps a rev such as "--all" from being read as an option.
func (c *Client) ResolveCommit(ctx context.Context, repo, rev string) (string, error) {
	if err := checkAbs("repo", repo); err != nil {
		return "", err
	}
	if rev == "" || strings.ContainsRune(rev, 0) {
		return "", fmt.Errorf("%w: revision %q", ErrInvalidArgument, truncate(rev, 80))
	}
	out, err := c.run(ctx, repo, "rev-parse", "--verify", "--end-of-options", rev+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", truncate(rev, 80), err)
	}
	return singleSHA("rev-parse", out)
}

// BranchExists reports whether refs/heads/<name> exists, with
// `git -C <repo> show-ref --verify --quiet refs/heads/<name>`. Exit code 1
// means it does not; every other failure is an error.
func (c *Client) BranchExists(ctx context.Context, repo, name string) (bool, error) {
	if err := checkAbs("repo", repo); err != nil {
		return false, err
	}
	if err := checkBranchShape(name); err != nil {
		return false, err
	}
	_, err := c.run(ctx, repo, "show-ref", "--verify", "--quiet", "refs/heads/"+name)
	if err == nil {
		return true, nil
	}
	var ge *Error
	if errors.As(err, &ge) && ge.ExitCode == 1 {
		return false, nil
	}
	return false, fmt.Errorf("branch exists %q: %w", name, err)
}

// DeleteBranchIfAt deletes refs/heads/<name> only while it still points at
// sha, with `git -C <repo> update-ref -d refs/heads/<name> <sha>`; git does
// the compare and the delete atomically. A branch that moved or does not
// exist is left alone and the call fails. It is meant for compensation after
// a failed create, never for deleting a task's work.
func (c *Client) DeleteBranchIfAt(ctx context.Context, repo, name, sha string) error {
	if err := checkAbs("repo", repo); err != nil {
		return err
	}
	if err := checkBranchShape(name); err != nil {
		return err
	}
	if err := checkSHA("sha", sha); err != nil {
		return err
	}
	if _, err := c.run(ctx, repo, "update-ref", "-d", "refs/heads/"+name, sha); err != nil {
		return fmt.Errorf("delete branch %q at %s: %w", name, sha, err)
	}
	return nil
}

// Head returns the commit SHA HEAD points at in worktree, with
// `git -C <wt> rev-parse --verify HEAD`.
func (c *Client) Head(ctx context.Context, worktree string) (string, error) {
	if err := checkAbs("worktree", worktree); err != nil {
		return "", err
	}
	out, err := c.run(ctx, worktree, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", fmt.Errorf("head: %w", err)
	}
	return singleSHA("rev-parse HEAD", out)
}
