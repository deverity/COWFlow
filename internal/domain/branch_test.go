package domain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateSlug(t *testing.T) {
	good := []string{"a", "fix-login", "v1.2", "a_b", "0day", strings.Repeat("a", 64), "console", "com10"}
	for _, s := range good {
		require.NoError(t, ValidateSlug(s), s)
	}
	bad := []string{
		"", "-x", ".x", "_x", "Upper", "a b", "a/b", "a..b", "a.", "x.lock", "a:b",
		strings.Repeat("a", 65), "con", "nul", "com1", "lpt9", "aux.txt", "prn.v2",
	}
	for _, s := range bad {
		require.ErrorIs(t, ValidateSlug(s), ErrInvalidSlug, s)
	}
}

func TestGitflowNamer(t *testing.T) {
	n, err := NewBranchNamer(PresetGitflow)
	require.NoError(t, err)
	got, err := n.TaskBranch("demo", "", "login")
	require.NoError(t, err)
	require.Equal(t, "feature/login", got)
	for _, k := range []string{"feature", "bugfix", "chore", "hotfix", "release"} {
		got, err = n.TaskBranch("demo", k, "x")
		require.NoError(t, err)
		require.Equal(t, k+"/x", got)
	}
	_, err = n.TaskBranch("demo", "wip", "x")
	require.ErrorIs(t, err, ErrNonConformingBranch)
	_, err = n.TaskBranch("demo", "", "Bad Slug")
	require.ErrorIs(t, err, ErrInvalidSlug)
}

func TestPlainNamer(t *testing.T) {
	for _, preset := range []string{PresetTrunk, PresetNone} {
		n, err := NewBranchNamer(preset)
		require.NoError(t, err)
		got, err := n.TaskBranch("demo", "", "login")
		require.NoError(t, err)
		require.Equal(t, "cowflow/demo/login", got)
		_, err = n.TaskBranch("demo", "feature", "login")
		require.ErrorIs(t, err, ErrKindNotAllowed)
		_, err = n.TaskBranch("my project", "", "login")
		require.ErrorIs(t, err, ErrInvalidProject)
		_, err = n.TaskBranch("", "", "login")
		require.ErrorIs(t, err, ErrInvalidProject)
		_, err = n.TaskBranch("demo", "", "con")
		require.ErrorIs(t, err, ErrInvalidSlug)
	}
}

func TestNewBranchNamerUnknownPreset(t *testing.T) {
	_, err := NewBranchNamer("svn")
	require.Error(t, err)
}

func TestWorkspaceStateLive(t *testing.T) {
	for s, live := range map[WorkspaceState]bool{
		WorkspaceCreating: true, WorkspacePendingMount: true, WorkspaceReady: true,
		WorkspacePendingCleanup: true, WorkspaceFailed: false, WorkspaceDestroyed: false,
		"bogus": false,
	} {
		require.Equal(t, live, s.Live(), s)
	}
}
