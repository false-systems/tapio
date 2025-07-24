package adapters

import (
	"context"

	"github.com/yairfalse/tapio/pkg/domain"
	intelligenceContext "github.com/yairfalse/tapio/pkg/intelligence/context"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
	"github.com/yairfalse/tapio/pkg/intelligence/interfaces"
)

// CorrelationEngineAdapter adapts SemanticCorrelationEngine to the CorrelationEngine interface
type CorrelationEngineAdapter struct {
	engine *correlation.SemanticCorrelationEngine
}

// NewCorrelationEngineAdapter creates a new adapter for the correlation engine
func NewCorrelationEngineAdapter() *CorrelationEngineAdapter {
	return &CorrelationEngineAdapter{
		engine: correlation.NewSemanticCorrelationEngine(),
	}
}

// Start initializes the correlation engine
func (a *CorrelationEngineAdapter) Start() error {
	return a.engine.Start()
}

// Stop gracefully shuts down the correlation engine
func (a *CorrelationEngineAdapter) Stop() error {
	return a.engine.Stop()
}

// ProcessEvent processes a single event for correlation
func (a *CorrelationEngineAdapter) ProcessEvent(ctx context.Context, event *domain.UnifiedEvent) error {
	// The underlying engine doesn't return an error, so we just call it
	a.engine.ProcessUnifiedEvent(event)
	return nil
}

// GetLatestFindings returns the most recent correlation findings
func (a *CorrelationEngineAdapter) GetLatestFindings() *interfaces.Finding {
	findings := a.engine.GetLatestFindings()
	if findings == nil {
		return nil
	}

	// Convert internal Finding to interface Finding
	return &interfaces.Finding{
		ID:            findings.ID,
		Confidence:    findings.Confidence,
		PatternType:   findings.PatternType,
		Description:   findings.Description,
		RelatedEvents: findings.RelatedEvents,
		SemanticGroup: convertSemanticGroup(findings.SemanticGroup),
	}
}

// GetSemanticGroups returns current semantic groups
func (a *CorrelationEngineAdapter) GetSemanticGroups() []*interfaces.SemanticGroup {
	groups := a.engine.GetSemanticTracer().GetSemanticGroups()

	result := make([]*interfaces.SemanticGroup, 0, len(groups))
	for _, group := range groups {
		result = append(result, &interfaces.SemanticGroup{
			ID:     group.ID,
			Intent: group.Intent,
			Type:   group.SemanticType,
		})
	}

	return result
}

// convertSemanticGroup converts internal semantic group to interface type
func convertSemanticGroup(group *correlation.SemanticGroupSummary) *interfaces.SemanticGroup {
	if group == nil {
		return nil
	}

	return &interfaces.SemanticGroup{
		ID:     group.ID,
		Intent: group.Intent,
		Type:   group.Type,
	}
}

// CreateDefaultContextProcessor creates a context processor with default configuration
func CreateDefaultContextProcessor() interfaces.ContextProcessor {
	return intelligenceContext.NewContextProcessor()
}

// CreateDefaultCorrelationEngine creates a correlation engine with default configuration
func CreateDefaultCorrelationEngine() interfaces.CorrelationEngine {
	return NewCorrelationEngineAdapter()
}
