package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSize(t *testing.T) {
	ok := []struct {
		in   string
		want uint64
	}{
		{"50GB", 50 << 30},
		{"1tb", 1 << 40},
		{"50 GB", 50 << 30},
		{"64gB", 64 << 30},
		{"2TB", 2 << 40},
	}
	for _, tt := range ok {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseSize(tt.in)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	bad := []string{
		"50", "50MB", "-1GB", "GB", "", " 50GB", "50GB ", "1.5GB", "0GB",
		"50GiB", "50G", "99999999999999999999GB", "16777216TB",
	}
	for _, in := range bad {
		t.Run("bad "+in, func(t *testing.T) {
			_, err := parseSize(in)
			require.Error(t, err)
			assert.Equal(t, ExitUsage, codeOf(err, true))
		})
	}
}

func FuzzParseSize(f *testing.F) {
	for _, s := range []string{"50GB", "1tb", "50 GB", "GB", "-1GB", "18446744073709551615TB"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		n, err := parseSize(s)
		if err != nil {
			assert.Equal(t, ExitUsage, codeOf(err, true))
			return
		}
		assert.NotZero(t, n)
		assert.Zero(t, n%(1<<30), "size %q is not a whole number of GiB", s)
	})
}
