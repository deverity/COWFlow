package store

import "errors"

var (
	// ErrNotFound means the requested row does not exist.
	ErrNotFound = errors.New("store: not found")
	// ErrConflict means a uniqueness constraint was violated (CLI exit code 3).
	ErrConflict = errors.New("store: conflict")
)
