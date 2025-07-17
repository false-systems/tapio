package patterns

import (
	"context"
	"sync"
	"time"

	"github.com/falseyair/tapio/pkg/correlation/types"
)

// BaseDetector provides base implementation for PatternDetector interface
type BaseDetector struct {
	id          string
	name        string
	description string
	category    types.Category
	config      types.PatternConfig

	// Performance metrics
	accuracy          float64
	falsePositiveRate float64
	latency           time.Duration
	mu                sync.RWMutex
}

// NewBaseDetector creates a new base detector
func NewBaseDetector(id, name, description string, category types.Category) *BaseDetector {
	return &BaseDetector{
		id:          id,
		name:        name,
		description: description,
		category:    category,
		config:      DefaultPatternConfig(),
	}
}

// ID returns the pattern ID
func (b *BaseDetector) ID() string { return b.id }

// Name returns the pattern name
func (b *BaseDetector) Name() string { return b.name }

// Description returns the pattern description
func (b *BaseDetector) Description() string { return b.description }

// Category returns the pattern category
func (b *BaseDetector) Category() types.Category { return b.category }

// TimeWindow returns the required time window for detection
func (b *BaseDetector) TimeWindow() time.Duration {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.config.EventWindow
}

// RequiredEventTypes returns the event types this pattern needs
func (b *BaseDetector) RequiredEventTypes() []string {
	// Default implementation - should be overridden by specific detectors
	return []string{}
}

// RequiredMetricTypes returns the metric types this pattern needs
func (b *BaseDetector) RequiredMetricTypes() []string {
	// Default implementation - should be overridden by specific detectors
	return []string{}
}

// Detect must be implemented by specific detectors
func (b *BaseDetector) Detect(ctx context.Context, events []types.Event, metrics map[string]types.MetricSeries) (*types.PatternResult, error) {
	// This must be overridden by specific detectors
	return nil, nil
}
