package bpf

import "runtime"

// IsSupported checks if eBPF is supported on this platform
func IsSupported() bool {
	// eBPF is only supported on Linux
	return runtime.GOOS == "linux"
}

// Note: The generated types are in the parent package (runtime), not in bpf package
// They need to be referenced from there
