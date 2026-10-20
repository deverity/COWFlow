package harness_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/git"
	"github.com/deverity/cowflow/test/harness"
)

// stubRunner returns canned output and counts calls.
type stubRunner struct {
	out   string
	calls int
}

func (s *stubRunner) Run(context.Context, string, ...string) ([]byte, error) {
	s.calls++
	return []byte(s.out), nil
}

func TestFaultyRunner_ExitCode(t *testing.T) {
	inner := &stubRunner{out: "ok"}
	f := &harness.FaultyRunner{Inner: inner, Faults: []harness.GitFault{
		{Match: []string{"check-ref-format"}, ExitCode: 128},
	}}

	_, err := f.Run(context.Background(), "/repo", "check-ref-format", "--branch", "x")
	var ge *git.Error
	require.ErrorAs(t, err, &ge)
	assert.Equal(t, 128, ge.ExitCode)
	assert.Equal(t, []string{"check-ref-format", "--branch", "x"}, ge.Args)
	assert.Contains(t, err.Error(), "exit code 128")
	assert.Zero(t, inner.calls, "git is not run")
}

func TestFaultyRunner_Timeout(t *testing.T) {
	f := &harness.FaultyRunner{Inner: &stubRunner{}, Faults: []harness.GitFault{
		{Match: []string{"fetch"}, Timeout: true},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := f.Run(ctx, "", "fetch", "origin")
		done <- err
	}()
	var err error
	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after the context deadline")
	}
	var ge *git.Error
	require.ErrorAs(t, err, &ge)
	assert.Equal(t, -1, ge.ExitCode)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestFaultyRunner_TimeoutCanceled(t *testing.T) {
	f := &harness.FaultyRunner{Faults: []harness.GitFault{{Timeout: true}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := f.Run(ctx, "", "status")
	assert.ErrorIs(t, err, context.Canceled)
}

func TestFaultyRunner_Truncate(t *testing.T) {
	inner := &stubRunner{out: "0123456789"}
	f := &harness.FaultyRunner{Inner: inner, Faults: []harness.GitFault{
		{Match: []string{"diff-tree"}, TruncateAt: 4},
	}}

	out, err := f.Run(context.Background(), "", "diff-tree", "-r", "--numstat", "HEAD")
	require.NoError(t, err)
	assert.Equal(t, "0123", string(out))
	assert.Equal(t, 1, inner.calls, "git runs, then output is cut")

	short, err := (&harness.FaultyRunner{Inner: &stubRunner{out: "ab"}, Faults: []harness.GitFault{{TruncateAt: 4}}}).
		Run(context.Background(), "", "x")
	require.NoError(t, err)
	assert.Equal(t, "ab", string(short), "shorter output is untouched")
}

func TestFaultyRunner_CannedStdout(t *testing.T) {
	inner := &stubRunner{}
	f := &harness.FaultyRunner{Inner: inner, Faults: []harness.GitFault{
		{Match: []string{"rev-parse"}, Stdout: "deadbeef\n"},
	}}
	out, err := f.Run(context.Background(), "", "rev-parse", "HEAD")
	require.NoError(t, err)
	assert.Equal(t, "deadbeef\n", string(out))
	assert.Zero(t, inner.calls)
}

func TestFaultyRunner_PassThrough(t *testing.T) {
	inner := &stubRunner{out: "real"}
	f := &harness.FaultyRunner{Inner: inner, Faults: []harness.GitFault{
		{Match: []string{"fetch"}, ExitCode: 1},
		{Match: []string{"status", "--porcelain"}, ExitCode: 1},
	}}
	out, err := f.Run(context.Background(), "", "status")
	require.NoError(t, err)
	assert.Equal(t, "real", string(out))
	assert.Equal(t, 1, inner.calls)

	_, err = (&harness.FaultyRunner{}).Run(context.Background(), "", "status")
	assert.Error(t, err, "no Inner and no fault is an error, not a panic")
}

func TestFaultyRunner_FirstMatchWinsAndInject(t *testing.T) {
	f := &harness.FaultyRunner{Inner: &stubRunner{}, Faults: []harness.GitFault{
		{Match: []string{"log"}, ExitCode: 2},
	}}
	f.Inject(harness.GitFault{ExitCode: 3}) // catch-all, later
	_, err := f.Run(context.Background(), "", "log", "-1")
	var ge *git.Error
	require.ErrorAs(t, err, &ge)
	assert.Equal(t, 2, ge.ExitCode)
	_, err = f.Run(context.Background(), "", "status")
	require.ErrorAs(t, err, &ge)
	assert.Equal(t, 3, ge.ExitCode)
}

func TestFaultyRunner_RecordsCalls(t *testing.T) {
	f := &harness.FaultyRunner{Inner: &stubRunner{}, Faults: []harness.GitFault{
		{Match: []string{"fetch"}, ExitCode: 1},
	}}
	_, _ = f.Run(context.Background(), "/a", "status", "-s")
	_, _ = f.Run(context.Background(), "", "fetch")

	assert.Equal(t, [][]string{{"status", "-s"}, {"fetch"}}, f.Recorded(), "faulted calls are recorded too, dir is not")
	assert.Equal(t, f.Calls, f.Recorded())
}

func TestFaultyRunner_WrapsRealRepo(t *testing.T) {
	r := harness.NewRepo(t)
	f := &harness.FaultyRunner{Inner: r.Runner}
	out, err := f.Run(context.Background(), r.Dir, "rev-parse", "--abbrev-ref", "HEAD")
	require.NoError(t, err)
	assert.Equal(t, "develop\n", string(out))
}

// optsStub is a stubRunner that also takes per-call options.
type optsStub struct {
	stubRunner
	opts []git.RunOpts
}

func (s *optsStub) RunWith(_ context.Context, _ string, o git.RunOpts, _ ...string) ([]byte, error) {
	s.calls++
	s.opts = append(s.opts, o)
	return []byte(s.out), nil
}

func TestFaultyOptsRunner_AppliesFaultsAndPassesOptions(t *testing.T) {
	inner := &optsStub{stubRunner: stubRunner{out: "abcdef"}}
	f := &harness.FaultyRunner{Inner: inner, Faults: []harness.GitFault{
		{Match: []string{"write-tree"}, ExitCode: 128},
		{Match: []string{"ls-files"}, TruncateAt: 3},
	}}
	opts := git.RunOpts{Env: []string{"GIT_INDEX_FILE=/tmp/x"}}

	_, err := harness.FaultyOptsRunner{FaultyRunner: f}.RunWith(context.Background(), "/repo", opts, "write-tree")
	var ge *git.Error
	require.ErrorAs(t, err, &ge)
	assert.Equal(t, 128, ge.ExitCode)

	out, err := harness.FaultyOptsRunner{FaultyRunner: f}.RunWith(context.Background(), "/repo", opts, "ls-files")
	require.NoError(t, err)
	assert.Equal(t, "abc", string(out))

	out, err = harness.FaultyOptsRunner{FaultyRunner: f}.RunWith(context.Background(), "/repo", opts, "read-tree")
	require.NoError(t, err)
	assert.Equal(t, "abcdef", string(out))
	assert.Equal(t, 2, inner.calls, "the exit-code fault never reaches Inner")
	require.Len(t, inner.opts, 2)
	assert.Equal(t, opts.Env, inner.opts[0].Env)
	assert.Equal(t, [][]string{{"write-tree"}, {"ls-files"}, {"read-tree"}}, f.Recorded())
}

func TestFaultyOptsRunner_NeedsOptsInner(t *testing.T) {
	f := &harness.FaultyRunner{Inner: &stubRunner{}}
	_, err := harness.FaultyOptsRunner{FaultyRunner: f}.RunWith(context.Background(), "/repo", git.RunOpts{}, "status")
	assert.ErrorIs(t, err, git.ErrOptsUnsupported)
}

func TestFaultyRunner_Clear(t *testing.T) {
	inner := &stubRunner{out: "ok"}
	f := &harness.FaultyRunner{Inner: inner}
	f.Inject(harness.GitFault{ExitCode: 1})
	_, err := f.Run(context.Background(), "/repo", "status")
	require.Error(t, err)

	f.Clear()
	out, err := f.Run(context.Background(), "/repo", "status")
	require.NoError(t, err)
	assert.Equal(t, "ok", string(out))
}
