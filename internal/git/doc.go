// Package git drives the git CLI for COWFlow (SCOPE.md §8.1).
//
// Every git subprocess goes through a Runner: argument slices only (never a
// shell string, §7, §17), "-C <dir>" instead of a working directory,
// GIT_TERMINAL_PROMPT=0 and a per-call timeout. The Runner interface is also
// the seam where the hermetic harness injects faults (§18.1).
package git
