package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/test/fixtures/benchrepo"
)

func TestRunUsageErrors(t *testing.T) {
	for name, args := range map[string][]string{
		"nothing":      {},
		"both":         {"--out", "a", "--verify", "b"},
		"bad flag":     {"--nope"},
		"bad profile":  {"--out", "x", "--profile", "huge"},
		"missing repo": {"--verify", filepath.Join(t.TempDir(), "missing")},
	} {
		var out, errb bytes.Buffer
		code := run(context.Background(), args, &out, &errb)
		assert.NotZero(t, code, name)
		assert.Empty(t, out.String(), name)
		assert.NotEmpty(t, errb.String(), name)
	}
}

func TestRunGenerateThenVerify(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repo")
	var out, errb bytes.Buffer
	require.Zero(t, run(context.Background(), []string{"--profile", "small", "--seed", "2", "--out", dir}, &out, &errb), errb.String())

	var gen benchrepo.Report
	require.NoError(t, json.Unmarshal(out.Bytes(), &gen))
	assert.Greater(t, gen.Bytes, int64(40<<20))

	out.Reset()
	require.Zero(t, run(context.Background(), []string{"--verify", dir}, &out, &errb), errb.String())
	var ver benchrepo.Report
	require.NoError(t, json.Unmarshal(out.Bytes(), &ver))
	assert.Equal(t, gen, ver)
}
