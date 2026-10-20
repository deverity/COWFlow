package config

import "strings"

// Config is the root of config.toml. Its fields mirror SCOPE.md §22.
type Config struct {
	Daemon        Daemon        `toml:"daemon"`
	Project       Project       `toml:"project"`
	DevDrive      DevDrive      `toml:"devdrive"`
	Workspace     Workspace     `toml:"workspace"`
	Cache         Cache         `toml:"cache"`
	Git           Git           `toml:"git"`
	Scheduler     Scheduler     `toml:"-"` // decoded manually: [scheduler.llm] mixes scalars and tables
	Collaboration Collaboration `toml:"collaboration"`
	Integration   Integration   `toml:"integration"`
	Gates         []Gate        `toml:"gates"`
	Invariants    []Invariant   `toml:"invariants"`
	Resources     []Resource    `toml:"resources"`
	Conventions   Conventions   `toml:"conventions"`
	Runner        Runner        `toml:"-"` // decoded manually: [runner] mixes scalars and tables
	Watchdog      Watchdog      `toml:"watchdog"`
	Security      Security      `toml:"security"`
}

// Daemon is [daemon].
type Daemon struct {
	Bind     string `toml:"bind"`
	StateDir string `toml:"state_dir"`
	LogLevel string `toml:"log_level"`
}

// Project is [project].
type Project struct {
	Lead        string `toml:"lead"`
	Branching   string `toml:"branching"`
	DefaultBase string `toml:"default_base"`
}

// DevDrive is [devdrive].
type DevDrive struct {
	Enabled    bool   `toml:"enabled"`
	Path       string `toml:"path"`
	Mount      string `toml:"mount"`
	SizeGB     int    `toml:"size_gb"`
	AutoAttach bool   `toml:"auto_attach"`
}

// Workspace is [workspace].
type Workspace struct {
	Provider              string `toml:"provider"`
	Root                  string `toml:"root"`
	GoldenRoot            string `toml:"golden_root"` // optional; golden tree at <golden_root>/<sha>; empty means <root>/golden
	KeepFailedWorkspaces  bool   `toml:"keep_failed_workspaces"`
	KeepFailedHours       int    `toml:"keep_failed_hours"`
	CleanupCompleted      bool   `toml:"cleanup_completed"`
	SparseByIntent        bool   `toml:"sparse_by_intent"`
	PortRangePerWorkspace int    `toml:"port_range_per_workspace"`
	PortRangeStart        int    `toml:"port_range_start"`
	ProjFS                ProjFS `toml:"projfs"`
}

// ProjFS is [workspace.projfs].
type ProjFS struct {
	Enabled     bool `toml:"enabled"`
	ScopeOnly   bool `toml:"scope_only"`
	FSMonitor   bool `toml:"fsmonitor"`
	BlobCacheMB int  `toml:"blob_cache_mb"`
}

// Cache is [cache] (§22, §6.7).
type Cache struct {
	PnpmStore   string `toml:"pnpm_store"`
	GoModCache  string `toml:"go_modcache"`
	GoCache     string `toml:"go_cache"`
	CargoTarget string `toml:"cargo_target"`
	GradleHome  string `toml:"gradle_home"`
	PipCache    string `toml:"pip_cache"`
}

// Git is [git].
type Git struct {
	SharedRefNamespace  string `toml:"shared_ref_namespace"`
	ChangeRetentionDays int    `toml:"change_retention_days"`
	DefaultSyncStrategy string `toml:"default_sync_strategy"` // merge | cherry-pick | squash
	RequireGitattrs     bool   `toml:"require_gitattributes"`
}

// Scheduler is [scheduler], including [scheduler.llm] (§11, §15.3).
type Scheduler struct {
	MaxAgents                int          `toml:"max_agents"`
	MaxBuilds                int          `toml:"max_builds"`
	MaxTests                 int          `toml:"max_tests"`
	MaxIntegrationsPerTarget int          `toml:"max_integrations_per_target"`
	SessionTimeoutSeconds    int          `toml:"session_timeout_seconds"`
	CommandTimeoutSeconds    int          `toml:"command_timeout_seconds"`
	RetryLimit               int          `toml:"retry_limit"`
	RetryBackoffSeconds      []int        `toml:"retry_backoff_seconds"`
	WorkspaceDiskBudgetGB    int          `toml:"workspace_disk_budget_gb"`
	GoldenCacheGB            int          `toml:"golden_cache_gb"`
	LLM                      SchedulerLLM `toml:"-"`
}

// LLMBudget holds the rate and budget limits of [scheduler.llm] and of each
// [scheduler.llm.<provider>] table.
type LLMBudget struct {
	MaxConcurrentRequests int     `toml:"max_concurrent_requests"`
	TokensPerHourBudget   int64   `toml:"tokens_per_hour_budget"`
	CostPerDayBudgetUSD   float64 `toml:"cost_per_day_budget_usd"`
	OnBudgetExceeded      string  `toml:"on_budget_exceeded"`
}

// SchedulerLLM is [scheduler.llm]: scalars plus one table per provider.
type SchedulerLLM struct {
	LLMBudget                      // scalars of [scheduler.llm]
	Providers map[string]LLMBudget // [scheduler.llm.<provider>]
}

// Collaboration is [collaboration].
type Collaboration struct {
	PresenceTTLSeconds       int        `toml:"presence_ttl_seconds"`
	HeartbeatIntervalSeconds int        `toml:"heartbeat_interval_seconds"`
	EscalationAfterMinutes   Escalation `toml:"escalation_after_minutes"`
	EventDelivery            string     `toml:"event_delivery"`
}

// Escalation is [collaboration.escalation_after_minutes]: minutes an open
// cross request may wait for accept/reject, per priority, before
// cross_request.escalated goes to the lead. 0 means no SLA: never escalated.
type Escalation struct {
	High   int `toml:"high"`
	Normal int `toml:"normal"`
	Low    int `toml:"low"`
}

// Integration is [integration].
type Integration struct {
	RequireValidation bool          `toml:"require_validation"`
	PromotionMode     string        `toml:"promotion_mode"` // manual | auto ("pr" is post-v1)
	RerunOnStale      bool          `toml:"rerun_on_stale"`
	DiffOnlyFirst     bool          `toml:"diff_only_first"`
	PostPromotion     PostPromotion `toml:"post_promotion"`
}

// PostPromotion is [integration.post_promotion].
type PostPromotion struct {
	Enabled    bool     `toml:"enabled"`
	Gates      []string `toml:"gates"`
	AutoRevert bool     `toml:"auto_revert"`
}

// Gate is one [[gates]] entry.
type Gate struct {
	Name           string   `toml:"name"`
	Cmd            []string `toml:"cmd"` // argv array, never a shell string (§7)
	TimeoutSeconds int      `toml:"timeout_seconds"`
	Required       bool     `toml:"required"`
}

// Invariant is one [[invariants]] entry (§9.6).
type Invariant struct {
	Name           string   `toml:"name"`
	Kind           string   `toml:"kind"` // pattern | cmd
	Deny           []string `toml:"deny"`
	Cmd            []string `toml:"cmd"` // argv array, never a shell string (§7)
	CheckOnPublish bool     `toml:"check_on_publish"`
}

// Resource is one [[resources]] entry (§9.8).
type Resource struct {
	Name            string `toml:"name"`
	Slots           int    `toml:"slots"`
	LeaseTTLSeconds int    `toml:"lease_ttl_seconds"`
	Queue           string `toml:"queue"` // fifo | priority
}

// Conventions is [conventions] (§24).
type Conventions struct {
	Preset                   string `toml:"preset"` // gitflow | trunk | none
	EnforceBranchNames       bool   `toml:"enforce_branch_names"`
	EnforceProtectedBranches bool   `toml:"enforce_protected_branches"`
	EnforceCommitFormat      bool   `toml:"enforce_commit_format"`
	TagFormat                string `toml:"tag_format"`
}

// RunnerDef is one [runner.<name>] table (§15.3).
type RunnerDef struct {
	Provider       string   `toml:"provider"`
	Cmd            []string `toml:"cmd"` // argv array, never a shell string (§7, §17)
	MCPConfigFlag  []string `toml:"mcp_config_flag"`
	PermissionFlag []string `toml:"permission_flag"`
	ContextFile    string   `toml:"context_file"`
	PromptMode     string   `toml:"prompt_mode"` // arg | stdin | file
}

// Runner is [runner]: scalars plus one table per runner name.
type Runner struct {
	Default        string
	InjectProtocol bool
	Lead           string
	Defs           map[string]RunnerDef // [runner.<name>]
}

// Watchdog is [watchdog].
type Watchdog struct {
	Enabled             bool  `toml:"enabled"`
	BudgetTokensPerHour int64 `toml:"budget_tokens_per_hour"`
}

// Security is [security].
type Security struct {
	BlockedPatterns []string `toml:"blocked_patterns"` // replaces the default list; the built-in never-commit list (§24.13) is separate and fixed
}

// EffectiveGoldenRoot returns the directory holding golden trees
// (<dir>/<sha>): golden_root when set, else <workspace.root>/golden.
// It is empty when neither is set.
func (w Workspace) EffectiveGoldenRoot() string {
	if w.GoldenRoot != "" {
		return w.GoldenRoot
	}
	if w.Root == "" {
		return ""
	}
	return strings.TrimRight(w.Root, `/\`) + "/golden"
}
