package workspace_test

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/config"
	"github.com/deverity/cowflow/internal/domain"
	"github.com/deverity/cowflow/internal/git"
	"github.com/deverity/cowflow/internal/platform"
	"github.com/deverity/cowflow/internal/store"
	"github.com/deverity/cowflow/internal/workspace"
	"github.com/deverity/cowflow/internal/workspace/workspacetest"
	"github.com/deverity/cowflow/test/harness"
)

const goldenProject = "prj_1"

// shaN returns a distinct fake 40-character SHA.
func shaN(n int) string { return fmt.Sprintf("%040x", n) }

// writeIndexHeader writes a git index file that is only a header announcing
// entries entries; GoldenCache reads nothing more.
func writeIndexHeader(t testing.TB, path string, entries int) {
	t.Helper()
	b := make([]byte, 12)
	copy(b, "DIRC")
	binary.BigEndian.PutUint32(b[4:], 2)
	binary.BigEndian.PutUint32(b[8:], uint32(entries)) //nolint:gosec // G115: test sizes are tiny
	require.NoError(t, os.WriteFile(path, b, 0o600))
}

// fakeExport makes FakeGit's ExportTree materialize files (slash paths) and
// an index header with extra additional entries. extra > 0 simulates paths
// that collided on a case-insensitive filesystem.
func fakeExport(t testing.TB, g *workspacetest.FakeGit, files map[string]string, extra int) {
	t.Helper()
	g.Hook = func(c workspacetest.Call) error {
		if c.Method != "ExportTree" {
			return nil
		}
		dst, idx := c.Args[2].(string), c.Args[3].(string)
		for name, content := range files {
			p := filepath.Join(dst, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
				return err
			}
			if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
				return err
			}
		}
		writeIndexHeader(t, idx, len(files)+extra)
		return nil
	}
}

// autoAdvance moves clock forward by step every millisecond of real time
// until the test ends, so waits on the fake clock complete.
func autoAdvance(t *testing.T, clock *harness.Clock, step time.Duration) {
	t.Helper()
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
				clock.Advance(step)
			}
		}
	})
	t.Cleanup(func() { close(done); wg.Wait() })
}

type goldenFixture struct {
	base, wsRoot, goldenRoot, repo string
	cfg                            *config.Config
	store                          *workspacetest.MemStore
	git                            *workspacetest.FakeGit
	clock                          *harness.Clock
	vols                           *workspacetest.FakeVolumes
}

func newGoldenFixture(t *testing.T) *goldenFixture {
	t.Helper()
	base := realDir(t)
	clock := harness.NewClock(time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC))
	f := &goldenFixture{
		base:   base,
		wsRoot: filepath.Join(base, "ws"),
		repo:   filepath.Join(base, "repo"),
		cfg:    config.Default(),
		store:  workspacetest.NewMemStore(clock),
		git:    &workspacetest.FakeGit{},
		clock:  clock,
		vols:   workspacetest.NewFakeVolumes(platform.Volume{Filesystem: "btrfs", ID: "vol-base", MountPath: base}),
	}
	f.goldenRoot = filepath.Join(f.wsRoot, "golden") // the default: <workspace.root>/golden
	require.NoError(t, os.Mkdir(f.wsRoot, 0o750))
	require.NoError(t, os.Mkdir(f.repo, 0o750))
	f.cfg.Workspace.Root = f.wsRoot
	f.cfg.Scheduler.CommandTimeoutSeconds = 5
	return f
}

func (f *goldenFixture) cache() *workspace.GoldenCache {
	return workspace.NewGoldenCache(workspace.GoldenDeps{
		Config: f.cfg, Store: f.store, Git: f.git, Volumes: f.vols, Clock: f.clock,
		Logger: slog.New(slog.DiscardHandler),
	})
}

func (f *goldenFixture) row(t *testing.T, sha string) (domain.GoldenTree, bool) {
	t.Helper()
	rows, err := f.store.ListGolden(context.Background(), goldenProject)
	require.NoError(t, err)
	for _, g := range rows {
		if g.BaseSHA == sha {
			return g, true
		}
	}
	return domain.GoldenTree{}, false
}

// rootEntries lists the names in the golden root.
func rootEntries(t *testing.T, root string) []string {
	t.Helper()
	es, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	require.NoError(t, err)
	var names []string
	for _, e := range es {
		names = append(names, e.Name())
	}
	return names
}

func TestGoldenAcquireBuildsThenHits(t *testing.T) {
	f := newGoldenFixture(t)
	autoAdvance(t, f.clock, 100*time.Millisecond)
	files := map[string]string{"a.txt": "aaaa", "sub/b.txt": "bb", "sub/deep/c.txt": "c"}
	fakeExport(t, f.git, files, 0)
	c := f.cache()
	sha := shaN(1)

	g, release, err := c.Acquire(context.Background(), goldenProject, f.repo, sha)
	require.NoError(t, err)
	dir := filepath.Join(f.goldenRoot, sha)
	assert.Equal(t, dir, g.Dir, "golden tree at <workspace.root>/golden/<sha>")
	assert.Equal(t, dir+".index", g.IndexFile, "index beside the tree")
	assert.Equal(t, sha, g.BaseSHA)
	assert.Equal(t, 3, g.FileCount)
	assert.Equal(t, int64(7), g.SizeBytes)
	assert.FileExists(t, filepath.Join(dir, "sub", "deep", "c.txt"))
	assert.NoFileExists(t, filepath.Join(dir, ".index"), "nothing of the index inside the tree")
	assert.ElementsMatch(t, []string{sha, sha + ".index"}, rootEntries(t, f.goldenRoot), "no temp left")

	row, ok := f.row(t, sha)
	require.True(t, ok)
	assert.Equal(t, domain.GoldenReady, row.State)
	assert.Equal(t, 1, row.InUse)
	assert.Equal(t, int64(7), row.SizeBytes)
	assert.Equal(t, 3, row.FileCount)

	// Export, then refresh after the racy wait, on the temp paths.
	var methods []string
	for _, call := range f.git.Calls() {
		methods = append(methods, call.Method)
	}
	assert.Equal(t, []string{"ExportTree", "RefreshIndex"}, methods)
	exp := f.git.Calls()[0].Args
	assert.Equal(t, f.repo, exp[0])
	assert.True(t, strings.HasPrefix(exp[2].(string), dir+".tmp-op_"), "build in <dir>.tmp-<op_id>: %v", exp[2])
	assert.Equal(t, exp[2].(string)+".index", exp[3])

	release()
	release() // idempotent
	row, _ = f.row(t, sha)
	assert.Equal(t, 0, row.InUse)

	g2, release2, err := c.Acquire(context.Background(), goldenProject, f.repo, sha)
	require.NoError(t, err)
	defer release2()
	assert.Equal(t, g, g2)
	assert.Equal(t, 1, f.git.Count("ExportTree"), "a hit builds nothing")
}

func TestGoldenRootFromConfig(t *testing.T) {
	f := newGoldenFixture(t)
	autoAdvance(t, f.clock, 100*time.Millisecond)
	f.cfg.Workspace.GoldenRoot = filepath.ToSlash(filepath.Join(f.base, "gold"))
	fakeExport(t, f.git, map[string]string{"a": "a"}, 0)

	g, release, err := f.cache().Acquire(context.Background(), goldenProject, f.repo, shaN(2))
	require.NoError(t, err)
	defer release()
	assert.Equal(t, filepath.Join(f.base, "gold", shaN(2)), g.Dir)
}

func TestGoldenAcquireRejectsBadInput(t *testing.T) {
	f := newGoldenFixture(t)
	c := f.cache()
	ctx := context.Background()
	for _, tc := range []struct{ name, project, repo, sha string }{
		{"short sha", goldenProject, f.repo, "abc123"},
		{"traversal", goldenProject, f.repo, "../" + shaN(1)[3:]},
		{"relative repo", goldenProject, "repo", shaN(1)},
		{"no project", "", f.repo, shaN(1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := c.Acquire(ctx, tc.project, tc.repo, tc.sha)
			require.Error(t, err)
		})
	}
	f.cfg.Workspace.Root = ""
	_, _, err := f.cache().Acquire(ctx, goldenProject, f.repo, shaN(1))
	require.ErrorIs(t, err, workspace.ErrRootNotSet)
	assert.Zero(t, f.git.Count("ExportTree"))
}

func TestGoldenBuildOncePerSHA(t *testing.T) {
	base := realDir(t)
	wsRoot := filepath.Join(base, "ws")
	repo := filepath.Join(base, "repo")
	require.NoError(t, os.Mkdir(wsRoot, 0o750))
	require.NoError(t, os.Mkdir(repo, 0o750))
	cfg := config.Default()
	cfg.Workspace.Root = wsRoot
	dbPath := filepath.Join(base, "cowflow.db")
	ctx := context.Background()

	first, err := store.Open(ctx, dbPath, store.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })
	var project domain.Project
	require.NoError(t, first.WithTx(ctx, "", func(tx *store.Tx) error {
		project, err = tx.InsertProject("demo", repo)
		return err
	}))

	fg := &workspacetest.FakeGit{}
	fakeExport(t, fg, map[string]string{"a.txt": "a", "b/c.txt": "c"}, 0)
	vols := workspacetest.NewFakeVolumes(platform.Volume{Filesystem: "refs", ID: "vol", MountPath: base})
	sha := shaN(7)

	const n = 20
	caches := make([]*workspace.GoldenCache, n)
	for i := range caches {
		s, err := store.Open(ctx, dbPath, store.Options{}) // a connection per "process"
		require.NoError(t, err)
		t.Cleanup(func() { _ = s.Close() })
		caches[i] = workspace.NewGoldenCache(workspace.GoldenDeps{
			Config: cfg, Store: s, Git: fg, Volumes: vols, Logger: slog.New(slog.DiscardHandler),
		})
	}

	dirs := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Go(func() {
			<-start
			g, release, err := caches[i].Acquire(ctx, project.ID, repo, sha)
			if err == nil {
				dirs[i] = g.Dir
				release()
			}
			errs[i] = err
		})
	}
	close(start)
	wg.Wait()

	for i := range n {
		require.NoError(t, errs[i], "acquire %d", i)
	}
	assert.Equal(t, 1, fg.Count("ExportTree"), "exactly one build")
	want := filepath.Join(wsRoot, "golden", sha)
	for i := range n {
		assert.Equal(t, want, dirs[i], "acquire %d", i)
	}
	rows, err := first.ListGolden(ctx, project.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, domain.GoldenReady, rows[0].State)
	assert.Equal(t, 0, rows[0].InUse, "every reference released")
}

func TestGoldenCaseCollisionFails(t *testing.T) {
	f := newGoldenFixture(t)
	autoAdvance(t, f.clock, 100*time.Millisecond)
	// Two index entries (A.txt, a.txt) but one file on disk.
	fakeExport(t, f.git, map[string]string{"a.txt": "a"}, 1)

	_, _, err := f.cache().Acquire(context.Background(), goldenProject, f.repo, shaN(3))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "case-colliding")
	row, ok := f.row(t, shaN(3))
	require.True(t, ok)
	assert.Equal(t, domain.GoldenFailed, row.State)
	assert.Contains(t, row.LastError, "case-colliding")
	assert.Empty(t, rootEntries(t, f.goldenRoot), "no tree, no temp")
}

func TestGoldenOtherVolumeFails(t *testing.T) {
	f := newGoldenFixture(t)
	autoAdvance(t, f.clock, 100*time.Millisecond)
	other := filepath.Join(f.base, "other")
	require.NoError(t, os.Mkdir(other, 0o750))
	f.vols.Set(platform.Volume{Filesystem: "refs", ID: "vol-other", MountPath: other})
	f.cfg.Workspace.GoldenRoot = filepath.Join(other, "golden")
	fakeExport(t, f.git, map[string]string{"a": "a"}, 0)

	_, _, err := f.cache().Acquire(context.Background(), goldenProject, f.repo, shaN(4))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "different volumes")
	assert.Zero(t, f.git.Count("ExportTree"), "checked before the export")
	assert.Empty(t, rootEntries(t, filepath.Join(other, "golden")))
	row, _ := f.row(t, shaN(4))
	assert.Equal(t, domain.GoldenFailed, row.State)
}

func TestGoldenStaleBuilderTakeover(t *testing.T) {
	f := newGoldenFixture(t)
	ctx := context.Background()
	sha := shaN(5)
	dir := filepath.Join(f.goldenRoot, sha)

	// A builder claimed the SHA, renamed a tree in place and crashed before
	// finishing the row; its temp directory is left too.
	claim, err := f.store.ClaimGolden(ctx, goldenProject, sha, "op_dead", 5*time.Second)
	require.NoError(t, err)
	require.Equal(t, domain.GoldenActionBuild, claim.Action)
	require.NoError(t, os.MkdirAll(filepath.Join(dir+".tmp-op_dead", "x"), 0o750))
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stale.txt"), []byte("old"), 0o600))

	fakeExport(t, f.git, map[string]string{"new.txt": "new"}, 0)
	autoAdvance(t, f.clock, 200*time.Millisecond)
	c := f.cache()
	g, release, err := c.Acquire(ctx, goldenProject, f.repo, sha)
	require.NoError(t, err)
	release()

	assert.Equal(t, 1, f.git.Count("ExportTree"))
	assert.FileExists(t, filepath.Join(g.Dir, "new.txt"))
	assert.NoFileExists(t, filepath.Join(g.Dir, "stale.txt"), "the crashed builder's tree is replaced")
	row, _ := f.row(t, sha)
	assert.Equal(t, domain.GoldenReady, row.State)
	assert.NotEqual(t, "op_dead", row.BuilderOpID)

	rep, err := c.GC(ctx, goldenProject, 1<<30)
	require.NoError(t, err)
	assert.Empty(t, rep.Removed)
	assert.ElementsMatch(t, []string{sha, sha + ".index"}, rootEntries(t, f.goldenRoot), "gc removed the dead temp")
}

func TestGoldenWaitHonorsContext(t *testing.T) {
	f := newGoldenFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	_, err := f.store.ClaimGolden(ctx, goldenProject, shaN(6), "op_other", time.Hour)
	require.NoError(t, err)
	f.cfg.Scheduler.CommandTimeoutSeconds = 3600

	errc := make(chan error, 1)
	go func() {
		_, _, err := f.cache().Acquire(ctx, goldenProject, f.repo, shaN(6))
		errc <- err
	}()
	cancel()
	select {
	case err := <-errc:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(10 * time.Second):
		t.Fatal("Acquire did not return after cancel")
	}
	assert.Zero(t, f.git.Count("ExportTree"))
}

// buildGolden acquires and releases sha with files, advancing the clock
// one second afterwards so last_used_at orders the trees.
func (f *goldenFixture) buildGolden(t *testing.T, c *workspace.GoldenCache, sha string, files map[string]string) workspace.Golden {
	t.Helper()
	fakeExport(t, f.git, files, 0)
	g, release, err := c.Acquire(context.Background(), goldenProject, f.repo, sha)
	require.NoError(t, err)
	release()
	f.clock.Advance(time.Second)
	return g
}

func TestGoldenGCLRUAndBudget(t *testing.T) {
	f := newGoldenFixture(t)
	autoAdvance(t, f.clock, 100*time.Millisecond)
	c := f.cache()
	ctx := context.Background()
	g1 := f.buildGolden(t, c, shaN(1), map[string]string{"f": strings.Repeat("1", 100)})
	g2 := f.buildGolden(t, c, shaN(2), map[string]string{"f": strings.Repeat("2", 200)})
	g3 := f.buildGolden(t, c, shaN(3), map[string]string{"f": strings.Repeat("3", 300)})
	// Use g1 again: LRU order is now g2, g3, g1.
	_, release, err := c.Acquire(ctx, goldenProject, f.repo, shaN(1))
	require.NoError(t, err)
	release()

	list, err := c.List(ctx, goldenProject)
	require.NoError(t, err)
	require.Len(t, list, 3)
	assert.Equal(t, []string{shaN(2), shaN(3), shaN(1)},
		[]string{list[0].BaseSHA, list[1].BaseSHA, list[2].BaseSHA}, "least recently used first")
	assert.Equal(t, g2.Dir, list[0].Dir)

	rep, err := c.GC(ctx, goldenProject, 300) // 600 bytes cached
	require.NoError(t, err)
	assert.Equal(t, []string{shaN(2), shaN(3)}, rep.Removed)
	assert.Equal(t, int64(500), rep.FreedBytes)
	assert.Empty(t, rep.SkippedInUse)
	assert.Empty(t, rep.Pending)

	assert.NoDirExists(t, g2.Dir)
	assert.NoDirExists(t, g3.Dir)
	assert.DirExists(t, g1.Dir)
	assert.ElementsMatch(t, []string{shaN(1), shaN(1) + ".index"}, rootEntries(t, f.goldenRoot))
	_, ok := f.row(t, shaN(2))
	assert.False(t, ok, "row deleted")

	rep, err = c.GC(ctx, goldenProject, 300)
	require.NoError(t, err)
	assert.Empty(t, rep.Removed, "within budget")
}

func TestGoldenGCSkipsInUse(t *testing.T) {
	f := newGoldenFixture(t)
	f.cfg.Scheduler.CommandTimeoutSeconds = 3600
	autoAdvance(t, f.clock, 100*time.Millisecond)
	c := f.cache()
	ctx := context.Background()
	g1 := f.buildGolden(t, c, shaN(1), map[string]string{"f": "1111"})
	g2 := f.buildGolden(t, c, shaN(2), map[string]string{"f": "2222"})
	_, hold, err := c.Acquire(ctx, goldenProject, f.repo, shaN(1)) // g1 in use
	require.NoError(t, err)
	// A live build of a third SHA must stay untouched.
	_, err = f.store.ClaimGolden(ctx, goldenProject, shaN(9), "op_live", time.Hour)
	require.NoError(t, err)
	liveTmp := filepath.Join(f.goldenRoot, shaN(9)+".tmp-op_live")
	require.NoError(t, os.Mkdir(liveTmp, 0o750))

	rep, err := c.GC(ctx, goldenProject, 0)
	require.NoError(t, err)
	assert.Equal(t, []string{shaN(2)}, rep.Removed)
	assert.Equal(t, []string{shaN(1)}, rep.SkippedInUse)
	assert.DirExists(t, g1.Dir)
	assert.NoDirExists(t, g2.Dir)
	assert.DirExists(t, liveTmp, "a live builder's temp is kept")
	row, _ := f.row(t, shaN(9))
	assert.Equal(t, domain.GoldenBuilding, row.State)

	// A reference held past command_timeout_seconds is reported, not reset.
	f.clock.Advance(2 * time.Hour)
	rep, err = c.GC(ctx, goldenProject, 0)
	require.NoError(t, err)
	assert.Equal(t, []string{shaN(1)}, rep.SkippedInUse)
	require.Len(t, rep.Warnings, 1)
	assert.Contains(t, rep.Warnings[0], "in_use=1")
	assert.NoDirExists(t, liveTmp, "the builder is stale now")

	hold()
	rep, err = c.GC(ctx, goldenProject, 0)
	require.NoError(t, err)
	assert.Equal(t, []string{shaN(1)}, rep.Removed)
}

func TestGoldenGCRemovesFailedAndLeftovers(t *testing.T) {
	f := newGoldenFixture(t)
	autoAdvance(t, f.clock, 100*time.Millisecond)
	c := f.cache()
	ctx := context.Background()
	f.buildGolden(t, c, shaN(1), map[string]string{"f": "1"})

	// A failed build whose cleanup did not finish.
	_, err := f.store.ClaimGolden(ctx, goldenProject, shaN(2), "op_x", time.Hour)
	require.NoError(t, err)
	require.NoError(t, f.store.FailGolden(ctx, goldenProject, shaN(2), "boom"))
	for _, p := range []string{shaN(2), shaN(2) + ".tmp-op_x", shaN(1) + ".deleting-01ABC", shaN(1) + ".tmp-op_y"} {
		require.NoError(t, os.MkdirAll(filepath.Join(f.goldenRoot, p, "d"), 0o750))
	}
	require.NoError(t, os.WriteFile(filepath.Join(f.goldenRoot, shaN(2)+".index"), nil, 0o600))
	// Not this project's, or not a golden path: never touched.
	foreign := []string{shaN(8), shaN(8) + ".tmp-op_z", "notes.txt"}
	for _, p := range foreign {
		require.NoError(t, os.MkdirAll(filepath.Join(f.goldenRoot, p), 0o750))
	}

	rep, err := c.GC(ctx, goldenProject, 1<<30)
	require.NoError(t, err)
	assert.Equal(t, []string{shaN(2)}, rep.FailedRows)
	assert.Empty(t, rep.Removed)
	_, ok := f.row(t, shaN(2))
	assert.False(t, ok)
	assert.ElementsMatch(t, append([]string{shaN(1), shaN(1) + ".index"}, foreign...), rootEntries(t, f.goldenRoot))
}

func TestGoldenGCRetriesDeleting(t *testing.T) {
	f := newGoldenFixture(t)
	autoAdvance(t, f.clock, 100*time.Millisecond)
	c := f.cache()
	ctx := context.Background()
	g := f.buildGolden(t, c, shaN(1), map[string]string{"f": "123"})
	// An earlier GC marked it deleting and could not remove the directory.
	marked, err := f.store.MarkGoldenDeleting(ctx, goldenProject, shaN(1))
	require.NoError(t, err)
	require.True(t, marked)

	rep, err := c.GC(ctx, goldenProject, 1<<30)
	require.NoError(t, err)
	assert.Equal(t, []string{shaN(1)}, rep.Removed)
	assert.Equal(t, int64(3), rep.FreedBytes)
	assert.NoDirExists(t, g.Dir)
	assert.Empty(t, rootEntries(t, f.goldenRoot))
}

func TestGoldenGCStoreErrorKeepsRow(t *testing.T) {
	f := newGoldenFixture(t)
	autoAdvance(t, f.clock, 100*time.Millisecond)
	c := f.cache()
	ctx := context.Background()
	f.buildGolden(t, c, shaN(1), map[string]string{"f": "1"})
	f.store.Errs = map[string]error{"DeleteGolden": errors.New("disk I/O error")}

	rep, err := c.GC(ctx, goldenProject, 0)
	require.NoError(t, err)
	assert.Equal(t, []string{shaN(1)}, rep.Pending)
	row, _ := f.row(t, shaN(1))
	assert.Equal(t, domain.GoldenDeleting, row.State, "retried by the next gc")

	f.store.Errs = nil
	rep, err = c.GC(ctx, goldenProject, 0)
	require.NoError(t, err)
	assert.Equal(t, []string{shaN(1)}, rep.Removed)
}

// --- real Git ---

// faultRunner fails every git call whose argv contains match with exit
// code 1, like a harness.GitFault, and passes the others to inner. Unlike
// harness.FaultyRunner it also implements git.OptsRunner, which ExportTree
// needs.
type faultRunner struct {
	inner git.OptsRunner
	match string
}

func (f faultRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return f.RunWith(ctx, dir, git.RunOpts{}, args...)
}

func (f faultRunner) RunWith(ctx context.Context, dir string, o git.RunOpts, args ...string) ([]byte, error) {
	if slices.Contains(args, f.match) {
		return nil, &git.Error{Args: slices.Clone(args), ExitCode: 1, Err: errors.New("exit status 1")}
	}
	return f.inner.RunWith(ctx, dir, o, args...)
}

// realGolden is a fixture over real Git: the repository and the golden root
// share one filesystem (read-tree --index-output needs it).
type realGolden struct {
	base, wsRoot string
	repo         *harness.Repo
	cfg          *config.Config
	store        *workspacetest.MemStore
	vols         *workspacetest.FakeVolumes
}

func newRealGolden(t *testing.T) *realGolden {
	t.Helper()
	base := realDir(t)
	r := &realGolden{
		base:   base,
		wsRoot: filepath.Join(base, "ws"),
		repo:   harness.NewRepo(t, harness.WithDir(filepath.Join(base, "repo"))),
		cfg:    config.Default(),
		store:  workspacetest.NewMemStore(domain.SystemClock{}),
		vols:   workspacetest.NewFakeVolumes(platform.Volume{Filesystem: "btrfs", ID: "vol", MountPath: base}),
	}
	require.NoError(t, os.Mkdir(r.wsRoot, 0o750))
	r.cfg.Workspace.Root = r.wsRoot
	return r
}

func (r *realGolden) cache(runner git.Runner) *workspace.GoldenCache {
	return workspace.NewGoldenCache(workspace.GoldenDeps{
		Config: r.cfg, Store: r.store, Git: git.NewClient(runner), Volumes: r.vols,
		Logger: slog.New(slog.DiscardHandler),
	})
}

func (r *realGolden) commit(t *testing.T) string {
	t.Helper()
	return r.repo.Commit(t, "feat: content", map[string]string{
		"README.md":           "# demo\n",
		"cmd/app/main.go":     "package main\n\nfunc main() {}\n",
		"internal/x/x.go":     "package x\n",
		"contracts/api.yaml":  "openapi: 3.1.0\n",
		"docs/deep/a/b/c.txt": strings.Repeat("c", 4096),
	})
}

func TestGoldenHalfBuiltNeverVisible(t *testing.T) {
	r := newRealGolden(t)
	sha := r.commit(t)
	inner, ok := r.repo.Runner.(git.OptsRunner)
	require.True(t, ok)
	c := r.cache(faultRunner{inner: inner, match: "checkout-index"})

	_, _, err := c.Acquire(context.Background(), goldenProject, r.repo.Dir, sha)
	require.Error(t, err)
	var gerr *git.Error
	require.ErrorAs(t, err, &gerr)
	assert.Equal(t, 1, gerr.ExitCode)

	golden := filepath.Join(r.wsRoot, "golden")
	assert.NoDirExists(t, filepath.Join(golden, sha), "no final directory")
	assert.Empty(t, rootEntries(t, golden), "temp tree and index removed")
	rows, err := r.store.ListGolden(context.Background(), goldenProject)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, domain.GoldenFailed, rows[0].State)
	assert.NotEmpty(t, rows[0].LastError)
}

// gitStatus runs git status against the golden tree with its own index.
func gitStatus(t *testing.T, r *harness.Repo, g workspace.Golden) string {
	t.Helper()
	or, ok := r.Runner.(git.OptsRunner)
	require.True(t, ok)
	out, err := or.RunWith(context.Background(), r.Dir, git.RunOpts{Env: []string{"GIT_INDEX_FILE=" + g.IndexFile}},
		"--work-tree="+g.Dir, "status", "--porcelain", "--untracked-files=all")
	require.NoError(t, err)
	return string(out)
}

func TestGoldenIndexClean(t *testing.T) {
	r := newRealGolden(t)
	sha := r.commit(t)
	g, release, err := r.cache(r.repo.Runner).Acquire(context.Background(), goldenProject, r.repo.Dir, sha)
	require.NoError(t, err)
	defer release()

	assert.Empty(t, gitStatus(t, r.repo, g))
	assert.Equal(t, 6, g.FileCount, "five files and .gitattributes")
	assert.FileExists(t, filepath.Join(g.Dir, "docs", "deep", "a", "b", "c.txt"))
	assert.NoDirExists(t, filepath.Join(g.Dir, ".git"), "a golden tree is files only")
}

// treeHashes hashes every file under dir by relative path.
func treeHashes(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	require.NoError(t, filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p) //nolint:gosec // G304: test tree
		if err != nil {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		out[rel] = hex.EncodeToString(sum[:]) + fi.Mode().String() + fi.ModTime().String()
		return nil
	}))
	return out
}

// cloneTree copies the golden tree into dst file by file through cl, the
// way the cow_clone provider walks it (S1-T10).
func cloneTree(t *testing.T, cl workspace.Cloner, src, dst string) {
	t.Helper()
	require.NoError(t, filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		return cl.CloneFile(context.Background(), p, target)
	}))
}

func TestGoldenImmutableAfterWorkspaces(t *testing.T) {
	r := newRealGolden(t)
	sha := r.commit(t)
	g, release, err := r.cache(r.repo.Runner).Acquire(context.Background(), goldenProject, r.repo.Dir, sha)
	require.NoError(t, err)
	defer release()
	before := treeHashes(t, g.Dir)
	indexBefore, err := os.ReadFile(g.IndexFile)
	require.NoError(t, err)

	cl := &workspacetest.FakeCloner{}
	for _, name := range []string{"w1", "w2"} {
		ws := filepath.Join(r.wsRoot, name)
		cloneTree(t, cl, g.Dir, ws)
		require.NoError(t, os.WriteFile(filepath.Join(ws, "README.md"), []byte("changed in "+name), 0o600))
		fh, err := os.OpenFile(filepath.Join(ws, "docs", "deep", "a", "b", "c.txt"), os.O_APPEND|os.O_WRONLY, 0) //nolint:gosec // G304: test tree
		require.NoError(t, err)
		_, err = fh.WriteString("more")
		require.NoError(t, err)
		require.NoError(t, fh.Close())
		require.NoError(t, os.Remove(filepath.Join(ws, "internal", "x", "x.go")))
		require.NoError(t, os.WriteFile(filepath.Join(ws, "new.txt"), []byte(name), 0o600))
	}

	assert.Equal(t, before, treeHashes(t, g.Dir), "golden files unchanged")
	indexAfter, err := os.ReadFile(g.IndexFile)
	require.NoError(t, err)
	assert.Equal(t, indexBefore, indexAfter, "golden index unchanged")
	assert.Empty(t, gitStatus(t, r.repo, g))
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(filepath.Join(g.Dir, "README.md"))
		require.NoError(t, err)
		assert.NotZero(t, fi.Mode().Perm()&0o200, "golden files are not made read-only")
	}
}
