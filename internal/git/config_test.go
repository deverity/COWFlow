package git

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigGet(t *testing.T) {
	ctx := context.Background()
	r := realRunner(t)
	repo := initRepo(t, r)

	v, found, err := ConfigGet(ctx, r, repo, "core.longpaths")
	require.NoError(t, err)
	assert.False(t, found)
	assert.Empty(t, v)

	_, err = r.Run(ctx, repo, "config", "core.longpaths", "true")
	require.NoError(t, err)
	v, found, err = ConfigGet(ctx, r, repo, "core.longpaths")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "true", v)
}

func TestConfigGet_InvalidKey(t *testing.T) {
	for _, key := range []string{"", "core", "-x.y", "core.", "Core.x", "core.a b", "--get.x", "core.x\n"} {
		f := &fakeRunner{}
		_, _, err := ConfigGet(context.Background(), f, "", key)
		require.Error(t, err, key)
		assert.Nil(t, f.args, "invalid key %q must not reach git", key)
	}
}

func TestConfigGet_ExitCodes(t *testing.T) {
	ctx := context.Background()

	_, found, err := ConfigGet(ctx, &fakeRunner{err: &Error{ExitCode: 1}}, "", "core.x")
	require.NoError(t, err)
	assert.False(t, found)

	failure := &Error{ExitCode: 128, Stderr: "fatal: bad config"}
	_, _, err = ConfigGet(ctx, &fakeRunner{err: failure}, "", "core.x")
	require.ErrorIs(t, err, failure)

	timeout := &Error{ExitCode: -1, Err: context.DeadlineExceeded}
	_, found, err = ConfigGet(ctx, &fakeRunner{err: timeout}, "", "core.x")
	require.Error(t, err)
	assert.False(t, found)

	_, _, err = ConfigGet(ctx, &fakeRunner{err: errors.New("boom")}, "", "core.x")
	require.Error(t, err)
}
