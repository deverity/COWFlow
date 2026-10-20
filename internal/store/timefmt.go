package store

import (
	"fmt"
	"time"
)

// timeLayout is RFC 3339 in UTC with a fixed nine-digit fraction. Unlike
// time.RFC3339Nano it never trims trailing zeros, so comparing two stored
// values as strings orders them in time (LRU queries rely on this).
const timeLayout = "2006-01-02T15:04:05.000000000Z"

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("store: parse time %q: %w", s, err)
	}
	return t.UTC(), nil
}
