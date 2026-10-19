package git

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// refFakeRunner is a fake Runner that records calls and returns canned
// results.
type refFakeRunner struct {
	calls [][]string
	out   string
	err   error
}

func (f *refFakeRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, args)
	return []byte(f.out), f.err
}

// refEchoRunner accepts every name and echoes it as the real command would.
type refEchoRunner struct{}

func (refEchoRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	return []byte(args[len(args)-1] + "\n"), nil
}

func TestCheckBranchName(t *testing.T) {
	r := realRunner(t)
	for _, name := range []string{"feat/x", "cowflow/p/auth"} {
		t.Run("valid/"+name, func(t *testing.T) {
			got, err := CheckBranchName(context.Background(), r, name)
			require.NoError(t, err)
			assert.Equal(t, name, got)
		})
	}
	invalid := []string{"-x", "a..b", "a.lock", "a b", "a~1", "a^", "a:b", `a\b`, "@{-1}", "x/", ".x", "", "a\x00b", "a?b", "a*b", "a[b"}
	for _, name := range invalid {
		t.Run("invalid/"+name, func(t *testing.T) {
			got, err := CheckBranchName(context.Background(), r, name)
			require.ErrorIs(t, err, ErrInvalidRefName)
			assert.Empty(t, got)
		})
	}
}

func TestCheckBranchNameNeverSendsOptionLikeNames(t *testing.T) {
	f := &refFakeRunner{}
	for _, name := range []string{"", "-x", "--help", "a@{-1}", "a\x00b"} {
		_, err := CheckBranchName(context.Background(), f, name)
		require.ErrorIs(t, err, ErrInvalidRefName, name)
	}
	assert.Empty(t, f.calls, "rejected names must not reach git")
}

func TestCheckBranchNameErrorClassification(t *testing.T) {
	ctx := context.Background()

	_, err := CheckBranchName(ctx, &refFakeRunner{err: &Error{ExitCode: 1}}, "ok")
	require.ErrorIs(t, err, ErrInvalidRefName)

	timeout := &Error{ExitCode: -1, Err: context.DeadlineExceeded}
	_, err = CheckBranchName(ctx, &refFakeRunner{err: timeout}, "ok")
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrInvalidRefName)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	_, err = CheckBranchName(ctx, &refFakeRunner{err: ErrNotFound}, "ok")
	assert.ErrorIs(t, err, ErrNotFound)
	assert.NotErrorIs(t, err, ErrInvalidRefName)

	f := &refFakeRunner{out: "  feat/x\n"}
	got, err := CheckBranchName(ctx, f, "feat/x")
	require.NoError(t, err)
	assert.Equal(t, "feat/x", got)
	assert.Equal(t, [][]string{{"check-ref-format", "--branch", "feat/x"}}, f.calls)

	_, err = CheckBranchName(ctx, &refFakeRunner{out: "-evil\n"}, "ok")
	require.ErrorIs(t, err, ErrInvalidRefName)
}

func TestCheckRefName(t *testing.T) {
	r := realRunner(t)
	require.NoError(t, CheckRefName(context.Background(), r, "refs/cowflow/changes/01HZXABCDEF"))
	for _, ref := range []string{"cowflow/changes/x", "", "refs/", "refs/a..b", "refs/a b", "refs/x.lock", "-refs/x"} {
		err := CheckRefName(context.Background(), r, ref)
		assert.ErrorIs(t, err, ErrInvalidRefName, ref)
	}
}

func TestCheckRefNameErrorClassification(t *testing.T) {
	ctx := context.Background()
	f := &refFakeRunner{}
	require.ErrorIs(t, CheckRefName(ctx, f, "heads/x"), ErrInvalidRefName)
	assert.Empty(t, f.calls)

	err := CheckRefName(ctx, &refFakeRunner{err: &Error{ExitCode: -1, Err: context.Canceled}}, "refs/x")
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrInvalidRefName)

	err = CheckRefName(ctx, &refFakeRunner{err: errors.New("boom")}, "refs/x")
	assert.NotErrorIs(t, err, ErrInvalidRefName)
}

func FuzzCheckBranchName(f *testing.F) {
	for _, s := range []string{"feat/x", "cowflow/p/auth", "-x", "a..b", "@{-1}", "", "x/", "a\x00b", "--help"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, name string) {
		got, err := CheckBranchName(context.Background(), refEchoRunner{}, name)
		if err != nil {
			require.ErrorIs(t, err, ErrInvalidRefName)
			return
		}
		if strings.HasPrefix(got, "-") || got == "" {
			t.Fatalf("accepted name %q yielded %q", name, got)
		}
	})
}
