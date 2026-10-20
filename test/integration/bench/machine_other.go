//go:build !linux && !darwin && !windows

package bench

func hostCPU() string      { return "" }
func hostRAMBytes() uint64 { return 0 }
