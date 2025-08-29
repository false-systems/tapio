//go:build linux
// +build linux

package bpf

import "runtime"

// IsSupported checks if eBPF is supported on this platform
func IsSupported() bool {
	// eBPF is only supported on Linux
	return runtime.GOOS == "linux"
}

// Export generated types for resource starvation monitoring
type StarvationmonitorObjects = starvationmonitorObjects
type StarvationmonitorMaps = starvationmonitorMaps
type StarvationmonitorPrograms = starvationmonitorPrograms

// Export the generated loader functions
var LoadStarvationmonitor = loadStarvationmonitor
var LoadStarvationmonitorObjects = loadStarvationmonitorObjects
