package config

// Default returns a configuration holding the spec-stated defaults only
// (§22, §6.3, §9.4, §23 decisions 6, 7 and 10). Every other §22 value is an
// example, not a default, and stays at its zero value until SPEC-GAPS
// U-API-11 decides which ones are defaults.
func Default() *Config {
	return &Config{
		Project: Project{
			Lead:        "human",
			DefaultBase: "develop",
			// PENDING U-API-11: Branching has no stated default.
		},
		Workspace: Workspace{
			Provider: "auto",
			ProjFS:   ProjFS{Enabled: false},
			// PENDING U-API-11: Root, GoldenRoot (also C7), the keep/cleanup
			// settings and the port range are example values, not defaults.
		},
		Git: Git{
			DefaultSyncStrategy: "merge", // accepted value set is PENDING C4
			// PENDING U-API-11: the other [git] keys.
		},
		Integration: Integration{
			PromotionMode: "manual", // accepted value set is PENDING C3
			PostPromotion: PostPromotion{Enabled: false},
			// PENDING U-API-11: the other [integration] keys.
		},
		Watchdog: Watchdog{Enabled: false},
		// PENDING U-API-11: [daemon], [devdrive], [cache], [scheduler],
		// [collaboration], [conventions] and [security] defaults. In
		// particular security.blocked_patterns stays empty until C12 and
		// scheduler.retry_backoff_seconds stays nil.
	}
}
