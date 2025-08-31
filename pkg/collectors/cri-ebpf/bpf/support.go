package bpf

import "runtime"

// IsSupported checks if eBPF is supported on this platform
func IsSupported() bool {
	// eBPF is only supported on Linux
	return runtime.GOOS == "linux"
}

// Export generated types for CRI monitoring
type CrimonitorObjects = crimonitorObjects
type CrimonitorMaps = crimonitorMaps
type CrimonitorPrograms = crimonitorPrograms

// Export the generated loader functions
var LoadCrimonitor = loadCrimonitor
var LoadCrimonitorObjects = loadCrimonitorObjects