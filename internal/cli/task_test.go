package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/config"
	"github.com/deverity/cowflow/internal/domain"
	"github.com/deverity/cowflow/internal/platform"
	"github.com/deverity/cowflow/internal/store"
	"github.com/deverity/cowflow/internal/workspace"
	"github.com/deverity/cowflow/internal/workspace/workspacetest"
	"github.com/deverity/cowflow/test/harness"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden files")

const (
	taskDevelopSHA = "3f2c9e1aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	taskMainSHA    = "9b8a7c6bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// taskEnv is a repository directory with a config file, a real SQLite store
// and a real workspace.Manager over fake git, volumes and provider.
type taskEnv struct {
	t        *testing.T
	repo     string
	root     string
	cfgPath  string
	cfgText  string
	git      *workspacetest.FakeGit
	vols     *workspacetest.FakeVolumes
	prov     *workspacetest.FakeProvider
	dev      *harness.SimDevDrive
	register bool // register the project in the store
	noConfig bool // do not write the config file
	// workspaces, when set, replaces the Manager.
	workspaces TaskWorkspaces
	step       time.Duration
}

func newTaskEnv(t *testing.T) *taskEnv {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	e := &taskEnv{
		t:        t,
		repo:     filepath.Join(base, "repo"),
		root:     filepath.Join(base, "ws"),
		git:      &workspacetest.FakeGit{Commits: map[string]string{"develop": taskDevelopSHA, "main": taskMainSHA}},
		vols:     workspacetest.NewFakeVolumes(platform.Volume{Filesystem: "ext4", ID: "vol-base", MountPath: base}),
		prov:     &workspacetest.FakeProvider{ProviderName: workspace.ProviderGitWorktree, Caps: workspace.Capabilities{Filesystem: "ext4", SparseSupported: true}},
		dev:      &harness.SimDevDrive{},
		register: true,
		step:     840 * time.Millisecond,
	}
	require.NoError(t, os.MkdirAll(filepath.Join(e.repo, config.DirName), 0o750))
	require.NoError(t, os.MkdirAll(e.root, 0o750))
	e.cfgPath = filepath.Join(e.repo, config.DirName, config.FileName)
	e.cfgText = fmt.Sprintf("[workspace]\nroot = %q\n\n[conventions]\npreset = \"gitflow\"\n", filepath.ToSlash(e.root))
	return e
}

// writeConfig writes the config file; tests that edit cfgText call it again.
func (e *taskEnv) writeConfig() {
	e.t.Helper()
	if e.noConfig {
		return
	}
	require.NoError(e.t, os.WriteFile(e.cfgPath, []byte(e.cfgText), 0o600))
}

func (e *taskEnv) dbPath() string {
	return filepath.Join(filepath.Dir(e.cfgPath), "state", "cowflow.db")
}

// openStore opens the database the command used.
func (e *taskEnv) openStore() *store.Store {
	e.t.Helper()
	require.NoError(e.t, os.MkdirAll(filepath.Dir(e.dbPath()), 0o750))
	s, err := store.Open(context.Background(), e.dbPath(), store.Options{})
	require.NoError(e.t, err)
	e.t.Cleanup(func() { _ = s.Close() })
	return s
}

func (e *taskEnv) canonRepo() string {
	e.t.Helper()
	canon, err := platform.Canonical(e.repo)
	require.NoError(e.t, err)
	return canon
}

func (e *taskEnv) deps() Deps {
	e.writeConfig()
	if e.register {
		s := e.openStore()
		canon, err := platform.Canonical(e.repo)
		require.NoError(e.t, err)
		require.NoError(e.t, s.WithTx(context.Background(), "", func(tx *store.Tx) error {
			_, err := tx.InsertProject("demo", canon)
			return err
		}))
		require.NoError(e.t, s.Close())
	}
	tick := time.Unix(1_700_000_000, 0)
	calls := 0
	return Deps{
		Version:    "dev",
		ConfigPath: func() (string, error) { return e.cfgPath, nil },
		Now: func() time.Time {
			calls++
			if calls == 1 {
				return tick
			}
			return tick.Add(e.step)
		},
		OpenTasks: func(ctx context.Context, cfg *config.Config, cfgPath string) (TaskBackend, error) {
			if err := os.MkdirAll(config.StatePath(cfgPath, cfg), 0o750); err != nil {
				return TaskBackend{}, err
			}
			s, err := store.Open(ctx, config.DBPath(cfgPath, cfg), store.Options{})
			if err != nil {
				return TaskBackend{}, err
			}
			m := workspace.NewManager(workspace.Deps{
				Config: cfg, Store: s, Git: e.git, Volumes: e.vols, DevDrive: e.dev,
				Worktree: e.prov, Logger: slog.New(slog.DiscardHandler),
			})
			var ws TaskWorkspaces = m
			if e.workspaces != nil {
				ws = e.workspaces
			}
			return TaskBackend{Store: s, Workspaces: ws, Repo: e.git, Close: s.Close}, nil
		},
	}
}

func (e *taskEnv) run(args ...string) result {
	e.t.Helper()
	var out, errb bytes.Buffer
	ios := IOStreams{In: strings.NewReader(""), Out: &out, Err: &errb}
	code := Execute(context.Background(), append([]string{"task", "create"}, args...), ios, e.deps())
	return result{code: code, stdout: out.String(), stderr: errb.String()}
}

// failingWorkspaces is a TaskWorkspaces that always fails with err.
type failingWorkspaces struct{ err error }

func (f failingWorkspaces) Create(context.Context, workspace.CreateRequest) (*workspace.Workspace, workspace.Selection, error) {
	return nil, workspace.Selection{}, f.err
}

func (failingWorkspaces) List(context.Context, workspace.ListFilter) ([]workspace.Workspace, error) {
	return nil, nil
}

// docOut is the part of the task create document the tests read back
// (taskDoc cannot be decoded: workspace.CowKind only marshals).
type docOut struct {
	Workspace struct {
		Branch  string `json:"branch"`
		BaseSHA string `json:"base_sha"`
	} `json:"workspace"`
	Warnings []string `json:"warnings"`
}

var taskIDPattern = regexp.MustCompile(`\b(tsk|wks)_[0-9A-HJKMNP-TV-Z]{26}\b`)

// normalize replaces the parts of out that differ between runs.
func (e *taskEnv) normalize(out string) string {
	out = strings.ReplaceAll(out, filepath.ToSlash(e.root), "<ROOT>")
	out = strings.ReplaceAll(out, e.root, "<ROOT>")
	return taskIDPattern.ReplaceAllString(out, "${1}_ID")
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", name)
	if *updateGolden {
		require.NoError(t, os.MkdirAll("testdata", 0o750))
		require.NoError(t, os.WriteFile(p, []byte(got), 0o600))
	}
	want, err := os.ReadFile(p) //nolint:gosec // fixed testdata path
	require.NoError(t, err)
	assert.Equal(t, string(want), got)
}

func TestTaskCreateJSONGolden(t *testing.T) {
	e := newTaskEnv(t)
	r := e.run("auth", "--json", "--paths", "src/auth,contracts", "--title", "Auth")
	require.Equal(t, ExitOK, r.code, r.stderr)
	assert.True(t, strings.HasPrefix(r.stderr, "warning: "), "only the fallback warning goes to stderr: %q", r.stderr)
	checkGolden(t, "task_create_json.golden", e.normalize(r.stdout))

	// One document, and only one.
	dec := json.NewDecoder(strings.NewReader(r.stdout))
	var doc map[string]any
	require.NoError(t, dec.Decode(&doc))
	_, err := dec.Token()
	assert.ErrorIs(t, err, io.EOF)
}

func TestTaskCreateHumanGolden(t *testing.T) {
	e := newTaskEnv(t)
	r := e.run("auth", "--paths", "src/auth", "--paths", "contracts")
	require.Equal(t, ExitOK, r.code, r.stderr)
	checkGolden(t, "task_create_human.golden", e.normalize(r.stdout))
}

func TestTaskCreateDaemonless(t *testing.T) {
	e := newTaskEnv(t)
	d := e.deps()
	var out, errb bytes.Buffer
	code := Execute(context.Background(), []string{"task", "create", "auth", "--kind", "bugfix", "--paths", "src/auth", "--json"},
		IOStreams{In: strings.NewReader(""), Out: &out, Err: &errb}, d)
	require.Equal(t, ExitOK, code, errb.String())

	// Nothing but the config file and the SQLite database was involved: read
	// the rows back with a fresh store.
	s := e.openStore()
	canon, err := platform.Canonical(e.repo)
	require.NoError(t, err)
	proj, err := s.ProjectByRoot(context.Background(), canon)
	require.NoError(t, err)
	task, err := s.GetTaskBySlug(context.Background(), proj.ID, "auth")
	require.NoError(t, err)
	assert.Equal(t, "bugfix/auth", task.Branch)
	assert.Equal(t, "bugfix", task.Kind)
	assert.Equal(t, "develop", task.Base)
	assert.Equal(t, taskDevelopSHA, task.BaseSHA)
	assert.Equal(t, []string{"src/auth"}, task.Paths)
	assert.True(t, task.Sparse)

	rows, err := s.ListWorkspaces(context.Background(), domain.WorkspaceFilter{TaskID: task.ID})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, domain.WorkspaceReady, rows[0].State)
	assert.Equal(t, filepath.Join(e.root, "auth"), rows[0].Path)
	assert.Equal(t, 40000, rows[0].PortStart)
	assert.Equal(t, 40009, rows[0].PortEnd)
}

func TestTaskCreateSparseFalse(t *testing.T) {
	e := newTaskEnv(t)
	r := e.run("auth", "--paths", "src/auth", "--sparse=false")
	require.Equal(t, ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "sparse     off")
	s := e.openStore()
	canon, err := platform.Canonical(e.repo)
	require.NoError(t, err)
	proj, err := s.ProjectByRoot(context.Background(), canon)
	require.NoError(t, err)
	task, err := s.GetTaskBySlug(context.Background(), proj.ID, "auth")
	require.NoError(t, err)
	assert.False(t, task.Sparse)
}

func TestTaskCreateFallbackWarningOnStderr(t *testing.T) {
	for _, asJSON := range []bool{false, true} {
		t.Run(fmt.Sprintf("json=%v", asJSON), func(t *testing.T) {
			e := newTaskEnv(t)
			// With no cow_clone provider, auto falls back and warns.
			args := []string{"auth"}
			if asJSON {
				args = append(args, "--json")
			}
			r := e.run(args...)
			require.Equal(t, ExitOK, r.code, r.stderr)
			assert.Contains(t, r.stderr, "warning: ")
			assert.Contains(t, r.stderr, "no copy-on-write")
			assert.NotContains(t, r.stdout, "warning: ")
			if asJSON {
				var doc docOut
				require.NoError(t, json.Unmarshal([]byte(r.stdout), &doc))
				require.Len(t, doc.Warnings, 1)
				assert.Contains(t, doc.Warnings[0], "no copy-on-write")
			}
		})
	}
}

func TestTaskCreatePendingMountExit2(t *testing.T) {
	e := newTaskEnv(t)
	e.cfgText += fmt.Sprintf("\n[devdrive]\nenabled = true\npath = %q\nmount = \"X:\"\nsize_gb = 50\n",
		filepath.ToSlash(filepath.Join(filepath.Dir(e.root), "dd.vhdx")))
	r := e.run("auth")
	assert.Equal(t, ExitState, r.code, r.stderr)
	assert.Contains(t, r.stderr, "volume not mounted")
	assert.Empty(t, r.stdout)
}

func TestTaskCreateUnimplementedFlagsExit1(t *testing.T) {
	for _, args := range [][]string{
		{"--contract", "c.yaml"},
		{"--depends", "api"},
		{"--runner", "claude"},
		{"--watch", "main"},
		{"--no-contract"},
	} {
		t.Run(args[0], func(t *testing.T) {
			e := newTaskEnv(t)
			r := e.run(append([]string{"auth"}, args...)...)
			assert.Equal(t, ExitUsage, r.code)
			assert.Contains(t, r.stderr, "not available yet")
			assert.Empty(t, r.stdout)
		})
	}
}

func TestTaskCreateExitCodes(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		setup func(e *taskEnv)
		want  int
		msg   string
	}{
		{name: "invalid slug", args: []string{"Bad_Slug"}, want: ExitUsage},
		{name: "reserved device slug", args: []string{"con"}, want: ExitUsage},
		{name: "bad paths: parent", args: []string{"auth", "--paths", "../x"}, want: ExitUsage},
		{name: "bad paths: absolute", args: []string{"auth", "--paths", "/etc"}, want: ExitUsage},
		{name: "bad paths: drive", args: []string{"auth", "--paths", `C:\x`}, want: ExitUsage},
		{name: "bad paths: dot", args: []string{"auth", "--paths", "."}, want: ExitUsage},
		{name: "bad paths: empty", args: []string{"auth", "--paths", "a,,b"}, want: ExitUsage},
		{name: "unknown base", args: []string{"auth", "--base", "nope", "--force"}, want: ExitUsage},
		{name: "project not initialized", args: []string{"auth"}, want: ExitState,
			setup: func(e *taskEnv) { e.register = false }, msg: "not initialized"},
		{name: "config missing", args: []string{"auth"}, want: ExitState,
			setup: func(e *taskEnv) { e.noConfig, e.register = true, false }},
		{name: "workspace root not set", args: []string{"auth"}, want: ExitState,
			setup: func(e *taskEnv) { e.cfgText = "[conventions]\npreset = \"gitflow\"\n" }, msg: "workspace.root"},
		{name: "branch exists", args: []string{"auth"}, want: ExitConflict,
			setup: func(e *taskEnv) { e.git.Branches = map[string]string{"feature/auth": taskDevelopSHA} }},
		{name: "kind outside gitflow", args: []string{"auth", "--kind", "feature"}, want: ExitUsage,
			setup: func(e *taskEnv) { e.cfgText = strings.Replace(e.cfgText, `preset = "gitflow"`, `preset = "trunk"`, 1) }},
		{name: "non-conforming kind", args: []string{"auth", "--kind", "bogus"}, want: ExitPolicy},
		{name: "base other than the kind's without force", args: []string{"auth", "--base", "main"}, want: ExitPolicy,
			msg: "--force"},
		{name: "hotfix off develop without force", args: []string{"auth", "--kind", "hotfix", "--base", "develop"}, want: ExitPolicy},
		{name: "path exists", args: []string{"auth"}, want: ExitConflict,
			setup: func(e *taskEnv) { e.workspaces = failingWorkspaces{workspace.ErrPathExists} }},
		{name: "protected branch", args: []string{"auth"}, want: ExitPolicy,
			setup: func(e *taskEnv) { e.workspaces = failingWorkspaces{workspace.ErrProtectedBranch} }},
		{name: "branch exists in manager", args: []string{"auth"}, want: ExitConflict,
			setup: func(e *taskEnv) { e.workspaces = failingWorkspaces{fmt.Errorf("plan: %w", workspace.ErrBranchExists)} }},
		{name: "clone unsupported", args: []string{"auth"}, want: ExitState,
			setup: func(e *taskEnv) { e.workspaces = failingWorkspaces{workspace.ErrCloneUnsupported} }},
		{name: "provider unavailable", args: []string{"auth"}, want: ExitState,
			setup: func(e *taskEnv) { e.workspaces = failingWorkspaces{workspace.ErrUnsupportedProvider} }},
		{name: "invalid slug from manager", args: []string{"auth"}, want: ExitUsage,
			setup: func(e *taskEnv) { e.workspaces = failingWorkspaces{domain.ErrInvalidSlug} }},
		{name: "unclassified manager error", args: []string{"auth"}, want: ExitState,
			setup: func(e *taskEnv) { e.workspaces = failingWorkspaces{errors.New("boom")} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTaskEnv(t)
			if tt.setup != nil {
				tt.setup(e)
			}
			r := e.run(tt.args...)
			assert.Equal(t, tt.want, r.code, r.stderr)
			assert.Empty(t, r.stdout)
			assert.NotEmpty(t, r.stderr)
			if tt.msg != "" {
				assert.Contains(t, r.stderr, tt.msg)
			}
		})
	}
}

func TestTaskCreateSlugExistsExit3(t *testing.T) {
	e := newTaskEnv(t)
	require.Equal(t, ExitOK, e.run("auth").code)
	e.register = false // already registered by the first run
	r := e.run("auth")
	assert.Equal(t, ExitConflict, r.code, r.stderr)
	assert.Contains(t, r.stderr, "already exists")
}

// A Manager failure after CreateTask leaves the task row; the same command
// line must work again once the cause is gone.
func TestTaskCreateRetryAfterFailedWorkspace(t *testing.T) {
	e := newTaskEnv(t)
	e.prov.CreateErr = errors.New("disk on fire")
	first := e.run("auth", "--paths", "src/auth")
	require.NotEqual(t, ExitOK, first.code)
	assert.Contains(t, first.stderr, "disk on fire")

	e.prov.CreateErr = nil
	e.register = false // registered by the first run
	second := e.run("auth", "--paths", "src/auth", "--json")
	require.Equal(t, ExitOK, second.code, second.stderr)

	s := e.openStore()
	proj, err := s.ProjectByRoot(context.Background(), e.canonRepo())
	require.NoError(t, err)
	task, err := s.GetTaskBySlug(context.Background(), proj.ID, "auth")
	require.NoError(t, err)
	rows, err := s.ListWorkspaces(context.Background(), domain.WorkspaceFilter{TaskID: task.ID})
	require.NoError(t, err)
	states := map[domain.WorkspaceState]int{}
	for _, r := range rows {
		states[r.State]++
	}
	assert.Equal(t, map[domain.WorkspaceState]int{domain.WorkspaceFailed: 1, domain.WorkspaceReady: 1}, states,
		"one task, the failed attempt and the retry")
}

// A leftover task is reused only for the same request; with other options it
// stays a conflict rather than silently using the stored paths.
func TestTaskCreateRetryWithOtherOptionsConflicts(t *testing.T) {
	e := newTaskEnv(t)
	e.prov.CreateErr = errors.New("disk on fire")
	require.NotEqual(t, ExitOK, e.run("auth", "--paths", "src/auth").code)

	e.prov.CreateErr = nil
	e.register = false
	r := e.run("auth", "--paths", "src/other")
	assert.Equal(t, ExitConflict, r.code, r.stderr)
	assert.Contains(t, r.stderr, "earlier failed create")
}

func TestTaskCreateForceBase(t *testing.T) {
	e := newTaskEnv(t)
	r := e.run("auth", "--base", "main", "--force", "--json")
	require.Equal(t, ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stderr, "--force: base \"main\" instead of \"develop\"")
	var doc docOut
	require.NoError(t, json.Unmarshal([]byte(r.stdout), &doc))
	assert.Equal(t, taskMainSHA, doc.Workspace.BaseSHA)
	assert.NotEmpty(t, doc.Warnings)
}

func TestTaskCreateHotfixDefaultsToMain(t *testing.T) {
	e := newTaskEnv(t)
	r := e.run("auth", "--kind", "hotfix", "--json")
	require.Equal(t, ExitOK, r.code, r.stderr)
	var doc docOut
	require.NoError(t, json.Unmarshal([]byte(r.stdout), &doc))
	assert.Equal(t, taskMainSHA, doc.Workspace.BaseSHA)
	assert.Equal(t, "hotfix/auth", doc.Workspace.Branch)
}

func TestTaskCreateTrunkBranch(t *testing.T) {
	e := newTaskEnv(t)
	e.cfgText = strings.Replace(e.cfgText, `preset = "gitflow"`, `preset = "trunk"`, 1)
	r := e.run("auth", "--base", "main", "--json")
	require.Equal(t, ExitOK, r.code, r.stderr)
	var doc docOut
	require.NoError(t, json.Unmarshal([]byte(r.stdout), &doc))
	assert.Equal(t, "cowflow/demo/auth", doc.Workspace.Branch)
}

func TestNormalizePaths(t *testing.T) {
	got, err := normalizePaths([]string{`src\auth`, "src/auth/", "./contracts", " docs "})
	require.NoError(t, err)
	assert.Equal(t, []string{"src/auth", "contracts", "docs"}, got)

	got, err = normalizePaths(nil)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestTaskCreateUnavailableWithoutBackend(t *testing.T) {
	e := newTaskEnv(t)
	d := e.deps()
	d.OpenTasks = nil
	var out, errb bytes.Buffer
	code := Execute(context.Background(), []string{"task", "create", "auth"},
		IOStreams{In: strings.NewReader(""), Out: &out, Err: &errb}, d)
	assert.Equal(t, exitUnsupported, code)
}
