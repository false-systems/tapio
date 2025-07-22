package interfaces

import (
	"context"

	"github.com/yairfalse/tapio/pkg/domain"
)

// ContextProcessor defines the interface for context validation and scoring
type ContextProcessor interface {
	// Validate validates an event's context
	Validate(ctx context.Context, event *domain.UnifiedEvent) error

	// Score calculates a confidence score for an event
	Score(ctx context.Context, event *domain.UnifiedEvent) float64

	// AssessImpact assesses the business impact of an event
	AssessImpact(ctx context.Context, event *domain.UnifiedEvent) (*ImpactResult, error)
}

// ImpactResult represents impact assessment results
type ImpactResult struct {
	BusinessImpact     float64
	TechnicalSeverity  string
	CascadeRisk        float64
	AffectedServices   []string
	RecommendedActions []string
}
