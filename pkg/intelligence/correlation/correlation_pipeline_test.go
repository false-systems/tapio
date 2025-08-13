package correlation

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap/zaptest"
)

// Mock correlator for testing
type mockCorrelator struct {
	name         string
	processFunc  func(context.Context, *domain.UnifiedEvent) ([]*CorrelationResult, error)
	processDelay time.Duration
	processCount int32
	shouldFail   bool
	failureError error
}

func (m *mockCorrelator) Name() string {
	return m.name
}

func (m *mockCorrelator) Process(ctx context.Context, event *domain.UnifiedEvent) ([]*CorrelationResult, error) {
	atomic.AddInt32(&m.processCount, 1)

	if m.processDelay > 0 {
		select {
		case <-time.After(m.processDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if m.shouldFail {
		if m.failureError != nil {
			return nil, m.failureError
		}
		return nil, errors.New("mock correlator failure")
	}

	if m.processFunc != nil {
		return m.processFunc(ctx, event)
	}

	// Default: return a single result
	return []*CorrelationResult{
		{
			ID:         fmt.Sprintf("%s-result-%s", m.name, event.ID),
			Type:       "test",
			Confidence: 0.8,
			Events:     []string{event.ID},
			Message:    fmt.Sprintf("Test correlation from %s", m.name),
		},
	}, nil
}

func (m *mockCorrelator) GetProcessCount() int32 {
	return atomic.LoadInt32(&m.processCount)
}

func (m *mockCorrelator) ResetProcessCount() {
	atomic.StoreInt32(&m.processCount, 0)
}

// Mock result handler for testing
type mockResultHandler struct {
	handleFunc   func(context.Context, []*CorrelationResult) error
	results      []*CorrelationResult
	handleCount  int32
	shouldFail   bool
	failureError error
	mu           sync.Mutex
}

func (h *mockResultHandler) HandleResults(ctx context.Context, results []*CorrelationResult) error {
	atomic.AddInt32(&h.handleCount, 1)

	h.mu.Lock()
	h.results = append(h.results, results...)
	h.mu.Unlock()

	if h.shouldFail {
		if h.failureError != nil {
			return h.failureError
		}
		return errors.New("mock result handler failure")
	}

	if h.handleFunc != nil {
		return h.handleFunc(ctx, results)
	}

	return nil
}

func (h *mockResultHandler) GetResults() []*CorrelationResult {
	h.mu.Lock()
	defer h.mu.Unlock()
	results := make([]*CorrelationResult, len(h.results))
	copy(results, h.results)
	return results
}

func (h *mockResultHandler) GetHandleCount() int32 {
	return atomic.LoadInt32(&h.handleCount)
}

func (h *mockResultHandler) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.results = h.results[:0]
	atomic.StoreInt32(&h.handleCount, 0)
}

func TestNewCorrelationPipeline(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracer := otel.Tracer("test")
	timeoutCoordinator := NewTimeoutCoordinator(logger, tracer, DefaultTimeoutConfig())
	resultHandler := &mockResultHandler{}
	errorAggregator := NewSimpleErrorAggregator(logger)

	correlators := []Correlator{
		&mockCorrelator{name: "test1"},
		&mockCorrelator{name: "test2"},
	}

	tests := []struct {
		name          string
		correlators   []Correlator
		config        *PipelineConfig
		expectedError string
	}{
		{
			name:          "empty correlators",
			correlators:   []Correlator{},
			config:        &PipelineConfig{},
			expectedError: "at least one correlator is required",
		},
		{
			name:          "nil config",
			correlators:   correlators,
			config:        nil,
			expectedError: "pipeline config is required",
		},
		{
			name:        "nil timeout coordinator",
			correlators: correlators,
			config: &PipelineConfig{
				ResultHandler:   resultHandler,
				ErrorAggregator: errorAggregator,
			},
			expectedError: "timeout coordinator is required",
		},
		{
			name:        "nil result handler",
			correlators: correlators,
			config: &PipelineConfig{
				TimeoutCoordinator: timeoutCoordinator,
				ErrorAggregator:    errorAggregator,
			},
			expectedError: "result handler is required",
		},
		{
			name:        "nil error aggregator",
			correlators: correlators,
			config: &PipelineConfig{
				TimeoutCoordinator: timeoutCoordinator,
				ResultHandler:      resultHandler,
			},
			expectedError: "error aggregator is required",
		},
		{
			name:        "valid configuration",
			correlators: correlators,
			config: &PipelineConfig{
				Mode:               PipelineModeSequential,
				MaxConcurrency:     2,
				TimeoutCoordinator: timeoutCoordinator,
				ResultHandler:      resultHandler,
				ErrorAggregator:    errorAggregator,
			},
			expectedError: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pipeline, err := NewCorrelationPipeline(tt.correlators, tt.config, logger)

			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				assert.Nil(t, pipeline)
			} else {
				require.NoError(t, err)
				require.NotNil(t, pipeline)
				assert.Equal(t, len(tt.correlators), len(pipeline.GetCorrelators()))
				assert.Equal(t, tt.config.Mode, pipeline.GetMode())
				assert.Equal(t, tt.config.MaxConcurrency, pipeline.GetMaxConcurrency())
			}
		})
	}
}

func TestCorrelationPipeline_ProcessSequential(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracer := otel.Tracer("test")
	timeoutCoordinator := NewTimeoutCoordinator(logger, tracer, DefaultTimeoutConfig())
	resultHandler := &mockResultHandler{}
	errorAggregator := NewSimpleErrorAggregator(logger)

	correlators := []Correlator{
		&mockCorrelator{name: "test1"},
		&mockCorrelator{name: "test2"},
		&mockCorrelator{name: "test3"},
	}

	config := &PipelineConfig{
		Mode:               PipelineModeSequential,
		MaxConcurrency:     1,
		TimeoutCoordinator: timeoutCoordinator,
		ResultHandler:      resultHandler,
		ErrorAggregator:    errorAggregator,
	}

	pipeline, err := NewCorrelationPipeline(correlators, config, logger)
	require.NoError(t, err)

	event := &domain.UnifiedEvent{
		ID:   "test-event-1",
		Type: domain.EventTypeKubernetes,
	}

	ctx := context.Background()
	err = pipeline.Process(ctx, event)
	require.NoError(t, err)

	// Verify all correlators were called
	for i, correlator := range correlators {
		mockCorr := correlator.(*mockCorrelator)
		assert.Equal(t, int32(1), mockCorr.GetProcessCount(), "correlator %d should be called once", i)
	}

	// Verify results were handled
	assert.Equal(t, int32(1), resultHandler.GetHandleCount())
	results := resultHandler.GetResults()
	assert.Equal(t, 3, len(results)) // One result per correlator

	// Verify no errors were aggregated
	pipelineErrors := errorAggregator.GetErrors()
	assert.Empty(t, pipelineErrors)
}

func TestCorrelationPipeline_ProcessParallel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracer := otel.Tracer("test")
	timeoutCoordinator := NewTimeoutCoordinator(logger, tracer, DefaultTimeoutConfig())
	resultHandler := &mockResultHandler{}
	errorAggregator := NewSimpleErrorAggregator(logger)

	correlators := []Correlator{
		&mockCorrelator{name: "test1", processDelay: 50 * time.Millisecond},
		&mockCorrelator{name: "test2", processDelay: 30 * time.Millisecond},
		&mockCorrelator{name: "test3", processDelay: 40 * time.Millisecond},
	}

	config := &PipelineConfig{
		Mode:               PipelineModeParallel,
		MaxConcurrency:     3,
		TimeoutCoordinator: timeoutCoordinator,
		ResultHandler:      resultHandler,
		ErrorAggregator:    errorAggregator,
	}

	pipeline, err := NewCorrelationPipeline(correlators, config, logger)
	require.NoError(t, err)

	event := &domain.UnifiedEvent{
		ID:   "test-event-1",
		Type: domain.EventTypeKubernetes,
	}

	start := time.Now()
	ctx := context.Background()
	err = pipeline.Process(ctx, event)
	duration := time.Since(start)
	require.NoError(t, err)

	// Parallel processing should complete faster than sequential
	// With 50ms max delay, parallel should complete in ~50ms vs ~120ms sequential
	assert.Less(t, duration, 100*time.Millisecond, "parallel processing should be faster")

	// Verify all correlators were called
	for i, correlator := range correlators {
		mockCorr := correlator.(*mockCorrelator)
		assert.Equal(t, int32(1), mockCorr.GetProcessCount(), "correlator %d should be called once", i)
	}

	// Verify results were handled
	assert.Equal(t, int32(1), resultHandler.GetHandleCount())
	results := resultHandler.GetResults()
	assert.Equal(t, 3, len(results)) // One result per correlator
}

func TestCorrelationPipeline_HandleErrors(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracer := otel.Tracer("test")
	timeoutCoordinator := NewTimeoutCoordinator(logger, tracer, DefaultTimeoutConfig())
	resultHandler := &mockResultHandler{}
	errorAggregator := NewSimpleErrorAggregator(logger)

	correlators := []Correlator{
		&mockCorrelator{name: "success1"},
		&mockCorrelator{name: "failure1", shouldFail: true, failureError: errors.New("test error 1")},
		&mockCorrelator{name: "success2"},
		&mockCorrelator{name: "failure2", shouldFail: true, failureError: errors.New("test error 2")},
	}

	config := &PipelineConfig{
		Mode:               PipelineModeSequential,
		TimeoutCoordinator: timeoutCoordinator,
		ResultHandler:      resultHandler,
		ErrorAggregator:    errorAggregator,
	}

	pipeline, err := NewCorrelationPipeline(correlators, config, logger)
	require.NoError(t, err)

	event := &domain.UnifiedEvent{
		ID:   "test-event-1",
		Type: domain.EventTypeKubernetes,
	}

	ctx := context.Background()
	err = pipeline.Process(ctx, event)
	require.NoError(t, err) // Pipeline should not fail even if some correlators fail

	// Verify all correlators were called
	for _, correlator := range correlators {
		mockCorr := correlator.(*mockCorrelator)
		assert.Equal(t, int32(1), mockCorr.GetProcessCount())
	}

	// Verify results were handled (only successful correlators)
	results := resultHandler.GetResults()
	assert.Equal(t, 2, len(results)) // Only 2 successful correlators

	// Verify errors were aggregated
	pipelineErrors := errorAggregator.GetErrors()
	assert.Equal(t, 2, len(pipelineErrors)) // 2 failed correlators
	assert.Equal(t, "failure1", pipelineErrors[0].CorrelatorName)
	assert.Equal(t, "failure2", pipelineErrors[1].CorrelatorName)
	assert.Contains(t, pipelineErrors[0].Error.Error(), "test error 1")
	assert.Contains(t, pipelineErrors[1].Error.Error(), "test error 2")
}

func TestCorrelationPipeline_ConcurrencyLimit(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracer := otel.Tracer("test")
	timeoutCoordinator := NewTimeoutCoordinator(logger, tracer, DefaultTimeoutConfig())
	resultHandler := &mockResultHandler{}
	errorAggregator := NewSimpleErrorAggregator(logger)

	var concurrentCount int32
	var maxConcurrent int32

	correlators := make([]Correlator, 5)
	for i := 0; i < 5; i++ {
		correlators[i] = &mockCorrelator{
			name: fmt.Sprintf("test%d", i+1),
			processFunc: func(ctx context.Context, event *domain.UnifiedEvent) ([]*CorrelationResult, error) {
				current := atomic.AddInt32(&concurrentCount, 1)
				defer atomic.AddInt32(&concurrentCount, -1)

				// Update max concurrent if this is higher
				for {
					max := atomic.LoadInt32(&maxConcurrent)
					if current <= max || atomic.CompareAndSwapInt32(&maxConcurrent, max, current) {
						break
					}
				}

				// Simulate work
				time.Sleep(100 * time.Millisecond)

				return []*CorrelationResult{
					{
						ID:         fmt.Sprintf("result-%s", event.ID),
						Type:       "test",
						Confidence: 0.8,
						Events:     []string{event.ID},
						Message:    "Test correlation",
					},
				}, nil
			},
		}
	}

	config := &PipelineConfig{
		Mode:               PipelineModeParallel,
		MaxConcurrency:     2, // Limit to 2 concurrent
		TimeoutCoordinator: timeoutCoordinator,
		ResultHandler:      resultHandler,
		ErrorAggregator:    errorAggregator,
	}

	pipeline, err := NewCorrelationPipeline(correlators, config, logger)
	require.NoError(t, err)

	event := &domain.UnifiedEvent{
		ID:   "test-event-1",
		Type: domain.EventTypeKubernetes,
	}

	ctx := context.Background()
	err = pipeline.Process(ctx, event)
	require.NoError(t, err)

	// Verify concurrency was limited
	maxObserved := atomic.LoadInt32(&maxConcurrent)
	assert.LessOrEqual(t, maxObserved, int32(2), "should not exceed max concurrency of 2")
	assert.Greater(t, maxObserved, int32(0), "should have some concurrency")
}

func TestCorrelationPipeline_ContextCancellation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracer := otel.Tracer("test")
	timeoutCoordinator := NewTimeoutCoordinator(logger, tracer, DefaultTimeoutConfig())
	resultHandler := &mockResultHandler{}
	errorAggregator := NewSimpleErrorAggregator(logger)

	correlators := []Correlator{
		&mockCorrelator{name: "slow1", processDelay: 500 * time.Millisecond},
		&mockCorrelator{name: "slow2", processDelay: 500 * time.Millisecond},
	}

	config := &PipelineConfig{
		Mode:               PipelineModeSequential,
		TimeoutCoordinator: timeoutCoordinator,
		ResultHandler:      resultHandler,
		ErrorAggregator:    errorAggregator,
	}

	pipeline, err := NewCorrelationPipeline(correlators, config, logger)
	require.NoError(t, err)

	event := &domain.UnifiedEvent{
		ID:   "test-event-1",
		Type: domain.EventTypeKubernetes,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err = pipeline.Process(ctx, event)
	duration := time.Since(start)

	// Should fail due to context cancellation
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context")

	// Should complete quickly due to cancellation
	assert.Less(t, duration, 200*time.Millisecond)
}

func TestDefaultResultHandler(t *testing.T) {
	logger := zaptest.NewLogger(t)
	resultsChan := make(chan *CorrelationResult, 10)

	handler := NewDefaultResultHandler(resultsChan, logger)

	results := []*CorrelationResult{
		{ID: "result1", Type: "test"},
		{ID: "result2", Type: "test"},
		nil, // Should be skipped
		{ID: "result3", Type: "test"},
	}

	ctx := context.Background()
	err := handler.HandleResults(ctx, results)
	require.NoError(t, err)

	// Should receive 3 results (nil is skipped)
	assert.Equal(t, 3, len(resultsChan))

	// Verify results
	result1 := <-resultsChan
	assert.Equal(t, "result1", result1.ID)

	result2 := <-resultsChan
	assert.Equal(t, "result2", result2.ID)

	result3 := <-resultsChan
	assert.Equal(t, "result3", result3.ID)
}

func TestDefaultResultHandler_ContextCancellation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	resultsChan := make(chan *CorrelationResult) // No buffer - will block

	handler := NewDefaultResultHandler(resultsChan, logger)

	results := []*CorrelationResult{
		{ID: "result1", Type: "test"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := handler.HandleResults(ctx, results)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context cancelled")
}

func TestSimpleErrorAggregator(t *testing.T) {
	logger := zaptest.NewLogger(t)
	aggregator := NewSimpleErrorAggregator(logger)

	// Test initial state
	pipelineErrors := aggregator.GetErrors()
	assert.Empty(t, pipelineErrors)

	// Record some errors
	aggregator.RecordError("correlator1", "event1", errors.New("error 1"))
	aggregator.RecordError("correlator2", "event1", errors.New("context deadline exceeded"))
	aggregator.RecordError("correlator3", "event2", errors.New("engine is shutting down"))

	// Get errors
	errorList := aggregator.GetErrors()
	require.Equal(t, 3, len(errorList))

	// Verify error details
	assert.Equal(t, "correlator1", errorList[0].CorrelatorName)
	assert.Equal(t, "event1", errorList[0].EventID)
	assert.Equal(t, "correlator_failed", errorList[0].ErrorType)

	assert.Equal(t, "correlator2", errorList[1].CorrelatorName)
	assert.Equal(t, "correlator_timeout", errorList[1].ErrorType)

	assert.Equal(t, "correlator3", errorList[2].CorrelatorName)
	assert.Equal(t, "engine_shutdown", errorList[2].ErrorType)

	// Test reset
	aggregator.Reset()
	pipelineErrors = aggregator.GetErrors()
	assert.Empty(t, pipelineErrors)
}

func TestCorrelationPipeline_ModeAndConcurrencyGettersSetters(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tracer := otel.Tracer("test")
	timeoutCoordinator := NewTimeoutCoordinator(logger, tracer, DefaultTimeoutConfig())
	resultHandler := &mockResultHandler{}
	errorAggregator := NewSimpleErrorAggregator(logger)

	correlators := []Correlator{
		&mockCorrelator{name: "test1"},
		&mockCorrelator{name: "test2"},
	}

	config := &PipelineConfig{
		Mode:               PipelineModeSequential,
		MaxConcurrency:     2,
		TimeoutCoordinator: timeoutCoordinator,
		ResultHandler:      resultHandler,
		ErrorAggregator:    errorAggregator,
	}

	pipeline, err := NewCorrelationPipeline(correlators, config, logger)
	require.NoError(t, err)

	// Test mode getter/setter
	assert.Equal(t, PipelineModeSequential, pipeline.GetMode())
	pipeline.SetMode(PipelineModeParallel)
	assert.Equal(t, PipelineModeParallel, pipeline.GetMode())

	// Test concurrency getter/setter
	assert.Equal(t, 2, pipeline.GetMaxConcurrency())
	pipeline.SetMaxConcurrency(4)
	assert.Equal(t, 4, pipeline.GetMaxConcurrency())

	// Test invalid concurrency (should default to number of correlators)
	pipeline.SetMaxConcurrency(-1)
	assert.Equal(t, 2, pipeline.GetMaxConcurrency()) // Number of correlators
}

func BenchmarkCorrelationPipeline_Sequential(b *testing.B) {
	logger := zaptest.NewLogger(b)
	tracer := otel.Tracer("test")
	timeoutCoordinator := NewTimeoutCoordinator(logger, tracer, DefaultTimeoutConfig())
	resultHandler := &mockResultHandler{}
	errorAggregator := NewSimpleErrorAggregator(logger)

	correlators := []Correlator{
		&mockCorrelator{name: "bench1"},
		&mockCorrelator{name: "bench2"},
		&mockCorrelator{name: "bench3"},
	}

	config := &PipelineConfig{
		Mode:               PipelineModeSequential,
		TimeoutCoordinator: timeoutCoordinator,
		ResultHandler:      resultHandler,
		ErrorAggregator:    errorAggregator,
	}

	pipeline, err := NewCorrelationPipeline(correlators, config, logger)
	require.NoError(b, err)

	event := &domain.UnifiedEvent{
		ID:   "bench-event",
		Type: domain.EventTypeKubernetes,
	}

	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err := pipeline.Process(ctx, event)
		if err != nil {
			b.Fatal(err)
		}
		resultHandler.Reset()
		errorAggregator.Reset()
	}
}

func BenchmarkCorrelationPipeline_Parallel(b *testing.B) {
	logger := zaptest.NewLogger(b)
	tracer := otel.Tracer("test")
	timeoutCoordinator := NewTimeoutCoordinator(logger, tracer, DefaultTimeoutConfig())
	resultHandler := &mockResultHandler{}
	errorAggregator := NewSimpleErrorAggregator(logger)

	correlators := []Correlator{
		&mockCorrelator{name: "bench1"},
		&mockCorrelator{name: "bench2"},
		&mockCorrelator{name: "bench3"},
	}

	config := &PipelineConfig{
		Mode:               PipelineModeParallel,
		MaxConcurrency:     3,
		TimeoutCoordinator: timeoutCoordinator,
		ResultHandler:      resultHandler,
		ErrorAggregator:    errorAggregator,
	}

	pipeline, err := NewCorrelationPipeline(correlators, config, logger)
	require.NoError(b, err)

	event := &domain.UnifiedEvent{
		ID:   "bench-event",
		Type: domain.EventTypeKubernetes,
	}

	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err := pipeline.Process(ctx, event)
		if err != nil {
			b.Fatal(err)
		}
		resultHandler.Reset()
		errorAggregator.Reset()
	}
}
