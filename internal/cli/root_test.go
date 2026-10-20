package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type result struct {
	code           int
	stdout, stderr string
}

func run(t *testing.T, args ...string) result {
	t.Helper()
	var out, errb bytes.Buffer
	ios := IOStreams{In: strings.NewReader(""), Out: &out, Err: &errb}
	code := Execute(context.Background(), args, ios, Deps{Version: "dev", Commit: "none", Date: "unknown"})
	return result{code: code, stdout: out.String(), stderr: errb.String()}
}

func TestExecute_NoArgsPrintsHelp(t *testing.T) {
	r := run(t)
	assert.Equal(t, ExitOK, r.code)
	assert.Contains(t, r.stdout, "Usage:")
	assert.NotContains(t, r.stdout, "completion")
}

func TestExecute_UsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "unknown command", args: []string{"nope"}, want: `unknown command "nope"`},
		{name: "unknown flag", args: []string{"--nope"}, want: "unknown flag: --nope"},
		{name: "completion is not a command", args: []string{"completion"}, want: `unknown command "completion"`},
		{name: "no version subcommand (U-API-13)", args: []string{"version"}, want: `unknown command "version"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := run(t, tt.args...)
			assert.Equal(t, ExitUsage, r.code)
			assert.Empty(t, r.stdout)
			assert.Contains(t, r.stderr, "cowflow: ")
			assert.Contains(t, r.stderr, tt.want)
		})
	}
}

func TestExecute_Version(t *testing.T) {
	r := run(t, "--version")
	assert.Equal(t, ExitOK, r.code)
	assert.Contains(t, r.stdout, "dev (commit none, built unknown)")
}

func TestExecute_VersionJSON(t *testing.T) {
	r := run(t, "--version", "--json")
	require.Equal(t, ExitOK, r.code, r.stderr)
	var doc map[string]string
	require.NoError(t, json.Unmarshal([]byte(r.stdout), &doc), r.stdout)
	assert.Equal(t, map[string]string{"version": "dev", "commit": "none", "date": "unknown"}, doc)
}

func TestExitCodes_UnnamedFailures(t *testing.T) {
	// SPEC-GAPS U-API-12: state failures are 2; no new numbers.
	for _, code := range []int{exitUnclassified, exitNotImplemented, exitDoctorFailed, exitNotElevated, exitUnsupported} {
		assert.Equal(t, ExitState, code)
	}
}

func TestMCP_ProjectLocatesConfig(t *testing.T) {
	failing := func() (string, error) { return "", errors.New("cwd unavailable") }
	execute := func(deps Deps, args ...string) int {
		var out, errb bytes.Buffer
		return Execute(context.Background(), args, IOStreams{In: strings.NewReader(""), Out: &out, Err: &errb}, deps)
	}

	t.Run("project given", func(t *testing.T) {
		// Located from --project, not from Deps.ConfigPath; the server
		// itself is still a stub.
		assert.Equal(t, exitNotImplemented, execute(Deps{ConfigPath: failing}, "mcp", "--project", t.TempDir()))
	})
	t.Run("no project", func(t *testing.T) {
		assert.Equal(t, ExitState, execute(Deps{ConfigPath: failing}, "mcp"))
	})
	t.Run("located path", func(t *testing.T) {
		dir := t.TempDir()
		got, err := projectConfigPath(Deps{ConfigPath: failing}, dir)
		require.NoError(t, err)
		want, err := filepath.Abs(filepath.Join(dir, ".cowflow", "config.toml"))
		require.NoError(t, err)
		assert.Equal(t, want, got)
	})
}

func TestExecute_JSONIsPersistent(t *testing.T) {
	root := NewRootCmd(IOStreams{}, Deps{}, &runState{})
	assert.NotNil(t, root.PersistentFlags().Lookup("json"))
}

func TestRenderError_JSON(t *testing.T) {
	r := run(t, "--json", "nope")
	require.Equal(t, ExitUsage, r.code)
	assert.Empty(t, r.stdout)

	var doc struct {
		Error struct {
			Message  string `json:"message"`
			ExitCode int    `json:"exit_code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal([]byte(r.stderr), &doc))
	assert.Equal(t, ExitUsage, doc.Error.ExitCode)
	assert.Contains(t, doc.Error.Message, `unknown command "nope"`)
}

func TestPrintResult(t *testing.T) {
	v := map[string]int{"n": 1}
	human := func(w io.Writer) error {
		_, err := io.WriteString(w, "n=1\n")
		return err
	}

	t.Run("json", func(t *testing.T) {
		var out bytes.Buffer
		require.NoError(t, printResult(IOStreams{Out: &out}, true, v, human))
		var got map[string]int
		require.NoError(t, json.Unmarshal(out.Bytes(), &got))
		assert.Equal(t, v, got)
	})

	t.Run("human", func(t *testing.T) {
		var out bytes.Buffer
		require.NoError(t, printResult(IOStreams{Out: &out}, false, v, human))
		assert.Equal(t, "n=1\n", out.String())
	})

	t.Run("human error", func(t *testing.T) {
		boom := errors.New("boom")
		err := printResult(IOStreams{Out: io.Discard}, false, v, func(io.Writer) error { return boom })
		assert.ErrorIs(t, err, boom)
	})
}
