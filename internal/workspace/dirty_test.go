package workspace_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/config"
	"github.com/deverity/cowflow/internal/domain"
	"github.com/deverity/cowflow/internal/workspace"
	"github.com/deverity/cowflow/internal/workspace/workspacetest"
	"github.com/deverity/cowflow/test/harness"
)

// dirtyFixture is a git_worktree workspace created over a real repository,
// with a row in a MemStore and an Inspector wired into the provider.
type dirtyFixture struct {
	*wtFixture
	store *workspacetest.MemStore
	insp  *workspace.Inspector
	ws    *workspace.Workspace
	state string
}

func newDirtyFixture(t *testing.T, fsName string) *dirtyFixture {
	t.Helper()
	f := &dirtyFixture{wtFixture: newWtFixture(t, fsName)}
	f.store = workspacetest.NewMemStore(domain.SystemClock{})
	cfg := config.Default()
	f.state = filepath.Join(t.TempDir(), "state")
	cfg.Daemon.StateDir = f.state
	f.insp = workspace.NewInspector(f.store, f.client, f.vols, cfg)
	f.prov.WithInspector(f.insp)

	ctx := context.Background()
	spec := f.spec("auth", "feature/auth")
	ws, err := f.prov.Create(ctx, spec)
	require.NoError(t, err)
	f.ws = ws
	_, err = f.store.InsertWorkspace(ctx, domain.WorkspaceRecord{
		ID: ws.ID, ProjectID: ws.ProjectID, TaskID: ws.TaskID, Provider: workspace.WorktreeName,
		CowKind: "none", Filesystem: fsName, Path: ws.Path, Branch: ws.Branch, BaseSHA: ws.BaseSHA,
		OpID: spec.OpID,
	}, 10, 40000)
	require.NoError(t, err)
	require.NoError(t, f.store.SetWorkspaceState(ctx, ws.ID, domain.WorkspaceReady, domain.StateChange{}))
	return f
}

func (f *dirtyFixture) write(t *testing.T, rel, content string) {
	t.Helper()
	p := filepath.Join(f.ws.Path, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
}

func (f *dirtyFixture) git(t *testing.T, args ...string) string {
	t.Helper()
	out, err := f.repo.Runner.Run(context.Background(), f.ws.Path, args...)
	require.NoError(t, err, "git %v", args)
	return string(out)
}

func TestDirtyCleanAfterCreate(t *testing.T) {
	t.Parallel()
	f := newDirtyFixture(t, "ntfs")
	r, err := f.insp.Dirty(context.Background(), *f.ws)
	require.NoError(t, err)
	assert.True(t, r.Clean(), "%+v", r)
}

func TestDirtyUncommittedKinds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		do   func(t *testing.T, f *dirtyFixture)
		want []string
	}{
		{"modified", func(t *testing.T, f *dirtyFixture) { f.write(t, "a.txt", "changed\n") }, []string{"a.txt"}},
		{"staged", func(t *testing.T, f *dirtyFixture) {
			f.write(t, "new.txt", "x")
			f.git(t, "add", "new.txt")
		}, []string{"new.txt"}},
		{"deleted", func(t *testing.T, f *dirtyFixture) { require.NoError(t, os.Remove(filepath.Join(f.ws.Path, "a.txt"))) }, []string{"a.txt"}},
		{"renamed", func(t *testing.T, f *dirtyFixture) { f.git(t, "mv", "a.txt", "b.txt") }, []string{"b.txt"}},
		{"untracked in new directory", func(t *testing.T, f *dirtyFixture) { f.write(t, "d/e/f.txt", "x") }, []string{"d/e/f.txt"}},
		{"space", func(t *testing.T, f *dirtyFixture) { f.write(t, "my file.txt", "x") }, []string{"my file.txt"}},
		{"newline", func(t *testing.T, f *dirtyFixture) { f.write(t, "line\nbreak.txt", "x") }, []string{"line\nbreak.txt"}},
		{"unicode", func(t *testing.T, f *dirtyFixture) { f.write(t, "çãø/日本.txt", "x") }, []string{"çãø/日本.txt"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newDirtyFixture(t, "ntfs")
			tc.do(t, f)
			r, err := f.insp.Dirty(context.Background(), *f.ws)
			require.NoError(t, err)
			assert.Equal(t, tc.want, r.Uncommitted)
			assert.Empty(t, r.Unpublished)
			assert.False(t, r.Clean())
		})
	}
}

func TestDirtyUnpublishedCommit(t *testing.T) {
	t.Parallel()
	f := newDirtyFixture(t, "ntfs")
	f.write(t, "c.txt", "c")
	f.git(t, "add", "-A")
	f.git(t, "commit", "-q", "-m", "feat: c")
	head := trimNL(f.git(t, "rev-parse", "HEAD"))
	r, err := f.insp.Dirty(context.Background(), *f.ws)
	require.NoError(t, err)
	assert.Empty(t, r.Uncommitted)
	assert.Equal(t, []string{head}, r.Unpublished)
}

func TestDirtyPublishedCommitIsClean(t *testing.T) {
	t.Parallel()
	f := newDirtyFixture(t, "ntfs")
	f.write(t, "c.txt", "c")
	f.git(t, "add", "-A")
	f.git(t, "commit", "-q", "-m", "feat: c")
	f.git(t, "update-ref", "refs/cowflow/changes/x", "HEAD")
	r, err := f.insp.Dirty(context.Background(), *f.ws)
	require.NoError(t, err)
	assert.True(t, r.Clean(), "%+v", r)
}

func TestDirtyIgnoresEnvCowflow(t *testing.T) {
	t.Parallel()
	f := newDirtyFixture(t, "ntfs")
	f.write(t, workspace.EnvFileName, "COWFLOW_X=1\n")
	r, err := f.insp.Dirty(context.Background(), *f.ws)
	require.NoError(t, err)
	assert.True(t, r.Clean(), "%+v", r)

	// Only the root-level file is COWFlow's; the same name deeper is work.
	f.write(t, "sub/"+workspace.EnvFileName, "x")
	r, err = f.insp.Dirty(context.Background(), *f.ws)
	require.NoError(t, err)
	assert.Equal(t, []string{"sub/" + workspace.EnvFileName}, r.Uncommitted)
}

func TestDirtyIgnoredFilesAreNotCounted(t *testing.T) {
	t.Parallel()
	f := newDirtyFixture(t, "ntfs")
	excl := trimNL(f.git(t, "rev-parse", "--git-path", "info/exclude"))
	if !filepath.IsAbs(excl) {
		excl = filepath.Join(f.ws.Path, excl)
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(excl), 0o750))
	require.NoError(t, os.WriteFile(excl, []byte("*.log\n"), 0o600))
	f.write(t, "build.log", "noise")
	r, err := f.insp.Dirty(context.Background(), *f.ws)
	require.NoError(t, err)
	assert.True(t, r.Clean(), "%+v", r)
}

func TestDirtyGitFailureFailsClosed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		fault  harness.GitFault
		budget time.Duration
	}{
		{"status exit 128", harness.GitFault{Match: []string{"status"}, ExitCode: 128}, 0},
		{"rev-list exit 128", harness.GitFault{Match: []string{"rev-list"}, ExitCode: 128}, 0},
		{"status timeout", harness.GitFault{Match: []string{"status"}, Timeout: true}, 100 * time.Millisecond},
		{"status truncated", harness.GitFault{Match: []string{"status"}, TruncateAt: 5}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newDirtyFixture(t, "ntfs")
			f.write(t, "dirty.txt", "x") // a truncated status must not read as clean
			f.runner.Inject(tc.fault)
			ctx := context.Background()
			if tc.budget > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tc.budget)
				defer cancel()
			}
			r, err := f.insp.Dirty(ctx, *f.ws)
			require.Error(t, err)
			assert.Contains(t, err.Error(), f.ws.ID)
			assert.Equal(t, workspace.DirtyReport{}, r)
		})
	}
}

func trimNL(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
