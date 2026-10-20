package bench

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// settle flushes the volume holding root so that FreeBytes is exact: btrfs
// updates its free-space counters lazily (sprint-1.md S1-T21 step 2).
func settle(root string) error {
	d, err := os.Open(root) //nolint:gosec // G304: the benchmark root chosen by the caller
	if err != nil {
		return fmt.Errorf("settle: %w", err)
	}
	defer func() { _ = d.Close() }()
	if err := unix.Syncfs(int(d.Fd())); err != nil {
		return fmt.Errorf("settle: syncfs %s: %w", root, err)
	}
	return nil
}
