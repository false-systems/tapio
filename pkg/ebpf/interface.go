package ebpf

import (
	"context"
	"errors"
	"time"
)

// ErrNotSupported is returned when eBPF is not supported on the platform
var ErrNotSupported = errors.New("eBPF not supported on this platform")

// ErrNotEnabled is returned when eBPF is disabled
var ErrNotEnabled = errors.New("eBPF monitoring is disabled")

// Monitor defines the interface for eBPF monitoring
type Monitor interface {
	// Start begins eBPF monitoring
	Start(ctx context.Context) error

	// Stop gracefully stops monitoring
	Stop() error

	// GetMemoryStats returns current memory statistics
	GetMemoryStats() ([]ProcessMemoryStats, error)

	// GetMemoryPredictions returns OOM predictions
	GetMemoryPredictions(limits map[uint32]uint64) (map[uint32]*OOMPrediction, error)

	// IsAvailable checks if eBPF is available on this system
	IsAvailable() bool

	// GetLastError returns the last error encountered
	GetLastError() error
}

// Config holds eBPF monitor configuration
type Config struct {
	Enabled         bool
	EventBufferSize int
	RetentionPeriod string
}

// DefaultConfig returns default eBPF configuration
func DefaultConfig() *Config {
	return &Config{
		Enabled:         false, // Disabled by default
		EventBufferSize: 1000,
		RetentionPeriod: "5m",
	}
}

// ProcessMemoryStats represents memory statistics for a process
type ProcessMemoryStats struct {
	PID            uint32                 `json:"pid"`
	Command        string                 `json:"command"`
	TotalAllocated uint64                 `json:"total_allocated"`
	TotalFreed     uint64                 `json:"total_freed"`
	CurrentUsage   uint64                 `json:"current_usage"`
	AllocationRate float64                `json:"allocation_rate"` // bytes per second
	LastUpdate     time.Time              `json:"last_update"`
	InContainer    bool                   `json:"in_container"`
	ContainerPID   uint32                 `json:"container_pid"`
	GrowthPattern  []MemoryDataPoint      `json:"growth_pattern"`
}

// MemoryDataPoint represents a single memory measurement
type MemoryDataPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Usage     uint64    `json:"usage"`
}

// OOMPrediction represents an out-of-memory prediction
type OOMPrediction struct {
	PID              uint32        `json:"pid"`
	TimeToOOM        time.Duration `json:"time_to_oom"`
	Confidence       float64       `json:"confidence"`
	CurrentUsage     uint64        `json:"current_usage"`
	MemoryLimit      uint64        `json:"memory_limit"`
	PredictedPeakUsage uint64      `json:"predicted_peak_usage"`
}

// NewMonitor creates a new eBPF monitor
func NewMonitor(config *Config) Monitor {
	if config == nil {
		config = DefaultConfig()
	}
	return &stubMonitor{lastError: ErrNotSupported}
}
