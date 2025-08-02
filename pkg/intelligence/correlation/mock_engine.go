package correlation

import (
	"context"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// CorrelationEngine interface for correlation processing
type CorrelationEngine interface {
	Process(ctx context.Context, event *domain.UnifiedEvent) ([]*MultiDimCorrelationResult, error)
}

// MockCorrelationEngine implements a simple mock for testing
type MockCorrelationEngine struct {
	processFunc func(context.Context, *domain.UnifiedEvent) ([]*MultiDimCorrelationResult, error)
	results     []*MultiDimCorrelationResult
}

// NewMockCorrelationEngine creates a new mock correlation engine
func NewMockCorrelationEngine() *MockCorrelationEngine {
	return &MockCorrelationEngine{
		results: make([]*MultiDimCorrelationResult, 0),
	}
}

// Process implements the correlation engine interface for testing
func (m *MockCorrelationEngine) Process(ctx context.Context, event *domain.UnifiedEvent) ([]*MultiDimCorrelationResult, error) {
	if m.processFunc != nil {
		return m.processFunc(ctx, event)
	}

	// Default mock behavior: create a simple correlation result
	result := &MultiDimCorrelationResult{
		ID:         "mock-correlation-" + event.ID,
		Type:       "mock-pattern",
		Confidence: 0.8,
		Events:     []string{event.ID},
		Dimensions: []DimensionMatch{},
		RootCause: &MultiDimRootCauseAnalysis{
			EventID:    event.ID,
			Confidence: 0.8,
			Reasoning:  "Mock root cause for testing",
			Evidence:   []string{"Mock evidence"},
		},
		Impact: &ImpactAnalysis{
			Severity: "medium",
		},
		Recommendation: "Mock recommendation",
		CreatedAt:      time.Now(),
	}

	return []*MultiDimCorrelationResult{result}, nil
}

// SetProcessFunc allows customizing the mock behavior
func (m *MockCorrelationEngine) SetProcessFunc(fn func(context.Context, *domain.UnifiedEvent) ([]*MultiDimCorrelationResult, error)) {
	m.processFunc = fn
}
