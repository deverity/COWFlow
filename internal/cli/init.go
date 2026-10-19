package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/deverity/cowflow/internal/config"
	"github.com/deverity/cowflow/internal/platform"
)

// minDevDriveSize is the Dev Drive minimum (50 GB, as binary units).
const minDevDriveSize = 50 << 30

// driveLetter is the accepted --letter value; C: is the system drive and
// A:/B: are reserved.
var driveLetter = regexp.MustCompile(`^[D-Za-z]$`)

// autoAttachWarning is printed while attach-at-boot is not configured.
const autoAttachWarning = "auto-attach not configured; run `cowflow devdrive attach` after reboot"

func newInitCmd(ios IOStreams, deps Deps, st *runState) *cobra.Command {
	var o initDevDriveOpts
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize COWFlow in the current repository",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			f := cmd.Flags()
			// .mcp.json, the lead token, --provider and --conventions
			// belong to S3-E3; only --devdrive is implemented now.
			if !f.Changed("devdrive") || f.Changed("provider") || f.Changed("conventions") {
				return &ExitError{Code: exitNotImplemented, Err: errNotImplemented}
			}
			in := devDriveInit{
				ios:    ios,
				deps:   deps,
				st:     st,
				layout: devdriveLayout,
				// PENDING SPEC-GAPS U-PLAT-5: Enable reports
				// ErrAutoAttachUndecided until the mechanism is decided.
				attacher: platform.NewAutoAttacher(),
			}
			return in.run(cmd.Context(), o)
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.size, "devdrive", "", "provision a Dev Drive VHDX of this size (e.g. 50GB)")
	// Defaults of --path and --letter: PENDING SPEC-GAPS U-PLAT-9. Until
	// decided both are required with --devdrive.
	f.StringVar(&o.path, "path", "", "VHDX file path for --devdrive")
	f.StringVar(&o.letter, "letter", "", "drive letter for --devdrive")
	// §14 omits "container" here although config allows it: PENDING SPEC-GAPS U-API-10.
	f.String("provider", "", "workspace provider: auto|cow_clone|projfs|git_worktree")
	f.String("conventions", "", "apply a conventions preset (e.g. gitflow)")
	return cmd
}

// initDevDriveOpts are the flags of `init --devdrive`.
type initDevDriveOpts struct {
	size, path, letter string
}

// devDriveInit runs `init --devdrive` (SCOPE.md §6.4 steps 1-7).
type devDriveInit struct {
	ios      IOStreams
	deps     Deps
	st       *runState
	layout   func(mount string) map[string]string
	attacher platform.AutoAttacher // nil: auto-attach not available
}

// initDevDriveResult is the --json output of `init --devdrive`.
type initDevDriveResult struct {
	DevDrive      devDriveView `json:"devdrive"`
	ConfigWritten string       `json:"config_written"`
	Warnings      []string     `json:"warnings"`
}

// spec validates the flags and builds the DevDriveSpec (step 2.1).
func (o initDevDriveOpts) spec() (platform.DevDriveSpec, error) {
	size, err := parseSize(o.size)
	if err != nil {
		return platform.DevDriveSpec{}, err
	}
	if size < minDevDriveSize {
		return platform.DevDriveSpec{}, usageErr(fmt.Errorf("--devdrive %s is below the Dev Drive minimum of 50GB", o.size))
	}
	if o.path == "" || o.letter == "" {
		return platform.DevDriveSpec{}, usageErr(errors.New(
			"--path and --letter are required with --devdrive (their defaults are pending SPEC-GAPS U-PLAT-9)"))
	}
	if !driveLetter.MatchString(o.letter) {
		return platform.DevDriveSpec{}, usageErr(fmt.Errorf("--letter %q: must be one drive letter D to Z", o.letter))
	}
	if !strings.EqualFold(filepath.Ext(o.path), ".vhdx") {
		return platform.DevDriveSpec{}, usageErr(fmt.Errorf("--path %q: must end in .vhdx", o.path))
	}
	p, err := platform.Canonical(o.path)
	if err != nil {
		return platform.DevDriveSpec{}, usageErr(fmt.Errorf("--path %q: %w", o.path, err))
	}
	return platform.DevDriveSpec{
		VHDXPath:  p,
		SizeBytes: size,
		Mount:     strings.ToUpper(o.letter) + ":",
	}, nil
}

func (in devDriveInit) run(ctx context.Context, o initDevDriveOpts) error {
	if in.deps.Host == nil || in.deps.DevDrive == nil || in.deps.ConfigPath == nil {
		return errDevDriveNotWired
	}
	spec, err := o.spec()
	if err != nil {
		return err
	}
	info, err := checkDevDriveHost(ctx, in.deps.Host)
	if err != nil {
		return err
	}
	// PENDING SPEC-GAPS U-API-1 (config.toml location) and U-PLAT-10
	// (whether SQLite also records the Dev Drive): the registration goes
	// to the config file at Deps.ConfigPath only. It is resolved before
	// anything is provisioned so a missing location fails early.
	cfgPath, err := in.deps.ConfigPath()
	if err != nil {
		return &ExitError{Code: ExitState, Err: fmt.Errorf("config location: %w", err)}
	}

	if !info.Elevated {
		// Step 1/3: elevate once. The elevated child performs steps 4-7;
		// its console closes on exit, so the parent prints the result.
		if err := elevateOnce(ctx, in.ios, in.deps, in.st); err != nil {
			return err
		}
		status, err := in.deps.DevDrive.Status(ctx, spec)
		if err != nil {
			return devDriveErr(fmt.Errorf("devdrive status: %w", err))
		}
		// PENDING SPEC-GAPS U-PLAT-5: while the mechanism is undecided the
		// child's auto-attach outcome is always "not configured", so the
		// parent can report it. Once it is decided, the parent must learn
		// the real outcome (e.g. from config auto_attach) instead.
		return in.print(status, cfgPath, in.warnings(spec, platform.ErrAutoAttachUndecided))
	}
	return in.provision(ctx, spec, cfgPath)
}

// provision runs steps 4-7 in an elevated process.
func (in devDriveInit) provision(ctx context.Context, spec platform.DevDriveSpec, cfgPath string) error {
	// Load (or start) the config before provisioning, so a broken config
	// fails before any disk is created.
	cfg, err := loadOrDefault(cfgPath)
	if err != nil {
		return &ExitError{Code: ExitState, Err: err}
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o750); err != nil {
		return &ExitError{Code: ExitState, Err: fmt.Errorf("config directory: %w", err)}
	}

	// Step 4. On failure nothing is written: config never points at a
	// disk that does not exist, and NTFS is never used silently (§6.4).
	status, err := in.deps.DevDrive.Provision(ctx, spec)
	if err != nil {
		return devDriveErr(fmt.Errorf("provision Dev Drive: %w", err))
	}
	// From here the disk exists; errors name it so the user can recover.
	provisioned := func(err error) error {
		return fmt.Errorf("the Dev Drive %s is mounted at %s but %w", spec.VHDXPath, spec.Mount, err)
	}

	// Step 6: workspace.root and cache.* inside the volume.
	layout := in.layout(spec.Mount)
	if err := applyLayout(cfg, layout); err != nil {
		return provisioned(err)
	}
	for _, dir := range sortedValues(layout) {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return provisioned(fmt.Errorf("creating %s failed: %w", dir, err))
		}
	}

	// Step 5: auto-attach at boot (mechanism PENDING U-PLAT-5) and
	// registration in [devdrive].
	aaErr := platform.ErrAutoAttachUndecided
	if in.attacher != nil {
		aaErr = in.attacher.Enable(ctx, spec)
	}
	cfg.DevDrive = config.DevDrive{
		Enabled:    true,
		Path:       spec.VHDXPath,
		Mount:      spec.Mount,
		SizeGB:     int(spec.SizeBytes >> 30),
		AutoAttach: aaErr == nil,
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		return provisioned(fmt.Errorf("writing the config failed: %w", err))
	}
	return in.print(status, cfgPath, in.warnings(spec, aaErr))
}

// warnings lists what the user must know after init: the auto-attach
// outcome (S0-T17) and the Defender exclusion suggestion (step 7). Whether
// init must configure the exclusion itself is PENDING SPEC-GAPS C14; it
// only prints the command.
func (in devDriveInit) warnings(spec platform.DevDriveSpec, autoAttach error) []string {
	var w []string
	if autoAttach != nil {
		w = append(w, autoAttachWarning)
		if !errors.Is(autoAttach, platform.ErrAutoAttachUndecided) {
			w = append(w, "auto-attach: "+autoAttach.Error())
		}
	}
	// spec.Mount is a validated drive letter, so the quoting is safe.
	w = append(w, fmt.Sprintf("Suggested (run as administrator): Add-MpPreference -ExclusionPath '%s\\'", spec.Mount))
	return w
}

func (in devDriveInit) print(status platform.DevDriveStatus, cfgPath string, warnings []string) error {
	res := initDevDriveResult{DevDrive: newDevDriveView(status), ConfigWritten: cfgPath, Warnings: warnings}
	return printResult(in.ios, in.st.json, res, func(w io.Writer) error {
		if err := writeDevDriveStatus(w, res.DevDrive); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "Config written: %s\n", res.ConfigWritten); err != nil {
			return err
		}
		for _, s := range res.Warnings {
			if _, err := fmt.Fprintln(w, s); err != nil {
				return err
			}
		}
		return nil
	})
}

// loadOrDefault loads the config at path, or returns the defaults when the
// file does not exist yet.
func loadOrDefault(path string) (*config.Config, error) {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return config.Default(), nil
	} else if err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	return config.Load(path)
}

// devdriveLayout maps the config keys init --devdrive writes inside the
// volume (SCOPE.md §6.4 step 6) to their directories under mount.
//
// PENDING SPEC-GAPS U-API-11 (whether §22's X:/ws, X:/cache/... are
// defaults or examples), C7 (workspace.golden_root) and C13 (cache.gradle_home,
// cache.pip_cache): the table stays empty until they are decided, so init
// writes no workspace.root or cache.* value yet.
func devdriveLayout(mount string) map[string]string {
	_ = mount // the entries will be paths under mount
	return map[string]string{}
}

// layoutKeys are the config keys a devdriveLayout entry may set.
var layoutKeys = map[string]func(*config.Config, string){
	"workspace.root":        func(c *config.Config, v string) { c.Workspace.Root = v },
	"workspace.golden_root": func(c *config.Config, v string) { c.Workspace.GoldenRoot = v }, // PENDING C7
	"cache.pnpm_store":      func(c *config.Config, v string) { c.Cache.PnpmStore = v },
	"cache.go_modcache":     func(c *config.Config, v string) { c.Cache.GoModCache = v },
	"cache.go_cache":        func(c *config.Config, v string) { c.Cache.GoCache = v },
	"cache.cargo_target":    func(c *config.Config, v string) { c.Cache.CargoTarget = v },
	"cache.gradle_home":     func(c *config.Config, v string) { c.Cache.GradleHome = v }, // PENDING C13
	"cache.pip_cache":       func(c *config.Config, v string) { c.Cache.PipCache = v },   // PENDING C13
}

// applyLayout sets each layout key in c.
func applyLayout(c *config.Config, layout map[string]string) error {
	for key, dir := range layout {
		set, ok := layoutKeys[key]
		if !ok {
			return fmt.Errorf("layout key %q is not a workspace or cache setting", key)
		}
		set(c, dir)
	}
	return nil
}

// sortedValues returns the values of m in key order.
func sortedValues(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	vals := make([]string, 0, len(keys))
	for _, k := range keys {
		vals = append(vals, m[k])
	}
	return vals
}
