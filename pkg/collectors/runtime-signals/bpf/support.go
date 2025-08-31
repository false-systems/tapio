package bpf

import "runtime"

// IsSupported checks if eBPF is supported on this platform
func IsSupported() bool {
	// eBPF is only supported on Linux
	return runtime.GOOS == "linux"
}

// Export generated types for runtime monitoring
type RuntimemonitorObjects = runtimemonitorObjects
type RuntimemonitorMaps = runtimemonitorMaps
type RuntimemonitorPrograms = runtimemonitorPrograms

// Export the generated loader functions
var LoadRuntimemonitor = loadRuntimemonitor
var LoadRuntimemonitorObjects = loadRuntimemonitorObjects