package bpf

import "runtime"

// IsSupported checks if eBPF is supported on this platform
func IsSupported() bool {
	// eBPF is only supported on Linux
	return runtime.GOOS == "linux"
}

// Export generated types for memory monitoring
type MemorymonitorObjects = memorymonitorObjects
type MemorymonitorMaps = memorymonitorMaps
type MemorymonitorPrograms = memorymonitorPrograms

// Export the generated loader functions
var LoadMemorymonitor = loadMemorymonitor
var LoadMemorymonitorObjects = loadMemorymonitorObjects