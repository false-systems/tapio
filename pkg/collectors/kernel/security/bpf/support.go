package bpf

import "runtime"

// IsSupported checks if eBPF is supported on this platform
func IsSupported() bool {
	// eBPF is only supported on Linux
	return runtime.GOOS == "linux"
}

// Export the generated types for external use
type SecuritymonitorObjects = securitymonitorObjects
type SecuritymonitorMaps = securitymonitorMaps
type SecuritymonitorPrograms = securitymonitorPrograms

// Export the generated loader functions
var LoadSecuritymonitor = loadSecuritymonitor
var LoadSecuritymonitorObjects = loadSecuritymonitorObjects
