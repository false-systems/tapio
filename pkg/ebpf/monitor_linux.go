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
	config    *Config
	collector *Collector
	mu        sync.RWMutex
	running   bool
	ctx       context.Context
	cancel    context.CancelFunc
	lastError error
}

// NewMonitor creates a new eBPF monitor on Linux
func NewMonitor(config *Config) Monitor {
	if config == nil {
		config = DefaultConfig()
	}

	return &linuxMonitor{
		config: config,
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

	// Create collector
	collector, err := NewCollector()
	if err != nil {
		m.lastError = fmt.Errorf("failed to create eBPF collector: %w", err)
		return m.lastError
	}

	m.collector = collector
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.running = true

	// Start monitoring in background
	go m.monitorLoop(ctx)

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

	if m.collector != nil {
		m.collector.Close()
	}

	m.running = false
	return nil
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

	if !m.running || m.collector == nil {
		return nil, fmt.Errorf("eBPF monitoring not running")
	}

	// Get stats from collector - convert from map to slice
	statsMap := m.collector.GetProcessStats()
	stats := make([]ProcessMemoryStats, 0, len(statsMap))
	for _, stat := range statsMap {
		stats = append(stats, *stat)
	}

	return stats, nil
}

func (m *linuxMonitor) GetMemoryPredictions(limits map[uint32]uint64) (map[uint32]*OOMPrediction, error) {
	if m.collector == nil {
		return nil, fmt.Errorf("eBPF monitor not started")
	}

	return m.collector.GetMemoryPredictions(limits), nil
}

func (m *linuxMonitor) GetLastError() error {
	return m.lastError
}

// CollectEvents triggers manual event collection
func (m *linuxMonitor) CollectEvents() {
	// This is normally handled by the automatic monitoring loop
	// but we provide this method for compatibility
}

func (m *linuxMonitor) monitorLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !m.running {
				return
			}
			// Collect events periodically
			m.collector.CollectEvents()
		}
	}
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
