package config

import (
	"errors"
	"fmt"
	"net"
	"path"
	"slices"
	"strings"
)

// check is one validation rule. A rule with a non-empty pending field names
// the SPEC-GAPS item that blocks it: the rule is written but not enforced.
type check struct {
	name    string
	pending string
	run     func(*Config) []error
}

// checks lists every validation rule in the order it reports.
var checks = []check{
	{name: "daemon.bind", run: checkBind},
	{name: "workspace.provider", run: checkProvider},
	{name: "project", run: checkProject},
	{name: "conventions.preset", run: checkPreset},
	{name: "gates", run: checkGates},
	{name: "invariants", run: checkInvariants},
	{name: "resources", run: checkResources},
	{name: "runner", run: checkRunner},
	{name: "scheduler.retry_backoff_seconds", run: checkRetryBackoff},
	{name: "security.blocked_patterns", run: checkBlockedPatterns},

	{name: "integration.promotion_mode", pending: "C3", run: checkPromotionMode},
	{name: "git.default_sync_strategy", pending: "C4", run: checkSyncStrategy},
	{name: "runner.prompt_mode", pending: "C10", run: checkPromptMode},
}

// Validate reports every problem found in c, joined with errors.Join, so the
// user can fix them in one pass. Rules blocked by an undecided SPEC-GAPS item
// are not enforced.
func (c *Config) Validate() error {
	var errs []error
	for _, ck := range checks {
		if ck.pending != "" {
			continue
		}
		errs = append(errs, ck.run(c)...)
	}
	return errors.Join(errs...)
}

// checkBind requires a loopback host (§12, §17). A string prefix test would
// accept "127.evil.example" and reject "[::1]".
func checkBind(c *Config) []error {
	bind := c.Daemon.Bind
	if bind == "" {
		return nil // PENDING U-API-11: no stated default
	}
	host, _, err := net.SplitHostPort(bind)
	if err != nil {
		return []error{fmt.Errorf("daemon.bind %q: %w", bind, err)}
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return []error{fmt.Errorf("daemon.bind %q: host must be a loopback address or localhost", bind)}
}

func checkProvider(c *Config) []error {
	p := c.Workspace.Provider
	if !slices.Contains([]string{"auto", "cow_clone", "projfs", "git_worktree", "container"}, p) {
		return []error{fmt.Errorf("workspace.provider %q: must be auto, cow_clone, projfs, git_worktree or container", p)}
	}
	if p == "projfs" && !c.Workspace.ProjFS.Enabled {
		return []error{errors.New(`workspace.provider "projfs" requires workspace.projfs.enabled = true`)}
	}
	return nil
}

func checkProject(c *Config) []error {
	var errs []error
	lead := c.Project.Lead
	if id, ok := strings.CutPrefix(lead, "agent:"); lead != "human" && (!ok || id == "") {
		errs = append(errs, fmt.Errorf("project.lead %q: must be human or agent:<task-id>", lead))
	}
	// PENDING U-API-11: an empty branching is allowed, it has no stated default.
	if b := c.Project.Branching; b != "" && !slices.Contains([]string{"gitflow", "trunk"}, b) {
		errs = append(errs, fmt.Errorf("project.branching %q: must be gitflow or trunk", b))
	}
	return errs
}

func checkPreset(c *Config) []error {
	// PENDING U-API-11: an empty preset is allowed, it has no stated default.
	if p := c.Conventions.Preset; p != "" && !slices.Contains([]string{"gitflow", "trunk", "none"}, p) {
		return []error{fmt.Errorf("conventions.preset %q: must be gitflow, trunk or none", p)}
	}
	return nil
}

// checkPromotionMode is PENDING C3: manual|auto vs also pr.
func checkPromotionMode(c *Config) []error {
	if m := c.Integration.PromotionMode; !slices.Contains([]string{"manual", "auto"}, m) {
		return []error{fmt.Errorf("integration.promotion_mode %q: must be manual or auto", m)}
	}
	return nil
}

// checkSyncStrategy is PENDING C4: squash-merge vs squash.
func checkSyncStrategy(c *Config) []error {
	if s := c.Git.DefaultSyncStrategy; !slices.Contains([]string{"merge", "cherry-pick", "squash"}, s) {
		return []error{fmt.Errorf("git.default_sync_strategy %q: must be merge, cherry-pick or squash", s)}
	}
	return nil
}

// checkPromptMode is PENDING C10: arg|stdin|file vs also message.
func checkPromptMode(c *Config) []error {
	var errs []error
	for _, name := range sortedRunnerNames(c.Runner.Defs) {
		if m := c.Runner.Defs[name].PromptMode; m != "" && !slices.Contains([]string{"arg", "stdin", "file"}, m) {
			errs = append(errs, fmt.Errorf("runner.%s.prompt_mode %q: must be arg, stdin or file", name, m))
		}
	}
	return errs
}

func checkGates(c *Config) []error {
	var errs []error
	seen := map[string]bool{}
	for i, g := range c.Gates {
		if seen[g.Name] {
			errs = append(errs, fmt.Errorf("gates[%d]: duplicate name %q", i, g.Name))
		}
		seen[g.Name] = true
		if len(g.Cmd) == 0 {
			errs = append(errs, fmt.Errorf("gates[%d] %q: cmd must be a non-empty array", i, g.Name))
		}
	}
	return errs
}

func checkInvariants(c *Config) []error {
	var errs []error
	seen := map[string]bool{}
	for i, inv := range c.Invariants {
		if seen[inv.Name] {
			errs = append(errs, fmt.Errorf("invariants[%d]: duplicate name %q", i, inv.Name))
		}
		seen[inv.Name] = true
		if !slices.Contains([]string{"pattern", "cmd"}, inv.Kind) {
			errs = append(errs, fmt.Errorf("invariants[%d] %q: kind %q must be pattern or cmd", i, inv.Name, inv.Kind))
		}
	}
	return errs
}

func checkResources(c *Config) []error {
	var errs []error
	seen := map[string]bool{}
	for i, r := range c.Resources {
		if seen[r.Name] {
			errs = append(errs, fmt.Errorf("resources[%d]: duplicate name %q", i, r.Name))
		}
		seen[r.Name] = true
		if r.Slots <= 0 {
			errs = append(errs, fmt.Errorf("resources[%d] %q: slots must be greater than 0", i, r.Name))
		}
		// PENDING U-API-11: an empty queue is allowed, it has no stated default.
		if r.Queue != "" && !slices.Contains([]string{"fifo", "priority"}, r.Queue) {
			errs = append(errs, fmt.Errorf("resources[%d] %q: queue %q must be fifo or priority", i, r.Name, r.Queue))
		}
	}
	return errs
}

func checkRunner(c *Config) []error {
	var errs []error
	for _, name := range sortedRunnerNames(c.Runner.Defs) {
		if len(c.Runner.Defs[name].Cmd) == 0 {
			errs = append(errs, fmt.Errorf("runner.%s: cmd must be a non-empty array", name))
		}
	}
	for _, ref := range []struct{ key, value string }{
		{"runner.default", c.Runner.Default},
		{"runner.lead", c.Runner.Lead},
	} {
		if _, ok := c.Runner.Defs[ref.value]; ref.value != "" && !ok {
			errs = append(errs, fmt.Errorf("%s %q: no [runner.%s] table is defined", ref.key, ref.value, ref.value))
		}
	}
	return errs
}

// checkRetryBackoff rejects an explicitly empty list. PENDING U-API-11: the
// default list is undecided, so an absent (nil) value is accepted.
func checkRetryBackoff(c *Config) []error {
	if b := c.Scheduler.RetryBackoffSeconds; b != nil && len(b) == 0 {
		return []error{errors.New("scheduler.retry_backoff_seconds: must not be empty")}
	}
	return nil
}

// checkBlockedPatterns requires every pattern to compile. The default list is
// PENDING C12.
func checkBlockedPatterns(c *Config) []error {
	var errs []error
	for _, p := range c.Security.BlockedPatterns {
		if _, err := path.Match(p, ""); errors.Is(err, path.ErrBadPattern) {
			errs = append(errs, fmt.Errorf("security.blocked_patterns %q: %w", p, err))
		}
	}
	return errs
}

func sortedRunnerNames(m map[string]RunnerDef) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}
