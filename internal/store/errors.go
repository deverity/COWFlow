package store

import "errors"

var (
	// ErrNotFound means the requested row does not exist.
	ErrNotFound = errors.New("store: not found")
	// ErrConflict means a uniqueness constraint was violated (CLI exit code 3).
	ErrConflict = errors.New("store: conflict")
	// ErrInvalidTransition means the row's current state does not allow the
	// requested change.
	ErrInvalidTransition = errors.New("store: invalid state transition")
	// ErrPortRangeExhausted means no port slot fits below port 65535.
	ErrPortRangeExhausted = errors.New("store: port range exhausted")
)
