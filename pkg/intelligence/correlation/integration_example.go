// NOTE: This example is temporarily disabled because the analytics adapter
// has been moved to pkg/integrations/analytics to fix architecture violations

package correlation

import (
	"context"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/interfaces"
)

// mockEventPipeline implements interfaces.EventPipeline for testing
type mockEventPipeline struct{}

func (m *mockEventPipeline) Process(ctx context.Context, event *domain.UnifiedEvent) error {
	return nil
}

func (m *mockEventPipeline) Subscribe(ctx context.Context, opts domain.SubscriptionOptions) (<-chan *domain.UnifiedEvent, error) {
	ch := make(chan *domain.UnifiedEvent)
	return ch, nil
}

func (m *mockEventPipeline) GetStats() map[string]interface{} {
	return map[string]interface{}{"mock": true}
}

// mockSemanticTracer implements interfaces.SemanticTracer for testing
type mockSemanticTracer struct{}

func (m *mockSemanticTracer) ExtractSemanticContext(ctx context.Context, event *domain.UnifiedEvent) (*domain.SemanticContext, error) {
	return &domain.SemanticContext{
		Intent:   "test",
		Category: "test",
	}, nil
}

func (m *mockSemanticTracer) GetTraceContext(eventID string) *interfaces.TraceContext {
	return &interfaces.TraceContext{
		TraceID: "test-trace",
		SpanID:  "test-span",
	}
}

// TODO: Create a proper integration example in pkg/integrations/analytics/example/
// that demonstrates how to use the AnalyticsCorrelationAdapter with proper imports
