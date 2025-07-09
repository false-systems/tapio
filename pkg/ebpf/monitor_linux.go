//go:build linux && ebpf
// +build linux,ebpf

package ebpf

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// linuxMonitor provides eBPF monitoring for Linux systems
type linuxMonitor struct {
	config    *Config
	collector *Collector
	mu        sync.RWMutex
	running   bool
}

func init() {
	// Override the default monitor constructor for Linux with eBPF
	NewMonitor = func(config *Config) Monitor {
		if config == nil {
			config = &Config{
				Enabled:         false,
				EventBufferSize: 1000,
				RetentionPeriod: "5m",
			}
		}
		return &linuxMonitor{
			config: config,
		}
	}
}

func (m *linuxMonitor) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.config.Enabled {
		return fmt.Errorf("eBPF monitoring disabled in config")
	}

	if m.running {
		return fmt.Errorf("eBPF monitoring already running")
	}

	// Create collector
	collector, err := NewCollector()
	if err != nil {
		return fmt.Errorf("failed to create eBPF collector: %w", err)
	}

	m.collector = collector
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

	if m.collector != nil {
		m.collector.Close()
	}

	m.running = false
	return nil
}

func (m *linuxMonitor) IsAvailable() bool {
	return m.config != nil && m.config.Enabled
}

func (m *linuxMonitor) GetMemoryStats() ([]ProcessMemoryStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.running || m.collector == nil {
		return nil, fmt.Errorf("eBPF monitoring not running")
	}

	// Get stats from collector
	return m.collector.GetMemoryStats()
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