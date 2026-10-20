package git

import (
	"context"
	"fmt"
	"strings"
	"unicode"
)

// ValidateSparseDir normalizes and validates one cone-mode sparse directory
// taken from user input (`task create --paths`). It converts `\` to `/`,
// trims trailing slashes and returns the normalized, repository-relative
// path. It rejects empty strings, absolute and UNC paths, drive letters,
// empty, `.` and `..` segments, a leading `-`, a leading `"` (git would
// unquote the stdin line), the glob characters `*?[` (cone mode takes
// literal directories) and control characters.
func ValidateSparseDir(p string) (string, error) {
	bad := func(why string) (string, error) {
		return "", fmt.Errorf("%w: sparse path %q: %s", ErrInvalidArgument, truncate(p, 200), why)
	}
	n := strings.TrimRight(strings.ReplaceAll(p, `\`, "/"), "/")
	if strings.TrimSpace(n) == "" {
		return bad("empty path")
	}
	for _, r := range n {
		if unicode.IsControl(r) {
			return bad("control characters are not allowed")
		}
		if strings.ContainsRune("*?[", r) {
			return bad("glob characters *, ? and [ are not allowed")
		}
	}
	switch {
	case strings.HasPrefix(n, "/"):
		return bad("absolute paths are not allowed")
	case len(n) >= 2 && n[1] == ':' && (n[0]|0x20) >= 'a' && (n[0]|0x20) <= 'z':
		return bad("drive letters are not allowed")
	case strings.HasPrefix(n, "-"):
		return bad("must not start with '-'")
	case strings.HasPrefix(n, `"`):
		return bad(`must not start with '"'`)
	}
	for _, seg := range strings.Split(n, "/") {
		switch seg {
		case "":
			return bad("empty path segment")
		case ".":
			return bad("'.' segments are not allowed")
		case "..":
			return bad("'..' segments are not allowed")
		}
	}
	return n, nil
}

// SparseSet restricts the worktree to the given directories with
// `git -C <wt> sparse-checkout set --cone --stdin`. Every directory is
// validated first (ValidateSparseDir) and nothing is run when one is
// invalid. The directories travel newline-separated on stdin, never on argv,
// so they can neither inject options nor overflow the command-line limit.
// Cone mode also keeps the files directly in the root and in each parent of
// a listed directory. An empty list keeps only those root files. A path that
// is not a directory makes git fail, and the error names the path.
func (c *Client) SparseSet(ctx context.Context, worktree string, dirs []string) error {
	if err := checkAbs("worktree", worktree); err != nil {
		return err
	}
	var in strings.Builder
	norm := make([]string, 0, len(dirs))
	for _, d := range dirs {
		n, err := ValidateSparseDir(d)
		if err != nil {
			return err
		}
		norm = append(norm, n)
		in.WriteString(n)
		in.WriteByte('\n')
	}
	if err := c.rejectFiles(ctx, worktree, norm); err != nil {
		return err
	}
	if _, err := c.runWith(ctx, worktree, RunOpts{Stdin: strings.NewReader(in.String())},
		"sparse-checkout", "set", "--cone", "--stdin"); err != nil {
		return fmt.Errorf("sparse-checkout set: %w", err)
	}
	return nil
}

// rejectFiles fails when one of dirs names a file at HEAD: git accepts that
// silently over stdin and would materialize nothing for it. The paths go to
// `git cat-file --batch-check` on stdin as HEAD:<path> lines, so they stay
// off argv here too. Paths missing at HEAD pass (a directory may be new).
func (c *Client) rejectFiles(ctx context.Context, worktree string, dirs []string) error {
	if len(dirs) == 0 {
		return nil
	}
	var in strings.Builder
	for _, d := range dirs {
		in.WriteString("HEAD:" + d + "\n")
	}
	out, err := c.runWith(ctx, worktree, RunOpts{Stdin: strings.NewReader(in.String())},
		"cat-file", "--batch-check")
	if err != nil {
		return fmt.Errorf("sparse-checkout set: check paths: %w", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != len(dirs) {
		return fmt.Errorf("%w: cat-file --batch-check: %d answers for %d paths", ErrUnexpectedOutput, len(lines), len(dirs))
	}
	for i, line := range lines {
		if f := strings.Fields(line); len(f) == 3 && f[1] == "blob" {
			return fmt.Errorf("%w: sparse path %q is a file, not a directory", ErrInvalidArgument, truncate(dirs[i], 200))
		}
	}
	return nil
}

// SparseDisable turns sparse checkout off and restores the full working tree
// with `git -C <wt> sparse-checkout disable`.
func (c *Client) SparseDisable(ctx context.Context, worktree string) error {
	if err := checkAbs("worktree", worktree); err != nil {
		return err
	}
	if _, err := c.run(ctx, worktree, "sparse-checkout", "disable"); err != nil {
		return fmt.Errorf("sparse-checkout disable: %w", err)
	}
	return nil
}

// SparseList returns the directories of the worktree's sparse set with
// `git -C <wt> sparse-checkout list`. It fails when sparse checkout is off.
func (c *Client) SparseList(ctx context.Context, worktree string) ([]string, error) {
	if err := checkAbs("worktree", worktree); err != nil {
		return nil, err
	}
	out, err := c.run(ctx, worktree, "sparse-checkout", "list")
	if err != nil {
		return nil, fmt.Errorf("sparse-checkout list: %w", err)
	}
	var dirs []string
	for line := range strings.SplitSeq(string(out), "\n") {
		if line = strings.TrimSuffix(line, "\r"); line != "" {
			dirs = append(dirs, line)
		}
	}
	return dirs, nil
}

// PopulateSparse brings the index and the working tree to HEAD while
// honoring the sparse patterns, with `git -C <wt> read-tree -mu HEAD`.
func (c *Client) PopulateSparse(ctx context.Context, worktree string) error {
	return c.ReadTree(ctx, worktree, "-mu", "HEAD")
}
