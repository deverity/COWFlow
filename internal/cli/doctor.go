package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/deverity/cowflow/internal/config"
	"github.com/deverity/cowflow/internal/git"
	"github.com/deverity/cowflow/internal/platform"
)

// CheckStatus is the outcome of one doctor check.
type CheckStatus string

// Check statuses. Only fail changes the exit code.
const (
	StatusOK   CheckStatus = "ok"
	StatusWarn CheckStatus = "warn"
	StatusFail CheckStatus = "fail"
	StatusSkip CheckStatus = "skip"
)

// CheckResult is one line of the doctor report (Contracts §6.8).
type CheckResult struct {
	Name   string      `json:"name"`
	Status CheckStatus `json:"status"`
	Detail string      `json:"detail"`
	Fix    string      `json:"fix,omitempty"` // exact command to run
}

// DoctorReport is the --json document of doctor.
type DoctorReport struct {
	Checks []CheckResult `json:"checks"`
	OK     bool          `json:"ok"` // false when any check failed
}

// doctorCheckTimeout bounds each probe (Guidance: 10 s).
var doctorCheckTimeout = 10 * time.Second

func newDoctorCmd(ios IOStreams, deps Deps, st *runState) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check the machine and repository for COWFlow requirements",
		Long: "Check the machine and repository for COWFlow requirements.\n\n" +
			"doctor only reads: it never changes the system, the repository or the configuration.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rep := runDoctor(cmd.Context(), deps)
			err := printResult(ios, st.json, rep, func(w io.Writer) error { return renderDoctor(w, rep) })
			if err != nil {
				return err
			}
			if !rep.OK {
				return &ExitError{Code: exitDoctorFailed, Err: fmt.Errorf("doctor: %d check(s) failed", countStatus(rep.Checks, StatusFail))}
			}
			return nil
		},
	}
}

// doctorEnv is what the checks share. Gathering it is itself probed with the
// same timeout; a probe that fails is recorded, never fatal.
type doctorEnv struct {
	deps Deps
	cfg  *config.Config

	cwd    string
	osInfo platform.OSInfo
	osErr  error

	inRepo  bool
	repoDir string
	repoMsg string // why repo checks are skipped
}

// goos returns the host OS, falling back to the running one when the probe
// failed.
func (e *doctorEnv) goos() string {
	if e.osErr != nil || e.osInfo.GOOS == "" {
		return runtime.GOOS
	}
	return e.osInfo.GOOS
}

type doctorCheck func(ctx context.Context, e *doctorEnv) []CheckResult

// runDoctor runs every check independently, each under its own timeout.
func runDoctor(ctx context.Context, deps Deps) DoctorReport {
	if deps.Host == nil {
		deps.Host = platform.NewHost()
	}
	env := &doctorEnv{deps: deps}

	var checks []CheckResult
	checks = append(checks, withTimeout(ctx, env, checkConfig)...) // sets env.cfg
	if env.cfg == nil {
		env.cfg = config.Default()
	}
	withTimeout(ctx, env, gatherHost)
	checks = append(checks, withTimeout(ctx, env, checkGitVersion)...)
	withTimeout(ctx, env, detectRepo)

	for _, c := range []doctorCheck{
		checkOSVersion,
		checkDevDrive,
		checkCOW,
		checkProjFS,
		checkLongPaths,
		checkAutoCRLF,
		checkGitattributes,
		checkDefenderExclusion,
	} {
		checks = append(checks, withTimeout(ctx, env, c)...)
	}

	rep := DoctorReport{Checks: checks, OK: true}
	if rep.Checks == nil {
		rep.Checks = []CheckResult{}
	}
	if countStatus(rep.Checks, StatusFail) > 0 {
		rep.OK = false
	}
	return rep
}

func withTimeout(ctx context.Context, e *doctorEnv, c doctorCheck) []CheckResult {
	ctx, cancel := context.WithTimeout(ctx, doctorCheckTimeout)
	defer cancel()
	return c(ctx, e)
}

func countStatus(checks []CheckResult, s CheckStatus) int {
	n := 0
	for _, c := range checks {
		if c.Status == s {
			n++
		}
	}
	return n
}

// gatherHost records the working directory and the OS information.
func gatherHost(ctx context.Context, e *doctorEnv) []CheckResult {
	e.cwd, _ = os.Getwd()
	e.osInfo, e.osErr = e.deps.Host.OS(ctx)
	return nil
}

// detectRepo finds the repository root with `git rev-parse --show-toplevel`.
// Outside a repository the repository checks are skipped, not failed: doctor
// must stay useful before init.
func detectRepo(ctx context.Context, e *doctorEnv) []CheckResult {
	if e.deps.Git == nil {
		e.repoMsg = "git is unavailable"
		return nil
	}
	out, err := e.deps.Git.Run(ctx, "", "rev-parse", "--show-toplevel")
	if err != nil {
		var ge *git.Error
		if errors.As(err, &ge) && ge.ExitCode == 128 {
			e.repoMsg = "not a Git repository"
		} else {
			e.repoMsg = "cannot detect the repository: " + err.Error()
		}
		return nil
	}
	e.repoDir = strings.TrimSpace(string(out))
	e.inRepo = e.repoDir != ""
	if !e.inRepo {
		e.repoMsg = "not a Git repository"
	}
	return nil
}

// renderDoctor writes the human table: CHECK STATUS DETAIL, with an
// indented fix line under the checks that have one.
func renderDoctor(w io.Writer, rep DoctorReport) error {
	nameW, statusW := len("CHECK"), len("STATUS")
	for _, c := range rep.Checks {
		nameW = max(nameW, len(c.Name))
		statusW = max(statusW, len(c.Status))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-*s  %-*s  %s\n", nameW, "CHECK", statusW, "STATUS", "DETAIL")
	for _, c := range rep.Checks {
		fmt.Fprintf(&b, "%-*s  %-*s  %s\n", nameW, c.Name, statusW, c.Status, c.Detail)
		if c.Fix != "" {
			fmt.Fprintf(&b, "%-*s  %-*s  fix: %s\n", nameW, "", statusW, "", c.Fix)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}
