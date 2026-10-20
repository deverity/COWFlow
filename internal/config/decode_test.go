package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readFixture(t testing.TB, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name)) //nolint:gosec // G304: fixed testdata directory
	require.NoError(t, err)
	return string(data)
}

func TestDecode_FullSpecExample(t *testing.T) {
	c, err := Load(filepath.Join("testdata", "spec22.toml"))
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1:8787", c.Daemon.Bind)
	assert.Equal(t, "X:/state", c.Daemon.StateDir)
	assert.Equal(t, "human", c.Project.Lead)
	assert.Equal(t, 50, c.DevDrive.SizeGB)
	assert.True(t, c.DevDrive.AutoAttach)
	assert.Equal(t, "X:/golden", c.Workspace.GoldenRoot)
	assert.Equal(t, 40000, c.Workspace.PortRangeStart)
	assert.Equal(t, 512, c.Workspace.ProjFS.BlobCacheMB)
	assert.False(t, c.Workspace.ProjFS.Enabled)
	assert.Equal(t, "X:/cache/cargo-target", c.Cache.CargoTarget)
	assert.Equal(t, "X:/cache/gradle", c.Cache.GradleHome)
	assert.Equal(t, "X:/cache/pip", c.Cache.PipCache)
	assert.Equal(t, "merge", c.Git.DefaultSyncStrategy)
	assert.Equal(t, 1, c.Scheduler.MaxIntegrationsPerTarget)
	assert.Equal(t, []int{30, 120}, c.Scheduler.RetryBackoffSeconds)
	assert.Equal(t, 8, c.Scheduler.LLM.MaxConcurrentRequests)
	assert.Equal(t, int64(4000000), c.Scheduler.LLM.TokensPerHourBudget)
	assert.InDelta(t, 150, c.Scheduler.LLM.CostPerDayBudgetUSD, 0)
	assert.Equal(t, "pause", c.Scheduler.LLM.OnBudgetExceeded)
	require.Contains(t, c.Scheduler.LLM.Providers, "openai")
	assert.Equal(t, 6, c.Scheduler.LLM.Providers["openai"].MaxConcurrentRequests)
	assert.Equal(t, 5, c.Scheduler.LLM.Providers["anthropic"].MaxConcurrentRequests)
	assert.Equal(t, int64(2000000), c.Scheduler.LLM.Providers["anthropic"].TokensPerHourBudget)
	assert.Equal(t, Escalation{High: 15, Normal: 60, Low: 0}, c.Collaboration.EscalationAfterMinutes)
	assert.Equal(t, "manual", c.Integration.PromotionMode)
	assert.Equal(t, []string{"test"}, c.Integration.PostPromotion.Gates)
	require.Len(t, c.Gates, 3)
	assert.Equal(t, []string{"go", "test", "./...", "-race"}, c.Gates[1].Cmd)
	assert.False(t, c.Gates[2].Required)
	require.Len(t, c.Invariants, 1)
	assert.Equal(t, "pattern", c.Invariants[0].Kind)
	assert.Equal(t, []string{`internal/**: import "github.com/deverity/cowflow/cmd`}, c.Invariants[0].Deny)
	assert.True(t, c.Invariants[0].CheckOnPublish)
	require.Len(t, c.Resources, 3)
	assert.Equal(t, "priority", c.Resources[2].Queue)
	assert.Equal(t, "gitflow", c.Conventions.Preset)
	assert.Equal(t, "v{major}.{minor}.{patch}", c.Conventions.TagFormat)
	assert.Equal(t, "claude-code", c.Runner.Default)
	assert.True(t, c.Runner.InjectProtocol)
	assert.Equal(t, "claude-code", c.Runner.Lead)
	require.Contains(t, c.Runner.Defs, "codex")
	assert.Equal(t, "codex", c.Runner.Defs["codex"].Cmd[0])
	assert.False(t, c.Watchdog.Enabled)
	assert.Equal(t, int64(200000), c.Watchdog.BudgetTokensPerHour)
	assert.Contains(t, c.Security.BlockedPatterns, "*.pem")
}

func TestDecode_RunnersFromSection15_3(t *testing.T) {
	c, err := decode(readFixture(t, "runners.toml"))
	require.NoError(t, err)

	require.Len(t, c.Runner.Defs, 5)
	assert.Equal(t, "arg", c.Runner.Defs["aider"].PromptMode)
	assert.Equal(t, []string{"aider", "--yes", "--message"}, c.Runner.Defs["aider"].Cmd)
	assert.Equal(t, []string{"--yolo"}, c.Runner.Defs["gemini"].PermissionFlag)
	assert.Equal(t, []string{"{cmd}"}, c.Runner.Defs["generic"].Cmd)
	assert.Equal(t, []string{"--mcp-config", "{mcp_config}"}, c.Runner.Defs["claude-code"].MCPConfigFlag)
}

func TestDecode_UnknownKeyFails(t *testing.T) {
	tests := []struct {
		name string
		toml string
		want string
	}{
		{"scheduler typo", "[scheduler]\nmax_agent = 3\n", `config: unknown key "scheduler.max_agent"`},
		{"runner def typo", "[runner.codex]\ncmd = [\"codex\"]\nprompt_mod = \"arg\"\n", `config: unknown key "runner.codex.prompt_mod"`},
		{"llm provider typo", "[scheduler.llm.openai]\nmax_concurent_requests = 1\n", `config: unknown key "scheduler.llm.openai.max_concurent_requests"`},
		{"unknown runner scalar", "[runner]\ndefalt = \"x\"\n", `config: unknown key "runner.defalt"`},
		{"unknown llm scalar", "[scheduler.llm]\non_budget = \"pause\"\n", `config: unknown key "scheduler.llm.on_budget"`},
		{"unknown section", "[bogus]\nx = 1\n", `config: unknown key "bogus`},
		{"gate typo", "[[gates]]\nname = \"a\"\ncommand = [\"x\"]\n", `config: unknown key "gates.command"`},
		{"nested projfs", "[workspace.projfs]\nenabld = true\n", `config: unknown key "workspace.projfs.enabld"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := decode(tt.toml)
			require.Error(t, err)
			assert.Nil(t, c)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestDecode_ReportsEveryUnknownKey(t *testing.T) {
	_, err := decode("[scheduler]\nmax_agent = 3\n[runner]\nbad = 1\n[runner.codex]\nnope = 1\n")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"scheduler.max_agent"`)
	assert.Contains(t, err.Error(), `"runner.bad"`)
	assert.Contains(t, err.Error(), `"runner.codex.nope"`)
}

func TestDecode_PrimitiveKeysAreNotUndecoded(t *testing.T) {
	// Every key of the full example goes through PrimitiveDecode for the
	// mixed tables; none of them may be reported as unknown.
	_, err := decode(readFixture(t, "spec22.toml"))
	require.NoError(t, err)
}

func TestDecode_MixedTables(t *testing.T) {
	c, err := decode(`
[runner]
default = "claude-code"
inject_protocol = true
lead = "claude-code"

[runner.claude-code]
provider = "anthropic"
cmd = ["claude", "-p"]

[scheduler.llm]
max_concurrent_requests = 8
on_budget_exceeded = "pause"

[scheduler.llm.anthropic]
cost_per_day_budget_usd = 80
`)
	require.NoError(t, err)
	assert.Equal(t, "claude-code", c.Runner.Default)
	assert.True(t, c.Runner.InjectProtocol)
	assert.Equal(t, "anthropic", c.Runner.Defs["claude-code"].Provider)
	assert.Equal(t, 8, c.Scheduler.LLM.MaxConcurrentRequests)
	assert.InDelta(t, 80, c.Scheduler.LLM.Providers["anthropic"].CostPerDayBudgetUSD, 0)
}

func TestDecode_CmdMustBeArray(t *testing.T) {
	_, err := decode("[[gates]]\nname = \"build\"\ncmd = \"go build ./...\"\n")
	require.Error(t, err)
	_, err = decode("[runner.codex]\ncmd = \"codex exec\"\n")
	require.Error(t, err)
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func FuzzDecode(f *testing.F) {
	entries, err := os.ReadDir("testdata")
	require.NoError(f, err)
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join("testdata", e.Name()))
		require.NoError(f, err)
		f.Add(string(data))
	}
	f.Fuzz(func(_ *testing.T, text string) {
		// Must never panic; errors are expected for most inputs.
		_, _ = decode(text) // only panics matter here
	})
}
