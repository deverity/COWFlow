package integration_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/cli"
	"github.com/deverity/cowflow/internal/domain"
	"github.com/deverity/cowflow/internal/platform"
	"github.com/deverity/cowflow/internal/workspace"
	"github.com/deverity/cowflow/internal/workspace/workspacetest"
	"github.com/deverity/cowflow/test/harness"
)

// requireNoPartialWorkspace asserts that a failed create left nothing
// behind but a failed row: no ready or creating row, no workspace
// directory, no worktree entry and no task branch (SCOPE.md §18 "never
// partial ready; compensation").
func requireNoPartialWorkspace(t *testing.T, e *wsEnv, slug, branch string) {
	t.Helper()
	rows := e.list()
	require.Len(t, rows, 1, "one row records the failed create: %+v", rows)
	assert.Equal(t, string(domain.WorkspaceFailed), rows[0].State)
	assert.NoDirExists(t, filepath.Join(e.wsRoot, slug), "no workspace directory")
	assert.Equal(t, []string{filepath.Clean(e.repoDir)}, e.worktreePaths(), "no worktree entry")
	assert.False(t, e.refExists("refs/heads/"+branch), "the branch this create made is deleted")
}

// §18 "Disk full / provider fails": a block clone that runs out of space
// half-way fails the create explicitly and compensates; a retry succeeds.
func TestFault_DiskFullDuringCloneNoPartialReady(t *testing.T) {
	t.Parallel()
	e := newEnv(t, providerCow, withBase(harness.ScratchDir(t)))
	// The volume is scripted to block-clone; the fake cloner byte-copies
	// (test only) and reports ENOSPC after 50 files.
	e.vols = workspacetest.NewFakeVolumes(platform.Volume{Filesystem: "btrfs", ID: "sim", MountPath: e.base})
	cloner := &workspacetest.FakeCloner{FailAfter: 50}
	e.cloner = cloner

	r := e.runCLI("task", "create", "t01", "--json")
	require.Equal(t, cli.ExitState, r.code, "disk full is a state error\n%s", r)
	assert.Contains(t, r.stderr, "fake clone")
	assert.Greater(t, cloner.Calls(), 50, "the failure came from the clone, after some files")
	requireNoPartialWorkspace(t, e, "t01", "feature/t01")
	for _, name := range entriesUnder(t, filepath.Join(e.wsRoot, "golden")) {
		assert.NotContains(t, name, ".tmp-", "no half-built golden tree")
	}
	assert.Equal(t, []string{"golden"}, entriesUnder(t, e.wsRoot), "nothing but the golden cache under the root")

	// Space is back: the retry reuses the task and gets a ready workspace.
	e.cloner = &workspacetest.FakeCloner{}
	w := e.createTask("t01")
	assert.Equal(t, providerCow, w.Workspace.Provider)
	assert.Equal(t, "ready", e.inspect(w.Workspace.ID).State)
	assert.FileExists(t, filepath.Join(w.Workspace.Path, sharedFile))
}

// §18 "Disk full / provider fails" through Git: `git worktree add` hangs
// until the per-call timeout kills it; the create fails and compensates.
func TestFault_GitWorktreeAddTimeout(t *testing.T) {
	t.Parallel()
	e := newEnv(t, providerWorktree)
	e.callTimeout = 2 * time.Second
	e.git.Inject(harness.GitFault{Match: []string{"worktree", "add"}, Timeout: true})

	r := e.runCLI("task", "create", "t01")
	require.Equal(t, cli.ExitState, r.code, "a git timeout is a state error\n%s", r)
	assert.Contains(t, r.stderr, "deadline exceeded")
	adds := 0
	for _, c := range e.git.Recorded() {
		if len(c) >= 2 && c[0] == "worktree" && c[1] == "add" {
			adds++
		}
	}
	assert.Equal(t, 1, adds, "worktree add was attempted once")
	requireNoPartialWorkspace(t, e, "t01", "feature/t01")

	e.git.Clear()
	e.callTimeout = 0
	w := e.createTask("t01")
	assert.Equal(t, "ready", e.inspect(w.Workspace.ID).State)
}

// Destroy fails closed: a truncated `git status` or `git rev-list` is never
// read as clean, even with --force (SCOPE.md §17, §20.12).
func TestFault_TruncatedStatusFailsClosed(t *testing.T) {
	t.Parallel()
	e := newEnv(t, providerWorktree)
	cases := []struct {
		name  string
		match []string
		dirty func(t *testing.T, dir string)
	}{{
		name:  "status",
		match: []string{"status", "--porcelain=v2"},
		dirty: func(t *testing.T, dir string) { appendFile(t, dir, sharedFile, "unsaved\n") },
	}, {
		name:  "rev-list",
		match: []string{"rev-list"},
		dirty: func(t *testing.T, dir string) {
			writeFile(t, dir, "committed.txt", "committed\n")
			e.gitIn(dir, "add", "committed.txt")
			e.gitIn(dir, "commit", "-q", "-m", "feat: not published")
		},
	}}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := e.createTask(fmt.Sprintf("trunc%d", i+1))
			dir, id := w.Workspace.Path, w.Workspace.ID
			tc.dirty(t, dir)
			head := e.gitIn(dir, "rev-parse", "HEAD")
			before := e.statusPaths(dir)

			e.git.Inject(harness.GitFault{Match: tc.match, TruncateAt: 10})
			for _, args := range [][]string{{"workspace", "destroy", id}, {"workspace", "destroy", id, "--force"}} {
				r := e.runCLI(args...)
				assert.Equal(t, cli.ExitState, r.code, "%v: refused because the dirty check failed\n%s", args, r)
				assert.Contains(t, r.stderr, "refusing")
			}
			e.git.Clear()

			assert.DirExists(t, dir)
			assert.Equal(t, before, e.statusPaths(dir), "nothing in the workspace changed")
			assert.Equal(t, head, e.gitIn(dir, "rev-parse", "HEAD"))
			assert.False(t, e.refExists(workspace.RescueRef(id)), "no rescue was attempted")
			got := e.inspect(id)
			assert.Equal(t, "ready", got.State)
			assert.False(t, len(got.Dirty.Uncommitted) == 0 && len(got.Dirty.Unpublished) == 0, "still dirty")
		})
	}
}

// §18 "VHDX not mounted": with the Dev Drive detached the create waits in
// pending_mount, emits the event and never writes under the mount point, so
// there is no silent fallback to the NTFS folder behind it.
func TestFault_VHDXNotMountedPendingMount(t *testing.T) {
	t.Parallel()
	scratch, err := filepath.EvalSymlinks(harness.ScratchDir(t))
	require.NoError(t, err)
	// The simulated drive is X: (v1 mounts on a drive letter only, §23
	// decision 35). The workspace root stands in for a folder on it: the
	// directory "drive" exists and is empty, as the folder a detached drive
	// leaves behind would be.
	const letter = "X:"
	drive := filepath.Join(scratch, "drive")
	require.NoError(t, os.MkdirAll(drive, 0o750))
	root := filepath.Join(drive, "ws")
	// The config value is what the Manager asks the Dev Drive layer about.
	vhdx := filepath.ToSlash(filepath.Join(scratch, "cowflow.vhdx"))

	sim := &harness.SimDevDrive{}
	_, err = sim.Provision(t.Context(), platform.DevDriveSpec{VHDXPath: vhdx, Mount: letter})
	require.NoError(t, err)
	sim.SimulateReboot()

	e := newEnv(t, providerWorktree, withBase(scratch), withRoot(root), withProviderSetting(providerAuto),
		withConfig(fmt.Sprintf("\n[devdrive]\nenabled = true\npath = %q\nmount = %q\n",
			vhdx, letter)))
	e.devDrive = sim

	r := e.runCLI("task", "create", "t01")
	require.Equal(t, cli.ExitState, r.code, "pending_mount is a state error (§14)\n%s", r)
	assert.Contains(t, r.stderr, "not attached")

	rows := e.list()
	require.Len(t, rows, 1)
	assert.Equal(t, string(domain.WorkspacePendingMount), rows[0].State)
	evs := e.events.ofType(domain.EventWorkspacePendingMount)
	require.Len(t, evs, 1, "workspace.pending_mount is emitted")
	assert.Equal(t, rows[0].ID, evs[0].WorkspaceID)
	assert.Empty(t, entriesUnder(t, drive), "nothing is written where the drive should be: no silent fallback")
	assert.Contains(t, sim.Calls, "reboot")
	assert.Equal(t, []string{filepath.Clean(e.repoDir)}, e.worktreePaths(), "no worktree entry")
	assert.False(t, e.refExists("refs/heads/feature/t01"), "no branch")
}

// stuckErr is the error a removal blocked by an open handle returns.
func stuckErr(path string) error {
	errno := syscall.EBUSY
	if runtime.GOOS == "windows" {
		errno = syscall.Errno(32) // ERROR_SHARING_VIOLATION
	}
	return &fs.PathError{Op: "unlinkat", Path: path, Err: errno}
}

// §18 "Stuck handle": a directory held open moves the workspace to
// pending_cleanup with the event recorded; once released, destroying again
// finishes the removal.
func TestFault_StuckHandlePendingCleanup(t *testing.T) {
	t.Parallel()
	e := newEnv(t, providerWorktree)
	w := e.createTask("t01")
	dir, id := w.Workspace.Path, w.Workspace.ID

	// git cannot remove the worktree while the handle is open, and neither
	// can the directory removal that follows.
	e.git.Inject(harness.GitFault{Match: []string{"worktree", "remove"}, ExitCode: 128})
	e.removeAll = func(p string) error {
		if p == dir {
			return stuckErr(p)
		}
		return os.RemoveAll(p)
	}
	clk := harness.NewClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	e.clock = clk

	done := make(chan cliResult, 1)
	go func() { done <- e.runCLI("workspace", "destroy", id) }()
	var r cliResult
	for waiting := true; waiting; {
		select {
		case r = <-done:
			waiting = false
		case <-time.After(5 * time.Millisecond):
			clk.Advance(5 * time.Second) // drive the retry back-off on fake time
		}
	}
	require.Equal(t, cli.ExitState, r.code, "pending_cleanup is a state error (§14)\n%s", r)
	assert.Contains(t, r.stderr, "close editors/terminals using this folder")

	rows := e.list()
	require.Len(t, rows, 1)
	assert.Equal(t, string(domain.WorkspacePendingCleanup), rows[0].State)
	evs := e.events.ofType(domain.EventWorkspacePendingCleanup)
	require.Len(t, evs, 1, "workspace.pending_cleanup is recorded once")
	assert.Equal(t, id, evs[0].WorkspaceID)
	assert.Equal(t, dir, evs[0].Payload["path"])
	assert.DirExists(t, dir, "nothing was force-removed")

	// The handle is released: destroy again resumes and finishes.
	e.git.Clear()
	e.removeAll, e.clock = nil, nil
	e.mustCLI("workspace", "destroy", id)
	assert.NoDirExists(t, dir)
	assert.Equal(t, "destroyed", e.inspect(id).State)
	assert.Empty(t, e.list())
	assert.False(t, slices.Contains(e.worktreePaths(), filepath.Clean(dir)), "the worktree entry is pruned")
}
