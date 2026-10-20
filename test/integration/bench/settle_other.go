//go:build !linux

package bench

// settle does nothing: ReFS reports free space immediately, and on macOS the
// benchmarks run on a dedicated APFS volume with Spotlight turned off
// (`mdutil -i off <volume>`, done by the workflow, never by the test).
func settle(string) error { return nil }
