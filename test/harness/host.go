package harness

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/deverity/cowflow/internal/platform"
)

// FakeHost is an in-memory platform.Host. Tests set its exported fields
// before use; SimDevDrive mutates Volumes while it provisions.
type FakeHost struct {
	mu      sync.Mutex
	OSInfo  platform.OSInfo
	Volumes map[string]platform.VolumeInfo // key: mount or path prefix
	ProjFS  bool
}

var _ platform.Host = (*FakeHost)(nil)

// OS returns the configured OSInfo.
func (h *FakeHost) OS(ctx context.Context) (platform.OSInfo, error) {
	if err := ctx.Err(); err != nil {
		return platform.OSInfo{}, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.OSInfo, nil
}

// Volume returns the volume whose key is the longest path prefix of path.
// Keys match on whole path elements, ignoring case and the difference
// between '/' and '\'. When no key matches it returns
// platform.ErrNotMounted: the fake never invents a fallback volume.
func (h *FakeHost) Volume(ctx context.Context, path string) (platform.VolumeInfo, error) {
	if err := ctx.Err(); err != nil {
		return platform.VolumeInfo{}, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	keys := make([]string, 0, len(h.Volumes))
	for k := range h.Volumes {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic tie-break between equivalent keys

	want := normalizePath(path)
	best, bestLen := "", -1
	for _, k := range keys {
		nk := normalizePath(k)
		if hasPathPrefix(want, nk) && len(nk) > bestLen {
			best, bestLen = k, len(nk)
		}
	}
	if bestLen < 0 {
		return platform.VolumeInfo{}, platform.ErrNotMounted
	}
	return h.Volumes[best], nil
}

// ProjFSAvailable returns the configured ProjFS flag.
func (h *FakeHost) ProjFSAvailable(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.ProjFS, nil
}

func (h *FakeHost) setVolume(key string, v platform.VolumeInfo) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.Volumes == nil {
		h.Volumes = make(map[string]platform.VolumeInfo)
	}
	h.Volumes[key] = v
}

func (h *FakeHost) removeVolume(key string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.Volumes, key)
}

func (h *FakeHost) volume(key string) (platform.VolumeInfo, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	v, ok := h.Volumes[key]
	return v, ok
}

func normalizePath(p string) string {
	p = strings.ToLower(strings.ReplaceAll(p, `\`, "/"))
	if p == "/" {
		return p
	}
	return strings.TrimRight(p, "/")
}

// hasPathPrefix reports whether path equals prefix or lies below it.
func hasPathPrefix(path, prefix string) bool {
	if prefix == "/" {
		return strings.HasPrefix(path, "/")
	}
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	return len(path) == len(prefix) || path[len(prefix)] == '/'
}
