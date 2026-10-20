package bench

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// Environment variables of the benchmarks.
const (
	// EnvRoot is a directory on the volume under test. It takes precedence
	// over COWFLOW_TEST_COW_DIR and COWFLOW_TEST_REFS_DIR.
	EnvRoot = "COWFLOW_BENCH_ROOT"
	// EnvOut is the directory that receives the result JSON files.
	EnvOut = "COWFLOW_BENCH_OUT"
	// EnvProfile names the benchrepo profile of TestPhase1 and TestOQ6
	// (small, large or monorepo; default small).
	EnvProfile = "COWFLOW_BENCH_PROFILE"
	// EnvLarge enables the tests that need a repository of at least 1 GiB.
	EnvLarge = "COWFLOW_BENCH_LARGE"
	// EnvDefender records whether the volume is excluded from Defender
	// real-time scanning ("true" or "false"). The benchmarks never change
	// Defender settings (sprint-1.md C14).
	EnvDefender = "COWFLOW_BENCH_DEFENDER_EXCLUDED"
)

// createsPerRun is the number of sequential creates in one run (§20.1).
const createsPerRun = 20

// Create is one workspace creation.
type Create struct {
	CreateMS   float64 `json:"create_ms"`
	CLIMS      float64 `json:"cli_ms,omitempty"`
	DFreeBytes int64   `json:"dfree_bytes"`
}

// RunSummary is one run of 20 sequential creates.
type RunSummary struct {
	MaxMS          float64 `json:"max_ms"`
	IncrementalPct float64 `json:"incremental_pct"`
}

// Concurrent is the unasserted run of 20 simultaneous creates.
type Concurrent struct {
	WallMS   float64   `json:"wall_ms"`
	P50MS    float64   `json:"p50_ms"`
	P95MS    float64   `json:"p95_ms"`
	MaxMS    float64   `json:"max_ms"`
	CreateMS []float64 `json:"create_ms"`
	Failed   int       `json:"failed"`
}

// CacheResult is the outcome of TestAC17_SharedCacheSecondSetup. Setup is
// workspace create + go mod download + go build ./... (decision 43).
type CacheResult struct {
	IsolatedWS2MS []float64 `json:"isolated_ws2_ms"`
	SharedWS2MS   []float64 `json:"shared_ws2_ms"`
	IsolatedMS    float64   `json:"isolated_median_ms"`
	SharedMS      float64   `json:"shared_median_ms"`
	Ratio         float64   `json:"shared_over_isolated"`
	Modules       int       `json:"modules"`
}

// GoldenPerSHA is the data of TestOQ6_GoldenPerBaseSHA (SCOPE.md §23 open
// question 6).
type GoldenPerSHA struct {
	ChangedFilesPct   float64 `json:"changed_files_pct"`
	FirstBuildMS      float64 `json:"first_build_ms"`
	FirstDiskBytes    int64   `json:"first_disk_bytes"`
	SecondBuildMS     float64 `json:"second_build_ms"`
	SecondDiskBytes   int64   `json:"second_disk_bytes"`
	SecondGoldenBytes int64   `json:"second_golden_bytes"`
}

// Result is the JSON document of one benchmark.
type Result struct {
	Test                   string        `json:"test"`
	GOOS                   string        `json:"goos"`
	Filesystem             string        `json:"filesystem"`
	DevDrive               bool          `json:"dev_drive"`
	DefenderExcluded       *bool         `json:"defender_excluded"` // null: not recorded
	GitVersion             string        `json:"git_version"`
	CPU                    string        `json:"cpu"`
	RAMGB                  float64       `json:"ram_gb"`
	RepoProfile            string        `json:"repo_profile"`
	Bytes                  int64         `json:"bytes"`
	Files                  int           `json:"files"`
	GoldenBytes            int64         `json:"golden_bytes"`
	GoldenBuildMS          float64       `json:"golden_build_ms"`
	FirstCreateMS          float64       `json:"first_create_ms"` // cold metadata, reported apart
	Creates                []Create      `json:"creates"`
	Runs                   []RunSummary  `json:"runs"`
	P50MS                  float64       `json:"p50_ms"`
	P95MS                  float64       `json:"p95_ms"`
	MaxMS                  float64       `json:"max_ms"`
	IncrementalPct         float64       `json:"incremental_pct"`
	BytesPerFile           float64       `json:"incremental_bytes_per_file"`
	Seeder                 string        `json:"seeder"`
	IndexOnWorkspaceVolume bool          `json:"index_on_workspace_volume"`
	Commit                 string        `json:"commit"`
	TimingAsserted         bool          `json:"timing_asserted"`
	DiskAsserted           bool          `json:"disk_asserted"`
	Concurrent             *Concurrent   `json:"concurrent,omitempty"`
	Cache                  *CacheResult  `json:"cache,omitempty"`
	GoldenPerSHA           *GoldenPerSHA `json:"golden_per_sha,omitempty"`
	Notes                  []string      `json:"notes,omitempty"`
}

// Median returns the median of xs; 0 for no values. The input is not
// modified.
func Median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := slices.Clone(xs)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// Percentile returns the nearest-rank percentile p (0 < p <= 100) of xs.
func Percentile(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := slices.Clone(xs)
	sort.Float64s(s)
	rank := int(math.Ceil(p / 100 * float64(len(s))))
	return s[min(max(rank, 1), len(s))-1]
}

// Max returns the largest of xs; 0 for no values.
func Max(xs []float64) float64 {
	m := 0.0
	for _, x := range xs {
		m = max(m, x)
	}
	return m
}

// IncrementalPct is the free-space delta per workspace as a percentage of
// the golden tree: (sum of dfree / n) / golden x 100 (decision 43).
func IncrementalPct(dfree []int64, goldenBytes int64) float64 {
	if len(dfree) == 0 || goldenBytes <= 0 {
		return 0
	}
	var sum int64
	for _, d := range dfree {
		sum += d
	}
	return float64(sum) / float64(len(dfree)) / float64(goldenBytes) * 100
}

// Summarize fills the timing and disk statistics of r from its sequential
// runs: the run whose maximum is the median of the run maxima supplies
// Creates, P50MS, P95MS and MaxMS (so MaxMS is "max of 20, median of 3"),
// and IncrementalPct is the median over the runs. GoldenBytes and Files
// must be set first.
func (r *Result) Summarize(all [][]Create) error {
	if len(all) == 0 {
		return errors.New("bench: no runs to summarize")
	}
	r.Runs = r.Runs[:0]
	maxima := make([]float64, 0, len(all))
	pcts := make([]float64, 0, len(all))
	for _, run := range all {
		if len(run) == 0 {
			return errors.New("bench: empty run")
		}
		d := make([]int64, len(run))
		for i, c := range run {
			d[i] = c.DFreeBytes
		}
		s := RunSummary{MaxMS: Max(createMS(run)), IncrementalPct: IncrementalPct(d, r.GoldenBytes)}
		r.Runs = append(r.Runs, s)
		maxima = append(maxima, s.MaxMS)
		pcts = append(pcts, s.IncrementalPct)
	}
	// The run at the median maximum (the upper one for an even count).
	idx := make([]int, len(all))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return maxima[idx[a]] < maxima[idx[b]] })
	mid := all[idx[len(idx)/2]]
	ms := createMS(mid)
	r.Creates = slices.Clone(mid)
	r.P50MS, r.P95MS, r.MaxMS = Median(ms), Percentile(ms, 95), Max(ms)
	r.IncrementalPct = Median(pcts)
	if r.Files > 0 && r.GoldenBytes > 0 {
		r.BytesPerFile = r.IncrementalPct / 100 * float64(r.GoldenBytes) / float64(r.Files)
	}
	return nil
}

func createMS(cs []Create) []float64 {
	out := make([]float64, len(cs))
	for i, c := range cs {
		out[i] = c.CreateMS
	}
	return out
}

// ParseDefender converts the value of EnvDefender: nil when unset or not a
// boolean.
func ParseDefender(v string) *bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes":
		t := true
		return &t
	case "0", "false", "no":
		f := false
		return &f
	}
	return nil
}

// Write stores r as <dir>/<test>-<profile>-<seeder>-<goos>-<filesystem>.json
// and returns the path. An empty dir writes nothing.
func (r *Result) Write(dir string) (string, error) {
	if dir == "" {
		return "", nil
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("bench: create %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", fmt.Errorf("bench: encode result: %w", err)
	}
	name := fileName(r.Test, r.RepoProfile, r.Seeder, r.GOOS, r.Filesystem) + ".json"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return "", fmt.Errorf("bench: write %s: %w", path, err)
	}
	return path, nil
}

// fileName joins the non-empty parts and keeps only characters that are safe
// in a file name on every OS.
func fileName(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		}
		return '-'
	}, strings.Join(kept, "-"))
}
