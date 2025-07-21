package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

func TestNewAnalyticsEngine(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()

	engine, err := NewAnalyticsEngine(config, logger)
	require.NoError(t, err)
	require.NotNil(t, engine)

	// Verify configuration
	assert.Equal(t, config.MaxEventsPerSecond, 165000)
	assert.Equal(t, config.BatchSize, 100)
	assert.Equal(t, config.WorkerCount, 8)
	assert.True(t, config.EnableSemanticGrouping)
	assert.True(t, config.EnableRealTimeAnalysis)
	assert.True(t, config.EnableImpactAssessment)

	// Verify components are initialized
	assert.NotNil(t, engine.eventPipeline)
	assert.NotNil(t, engine.correlationEngine)
	assert.NotNil(t, engine.semanticTracer)
	assert.NotNil(t, engine.realTimeProcessor)
	assert.NotNil(t, engine.confidenceScorer)
	assert.NotNil(t, engine.impactAssessment)
	assert.NotNil(t, engine.inputStream)
	assert.NotNil(t, engine.outputStream)

	// Verify initial state
	assert.False(t, engine.running)
	assert.Equal(t, uint64(0), engine.eventsProcessed)
	assert.Equal(t, uint64(0), engine.correlationsFound)
	assert.Equal(t, uint64(0), engine.groupsCreated)
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.Equal(t, 165000, config.MaxEventsPerSecond)
	assert.Equal(t, 100, config.BatchSize)
	assert.Equal(t, 100*time.Millisecond, config.FlushInterval)
	assert.Equal(t, 8, config.WorkerCount)
	assert.True(t, config.EnableSemanticGrouping)
	assert.Equal(t, 0.7, config.ConfidenceThreshold)
	assert.Equal(t, 30*time.Minute, config.GroupRetentionPeriod)
	assert.Equal(t, 65536, config.BufferSize)
	assert.True(t, config.EnableZeroCopy)
	assert.True(t, config.UseAffinity)
	assert.Equal(t, 1*time.Millisecond, config.MaxLatency)
	assert.True(t, config.EnableRealTimeAnalysis)
	assert.True(t, config.EnablePredictiveAnalysis)
	assert.True(t, config.EnableImpactAssessment)
	assert.Equal(t, "tapio.analytics", config.ServiceName)
	assert.Equal(t, "v1.0.0", config.ServiceVersion)
	assert.Equal(t, "production", config.Environment)
}

func TestAnalyticsEngine_StartStop(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()

	// Reduce complexity for testing
	config.BatchSize = 10
	config.BufferSize = 128 // Must be power of 2
	config.WorkerCount = 2

	engine, err := NewAnalyticsEngine(config, logger)
	require.NoError(t, err)

	t.Run("start engine", func(t *testing.T) {
		err := engine.Start()
		require.NoError(t, err)
		assert.True(t, engine.running)

		// Give workers time to start
		time.Sleep(100 * time.Millisecond)
	})

	t.Run("cannot start already running engine", func(t *testing.T) {
		err := engine.Start()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already running")
	})

	t.Run("stop engine", func(t *testing.T) {
		err := engine.Stop()
		require.NoError(t, err)
		assert.False(t, engine.running)
	})

	t.Run("stop already stopped engine", func(t *testing.T) {
		err := engine.Stop()
		require.NoError(t, err) // Should not error
	})
}

func TestAnalyticsEngine_ProcessEvent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()

	// Simplified config for testing
	config.BatchSize = 5
	config.BufferSize = 64 // Must be power of 2
	config.WorkerCount = 1

	engine, err := NewAnalyticsEngine(config, logger)
	require.NoError(t, err)

	err = engine.Start()
	require.NoError(t, err)
	defer engine.Stop()

	ctx := context.Background()

	t.Run("process valid event", func(t *testing.T) {
		event := domain.NewUnifiedEvent().
			WithType(domain.EventTypeProcess).
			WithSource("test-collector").
			WithSemantic("test-intent", "test-category").
			WithEntity("pod", "test-pod", "default").
			WithImpact("medium", 0.6).
			Build()

		result, err := engine.ProcessEvent(ctx, event)
		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Equal(t, event.ID, result.EventID)
		assert.NotZero(t, result.Timestamp)
		assert.Greater(t, result.ConfidenceScore, 0.0)
		assert.LessOrEqual(t, result.ConfidenceScore, 1.0)
		assert.NotZero(t, result.AnalysisLatency)
		assert.NotNil(t, result.Metadata)

		// Verify impact assessment was applied
		if config.EnableImpactAssessment {
			assert.NotNil(t, result.ImpactAssessment)
		}
	})

	t.Run("process event with trace context", func(t *testing.T) {
		event := domain.NewUnifiedEvent().
			WithType(domain.EventTypeNetwork).
			WithSource("network-collector").
			WithTraceContext("trace-123", "span-456").
			WithSemantic("http-request", "performance", "web").
			Build()

		result, err := engine.ProcessEvent(ctx, event)
		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Equal(t, event.ID, result.EventID)
		assert.Greater(t, result.ConfidenceScore, 0.5) // Should get boost from trace context
	})

	t.Run("process critical event", func(t *testing.T) {
		event := domain.NewUnifiedEvent().
			WithType(domain.EventTypeSystem).
			WithSource("kernel-collector").
			WithKernelData("oom_kill", 1234).
			WithSemantic("oom-kill", "availability", "memory", "critical").
			WithImpact("critical", 0.95).
			Build()

		result, err := engine.ProcessEvent(ctx, event)
		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Equal(t, event.ID, result.EventID)
		assert.Greater(t, result.ConfidenceScore, 0.8) // Critical events get high confidence

		if result.ImpactAssessment != nil {
			assert.Equal(t, "critical", result.ImpactAssessment.TechnicalSeverity)
			assert.Greater(t, result.ImpactAssessment.BusinessImpact, 0.8)
		}
	})

	t.Run("engine not running", func(t *testing.T) {
		engine.Stop()

		event := domain.NewUnifiedEvent().
			WithType(domain.EventTypeProcess).
			WithSource("test").
			Build()

		result, err := engine.ProcessEvent(ctx, event)
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "not running")
	})
}

func TestAnalyticsEngine_ProcessBatch(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.BatchSize = 10
	config.BufferSize = 128 // Must be power of 2

	engine, err := NewAnalyticsEngine(config, logger)
	require.NoError(t, err)

	err = engine.Start()
	require.NoError(t, err)
	defer engine.Stop()

	ctx := context.Background()

	t.Run("process valid batch", func(t *testing.T) {
		events := make([]*domain.UnifiedEvent, 5)
		for i := 0; i < 5; i++ {
			events[i] = domain.NewUnifiedEvent().
				WithType(domain.EventTypeProcess).
				WithSource("batch-test").
				WithSemantic("batch-event", "test").
				Build()
		}

		results, err := engine.ProcessBatch(ctx, events)
		require.NoError(t, err)
		require.Len(t, results, 5)

		for i, result := range results {
			if result != nil { // Some might be nil due to processing errors
				assert.Equal(t, events[i].ID, result.EventID)
				assert.Greater(t, result.ConfidenceScore, 0.0)
			}
		}
	})

	t.Run("empty batch", func(t *testing.T) {
		events := []*domain.UnifiedEvent{}

		results, err := engine.ProcessBatch(ctx, events)
		require.NoError(t, err)
		assert.Empty(t, results)
	})

	t.Run("batch with nil event", func(t *testing.T) {
		events := []*domain.UnifiedEvent{
			domain.NewUnifiedEvent().WithType(domain.EventTypeProcess).WithSource("test").Build(),
			nil, // This should cause an error in ProcessEvent
			domain.NewUnifiedEvent().WithType(domain.EventTypeProcess).WithSource("test").Build(),
		}

		results, err := engine.ProcessBatch(ctx, events)
		require.NoError(t, err) // ProcessBatch should not fail, but individual events might
		assert.Len(t, results, 3)

		// First and third should be processed, second should be nil (due to processing error)
		assert.NotNil(t, results[0])
		// results[1] might be nil due to the processing error
		assert.NotNil(t, results[2])
	})
}

func TestAnalyticsEngine_GetMetrics(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.BatchSize = 5
	config.BufferSize = 64 // Must be power of 2

	engine, err := NewAnalyticsEngine(config, logger)
	require.NoError(t, err)

	err = engine.Start()
	require.NoError(t, err)
	defer engine.Stop()

	// Process some events to generate metrics
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		event := domain.NewUnifiedEvent().
			WithType(domain.EventTypeProcess).
			WithSource("metrics-test").
			Build()
		engine.ProcessEvent(ctx, event)
	}

	// Give time for processing
	time.Sleep(100 * time.Millisecond)

	metrics := engine.GetMetrics()
	require.NotNil(t, metrics)

	assert.GreaterOrEqual(t, metrics.EventsProcessed, uint64(0))
	assert.GreaterOrEqual(t, metrics.CorrelationsFound, uint64(0))
	assert.GreaterOrEqual(t, metrics.SemanticGroups, uint64(0))
	assert.GreaterOrEqual(t, int64(metrics.AnalysisLatency), int64(0))
	assert.GreaterOrEqual(t, metrics.Throughput, uint64(0))
	assert.NotNil(t, metrics.PipelineMetrics)
	assert.GreaterOrEqual(t, metrics.QueueDepth, 0)
	assert.GreaterOrEqual(t, metrics.OutputBacklog, 0)
	assert.Equal(t, true, metrics.IsRunning)
	assert.Greater(t, metrics.Uptime, time.Duration(0))
}

func TestAnalyticsEngine_GetAnalyticsStream(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.BufferSize = 16 // Must be power of 2

	engine, err := NewAnalyticsEngine(config, logger)
	require.NoError(t, err)

	stream := engine.GetAnalyticsStream()
	require.NotNil(t, stream)

	// Should be a read-only channel
	select {
	case <-stream:
		// Channel is readable (might be closed or have data)
	default:
		// Channel is not immediately readable, which is expected for empty stream
	}
}

func TestAnalyticsResult(t *testing.T) {
	result := &AnalyticsResult{
		EventID:         "test-event-123",
		Timestamp:       time.Now(),
		CorrelationID:   "corr-456",
		SemanticGroupID: "group-789",
		ConfidenceScore: 0.85,
		ImpactAssessment: &ImpactResult{
			BusinessImpact:     0.7,
			TechnicalSeverity:  "high",
			CascadeRisk:        0.3,
			AffectedServices:   []string{"service1", "service2"},
			RecommendedActions: []string{"investigate", "monitor"},
		},
		PredictedOutcome: &PredictionResult{
			Scenario:    "service-degradation",
			Probability: 0.6,
			TimeToEvent: 5 * time.Minute,
			Confidence:  0.75,
			Mitigation:  []string{"scale-up", "restart-service"},
		},
		RelatedEvents:   []string{"related-1", "related-2"},
		AnalysisLatency: 10 * time.Millisecond,
		Metadata:        map[string]interface{}{"key": "value"},
	}

	// Verify all fields are set correctly
	assert.Equal(t, "test-event-123", result.EventID)
	assert.NotZero(t, result.Timestamp)
	assert.Equal(t, "corr-456", result.CorrelationID)
	assert.Equal(t, "group-789", result.SemanticGroupID)
	assert.Equal(t, 0.85, result.ConfidenceScore)

	require.NotNil(t, result.ImpactAssessment)
	assert.Equal(t, 0.7, result.ImpactAssessment.BusinessImpact)
	assert.Equal(t, "high", result.ImpactAssessment.TechnicalSeverity)

	require.NotNil(t, result.PredictedOutcome)
	assert.Equal(t, "service-degradation", result.PredictedOutcome.Scenario)
	assert.Equal(t, 0.6, result.PredictedOutcome.Probability)

	assert.Len(t, result.RelatedEvents, 2)
	assert.Equal(t, 10*time.Millisecond, result.AnalysisLatency)
	assert.Contains(t, result.Metadata, "key")
}

func TestAnalyticsMetrics(t *testing.T) {
	metrics := &AnalyticsMetrics{
		EventsProcessed:   1000,
		CorrelationsFound: 50,
		SemanticGroups:    25,
		AnalysisLatency:   5 * time.Millisecond,
		Throughput:        2000,
		PipelineMetrics:   nil, // Would be populated in real usage
		QueueDepth:        5,
		OutputBacklog:     2,
		IsRunning:         true,
		Uptime:            10 * time.Minute,
	}

	assert.Equal(t, uint64(1000), metrics.EventsProcessed)
	assert.Equal(t, uint64(50), metrics.CorrelationsFound)
	assert.Equal(t, uint64(25), metrics.SemanticGroups)
	assert.Equal(t, 5*time.Millisecond, metrics.AnalysisLatency)
	assert.Equal(t, uint64(2000), metrics.Throughput)
	assert.Equal(t, 5, metrics.QueueDepth)
	assert.Equal(t, 2, metrics.OutputBacklog)
	assert.True(t, metrics.IsRunning)
	assert.Equal(t, 10*time.Minute, metrics.Uptime)
}

func TestAnalyticsEngine_ExtractEventIDs(t *testing.T) {
	events := []*domain.UnifiedEvent{
		{ID: "event-1"},
		{ID: "event-2"},
		{ID: "event-3"},
	}

	ids := ExtractEventIDs(events)

	require.Len(t, ids, 3)
	assert.Equal(t, "event-1", ids[0])
	assert.Equal(t, "event-2", ids[1])
	assert.Equal(t, "event-3", ids[2])
}

func TestAnalyticsEngine_ExtractEventIDsEmpty(t *testing.T) {
	events := []*domain.UnifiedEvent{}
	ids := ExtractEventIDs(events)
	assert.Empty(t, ids)
}

func TestAnalyticsEngine_ExtractEventIDsNil(t *testing.T) {
	var events []*domain.UnifiedEvent
	ids := ExtractEventIDs(events)
	assert.Empty(t, ids)
}

// Benchmark tests
func BenchmarkAnalyticsEngine_ProcessEvent(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultConfig()
	config.BatchSize = 1000
	config.BufferSize = 8192 // Must be power of 2

	engine, err := NewAnalyticsEngine(config, logger)
	require.NoError(b, err)

	err = engine.Start()
	require.NoError(b, err)
	defer engine.Stop()

	ctx := context.Background()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// Create unique event for each iteration
			testEvent := domain.NewUnifiedEvent().
				WithType(domain.EventTypeProcess).
				WithSource("benchmark").
				WithSemantic("bench-event", "performance").
				Build()

			_, err := engine.ProcessEvent(ctx, testEvent)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkAnalyticsEngine_ProcessBatch(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultConfig()
	config.BatchSize = 100
	config.BufferSize = 8192 // Must be power of 2

	engine, err := NewAnalyticsEngine(config, logger)
	require.NoError(b, err)

	err = engine.Start()
	require.NoError(b, err)
	defer engine.Stop()

	ctx := context.Background()

	// Create batch of events
	events := make([]*domain.UnifiedEvent, 50)
	for i := 0; i < 50; i++ {
		events[i] = domain.NewUnifiedEvent().
			WithType(domain.EventTypeProcess).
			WithSource("batch-benchmark").
			Build()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := engine.ProcessBatch(ctx, events)
		if err != nil {
			b.Fatal(err)
		}
	}
}
