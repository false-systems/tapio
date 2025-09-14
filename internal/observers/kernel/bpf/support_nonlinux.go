//go:build !linux
// +build !linux

package bpf

// IsSupported checks if eBPF is supported on this platform
func IsSupported() bool {
	// eBPF is only supported on Linux
	return false
}
