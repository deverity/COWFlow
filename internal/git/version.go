package git

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version is a parsed git version. Raw is the trimmed `git version` output.
type Version struct {
	Major, Minor, Patch int
	Raw                 string
}

var versionRE = regexp.MustCompile(`^git version (\d+)\.(\d+)(?:\.(\d+))?`)

// ParseVersion parses `git version` output. It handles "2.47.1.windows.2" and
// "2.39.5 (Apple Git-154)"; a missing patch component is 0.
func ParseVersion(out string) (Version, error) {
	raw := strings.TrimSpace(out)
	m := versionRE.FindStringSubmatch(raw)
	if m == nil {
		return Version{}, fmt.Errorf("git: cannot parse version from %q", truncate(raw, 80))
	}
	v := Version{Raw: raw}
	var err error
	if v.Major, err = strconv.Atoi(m[1]); err != nil {
		return Version{}, fmt.Errorf("git: bad major version in %q: %w", truncate(raw, 80), err)
	}
	if v.Minor, err = strconv.Atoi(m[2]); err != nil {
		return Version{}, fmt.Errorf("git: bad minor version in %q: %w", truncate(raw, 80), err)
	}
	if m[3] != "" {
		if v.Patch, err = strconv.Atoi(m[3]); err != nil {
			return Version{}, fmt.Errorf("git: bad patch version in %q: %w", truncate(raw, 80), err)
		}
	}
	return v, nil
}

// CheckVersion runs `git version` and enforces the minimum of 2.40 (§3.1).
// A too-old git returns the parsed Version and an error wrapping
// ErrVersionTooOld that names the version found.
func CheckVersion(ctx context.Context, r Runner) (Version, error) {
	out, err := r.Run(ctx, "", "version")
	if err != nil {
		return Version{}, fmt.Errorf("git version: %w", err)
	}
	v, err := ParseVersion(strings.TrimSpace(string(out)))
	if err != nil {
		return Version{}, err
	}
	if v.Major < 2 || (v.Major == 2 && v.Minor < 40) {
		return v, fmt.Errorf("%w: found %s", ErrVersionTooOld, v.Raw)
	}
	return v, nil
}
