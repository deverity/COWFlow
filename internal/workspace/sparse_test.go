package workspace_test

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/domain"
	"github.com/deverity/cowflow/internal/git"
	"github.com/deverity/cowflow/internal/platform"
	"github.com/deverity/cowflow/internal/workspace"
	"github.com/deverity/cowflow/internal/workspace/workspacetest"
	"github.com/deverity/cowflow/test/harness"
)

// sparseFiles is a tree with files at every depth, directories nested in
// each other, siblings of ancestors and a contracts directory.
func sparseFiles() map[string]string {
	files := map[string]string{}
	for _, p := range []string{
		"README.md", "go.mod", "a.txt",
		"contracts/api.yaml", "contracts/v2/schema.json",
		"cmd/root.go", "cmd/app/main.go", "cmd/app/flags.go", "cmd/tool/main.go",
		"internal/z.go", "internal/x/x.go", "internal/x/sub/y.go", "internal/y/y.go",
		"docs/readme.md", "docs/deep/e.txt", "docs/deep/a/d.txt", "docs/deep/a/b/c.txt",
		"pkg/h.go", "pkg/a/g.go", "pkg/a/b/f.go", "pkg/a/b/c/d/e.go", "pkg/a/b/c/d/e2.go",
		"tools/gen/gen.go", "tools/gen/tmpl/t.tmpl",
	} {
		files[p] = "content of " + p + "\n"
	}
	return files
}

// sparseDirs lists every directory of sparseFiles.
func sparseDirs() []string {
	seen := map[string]bool{}
	for p := range sparseFiles() {
		for d := path.Dir(p); d != "."; d = path.Dir(d) {
			seen[d] = true
		}
	}
	dirs := make([]string, 0, len(seen))
	for d := range seen {
		dirs = append(dirs, d)
	}
	slices.Sort(dirs)
	return dirs
}

// lsFilesTags returns the `git ls-files -t` tag of every tracked file of wt:
// H is materialized, S is skip-worktree.
func lsFilesTags(t testing.TB, r git.Runner, wt string) map[string]string {
	t.Helper()
	out, err := r.Run(context.Background(), wt, "ls-files", "-t")
	require.NoError(t, err)
	tags := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		tag, p, ok := strings.Cut(line, " ")
		require.True(t, ok, "ls-files line %q", line)
		tags[p] = tag
	}
	return tags
}

// assertDiskIsNotSkipped checks acceptance: the files on disk are exactly
// those git reports as not skip-worktree.
func assertDiskIsNotSkipped(t testing.TB, r git.Runner, wt string) {
	t.Helper()
	var want []string
	for p, tag := range lsFilesTags(t, r, wt) {
		if tag == "H" {
			want = append(want, p)
		}
	}
	var got []string
	for p := range ccTree(t, wt) {
		got = append(got, p)
	}
	slices.Sort(want)
	slices.Sort(got)
	assert.Equal(t, want, got, "files on disk vs files git does not skip")
}

// sparseWtProvider is a git_worktree provider over a repo holding files.
func sparseWtProvider(t *testing.T) (*harness.Repo, *git.Client, *workspace.WorktreeProvider, string, string) {
	t.Helper()
	repo := harness.NewRepo(t)
	sha := repo.Commit(t, "feat: tree", sparseFiles())
	client := git.NewClient(repo.Runner)
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	vols := workspacetest.NewFakeVolumes(platform.Volume{Filesystem: "ext4", ID: "v1", MountPath: root})
	return repo, client, workspace.NewWorktreeProvider(client, vols), root, sha
}

func sparseSpec(repo *harness.Repo, root, sha, slug string, paths []string) workspace.WorkspaceSpec {
	return workspace.WorkspaceSpec{
		ID: "wks_" + slug, OpID: "op-" + slug, ProjectID: "prj_1", TaskID: "tsk_" + slug,
		RepoDir: repo.Dir, Path: filepath.Join(root, slug), Branch: "feature/" + slug, BaseSHA: sha,
		Sparse: workspace.SparseSpec{Enabled: len(paths) > 0, Paths: paths},
	}
}

func TestConeMatcherAgreesWithGit(t *testing.T) {
	t.Parallel()
	repo, _, prov, root, sha := sparseWtProvider(t)
	dirs := sparseDirs()
	rng := rand.New(rand.NewPCG(7, 11)) //nolint:gosec // G404: a seeded generator picks reproducible test path sets
	for i := range 20 {
		n := 1 + rng.IntN(3)
		var paths []string
		for range n {
			paths = append(paths, dirs[rng.IntN(len(dirs))])
		}
		if i%5 == 4 {
			paths = append(paths, `cmd\app\`) // normalization
		}
		set, err := workspace.SparseSet(paths)
		require.NoError(t, err)

		slug := fmt.Sprintf("m%02d", i)
		w, err := prov.Create(context.Background(), sparseSpec(repo, root, sha, slug, paths))
		require.NoError(t, err, "paths %v", paths)
		tags := lsFilesTags(t, repo.Runner, w.Path)
		require.NotEmpty(t, tags)
		onDisk := ccTree(t, w.Path)
		for p, tag := range tags {
			assert.Equal(t, tag == "H", workspace.ConeMatch(set, p, false), "set %v: file %s tagged %s", set, p, tag)
			_, here := onDisk[p]
			assert.Equal(t, tag == "H", here, "set %v: file %s on disk", set, p)
			if tag == "H" {
				for d := path.Dir(p); d != "."; d = path.Dir(d) {
					assert.True(t, workspace.ConeMatch(set, d, true), "set %v: dir %s holds %s", set, d, p)
				}
			}
		}
		for _, d := range dirs {
			if !workspace.ConeMatch(set, d, true) {
				for p, tag := range tags {
					assert.False(t, tag == "H" && strings.HasPrefix(p, d+"/"),
						"set %v: skipped dir %s holds materialized %s", set, d, p)
				}
			}
		}
	}
}

func TestAlwaysSetIncluded(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"contracts"}, workspace.AlwaysSet())
	got := workspace.AlwaysSet()
	got[0] = "mutated"
	assert.Equal(t, []string{"contracts"}, workspace.AlwaysSet(), "a copy")

	set, err := workspace.SparseSet([]string{`cmd\app\`, "cmd/app", "contracts"})
	require.NoError(t, err)
	assert.Equal(t, []string{"cmd/app", "contracts"}, set)

	for _, bad := range []string{"", "/abs", "../x", "a/*", "-x"} {
		_, err := workspace.SparseSet([]string{"ok", bad})
		require.ErrorIs(t, err, git.ErrInvalidArgument, "%q", bad)
	}

	repo, _, prov, root, sha := sparseWtProvider(t)
	w, err := prov.Create(context.Background(), sparseSpec(repo, root, sha, "always", []string{"docs/deep"}))
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(w.Path, "contracts", "api.yaml"))
	assert.FileExists(t, filepath.Join(w.Path, "contracts", "v2", "schema.json"))
	assert.FileExists(t, filepath.Join(w.Path, "docs", "deep", "a", "b", "c.txt"))
	assert.FileExists(t, filepath.Join(w.Path, "docs", "readme.md"), "file directly in an ancestor")
	assert.FileExists(t, filepath.Join(w.Path, "go.mod"))
	assert.NoFileExists(t, filepath.Join(w.Path, "cmd", "app", "main.go"))
}

func TestSparseOffWithoutPaths(t *testing.T) {
	t.Parallel()
	repo, client, prov, root, sha := sparseWtProvider(t)
	spec := sparseSpec(repo, root, sha, "off", nil)
	w, err := prov.Create(context.Background(), spec)
	require.NoError(t, err)
	assert.False(t, w.Sparse.Enabled)
	assert.Len(t, ccTree(t, w.Path), len(sparseFiles())+1, "everything, .gitattributes included")
	_, err = client.SparseList(context.Background(), w.Path)
	require.Error(t, err, "sparse checkout is not enabled")
}

func TestSparseOffByConfig(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		byIntent bool
		sparse   bool
		paths    []string
		want     bool
	}{
		{"config off", false, true, []string{"cmd"}, false},
		{"task flag off", true, false, []string{"cmd"}, false},
		{"no paths", true, true, nil, false},
		{"all ask", true, true, []string{"cmd"}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newSparseManager(t, workspace.ProviderGitWorktree)
			f.cfg.Workspace.SparseByIntent = tt.byIntent
			w := f.create(t, "off", tt.paths, tt.sparse)
			assert.Equal(t, tt.want, w.Sparse.Enabled)
			_, err := f.client.SparseList(context.Background(), w.Path)
			if tt.want {
				require.NoError(t, err)
				assert.Less(t, len(ccTree(t, w.Path)), len(sparseFiles()))
			} else {
				require.Error(t, err)
				assert.Len(t, ccTree(t, w.Path), len(sparseFiles())+1)
			}
		})
	}
}

func TestWorktreeSparseOnlyMaterializesScope(t *testing.T) {
	t.Parallel()
	repo, _, prov, root, sha := sparseWtProvider(t)
	w, err := prov.Create(context.Background(), sparseSpec(repo, root, sha, "scope", []string{"cmd/app", "pkg/a/b"}))
	require.NoError(t, err)
	got := ccTree(t, w.Path)
	var names []string
	for p := range got {
		names = append(names, p)
	}
	slices.Sort(names)
	assert.Equal(t, []string{
		".gitattributes", "README.md", "a.txt", "cmd/app/flags.go", "cmd/app/main.go", "cmd/root.go",
		"contracts/api.yaml", "contracts/v2/schema.json", "go.mod",
		"pkg/a/b/c/d/e.go", "pkg/a/b/c/d/e2.go", "pkg/a/b/f.go", "pkg/a/g.go", "pkg/h.go",
	}, names)
	assertDiskIsNotSkipped(t, repo.Runner, w.Path)
	out, err := repo.Runner.Run(context.Background(), w.Path, "status", "--porcelain", "--untracked-files=all")
	require.NoError(t, err)
	assert.Empty(t, string(out), "clean right after create")
}

func TestWorktreeSparseCreateFailureCompensates(t *testing.T) {
	t.Parallel()
	repo, _, prov, root, sha := sparseWtProvider(t)
	// A file path is refused by SparseSet after the worktree exists.
	_, err := prov.Create(context.Background(), sparseSpec(repo, root, sha, "bad", []string{"a.txt"}))
	require.ErrorIs(t, err, git.ErrInvalidArgument)
	assert.NoDirExists(t, filepath.Join(root, "bad"))
}

func TestCowCloneSparseStatusClean(t *testing.T) {
	t.Parallel()
	cl := &workspacetest.FakeCloner{}
	f := newCCFixture(t, false, cl, sparseFiles())
	spec := f.spec("sp")
	spec.Sparse = workspace.SparseSpec{Enabled: true, Paths: []string{"cmd/app", "pkg/a/b"}}
	w, err := f.prov.Create(context.Background(), spec)
	require.NoError(t, err)

	assert.Empty(t, f.status(t, w.Path), "git status is clean right after create")
	assertDiskIsNotSkipped(t, f.repo.Runner, w.Path)
	want := map[string]bool{}
	for _, p := range []string{
		".gitattributes", "README.md", "a.txt", "cmd/app/flags.go", "cmd/app/main.go", "cmd/root.go",
		"contracts/api.yaml", "contracts/v2/schema.json", "go.mod",
		"pkg/a/b/c/d/e.go", "pkg/a/b/c/d/e2.go", "pkg/a/b/f.go", "pkg/a/g.go", "pkg/h.go",
	} {
		want[p] = true
	}
	for p := range ccTree(t, w.Path) {
		assert.True(t, want[p], "unexpected file %s", p)
		delete(want, p)
	}
	assert.Empty(t, want, "missing files")
	assert.Equal(t, 14, cl.Calls()-2, "only the matching files were cloned, besides the probe and the index")
	assert.Equal(t, f.sha, strings.TrimSpace(f.git(t, w.Path, "rev-parse", "HEAD")))
}

func TestCowCloneSparseFileCountCannotExceedGolden(t *testing.T) {
	t.Parallel()
	f := newFakeCC(t, func(g *workspace.Golden) { g.FileCount = 0 })
	f.spec.Sparse = workspace.SparseSpec{Enabled: true, Paths: []string{"zzz"}}
	_, err := f.prov.Create(context.Background(), f.spec)
	require.Error(t, err)
}

func TestCowCloneSparseFakeGitOrder(t *testing.T) {
	t.Parallel()
	f := newFakeCC(t, nil)
	f.spec.Sparse = workspace.SparseSpec{Enabled: true, Paths: []string{"src"}}
	_, err := f.prov.Create(context.Background(), f.spec)
	require.NoError(t, err)
	var order []string
	for _, c := range f.git.Calls() {
		switch c.Method {
		case "SparseSet":
			assert.Equal(t, []string{"contracts", "src"}, c.Args[1])
			order = append(order, "SparseSet")
		case "ConfigSetWorktree":
			if c.Args[1] == "core.trustCtime" {
				order = append(order, "seed")
			}
		case "Head":
			order = append(order, "Head")
		}
	}
	assert.Equal(t, []string{"seed", "SparseSet", "Head"}, order, "index first, then SparseSet (decision 39)")
}

// --- ExpandScope ---

type sparseManager struct {
	*ccFixture
	m        *workspace.Manager
	cloner   *workspacetest.FakeCloner
	provider string
}

func newSparseManager(t *testing.T, provider string) *sparseManager {
	t.Helper()
	cl := &workspacetest.FakeCloner{}
	f := newCCFixture(t, false, cl, sparseFiles())
	wt := workspace.NewWorktreeProvider(f.client, f.vols)
	m := workspace.NewManager(workspace.Deps{
		Config: f.cfg, Store: f.store, Git: f.client, Volumes: f.vols, DevDrive: &harness.SimDevDrive{},
		Cow: f.prov, Worktree: wt, Logger: slog.New(slog.DiscardHandler),
	})
	f.cfg.Workspace.SparseByIntent = true
	return &sparseManager{ccFixture: f, m: m, cloner: cl, provider: provider}
}

func (f *sparseManager) create(t *testing.T, slug string, paths []string, sparse bool) *workspace.Workspace {
	t.Helper()
	// The Manager copies the config at construction; rebuild with the final one.
	f.m = workspace.NewManager(workspace.Deps{
		Config: f.cfg, Store: f.store, Git: f.client, Volumes: f.vols, DevDrive: &harness.SimDevDrive{},
		Cow: f.prov, Worktree: workspace.NewWorktreeProvider(f.client, f.vols), Logger: slog.New(slog.DiscardHandler),
	})
	w, _, err := f.m.Create(context.Background(), workspace.CreateRequest{
		OpID: "op-" + slug, ProjectID: "prj_1", RepoDir: f.repo.Dir, Provider: f.provider,
		Task: domain.Task{
			ID: "tsk_" + slug, ProjectID: "prj_1", Slug: slug, Branch: "feature/" + slug, Base: "develop",
			BaseSHA: f.sha, Paths: paths, Sparse: sparse, State: domain.TaskStateCreated,
		},
	})
	require.NoError(t, err)
	require.Equal(t, f.provider, w.Provider)
	return w
}

func TestExpandScopeAddsFiles(t *testing.T) {
	for _, provider := range []string{workspace.ProviderGitWorktree, workspace.ProviderCowClone} {
		t.Run(provider, func(t *testing.T) {
			f := newSparseManager(t, provider)
			w := f.create(t, "exp", []string{"cmd/app"}, true)
			require.NoFileExists(t, filepath.Join(w.Path, "docs", "deep", "a", "b", "c.txt"))
			before := f.cloner.Calls()

			require.NoError(t, f.m.ExpandScope(context.Background(), w.ID, []string{"docs/deep", `internal/x\`}))
			assert.FileExists(t, filepath.Join(w.Path, "docs", "deep", "a", "b", "c.txt"))
			assert.FileExists(t, filepath.Join(w.Path, "internal", "x", "sub", "y.go"))
			assert.NoFileExists(t, filepath.Join(w.Path, "internal", "y", "y.go"))
			assert.FileExists(t, filepath.Join(w.Path, "cmd", "app", "main.go"), "the old scope stays")
			assert.Empty(t, f.status(t, w.Path))
			assertDiskIsNotSkipped(t, f.repo.Runner, w.Path)
			if provider == workspace.ProviderCowClone {
				// docs/readme.md, docs/deep/{e.txt,a/d.txt,a/b/c.txt}, internal/z.go, internal/x/{x.go,sub/y.go}
				assert.Equal(t, 7, f.cloner.Calls()-before, "the new files were cloned from the golden, not written by git")
			}

			// A second expansion keeps the first.
			require.NoError(t, f.m.ExpandScope(context.Background(), w.ID, []string{"tools/gen"}))
			row, err := f.store.GetWorkspace(context.Background(), w.ID)
			require.NoError(t, err)
			assert.Equal(t, []string{"cmd/app", "docs/deep", "internal/x", "tools/gen"}, row.SparsePaths,
				"the row records the old paths plus the normalized new ones")
			assert.FileExists(t, filepath.Join(w.Path, "tools", "gen", "tmpl", "t.tmpl"))
			assert.FileExists(t, filepath.Join(w.Path, "docs", "deep", "a", "b", "c.txt"))
			assert.Empty(t, f.status(t, w.Path))
			assertDiskIsNotSkipped(t, f.repo.Runner, w.Path)
		})
	}
}

func TestExpandScopeAfterCommitUsesGit(t *testing.T) {
	f := newSparseManager(t, workspace.ProviderCowClone)
	w := f.create(t, "post", []string{"cmd/app"}, true)
	require.NoError(t, os.WriteFile(filepath.Join(w.Path, "cmd", "app", "new.go"), []byte("package app\n"), 0o600))
	f.git(t, w.Path, "add", "cmd/app/new.go")
	f.git(t, w.Path, "-c", "user.name=t", "-c", "user.email=t@example.test", "commit", "-q", "-m", "feat: new")
	before := f.cloner.Calls()

	require.NoError(t, f.m.ExpandScope(context.Background(), w.ID, []string{"docs/deep"}))
	assert.Equal(t, before, f.cloner.Calls(), "HEAD moved: the golden is not used")
	assert.FileExists(t, filepath.Join(w.Path, "docs", "deep", "a", "b", "c.txt"))
	assert.Empty(t, f.status(t, w.Path))
	assertDiskIsNotSkipped(t, f.repo.Runner, w.Path)
}

func TestExpandScopeValidatesAndSkipsNonSparse(t *testing.T) {
	f := newSparseManager(t, workspace.ProviderGitWorktree)
	w := f.create(t, "val", []string{"cmd/app"}, true)
	err := f.m.ExpandScope(context.Background(), w.ID, []string{"ok", "../escape"})
	require.ErrorIs(t, err, git.ErrInvalidArgument)
	assert.NoDirExists(t, filepath.Join(w.Path, "ok"))
	assert.NoFileExists(t, filepath.Join(w.Path, "docs", "readme.md"), "nothing was widened")

	full := f.create(t, "full", nil, true)
	require.NoError(t, f.m.ExpandScope(context.Background(), full.ID, []string{"docs"}), "not sparse: no-op")
	require.ErrorIs(t, f.m.ExpandScope(context.Background(), "wks_missing", []string{"docs"}), workspace.ErrNotFound)
}
