package git

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// WorktreeAddOptions configures WorktreeAdd.
type WorktreeAddOptions struct {
	NewBranch  string // -b <name>; empty = check out an existing branch named in Commitish
	Commitish  string // base SHA (hex) or existing branch
	NoCheckout bool
}

// Worktree is one entry of `git worktree list --porcelain -z`.
type Worktree struct {
	Path   string // as git reports it
	Head   string // commit SHA; empty for a bare main worktree
	Branch string // short branch name; empty when detached or bare
	// Locked and Prunable are true when git reports the attribute, with or
	// without a reason.
	Locked, Prunable bool
}

// WorktreeAdd creates a worktree at path with
// `git -C <repo> worktree add [--no-checkout] [-b <new>] <path> <commitish>`.
// With NewBranch set, git fails if that branch already exists.
func (c *Client) WorktreeAdd(ctx context.Context, repo, path string, o WorktreeAddOptions) error {
	if err := checkAbs("repo", repo); err != nil {
		return err
	}
	if err := checkAbs("path", path); err != nil {
		return err
	}
	if !IsSHA(o.Commitish) {
		if err := checkBranchShape(o.Commitish); err != nil {
			return fmt.Errorf("worktree add commitish: %w", err)
		}
	}
	args := []string{"worktree", "add"}
	if o.NoCheckout {
		args = append(args, "--no-checkout")
	}
	if o.NewBranch != "" {
		if err := checkBranchShape(o.NewBranch); err != nil {
			return fmt.Errorf("worktree add new branch: %w", err)
		}
		args = append(args, "-b", o.NewBranch)
	}
	args = append(args, path, o.Commitish)
	if _, err := c.run(ctx, repo, args...); err != nil {
		return fmt.Errorf("worktree add %s: %w", path, err)
	}
	return nil
}

// WorktreeRemove removes the worktree at path with
// `git -C <repo> worktree remove [--force] <path>`. Without force, git
// refuses a worktree with modified or untracked files.
func (c *Client) WorktreeRemove(ctx context.Context, repo, path string, force bool) error {
	if err := checkAbs("repo", repo); err != nil {
		return err
	}
	if err := checkAbs("path", path); err != nil {
		return err
	}
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	if _, err := c.run(ctx, repo, args...); err != nil {
		return fmt.Errorf("worktree remove %s: %w", path, err)
	}
	return nil
}

// WorktreePrune drops administrative data of worktrees whose directory is
// gone, with `git -C <repo> worktree prune`.
func (c *Client) WorktreePrune(ctx context.Context, repo string) error {
	if err := checkAbs("repo", repo); err != nil {
		return err
	}
	if _, err := c.run(ctx, repo, "worktree", "prune"); err != nil {
		return fmt.Errorf("worktree prune: %w", err)
	}
	return nil
}

// WorktreeList lists the repository's worktrees with
// `git -C <repo> worktree list --porcelain -z`.
func (c *Client) WorktreeList(ctx context.Context, repo string) ([]Worktree, error) {
	if err := checkAbs("repo", repo); err != nil {
		return nil, err
	}
	out, err := c.run(ctx, repo, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, fmt.Errorf("worktree list: %w", err)
	}
	return ParseWorktreeList(out)
}

// ParseWorktreeList parses `git worktree list --porcelain -z` output: blocks
// of NUL-terminated attribute lines (`worktree <path>`, `HEAD <sha>`,
// `branch <ref>` or `detached`, `bare`, `locked [reason]`,
// `prunable [reason]`), each block ended by an empty line (an extra NUL).
// Unknown attributes are ignored. Truncated or malformed output returns
// ErrUnexpectedOutput and no entries.
func ParseWorktreeList(out []byte) ([]Worktree, error) {
	bad := func(format string, a ...any) ([]Worktree, error) {
		return nil, fmt.Errorf("%w: worktree list: %s", ErrUnexpectedOutput, fmt.Sprintf(format, a...))
	}
	if len(out) == 0 {
		return bad("empty output")
	}
	if !bytes.HasSuffix(out, []byte{0, 0}) {
		return bad("output does not end with a block terminator")
	}
	var (
		list []Worktree
		cur  *Worktree
		seen map[string]bool
	)
	// The final two NULs end the last line and the last block; drop the
	// last empty field.
	fields := strings.Split(string(out[:len(out)-1]), "\x00")
	for _, line := range fields {
		if line == "" {
			if cur == nil {
				return bad("empty block")
			}
			list = append(list, *cur)
			cur = nil
			continue
		}
		key, val, hasVal := strings.Cut(line, " ")
		if cur == nil {
			if key != "worktree" || !hasVal || val == "" {
				return bad("block does not start with a worktree line: %q", truncate(line, 80))
			}
			cur = &Worktree{Path: val}
			seen = map[string]bool{}
			continue
		}
		if seen[key] {
			return bad("duplicate %q attribute", truncate(key, 40))
		}
		seen[key] = true
		switch key {
		case "worktree":
			return bad("worktree line inside a block")
		case "HEAD":
			if !IsSHA(val) {
				return bad("bad HEAD %q", truncate(val, 80))
			}
			cur.Head = val
		case "branch":
			name, ok := strings.CutPrefix(val, "refs/heads/")
			if !ok || name == "" || seen["detached"] {
				return bad("bad branch %q", truncate(val, 80))
			}
			cur.Branch = name
		case "detached":
			if hasVal || cur.Branch != "" {
				return bad("bad detached line")
			}
		case "bare":
			if hasVal {
				return bad("bad bare line")
			}
		case "locked":
			cur.Locked = true
		case "prunable":
			cur.Prunable = true
		}
	}
	if cur != nil {
		return bad("unterminated block")
	}
	return list, nil
}

// GitPath resolves a path inside the worktree's git directory (for example
// "index") with `git -C <wt> rev-parse --git-path <name>`. A relative result
// is resolved against wt; the returned path is absolute and cleaned.
func (c *Client) GitPath(ctx context.Context, worktree, name string) (string, error) {
	if err := checkAbs("worktree", worktree); err != nil {
		return "", err
	}
	if err := checkPlain("git path name", name); err != nil {
		return "", err
	}
	if !filepath.IsLocal(filepath.FromSlash(name)) {
		return "", fmt.Errorf("%w: git path name %q must be relative and stay inside the git directory", ErrInvalidArgument, truncate(name, 80))
	}
	out, err := c.run(ctx, worktree, "rev-parse", "--git-path", name)
	if err != nil {
		return "", fmt.Errorf("git path %q: %w", name, err)
	}
	p := strings.TrimRight(string(out), "\r\n")
	if p == "" || len(p) == len(out) || strings.ContainsAny(p, "\r\n\x00") {
		return "", fmt.Errorf("%w: rev-parse --git-path: %q", ErrUnexpectedOutput, truncate(string(out), 200))
	}
	p = filepath.FromSlash(p)
	if !filepath.IsAbs(p) {
		p = filepath.Join(worktree, p)
	}
	return filepath.Clean(p), nil
}
