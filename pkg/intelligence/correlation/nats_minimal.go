package correlation

import (
	"context"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// MinimalCorrelationResult represents a simplified correlation result
type MinimalCorrelationResult struct {
	ID               string
	Type             string
	Confidence       float64
	Events           []string
	TraceID          string
	StartTime        time.Time
	EndTime          time.Time
	RootCauseEventID string
	Description      string
}

// MinimalCorrelationEngine is a simplified interface that our NATS subscriber can use
type MinimalCorrelationEngine interface {
	Process(ctx context.Context, event *domain.UnifiedEvent) ([]*MinimalCorrelationResult, error)
}

// MinimalMockEngine implements MinimalCorrelationEngine for testing
type MinimalMockEngine struct {
	processFunc func(context.Context, *domain.UnifiedEvent) ([]*MinimalCorrelationResult, error)
}

// NewMinimalMockEngine creates a new minimal mock engine
func NewMinimalMockEngine() *MinimalMockEngine {
	return &MinimalMockEngine{}
}

// Process implements the minimal correlation engine interface
func (m *MinimalMockEngine) Process(ctx context.Context, event *domain.UnifiedEvent) ([]*MinimalCorrelationResult, error) {
	if m.processFunc != nil {
		return m.processFunc(ctx, event)
	}
	
	// Default behavior: create a simple correlation
	result := &MinimalCorrelationResult{
		ID:               "corr-" + event.ID,
		Type:             "test-correlation",
		Confidence:       0.8,
		Events:           []string{event.ID},
		StartTime:        event.Timestamp,
		EndTime:          event.Timestamp,
		RootCauseEventID: event.ID,
		Description:      "Test correlation for " + event.ID,
	}
	
	// Add trace ID if available
	if event.TraceContext != nil && event.TraceContext.TraceID != "" {
		result.TraceID = event.TraceContext.TraceID
	}
	
	return []*MinimalCorrelationResult{result}, nil
}

// SetProcessFunc allows customizing the mock behavior
func (m *MinimalMockEngine) SetProcessFunc(fn func(context.Context, *domain.UnifiedEvent) ([]*MinimalCorrelationResult, error)) {
	m.processFunc = fn
}