package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/cli"
	"github.com/deverity/cowflow/internal/config"
	"github.com/deverity/cowflow/internal/git"
	"github.com/deverity/cowflow/internal/platform"
	"github.com/deverity/cowflow/internal/store"
)

// TestWorkspaceDestroyGitWorktreeThroughOpenTasks drives the shipped wiring
// end to end: task create, then workspace destroy of a git_worktree
// workspace. Without a Destroyer on that provider the destroy failed with
// "destroy has no store".
func TestWorkspaceDestroyGitWorktreeThroughOpenTasks(t *testing.T) {
	bin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	repo := filepath.Join(base, "repo")
	require.NoError(t, os.MkdirAll(filepath.Join(repo, config.DirName), 0o750))
	isolated := []string{
		"GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_SYSTEM=" + os.DevNull,
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	}
	setup := func(args ...string) {
		t.Helper()
		cmd := exec.Command(bin, args...) //nolint:gosec // test-only: fixed binary, literal arguments
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), isolated...)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	setup("init", "-q", "-b", "develop")
	require.NoError(t, os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o600))
	setup("add", "a.txt")
	setup("-c", "commit.gpgsign=false", "commit", "-q", "-m", "init")

	cfgPath := filepath.Join(repo, config.DirName, config.FileName)
	text := fmt.Sprintf("[workspace]\nroot = %q\nprovider = \"git_worktree\"\n\n[conventions]\npreset = \"gitflow\"\n",
		filepath.ToSlash(filepath.Join(base, "ws")))
	require.NoError(t, os.MkdirAll(filepath.Join(base, "ws"), 0o750))
	require.NoError(t, os.WriteFile(cfgPath, []byte(text), 0o600))

	cfg, err := config.Load(cfgPath)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(config.StatePath(cfgPath, cfg), 0o750))
	canon, err := platform.Canonical(repo)
	require.NoError(t, err)
	s, err := store.Open(context.Background(), config.DBPath(cfgPath, cfg), store.Options{})
	require.NoError(t, err)
	require.NoError(t, s.WithTx(context.Background(), "", func(tx *store.Tx) error {
		_, err := tx.InsertProject("demo", canon)
		return err
	}))
	require.NoError(t, s.Close())

	runner, err := git.NewExecRunner(git.ExecOptions{Timeout: time.Minute, ExtraEnv: isolated})
	require.NoError(t, err)
	deps := cli.Deps{
		Version:    "dev",
		ConfigPath: func() (string, error) { return cfgPath, nil },
		OpenTasks:  openTasks(runner),
	}
	run := func(args ...string) (int, string, string) {
		var out, errb bytes.Buffer
		code := cli.Execute(context.Background(), args,
			cli.IOStreams{In: strings.NewReader(""), Out: &out, Err: &errb}, deps)
		return code, out.String(), errb.String()
	}

	code, out, errOut := run("task", "create", "auth", "--json")
	require.Equal(t, cli.ExitOK, code, errOut)
	var doc struct {
		Workspace struct {
			ID       string `json:"id"`
			Path     string `json:"path"`
			Provider string `json:"provider"`
		} `json:"workspace"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &doc))
	require.Equal(t, "git_worktree", doc.Workspace.Provider)
	require.DirExists(t, doc.Workspace.Path)

	code, out, errOut = run("workspace", "destroy", doc.Workspace.ID)
	require.Equal(t, cli.ExitOK, code, errOut)
	assert.Contains(t, out, "destroyed workspace "+doc.Workspace.ID)
	assert.NoDirExists(t, doc.Workspace.Path)
}
