package workspacetest

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/deverity/cowflow/internal/platform"
)

// FakeVolumes is a scripted workspace.Volumes. Volumes are registered by
// mount path; a path belongs to the volume with the longest mount path that
// contains it. An unmounted volume, or a path under no volume, reports an
// error wrapping fs.ErrNotExist, as a detached Dev Drive would.
type FakeVolumes struct {
	mu        sync.Mutex
	vols      map[string]platform.Volume // by cleaned mount path
	unmounted map[string]bool
}

// NewFakeVolumes returns FakeVolumes with the given volumes registered and
// mounted.
func NewFakeVolumes(vols ...platform.Volume) *FakeVolumes {
	f := &FakeVolumes{}
	for _, v := range vols {
		f.Set(v)
	}
	return f
}

// Set registers or replaces the volume mounted at v.MountPath, for example
// to change the filesystem between a create and an inspect.
func (f *FakeVolumes) Set(v platform.Volume) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.vols == nil {
		f.vols = map[string]platform.Volume{}
		f.unmounted = map[string]bool{}
	}
	f.vols[filepath.Clean(v.MountPath)] = v
}

// Unmount marks the volume at mount as not mounted.
func (f *FakeVolumes) Unmount(mount string) { f.setMounted(mount, false) }

// Mount marks the volume at mount as mounted again.
func (f *FakeVolumes) Mount(mount string) { f.setMounted(mount, true) }

func (f *FakeVolumes) setMounted(mount string, on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.unmounted == nil {
		f.unmounted = map[string]bool{}
	}
	f.unmounted[filepath.Clean(mount)] = !on
}

// VolumeOf returns the volume holding path.
func (f *FakeVolumes) VolumeOf(ctx context.Context, path string) (platform.Volume, error) {
	if err := ctx.Err(); err != nil {
		return platform.Volume{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	p := filepath.Clean(path)
	best := ""
	found := false
	for m := range f.vols {
		if within(p, m) && (!found || len(m) > len(best)) {
			best, found = m, true
		}
	}
	if !found {
		return platform.Volume{}, fmt.Errorf("fake volume of %s: no volume: %w", path, fs.ErrNotExist)
	}
	if f.unmounted[best] {
		return platform.Volume{}, fmt.Errorf("fake volume of %s: %s is not mounted: %w", path, best, fs.ErrNotExist)
	}
	return f.vols[best], nil
}

// SameVolume reports whether a and b are on volumes with the same ID.
func (f *FakeVolumes) SameVolume(ctx context.Context, a, b string) (bool, error) {
	va, err := f.VolumeOf(ctx, a)
	if err != nil {
		return false, err
	}
	vb, err := f.VolumeOf(ctx, b)
	if err != nil {
		return false, err
	}
	return va.ID == vb.ID, nil
}

// within reports whether path p is mount or lies under it.
func within(p, mount string) bool {
	if p == mount {
		return true
	}
	if !strings.HasSuffix(mount, string(os.PathSeparator)) {
		mount += string(os.PathSeparator)
	}
	return strings.HasPrefix(p, mount)
}
