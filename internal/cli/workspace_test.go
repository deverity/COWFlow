package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/config"
	"github.com/deverity/cowflow/internal/domain"
	"github.com/deverity/cowflow/internal/workspace"
	"github.com/deverity/cowflow/internal/workspace/workspacetest"
)

// wsEnv is an openEnv whose workspaces live in a FakeManager.
type wsEnv struct {
	*openEnv
	mgr *workspacetest.FakeManager
}

const (
	wsID1 = "wks_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	wsID2 = "wks_01ARZ3NDEKTSV4RRFFQ69G5FAW"
	wsID3 = "wks_01ARZ3NDEKTSV4RRFFQ69G5FAX"
)

type seqIDs struct {
	ids []string
	n   int
}

func (s *seqIDs) New(domain.Kind) (string, error) {
	id := s.ids[s.n]
	s.n++
	return id, nil
}

func newWSEnv(t *testing.T) *wsEnv {
	t.Helper()
	e := &wsEnv{openEnv: newOpenEnv(t)}
	e.mgr = &workspacetest.FakeManager{
		IDs: &seqIDs{ids: []string{wsID1, wsID2, wsID3}},
		Provider: &workspacetest.FakeProvider{
			ProviderName: "cow_clone",
			Caps:         workspace.Capabilities{CopyOnWrite: workspace.CowBlock, Filesystem: "refs"},
		},
	}
	return e
}

func (e *wsEnv) create(op, slug string, paths ...string) {
	e.t.Helper()
	_, _, err := e.mgr.Create(context.Background(), workspace.CreateRequest{
		OpID: op, ProjectID: "prj_X", Provider: "cow_clone",
		Task: domain.Task{ID: "tsk_" + slug, Slug: slug, Branch: "feature/" + slug, BaseSHA: "3f2c9e1a", Paths: paths, Sparse: len(paths) > 0},
	})
	require.NoError(e.t, err)
}

func (e *wsEnv) run(args ...string) result {
	e.t.Helper()
	deps := Deps{
		ConfigPath: func() (string, error) { return e.cfgPath, nil },
		OpenTasks: func(context.Context, *config.Config, string) (TaskBackend, error) {
			return TaskBackend{Store: e.store, Lister: e.mgr, Admin: e.mgr}, nil
		},
	}
	var out, errb bytes.Buffer
	ios := IOStreams{In: strings.NewReader(""), Out: &out, Err: &errb}
	code := Execute(context.Background(), args, ios, deps)
	return result{code: code, stdout: out.String(), stderr: errb.String()}
}

func TestWorkspaceListGolden(t *testing.T) {
	e := newWSEnv(t)
	e.create("op1", "auth")
	e.create("op2", "billing")
	e.create("op3", "gone")
	require.NoError(t, e.mgr.Provider.SetState(wsID3, domain.WorkspaceDestroyed))
	require.NoError(t, e.mgr.Provider.SetState(wsID2, domain.WorkspacePendingCleanup))

	r := e.run("workspace", "list")
	require.Equal(t, ExitOK, r.code, r.stderr)
	checkGolden(t, "workspace_list_human.golden", r.stdout)
	assert.NotContains(t, r.stdout, wsID3, "destroyed workspaces are hidden")

	r = e.run("workspace", "list", "--json")
	require.Equal(t, ExitOK, r.code, r.stderr)
	checkGolden(t, "workspace_list_json.golden", r.stdout)

	assert.Equal(t, ExitUsage, e.run("workspace", "list", "--force").code, "no flags beyond §14")
}

func TestWorkspaceListEmpty(t *testing.T) {
	e := newWSEnv(t)
	r := e.run("workspace", "list", "--json")
	require.Equal(t, ExitOK, r.code, r.stderr)
	assert.JSONEq(t, `[]`, r.stdout)
	r = e.run("workspace", "list")
	assert.Equal(t, "no workspaces\n", r.stdout)
}

func TestWorkspaceInspectJSONHasFilesystemAndCowKind(t *testing.T) {
	e := newWSEnv(t)
	e.create("op1", "auth", "src/auth", "contracts")
	require.NoError(t, e.mgr.Provider.SetWarnings(wsID1, `filesystem is "ntfs" now, recorded "refs" at create`))

	r := e.run("workspace", "inspect", wsID1, "--json")
	require.Equal(t, ExitOK, r.code, r.stderr)
	assert.Empty(t, r.stderr, "JSON mode keeps warnings in the document")
	checkGolden(t, "workspace_inspect_json.golden", r.stdout)

	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(r.stdout), &doc))
	assert.Equal(t, "refs", doc["filesystem"])
	assert.Equal(t, "block", doc["cow_kind"])
	assert.Contains(t, doc, "logs_dir")
}

func TestWorkspaceInspectHumanWarningsToStderr(t *testing.T) {
	e := newWSEnv(t)
	e.create("op1", "auth")
	require.NoError(t, e.mgr.Provider.SetWarnings(wsID1, "workspace path /x is missing"))

	r := e.run("workspace", "inspect", wsID1)
	require.Equal(t, ExitOK, r.code, r.stderr)
	assert.Regexp(t, `filesystem:\s+refs`, r.stdout)
	assert.Regexp(t, `cow:\s+block`, r.stdout)
	assert.NotContains(t, r.stdout, "missing")
	assert.Equal(t, "warning: workspace path /x is missing\n", r.stderr)
}

func TestWorkspaceInspectPendingMountShowsStatusAndExits2(t *testing.T) {
	e := newWSEnv(t)
	e.create("op1", "auth")
	e.mgr.Provider.InspectErr = nil
	adm := pendingMountAdmin{inner: e.mgr}
	deps := Deps{
		ConfigPath: func() (string, error) { return e.cfgPath, nil },
		OpenTasks: func(context.Context, *config.Config, string) (TaskBackend, error) {
			return TaskBackend{Store: e.store, Lister: e.mgr, Admin: adm}, nil
		},
	}
	var out, errb bytes.Buffer
	code := Execute(context.Background(), []string{"workspace", "inspect", wsID1},
		IOStreams{In: strings.NewReader(""), Out: &out, Err: &errb}, deps)
	assert.Equal(t, ExitState, code)
	assert.Regexp(t, `state:\s+pending_mount`, out.String())
}

type pendingMountAdmin struct{ inner *workspacetest.FakeManager }

func (p pendingMountAdmin) Inspect(ctx context.Context, id string) (*workspace.WorkspaceStatus, error) {
	st, err := p.inner.Inspect(ctx, id)
	if err != nil {
		return nil, err
	}
	st.State = domain.WorkspacePendingMount
	return st, fmt.Errorf("inspect workspace %s: %w", id, workspace.ErrPendingMount)
}

func (p pendingMountAdmin) DestroyWithResult(context.Context, string, workspace.DestroyOptions) (workspace.DestroyResult, error) {
	return workspace.DestroyResult{}, nil
}

func TestWorkspaceDestroyRefusesWithoutForce(t *testing.T) {
	e := newWSEnv(t)
	e.create("op1", "auth")
	var files []string
	for i := range 12 {
		files = append(files, fmt.Sprintf("src/file%02d.go", i))
	}
	files[0] = "with space.go"
	require.NoError(t, e.mgr.Provider.SetDirty(wsID1, workspace.DirtyReport{
		Uncommitted: files, Unpublished: []string{"abc1234"},
	}))

	r := e.run("workspace", "destroy", wsID1)
	assert.Equal(t, ExitPolicy, r.code, "U-API-12: unpreserved work is exit 4")
	assert.Empty(t, r.stdout)
	assert.Contains(t, r.stderr, "12 uncommitted file(s):")
	assert.Contains(t, r.stderr, `"with space.go"`)
	assert.Contains(t, r.stderr, `"src/file09.go"`)
	assert.NotContains(t, r.stderr, "src/file10.go", "truncated after 10")
	assert.Contains(t, r.stderr, "... and 2 more")
	assert.Contains(t, r.stderr, "1 unpublished commit(s):")
	assert.Contains(t, r.stderr, `"abc1234"`)
	assert.Contains(t, r.stderr, "use --force to rescue and destroy")

	st, err := e.mgr.Inspect(context.Background(), wsID1)
	require.NoError(t, err)
	assert.Equal(t, domain.WorkspaceReady, st.State, "nothing was destroyed")
}

func TestWorkspaceDestroyForceReportsRescue(t *testing.T) {
	e := newWSEnv(t)
	e.create("op1", "auth")
	require.NoError(t, e.mgr.Provider.SetDirty(wsID1, workspace.DirtyReport{Uncommitted: []string{"a.go"}}))

	r := e.run("workspace", "destroy", wsID1, "--force")
	require.Equal(t, ExitOK, r.code, r.stderr)
	ref := "refs/cowflow/rescue/" + wsID1
	assert.Contains(t, r.stdout, "rescued to "+ref+" (restore with: git cherry-pick -n "+ref+")")
	assert.Contains(t, r.stdout, "destroyed workspace "+wsID1)

	e2 := newWSEnv(t)
	e2.create("op1", "auth")
	r = e2.run("workspace", "destroy", wsID1, "--json")
	require.Equal(t, ExitOK, r.code, r.stderr)
	assert.JSONEq(t, `{"id":"`+wsID1+`","destroyed":true}`, r.stdout, "a clean destroy rescues nothing")
}

func TestWorkspaceDestroyPendingCleanupExit2(t *testing.T) {
	e := newWSEnv(t)
	e.create("op1", "auth")
	e.mgr.Provider.DestroyErr = fmt.Errorf("destroy workspace %s: %w: X:\\ws\\auth is in use (ERROR_SHARING_VIOLATION): close editors/terminals using this folder",
		wsID1, workspace.ErrPendingCleanup)
	r := e.run("workspace", "destroy", wsID1)
	assert.Equal(t, ExitState, r.code)
	assert.Contains(t, r.stderr, "ERROR_SHARING_VIOLATION")
	assert.Contains(t, r.stderr, "close editors/terminals")
}

func TestWorkspaceDestroyRescueFailureExit2(t *testing.T) {
	e := newWSEnv(t)
	e.create("op1", "auth")
	e.mgr.Provider.DestroyErr = fmt.Errorf("destroy workspace %s: %w: disk full", wsID1, workspace.ErrRescueFailed)
	r := e.run("workspace", "destroy", wsID1, "--force")
	assert.Equal(t, ExitState, r.code)
	assert.Contains(t, r.stderr, "rescue failed")
}

func TestWorkspaceDestroyUnknownIDExit2(t *testing.T) {
	e := newWSEnv(t)
	for _, args := range [][]string{
		{"workspace", "destroy", wsID2},
		{"workspace", "inspect", wsID2},
	} {
		r := e.run(args...)
		assert.Equal(t, ExitState, r.code, args)
		assert.Contains(t, r.stderr, "not found", args)
	}
}

func TestWorkspaceBadIDIsUsageError(t *testing.T) {
	e := newWSEnv(t)
	for _, id := range []string{"nope", "../../etc", "tsk_01ARZ3NDEKTSV4RRFFQ69G5FAV"} {
		assert.Equal(t, ExitUsage, e.run("workspace", "destroy", id).code, id)
		assert.Equal(t, ExitUsage, e.run("workspace", "inspect", id).code, id)
	}
}

func TestWorkspaceDestroyNeverPrompts(t *testing.T) {
	e := newWSEnv(t)
	e.create("op1", "auth")
	r := e.run("workspace", "destroy", wsID1) // stdin is empty: a prompt would fail
	require.Equal(t, ExitOK, r.code, r.stderr)
	assert.NotContains(t, r.stdout, "?")
}

func TestWorkspaceDestroyHelpMentionsIgnoredFiles(t *testing.T) {
	e := newWSEnv(t)
	r := e.run("workspace", "destroy", "--help")
	require.Equal(t, ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "ignored files are not preserved")
}

// rescuingAdmin destroys like a stuck handle after a rescue: pending_cleanup
// together with the rescue ref; it records the options it was called with.
type rescuingAdmin struct {
	inner *workspacetest.FakeManager
	opts  workspace.DestroyOptions
}

func (r *rescuingAdmin) Inspect(ctx context.Context, id string) (*workspace.WorkspaceStatus, error) {
	return r.inner.Inspect(ctx, id)
}

func (r *rescuingAdmin) DestroyWithResult(_ context.Context, id string, opts workspace.DestroyOptions) (workspace.DestroyResult, error) {
	r.opts = opts
	return workspace.DestroyResult{RescueRef: workspace.RescueRef(id)},
		fmt.Errorf("destroy workspace %s: %w: in use", id, workspace.ErrPendingCleanup)
}

func TestWorkspaceDestroyPendingCleanupStillReportsRescue(t *testing.T) {
	e := newWSEnv(t)
	adm := &rescuingAdmin{inner: e.mgr}
	deps := Deps{
		ConfigPath: func() (string, error) { return e.cfgPath, nil },
		OpenTasks: func(context.Context, *config.Config, string) (TaskBackend, error) {
			return TaskBackend{Store: e.store, Lister: e.mgr, Admin: adm}, nil
		},
	}
	var out, errb bytes.Buffer
	code := Execute(context.Background(), []string{"workspace", "destroy", wsID1, "--force"},
		IOStreams{In: strings.NewReader(""), Out: &out, Err: &errb}, deps)
	r := result{code: code, stdout: out.String(), stderr: errb.String()}
	assert.Equal(t, ExitState, r.code)
	ref := "refs/cowflow/rescue/" + wsID1
	assert.Contains(t, r.stderr, "rescued to "+ref+" (restore with: git cherry-pick -n "+ref+")")
	assert.Contains(t, r.stderr, "cleanup pending")
	assert.Empty(t, r.stdout)
	assert.Equal(t, "user", adm.opts.Actor.Kind, "the audit actor is the OS user")
	assert.NotEmpty(t, adm.opts.Actor.ID)
	assert.True(t, adm.opts.Force)
}
