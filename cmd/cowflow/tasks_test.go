package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/config"
	"github.com/deverity/cowflow/internal/workspace"
)

type noGit struct{}

func (noGit) Run(context.Context, string, ...string) ([]byte, error) {
	return nil, errors.New("git is not available in this test")
}

// TestOpenTasksWiresCowClone: the Manager has cow_clone built in, so an
// explicit cow_clone fails because the volume cannot block-clone, never
// because the provider is missing.
func TestOpenTasksWiresCowClone(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Workspace.Root = filepath.Join(dir, "ws")
	require.NoError(t, os.MkdirAll(cfg.Workspace.Root, 0o750))
	cfgPath := filepath.Join(dir, config.DirName, config.FileName)

	be, err := openTasks(noGit{})(context.Background(), cfg, cfgPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = be.Close() })

	m, ok := be.Workspaces.(*workspace.Manager)
	require.True(t, ok)
	_, err = m.Select(context.Background(), workspace.ProviderCowClone)
	if err != nil {
		assert.NotErrorIs(t, err, workspace.ErrUnsupportedProvider, "cow_clone is built in")
	}
}
