package domain

import "time"

// Project is a repository registered with COWFlow (SCOPE.md §13).
type Project struct {
	ID        string
	Name      string
	RepoRoot  string // canonical path
	CreatedAt time.Time
}
