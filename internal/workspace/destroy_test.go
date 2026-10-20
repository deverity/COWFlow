package workspace_test

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/config"
	"github.com/deverity/cowflow/internal/domain"
	"github.com/deverity/cowflow/internal/git"
	"github.com/deverity/cowflow/internal/platform"
	"github.com/deverity/cowflow/internal/workspace"
	"github.com/deverity/cowflow/internal/workspace/workspacetest"
	"github.com/deverity/cowflow/test/harness"
)

// snapshotClient adds the snapshot plumbing the rescue needs (AG-GIT has not
// added it to *git.Client yet) to a real client, so the decided recipe
// (SCOPE.md §23 decision 40) runs against real Git in these tests. It runs
// on the hermetic harness runner, which accepts GIT_INDEX_FILE and stdin.
type snapshotClient struct {
	*git.Client
	r git.OptsRunner
}

var _ workspace.SnapshotGit = snapshotClient{}

func (c snapshotClient) AddAll(ctx context.Context, wt, idx string, exclude []string) error {
	args := []string{"add", "-A", "--"}
	for _, p := range exclude {
		args = append(args, ":(exclude,top,literal)"+p)
	}
	_, err := c.r.RunWith(ctx, wt, git.RunOpts{Env: []string{"GIT_INDEX_FILE=" + idx}}, args...)
	return err
}

func (c snapshotClient) WriteTree(ctx context.Context, wt, idx string) (string, error) {
	out, err := c.r.RunWith(ctx, wt, git.RunOpts{Env: []string{"GIT_INDEX_FILE=" + idx}}, "write-tree")
	return strings.TrimSpace(string(out)), err
}

func (c snapshotClient) CommitTree(ctx context.Context, wt, tree, parent, name, email, msg string) (string, error) {
	out, err := c.r.RunWith(ctx, wt, git.RunOpts{Stdin: strings.NewReader(msg)},
		"-c", "user.name="+name, "-c", "user.email="+email, "commit-tree", tree, "-p", parent, "-F", "-")
	return strings.TrimSpace(string(out)), err
}

func (c snapshotClient) UpdateRef(ctx context.Context, wt, ref, sha string) error {
	_, err := c.r.Run(ctx, wt, "update-ref", ref, sha)
	return err
}

// instantClock fires every After at once and records the waits.
type instantClock struct {
	domain.SystemClock
	mu    sync.Mutex
	waits []time.Duration
}

func (c *instantClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	c.waits = append(c.waits, d)
	c.mu.Unlock()
	ch := make(chan time.Time, 1)
	ch <- time.Now()
	return ch
}

func (c *instantClock) Waits() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.waits...)
}

// orderLog records the order of the destroy's side effects.
type orderLog struct {
	mu  sync.Mutex
	evs []string
}

func (l *orderLog) add(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.evs = append(l.evs, s)
}

func (l *orderLog) events() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.evs...)
}

// orderStore logs every state change before delegating.
type orderStore struct {
	workspace.Store
	log *orderLog
}

func (s orderStore) SetWorkspaceState(ctx context.Context, id string, to domain.WorkspaceState, c domain.StateChange) error {
	ev := "state:" + string(to)
	if c.Audit != nil {
		ev += "+audit"
	}
	s.log.add(ev)
	return s.Store.SetWorkspaceState(ctx, id, to, c)
}

// rescuerFunc adapts a function to workspace.Rescuer.
type rescuerFunc func(ctx context.Context, ws workspace.Workspace, r workspace.DirtyReport) (string, error)

func (f rescuerFunc) Rescue(ctx context.Context, ws workspace.Workspace, r workspace.DirtyReport) (string, error) {
	return f(ctx, ws, r)
}

// destroyFixture is a ready git_worktree workspace over a real repository
// with the shared Destroyer wired into the provider.
type destroyFixture struct {
	*dirtyFixture
	cfg   *config.Config
	clock *instantClock
	snap  snapshotClient
}

func newDestroyFixture(t *testing.T) *destroyFixture {
	t.Helper()
	f := &destroyFixture{dirtyFixture: newDirtyFixture(t, "ntfs"), clock: &instantClock{}}
	f.cfg = config.Default()
	f.cfg.Workspace.Root = f.root
	f.cfg.Daemon.StateDir = f.state
	opts, ok := f.repo.Runner.(git.OptsRunner)
	require.True(t, ok)
	f.snap = snapshotClient{Client: f.client, r: opts}
	f.wire(nil)
	return f
}

// wire builds the Destroyer with mod applied to its deps and installs it.
func (f *destroyFixture) wire(mod func(d *workspace.DestroyerDeps)) {
	d := workspace.DestroyerDeps{
		Config: f.cfg, Store: f.store, Git: f.snap, Volumes: f.vols, Clock: f.clock,
		Logger: slog.New(slog.DiscardHandler),
	}
	if mod != nil {
		mod(&d)
	}
	f.prov.WithDestroyer(workspace.NewDestroyer(d))
}

func (f *destroyFixture) destroy(force bool) error {
	_, err := f.destroyWith(workspace.DestroyOptions{Force: force})
	return err
}

func (f *destroyFixture) destroyWith(opts workspace.DestroyOptions) (workspace.DestroyResult, error) {
	return f.prov.Destroy(context.Background(), f.ws.ID, opts)
}

func (f *destroyFixture) row(t *testing.T) domain.WorkspaceRecord {
	t.Helper()
	r, err := f.store.GetWorkspace(context.Background(), f.ws.ID)
	require.NoError(t, err)
	return r
}

func (f *destroyFixture) branchExists(t *testing.T) bool {
	t.Helper()
	ok, err := f.client.BranchExists(context.Background(), f.repo.Dir, f.ws.Branch)
	require.NoError(t, err)
	return ok
}

func TestDestroyRefusesWithoutForce(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(t *testing.T, f *destroyFixture) string // returns a path that must survive
	}{
		{"modified", func(t *testing.T, f *destroyFixture) string {
			f.write(t, "a.txt", "changed\n")
			return "a.txt"
		}},
		{"staged", func(t *testing.T, f *destroyFixture) string {
			f.write(t, "staged.txt", "s\n")
			f.git(t, "add", "staged.txt")
			return "staged.txt"
		}},
		{"untracked", func(t *testing.T, f *destroyFixture) string {
			f.write(t, "new/untracked.txt", "u\n")
			return "new/untracked.txt"
		}},
		{"unpublished commit", func(t *testing.T, f *destroyFixture) string {
			f.write(t, "c.txt", "c\n")
			f.git(t, "add", "c.txt")
			f.git(t, "commit", "-q", "-m", "feat: local")
			return "c.txt"
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newDestroyFixture(t)
			keep := tc.setup(t, f)
			err := f.destroy(false)
			var ue *workspace.UnpreservedError
			require.ErrorAs(t, err, &ue)
			assert.False(t, ue.Report.Clean())
			assert.FileExists(t, filepath.Join(f.ws.Path, filepath.FromSlash(keep)))
			assert.Equal(t, domain.WorkspaceReady, f.row(t).State)
			assert.Empty(t, f.store.Audits(), "a refusal is not a destroy")
		})
	}
}

func TestDestroyCleanSucceeds(t *testing.T) {
	t.Parallel()
	f := newDestroyFixture(t)
	f.write(t, workspace.EnvFileName, "COWFLOW_TASK_ID=x\n") // COWFlow-owned: not unpreserved
	require.NoError(t, f.destroy(false))
	assert.NoDirExists(t, f.ws.Path)
	assertNoAdminEntry(t, f.wtFixture, "auth")
	assert.Equal(t, domain.WorkspaceDestroyed, f.row(t).State)
	assert.Empty(t, f.store.Events(), "no stuck handle, no event")
}

func TestDestroyIdempotent(t *testing.T) {
	t.Parallel()
	f := newDestroyFixture(t)
	require.NoError(t, f.destroy(false))
	before := len(f.runner.Recorded())
	require.NoError(t, f.destroy(false))
	require.NoError(t, f.destroy(true))
	assert.Len(t, f.runner.Recorded(), before, "a destroyed workspace runs no git")
	assert.Len(t, f.store.Audits(), 1)
	assert.Equal(t, domain.WorkspaceDestroyed, f.row(t).State)
}

func TestDestroyWritesAudit(t *testing.T) {
	t.Parallel()
	t.Run("clean", func(t *testing.T) {
		t.Parallel()
		f := newDestroyFixture(t)
		require.NoError(t, f.destroy(false))
		audits := f.store.Audits()
		require.Len(t, audits, 1)
		a := audits[0]
		assert.Equal(t, domain.WorkspaceReady, a.From)
		assert.Equal(t, domain.WorkspacePendingCleanup, a.To, "audited before anything is deleted")
		assert.Equal(t, "destroy", a.Audit.Action)
		assert.False(t, a.Audit.Force)
		assert.Empty(t, a.Audit.RescueRef)
		assert.Empty(t, a.Audit.Dirty)
	})
	t.Run("force", func(t *testing.T) {
		t.Parallel()
		f := newDestroyFixture(t)
		f.write(t, "wip.txt", "w\n")
		res, err := f.destroyWith(workspace.DestroyOptions{Force: true, Actor: workspace.Actor{Kind: "user", ID: "ana"}})
		require.NoError(t, err)
		audits := f.store.Audits()
		require.Len(t, audits, 1)
		a := audits[0].Audit
		assert.True(t, a.Force)
		assert.Equal(t, "refs/cowflow/rescue/"+f.ws.ID, a.RescueRef)
		assert.Equal(t, a.RescueRef, res.RescueRef, "the caller learns where the work went")
		assert.Contains(t, a.Dirty, "wip.txt")
		assert.Equal(t, "user", a.ActorKind)
		assert.Equal(t, "ana", a.ActorID)
	})
}

func TestDestroyForceRescuesBeforeRemoval(t *testing.T) {
	t.Parallel()
	f := newDestroyFixture(t)
	f.write(t, "wip.txt", "w\n")
	failWorktreeRemove(f)
	log := &orderLog{}
	f.wire(func(d *workspace.DestroyerDeps) {
		d.Store = orderStore{Store: f.store, log: log}
		d.Rescuer = rescuerFunc(func(_ context.Context, ws workspace.Workspace, r workspace.DirtyReport) (string, error) {
			log.add("rescue")
			assert.FileExists(t, filepath.Join(ws.Path, "wip.txt"), "rescue sees the files")
			assert.Equal(t, []string{"wip.txt"}, r.Uncommitted)
			return workspace.RescueRef(ws.ID), nil
		})
		d.RemoveAll = func(p string) error {
			log.add("remove")
			return os.RemoveAll(p)
		}
	})
	require.NoError(t, f.destroy(true))
	assert.Equal(t, []string{"rescue", "state:pending_cleanup+audit", "remove", "state:destroyed"}, log.events())
}

func TestDestroyForceRescueSnapshot(t *testing.T) {
	t.Parallel()
	f := newDestroyFixture(t)
	f.repo.Commit(t, "chore: ignore logs", map[string]string{".gitignore": "*.log\n"})
	// The workspace is at the base: give it the ignore rule as a local
	// change so the snapshot carries a modified tracked file too.
	f.write(t, ".gitignore", "*.log\n")
	f.write(t, "a.txt", "changed\n")
	f.write(t, "staged.txt", "s\n")
	f.git(t, "add", "staged.txt")
	f.write(t, "dir/untracked.txt", "u\n")
	f.write(t, "debug.log", "ignored\n")
	f.write(t, workspace.EnvFileName, "COWFLOW_TASK_ID=x\n")
	head := trimNL(f.git(t, "rev-parse", "HEAD"))

	require.NoError(t, f.destroy(true))
	assert.NoDirExists(t, f.ws.Path)
	ref := workspace.RescueRef(f.ws.ID)
	run := func(args ...string) string {
		out, err := f.repo.Runner.Run(context.Background(), f.repo.Dir, args...)
		require.NoError(t, err, "git %v", args)
		return string(out)
	}
	files := strings.Fields(run("ls-tree", "-r", "--name-only", ref))
	assert.ElementsMatch(t, []string{".gitattributes", ".gitignore", "a.txt", "staged.txt", "dir/untracked.txt"}, files,
		"tracked, staged and untracked; ignored files and .env.cowflow are not rescued")
	assert.Equal(t, "changed\n", run("show", ref+":a.txt"))
	assert.Equal(t, head, trimNL(run("rev-parse", ref+"^")), "the snapshot's parent is HEAD")
	// The identity (COWFlow <cowflow@localhost>) is not asserted: the
	// hermetic harness pins GIT_AUTHOR_* and GIT_COMMITTER_*, which win over
	// -c user.name/user.email.
	msg := run("log", "-1", "--format=%B", ref)
	assert.Contains(t, msg, "cowflow rescue: workspace "+f.ws.ID+" (task auth) before destroy --force")
	assert.Contains(t, msg, "Cowflow-Workspace: "+f.ws.ID)
	assert.Contains(t, msg, "Cowflow-Task: "+f.ws.TaskID)
	assert.True(t, f.branchExists(t), "the task branch is kept")
	assert.Equal(t, head, trimNL(run("rev-parse", f.ws.Branch)), "the task branch does not move")
	leftovers, err := filepath.Glob(filepath.Join(f.repo.Dir, ".git", "worktrees", "*", "cowflow-rescue-index-*"))
	require.NoError(t, err)
	assert.Empty(t, leftovers)
}

func TestDestroyForceRescueSnapshotOfUnpublishedCommit(t *testing.T) {
	t.Parallel()
	f := newDestroyFixture(t)
	f.write(t, "c.txt", "c\n")
	f.git(t, "add", "c.txt")
	f.git(t, "commit", "-q", "-m", "feat: local")
	head := trimNL(f.git(t, "rev-parse", "HEAD"))
	require.NoError(t, f.destroy(true))
	out, err := f.repo.Runner.Run(context.Background(), f.repo.Dir, "rev-parse", workspace.RescueRef(f.ws.ID)+"^")
	require.NoError(t, err)
	assert.Equal(t, head, trimNL(string(out)), "the rescue ref keeps the unpublished commit reachable")
}

func TestDestroyForceRescueFailureRefuses(t *testing.T) {
	t.Parallel()
	t.Run("git client without snapshot operations", func(t *testing.T) {
		t.Parallel()
		f := newDestroyFixture(t)
		f.wire(func(d *workspace.DestroyerDeps) { d.Git = f.client }) // plain *git.Client
		f.write(t, "wip.txt", "w\n")
		err := f.destroy(true)
		require.ErrorIs(t, err, workspace.ErrRescueFailed)
		assert.FileExists(t, filepath.Join(f.ws.Path, "wip.txt"))
		assert.Equal(t, domain.WorkspaceReady, f.row(t).State)
		assert.Empty(t, f.store.Audits())
	})
	t.Run("snapshot step fails", func(t *testing.T) {
		t.Parallel()
		f := newDestroyFixture(t)
		f.runner.Inject(harness.GitFault{Match: []string{"rev-parse", "--git-path", "index"}, ExitCode: 128})
		f.write(t, "wip.txt", "w\n")
		err := f.destroy(true)
		require.ErrorIs(t, err, workspace.ErrRescueFailed)
		assert.FileExists(t, filepath.Join(f.ws.Path, "wip.txt"))
		assert.Equal(t, domain.WorkspaceReady, f.row(t).State)
	})
	t.Run("rescuer error is wrapped", func(t *testing.T) {
		t.Parallel()
		f := newDestroyFixture(t)
		f.wire(func(d *workspace.DestroyerDeps) {
			d.Rescuer = rescuerFunc(func(context.Context, workspace.Workspace, workspace.DirtyReport) (string, error) {
				return "", errors.New("disk full")
			})
		})
		f.write(t, "wip.txt", "w\n")
		require.ErrorIs(t, f.destroy(true), workspace.ErrRescueFailed)
		assert.FileExists(t, filepath.Join(f.ws.Path, "wip.txt"))
	})
}

func TestDestroyGitFailureRefusesEvenWithForce(t *testing.T) {
	t.Parallel()
	for _, fault := range []harness.GitFault{
		{Match: []string{"status"}, ExitCode: 128},
		{Match: []string{"rev-list"}, ExitCode: 128},
		{Match: []string{"status"}, TruncateAt: 3},
	} {
		f := newDestroyFixture(t)
		f.write(t, "wip.txt", "w\n")
		f.runner.Inject(fault)
		rescued := false
		f.wire(func(d *workspace.DestroyerDeps) {
			d.Rescuer = rescuerFunc(func(context.Context, workspace.Workspace, workspace.DirtyReport) (string, error) {
				rescued = true
				return "", nil
			})
		})
		for _, force := range []bool{false, true} {
			err := f.destroy(force)
			require.Error(t, err, "%v force=%v", fault.Match, force)
			var ue *workspace.UnpreservedError
			assert.False(t, errors.As(err, &ue))
		}
		assert.False(t, rescued)
		assert.FileExists(t, filepath.Join(f.ws.Path, "wip.txt"))
		assert.Equal(t, domain.WorkspaceReady, f.row(t).State)
		assert.Empty(t, f.store.Audits())
	}
}

// busy is the error os.RemoveAll returns for a file held open on Unix.
func busy(p string) error { return &fs.PathError{Op: "unlinkat", Path: p, Err: syscall.EBUSY} }

// failWorktreeRemove makes `git worktree remove` fail, as it does when a
// file in the worktree is held open, so the directory removal runs.
func failWorktreeRemove(f *destroyFixture) {
	f.runner.Inject(harness.GitFault{Match: []string{"worktree", "remove"}, ExitCode: 255})
}

func TestDestroyStuckHandlePendingCleanup(t *testing.T) {
	t.Parallel()
	f := newDestroyFixture(t)
	failWorktreeRemove(f)
	held := filepath.Join(f.ws.Path, "a.txt")
	var mu sync.Mutex
	stuck := true
	f.wire(func(d *workspace.DestroyerDeps) {
		d.RemoveAll = func(p string) error {
			mu.Lock()
			defer mu.Unlock()
			if stuck {
				return busy(held)
			}
			return os.RemoveAll(p)
		}
	})
	f.write(t, "wip.txt", "w\n")
	res, err := f.destroyWith(workspace.DestroyOptions{Force: true})
	require.ErrorIs(t, err, workspace.ErrPendingCleanup)
	assert.Equal(t, workspace.RescueRef(f.ws.ID), res.RescueRef, "the rescue ref survives a pending_cleanup")
	assert.Contains(t, err.Error(), held)
	assert.Equal(t, []time.Duration{250 * time.Millisecond, time.Second, 4 * time.Second}, f.clock.Waits())

	r := f.row(t)
	assert.Equal(t, domain.WorkspacePendingCleanup, r.State, "never a half-deleted ready")
	assert.Contains(t, r.LastError, "EBUSY")
	evs := f.store.Events()
	require.Len(t, evs, 1)
	assert.Equal(t, domain.EventWorkspacePendingCleanup, evs[0].Type)
	assert.Equal(t, f.ws.ID, evs[0].WorkspaceID)
	assert.Equal(t, held, evs[0].Payload["path"])
	assert.Equal(t, "EBUSY", evs[0].Payload["code"])
	assert.Equal(t, "close editors/terminals using this folder", evs[0].Payload["hint"])
	assert.True(t, f.branchExists(t))

	// The handle is closed; destroying again resumes without a new dirty
	// check (the workspace was checked and audited before).
	mu.Lock()
	stuck = false
	mu.Unlock()
	before := countArgv(f.runner.Recorded(), "status")
	require.NoError(t, f.destroy(false))
	assert.Equal(t, before, countArgv(f.runner.Recorded(), "status"))
	assert.Equal(t, domain.WorkspaceDestroyed, f.row(t).State)
	assert.NoDirExists(t, f.ws.Path)
	assertNoAdminEntry(t, f.wtFixture, "auth")
	audits := f.store.Audits()
	require.Len(t, audits, 2, "the resume is audited too")
	assert.Equal(t, domain.WorkspacePendingCleanup, audits[1].From)
}

func TestDestroyStuckHandleClearsOnRetry(t *testing.T) {
	t.Parallel()
	f := newDestroyFixture(t)
	failWorktreeRemove(f)
	calls := 0
	f.wire(func(d *workspace.DestroyerDeps) {
		d.RemoveAll = func(p string) error {
			calls++
			if calls <= 2 {
				return busy(p)
			}
			return os.RemoveAll(p)
		}
	})
	require.NoError(t, f.destroy(false))
	assert.Equal(t, []time.Duration{250 * time.Millisecond, time.Second}, f.clock.Waits())
	assert.Equal(t, domain.WorkspaceDestroyed, f.row(t).State)
	assert.Len(t, f.store.Events(), 1)
}

func TestDestroyOtherRemovalErrorIsNotStuck(t *testing.T) {
	t.Parallel()
	f := newDestroyFixture(t)
	failWorktreeRemove(f)
	f.wire(func(d *workspace.DestroyerDeps) {
		d.RemoveAll = func(p string) error { return &fs.PathError{Op: "unlinkat", Path: p, Err: syscall.EIO} }
	})
	err := f.destroy(false)
	require.Error(t, err)
	assert.NotErrorIs(t, err, workspace.ErrPendingCleanup)
	assert.Empty(t, f.clock.Waits())
	assert.Equal(t, domain.WorkspacePendingCleanup, f.row(t).State, "audited: a later destroy resumes")
	assert.Empty(t, f.store.Events())
}

func TestDestroyNeverEscapesRoot(t *testing.T) {
	t.Parallel()
	t.Run("link inside the workspace", func(t *testing.T) {
		t.Parallel()
		f := newDestroyFixture(t)
		outside := t.TempDir()
		victim := filepath.Join(outside, "victim.txt")
		require.NoError(t, os.WriteFile(victim, []byte("keep"), 0o600))
		if err := os.Symlink(outside, filepath.Join(f.ws.Path, "link")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		require.NoError(t, os.Symlink(victim, filepath.Join(f.ws.Path, "file-link")))
		require.NoError(t, f.destroy(true))
		assert.NoDirExists(t, f.ws.Path)
		assert.FileExists(t, victim, "the link target survives")
	})
	t.Run("workspace path is a link", func(t *testing.T) {
		t.Parallel()
		f := newDestroyFixture(t)
		other := filepath.Join(f.root, "other")
		require.NoError(t, os.MkdirAll(other, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(other, "keep.txt"), []byte("k"), 0o600))
		swapped := filepath.Join(f.root, "swapped")
		if err := os.Symlink(other, swapped); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		_, err := f.store.InsertWorkspace(context.Background(), domain.WorkspaceRecord{
			ID: "wks_swapped", ProjectID: "prj_1", TaskID: "tsk_s", Provider: workspace.WorktreeName,
			CowKind: "none", Filesystem: "ntfs", Path: swapped, Branch: "feature/s", BaseSHA: f.base, OpID: "op-s",
		}, 10, 40000)
		require.NoError(t, err)
		require.NoError(t, f.store.SetWorkspaceState(context.Background(), "wks_swapped", domain.WorkspaceReady, domain.StateChange{}))
		_, err = f.prov.Destroy(context.Background(), "wks_swapped", workspace.DestroyOptions{Force: true})
		require.ErrorIs(t, err, platform.ErrOutsideRoot)
		assert.FileExists(t, filepath.Join(other, "keep.txt"))
	})
	t.Run("path outside root", func(t *testing.T) {
		t.Parallel()
		f := newDestroyFixture(t)
		f.cfg.Workspace.Root = t.TempDir() // the workspace is no longer under the root
		f.wire(nil)
		err := f.destroy(true)
		require.ErrorIs(t, err, platform.ErrOutsideRoot)
		assert.DirExists(t, f.ws.Path)
		assert.Equal(t, domain.WorkspaceReady, f.row(t).State)
	})
	t.Run("root not set", func(t *testing.T) {
		t.Parallel()
		f := newDestroyFixture(t)
		f.cfg.Workspace.Root = ""
		f.wire(nil)
		require.ErrorIs(t, f.destroy(false), workspace.ErrRootNotSet)
		assert.DirExists(t, f.ws.Path)
	})
}

func TestDestroyKeepsBranch(t *testing.T) {
	t.Parallel()
	f := newDestroyFixture(t)
	require.NoError(t, f.destroy(false))
	assert.True(t, f.branchExists(t))
	for _, c := range f.runner.Recorded() {
		assert.NotEqual(t, "branch", c[0], "no branch command: %v", c)
	}
}

func TestDestroyKeepsLogs(t *testing.T) {
	t.Parallel()
	f := newDestroyFixture(t)
	logs := workspace.LogsDir(f.state, f.ws.ID)
	require.NoError(t, os.MkdirAll(logs, 0o750))
	require.NoError(t, f.destroy(false))
	assert.DirExists(t, logs)
}

func TestDestroyRemovesCargoTarget(t *testing.T) {
	t.Parallel()
	f := newDestroyFixture(t)
	ct := t.TempDir()
	f.cfg.Cache.CargoTarget = ct
	f.wire(nil)
	mine := filepath.Join(ct, f.ws.ID)
	require.NoError(t, os.MkdirAll(filepath.Join(mine, "debug"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(mine, "debug", "x"), []byte("x"), 0o600))
	other := filepath.Join(ct, "wks_other")
	require.NoError(t, os.MkdirAll(other, 0o750))
	require.NoError(t, f.destroy(false))
	assert.NoDirExists(t, mine)
	assert.DirExists(t, other, "another workspace's target stays")
}

func TestDestroyMissingPath(t *testing.T) {
	t.Parallel()
	t.Run("same volume", func(t *testing.T) {
		t.Parallel()
		f := newDestroyFixture(t)
		require.NoError(t, os.RemoveAll(f.ws.Path))
		require.NoError(t, f.destroy(false))
		assert.Equal(t, domain.WorkspaceDestroyed, f.row(t).State)
		assert.Equal(t, "workspace path missing", f.store.Audits()[0].Audit.Dirty)
	})
	t.Run("volume changed", func(t *testing.T) {
		t.Parallel()
		f := newDestroyFixture(t)
		require.NoError(t, os.RemoveAll(f.ws.Path))
		f.vols.Set(platform.Volume{Filesystem: "ext4", ID: "v2", MountPath: f.root})
		require.ErrorIs(t, f.destroy(true), workspace.ErrPendingMount)
		assert.Equal(t, domain.WorkspaceReady, f.row(t).State)
	})
}

func TestDestroyPendingMountVolume(t *testing.T) {
	t.Parallel()
	f := newDestroyFixture(t)
	f.vols.Unmount(f.root)
	require.ErrorIs(t, f.destroy(true), workspace.ErrPendingMount)
	assert.DirExists(t, f.ws.Path)
	assert.Equal(t, domain.WorkspaceReady, f.row(t).State)
}

func TestDestroyUnknownAndWrongProvider(t *testing.T) {
	t.Parallel()
	f := newDestroyFixture(t)
	_, err := f.prov.Destroy(context.Background(), "wks_nope", workspace.DestroyOptions{})
	require.ErrorIs(t, err, workspace.ErrNotFound)

	d := workspace.NewDestroyer(workspace.DestroyerDeps{Config: f.cfg, Store: f.store, Git: f.snap, Volumes: f.vols})
	_, err = d.Destroy(context.Background(), workspace.ProviderCowClone, f.ws.ID, workspace.DestroyOptions{})
	require.ErrorIs(t, err, workspace.ErrUnsupportedProvider)
	assert.DirExists(t, f.ws.Path)
}

func TestDestroyCowCloneDelegates(t *testing.T) {
	t.Parallel()
	store := workspacetest.NewMemStore(domain.SystemClock{})
	g := &workspacetest.FakeGit{}
	p := workspace.NewCowCloneProvider(workspace.CowCloneDeps{Git: g}).
		WithDestroyer(workspace.NewDestroyer(workspace.DestroyerDeps{Store: store, Git: g}))
	_, err := p.Destroy(context.Background(), "wks_nope", workspace.DestroyOptions{})
	require.ErrorIs(t, err, workspace.ErrNotFound)
}

func TestRescueRef(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "refs/cowflow/rescue/wks_01H", workspace.RescueRef("wks_01H"))
}

func countArgv(calls [][]string, first string) int {
	n := 0
	for _, c := range calls {
		if len(c) > 0 && c[0] == first {
			n++
		}
	}
	return n
}
