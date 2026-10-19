//go:build linux

package platform

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestFsNameFromMagic(t *testing.T) {
	require.Equal(t, "btrfs", fsNameFromMagic(unix.BTRFS_SUPER_MAGIC))
	require.Equal(t, "xfs", fsNameFromMagic(unix.XFS_SUPER_MAGIC))
	require.Equal(t, "ext4", fsNameFromMagic(unix.EXT4_SUPER_MAGIC))
	require.Equal(t, "unknown(0x1234)", fsNameFromMagic(0x1234))
}

func TestIsUnder(t *testing.T) {
	require.True(t, isUnder("/", "/a/b"))
	require.True(t, isUnder("/a", "/a"))
	require.True(t, isUnder("/a", "/a/b"))
	require.False(t, isUnder("/a", "/ab"))
}

func TestUnescapeMountField(t *testing.T) {
	require.Equal(t, "/mnt/my disk", unescapeMountField(`/mnt/my\040disk`))
	require.Equal(t, "/plain", unescapeMountField("/plain"))
}

func TestDeepestMount(t *testing.T) {
	info := "1 0 8:1 / / rw - ext4 /dev/sda1 rw\n" +
		"2 1 8:2 / /mnt/my\\040disk rw - btrfs /dev/sdb rw\n" +
		"3 1 8:3 / /mnt rw - xfs /dev/sdc rw\n"
	require.Equal(t, "/mnt/my disk", deepestMount(strings.NewReader(info), "/mnt/my disk/x"))
	require.Equal(t, "/mnt", deepestMount(strings.NewReader(info), "/mnt/other"))
	require.Equal(t, "/", deepestMount(strings.NewReader(info), "/home"))
}
