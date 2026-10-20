package git

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	sha1a = "1111111111111111111111111111111111111111"
	sha1b = "2222222222222222222222222222222222222222"
	sha1c = "3333333333333333333333333333333333333333"
	zero  = "0000000000000000000000000000000000000000"
)

func rec1(xy, path string) string {
	return "1 " + xy + " N... 100644 100644 100644 " + sha1a + " " + sha1b + " " + path + "\x00"
}

func TestStatusParse(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want []StatusEntry
	}{
		{"empty", "", nil},
		{"spaces", rec1(".M", "dir with space/a b.txt"),
			[]StatusEntry{{Kind: StatusChanged, XY: ".M", Path: "dir with space/a b.txt"}}},
		{"newline in name", rec1("M.", "line\nbreak"),
			[]StatusEntry{{Kind: StatusChanged, XY: "M.", Path: "line\nbreak"}}},
		{"unicode", rec1(".D", "café/日本.txt"),
			[]StatusEntry{{Kind: StatusChanged, XY: ".D", Path: "café/日本.txt"}}},
		{"added", "1 A. N... 000000 100644 100644 " + zero + " " + sha1a + " new.go\x00",
			[]StatusEntry{{Kind: StatusChanged, XY: "A.", Path: "new.go"}}},
		{"rename", "2 R. N... 100644 100644 100644 " + sha1a + " " + sha1a + " R100 new name\x00old name\x00",
			[]StatusEntry{{Kind: StatusRenamed, XY: "R.", Path: "new name", OrigPath: "old name"}}},
		{"copy", "2 C. N... 100644 100644 100644 " + sha1a + " " + sha1a + " C75 b\x00a\x00",
			[]StatusEntry{{Kind: StatusRenamed, XY: "C.", Path: "b", OrigPath: "a"}}},
		{"unmerged", "u UU N... 100644 100644 100644 100644 " + sha1a + " " + sha1b + " " + sha1c + " conflict.txt\x00",
			[]StatusEntry{{Kind: StatusUnmerged, XY: "UU", Path: "conflict.txt"}}},
		{"untracked directory", "? build/out/x.bin\x00? build/y\x00",
			[]StatusEntry{
				{Kind: StatusUntracked, XY: "??", Path: "build/out/x.bin"},
				{Kind: StatusUntracked, XY: "??", Path: "build/y"},
			}},
		{"submodule", "1 .M SC.. 160000 160000 160000 " + sha1a + " " + sha1a + " sub\x00",
			[]StatusEntry{{Kind: StatusChanged, XY: ".M", Path: "sub"}}},
		{"mixed", rec1(".M", "a") + "2 R. N... 100644 100644 100644 " + sha1a + " " + sha1a + " R100 c\x00b\x00? d\x00",
			[]StatusEntry{
				{Kind: StatusChanged, XY: ".M", Path: "a"},
				{Kind: StatusRenamed, XY: "R.", Path: "c", OrigPath: "b"},
				{Kind: StatusUntracked, XY: "??", Path: "d"},
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseStatusV2([]byte(tt.out))
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestStatusParseRejects(t *testing.T) {
	good := rec1(".M", "a")
	rename := "2 R. N... 100644 100644 100644 " + sha1a + " " + sha1a + " R100 new\x00old\x00"
	tests := map[string]string{
		"no final NUL":        strings.TrimSuffix(good, "\x00"),
		"truncated record":    good[:10],
		"truncated mid-SHA":   good[:40] + "\x00",
		"rename without orig": strings.TrimSuffix(rename, "old\x00"),
		"rename empty orig":   strings.TrimSuffix(rename, "old\x00") + "\x00",
		"ignored entry":       "! ignored\x00",
		"branch header":       "# branch.oid " + sha1a + "\x00",
		"unknown type":        "x foo\x00",
		"bad XY":              "1 ZZ N... 100644 100644 100644 " + sha1a + " " + sha1b + " a\x00",
		"bad sub":             "1 .M X... 100644 100644 100644 " + sha1a + " " + sha1b + " a\x00",
		"bad mode":            "1 .M N... 100648 100644 100644 " + sha1a + " " + sha1b + " a\x00",
		"bad score":           "2 R. N... 100644 100644 100644 " + sha1a + " " + sha1a + " X100 new\x00old\x00",
		"score over 100":      "2 R. N... 100644 100644 100644 " + sha1a + " " + sha1a + " R101 new\x00old\x00",
		"empty path":          "1 .M N... 100644 100644 100644 " + sha1a + " " + sha1b + " \x00",
		"untracked no path":   "? \x00",
		"empty record":        "\x00",
		"trailing garbage":    good + "garbage",
	}
	for name, out := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := ParseStatusV2([]byte(out))
			require.ErrorIs(t, err, ErrUnexpectedOutput)
			assert.Nil(t, got, "no partial result")
		})
	}
}

func TestStatusKindString(t *testing.T) {
	assert.Equal(t, "changed", StatusChanged.String())
	assert.Equal(t, "renamed", StatusRenamed.String())
	assert.Equal(t, "unmerged", StatusUnmerged.String())
	assert.Equal(t, "untracked", StatusUntracked.String())
	assert.Equal(t, "StatusKind(9)", StatusKind(9).String())
}

func FuzzParseStatusV2(f *testing.F) {
	f.Add([]byte(""))
	f.Add([]byte(rec1(".M", "a b")))
	f.Add([]byte("2 R. N... 100644 100644 100644 " + sha1a + " " + sha1a + " R100 new\x00old\x00"))
	f.Add([]byte("u UU N... 100644 100644 100644 100644 " + sha1a + " " + sha1b + " " + sha1c + " c\x00"))
	f.Add([]byte("? x\x00? y\nz\x00"))
	f.Fuzz(func(t *testing.T, out []byte) {
		got, err := ParseStatusV2(out)
		if err != nil {
			if got != nil {
				t.Fatalf("partial result with error: %v", got)
			}
			return
		}
		if len(out) > 0 && out[len(out)-1] != 0 {
			t.Fatalf("accepted output without final NUL")
		}
		for _, e := range got {
			if e.Path == "" || len(e.XY) != 2 || e.Kind < StatusChanged || e.Kind > StatusUntracked {
				t.Fatalf("invalid entry %+v", e)
			}
			if (e.Kind == StatusRenamed) != (e.OrigPath != "") {
				t.Fatalf("OrigPath mismatch in %+v", e)
			}
		}
	})
}

func TestParseSHALines(t *testing.T) {
	got, err := parseSHALines("x", []byte(sha1a+"\n"+sha1b+"\n"))
	require.NoError(t, err)
	assert.Equal(t, []string{sha1a, sha1b}, got)

	got, err = parseSHALines("x", nil)
	require.NoError(t, err)
	assert.Empty(t, got)

	for _, out := range []string{sha1a, sha1a + "\n" + sha1b[:10], "zz\n", "\n"} {
		got, err := parseSHALines("x", []byte(out))
		require.ErrorIs(t, err, ErrUnexpectedOutput, "%q", out)
		assert.Nil(t, got)
	}
}
