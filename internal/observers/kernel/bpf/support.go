//go:build linux
// +build linux

package bpf

import "runtime"

// IsSupported checks if eBPF is supported on this platform
func IsSupported() bool {
	// eBPF is only supported on Linux
	return runtime.GOOS == "linux"
}

// Export generated types for kernel monitoring
type KernelmonitorObjects = kernelmonitorObjects
type KernelmonitorMaps = kernelmonitorMaps
type KernelmonitorPrograms = kernelmonitorPrograms

// Export the generated loader functions
var LoadKernelmonitor = loadKernelmonitor
var LoadKernelmonitorObjects = loadKernelmonitorObjects
