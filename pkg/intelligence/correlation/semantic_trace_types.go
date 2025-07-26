package correlation

import (
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/trace"
)

// SemanticTraceGroup represents events grouped by meaning and causality
type SemanticTraceGroup struct {
	ID                 string
	Intent             string // What is this group trying to achieve?
	SemanticType       string // memory_cascade, network_failure, etc.
	RootCause          *domain.Event
	CausalChain        []*domain.Event
	UnifiedCausalChain []*domain.UnifiedEvent // New field for UnifiedEvents
	ConfidenceScore    float64
	ImpactAssessment   *ImpactAssessment
	PredictedOutcome   *PredictedOutcome
	TraceID            string
	SpanContext        trace.SpanContext
}

// ImpactAssessment assesses business and technical impact
type ImpactAssessment struct {
	BusinessImpact     float32
	TechnicalSeverity  string
	CascadeRisk        float32
	AffectedResources  []string
	TimeToResolution   time.Duration
	RecommendedActions []string
}

// PredictedOutcome predicts what will happen
type PredictedOutcome struct {
	Scenario          string // "cascade_failure", "recovery", etc.
	Probability       float64
	TimeToOutcome     time.Duration
	PreventionActions []string
	ConfidenceLevel   float64
}

// SimpleCausalityTracker tracks causal relationships
type SimpleCausalityTracker struct {
	causalLinks       map[string][]CausalLink
	timeWindow        time.Duration
	strengthThreshold float64
}

// CausalLink represents a causal relationship
type CausalLink struct {
	SourceEventID string
	TargetEventID string
	Strength      float64
	Type          string // "triggers", "causes", "correlates"
}
