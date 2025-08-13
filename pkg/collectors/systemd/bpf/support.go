package bpf

import "runtime"

// IsSupported checks if eBPF is supported on this platform
func IsSupported() bool {
	// eBPF is only supported on Linux
	return runtime.GOOS == "linux"
}

// Export generated types for systemd monitoring
type SystemdMonitorObjects = systemdmonitorObjects
type SystemdMonitorMaps = systemdmonitorMaps
type SystemdMonitorPrograms = systemdmonitorPrograms

// Export the generated loader functions
var LoadSystemdMonitor = loadSystemdmonitor
var LoadSystemdMonitorObjects = loadSystemdmonitorObjects
