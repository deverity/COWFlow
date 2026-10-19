//go:build windows

package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modShell32          = windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteExW = modShell32.NewProc("ShellExecuteExW")
)

const (
	seeMaskNoCloseProcess = 0x00000040 // SEE_MASK_NOCLOSEPROCESS: return hProcess
	seeMaskNoAsync        = 0x00000100 // SEE_MASK_NOASYNC: finish before returning (console callers)
	swShowNormal          = 1          // SW_SHOWNORMAL

	// elevatePollInterval bounds how long RunElevated takes to notice a
	// canceled context while it waits for the elevated child.
	elevatePollInterval = 100 * time.Millisecond
)

// errUACDeclined is the Win32 error ShellExecuteExW reports when the user
// declines the UAC prompt.
const errUACDeclined = windows.ERROR_CANCELLED //nolint:misspell // Win32 constant name

// errAlreadyElevated is returned when an elevated process asks to elevate
// again; RunElevated never relaunches itself in a loop.
var errAlreadyElevated = errors.New("platform: process is already elevated; refusing to elevate again")

// shellExecuteInfo mirrors SHELLEXECUTEINFOW (shellapi.h).
type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         windows.Handle
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     windows.Handle
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    windows.Handle
	dwHotKey     uint32
	hIconOrMon   windows.Handle // union { hIcon; hMonitor }
	hProcess     windows.Handle
}

// processElevated reports whether the current process token is elevated.
// It is a variable so tests can simulate an elevated caller.
var processElevated = func() bool { return windows.GetCurrentProcessToken().IsElevated() }

// RunElevated starts exe with args through the UAC "runas" verb (one
// prompt), waits for it and returns its exit code. dir is the child's
// working directory; "" means the caller's current directory (without it an
// elevated child would start in System32).
//
// Arguments are quoted with the standard Windows argv rules
// (syscall.EscapeArg); no shell is involved. exe must be an absolute path so
// no search path is consulted. A declined UAC prompt yields ErrNotElevated.
// An already elevated caller gets an error instead of a second elevation, so
// elevation can never loop.
//
// When ctx is done RunElevated stops waiting and returns ctx.Err(); the
// elevated child keeps running, because a non-elevated parent cannot be
// relied on to terminate it and interrupting it mid-operation would be worse.
func RunElevated(ctx context.Context, exe string, args []string, dir string) (int, error) {
	if !filepath.IsAbs(exe) {
		return -1, fmt.Errorf("platform: elevate: executable path %q is not absolute", exe)
	}
	if processElevated() {
		return -1, errAlreadyElevated
	}
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return -1, fmt.Errorf("platform: elevate: working directory: %w", err)
		}
		dir = wd
	}
	if err := ctx.Err(); err != nil {
		return -1, err
	}
	if err := procShellExecuteExW.Find(); err != nil {
		return -1, fmt.Errorf("platform: elevate: %w", err)
	}

	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return -1, err
	}
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return -1, fmt.Errorf("platform: elevate: %w", err)
	}
	params, err := windows.UTF16PtrFromString(elevateParameters(args))
	if err != nil {
		return -1, fmt.Errorf("platform: elevate: %w", err)
	}
	wd, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return -1, fmt.Errorf("platform: elevate: %w", err)
	}

	info := shellExecuteInfo{
		fMask:        seeMaskNoCloseProcess | seeMaskNoAsync,
		lpVerb:       verb,
		lpFile:       file,
		lpParameters: params,
		lpDirectory:  wd,
		nShow:        swShowNormal,
	}
	info.cbSize = uint32(unsafe.Sizeof(info))
	// ShellExecuteExW returns a BOOL and reports failure through
	// GetLastError, so the third result of Call is the right error here.
	r, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info))) //nolint:gosec // G103: Win32 ABI requires a pointer to the struct
	if r == 0 {
		if errors.Is(callErr, errUACDeclined) {
			return -1, fmt.Errorf("%w: UAC prompt declined", ErrNotElevated)
		}
		return -1, fmt.Errorf("platform: elevate: ShellExecuteExW: %w", callErr)
	}
	if info.hProcess == 0 {
		return -1, errors.New("platform: elevate: ShellExecuteExW returned no process handle")
	}
	defer func() { _ = windows.CloseHandle(info.hProcess) }()
	return waitExitCode(ctx, info.hProcess)
}

// waitExitCode waits for the process behind h to exit, honoring ctx, and
// returns its exit code.
func waitExitCode(ctx context.Context, h windows.Handle) (int, error) {
	timeout := uint32(elevatePollInterval / time.Millisecond)
	for {
		ev, err := windows.WaitForSingleObject(h, timeout)
		switch {
		case err != nil:
			return -1, fmt.Errorf("platform: elevate: wait: %w", err)
		case ev == windows.WAIT_OBJECT_0:
			var code uint32
			if err := windows.GetExitCodeProcess(h, &code); err != nil {
				return -1, fmt.Errorf("platform: elevate: exit code: %w", err)
			}
			return int(code), nil
		case ev == uint32(windows.WAIT_TIMEOUT):
			if err := ctx.Err(); err != nil {
				return -1, err
			}
		default:
			return -1, fmt.Errorf("platform: elevate: unexpected wait result %#x", ev)
		}
	}
}

// elevateParameters builds lpParameters: each argument quoted with the
// standard Windows argv rules and joined by single spaces.
func elevateParameters(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = syscall.EscapeArg(a)
	}
	return strings.Join(quoted, " ")
}
