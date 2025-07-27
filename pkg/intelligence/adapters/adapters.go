//go:build experimental
// +build experimental

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
	// Get internal finding from engine
	internalFinding := a.engine.GetLatestFindings()
	if internalFinding == nil {
		return nil
	}
	// Convert to interfaces.Finding
	return correlation.ConvertToInterfacesFinding(internalFinding)
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

// CreateDefaultContextProcessor creates a context processor with default configuration
func CreateDefaultContextProcessor() interfaces.ContextProcessor {
	return intelligenceContext.NewContextProcessor()
}

// CreateDefaultCorrelationEngine creates a correlation engine with default configuration
func CreateDefaultCorrelationEngine() interfaces.CorrelationEngine {
	return NewCorrelationEngineAdapter()
}
