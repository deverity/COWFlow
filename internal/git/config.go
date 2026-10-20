package git

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var configKeyRE = regexp.MustCompile(`^[a-z0-9]+(\.[a-zA-Z0-9-]+)+$`)

// ConfigGet returns the effective value of a config key via
// `git config --get <key>`, run in dir ("" for no -C). Exit code 1 means the
// key is not set: found is false and err is nil. Any other failure is an
// error. The key is validated before it reaches git.
func ConfigGet(ctx context.Context, r Runner, dir, key string) (value string, found bool, err error) {
	if !configKeyRE.MatchString(key) {
		return "", false, fmt.Errorf("git: invalid config key %q", truncate(key, 80))
	}
	out, err := r.Run(ctx, dir, "config", "--get", key)
	if err != nil {
		var ge *Error
		if errors.As(err, &ge) && ge.ExitCode == 1 {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimRight(string(out), "\r\n"), true, nil
}

// ConfigSetWorktree sets key=value in the worktree's own config.worktree
// (§7: per-worktree core.autocrlf=false). The first time, it turns on
// extensions.worktreeConfig with
// `git -C <wt> config extensions.worktreeConfig true`, which writes the
// repository's shared config; it then runs
// `git -C <wt> config --worktree <key> <value>`.
//
// git-config(1): with extensions.worktreeConfig on, core.bare=true or
// core.worktree in the shared config must first move to the main worktree's
// config.worktree. COWFlow does not move them; it refuses with
// ErrWorktreeConfigUnsafe instead.
func (c *Client) ConfigSetWorktree(ctx context.Context, worktree, key, value string) error {
	if err := checkAbs("worktree", worktree); err != nil {
		return err
	}
	if !configKeyRE.MatchString(key) {
		return fmt.Errorf("%w: config key %q", ErrInvalidArgument, truncate(key, 80))
	}
	// A value starting with "-" would be parsed as an option.
	if strings.HasPrefix(value, "-") || strings.ContainsRune(value, 0) {
		return fmt.Errorf("%w: config value %q", ErrInvalidArgument, truncate(value, 80))
	}
	if err := c.enableWorktreeConfig(ctx, worktree); err != nil {
		return err
	}
	if _, err := c.run(ctx, worktree, "config", "--worktree", key, value); err != nil {
		return fmt.Errorf("config --worktree %s: %w", key, err)
	}
	return nil
}

// enableWorktreeConfig turns on extensions.worktreeConfig unless it is
// already true.
func (c *Client) enableWorktreeConfig(ctx context.Context, worktree string) error {
	const ext = "extensions.worktreeConfig"
	v, found, err := ConfigGet(ctx, c.r, worktree, ext)
	if err != nil {
		return fmt.Errorf("config %s: %w", ext, err)
	}
	if found && configTrue(v) {
		return nil
	}
	bare, found, err := ConfigGet(ctx, c.r, worktree, "core.bare")
	if err != nil {
		return fmt.Errorf("config core.bare: %w", err)
	}
	if found && configTrue(bare) {
		return ErrWorktreeConfigUnsafe
	}
	if _, found, err = ConfigGet(ctx, c.r, worktree, "core.worktree"); err != nil {
		return fmt.Errorf("config core.worktree: %w", err)
	}
	if found {
		return ErrWorktreeConfigUnsafe
	}
	if _, err := c.run(ctx, worktree, "config", ext, "true"); err != nil {
		return fmt.Errorf("config %s: %w", ext, err)
	}
	return nil
}

// configTrue reports whether a raw config value is a git boolean true.
func configTrue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "yes", "on", "1":
		return true
	}
	return false
}
