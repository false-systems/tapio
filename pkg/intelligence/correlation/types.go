package correlation

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

// Engine orchestrates all correlators
type IEngine interface {
	// Process an event through all correlators
	Process(ctx context.Context, event *domain.UnifiedEvent) error

	// Get correlation results channel
	Results() <-chan *CorrelationResult

	// Start the engine
	Start(ctx context.Context) error

	// Stop the engine
	Stop() error
}

// Dependency represents a correlator dependency
type Dependency struct {
	Name        string
	Type        string
	Description string
	Required    bool
	HealthCheck func(context.Context) error
}
