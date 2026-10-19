package domain

// setTestPrefix registers a prefix for kind and returns a restore func.
// Tests only: the real table is pending SPEC-GAPS U-DATA-2.
func setTestPrefix(kind Kind, prefix string) (restore func()) {
	prefixes[kind] = prefix
	return func() { delete(prefixes, kind) }
}
