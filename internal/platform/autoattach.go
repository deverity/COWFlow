package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Dev Drive auto-attach (SCOPE.md §6.4 step 5, §7): a VHDX attached for the
// lifetime of the boot is gone after a reboot, so `init --devdrive`
// registers a Windows Task Scheduler task that runs `cowflow devdrive
// attach` at logon of the user who ran init. The orchestration in this file
// is OS-independent so validation and script rendering are tested on every
// OS with a fake script runner; the real runner and user lookup are wired in
// autoattach_windows.go. Other OSes report ErrUnsupported
// (autoattach_other.go).

const (
	// autoAttachTaskFolder is the Task Scheduler folder of every COWFlow
	// task. The scripts carry it as fixed text.
	autoAttachTaskFolder = `\COWFlow\`

	// autoAttachTaskPrefix starts the name of a Dev Drive auto-attach
	// task; the rest is autoAttachHashLen hex digits of the VHDX path hash.
	autoAttachTaskPrefix = "devdrive-attach-"
	autoAttachHashLen    = 12

	// maxTaskValue bounds each value passed to the scheduled task.
	maxTaskValue = 1024
)

// errInvalidAutoAttach is wrapped by every AutoAttachTarget validation
// failure.
var errInvalidAutoAttach = errors.New("platform: auto-attach: invalid target")

// accountPart is one half of DOMAIN\user: no character Windows forbids in
// account or domain names, no control character.
const accountPart = `[^"/\\\[\]:;|=,+*?<>\x00-\x1f\x7f]{1,256}`

// accountName is the DOMAIN\user form of the logon user.
var accountName = regexp.MustCompile(`^` + accountPart + `\\` + accountPart + `$`)

// AutoAttachTarget is what the auto-attach task runs and for whom. An empty
// field is resolved when Enable runs: Exe to the running executable,
// WorkDir to the current directory and User to the user of the current
// process token.
type AutoAttachTarget struct {
	Exe     string // absolute path of cowflow.exe
	WorkDir string // project root: `devdrive attach` locates config.toml from it
	User    string // DOMAIN\user who ran init: logon trigger and task principal
}

// NewAutoAttacher returns the AutoAttacher for this OS with every
// AutoAttachTarget field resolved at Enable time. On Windows it registers
// a Task Scheduler task; other OSes report ErrUnsupported, because Dev
// Drive is Windows-only.
func NewAutoAttacher() AutoAttacher { return NewAutoAttacherFor(AutoAttachTarget{}) }

// NewAutoAttacherFor is NewAutoAttacher with an explicit target. Callers
// pass the project root and, when the registering process runs as another
// account than the user who ran init (an elevated child under different
// credentials), that user.
func NewAutoAttacherFor(t AutoAttachTarget) AutoAttacher { return newAutoAttacher(t) }

// taskOps are the OS primitives the scheduled-task AutoAttacher depends on.
// They are injected so tests can fake every one.
type taskOps struct {
	// run runs a fixed PowerShell script (runPowerShell outside tests).
	run func(ctx context.Context, script string) (string, error)
	// executable returns the path of the running executable.
	executable func() (string, error)
	// getwd returns the current directory.
	getwd func() (string, error)
	// currentUser returns DOMAIN\user of the current process token.
	currentUser func() (string, error)
}

// taskAttacher is the Task Scheduler AutoAttacher.
type taskAttacher struct {
	target AutoAttachTarget
	ops    taskOps
}

// autoAttachTaskName returns the task name for a canonical VHDX path:
// autoAttachTaskPrefix plus the first 12 hex digits of the SHA-256 of the
// lower-cased path, so one VHDX always maps to one task.
func autoAttachTaskName(vhdx string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(vhdx)))
	return autoAttachTaskPrefix + hex.EncodeToString(sum[:])[:autoAttachHashLen]
}

// Enable registers (or replaces) the auto-attach task of spec.VHDXPath.
// Registering an already registered task is a no-op apart from refreshing
// it.
func (a *taskAttacher) Enable(ctx context.Context, spec DevDriveSpec) error {
	vhdx, err := canonicalVHDX(spec.VHDXPath)
	if err != nil {
		return err
	}
	t, err := a.resolve()
	if err != nil {
		return err
	}
	name := autoAttachTaskName(vhdx)
	s, err := renderRegisterTaskScript(name, t)
	if err != nil {
		return err
	}
	if _, err := a.ops.run(ctx, s); err != nil {
		return fmt.Errorf("platform: auto-attach: register task %s%s: %w", autoAttachTaskFolder, name, err)
	}
	return nil
}

// Disable removes the auto-attach task of vhdxPath. Removing a task that
// does not exist is a no-op.
func (a *taskAttacher) Disable(ctx context.Context, vhdxPath string) error {
	vhdx, err := canonicalVHDX(vhdxPath)
	if err != nil {
		return err
	}
	name := autoAttachTaskName(vhdx)
	s, err := renderUnregisterTaskScript(name)
	if err != nil {
		return err
	}
	if _, err := a.ops.run(ctx, s); err != nil {
		return fmt.Errorf("platform: auto-attach: unregister task %s%s: %w", autoAttachTaskFolder, name, err)
	}
	return nil
}

// resolve fills the empty target fields and validates all of them.
func (a *taskAttacher) resolve() (AutoAttachTarget, error) {
	t := a.target
	var err error
	if t.Exe == "" {
		if t.Exe, err = a.ops.executable(); err != nil {
			return t, fmt.Errorf("platform: auto-attach: executable path: %w", err)
		}
	}
	if t.WorkDir == "" {
		if t.WorkDir, err = a.ops.getwd(); err != nil {
			return t, fmt.Errorf("platform: auto-attach: working directory: %w", err)
		}
	}
	if t.User == "" {
		if t.User, err = a.ops.currentUser(); err != nil {
			return t, fmt.Errorf("platform: auto-attach: current user: %w", err)
		}
	}
	return validateAutoAttachTarget(t)
}

// validateAutoAttachTarget checks t and returns it with canonical paths:
// Exe an existing regular file, WorkDir an existing directory, User in
// DOMAIN\user form. Every value is bounded and free of control characters.
func validateAutoAttachTarget(t AutoAttachTarget) (AutoAttachTarget, error) {
	for _, v := range []struct{ name, value string }{
		{"executable", t.Exe}, {"working directory", t.WorkDir}, {"user", t.User},
	} {
		if len(v.value) > maxTaskValue || strings.ContainsFunc(v.value, isControl) {
			return t, fmt.Errorf("%w: %s %q is too long or has control characters",
				errInvalidAutoAttach, v.name, truncate(v.value, 256))
		}
	}
	exe, err := existingPath(t.Exe, "executable", false)
	if err != nil {
		return t, err
	}
	dir, err := existingPath(t.WorkDir, "working directory", true)
	if err != nil {
		return t, err
	}
	if !accountName.MatchString(t.User) {
		return t, fmt.Errorf("%w: user %q is not in DOMAIN\\user form", errInvalidAutoAttach, t.User)
	}
	return AutoAttachTarget{Exe: exe, WorkDir: dir, User: t.User}, nil
}

// existingPath canonicalizes the absolute path p and checks that it is an
// existing directory (dir) or regular file (!dir).
func existingPath(p, what string, dir bool) (string, error) {
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("%w: %s %q is not absolute", errInvalidAutoAttach, what, p)
	}
	c, err := Canonical(p)
	if err != nil {
		return "", fmt.Errorf("%w: %s: %w", errInvalidAutoAttach, what, err)
	}
	fi, err := os.Stat(c)
	if err != nil {
		return "", fmt.Errorf("%w: %s: %w", errInvalidAutoAttach, what, err)
	}
	if dir && !fi.IsDir() {
		return "", fmt.Errorf("%w: %s %s is not a directory", errInvalidAutoAttach, what, c)
	}
	if !dir && !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%w: %s %s is not a regular file", errInvalidAutoAttach, what, c)
	}
	return c, nil
}

// isControl reports a C0 control character or DEL.
func isControl(r rune) bool { return r < 0x20 || r == 0x7f }

// unsupportedAutoAttacher is the AutoAttacher of every OS but Windows.
type unsupportedAutoAttacher struct{}

func (unsupportedAutoAttacher) Enable(context.Context, DevDriveSpec) error { return ErrUnsupported }

func (unsupportedAutoAttacher) Disable(context.Context, string) error { return ErrUnsupported }
