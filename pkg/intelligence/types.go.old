package intelligence_v2

import (
	"context"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// CorrelationResult represents a discovered correlation between events
type CorrelationResult struct {
	ID         string
	Type       string // k8s_ownership, temporal_pattern, sequence_match
	Confidence float64
	Events     []string // Event IDs involved
	TraceID    string   // Trace ID if available
	RootCause  *RootCause
	Impact     *Impact
	Summary    string
	Details    string
	Evidence   []string
	StartTime  time.Time
	EndTime    time.Time
}

// RootCause identifies the source of the issue
type RootCause struct {
	EventID     string
	Confidence  float64
	Description string
	Evidence    []string
}

// Impact describes what's affected
type Impact struct {
	Severity  domain.EventSeverity
	Resources []string // Affected K8s resources
	Services  []string // Affected services
}

// Correlator processes events and finds correlations
type Correlator interface {
	// Process an event and return any correlations found
	Process(ctx context.Context, event *domain.UnifiedEvent) ([]*CorrelationResult, error)

	// Name returns the correlator name
	Name() string
}

// Storage persists and retrieves correlations
type Storage interface {
	// Store a correlation result
	Store(ctx context.Context, result *CorrelationResult) error

	// Get recent correlations
	GetRecent(ctx context.Context, limit int) ([]*CorrelationResult, error)

	// Get correlations by trace ID
	GetByTraceID(ctx context.Context, traceID string) ([]*CorrelationResult, error)

	// Clean up old correlations
	Cleanup(ctx context.Context, olderThan time.Duration) error
}

// Engine orchestrates all correlators
type Engine interface {
	// Process an event through all correlators
	Process(ctx context.Context, event *domain.UnifiedEvent) error

	// Get correlation results channel
	Results() <-chan *CorrelationResult

	// Start the engine
	Start(ctx context.Context) error

	// Stop the engine
	Stop() error
}

// NATSSubscriber handles NATS integration
type NATSSubscriber interface {
	// Start subscribing to NATS
	Start(ctx context.Context) error

	// Stop the subscriber
	Stop() error
}
