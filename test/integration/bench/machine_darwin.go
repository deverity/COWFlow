package bench

import "golang.org/x/sys/unix"

func hostCPU() string {
	s, err := unix.Sysctl("machdep.cpu.brand_string")
	if err != nil {
		return ""
	}
	return s
}

func hostRAMBytes() uint64 {
	n, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return 0
	}
	return n
}
