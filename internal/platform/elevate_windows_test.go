//go:build windows

package platform

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

// commandLineToArgs parses a full command line with the shell's own parser
// (CommandLineToArgvW), the one the elevated child's runtime mirrors.
func commandLineToArgs(t *testing.T, cmdline string) []string {
	t.Helper()
	p, err := windows.UTF16PtrFromString(cmdline)
	require.NoError(t, err)
	var argc int32
	argv, err := windows.CommandLineToArgv(p, &argc)
	require.NoError(t, err)
	defer func() { _, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(argv))) }() //nolint:gosec // G103: LocalFree takes the pointer CommandLineToArgv returned
	out := make([]string, argc)
	for i := range out {
		out[i] = windows.UTF16ToString(argv[i][:])
	}
	return out
}

func TestEscapeArgs(t *testing.T) {
	cases := [][]string{
		{"init", "--devdrive", "50GB"},
		{"init", "--path", `C:\Program Files\cf\cowflow.vhdx`},
		{`with "quotes"`, `"`, `""`},
		{`trailing\`, `trailing space\ `, `C:\dir with space\`, `\\server\share\`},
		{"", "empty-before", ""},
		{"tab\there", `back\\"slash`, `a\"b`},
		{"--letter", "X:"},
	}
	for _, args := range cases {
		// ShellExecuteEx builds "<file> <lpParameters>"; mimic that with a
		// quoted program name so only the parameters are under test.
		got := commandLineToArgs(t, `"C:\cf\cowflow.exe" `+elevateParameters(args))
		require.Equal(t, append([]string{`C:\cf\cowflow.exe`}, args...), got, "args %q", args)
	}
}

func TestShellExecuteInfoLayout(t *testing.T) {
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skipf("layout is specified for amd64 and arm64, not %s", runtime.GOARCH)
	}
	// sizeof(SHELLEXECUTEINFOW) on 64-bit Windows (shellapi.h, natural
	// alignment): 112, with hProcess last at offset 104.
	var info shellExecuteInfo
	require.EqualValues(t, 112, unsafe.Sizeof(info))
	require.EqualValues(t, 8, unsafe.Offsetof(info.hwnd))
	require.EqualValues(t, 48, unsafe.Offsetof(info.nShow))
	require.EqualValues(t, 56, unsafe.Offsetof(info.hInstApp))
	require.EqualValues(t, 88, unsafe.Offsetof(info.dwHotKey))
	require.EqualValues(t, 96, unsafe.Offsetof(info.hIconOrMon))
	require.EqualValues(t, 104, unsafe.Offsetof(info.hProcess))
}

func TestRunElevated_RejectsRelativeExe(t *testing.T) {
	_, err := RunElevated(context.Background(), "cowflow.exe", nil, "")
	require.Error(t, err)
}

func TestRunElevated_NeverElevatesTwice(t *testing.T) {
	orig := processElevated
	processElevated = func() bool { return true }
	t.Cleanup(func() { processElevated = orig })

	_, err := RunElevated(context.Background(), `C:\cf\cowflow.exe`, []string{"init"}, `C:\`)
	require.ErrorIs(t, err, errAlreadyElevated)
}

func TestRunElevated_CancelledBeforeStart(t *testing.T) {
	orig := processElevated
	processElevated = func() bool { return false }
	t.Cleanup(func() { processElevated = orig })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := RunElevated(ctx, `C:\cf\cowflow.exe`, nil, `C:\`)
	require.ErrorIs(t, err, context.Canceled)
}

func TestWaitExitCode(t *testing.T) {
	// The test binary with no matching test exits 0 right away.
	exe, err := os.Executable()
	require.NoError(t, err)
	cmd := exec.Command(exe, "-test.run=^$") //nolint:gosec // G204: re-executes the test binary itself
	require.NoError(t, cmd.Start())
	// os/exec keeps its own process handle until Wait, so the PID stays valid.
	h, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false, uint32(cmd.Process.Pid)) //nolint:gosec // G115: Windows PIDs are DWORDs
	require.NoError(t, err)
	defer func() { _ = windows.CloseHandle(h) }()

	code, err := waitExitCode(context.Background(), h)
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.NoError(t, cmd.Wait())
}

func TestWaitExitCode_ContextDone(t *testing.T) {
	// The current process never exits during the test, so the wait must
	// return the context error after one poll instead of blocking.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := waitExitCode(ctx, windows.CurrentProcess())
	require.ErrorIs(t, err, context.Canceled)
}
