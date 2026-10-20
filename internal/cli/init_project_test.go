package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/config"
	"github.com/deverity/cowflow/internal/platform"
	"github.com/deverity/cowflow/internal/store"
)

// initRepo is a directory with a .git entry, as init expects.
func initRepo(t *testing.T, name string) (repo string, deps Deps) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	repo = filepath.Join(base, name)
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o750))
	cfgPath := filepath.Join(repo, config.DirName, config.FileName)
	return repo, Deps{Version: "dev", ConfigPath: func() (string, error) { return cfgPath, nil }}
}

func runInit(deps Deps, args ...string) result {
	var out, errb bytes.Buffer
	ios := IOStreams{In: strings.NewReader(""), Out: &out, Err: &errb}
	code := Execute(context.Background(), append([]string{"init"}, args...), ios, deps)
	return result{code: code, stdout: out.String(), stderr: errb.String()}
}

// projectsAt reads the project registered for repo from the store init made.
func projectsAt(t *testing.T, cfgPath, repo string) (id, name string) {
	t.Helper()
	cfg, err := config.Load(cfgPath)
	require.NoError(t, err)
	s, err := store.Open(context.Background(), config.DBPath(cfgPath, cfg), store.Options{})
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	canon, err := platform.Canonical(repo)
	require.NoError(t, err)
	p, err := s.ProjectByRoot(context.Background(), canon)
	require.NoError(t, err)
	return p.ID, p.Name
}

func TestInitPlainRegistersProject(t *testing.T) {
	repo, deps := initRepo(t, "demo")
	cfgPath, _ := deps.ConfigPath()

	r := runInit(deps, "--json")
	require.Equal(t, ExitOK, r.code, r.stderr)
	var doc struct {
		Project       projectView `json:"project"`
		Config        string      `json:"config"`
		ConfigCreated bool        `json:"config_created"`
		Database      string      `json:"database"`
	}
	require.NoError(t, json.Unmarshal([]byte(r.stdout), &doc), r.stdout)
	assert.True(t, doc.Project.Created)
	assert.Equal(t, "demo", doc.Project.Name)
	assert.True(t, doc.ConfigCreated)
	assert.Equal(t, cfgPath, doc.Config)
	assert.FileExists(t, doc.Database)

	want := filepath.Join(t.TempDir(), "default.toml")
	require.NoError(t, config.Save(want, config.Default()))
	wantBytes, err := os.ReadFile(want) //nolint:gosec // test path
	require.NoError(t, err)
	gotBytes, err := os.ReadFile(cfgPath) //nolint:gosec // test path
	require.NoError(t, err)
	assert.Equal(t, string(wantBytes), string(gotBytes), "the file holds config.Default()")
	id, name := projectsAt(t, cfgPath, repo)
	assert.Equal(t, doc.Project.ID, id)
	assert.Equal(t, "demo", name)
	assert.NoFileExists(t, filepath.Join(repo, ".mcp.json"), ".mcp.json is S3-E3")
}

func TestInitPlainIsIdempotent(t *testing.T) {
	repo, deps := initRepo(t, "demo")
	cfgPath, _ := deps.ConfigPath()
	first := runInit(deps)
	require.Equal(t, ExitOK, first.code, first.stderr)
	assert.Contains(t, first.stdout, "project demo registered")
	id1, _ := projectsAt(t, cfgPath, repo)
	before, err := os.ReadFile(cfgPath) //nolint:gosec // test path
	require.NoError(t, err)

	second := runInit(deps)
	require.Equal(t, ExitOK, second.code, second.stderr)
	assert.Contains(t, second.stdout, "already registered")
	assert.Contains(t, second.stdout, "(kept)")
	id2, _ := projectsAt(t, cfgPath, repo)
	assert.Equal(t, id1, id2)
	after, err := os.ReadFile(cfgPath) //nolint:gosec // test path
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestInitPlainNeverOverwritesConfig(t *testing.T) {
	repo, deps := initRepo(t, "demo")
	cfgPath, _ := deps.ConfigPath()
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0o750))
	custom := "[project]\ndefault_base = \"trunk\"\n"
	require.NoError(t, os.WriteFile(cfgPath, []byte(custom), 0o600))

	r := runInit(deps)
	require.Equal(t, ExitOK, r.code, r.stderr)
	got, err := os.ReadFile(cfgPath) //nolint:gosec // test path
	require.NoError(t, err)
	assert.Equal(t, custom, string(got))
	_, name := projectsAt(t, cfgPath, repo)
	assert.Equal(t, "demo", name)
}

func TestInitPlainErrors(t *testing.T) {
	t.Run("not a repository", func(t *testing.T) {
		_, deps := initRepo(t, "demo")
		cfgPath, _ := deps.ConfigPath()
		require.NoError(t, os.RemoveAll(filepath.Join(projectRoot(cfgPath), ".git")))
		r := runInit(deps)
		assert.Equal(t, ExitState, r.code)
		assert.Contains(t, r.stderr, "not a Git repository")
		assert.NoFileExists(t, cfgPath)
	})
	t.Run("broken config", func(t *testing.T) {
		_, deps := initRepo(t, "demo")
		cfgPath, _ := deps.ConfigPath()
		require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0o750))
		require.NoError(t, os.WriteFile(cfgPath, []byte("not = [valid"), 0o600))
		r := runInit(deps)
		assert.Equal(t, ExitUsage, r.code, r.stderr)
	})
}

func TestInitDevDriveRegistersProject(t *testing.T) {
	e := newDDEnv(t, win11)
	r := e.run(e.initArgs("--json")...)
	require.Equal(t, ExitOK, r.code, r.stderr)
	var doc struct {
		Project projectView `json:"project"`
	}
	require.NoError(t, json.Unmarshal([]byte(r.stdout), &doc), r.stdout)
	assert.True(t, doc.Project.Created)

	id, name := projectsAt(t, e.cfgPath, projectRoot(e.cfgPath))
	assert.Equal(t, doc.Project.ID, id)
	assert.Equal(t, doc.Project.Name, name)
}
