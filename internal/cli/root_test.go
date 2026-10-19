package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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
		{name: "version subcommand pending U-API-13", args: []string{"version"}, want: `unknown command "version"`},
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
