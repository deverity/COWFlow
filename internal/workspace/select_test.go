package workspace_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/platform"
	"github.com/deverity/cowflow/internal/workspace"
	"github.com/deverity/cowflow/internal/workspace/workspacetest"
)

// probeCaps stands in for the cow_clone provider's Capabilities (S1-T10):
// block clone only on a filesystem that can clone, with golden_root on the
// root's volume (U-PLAT-4) and a probe clone that works.
func probeCaps(vols workspace.Volumes, cloner workspace.Cloner, golden, probeDir string) func(context.Context, string) (workspace.Capabilities, error) {
	var n atomic.Int64
	return func(ctx context.Context, root string) (workspace.Capabilities, error) {
		v, err := vols.VolumeOf(ctx, root)
		if err != nil {
			return workspace.Capabilities{}, err
		}
		caps := workspace.Capabilities{Filesystem: v.Filesystem, SparseSupported: true}
		if !slices.Contains([]string{"refs", "btrfs", "xfs", "apfs"}, v.Filesystem) {
			return caps, nil
		}
		same, err := vols.SameVolume(ctx, golden, root)
		if err != nil || !same {
			return caps, err
		}
		src := filepath.Join(probeDir, "probe-src")
		if err := os.WriteFile(src, []byte("probe"), 0o600); err != nil {
			return caps, err
		}
		dst := filepath.Join(probeDir, fmt.Sprintf("probe-dst-%d", n.Add(1)))
		if err := cloner.CloneFile(ctx, src, dst); err != nil {
			return caps, nil //nolint:nilerr // a failed probe means no block clone, not a failure
		}
		caps.CopyOnWrite = workspace.CowBlock
		return caps, nil
	}
}

func TestSelectAutoMatrix(t *testing.T) {
	tests := []struct {
		name        string
		fs          string
		crossVolume bool
		probeFails  bool
		want        string
		warn        bool
	}{
		{name: "refs with block clone", fs: "refs", want: workspace.ProviderCowClone},
		{name: "ntfs", fs: "ntfs", want: workspace.ProviderGitWorktree, warn: true},
		{name: "btrfs", fs: "btrfs", want: workspace.ProviderCowClone},
		{name: "apfs", fs: "apfs", want: workspace.ProviderCowClone},
		{name: "xfs without reflink", fs: "xfs", probeFails: true, want: workspace.ProviderGitWorktree, warn: true},
		{name: "ext4", fs: "ext4", want: workspace.ProviderGitWorktree, warn: true},
		{name: "refs across volumes", fs: "refs", crossVolume: true, want: workspace.ProviderGitWorktree, warn: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t).withCow()
			f.vols.Set(platform.Volume{Filesystem: tt.fs, ID: "vol-base", MountPath: f.base})
			golden := filepath.Join(f.base, "golden")
			if tt.crossVolume {
				f.vols.Set(platform.Volume{Filesystem: tt.fs, ID: "vol-other", MountPath: golden})
			}
			cloner := &workspacetest.FakeCloner{}
			if tt.probeFails {
				cloner.Err = errors.New("operation not supported")
			}
			f.cow.caps = probeCaps(f.vols, cloner, golden, f.base)

			sel, err := f.manager().Select(context.Background(), workspace.ProviderAuto)
			require.NoError(t, err)
			assert.Equal(t, tt.want, sel.Provider.Name())
			assert.Equal(t, tt.fs, sel.Caps.Filesystem)
			if tt.want == workspace.ProviderCowClone {
				assert.Equal(t, workspace.CowBlock, sel.Caps.CopyOnWrite)
			} else {
				assert.Equal(t, workspace.CowNone, sel.Caps.CopyOnWrite)
			}
			if tt.warn {
				assert.Equal(t, fmt.Sprintf("no copy-on-write on %q (%s): using git_worktree", f.root, tt.fs), sel.Warning)
			} else {
				assert.Empty(t, sel.Warning)
			}
		})
	}
}

func TestSelectAutoWithoutCowProvider(t *testing.T) {
	f := newFixture(t) // no cow_clone in this build
	sel, err := f.manager().Select(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, workspace.ProviderGitWorktree, sel.Provider.Name())
	assert.NotEmpty(t, sel.Warning)
}

func TestSelectAutoFallsBackWhenCowProbeErrors(t *testing.T) {
	f := newFixture(t).withCow()
	f.cow.caps = func(context.Context, string) (workspace.Capabilities, error) {
		return workspace.Capabilities{}, errors.New("probe exploded")
	}
	sel, err := f.manager().Select(context.Background(), workspace.ProviderAuto)
	require.NoError(t, err)
	assert.Equal(t, workspace.ProviderGitWorktree, sel.Provider.Name())
	assert.NotEmpty(t, sel.Warning)
}

func TestSelectExplicit(t *testing.T) {
	t.Run("cow_clone honored", func(t *testing.T) {
		f := newFixture(t).withCow()
		sel, err := f.manager().Select(context.Background(), workspace.ProviderCowClone)
		require.NoError(t, err)
		assert.Equal(t, workspace.ProviderCowClone, sel.Provider.Name())
		assert.Empty(t, sel.Warning)
	})
	t.Run("cow_clone without block clone fails", func(t *testing.T) {
		f := newFixture(t).withCow()
		f.cow.caps = volumeCaps(f.vols, workspace.CowNone)
		_, err := f.manager().Select(context.Background(), workspace.ProviderCowClone)
		require.ErrorIs(t, err, workspace.ErrCloneUnsupported)
		assert.Contains(t, err.Error(), "auto")
		assert.Contains(t, err.Error(), "git_worktree")
	})
	t.Run("cow_clone reporting unsupported fails", func(t *testing.T) {
		f := newFixture(t).withCow()
		f.cow.caps = func(context.Context, string) (workspace.Capabilities, error) {
			return workspace.Capabilities{}, workspace.ErrCloneUnsupported
		}
		_, err := f.manager().Select(context.Background(), workspace.ProviderCowClone)
		require.ErrorIs(t, err, workspace.ErrCloneUnsupported)
		assert.Contains(t, err.Error(), "git_worktree")
	})
	t.Run("cow_clone not built in fails", func(t *testing.T) {
		f := newFixture(t)
		_, err := f.manager().Select(context.Background(), workspace.ProviderCowClone)
		require.ErrorIs(t, err, workspace.ErrCloneUnsupported)
	})
	t.Run("git_worktree honored on a COW volume", func(t *testing.T) {
		f := newFixture(t).withCow()
		f.vols.Set(platform.Volume{Filesystem: "refs", ID: "vol-base", MountPath: f.base})
		sel, err := f.manager().Select(context.Background(), workspace.ProviderGitWorktree)
		require.NoError(t, err)
		assert.Equal(t, workspace.ProviderGitWorktree, sel.Provider.Name())
		assert.Empty(t, sel.Warning)
	})
	t.Run("unknown", func(t *testing.T) {
		f := newFixture(t)
		_, err := f.manager().Select(context.Background(), "zfs_magic")
		require.Error(t, err)
	})
	t.Run("no root", func(t *testing.T) {
		f := newFixture(t)
		f.cfg.Workspace.Root = ""
		_, err := f.manager().Select(context.Background(), workspace.ProviderAuto)
		require.ErrorIs(t, err, workspace.ErrRootNotSet)
	})
}

func TestSelectNeverProjfs(t *testing.T) {
	f := newFixture(t).withCow()
	f.cfg.Workspace.ProjFS.Enabled = true
	m := f.manager()
	for _, name := range []string{workspace.ProviderProjFS, workspace.ProviderContainer} {
		_, err := m.Select(context.Background(), name)
		require.ErrorIs(t, err, workspace.ErrUnsupportedProvider, name)
	}
	for _, fsName := range []string{"refs", "ntfs", "ext4"} {
		f.vols.Set(platform.Volume{Filesystem: fsName, ID: "vol-base", MountPath: f.base})
		sel, err := m.Select(context.Background(), workspace.ProviderAuto)
		require.NoError(t, err)
		assert.Contains(t, []string{workspace.ProviderCowClone, workspace.ProviderGitWorktree}, sel.Provider.Name())
		assert.NotEqual(t, workspace.CowVirtual, sel.Caps.CopyOnWrite)
	}
}
