package platform

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCanonicalConcurrentMkdirAll guards against Canonical reporting a
// dangling link for a directory that a concurrent MkdirAll creates between
// the resolve and the Lstat.
func TestCanonicalConcurrentMkdirAll(t *testing.T) {
	root := t.TempDir()
	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers*65)
	for w := range workers {
		wg.Add(2)
		target := filepath.Join(root, "a", "b", "c", fmt.Sprintf("d%d", w%4), fmt.Sprintf("e%d", w))
		go func() {
			defer wg.Done()
			for range 64 {
				if _, err := Canonical(filepath.Join(target, "file")); err != nil {
					errs <- err
				}
			}
		}()
		go func() {
			defer wg.Done()
			if err := os.MkdirAll(target, 0o750); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		assert.NoError(t, err)
	}
}

func TestResolveExistingAppearingComponentIsRetried(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	eval := func(p string) (string, error) {
		calls++
		if calls == 1 {
			return "", fs.ErrNotExist // reported missing, yet Lstat finds it
		}
		return filepath.EvalSymlinks(p)
	}
	got, err := resolveExisting(dir, eval)
	require.NoError(t, err)
	want, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestResolveExistingRealDanglingLinkRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	dir := t.TempDir()
	link := filepath.Join(dir, "dangling")
	require.NoError(t, os.Symlink(filepath.Join(dir, "nowhere"), link))
	_, err := resolveExisting(link, filepath.EvalSymlinks)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dangling link")
}
