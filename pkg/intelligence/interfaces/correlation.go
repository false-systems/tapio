package interfaces

import (
	"context"

	"github.com/yairfalse/tapio/pkg/domain"
)

// CorrelationEngine defines the interface for semantic correlation functionality
type CorrelationEngine interface {
	// Start initializes the correlation engine
	Start() error

	// Stop gracefully shuts down the correlation engine
	Stop() error

	// ProcessEvent processes a single event for correlation
	ProcessEvent(ctx context.Context, event *domain.UnifiedEvent) error

	// GetLatestFindings returns the most recent correlation findings
	GetLatestFindings() *Finding

	// GetSemanticGroups returns current semantic groups
	GetSemanticGroups() []*SemanticGroup
}

// SemanticTracer defines the interface for semantic tracing
type SemanticTracer interface {
	// TraceEvent traces an event with semantic context
	TraceEvent(ctx context.Context, event *domain.UnifiedEvent) error

	// GetSemanticGroups returns current semantic groups
	GetSemanticGroups() []*SemanticGroup

	// GetTraceContext returns trace context for an event
	GetTraceContext(eventID string) *TraceContext
}

// Finding represents a correlation finding
type Finding struct {
	ID            string
	Confidence    float64
	PatternType   string
	Description   string
	RelatedEvents []*domain.Event
	SemanticGroup *SemanticGroup
}

// SemanticGroup represents a semantic grouping of events
type SemanticGroup struct {
	ID     string
	Intent string
	Type   string
}

// TraceContext represents trace context information
type TraceContext struct {
	TraceID  string
	SpanID   string
	ParentID string
}
