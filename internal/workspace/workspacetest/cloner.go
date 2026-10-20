package workspacetest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// FakeCloner is a workspace.Cloner that copies bytes with io.Copy. It exists
// for tests only: the real cow_clone primitive never byte-copies.
//
// It honors the rest of the clone contract: dst is created exclusively, the
// permission bits and modification time of src are preserved, and dst is
// removed on any error.
//
// Fault injection: the zero value never fails. With FailAfter = N > 0 the
// first N calls succeed and every later call fails with Err (default
// syscall.ENOSPC). With FailAfter = 0 and a non-nil Err, every call fails.
// A failing call removes the dst it may have created.
type FakeCloner struct {
	FailAfter int
	Err       error

	mu    sync.Mutex
	calls int
}

// CloneFile copies src to dst, or injects the configured fault.
func (c *FakeCloner) CloneFile(ctx context.Context, src, dst string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.fault(); err != nil {
		return fmt.Errorf("fake clone %s: %w", dst, err)
	}
	if err := copyFile(src, dst); err != nil {
		return fmt.Errorf("fake clone %s: %w", dst, err)
	}
	return nil
}

// Calls reports how many times CloneFile was called.
func (c *FakeCloner) Calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func (c *FakeCloner) fault() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	switch {
	case c.FailAfter > 0 && c.calls > c.FailAfter:
	case c.FailAfter == 0 && c.Err != nil:
	default:
		return nil
	}
	if c.Err != nil {
		return c.Err
	}
	return syscall.ENOSPC
}

func copyFile(src, dst string) (err error) {
	in, err := os.Open(filepath.Clean(src))
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }() // read-only; a close error loses nothing
	fi, err := in.Stat()
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", src)
	}
	out, err := os.OpenFile(filepath.Clean(dst), os.O_WRONLY|os.O_CREATE|os.O_EXCL, fi.Mode().Perm())
	if err != nil {
		return err // dst existed or could not be created: nothing of ours to remove
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.Remove(dst))
		}
	}()
	// Assign, never shadow, err below: the deferred cleanup reads it.
	if _, err = io.Copy(out, in); err != nil {
		_ = out.Close() // the copy error is what matters
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	if err = os.Chmod(dst, fi.Mode().Perm()); err != nil { // undo the umask
		return err
	}
	err = os.Chtimes(dst, fi.ModTime(), fi.ModTime())
	return err
}
