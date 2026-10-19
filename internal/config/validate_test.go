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

func TestDefault_SpecStatedOnly(t *testing.T) {
	c := Default()
	assert.Equal(t, "auto", c.Workspace.Provider)
	assert.Equal(t, "human", c.Project.Lead)
	assert.Equal(t, "develop", c.Project.DefaultBase)
	assert.Equal(t, "merge", c.Git.DefaultSyncStrategy)
	assert.Equal(t, "manual", c.Integration.PromotionMode)
	assert.False(t, c.Workspace.ProjFS.Enabled)
	assert.False(t, c.Watchdog.Enabled)
	assert.False(t, c.Integration.PostPromotion.Enabled)

	// Everything else waits for U-API-11 / C12.
	assert.Empty(t, c.Daemon.Bind)
	assert.Empty(t, c.Project.Branching)
	assert.Nil(t, c.Scheduler.RetryBackoffSeconds)
	assert.Nil(t, c.Security.BlockedPatterns)
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

// TestValidate_PendingGaps lists every rule blocked by an undecided SPEC-GAPS
// item. They are skipped until the maintainer decides the gap.
func TestValidate_PendingGaps(t *testing.T) {
	rows := []struct {
		id     string
		mutate func(*Config)
	}{
		{"C3", func(c *Config) { c.Integration.PromotionMode = "pr" }},
		{"C4", func(c *Config) { c.Git.DefaultSyncStrategy = "squash-merge" }},
		{"C10", func(c *Config) { c.Runner.Defs["claude-code"] = RunnerDef{Cmd: []string{"x"}, PromptMode: "message"} }},
		{"C12", func(*Config) {}},
	}
	for _, row := range rows {
		t.Run(row.id, func(t *testing.T) {
			t.Skip("PENDING " + row.id)
			c := valid()
			row.mutate(c)
			assert.Error(t, c.Validate())
		})
	}
}

func TestValidate_PendingRulesAreNotEnforced(t *testing.T) {
	c := valid()
	c.Integration.PromotionMode = "anything"
	c.Git.DefaultSyncStrategy = "anything"
	c.Runner.Defs["claude-code"] = RunnerDef{Cmd: []string{"x"}, PromptMode: "message"}
	require.NoError(t, c.Validate())

	pending := map[string]bool{}
	for _, ck := range checks {
		if ck.pending != "" {
			pending[ck.pending] = true
		}
	}
	assert.Equal(t, map[string]bool{"C3": true, "C4": true, "C10": true}, pending)
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
