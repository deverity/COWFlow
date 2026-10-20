package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Branch naming errors. The CLI maps ErrInvalidSlug, ErrInvalidProject and
// ErrKindNotAllowed to exit 1 and ErrNonConformingBranch to exit 4.
var (
	ErrInvalidSlug         = errors.New("branch: invalid task slug")
	ErrInvalidProject      = errors.New("branch: invalid project name")
	ErrKindNotAllowed      = errors.New("branch: --kind is not allowed with this conventions preset")
	ErrNonConformingBranch = errors.New("branch: name does not conform to the gitflow preset")
)

// Conventions presets ([conventions].preset, SCOPE.md §24).
const (
	PresetGitflow = "gitflow"
	PresetTrunk   = "trunk"
	PresetNone    = "none"
)

// BranchNamer computes a task's branch name (SPEC-GAPS C2). The result is
// stored in tasks.branch and never recomputed. The caller still runs
// `git check-ref-format --branch` on the full name.
type BranchNamer interface {
	TaskBranch(project, kind, slug string) (string, error)
}

// NewBranchNamer returns the BranchNamer for a [conventions].preset value.
func NewBranchNamer(preset string) (BranchNamer, error) {
	switch preset {
	case PresetGitflow:
		return gitflowNamer{}, nil
	case PresetTrunk, PresetNone:
		return plainNamer{}, nil
	}
	return nil, fmt.Errorf("branch: unknown conventions preset %q", preset)
}

// gitflowKinds are the branch kinds of the gitflow preset (§24.2).
var gitflowKinds = map[string]bool{
	"feature": true, "bugfix": true, "chore": true, "hotfix": true, "release": true,
}

// defaultGitflowKind is used when --kind is omitted.
const defaultGitflowKind = "feature"

type gitflowNamer struct{}

// TaskBranch returns <kind>/<slug>; an empty kind means feature.
func (gitflowNamer) TaskBranch(_, kind, slug string) (string, error) {
	if err := ValidateSlug(slug); err != nil {
		return "", err
	}
	if kind == "" {
		kind = defaultGitflowKind
	}
	if !gitflowKinds[kind] {
		return "", fmt.Errorf("%w: unknown kind %q", ErrNonConformingBranch, kind)
	}
	return kind + "/" + slug, nil
}

type plainNamer struct{}

// TaskBranch returns cowflow/<project>/<slug>. Any kind is rejected: the
// trunk and none presets have no kinds.
func (plainNamer) TaskBranch(project, kind, slug string) (string, error) {
	if kind != "" {
		return "", fmt.Errorf("%w: %q", ErrKindNotAllowed, kind)
	}
	if err := ValidateSlug(slug); err != nil {
		return "", err
	}
	if err := validateProjectComponent(project); err != nil {
		return "", err
	}
	return "cowflow/" + project + "/" + slug, nil
}

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// windowsReserved holds the Windows reserved device names (lower case),
// compared with the part of the slug before the first dot.
var windowsReserved = func() map[string]bool {
	m := map[string]bool{"con": true, "prn": true, "aux": true, "nul": true}
	for i := '1'; i <= '9'; i++ {
		m["com"+string(i)] = true
		m["lpt"+string(i)] = true
	}
	return m
}()

// ValidateSlug checks a task slug against the C2 grammar:
// ^[a-z0-9][a-z0-9._-]{0,63}$, no "..", no trailing ".", not ending ".lock",
// and not a Windows reserved device name (the slug becomes a directory name).
func ValidateSlug(slug string) error {
	switch {
	case !slugPattern.MatchString(slug):
		return fmt.Errorf("%w: %q must match %s", ErrInvalidSlug, slug, slugPattern)
	case strings.Contains(slug, ".."):
		return fmt.Errorf("%w: %q contains \"..\"", ErrInvalidSlug, slug)
	case strings.HasSuffix(slug, "."):
		return fmt.Errorf("%w: %q ends with \".\"", ErrInvalidSlug, slug)
	case strings.HasSuffix(slug, ".lock"):
		return fmt.Errorf("%w: %q ends with \".lock\"", ErrInvalidSlug, slug)
	}
	stem, _, _ := strings.Cut(slug, ".")
	if windowsReserved[stem] {
		return fmt.Errorf("%w: %q is a reserved Windows device name", ErrInvalidSlug, slug)
	}
	return nil
}

// validateProjectComponent rejects project names that cannot be one ref path
// component. It is a pre-check; git check-ref-format has the last word.
func validateProjectComponent(p string) error {
	bad := p == "" || p == "@" ||
		strings.HasPrefix(p, ".") || strings.HasSuffix(p, ".") ||
		strings.HasSuffix(p, ".lock") ||
		strings.Contains(p, "..") || strings.Contains(p, "@{") ||
		strings.ContainsAny(p, " ~^:?*[\\/\x7f") ||
		strings.IndexFunc(p, func(r rune) bool { return r < 0x20 }) >= 0
	if bad {
		return fmt.Errorf("%w: %q cannot be a branch path component", ErrInvalidProject, p)
	}
	return nil
}
