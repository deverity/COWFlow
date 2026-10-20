// Package goproxy builds an offline, file-based Go module proxy with N
// synthetic modules and a consumer module that imports them all. It is the
// hermetic fixture for acceptance criterion 17 (shared dependency cache):
// `go mod download` and `go build` need no network.
package goproxy
