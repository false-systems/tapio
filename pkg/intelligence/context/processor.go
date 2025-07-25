package context

import (
	"context"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/interfaces"
)

// ContextProcessor provides unified event context processing
type ContextProcessor struct {
	validator      *EventValidator
	impactAnalyzer *ImpactAnalyzer
	scorer         *ConfidenceScorer
}

// NewContextProcessor creates a new context processor with default configuration
func NewContextProcessor() interfaces.ContextProcessor {
	return &ContextProcessor{
		validator:      NewEventValidator(),
		impactAnalyzer: NewImpactAnalyzer(),
		scorer:         NewConfidenceScorer(),
	}
}

// Validate validates an event's context
func (cp *ContextProcessor) Validate(ctx context.Context, event *domain.UnifiedEvent) error {
	// Use the existing validator - adapt the signature
	return cp.validator.Validate(event)
}

// Score calculates a confidence score for an event
func (cp *ContextProcessor) Score(ctx context.Context, event *domain.UnifiedEvent) float64 {
	// Use the existing scorer - adapt the signature
	return cp.scorer.CalculateConfidence(event)
}

// AssessImpact assesses the business impact of an event
func (cp *ContextProcessor) AssessImpact(ctx context.Context, event *domain.UnifiedEvent) (*interfaces.ImpactResult, error) {
	// Use the existing impact analyzer and convert the result
	impactCtx := cp.impactAnalyzer.AssessImpact(event)
	if impactCtx == nil {
		return &interfaces.ImpactResult{
			BusinessImpact:     0.0,
			TechnicalSeverity:  "unknown",
			CascadeRisk:        0.0,
			AffectedServices:   []string{},
			RecommendedActions: []string{},
		}, nil
	}

	// Convert domain.ImpactContext to interfaces.ImpactResult
	// Calculate cascade risk based on impact characteristics
	cascadeRisk := 0.0
	if impactCtx.SLOImpact {
		cascadeRisk += 0.3
	}
	if impactCtx.CustomerFacing {
		cascadeRisk += 0.3
	}
	if impactCtx.RevenueImpacting {
		cascadeRisk += 0.4
	}

	// Generate recommended actions based on impact assessment
	recommendedActions := []string{}
	if impactCtx.BusinessImpact > 0.8 {
		recommendedActions = append(recommendedActions, "Escalate to on-call engineer")
	}
	if impactCtx.SLOImpact {
		recommendedActions = append(recommendedActions, "Check SLA dashboard")
	}
	if impactCtx.CustomerFacing {
		recommendedActions = append(recommendedActions, "Update status page")
	}
	if impactCtx.RevenueImpacting {
		recommendedActions = append(recommendedActions, "Notify business stakeholders")
	}
	if len(recommendedActions) == 0 {
		recommendedActions = append(recommendedActions, "Monitor for escalation")
	}

	return &interfaces.ImpactResult{
		BusinessImpact:     impactCtx.BusinessImpact,
		TechnicalSeverity:  impactCtx.Severity,
		CascadeRisk:        cascadeRisk,
		AffectedServices:   impactCtx.AffectedServices,
		RecommendedActions: recommendedActions,
	}, nil
}