//go:build linux && ebpf
// +build linux,ebpf

package ebpf

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"
)

// linuxMonitor provides eBPF monitoring for Linux systems
type linuxMonitor struct {
	config           *Config
	collectorManager *internalCollectorManager
	mu               sync.RWMutex
	running          bool
	ctx              context.Context
	cancel           context.CancelFunc
	lastError        error
}

// NewMonitor creates a new eBPF monitor on Linux
func NewMonitor(config *Config) Monitor {
	if config == nil {
		config = DefaultConfig()
	}

	return &linuxMonitor{
		config:           config,
		collectorManager: newInternalCollectorManager(),
	}
}

func (m *linuxMonitor) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.config.Enabled {
		m.lastError = ErrNotEnabled
		return ErrNotEnabled
	}

	if m.running {
		return fmt.Errorf("eBPF monitoring already running")
	}

	// Check if running as root or with CAP_BPF
	if !m.hasRequiredPermissions() {
		m.lastError = fmt.Errorf("eBPF requires root or CAP_BPF capability")
		return m.lastError
	}

	// Initialize collectors - this is now handled externally to avoid import cycles
	// Collectors should be registered through the pkg/collectors package instead

	m.ctx, m.cancel = context.WithCancel(ctx)

	// Start collectors
	if err := m.collectorManager.Start(m.ctx); err != nil {
		m.lastError = fmt.Errorf("failed to start collectors: %w", err)
		return m.lastError
	}

	m.running = true

	return nil
}

func (m *linuxMonitor) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return nil
	}

	if m.cancel != nil {
		m.cancel()
	}

	// Stop collectors
	if err := m.collectorManager.Stop(); err != nil {
		m.lastError = err
	}

	m.running = false
	return m.lastError
}

func (m *linuxMonitor) IsAvailable() bool {
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

func (m *linuxMonitor) GetMemoryStats() ([]ProcessMemoryStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.running {
		return nil, fmt.Errorf("eBPF monitoring not running")
	}

	// Get stats from memory collector
	memCollector, exists := m.collectorManager.GetCollector("memory")
	if !exists {
		return nil, fmt.Errorf("memory collector not available")
	}

	// TODO: Add GetProcessStats method to memory collector interface
	// For now, return empty stats
	return []ProcessMemoryStats{}, nil
}

func (m *linuxMonitor) GetMemoryPredictions(limits map[uint32]uint64) (map[uint32]*OOMPrediction, error) {
	if !m.running {
		return nil, fmt.Errorf("eBPF monitoring not running")
	}

	// Get predictions from memory collector
	memCollector, exists := m.collectorManager.GetCollector("memory")
	if !exists {
		return nil, fmt.Errorf("memory collector not available")
	}

	// TODO: Add GetMemoryPredictions method to memory collector interface
	// For now, return empty predictions
	return map[uint32]*OOMPrediction{}, nil
}

func (m *linuxMonitor) GetLastError() error {
	return m.lastError
}

// CollectEvents triggers manual event collection
func (m *linuxMonitor) CollectEvents() {
	// Events are collected automatically by the collectors
	// This method is a no-op for compatibility
}

// hasRequiredPermissions checks if we have the necessary permissions for eBPF
func (m *linuxMonitor) hasRequiredPermissions() bool {
	// Check if running as root
	if os.Geteuid() == 0 {
		return true
	}

	// TODO: Check for CAP_BPF capability
	// For now, require root
	return false
}

// hasMinimumKernel checks if kernel version supports eBPF
func (m *linuxMonitor) hasMinimumKernel() bool {
	// TODO: Implement actual kernel version check
	// For now, assume it's supported on Linux
	return true
}
