package ebpf

import (
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/ebpf/core"
	"github.com/yairfalse/tapio/pkg/collectors/ebpf/internal"
)

// Collector is the public interface for the eBPF collector
type Collector = core.Collector

// Config is the public configuration type
type Config = core.Config

// Health is the public health status type
type Health = core.Health

// Statistics is the public statistics type
type Statistics = core.Statistics

// HealthStatus constants
const (
	HealthStatusHealthy   = core.HealthStatusHealthy
	HealthStatusDegraded  = core.HealthStatusDegraded
	HealthStatusUnhealthy = core.HealthStatusUnhealthy
	HealthStatusUnknown   = core.HealthStatusUnknown
)

// NewCollector creates a new eBPF collector with the given configuration
func NewCollector(config Config) (Collector, error) {
	return internal.NewCollector(config)
}

// DefaultConfig returns a default configuration with advanced features
func DefaultConfig() Config {
	return Config{
		Name:               "ebpf-collector",
		Enabled:            true,
		EventBufferSize:    10000, // Increased from 1000
		EnableNetwork:      true,
		EnableMemory:       true,
		EnableProcess:      true,
		EnableFile:         false,
		RingBufferSize:     65536,                  // Increased from 8192 for better performance
		EventRateLimit:     10000,                  // Increased from 1000
		BatchSize:          100,                    // New: process events in batches
		CollectionInterval: 100 * time.Millisecond, // New: batch collection interval
		MaxEventsPerSecond: 10000,                  // New: rate limiting
		Programs:           []core.ProgramSpec{},   // New: configured by user
		Filter:             core.Filter{},          // New: no filtering by default
		RetentionPeriod:    "24h",
		Timeout:            30 * time.Second, // New: operation timeout
	}
}
