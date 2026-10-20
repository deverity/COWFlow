package workspace_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/domain"
	"github.com/deverity/cowflow/internal/platform"
	"github.com/deverity/cowflow/internal/workspace"
)

func TestInspectReportsActualFilesystem(t *testing.T) {
	t.Parallel()
	f := newDirtyFixture(t, "refs")
	ctx := context.Background()

	st, err := f.prov.Inspect(ctx, f.ws.ID)
	require.NoError(t, err)
	assert.Equal(t, "refs", st.Filesystem)
	assert.Equal(t, workspace.CowNone, st.CowKind, "git_worktree never reports block")
	assert.Equal(t, f.base, st.Head)
	assert.True(t, st.Dirty.Clean())
	assert.Empty(t, st.Warnings)
	assert.Equal(t, domain.WorkspaceReady, st.State)
	assert.Equal(t, f.ws.Branch, st.Branch)
	assert.Equal(t, workspace.LogsDir(f.state, f.ws.ID), st.LogsDir)

	// The volume changes under the workspace: inspect reads it now.
	f.vols.Set(platform.Volume{Filesystem: "ntfs", ID: "v1", MountPath: f.root})
	st, err = f.prov.Inspect(ctx, f.ws.ID)
	require.NoError(t, err)
	assert.Equal(t, "ntfs", st.Filesystem)
	require.Len(t, st.Warnings, 1)
	assert.Contains(t, st.Warnings[0], "ntfs")
	assert.Contains(t, st.Warnings[0], "refs")
}

func TestInspectReportsDirtyNow(t *testing.T) {
	t.Parallel()
	f := newDirtyFixture(t, "ntfs")
	f.write(t, "late.txt", "x")
	st, err := f.prov.Inspect(context.Background(), f.ws.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"late.txt"}, st.Dirty.Uncommitted)
}

func TestInspectPendingMount(t *testing.T) {
	t.Parallel()
	f := newDirtyFixture(t, "ntfs")
	f.vols.Unmount(f.root)
	st, err := f.prov.Inspect(context.Background(), f.ws.ID)
	require.ErrorIs(t, err, workspace.ErrPendingMount)
	require.NotNil(t, st)
	assert.Equal(t, domain.WorkspacePendingMount, st.State)
	assert.Equal(t, f.ws.ID, st.ID)
}

func TestInspectMissingPathWarns(t *testing.T) {
	t.Parallel()
	f := newDirtyFixture(t, "ntfs")
	require.NoError(t, os.RemoveAll(f.ws.Path))
	st, err := f.prov.Inspect(context.Background(), f.ws.ID)
	require.NoError(t, err)
	require.Len(t, st.Warnings, 1)
	assert.Contains(t, st.Warnings[0], "missing")
	assert.Empty(t, st.Head)
}

func TestInspectUnknownWorkspace(t *testing.T) {
	t.Parallel()
	f := newDirtyFixture(t, "ntfs")
	_, err := f.prov.Inspect(context.Background(), "wks_nope")
	require.ErrorIs(t, err, workspace.ErrNotFound)
}

func TestInspectWithoutInspectorFails(t *testing.T) {
	t.Parallel()
	p := workspace.NewWorktreeProvider(nil, nil)
	_, err := p.Inspect(context.Background(), "wks_x")
	require.Error(t, err)
}
