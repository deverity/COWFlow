package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// onDisk is the encodable shape of a Config: [scheduler.llm] and [runner]
// mix scalars and tables, so they are encoded as maps.
type onDisk struct {
	Daemon        Daemon         `toml:"daemon"`
	Project       Project        `toml:"project"`
	DevDrive      DevDrive       `toml:"devdrive"`
	Workspace     Workspace      `toml:"workspace"`
	Cache         Cache          `toml:"cache"`
	Git           Git            `toml:"git"`
	Scheduler     onDiskSched    `toml:"scheduler"`
	Collaboration Collaboration  `toml:"collaboration"`
	Integration   Integration    `toml:"integration"`
	Gates         []Gate         `toml:"gates"`
	Invariants    []Invariant    `toml:"invariants"`
	Resources     []Resource     `toml:"resources"`
	Conventions   Conventions    `toml:"conventions"`
	Runner        map[string]any `toml:"runner"`
	Watchdog      Watchdog       `toml:"watchdog"`
	Security      Security       `toml:"security"`
}

type onDiskSched struct {
	Scheduler
	LLM map[string]any `toml:"llm"`
}

func toOnDisk(c *Config) onDisk {
	llm := map[string]any{
		"max_concurrent_requests": c.Scheduler.LLM.MaxConcurrentRequests,
		"tokens_per_hour_budget":  c.Scheduler.LLM.TokensPerHourBudget,
		"cost_per_day_budget_usd": c.Scheduler.LLM.CostPerDayBudgetUSD,
		"on_budget_exceeded":      c.Scheduler.LLM.OnBudgetExceeded,
	}
	for name, b := range c.Scheduler.LLM.Providers {
		llm[name] = b
	}
	runner := map[string]any{
		"default":         c.Runner.Default,
		"inject_protocol": c.Runner.InjectProtocol,
		"lead":            c.Runner.Lead,
	}
	for name, d := range c.Runner.Defs {
		runner[name] = d
	}
	return onDisk{
		Daemon:        c.Daemon,
		Project:       c.Project,
		DevDrive:      c.DevDrive,
		Workspace:     c.Workspace,
		Cache:         c.Cache,
		Git:           c.Git,
		Scheduler:     onDiskSched{Scheduler: c.Scheduler, LLM: llm},
		Collaboration: c.Collaboration,
		Integration:   c.Integration,
		Gates:         c.Gates,
		Invariants:    c.Invariants,
		Resources:     c.Resources,
		Conventions:   c.Conventions,
		Runner:        runner,
		Watchdog:      c.Watchdog,
		Security:      c.Security,
	}
}

// encodeConfig writes c as TOML to w. It is a variable so tests can inject a
// failing writer.
var encodeConfig = func(w io.Writer, c *Config) error {
	return toml.NewEncoder(w).Encode(toOnDisk(c))
}

// Save writes c to path atomically: it encodes into a temporary file in the
// same directory, syncs it, copies any previous file to path+".bak", then
// renames the temporary file over path. On failure the original is untouched.
// Comments in a hand-edited file are lost on rewrite.
func Save(path string, c *Config) (err error) {
	f, err := os.CreateTemp(filepath.Dir(path), ".config-*.toml")
	if err != nil {
		return fmt.Errorf("config: save %s: %w", path, err)
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmp)
		}
	}()
	if err = encodeConfig(f, c); err != nil {
		_ = f.Close()
		return fmt.Errorf("config: save %s: encode: %w", path, err)
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("config: save %s: sync: %w", path, err)
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("config: save %s: close: %w", path, err)
	}
	if err = backup(path); err != nil {
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		return fmt.Errorf("config: save %s: replace failed (is the file open in another program?): %w", path, err)
	}
	return nil
}

// backup copies the existing file at path to path+".bak". A missing file is
// not an error.
func backup(path string) error {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the config path is chosen by the operator
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("config: save %s: read previous: %w", path, err)
	}
	if err := os.WriteFile(path+".bak", data, 0o600); err != nil {
		return fmt.Errorf("config: save %s: write backup: %w", path, err)
	}
	return nil
}
