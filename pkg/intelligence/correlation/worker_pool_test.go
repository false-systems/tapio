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
)

// TestWorkerProcessor implements WorkerProcessor for testing
type TestWorkerProcessor struct {
	workerType    WorkerType
	workChannel   chan interface{}
	processedWork []interface{}
	mu            sync.Mutex
	processFunc   func(ctx context.Context, workItem interface{}) error
}

func NewTestWorkerProcessor(workerType WorkerType, bufferSize int) *TestWorkerProcessor {
	return &TestWorkerProcessor{
		workerType:    workerType,
		workChannel:   make(chan interface{}, bufferSize),
		processedWork: make([]interface{}, 0),
	}
}

func (twp *TestWorkerProcessor) ProcessWork(ctx context.Context, workItem interface{}) error {
	twp.mu.Lock()
	defer twp.mu.Unlock()

	twp.processedWork = append(twp.processedWork, workItem)

	if twp.processFunc != nil {
		return twp.processFunc(ctx, workItem)
	}
	return nil
}

func (twp *TestWorkerProcessor) GetWorkChannel() <-chan interface{} {
	return twp.workChannel
}

func (twp *TestWorkerProcessor) GetWorkerType() WorkerType {
	return twp.workerType
}

func (twp *TestWorkerProcessor) SendWork(workItem interface{}) bool {
	select {
	case twp.workChannel <- workItem:
		return true
	default:
		return false
	}
}

func (twp *TestWorkerProcessor) Close() {
	close(twp.workChannel)
}

func (twp *TestWorkerProcessor) GetProcessedWork() []interface{} {
	twp.mu.Lock()
	defer twp.mu.Unlock()
	return append([]interface{}{}, twp.processedWork...)
}

func (twp *TestWorkerProcessor) GetProcessedCount() int {
	twp.mu.Lock()
	defer twp.mu.Unlock()
	return len(twp.processedWork)
}

func TestWorkerType_String(t *testing.T) {
	tests := []struct {
		workerType WorkerType
		expected   string
	}{
		{EventWorker, "event"},
		{StorageWorker, "storage"},
		{WorkerType(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.workerType.String())
		})
	}
}

func TestNewWorkerPoolManager(t *testing.T) {
	logger := zaptest.NewLogger(t)
	var wg sync.WaitGroup

	// Setup test metric provider
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	// Create test metrics
	factory := NewMetricFactory("test", logger)
	metrics, err := factory.CreateEngineMetrics()
	require.NoError(t, err)

	wpm := NewWorkerPoolManager(logger, 2, 1, metrics, &wg)

	assert.NotNil(t, wpm)
	assert.Equal(t, logger, wpm.logger)
	assert.NotNil(t, wpm.tracer)
	assert.NotNil(t, wpm.ctx)
	assert.Equal(t, &wg, wpm.wg)
}

func TestWorkerPoolManager_StartWorkerPools(t *testing.T) {
	logger := zaptest.NewLogger(t)
	var wg sync.WaitGroup

	// Setup test metric provider
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	// Create test metrics
	factory := NewMetricFactory("test", logger)
	metrics, err := factory.CreateEngineMetrics()
	require.NoError(t, err)

	wpm := NewWorkerPoolManager(logger, 2, 1, metrics, &wg)

	// Create test processors
	eventProcessor := NewTestWorkerProcessor(EventWorker, 10)
	storageProcessor := NewTestWorkerProcessor(StorageWorker, 10)

	err = wpm.StartWorkerPools(eventProcessor, storageProcessor, 2, 1)
	require.NoError(t, err)

	// Verify worker pools were created
	assert.NotNil(t, wpm.eventWorkers)
	assert.NotNil(t, wpm.storageWorkers)
	assert.Equal(t, 2, wpm.eventWorkers.workerCount)
	assert.Equal(t, 1, wpm.storageWorkers.workerCount)

	// Stop and wait for workers
	wpm.Stop()
	done := make(chan bool)
	go func() {
		wg.Wait()
		done <- true
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("Workers did not stop within timeout")
	}

	eventProcessor.Close()
	storageProcessor.Close()
}

func TestWorkerPoolManager_WorkerProcessing(t *testing.T) {
	logger := zaptest.NewLogger(t)
	var wg sync.WaitGroup

	// Setup test metric provider
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	// Create test metrics
	factory := NewMetricFactory("test", logger)
	metrics, err := factory.CreateEngineMetrics()
	require.NoError(t, err)

	wpm := NewWorkerPoolManager(logger, 1, 1, metrics, &wg)

	// Create test processors
	eventProcessor := NewTestWorkerProcessor(EventWorker, 10)
	storageProcessor := NewTestWorkerProcessor(StorageWorker, 10)

	err = wpm.StartWorkerPools(eventProcessor, storageProcessor, 1, 1)
	require.NoError(t, err)

	// Send test work items
	testEventWork := "test-event"
	testStorageWork := "test-storage"

	eventProcessor.SendWork(testEventWork)
	storageProcessor.SendWork(testStorageWork)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Verify work was processed
	eventProcessed := eventProcessor.GetProcessedWork()
	storageProcessed := storageProcessor.GetProcessedWork()

	assert.Len(t, eventProcessed, 1)
	assert.Equal(t, testEventWork, eventProcessed[0])

	assert.Len(t, storageProcessed, 1)
	assert.Equal(t, testStorageWork, storageProcessed[0])

	// Stop workers
	wpm.Stop()
	eventProcessor.Close()
	storageProcessor.Close()

	// Wait for workers to stop
	done := make(chan bool)
	go func() {
		wg.Wait()
		done <- true
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("Workers did not stop within timeout")
	}
}

func TestWorkerPoolManager_GetStats(t *testing.T) {
	logger := zaptest.NewLogger(t)
	var wg sync.WaitGroup

	// Setup test metric provider
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	// Create test metrics
	factory := NewMetricFactory("test", logger)
	metrics, err := factory.CreateEngineMetrics()
	require.NoError(t, err)

	wpm := NewWorkerPoolManager(logger, 3, 2, metrics, &wg)

	// Create test processors
	eventProcessor := NewTestWorkerProcessor(EventWorker, 10)
	storageProcessor := NewTestWorkerProcessor(StorageWorker, 10)

	err = wpm.StartWorkerPools(eventProcessor, storageProcessor, 3, 2)
	require.NoError(t, err)

	stats := wpm.GetStats()

	assert.Equal(t, EventWorker, stats.EventWorkers.Type)
	assert.Equal(t, 3, stats.EventWorkers.Count)
	assert.True(t, stats.EventWorkers.Active)

	assert.Equal(t, StorageWorker, stats.StorageWorkers.Type)
	assert.Equal(t, 2, stats.StorageWorkers.Count)
	assert.True(t, stats.StorageWorkers.Active)

	// Stop workers
	wpm.Stop()
	eventProcessor.Close()
	storageProcessor.Close()

	// Wait for workers to stop
	done := make(chan bool)
	go func() {
		wg.Wait()
		done <- true
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("Workers did not stop within timeout")
	}
}

func TestWorkerPoolManager_NilStorageProcessor(t *testing.T) {
	logger := zaptest.NewLogger(t)
	var wg sync.WaitGroup

	// Setup test metric provider
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	// Create test metrics
	factory := NewMetricFactory("test", logger)
	metrics, err := factory.CreateEngineMetrics()
	require.NoError(t, err)

	wpm := NewWorkerPoolManager(logger, 2, 0, metrics, &wg)

	// Create only event processor
	eventProcessor := NewTestWorkerProcessor(EventWorker, 10)

	err = wpm.StartWorkerPools(eventProcessor, nil, 2, 0)
	require.NoError(t, err)

	// Verify only event workers were created
	assert.NotNil(t, wpm.eventWorkers)
	assert.NotNil(t, wpm.storageWorkers) // Still created but not started

	stats := wpm.GetStats()
	assert.Equal(t, 2, stats.EventWorkers.Count)
	assert.True(t, stats.EventWorkers.Active)
	assert.Equal(t, 0, stats.StorageWorkers.Count)

	// Stop workers
	wpm.Stop()
	eventProcessor.Close()

	// Wait for workers to stop
	done := make(chan bool)
	go func() {
		wg.Wait()
		done <- true
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("Workers did not stop within timeout")
	}
}

func TestWorkerPoolManager_ProcessingError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	var wg sync.WaitGroup

	// Setup test metric provider
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	// Create test metrics
	factory := NewMetricFactory("test", logger)
	metrics, err := factory.CreateEngineMetrics()
	require.NoError(t, err)

	wpm := NewWorkerPoolManager(logger, 1, 0, metrics, &wg)

	// Create processor that returns errors
	eventProcessor := NewTestWorkerProcessor(EventWorker, 10)
	eventProcessor.processFunc = func(ctx context.Context, workItem interface{}) error {
		return assert.AnError
	}

	err = wpm.StartWorkerPools(eventProcessor, nil, 1, 0)
	require.NoError(t, err)

	// Send work that will cause an error
	eventProcessor.SendWork("error-work")

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Verify work was still processed (even with error)
	assert.Equal(t, 1, eventProcessor.GetProcessedCount())

	// Stop workers
	wpm.Stop()
	eventProcessor.Close()

	// Wait for workers to stop
	done := make(chan bool)
	go func() {
		wg.Wait()
		done <- true
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("Workers did not stop within timeout")
	}
}

// Benchmark worker pool performance
func BenchmarkWorkerPoolManager_ProcessWork(b *testing.B) {
	logger := zap.NewNop()
	var wg sync.WaitGroup

	// Setup test metric provider
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer otel.SetMeterProvider(nil)

	// Create test metrics
	factory := NewMetricFactory("benchmark", logger)
	metrics, err := factory.CreateEngineMetrics()
	if err != nil {
		b.Fatal(err)
	}

	wpm := NewWorkerPoolManager(logger, 4, 0, metrics, &wg)

	// Create processor
	eventProcessor := NewTestWorkerProcessor(EventWorker, 1000)

	err = wpm.StartWorkerPools(eventProcessor, nil, 4, 0)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	// Send work items
	for i := 0; i < b.N; i++ {
		eventProcessor.SendWork(i)
	}

	// Wait for all work to be processed
	for eventProcessor.GetProcessedCount() < b.N {
		time.Sleep(time.Millisecond)
	}

	b.StopTimer()

	// Cleanup
	wpm.Stop()
	eventProcessor.Close()

	done := make(chan bool)
	go func() {
		wg.Wait()
		done <- true
	}()

	select {
	case <-done:
		// Success
	case <-time.After(10 * time.Second):
		b.Fatal("Workers did not stop within timeout")
	}
}
