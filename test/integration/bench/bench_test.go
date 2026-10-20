package bench

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/config"
	"github.com/deverity/cowflow/internal/workspace"
	"github.com/deverity/cowflow/test/fixtures/benchrepo"
	"github.com/deverity/cowflow/test/fixtures/goproxy"
	"github.com/deverity/cowflow/test/harness"
)

// Budgets of SCOPE.md §20.17, §20.21 and §23 decision 43.
const (
	maxCreateMS       = 2000.0 // Manager.Create, sequential, warm golden, max of 20
	maxIncrementalPct = 1.0    // free-space delta per workspace over the golden size
	maxCacheRatio     = 0.5    // shared_ws2 <= 0.5 x isolated_ws2
	oneGiB            = int64(1) << 30
	runs              = 3 // median of 3 runs
	cacheModules      = 60
)

// finish records which budgets apply to r, publishes it (so that the numbers
// of a failing run are not lost) and asserts them. The time budget is not
// checked under the race detector, and the disk budget only for a golden
// tree of at least 1 GiB, the size the criterion is about (per-file metadata
// dominates a smaller tree).
func finish(t *testing.T, r *Result) {
	t.Helper()
	r.TimingAsserted = !raceEnabled
	r.DiskAsserted = r.GoldenBytes >= oneGiB
	publish(t, r)
	if r.TimingAsserted {
		assert.LessOrEqual(t, r.MaxMS, maxCreateMS, "max Manager.Create time (ms), median of %d runs", runs)
	} else {
		t.Log("timing budget not asserted: the race detector is on")
	}
	if r.DiskAsserted {
		assert.Less(t, r.IncrementalPct, maxIncrementalPct, "incremental disk per workspace (%% of the golden tree)")
	} else {
		t.Logf("disk budget not asserted: the golden tree is %d bytes, below 1 GiB", r.GoldenBytes)
	}
}

// TestPhase1_TwentyCowWorkspaces is the Phase 1 output of §19 and criterion
// §20.21: 20 sequential cow_clone workspaces, each created in under 2 s, each
// adding under 1% of the golden tree to the volume.
func TestPhase1_TwentyCowWorkspaces(t *testing.T) {
	root := volumeRoot(t)
	repoDir, rep, label := prepareRepo(t, root, profileFromEnv())
	e := newBenchEnv(t, root, repoDir, label, rep, config.Cache{})
	ctx := t.Context()
	s := e.stack(e.cfg)
	res := e.newResult("TestPhase1_TwentyCowWorkspaces", seederGoldenIndex)

	// task create warm: the first workspace pays for the golden tree.
	golden, buildMS, _ := e.buildGolden(s.golden, baseBranch)
	res.GoldenBuildMS, res.GoldenBytes = buildMS, golden.SizeBytes
	ws, first := e.create(s.mgr, "warm", baseBranch)
	res.FirstCreateMS = first.CreateMS // the cold number, reported apart
	same, err := e.vols.SameVolume(ctx, golden.IndexFile, ws.Path)
	require.NoError(t, err)
	res.IndexOnWorkspaceVolume = same

	all := make([][]Create, 0, runs)
	for r := range runs {
		all = append(all, e.series(s.mgr, fmt.Sprintf("r%d", r+1), createsPerRun))
	}
	require.NoError(t, res.Summarize(all))

	for i := range res.Creates {
		res.Creates[i].CLIMS = e.cli(fmt.Sprintf("cli-%02d", i+1))
	}
	res.Concurrent = e.concurrent(s.mgr, createsPerRun)
	finish(t, &res)
}

// concurrent creates n workspaces at the same moment and reports the
// numbers without asserting (U-PLAT-12).
func (e *benchEnv) concurrent(m *workspace.Manager, n int) *Concurrent {
	t := e.t
	reqs := make([]workspace.CreateRequest, n)
	for i := range reqs {
		reqs[i] = e.request(e.task(fmt.Sprintf("conc-%02d", i+1), baseBranch))
	}
	times := make([]float64, n)
	errs := make([]error, n)
	gate := make(chan struct{})
	var wg sync.WaitGroup
	for i := range reqs {
		wg.Go(func() {
			<-gate
			_, times[i], errs[i] = timedCreate(t.Context(), m, reqs[i])
		})
	}
	start := time.Now()
	close(gate)
	wg.Wait()
	c := &Concurrent{WallMS: ms(time.Since(start))}
	for i, err := range errs {
		if err != nil {
			c.Failed++
			t.Logf("concurrent create %d failed (not asserted): %v", i+1, err)
			continue
		}
		c.CreateMS = append(c.CreateMS, times[i])
	}
	c.P50MS, c.P95MS, c.MaxMS = Median(c.CreateMS), Percentile(c.CreateMS, 95), Max(c.CreateMS)
	return c
}

// TestAC21_CowCloneLargeRepo is criterion §20.21 on a repository of at
// least 1 GiB: one create after the golden tree is warm, under 2 s and under
// 1% of the golden tree. It runs the large profile (few big files) and the
// monorepo profile (many small files), or COWFLOW_BENCH_REPO alone.
func TestAC21_CowCloneLargeRepo(t *testing.T) {
	requireLarge(t)
	names := []string{benchrepo.Large.Name, benchrepo.Monorepo.Name}
	if _, ok := benchrepo.ExternalRepo(); ok {
		names = []string{externalLabel}
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			root := volumeRoot(t)
			repoDir, rep, label := prepareRepo(t, root, name)
			require.GreaterOrEqual(t, rep.Bytes, oneGiB, "the criterion is about a repository of at least 1 GiB")
			e := newBenchEnv(t, root, repoDir, label, rep, config.Cache{})
			s := e.stack(e.cfg)
			res := e.newResult("TestAC21_CowCloneLargeRepo", seederGoldenIndex)

			golden, buildMS, _ := e.buildGolden(s.golden, baseBranch)
			res.GoldenBuildMS, res.GoldenBytes = buildMS, golden.SizeBytes
			ws, first := e.create(s.mgr, "warm", baseBranch)
			res.FirstCreateMS = first.CreateMS
			same, err := e.vols.SameVolume(t.Context(), golden.IndexFile, ws.Path)
			require.NoError(t, err)
			res.IndexOnWorkspaceVolume = same

			all := make([][]Create, 0, runs)
			for r := range runs {
				all = append(all, e.series(s.mgr, fmt.Sprintf("r%d", r+1), 1))
			}
			require.NoError(t, res.Summarize(all))
			res.Notes = append(res.Notes, fmt.Sprintf("one create per run, median of %d; files and bytes are those of HEAD", runs))
			finish(t, &res)
		})
	}
}

// TestAC17_SharedCacheSecondSetup is criterion §20.17. "Setup" is workspace
// create + go mod download + go build ./... against the offline GOPROXY
// fixture (decision 43). Workspace 2 of the shared scenario must set up in at
// most half the time of workspace 2 of the isolated scenario.
func TestAC17_SharedCacheSecondSetup(t *testing.T) {
	root := volumeRoot(t)
	_, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go command not on PATH")
	}
	fx, err := goproxy.Generate(filepath.Join(root, "goproxy"), cacheModules)
	require.NoError(t, err)

	repoDir := filepath.Join(root, "repo")
	hr := harness.NewRepo(t, harness.WithDir(repoDir), harness.WithBranch(baseBranch))
	consumer := map[string]string{}
	for _, name := range []string{"go.mod", "main.go"} {
		data, err := os.ReadFile(filepath.Join(fx.ConsumerDir, name)) //nolint:gosec // G304: generated fixture
		require.NoError(t, err)
		consumer[name] = string(data)
	}
	hr.Commit(t, "feat: add the consumer module", consumer)
	rep, err := benchrepo.Inspect(t.Context(), repoDir)
	require.NoError(t, err)
	e := newBenchEnv(t, root, repoDir, fmt.Sprintf("gomod-%d", cacheModules), rep, config.Cache{})

	var isolated, shared []float64
	isoStack := e.stack(e.cfg)
	for trial := range runs {
		// Isolated: every workspace gets private caches. Workspace 1 also
		// builds the golden tree on the first trial; only workspace 2 counts.
		dir := func(n int, kind string) string {
			return filepath.Join(root, "iso", fmt.Sprint(trial+1), fmt.Sprint(n), kind)
		}
		_, err := e.setup(isoStack.mgr, fmt.Sprintf("iso%d-a", trial+1), fx.Env(), dir(1, "mod"), dir(1, "build"))
		require.NoError(t, err)
		d, err := e.setup(isoStack.mgr, fmt.Sprintf("iso%d-b", trial+1), fx.Env(), dir(2, "mod"), dir(2, "build"))
		require.NoError(t, err)
		isolated = append(isolated, d)

		// Shared: the caches are the [cache] directories of .env.cowflow.
		cfg := *e.cfg
		cfg.Cache = config.Cache{
			GoModCache: filepath.Join(root, "shared", fmt.Sprint(trial+1), "mod"),
			GoCache:    filepath.Join(root, "shared", fmt.Sprint(trial+1), "build"),
		}
		sharedStack := e.stack(&cfg)
		_, err = e.setup(sharedStack.mgr, fmt.Sprintf("shr%d-a", trial+1), fx.Env(), "", "")
		require.NoError(t, err)
		d, err = e.setup(sharedStack.mgr, fmt.Sprintf("shr%d-b", trial+1), fx.Env(), "", "")
		require.NoError(t, err)
		shared = append(shared, d)
	}

	res := e.newResult("TestAC17_SharedCacheSecondSetup", seederGoldenIndex)
	res.Cache = &CacheResult{
		IsolatedWS2MS: isolated, SharedWS2MS: shared, Modules: cacheModules,
		IsolatedMS: Median(isolated), SharedMS: Median(shared),
	}
	res.Cache.Ratio = res.Cache.SharedMS / res.Cache.IsolatedMS
	res.Notes = append(res.Notes, "setup = workspace create + go mod download + go build ./...; median of 3 trials")
	publish(t, &res)
	assert.LessOrEqual(t, res.Cache.Ratio, maxCacheRatio,
		"shared ws2 %.0f ms vs isolated ws2 %.0f ms", res.Cache.SharedMS, res.Cache.IsolatedMS)
}

// setup creates a workspace and runs the Go commands of a first build in it.
// With modcache and gocache empty the caches come from the workspace's
// .env.cowflow (the shared scenario); otherwise they are those directories.
// It returns the wall time of the whole setup in milliseconds.
func (e *benchEnv) setup(m *workspace.Manager, slug string, goproxyEnv []string, modcache, gocache string) (float64, error) {
	t := e.t
	req := e.request(e.task(slug, baseBranch))
	start := time.Now()
	ws, _, err := timedCreate(t.Context(), m, req)
	if err != nil {
		return 0, err
	}
	if modcache == "" {
		vars, err := readEnvFile(filepath.Join(ws.Path, workspace.EnvFileName))
		if err != nil {
			return 0, err
		}
		modcache, gocache = vars["GOMODCACHE"], vars["GOCACHE"]
		if modcache == "" || gocache == "" {
			return 0, fmt.Errorf("%s has no GOMODCACHE/GOCACHE: %v", workspace.EnvFileName, vars)
		}
	}
	env := append(append([]string{}, goproxyEnv...), "GOMODCACHE="+modcache, "GOCACHE="+gocache, "CGO_ENABLED=0")
	for _, args := range [][]string{{"mod", "download"}, {"build", "./..."}} {
		if _, err := runCmd(t.Context(), ws.Path, env, "go", args...); err != nil {
			return 0, err
		}
	}
	return ms(time.Since(start)), nil
}

// readEnvFile parses a .env.cowflow file: KEY=VALUE lines, # comments.
func readEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path) //nolint:gosec // G304: the file the Manager just wrote
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	vars := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			vars[k] = v
		}
	}
	return vars, sc.Err()
}

// TestOQ6_GoldenPerBaseSHA produces the data for SCOPE.md §23 open question
// 6 (golden tree per base_sha or per branch). It asserts nothing: it builds
// the golden tree of develop and of develop~1 (1% of the files differ) and
// reports the build time and the disk of each.
func TestOQ6_GoldenPerBaseSHA(t *testing.T) {
	root := volumeRoot(t)
	repoDir, rep, label := prepareRepo(t, root, profileFromEnv())
	e := newBenchEnv(t, root, repoDir, label, rep, config.Cache{})
	s := e.stack(e.cfg)
	res := e.newResult("TestOQ6_GoldenPerBaseSHA", seederGoldenIndex)

	g1, ms1, d1 := e.buildGolden(s.golden, baseBranch)
	g2, ms2, d2 := e.buildGolden(s.golden, baseBranch+"~1")
	require.NotEqual(t, g1.BaseSHA, g2.BaseSHA)
	res.GoldenBuildMS, res.GoldenBytes = ms1, g1.SizeBytes
	res.GoldenPerSHA = &GoldenPerSHA{
		ChangedFilesPct: 1, FirstBuildMS: ms1, FirstDiskBytes: d1,
		SecondBuildMS: ms2, SecondDiskBytes: d2, SecondGoldenBytes: g2.SizeBytes,
	}

	ws1, c1 := e.create(s.mgr, "from-develop", baseBranch)
	ws2, c2 := e.create(s.mgr, "from-parent", baseBranch+"~1")
	res.Creates = []Create{c1, c2}
	res.MaxMS = Max(createMS(res.Creates))
	same, err := e.vols.SameVolume(t.Context(), g1.IndexFile, ws1.Path)
	require.NoError(t, err)
	res.IndexOnWorkspaceVolume = same
	res.Notes = append(res.Notes, "data only, no assertion; feeds SCOPE.md §23 open question 6",
		"creates[0] is from develop, creates[1] from develop~1; workspaces "+ws1.ID+" and "+ws2.ID)
	publish(t, &res)
}
