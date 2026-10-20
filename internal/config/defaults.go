package config

// Default returns the built-in configuration: every SCOPE.md §22 scalar is a
// default except the ones that are derived or deployment-specific
// (SPEC-GAPS U-API-11):
//
//   - daemon.state_dir (derived from the config location, see StatePath);
//   - devdrive.path, devdrive.mount, devdrive.size_gb (enabled is false);
//   - workspace.root and workspace.golden_root (see EffectiveGoldenRoot);
//   - every cache.* key;
//   - runner.default and runner.lead (decided in Sprint 3).
//
// [[gates]], [[invariants]], [[resources]], [runner.<name>] and
// [scheduler.llm.<provider>] are examples only and have no defaults.
func Default() *Config {
	return &Config{
		Daemon:  Daemon{Bind: "127.0.0.1:8787", LogLevel: "info"},
		Project: Project{Lead: "human", Branching: "gitflow", DefaultBase: "develop"},
		DevDrive: DevDrive{
			AutoAttach: true,
		},
		Workspace: Workspace{
			Provider:              "auto",
			KeepFailedWorkspaces:  true,
			KeepFailedHours:       72,
			CleanupCompleted:      false,
			SparseByIntent:        true,
			PortRangePerWorkspace: 10,
			PortRangeStart:        40000,
			ProjFS: ProjFS{
				Enabled:     false,
				ScopeOnly:   true,
				FSMonitor:   true,
				BlobCacheMB: 512,
			},
		},
		Git: Git{
			SharedRefNamespace:  "refs/cowflow/changes",
			ChangeRetentionDays: 30,
			DefaultSyncStrategy: "merge",
			RequireGitattrs:     true,
		},
		Scheduler: Scheduler{
			MaxAgents:                20,
			MaxBuilds:                3,
			MaxTests:                 6,
			MaxIntegrationsPerTarget: 1,
			SessionTimeoutSeconds:    3600,
			CommandTimeoutSeconds:    900,
			RetryLimit:               2,
			RetryBackoffSeconds:      []int{30, 120},
			WorkspaceDiskBudgetGB:    100,
			GoldenCacheGB:            30,
			LLM: SchedulerLLM{
				LLMBudget: LLMBudget{
					MaxConcurrentRequests: 8,
					TokensPerHourBudget:   4000000,
					CostPerDayBudgetUSD:   150,
					OnBudgetExceeded:      "pause",
				},
			},
		},
		Collaboration: Collaboration{
			PresenceTTLSeconds:       90,
			HeartbeatIntervalSeconds: 20,
			EscalationAfterMinutes:   Escalation{High: 15, Normal: 60, Low: 0},
			EventDelivery:            "sse",
		},
		Integration: Integration{
			RequireValidation: true,
			PromotionMode:     "manual",
			RerunOnStale:      true,
			DiffOnlyFirst:     true,
			PostPromotion: PostPromotion{
				Enabled:    false,
				Gates:      []string{"test"},
				AutoRevert: true,
			},
		},
		Conventions: Conventions{
			Preset:                   "gitflow",
			EnforceBranchNames:       true,
			EnforceProtectedBranches: true,
			EnforceCommitFormat:      true,
			TagFormat:                "v{major}.{minor}.{patch}",
		},
		Runner:   Runner{InjectProtocol: true},
		Watchdog: Watchdog{Enabled: false, BudgetTokensPerHour: 200000},
		Security: Security{BlockedPatterns: []string{".env", ".env.*", "*.pem", "*.key", "id_rsa*"}},
	}
}
