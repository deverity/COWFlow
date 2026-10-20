package workspace_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/config"
	"github.com/deverity/cowflow/internal/domain"
	"github.com/deverity/cowflow/internal/platform"
	"github.com/deverity/cowflow/internal/workspace"
	"github.com/deverity/cowflow/internal/workspace/workspacetest"
)

// The Manager builds cow_clone with the shared Inspector, so callers need no
// extra wiring for cow_clone workspaces to be inspectable.
func TestManagerCowCloneInspectsThroughSharedInspector(t *testing.T) {
	t.Parallel()
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	cfg := config.Default()
	cfg.Workspace.Root = base
	vols := workspacetest.NewFakeVolumes(platform.Volume{Filesystem: "btrfs", ID: "v", MountPath: base})
	st := workspacetest.NewMemStore(domain.SystemClock{})
	m := workspace.NewManager(workspace.Deps{
		Config: cfg, Store: st, Git: &workspacetest.FakeGit{},
		Volumes: vols, CowClone: true, Logger: slog.New(slog.DiscardHandler),
	})
	ctx := context.Background()
	_, err = st.InsertWorkspace(ctx, domain.WorkspaceRecord{
		ID: "wks_cc", ProjectID: "prj_1", TaskID: "tsk_cc", Provider: workspace.ProviderCowClone,
		CowKind: "block", Filesystem: "btrfs", Path: filepath.Join(base, "gone"),
		Branch: "feature/cc", BaseSHA: "abc", OpID: "op-cc",
	}, 10, 40000)
	require.NoError(t, err)
	require.NoError(t, st.SetWorkspaceState(ctx, "wks_cc", domain.WorkspaceReady, domain.StateChange{}))

	got, err := m.Inspect(ctx, "wks_cc")
	require.NoError(t, err)
	assert.Equal(t, workspace.CowBlock, got.CowKind)
	require.NotEmpty(t, got.Warnings)
	assert.Contains(t, got.Warnings[0], "missing")
}
