package benchrepo

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/deverity/cowflow/internal/git"
)

// EnvRepo names the environment variable that points benchmarks at an
// existing repository instead of a generated one.
const EnvRepo = "COWFLOW_BENCH_REPO"

const (
	// MaxPathLen is the longest repo-relative path the generator emits, so
	// checkouts stay far from the Windows path limit.
	MaxPathLen = 180

	// gitAttributes is the SCOPE.md §7 line-ending policy.
	gitAttributes = "* text=auto eol=lf\n"

	changeEvery   = 100 // develop's third commit changes 1% of the files
	filesPerPkg   = 20
	symlinkCount  = 20
	gitTimeout    = 30 * time.Minute
	baseUnix      = 946684800 // 2000-01-01T00:00:00Z
	commitAuthor  = "COWFlow Bench <bench@cowflow.invalid>"
	streamBufSize = 1 << 20
	sizeFixpoint  = 12
)

// Profile describes one benchmark repository shape.
type Profile struct {
	Name       string
	Files      int   // number of files under src/
	TargetSize int64 // total bytes of the files under src/
	MinSize    int64 // smallest src file
	MaxSize    int64 // largest src file
	Median     int64 // median of the log-normal size distribution
	Sigma      float64
	// Symlinks adds a links/ directory with relative symbolic links (Unix
	// tests). Without it the repository contains no symlinks at all.
	Symlinks bool
}

// Small is about 50 MiB in about 2,000 files: unit and CI tests.
var Small = Profile{Name: "small", Files: 2000, TargetSize: 50 << 20,
	MinSize: 512, MaxSize: 512 << 10, Median: 12 << 10, Sigma: 1.0}

// Large is about 1.2 GiB in about 6,000 files of 50 KiB to 5 MiB: the
// file-size bound of acceptance criterion 21.
var Large = Profile{Name: "large", Files: 6000, TargetSize: 1200 << 20,
	MinSize: 50 << 10, MaxSize: 5 << 20, Median: 120 << 10, Sigma: 1.0}

// Monorepo is about 1.2 GiB in about 60,000 files with a log-normal size
// distribution (median about 8 KiB): the file-count bound of criterion 21.
var Monorepo = Profile{Name: "monorepo", Files: 60000, TargetSize: 1200 << 20,
	MinSize: 64, MaxSize: 8 << 20, Median: 8 << 10, Sigma: 1.38}

// ProfileByName returns the built-in profile with that name.
func ProfileByName(name string) (Profile, error) {
	for _, p := range []Profile{Small, Large, Monorepo} {
		if p.Name == name {
			return p, nil
		}
	}
	return Profile{}, fmt.Errorf("benchrepo: unknown profile %q (want small, large or monorepo)", name)
}

// Report summarizes a repository. Bytes and Files count every blob in the
// tree of HEAD, as `git ls-tree -r -l HEAD` lists them.
type Report struct {
	Bytes    int64             `json:"bytes"`
	Files    int               `json:"files"`
	HeadTree string            `json:"head_tree"`
	Branches map[string]string `json:"branches"`
}

// Generate creates a repository in dir (which must be missing or empty) and
// checks out main. It holds two commits on main, a develop branch from main
// and a third commit on develop that rewrites 1% of the src files. The same
// profile and seed always produce the same commits and trees.
func Generate(ctx context.Context, dir string, p Profile, seed int64) (Report, error) {
	if err := validateProfile(p); err != nil {
		return Report{}, err
	}
	if err := prepareDir(dir); err != nil {
		return Report{}, err
	}
	env, cleanup, err := hermeticEnv()
	if err != nil {
		return Report{}, err
	}
	defer cleanup()

	if err := runGit(ctx, env, dir, "init", "-q", "--template=", "-b", "main"); err != nil {
		return Report{}, err
	}
	if err := runGit(ctx, env, dir, "config", "core.autocrlf", "false"); err != nil {
		return Report{}, err
	}

	pl, err := newPlan(p, seed)
	if err != nil {
		return Report{}, err
	}
	if err := fastImport(ctx, env, dir, pl); err != nil {
		return Report{}, err
	}
	if err := runGit(ctx, env, dir, "reset", "-q", "--hard", "main"); err != nil {
		return Report{}, err
	}
	return Inspect(ctx, dir)
}

// Inspect reports on any repository: the bytes and files of HEAD (so a real
// repository can be checked against "at least 1 GiB"), the tree of HEAD and
// the local branches.
func Inspect(ctx context.Context, dir string) (Report, error) {
	env, cleanup, err := hermeticEnv()
	if err != nil {
		return Report{}, err
	}
	defer cleanup()
	runner, err := git.NewExecRunner(git.ExecOptions{Timeout: gitTimeout, ExtraEnv: env})
	if err != nil {
		return Report{}, fmt.Errorf("benchrepo: %w", err)
	}

	out, err := runner.Run(ctx, dir, "ls-tree", "-r", "-l", "-z", "HEAD")
	if err != nil {
		return Report{}, fmt.Errorf("benchrepo: list HEAD: %w", err)
	}
	rep := Report{Branches: map[string]string{}}
	for _, rec := range bytes.Split(out, []byte{0}) {
		if len(rec) == 0 {
			continue
		}
		meta, _, ok := strings.Cut(string(rec), "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 4 {
			return Report{}, fmt.Errorf("benchrepo: unexpected ls-tree record %q", rec)
		}
		if fields[1] != "blob" { // submodule commits have no size
			continue
		}
		n, err := strconv.ParseInt(fields[3], 10, 64)
		if err != nil {
			return Report{}, fmt.Errorf("benchrepo: bad size in ls-tree record %q: %w", rec, err)
		}
		rep.Bytes += n
		rep.Files++
	}

	tree, err := runner.Run(ctx, dir, "rev-parse", "HEAD^{tree}")
	if err != nil {
		return Report{}, fmt.Errorf("benchrepo: HEAD tree: %w", err)
	}
	rep.HeadTree = strings.TrimSpace(string(tree))

	refs, err := runner.Run(ctx, dir, "for-each-ref", "--format=%(refname:short) %(objectname)", "refs/heads")
	if err != nil {
		return Report{}, fmt.Errorf("benchrepo: list branches: %w", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(refs)), "\n") {
		if name, sha, ok := strings.Cut(line, " "); ok {
			rep.Branches[name] = sha
		}
	}
	return rep, nil
}

// ExternalRepo returns the repository named by COWFLOW_BENCH_REPO, if set.
func ExternalRepo() (string, bool) {
	v := os.Getenv(EnvRepo)
	return v, v != ""
}

func validateProfile(p Profile) error {
	switch {
	case p.Files <= 0:
		return errors.New("benchrepo: profile needs at least one file")
	case p.MinSize <= 0 || p.MaxSize < p.MinSize:
		return fmt.Errorf("benchrepo: bad size bounds %d..%d", p.MinSize, p.MaxSize)
	case p.TargetSize < p.MinSize*int64(p.Files) || p.TargetSize > p.MaxSize*int64(p.Files):
		return fmt.Errorf("benchrepo: target %d bytes is outside %d files of %d..%d bytes",
			p.TargetSize, p.Files, p.MinSize, p.MaxSize)
	case p.Median <= 0 || p.Sigma < 0:
		return errors.New("benchrepo: bad size distribution")
	}
	return nil
}

func prepareDir(dir string) error {
	entries, err := os.ReadDir(dir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("benchrepo: create %s: %w", dir, err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("benchrepo: read %s: %w", dir, err)
	case len(entries) > 0:
		return fmt.Errorf("benchrepo: %s is not empty", dir)
	}
	return nil
}

// hermeticEnv isolates git from the developer's configuration, so the same
// input produces the same objects on every machine.
func hermeticEnv() (env []string, cleanup func(), err error) {
	home, err := os.MkdirTemp("", "cowflow-benchrepo-*")
	if err != nil {
		return nil, nil, fmt.Errorf("benchrepo: temp home: %w", err)
	}
	cleanup = func() { _ = os.RemoveAll(home) } // best effort: a leftover temp dir is harmless
	cfg := filepath.Join(home, "gitconfig")
	if err := os.WriteFile(cfg, nil, 0o600); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("benchrepo: empty git config: %w", err)
	}
	return []string{
		"GIT_CONFIG_GLOBAL=" + cfg,
		"GIT_CONFIG_NOSYSTEM=1",
		"HOME=" + home,
		"USERPROFILE=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, "xdg"),
	}, cleanup, nil
}

// gitCommand builds a git command with a clean GIT_* environment. Callers
// that need stdin (fast-import) cannot use git.Runner.
func gitCommand(ctx context.Context, env []string, dir string, args ...string) (*exec.Cmd, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("benchrepo: %w: %w", git.ErrNotFound, err)
	}
	cmd := exec.CommandContext(ctx, gitPath, append([]string{"-C", dir}, args...)...) //nolint:gosec // G204: fixed git executable, argv slice, no shell
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(kv), "GIT_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	cmd.Env = append(cmd.Env, env...)
	cmd.WaitDelay = 5 * time.Second
	return cmd, nil
}

func runGit(ctx context.Context, env []string, dir string, args ...string) error {
	cmd, err := gitCommand(ctx, env, dir, args...)
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("benchrepo: git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// fastImport streams the whole history into one git fast-import process.
func fastImport(ctx context.Context, env []string, dir string, pl *plan) error {
	cmd, err := gitCommand(ctx, env, dir, "-c", "core.compression=1", "fast-import", "--quiet", "--depth=0")
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("benchrepo: fast-import stdin: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("benchrepo: start fast-import: %w", err)
	}
	w := bufio.NewWriterSize(stdin, streamBufSize)
	werr := pl.writeStream(ctx, w)
	if werr == nil {
		werr = w.Flush()
	}
	_ = stdin.Close() // Wait closes it too; a second close error carries no information
	if werr != nil {
		// git exiting early makes the write fail with a broken pipe; its
		// own stderr is the useful message.
		_ = cmd.Wait() // the stream error is the one reported
		if stderr.Len() > 0 {
			return fmt.Errorf("benchrepo: fast-import: %w: %s", werr, strings.TrimSpace(stderr.String()))
		}
		return fmt.Errorf("benchrepo: fast-import: %w", werr)
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("benchrepo: fast-import: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// plan is the deterministic description of the repository.
type plan struct {
	seed  int64
	paths []string // src/<area>/<pkg>/<file>, index = file number
	sizes []int64
	links []link
}

type link struct{ path, target string }

var areas = []string{"auth", "billing", "catalog", "checkout", "search",
	"media", "orders", "reports", "shipping", "users"}

var extensions = []string{"go", "txt", "json", "bin"}

func newPlan(p Profile, seed int64) (*plan, error) {
	pl := &plan{seed: seed}
	rng := rand.New(rand.NewPCG(uint64(seed), 0x53495a45)) //nolint:gosec // G115: the bit pattern only seeds a deterministic PRNG
	pl.sizes = planSizes(p, rng)
	seen := make(map[string]struct{}, p.Files+32)
	check := func(name string) error {
		if err := CheckPath(name); err != nil {
			return err
		}
		key := strings.ToLower(name)
		if _, dup := seen[key]; dup {
			return fmt.Errorf("benchrepo: duplicate path (case-insensitive) %q", name)
		}
		seen[key] = struct{}{}
		return nil
	}
	for _, f := range scaffoldNames {
		if err := check(f); err != nil {
			return nil, err
		}
	}
	pl.paths = make([]string, p.Files)
	for i := range pl.paths {
		pkg := i / filesPerPkg
		name := fmt.Sprintf("src/%s/pkg%04d/f%06d.%s", areas[pkg%len(areas)], pkg, i, extensions[i%len(extensions)])
		if err := check(name); err != nil {
			return nil, err
		}
		pl.paths[i] = name
	}
	if p.Symlinks {
		for i := range symlinkCount {
			l := link{
				path:   fmt.Sprintf("links/l%03d", i),
				target: "../" + pl.paths[(i*(len(pl.paths)/symlinkCount+1))%len(pl.paths)],
			}
			if err := check(l.path); err != nil {
				return nil, err
			}
			pl.links = append(pl.links, l)
		}
	}
	return pl, nil
}

// planSizes draws log-normal sizes, clamps them to the profile bounds and
// rescales until they sum to exactly TargetSize.
func planSizes(p Profile, rng *rand.Rand) []int64 {
	raw := make([]float64, p.Files)
	for i := range raw {
		raw[i] = float64(p.Median) * math.Exp(p.Sigma*rng.NormFloat64())
	}
	sizes := make([]int64, p.Files)
	clamp := func(v float64) int64 {
		return min(max(int64(math.Round(v)), p.MinSize), p.MaxSize)
	}
	scale := 1.0
	for range sizeFixpoint {
		var sum int64
		for i, r := range raw {
			sizes[i] = clamp(r * scale)
			sum += sizes[i]
		}
		if sum == p.TargetSize {
			return sizes
		}
		scale *= float64(p.TargetSize) / float64(sum)
	}
	// Spread the remaining difference over the files that have room.
	var sum int64
	for _, s := range sizes {
		sum += s
	}
	for i := 0; sum != p.TargetSize; i = (i + 1) % len(sizes) {
		switch {
		case sum < p.TargetSize && sizes[i] < p.MaxSize:
			d := min(p.TargetSize-sum, p.MaxSize-sizes[i], 4096)
			sizes[i] += d
			sum += d
		case sum > p.TargetSize && sizes[i] > p.MinSize:
			d := min(sum-p.TargetSize, sizes[i]-p.MinSize, 4096)
			sizes[i] -= d
			sum -= d
		}
	}
	return sizes
}

// content fills buf with incompressible bytes that only depend on the seed,
// the file number and the variant. The first byte is NUL, so Git always
// treats the file as binary and "text=auto" never rewrites it.
func (pl *plan) content(buf []byte, file, variant int) {
	var key [32]byte
	binary.LittleEndian.PutUint64(key[0:], uint64(pl.seed))  //nolint:gosec // G115: the bit pattern is the point
	binary.LittleEndian.PutUint64(key[8:], uint64(file))     //nolint:gosec // G115: file is non-negative
	binary.LittleEndian.PutUint64(key[16:], uint64(variant)) //nolint:gosec // G115: variant is non-negative
	_, _ = rand.NewChaCha8(key).Read(buf)                    // Read on a ChaCha8 generator never fails
	buf[0] = 0
}

var scaffold = map[string]string{
	".gitattributes":           gitAttributes,
	"go.mod":                   "module example.invalid/benchrepo\n\ngo 1.22\n",
	"README.md":                "# Benchmark repository\n\nGenerated by COWFlow's benchrepo. Do not edit.\n",
	"contracts/api.v1.md":      "# API contract v1\n\nSynthetic contract file.\n",
	"contracts/events.v1.json": "{\"version\":1,\"events\":[]}\n",
}

var scaffoldNames = func() []string {
	names := make([]string, 0, len(scaffold))
	for n := range scaffold {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}()

func (pl *plan) writeStream(ctx context.Context, w *bufio.Writer) error {
	// Commit 1 on main: scaffolding.
	writeCommit(w, "refs/heads/main", "", 1, "chore: scaffold benchmark repository")
	for _, n := range scaffoldNames {
		writeInline(w, "100644", n, []byte(scaffold[n]))
	}

	// Commit 2 on main: every src file (and the symlinks).
	writeCommit(w, "refs/heads/main", "", 2, "feat: add benchmark sources")
	var largest int64
	for _, s := range pl.sizes {
		largest = max(largest, s)
	}
	buf := make([]byte, largest)
	for i, name := range pl.paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		b := buf[:pl.sizes[i]]
		pl.content(b, i, 0)
		writeInline(w, "100644", name, b)
	}
	for _, l := range pl.links {
		writeInline(w, "120000", l.path, []byte(l.target))
	}

	// Commit 3 on develop: rewrite 1% of the files, same sizes.
	writeCommit(w, "refs/heads/develop", "refs/heads/main", 3, "fix: change one percent of the files")
	offset := int(((pl.seed % changeEvery) + changeEvery) % changeEvery)
	for i := offset; i < len(pl.paths); i += changeEvery {
		if err := ctx.Err(); err != nil {
			return err
		}
		b := buf[:pl.sizes[i]]
		pl.content(b, i, 1)
		writeInline(w, "100644", pl.paths[i], b)
	}
	_, err := fmt.Fprint(w, "done\n")
	return err
}

// writeCommit and writeInline ignore write errors: bufio.Writer keeps the
// first one and Flush reports it.
func writeCommit(w *bufio.Writer, ref, from string, n int, msg string) {
	_, _ = fmt.Fprintf(w, "commit %s\ncommitter %s %d +0000\ndata %d\n%s\n", ref, commitAuthor, baseUnix+n*60, len(msg), msg)
	if from != "" {
		_, _ = fmt.Fprintf(w, "from %s\n", from)
	}
}

func writeInline(w *bufio.Writer, mode, name string, data []byte) {
	_, _ = fmt.Fprintf(w, "M %s inline %s\ndata %d\n", mode, name, len(data))
	_, _ = w.Write(data)
	_ = w.WriteByte('\n')
}

// reserved are the device names Windows refuses as file names, with or
// without an extension.
var reserved = func() map[string]struct{} {
	m := map[string]struct{}{"CON": {}, "PRN": {}, "AUX": {}, "NUL": {}}
	for i := '1'; i <= '9'; i++ {
		m["COM"+string(i)] = struct{}{}
		m["LPT"+string(i)] = struct{}{}
	}
	return m
}()

// CheckPath reports why a repo-relative, slash-separated path cannot be
// created on Windows, or is longer than MaxPathLen.
func CheckPath(p string) error {
	if p == "" || len(p) > MaxPathLen {
		return fmt.Errorf("benchrepo: path %q is empty or longer than %d characters", p, MaxPathLen)
	}
	if path.IsAbs(p) || strings.Contains(p, "\\") {
		return fmt.Errorf("benchrepo: path %q is not repo-relative with slashes", p)
	}
	for _, part := range strings.Split(p, "/") {
		switch {
		case part == "" || part == "." || part == "..":
			return fmt.Errorf("benchrepo: path %q has an empty or dot component", p)
		case strings.HasSuffix(part, ".") || strings.HasSuffix(part, " "):
			return fmt.Errorf("benchrepo: path %q has a component ending in a dot or space", p)
		case strings.ContainsAny(part, "<>:\"|?*"):
			return fmt.Errorf("benchrepo: path %q has a character Windows forbids", p)
		}
		for _, r := range part {
			if r < 0x20 {
				return fmt.Errorf("benchrepo: path %q has a control character", p)
			}
		}
		stem, _, _ := strings.Cut(part, ".")
		if _, bad := reserved[strings.ToUpper(strings.TrimRight(stem, " "))]; bad {
			return fmt.Errorf("benchrepo: path %q uses the reserved Windows name %q", p, stem)
		}
	}
	return nil
}
