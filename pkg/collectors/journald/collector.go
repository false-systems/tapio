package journald

import (
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/journald/core"
	"github.com/yairfalse/tapio/pkg/collectors/journald/internal"
)

// Collector is the public interface for the journald collector
type Collector = core.Collector

// Config is the public configuration type
type Config = core.Config

// Health is the public health status type
type Health = core.Health

// Statistics is the public statistics type
type Statistics = core.Statistics

// DefaultConfig returns the default configuration
func DefaultConfig() Config {
	return Config{
		Name:             "journald-collector",
		BufferSize:       1000,
		ProcessInterval:  100 * time.Millisecond,
		MaxBatchSize:     100,
		IncludeSystemd:   true,
		IncludeKernel:    true,
		IncludeUserUnits: false,
		Since:            "1h",
		Matches:          []string{},
	}
}

// NewCollector creates a new journald collector
func NewCollector(config Config) (Collector, error) {
	return internal.NewCollector(config)
}

