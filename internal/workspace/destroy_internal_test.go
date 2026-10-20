package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStuckCodeClassification(t *testing.T) {
	t.Parallel()
	pe := func(errno syscall.Errno) error {
		return fmt.Errorf("remove: %w", &fs.PathError{Op: "unlinkat", Path: "/ws/a", Err: errno})
	}
	tests := []struct {
		goos string
		err  error
		code string
	}{
		{"windows", pe(32), "ERROR_SHARING_VIOLATION"},
		{"windows", pe(5), "ERROR_ACCESS_DENIED"},
		{"windows", pe(33), "ERROR_LOCK_VIOLATION"},
		{"windows", pe(145), "ERROR_DIR_NOT_EMPTY"},
		{"windows", pe(2), ""},   // ERROR_FILE_NOT_FOUND
		{"windows", pe(112), ""}, // ERROR_DISK_FULL
		{"linux", pe(syscall.EBUSY), "EBUSY"},
		{"darwin", pe(syscall.EBUSY), "EBUSY"},
		{"linux", pe(syscall.EACCES), ""},
		{"linux", pe(32), ""}, // EPIPE on Linux: the number means nothing there
		{"linux", errors.New("plain"), ""},
	}
	for _, tc := range tests {
		code, ok := stuckCode(tc.goos, tc.err)
		assert.Equal(t, tc.code, code, "%s %v", tc.goos, tc.err)
		assert.Equal(t, tc.code != "", ok)
	}

	d := &Destroyer{goos: "windows"}
	diag, ok := d.stuck(pe(32))
	require.True(t, ok)
	assert.Equal(t, "/ws/a", diag.path)
	assert.Contains(t, diag.String(), "close editors/terminals using this folder")
	_, ok = d.stuck(nil)
	assert.False(t, ok)
}
