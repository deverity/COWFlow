package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/config"
	"github.com/deverity/cowflow/internal/platform"
	"github.com/deverity/cowflow/test/harness"
)

// win11 is an elevated Windows 11 23H2 host.
var win11 = platform.OSInfo{GOOS: "windows", Build: 22631, Elevated: true}

// ddEnv is a Dev Drive test environment: simulated VHDX, fake host and a
// config path in a temp dir.
type ddEnv struct {
	t        *testing.T
	host     *harness.FakeHost
	dd       *harness.SimDevDrive
	cfgPath  string
	vhdx     string
	elevated [][]string // args of each Elevate call
	elevCode int
	elevErr  error
}

func newDDEnv(t *testing.T, info platform.OSInfo) *ddEnv {
	t.Helper()
	dir := t.TempDir()
	host := &harness.FakeHost{OSInfo: info}
	vhdx, err := platform.Canonical(filepath.Join(dir, "cf", "cowflow.vhdx"))
	require.NoError(t, err)
	return &ddEnv{
		t:       t,
		host:    host,
		dd:      &harness.SimDevDrive{Host: host},
		cfgPath: filepath.Join(dir, "state", "config.toml"),
		vhdx:    vhdx,
	}
}

func (e *ddEnv) deps() Deps {
	return Deps{
		Version:    "dev",
		ConfigPath: func() (string, error) { return e.cfgPath, nil },
		Host:       e.host,
		DevDrive:   e.dd,
		Elevate: func(_ context.Context, args []string) (int, error) {
			e.elevated = append(e.elevated, append([]string(nil), args...))
			return e.elevCode, e.elevErr
		},
	}
}

func (e *ddEnv) run(args ...string) result {
	e.t.Helper()
	var out, errb bytes.Buffer
	ios := IOStreams{In: strings.NewReader(""), Out: &out, Err: &errb}
	code := Execute(context.Background(), args, ios, e.deps())
	return result{code: code, stdout: out.String(), stderr: errb.String()}
}

func (e *ddEnv) initArgs(extra ...string) []string {
	return append([]string{"init", "--devdrive", "50GB", "--path", e.vhdx, "--letter", "x"}, extra...)
}

func (e *ddEnv) provisionCalls() int {
	n := 0
	for _, c := range e.dd.Calls {
		if strings.HasPrefix(c, "provision:") {
			n++
		}
	}
	return n
}

func TestInitDevDrive_HappyPath(t *testing.T) {
	e := newDDEnv(t, win11)
	r := e.run(e.initArgs()...)
	require.Equal(t, ExitOK, r.code, r.stderr)

	cfg, err := config.Load(e.cfgPath)
	require.NoError(t, err)
	assert.Equal(t, config.DevDrive{Enabled: true, Path: e.vhdx, Mount: "X:", SizeGB: 50, AutoAttach: false}, cfg.DevDrive)

	assert.Contains(t, r.stdout, `Add-MpPreference -ExclusionPath 'X:\'`)
	assert.Contains(t, r.stdout, autoAttachWarning)
	assert.Contains(t, r.stdout, "Config written: "+e.cfgPath)
	assert.Contains(t, r.stdout, "ReFS")
	assert.Empty(t, e.elevated)
	assert.NoFileExists(t, e.cfgPath+".bak", "no previous config to back up")
}

func TestInitDevDrive_JSON(t *testing.T) {
	e := newDDEnv(t, win11)
	r := e.run(e.initArgs("--json")...)
	require.Equal(t, ExitOK, r.code, r.stderr)

	var doc struct {
		DevDrive struct {
			VHDXPath string `json:"vhdx_path"`
			Attached bool   `json:"attached"`
			Mount    string `json:"mount"`
			Volume   struct {
				Filesystem string `json:"filesystem"`
				DevDrive   bool   `json:"dev_drive"`
			} `json:"volume"`
		} `json:"devdrive"`
		ConfigWritten string   `json:"config_written"`
		Warnings      []string `json:"warnings"`
	}
	require.NoError(t, json.Unmarshal([]byte(r.stdout), &doc), r.stdout)
	assert.Equal(t, e.vhdx, doc.DevDrive.VHDXPath)
	assert.True(t, doc.DevDrive.Attached)
	assert.Equal(t, "X:", doc.DevDrive.Mount)
	assert.Equal(t, "ReFS", doc.DevDrive.Volume.Filesystem)
	assert.True(t, doc.DevDrive.Volume.DevDrive)
	assert.Equal(t, e.cfgPath, doc.ConfigWritten)
	assert.Contains(t, doc.Warnings, autoAttachWarning)
}

func TestInitDevDrive_KeepsPreviousConfigAsBackup(t *testing.T) {
	e := newDDEnv(t, win11)
	require.NoError(t, os.MkdirAll(filepath.Dir(e.cfgPath), 0o750))
	prev := config.Default()
	prev.Project.Lead = "agent:t1"
	require.NoError(t, config.Save(e.cfgPath, prev))
	before, err := os.ReadFile(e.cfgPath)
	require.NoError(t, err)

	r := e.run(e.initArgs()...)
	require.Equal(t, ExitOK, r.code, r.stderr)

	bak, err := os.ReadFile(e.cfgPath + ".bak")
	require.NoError(t, err)
	assert.Equal(t, before, bak)
	cfg, err := config.Load(e.cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "agent:t1", cfg.Project.Lead, "existing settings are kept")
	assert.True(t, cfg.DevDrive.Enabled)
}

func TestInitDevDrive_ElevatesOnce(t *testing.T) {
	notElevated := win11
	notElevated.Elevated = false
	e := newDDEnv(t, notElevated)
	args := e.initArgs()

	r := e.run(args...)
	require.Equal(t, ExitOK, r.code, r.stderr)
	require.Len(t, e.elevated, 1)
	assert.Equal(t, args, e.elevated[0])
	assert.Contains(t, r.stderr, "Requesting administrator rights (one UAC prompt)")
	assert.Zero(t, e.provisionCalls(), "the parent never provisions; the elevated child does")
	assert.Contains(t, r.stdout, "Dev Drive "+e.vhdx, "the parent prints the final status")

	// The elevated child performs the work and never elevates again.
	child := newDDEnv(t, win11)
	r = child.run(child.initArgs()...)
	require.Equal(t, ExitOK, r.code, r.stderr)
	assert.Empty(t, child.elevated)
	assert.Positive(t, child.provisionCalls())
}

func TestInitDevDrive_ElevationFailures(t *testing.T) {
	notElevated := win11
	notElevated.Elevated = false
	tests := []struct {
		name string
		code int
		err  error
		want int
	}{
		{name: "UAC declined", code: -1, err: platform.ErrNotElevated, want: exitNotElevated},
		{name: "child failed", code: ExitConflict, want: ExitConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newDDEnv(t, notElevated)
			e.elevCode, e.elevErr = tt.code, tt.err
			r := e.run(e.initArgs()...)
			assert.Equal(t, tt.want, r.code, r.stderr)
			assert.Len(t, e.elevated, 1)
			assert.NoFileExists(t, e.cfgPath)
		})
	}
}

func TestInitDevDrive_ProvisionFails_NoConfigWritten(t *testing.T) {
	e := newDDEnv(t, win11)
	require.NoError(t, os.MkdirAll(filepath.Dir(e.cfgPath), 0o750))
	require.NoError(t, config.Save(e.cfgPath, config.Default()))
	before, err := os.ReadFile(e.cfgPath)
	require.NoError(t, err)

	e.dd.FailAt = harness.StepFormat
	r := e.run(e.initArgs()...)
	assert.NotEqual(t, ExitOK, r.code)
	assert.Contains(t, r.stderr, "format")

	after, err := os.ReadFile(e.cfgPath)
	require.NoError(t, err)
	assert.Equal(t, before, after, "config unchanged")
	assert.NoFileExists(t, e.cfgPath+".bak")
}

func TestInitDevDrive_NotWindows(t *testing.T) {
	e := newDDEnv(t, platform.OSInfo{GOOS: "linux", Elevated: true})
	r := e.run(e.initArgs()...)
	assert.Equal(t, exitUnsupported, r.code)
	assert.Contains(t, r.stderr, fallbackMessage)
	assert.Empty(t, e.dd.Calls, "no provisioning call")
	assert.Empty(t, e.elevated)
	assert.NoFileExists(t, e.cfgPath)
}

func TestInitDevDrive_OldBuild(t *testing.T) {
	e := newDDEnv(t, platform.OSInfo{GOOS: "windows", Build: 19045, Elevated: false})
	r := e.run(e.initArgs()...)
	assert.Equal(t, exitUnsupported, r.code)
	assert.Contains(t, r.stderr, fallbackMessage)
	assert.Contains(t, r.stderr, "19045")
	assert.Empty(t, e.dd.Calls, "no provisioning call")
	assert.Empty(t, e.elevated, "no UAC prompt on an unsupported build")
}

func TestInitDevDrive_AlreadyExists(t *testing.T) {
	e := newDDEnv(t, win11)
	r := e.run(e.initArgs()...)
	require.Equal(t, ExitOK, r.code, r.stderr)

	r = e.run(e.initArgs()...)
	assert.Equal(t, ExitConflict, r.code, r.stderr)
}

func TestInitDevDrive_UsageErrors(t *testing.T) {
	e := newDDEnv(t, win11)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "bad size", args: []string{"init", "--devdrive", "50", "--path", e.vhdx, "--letter", "X"}, want: "invalid size"},
		{name: "too small", args: []string{"init", "--devdrive", "40GB", "--path", e.vhdx, "--letter", "X"}, want: "minimum"},
		{name: "missing path", args: []string{"init", "--devdrive", "50GB", "--letter", "X"}, want: "U-PLAT-9"},
		{name: "missing letter", args: []string{"init", "--devdrive", "50GB", "--path", e.vhdx}, want: "U-PLAT-9"},
		{name: "system letter", args: []string{"init", "--devdrive", "50GB", "--path", e.vhdx, "--letter", "C"}, want: "--letter"},
		{name: "letter with colon", args: []string{"init", "--devdrive", "50GB", "--path", e.vhdx, "--letter", "X:"}, want: "--letter"},
		{name: "not a vhdx", args: []string{"init", "--devdrive", "50GB", "--path", e.vhdx + ".txt", "--letter", "X"}, want: ".vhdx"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := e.run(tt.args...)
			assert.Equal(t, ExitUsage, r.code, r.stderr)
			assert.Contains(t, r.stderr, tt.want)
		})
	}
	assert.Empty(t, e.dd.Calls)
	assert.NoFileExists(t, e.cfgPath)
}

func TestInit_WithoutDevDriveIsStub(t *testing.T) {
	e := newDDEnv(t, win11)
	for _, args := range [][]string{
		{"init"},
		{"init", "--provider", "auto"},
		append(e.initArgs(), "--conventions", "gitflow"),
	} {
		r := e.run(args...)
		assert.Equal(t, exitNotImplemented, r.code, args)
		assert.Contains(t, r.stderr, errNotImplemented.Error())
	}
	assert.Empty(t, e.dd.Calls)
}

func TestInitDevDrive_ConfigLocationUndecided(t *testing.T) {
	e := newDDEnv(t, win11)
	deps := e.deps()
	deps.ConfigPath = config.Locate
	var out, errb bytes.Buffer
	code := Execute(context.Background(), e.initArgs(), IOStreams{Out: &out, Err: &errb}, deps)
	assert.Equal(t, ExitState, code)
	assert.Contains(t, errb.String(), "U-API-1")
	assert.Empty(t, e.dd.Calls, "nothing is provisioned without a config location")
}

type fakeAttacher struct {
	err   error
	specs []platform.DevDriveSpec
}

func (a *fakeAttacher) Enable(_ context.Context, spec platform.DevDriveSpec) error {
	a.specs = append(a.specs, spec)
	return a.err
}

func (a *fakeAttacher) Disable(context.Context, string) error { return nil }

func TestInitDevDrive_AutoAttachAndLayout(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantAuto bool
	}{
		{name: "enabled", wantAuto: true},
		{name: "undecided", err: errors.New("auto-attach undecided (U-PLAT-5)")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newDDEnv(t, win11)
			ws := filepath.Join(t.TempDir(), "ws")
			aa := &fakeAttacher{err: tt.err}
			var out bytes.Buffer
			in := devDriveInit{
				ios:      IOStreams{Out: &out, Err: &out},
				deps:     e.deps(),
				st:       &runState{},
				layout:   func(string) map[string]string { return map[string]string{"workspace.root": ws} },
				attacher: aa,
			}
			require.NoError(t, in.run(context.Background(),
				initDevDriveOpts{size: "50GB", path: e.vhdx, letter: "X"}))

			require.Len(t, aa.specs, 1)
			assert.Equal(t, "X:", aa.specs[0].Mount)
			assert.DirExists(t, ws)
			cfg, err := config.Load(e.cfgPath)
			require.NoError(t, err)
			assert.Equal(t, ws, cfg.Workspace.Root)
			assert.Equal(t, tt.wantAuto, cfg.DevDrive.AutoAttach)
			if tt.wantAuto {
				assert.NotContains(t, out.String(), autoAttachWarning)
			} else {
				assert.Contains(t, out.String(), autoAttachWarning)
				assert.Contains(t, out.String(), "U-PLAT-5")
			}
		})
	}
}

func TestDevDriveLayout_PendingIsEmpty(t *testing.T) {
	assert.Empty(t, devdriveLayout("X:"), "PENDING U-API-11, C7, C13")
}

func TestApplyLayout_UnknownKey(t *testing.T) {
	require.Error(t, applyLayout(config.Default(), map[string]string{"daemon.state_dir": "X:/state"}))
}

// initialized provisions the Dev Drive through init and returns the env.
func initialized(t *testing.T) *ddEnv {
	t.Helper()
	e := newDDEnv(t, win11)
	r := e.run(e.initArgs()...)
	require.Equal(t, ExitOK, r.code, r.stderr)
	return e
}

func TestDevDriveStatus_JSON(t *testing.T) {
	e := initialized(t)
	e.host.OSInfo.Elevated = false // status needs no elevation

	r := e.run("devdrive", "status", "--json")
	require.Equal(t, ExitOK, r.code, r.stderr)
	assert.Empty(t, e.elevated)

	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(r.stdout), &doc), r.stdout)
	assert.Equal(t, true, doc["attached"])
	assert.Equal(t, "X:", doc["mount"])
	vol, ok := doc["volume"].(map[string]any)
	require.True(t, ok, r.stdout)
	assert.Equal(t, "ReFS", vol["filesystem"])
}

func TestDevDrive_DetachAttach(t *testing.T) {
	e := initialized(t)

	r := e.run("devdrive", "detach", "--json")
	require.Equal(t, ExitOK, r.code, r.stderr)
	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(r.stdout), &doc))
	assert.Equal(t, false, doc["attached"])
	assert.Nil(t, doc["volume"])

	r = e.run("devdrive", "attach")
	require.Equal(t, ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "attached: yes")
	assert.Contains(t, r.stdout, "ReFS")
	assert.Empty(t, e.elevated)
}

func TestDevDrive_ElevatesForAdminCommands(t *testing.T) {
	for _, sub := range []string{"create", "attach", "detach"} {
		t.Run(sub, func(t *testing.T) {
			e := initialized(t)
			e.host.OSInfo.Elevated = false
			before := len(e.dd.Calls)

			r := e.run("devdrive", sub)
			require.Equal(t, ExitOK, r.code, r.stderr)
			require.Len(t, e.elevated, 1)
			assert.Equal(t, []string{"devdrive", sub}, e.elevated[0])
			assert.Len(t, e.dd.Calls, before, "the parent only reads the status")
			assert.Contains(t, r.stdout, "Dev Drive "+e.vhdx)
		})
	}
}

func TestDevDriveCreate_FromConfig(t *testing.T) {
	e := newDDEnv(t, win11)
	require.NoError(t, os.MkdirAll(filepath.Dir(e.cfgPath), 0o750))
	cfg := config.Default()
	cfg.DevDrive = config.DevDrive{Path: e.vhdx, Mount: "Y:", SizeGB: 64}
	require.NoError(t, config.Save(e.cfgPath, cfg))

	r := e.run("devdrive", "create")
	require.Equal(t, ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "mount:    Y:")

	r = e.run("devdrive", "create")
	assert.Equal(t, ExitConflict, r.code, r.stderr)
}

func TestDevDrive_NotConfigured(t *testing.T) {
	e := newDDEnv(t, win11)
	require.NoError(t, os.MkdirAll(filepath.Dir(e.cfgPath), 0o750))
	require.NoError(t, config.Save(e.cfgPath, config.Default()))

	r := e.run("devdrive", "status")
	assert.Equal(t, ExitState, r.code)
	assert.Contains(t, r.stderr, "init --devdrive")
	assert.Empty(t, e.dd.Calls)
}

func TestDevDrive_NotWindows(t *testing.T) {
	e := newDDEnv(t, platform.OSInfo{GOOS: "darwin"})
	r := e.run("devdrive", "status")
	assert.Equal(t, exitUnsupported, r.code)
	assert.Contains(t, r.stderr, fallbackMessage)
}

func TestDevDriveErr(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{platform.ErrAlreadyExists, ExitConflict},
		{platform.ErrNotElevated, exitNotElevated},
		{platform.ErrOSTooOld, exitUnsupported},
		{platform.ErrUnsupported, exitUnsupported},
		{usageErr(errors.New("x")), ExitUsage},
		{errors.New("other"), exitUnclassified},
	}
	for _, tt := range tests {
		t.Run(tt.err.Error(), func(t *testing.T) {
			assert.Equal(t, tt.want, codeOf(devDriveErr(errors.Join(errors.New("ctx"), tt.err)), true))
		})
	}
	assert.NoError(t, devDriveErr(nil))
}
