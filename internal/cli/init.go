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

// Defaults of `init --devdrive` (SCOPE.md §6.4 step 4).
const (
	defaultVHDXPath = `C:\cf\cowflow.vhdx`
	defaultLetter   = "X"
)

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
			in := devDriveInit{ios: ios, deps: deps, st: st, layout: devdriveLayout}
			return in.run(cmd.Context(), o)
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.size, "devdrive", "", "provision a Dev Drive VHDX of this size (e.g. 50GB)")
	// Internal: set by the non-elevated init on its elevated child, which may
	// run under other credentials, so the auto-attach task triggers at the
	// logon of the user who ran init (SPEC-GAPS U-PLAT-5). platform
	// validates the DOMAIN\user form.
	f.StringVar(&o.attachUser, autoAttachUserFlag, "", "user whose logon attaches the Dev Drive (set internally)")
	_ = f.MarkHidden(autoAttachUserFlag) // fails only for an undefined flag; it is defined just above
	// Fixed defaults (SPEC-GAPS U-PLAT-9): nothing is chosen automatically;
	// a taken letter or an existing file is a conflict naming the flag.
	f.StringVar(&o.path, "path", defaultVHDXPath, "VHDX file path for --devdrive")
	f.StringVar(&o.letter, "letter", defaultLetter, "drive letter for --devdrive (one letter D to Z)")
	// Exactly these four (SPEC-GAPS U-API-10): "container" is config-only and
	// valid only in a container-tagged build.
	f.String("provider", "", "workspace provider: auto|cow_clone|projfs|git_worktree")
	f.String("conventions", "", "apply a conventions preset (e.g. gitflow)")
	return cmd
}

// autoAttachUserFlag is the hidden flag that carries the user who ran init
// to the elevated child.
const autoAttachUserFlag = "auto-attach-user"

// initDevDriveOpts are the flags of `init --devdrive`.
type initDevDriveOpts struct {
	size, path, letter string
	attachUser         string // DOMAIN\user who ran init; "" in a direct run
}

// devDriveInit runs `init --devdrive` (SCOPE.md §6.4 steps 1-7).
type devDriveInit struct {
	ios    IOStreams
	deps   Deps
	st     *runState
	layout func(mount string) map[string]string
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
	// The Dev Drive is registered in [devdrive] of
	// <project root>/.cowflow/config.toml only, with no SQLite row
	// (SPEC-GAPS U-API-1, U-PLAT-10). The location is resolved before
	// anything is provisioned so a failure stops early.
	cfgPath, err := in.deps.ConfigPath()
	if err != nil {
		return &ExitError{Code: ExitState, Err: fmt.Errorf("config location: %w", err)}
	}
	// Before the UAC prompt: a taken letter or an existing VHDX is a
	// conflict the user resolves with a flag.
	if err := in.checkConflicts(spec); err != nil {
		return err
	}

	if !info.Elevated {
		// Step 1/3: elevate once. The elevated child performs steps 4-7;
		// its console closes on exit, so the parent prints the result.
		if err := elevateOnce(ctx, in.ios, in.deps, in.childArgs(o)); err != nil {
			return err
		}
		status, err := in.deps.DevDrive.Status(ctx, spec)
		if err != nil {
			return devDriveErr(fmt.Errorf("devdrive status: %w", err))
		}
		// The child recorded whether auto-attach was registered in
		// devdrive.auto_attach; report that real outcome.
		attached, detail := savedAutoAttach(cfgPath, spec)
		return in.print(status, cfgPath, in.warnings(spec, attached, detail))
	}
	return in.provision(ctx, spec, cfgPath, o.attachUser)
}

// childArgs is the command line of the elevated child: the current one,
// plus the user who ran init unless it already names one. When that user
// cannot be read, the child falls back to its own token's user.
func (in devDriveInit) childArgs(o initDevDriveOpts) []string {
	args := append([]string(nil), in.st.args...)
	if o.attachUser != "" || in.deps.CurrentUser == nil {
		return args
	}
	u, err := in.deps.CurrentUser()
	if err != nil || u == "" {
		return args
	}
	return append(args, "--"+autoAttachUserFlag, u)
}

// savedAutoAttach reads devdrive.auto_attach back from the config the
// elevated child saved. detail explains a failed read.
func savedAutoAttach(cfgPath string, spec platform.DevDriveSpec) (attached bool, detail string) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return false, "cannot read the result back: " + err.Error()
	}
	if !cfg.DevDrive.Enabled || cfg.DevDrive.Path != spec.VHDXPath {
		return false, "the elevated process did not record the Dev Drive in " + cfgPath
	}
	return cfg.DevDrive.AutoAttach, ""
}

// checkConflicts fails with exit 3 when the VHDX file exists or the drive
// letter is taken (SPEC-GAPS U-PLAT-9): nothing is chosen automatically.
func (in devDriveInit) checkConflicts(spec platform.DevDriveSpec) error {
	switch _, err := os.Lstat(spec.VHDXPath); {
	case err == nil:
		return &ExitError{Code: ExitConflict, Err: fmt.Errorf(
			"%s already exists; pass --path to choose another VHDX file", spec.VHDXPath)}
	case !errors.Is(err, fs.ErrNotExist):
		return &ExitError{Code: ExitState, Err: fmt.Errorf("vhdx path: %w", err)}
	}
	letter := spec.Mount[:1]
	inUse := in.deps.LetterInUse
	if inUse == nil {
		inUse = letterRootExists
	}
	taken, err := inUse(letter)
	if err != nil {
		return &ExitError{Code: ExitState, Err: fmt.Errorf("drive letter %s: %w", spec.Mount, err)}
	}
	if taken {
		return &ExitError{Code: ExitConflict, Err: fmt.Errorf(
			"drive letter %s is already in use; pass --letter to choose another one", spec.Mount)}
	}
	return nil
}

// letterRootExists reports whether the root directory of drive letter
// exists, which on Windows means a volume is mounted there. Any other error
// (e.g. an empty card reader: "device not ready") still means the letter is
// held. platform checks the drive letters again before provisioning.
func letterRootExists(letter string) (bool, error) {
	_, err := os.Stat(letter + `:\`)
	return !errors.Is(err, fs.ErrNotExist), nil
}

// provision runs steps 4-7 in an elevated process. attachUser is the user
// who ran init ("" for the current token's user).
func (in devDriveInit) provision(ctx context.Context, spec platform.DevDriveSpec, cfgPath, attachUser string) error {
	// Load (or start) the config before provisioning, so a broken config
	// fails before any disk is created.
	cfg, err := loadOrDefault(cfgPath)
	if err != nil {
		return configErr(err)
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
	mkdir := in.deps.MkdirAll
	if mkdir == nil {
		mkdir = os.MkdirAll
	}
	for _, dir := range sortedValues(layout) {
		if err := mkdir(filepath.FromSlash(dir), 0o750); err != nil {
			return provisioned(fmt.Errorf("creating %s failed: %w", dir, err))
		}
	}

	// Step 5: auto-attach at boot (a Task Scheduler task running `cowflow
	// devdrive attach` from the project root at logon of the user who ran
	// init, SPEC-GAPS U-PLAT-5) and registration in [devdrive].
	// auto_attach is true only when the registration succeeded; a failure
	// is a warning with the manual fallback.
	aaErr := in.enableAutoAttach(ctx, spec, cfgPath, attachUser)
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
	detail := ""
	if aaErr != nil {
		detail = aaErr.Error()
	}
	return in.print(status, cfgPath, in.warnings(spec, aaErr == nil, detail))
}

// enableAutoAttach registers the auto-attach task. Its working directory is
// the project root (the directory above .cowflow), from which `devdrive
// attach` locates config.toml; an empty user means the current token's.
func (in devDriveInit) enableAutoAttach(ctx context.Context, spec platform.DevDriveSpec, cfgPath, user string) error {
	if in.deps.NewAutoAttacher == nil {
		return platform.ErrUnsupported
	}
	t := platform.AutoAttachTarget{WorkDir: projectRoot(cfgPath), User: user}
	return in.deps.NewAutoAttacher(t).Enable(ctx, spec)
}

// projectRoot is the directory that holds .cowflow/config.toml.
func projectRoot(cfgPath string) string { return filepath.Dir(filepath.Dir(cfgPath)) }

// warnings lists what the user must know after init: the auto-attach
// outcome (detail says why it is not configured, when known) and the
// Defender exclusion, which init only suggests and never applies
// (SPEC-GAPS C14).
func (in devDriveInit) warnings(spec platform.DevDriveSpec, autoAttached bool, detail string) []string {
	var w []string
	if !autoAttached {
		w = append(w, autoAttachWarning)
		if detail != "" {
			w = append(w, "auto-attach: "+detail)
		}
	}
	w = append(w, "Suggested (run as administrator): "+defenderExclusionCmd(spec.Mount))
	return w
}

// defenderExclusionCmd is the PowerShell command that excludes the Dev
// Drive from Defender scans. mount is a validated drive letter ("X:"), so
// the quoting is safe.
func defenderExclusionCmd(mount string) string {
	return fmt.Sprintf("Add-MpPreference -ExclusionPath '%s\\'", mount)
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

// configErr maps a config.Load failure to its exit code (SPEC-GAPS
// U-API-12): a missing or unreadable file is state (2); a file that does
// not parse or validate is wrong input (1).
func configErr(err error) error {
	var pe *fs.PathError
	if errors.Is(err, fs.ErrNotExist) || errors.As(err, &pe) {
		return &ExitError{Code: ExitState, Err: err}
	}
	return &ExitError{Code: ExitUsage, Err: err}
}

// devdriveLayout maps the config keys init --devdrive writes inside the
// volume (SCOPE.md §6.4 step 6, §6.7) to their directories under mount,
// with forward slashes (SPEC-GAPS U-API-11, C7, C13). daemon.state_dir is
// never part of it: it derives from the config location.
func devdriveLayout(mount string) map[string]string {
	return map[string]string{
		"workspace.root":        mount + "/ws",
		"workspace.golden_root": mount + "/golden",
		"cache.pnpm_store":      mount + "/cache/pnpm",
		"cache.go_modcache":     mount + "/cache/gomod",
		"cache.go_cache":        mount + "/cache/gobuild",
		"cache.cargo_target":    mount + "/cache/cargo-target",
		"cache.gradle_home":     mount + "/cache/gradle",
		"cache.pip_cache":       mount + "/cache/pip",
	}
}

// layoutKeys are the config keys a devdriveLayout entry may set.
var layoutKeys = map[string]func(*config.Config, string){
	"workspace.root":        func(c *config.Config, v string) { c.Workspace.Root = v },
	"workspace.golden_root": func(c *config.Config, v string) { c.Workspace.GoldenRoot = v },
	"cache.pnpm_store":      func(c *config.Config, v string) { c.Cache.PnpmStore = v },
	"cache.go_modcache":     func(c *config.Config, v string) { c.Cache.GoModCache = v },
	"cache.go_cache":        func(c *config.Config, v string) { c.Cache.GoCache = v },
	"cache.cargo_target":    func(c *config.Config, v string) { c.Cache.CargoTarget = v },
	"cache.gradle_home":     func(c *config.Config, v string) { c.Cache.GradleHome = v },
	"cache.pip_cache":       func(c *config.Config, v string) { c.Cache.PipCache = v },
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
