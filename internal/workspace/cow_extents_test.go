package workspace

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCloneExtents(t *testing.T) {
	const gib = int64(1 << 30)
	tests := []struct {
		name          string
		size, cluster int64
		want          []cloneExtent
	}{
		{"empty", 0, 4096, nil},
		{"one byte", 1, 4096, []cloneExtent{{0, 4096}}},
		{"cluster minus one", 4095, 4096, []cloneExtent{{0, 4096}}},
		{"one cluster", 4096, 4096, []cloneExtent{{0, 4096}}},
		{"cluster plus one", 4097, 4096, []cloneExtent{{0, 8192}}},
		{"just below the chunk limit", 4*gib - 4096, 4096, []cloneExtent{{0, 4*gib - 4096}}},
		{"chunk limit", 4 * gib, 4096, []cloneExtent{{0, 4*gib - 4096}, {4*gib - 4096, 4096}}},
		{"over 4 GiB with an unaligned tail", 4*gib + 17, 65536, []cloneExtent{
			{0, 4*gib - 65536}, {4*gib - 65536, 131072},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := cloneExtents(tt.size, tt.cluster)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Every range is cluster-aligned, below the 4 GiB limit, contiguous, and
// together they cover the file with less than one cluster of overhang.
func TestCloneExtentsCoverFile(t *testing.T) {
	const gib = int64(1 << 30)
	for _, cluster := range []int64{512, 4096, 65536} {
		for _, size := range []int64{1, cluster - 1, cluster, cluster + 1, 10 << 20, 4*gib - 1, 9*gib + 3} {
			got, err := cloneExtents(size, cluster)
			require.NoError(t, err)
			next := int64(0)
			for _, e := range got {
				assert.Equal(t, next, e.Offset, "contiguous (size %d, cluster %d)", size, cluster)
				assert.Zero(t, e.Offset%cluster, "aligned offset")
				assert.Zero(t, e.Length%cluster, "aligned length")
				assert.Less(t, e.Length, maxCloneChunk, "below 4 GiB")
				assert.Positive(t, e.Length)
				next = e.Offset + e.Length
			}
			assert.GreaterOrEqual(t, next, size, "covers the file")
			assert.Less(t, next-size, cluster, "overhang below one cluster")
		}
	}
}

func TestCloneExtentsInvalid(t *testing.T) {
	for _, tc := range []struct{ size, cluster int64 }{
		{-1, 4096}, {1, 0}, {1, -4096}, {1, maxCloneChunk},
	} {
		_, err := cloneExtents(tc.size, tc.cluster)
		assert.Error(t, err, "size %d, cluster %d", tc.size, tc.cluster)
	}
}
