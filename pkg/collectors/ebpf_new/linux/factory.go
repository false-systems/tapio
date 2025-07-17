//go:build linux
// +build linux

package linux

import (
	"fmt"
	"os"
	"runtime"

	"github.com/yairfalse/tapio/pkg/collectors/ebpf_new/core"
	"github.com/yairfalse/tapio/pkg/collectors/ebpf_new/internal"
)

// NewCollector creates a new Linux eBPF collector
func NewCollector(config core.Config) (core.Collector, error) {
	// Verify we're on Linux
	if runtime.GOOS != "linux" {
		return nil, core.NotSupportedError{
			Feature:  "eBPF collector",
			Platform: runtime.GOOS,
			Reason:   "eBPF is only supported on Linux",
		}
	}

	// Check for required permissions
	if os.Geteuid() != 0 {
		return nil, core.PermissionError{
			Operation:   "create eBPF collector",
			Requirement: "root privileges or CAP_BPF capability",
		}
	}

	// Create components
	loader, err := NewProgramLoader()
	if err != nil {
		return nil, fmt.Errorf("failed to create program loader: %w", err)
	}

	parser := NewEventParser()
	manager := NewMapManager()

	// Create collector using internal implementation
	collector, err := internal.NewCollector(config, loader, parser, manager)
	if err != nil {
		return nil, fmt.Errorf("failed to create collector: %w", err)
	}

	return &linuxCollector{
		Collector:  collector,
		loader:     loader,
		parser:     parser,
		manager:    manager,
	}, nil
}

// linuxCollector wraps the internal collector with Linux-specific functionality
type linuxCollector struct {
	core.Collector
	loader  core.ProgramLoader
	parser  core.EventParser
	manager core.MapManager
}

// Additional Linux-specific methods can be added here if needed

// CreateRingBufferReader creates a ring buffer reader for the specified map
func (lc *linuxCollector) CreateRingBufferReader(mapName string) (core.RingBufferReader, error) {
	// Get the map
	mapHandle, err := lc.manager.GetMap(mapName)
	if err != nil {
		return nil, fmt.Errorf("failed to get map %s: %w", mapName, err)
	}

	// We need access to the underlying eBPF map
	// This is a limitation of our abstraction - in a real implementation,
	// we'd need to expose this through the interface or handle it differently
	return nil, fmt.Errorf("ring buffer reader creation requires platform-specific implementation")
}

// GetKernelVersion returns the Linux kernel version
func GetKernelVersion() (string, error) {
	// Read from /proc/version
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return "", fmt.Errorf("failed to read kernel version: %w", err)
	}

	return string(data), nil
}

// CheckBPFSupport checks if BPF is supported on this system
func CheckBPFSupport() error {
	// Check if BPF syscall is available
	// This is a simplified check - a real implementation would be more thorough
	if _, err := os.Stat("/sys/fs/bpf"); os.IsNotExist(err) {
		return fmt.Errorf("BPF filesystem not mounted at /sys/fs/bpf")
	}

	return nil
}