package bench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMedianAndPercentile(t *testing.T) {
	assert.InDelta(t, 0, Median(nil), 0)
	assert.InDelta(t, 3, Median([]float64{5, 1, 3}), 0)
	assert.InDelta(t, 2.5, Median([]float64{4, 1, 2, 3}), 0)

	xs := make([]float64, 20)
	for i := range xs {
		xs[i] = float64(i + 1)
	}
	assert.InDelta(t, 10, Percentile(xs, 50), 0)
	assert.InDelta(t, 19, Percentile(xs, 95), 0)
	assert.InDelta(t, 20, Percentile(xs, 100), 0)
	assert.InDelta(t, 1, Percentile(xs, 0.1), 0)
	assert.InDelta(t, 0, Percentile(nil, 50), 0)
	assert.InDelta(t, 20, Max(xs), 0)

	in := []float64{3, 1, 2}
	_ = Median(in)
	assert.Equal(t, []float64{3, 1, 2}, in, "the input must not be sorted in place")
}

func TestIncrementalPct(t *testing.T) {
	// 20 workspaces that each took 6 MiB of a 1 GiB golden tree.
	d := make([]int64, 20)
	for i := range d {
		d[i] = 6 << 20
	}
	assert.InDelta(t, 6.0/1024*100, IncrementalPct(d, 1<<30), 1e-9)
	assert.InDelta(t, 0, IncrementalPct(nil, 1<<30), 0)
	assert.InDelta(t, 0, IncrementalPct(d, 0), 0)
	assert.Less(t, IncrementalPct([]int64{-5, 5}, 1<<30), 1e-9, "a net zero delta is zero")
}

func run(maxMS float64, dfree int64) []Create {
	cs := make([]Create, createsPerRun)
	for i := range cs {
		cs[i] = Create{CreateMS: maxMS / 2, DFreeBytes: dfree}
	}
	cs[createsPerRun-1].CreateMS = maxMS
	return cs
}

func TestSummarizeUsesTheMedianRun(t *testing.T) {
	r := Result{GoldenBytes: 1 << 30, Files: 1000}
	// Maxima 900, 3000 and 1500: the median run is the 1500 one.
	require.NoError(t, r.Summarize([][]Create{run(900, 1<<20), run(3000, 8<<20), run(1500, 4<<20)}))
	assert.InDelta(t, 1500, r.MaxMS, 0)
	require.Len(t, r.Runs, 3)
	assert.InDelta(t, 3000, r.Runs[1].MaxMS, 0)
	assert.Len(t, r.Creates, createsPerRun)
	assert.InDelta(t, 750, r.P50MS, 0)
	assert.InDelta(t, 750, r.P95MS, 0, "19 of 20 creates are at half the maximum")
	// Incremental: 1, 8 and 4 MiB of 1 GiB; the median is 4 MiB.
	assert.InDelta(t, 4.0/1024*100, r.IncrementalPct, 1e-9)
	assert.InDelta(t, r.IncrementalPct/100*float64(1<<30)/1000, r.BytesPerFile, 1e-6)
}

func TestSummarizeRejectsNoRuns(t *testing.T) {
	var r Result
	require.Error(t, r.Summarize(nil))
	require.Error(t, r.Summarize([][]Create{{}}))
}

func TestParseDefender(t *testing.T) {
	require.NotNil(t, ParseDefender("true"))
	assert.True(t, *ParseDefender(" TRUE "))
	require.NotNil(t, ParseDefender("false"))
	assert.False(t, *ParseDefender("0"))
	assert.Nil(t, ParseDefender(""))
	assert.Nil(t, ParseDefender("maybe"))
}

func TestResultWriteSchema(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "out")
	r := Result{
		Test: "TestPhase1_TwentyCowWorkspaces", GOOS: "linux", Filesystem: "btrfs",
		RepoProfile: "small", Seeder: "golden-index", GitVersion: "2.50.0",
		Creates: []Create{{CreateMS: 12.5, CLIMS: 40, DFreeBytes: 4096}},
	}
	path, err := r.Write(dir)
	require.NoError(t, err)
	assert.Equal(t, "TestPhase1_TwentyCowWorkspaces-small-golden-index-linux-btrfs.json", filepath.Base(path))

	data, err := os.ReadFile(path) //nolint:gosec // G304: path returned by Write
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(data, &doc))
	for _, key := range []string{
		"goos", "filesystem", "dev_drive", "defender_excluded", "git_version", "cpu", "ram_gb",
		"repo_profile", "bytes", "files", "golden_build_ms", "creates", "p50_ms", "p95_ms",
		"max_ms", "incremental_pct", "seeder", "commit", "index_on_workspace_volume",
	} {
		assert.Contains(t, doc, key)
	}
	assert.Nil(t, doc["defender_excluded"], "an unrecorded Defender state is null, never false")
	creates, ok := doc["creates"].([]any)
	require.True(t, ok)
	first, ok := creates[0].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, first, "create_ms")
	assert.Contains(t, first, "cli_ms")
	assert.Contains(t, first, "dfree_bytes")
}

func TestResultWriteWithoutDirWritesNothing(t *testing.T) {
	path, err := (&Result{}).Write("")
	require.NoError(t, err)
	assert.Empty(t, path)
}

func TestFileNameIsSafe(t *testing.T) {
	assert.Equal(t, "a-b-c-d", fileName("a/b", "", "c", `d`))
	assert.Equal(t, "Test_1.x", fileName("Test_1.x"))
}

func TestHostMachineAndVersions(t *testing.T) {
	m := HostMachine()
	assert.NotEmpty(t, m.CPU)
	assert.GreaterOrEqual(t, m.RAMGB, 0.0)
	assert.NotEmpty(t, GitVersion(t.Context()))
	assert.Equal(t, "unknown", SourceCommit(t.Context(), t.TempDir()))
}

func TestFreeSpaceIsPositive(t *testing.T) {
	n, err := FreeSpace(t.TempDir())
	require.NoError(t, err)
	assert.Positive(t, n)
}
