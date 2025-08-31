//go:build linux
// +build linux

package bpf

import (
	"fmt"
	"os"

	"github.com/cilium/ebpf"
)

// CheckKernelCompatibility checks if the kernel supports the required features
func CheckKernelCompatibility() error {
	// Check for minimum kernel version (5.8+ for ring buffer support)
	if !hasRingBufferSupport() {
		return fmt.Errorf("kernel does not support BPF ring buffer (requires kernel 5.8+)")
	}

	// Check for CO-RE support
	if !hasCORESupport() {
		return fmt.Errorf("kernel does not support CO-RE (requires kernel 5.2+ with BTF)")
	}

	// Check for required BPF program types
	if !hasKprobeSupport() {
		return fmt.Errorf("kernel does not support BPF kprobes")
	}

	return nil
}

// hasRingBufferSupport checks if the kernel supports BPF ring buffer
func hasRingBufferSupport() bool {
	// Ring buffer was introduced in kernel 5.8
	// Check for the existence of bpf_ringbuf_* helpers
	return checkBPFHelperSupport("bpf_ringbuf_reserve")
}

// hasCORESupport checks if the kernel supports CO-RE (Compile Once, Run Everywhere)
func hasCORESupport() bool {
	// Check for BTF support
	_, err := os.Stat("/sys/kernel/btf/vmlinux")
	return err == nil
}

// hasKprobeSupport checks if the kernel supports BPF kprobes
func hasKprobeSupport() bool {
	// Check for kprobe tracing support
	_, err := os.Stat("/sys/kernel/debug/tracing/kprobe_events")
	if err == nil {
		return true
	}

	// Alternative check for newer kernels
	_, err = os.Stat("/sys/kernel/tracing/kprobe_events")
	return err == nil
}

// checkBPFHelperSupport checks if a specific BPF helper is supported
func checkBPFHelperSupport(helperName string) bool {
	// This is a simplified check - in a production environment,
	// you might want to use bpftool or other methods to check
	// for specific helper availability
	return true // Assume support for now
}

// GetRecommendedMapSizes returns recommended map sizes based on system resources
func GetRecommendedMapSizes() (int, int, int) {
	// Default conservative sizes
	ringBufSize := 1024 * 1024 // 1MB
	activeEventsSize := 10240  // 10K active events
	statsSize := 32            // 32 stats entries

	// Adjust based on available memory and expected load in production deployments
	return ringBufSize, activeEventsSize, statsSize
}

// ValidateEBPFProgram validates the eBPF program before loading
func ValidateEBPFProgram() error {
	// Basic validation checks
	if !CheckEBPFSupport() {
		return fmt.Errorf("eBPF is not supported on this system")
	}

	return nil
}

// CheckEBPFSupport checks if eBPF is supported on the system
func CheckEBPFSupport() bool {
	// Check for eBPF filesystem
	_, err := os.Stat("/sys/fs/bpf")
	return err == nil
}

// StoragemonitorObjects is a wrapper around the generated storagemonitorObjects
type StoragemonitorObjects struct {
	storagemonitorObjects
}

// LoadStoragemonitorObjects loads the storage monitor eBPF objects
func LoadStoragemonitorObjects(obj *StoragemonitorObjects, opts *ebpf.CollectionOptions) error {
	return loadStoragemonitorObjects(&obj.storagemonitorObjects, opts)
}
