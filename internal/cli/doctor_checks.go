package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/deverity/cowflow/internal/config"
	"github.com/deverity/cowflow/internal/git"
	"github.com/deverity/cowflow/internal/platform"
)

const (
	fixInitDevDrive = "cowflow init --devdrive 50GB"
)

// single wraps one result as the slice a doctorCheck returns.
func single(name string, st CheckStatus, detail, fix string) []CheckResult {
	return []CheckResult{{Name: name, Status: st, Detail: detail, Fix: fix}}
}

// checkConfig loads config.toml. A missing file or an undecided location is
// a warning: doctor then reports against config.Default().
func checkConfig(_ context.Context, e *doctorEnv) []CheckResult {
	const name = "config.found"
	if e.deps.ConfigPath == nil {
		return single(name, StatusWarn, "no config path available; using defaults", "cowflow init")
	}
	path, err := e.deps.ConfigPath()
	if err != nil {
		return single(name, StatusWarn, err.Error()+"; using defaults", "")
	}
	cfg, err := config.Load(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return single(name, StatusWarn, "no config at "+path+"; using defaults", "cowflow init")
	case err != nil:
		return single(name, StatusFail, err.Error(), "")
	}
	e.cfg = cfg
	return single(name, StatusOK, path, "")
}

// checkGitVersion enforces git >= 2.40 (§3.1).
func checkGitVersion(ctx context.Context, e *doctorEnv) []CheckResult {
	const name = "git.version"
	if e.deps.Git == nil {
		return single(name, StatusFail, "git is not available", "")
	}
	v, err := git.CheckVersion(ctx, e.deps.Git)
	switch {
	case errors.Is(err, git.ErrVersionTooOld):
		return single(name, StatusFail, err.Error()+"; COWFlow needs git >= 2.40", "")
	case err != nil:
		return single(name, StatusFail, err.Error(), "")
	}
	return single(name, StatusOK, v.Raw, "")
}

// checkOSVersion flags Windows builds without Dev Drive (§23 risk).
func checkOSVersion(_ context.Context, e *doctorEnv) []CheckResult {
	const name = "os.version"
	switch {
	case e.osErr != nil:
		return single(name, StatusFail, "cannot read OS information: "+e.osErr.Error(), "")
	case e.osInfo.GOOS == "windows" && e.osInfo.Build < minDevDriveBuild:
		return single(name, StatusWarn,
			fmt.Sprintf("Windows build %d is older than %d (Windows 11 22H2): Dev Drive unavailable; git_worktree fallback",
				e.osInfo.Build, minDevDriveBuild), "")
	case e.osInfo.GOOS == "windows":
		return single(name, StatusOK, fmt.Sprintf("Windows build %d", e.osInfo.Build), "")
	}
	return single(name, StatusOK, e.osInfo.GOOS, "")
}

// checkDevDrive reports whether the configured Dev Drive is mounted. A
// configured but unmounted drive is a failure whose fix attaches it; NTFS is
// never suggested as a substitute (§6.4, §18).
func checkDevDrive(ctx context.Context, e *doctorEnv) []CheckResult {
	const name = "devdrive.mounted"
	if e.goos() != "windows" {
		return single(name, StatusSkip, "Dev Drive is Windows-only", "")
	}
	dd := e.cfg.DevDrive
	if !dd.Enabled {
		if e.osErr == nil && e.osInfo.Build < minDevDriveBuild {
			return single(name, StatusWarn, "no Dev Drive configured and this Windows build cannot host one", "")
		}
		return single(name, StatusWarn, "no Dev Drive configured; workspaces need one for copy-on-write", fixInitDevDrive)
	}
	if e.deps.DevDrive == nil {
		return single(name, StatusFail, "Dev Drive support is unavailable", "")
	}
	spec := platform.DevDriveSpec{VHDXPath: dd.Path, Mount: dd.Mount}
	if dd.SizeGB > 0 {
		spec.SizeBytes = uint64(dd.SizeGB) << 30
	}
	st, err := e.deps.DevDrive.Status(ctx, spec)
	switch {
	case err != nil:
		return single(name, StatusFail, "cannot read Dev Drive status: "+err.Error(), "cowflow devdrive status")
	case !st.Exists:
		return single(name, StatusFail, "Dev Drive VHDX "+dd.Path+" does not exist", "cowflow devdrive create")
	case !st.Attached:
		return single(name, StatusFail, "Dev Drive VHDX "+dd.Path+" is not attached", "cowflow devdrive attach")
	case st.Volume == nil:
		return single(name, StatusFail, "Dev Drive VHDX is attached but no volume is mounted at "+dd.Mount, "cowflow devdrive attach")
	case !strings.EqualFold(st.Volume.Filesystem, "ReFS"):
		return single(name, StatusFail,
			fmt.Sprintf("volume at %s is %s, expected ReFS", dd.Mount, st.Volume.Filesystem), "")
	case !st.Volume.DevDrive:
		return single(name, StatusWarn, "ReFS volume at "+dd.Mount+" is not flagged as a Dev Drive", "")
	}
	return single(name, StatusOK, "mounted at "+dd.Mount+" (ReFS, Dev Drive)", "")
}

// checkCOW probes the volume that holds workspace.root (the cwd only when no
// root is configured) and reports what the probe says.
func checkCOW(ctx context.Context, e *doctorEnv) []CheckResult {
	const name = "fs.cow"
	path := e.cfg.Workspace.Root
	if path == "" {
		path = e.cwd
	}
	if path == "" {
		return single(name, StatusFail, "no workspace root configured and the working directory is unknown", "")
	}
	v, err := e.deps.Host.Volume(ctx, path)
	if err != nil {
		return single(name, StatusFail, fmt.Sprintf("cannot probe the volume of %s: %v", path, err), "")
	}
	fsName := v.Filesystem
	switch {
	case strings.EqualFold(fsName, "ReFS") && v.BlockClone:
		if v.DevDrive {
			return single(name, StatusOK, "copy-on-write: block clone (ReFS Dev Drive)", "")
		}
		return single(name, StatusOK, "copy-on-write: block clone (ReFS)", "")
	case strings.EqualFold(fsName, "ReFS"):
		return single(name, StatusWarn, "ReFS without block clone: no COW (git_worktree fallback)", "")
	case strings.EqualFold(fsName, "NTFS"):
		return single(name, StatusWarn, "NTFS: no COW (git_worktree fallback)", "")
	case v.BlockClone:
		return single(name, StatusOK, fmt.Sprintf("copy-on-write: block clone (%s)", fsName), "")
	}
	return single(name, StatusWarn, fmt.Sprintf("%s: no block clone reported (git_worktree fallback)", fsName), "")
}

// checkProjFS runs only when ProjFS is configured (§7): it is never chosen
// automatically (§6.3).
func checkProjFS(ctx context.Context, e *doctorEnv) []CheckResult {
	const name = "projfs.feature"
	explicit := e.cfg.Workspace.Provider == "projfs"
	if !explicit && !e.cfg.Workspace.ProjFS.Enabled {
		return nil
	}
	ok, err := e.deps.Host.ProjFSAvailable(ctx)
	switch {
	case err != nil:
		return single(name, StatusFail, "cannot probe ProjFS: "+err.Error(), "")
	case ok:
		return single(name, StatusOK, "Windows Projected File System is available", "")
	}
	st := StatusWarn
	if explicit {
		st = StatusFail
	}
	return single(name, st, "ProjFS is configured but the Windows optional feature Client-ProjFS is not enabled", "")
}

// skipRepo is the result of a repository check outside a repository.
func (e *doctorEnv) skipRepo(name string) []CheckResult {
	return single(name, StatusSkip, e.repoMsg, "")
}

func checkLongPaths(ctx context.Context, e *doctorEnv) []CheckResult {
	const name = "git.longpaths"
	if e.goos() != "windows" {
		return single(name, StatusSkip, "only needed on Windows", "")
	}
	if !e.inRepo {
		return e.skipRepo(name)
	}
	v, found, err := git.ConfigGet(ctx, e.deps.Git, e.repoDir, "core.longpaths")
	switch {
	case err != nil:
		return single(name, StatusFail, "cannot read core.longpaths: "+err.Error(), "")
	case found && strings.EqualFold(v, "true"):
		return single(name, StatusOK, "core.longpaths = true", "")
	case !found:
		return single(name, StatusWarn, "core.longpaths is not set", "git config core.longpaths true")
	}
	return single(name, StatusWarn, "core.longpaths = "+v+", expected true", "git config core.longpaths true")
}

func checkAutoCRLF(ctx context.Context, e *doctorEnv) []CheckResult {
	const name = "git.autocrlf"
	if !e.inRepo {
		return e.skipRepo(name)
	}
	v, found, err := git.ConfigGet(ctx, e.deps.Git, e.repoDir, "core.autocrlf")
	switch {
	case err != nil:
		return single(name, StatusFail, "cannot read core.autocrlf: "+err.Error(), "")
	case found && strings.EqualFold(v, "false"):
		return single(name, StatusOK, "core.autocrlf = false", "")
	case !found:
		return single(name, StatusWarn, "core.autocrlf is not set, expected false", "git config core.autocrlf false")
	}
	return single(name, StatusWarn, "core.autocrlf = "+v+", expected false", "git config core.autocrlf false")
}

// checkGitattributes looks for a `* text=auto eol=lf` line when
// git.require_gitattributes is set (§7).
func checkGitattributes(_ context.Context, e *doctorEnv) []CheckResult {
	const name = "git.gitattributes"
	if !e.cfg.Git.RequireGitattrs {
		return single(name, StatusSkip, "git.require_gitattributes is not set", "")
	}
	if !e.inRepo {
		return e.skipRepo(name)
	}
	path := filepath.Join(e.repoDir, ".gitattributes")
	f, err := os.Open(path) //nolint:gosec // G304: fixed file name under the repository root
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return single(name, StatusWarn, ".gitattributes does not exist; expected the line \"* text=auto eol=lf\"", "")
		}
		return single(name, StatusFail, "cannot read .gitattributes: "+err.Error(), "")
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) > 0 && fields[0] == "*" && slices.Contains(fields, "text=auto") && slices.Contains(fields, "eol=lf") {
			return single(name, StatusOK, ".gitattributes sets * text=auto eol=lf", "")
		}
	}
	if err := sc.Err(); err != nil {
		return single(name, StatusFail, "cannot read .gitattributes: "+err.Error(), "")
	}
	return single(name, StatusWarn, ".gitattributes lacks the line \"* text=auto eol=lf\"", "")
}
