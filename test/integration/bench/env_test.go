package bench

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/config"
	"github.com/deverity/cowflow/internal/domain"
	"github.com/deverity/cowflow/internal/git"
	"github.com/deverity/cowflow/internal/platform"
	"github.com/deverity/cowflow/internal/store"
	"github.com/deverity/cowflow/internal/workspace"
	"github.com/deverity/cowflow/test/fixtures/benchrepo"
)

// seederGoldenIndex is the production index seeder (SCOPE.md §23 decision
// 39). The read-tree seeder of U-PLAT-1 option A is only exported to the
// workspace package's own tests, so it is not compared here.
const seederGoldenIndex = "golden-index"

const (
	baseBranch    = "develop"
	gitTimeout    = 10 * time.Minute
	cliTimeout    = 5 * time.Minute
	externalLabel = "external"
)

var (
	binOnce sync.Once
	binPath string
	binErr  error
	binDir  string
)

func TestMain(m *testing.M) {
	code := m.Run()
	if binDir != "" {
		_ = os.RemoveAll(binDir)
	}
	os.Exit(code)
}

// moduleRoot is the directory of go.mod.
func moduleRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

// cowflowBinary builds cmd/cowflow once per test run.
func cowflowBinary(t *testing.T) string {
	t.Helper()
	binOnce.Do(func() {
		dir, err := os.MkdirTemp("", "cowflow-bench-bin-")
		if err != nil {
			binErr = err
			return
		}
		binDir = dir
		name := "cowflow"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		binPath = filepath.Join(dir, name)
		cmd := exec.Command("go", "build", "-o", binPath, "./cmd/cowflow") //nolint:gosec // G204: fixed arguments
		cmd.Dir = moduleRoot()
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			binErr = fmt.Errorf("go build ./cmd/cowflow: %w\n%s", err, out)
		}
	})
	require.NoError(t, binErr)
	return binPath
}

// volumeRoot returns a fresh directory on the volume under test, or skips.
func volumeRoot(t *testing.T) string {
	t.Helper()
	for _, name := range []string{EnvRoot, "COWFLOW_TEST_COW_DIR", "COWFLOW_TEST_REFS_DIR"} {
		parent := os.Getenv(name)
		if parent == "" {
			continue
		}
		require.NoError(t, os.MkdirAll(parent, 0o750), "%s=%s", name, parent) //nolint:gosec // G703: benchmark root chosen by the operator
		dir, err := os.MkdirTemp(parent, "cowflow-bench-")
		require.NoError(t, err)
		t.Cleanup(func() { removeAll(dir) })
		dir, err = filepath.EvalSymlinks(dir)
		require.NoError(t, err)
		return dir
	}
	t.Skip("no copy-on-write volume: set COWFLOW_BENCH_ROOT, COWFLOW_TEST_COW_DIR or COWFLOW_TEST_REFS_DIR")
	return ""
}

// removeAll deletes dir, best effort. The Go module cache is read-only, so
// directories are made writable first.
func removeAll(dir string) {
	unlock := func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(p, 0o700) //nolint:gosec // G302: owner-only; the tree is deleted next
		}
		return nil
	}
	_ = filepath.WalkDir(dir, unlock) //nolint:gosec // G703: the test's own temporary directory
	_ = os.RemoveAll(dir)             //nolint:gosec // G703: the test's own temporary directory
}

// requireLarge skips unless the ≥ 1 GiB tests are enabled.
func requireLarge(t *testing.T) {
	t.Helper()
	if os.Getenv(EnvLarge) == "" {
		t.Skipf("set %s=1 to run the benchmarks that need a repository of at least 1 GiB", EnvLarge)
	}
}

// hermeticGitEnv keeps the developer's Git configuration out of every Git
// process the benchmarks start.
func hermeticGitEnv(t *testing.T) []string {
	t.Helper()
	home := t.TempDir()
	cfg := filepath.Join(home, "gitconfig")
	require.NoError(t, os.WriteFile(cfg, nil, 0o600))
	return []string{
		"GIT_CONFIG_GLOBAL=" + cfg, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0",
		"HOME=" + home, "USERPROFILE=" + home,
	}
}

// runCmd runs name in dir with the extra environment and returns its output.
func runCmd(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // G204: callers pass fixed commands; no shell is involved
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// profileFromEnv is the profile of TestPhase1 and TestOQ6.
func profileFromEnv() string {
	if v := os.Getenv(EnvProfile); v != "" {
		return v
	}
	return benchrepo.Small.Name
}

// prepareRepo creates the benchmark repository <root>/repo: a local clone of
// COWFLOW_BENCH_REPO when that is set, otherwise the generated profile. It
// returns the directory, the report and the profile label.
func prepareRepo(t *testing.T, root, profile string) (string, benchrepo.Report, string) {
	t.Helper()
	ctx := t.Context()
	dir := filepath.Join(root, "repo")
	if ext, ok := benchrepo.ExternalRepo(); ok {
		env := hermeticGitEnv(t)
		_, err := runCmd(ctx, root, env, "git", "clone", "--quiet", "--local", ext, dir)
		require.NoError(t, err)
		if _, err := runCmd(ctx, dir, env, "git", "rev-parse", "--verify", "--quiet", baseBranch); err != nil {
			if _, err := runCmd(ctx, dir, env, "git", "branch", baseBranch, "origin/"+baseBranch); err != nil {
				_, err = runCmd(ctx, dir, env, "git", "branch", baseBranch, "HEAD")
				require.NoError(t, err)
			}
		}
		rep, err := benchrepo.Inspect(ctx, dir)
		require.NoError(t, err)
		return dir, rep, externalLabel
	}
	p, err := benchrepo.ProfileByName(profile)
	require.NoError(t, err)
	rep, err := benchrepo.Generate(ctx, dir, p, 1)
	require.NoError(t, err)
	require.NotEmpty(t, rep.Branches[baseBranch], "the generated repository has a %s branch", baseBranch)
	return dir, rep, p.Name
}

// benchEnv is a repository with a project registered in a real store and the
// pieces a Manager needs. The store lives next to the config file, exactly
// where `cowflow task create` finds it, so the CLI child process and the
// in-process Manager see the same project.
type benchEnv struct {
	t       *testing.T
	root    string // the volume directory of this test
	repoDir string // canonical repository directory
	cfgPath string
	cfg     *config.Config
	st      *store.Store
	gc      *git.Client
	gitEnv  []string
	proj    domain.Project
	vols    workspace.PlatformVolumes
	log     *slog.Logger
	ids     domain.IDGen
	namer   domain.BranchNamer
	vol     platform.Volume
	label   string
	rep     benchrepo.Report
}

// newBenchEnv registers the repository at repoDir as a project whose
// workspaces live under <root>/ws.
func newBenchEnv(t *testing.T, root, repoDir, label string, rep benchrepo.Report, cache config.Cache) *benchEnv {
	t.Helper()
	ctx := t.Context()
	wsRoot := filepath.Join(root, "ws")
	require.NoError(t, os.MkdirAll(wsRoot, 0o750))
	cfgPath := filepath.Join(repoDir, config.DirName, config.FileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0o750))
	text := fmt.Sprintf("[workspace]\nroot = %q\n", filepath.ToSlash(wsRoot))
	if cache != (config.Cache{}) {
		text += fmt.Sprintf("\n[cache]\ngo_modcache = %q\ngo_cache = %q\n",
			filepath.ToSlash(cache.GoModCache), filepath.ToSlash(cache.GoCache))
	}
	require.NoError(t, os.WriteFile(cfgPath, []byte(text), 0o600))
	cfg, err := config.Load(cfgPath)
	require.NoError(t, err)

	require.NoError(t, os.MkdirAll(config.StatePath(cfgPath, cfg), 0o750))
	st, err := store.Open(ctx, config.DBPath(cfgPath, cfg), store.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	canon, err := platform.Canonical(repoDir)
	require.NoError(t, err)
	var proj domain.Project
	require.NoError(t, st.WithTx(ctx, "", func(tx *store.Tx) error {
		var err error
		proj, err = tx.InsertProject(filepath.Base(canon), canon)
		return err
	}))

	env := hermeticGitEnv(t)
	runner, err := git.NewExecRunner(git.ExecOptions{Timeout: gitTimeout, ExtraEnv: env})
	if errors.Is(err, git.ErrNotFound) {
		t.Skip("git not on PATH")
	}
	require.NoError(t, err)
	namer, err := domain.NewBranchNamer(cfg.Conventions.Preset)
	require.NoError(t, err)
	vol, err := platform.VolumeOf(ctx, wsRoot)
	require.NoError(t, err)

	return &benchEnv{
		t: t, root: root, repoDir: canon, cfgPath: cfgPath, cfg: cfg, st: st,
		gc: git.NewClient(runner), gitEnv: env, proj: proj, vols: workspace.PlatformVolumes{},
		log: slog.New(slog.DiscardHandler), ids: domain.NewULIDGen(domain.SystemClock{}),
		namer: namer, vol: vol, label: label, rep: rep,
	}
}

// stack is a Manager and the golden cache beneath it.
type stack struct {
	mgr    *workspace.Manager
	golden *workspace.GoldenCache
}

// stack builds the production wiring of cmd/cowflow over cfg. The OS block
// clone primitive and the platform volumes are used: nothing is faked.
func (e *benchEnv) stack(cfg *config.Config) stack {
	golden := workspace.NewGoldenCache(workspace.GoldenDeps{
		Config: cfg, Store: e.st, Git: e.gc, Volumes: e.vols, Logger: e.log,
	})
	cow := workspace.NewCowCloneProvider(workspace.CowCloneDeps{
		Config: cfg, Git: e.gc, Golden: golden, Volumes: e.vols, Logger: e.log,
	})
	cow.WithDestroyer(workspace.NewDestroyer(workspace.DestroyerDeps{
		Config: cfg, Store: e.st, Git: e.gc, Volumes: e.vols, Clock: domain.SystemClock{}, Logger: e.log,
	}))
	mgr := workspace.NewManager(workspace.Deps{
		Config: cfg, Store: e.st, Git: e.gc, Volumes: e.vols,
		Cow: cow, Worktree: workspace.NewWorktreeProvider(e.gc, e.vols),
		Env: workspace.NewEnvFileWriter(cfg, e.vols, e.log), Logger: e.log,
	})
	return stack{mgr: mgr, golden: golden}
}

// task stores a task for slug based on the revision base.
func (e *benchEnv) task(slug, base string) domain.Task {
	e.t.Helper()
	ctx := e.t.Context()
	sha, err := e.gc.ResolveCommit(ctx, e.repoDir, base)
	require.NoError(e.t, err)
	branch, err := e.namer.TaskBranch(e.proj.Name, "", slug)
	require.NoError(e.t, err)
	task, err := e.st.CreateTask(ctx, domain.Task{
		ProjectID: e.proj.ID, Slug: slug, Title: "bench " + slug, Kind: "feature",
		Base: base, BaseSHA: sha, Branch: branch,
	})
	require.NoError(e.t, err)
	return task
}

func (e *benchEnv) request(task domain.Task) workspace.CreateRequest {
	e.t.Helper()
	opID, err := e.ids.New(domain.KindOperation)
	require.NoError(e.t, err)
	return workspace.CreateRequest{OpID: opID, ProjectID: e.proj.ID, RepoDir: e.repoDir, Task: task}
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// timedCreate runs Manager.Create once and returns its wall time. It is safe
// to call from several goroutines.
func timedCreate(ctx context.Context, m *workspace.Manager, req workspace.CreateRequest) (*workspace.Workspace, float64, error) {
	start := time.Now()
	ws, sel, err := m.Create(ctx, req)
	elapsed := ms(time.Since(start))
	if err != nil {
		return nil, elapsed, err
	}
	if ws.Provider != workspace.ProviderCowClone {
		return ws, elapsed, fmt.Errorf("workspace %s used provider %q (warning %q), not cow_clone: the volume has no block clone",
			ws.ID, ws.Provider, sel.Warning)
	}
	return ws, elapsed, nil
}

func (e *benchEnv) free() int64 {
	e.t.Helper()
	n, err := FreeSpace(e.root)
	require.NoError(e.t, err)
	return n
}

// create makes one workspace and measures its time and free-space delta.
func (e *benchEnv) create(m *workspace.Manager, slug, base string) (*workspace.Workspace, Create) {
	e.t.Helper()
	req := e.request(e.task(slug, base))
	before := e.free()
	ws, elapsed, err := timedCreate(e.t.Context(), m, req)
	require.NoError(e.t, err)
	return ws, Create{CreateMS: elapsed, DFreeBytes: before - e.free()}
}

// series makes n workspaces sequentially.
func (e *benchEnv) series(m *workspace.Manager, prefix string, n int) []Create {
	e.t.Helper()
	out := make([]Create, 0, n)
	for i := range n {
		_, c := e.create(m, fmt.Sprintf("%s-%02d", prefix, i+1), baseBranch)
		out = append(out, c)
	}
	return out
}

// buildGolden builds the golden tree of base and reports the time and the
// free-space delta of the build.
func (e *benchEnv) buildGolden(g *workspace.GoldenCache, base string) (workspace.Golden, float64, int64) {
	e.t.Helper()
	sha, err := e.gc.ResolveCommit(e.t.Context(), e.repoDir, base)
	require.NoError(e.t, err)
	before := e.free()
	start := time.Now()
	golden, release, err := g.Acquire(e.t.Context(), e.proj.ID, e.repoDir, sha)
	elapsed := ms(time.Since(start))
	require.NoError(e.t, err)
	release()
	return golden, elapsed, before - e.free()
}

// cli runs `cowflow task create` as a child process and returns its wall
// time. The child opens the same store and finds the same configuration as
// the Manager above.
func (e *benchEnv) cli(slug string) float64 {
	e.t.Helper()
	bin := cowflowBinary(e.t)
	ctx, cancel := context.WithTimeout(e.t.Context(), cliTimeout)
	defer cancel()
	start := time.Now()
	out, err := runCmd(ctx, e.repoDir, e.gitEnv, bin, "task", "create", slug, "--base", baseBranch, "--title", "bench "+slug, "--json", "--sparse=false")
	elapsed := ms(time.Since(start))
	require.NoError(e.t, err)
	var doc struct {
		Workspace struct {
			Provider string `json:"provider"`
		} `json:"workspace"`
	}
	require.NoError(e.t, json.Unmarshal(out, &doc), "task create output: %s", out)
	require.Equal(e.t, workspace.ProviderCowClone, doc.Workspace.Provider)
	return elapsed
}

// newResult fills the fields of a result that every test shares.
func (e *benchEnv) newResult(test, seeder string) Result {
	m := HostMachine()
	return Result{
		Test: test, GOOS: runtime.GOOS, Filesystem: e.vol.Filesystem, DevDrive: e.vol.DevDrive,
		DefenderExcluded: ParseDefender(os.Getenv(EnvDefender)),
		GitVersion:       GitVersion(e.t.Context()), CPU: m.CPU, RAMGB: m.RAMGB,
		RepoProfile: e.label, Bytes: e.rep.Bytes, Files: e.rep.Files, Seeder: seeder,
		Commit:  SourceCommit(e.t.Context(), moduleRoot()),
		Creates: []Create{}, Runs: []RunSummary{},
	}
}

// publish writes the result to COWFLOW_BENCH_OUT and logs it.
func publish(t *testing.T, r *Result) {
	t.Helper()
	data, err := json.Marshal(r)
	require.NoError(t, err)
	t.Logf("%s", data)
	path, err := r.Write(os.Getenv(EnvOut))
	require.NoError(t, err)
	if path != "" {
		t.Logf("result written to %s", path)
	}
}
