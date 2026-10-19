package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/deverity/cowflow/internal/config"
	"github.com/deverity/cowflow/internal/platform"
)

// Exit codes of Dev Drive failures that SCOPE.md §14 does not classify.
// PENDING SPEC-GAPS U-API-12 — AG-LEAD sets the values at kickoff.
var (
	// exitNotElevated: the UAC prompt was declined, or an operation that
	// needs administrator rights ran without them.
	exitNotElevated = exitUnclassified
	// exitUnsupported: no Dev Drive on this OS or Windows build.
	exitUnsupported = exitUnclassified
)

// minDevDriveBuild is Windows 11 22H2, the first build with Dev Drive
// (SCOPE.md §6.4 step 1). platform enforces the same bound.
const minDevDriveBuild = 22621

// fallbackMessage is the explicit worktree fallback of SCOPE.md §1.2 and
// the §23 risk "Dev Drive unavailable (Win10, no admin)".
const fallbackMessage = "Dev Drive requires Windows 11; COWFlow will use git_worktree"

var (
	errDevDriveNotWired = errors.New("internal error: Dev Drive dependencies are not wired")
	errNoDevDrive       = errors.New("no Dev Drive configured: [devdrive] path and mount are not set; " +
		"run `cowflow init --devdrive <size> --path <file.vhdx> --letter <X>`")
)

// newDevDriveCmd builds `devdrive create|attach|detach|status` (SCOPE.md §14).
//
// PENDING SPEC-GAPS U-PLAT-9: the subcommands take no flags; they read the
// VHDX path, mount and size from [devdrive] in config.toml (the working
// proposal of U-PLAT-9).
func newDevDriveCmd(ios IOStreams, deps Deps, st *runState) *cobra.Command {
	return group("devdrive", "Manage the Dev Drive VHDX",
		devDriveCmd(ios, deps, st, "create", "Create the Dev Drive VHDX from [devdrive] in config.toml", true,
			func(ctx context.Context, spec platform.DevDriveSpec) (platform.DevDriveStatus, error) {
				return deps.DevDrive.Provision(ctx, spec)
			}),
		devDriveCmd(ios, deps, st, "attach", "Attach the Dev Drive VHDX", true,
			func(ctx context.Context, spec platform.DevDriveSpec) (platform.DevDriveStatus, error) {
				return deps.DevDrive.Attach(ctx, spec)
			}),
		devDriveCmd(ios, deps, st, "detach", "Detach the Dev Drive VHDX", true,
			func(ctx context.Context, spec platform.DevDriveSpec) (platform.DevDriveStatus, error) {
				if err := deps.DevDrive.Detach(ctx, spec.VHDXPath); err != nil {
					return platform.DevDriveStatus{}, err
				}
				return deps.DevDrive.Status(ctx, spec)
			}),
		devDriveCmd(ios, deps, st, "status", "Show Dev Drive status", false,
			func(ctx context.Context, spec platform.DevDriveSpec) (platform.DevDriveStatus, error) {
				return deps.DevDrive.Status(ctx, spec)
			}),
	)
}

// devDriveCmd builds one devdrive subcommand. When admin is set and the
// process is not elevated, the same command line is relaunched elevated
// once and the parent reports the resulting status itself (the elevated
// console window closes on exit).
func devDriveCmd(ios IOStreams, deps Deps, st *runState, use, short string, admin bool,
	op func(context.Context, platform.DevDriveSpec) (platform.DevDriveStatus, error),
) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if deps.Host == nil || deps.DevDrive == nil || deps.ConfigPath == nil {
				return errDevDriveNotWired
			}
			info, err := checkDevDriveHost(ctx, deps.Host)
			if err != nil {
				return err
			}
			spec, err := configuredDevDrive(deps)
			if err != nil {
				return err
			}
			var status platform.DevDriveStatus
			if admin && !info.Elevated {
				if err := elevateOnce(ctx, ios, deps, st); err != nil {
					return err
				}
				status, err = deps.DevDrive.Status(ctx, spec)
			} else {
				status, err = op(ctx, spec)
			}
			if err != nil {
				return devDriveErr(fmt.Errorf("devdrive %s: %w", use, err))
			}
			view := newDevDriveView(status)
			return printResult(ios, st.json, view, func(w io.Writer) error {
				return writeDevDriveStatus(w, view)
			})
		},
	}
}

// configuredDevDrive reads the Dev Drive spec from [devdrive] in the config
// file. Whether SQLite also records the Dev Drive is PENDING SPEC-GAPS
// U-PLAT-10; the config file location is PENDING U-API-1 (injected through
// Deps.ConfigPath).
func configuredDevDrive(deps Deps) (platform.DevDriveSpec, error) {
	path, err := deps.ConfigPath()
	if err != nil {
		return platform.DevDriveSpec{}, &ExitError{Code: ExitState, Err: fmt.Errorf("config location: %w", err)}
	}
	cfg, err := config.Load(path)
	if err != nil {
		return platform.DevDriveSpec{}, &ExitError{Code: ExitState, Err: err}
	}
	d := cfg.DevDrive
	if d.Path == "" || d.Mount == "" {
		return platform.DevDriveSpec{}, &ExitError{Code: ExitState, Err: errNoDevDrive}
	}
	if d.SizeGB < 0 {
		return platform.DevDriveSpec{}, &ExitError{Code: ExitState,
			Err: fmt.Errorf("config %s: devdrive.size_gb %d must not be negative", path, d.SizeGB)}
	}
	return platform.DevDriveSpec{
		VHDXPath:  d.Path,
		SizeBytes: uint64(d.SizeGB) << 30,
		Mount:     d.Mount,
	}, nil
}

// checkDevDriveHost enforces SCOPE.md §6.4 step 1 at the CLI: anything but
// Windows 11 22H2+ gets the explicit git_worktree fallback message, never a
// silent NTFS fallback (§1.2, §23).
func checkDevDriveHost(ctx context.Context, h platform.Host) (platform.OSInfo, error) {
	info, err := h.OS(ctx)
	if err != nil {
		return info, fmt.Errorf("os info: %w", err)
	}
	switch {
	case info.GOOS != "windows":
		return info, &ExitError{Code: exitUnsupported,
			Err: fmt.Errorf("%s (%w)", fallbackMessage, platform.ErrUnsupported)}
	case info.Build < minDevDriveBuild:
		return info, &ExitError{Code: exitUnsupported,
			Err: fmt.Errorf("%s (%w; this is build %d)", fallbackMessage, platform.ErrOSTooOld, info.Build)}
	}
	return info, nil
}

// elevateOnce relaunches the current command line elevated (one UAC prompt)
// and waits for it. Only a non-elevated process calls it; platform's
// RunElevated refuses to elevate an elevated process, so elevation cannot
// loop.
func elevateOnce(ctx context.Context, ios IOStreams, deps Deps, st *runState) error {
	if deps.Elevate == nil {
		return errDevDriveNotWired
	}
	// Diagnostics go to stderr so stdout stays one JSON document.
	if _, err := fmt.Fprintln(ios.Err, "Requesting administrator rights (one UAC prompt)…"); err != nil {
		return err
	}
	code, err := deps.Elevate(ctx, st.args)
	if err != nil {
		return devDriveErr(fmt.Errorf("elevate: %w", err))
	}
	if code != ExitOK {
		return &ExitError{Code: code, Err: fmt.Errorf(
			"the elevated cowflow process failed with exit code %d (its messages were in its own console window)", code)}
	}
	return nil
}

// devDriveErr maps platform sentinels to exit codes (S0-T18 step 4).
func devDriveErr(err error) error {
	var ee *ExitError
	switch {
	case err == nil, errors.As(err, &ee):
		return err
	case errors.Is(err, platform.ErrAlreadyExists):
		return &ExitError{Code: ExitConflict, Err: err}
	case errors.Is(err, platform.ErrNotElevated):
		return &ExitError{Code: exitNotElevated, Err: err}
	case errors.Is(err, platform.ErrOSTooOld), errors.Is(err, platform.ErrUnsupported):
		return &ExitError{Code: exitUnsupported, Err: err}
	}
	return err
}

// devDriveView is the --json shape of platform.DevDriveStatus.
type devDriveView struct {
	VHDXPath string      `json:"vhdx_path"`
	Exists   bool        `json:"exists"`
	Attached bool        `json:"attached"`
	Mount    string      `json:"mount"`
	Volume   *volumeView `json:"volume"`
}

// volumeView is the --json shape of platform.VolumeInfo.
type volumeView struct {
	Path       string `json:"path"`
	Filesystem string `json:"filesystem"`
	BlockClone bool   `json:"block_clone"`
	DevDrive   bool   `json:"dev_drive"`
}

func newDevDriveView(s platform.DevDriveStatus) devDriveView {
	v := devDriveView{VHDXPath: s.VHDXPath, Exists: s.Exists, Attached: s.Attached, Mount: s.Mount}
	if s.Volume != nil {
		v.Volume = &volumeView{
			Path:       s.Volume.Path,
			Filesystem: s.Volume.Filesystem,
			BlockClone: s.Volume.BlockClone,
			DevDrive:   s.Volume.DevDrive,
		}
	}
	return v
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// writeDevDriveStatus prints v for humans.
func writeDevDriveStatus(w io.Writer, v devDriveView) error {
	vol := "not mounted"
	if v.Volume != nil {
		vol = fmt.Sprintf("%s (Dev Drive: %s, block clone: %s)",
			v.Volume.Filesystem, yesNo(v.Volume.DevDrive), yesNo(v.Volume.BlockClone))
	}
	_, err := fmt.Fprintf(w, "Dev Drive %s\n  exists:   %s\n  attached: %s\n  mount:    %s\n  volume:   %s\n",
		v.VHDXPath, yesNo(v.Exists), yesNo(v.Attached), v.Mount, vol)
	return err
}
