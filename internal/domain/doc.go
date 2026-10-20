// Package domain holds the dependency-free seams shared by every service: the
// injectable Clock, the prefixed ULID generator, and the workspace, golden
// tree, task and branch-naming types. It imports nothing internal and
// performs no I/O.
package domain
