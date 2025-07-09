//go:build linux && ebpf
// +build linux,ebpf

package ebpf

import (
	"context"
	"fmt"
	"os"
	"time"
)

// LinuxMonitor implements eBPF monitoring on Linux
type LinuxMonitor struct {
	config    *Config
	collector *Collector
	ctx       context.Context
	cancel    context.CancelFunc
	lastError error
}

// NewMonitor creates a new eBPF monitor on Linux
func NewMonitor(config *Config) Monitor {
	if config == nil {
		config = DefaultConfig()
	}

	return &LinuxMonitor{
		config: config,
	}
}

func (m *LinuxMonitor) Start(ctx context.Context) error {
	if !m.config.Enabled {
		m.lastError = ErrNotEnabled
		return ErrNotEnabled
	}

	// Check if running as root or with CAP_BPF
	if !m.hasRequiredPermissions() {
		m.lastError = fmt.Errorf("eBPF requires root or CAP_BPF capability")
		return m.lastError
	}

	// Create collector
	collector, err := NewCollector()
	if err != nil {
		m.lastError = fmt.Errorf("failed to create eBPF collector: %w", err)
		return m.lastError
	}

	m.collector = collector
	m.ctx, m.cancel = context.WithCancel(ctx)

	return nil
}

func (m *LinuxMonitor) Stop() error {
	if m.cancel != nil {
		m.cancel()
	}

	if m.collector != nil {
		return m.collector.Close()
	}

	return nil
}

func (m *LinuxMonitor) GetMemoryStats() (map[uint32]*ProcessMemoryStats, error) {
	if m.collector == nil {
		return nil, fmt.Errorf("eBPF monitor not started")
	}

	return m.collector.GetProcessStats(), nil
}

func (m *LinuxMonitor) GetMemoryPredictions(limits map[uint32]uint64) (map[uint32]*OOMPrediction, error) {
	if m.collector == nil {
		return nil, fmt.Errorf("eBPF monitor not started")
	}

	return m.collector.GetMemoryPredictions(limits), nil
}

func (m *LinuxMonitor) IsAvailable() bool {
	// Check kernel version
	if !m.hasMinimumKernel() {
		return false
	}

	// Check if eBPF is available
	if _, err := os.Stat("/sys/kernel/btf/vmlinux"); os.IsNotExist(err) {
		// BTF not available, but eBPF might still work
	}

	return true
}

func (m *LinuxMonitor) GetLastError() error {
	return m.lastError
}

// hasRequiredPermissions checks if we have the necessary permissions for eBPF
func (m *LinuxMonitor) hasRequiredPermissions() bool {
	// Check if running as root
	if os.Geteuid() == 0 {
		return true
	}

	// TODO: Check for CAP_BPF capability
	// For now, require root
	return false
}

// hasMinimumKernel checks if kernel version supports eBPF
func (m *LinuxMonitor) hasMinimumKernel() bool {
	// TODO: Implement actual kernel version check
	// For now, assume it's supported on Linux
	return true
}
