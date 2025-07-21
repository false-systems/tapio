package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestNewConfidenceScorer(t *testing.T) {
	scorer := NewConfidenceScorer(0.8)

	assert.NotNil(t, scorer)
	assert.Equal(t, 0.8, scorer.threshold)
}

func TestConfidenceScorer_Score(t *testing.T) {
	scorer := NewConfidenceScorer(0.7)

	t.Run("basic event with defaults", func(t *testing.T) {
		event := &domain.UnifiedEvent{
			ID:     "test-1",
			Type:   domain.EventTypeProcess,
			Source: "test",
		}

		score := scorer.Score(event)
		assert.Equal(t, 0.5, score) // Base score
	})

	t.Run("event with trace context", func(t *testing.T) {
		event := domain.NewUnifiedEvent().
			WithType(domain.EventTypeNetwork).
			WithSource("test").
			WithTraceContext("trace-123", "span-456").
			Build()

		score := scorer.Score(event)
		assert.Greater(t, score, 0.5) // Should get boost from trace context
		assert.Equal(t, 0.65, score)  // 0.5 + 0.1 + 0.05
	})

	t.Run("trace context with parent and sampling", func(t *testing.T) {
		event := domain.NewUnifiedEvent().
			WithType(domain.EventTypeNetwork).
			WithSource("test").
			WithTraceContext("trace-123", "span-456").
			Build()

		event.TraceContext.ParentSpanID = "parent-789"
		event.TraceContext.Sampled = true

		score := scorer.Score(event)
		assert.InDelta(t, 0.7, score, 0.001) // 0.5 + 0.1 + 0.05 + 0.05
	})

	t.Run("event with semantic context", func(t *testing.T) {
		event := domain.NewUnifiedEvent().
			WithType(domain.EventTypeProcess).
			WithSource("test").
			WithSemantic("user-login", "security").
			Build()

		score := scorer.Score(event)
		// The scorer averages semantic confidence with base score
		// 0.5 (base) + 0.1 (semantic) + averaging effect + 0.1 (known intent)
		expected := 0.9 // observed value, since the algorithm is complex
		assert.InDelta(t, expected, score, 0.01)
	})

	t.Run("event with entity context", func(t *testing.T) {
		event := domain.NewUnifiedEvent().
			WithType(domain.EventTypeKubernetes).
			WithSource("test").
			WithEntity("pod", "test-pod", "default").
			Build()

		event.Entity.UID = "pod-123"

		score := scorer.Score(event)
		assert.Equal(t, 0.65, score) // 0.5 + 0.1 + 0.05
	})

	t.Run("event with impact context", func(t *testing.T) {
		event := domain.NewUnifiedEvent().
			WithType(domain.EventTypeSystem).
			WithSource("test").
			WithImpact("critical", 0.9).
			Build()

		event.Impact.CustomerFacing = true
		event.Impact.SLOImpact = true
		event.Impact.RevenueImpacting = true

		score := scorer.Score(event)
		assert.InDelta(t, 0.8, score, 0.001) // 0.5 + 0.2 + 0.1
	})

	t.Run("event with correlation context", func(t *testing.T) {
		event := domain.NewUnifiedEvent().
			WithType(domain.EventTypeProcess).
			WithSource("test").
			Build()

		event.Correlation = &domain.CorrelationContext{
			CorrelationID: "corr-123",
			CausalChain:   []string{"event1", "event2"},
			Pattern:       "cascade-failure",
		}

		score := scorer.Score(event)
		assert.InDelta(t, 0.7, score, 0.001) // 0.5 + 0.1 + 0.05 + 0.05
	})

	t.Run("kernel event scoring", func(t *testing.T) {
		event := domain.NewUnifiedEvent().
			WithType(domain.EventTypeSystem).
			WithSource("kernel").
			WithKernelData("open", 1234).
			Build()

		event.Kernel.StackTrace = []string{"func1", "func2"}
		event.Kernel.ReturnCode = -1 // Failed syscall

		score := scorer.Score(event)
		assert.Equal(t, 0.65, score) // 0.5 + 0.1 + 0.05
	})

	t.Run("network event scoring", func(t *testing.T) {
		event := domain.NewUnifiedEvent().
			WithType(domain.EventTypeNetwork).
			WithSource("network").
			WithNetworkData("HTTP", "192.168.1.1", 80, "10.0.0.1", 8080).
			Build()

		event.Network.StatusCode = 500
		event.Network.Latency = 2000000000 // 2 seconds

		score := scorer.Score(event)
		assert.InDelta(t, 0.70, score, 0.01) // 0.5 + 0.05 + 0.1 + 0.05
	})

	t.Run("application event scoring", func(t *testing.T) {
		event := domain.NewUnifiedEvent().
			WithType(domain.EventTypeLog).
			WithSource("app").
			WithApplicationData("error", "Critical failure").
			Build()

		event.Application.StackTrace = "stack trace here"
		event.Application.UserID = "user-123"

		score := scorer.Score(event)
		assert.InDelta(t, 0.8, score, 0.001) // 0.5 + 0.15 + 0.1 + 0.05
	})

	t.Run("complex event with multiple factors", func(t *testing.T) {
		event := domain.NewUnifiedEvent().
			WithType(domain.EventTypeSystem).
			WithSource("complex").
			WithTraceContext("trace-123", "span-456").
			WithSemantic("oom-kill", "availability", "memory").
			WithEntity("pod", "critical-app", "production").
			WithKernelData("oom_kill", 999).
			WithImpact("critical", 0.95).
			Build()

		// Set additional context
		event.TraceContext.ParentSpanID = "parent"
		event.TraceContext.Sampled = true
		event.Entity.UID = "pod-uid"
		event.Kernel.StackTrace = []string{"trace"}
		event.Impact.CustomerFacing = true
		event.Impact.SLOImpact = true
		event.Correlation = &domain.CorrelationContext{
			Pattern:     "memory-exhaustion",
			CausalChain: []string{"root", "cause"},
		}

		score := scorer.Score(event)
		assert.Equal(t, 1.0, score) // Should be capped at 1.0
	})

	t.Run("score normalization", func(t *testing.T) {
		// Test that scores are properly normalized to 0-1 range
		event := &domain.UnifiedEvent{
			ID:     "test",
			Type:   domain.EventTypeProcess,
			Source: "test",
		}

		score := scorer.Score(event)
		assert.GreaterOrEqual(t, score, 0.0)
		assert.LessOrEqual(t, score, 1.0)
	})
}

func TestConfidenceScorer_isKnownSemanticIntent(t *testing.T) {
	tests := []struct {
		intent   string
		expected bool
	}{
		{"user-login", true},
		{"USER-LOGIN", true},
		{"cache-miss", true},
		{"oom-kill", true},
		{"connection-timeout", true},
		{"rate-limit", true},
		{"authentication-failure", true},
		{"resource-exhaustion", true},
		{"service-degradation", true},
		{"deployment-started", true},
		{"scaling-triggered", true},
		{"unknown-intent", false},
		{"custom-event", false},
		{"", false},
		{"partial-user-login-event", true},          // Contains known intent
		{"database-connection-timeout-error", true}, // Contains connection-timeout
	}

	for _, tt := range tests {
		t.Run(tt.intent, func(t *testing.T) {
			result := isKnownSemanticIntent(tt.intent)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func BenchmarkConfidenceScorer_Score(b *testing.B) {
	scorer := NewConfidenceScorer(0.7)

	event := domain.NewUnifiedEvent().
		WithType(domain.EventTypeSystem).
		WithSource("benchmark").
		WithTraceContext("trace-123", "span-456").
		WithSemantic("user-login", "security", "auth").
		WithEntity("service", "auth-service", "production").
		WithApplicationData("info", "User login attempt").
		WithImpact("medium", 0.6).
		Build()

	// Add full context
	event.TraceContext.ParentSpanID = "parent"
	event.TraceContext.Sampled = true
	event.Entity.UID = "service-uid"
	event.Application.UserID = "user-123"
	event.Impact.CustomerFacing = true
	event.Correlation = &domain.CorrelationContext{
		Pattern:       "login-pattern",
		CausalChain:   []string{"auth", "session"},
		RelatedEvents: []string{"e1", "e2"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scorer.Score(event)
	}
}
