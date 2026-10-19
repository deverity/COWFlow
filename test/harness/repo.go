package harness

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/deverity/cowflow/internal/git"
)

const (
	// Identity and date every harness repository uses, so the same commits
	// produce the same SHAs on every machine.
	repoName  = "COWFlow Harness"
	repoEmail = "harness@cowflow.invalid"
	repoDate  = "2000-01-01T00:00:00Z"

	// defaultAttributes keeps line endings identical on every OS.
	defaultAttributes = "* text=auto eol=lf\n"

	repoGitTimeout = 30 * time.Second
	shmDir         = "/dev/shm"
)

// Repo is a throw-away Git repository isolated from the developer's Git
// configuration, hooks and credentials.
type Repo struct {
	Dir    string     // working tree root
	Env    []string   // hermetic Git environment (KEY=value), for tests that run git directly
	Runner git.Runner // runs git with Env; per-call timeout 30 s
}

type repoConfig struct {
	dir        string
	branch     string
	attributes string
}

// RepoOption customizes NewRepo.
type RepoOption func(*repoConfig)

// WithDir creates the repository in dir instead of the default location. The
// caller owns dir's cleanup. It is the seam for SPEC-GAPS U-PLAT-8 (default
// tmpfs-like location on Windows and macOS).
func WithDir(dir string) RepoOption { return func(c *repoConfig) { c.dir = dir } }

// WithBranch sets the initial branch (default "develop").
func WithBranch(name string) RepoOption { return func(c *repoConfig) { c.branch = name } }

// WithAttributes sets the content of the initial .gitattributes (default
// "* text=auto eol=lf"). An empty string creates no .gitattributes.
func WithAttributes(content string) RepoOption {
	return func(c *repoConfig) { c.attributes = content }
}

// NewRepo creates an initialized repository with one commit (.gitattributes,
// unless disabled). On Linux it lives under /dev/shm when that is writable;
// elsewhere, or otherwise, under t.TempDir(). All state is removed by
// t.Cleanup.
func NewRepo(t testing.TB, opts ...RepoOption) *Repo {
	t.Helper()
	cfg := repoConfig{branch: "develop", attributes: defaultAttributes}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.dir == "" {
		cfg.dir = defaultRepoDir(t)
	}

	if err := os.MkdirAll(cfg.dir, 0o750); err != nil {
		t.Fatalf("harness: create repo dir: %v", err)
	}

	home := t.TempDir()
	globalCfg := filepath.Join(home, "gitconfig")
	if err := os.WriteFile(globalCfg, nil, 0o600); err != nil {
		t.Fatalf("harness: write empty git config: %v", err)
	}
	env := []string{
		"GIT_CONFIG_GLOBAL=" + globalCfg,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=" + repoName,
		"GIT_AUTHOR_EMAIL=" + repoEmail,
		"GIT_COMMITTER_NAME=" + repoName,
		"GIT_COMMITTER_EMAIL=" + repoEmail,
		"GIT_AUTHOR_DATE=" + repoDate,
		"GIT_COMMITTER_DATE=" + repoDate,
		"HOME=" + home,
		"USERPROFILE=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, "xdg"),
	}
	runner, err := git.NewExecRunner(git.ExecOptions{Timeout: repoGitTimeout, ExtraEnv: env})
	if errors.Is(err, git.ErrNotFound) {
		t.Skip("git not on PATH")
	}
	if err != nil {
		t.Fatalf("harness: git runner: %v", err)
	}
	r := &Repo{Dir: cfg.dir, Env: env, Runner: runner}

	// An empty --template keeps system hook samples and other host files out.
	r.git(t, "init", "-q", "--template=", "-b", cfg.branch)
	r.git(t, "config", "core.autocrlf", "false")
	if cfg.attributes != "" {
		r.Commit(t, "chore: add gitattributes", map[string]string{".gitattributes": cfg.attributes})
	}
	return r
}

// defaultRepoDir returns a fresh directory on tmpfs (Linux) or under
// t.TempDir().
func defaultRepoDir(t testing.TB) string {
	t.Helper()
	if runtime.GOOS == "linux" {
		if dir, err := os.MkdirTemp(shmDir, "cowflow-harness-*"); err == nil {
			t.Cleanup(func() { _ = os.RemoveAll(dir) })
			return dir
		}
	}
	return t.TempDir()
}

// Commit writes files (slash-separated paths relative to the repository
// root), stages everything and commits with message, returning the new
// commit's SHA. With no files it creates an empty commit.
func (r *Repo) Commit(t testing.TB, message string, files map[string]string) string {
	t.Helper()
	for name, content := range files {
		if !filepath.IsLocal(filepath.FromSlash(name)) {
			t.Fatalf("harness: file path %q escapes the repository", name)
		}
		p := filepath.Join(r.Dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatalf("harness: mkdir for %s: %v", name, err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatalf("harness: write %s: %v", name, err)
		}
	}
	r.git(t, "add", "-A")
	r.git(t, "commit", "-q", "--allow-empty", "-m", message)
	return strings.TrimSpace(r.git(t, "rev-parse", "HEAD"))
}

// git runs git in the repository and fails the test on error.
func (r *Repo) git(t testing.TB, args ...string) string {
	t.Helper()
	out, err := r.Runner.Run(context.Background(), r.Dir, args...)
	if err != nil {
		t.Fatalf("harness: git %s: %v", strings.Join(args, " "), err)
	}
	return string(out)
}
