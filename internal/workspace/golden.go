package workspace

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/deverity/cowflow/internal/config"
	"github.com/deverity/cowflow/internal/domain"
	"github.com/deverity/cowflow/internal/git"
	"github.com/deverity/cowflow/internal/platform"
	"github.com/deverity/cowflow/internal/store"
)

// Golden tree tuning. None of these is a config key (SCOPE.md §22 has none).
const (
	goldenWaitFirst  = 100 * time.Millisecond // first back-off while another process builds
	goldenWaitMax    = time.Second            // back-off cap
	goldenRacyWait   = time.Second            // wait before the index refresh (racily clean entries)
	renameAttempts   = 5                      // os.Rename tries (Windows: AV or indexer handles)
	renameRetryFirst = 50 * time.Millisecond
	maxReasonLen     = 1000 // bytes of an error kept in golden_trees.last_error
)

// Golden is a ready golden tree: the materialized base_sha that cow_clone
// workspaces clone from (SCOPE.md §6.4). Its files are immutable: no code
// writes into Dir or IndexFile once the row is ready.
type Golden struct {
	ProjectID string
	BaseSHA   string
	Dir       string // <golden_root>/<sha>, canonical
	IndexFile string // <Dir>.index, beside the tree, never inside it (U-PLAT-1)
	SizeBytes int64
	FileCount int // regular files and symlinks
}

// GoldenInfo is one golden tree as `cowflow golden list` shows it.
type GoldenInfo struct {
	Golden
	ID         string // gld_<ULID>, the row identity
	State      domain.GoldenState
	InUse      int
	LastUsedAt time.Time
	LastError  string
}

// GCReport is what GC did. SHAs are listed in LRU order.
type GCReport struct {
	Removed      []string // golden trees deleted (directory and row)
	FreedBytes   int64    // recorded size of the removed trees
	SkippedInUse []string // over budget but in use (in_use > 0), kept
	Pending      []string // left in deleting because removal failed; retried by the next GC
	FailedRows   []string // failed builds whose rows were removed
	Warnings     []string
}

// GoldenDeps are the GoldenCache's collaborators. Config, Store and Git are
// required; every other field has a production default.
type GoldenDeps struct {
	Config  *config.Config
	Store   Store
	Git     GitOps
	Volumes Volumes      // default PlatformVolumes
	IDs     domain.IDGen // default: ULIDs from Clock
	Clock   domain.Clock // default domain.SystemClock
	Logger  *slog.Logger // default slog.Default()
}

// GoldenCache builds, hands out and garbage-collects golden trees, one per
// (project, base_sha) (SCOPE.md §6.4, §11). It keeps three properties:
//
//   - built once: the store's ClaimGolden elects one builder, even across
//     daemonless CLI processes; everyone else waits;
//   - never half-visible: a tree is built in <dir>.tmp-<op_id> on the same
//     volume and renamed into place, and only a ready row is ever handed
//     out;
//   - immutable once ready: nothing in this package writes into a ready
//     tree, and files are never marked read-only (a clone would inherit
//     the attribute).
//
// Callers must pass the Manager's Dev Drive mount gate before Acquire, which
// creates directories under the golden root. A GoldenCache is safe for
// concurrent use.
type GoldenCache struct {
	cfg   config.Config
	store Store
	git   GitOps
	vols  Volumes
	ids   domain.IDGen
	clock domain.Clock
	log   *slog.Logger
}

// NewGoldenCache returns a GoldenCache over d, filling the defaults of the
// optional dependencies. A missing required dependency is reported by the
// first method that needs it.
func NewGoldenCache(d GoldenDeps) *GoldenCache {
	c := &GoldenCache{store: d.Store, git: d.Git, vols: d.Volumes, ids: d.IDs, clock: d.Clock, log: d.Logger}
	if d.Config != nil {
		c.cfg = *d.Config
	}
	if c.vols == nil {
		c.vols = PlatformVolumes{}
	}
	if c.clock == nil {
		c.clock = domain.SystemClock{}
	}
	if c.ids == nil {
		c.ids = domain.NewULIDGen(c.clock)
	}
	if c.log == nil {
		c.log = slog.Default()
	}
	return c
}

// Golden returns the golden tree cache over the Manager's configuration and
// collaborators: the cow_clone provider acquires from it, and `cowflow
// golden list|gc` (S1-T18) lists and collects it.
func (m *Manager) Golden() *GoldenCache {
	return &GoldenCache{
		cfg: m.cfg, store: m.store, git: m.git, vols: m.vols,
		ids: m.ids, clock: m.clock, log: m.log,
	}
}

// goldenDir returns the directory of the golden tree of sha:
// <golden_root>/<sha>, where an empty [workspace].golden_root means
// <workspace.root>/golden (SCOPE.md §6.4, §23 decision 18; SPEC-GAPS C7).
// It is the only place that knows the location. It is empty when neither
// root is set. sha must already be validated as a hex object name.
func goldenDir(cfg config.Config, sha string) string {
	root := cfg.Workspace.EffectiveGoldenRoot()
	if root == "" {
		return ""
	}
	return filepath.Join(filepath.Clean(filepath.FromSlash(root)), sha)
}

// goldenIndexFile is the index of the golden tree at dir. It lives beside
// the tree, never inside it, so no workspace receives a copy of it
// (SCOPE.md §23 decision 39).
func goldenIndexFile(dir string) string { return dir + ".index" }

func (c *GoldenCache) ready() error {
	switch {
	case c.store == nil:
		return fmt.Errorf("%w: no store", errNotConfigured)
	case c.git == nil:
		return fmt.Errorf("%w: no git", errNotConfigured)
	}
	return nil
}

// paths returns the canonical golden root and the canonical, confined
// directory of sha. It reads the filesystem but never writes it.
func (c *GoldenCache) paths(sha string) (root, dir string, err error) {
	if !git.IsSHA(sha) {
		return "", "", fmt.Errorf("golden tree: base %q is not a full hex SHA", sha)
	}
	d := goldenDir(c.cfg, sha)
	if d == "" {
		return "", "", ErrRootNotSet
	}
	if !filepath.IsAbs(d) {
		return "", "", fmt.Errorf("golden tree: golden root %q is not absolute", filepath.Dir(d))
	}
	if root, err = platform.Canonical(filepath.Dir(d)); err != nil {
		return "", "", fmt.Errorf("golden root: %w", err)
	}
	if dir, err = c.confine(root, d); err != nil {
		return "", "", err
	}
	return root, dir, nil
}

// confine returns p canonicalized and confined strictly below root
// (SCOPE.md §17). Every write and removal goes through it.
func (c *GoldenCache) confine(root, p string) (string, error) {
	cp, err := platform.Confine(root, p)
	if err != nil {
		return "", fmt.Errorf("golden tree path: %w", err)
	}
	if samePath(cp, root) {
		return "", fmt.Errorf("%w: %q is the golden root itself", platform.ErrOutsideRoot, p)
	}
	return cp, nil
}

// staleAfter is how long a building row may go without progress before
// another process takes it over, and how old a last use must be for an
// in_use count to be stale: [scheduler].command_timeout_seconds (§6.4).
func (c *GoldenCache) staleAfter() time.Duration {
	s := c.cfg.Scheduler.CommandTimeoutSeconds
	if s <= 0 {
		s = config.Default().Scheduler.CommandTimeoutSeconds
	}
	return time.Duration(s) * time.Second
}

// Acquire returns the ready golden tree of sha for projectID, building it
// from repo first when nobody has. Concurrent callers, in this process or
// others, build each SHA once: the store elects one builder and the others
// poll with a back-off from 100 ms to 1 s until it is ready.
//
// The caller holds an in_use reference until it calls release (idempotent),
// which GC respects. release uses a context detached from ctx, so it works
// after ctx is canceled.
func (c *GoldenCache) Acquire(ctx context.Context, projectID, repo, sha string) (g Golden, release func(), err error) {
	if err := c.ready(); err != nil {
		return Golden{}, nil, err
	}
	if projectID == "" {
		return Golden{}, nil, errors.New("golden tree: project ID is required")
	}
	if !filepath.IsAbs(repo) {
		return Golden{}, nil, fmt.Errorf("golden tree: repository %q is not absolute", repo)
	}
	root, dir, err := c.paths(sha)
	if err != nil {
		return Golden{}, nil, err
	}
	opID, err := c.ids.New(domain.KindOperation)
	if err != nil {
		return Golden{}, nil, fmt.Errorf("golden tree: new op id: %w", err)
	}
	stale := c.staleAfter()
	wait := goldenWaitFirst
	missed := false
	for {
		claim, err := c.store.ClaimGolden(ctx, projectID, sha, opID, stale)
		if err != nil {
			return Golden{}, nil, fmt.Errorf("claim golden tree %s: %w", sha, err)
		}
		switch claim.Action {
		case domain.GoldenActionReady:
			release := c.releaser(ctx, projectID, sha)
			if _, err := os.Lstat(dir); err != nil {
				release()
				return Golden{}, nil, fmt.Errorf("golden tree %s is ready but its directory is unusable: %w", sha, err)
			}
			if !missed {
				c.log.Info("golden cache hit", "project_id", projectID, "base_sha", sha,
					"size_bytes", claim.Tree.SizeBytes, "file_count", claim.Tree.FileCount)
			}
			return Golden{
				ProjectID: projectID, BaseSHA: sha, Dir: dir, IndexFile: goldenIndexFile(dir),
				SizeBytes: claim.Tree.SizeBytes, FileCount: claim.Tree.FileCount,
			}, release, nil
		case domain.GoldenActionBuild:
			missed = c.logMiss(missed, projectID, sha, "build")
			if err := c.build(ctx, projectID, repo, sha, opID, root, dir); err != nil {
				return Golden{}, nil, err
			}
			// The next claim sees ready and takes the in_use reference.
		case domain.GoldenActionWait:
			missed = c.logMiss(missed, projectID, sha, "wait")
			select {
			case <-ctx.Done():
				return Golden{}, nil, fmt.Errorf("wait for golden tree %s: %w", sha, ctx.Err())
			case <-c.clock.After(wait):
			}
			wait = min(2*wait, goldenWaitMax)
		default:
			return Golden{}, nil, fmt.Errorf("claim golden tree %s: unknown action %q", sha, claim.Action)
		}
	}
}

func (c *GoldenCache) logMiss(missed bool, projectID, sha, action string) bool {
	if !missed {
		c.log.Info("golden cache miss", "project_id", projectID, "base_sha", sha, "action", action)
	}
	return true
}

// releaser returns the idempotent release of one in_use reference.
func (c *GoldenCache) releaser(ctx context.Context, projectID, sha string) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.staleAfter())
			defer cancel()
			if err := c.store.UseGolden(rctx, projectID, sha, -1); err != nil {
				// GC treats a reference that is never released as stale.
				c.log.Warn("release golden tree reference", "project_id", projectID, "base_sha", sha, "err", err)
			}
		})
	}
}

// errTakenOver means another process took over this build as stale; the
// row is no longer ours to finish or fail.
var errTakenOver = errors.New("golden tree build taken over by another process")

// build materializes sha into dir. It runs only while this op holds the
// building claim. On any error it removes everything it wrote, marks the
// row failed (unless another builder took it over) and returns the error.
func (c *GoldenCache) build(ctx context.Context, projectID, repo, sha, opID, root, dir string) (err error) {
	start := c.clock.Now()
	tmp := dir + ".tmp-" + opID
	tmpIndex := goldenIndexFile(tmp)
	index := goldenIndexFile(dir)
	renamed := false
	defer func() {
		if err == nil {
			return
		}
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.staleAfter())
		defer cancel()
		cleanup := []string{tmp, tmpIndex}
		if renamed {
			cleanup = append(cleanup, dir, index)
		}
		for _, p := range cleanup {
			if rerr := c.removeAside(cctx, root, p); rerr != nil {
				c.log.Warn("remove partial golden tree", "path", p, "err", rerr)
			}
		}
		if !errors.Is(err, errTakenOver) {
			if ferr := c.store.FailGolden(cctx, projectID, sha, truncateReason(err.Error())); ferr != nil {
				err = errors.Join(err, fmt.Errorf("mark golden tree failed: %w", ferr))
			}
		}
		c.log.Warn("golden tree build failed", "project_id", projectID, "base_sha", sha, "op_id", opID, "err", err)
		err = fmt.Errorf("build golden tree %s: %w", sha, err)
	}()

	if tmp, err = c.confine(root, tmp); err != nil {
		return err
	}
	if tmpIndex, err = c.confine(root, tmpIndex); err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return fmt.Errorf("create golden root: %w", err)
	}
	if err := os.Mkdir(tmp, 0o750); err != nil {
		return fmt.Errorf("create build directory: %w", err)
	}
	if err := c.sameVolume(ctx, tmp); err != nil {
		return err
	}
	if err := c.git.ExportTree(ctx, repo, sha, tmp, tmpIndex); err != nil {
		return err
	}
	// Files written within the index's own timestamp granularity are
	// "racily clean"; a refresh after the granularity has passed records
	// their stat data for good (sprint-1.md §4.7).
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.clock.After(goldenRacyWait):
	}
	if err := c.git.RefreshIndex(ctx, repo, tmp, tmpIndex); err != nil {
		return err
	}
	files, size, gitlinks, err := measureTree(ctx, tmp)
	if err != nil {
		return err
	}
	if err := checkIndexMatches(tmpIndex, files+gitlinks); err != nil {
		return err
	}
	if err := c.stillBuilder(ctx, projectID, sha, opID); err != nil {
		return err
	}
	// A crashed builder may have renamed a tree in place without finishing
	// the row. Nobody reads it (the row is not ready), so it goes.
	if err := c.removeAside(ctx, root, dir); err != nil {
		return fmt.Errorf("remove leftover golden tree: %w", err)
	}
	renamed = true // from here a failure also cleans the final paths
	if err := c.rename(ctx, tmpIndex, index); err != nil {
		return err
	}
	if err := c.rename(ctx, tmp, dir); err != nil {
		return err
	}
	if err := c.store.FinishGolden(ctx, projectID, sha, size, files); err != nil {
		return fmt.Errorf("mark golden tree ready: %w", err)
	}
	c.log.Info("golden tree built", "project_id", projectID, "base_sha", sha, "op_id", opID,
		"golden_build_ms", c.clock.Now().Sub(start).Milliseconds(), "size_bytes", size, "file_count", files)
	return nil
}

// sameVolume asserts that the build directory and [workspace].root share a
// volume: block clone works only within one (SCOPE.md §23 decision 31).
func (c *GoldenCache) sameVolume(ctx context.Context, tmp string) error {
	wsRoot := c.cfg.Workspace.Root
	if wsRoot == "" {
		return ErrRootNotSet
	}
	same, err := c.vols.SameVolume(ctx, tmp, wsRoot)
	if err != nil {
		return fmt.Errorf("compare golden and workspace volumes: %w", err)
	}
	if !same {
		return fmt.Errorf("golden root %s and workspace root %s are on different volumes", filepath.Dir(tmp), wsRoot)
	}
	return nil
}

// stillBuilder checks, just before the renames, that no other process took
// this build over as stale.
//
// The store's FinishGolden and FailGolden do not compare builder_op_id, so
// a narrow race remains between this check and the renames; it needs a
// builder held past command_timeout_seconds.
func (c *GoldenCache) stillBuilder(ctx context.Context, projectID, sha, opID string) error {
	rows, err := c.store.ListGolden(ctx, projectID)
	if err != nil {
		return fmt.Errorf("re-read golden tree: %w", err)
	}
	for _, g := range rows {
		if g.BaseSHA == sha {
			if g.State == domain.GoldenBuilding && g.BuilderOpID == opID {
				return nil
			}
			return fmt.Errorf("%w: row is %s by %s", errTakenOver, g.State, g.BuilderOpID)
		}
	}
	return fmt.Errorf("%w: row is gone", errTakenOver)
}

// measureTree counts the regular files and symlinks under dir and their
// bytes, and the empty directories: an export creates a directory with no
// entries only for a submodule (gitlink) entry.
func measureTree(ctx context.Context, dir string) (files int, size int64, gitlinks int, err error) {
	children := map[string]int{}
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if p == dir {
			return nil
		}
		if files%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		children[filepath.Dir(p)]++
		switch t := d.Type(); {
		case t.IsDir():
			children[p] += 0
		case t.IsRegular():
			fi, err := d.Info()
			if err != nil {
				return err
			}
			files++
			size += fi.Size()
		case t&fs.ModeSymlink != 0:
			files++
		default:
			return fmt.Errorf("golden tree: unexpected file type %v at %s", t, p)
		}
		return nil
	})
	if err != nil {
		return 0, 0, 0, fmt.Errorf("measure golden tree: %w", err)
	}
	for p, n := range children {
		if n == 0 && p != dir {
			gitlinks++
		}
	}
	return files, size, gitlinks, nil
}

// indexMagic is the signature of a git index file (gitformat-index(5)).
var indexMagic = []byte("DIRC")

// checkIndexMatches compares the entry count in the index header with the
// number of paths the export materialized. They differ when paths collide
// on a case-insensitive filesystem (A.txt and a.txt): one overwrites the
// other and the golden would silently differ from the commit. A split
// index (core.splitIndex) also differs and fails here, explicitly.
func checkIndexMatches(indexFile string, paths int) error {
	f, err := os.Open(indexFile) //nolint:gosec // G304: indexFile is confined under the golden root
	if err != nil {
		return fmt.Errorf("read golden index: %w", err)
	}
	defer func() { _ = f.Close() }() // read-only; the read error is what matters
	var hdr [12]byte
	if _, err := io.ReadFull(f, hdr[:]); err != nil {
		return fmt.Errorf("read golden index header: %w", err)
	}
	if !bytes.Equal(hdr[:4], indexMagic) {
		return errors.New("golden index: not a git index file")
	}
	if v := binary.BigEndian.Uint32(hdr[4:8]); v < 2 || v > 4 {
		return fmt.Errorf("golden index: unsupported version %d", v)
	}
	if n := binary.BigEndian.Uint32(hdr[8:12]); int64(n) != int64(paths) {
		return fmt.Errorf("golden tree has %d paths but the commit has %d index entries "+
			"(case-colliding paths on a case-insensitive filesystem?)", paths, n)
	}
	return nil
}

// rename renames src to dst, retrying with a short back-off: on Windows an
// antivirus or indexer handle inside the tree makes it fail for a moment.
func (c *GoldenCache) rename(ctx context.Context, src, dst string) error {
	wait := renameRetryFirst
	var err error
	for i := range renameAttempts {
		if err = os.Rename(src, dst); err == nil || errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if i == renameAttempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return errors.Join(err, ctx.Err())
		case <-c.clock.After(wait):
		}
		wait *= 2
	}
	return err
}

// removeAside removes p, confined under root, if it exists. A directory is
// first renamed to <p>.deleting-<ulid> so that no reader ever sees it half
// deleted; a failed removal leaves that name for the next GC.
func (c *GoldenCache) removeAside(ctx context.Context, root, p string) error {
	p, err := c.confine(root, p)
	if err != nil {
		return err
	}
	fi, err := os.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	id, err := ulid.New(ulid.Timestamp(c.clock.Now()), rand.Reader)
	if err != nil {
		return fmt.Errorf("new ulid: %w", err)
	}
	aside := p + ".deleting-" + id.String()
	if err := c.rename(ctx, p, aside); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	return os.RemoveAll(aside)
}

func truncateReason(s string) string {
	if len(s) <= maxReasonLen {
		return s
	}
	return s[:maxReasonLen] + "…"
}

// List returns the project's golden trees, least recently used first.
func (c *GoldenCache) List(ctx context.Context, projectID string) ([]GoldenInfo, error) {
	if err := c.ready(); err != nil {
		return nil, err
	}
	rows, err := c.store.ListGolden(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list golden trees: %w", err)
	}
	sortLRU(rows)
	out := make([]GoldenInfo, 0, len(rows))
	for _, g := range rows {
		_, dir, err := c.paths(g.BaseSHA)
		if err != nil {
			return nil, err
		}
		out = append(out, GoldenInfo{
			ID: g.ID,
			Golden: Golden{
				ProjectID: g.ProjectID, BaseSHA: g.BaseSHA, Dir: dir, IndexFile: goldenIndexFile(dir),
				SizeBytes: g.SizeBytes, FileCount: g.FileCount,
			},
			State: g.State, InUse: g.InUse, LastUsedAt: g.LastUsedAt, LastError: g.LastError,
		})
	}
	return out, nil
}

func sortLRU(rows []domain.GoldenTree) {
	slices.SortStableFunc(rows, func(a, b domain.GoldenTree) int {
		if n := a.LastUsedAt.Compare(b.LastUsedAt); n != 0 {
			return n
		}
		return strings.Compare(a.BaseSHA, b.BaseSHA)
	})
}

// GC keeps the project's ready golden trees within budgetBytes
// ([scheduler].golden_cache_gb in bytes), deleting least recently used
// first (SCOPE.md §6.4, §11). It never deletes a tree that is building or in
// use (in_use > 0): the store's MarkGoldenDeleting is the check that keeps
// GC from racing an Acquire. A tree is renamed to <dir>.deleting-<ulid>
// before removal; when removal fails (Windows handles) the row stays
// deleting and the next GC retries it.
//
// GC also retries deleting rows, removes failed rows, and removes the
// leftover <dir>.tmp-*, <dir>.deleting-* and failed-build paths of the
// project's SHAs, sparing the build directory of a live builder.
//
// A stale in_use (last use older than command_timeout_seconds) is reported
// as a warning but not reset: the store has no method for it yet.
func (c *GoldenCache) GC(ctx context.Context, projectID string, budgetBytes int64) (GCReport, error) {
	var rep GCReport
	if err := c.ready(); err != nil {
		return rep, err
	}
	if budgetBytes < 0 {
		return rep, fmt.Errorf("golden gc: negative budget %d", budgetBytes)
	}
	now := c.clock.Now()
	stale := c.staleAfter()
	// U-PLAT-13: a count with no use for command_timeout_seconds leaked from a
	// crashed process; reset it with a warning before judging in-use trees.
	reset, err := c.store.ResetStaleGoldenInUse(ctx, projectID, now.Add(-stale))
	if err != nil {
		return rep, fmt.Errorf("golden gc: reset stale references: %w", err)
	}
	for _, sha := range reset {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf(
			"golden tree %s: in_use reset to 0, not used for %s", sha, stale))
		c.log.Warn("golden gc: reset stale in_use", "project_id", projectID, "base_sha", sha, "stale_after", stale)
	}
	rows, err := c.store.ListGolden(ctx, projectID)
	if err != nil {
		return rep, fmt.Errorf("golden gc: list: %w", err)
	}
	sortLRU(rows)
	shas := make([]string, 0, len(rows))
	var ready []domain.GoldenTree
	var total int64
	for _, g := range rows {
		shas = append(shas, g.BaseSHA)
		switch g.State {
		case domain.GoldenDeleting:
			c.gcDelete(ctx, projectID, g, &rep)
		case domain.GoldenFailed:
			switch err := c.store.DeleteGolden(ctx, projectID, g.BaseSHA); {
			case err == nil:
				rep.FailedRows = append(rep.FailedRows, g.BaseSHA)
			case errors.Is(err, store.ErrInvalidTransition):
				// A new build claimed it in the meantime.
			default:
				return rep, fmt.Errorf("golden gc: remove failed row %s: %w", g.BaseSHA, err)
			}
		case domain.GoldenReady:
			ready = append(ready, g)
			total += g.SizeBytes
		}
	}
	for _, g := range ready {
		if total <= budgetBytes {
			break
		}
		marked, err := c.store.MarkGoldenDeleting(ctx, projectID, g.BaseSHA)
		if err != nil {
			return rep, fmt.Errorf("golden gc: mark %s deleting: %w", g.BaseSHA, err)
		}
		if !marked {
			rep.SkippedInUse = append(rep.SkippedInUse, g.BaseSHA)
			continue
		}
		if c.gcDelete(ctx, projectID, g, &rep) {
			total -= g.SizeBytes
		}
	}
	if err := c.sweep(ctx, projectID, shas, &rep); err != nil {
		return rep, err
	}
	c.log.Info("golden gc", "project_id", projectID, "budget_bytes", budgetBytes,
		"removed", len(rep.Removed), "freed_bytes", rep.FreedBytes,
		"skipped_in_use", len(rep.SkippedInUse), "pending", len(rep.Pending))
	return rep, nil
}

// gcDelete removes the tree of a deleting row, then the row. It reports
// whether the tree is gone; on failure the row stays deleting.
func (c *GoldenCache) gcDelete(ctx context.Context, projectID string, g domain.GoldenTree, rep *GCReport) bool {
	err := func() error {
		root, dir, err := c.paths(g.BaseSHA)
		if err != nil {
			return err
		}
		if err := c.removeAside(ctx, root, dir); err != nil {
			return err
		}
		if err := c.removeAside(ctx, root, goldenIndexFile(dir)); err != nil {
			return err
		}
		return c.store.DeleteGolden(ctx, projectID, g.BaseSHA)
	}()
	if err != nil {
		rep.Pending = append(rep.Pending, g.BaseSHA)
		rep.Warnings = append(rep.Warnings, fmt.Sprintf("golden tree %s: removal failed, retried by the next gc: %v", g.BaseSHA, err))
		c.log.Warn("golden gc: remove", "project_id", projectID, "base_sha", g.BaseSHA, "err", err)
		return false
	}
	rep.Removed = append(rep.Removed, g.BaseSHA)
	rep.FreedBytes += g.SizeBytes
	return true
}

// sweep removes leftovers of the given SHAs from the golden root: every
// <sha>.deleting-*, every <sha>.tmp-* except the build directory of a live
// (non-stale) builder, and <sha> and <sha>.index when the row is failed or
// gone. The directory is read before the rows, so a builder that started
// before the read is seen as live. Names of other SHAs or projects, and
// anything not shaped like a golden path, are never touched.
func (c *GoldenCache) sweep(ctx context.Context, projectID string, shas []string, rep *GCReport) error {
	if len(shas) == 0 {
		return nil
	}
	root, _, err := c.paths(shas[0])
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("golden gc: read golden root: %w", err)
	}
	rows, err := c.store.ListGolden(ctx, projectID)
	if err != nil {
		return fmt.Errorf("golden gc: list: %w", err)
	}
	byS := make(map[string]domain.GoldenTree, len(rows))
	for _, g := range rows {
		byS[g.BaseSHA] = g
	}
	now, stale := c.clock.Now(), c.staleAfter()
	for _, e := range entries {
		sha, rest, _ := strings.Cut(e.Name(), ".")
		if !slices.Contains(shas, sha) {
			continue
		}
		g, hasRow := byS[sha]
		var remove bool
		switch {
		case strings.HasPrefix(rest, "deleting-"):
			remove = true
		case strings.HasPrefix(rest, "tmp-"):
			op := strings.TrimSuffix(strings.TrimPrefix(rest, "tmp-"), ".index")
			live := hasRow && g.State == domain.GoldenBuilding && g.BuilderOpID == op && g.UpdatedAt.Add(stale).After(now)
			remove = !live
		case rest == "" || rest == "index":
			remove = !hasRow || g.State == domain.GoldenFailed
		}
		if !remove {
			continue
		}
		if err := c.removeAside(ctx, root, filepath.Join(root, e.Name())); err != nil {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("golden gc: remove %s: %v", e.Name(), err))
		}
	}
	return nil
}
