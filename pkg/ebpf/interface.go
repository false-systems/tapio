package ebpf

import (
	"context"
	"errors"
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
	GetMemoryStats() (map[uint32]*ProcessMemoryStats, error)

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
