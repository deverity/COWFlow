package platform

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCurrentUser(t *testing.T) {
	u, err := currentUser()
	require.NoError(t, err)
	require.Regexp(t, accountName, u)
}

// TestAutoAttacher_Real registers, re-registers and removes a real
// scheduled task. It needs an elevated Windows machine and
// COWFLOW_TEST_DEVDRIVE=1, and is run manually.
func TestAutoAttacher_Real(t *testing.T) {
	if os.Getenv("COWFLOW_TEST_DEVDRIVE") != "1" {
		t.Skip("set COWFLOW_TEST_DEVDRIVE=1 on an elevated Windows machine")
	}
	ctx := context.Background()
	info, err := NewHost().OS(ctx)
	require.NoError(t, err)
	if !info.Elevated {
		t.Skip("needs an elevated process")
	}
	spec := DevDriveSpec{VHDXPath: filepath.Join(t.TempDir(), "autoattach-test.vhdx")}
	a := NewAutoAttacherFor(AutoAttachTarget{WorkDir: t.TempDir()})
	t.Cleanup(func() { _ = a.Disable(context.Background(), spec.VHDXPath) })

	vhdx, err := canonicalVHDX(spec.VHDXPath)
	require.NoError(t, err)
	exists := func() string {
		s, err := renderScript(psPrelude+`$name = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('{{.NameB64}}'))
@(Get-ScheduledTask -TaskPath '\COWFlow\' -TaskName $name -ErrorAction SilentlyContinue).Count
`, scriptArgs{NameB64: b64(autoAttachTaskName(vhdx))})
		require.NoError(t, err)
		out, err := runPowerShell(ctx, s)
		require.NoError(t, err)
		return out
	}

	require.NoError(t, a.Enable(ctx, spec))
	require.NoError(t, a.Enable(ctx, spec), "idempotent")
	n, err := parseScriptInt(exists())
	require.NoError(t, err)
	require.Equal(t, 1, n)

	require.NoError(t, a.Disable(ctx, spec.VHDXPath))
	require.NoError(t, a.Disable(ctx, spec.VHDXPath), "idempotent")
	n, err = parseScriptInt(exists())
	require.NoError(t, err)
	require.Equal(t, 0, n)
}
