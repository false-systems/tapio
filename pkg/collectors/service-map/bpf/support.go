package bpf

import (
	"fmt"
	"runtime"
)

// CheckSupport checks if eBPF is supported on this system
func CheckSupport() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("eBPF only supported on Linux, current OS: %s", runtime.GOOS)
	}
	
	// Additional checks could be added here:
	// - Kernel version check
	// - BPF capability check
	// - Required kernel features
	
	return nil
}