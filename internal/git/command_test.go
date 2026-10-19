package git

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testTimeout = 30 * time.Second

// realRunner returns an ExecRunner on the git found on PATH, isolated from
// the machine's global and system config.
func realRunner(t *testing.T) *ExecRunner {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	r, err := NewExecRunner(ExecOptions{
		Timeout:  testTimeout,
		ExtraEnv: []string{"GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1"},
	})
	require.NoError(t, err)
	return r
}

// initRepo creates an empty repository in a temp dir.
func initRepo(t *testing.T, r Runner) string {
	t.Helper()
	repo := t.TempDir()
	_, err := r.Run(context.Background(), repo, "init", "-q")
	require.NoError(t, err)
	return repo
}

func canonical(t *testing.T, p string) string {
	t.Helper()
	c, err := filepath.EvalSymlinks(filepath.FromSlash(p))
	require.NoError(t, err)
	return c
}

func TestRun_UsesDashC(t *testing.T) {
	r := realRunner(t)
	repo := initRepo(t, r)
	t.Chdir(t.TempDir()) // the process working directory is not the repo

	out, err := r.Run(context.Background(), repo, "rev-parse", "--show-toplevel")
	require.NoError(t, err)
	assert.Equal(t, canonical(t, repo), canonical(t, strings.TrimSpace(string(out))))
}

func TestRun_Env(t *testing.T) {
	for _, k := range strippedEnv {
		t.Setenv(k, filepath.Join(t.TempDir(), "elsewhere"))
	}
	t.Setenv("GIT_TERMINAL_PROMPT", "1")
	t.Setenv("LC_ALL", "pt_BR.UTF-8")
	t.Setenv("COWFLOW_TEST_INHERITED", "kept")

	r := helperRunner(t, "env", testTimeout)
	out, err := r.Run(context.Background(), "", "anything")
	require.NoError(t, err)

	env := map[string][]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		k, v, _ := strings.Cut(strings.TrimRight(line, "\r"), "=")
		env[k] = append(env[k], v)
	}
	assert.Equal(t, []string{"0"}, env["GIT_TERMINAL_PROMPT"])
	assert.Equal(t, []string{"C"}, env["LC_ALL"])
	assert.Equal(t, []string{"never"}, env["GCM_INTERACTIVE"])
	assert.Equal(t, []string{"env"}, env[helperEnv], "ExtraEnv is passed")
	assert.Equal(t, []string{"kept"}, env["COWFLOW_TEST_INHERITED"], "other variables are inherited")
	for _, k := range strippedEnv {
		assert.NotContains(t, env, k, "%s must be stripped", k)
	}
}

func TestRun_Timeout(t *testing.T) {
	r := helperRunner(t, "sleep", 100*time.Millisecond)
	start := time.Now()
	_, err := r.Run(context.Background(), "", "anything")
	require.Error(t, err)
	assert.Less(t, time.Since(start), 20*time.Second)

	var ge *Error
	require.ErrorAs(t, err, &ge)
	assert.Equal(t, -1, ge.ExitCode)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestRun_Canceled(t *testing.T) {
	r := helperRunner(t, "sleep", testTimeout)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	_, err := r.Run(ctx, "", "anything")

	var ge *Error
	require.ErrorAs(t, err, &ge)
	assert.Equal(t, -1, ge.ExitCode)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestRun_ExitCode(t *testing.T) {
	r := realRunner(t)
	repo := initRepo(t, r)

	_, err := r.Run(context.Background(), repo, "rev-parse", "--verify", "nope")
	var ge *Error
	require.ErrorAs(t, err, &ge)
	assert.Equal(t, 128, ge.ExitCode)
	assert.Equal(t, []string{"rev-parse", "--verify", "nope"}, ge.Args, "Args exclude -C dir")
	assert.NotEmpty(t, ge.Stderr)
	var ee *exec.ExitError
	assert.ErrorAs(t, err, &ee)
}

func TestNewExecRunner(t *testing.T) {
	t.Run("missing executable", func(t *testing.T) {
		_, err := NewExecRunner(ExecOptions{
			GitPath: filepath.Join(t.TempDir(), "no-such-git"),
			Timeout: time.Second,
		})
		assert.ErrorIs(t, err, ErrNotFound)
	})
	t.Run("empty PATH", func(t *testing.T) {
		t.Setenv("PATH", "")
		_, err := NewExecRunner(ExecOptions{Timeout: time.Second})
		assert.ErrorIs(t, err, ErrNotFound)
	})
	t.Run("non-positive timeout", func(t *testing.T) {
		self, err := os.Executable()
		require.NoError(t, err)
		for _, d := range []time.Duration{0, -time.Second} {
			_, err := NewExecRunner(ExecOptions{GitPath: self, Timeout: d})
			assert.Error(t, err, "timeout %s", d)
		}
	})
}

func TestError_Error(t *testing.T) {
	t.Setenv("COWFLOW_SECRET_FOR_TEST", "s3cr3t-value")
	e := &Error{
		Args:     []string{"rev-parse", "--verify", "nope"},
		ExitCode: 128,
		Stderr:   "fatal: Needed a single revision\n",
		Err:      errors.New("exit status 128"),
	}
	msg := e.Error()
	assert.Equal(t, "git rev-parse --verify nope: exit code 128: fatal: Needed a single revision", msg)
	assert.NotContains(t, msg, "s3cr3t-value")

	timeout := &Error{Args: []string{"fetch"}, ExitCode: -1, Err: context.DeadlineExceeded}
	assert.Equal(t, "git fetch: exit code -1: context deadline exceeded", timeout.Error())
}

func TestTruncate(t *testing.T) {
	assert.Equal(t, "short", truncate("short", 10))
	long := strings.Repeat("a", 5000)
	got := truncate(long, maxStderr)
	assert.True(t, strings.HasPrefix(got, strings.Repeat("a", maxStderr)))
	assert.Contains(t, got, "[truncated]")
	// Never split a multi-byte rune.
	got = truncate("ab€", 3)
	assert.Equal(t, "ab…[truncated]", got)
}

func TestBuildEnv(t *testing.T) {
	base := []string{"PATH=/bin", "GIT_DIR=/x", "GIT_TERMINAL_PROMPT=1", "HOME=/h"}
	got := buildEnv(base, []string{"GIT_CONFIG_GLOBAL=/dev/null"})
	assert.Equal(t, []string{
		"PATH=/bin", "HOME=/h",
		"GIT_TERMINAL_PROMPT=0", "LC_ALL=C", "GCM_INTERACTIVE=never",
		"GIT_CONFIG_GLOBAL=/dev/null",
	}, got)
}

// shellPairRE matches a shell executable followed by its "run this string"
// flag, e.g. a "cmd" or "cmd.exe" literal followed by "/c", or "sh"/"bash"
// followed by "-c". Written as a regexp so this file does not match itself.
var shellPairRE = regexp.MustCompile(`(?i)"(?:cmd(?:\.exe)?)"\s*,\s*"/c"|"(?:ba)?sh"\s*,\s*"-c"`)

func TestNoShellInvocations(t *testing.T) {
	q := `"`
	for _, sample := range []string{
		q + "cmd" + q + ", " + q + "/c" + q,
		q + "CMD.EXE" + q + "," + q + "/C" + q,
		q + "sh" + q + ", " + q + "-c" + q,
		q + "bash" + q + ",\n\t" + q + "-c" + q,
	} {
		require.True(t, shellPairRE.MatchString(sample), "pattern must catch %s", sample)
	}

	const root = ".." // internal/
	var offenders []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") {
			return nil
		}
		data, err := os.ReadFile(p) //nolint:gosec // G304/G122: test reads repo source files it walks itself
		if err != nil {
			return err
		}
		if shellPairRE.Match(data) {
			offenders = append(offenders, p)
		}
		return nil
	})
	require.NoError(t, err)
	assert.Empty(t, offenders, "shell invocations are forbidden (§7, §17)")
}
