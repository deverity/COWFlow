package platform

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Volume identifies the volume a path lives on (SCOPE.md §6.3, §20.13).
// Filesystem names are lowercase and stable because they appear in --json.
type Volume struct {
	Filesystem       string // lowercase: "refs", "ntfs", "btrfs", "xfs", "ext4", "apfs", "tmpfs", ...
	ID               string // stable per volume: serial number (Windows), st_dev/fsid (Unix)
	MountPath        string // volume root ("X:\\", "/mnt/cf")
	DevDrive         bool
	BlockRefcounting bool   // a hint only; the probe clone is authoritative
	ClusterSize      uint32 // 0 when unknown
}

// VolumeOf reports the volume holding path. The path must exist; use
// [SameVolume] for paths that are still to be created.
func VolumeOf(ctx context.Context, path string) (Volume, error) {
	if err := ctx.Err(); err != nil {
		return Volume{}, err
	}
	return volumeOf(ctx, path)
}

// SameVolume reports whether a and b live on one volume, by comparing
// [Volume.ID]. A path that does not exist yet is replaced by its nearest
// existing ancestor. The answer is conservative: btrfs subvolumes have
// distinct IDs although a clone between them would work, so the probe clone
// decides, not this function.
func SameVolume(ctx context.Context, a, b string) (bool, error) {
	va, err := volumeOfExisting(ctx, a)
	if err != nil {
		return false, err
	}
	vb, err := volumeOfExisting(ctx, b)
	if err != nil {
		return false, err
	}
	return va.ID == vb.ID, nil
}

// FreeBytes returns the bytes available to the calling user on the volume
// holding path. A path that does not exist yet is replaced by its nearest
// existing ancestor.
func FreeBytes(path string) (uint64, error) {
	p, err := nearestExisting(path)
	if err != nil {
		return 0, err
	}
	return freeBytes(p)
}

func volumeOfExisting(ctx context.Context, path string) (Volume, error) {
	p, err := nearestExisting(path)
	if err != nil {
		return Volume{}, err
	}
	return VolumeOf(ctx, p)
}

// nearestExisting walks up from path to the first ancestor that exists.
func nearestExisting(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", path, err)
	}
	p := abs
	for {
		_, err := os.Lstat(p)
		if err == nil {
			return p, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("stat %q: %w", p, err)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return "", fmt.Errorf("no existing ancestor of %q: %w", path, err)
		}
		p = parent
	}
}
