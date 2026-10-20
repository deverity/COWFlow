package bench

import (
	"context"
	"fmt"
	"math"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/deverity/cowflow/internal/platform"
)

// Machine describes the host for the result JSON.
type Machine struct {
	CPU   string
	RAMGB float64
}

// HostMachine reports the CPU model and the installed memory. A value that
// cannot be read is left at its zero value: the result stays usable.
func HostMachine() Machine {
	cpu, ram := hostCPU(), hostRAMBytes()
	m := Machine{CPU: cpu}
	if cpu == "" {
		m.CPU = runtime.GOARCH
	}
	// One decimal place is enough to tell 15.6 GB from 16 GB.
	m.RAMGB = float64(int64(float64(ram)/float64(1<<30)*10+0.5)) / 10
	return m
}

// FreeSpace returns the free bytes of the volume holding root after the
// volume has been settled, so two readings can be subtracted.
func FreeSpace(root string) (int64, error) {
	if err := settle(root); err != nil {
		return 0, err
	}
	n, err := platform.FreeBytes(root)
	if err != nil {
		return 0, fmt.Errorf("free bytes of %s: %w", root, err)
	}
	return int64(min(n, math.MaxInt64)), nil
}

// GitVersion returns the output of the version command without its prefix.
func GitVersion(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "--version").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimPrefix(strings.TrimSpace(string(out)), "git version ")
}

// SourceCommit returns the commit of the COWFlow source tree under test, or
// "unknown" outside a checkout.
func SourceCommit(ctx context.Context, dir string) string {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD").Output() //nolint:gosec // G204: fixed arguments; dir is the source tree
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}
