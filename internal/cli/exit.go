package cli

import "errors"

// Exit codes (SCOPE.md §14).
const (
	ExitOK       = 0
	ExitUsage    = 1
	ExitState    = 2
	ExitConflict = 3
	ExitPolicy   = 4
)

// ExitError carries the process exit code for err through wrapping.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }

func (e *ExitError) Unwrap() error { return e.Err }

func usageErr(err error) error { return &ExitError{Code: ExitUsage, Err: err} }

// exitUnclassified is the code for errors §14 does not classify.
// PENDING SPEC-GAPS U-API-12 — AG-LEAD sets the value at kickoff.
var exitUnclassified = ExitState

// codeOf maps err to an exit code. parsed reports whether cobra got past
// flag and argument parsing; errors before that point are usage errors.
func codeOf(err error, parsed bool) int {
	if err == nil {
		return ExitOK
	}
	var ee *ExitError
	if errors.As(err, &ee) {
		return ee.Code
	}
	if !parsed {
		return ExitUsage
	}
	return exitUnclassified
}
