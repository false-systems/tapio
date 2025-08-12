package correlation

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/yairfalse/tapio/pkg/domain"
)

// TestStorage for testing with simple tracking
type TestStorage struct {
	stored []CorrelationResult
	mu     sync.Mutex
	delay  time.Duration
	fail   bool
}

func (ts *TestStorage) Store(ctx context.Context, result *CorrelationResult) error {
	if ts.fail {
		return assert.AnError
	}

	if ts.delay > 0 {
		time.Sleep(ts.delay)
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.stored = append(ts.stored, *result)
	return nil
}

func (ts *TestStorage) GetRecent(ctx context.Context, limit int) ([]*CorrelationResult, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	results := make([]*CorrelationResult, 0)
	for i := range ts.stored {
		if len(results) >= limit {
			break
		}
		results = append(results, &ts.stored[i])
	}
	return results, nil
}

func (ts *TestStorage) GetByTraceID(ctx context.Context, traceID string) ([]*CorrelationResult, error) {
	return []*CorrelationResult{}, nil
}

func (ts *TestStorage) GetByTimeRange(ctx context.Context, start, end time.Time) ([]*CorrelationResult, error) {
	return []*CorrelationResult{}, nil
}

func (ts *TestStorage) GetByResource(ctx context.Context, resourceType, namespace, name string) ([]*CorrelationResult, error) {
	return []*CorrelationResult{}, nil
}

func (ts *TestStorage) Cleanup(ctx context.Context, olderThan time.Duration) error {
	return nil
}

func (ts *TestStorage) HealthCheck(ctx context.Context) error {
	return nil
}

func (ts *TestStorage) GetStored() []CorrelationResult {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return append([]CorrelationResult{}, ts.stored...)
}

func (ts *TestStorage) Count() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return len(ts.stored)
}

// Enhanced TestCorrelator for processor testing
type ProcessorTestCorrelator struct {
	name    string
	results []*CorrelationResult
	failErr error
}

func (tc *ProcessorTestCorrelator) Process(ctx context.Context, event *domain.UnifiedEvent) ([]*CorrelationResult, error) {
	if tc.failErr != nil {
		return nil, tc.failErr
	}
	return tc.results, nil
}

func (tc *ProcessorTestCorrelator) Name() string {
	return tc.name
}

func TestNewEventProcessor(t *testing.T) {
	logger := zaptest.NewLogger(t)
	correlators := []Correlator{&TestCorrelator{name: "test"}}
	eventChan := make(chan *domain.UnifiedEvent, 10)
	resultChan := make(chan *CorrelationResult, 10)
	storageJobChan := make(chan *storageJob, 10)
	storage := &TestStorage{}

	// Setup test metrics
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	factory := NewMetricFactory("test", logger)
	metrics, err := factory.CreateEngineMetrics()
	require.NoError(t, err)

	processor := NewEventProcessor(
		logger, correlators, eventChan, resultChan,
		storage, storageJobChan, metrics,
	)

	assert.NotNil(t, processor)
	assert.Equal(t, logger, processor.logger)
	assert.Equal(t, correlators, processor.correlators)
	assert.Equal(t, storage, processor.storage)
	assert.Equal(t, metrics, processor.metrics)
	assert.Equal(t, EventWorker, processor.GetWorkerType())
}

func TestEventProcessor_ProcessWork(t *testing.T) {
	logger := zaptest.NewLogger(t)
	eventChan := make(chan *domain.UnifiedEvent, 10)
	resultChan := make(chan *CorrelationResult, 10)
	storageJobChan := make(chan *storageJob, 10)
	storage := &TestStorage{}

	// Setup test metrics
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	factory := NewMetricFactory("test", logger)
	metrics, err := factory.CreateEngineMetrics()
	require.NoError(t, err)

	// Create test correlator that returns results
	testCorrelator := &ProcessorTestCorrelator{
		name: "test",
		results: []*CorrelationResult{
			{
				ID:         "test-result-1",
				Type:       "test",
				Confidence: 0.9,
			},
		},
	}

	processor := NewEventProcessor(
		logger, []Correlator{testCorrelator}, eventChan, resultChan,
		storage, storageJobChan, metrics,
	)

	ctx := context.Background()
	testEvent := &domain.UnifiedEvent{
		ID:   "test-event",
		Type: domain.EventTypeKubernetes,
	}

	t.Run("successful processing", func(t *testing.T) {
		err := processor.ProcessWork(ctx, testEvent)
		require.NoError(t, err)

		// Verify result was sent
		select {
		case result := <-resultChan:
			assert.Equal(t, "test-result-1", result.ID)
		case <-time.After(time.Second):
			t.Fatal("Expected result not received")
		}

		// Verify storage job was queued
		select {
		case job := <-storageJobChan:
			assert.Equal(t, "test-result-1", job.result.ID)
		case <-time.After(time.Second):
			t.Fatal("Expected storage job not received")
		}
	})

	t.Run("invalid work item type", func(t *testing.T) {
		err := processor.ProcessWork(ctx, "invalid")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid work item type")
	})
}

func TestEventProcessor_GetWorkChannel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	eventChan := make(chan *domain.UnifiedEvent, 10)
	resultChan := make(chan *CorrelationResult, 10)
	storageJobChan := make(chan *storageJob, 10)

	// Setup test metrics
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	factory := NewMetricFactory("test", logger)
	metrics, err := factory.CreateEngineMetrics()
	require.NoError(t, err)

	processor := NewEventProcessor(
		logger, []Correlator{}, eventChan, resultChan,
		nil, storageJobChan, metrics,
	)

	workChan := processor.GetWorkChannel()
	assert.NotNil(t, workChan)

	// Send test event
	testEvent := &domain.UnifiedEvent{ID: "test", Type: domain.EventTypeKubernetes}
	eventChan <- testEvent
	close(eventChan)

	// Verify it comes through work channel
	select {
	case workItem := <-workChan:
		event, ok := workItem.(*domain.UnifiedEvent)
		require.True(t, ok)
		assert.Equal(t, "test", event.ID)
	case <-time.After(time.Second):
		t.Fatal("Expected work item not received")
	}
}

func TestEventProcessor_ProcessWithCorrelator(t *testing.T) {
	logger := zaptest.NewLogger(t)
	eventChan := make(chan *domain.UnifiedEvent, 10)
	resultChan := make(chan *CorrelationResult, 10)
	storageJobChan := make(chan *storageJob, 10)

	// Setup test metrics
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	factory := NewMetricFactory("test", logger)
	metrics, err := factory.CreateEngineMetrics()
	require.NoError(t, err)

	processor := NewEventProcessor(
		logger, []Correlator{}, eventChan, resultChan,
		nil, storageJobChan, metrics,
	)

	ctx := context.Background()
	testEvent := &domain.UnifiedEvent{
		ID:   "test-event",
		Type: domain.EventTypeKubernetes,
	}

	t.Run("successful correlation", func(t *testing.T) {
		correlator := &ProcessorTestCorrelator{
			name: "success",
			results: []*CorrelationResult{
				{ID: "result1", Type: "test", Confidence: 0.8},
			},
		}

		err := processor.processWithCorrelator(ctx, testEvent, correlator)
		assert.NoError(t, err)

		// Verify result was processed
		select {
		case result := <-resultChan:
			assert.Equal(t, "result1", result.ID)
		case <-time.After(time.Second):
			t.Fatal("Expected result not received")
		}
	})

	t.Run("correlator error", func(t *testing.T) {
		correlator := &ProcessorTestCorrelator{
			name:    "error",
			failErr: assert.AnError,
		}

		err := processor.processWithCorrelator(ctx, testEvent, correlator)
		assert.Error(t, err)
		assert.Equal(t, assert.AnError, err)
	})
}

func TestNewStorageProcessor(t *testing.T) {
	logger := zaptest.NewLogger(t)
	storage := &TestStorage{}
	storageJobChan := make(chan *storageJob, 10)

	// Setup test metrics
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	factory := NewMetricFactory("test", logger)
	metrics, err := factory.CreateEngineMetrics()
	require.NoError(t, err)

	processor := NewStorageProcessor(logger, storage, storageJobChan, metrics)

	assert.NotNil(t, processor)
	assert.Equal(t, logger, processor.logger)
	assert.Equal(t, storage, processor.storage)
	assert.Equal(t, metrics, processor.metrics)
	assert.Equal(t, StorageWorker, processor.GetWorkerType())
}

func TestStorageProcessor_ProcessWork(t *testing.T) {
	logger := zaptest.NewLogger(t)
	storage := &TestStorage{}
	storageJobChan := make(chan *storageJob, 10)

	// Setup test metrics
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	factory := NewMetricFactory("test", logger)
	metrics, err := factory.CreateEngineMetrics()
	require.NoError(t, err)

	processor := NewStorageProcessor(logger, storage, storageJobChan, metrics)

	ctx := context.Background()

	t.Run("successful storage", func(t *testing.T) {
		job := &storageJob{
			result: &CorrelationResult{
				ID:         "test-result",
				Type:       "test",
				Confidence: 0.9,
			},
			timestamp: time.Now(),
		}

		err := processor.ProcessWork(ctx, job)
		require.NoError(t, err)

		// Verify result was stored
		stored := storage.GetStored()
		require.Len(t, stored, 1)
		assert.Equal(t, "test-result", stored[0].ID)
	})

	t.Run("storage failure", func(t *testing.T) {
		storage.fail = true
		defer func() { storage.fail = false }()

		job := &storageJob{
			result: &CorrelationResult{
				ID:         "fail-result",
				Type:       "test",
				Confidence: 0.9,
			},
			timestamp: time.Now(),
		}

		err := processor.ProcessWork(ctx, job)
		assert.Error(t, err)
		assert.Equal(t, assert.AnError, err)
	})

	t.Run("invalid work item type", func(t *testing.T) {
		err := processor.ProcessWork(ctx, "invalid")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid work item type")
	})
}

func TestStorageProcessor_GetWorkChannel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	storage := &TestStorage{}
	storageJobChan := make(chan *storageJob, 10)

	// Setup test metrics
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	factory := NewMetricFactory("test", logger)
	metrics, err := factory.CreateEngineMetrics()
	require.NoError(t, err)

	processor := NewStorageProcessor(logger, storage, storageJobChan, metrics)

	workChan := processor.GetWorkChannel()
	assert.NotNil(t, workChan)

	// Send test job
	testJob := &storageJob{
		result:    &CorrelationResult{ID: "test", Type: "test"},
		timestamp: time.Now(),
	}
	storageJobChan <- testJob
	close(storageJobChan)

	// Verify it comes through work channel
	select {
	case workItem := <-workChan:
		job, ok := workItem.(*storageJob)
		require.True(t, ok)
		assert.Equal(t, "test", job.result.ID)
	case <-time.After(time.Second):
		t.Fatal("Expected work item not received")
	}
}

func TestEventProcessor_AsyncStoreResult(t *testing.T) {
	logger := zaptest.NewLogger(t)
	eventChan := make(chan *domain.UnifiedEvent, 10)
	resultChan := make(chan *CorrelationResult, 10)
	storageJobChan := make(chan *storageJob, 1) // Small buffer to test queue full
	storage := &TestStorage{}

	// Setup test metrics
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	factory := NewMetricFactory("test", logger)
	metrics, err := factory.CreateEngineMetrics()
	require.NoError(t, err)

	processor := NewEventProcessor(
		logger, []Correlator{}, eventChan, resultChan,
		storage, storageJobChan, metrics,
	)

	ctx := context.Background()

	t.Run("successful job submission", func(t *testing.T) {
		result := &CorrelationResult{
			ID:         "test-result",
			Type:       "test",
			Confidence: 0.9,
		}

		processor.asyncStoreResult(ctx, result)

		// Verify job was queued
		select {
		case job := <-storageJobChan:
			assert.Equal(t, "test-result", job.result.ID)
			assert.NotSame(t, result, job.result) // Should be a copy, not the same instance
		case <-time.After(time.Second):
			t.Fatal("Expected storage job not received")
		}
	})

	t.Run("queue full rejection", func(t *testing.T) {
		// Fill the queue
		storageJobChan <- &storageJob{
			result:    &CorrelationResult{ID: "filler"},
			timestamp: time.Now(),
		}

		result := &CorrelationResult{
			ID:         "rejected-result",
			Type:       "test",
			Confidence: 0.9,
		}

		// This should not block and should log a warning
		processor.asyncStoreResult(ctx, result)

		// Queue should still have the filler job
		select {
		case job := <-storageJobChan:
			assert.Equal(t, "filler", job.result.ID)
		case <-time.After(time.Second):
			t.Fatal("Expected filler job not received")
		}
	})
}

func TestEventProcessor_SendResult(t *testing.T) {
	logger := zaptest.NewLogger(t)
	eventChan := make(chan *domain.UnifiedEvent, 10)
	resultChan := make(chan *CorrelationResult, 1) // Small buffer to test channel full
	storageJobChan := make(chan *storageJob, 10)

	// Setup test metrics
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	factory := NewMetricFactory("test", logger)
	metrics, err := factory.CreateEngineMetrics()
	require.NoError(t, err)

	processor := NewEventProcessor(
		logger, []Correlator{}, eventChan, resultChan,
		nil, storageJobChan, metrics,
	)

	ctx := context.Background()

	t.Run("successful send", func(t *testing.T) {
		result := &CorrelationResult{
			ID:         "test-result",
			Type:       "test",
			Confidence: 0.9,
		}

		processor.sendResult(ctx, result)

		// Verify result was sent
		select {
		case sentResult := <-resultChan:
			assert.Equal(t, "test-result", sentResult.ID)
		case <-time.After(time.Second):
			t.Fatal("Expected result not received")
		}
	})

	t.Run("channel full drop", func(t *testing.T) {
		// Fill the channel
		resultChan <- &CorrelationResult{ID: "filler"}

		result := &CorrelationResult{
			ID:         "dropped-result",
			Type:       "test",
			Confidence: 0.9,
		}

		// This should not block and should log a warning
		processor.sendResult(ctx, result)

		// Channel should still have the filler result
		select {
		case sentResult := <-resultChan:
			assert.Equal(t, "filler", sentResult.ID)
		case <-time.After(time.Second):
			t.Fatal("Expected filler result not received")
		}
	})
}

// Benchmark processors
func BenchmarkEventProcessor_ProcessWork(b *testing.B) {
	logger := zap.NewNop()
	eventChan := make(chan *domain.UnifiedEvent, 1000)
	resultChan := make(chan *CorrelationResult, 1000)
	storageJobChan := make(chan *storageJob, 1000)

	// Setup test metrics
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	factory := NewMetricFactory("benchmark", logger)
	metrics, err := factory.CreateEngineMetrics()
	if err != nil {
		b.Fatal(err)
	}

	correlator := &ProcessorTestCorrelator{
		name: "benchmark",
		results: []*CorrelationResult{
			{ID: "result", Type: "test", Confidence: 0.8},
		},
	}

	processor := NewEventProcessor(
		logger, []Correlator{correlator}, eventChan, resultChan,
		&TestStorage{}, storageJobChan, metrics,
	)

	ctx := context.Background()
	testEvent := &domain.UnifiedEvent{
		ID:   "benchmark-event",
		Type: domain.EventTypeKubernetes,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		processor.ProcessWork(ctx, testEvent)
		// Drain channels
		<-resultChan
		<-storageJobChan
	}
}

func BenchmarkStorageProcessor_ProcessWork(b *testing.B) {
	logger := zap.NewNop()
	storage := &TestStorage{}
	storageJobChan := make(chan *storageJob, 1000)

	// Setup test metrics
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	factory := NewMetricFactory("benchmark", logger)
	metrics, err := factory.CreateEngineMetrics()
	if err != nil {
		b.Fatal(err)
	}

	processor := NewStorageProcessor(logger, storage, storageJobChan, metrics)

	ctx := context.Background()
	job := &storageJob{
		result: &CorrelationResult{
			ID:         "benchmark-result",
			Type:       "test",
			Confidence: 0.9,
		},
		timestamp: time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		processor.ProcessWork(ctx, job)
	}
}
