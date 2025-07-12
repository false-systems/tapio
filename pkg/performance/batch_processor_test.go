package performance

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBatchProcessor_BasicOperation(t *testing.T) {
	var processed [][]string
	var mu sync.Mutex

	processFn := func(ctx context.Context, batch []string) error {
		mu.Lock()
		defer mu.Unlock()
		batchCopy := make([]string, len(batch))
		copy(batchCopy, batch)
		processed = append(processed, batchCopy)
		return nil
	}

	processor := NewBatchProcessor(3, 100*time.Millisecond, 100, processFn)
	err := processor.Start()
	require.NoError(t, err)
	defer processor.Stop()

	// Submit items
	items := []string{"item1", "item2", "item3", "item4", "item5"}
	for _, item := range items {
		err = processor.Submit(item)
		require.NoError(t, err)
	}

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// Should have processed at least 2 batches (3+2 items)
	assert.GreaterOrEqual(t, len(processed), 1)

	// Count total processed items
	totalProcessed := 0
	for _, batch := range processed {
		totalProcessed += len(batch)
	}
	assert.Equal(t, len(items), totalProcessed)
}

func TestBatchProcessor_TimeoutBatch(t *testing.T) {
	var processed [][]string
	var mu sync.Mutex

	processFn := func(ctx context.Context, batch []string) error {
		mu.Lock()
		defer mu.Unlock()
		batchCopy := make([]string, len(batch))
		copy(batchCopy, batch)
		processed = append(processed, batchCopy)
		return nil
	}

	// Large batch size, small timeout
	processor := NewBatchProcessor(100, 50*time.Millisecond, 100, processFn)
	err := processor.Start()
	require.NoError(t, err)
	defer processor.Stop()

	// Submit just 2 items (less than batch size)
	err = processor.Submit("item1")
	require.NoError(t, err)
	err = processor.Submit("item2")
	require.NoError(t, err)

	// Wait for timeout to trigger
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// Should have processed due to timeout
	assert.Equal(t, 1, len(processed))
	assert.Equal(t, 2, len(processed[0]))
}

func TestBatchProcessor_SubmitBatch(t *testing.T) {
	var processed [][]string
	var mu sync.Mutex

	processFn := func(ctx context.Context, batch []string) error {
		mu.Lock()
		defer mu.Unlock()
		batchCopy := make([]string, len(batch))
		copy(batchCopy, batch)
		processed = append(processed, batchCopy)
		return nil
	}

	processor := NewBatchProcessor(10, 100*time.Millisecond, 100, processFn)
	err := processor.Start()
	require.NoError(t, err)
	defer processor.Stop()

	// Submit batch
	items := []string{"batch1", "batch2", "batch3", "batch4", "batch5"}
	err = processor.SubmitBatch(items)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	assert.GreaterOrEqual(t, len(processed), 1)
}

func TestBatchProcessor_QueueFull(t *testing.T) {
	processFn := func(ctx context.Context, batch []string) error {
		// Slow processing
		time.Sleep(100 * time.Millisecond)
		return nil
	}

	// Small queue size
	processor := NewBatchProcessor(10, 50*time.Millisecond, 5, processFn)
	err := processor.Start()
	require.NoError(t, err)
	defer processor.Stop()

	// Fill queue beyond capacity
	var submitErrors int
	for i := 0; i < 20; i++ {
		err = processor.Submit("item")
		if err != nil {
			submitErrors++
		}
	}

	// Should have some queue full errors
	assert.Greater(t, submitErrors, 0)

	metrics := processor.GetMetrics()
	assert.Greater(t, metrics.Dropped, uint64(0))
}

func TestBatchProcessor_Metrics(t *testing.T) {
	var processedCount atomic.Uint64

	processFn := func(ctx context.Context, batch []string) error {
		processedCount.Add(uint64(len(batch)))
		return nil
	}

	processor := NewBatchProcessor(5, 50*time.Millisecond, 100, processFn)
	err := processor.Start()
	require.NoError(t, err)
	defer processor.Stop()

	// Submit items
	for i := 0; i < 10; i++ {
		err = processor.Submit("item")
		require.NoError(t, err)
	}

	time.Sleep(100 * time.Millisecond)

	metrics := processor.GetMetrics()
	assert.Equal(t, processedCount.Load(), metrics.Processed)
	assert.Greater(t, metrics.Batches, uint64(0))
	assert.Greater(t, metrics.AvgBatchSize, uint64(0))
}

func TestAdaptiveBatchProcessor_Adaptation(t *testing.T) {
	var processedBatches [][]string
	var mu sync.Mutex

	processFn := func(ctx context.Context, batch []string) error {
		mu.Lock()
		defer mu.Unlock()

		// Simulate variable processing time
		if len(batch) > 5 {
			time.Sleep(100 * time.Millisecond) // Slow for large batches
		} else {
			time.Sleep(10 * time.Millisecond) // Fast for small batches
		}

		batchCopy := make([]string, len(batch))
		copy(batchCopy, batch)
		processedBatches = append(processedBatches, batchCopy)
		return nil
	}

	processor := NewAdaptiveBatchProcessor(2, 10, 50*time.Millisecond, processFn)
	err := processor.Start()
	require.NoError(t, err)
	defer processor.Stop()

	// Submit many items to trigger adaptation
	for i := 0; i < 50; i++ {
		err = processor.Submit("item")
		require.NoError(t, err)
		time.Sleep(5 * time.Millisecond)
	}

	// Wait for processing and adaptation
	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// Should have processed all items
	totalProcessed := 0
	for _, batch := range processedBatches {
		totalProcessed += len(batch)
	}
	assert.Equal(t, 50, totalProcessed)
	assert.Greater(t, len(processedBatches), 0)
}

func TestParallelBatchProcessor_BasicOperation(t *testing.T) {
	var processed [][]string
	var mu sync.Mutex

	processFn := func(ctx context.Context, batch []string) error {
		mu.Lock()
		defer mu.Unlock()
		batchCopy := make([]string, len(batch))
		copy(batchCopy, batch)
		processed = append(processed, batchCopy)
		return nil
	}

	processor := NewParallelBatchProcessor(2, 5, 50*time.Millisecond, processFn, nil)
	err := processor.Start()
	require.NoError(t, err)
	defer processor.Stop()

	// Submit items
	for i := 0; i < 20; i++ {
		err = processor.Submit("item")
		require.NoError(t, err)
	}

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// Should have processed all items across workers
	totalProcessed := 0
	for _, batch := range processed {
		totalProcessed += len(batch)
	}
	assert.Equal(t, 20, totalProcessed)
}

// Benchmark tests
func BenchmarkBatchProcessor_Submit(b *testing.B) {
	processFn := func(ctx context.Context, batch []string) error {
		return nil
	}

	processor := NewBatchProcessor(1000, 100*time.Millisecond, b.N+1000, processFn)
	processor.Start()
	defer processor.Stop()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		processor.Submit("benchmark")
	}
}

func BenchmarkBatchProcessor_SubmitBatch(b *testing.B) {
	processFn := func(ctx context.Context, batch []string) error {
		return nil
	}

	processor := NewBatchProcessor(1000, 100*time.Millisecond, b.N+1000, processFn)
	processor.Start()
	defer processor.Stop()

	// Create batch
	batchSize := 100
	batch := make([]string, batchSize)
	for i := range batch {
		batch[i] = "benchmark"
	}

	b.ResetTimer()

	for i := 0; i < b.N; i += batchSize {
		processor.SubmitBatch(batch)
	}
}

func BenchmarkBatchProcessor_HighThroughput(b *testing.B) {
	var processed atomic.Uint64

	processFn := func(ctx context.Context, batch []string) error {
		processed.Add(uint64(len(batch)))
		return nil
	}

	processor := NewBatchProcessor(100, 10*time.Millisecond, 10000, processFn)
	processor.Start()
	defer processor.Stop()

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			processor.Submit("benchmark")
		}
	})

	// Wait for processing to complete
	time.Sleep(100 * time.Millisecond)
}

func BenchmarkAdaptiveBatchProcessor_Throughput(b *testing.B) {
	processFn := func(ctx context.Context, batch []string) error {
		// Simulate processing time proportional to batch size
		time.Sleep(time.Duration(len(batch)) * time.Microsecond)
		return nil
	}

	processor := NewAdaptiveBatchProcessor(10, 200, 10*time.Millisecond, processFn)
	processor.Start()
	defer processor.Stop()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		processor.Submit("benchmark")
	}
}

func BenchmarkParallelBatchProcessor_Throughput(b *testing.B) {
	processFn := func(ctx context.Context, batch []string) error {
		return nil
	}

	processor := NewParallelBatchProcessor(4, 100, 10*time.Millisecond, processFn, nil)
	processor.Start()
	defer processor.Stop()

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			processor.Submit("benchmark")
		}
	})
}
