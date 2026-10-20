package cli

import "errors"

// Exit codes (SCOPE.md §14): 1 the command line or config file is wrong;
// 2 machine or project state prevents the command; 3 it collides with
// something that exists; 4 a COWFlow rule refused it and only --force or
// the lead may override.
const (
	ExitOK       = 0
	ExitUsage    = 1
	ExitState    = 2
	ExitConflict = 3
	ExitPolicy   = 4
)

// Codes of the failures §14 does not name, per the rule above (SPEC-GAPS
// U-API-12). No new numbers: each is one of the §14 codes.
const (
	// exitUnclassified: an unclassified or internal error.
	exitUnclassified = ExitState
	// exitNotImplemented: a command stub whose behavior lands later.
	exitNotImplemented = ExitState
	// exitDoctorFailed: doctor found at least one failing check (warnings
	// never fail).
	exitDoctorFailed = ExitState
	// exitNotElevated: the UAC prompt was declined, or an operation that
	// needs administrator rights ran without them.
	exitNotElevated = ExitState
	// exitUnsupported: the OS or Windows build cannot run the command.
	exitUnsupported = ExitState
)

// ExitError carries the process exit code for err through wrapping.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }

func (e *ExitError) Unwrap() error { return e.Err }

func usageErr(err error) error { return &ExitError{Code: ExitUsage, Err: err} }

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
