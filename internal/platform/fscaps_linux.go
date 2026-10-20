//go:build linux

package platform

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// Volume reports the filesystem of the volume holding path. Only btrfs is
// reported as block-cloning: XFS reflink support cannot be read from statfs,
// so XFS reports BlockClone=false and the S1-E3 probe clone confirms it.
func (h *host) Volume(ctx context.Context, path string) (VolumeInfo, error) {
	if err := ctx.Err(); err != nil {
		return VolumeInfo{}, err
	}
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return VolumeInfo{}, fmt.Errorf("statfs %q: %w", path, err)
	}
	fs := fsNameFromMagic(int64(st.Type)) //nolint:unconvert // Type is int32 on some arches
	return VolumeInfo{
		Path:       mountPointOf(path),
		Filesystem: fs,
		BlockClone: fs == "btrfs",
	}, nil
}

// volumeOf fills a [Volume] from statfs(2) and the mount root's st_dev.
// BlockRefcounting is a hint: btrfs and xfs may reflink, the probe decides.
func volumeOf(_ context.Context, path string) (Volume, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return Volume{}, fmt.Errorf("statfs %q: %w", path, err)
	}
	name := fsNameFromMagic(int64(st.Type)) //nolint:unconvert // Type is int32 on some arches
	mount := mountPointOf(path)
	var ms unix.Stat_t
	if err := unix.Stat(mount, &ms); err != nil {
		return Volume{}, fmt.Errorf("stat mount root %q: %w", mount, err)
	}
	return Volume{
		Filesystem:       name,
		ID:               strconv.FormatUint(uint64(ms.Dev), 16), //nolint:unconvert // Dev width varies
		MountPath:        mount,
		BlockRefcounting: name == "btrfs" || name == "xfs",
	}, nil
}

// fsNameFromMagic maps a statfs f_type magic number to a filesystem name.
func fsNameFromMagic(magic int64) string {
	switch magic {
	case unix.BTRFS_SUPER_MAGIC:
		return "btrfs"
	case unix.XFS_SUPER_MAGIC:
		return "xfs"
	case unix.EXT4_SUPER_MAGIC: // shared by ext2, ext3 and ext4
		return "ext4"
	case unix.TMPFS_MAGIC:
		return "tmpfs"
	case unix.OVERLAYFS_SUPER_MAGIC:
		return "overlayfs"
	case unix.NFS_SUPER_MAGIC:
		return "nfs"
	case unix.F2FS_SUPER_MAGIC:
		return "f2fs"
	case unix.MSDOS_SUPER_MAGIC:
		return "vfat"
	default:
		return fmt.Sprintf("unknown(0x%x)", magic)
	}
}

// mountPointOf returns the deepest mount point containing path according to
// /proc/self/mountinfo, or the absolute path when it cannot be determined.
func mountPointOf(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return abs
	}
	defer func() { _ = f.Close() }()
	if mp := deepestMount(f, abs); mp != "" {
		return mp
	}
	return abs
}

func deepestMount(r io.Reader, abs string) string {
	best := ""
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 5 {
			continue
		}
		mp := unescapeMountField(fields[4])
		if isUnder(mp, abs) && len(mp) > len(best) {
			best = mp
		}
	}
	return best
}

func isUnder(mp, p string) bool {
	if mp == "/" {
		return true
	}
	return p == mp || strings.HasPrefix(p, mp+"/")
}

// unescapeMountField decodes the octal escapes (\040 for space, ...) used in
// mountinfo.
func unescapeMountField(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			var v int
			ok := true
			for _, c := range s[i+1 : i+4] {
				if c < '0' || c > '7' {
					ok = false
					break
				}
				v = v*8 + int(c-'0')
			}
			if ok {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
