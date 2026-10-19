package platform

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// maxScriptOutput bounds the stdout and stderr kept from one script run.
const maxScriptOutput = 2048

// powerShellPath returns the absolute path of Windows PowerShell under the
// system directory (%SystemRoot%\System32), so PATH is never searched and a
// planted powershell.exe is never run.
func powerShellPath() (string, error) {
	sys, err := windows.GetSystemDirectory()
	if err != nil {
		return "", fmt.Errorf("powershell: system directory: %w", err)
	}
	return filepath.Join(sys, "WindowsPowerShell", "v1.0", "powershell.exe"), nil
}

// runPowerShell runs a fixed script through -EncodedCommand (no shell, no
// string concatenation into a command line) and returns its stdout. On
// failure the error carries PowerShell's own stderr text, which is
// localized and therefore surfaced as is, never parsed.
func runPowerShell(ctx context.Context, script string) (string, error) {
	exe, err := powerShellPath()
	if err != nil {
		return "", err
	}
	//nolint:gosec // G204: exe is the fixed system PowerShell; the script is fixed text with encoded values
	cmd := exec.CommandContext(ctx, exe, "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass", "-EncodedCommand", encodeCommand(script))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("powershell: %w: %s", err, truncate(stderr.String(), maxScriptOutput))
	}
	return truncate(stdout.String(), maxScriptOutput), nil
}
