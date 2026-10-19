package git

import "context"

// Runner is the single entry point for every git subprocess (§8.1).
// dir == "" means "no -C"; otherwise "-C dir" is prepended.
type Runner interface {
	Run(ctx context.Context, dir string, args ...string) ([]byte, error)
}
