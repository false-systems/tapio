package ebpf

import (
	"context"
	"time"
)

// Monitor provides the interface for eBPF monitoring
type Monitor interface {
	// Start begins eBPF monitoring
	Start(ctx context.Context) error
	
	// Stop terminates eBPF monitoring
	Stop() error
	
	// IsAvailable checks if eBPF monitoring is available on this system
	IsAvailable() bool
	
	// GetMemoryStats returns memory usage statistics from eBPF
	GetMemoryStats() ([]ProcessMemoryStats, error)
}

// Config holds eBPF configuration
type Config struct {
	Enabled         bool   `json:"enabled"`
	EventBufferSize int    `json:"event_buffer_size"`
	RetentionPeriod string `json:"retention_period"`
}

// ProcessMemoryStats represents memory statistics for a process
type ProcessMemoryStats struct {
	PID            uint32                 `json:"pid"`
	Command        string                 `json:"command"`
	CurrentUsage   uint64                 `json:"current_usage"`
	InContainer    bool                   `json:"in_container"`
	GrowthPattern  []MemoryDataPoint      `json:"growth_pattern"`
}

// MemoryDataPoint represents a single memory measurement
type MemoryDataPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Usage     uint64    `json:"usage"`
}

// NewMonitor creates a new eBPF monitor
func NewMonitor(config *Config) Monitor {
	return &stubMonitor{}
}