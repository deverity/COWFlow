package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
)

// fakeOpenStore answers the project and task lookups of open and golden.
type fakeOpenStore struct {
	root    string
	tasks   map[string]domain.Task
	project domain.Project
}

func (f *fakeOpenStore) ProjectByRoot(_ context.Context, root string) (domain.Project, error) {
	if root != f.root {
		return domain.Project{}, store.ErrNotFound
	}
	return f.project, nil
}

func (f *fakeOpenStore) CreateTask(context.Context, domain.Task) (domain.Task, error) {
	return domain.Task{}, errors.New("unused")
}

func (f *fakeOpenStore) DeleteTask(context.Context, string, string) error {
	return errors.New("unused")
}

func (f *fakeOpenStore) GetTaskBySlug(_ context.Context, _, slug string) (domain.Task, error) {
	t, ok := f.tasks[slug]
	if !ok {
		return domain.Task{}, fmt.Errorf("%w: task %q", store.ErrNotFound, slug)
	}
	return t, nil
}

type fakeLister struct {
	list   []workspace.Workspace
	filter workspace.ListFilter
}

func (f *fakeLister) List(_ context.Context, flt workspace.ListFilter) ([]workspace.Workspace, error) {
	f.filter = flt
	return f.list, nil
}

type fakeGolden struct {
	infos     []workspace.GoldenInfo
	report    workspace.GCReport
	gotBudget int64
	gotProj   string
}

func (f *fakeGolden) List(_ context.Context, p string) ([]workspace.GoldenInfo, error) {
	f.gotProj = p
	return f.infos, nil
}

func (f *fakeGolden) GC(_ context.Context, p string, budget int64) (workspace.GCReport, error) {
	f.gotProj, f.gotBudget = p, budget
	return f.report, nil
}

// openEnv is a repository with a config file and fake backends.
type openEnv struct {
	t       *testing.T
	cfgPath string
	store   *fakeOpenStore
	lister  *fakeLister
	golden  *fakeGolden
	editor  EditorSeams
}

func newOpenEnv(t *testing.T) *openEnv {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	repo := filepath.Join(base, "repo")
	require.NoError(t, os.MkdirAll(filepath.Join(repo, config.DirName), 0o750))
	cfgPath := filepath.Join(repo, config.DirName, config.FileName)
	text := fmt.Sprintf("[workspace]\nroot = %q\n\n[scheduler]\ngolden_cache_gb = 2\n", filepath.ToSlash(filepath.Join(base, "ws")))
	require.NoError(t, os.WriteFile(cfgPath, []byte(text), 0o600))
	canon, err := platform.Canonical(repo)
	require.NoError(t, err)
	return &openEnv{
		t: t, cfgPath: cfgPath,
		store: &fakeOpenStore{
			root:    canon,
			project: domain.Project{ID: "prj_X", Name: "demo", RepoRoot: canon},
			tasks:   map[string]domain.Task{"auth": {ID: "tsk_AUTH", Slug: "auth"}},
		},
		lister: &fakeLister{},
		golden: &fakeGolden{},
	}
}

func (e *openEnv) run(args ...string) result {
	e.t.Helper()
	deps := Deps{
		ConfigPath: func() (string, error) { return e.cfgPath, nil },
		Editor:     e.editor,
		OpenTasks: func(context.Context, *config.Config, string) (TaskBackend, error) {
			return TaskBackend{Store: e.store, Tasks: e.store, Lister: e.lister, Golden: e.golden}, nil
		},
	}
	var out, errb bytes.Buffer
	ios := IOStreams{In: strings.NewReader(""), Out: &out, Err: &errb}
	code := Execute(context.Background(), args, ios, deps)
	return result{code: code, stdout: out.String(), stderr: errb.String()}
}

func readyWorkspaces(paths ...string) []workspace.Workspace {
	var out []workspace.Workspace
	for i, p := range paths {
		out = append(out, workspace.Workspace{
			ID: fmt.Sprintf("wks_%d", i), Path: p, State: domain.WorkspaceReady,
			CreatedAt: time.Unix(1_700_000_000+int64(i), 0),
		})
	}
	return out
}

func TestOpenPrintsPathWhenNoEditor(t *testing.T) {
	e := newOpenEnv(t)
	e.lister.list = readyWorkspaces("/ws/old", "/ws/auth")
	e.editor = EditorSeams{
		LookPath: func(string) (string, error) { return "", errors.New("not found") },
		Start:    func(context.Context, []string) error { t.Fatal("editor started"); return nil },
	}
	r := e.run("open", "auth")
	require.Equal(t, ExitOK, r.code, r.stderr)
	assert.Equal(t, "/ws/auth\n", r.stdout, "the most recent workspace")
	assert.Contains(t, r.stderr, "install the VS Code 'code' command")
	assert.Equal(t, "tsk_AUTH", e.lister.filter.TaskID)
	assert.Equal(t, []domain.WorkspaceState{domain.WorkspaceReady}, e.lister.filter.States)
}

func TestOpenJSONPrintsPathOnly(t *testing.T) {
	e := newOpenEnv(t)
	e.lister.list = readyWorkspaces("/ws/auth")
	e.editor = EditorSeams{
		LookPath: func(string) (string, error) { t.Fatal("looked up the editor"); return "", nil },
	}
	r := e.run("open", "auth", "--json")
	require.Equal(t, ExitOK, r.code, r.stderr)
	assert.JSONEq(t, `{"path":"/ws/auth"}`, r.stdout)
}

func TestOpenArgvNoShell(t *testing.T) {
	e := newOpenEnv(t)
	hostile := `/ws/a b;rm -rf $HOME&&"x"`
	e.lister.list = readyWorkspaces(hostile)
	var argv []string
	e.editor = EditorSeams{
		GOOS:     "linux",
		LookPath: func(string) (string, error) { return "/usr/bin/code", nil },
		Start:    func(_ context.Context, a []string) error { argv = a; return nil },
	}
	r := e.run("open", "auth")
	require.Equal(t, ExitOK, r.code, r.stderr)
	assert.Equal(t, []string{"/usr/bin/code", hostile}, argv, "one argv element per argument, no shell string")
}

func TestOpenRejectsUnsafePathForCmdShim(t *testing.T) {
	for _, p := range []string{
		`X:\ws\a b`, `X:\ws\a&calc`, `X:\ws\a^b`, `X:\ws\%PATH%`, `\\srv\share\a`,
		`X:ws\a`, `X:\ws\..\..\x`, `X:\ws\a"b`, "X:\\ws\\a\nb", `relative\a`,
	} {
		e := newOpenEnv(t)
		e.lister.list = readyWorkspaces(p)
		started := false
		e.editor = EditorSeams{
			GOOS:     "windows",
			LookPath: func(string) (string, error) { return `C:\vs\bin\code.cmd`, nil },
			Start:    func(context.Context, []string) error { started = true; return nil },
		}
		r := e.run("open", "auth")
		assert.Equal(t, ExitPolicy, r.code, "%q", p)
		assert.False(t, started, "%q", p)
		assert.Contains(t, r.stderr, "open it by hand", "%q", p)
	}

	e := newOpenEnv(t)
	e.lister.list = readyWorkspaces(`X:\ws\auth-api_v1.2`)
	var argv []string
	e.editor = EditorSeams{
		GOOS:     "windows",
		LookPath: func(string) (string, error) { return `C:\vs\bin\code.cmd`, nil },
		Start:    func(_ context.Context, a []string) error { argv = a; return nil },
	}
	r := e.run("open", "auth")
	require.Equal(t, ExitOK, r.code, r.stderr)
	assert.Equal(t, []string{`C:\vs\bin\code.cmd`, `X:\ws\auth-api_v1.2`}, argv)
}

func TestOpenErrors(t *testing.T) {
	e := newOpenEnv(t)
	r := e.run("open", "nope")
	assert.Equal(t, ExitState, r.code)
	assert.Contains(t, r.stderr, "not found")

	r = e.run("open", "auth") // no ready workspace
	assert.Equal(t, ExitState, r.code)
	assert.Contains(t, r.stderr, "no ready workspace")

	r = e.run("open", "Bad Slug")
	assert.Equal(t, ExitUsage, r.code)

	// A Unix editor and path: pin GOOS so the Windows cmd.exe shim check
	// does not refuse "/ws/auth" before the start failure is reached.
	e.editor = EditorSeams{
		GOOS:     "linux",
		LookPath: func(string) (string, error) { return "/usr/bin/code", nil },
		Start:    func(context.Context, []string) error { return errors.New("boom") },
	}
	e.lister.list = readyWorkspaces("/ws/auth")
	r = e.run("open", "auth")
	assert.Equal(t, ExitState, r.code)
}
