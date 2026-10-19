package platform

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

// junction creates a directory junction link -> target with PowerShell,
// passing every value as its own argv element (test-only helper).
// powershell.exe joins the arguments after -Command into one script, so the
// paths are passed as single-quoted literals.
func junction(t *testing.T, link, target string) {
	t.Helper()
	ps, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Skipf("powershell.exe not available to create a junction: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	//nolint:gosec // G204: test-only; fixed program, every value is its own argv element (no shell string).
	cmd := exec.CommandContext(ctx, ps, "-NoProfile", "-NonInteractive", "-Command",
		"New-Item", "-ItemType", "Junction", "-Path", psQuote(link), "-Target", psQuote(target))
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "create junction: %s", out)
}

// psQuote returns s as a PowerShell single-quoted string literal.
func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// shortName returns the 8.3 short form of an existing path, or skips the test
// when short names are disabled on the volume.
func shortName(t *testing.T, p string) string {
	t.Helper()
	long, err := windows.UTF16PtrFromString(p)
	require.NoError(t, err)
	var buf [windows.MAX_LONG_PATH]uint16
	n, err := windows.GetShortPathName(long, &buf[0], uint32(len(buf)))
	require.NoError(t, err)
	short := windows.UTF16ToString(buf[:n])
	if strings.EqualFold(short, p) {
		t.Skipf("8.3 short names are disabled on the volume of %s", p)
	}
	return short
}

func TestConfine_Windows(t *testing.T) {
	base := tree(t)
	ws := filepath.Join(base, "ws")
	outside := filepath.Join(base, "outside")

	t.Run("case difference", func(t *testing.T) {
		got, err := Confine(strings.ToUpper(ws), strings.ToLower(filepath.Join(ws, "a", "sub", "file.txt")))
		require.NoError(t, err)
		assert.True(t, strings.EqualFold(filepath.Join(ws, "a", "sub", "file.txt"), got), got)

		_, err = Confine(strings.ToUpper(filepath.Join(ws, "a")), strings.ToLower(filepath.Join(ws, "ab")))
		require.ErrorIs(t, err, ErrOutsideRoot)
	})

	t.Run("junction escaping the root", func(t *testing.T) {
		link := filepath.Join(ws, "junction")
		junction(t, link, outside)

		_, err := Confine(ws, filepath.Join(link, "secret.txt"))
		require.ErrorIs(t, err, ErrOutsideRoot)
		_, err = Confine(ws, filepath.Join(link, "new.txt"))
		require.ErrorIs(t, err, ErrOutsideRoot)
		_, err = Confine(ws, link)
		require.ErrorIs(t, err, ErrOutsideRoot)
	})

	t.Run("long-path prefixed input", func(t *testing.T) {
		got, err := Confine(ws, `\\?\`+filepath.Join(ws, "a", "sub"))
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(ws, "a", "sub"), got)

		got, err = Confine(`\\?\`+ws, filepath.Join(ws, "a"))
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(ws, "a"), got)

		_, err = Confine(ws, `\\?\`+filepath.Join(outside, "secret.txt"))
		require.ErrorIs(t, err, ErrOutsideRoot)
	})

	t.Run("8.3 short name", func(t *testing.T) {
		longDir := filepath.Join(ws, "a-directory-with-a-long-name")
		require.NoError(t, os.MkdirAll(longDir, 0o750))
		short := shortName(t, longDir)

		got, err := Confine(ws, filepath.Join(short, "new.txt"))
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(longDir, "new.txt"), got)

		got, err = Confine(short, filepath.Join(longDir, "x"))
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(longDir, "x"), got)
	})

	t.Run("path longer than MAX_PATH", func(t *testing.T) {
		deep := ws
		for len(deep) <= 300 {
			deep = filepath.Join(deep, strings.Repeat("d", 40))
		}
		require.NoError(t, os.MkdirAll(deep, 0o750))

		got, err := Confine(ws, filepath.Join(deep, "file.txt"))
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(deep, "file.txt"), got)
	})
}
