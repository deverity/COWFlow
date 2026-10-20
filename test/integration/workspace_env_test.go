package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/cli"
	"github.com/deverity/cowflow/internal/config"
	"github.com/deverity/cowflow/internal/domain"
	"github.com/deverity/cowflow/internal/git"
	"github.com/deverity/cowflow/internal/platform"
	"github.com/deverity/cowflow/internal/store"
	"github.com/deverity/cowflow/internal/workspace"
	"github.com/deverity/cowflow/test/fixtures/benchrepo"
	"github.com/deverity/cowflow/test/harness"
)

// Copy-on-write volumes the CI jobs provide (sprint-1.md S1-T19). Without
// one, the provider=cow_clone subtests skip with a reason.
const (
	envCowDir  = "COWFLOW_TEST_COW_DIR"  // btrfs, XFS or APFS directory
	envRefsDir = "COWFLOW_TEST_REFS_DIR" // ReFS directory (Windows)
	envNTFSDir = "COWFLOW_TEST_NTFS_DIR" // plain NTFS directory (Windows)
)

const (
	providerWorktree = workspace.WorktreeName     // "git_worktree"
	providerCow      = workspace.ProviderCowClone // "cow_clone"
	providerAuto     = "auto"

	// cliTimeout bounds one in-process cowflow invocation.
	cliTimeout = 5 * time.Minute
	// repoSeed makes every test's repository identical.
	repoSeed = 1
)

// forEachProvider runs fn as the subtests provider=git_worktree (always)
// and provider=cow_clone (only on a copy-on-write volume). Each subtest gets
// its own repository and SQLite file.
func forEachProvider(t *testing.T, fn func(t *testing.T, provider string)) {
	t.Helper()
	for _, p := range []string{providerWorktree, providerCow} {
		t.Run("provider="+p, func(t *testing.T) {
			t.Parallel()
			fn(t, p)
		})
	}
}

// eventLog records the workspace events the store and the Manager emit
// (PENDING U-DATA-1: no events table until S2-E2, so the sink is the only
// place an event can be observed).
type eventLog struct {
	mu  sync.Mutex
	evs []domain.WorkspaceEvent
}

func (l *eventLog) Emit(ev domain.WorkspaceEvent) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.evs = append(l.evs, ev)
	return nil
}

// ofType returns the recorded events of type typ.
func (l *eventLog) ofType(typ string) []domain.WorkspaceEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []domain.WorkspaceEvent
	for _, ev := range l.evs {
		if ev.Type == typ {
			out = append(out, ev)
		}
	}
	return out
}

// wsEnv is one project: a generated repository (benchrepo profile small,
// S1-T19), its .cowflow/config.toml and SQLite file, and the seams the
// in-process CLI is wired with. Seams are read at the start of every
// runCLI, so a test may change them between two invocations, never during
// one.
type wsEnv struct {
	t        *testing.T
	base     string // canonical scratch directory of the test
	repoDir  string // canonical main checkout
	wsRoot   string // canonical [workspace].root
	cfgPath  string
	provider string // [workspace].provider

	git    *harness.FaultyRunner // every git call of cowflow goes through it
	direct git.Runner            // hermetic runner for the test's own git calls
	events *eventLog

	// Seams; the zero value is the production wiring of cmd/cowflow.
	cloner      workspace.Cloner        // nil: the OS block-clone primitive
	vols        workspace.Volumes       // nil: workspace.PlatformVolumes
	devDrive    workspace.DevDrives     // nil: the real Dev Drive layer
	removeAll   func(path string) error // nil: os.RemoveAll (the destroy)
	clock       domain.Clock            // nil: domain.SystemClock (destroy retries)
	callTimeout time.Duration           // > 0: per git call, like command_timeout_seconds
}

type envOptions struct {
	base     string
	root     string
	provider string
	extra    string // appended to config.toml
}

// envOption customizes newEnv.
type envOption func(*envOptions)

// withRoot sets [workspace].root to dir, an absolute canonical path that
// newEnv does not create (for example one on a Dev Drive that is not
// mounted).
func withRoot(dir string) envOption { return func(o *envOptions) { o.root = dir } }

// withBase puts the repository and the workspace root under dir instead of
// the default scratch directory of the provider.
func withBase(dir string) envOption { return func(o *envOptions) { o.base = dir } }

// withProviderSetting sets [workspace].provider to p instead of the
// provider under test (for example "auto").
func withProviderSetting(p string) envOption { return func(o *envOptions) { o.provider = p } }

// withConfig appends TOML to config.toml.
func withConfig(toml string) envOption { return func(o *envOptions) { o.extra += toml } }

// newEnv generates the small benchmark repository under a fresh directory
// (a copy-on-write volume for cow_clone, tmpfs on Linux otherwise), writes
// a config whose [workspace].root is <base>/ws (unless withRoot) and whose
// provider is provider, and registers the project with `cowflow init`.
func newEnv(t *testing.T, provider string, opts ...envOption) *wsEnv {
	t.Helper()
	o := envOptions{provider: provider}
	for _, fn := range opts {
		fn(&o)
	}
	if o.base == "" {
		if provider == providerCow {
			o.base = cowDir(t)
		} else {
			o.base = harness.ScratchDir(t)
		}
	}
	base, err := filepath.EvalSymlinks(o.base)
	require.NoError(t, err)

	repoDir := filepath.Join(base, "repo")
	_, err = benchrepo.Generate(t.Context(), repoDir, benchrepo.Small, repoSeed)
	require.NoError(t, err)
	repoDir, err = platform.Canonical(repoDir)
	require.NoError(t, err)
	wsRoot := o.root
	if wsRoot == "" {
		wsRoot = filepath.Join(base, "ws")
		require.NoError(t, os.MkdirAll(wsRoot, 0o750))
	}

	cfgPath := filepath.Join(repoDir, config.DirName, config.FileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0o750))
	text := fmt.Sprintf("[workspace]\nroot = %q\nprovider = %q\n", filepath.ToSlash(wsRoot), o.provider) + o.extra
	require.NoError(t, os.WriteFile(cfgPath, []byte(text), 0o600))

	_, direct := harness.HermeticGit(t)
	e := &wsEnv{
		t: t, base: base, repoDir: repoDir, wsRoot: wsRoot, cfgPath: cfgPath, provider: o.provider,
		git: &harness.FaultyRunner{Inner: direct}, direct: direct, events: &eventLog{},
	}
	e.mustCLI("init")
	return e
}

// cowDir returns a fresh directory on the copy-on-write volume of the CI
// job, or skips the test.
func cowDir(t *testing.T) string {
	t.Helper()
	for _, name := range []string{envCowDir, envRefsDir} {
		parent := os.Getenv(name)
		if parent == "" {
			continue
		}
		require.NoError(t, os.MkdirAll(parent, 0o750), "%s=%s", name, parent) //nolint:gosec // G703: test volume chosen by the CI job
		dir, err := os.MkdirTemp(parent, "cowflow-ac-")
		require.NoError(t, err)
		t.Cleanup(func() { _ = os.RemoveAll(dir) }) //nolint:gosec // G703: the test's own temporary directory
		return dir
	}
	t.Skipf("provider=cow_clone needs a copy-on-write volume: set %s (btrfs, XFS, APFS) or %s (ReFS)", envCowDir, envRefsDir)
	return ""
}

// cliResult is what one cowflow invocation printed and returned.
type cliResult struct {
	stdout, stderr string
	code           int
}

func (r cliResult) String() string {
	return fmt.Sprintf("exit %d\nstdout:\n%s\nstderr:\n%s", r.code, r.stdout, r.stderr)
}

// runCLI runs cowflow with args in-process through cli.Execute, the entry
// point cmd/cowflow calls (cli.NewRootCmd needs the unexported run state, so
// Execute is the narrowest exported way to build it), with buffers for the
// streams and the project's config injected.
func (e *wsEnv) runCLI(args ...string) cliResult {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), cliTimeout)
	defer cancel()
	var out, errb bytes.Buffer
	cfgPath := e.cfgPath
	deps := cli.Deps{
		Version: "test", Commit: "none", Date: "unknown",
		ConfigPath: func() (string, error) { return cfgPath, nil },
		Git:        e.git,
		OpenTasks:  e.openTasks(),
	}
	code := cli.Execute(ctx, args, cli.IOStreams{In: strings.NewReader(""), Out: &out, Err: &errb}, deps)
	return cliResult{stdout: out.String(), stderr: errb.String(), code: code}
}

// mustCLI runs cowflow and requires exit 0.
func (e *wsEnv) mustCLI(args ...string) cliResult {
	e.t.Helper()
	r := e.runCLI(args...)
	require.Equal(e.t, cli.ExitOK, r.code, "cowflow %s\n%s", strings.Join(args, " "), r)
	return r
}

// openTasks mirrors cmd/cowflow's openTasks (package main, so it cannot be
// imported) over the test's seams: the store at config.DBPath, the git
// client over the faulty runner and the Manager with both providers sharing
// one Inspector and one Destroyer.
func (e *wsEnv) openTasks() func(context.Context, *config.Config, string) (cli.TaskBackend, error) {
	// Snapshot the seams now, on the test goroutine.
	cloner, vols, dev, removeAll, clock, timeout := e.cloner, e.vols, e.devDrive, e.removeAll, e.clock, e.callTimeout
	events, runner, t := e.events, git.Runner(harness.FaultyOptsRunner{FaultyRunner: e.git}), e.t
	if timeout > 0 {
		runner = callTimeoutRunner{inner: harness.FaultyOptsRunner{FaultyRunner: e.git}, timeout: timeout}
	}
	return func(ctx context.Context, cfg *config.Config, cfgPath string) (cli.TaskBackend, error) {
		if err := os.MkdirAll(config.StatePath(cfgPath, cfg), 0o750); err != nil {
			return cli.TaskBackend{}, err
		}
		st, err := store.Open(ctx, config.DBPath(cfgPath, cfg), store.Options{Events: events})
		if err != nil {
			return cli.TaskBackend{}, err
		}
		if vols == nil {
			vols = workspace.PlatformVolumes{}
		}
		logger := slog.New(slog.NewTextHandler(testLogWriter{t}, &slog.HandlerOptions{Level: slog.LevelWarn}))
		gc := git.NewClient(runner)
		insp := workspace.NewInspector(st, gc, vols, cfg)
		destroyer := workspace.NewDestroyer(workspace.DestroyerDeps{
			Config: cfg, Store: st, Git: gc, Inspector: insp, Volumes: vols,
			RemoveAll: removeAll, Clock: clock, Logger: logger,
		})
		golden := workspace.NewGoldenCache(workspace.GoldenDeps{
			Config: cfg, Store: st, Git: gc, Volumes: vols, Logger: logger,
		})
		cow := workspace.NewCowCloneProvider(workspace.CowCloneDeps{
			Config: cfg, Git: gc, Golden: golden, Volumes: vols, Cloner: cloner, Logger: logger,
		}).WithInspector(insp).WithDestroyer(destroyer)
		m := workspace.NewManager(workspace.Deps{
			Config: cfg, Store: st, Git: gc, Volumes: vols, DevDrive: dev,
			Cow:      cow,
			Worktree: workspace.NewWorktreeProvider(gc, vols).WithInspector(insp).WithDestroyer(destroyer),
			Env:      workspace.NewEnvFileWriter(cfg, vols, logger),
			Events:   events,
			Logger:   logger,
		})
		return cli.TaskBackend{
			Store: st, Workspaces: m, Repo: gc,
			Tasks: st, Lister: m, Admin: m, Golden: golden,
			Close: st.Close,
		}, nil
	}
}

// testLogWriter sends the Manager's warnings to the test log.
type testLogWriter struct{ t *testing.T }

func (w testLogWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

// callTimeoutRunner bounds every git call, as the real runner does with
// [scheduler].command_timeout_seconds, so an injected hang ends.
type callTimeoutRunner struct {
	inner   git.OptsRunner
	timeout time.Duration
}

func (r callTimeoutRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return r.inner.Run(ctx, dir, args...)
}

func (r callTimeoutRunner) RunWith(ctx context.Context, dir string, o git.RunOpts, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return r.inner.RunWith(ctx, dir, o, args...)
}

// gitIn runs git in dir with the hermetic environment, bypassing the
// faults, and returns its trimmed output.
func (e *wsEnv) gitIn(dir string, args ...string) string {
	e.t.Helper()
	out, err := e.direct.Run(context.Background(), dir, args...)
	require.NoError(e.t, err, "git %s in %s", strings.Join(args, " "), dir)
	return strings.TrimSpace(string(out))
}

// refExists reports whether ref resolves in the main repository.
func (e *wsEnv) refExists(ref string) bool {
	e.t.Helper()
	_, err := e.direct.Run(context.Background(), e.repoDir, "rev-parse", "--verify", "--quiet", ref)
	return err == nil
}

// worktreePaths lists the worktrees of the repository, main checkout first.
func (e *wsEnv) worktreePaths() []string {
	e.t.Helper()
	var paths []string
	for _, line := range strings.Split(e.gitIn(e.repoDir, "worktree", "list", "--porcelain"), "\n") {
		if p, ok := strings.CutPrefix(line, "worktree "); ok {
			paths = append(paths, filepath.Clean(p))
		}
	}
	return paths
}

// statusPaths is `git status --porcelain` in dir without the COWFlow-owned
// .env.cowflow, which destroy never counts (SCOPE.md §14) but which is
// untracked in a repository that does not ignore it.
func (e *wsEnv) statusPaths(dir string) []string {
	e.t.Helper()
	out, err := e.direct.Run(context.Background(), dir, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	require.NoError(e.t, err)
	var paths []string
	for _, rec := range strings.Split(string(out), "\x00") {
		if len(rec) < 4 {
			continue
		}
		if p := rec[3:]; p != workspace.EnvFileName {
			paths = append(paths, p)
		}
	}
	return paths
}

// taskDoc is the part of the `task create --json` document the tests read.
type taskDoc struct {
	Task struct {
		ID   string `json:"id"`
		Slug string `json:"slug"`
	} `json:"task"`
	Workspace struct {
		ID         string `json:"id"`
		Path       string `json:"path"`
		Provider   string `json:"provider"`
		CowKind    string `json:"cow_kind"`
		Filesystem string `json:"filesystem"`
		Branch     string `json:"branch"`
		BaseSHA    string `json:"base_sha"`
	} `json:"workspace"`
	Warnings []string `json:"warnings"`
}

// inspectDoc is the part of the `workspace inspect --json` document the
// tests read.
type inspectDoc struct {
	ID         string `json:"id"`
	Provider   string `json:"provider"`
	CowKind    string `json:"cow_kind"`
	Filesystem string `json:"filesystem"`
	Path       string `json:"path"`
	Branch     string `json:"branch"`
	BaseSHA    string `json:"base_sha"`
	Head       string `json:"head"`
	State      string `json:"state"`
	Dirty      struct {
		Uncommitted []string `json:"uncommitted"`
		Unpublished []string `json:"unpublished"`
	} `json:"dirty"`
}

// listRow is one entry of `workspace list --json`.
type listRow struct {
	ID    string `json:"id"`
	State string `json:"state"`
	Path  string `json:"path"`
}

// destroyDoc is the `workspace destroy --json` document.
type destroyDoc struct {
	ID        string `json:"id"`
	Destroyed bool   `json:"destroyed"`
	RescueRef string `json:"rescue_ref"`
}

func decode[T any](t *testing.T, r cliResult) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal([]byte(r.stdout), &v), "decode --json output\n%s", r)
	return v
}

// createTask runs `task create <slug> --json` and requires success.
func (e *wsEnv) createTask(slug string, extra ...string) taskDoc {
	e.t.Helper()
	doc := decode[taskDoc](e.t, e.mustCLI(append([]string{"task", "create", slug, "--json"}, extra...)...))
	require.NotEmpty(e.t, doc.Workspace.ID)
	return doc
}

// inspect runs `workspace inspect <id> --json` and requires success.
func (e *wsEnv) inspect(id string) inspectDoc {
	e.t.Helper()
	return decode[inspectDoc](e.t, e.mustCLI("workspace", "inspect", id, "--json"))
}

// list runs `workspace list --json` and requires success.
func (e *wsEnv) list() []listRow {
	e.t.Helper()
	return decode[[]listRow](e.t, e.mustCLI("workspace", "list", "--json"))
}

// writeFile writes content to the slash-separated rel under dir.
func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
}

// appendFile appends content to the slash-separated rel under dir.
func appendFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(dir, filepath.FromSlash(rel)), os.O_WRONLY|os.O_APPEND, 0) //nolint:gosec // G304: a file of the test's own workspace
	require.NoError(t, err)
	_, err = io.WriteString(f, content)
	require.NoError(t, errors.Join(err, f.Close())) // close before cleanup (Windows handles)
}

// readFile returns the content of the slash-separated rel under dir.
func readFile(t *testing.T, dir, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel))) //nolint:gosec // G304: a file of the test's own workspace
	require.NoError(t, err)
	return string(data)
}

// treeChecksum hashes every path, type, permission and content under dir,
// so any change to a golden tree changes it.
func treeChecksum(t *testing.T, dir string) string {
	t.Helper()
	type entry struct{ rel, line string }
	var entries []entry
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		line := fmt.Sprintf("%s %o", info.Mode().Type(), info.Mode().Perm())
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			line += " -> " + target
		case d.Type().IsRegular():
			data, err := os.ReadFile(p) //nolint:gosec // G122: the test's own golden tree
			if err != nil {
				return err
			}
			sum := sha256.Sum256(data)
			line += " " + hex.EncodeToString(sum[:])
		}
		entries = append(entries, entry{rel: filepath.ToSlash(rel), line: line})
		return nil
	})
	require.NoError(t, err)
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })
	h := sha256.New()
	for _, en := range entries {
		_, _ = fmt.Fprintf(h, "%s\x00%s\n", en.rel, en.line)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// goldenDir is the golden tree of sha under the default golden root
// (<workspace.root>/golden, SCOPE.md §23 decision 18).
func (e *wsEnv) goldenDir(sha string) string {
	return filepath.Join(e.wsRoot, "golden", sha)
}

// entriesUnder lists the names directly under dir; a missing dir is empty.
func entriesUnder(t *testing.T, dir string) []string {
	t.Helper()
	des, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	names := make([]string, 0, len(des))
	for _, d := range des {
		names = append(names, d.Name())
	}
	return names
}
