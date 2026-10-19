// Package platform holds the operating-system specifics of COWFlow
// (SCOPE.md §7, §16.1): path canonicalization and confinement, long paths,
// process trees, volumes and the Dev Drive.
//
// OS-specific code lives in files with a GOOS suffix (_windows.go) or a
// build-constrained counterpart (_other.go) behind one shared API, so the
// package builds on Windows, Linux and macOS with CGO_ENABLED=0.
package platform
