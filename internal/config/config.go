package config

import "errors"

// errNotImplemented is returned by contract entry points whose behavior has
// not landed yet.
var errNotImplemented = errors.New("config: not implemented")

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
	GoldenRoot            string `toml:"golden_root"` // PENDING C7: golden_root vs <root>/golden/<sha>
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
	GradleHome  string `toml:"gradle_home"` // PENDING C13: in §6.7, missing from the §22 example
	PipCache    string `toml:"pip_cache"`   // PENDING C13: in §6.7, missing from the §22 example
}

// Git is [git].
type Git struct {
	SharedRefNamespace  string `toml:"shared_ref_namespace"`
	ChangeRetentionDays int    `toml:"change_retention_days"`
	DefaultSyncStrategy string `toml:"default_sync_strategy"` // PENDING C4: accepted values
	RequireGitattrs     bool   `toml:"require_gitattributes"`
}

// Scheduler is [scheduler], including [scheduler.llm] (§11, §15.3).
type Scheduler struct {
	MaxAgents                int          `toml:"max_agents"`
	MaxBuilds                int          `toml:"max_builds"`
	MaxTests                 int          `toml:"max_tests"`
	MaxIntegrationsPerTarget int          `toml:"max_integrations_per_target"` // PENDING C5: vs max_concurrent_per_target (§10.3); no alias until decided
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
	PresenceTTLSeconds       int    `toml:"presence_ttl_seconds"`
	HeartbeatIntervalSeconds int    `toml:"heartbeat_interval_seconds"`
	EscalationAfterMinutes   int    `toml:"escalation_after_minutes"` // PENDING C9: vs per-priority SLA
	EventDelivery            string `toml:"event_delivery"`
}

// Integration is [integration].
type Integration struct {
	RequireValidation bool          `toml:"require_validation"`
	PromotionMode     string        `toml:"promotion_mode"` // PENDING C3: accepted values
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
	PromptMode     string   `toml:"prompt_mode"` // PENDING C10: enum check is S0-T04
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
	BlockedPatterns []string `toml:"blocked_patterns"` // PENDING C12: relationship with the §24.13 never-commit list
}

// Default returns a configuration holding the spec-stated defaults only
// (§22, §6.3, §9.4, §23).
func Default() *Config { return &Config{} }

// Validate reports every problem found in c, joined with errors.Join.
func (*Config) Validate() error { return errNotImplemented }

// Save writes c to path atomically (temporary file plus rename).
func Save(_ string, _ *Config) error { return errNotImplemented }

// Locate returns the location of config.toml.
//
// PENDING U-API-1: the location is undecided; callers inject explicit paths
// (see cli.Deps.ConfigPath) until it is.
func Locate() (string, error) { return "", errNotImplemented }
