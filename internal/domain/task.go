package domain

import "time"

// TaskStateCreated is the only tasks.state written in Sprint 1; the task
// lifecycle arrives with S2-E1.
const TaskStateCreated = "created"

// Task is the minimal tasks row Sprint 1 needs (SCOPE.md §13).
// S2-E1 extends it.
type Task struct {
	ID        string // tsk_<ULID>; minted by the store when empty
	ProjectID string
	Slug      string
	Title     string
	Kind      string // empty unless the gitflow preset is active
	Base      string // base ref name as the user gave it
	BaseSHA   string // resolved full commit SHA
	Branch    string // computed once by BranchNamer, never recomputed
	Paths     []string
	Sparse    bool
	State     string
	CreatedAt time.Time
}
