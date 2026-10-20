package git

import (
	"context"
	"fmt"
	"strings"
)

// excludePathspec returns the pathspec that excludes one repo-relative,
// slash-separated path literally (no glob, no attribute magic), from the
// repository root whatever the working directory.
func excludePathspec(p string) (string, error) {
	if p == "" || strings.ContainsRune(p, 0) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, ":") {
		return "", fmt.Errorf("%w: exclude path %q must be repo-relative", ErrInvalidArgument, truncate(p, 80))
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", fmt.Errorf("%w: exclude path %q leaves the repository", ErrInvalidArgument, truncate(p, 80))
		}
	}
	return ":(exclude,top,literal)" + p, nil
}

// AddAll stages tracked, staged and untracked files of worktree into
// indexFile, with
// `GIT_INDEX_FILE=<indexFile> git -C <wt> add -A -- ':(exclude,top,literal)<p>'...`
// for each path in exclude. Ignored files are never added. An empty
// indexFile uses the worktree's own index; a non-empty one must be absolute
// and needs an OptsRunner.
func (c *Client) AddAll(ctx context.Context, worktree, indexFile string, exclude []string) error {
	if err := checkAbs("worktree", worktree); err != nil {
		return err
	}
	if indexFile != "" {
		if err := checkAbs("index file", indexFile); err != nil {
			return err
		}
	}
	args := []string{"add", "-A", "--"}
	for _, p := range exclude {
		spec, err := excludePathspec(p)
		if err != nil {
			return err
		}
		args = append(args, spec)
	}
	if _, err := c.runWith(ctx, worktree, RunOpts{Env: indexEnv(indexFile)}, args...); err != nil {
		return fmt.Errorf("add -A: %w", err)
	}
	return nil
}

// WriteTree writes indexFile as a tree object, with
// `GIT_INDEX_FILE=<indexFile> git -C <wt> write-tree`, and returns its id.
func (c *Client) WriteTree(ctx context.Context, worktree, indexFile string) (string, error) {
	if err := checkAbs("worktree", worktree); err != nil {
		return "", err
	}
	if indexFile != "" {
		if err := checkAbs("index file", indexFile); err != nil {
			return "", err
		}
	}
	out, err := c.runWith(ctx, worktree, RunOpts{Env: indexEnv(indexFile)}, "write-tree")
	if err != nil {
		return "", fmt.Errorf("write-tree: %w", err)
	}
	return singleSHA("write-tree", out)
}

// CommitTree creates a commit of tree with one parent and message, with
// `git -C <wt> -c user.name=<name> -c user.email=<email> commit-tree <tree> -p <parent> -F -`
// and the message on stdin, and returns its id. The name and email are also
// set as the author and committer variables for the call, because inherited
// GIT_AUTHOR_* / GIT_COMMITTER_* would override -c user.*. The Runner must
// implement OptsRunner.
func (c *Client) CommitTree(ctx context.Context, worktree, tree, parent, name, email, message string) (string, error) {
	if err := checkAbs("worktree", worktree); err != nil {
		return "", err
	}
	if err := checkSHA("tree", tree); err != nil {
		return "", err
	}
	if err := checkSHA("parent", parent); err != nil {
		return "", err
	}
	for what, v := range map[string]string{"name": name, "email": email} {
		if err := checkEnvIdentity(v); err != nil {
			return "", fmt.Errorf("%w: identity %s %w", ErrInvalidArgument, what, err)
		}
	}
	if strings.ContainsRune(message, 0) {
		return "", fmt.Errorf("%w: commit message contains NUL", ErrInvalidArgument)
	}
	opts := RunOpts{
		Env: []string{
			"GIT_AUTHOR_NAME=" + name, "GIT_AUTHOR_EMAIL=" + email,
			"GIT_COMMITTER_NAME=" + name, "GIT_COMMITTER_EMAIL=" + email,
		},
		Stdin: strings.NewReader(message),
	}
	out, err := c.runWith(ctx, worktree, opts,
		"-c", "user.name="+name, "-c", "user.email="+email,
		"commit-tree", tree, "-p", parent, "-F", "-")
	if err != nil {
		return "", fmt.Errorf("commit-tree: %w", err)
	}
	return singleSHA("commit-tree", out)
}

// UpdateRef points ref at sha, overwriting it, with
// `git -C <wt> update-ref <ref> <sha>`. ref must be a full ref name that
// passes check-ref-format.
func (c *Client) UpdateRef(ctx context.Context, worktree, ref, sha string) error {
	if err := checkAbs("worktree", worktree); err != nil {
		return err
	}
	if err := checkSHA("sha", sha); err != nil {
		return err
	}
	if err := CheckRefName(ctx, c.r, ref); err != nil {
		return err
	}
	if _, err := c.run(ctx, worktree, "update-ref", ref, sha); err != nil {
		return fmt.Errorf("update-ref %s: %w", ref, err)
	}
	return nil
}
