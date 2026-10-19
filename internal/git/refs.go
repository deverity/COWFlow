package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// CheckBranchName asks git whether name is a valid branch name and returns the
// normalized name (§17). Rejections wrap ErrInvalidRefName; a missing git
// executable, a timeout or a canceled context are returned unchanged because
// they are not the caller's fault.
//
// Names that git would misread as options or that need a repository to expand
// ("@{-1}") never reach git.
func CheckBranchName(ctx context.Context, r Runner, name string) (string, error) {
	if name == "" || strings.HasPrefix(name, "-") || strings.Contains(name, "@{") || strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("%w: %q", ErrInvalidRefName, name)
	}
	out, err := r.Run(ctx, "", "check-ref-format", "--branch", name)
	if err != nil {
		return "", classifyRefError(err, name)
	}
	normalized := strings.TrimSpace(string(out))
	// Defense in depth: never hand back a name that could be read as an option.
	if normalized == "" || strings.HasPrefix(normalized, "-") {
		return "", fmt.Errorf("%w: %q", ErrInvalidRefName, name)
	}
	return normalized, nil
}

// CheckRefName validates a full ref such as refs/cowflow/changes/<id>. The ref
// must start with "refs/".
func CheckRefName(ctx context.Context, r Runner, ref string) error {
	if !strings.HasPrefix(ref, "refs/") || strings.ContainsRune(ref, 0) {
		return fmt.Errorf("%w: %q", ErrInvalidRefName, ref)
	}
	if _, err := r.Run(ctx, "", "check-ref-format", ref); err != nil {
		return classifyRefError(err, ref)
	}
	return nil
}

// classifyRefError maps a git exit with a positive code to ErrInvalidRefName
// and passes every other failure through.
func classifyRefError(err error, name string) error {
	var ge *Error
	if errors.As(err, &ge) && ge.ExitCode > 0 {
		return fmt.Errorf("%w: %q", ErrInvalidRefName, name)
	}
	return err
}
