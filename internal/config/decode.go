package config

import (
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/BurntSushi/toml"
)

// rawScheduler is [scheduler] with [scheduler.llm] left undecoded because it
// mixes scalars and per-provider tables.
type rawScheduler struct {
	Scheduler
	LLM map[string]toml.Primitive `toml:"llm"`
}

// raw is the on-disk shape of config.toml before the mixed tables are split.
type raw struct {
	Daemon        Daemon                    `toml:"daemon"`
	Project       Project                   `toml:"project"`
	DevDrive      DevDrive                  `toml:"devdrive"`
	Workspace     Workspace                 `toml:"workspace"`
	Cache         Cache                     `toml:"cache"`
	Git           Git                       `toml:"git"`
	Scheduler     rawScheduler              `toml:"scheduler"`
	Collaboration Collaboration             `toml:"collaboration"`
	Integration   Integration               `toml:"integration"`
	Gates         []Gate                    `toml:"gates"`
	Invariants    []Invariant               `toml:"invariants"`
	Resources     []Resource                `toml:"resources"`
	Conventions   Conventions               `toml:"conventions"`
	Runner        map[string]toml.Primitive `toml:"runner"`
	Watchdog      Watchdog                  `toml:"watchdog"`
	Security      Security                  `toml:"security"`
}

// Load decodes the file at path over the spec-stated defaults, rejects
// unknown keys, then validates the result.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the config path is chosen by the operator
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	c, err := decode(string(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	return c, nil
}

// newRaw pre-fills the on-disk shape with d so keys absent from the file keep
// their defaults.
func newRaw(d *Config) raw {
	return raw{
		Daemon:        d.Daemon,
		Project:       d.Project,
		DevDrive:      d.DevDrive,
		Workspace:     d.Workspace,
		Cache:         d.Cache,
		Git:           d.Git,
		Scheduler:     rawScheduler{Scheduler: d.Scheduler},
		Collaboration: d.Collaboration,
		Integration:   d.Integration,
		Gates:         d.Gates,
		Invariants:    d.Invariants,
		Resources:     d.Resources,
		Conventions:   d.Conventions,
		Watchdog:      d.Watchdog,
		Security:      d.Security,
	}
}

// decode parses TOML text into a Config pre-filled with Default(). Every key the schema does not know
// is reported, one error per key, with its dotted path.
func decode(text string) (*Config, error) {
	r := newRaw(Default())
	md, err := toml.Decode(text, &r)
	if err != nil {
		return nil, fmt.Errorf("config: decode: %w", err)
	}

	c := &Config{
		Daemon:        r.Daemon,
		Project:       r.Project,
		DevDrive:      r.DevDrive,
		Workspace:     r.Workspace,
		Cache:         r.Cache,
		Git:           r.Git,
		Scheduler:     r.Scheduler.Scheduler,
		Collaboration: r.Collaboration,
		Integration:   r.Integration,
		Gates:         r.Gates,
		Invariants:    r.Invariants,
		Resources:     r.Resources,
		Conventions:   r.Conventions,
		Watchdog:      r.Watchdog,
		Security:      r.Security,
	}

	var errs []error
	llm, err := decodeSchedulerLLM(md, r.Scheduler.LLM)
	c.Scheduler.LLM = llm
	if err != nil {
		errs = append(errs, err)
	}
	runner, err := decodeRunner(md, r.Runner)
	c.Runner = runner
	if err != nil {
		errs = append(errs, err)
	}

	// PrimitiveDecode marks the keys it consumes as decoded, so whatever
	// remains undecoded is unknown to the schema.
	for _, k := range md.Undecoded() {
		errs = append(errs, fmt.Errorf("config: unknown key %q", k.String()))
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return c, nil
}

// decodeSchedulerLLM splits [scheduler.llm] into its scalars and its
// per-provider tables.
func decodeSchedulerLLM(md toml.MetaData, prims map[string]toml.Primitive) (SchedulerLLM, error) {
	out := SchedulerLLM{Providers: map[string]LLMBudget{}}
	var errs []error
	for _, key := range sortedKeys(prims) {
		prim := prims[key]
		if md.Type("scheduler", "llm", key) == "Hash" {
			var b LLMBudget
			if err := md.PrimitiveDecode(prim, &b); err != nil {
				errs = append(errs, fmt.Errorf("config: scheduler.llm.%s: %w", key, err))
				continue
			}
			out.Providers[key] = b
			continue
		}
		var err error
		switch key {
		case "max_concurrent_requests":
			err = md.PrimitiveDecode(prim, &out.MaxConcurrentRequests)
		case "tokens_per_hour_budget":
			err = md.PrimitiveDecode(prim, &out.TokensPerHourBudget)
		case "cost_per_day_budget_usd":
			err = md.PrimitiveDecode(prim, &out.CostPerDayBudgetUSD)
		case "on_budget_exceeded":
			err = md.PrimitiveDecode(prim, &out.OnBudgetExceeded)
		default:
			errs = append(errs, fmt.Errorf("config: unknown key %q", "scheduler.llm."+key))
			continue
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("config: scheduler.llm.%s: %w", key, err))
		}
	}
	return out, errors.Join(errs...)
}

// decodeRunner splits [runner] into its scalars and its per-runner tables.
func decodeRunner(md toml.MetaData, prims map[string]toml.Primitive) (Runner, error) {
	out := Runner{Defs: map[string]RunnerDef{}}
	var errs []error
	for _, key := range sortedKeys(prims) {
		prim := prims[key]
		if md.Type("runner", key) == "Hash" {
			var d RunnerDef
			if err := md.PrimitiveDecode(prim, &d); err != nil {
				errs = append(errs, fmt.Errorf("config: runner.%s: %w", key, err))
				continue
			}
			out.Defs[key] = d
			continue
		}
		var err error
		switch key {
		case "default":
			err = md.PrimitiveDecode(prim, &out.Default)
		case "inject_protocol":
			err = md.PrimitiveDecode(prim, &out.InjectProtocol)
		case "lead":
			err = md.PrimitiveDecode(prim, &out.Lead)
		default:
			errs = append(errs, fmt.Errorf("config: unknown key %q", "runner."+key))
			continue
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("config: runner.%s: %w", key, err))
		}
	}
	return out, errors.Join(errs...)
}

func sortedKeys(m map[string]toml.Primitive) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
