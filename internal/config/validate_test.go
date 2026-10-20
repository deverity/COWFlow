package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// valid returns Default() plus the minimal runner definition.
func valid() *Config {
	c := Default()
	c.Runner.Default = "claude-code"
	c.Runner.Defs = map[string]RunnerDef{"claude-code": {Cmd: []string{"claude", "-p"}, PromptMode: "arg"}}
	return c
}

// TestDefault_MatchesSpec22 pins U-API-11: decoding the SCOPE.md section 22
// mirror gives Default() plus the values section 22 only gives as examples.
func TestDefault_MatchesSpec22(t *testing.T) {
	got, err := Load(filepath.Join("testdata", "spec22.toml"))
	require.NoError(t, err)

	want := Default()
	// Derived or deployment-specific: no built-in default.
	assert.Empty(t, want.Daemon.StateDir)
	assert.False(t, want.DevDrive.Enabled)
	assert.Empty(t, want.DevDrive.Path)
	assert.Empty(t, want.DevDrive.Mount)
	assert.Zero(t, want.DevDrive.SizeGB)
	assert.Empty(t, want.Workspace.Root)
	assert.Empty(t, want.Workspace.GoldenRoot)
	assert.Equal(t, Cache{}, want.Cache)
	assert.Empty(t, want.Runner.Default)
	assert.Empty(t, want.Runner.Lead)
	// Examples only.
	assert.Empty(t, want.Gates)
	assert.Empty(t, want.Invariants)
	assert.Empty(t, want.Resources)
	assert.Empty(t, want.Runner.Defs)
	assert.Empty(t, want.Scheduler.LLM.Providers)

	want.Daemon.StateDir = got.Daemon.StateDir
	want.DevDrive.Enabled = got.DevDrive.Enabled
	want.DevDrive.Path = got.DevDrive.Path
	want.DevDrive.Mount = got.DevDrive.Mount
	want.DevDrive.SizeGB = got.DevDrive.SizeGB
	want.Workspace.Root = got.Workspace.Root
	want.Workspace.GoldenRoot = got.Workspace.GoldenRoot
	want.Cache = got.Cache
	want.Runner.Default = got.Runner.Default
	want.Runner.Lead = got.Runner.Lead
	want.Runner.Defs = got.Runner.Defs
	want.Scheduler.LLM.Providers = got.Scheduler.LLM.Providers
	want.Gates = got.Gates
	want.Invariants = got.Invariants
	want.Resources = got.Resources
	assert.Equal(t, want, got)
}

func TestDefault_Values(t *testing.T) {
	c := Default()
	assert.Equal(t, "127.0.0.1:8787", c.Daemon.Bind)
	assert.Equal(t, "gitflow", c.Project.Branching)
	assert.Equal(t, Escalation{High: 15, Normal: 60, Low: 0}, c.Collaboration.EscalationAfterMinutes)
	assert.Equal(t, []string{".env", ".env.*", "*.pem", "*.key", "id_rsa*"}, c.Security.BlockedPatterns)
	assert.True(t, c.Runner.InjectProtocol)
	assert.True(t, c.DevDrive.AutoAttach)
}

func TestDecode_ResourceQueueDefaultsToFifo(t *testing.T) {
	c, err := decode("[[resources]]\nname = \"a\"\nslots = 1\n")
	require.NoError(t, err)
	assert.Equal(t, "fifo", c.Resources[0].Queue)
}

func TestDecode_BlockedPatternsReplaceDefault(t *testing.T) {
	c, err := decode("[security]\nblocked_patterns = [\"*.p12\"]\n")
	require.NoError(t, err)
	assert.Equal(t, []string{"*.p12"}, c.Security.BlockedPatterns)
}

func TestWorkspace_EffectiveGoldenRoot(t *testing.T) {
	assert.Empty(t, Workspace{}.EffectiveGoldenRoot())
	assert.Equal(t, "X:/ws/golden", Workspace{Root: "X:/ws"}.EffectiveGoldenRoot())
	assert.Equal(t, "X:/ws/golden", Workspace{Root: "X:/ws/"}.EffectiveGoldenRoot())
	assert.Equal(t, "X:/golden", Workspace{Root: "X:/ws", GoldenRoot: "X:/golden"}.EffectiveGoldenRoot())
}

func TestValidate_Defaults(t *testing.T) {
	require.NoError(t, valid().Validate())
	require.NoError(t, Default().Validate())
}

func TestValidate_Rules(t *testing.T) {
	tests := []struct {
		name            string
		mutate          func(*Config)
		wantErrContains string
	}{
		{"provider unknown", func(c *Config) { c.Workspace.Provider = "ntfs" }, "workspace.provider"},
		{"provider empty", func(c *Config) { c.Workspace.Provider = "" }, "workspace.provider"},
		{"projfs without flag", func(c *Config) { c.Workspace.Provider = "projfs" }, "workspace.projfs.enabled"},
		{"lead unknown", func(c *Config) { c.Project.Lead = "robot" }, "project.lead"},
		{"lead agent without task", func(c *Config) { c.Project.Lead = "agent:" }, "project.lead"},
		{"branching empty", func(c *Config) { c.Project.Branching = "" }, "project.branching"},
		{"preset empty", func(c *Config) { c.Conventions.Preset = "" }, "conventions.preset"},
		{"bind empty", func(c *Config) { c.Daemon.Bind = "" }, "daemon.bind"},
		{"queue empty", func(c *Config) { c.Resources = []Resource{{Name: "a", Slots: 1}} }, "queue"},
		{"nil backoff", func(c *Config) { c.Scheduler.RetryBackoffSeconds = nil }, "retry_backoff_seconds"},
		{"promotion pr", func(c *Config) { c.Integration.PromotionMode = "pr" }, "not supported in v1"},
		{"promotion unknown", func(c *Config) { c.Integration.PromotionMode = "x" }, "integration.promotion_mode"},
		{"sync squash-merge", func(c *Config) { c.Git.DefaultSyncStrategy = "squash-merge" }, "git.default_sync_strategy"},
		{"prompt mode message", func(c *Config) {
			c.Runner.Defs["claude-code"] = RunnerDef{Cmd: []string{"x"}, PromptMode: "message"}
		}, "runner.claude-code.prompt_mode"},
		{"mount point form", func(c *Config) { c.DevDrive.Mount = `C:\cf\` }, "devdrive.mount"},
		{"mount letter C", func(c *Config) { c.DevDrive.Mount = "C:" }, "devdrive.mount"},
		{"branching unknown", func(c *Config) { c.Project.Branching = "svn" }, "project.branching"},
		{"preset unknown", func(c *Config) { c.Conventions.Preset = "git-flow" }, "conventions.preset"},
		{"resource zero slots", func(c *Config) { c.Resources = []Resource{{Name: "a", Slots: 0}} }, "slots must be greater than 0"},
		{"resource bad queue", func(c *Config) { c.Resources = []Resource{{Name: "a", Slots: 1, Queue: "lifo"}} }, "queue"},
		{"resource duplicate", func(c *Config) { c.Resources = []Resource{{Name: "a", Slots: 1}, {Name: "a", Slots: 1}} }, "duplicate name"},
		{"gate empty cmd", func(c *Config) { c.Gates = []Gate{{Name: "build"}} }, "cmd must be a non-empty array"},
		{"gate duplicate", func(c *Config) {
			c.Gates = []Gate{{Name: "a", Cmd: []string{"x"}}, {Name: "a", Cmd: []string{"x"}}}
		}, "duplicate name"},
		{"invariant bad kind", func(c *Config) { c.Invariants = []Invariant{{Name: "a", Kind: "regex"}} }, "must be pattern or cmd"},
		{"invariant duplicate", func(c *Config) {
			c.Invariants = []Invariant{{Name: "a", Kind: "cmd"}, {Name: "a", Kind: "pattern"}}
		}, "duplicate name"},
		{"runner empty cmd", func(c *Config) { c.Runner.Defs["claude-code"] = RunnerDef{} }, "runner.claude-code: cmd"},
		{"runner default undefined", func(c *Config) { c.Runner.Default = "codex" }, "runner.default"},
		{"runner lead undefined", func(c *Config) { c.Runner.Lead = "codex" }, "runner.lead"},
		{"empty backoff", func(c *Config) { c.Scheduler.RetryBackoffSeconds = []int{} }, "retry_backoff_seconds"},
		{"bad blocked pattern", func(c *Config) { c.Security.BlockedPatterns = []string{"[abc"} }, "security.blocked_patterns"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := valid()
			tt.mutate(c)
			err := c.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErrContains)
		})
	}
}

func TestValidate_AcceptsValidValues(t *testing.T) {
	c := valid()
	c.Project.Lead = "agent:T-42"
	c.Project.Branching = "trunk"
	c.Conventions.Preset = "none"
	c.Workspace.Provider = "projfs"
	c.Workspace.ProjFS.Enabled = true
	c.Scheduler.RetryBackoffSeconds = []int{30, 120}
	c.Security.BlockedPatterns = []string{"*.pem", ".env*"}
	c.Integration.PromotionMode = "auto"
	c.Git.DefaultSyncStrategy = "squash"
	c.DevDrive.Mount = "X:"
	c.Resources = []Resource{{Name: "compile", Slots: 2, Queue: "fifo"}}
	c.Runner.Lead = "claude-code"
	require.NoError(t, c.Validate())
}

func TestValidate_NonLoopbackBind(t *testing.T) {
	tests := []struct {
		bind string
		ok   bool
	}{
		{"127.0.0.1:8787", true},
		{"[::1]:8787", true},
		{"localhost:8787", true},
		{"0.0.0.0:8787", false},
		{"192.168.0.2:1", false},
		{"127.evil.example:1", false},
		{":8787", false},
		{"[::]:8787", false},
		{"no-port", false},
	}
	for _, tt := range tests {
		t.Run(tt.bind, func(t *testing.T) {
			c := valid()
			c.Daemon.Bind = tt.bind
			err := c.Validate()
			if tt.ok {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), "daemon.bind")
		})
	}
}

func TestValidate_AllErrorsReported(t *testing.T) {
	c := valid()
	c.Daemon.Bind = "0.0.0.0:8787"
	c.Project.Lead = "robot"
	c.Gates = []Gate{{Name: "build"}}
	err := c.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "daemon.bind")
	assert.Contains(t, err.Error(), "project.lead")
	assert.Contains(t, err.Error(), "gates[0]")
}

func TestLoad_AppliesDefaultsAndValidates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("[project]\nbranching = \"trunk\"\n"), 0o600))
	c, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "trunk", c.Project.Branching)
	assert.Equal(t, "human", c.Project.Lead)
	assert.Equal(t, "auto", c.Workspace.Provider)
	assert.Equal(t, "develop", c.Project.DefaultBase)
}

func TestLoad_ValidationFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("[daemon]\nbind = \"0.0.0.0:8787\"\n[project]\nlead = \"robot\"\n"), 0o600))
	c, err := Load(path)
	require.Error(t, err)
	assert.Nil(t, c)
	assert.Contains(t, err.Error(), "config "+path)
	assert.Contains(t, err.Error(), "daemon.bind")
	assert.Contains(t, err.Error(), "project.lead")
}

func TestLoad_ExplicitEmptyBackoffRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("[scheduler]\nretry_backoff_seconds = []\n"), 0o600))
	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "retry_backoff_seconds")
}
