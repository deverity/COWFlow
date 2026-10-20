package workspace

import "fmt"

// maxCloneChunk bounds one block-clone call: FSCTL_DUPLICATE_EXTENTS_TO_FILE
// takes a ByteCount below 4 GiB.
const maxCloneChunk = int64(4 << 30)

// cloneExtent is the byte range of one block-clone call. Source and target
// offsets are equal: a whole file is cloned onto an empty file of the same
// size.
type cloneExtent struct {
	Offset, Length int64
}

// cloneExtents splits a file of size bytes into cluster-aligned ranges, each
// below [maxCloneChunk]. The last range is rounded up to a whole cluster,
// which the block-clone call requires and accepts past end of file. A
// zero-byte file needs no range.
func cloneExtents(size, cluster int64) ([]cloneExtent, error) {
	if size < 0 {
		return nil, fmt.Errorf("negative file size %d", size)
	}
	if cluster <= 0 || cluster > maxCloneChunk/2 {
		return nil, fmt.Errorf("invalid cluster size %d", cluster)
	}
	step := (maxCloneChunk - cluster) / cluster * cluster
	var out []cloneExtent
	for off := int64(0); off < size; off += step {
		n := min(step, roundUp(size-off, cluster))
		out = append(out, cloneExtent{Offset: off, Length: n})
	}
	return out, nil
}

// roundUp rounds n up to a multiple of unit; unit is positive.
func roundUp(n, unit int64) int64 {
	return (n + unit - 1) / unit * unit
}
