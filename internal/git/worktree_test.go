package git

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseWorktreeList(t *testing.T) {
	out := "worktree /repo\x00HEAD " + sha1a + "\x00branch refs/heads/develop\x00\x00" +
		"worktree /ws/a b\nc\x00HEAD " + sha1b + "\x00detached\x00locked\x00\x00" +
		"worktree /ws/gone\x00HEAD " + sha1c + "\x00branch refs/heads/cowflow/p/t\x00locked reason here\x00prunable gitdir file points to non-existent location\x00\x00" +
		"worktree /bare\x00bare\x00\x00"
	got, err := ParseWorktreeList([]byte(out))
	require.NoError(t, err)
	assert.Equal(t, []Worktree{
		{Path: "/repo", Head: sha1a, Branch: "develop"},
		{Path: "/ws/a b\nc", Head: sha1b, Locked: true},
		{Path: "/ws/gone", Head: sha1c, Branch: "cowflow/p/t", Locked: true, Prunable: true},
		{Path: "/bare"},
	}, got)
}

func TestParseWorktreeListIgnoresUnknownAttributes(t *testing.T) {
	got, err := ParseWorktreeList([]byte("worktree /r\x00HEAD " + sha1a + "\x00future thing\x00\x00"))
	require.NoError(t, err)
	assert.Equal(t, []Worktree{{Path: "/r", Head: sha1a}}, got)
}

func TestParseWorktreeListRejects(t *testing.T) {
	good := "worktree /repo\x00HEAD " + sha1a + "\x00branch refs/heads/develop\x00\x00"
	tests := map[string]string{
		"empty":                "",
		"no block terminator":  strings.TrimSuffix(good, "\x00"),
		"truncated":            good[:20],
		"no worktree line":     "HEAD " + sha1a + "\x00\x00",
		"empty path":           "worktree \x00\x00",
		"worktree without val": "worktree\x00\x00",
		"bad HEAD":             "worktree /r\x00HEAD abc\x00\x00",
		"branch not in heads":  "worktree /r\x00branch refs/tags/v1\x00\x00",
		"branch and detached":  "worktree /r\x00branch refs/heads/x\x00detached\x00\x00",
		"detached and branch":  "worktree /r\x00detached\x00branch refs/heads/x\x00\x00",
		"duplicate HEAD":       "worktree /r\x00HEAD " + sha1a + "\x00HEAD " + sha1a + "\x00\x00",
		"empty block":          good + "\x00\x00",
		"unterminated block":   good + "worktree /x\x00",
		"worktree in block":    "worktree /r\x00worktree /s\x00\x00",
	}
	for name, out := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := ParseWorktreeList([]byte(out))
			require.ErrorIs(t, err, ErrUnexpectedOutput)
			assert.Nil(t, got, "no partial result")
		})
	}
}

func FuzzParseWorktreeList(f *testing.F) {
	f.Add([]byte("worktree /repo\x00HEAD " + sha1a + "\x00branch refs/heads/develop\x00\x00"))
	f.Add([]byte("worktree /a\x00HEAD " + sha1b + "\x00detached\x00locked why\x00prunable\x00\x00worktree /b\x00bare\x00\x00"))
	f.Add([]byte(""))
	f.Fuzz(func(t *testing.T, out []byte) {
		got, err := ParseWorktreeList(out)
		if err != nil {
			if got != nil {
				t.Fatalf("partial result with error: %v", got)
			}
			return
		}
		if len(got) == 0 {
			t.Fatalf("success without entries")
		}
		for _, w := range got {
			if w.Path == "" || (w.Head != "" && !IsSHA(w.Head)) {
				t.Fatalf("invalid entry %+v", w)
			}
		}
	})
}
