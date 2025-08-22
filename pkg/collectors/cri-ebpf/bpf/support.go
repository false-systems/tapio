package bpf

// This file provides support functions for eBPF operations
// It serves as a placeholder for eBPF-related utilities and helpers

import (
	"fmt"
	"os"
	"runtime"

	"github.com/cilium/ebpf"
)

// IsEBPFSupported checks if eBPF is supported on the current system
func IsEBPFSupported() bool {
	// Check if running on Linux
	if runtime.GOOS != "linux" {
		return false
	}

	// Check for eBPF filesystem
	if _, err := os.Stat("/sys/fs/bpf"); err != nil {
		return false
	}

	// Additional capability checks could be added here
	// For now, assume eBPF is available on Linux systems
	return true
}

// CheckBPFCapabilities performs basic capability checks for eBPF
func CheckBPFCapabilities() error {
	if !IsEBPFSupported() {
		return fmt.Errorf("eBPF is not supported on this system")
	}

	// Check for required kernel features
	// This is a simplified check - production code would be more thorough

	return nil
}

// GetBPFProgramPath returns the path to eBPF programs
func GetBPFProgramPath() string {
	return "../bpf_src"
}

// LoadCrimonitor loads the eBPF collection spec (exported version)
func LoadCrimonitor() (*ebpf.CollectionSpec, error) {
	return loadCrimonitor()
}
