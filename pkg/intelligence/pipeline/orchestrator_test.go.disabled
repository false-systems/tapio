package pipeline

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
)

func TestDefaultOrchestratorConfig(t *testing.T) {
	config := DefaultOrchestratorConfig()

	assert.Equal(t, 1000, config.BatchSize)
	assert.Equal(t, runtime.NumCPU(), config.WorkerCount)
	assert.Equal(t, 10000, config.BufferSize)
	assert.Equal(t, 5*time.Second, config.ProcessingTimeout)
	assert.Equal(t, 1*time.Second, config.MetricsInterval)
	assert.True(t, config.CorrelationEnabled)
}

func TestNewHighPerformanceOrchestrator(t *testing.T) {
	tests := []struct {
		name   string
		config *OrchestratorConfig
		verify func(t *testing.T, orch *HighPerformanceOrchestrator, err error)
	}{
		{
			name:   "default config",
			config: nil,
			verify: func(t *testing.T, orch *HighPerformanceOrchestrator, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, orch)
				assert.NotNil(t, orch.config)
				assert.NotNil(t, orch.workerPool)
				assert.Len(t, orch.stages, 3) // validation, context, correlation
			},
		},
		{
			name: "custom config with correlation disabled",
			config: &OrchestratorConfig{
				BatchSize:          500,
				WorkerCount:        2,
				BufferSize:         5000,
				ProcessingTimeout:  3 * time.Second,
				MetricsInterval:    500 * time.Millisecond,
				CorrelationEnabled: false,
			},
			verify: func(t *testing.T, orch *HighPerformanceOrchestrator, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, orch)
				assert.Equal(t, 500, orch.config.BatchSize)
				assert.Equal(t, 2, orch.config.WorkerCount)
				assert.Len(t, orch.stages, 2) // validation, context only
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orch, err := NewHighPerformanceOrchestrator(tt.config)
			tt.verify(t, orch, err)
		})
	}
}

func TestValidationStage(t *testing.T) {
	stage := &ValidationStage{}
	ctx := context.Background()

	tests := []struct {
		name    string
		event   *domain.UnifiedEvent
		wantErr bool
	}{
		{
			name:    "nil event",
			event:   nil,
			wantErr: true,
		},
		{
			name: "empty ID",
			event: &domain.UnifiedEvent{
				Timestamp: time.Now(),
			},
			wantErr: true,
		},
		{
			name: "zero timestamp",
			event: &domain.UnifiedEvent{
				ID: "test-id",
			},
			wantErr: true,
		},
		{
			name: "valid event",
			event: &domain.UnifiedEvent{
				ID:        "test-id",
				Timestamp: time.Now(),
				Source:    "test-collector",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := stage.Process(ctx, tt.event)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestContextStage(t *testing.T) {
	stage := NewContextStage()
	ctx := context.Background()

	event := &domain.UnifiedEvent{
		ID:        "test-event",
		Type:      domain.EventTypeSystem,
		Timestamp: time.Now(),
		Source:    "test-collector",
		Entity: &domain.EntityContext{
			Type: "service",
			Name: "api",
		},
		Semantic: &domain.SemanticContext{
			Category: "error",
			Intent:   "connection-failed",
		},
	}

	err := stage.Process(ctx, event)
	assert.NoError(t, err)

	// Verify context enrichment
	assert.NotNil(t, event.Semantic)
	assert.Greater(t, event.Semantic.Confidence, 0.0)
	assert.NotNil(t, event.Impact)
}

func TestCorrelationStage(t *testing.T) {
	correlationConfig := &correlation.ProcessorConfig{
		BufferSize:        100,
		TimeWindow:        1 * time.Minute,
		CorrelationWindow: 2 * time.Minute,
	}

	stage, err := NewCorrelationStage(correlationConfig)
	require.NoError(t, err)

	ctx := context.Background()
	event := &domain.UnifiedEvent{
		ID:        "test-correlation",
		Type:      domain.EventTypeSystem,
		Timestamp: time.Now(),
		Source:    "test-collector",
		Entity: &domain.EntityContext{
			Type:      "service",
			Name:      "db",
			Namespace: "prod",
		},
		Semantic: &domain.SemanticContext{
			Category: "crash",
		},
	}

	err = stage.Process(ctx, event)
	assert.NoError(t, err)

	// Verify correlation enrichment
	assert.NotNil(t, event.Correlation)
	assert.NotEmpty(t, event.Correlation.CorrelationID)
	assert.NotEmpty(t, event.Correlation.Pattern)
}

func TestHighPerformanceOrchestrator_Lifecycle(t *testing.T) {
	config := &OrchestratorConfig{
		BatchSize:          100,
		WorkerCount:        2,
		BufferSize:         1000,
		ProcessingTimeout:  1 * time.Second,
		MetricsInterval:    100 * time.Millisecond,
		CorrelationEnabled: false, // Disable for simpler testing
	}

	orch, err := NewHighPerformanceOrchestrator(config)
	require.NoError(t, err)

	ctx := context.Background()

	// Test initial state
	assert.False(t, orch.IsRunning())

	// Start orchestrator
	err = orch.Start(ctx)
	assert.NoError(t, err)
	assert.True(t, orch.IsRunning())

	// Test double start
	err = orch.Start(ctx)
	assert.Error(t, err)

	// Stop orchestrator
	err = orch.Stop()
	assert.NoError(t, err)
	assert.False(t, orch.IsRunning())

	// Test double stop
	err = orch.Stop()
	assert.Error(t, err)
}

func TestHighPerformanceOrchestrator_ProcessEvent(t *testing.T) {
	config := &OrchestratorConfig{
		BatchSize:          10,
		WorkerCount:        2,
		BufferSize:         100,
		ProcessingTimeout:  1 * time.Second,
		MetricsInterval:    100 * time.Millisecond,
		CorrelationEnabled: false,
	}

	orch, err := NewHighPerformanceOrchestrator(config)
	require.NoError(t, err)

	ctx := context.Background()
	err = orch.Start(ctx)
	require.NoError(t, err)
	defer orch.Stop()

	// Test processing valid event
	event := &domain.UnifiedEvent{
		ID:        "test-process",
		Type:      domain.EventTypeSystem,
		Timestamp: time.Now(),
		Source:    "test-collector",
	}

	err = orch.ProcessEvent(event)
	assert.NoError(t, err)

	// Test processing nil event
	err = orch.ProcessEvent(nil)
	assert.Error(t, err)

	// Give time for processing
	time.Sleep(100 * time.Millisecond)

	// Check metrics
	metrics := orch.GetMetrics()
	assert.Greater(t, metrics.EventsProcessed, int64(0))
}

func TestHighPerformanceOrchestrator_ProcessBatch(t *testing.T) {
	config := &OrchestratorConfig{
		BatchSize:          5,
		WorkerCount:        2,
		BufferSize:         100,
		ProcessingTimeout:  1 * time.Second,
		MetricsInterval:    100 * time.Millisecond,
		CorrelationEnabled: false,
	}

	orch, err := NewHighPerformanceOrchestrator(config)
	require.NoError(t, err)

	ctx := context.Background()
	err = orch.Start(ctx)
	require.NoError(t, err)
	defer orch.Stop()

	// Create batch of events
	events := make([]*domain.UnifiedEvent, 10)
	for i := 0; i < 10; i++ {
		events[i] = &domain.UnifiedEvent{
			ID:        fmt.Sprintf("batch-event-%d", i),
			Type:      domain.EventTypeSystem,
			Timestamp: time.Now(),
			Source:    "test-collector",
		}
	}

	// Process batch
	err = orch.ProcessBatch(events)
	assert.NoError(t, err)

	// Test empty batch
	err = orch.ProcessBatch(nil)
	assert.NoError(t, err)

	// Give time for processing
	time.Sleep(200 * time.Millisecond)
}

func TestHighPerformanceOrchestrator_PerformanceBenchmark(t *testing.T) {
	config := &OrchestratorConfig{
		BatchSize:          1000,
		WorkerCount:        runtime.NumCPU(),
		BufferSize:         10000,
		ProcessingTimeout:  5 * time.Second,
		MetricsInterval:    100 * time.Millisecond,
		CorrelationEnabled: false, // Disable for pure throughput test
	}

	orch, err := NewHighPerformanceOrchestrator(config)
	require.NoError(t, err)

	ctx := context.Background()
	err = orch.Start(ctx)
	require.NoError(t, err)
	defer orch.Stop()

	// Benchmark parameters
	numEvents := 10000
	batchSize := 1000

	startTime := time.Now()

	// Send events in batches
	var wg sync.WaitGroup
	for i := 0; i < numEvents; i += batchSize {
		wg.Add(1)
		go func(start int) {
			defer wg.Done()

			batch := make([]*domain.UnifiedEvent, batchSize)
			for j := 0; j < batchSize && start+j < numEvents; j++ {
				batch[j] = &domain.UnifiedEvent{
					ID:        fmt.Sprintf("perf-event-%d", start+j),
					Type:      domain.EventTypeSystem,
					Timestamp: time.Now(),
					Source:    "test-collector",
					Entity: &domain.EntityContext{
						Type: "service",
						Name: "benchmark",
					},
				}
			}

			orch.ProcessBatch(batch)
		}(i)
	}

	wg.Wait()

	// Wait for processing to complete
	time.Sleep(1 * time.Second)

	duration := time.Since(startTime)
	eventsPerSecond := float64(numEvents) / duration.Seconds()

	t.Logf("Performance Benchmark Results:")
	t.Logf("  Events: %d", numEvents)
	t.Logf("  Duration: %v", duration)
	t.Logf("  Throughput: %.2f events/sec", eventsPerSecond)
	t.Logf("  Target: 165,000 events/sec")

	// Get final metrics
	metrics := orch.GetMetrics()
	t.Logf("Orchestrator Metrics:")
	t.Logf("  Events Processed: %d", metrics.EventsProcessed)
	t.Logf("  Events Validated: %d", metrics.EventsValidated)
	t.Logf("  Events Context Built: %d", metrics.EventsContextBuilt)
	t.Logf("  Validation Errors: %d", metrics.ValidationErrors)
	t.Logf("  Context Errors: %d", metrics.ContextErrors)

	// Basic performance assertions
	assert.Greater(t, eventsPerSecond, float64(1000), "Should process at least 1000 events/sec")
	assert.Greater(t, metrics.EventsValidated, int64(numEvents/2), "Should validate majority of events")
	assert.Greater(t, metrics.EventsContextBuilt, int64(numEvents/2), "Should build context for majority of events")
}

func TestHighPerformanceOrchestrator_ErrorHandling(t *testing.T) {
	config := &OrchestratorConfig{
		BatchSize:          10,
		WorkerCount:        1,
		BufferSize:         100,
		ProcessingTimeout:  1 * time.Second,
		MetricsInterval:    100 * time.Millisecond,
		CorrelationEnabled: false,
	}

	orch, err := NewHighPerformanceOrchestrator(config)
	require.NoError(t, err)

	ctx := context.Background()
	err = orch.Start(ctx)
	require.NoError(t, err)
	defer orch.Stop()

	// Test invalid events that will fail validation
	invalidEvents := []*domain.UnifiedEvent{
		nil, // This will fail at ProcessEvent level
		{ // This will fail validation due to missing source
			ID:        "test-no-source",
			Timestamp: time.Now(),
			Type:      domain.EventTypeSystem,
		},
		{ // This will fail validation due to old timestamp
			ID:        "test-old",
			Timestamp: time.Now().Add(-25 * time.Hour), // Older than 24h max age
			Type:      domain.EventTypeSystem,
			Source:    "test-collector",
		},
	}

	for i, event := range invalidEvents {
		err := orch.ProcessEvent(event)
		if event == nil {
			assert.Error(t, err, "Event %d should fail", i)
		} else {
			// Non-nil events should be accepted but will fail validation
			assert.NoError(t, err, "Event %d should be queued", i)
		}
	}

	// Wait for processing (longer wait for async processing)
	time.Sleep(500 * time.Millisecond)

	// Check error metrics
	metrics := orch.GetMetrics()
	t.Logf("Error Metrics: ValidationErrors=%d, ContextErrors=%d",
		metrics.ValidationErrors, metrics.ContextErrors)
	assert.Greater(t, metrics.ValidationErrors, int64(0))
}

func TestHighPerformanceOrchestrator_Metrics(t *testing.T) {
	config := &OrchestratorConfig{
		BatchSize:          5,
		WorkerCount:        1,
		BufferSize:         50,
		ProcessingTimeout:  1 * time.Second,
		MetricsInterval:    100 * time.Millisecond,
		CorrelationEnabled: false,
	}

	orch, err := NewHighPerformanceOrchestrator(config)
	require.NoError(t, err)

	ctx := context.Background()
	err = orch.Start(ctx)
	require.NoError(t, err)
	defer orch.Stop()

	// Initial metrics
	initialMetrics := orch.GetMetrics()
	assert.Equal(t, int64(0), initialMetrics.EventsProcessed)

	// Process some events
	for i := 0; i < 5; i++ {
		event := &domain.UnifiedEvent{
			ID:        fmt.Sprintf("metrics-test-%d", i),
			Type:      domain.EventTypeSystem,
			Timestamp: time.Now(),
			Source:    "test-collector",
		}
		err := orch.ProcessEvent(event)
		assert.NoError(t, err)
	}

	// Wait for processing and metrics collection
	time.Sleep(200 * time.Millisecond)

	// Check updated metrics
	finalMetrics := orch.GetMetrics()
	assert.Greater(t, finalMetrics.EventsProcessed, initialMetrics.EventsProcessed)
	assert.GreaterOrEqual(t, finalMetrics.EventsValidated, int64(0))
	assert.GreaterOrEqual(t, finalMetrics.EventsContextBuilt, int64(0))
}

func TestHighPerformanceOrchestrator_Concurrency(t *testing.T) {
	config := &OrchestratorConfig{
		BatchSize:          100,
		WorkerCount:        4,
		BufferSize:         1000,
		ProcessingTimeout:  2 * time.Second,
		MetricsInterval:    100 * time.Millisecond,
		CorrelationEnabled: false,
	}

	orch, err := NewHighPerformanceOrchestrator(config)
	require.NoError(t, err)

	ctx := context.Background()
	err = orch.Start(ctx)
	require.NoError(t, err)
	defer orch.Stop()

	// Concurrent event processing
	numGoroutines := 10
	eventsPerGoroutine := 50

	var wg sync.WaitGroup
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for i := 0; i < eventsPerGoroutine; i++ {
				event := &domain.UnifiedEvent{
					ID:        fmt.Sprintf("concurrent-%d-%d", goroutineID, i),
					Type:      domain.EventTypeSystem,
					Timestamp: time.Now(),
					Source:    "test-collector",
				}

				err := orch.ProcessEvent(event)
				assert.NoError(t, err)
			}
		}(g)
	}

	wg.Wait()

	// Wait for all processing to complete
	time.Sleep(500 * time.Millisecond)

	// Verify all events were processed
	metrics := orch.GetMetrics()
	expectedEvents := numGoroutines * eventsPerGoroutine
	t.Logf("Expected events: %d, Processed events: %d", expectedEvents, metrics.EventsProcessed)
	assert.Equal(t, int64(expectedEvents), metrics.EventsProcessed)
}

func TestHighPerformanceOrchestrator_StopWhileProcessing(t *testing.T) {
	config := &OrchestratorConfig{
		BatchSize:          50,
		WorkerCount:        2,
		BufferSize:         500,
		ProcessingTimeout:  1 * time.Second,
		MetricsInterval:    100 * time.Millisecond,
		CorrelationEnabled: false,
	}

	orch, err := NewHighPerformanceOrchestrator(config)
	require.NoError(t, err)

	ctx := context.Background()
	err = orch.Start(ctx)
	require.NoError(t, err)

	// Start processing events
	var wg sync.WaitGroup
	stopChan := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()

		for i := 0; ; i++ {
			select {
			case <-stopChan:
				return
			default:
				event := &domain.UnifiedEvent{
					ID:        fmt.Sprintf("stop-test-%d", i),
					Type:      domain.EventTypeSystem,
					Timestamp: time.Now(),
					Source:    "test-collector",
				}
				orch.ProcessEvent(event)
				time.Sleep(1 * time.Millisecond) // Small delay
			}
		}
	}()

	// Let it process for a bit
	time.Sleep(100 * time.Millisecond)

	// Stop while processing
	err = orch.Stop()
	assert.NoError(t, err)
	assert.False(t, orch.IsRunning())

	// Signal goroutine to stop
	close(stopChan)
	wg.Wait()

	// Verify we can't process after stop
	event := &domain.UnifiedEvent{
		ID:        "after-stop",
		Type:      domain.EventTypeSystem,
		Timestamp: time.Now(),
		Source:    "test-collector",
	}
	err = orch.ProcessEvent(event)
	assert.Error(t, err)
}

// Benchmark tests
func BenchmarkOrchestrator_ProcessEvent(b *testing.B) {
	config := &OrchestratorConfig{
		BatchSize:          1000,
		WorkerCount:        runtime.NumCPU(),
		BufferSize:         10000,
		ProcessingTimeout:  5 * time.Second,
		MetricsInterval:    1 * time.Second,
		CorrelationEnabled: false,
	}

	orch, err := NewHighPerformanceOrchestrator(config)
	require.NoError(b, err)

	ctx := context.Background()
	err = orch.Start(ctx)
	require.NoError(b, err)
	defer orch.Stop()

	event := &domain.UnifiedEvent{
		ID:        "benchmark-event",
		Type:      domain.EventTypeSystem,
		Timestamp: time.Now(),
		Source:    "test-collector",
		Entity: &domain.EntityContext{
			Type: "service",
			Name: "benchmark",
		},
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			orch.ProcessEvent(event)
		}
	})
}

func BenchmarkOrchestrator_ProcessBatch(b *testing.B) {
	config := &OrchestratorConfig{
		BatchSize:          1000,
		WorkerCount:        runtime.NumCPU(),
		BufferSize:         10000,
		ProcessingTimeout:  5 * time.Second,
		MetricsInterval:    1 * time.Second,
		CorrelationEnabled: false,
	}

	orch, err := NewHighPerformanceOrchestrator(config)
	require.NoError(b, err)

	ctx := context.Background()
	err = orch.Start(ctx)
	require.NoError(b, err)
	defer orch.Stop()

	// Create batch
	batch := make([]*domain.UnifiedEvent, 100)
	for i := 0; i < 100; i++ {
		batch[i] = &domain.UnifiedEvent{
			ID:        fmt.Sprintf("batch-bench-%d", i),
			Type:      domain.EventTypeSystem,
			Timestamp: time.Now(),
			Source:    "test-collector",
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		orch.ProcessBatch(batch)
	}
}
