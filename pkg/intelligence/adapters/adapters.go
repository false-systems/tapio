package adapters

import (
	"context"

	"github.com/yairfalse/tapio/pkg/domain"
	intelligenceContext "github.com/yairfalse/tapio/pkg/intelligence/context"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
	"github.com/yairfalse/tapio/pkg/intelligence/interfaces"
	"go.uber.org/zap"
)

// CorrelationEngineAdapter adapts SimpleCorrelationSystem to the CorrelationEngine interface
type CorrelationEngineAdapter struct {
	system *correlation.SimpleCorrelationSystem
	logger *zap.Logger
}

// NewCorrelationEngineAdapter creates a new adapter for the correlation engine
func NewCorrelationEngineAdapter() *CorrelationEngineAdapter {
	logger := zap.NewNop() // Default logger, can be configured later
	config := correlation.DefaultSimpleSystemConfig()
	return &CorrelationEngineAdapter{
		system: correlation.NewSimpleCorrelationSystem(logger, config),
		logger: logger,
	}
}

// Start initializes the correlation engine
func (a *CorrelationEngineAdapter) Start() error {
	return a.system.Start()
}

// Stop gracefully shuts down the correlation engine
func (a *CorrelationEngineAdapter) Stop() error {
	return a.system.Stop()
}

// ProcessEvent processes a single event for correlation
func (a *CorrelationEngineAdapter) ProcessEvent(ctx context.Context, event *domain.UnifiedEvent) error {
	return a.system.ProcessEvent(ctx, event)
}

// GetLatestFindings returns the most recent correlation findings
func (a *CorrelationEngineAdapter) GetLatestFindings() *interfaces.Finding {
	// SimpleCorrelationSystem doesn't have a GetLatestFindings method
	// We would need to collect insights from the Insights() channel
	// For now, return nil as this is a cleanup operation
	return nil
}

// GetSemanticGroups returns current semantic groups
func (a *CorrelationEngineAdapter) GetSemanticGroups() []*interfaces.SemanticGroup {
	// SimpleCorrelationSystem doesn't have semantic groups
	// It uses K8s native, temporal, and sequence correlations
	// Return empty array as semantic groups were part of the old "AI" system
	return []*interfaces.SemanticGroup{}
}

// CreateDefaultContextProcessor creates a context processor with default configuration
func CreateDefaultContextProcessor() interfaces.ContextProcessor {
	return intelligenceContext.NewContextProcessor()
}

// CreateDefaultCorrelationEngine creates a correlation engine with default configuration
func CreateDefaultCorrelationEngine() interfaces.CorrelationEngine {
	return NewCorrelationEngineAdapter()
}
