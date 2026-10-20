package integration_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/cli"
	"github.com/deverity/cowflow/internal/platform"
	"github.com/deverity/cowflow/internal/workspace"
)

// sharedFile is a tracked file at the root of the generated repository, so
// it is in every workspace whatever the sparse scope.
const sharedFile = "go.mod"

// wantCowKind is the cow_kind each provider reports (SCOPE.md §6.2).
func wantCowKind(provider string) string {
	if provider == providerCow {
		return workspace.CowBlock.String()
	}
	return workspace.CowNone.String()
}

// AC#1: 20 workspaces with independent branches.
func TestAC01_TwentyIndependentBranches(t *testing.T) {
	t.Parallel()
	forEachProvider(t, func(t *testing.T, provider string) {
		e := newEnv(t, provider)
		mainHead := e.gitIn(e.repoDir, "rev-parse", "HEAD")

		docs := make([]taskDoc, 0, 20)
		branches := map[string]bool{}
		for i := 1; i <= 20; i++ {
			doc := e.createTask(fmt.Sprintf("t%02d", i))
			assert.Equal(t, provider, doc.Workspace.Provider, "AC#1: provider of %s", doc.Task.Slug)
			assert.False(t, branches[doc.Workspace.Branch], "AC#1: branch %s is used twice", doc.Workspace.Branch)
			branches[doc.Workspace.Branch] = true
			assert.True(t, e.refExists("refs/heads/"+doc.Workspace.Branch), "AC#1: branch %s exists", doc.Workspace.Branch)
			docs = append(docs, doc)
		}
		require.Len(t, branches, 20, "AC#1: 20 distinct branches")

		// git worktree list: the main checkout plus one entry per workspace.
		wts := e.worktreePaths()
		require.Len(t, wts, 21, "AC#1: 20 extra worktree entries: %v", wts)
		for _, d := range docs {
			assert.Contains(t, wts, filepath.Clean(d.Workspace.Path), "AC#1: worktree entry of %s", d.Task.Slug)
		}

		heads := map[string]string{}
		for _, d := range docs {
			heads[d.Task.Slug] = e.gitIn(d.Workspace.Path, "rev-parse", "HEAD")
			assert.Equal(t, d.Workspace.BaseSHA, heads[d.Task.Slug], "AC#1: %s starts at its base", d.Task.Slug)
		}

		// One commit in t05 moves no other HEAD and no other branch.
		t05 := docs[4]
		require.Equal(t, "t05", t05.Task.Slug)
		writeFile(t, t05.Workspace.Path, "t05.txt", "only in t05\n")
		e.gitIn(t05.Workspace.Path, "add", "t05.txt")
		e.gitIn(t05.Workspace.Path, "commit", "-q", "-m", "feat: work in t05")
		newHead := e.gitIn(t05.Workspace.Path, "rev-parse", "HEAD")
		require.NotEqual(t, heads["t05"], newHead)
		assert.Equal(t, newHead, e.gitIn(e.repoDir, "rev-parse", "refs/heads/"+t05.Workspace.Branch))

		for _, d := range docs {
			if d.Task.Slug == "t05" {
				continue
			}
			assert.Equal(t, heads[d.Task.Slug], e.gitIn(d.Workspace.Path, "rev-parse", "HEAD"),
				"AC#1: the commit in t05 moved HEAD of %s", d.Task.Slug)
			assert.Equal(t, heads[d.Task.Slug], e.gitIn(e.repoDir, "rev-parse", "refs/heads/"+d.Workspace.Branch),
				"AC#1: the commit in t05 moved branch %s", d.Workspace.Branch)
			assert.NoFileExists(t, filepath.Join(d.Workspace.Path, "t05.txt"))
		}
		assert.Equal(t, mainHead, e.gitIn(e.repoDir, "rev-parse", "HEAD"), "AC#1: main checkout HEAD unchanged")
	})
}

// AC#2: the same file edited in two workspaces without interference.
func TestAC02_SameFileTwoWorkspaces(t *testing.T) {
	t.Parallel()
	forEachProvider(t, func(t *testing.T, provider string) {
		e := newEnv(t, provider)
		mainContent := readFile(t, e.repoDir, sharedFile)
		mainStatus := e.statusPaths(e.repoDir)
		mainHead := e.gitIn(e.repoDir, "rev-parse", "HEAD")

		w1, w2 := e.createTask("t01"), e.createTask("t02")
		require.Equal(t, w1.Workspace.BaseSHA, w2.Workspace.BaseSHA)
		original := readFile(t, w1.Workspace.Path, sharedFile)
		require.Equal(t, original, readFile(t, w2.Workspace.Path, sharedFile))

		var goldenBefore string
		if provider == providerCow {
			goldenBefore = treeChecksum(t, e.goldenDir(w1.Workspace.BaseSHA))
		}

		appendFile(t, w1.Workspace.Path, sharedFile, "edited in t01\n")
		appendFile(t, w2.Workspace.Path, sharedFile, "edited in t02\n")

		assert.Equal(t, original+"edited in t01\n", readFile(t, w1.Workspace.Path, sharedFile), "AC#2: t01 holds only its own text")
		assert.Equal(t, original+"edited in t02\n", readFile(t, w2.Workspace.Path, sharedFile), "AC#2: t02 holds only its own text")
		for _, w := range []taskDoc{w1, w2} {
			assert.Equal(t, []string{sharedFile}, e.statusPaths(w.Workspace.Path), "AC#2: git status in %s", w.Task.Slug)
			got := e.inspect(w.Workspace.ID)
			assert.Equal(t, []string{sharedFile}, got.Dirty.Uncommitted, "AC#2: inspect of %s", w.Task.Slug)
			assert.Empty(t, got.Dirty.Unpublished)
		}

		if provider == providerCow {
			assert.Equal(t, goldenBefore, treeChecksum(t, e.goldenDir(w1.Workspace.BaseSHA)), "AC#2: golden tree unchanged")
			assert.Equal(t, original, readFile(t, e.goldenDir(w1.Workspace.BaseSHA), sharedFile))
		}
		assert.Equal(t, mainContent, readFile(t, e.repoDir, sharedFile), "AC#2: main checkout file unchanged")
		assert.Equal(t, mainStatus, e.statusPaths(e.repoDir), "AC#2: main checkout status unchanged")
		assert.Equal(t, mainHead, e.gitIn(e.repoDir, "rev-parse", "HEAD"))
	})
}

// AC#12: destroy never removes unpreserved changes without --force; with
// --force it rescues them to refs/cowflow/rescue/<workspace_id> first.
// Git-ignored files are not preserved (SCOPE.md §23 decision 47).
func TestAC12_DestroyRefusesUnpreserved(t *testing.T) {
	t.Parallel()
	forEachProvider(t, func(t *testing.T, provider string) {
		e := newEnv(t, provider)
		// An ignore rule for the ignored-only case; info/exclude is shared by
		// every worktree of the repository.
		writeFile(t, e.gitIn(e.repoDir, "rev-parse", "--absolute-git-dir"), "info/exclude", "*.scratch\n")

		cases := []struct {
			name string
			// dirty makes the change and returns the files that must survive
			// a refused destroy, with their content.
			dirty func(t *testing.T, dir string) map[string]string
			// rescued checks the rescue ref of a forced destroy.
			rescued func(t *testing.T, ref, head string)
		}{{
			name: "modified",
			dirty: func(t *testing.T, dir string) map[string]string {
				appendFile(t, dir, sharedFile, "modified\n")
				return map[string]string{sharedFile: readFile(t, dir, sharedFile)}
			},
			rescued: func(t *testing.T, ref, _ string) {
				assert.Contains(t, e.gitIn(e.repoDir, "show", ref+":"+sharedFile), "modified")
			},
		}, {
			name: "staged",
			dirty: func(t *testing.T, dir string) map[string]string {
				writeFile(t, dir, "staged.txt", "staged\n")
				e.gitIn(dir, "add", "staged.txt")
				return map[string]string{"staged.txt": "staged\n"}
			},
			rescued: func(t *testing.T, ref, _ string) {
				assert.Equal(t, "staged", e.gitIn(e.repoDir, "show", ref+":staged.txt"))
			},
		}, {
			name: "untracked",
			dirty: func(t *testing.T, dir string) map[string]string {
				writeFile(t, dir, "new/untracked.txt", "untracked\n")
				return map[string]string{"new/untracked.txt": "untracked\n"}
			},
			rescued: func(t *testing.T, ref, _ string) {
				assert.Equal(t, "untracked", e.gitIn(e.repoDir, "show", ref+":new/untracked.txt"))
			},
		}, {
			name: "unpublished-commit",
			dirty: func(t *testing.T, dir string) map[string]string {
				writeFile(t, dir, "committed.txt", "committed\n")
				e.gitIn(dir, "add", "committed.txt")
				e.gitIn(dir, "commit", "-q", "-m", "feat: not published")
				return map[string]string{"committed.txt": "committed\n"}
			},
			rescued: func(t *testing.T, ref, head string) {
				e.gitIn(e.repoDir, "merge-base", "--is-ancestor", head, ref)
				assert.Equal(t, "committed", e.gitIn(e.repoDir, "show", ref+":committed.txt"))
			},
		}}
		for i, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				w := e.createTask(fmt.Sprintf("dirty%d", i+1))
				dir, id := w.Workspace.Path, w.Workspace.ID
				files := tc.dirty(t, dir)
				head := e.gitIn(dir, "rev-parse", "HEAD")

				r := e.runCLI("workspace", "destroy", id)
				assert.Equal(t, cli.ExitPolicy, r.code, "AC#12: refused for unpreserved work (exit 4, §14)\n%s", r)
				assert.Contains(t, r.stderr, "unpreserved changes")
				for rel, content := range files {
					assert.Equal(t, content, readFile(t, dir, rel), "AC#12: %s still on disk", rel)
				}
				assert.Equal(t, head, e.gitIn(dir, "rev-parse", "HEAD"))
				assert.Equal(t, "ready", e.inspect(id).State)
				assert.False(t, e.refExists(workspace.RescueRef(id)), "no rescue without --force")

				d := decode[destroyDoc](t, e.mustCLI("workspace", "destroy", id, "--force", "--json"))
				assert.True(t, d.Destroyed)
				assert.Equal(t, workspace.RescueRef(id), d.RescueRef, "AC#12: --force rescues first")
				require.True(t, e.refExists(d.RescueRef))
				tc.rescued(t, d.RescueRef, head)
				assert.NoDirExists(t, dir)
				assert.NotContains(t, e.worktreePaths(), filepath.Clean(dir))
			})
		}

		t.Run("clean", func(t *testing.T) {
			w := e.createTask("clean")
			r := e.mustCLI("workspace", "destroy", w.Workspace.ID, "--json")
			d := decode[destroyDoc](t, r)
			assert.True(t, d.Destroyed)
			assert.Empty(t, d.RescueRef, "nothing to rescue")
			assert.NoDirExists(t, w.Workspace.Path, "AC#12: a clean workspace is removed")
			assert.NotContains(t, e.worktreePaths(), filepath.Clean(w.Workspace.Path))
			assert.True(t, e.refExists("refs/heads/"+w.Workspace.Branch), "the task branch is kept")
			assert.Equal(t, "destroyed", e.inspect(w.Workspace.ID).State)
			for _, row := range e.list() {
				assert.NotEqual(t, w.Workspace.ID, row.ID, "destroyed workspaces are hidden")
			}
		})

		t.Run("ignored-only", func(t *testing.T) {
			w := e.createTask("ignored")
			writeFile(t, w.Workspace.Path, "build.scratch", "ignored\n")
			require.Empty(t, e.statusPaths(w.Workspace.Path), "the file is ignored")
			e.mustCLI("workspace", "destroy", w.Workspace.ID)
			assert.NoDirExists(t, w.Workspace.Path, "AC#12: git-ignored files are not preserved")
			assert.False(t, e.refExists(workspace.RescueRef(w.Workspace.ID)))
		})
	})
}

// AC#13: inspect reports the actual filesystem and CowKind.
func TestAC13_InspectReportsFilesystemAndCowKind(t *testing.T) {
	t.Parallel()
	forEachProvider(t, func(t *testing.T, provider string) {
		e := newEnv(t, provider)
		w := e.createTask("t01")
		checkInspect(t, e, w, provider, wantCowKind(provider))
	})

	// On Windows CI a plain NTFS directory gives git_worktree and none, even
	// with provider = auto (the cow_clone probe fails there).
	t.Run("ntfs", func(t *testing.T) {
		t.Parallel()
		dir := os.Getenv(envNTFSDir)
		if runtime.GOOS != "windows" || dir == "" {
			t.Skipf("needs Windows and %s (a plain NTFS directory)", envNTFSDir)
		}
		require.NoError(t, os.MkdirAll(dir, 0o750)) //nolint:gosec // G703: test volume chosen by the CI job
		base, err := os.MkdirTemp(dir, "cowflow-ac13-")
		require.NoError(t, err)
		t.Cleanup(func() { _ = os.RemoveAll(base) }) //nolint:gosec // G703: the test's own temporary directory

		e := newEnv(t, providerWorktree, withBase(base), withProviderSetting(providerAuto))
		w := e.createTask("t01")
		got := checkInspect(t, e, w, providerWorktree, workspace.CowNone.String())
		assert.Equal(t, "ntfs", got.Filesystem, "AC#13")
		assert.NotEmpty(t, w.Warnings, "auto fell back with a warning")
	})
}

// checkInspect asserts AC#13 for w: `workspace inspect --json` reports the
// filesystem the platform layer sees under the path now, and the CowKind
// of the provider.
func checkInspect(t *testing.T, e *wsEnv, w taskDoc, provider, cowKind string) inspectDoc {
	t.Helper()
	vol, err := platform.VolumeOf(context.Background(), w.Workspace.Path)
	require.NoError(t, err)
	got := e.inspect(w.Workspace.ID)
	assert.Equal(t, vol.Filesystem, got.Filesystem, "AC#13: filesystem")
	assert.Equal(t, cowKind, got.CowKind, "AC#13: cow_kind")
	assert.Equal(t, provider, got.Provider)
	assert.Equal(t, "ready", got.State)
	assert.Equal(t, w.Workspace.BaseSHA, got.Head)
	assert.Equal(t, got.Filesystem, w.Workspace.Filesystem, "task create and inspect agree")
	assert.Equal(t, got.CowKind, w.Workspace.CowKind)
	assert.False(t, strings.ContainsAny(got.Filesystem, "ABCDEFGHIJKLMNOPQRSTUVWXYZ"), "filesystem names are lowercase")
	return got
}
