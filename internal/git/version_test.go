package git

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRunner returns canned output or error for every call.
type fakeRunner struct {
	out  string
	err  error
	args []string
	dir  string
}

func (f *fakeRunner) Run(_ context.Context, dir string, args ...string) ([]byte, error) {
	f.dir, f.args = dir, args
	return []byte(f.out), f.err
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		in      string
		want    Version
		wantErr bool
	}{
		{in: "git version 2.40.0", want: Version{2, 40, 0, "git version 2.40.0"}},
		{in: "git version 2.47.1.windows.2", want: Version{2, 47, 1, "git version 2.47.1.windows.2"}},
		{in: "git version 2.39.5 (Apple Git-154)", want: Version{2, 39, 5, "git version 2.39.5 (Apple Git-154)"}},
		{in: "git version 2.40\n", want: Version{2, 40, 0, "git version 2.40"}},
		{in: "garbage", wantErr: true},
		{in: "", wantErr: true},
		{in: "git version x.y", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseVersion(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCheckVersion_TooOld(t *testing.T) {
	for _, out := range []string{"git version 2.39.5\n", "git version 1.9.0\n"} {
		v, err := CheckVersion(context.Background(), &fakeRunner{out: out})
		require.ErrorIs(t, err, ErrVersionTooOld)
		assert.Contains(t, err.Error(), v.Raw)
	}
}

func TestCheckVersion_OK(t *testing.T) {
	for _, out := range []string{"git version 2.40.0\n", "git version 2.47.1.windows.2\n", "git version 3.0.0\n"} {
		_, err := CheckVersion(context.Background(), &fakeRunner{out: out})
		require.NoError(t, err, out)
	}
}

func TestCheckVersion_RunFailure(t *testing.T) {
	_, err := CheckVersion(context.Background(), &fakeRunner{err: ErrNotFound})
	require.ErrorIs(t, err, ErrNotFound)
	assert.NotErrorIs(t, err, ErrVersionTooOld)
}

func TestCheckVersion_Unparsable(t *testing.T) {
	_, err := CheckVersion(context.Background(), &fakeRunner{out: "nonsense"})
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrVersionTooOld)
}
