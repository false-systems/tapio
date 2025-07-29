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

	// AssessImpact assesses the infrastructure impact of an event
	AssessImpact(ctx context.Context, event *domain.UnifiedEvent) (*ImpactResult, error)
}

// ImpactResult represents impact assessment results
type ImpactResult struct {
	InfrastructureImpact float64  // 0.0-1.0 score of infrastructure impact
	TechnicalSeverity    string   // critical, high, medium, low
	CascadeRisk          float64  // 0.0-1.0 risk of cascading failures
	AffectedServices     []string // List of affected service names
	RecommendedActions   []string // Actionable recommendations
}
