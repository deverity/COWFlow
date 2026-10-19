package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tree builds <tmp>/ws/{a/sub/file.txt, ab/file.txt} and <tmp>/outside and
// returns the canonical tmp directory.
func tree(t *testing.T) string {
	t.Helper()
	base, err := Canonical(t.TempDir())
	require.NoError(t, err)
	for _, d := range []string{"ws/a/sub", "ws/ab", "outside"} {
		require.NoError(t, os.MkdirAll(filepath.Join(base, filepath.FromSlash(d)), 0o750))
	}
	for _, f := range []string{"ws/a/sub/file.txt", "ws/ab/file.txt", "outside/secret.txt"} {
		require.NoError(t, os.WriteFile(filepath.Join(base, filepath.FromSlash(f)), []byte("x"), 0o600))
	}
	return base
}

// symlink creates link -> target or skips the test when the OS refuses
// (creating symlinks on Windows needs a privilege or Developer Mode).
func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlinks here (needs a privilege on Windows): %v", err)
	}
}

func TestConfine(t *testing.T) {
	base := tree(t)
	j := func(parts ...string) string { return filepath.Join(append([]string{base}, parts...)...) }

	tests := []struct {
		name string
		root string
		p    string
		want string // empty: ErrOutsideRoot expected
	}{
		{"inside existing file", j("ws"), j("ws", "a", "sub", "file.txt"), j("ws", "a", "sub", "file.txt")},
		{"inside directory", j("ws"), j("ws", "a"), j("ws", "a")},
		{"equal to root", j("ws"), j("ws"), j("ws")},
		{"root with trailing separator", j("ws") + string(filepath.Separator), j("ws", "a"), j("ws", "a")},
		{"dot-dot inside stays inside", j("ws"), j("ws", "a", "..", "ab"), j("ws", "ab")},
		{"dot-dot escape", j("ws"), j("ws", "a", "..", "..", "outside", "secret.txt"), ""},
		{"dot-dot escape to parent of root", j("ws"), j("ws", ".."), ""},
		{"absolute path outside", j("ws"), j("outside", "secret.txt"), ""},
		{"sibling prefix collision", j("ws", "a"), j("ws", "ab", "file.txt"), ""},
		{"sibling prefix collision on the directory", j("ws", "a"), j("ws", "ab"), ""},
		{"not-yet-existing file inside", j("ws"), j("ws", "a", "new", "deep", "file.txt"), j("ws", "a", "new", "deep", "file.txt")},
		{"not-yet-existing file outside", j("ws"), j("outside", "new.txt"), ""},
		{"not-yet-existing root", j("ws", "new-root"), j("ws", "new-root", "x"), j("ws", "new-root", "x")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Confine(tt.root, tt.p)
			if tt.want == "" {
				require.ErrorIs(t, err, ErrOutsideRoot)
				assert.Empty(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConfine_RelativePath(t *testing.T) {
	base := tree(t)
	t.Chdir(filepath.Join(base, "ws"))

	got, err := Confine(".", filepath.Join("a", "sub", "file.txt"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(base, "ws", "a", "sub", "file.txt"), got)

	_, err = Confine(".", filepath.Join("..", "outside"))
	require.ErrorIs(t, err, ErrOutsideRoot)
}

func TestConfine_Symlinks(t *testing.T) {
	base := tree(t)
	ws := filepath.Join(base, "ws")
	outside := filepath.Join(base, "outside")

	symlink(t, outside, filepath.Join(ws, "escape"))
	symlink(t, filepath.Join(ws, "a"), filepath.Join(ws, "alias"))
	symlink(t, filepath.Join(outside, "missing"), filepath.Join(ws, "dangling"))
	symlink(t, ws, filepath.Join(outside, "into-ws"))

	t.Run("symlink pointing outside", func(t *testing.T) {
		_, err := Confine(ws, filepath.Join(ws, "escape", "secret.txt"))
		require.ErrorIs(t, err, ErrOutsideRoot)
	})
	t.Run("symlink pointing outside, new leaf", func(t *testing.T) {
		_, err := Confine(ws, filepath.Join(ws, "escape", "new.txt"))
		require.ErrorIs(t, err, ErrOutsideRoot)
	})
	t.Run("symlink itself pointing outside", func(t *testing.T) {
		_, err := Confine(ws, filepath.Join(ws, "escape"))
		require.ErrorIs(t, err, ErrOutsideRoot)
	})
	t.Run("symlink inside is resolved", func(t *testing.T) {
		got, err := Confine(ws, filepath.Join(ws, "alias", "sub", "file.txt"))
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(ws, "a", "sub", "file.txt"), got)
	})
	t.Run("dangling symlink is refused", func(t *testing.T) {
		_, err := Confine(ws, filepath.Join(ws, "dangling"))
		require.Error(t, err)
		_, err = Confine(ws, filepath.Join(ws, "dangling", "x"))
		require.Error(t, err)
	})
	t.Run("root reached through a symlink", func(t *testing.T) {
		got, err := Confine(filepath.Join(outside, "into-ws"), filepath.Join(ws, "a"))
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(ws, "a"), got)
	})
}

func TestCanonical_InvalidPathFails(t *testing.T) {
	// A NUL byte is invalid in every OS path, so resolution cannot succeed.
	_, err := Canonical("bad\x00path")
	require.Error(t, err)
}

func TestLongPath(t *testing.T) {
	drive := func(n int) string { return `C:\` + strings.Repeat("a", n-3) }
	unc := func(n int) string { return `\\server\share\` + strings.Repeat("b", n-len(`\\server\share\`)) }
	long := drive(241)

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"239 chars", drive(239), drive(239)},
		{"240 chars", drive(240), drive(240)},
		{"241 chars", long, `\\?\` + long},
		{"forward slashes after the drive", "C:/" + strings.Repeat("a", 260), `\\?\C:/` + strings.Repeat("a", 260)},
		{"UNC 239 chars", unc(239), unc(239)},
		{"UNC 241 chars", unc(241), `\\?\UNC\` + unc(241)[2:]},
		{"already prefixed", `\\?\` + long, `\\?\` + long},
		{"already prefixed UNC", `\\?\UNC\` + unc(241)[2:], `\\?\UNC\` + unc(241)[2:]},
		{"device path", `\\.\` + long, `\\.\` + long},
		{"relative path", strings.Repeat("r", 300), strings.Repeat("r", 300)},
		{"drive-relative path", "C:" + strings.Repeat("r", 300), "C:" + strings.Repeat("r", 300)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, winLongPath(tt.in))
			if runtime.GOOS == "windows" {
				assert.Equal(t, tt.want, LongPath(tt.in))
			} else {
				assert.Equal(t, tt.in, LongPath(tt.in), "LongPath is the identity outside Windows")
			}
		})
	}
}

func TestWinStripLongPrefix(t *testing.T) {
	tests := []struct{ in, want string }{
		{`\\?\C:\ws\a`, `C:\ws\a`},
		{`\\?\c:\`, `c:\`},
		{`\\?\UNC\server\share\x`, `\\server\share\x`},
		{`\\?\Volume{0b1c2d3e-0000-0000-0000-000000000000}\x`, `\\?\Volume{0b1c2d3e-0000-0000-0000-000000000000}\x`},
		{`C:\ws`, `C:\ws`},
		{`\\server\share`, `\\server\share`},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, winStripLongPrefix(tt.in), tt.in)
	}
}

func TestHasPrefixFold(t *testing.T) {
	assert.True(t, hasPrefixFold(`c:\ws\a\x`, `C:\WS\A\`))
	assert.True(t, hasPrefixFold(`C:\WS\A\`, `c:\ws\a\`))
	assert.False(t, hasPrefixFold(`C:\ws\ab`, `C:\ws\a\`))
	assert.False(t, hasPrefixFold(`C:\w`, `C:\ws\`))
}

func TestWithin(t *testing.T) {
	sep := string(filepath.Separator)
	root := filepath.Join(sep, "ws", "a")
	if runtime.GOOS == "windows" {
		root = `C:\ws\a`
	}
	assert.True(t, within(root, root))
	assert.True(t, within(root, root+sep+"x"))
	assert.False(t, within(root, root+"b"))
	assert.False(t, within(root, root+"b"+sep+"x"))
	assert.False(t, within(root, filepath.Dir(root)))

	fsRoot := sep
	if runtime.GOOS == "windows" {
		fsRoot = `C:\`
	}
	assert.True(t, within(fsRoot, root), "a filesystem root already ends with a separator")
}

func FuzzConfine(f *testing.F) {
	for _, s := range []string{
		"", ".", "..", "a", "a/../..", "../ws-sibling", "a/sub/file.txt", "/etc/passwd",
		`..\..\x`, `C:\Windows`, `\\server\share`, `\\?\C:\x`, "a/./b//c", "ab", "a\x00b",
	} {
		f.Add(s)
	}
	base, err := Canonical(f.TempDir())
	require.NoError(f, err)
	root := filepath.Join(base, "ws")
	require.NoError(f, os.MkdirAll(filepath.Join(root, "a", "sub"), 0o750))
	require.NoError(f, os.MkdirAll(filepath.Join(base, "ws-sibling"), 0o750))

	sep := string(filepath.Separator)
	prefix := root + sep
	f.Fuzz(func(t *testing.T, s string) {
		for _, p := range []string{s, filepath.Join(root, s)} {
			got, err := Confine(root, p)
			if err != nil {
				continue
			}
			ok := samePath(got, root) || hasPathPrefix(got, prefix)
			if !ok {
				t.Fatalf("Confine(%q, %q) = %q: not root and not under %q", root, p, got, prefix)
			}
		}
	})
}
