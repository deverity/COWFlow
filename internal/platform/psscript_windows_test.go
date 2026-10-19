package platform

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPowerShellPath(t *testing.T) {
	p, err := powerShellPath()
	require.NoError(t, err)
	require.True(t, filepath.IsAbs(p))
	require.True(t, strings.EqualFold(filepath.Join(os.Getenv("SystemRoot"), "System32",
		"WindowsPowerShell", "v1.0", "powershell.exe"), p), p)
}

func TestRunPowerShell(t *testing.T) {
	exe, err := powerShellPath()
	require.NoError(t, err)
	if _, err := os.Stat(exe); err != nil {
		t.Skipf("powershell.exe not available: %v", err)
	}
	ctx := context.Background()

	out, err := runPowerShell(ctx, psPrelude+"Write-Output 42\n")
	require.NoError(t, err)
	n, err := parseScriptInt(out)
	require.NoError(t, err)
	require.Equal(t, 42, n)

	// A decoded base64 value round-trips intact, quotes included.
	const tricky = `C:\cf\it's "quoted" é.vhdx`
	s, err := renderScript(psPrelude+
		"[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('{{.PathB64}}')).Length\n",
		scriptArgs{PathB64: b64(tricky)})
	require.NoError(t, err)
	out, err = runPowerShell(ctx, s)
	require.NoError(t, err)
	n, err = parseScriptInt(out)
	require.NoError(t, err)
	require.Equal(t, len([]rune(tricky)), n)

	// A terminating error fails the run and carries PowerShell's message.
	_, err = runPowerShell(ctx, psPrelude+"throw 'cowflow-test-failure'\n")
	require.ErrorContains(t, err, "cowflow-test-failure")
}

func TestPSBackend_ReportsBothCauses(t *testing.T) {
	run := func(context.Context, string) (string, error) {
		return "", os.ErrPermission
	}
	b := psBackend{run: run, cause: errVirtDiskUnavailable}
	err := b.create(context.Background(), `C:\cf\cowflow.vhdx`, 50<<30)
	require.ErrorIs(t, err, errVirtDiskUnavailable)
	require.ErrorIs(t, err, os.ErrPermission)

	b.run = func(context.Context, string) (string, error) { return "-1\r\n", nil }
	_, attached, err := b.disk(context.Background(), `C:\cf\cowflow.vhdx`)
	require.NoError(t, err)
	require.False(t, attached)

	b.run = func(context.Context, string) (string, error) { return "5\r\n", nil }
	n, attached, err := b.disk(context.Background(), `C:\cf\cowflow.vhdx`)
	require.NoError(t, err)
	require.True(t, attached)
	require.Equal(t, 5, n)
}
