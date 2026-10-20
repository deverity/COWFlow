package git

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Client offers the repository, worktree, index and status operations the
// workspace providers use (sprint 1 contract §6.4). Every call is one argv
// invocation through the Runner; paths are absolute and SHAs are hex, so no
// argument can be read as an option. A Client is safe for concurrent use
// when its Runner is.
type Client struct {
	r Runner
}

// NewClient returns a Client that runs every git call through r.
func NewClient(r Runner) *Client { return &Client{r: r} }

// run calls git with argv only.
func (c *Client) run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return c.r.Run(ctx, dir, args...)
}

// runWith calls git with per-call options. Without options it is run; with
// options the Runner must implement OptsRunner.
func (c *Client) runWith(ctx context.Context, dir string, o RunOpts, args ...string) ([]byte, error) {
	if len(o.Env) == 0 && o.Stdin == nil {
		return c.run(ctx, dir, args...)
	}
	or, ok := c.r.(OptsRunner)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrOptsUnsupported, c.r)
	}
	return or.RunWith(ctx, dir, o, args...)
}

// shaRE matches a full SHA-1 (40) or SHA-256 (64) object name.
var shaRE = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

// IsSHA reports whether s is a full lowercase hex object name (40 or 64
// characters).
func IsSHA(s string) bool { return shaRE.MatchString(s) }

func checkSHA(what, s string) error {
	if !IsSHA(s) {
		return fmt.Errorf("%w: %s must be a 40 or 64 character hex SHA, got %q", ErrInvalidArgument, what, truncate(s, 80))
	}
	return nil
}

// checkAbs requires an absolute, NUL-free path. An absolute path never
// starts with "-", so git cannot read it as an option.
func checkAbs(what, p string) error {
	if p == "" || !filepath.IsAbs(p) || strings.ContainsRune(p, 0) {
		return fmt.Errorf("%w: %s must be an absolute path, got %q", ErrInvalidArgument, what, truncate(p, 200))
	}
	return nil
}

// checkPlain rejects empty, option-like and NUL-containing values.
func checkPlain(what, s string) error {
	if s == "" || strings.HasPrefix(s, "-") || strings.ContainsRune(s, 0) {
		return fmt.Errorf("%w: %s %q", ErrInvalidArgument, what, truncate(s, 80))
	}
	return nil
}

// checkBranchShape is the local pre-check for branch names that reach git
// inside refs/heads/<name> or after -b; full validation is CheckBranchName.
func checkBranchShape(name string) error {
	if name == "" || strings.HasPrefix(name, "-") || strings.HasPrefix(name, "refs/") ||
		strings.Contains(name, "@{") || strings.ContainsRune(name, 0) {
		return fmt.Errorf("%w: %q", ErrInvalidRefName, truncate(name, 80))
	}
	return nil
}

// CheckBranchName validates name with git check-ref-format --branch (§17).
// A name git would normalize to something else is rejected too, so the name
// a caller stores is exactly the branch git creates.
func (c *Client) CheckBranchName(ctx context.Context, name string) error {
	normalized, err := CheckBranchName(ctx, c.r, name)
	if err != nil {
		return err
	}
	if normalized != name {
		return fmt.Errorf("%w: %q normalizes to %q", ErrInvalidRefName, name, normalized)
	}
	return nil
}

// singleSHA parses output that must be exactly one SHA and a newline.
func singleSHA(what string, out []byte) (string, error) {
	s := strings.TrimRight(string(out), "\r\n")
	if !IsSHA(s) || len(out) == len(s) {
		return "", fmt.Errorf("%w: %s: %q", ErrUnexpectedOutput, what, truncate(string(out), 120))
	}
	return s, nil
}
