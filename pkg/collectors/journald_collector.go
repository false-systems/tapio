package collectors

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/collectors/journald"
	"github.com/yairfalse/tapio/pkg/collectors/types"
)

// JournaldFactory creates journald collectors
type JournaldFactory struct{}

// NewJournaldFactory creates a new journald collector factory
func NewJournaldFactory() CollectorFactory {
	return &JournaldFactory{}
}

// CreateCollector creates a new journald collector instance
func (f *JournaldFactory) CreateCollector(config types.CollectorConfig) (Collector, error) {
	if config.Type != "journald" {
		return nil, fmt.Errorf("invalid collector type: %s (expected: journald)", config.Type)
	}

	// Create the journald collector
	collector, err := journald.NewCollector(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create journald collector: %w", err)
	}

	return collector, nil
}

// Type returns the collector type this factory creates
func (f *JournaldFactory) Type() string {
	return "journald"
}

// DefaultConfig returns the default configuration for journald collectors
func (f *JournaldFactory) DefaultConfig() CollectorConfig {
	return CollectorConfig{
		Name:             "journald",
		Type:             "journald",
		Enabled:          true,
		SamplingRate:     1.0, // Process all events that pass filtering
		EventBufferSize:  10000,
		MaxEventsPerSec:  10000,
		MinSeverity:      SeverityInfo, // Let the OPINIONATED filter decide
		Labels: map[string]string{
			"collector": "journald",
		},
		Extra: map[string]interface{}{
			// OPINIONATED defaults for critical event detection
			"priorities": []string{"0", "1", "2", "3", "4"}, // Emergency through Warning
			"noise_reduction_target": 0.95,                   // 95% noise reduction
			"filter_noisy_units": true,
			"stream_batch_size": 1000,
			"follow_cursor": true,
		},
	}
}

// init registers the journald factory
func init() {
	RegisterFactory("journald", NewJournaldFactory())
}