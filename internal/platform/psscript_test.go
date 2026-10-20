package platform

import (
	"encoding/base64"
	"encoding/binary"
	"flag"
	"os"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/require"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

func TestFormatScript_Golden(t *testing.T) {
	got, err := renderFormatScript(3, "X:")
	require.NoError(t, err)

	const golden = "testdata/format_script_disk3_X.golden"
	if *updateGolden {
		require.NoError(t, os.WriteFile(golden, []byte(got), 0o600))
	}
	want, err := os.ReadFile(golden)
	require.NoError(t, err)
	require.Equal(t, string(want), got)
}

// TestScripts_NoUnencodedUserText renders every script with hostile values
// and checks they appear only base64-encoded.
func TestScripts_NoUnencodedUserText(t *testing.T) {
	const evil = `X:'; Remove-Item C:\ -Recurse #`
	const evilPath = `C:\cf\a'; Remove-Item C:\ -Recurse #.vhdx`

	check := func(t *testing.T, script, value string) {
		t.Helper()
		require.NotContains(t, script, value)
		require.NotContains(t, script, "Remove-Item")
		require.Contains(t, script, "'"+base64.StdEncoding.EncodeToString([]byte(value))+"'")
		require.NotContains(t, script, "{{", "every placeholder is filled")
	}

	for name, render := range map[string]func(int, string) (string, error){
		"format": renderFormatScript,
		"mount":  renderMountScript,
	} {
		t.Run(name, func(t *testing.T) {
			s, err := render(3, evil)
			require.NoError(t, err)
			check(t, s, evil)
			require.Contains(t, s, "$n = [int]'3'")
		})
	}
	for name, src := range map[string]string{
		"new-vhd":      newVHDScript,
		"mount-vhd":    mountVHDScript,
		"dismount-vhd": dismountVHDScript,
		"get-vhd":      vhdDiskScript,
	} {
		t.Run(name, func(t *testing.T) {
			s, err := renderPathScript(src, evilPath, 50<<30)
			require.NoError(t, err)
			check(t, s, evilPath)
		})
	}
	s, err := renderPathScript(newVHDScript, evilPath, 53687091200)
	require.NoError(t, err)
	require.Contains(t, s, "[uint64]'53687091200'")
}

func TestScripts_Prelude(t *testing.T) {
	for _, src := range []string{formatScript, mountScript, newVHDScript, mountVHDScript,
		dismountVHDScript, vhdDiskScript, registerTaskScript, unregisterTaskScript} {
		require.True(t, strings.HasPrefix(src, "$ErrorActionPreference = 'Stop'\n"))
	}
}

func TestEncodeCommand(t *testing.T) {
	script := "Write-Output 'é😀'\n"
	raw, err := base64.StdEncoding.DecodeString(encodeCommand(script))
	require.NoError(t, err)
	require.Zero(t, len(raw)%2)
	u := make([]uint16, len(raw)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(raw[2*i:])
	}
	require.Equal(t, script, string(utf16.Decode(u)))
	require.Equal(t, uint16('W'), u[0], "UTF-16LE, no byte-order mark")
}

func TestParseScriptInt(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want int
	}{{"3\r\n", 3}, {" 12 \n", 12}, {"-1\r\n", -1}} {
		n, err := parseScriptInt(tt.in)
		require.NoError(t, err, tt.in)
		require.Equal(t, tt.want, n, tt.in)
	}
	for _, in := range []string{"", "3\r\n4\r\n", "three", "3 4"} {
		_, err := parseScriptInt(in)
		require.Error(t, err, in)
	}
}

func TestTruncate(t *testing.T) {
	require.Equal(t, "abc", truncate("abc", 3))
	require.Equal(t, "ab...(truncated)", truncate("abc", 2))
	// "é" is two bytes: cutting inside it backs off to the rune start.
	require.Equal(t, "a...(truncated)", truncate("aé", 2))
}
