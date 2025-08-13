package bpf

import "runtime"

// IsSupported checks if eBPF is supported on this platform
func IsSupported() bool {
	// eBPF is only supported on Linux
	return runtime.GOOS == "linux"
}

// Export generated types for systemd monitoring
type SystemdmonitorObjects = systemdMonitorObjects
type SystemdmonitorMaps = systemdMonitorMaps
type SystemdmonitorPrograms = systemdMonitorPrograms

// Export the generated loader functions
var LoadSystemdmonitor = loadSystemdMonitor
var LoadSystemdmonitorObjects = loadSystemdMonitorObjects
