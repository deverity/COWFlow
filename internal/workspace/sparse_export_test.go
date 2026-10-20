package workspace

// ConeMatch exposes the cone matcher of dirs (already normalized) to the
// external tests.
func ConeMatch(dirs []string, rel string, isDir bool) bool {
	return newConeMatcher(dirs).Match(rel, isDir)
}
