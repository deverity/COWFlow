package config

import (
	"errors"
	"fmt"
	"net"
	"path"
	"regexp"
	"slices"
	"strings"
)

// check is one validation rule.
type check struct {
	name string
	run  func(*Config) []error
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
	{name: "integration.promotion_mode", run: checkPromotionMode},
	{name: "git.default_sync_strategy", run: checkSyncStrategy},
	{name: "runner.prompt_mode", run: checkPromptMode},
	{name: "devdrive.mount", run: checkMount},
}

// Validate reports every problem found in c, joined with errors.Join, so the
// user can fix them in one pass. Empty values fail: every key with a built-in
// default (see Default) must be set.
func (c *Config) Validate() error {
	var errs []error
	for _, ck := range checks {
		errs = append(errs, ck.run(c)...)
	}
	return errors.Join(errs...)
}

// checkBind requires a loopback host (§12, §17). A string prefix test would
// accept "127.evil.example" and reject "[::1]".
func checkBind(c *Config) []error {
	bind := c.Daemon.Bind
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
	if b := c.Project.Branching; !slices.Contains([]string{"gitflow", "trunk"}, b) {
		errs = append(errs, fmt.Errorf("project.branching %q: must be gitflow or trunk", b))
	}
	return errs
}

func checkPreset(c *Config) []error {
	if p := c.Conventions.Preset; !slices.Contains([]string{"gitflow", "trunk", "none"}, p) {
		return []error{fmt.Errorf("conventions.preset %q: must be gitflow, trunk or none", p)}
	}
	return nil
}

// checkPromotionMode accepts manual and auto; "pr" is post-v1 (SCOPE.md §23).
func checkPromotionMode(c *Config) []error {
	if c.Integration.PromotionMode == "pr" {
		return []error{errors.New(`integration.promotion_mode "pr": not supported in v1; use manual or auto`)}
	}
	if m := c.Integration.PromotionMode; !slices.Contains([]string{"manual", "auto"}, m) {
		return []error{fmt.Errorf("integration.promotion_mode %q: must be manual or auto", m)}
	}
	return nil
}

// checkSyncStrategy accepts merge, cherry-pick and squash.
func checkSyncStrategy(c *Config) []error {
	if s := c.Git.DefaultSyncStrategy; !slices.Contains([]string{"merge", "cherry-pick", "squash"}, s) {
		return []error{fmt.Errorf("git.default_sync_strategy %q: must be merge, cherry-pick or squash", s)}
	}
	return nil
}

// checkPromptMode accepts arg, stdin and file; an unset mode is allowed.
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
		if !slices.Contains([]string{"fifo", "priority"}, r.Queue) {
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

// checkRetryBackoff rejects an empty list.
func checkRetryBackoff(c *Config) []error {
	if len(c.Scheduler.RetryBackoffSeconds) == 0 {
		return []error{errors.New("scheduler.retry_backoff_seconds: must not be empty")}
	}
	return nil
}

// checkBlockedPatterns requires every pattern to compile. The list may be
// empty: a file value replaces the default list.
func checkBlockedPatterns(c *Config) []error {
	var errs []error
	for _, p := range c.Security.BlockedPatterns {
		if _, err := path.Match(p, ""); errors.Is(err, path.ErrBadPattern) {
			errs = append(errs, fmt.Errorf("security.blocked_patterns %q: %w", p, err))
		}
	}
	return errs
}

// letterMount is the accepted mount form: one drive letter D-Z and a colon,
// as in internal/platform. The mount point form (C:\cf\) is an open question
// (SCOPE.md section 23) and is rejected.
var letterMount = regexp.MustCompile(`^[D-Z]:$`)

// checkMount validates devdrive.mount when set; it has no default.
func checkMount(c *Config) []error {
	if m := c.DevDrive.Mount; m != "" && !letterMount.MatchString(m) {
		return []error{fmt.Errorf(`devdrive.mount %q: must be a drive letter D-Z such as "X:"; mount points are not supported in v1`, m)}
	}
	return nil
}

func sortedRunnerNames(m map[string]RunnerDef) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}
