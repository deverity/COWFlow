package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/platform"
	"github.com/deverity/cowflow/test/harness"
)

const win11Build = 22631

// doctorEnvT is a hermetic doctor environment. The working directory is the
// harness repository.
type doctorEnvT struct {
	t      *testing.T
	repo   *harness.Repo
	host   *harness.FakeHost
	dd     *harness.SimDevDrive
	runner *harness.FaultyRunner
	cfg    string // config.toml content; "" means no file
	vhdx   string

	defender func(context.Context) ([]string, error) // Deps.DefenderExclusions
}

func newDoctorEnv(t *testing.T, opts ...harness.RepoOption) *doctorEnvT {
	t.Helper()
	repo := harness.NewRepo(t, opts...)
	t.Chdir(repo.Dir)
	host := &harness.FakeHost{
		OSInfo:  platform.OSInfo{GOOS: "windows", Build: win11Build},
		Volumes: map[string]platform.VolumeInfo{filepath.ToSlash(repo.Dir): {Filesystem: "NTFS"}},
	}
	return &doctorEnvT{
		t:      t,
		repo:   repo,
		host:   host,
		dd:     &harness.SimDevDrive{Host: host},
		runner: &harness.FaultyRunner{Inner: repo.Runner},
		vhdx:   filepath.ToSlash(filepath.Join(t.TempDir(), "cowflow.vhdx")),
	}
}

// devDriveConfig configures a Dev Drive at X: with the workspace root on it.
func (e *doctorEnvT) devDriveConfig(extra string) {
	e.cfg = fmt.Sprintf("[devdrive]\nenabled = true\npath = %q\nmount = \"X:\"\nsize_gb = 50\n\n"+
		"[workspace]\nroot = \"X:/ws\"\n%s", e.vhdx, extra)
}

func (e *doctorEnvT) spec() platform.DevDriveSpec {
	return platform.DevDriveSpec{VHDXPath: e.vhdx, SizeBytes: 50 << 30, Mount: "X:"}
}

func (e *doctorEnvT) provision() {
	e.t.Helper()
	_, err := e.dd.Provision(context.Background(), e.spec())
	require.NoError(e.t, err)
}

type doctorRun struct {
	code   int
	stdout string
	stderr string
	report DoctorReport
}

func (r doctorRun) check(t *testing.T, name string) CheckResult {
	t.Helper()
	for _, c := range r.report.Checks {
		if c.Name == name {
			return c
		}
	}
	require.Failf(t, "check missing", "no check %q in %+v", name, r.report.Checks)
	return CheckResult{}
}

func (r doctorRun) has(name string) bool {
	for _, c := range r.report.Checks {
		if c.Name == name {
			return true
		}
	}
	return false
}

func (e *doctorEnvT) run(args ...string) doctorRun {
	e.t.Helper()
	cfgPath := filepath.Join(e.t.TempDir(), "config.toml")
	if e.cfg != "" {
		require.NoError(e.t, os.WriteFile(cfgPath, []byte(e.cfg), 0o600))
	}
	deps := Deps{
		Version:    "dev",
		ConfigPath: func() (string, error) { return cfgPath, nil },
		Host:       e.host,
		DevDrive:   e.dd,
		Git:        e.runner,

		DefenderExclusions: e.defender,
	}
	var out, errb bytes.Buffer
	ios := IOStreams{In: strings.NewReader(""), Out: &out, Err: &errb}
	code := Execute(context.Background(), append([]string{"doctor", "--json"}, args...), ios, deps)
	r := doctorRun{code: code, stdout: out.String(), stderr: errb.String()}
	require.NoError(e.t, json.Unmarshal(out.Bytes(), &r.report), "stdout: %s stderr: %s", r.stdout, r.stderr)
	return r
}

func TestDoctor_NTFSNoCOW(t *testing.T) {
	e := newDoctorEnv(t)
	e.host.Volumes = map[string]platform.VolumeInfo{"X:": {Path: `X:\`, Filesystem: "NTFS"}}
	e.cfg = "[workspace]\nroot = \"X:/ws\"\n"

	r := e.run()
	c := r.check(t, "fs.cow")
	assert.Equal(t, StatusWarn, c.Status)
	assert.Contains(t, c.Detail, "no COW")
	assert.Equal(t, ExitOK, r.code, "warn never fails the command")
}

func TestDoctor_ReFSDevDrive(t *testing.T) {
	e := newDoctorEnv(t)
	e.provision()
	e.devDriveConfig("")

	r := e.run()
	assert.Equal(t, StatusOK, r.check(t, "fs.cow").Status)
	assert.Contains(t, r.check(t, "fs.cow").Detail, "copy-on-write: block clone")
	assert.Equal(t, StatusOK, r.check(t, "devdrive.mounted").Status)
	assert.Equal(t, StatusOK, r.check(t, "os.version").Status)
	assert.Equal(t, ExitOK, r.code)
	assert.True(t, r.report.OK)
}

func TestDoctor_ReFSWithoutBlockCloneIsNotCOW(t *testing.T) {
	e := newDoctorEnv(t)
	e.host.Volumes = map[string]platform.VolumeInfo{"X:": {Filesystem: "ReFS"}}
	e.cfg = "[workspace]\nroot = \"X:/ws\"\n"

	c := e.run().check(t, "fs.cow")
	assert.Equal(t, StatusWarn, c.Status)
	assert.Contains(t, c.Detail, "no COW")
}

func TestDoctor_CWDVolumeWhenNoWorkspaceRoot(t *testing.T) {
	e := newDoctorEnv(t)
	cwd, err := os.Getwd()
	require.NoError(t, err)
	e.host.Volumes = map[string]platform.VolumeInfo{
		filepath.ToSlash(cwd): {Filesystem: "btrfs", BlockClone: true},
	}

	c := e.run().check(t, "fs.cow")
	assert.Equal(t, StatusOK, c.Status)
	assert.Contains(t, c.Detail, "btrfs")
}

func TestDoctor_ConfiguredButUnmounted(t *testing.T) {
	e := newDoctorEnv(t)
	e.provision()
	e.dd.SimulateReboot()
	e.devDriveConfig("")

	r := e.run()
	c := r.check(t, "devdrive.mounted")
	assert.Equal(t, StatusFail, c.Status)
	assert.Equal(t, "cowflow devdrive attach", c.Fix)
	assert.NotContains(t, strings.ToLower(c.Detail+c.Fix), "ntfs")
	assert.False(t, r.report.OK)
	assert.Equal(t, exitDoctorFailed, r.code)
	assert.Contains(t, r.stderr, "check(s) failed")
}

func TestDoctor_ConfiguredButMissingVHDX(t *testing.T) {
	e := newDoctorEnv(t)
	e.devDriveConfig("")

	c := e.run().check(t, "devdrive.mounted")
	assert.Equal(t, StatusFail, c.Status)
	assert.Equal(t, "cowflow devdrive create", c.Fix)
}

func TestDoctor_NoDevDriveOffersInit(t *testing.T) {
	e := newDoctorEnv(t)

	c := e.run().check(t, "devdrive.mounted")
	assert.Equal(t, StatusWarn, c.Status)
	assert.Contains(t, c.Fix, "cowflow init --devdrive")
}

func TestDoctor_OldWindowsBuild(t *testing.T) {
	e := newDoctorEnv(t)
	e.host.OSInfo.Build = 19045

	r := e.run()
	c := r.check(t, "os.version")
	assert.Equal(t, StatusWarn, c.Status)
	assert.Contains(t, c.Detail, "git_worktree fallback")
	d := r.check(t, "devdrive.mounted")
	assert.Equal(t, StatusWarn, d.Status)
	assert.Empty(t, d.Fix, "init --devdrive cannot work on this build")
}

func TestDoctor_NonWindowsSkipsWindowsChecks(t *testing.T) {
	e := newDoctorEnv(t)
	e.host.OSInfo = platform.OSInfo{GOOS: "linux"}

	r := e.run()
	assert.Equal(t, StatusSkip, r.check(t, "devdrive.mounted").Status)
	assert.Equal(t, StatusSkip, r.check(t, "git.longpaths").Status)
	assert.Equal(t, StatusOK, r.check(t, "os.version").Status)
}

func TestDoctor_GitTooOld(t *testing.T) {
	e := newDoctorEnv(t)
	e.runner.Inject(harness.GitFault{Match: []string{"version"}, Stdout: "git version 2.39.0\n"})

	r := e.run()
	c := r.check(t, "git.version")
	assert.Equal(t, StatusFail, c.Status)
	assert.Contains(t, c.Detail, "2.39.0")
	assert.Equal(t, exitDoctorFailed, r.code)
	assert.True(t, r.has("fs.cow"), "other checks still run")
}

func TestDoctor_GitVersionOK(t *testing.T) {
	e := newDoctorEnv(t)
	e.runner.Inject(harness.GitFault{Match: []string{"version"}, Stdout: "git version 2.47.1.windows.2\n"})

	c := e.run().check(t, "git.version")
	assert.Equal(t, StatusOK, c.Status)
}

func TestDoctor_GitMissing(t *testing.T) {
	e := newDoctorEnv(t)
	e.runner.Inner = nil // every pass-through call fails
	e.runner.Inject(harness.GitFault{Match: []string{"version"}, ExitCode: 127})

	r := e.run()
	assert.Equal(t, StatusFail, r.check(t, "git.version").Status)
	assert.Equal(t, StatusSkip, r.check(t, "git.autocrlf").Status, "repo checks skip, not fail")
}

func TestDoctor_RepoSettings(t *testing.T) {
	t.Run("all good", func(t *testing.T) {
		e := newDoctorEnv(t)
		_, err := e.repo.Runner.Run(context.Background(), e.repo.Dir, "config", "core.longpaths", "true")
		require.NoError(t, err)
		e.cfg = "[git]\nrequire_gitattributes = true\n"

		r := e.run()
		assert.Equal(t, StatusOK, r.check(t, "git.longpaths").Status)
		assert.Equal(t, StatusOK, r.check(t, "git.autocrlf").Status)
		assert.Equal(t, StatusOK, r.check(t, "git.gitattributes").Status)
	})

	t.Run("missing", func(t *testing.T) {
		e := newDoctorEnv(t, harness.WithAttributes(""))
		_, err := e.repo.Runner.Run(context.Background(), e.repo.Dir, "config", "--unset", "core.autocrlf")
		require.NoError(t, err)
		e.cfg = "[git]\nrequire_gitattributes = true\n"

		r := e.run()
		lp := r.check(t, "git.longpaths")
		assert.Equal(t, StatusWarn, lp.Status)
		assert.Equal(t, "git config core.longpaths true", lp.Fix)
		ac := r.check(t, "git.autocrlf")
		assert.Equal(t, StatusWarn, ac.Status)
		assert.Equal(t, "git config core.autocrlf false", ac.Fix)
		assert.Equal(t, StatusWarn, r.check(t, "git.gitattributes").Status)
		assert.Equal(t, ExitOK, r.code, "warnings never fail")
	})

	t.Run("wrong values", func(t *testing.T) {
		e := newDoctorEnv(t, harness.WithAttributes("* text=auto\n"))
		for _, kv := range [][]string{{"core.longpaths", "false"}, {"core.autocrlf", "true"}} {
			_, err := e.repo.Runner.Run(context.Background(), e.repo.Dir, "config", kv[0], kv[1])
			require.NoError(t, err)
		}
		e.cfg = "[git]\nrequire_gitattributes = true\n"

		r := e.run()
		assert.Equal(t, StatusWarn, r.check(t, "git.longpaths").Status)
		assert.Equal(t, StatusWarn, r.check(t, "git.autocrlf").Status)
		assert.Equal(t, StatusWarn, r.check(t, "git.gitattributes").Status)
	})

	t.Run("gitattributes not required", func(t *testing.T) {
		e := newDoctorEnv(t, harness.WithAttributes(""))
		e.cfg = "[git]\nrequire_gitattributes = false\n" // the default is true
		assert.Equal(t, StatusSkip, e.run().check(t, "git.gitattributes").Status)
	})
}

func TestDoctor_NotARepository(t *testing.T) {
	e := newDoctorEnv(t)
	t.Chdir(t.TempDir())
	e.host.Volumes = map[string]platform.VolumeInfo{"X:": {Filesystem: "NTFS"}}
	e.cfg = "[git]\nrequire_gitattributes = true\n\n[workspace]\nroot = \"X:/ws\"\n"
	e.runner.Inject(harness.GitFault{Match: []string{"rev-parse"}, ExitCode: 128})

	r := e.run()
	for _, name := range []string{"git.longpaths", "git.autocrlf", "git.gitattributes"} {
		c := r.check(t, name)
		assert.Equal(t, StatusSkip, c.Status, name)
		assert.Equal(t, "not a Git repository", c.Detail, name)
	}
	assert.Equal(t, ExitOK, r.code, "outside a repository doctor still passes")
}

func TestDoctor_ProjFSOnlyWhenConfigured(t *testing.T) {
	t.Run("not configured", func(t *testing.T) {
		e := newDoctorEnv(t)
		assert.False(t, e.run().has("projfs.feature"))
	})
	t.Run("provider projfs and missing", func(t *testing.T) {
		e := newDoctorEnv(t)
		e.cfg = "[workspace]\nprovider = \"projfs\"\n\n[workspace.projfs]\nenabled = true\n"
		c := e.run().check(t, "projfs.feature")
		assert.Equal(t, StatusFail, c.Status)
	})
	t.Run("provider projfs and available", func(t *testing.T) {
		e := newDoctorEnv(t)
		e.host.ProjFS = true
		e.cfg = "[workspace]\nprovider = \"projfs\"\n\n[workspace.projfs]\nenabled = true\n"
		assert.Equal(t, StatusOK, e.run().check(t, "projfs.feature").Status)
	})
	t.Run("experimental flag only", func(t *testing.T) {
		e := newDoctorEnv(t)
		e.cfg = "[workspace.projfs]\nenabled = true\n"
		assert.Equal(t, StatusWarn, e.run().check(t, "projfs.feature").Status)
	})
}

func TestDoctor_DefenderExclusion(t *testing.T) {
	excluded := func(paths ...string) func(context.Context) ([]string, error) {
		return func(context.Context) ([]string, error) { return paths, nil }
	}
	tests := []struct {
		name     string
		elevated bool
		devDrive bool
		goos     string
		probe    func(context.Context) ([]string, error)
		want     CheckStatus
		wantFix  string
	}{
		{name: "excluded", elevated: true, devDrive: true, probe: excluded(`C:\src`, `x:\`), want: StatusOK},
		{name: "excluded without slash", elevated: true, devDrive: true, probe: excluded("X:"), want: StatusOK},
		{name: "not excluded", elevated: true, devDrive: true, probe: excluded(`C:\src`, `X:\ws`), want: StatusWarn,
			wantFix: `Add-MpPreference -ExclusionPath 'X:\'`},
		{name: "not elevated", devDrive: true, probe: excluded(`X:\`), want: StatusSkip},
		{name: "defender inactive", elevated: true, devDrive: true, want: StatusSkip,
			probe: func(context.Context) ([]string, error) { return nil, errors.New("Defender is not active") }},
		{name: "no probe", elevated: true, devDrive: true, want: StatusSkip},
		{name: "no dev drive", elevated: true, probe: excluded(`X:\`), want: StatusSkip},
		{name: "not windows", elevated: true, devDrive: true, goos: "linux", probe: excluded(`X:\`), want: StatusSkip},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newDoctorEnv(t)
			e.host.OSInfo.Elevated = tt.elevated
			if tt.goos != "" {
				e.host.OSInfo = platform.OSInfo{GOOS: tt.goos, Elevated: tt.elevated}
			}
			if tt.devDrive {
				e.provision()
				e.devDriveConfig("")
			}
			e.defender = tt.probe

			r := e.run()
			c := r.check(t, "defender.exclusion")
			assert.Equal(t, tt.want, c.Status, c.Detail)
			assert.Equal(t, tt.wantFix, c.Fix)
			assert.NotEqual(t, StatusFail, c.Status, "never fail (C14)")
		})
	}
}

func TestDoctor_ConfigMissingUsesDefaults(t *testing.T) {
	e := newDoctorEnv(t)

	c := e.run().check(t, "config.found")
	assert.Equal(t, StatusWarn, c.Status)
	assert.Equal(t, "cowflow init", c.Fix)
}

func TestDoctor_ConfigLocationUndecided(t *testing.T) {
	e := newDoctorEnv(t)
	var out bytes.Buffer
	deps := Deps{
		ConfigPath: func() (string, error) { return "", errors.New("location undecided") },
		Host:       e.host, DevDrive: e.dd, Git: e.runner,
	}
	code := Execute(context.Background(), []string{"doctor", "--json"}, IOStreams{In: strings.NewReader(""), Out: &out, Err: &bytes.Buffer{}}, deps)
	assert.Equal(t, ExitOK, code)
	var rep DoctorReport
	require.NoError(t, json.Unmarshal(out.Bytes(), &rep))
	assert.Equal(t, StatusWarn, rep.Checks[0].Status)
	assert.Contains(t, rep.Checks[0].Detail, "location undecided")
}

func TestDoctor_InvalidConfigFails(t *testing.T) {
	e := newDoctorEnv(t)
	e.cfg = "[nope]\nx = 1\n"

	r := e.run()
	assert.Equal(t, StatusFail, r.check(t, "config.found").Status)
	assert.Equal(t, exitDoctorFailed, r.code)
	assert.True(t, r.has("fs.cow"), "other checks still run on defaults")
}

func TestDoctor_ProbeTimeoutDoesNotHideOthers(t *testing.T) {
	old := doctorCheckTimeout
	doctorCheckTimeout = 200 * time.Millisecond
	t.Cleanup(func() { doctorCheckTimeout = old })

	e := newDoctorEnv(t)
	e.runner.Inject(harness.GitFault{Match: []string{"version"}, Timeout: true})

	r := e.run()
	assert.Equal(t, StatusFail, r.check(t, "git.version").Status)
	assert.Equal(t, StatusOK, r.check(t, "os.version").Status)
	assert.True(t, r.has("git.autocrlf"))
}

func TestDoctor_DoesNotModifyTheSystem(t *testing.T) {
	e := newDoctorEnv(t)
	e.provision()
	e.devDriveConfig("")
	callsBefore := append([]string(nil), e.dd.Calls...)

	e.run()

	assert.Equal(t, callsBefore, e.dd.Calls, "doctor only reads Dev Drive status")
	for _, argv := range e.runner.Recorded() {
		assert.Contains(t, [][]string{{"version"}, {"rev-parse", "--show-toplevel"}, {"config", "--get", "core.longpaths"}, {"config", "--get", "core.autocrlf"}},
			argv, "unexpected git call")
	}
}

func TestDoctor_JSON(t *testing.T) {
	e := newDoctorEnv(t)
	e.provision()
	e.devDriveConfig("")

	r := e.run()
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(r.stdout), &raw))
	assert.Len(t, raw, 2)
	assert.JSONEq(t, "true", string(raw["ok"]))

	var checks []map[string]any
	require.NoError(t, json.Unmarshal(raw["checks"], &checks))
	for _, c := range checks {
		assert.Contains(t, c, "name")
		assert.Contains(t, c, "status")
		assert.Contains(t, c, "detail")
		assert.Contains(t, []any{"ok", "warn", "fail", "skip"}, c["status"])
	}

	e.runner.Inject(harness.GitFault{Match: []string{"version"}, Stdout: "git version 2.39.0\n"})
	r = e.run()
	assert.False(t, r.report.OK)
	assert.Equal(t, exitDoctorFailed, r.code)
}

func TestDoctor_HumanTable(t *testing.T) {
	e := newDoctorEnv(t)
	e.cfg = ""
	e.runner.Inject(harness.GitFault{Match: []string{"version"}, Stdout: "git version 2.47.1\n"})
	cfgPath := filepath.Join(t.TempDir(), "absent.toml")
	deps := Deps{ConfigPath: func() (string, error) { return cfgPath, nil }, Host: e.host, DevDrive: e.dd, Git: e.runner}
	var out bytes.Buffer
	code := Execute(context.Background(), []string{"doctor"}, IOStreams{In: strings.NewReader(""), Out: &out, Err: &bytes.Buffer{}}, deps)

	assert.Equal(t, ExitOK, code)
	lines := strings.Split(out.String(), "\n")
	assert.Regexp(t, `^CHECK\s+STATUS\s+DETAIL$`, lines[0])
	assert.Regexp(t, `(?m)^config\.found\s+warn\s+no config at`, out.String())
	assert.Regexp(t, `(?m)^\s+fix: cowflow init$`, out.String())
}

func TestDoctor_RejectsArguments(t *testing.T) {
	r := run(t, "doctor", "extra")
	assert.Equal(t, ExitUsage, r.code)
}
