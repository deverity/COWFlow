package platform

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// These tests drive the OS-independent Task Scheduler AutoAttacher with a
// fake script runner, so they run on every OS.

// aaFake records the scripts the AutoAttacher runs.
type aaFake struct {
	scripts []string
	runErr  error
	user    string
	exe     string
	wd      string
}

func (f *aaFake) attacher(target AutoAttachTarget) *taskAttacher {
	return &taskAttacher{target: target, ops: taskOps{
		run: func(_ context.Context, s string) (string, error) {
			f.scripts = append(f.scripts, s)
			return "", f.runErr
		},
		executable:  func() (string, error) { return f.exe, nil },
		getwd:       func() (string, error) { return f.wd, nil },
		currentUser: func() (string, error) { return f.user, nil },
	}}
}

// newAAFake returns a fake whose defaults are a real executable file and
// project directory under a temporary directory.
func newAAFake(t *testing.T, root string) (*aaFake, DevDriveSpec) {
	t.Helper()
	if root == "" {
		root = t.TempDir()
	}
	exe := filepath.Join(root, "bin", "cowflow.exe")
	wd := filepath.Join(root, "project")
	require.NoError(t, os.MkdirAll(filepath.Dir(exe), 0o750))
	require.NoError(t, os.MkdirAll(wd, 0o750))
	require.NoError(t, os.WriteFile(exe, []byte("exe"), 0o600))
	exe, err := Canonical(exe)
	require.NoError(t, err)
	wd, err = Canonical(wd)
	require.NoError(t, err)
	spec := DevDriveSpec{VHDXPath: filepath.Join(root, "cf", "cowflow.vhdx"), SizeBytes: 50 << 30, Mount: "X:"}
	return &aaFake{user: `CORP\alice`, exe: exe, wd: wd}, spec
}

// decoded returns every base64 literal of script, decoded.
func decoded(t *testing.T, script string) []string {
	t.Helper()
	var out []string
	for _, m := range regexp.MustCompile(`FromBase64String\('([^']*)'\)`).FindAllStringSubmatch(script, -1) {
		b, err := base64.StdEncoding.DecodeString(m[1])
		require.NoError(t, err)
		out = append(out, string(b))
	}
	return out
}

func TestAutoAttachTaskName(t *testing.T) {
	// First 12 hex digits of SHA-256("c:\cf\cowflow.vhdx").
	n := autoAttachTaskName(`C:\cf\cowflow.vhdx`)
	require.Equal(t, "devdrive-attach-7edde492b379", n)
	require.Equal(t, n, autoAttachTaskName(`c:\CF\COWFLOW.VHDX`), "case-insensitive")
	require.NotEqual(t, n, autoAttachTaskName(`C:\cf\other.vhdx`))
}

func TestAutoAttacher_Enable(t *testing.T) {
	f, spec := newAAFake(t, "")
	a := f.attacher(AutoAttachTarget{})
	require.NoError(t, a.Enable(context.Background(), spec))
	require.Len(t, f.scripts, 1)

	vhdx, err := canonicalVHDX(spec.VHDXPath)
	require.NoError(t, err)
	require.Equal(t, []string{autoAttachTaskName(vhdx), f.exe, f.wd, f.user}, decoded(t, f.scripts[0]))
	s := f.scripts[0]
	require.Contains(t, s, "-Argument 'devdrive attach'")
	require.Contains(t, s, "-AtLogOn -User $user")
	require.Contains(t, s, "-RunLevel Highest")
	require.Contains(t, s, `-TaskPath '\COWFlow\'`)
	require.Contains(t, s, "-Force")
}

func TestAutoAttacher_EnableTwice_SameScript(t *testing.T) {
	f, spec := newAAFake(t, "")
	a := f.attacher(AutoAttachTarget{})
	require.NoError(t, a.Enable(context.Background(), spec))
	require.NoError(t, a.Enable(context.Background(), spec))
	require.Len(t, f.scripts, 2)
	require.Equal(t, f.scripts[0], f.scripts[1], "re-registering replaces the same task")
}

func TestAutoAttacher_DisableAfterEnable(t *testing.T) {
	f, spec := newAAFake(t, "")
	a := f.attacher(AutoAttachTarget{})
	require.NoError(t, a.Enable(context.Background(), spec))
	require.NoError(t, a.Disable(context.Background(), spec.VHDXPath))
	require.NoError(t, a.Disable(context.Background(), spec.VHDXPath))
	require.Len(t, f.scripts, 3)
	require.Equal(t, decoded(t, f.scripts[0])[0], decoded(t, f.scripts[1])[0], "same task name")
	require.Contains(t, f.scripts[1], "-ErrorAction SilentlyContinue")
	require.Contains(t, f.scripts[1], "Unregister-ScheduledTask")
	require.Contains(t, f.scripts[1], "-Confirm:$false")
}

func TestAutoAttacher_ExplicitTarget(t *testing.T) {
	f, spec := newAAFake(t, "")
	other := t.TempDir()
	other, err := Canonical(other)
	require.NoError(t, err)
	a := f.attacher(AutoAttachTarget{WorkDir: other, User: `PC01\bob`})
	require.NoError(t, a.Enable(context.Background(), spec))
	got := decoded(t, f.scripts[0])
	require.Equal(t, []string{f.exe, other, `PC01\bob`}, got[1:])
}

func TestAutoAttacher_RunFailure(t *testing.T) {
	f, spec := newAAFake(t, "")
	f.runErr = os.ErrPermission
	a := f.attacher(AutoAttachTarget{})
	err := a.Enable(context.Background(), spec)
	require.ErrorIs(t, err, os.ErrPermission)
	require.ErrorContains(t, err, `\COWFlow\devdrive-attach-`)
	require.ErrorIs(t, a.Disable(context.Background(), spec.VHDXPath), os.ErrPermission)
}

func TestAutoAttacher_InvalidTarget(t *testing.T) {
	root := t.TempDir()
	for _, tt := range []struct {
		name   string
		mutate func(f *aaFake, s *DevDriveSpec)
		want   error
	}{
		{"relative vhdx", func(_ *aaFake, s *DevDriveSpec) { s.VHDXPath = "cowflow.vhdx" }, errInvalidSpec},
		{"not a vhdx", func(_ *aaFake, s *DevDriveSpec) { s.VHDXPath = filepath.Join(root, "x.txt") }, errInvalidSpec},
		{"relative exe", func(f *aaFake, _ *DevDriveSpec) { f.exe = "cowflow.exe" }, errInvalidAutoAttach},
		{"missing exe", func(f *aaFake, _ *DevDriveSpec) { f.exe = filepath.Join(root, "none.exe") }, errInvalidAutoAttach},
		{"exe is a directory", func(f *aaFake, _ *DevDriveSpec) { f.exe = f.wd }, errInvalidAutoAttach},
		{"relative workdir", func(f *aaFake, _ *DevDriveSpec) { f.wd = "project" }, errInvalidAutoAttach},
		{"workdir is a file", func(f *aaFake, _ *DevDriveSpec) { f.wd = f.exe }, errInvalidAutoAttach},
		{"control char in workdir", func(f *aaFake, _ *DevDriveSpec) { f.wd += "\r\nx" }, errInvalidAutoAttach},
		{"too long workdir", func(f *aaFake, _ *DevDriveSpec) { f.wd += strings.Repeat("a", maxTaskValue) }, errInvalidAutoAttach},
		{"user without domain", func(f *aaFake, _ *DevDriveSpec) { f.user = "alice" }, errInvalidAutoAttach},
		{"user with two backslashes", func(f *aaFake, _ *DevDriveSpec) { f.user = `A\B\c` }, errInvalidAutoAttach},
		{"user with quote", func(f *aaFake, _ *DevDriveSpec) { f.user = `CORP\a"b` }, errInvalidAutoAttach},
		{"user with semicolon", func(f *aaFake, _ *DevDriveSpec) { f.user = `CORP\a;b` }, errInvalidAutoAttach},
		{"user with newline", func(f *aaFake, _ *DevDriveSpec) { f.user = "CORP\\a\nb" }, errInvalidAutoAttach},
		{"empty user part", func(f *aaFake, _ *DevDriveSpec) { f.user = `CORP\` }, errInvalidAutoAttach},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, spec := newAAFake(t, "")
			tt.mutate(f, &spec)
			err := f.attacher(AutoAttachTarget{}).Enable(context.Background(), spec)
			require.ErrorIs(t, err, tt.want)
			require.Empty(t, f.scripts, "nothing runs on an invalid target")
		})
	}
}

func TestAutoAttacher_ResolveErrors(t *testing.T) {
	boom := errors.New("boom")
	_, spec := newAAFake(t, "")
	for name, ops := range map[string]func(*taskOps){
		"executable": func(o *taskOps) { o.executable = func() (string, error) { return "", boom } },
		"getwd":      func(o *taskOps) { o.getwd = func() (string, error) { return "", boom } },
		"user":       func(o *taskOps) { o.currentUser = func() (string, error) { return "", boom } },
	} {
		t.Run(name, func(t *testing.T) {
			f, _ := newAAFake(t, "")
			a := f.attacher(AutoAttachTarget{})
			ops(&a.ops)
			require.ErrorIs(t, a.Enable(context.Background(), spec), boom)
			require.Empty(t, f.scripts)
		})
	}
}

// TestAutoAttacher_HostilePaths registers a task whose project root, VHDX
// path and user carry PowerShell metacharacters; they reach the script only
// base64-encoded, never as source text.
func TestAutoAttacher_HostilePaths(t *testing.T) {
	const payload = `x'; Remove-Item C:\ -Recurse #$(Stop-Computer)` + "`" + `"`
	name := payload
	if runtime.GOOS == "windows" {
		// `"`, `\` and `:` cannot appear in a Windows file name; the rest
		// of the payload still exercises the quoting.
		name = strings.NewReplacer(`"`, "", `\`, "", `:`, "").Replace(payload)
	}
	root := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.MkdirAll(root, 0o750))
	f, spec := newAAFake(t, root)
	f.user = `CORP\o'brien$(Remove-Item)` + "`"
	spec.VHDXPath = filepath.Join(root, name+".vhdx")

	a := f.attacher(AutoAttachTarget{})
	require.NoError(t, a.Enable(context.Background(), spec))
	require.NoError(t, a.Disable(context.Background(), spec.VHDXPath))
	for _, s := range f.scripts {
		require.NotContains(t, s, "Remove-Item")
		require.NotContains(t, s, "Stop-Computer")
		require.NotContains(t, s, "o'brien")
		require.NotContains(t, s, "{{", "every placeholder is filled")
	}
	got := decoded(t, f.scripts[0])
	require.Equal(t, []string{f.exe, f.wd, f.user}, got[1:])
	require.Contains(t, f.wd, "Remove-Item")
}

func TestTaskScripts_Golden(t *testing.T) {
	reg, err := renderRegisterTaskScript("devdrive-attach-7edde492b379", AutoAttachTarget{
		Exe: `C:\Program Files\COWFlow\cowflow.exe`, WorkDir: `C:\src\app`, User: `CORP\alice`,
	})
	require.NoError(t, err)
	unreg, err := renderUnregisterTaskScript("devdrive-attach-7edde492b379")
	require.NoError(t, err)
	for golden, got := range map[string]string{
		"testdata/register_task_script.golden":   reg,
		"testdata/unregister_task_script.golden": unreg,
	} {
		if *updateGolden {
			require.NoError(t, os.WriteFile(golden, []byte(got), 0o600))
		}
		want, err := os.ReadFile(golden) //nolint:gosec // G304: fixed testdata paths
		require.NoError(t, err)
		require.Equal(t, string(want), got, golden)
	}
}

func TestAutoAttacher_Unsupported(t *testing.T) {
	a := unsupportedAutoAttacher{}
	require.ErrorIs(t, a.Enable(context.Background(), DevDriveSpec{}), ErrUnsupported)
	require.ErrorIs(t, a.Disable(context.Background(), "x"), ErrUnsupported)
}

func TestNewAutoAttacher_PerOS(t *testing.T) {
	a := NewAutoAttacher()
	if runtime.GOOS == "windows" {
		require.IsType(t, &taskAttacher{}, a)
		return
	}
	require.ErrorIs(t, a.Enable(context.Background(), DevDriveSpec{}), ErrUnsupported)
	require.ErrorIs(t, NewAutoAttacherFor(AutoAttachTarget{}).Disable(context.Background(), "x"), ErrUnsupported)
}
