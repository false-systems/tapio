package pipeline

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestPipelineBuilder_BasicConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		build    func() *PipelineBuilder
		validate func(t *testing.T, pipeline IntelligencePipeline, err error)
	}{
		{
			name: "default configuration",
			build: func() *PipelineBuilder {
				return NewPipelineBuilder()
			},
			validate: func(t *testing.T, pipeline IntelligencePipeline, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, pipeline)
				config := pipeline.GetConfig()
				assert.Equal(t, PipelineModeHighPerformance, config.Mode)
				assert.Equal(t, 1000, config.BatchSize)
				assert.Equal(t, 10000, config.BufferSize)
			},
		},
		{
			name: "standard mode",
			build: func() *PipelineBuilder {
				return NewPipelineBuilder().WithMode(PipelineModeStandard)
			},
			validate: func(t *testing.T, pipeline IntelligencePipeline, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, pipeline)
				config := pipeline.GetConfig()
				assert.Equal(t, PipelineModeStandard, config.Mode)
			},
		},
		{
			name: "debug mode",
			build: func() *PipelineBuilder {
				return NewPipelineBuilder().WithMode(PipelineModeDebug)
			},
			validate: func(t *testing.T, pipeline IntelligencePipeline, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, pipeline)
				config := pipeline.GetConfig()
				assert.Equal(t, PipelineModeDebug, config.Mode)
			},
		},
		{
			name: "custom batch size",
			build: func() *PipelineBuilder {
				return NewPipelineBuilder().
					WithBatchSize(500).
					WithBufferSize(5000)
			},
			validate: func(t *testing.T, pipeline IntelligencePipeline, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, pipeline)
				config := pipeline.GetConfig()
				assert.Equal(t, 500, config.BatchSize)
				assert.Equal(t, 5000, config.BufferSize)
			},
		},
		{
			name: "custom timeouts",
			build: func() *PipelineBuilder {
				return NewPipelineBuilder().
					WithProcessingTimeout(10 * time.Second).
					WithMetricsInterval(500 * time.Millisecond)
			},
			validate: func(t *testing.T, pipeline IntelligencePipeline, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, pipeline)
				config := pipeline.GetConfig()
				assert.Equal(t, 10*time.Second, config.ProcessingTimeout)
				assert.Equal(t, 500*time.Millisecond, config.MetricsInterval)
			},
		},
		{
			name: "features configuration",
			build: func() *PipelineBuilder {
				return NewPipelineBuilder().
					EnableValidation(true).
					EnableContext(true).
					EnableCorrelation(false).
					EnableMetrics(true).
					EnableCircuitBreaker(true)
			},
			validate: func(t *testing.T, pipeline IntelligencePipeline, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, pipeline)
				config := pipeline.GetConfig()
				assert.True(t, config.EnableValidation)
				assert.True(t, config.EnableContext)
				assert.False(t, config.EnableCorrelation)
				assert.True(t, config.EnableMetrics)
				assert.True(t, config.EnableCircuitBreaker)
			},
		},
		{
			name: "error thresholds",
			build: func() *PipelineBuilder {
				return NewPipelineBuilder().
					WithErrorThreshold(0.05).
					WithCircuitBreakerThreshold(0.25)
			},
			validate: func(t *testing.T, pipeline IntelligencePipeline, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, pipeline)
				config := pipeline.GetConfig()
				assert.Equal(t, 0.05, config.ErrorThreshold)
				assert.Equal(t, 0.25, config.CircuitBreakerThreshold)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := tt.build()
			pipeline, err := builder.Build()
			tt.validate(t, pipeline, err)
		})
	}
}

func TestPipelineBuilder_WithConfig(t *testing.T) {
	customConfig := &PipelineConfig{
		Mode:                    PipelineModeStandard,
		BatchSize:               2000,
		BufferSize:              20000,
		MaxConcurrency:          8,
		ProcessingTimeout:       15 * time.Second,
		MetricsInterval:         2 * time.Second,
		EnableValidation:        true,
		EnableContext:           true,
		EnableCorrelation:       true,
		EnableMetrics:           true,
		EnableCircuitBreaker:    true,
		ErrorThreshold:          0.15,
		CircuitBreakerThreshold: 0.4,
	}

	builder := NewPipelineBuilder().WithConfig(customConfig)
	pipeline, err := builder.Build()

	require.NoError(t, err)
	require.NotNil(t, pipeline)

	config := pipeline.GetConfig()
	assert.Equal(t, customConfig.Mode, config.Mode)
	assert.Equal(t, customConfig.BatchSize, config.BatchSize)
	assert.Equal(t, customConfig.BufferSize, config.BufferSize)
	assert.Equal(t, customConfig.MaxConcurrency, config.MaxConcurrency)
	assert.Equal(t, customConfig.ProcessingTimeout, config.ProcessingTimeout)
	assert.Equal(t, customConfig.MetricsInterval, config.MetricsInterval)
}

func TestPipelineFactory_Functions(t *testing.T) {
	tests := []struct {
		name     string
		factory  func() (IntelligencePipeline, error)
		validate func(t *testing.T, pipeline IntelligencePipeline)
	}{
		{
			name:    "NewHighPerformancePipeline",
			factory: NewHighPerformancePipeline,
			validate: func(t *testing.T, pipeline IntelligencePipeline) {
				config := pipeline.GetConfig()
				assert.Equal(t, PipelineModeHighPerformance, config.Mode)
			},
		},
		{
			name:    "NewStandardPipeline",
			factory: NewStandardPipeline,
			validate: func(t *testing.T, pipeline IntelligencePipeline) {
				config := pipeline.GetConfig()
				assert.Equal(t, PipelineModeStandard, config.Mode)
			},
		},
		{
			name:    "NewDebugPipeline",
			factory: NewDebugPipeline,
			validate: func(t *testing.T, pipeline IntelligencePipeline) {
				config := pipeline.GetConfig()
				assert.Equal(t, PipelineModeDebug, config.Mode)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pipeline, err := tt.factory()
			require.NoError(t, err)
			require.NotNil(t, pipeline)
			tt.validate(t, pipeline)
		})
	}
}

func TestPipelineConfig_Validate(t *testing.T) {
	tests := []struct {
		name   string
		config *PipelineConfig
		check  func(t *testing.T, config *PipelineConfig)
	}{
		{
			name: "negative values corrected",
			config: &PipelineConfig{
				BatchSize:         -1,
				BufferSize:        -1,
				ProcessingTimeout: -1,
				MetricsInterval:   -1,
			},
			check: func(t *testing.T, config *PipelineConfig) {
				err := config.Validate()
				assert.NoError(t, err)
				assert.Greater(t, config.BatchSize, 0)
				assert.Greater(t, config.BufferSize, 0)
				assert.Greater(t, config.ProcessingTimeout, time.Duration(0))
				assert.Greater(t, config.MetricsInterval, time.Duration(0))
			},
		},
		{
			name: "error thresholds bounded",
			config: &PipelineConfig{
				ErrorThreshold:          1.5,
				CircuitBreakerThreshold: -0.5,
			},
			check: func(t *testing.T, config *PipelineConfig) {
				err := config.Validate()
				assert.NoError(t, err)
				assert.Equal(t, 0.1, config.ErrorThreshold)
				assert.Equal(t, 0.5, config.CircuitBreakerThreshold)
			},
		},
		{
			name: "nil orchestrator config created",
			config: &PipelineConfig{
				OrchestratorConfig: nil,
			},
			check: func(t *testing.T, config *PipelineConfig) {
				err := config.Validate()
				assert.NoError(t, err)
				assert.NotNil(t, config.OrchestratorConfig)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, tt.config)
		})
	}
}

func TestPipelineConfig_Clone(t *testing.T) {
	original := &PipelineConfig{
		Mode:              PipelineModeDebug,
		BatchSize:         123,
		BufferSize:        456,
		MaxConcurrency:    7,
		EnableValidation:  false,
		EnableContext:     true,
		EnableCorrelation: false,
		OrchestratorConfig: &OrchestratorConfig{
			BatchSize:   789,
			WorkerCount: 3,
		},
	}

	clone := original.Clone()

	// Verify clone has same values
	assert.Equal(t, original.Mode, clone.Mode)
	assert.Equal(t, original.BatchSize, clone.BatchSize)
	assert.Equal(t, original.BufferSize, clone.BufferSize)
	assert.Equal(t, original.MaxConcurrency, clone.MaxConcurrency)
	assert.Equal(t, original.EnableValidation, clone.EnableValidation)
	assert.Equal(t, original.EnableContext, clone.EnableContext)
	assert.Equal(t, original.EnableCorrelation, clone.EnableCorrelation)

	// Verify orchestrator config is deep copied
	assert.Equal(t, original.OrchestratorConfig.BatchSize, clone.OrchestratorConfig.BatchSize)
	assert.Equal(t, original.OrchestratorConfig.WorkerCount, clone.OrchestratorConfig.WorkerCount)

	// Modify clone and ensure original is unchanged
	clone.BatchSize = 999
	clone.OrchestratorConfig.BatchSize = 888
	assert.NotEqual(t, original.BatchSize, clone.BatchSize)
	assert.NotEqual(t, original.OrchestratorConfig.BatchSize, clone.OrchestratorConfig.BatchSize)
}

func TestPipeline_Lifecycle(t *testing.T) {
	pipeline, err := NewHighPerformancePipeline()
	require.NoError(t, err)

	ctx := context.Background()

	// Should not be running initially
	assert.False(t, pipeline.IsRunning())

	// Start pipeline
	err = pipeline.Start(ctx)
	assert.NoError(t, err)
	assert.True(t, pipeline.IsRunning())

	// Starting again should fail
	err = pipeline.Start(ctx)
	assert.Error(t, err)

	// Stop pipeline
	err = pipeline.Stop()
	assert.NoError(t, err)
	assert.False(t, pipeline.IsRunning())

	// Stopping again should fail
	err = pipeline.Stop()
	assert.Error(t, err)

	// Test Shutdown alias
	err = pipeline.Start(ctx)
	assert.NoError(t, err)
	err = pipeline.Shutdown()
	assert.NoError(t, err)
	assert.False(t, pipeline.IsRunning())
}

func TestPipeline_ProcessEvent(t *testing.T) {
	pipeline, err := NewPipelineBuilder().
		EnableCorrelation(false). // Disable correlation for simpler testing
		Build()
	require.NoError(t, err)

	ctx := context.Background()
	err = pipeline.Start(ctx)
	require.NoError(t, err)
	defer pipeline.Stop()

	// Test valid event
	event := &domain.UnifiedEvent{
		ID:        "test-event-1",
		Type:      domain.EventTypeSystem,
		Timestamp: time.Now(),
		Source:    "test",
	}

	err = pipeline.ProcessEvent(event)
	assert.NoError(t, err)

	// Test nil event
	err = pipeline.ProcessEvent(nil)
	assert.Error(t, err)

	// Test processing when stopped
	pipeline.Stop()
	err = pipeline.ProcessEvent(event)
	assert.Error(t, err)
}

func TestPipeline_ProcessBatch(t *testing.T) {
	pipeline, err := NewPipelineBuilder().
		WithBatchSize(10).
		EnableCorrelation(false).
		Build()
	require.NoError(t, err)

	ctx := context.Background()
	err = pipeline.Start(ctx)
	require.NoError(t, err)
	defer pipeline.Stop()

	// Create batch of events
	events := make([]*domain.UnifiedEvent, 5)
	for i := 0; i < 5; i++ {
		events[i] = &domain.UnifiedEvent{
			ID:        domain.GenerateEventID(),
			Type:      domain.EventTypeSystem,
			Timestamp: time.Now(),
			Source:    "test",
		}
	}

	err = pipeline.ProcessBatch(events)
	assert.NoError(t, err)

	// Test empty batch
	err = pipeline.ProcessBatch([]*domain.UnifiedEvent{})
	assert.NoError(t, err)

	// Test nil batch
	err = pipeline.ProcessBatch(nil)
	assert.NoError(t, err)
}

func TestPipeline_Metrics(t *testing.T) {
	pipeline, err := NewPipelineBuilder().
		EnableMetrics(true).
		WithMetricsInterval(100 * time.Millisecond).
		EnableCorrelation(false).
		Build()
	require.NoError(t, err)

	ctx := context.Background()
	err = pipeline.Start(ctx)
	require.NoError(t, err)
	defer pipeline.Stop()

	// Process some events
	for i := 0; i < 10; i++ {
		event := &domain.UnifiedEvent{
			ID:        domain.GenerateEventID(),
			Type:      domain.EventTypeSystem,
			Timestamp: time.Now(),
			Source:    "test",
		}
		pipeline.ProcessEvent(event)
	}

	// Wait for metrics collection
	time.Sleep(200 * time.Millisecond)

	// Get metrics
	metrics := pipeline.GetMetrics()
	assert.GreaterOrEqual(t, metrics.EventsReceived, int64(10))
	assert.Greater(t, metrics.EventsProcessed, int64(0))
	assert.NotZero(t, metrics.StartTime)
	assert.NotZero(t, metrics.Uptime)
}

func TestCircuitBreaker(t *testing.T) {
	cb := NewCircuitBreaker(0.5, 0.1, 100*time.Millisecond)

	// Initially closed
	assert.Equal(t, "closed", cb.State())
	assert.True(t, cb.Allow())

	// Record failures to trip the breaker
	for i := 0; i < 10; i++ {
		cb.RecordFailure()
	}

	// Should be open now
	assert.Equal(t, "open", cb.State())
	assert.False(t, cb.Allow())
	assert.Equal(t, int64(1), cb.GetTrips())

	// Wait for recovery timeout
	time.Sleep(150 * time.Millisecond)

	// Should be half-open
	assert.True(t, cb.Allow())

	// Record success to close
	cb.RecordSuccess()
	assert.Equal(t, "closed", cb.State())
}

func TestPipeline_CircuitBreaker(t *testing.T) {
	pipeline, err := NewPipelineBuilder().
		EnableCircuitBreaker(true).
		WithCircuitBreakerThreshold(0.5).
		EnableCorrelation(false).
		Build()
	require.NoError(t, err)

	ctx := context.Background()
	err = pipeline.Start(ctx)
	require.NoError(t, err)
	defer pipeline.Stop()

	// Process events that will fail validation
	// Note: ProcessEvent doesn't return validation errors, they are tracked internally
	for i := 0; i < 10; i++ {
		event := &domain.UnifiedEvent{
			// ID and Timestamp will be auto-filled, but missing Source will fail validation
			ID:        domain.GenerateEventID(),
			Timestamp: time.Now(),
			Type:      domain.EventTypeSystem,
			// Missing Source field will cause validation failure
		}
		err := pipeline.ProcessEvent(event)
		// ProcessEvent returns nil even for events that fail validation
		// because they are queued for async processing
		assert.NoError(t, err)
	}

	// Wait for events to be processed
	time.Sleep(100 * time.Millisecond)

	// Get metrics to check circuit breaker state
	metrics := pipeline.GetMetrics()
	assert.NotEmpty(t, metrics.CircuitBreakerState)
}

func TestPipeline_ConcurrentProcessing(t *testing.T) {
	pipeline, err := NewPipelineBuilder().
		WithMaxConcurrency(4).
		EnableCorrelation(false).
		Build()
	require.NoError(t, err)

	ctx := context.Background()
	err = pipeline.Start(ctx)
	require.NoError(t, err)
	defer pipeline.Stop()

	// Process events concurrently
	var wg sync.WaitGroup
	numGoroutines := 10
	eventsPerGoroutine := 100

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < eventsPerGoroutine; i++ {
				event := &domain.UnifiedEvent{
					ID:        domain.GenerateEventID(),
					Type:      domain.EventTypeSystem,
					Timestamp: time.Now(),
					Source:    "test",
				}
				err := pipeline.ProcessEvent(event)
				assert.NoError(t, err)
			}
		}(g)
	}

	wg.Wait()

	// Verify metrics
	metrics := pipeline.GetMetrics()
	expectedEvents := numGoroutines * eventsPerGoroutine
	assert.Equal(t, int64(expectedEvents), metrics.EventsReceived)
}

func TestNewCustomPipeline(t *testing.T) {
	pipeline, err := NewCustomPipeline(
		func(c *PipelineConfig) {
			c.Mode = PipelineModeDebug
			c.BatchSize = 50
		},
		func(c *PipelineConfig) {
			c.EnableTracing = true
			c.EnableProfiling = true
		},
	)

	require.NoError(t, err)
	require.NotNil(t, pipeline)

	config := pipeline.GetConfig()
	assert.Equal(t, PipelineModeDebug, config.Mode)
	assert.Equal(t, 50, config.BatchSize)
	assert.True(t, config.EnableTracing)
	assert.True(t, config.EnableProfiling)
}

func TestMetricsCollector(t *testing.T) {
	mc := NewMetricsCollector()

	// Test counters
	mc.IncrementReceived(10)
	mc.IncrementProcessed(8)
	mc.IncrementFailed(2)

	// Test latency recording
	mc.RecordLatency(100 * time.Millisecond)
	mc.RecordLatency(200 * time.Millisecond)
	mc.RecordLatency(150 * time.Millisecond)

	// Test batch recording
	mc.RecordBatch(100)
	mc.RecordBatch(200)

	// Get metrics
	metrics := mc.GetMetrics()
	assert.Equal(t, int64(10), metrics.EventsReceived)
	assert.Equal(t, int64(8), metrics.EventsProcessed)
	assert.Equal(t, int64(2), metrics.EventsFailed)
	assert.Equal(t, float64(0.2), metrics.ErrorRate)
	assert.Equal(t, int64(2), metrics.BatchesProcessed)
	assert.Equal(t, float64(150), metrics.AverageBatchSize)
	assert.Equal(t, 200, metrics.MaxBatchSize)
	assert.NotZero(t, metrics.AverageLatency)
	assert.NotZero(t, metrics.P50Latency)
	assert.NotZero(t, metrics.P95Latency)
	assert.NotZero(t, metrics.P99Latency)
	assert.Equal(t, 200*time.Millisecond, metrics.MaxLatency)

	// Test reset
	mc.Reset()
	metrics = mc.GetMetrics()
	assert.Equal(t, int64(0), metrics.EventsReceived)
	assert.Equal(t, int64(0), metrics.EventsProcessed)
}

// Benchmark tests
func BenchmarkPipelineBuilder_Build(b *testing.B) {
	for i := 0; i < b.N; i++ {
		builder := NewPipelineBuilder().
			WithMode(PipelineModeHighPerformance).
			WithBatchSize(1000).
			EnableMetrics(true)

		pipeline, err := builder.Build()
		if err != nil {
			b.Fatal(err)
		}
		_ = pipeline
	}
}

func BenchmarkPipeline_ProcessEvent(b *testing.B) {
	pipeline, err := NewHighPerformancePipeline()
	if err != nil {
		b.Fatal(err)
	}

	ctx := context.Background()
	if err := pipeline.Start(ctx); err != nil {
		b.Fatal(err)
	}
	defer pipeline.Stop()

	event := &domain.UnifiedEvent{
		ID:        "benchmark-event",
		Type:      domain.EventTypeSystem,
		Timestamp: time.Now(),
		Source:    "benchmark",
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			pipeline.ProcessEvent(event)
		}
	})
}
