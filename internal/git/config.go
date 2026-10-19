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
