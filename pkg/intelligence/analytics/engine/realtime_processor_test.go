package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestNewRealTimeProcessor(t *testing.T) {
	processor := NewRealTimeProcessor(1000)

	assert.NotNil(t, processor)
	assert.Equal(t, 1000, processor.maxEventsPerSecond)
	assert.Equal(t, uint64(0), processor.eventsProcessed.Load())
	assert.WithinDuration(t, time.Now(), processor.lastReset, time.Second)
}

func TestRealTimeProcessor_Process(t *testing.T) {
	processor := NewRealTimeProcessor(1000)
	ctx := context.Background()

	t.Run("basic event processing", func(t *testing.T) {
		event := domain.NewUnifiedEvent().
			WithType(domain.EventTypeProcess).
			WithSource("test-collector").
			Build()

		result := &AnalyticsResult{
			EventID:   event.ID,
			Timestamp: time.Now(),
			Metadata:  make(map[string]interface{}),
		}

		err := processor.Process(ctx, event, result)
		require.NoError(t, err)

		// Verify event counter incremented
		assert.Equal(t, uint64(1), processor.eventsProcessed.Load())
	})

	t.Run("event with trace context", func(t *testing.T) {
		event := domain.NewUnifiedEvent().
			WithType(domain.EventTypeNetwork).
			WithSource("trace-collector").
			WithTraceContext("trace-123", "span-456").
			Build()

		// Set additional trace context fields
		event.TraceContext.ParentSpanID = "parent-789"
		event.TraceContext.Sampled = true

		result := &AnalyticsResult{
			EventID:         event.ID,
			Timestamp:       time.Now(),
			ConfidenceScore: 0.5,
			Metadata:        make(map[string]interface{}),
		}

		err := processor.Process(ctx, event, result)
		require.NoError(t, err)

		// Verify trace context metadata
		assert.Equal(t, "trace-123", result.Metadata["trace_id"])
		assert.Equal(t, "span-456", result.Metadata["span_id"])
		assert.Equal(t, true, result.Metadata["sampled"])
	})

	t.Run("event with semantic context", func(t *testing.T) {
		event := domain.NewUnifiedEvent().
			WithType(domain.EventTypeProcess).
			WithSource("semantic-collector").
			WithSemantic("user-login", "security", "auth", "critical").
			Build()

		// Set high confidence
		event.Semantic.Confidence = 0.9

		result := &AnalyticsResult{
			EventID:         event.ID,
			Timestamp:       time.Now(),
			ConfidenceScore: 0.6,
			Metadata:        make(map[string]interface{}),
		}

		err := processor.Process(ctx, event, result)
		require.NoError(t, err)

		// Verify semantic metadata
		assert.Equal(t, "user-login", result.Metadata["semantic_intent"])
		assert.Equal(t, "security", result.Metadata["semantic_category"])
		assert.Equal(t, 0.9, result.Metadata["semantic_confidence"])

		// Verify confidence boost for high semantic confidence
		assert.Greater(t, result.ConfidenceScore, 0.6) // Should be boosted
		assert.LessOrEqual(t, result.ConfidenceScore, 1.0)
	})

	t.Run("event with entity context", func(t *testing.T) {
		event := domain.NewUnifiedEvent().
			WithType(domain.EventTypeKubernetes).
			WithSource("k8s-collector").
			WithEntity("pod", "test-pod", "default").
			Build()

		// Set UID
		event.Entity.UID = "pod-uid-123"

		result := &AnalyticsResult{
			EventID:   event.ID,
			Timestamp: time.Now(),
			Metadata:  make(map[string]interface{}),
		}

		err := processor.Process(ctx, event, result)
		require.NoError(t, err)

		// Verify entity metadata
		assert.Equal(t, "pod", result.Metadata["entity_type"])
		assert.Equal(t, "test-pod", result.Metadata["entity_name"])
		assert.Equal(t, "pod-uid-123", result.Metadata["entity_id"])
	})

	t.Run("event with correlation context", func(t *testing.T) {
		event := domain.NewUnifiedEvent().
			WithType(domain.EventTypeSystem).
			WithSource("corr-collector").
			Build()

		// Set correlation context
		event.Correlation = &domain.CorrelationContext{
			CorrelationID: "corr-123",
			GroupID:       "group-456",
			RelatedEvents: []string{"event1", "event2"},
			CausalChain:   []string{"root", "cause1", "cause2"},
		}

		result := &AnalyticsResult{
			EventID:   event.ID,
			Timestamp: time.Now(),
			Metadata:  make(map[string]interface{}),
		}

		err := processor.Process(ctx, event, result)
		require.NoError(t, err)

		// Verify correlation data populated
		assert.Equal(t, "corr-123", result.CorrelationID)
		assert.Equal(t, "group-456", result.SemanticGroupID)
		assert.Equal(t, []string{"event1", "event2"}, result.RelatedEvents)
		assert.Equal(t, []string{"root", "cause1", "cause2"}, result.Metadata["causal_chain"])
	})

	t.Run("kernel event processing", func(t *testing.T) {
		event := domain.NewUnifiedEvent().
			WithType(domain.EventTypeSystem).
			WithSource("ebpf-collector").
			WithKernelData("open", 1234).
			Build()

		// Set additional kernel data
		event.Kernel.Comm = "test-process"

		result := &AnalyticsResult{
			EventID:   event.ID,
			Timestamp: time.Now(),
			Metadata:  make(map[string]interface{}),
		}

		err := processor.Process(ctx, event, result)
		require.NoError(t, err)

		// Verify kernel metadata
		assert.Equal(t, "open", result.Metadata["kernel_syscall"])
		assert.Equal(t, uint32(1234), result.Metadata["kernel_pid"])
		assert.Equal(t, "test-process", result.Metadata["kernel_comm"])
	})

	t.Run("network event processing", func(t *testing.T) {
		event := domain.NewUnifiedEvent().
			WithType(domain.EventTypeNetwork).
			WithSource("net-collector").
			WithNetworkData("TCP", "192.168.1.1", 80, "10.0.0.1", 8080).
			Build()

		// Set additional network data
		event.Network.Latency = 1500000 // 1.5ms
		event.Network.StatusCode = 200

		result := &AnalyticsResult{
			EventID:   event.ID,
			Timestamp: time.Now(),
			Metadata:  make(map[string]interface{}),
		}

		err := processor.Process(ctx, event, result)
		require.NoError(t, err)

		// Verify network metadata
		assert.Equal(t, "TCP", result.Metadata["network_protocol"])
		assert.Equal(t, int64(1500000), result.Metadata["network_latency_ns"])
		assert.Equal(t, 200, result.Metadata["network_status"])
	})

	t.Run("application event processing", func(t *testing.T) {
		event := domain.NewUnifiedEvent().
			WithType(domain.EventTypeLog).
			WithSource("app-collector").
			WithApplicationData("error", "Database connection failed").
			Build()

		// Set additional application data
		event.Application.Logger = "db.connection"
		event.Application.RequestID = "req-789"

		result := &AnalyticsResult{
			EventID:   event.ID,
			Timestamp: time.Now(),
			Metadata:  make(map[string]interface{}),
		}

		err := processor.Process(ctx, event, result)
		require.NoError(t, err)

		// Verify application metadata
		assert.Equal(t, "error", result.Metadata["app_level"])
		assert.Equal(t, "db.connection", result.Metadata["app_logger"])
		assert.Equal(t, "req-789", result.Metadata["app_request_id"])
	})
}

func TestRealTimeProcessor_GetRate(t *testing.T) {
	processor := NewRealTimeProcessor(1000)

	t.Run("no events processed", func(t *testing.T) {
		rate := processor.GetRate()
		assert.Equal(t, 0.0, rate)
	})

	t.Run("events processed", func(t *testing.T) {
		// Process some events
		processor.eventsProcessed.Add(10)

		// Wait a small amount of time
		time.Sleep(10 * time.Millisecond)

		rate := processor.GetRate()
		assert.Greater(t, rate, 0.0)

		// Rate should be reasonable (events / seconds)
		// With 10 events in ~0.01 seconds, rate should be around 1000 events/sec
		assert.Less(t, rate, 100000.0) // Sanity check
	})

	t.Run("rate calculation with reset", func(t *testing.T) {
		// Reset processor
		processor.eventsProcessed.Store(0)
		processor.lastReset = time.Now()

		// Process events over time
		for i := 0; i < 5; i++ {
			processor.eventsProcessed.Add(1)
			time.Sleep(1 * time.Millisecond)
		}

		rate := processor.GetRate()
		assert.Greater(t, rate, 0.0)
	})
}

func BenchmarkRealTimeProcessor_Process(b *testing.B) {
	processor := NewRealTimeProcessor(100000)
	ctx := context.Background()

	event := domain.NewUnifiedEvent().
		WithType(domain.EventTypeProcess).
		WithSource("benchmark").
		WithTraceContext("trace-123", "span-456").
		WithSemantic("benchmark-event", "performance").
		WithEntity("service", "bench-service", "default").
		WithKernelData("read", 999).
		WithNetworkData("HTTP", "192.168.1.1", 80, "10.0.0.1", 8080).
		WithApplicationData("info", "Benchmark event").
		Build()

	// Add correlation context
	event.Correlation = &domain.CorrelationContext{
		CorrelationID: "bench-corr",
		RelatedEvents: []string{"e1", "e2"},
	}

	result := &AnalyticsResult{
		EventID:         event.ID,
		Timestamp:       time.Now(),
		ConfidenceScore: 0.5,
		Metadata:        make(map[string]interface{}),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := processor.Process(ctx, event, result)
		if err != nil {
			b.Fatal(err)
		}
	}
}
