package performance

import (
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"
)

func TestIntelligenceRingBufferBatch(t *testing.T) {
	rb, err := NewRingBuffer(64)
	if err != nil {
		t.Fatalf("Failed to create ring buffer: %v", err)
	}

	// Test PutBatch
	items := make([]unsafe.Pointer, 10)
	for i := 0; i < 10; i++ {
		val := i * 10
		items[i] = unsafe.Pointer(&val)
	}

	added := rb.PutBatch(items)
	if added != 10 {
		t.Errorf("Expected to add 10 items, added %d", added)
	}

	// Test GetBatch
	retrieved := make([]unsafe.Pointer, 10)
	count := rb.GetBatch(retrieved)
	if count != 10 {
		t.Errorf("Expected to retrieve 10 items, got %d", count)
	}

	// Verify values
	for i := 0; i < count; i++ {
		val := *(*int)(retrieved[i])
		expected := i * 10
		if val != expected {
			t.Errorf("Expected value %d at index %d, got %d", expected, i, val)
		}
	}
}

func TestIntelligenceRingBufferBatchPartial(t *testing.T) {
	rb, err := NewRingBuffer(8)
	if err != nil {
		t.Fatalf("Failed to create ring buffer: %v", err)
	}

	// Try to add more items than capacity
	items := make([]unsafe.Pointer, 10)
	for i := 0; i < 10; i++ {
		val := i
		items[i] = unsafe.Pointer(&val)
	}

	added := rb.PutBatch(items)
	if added != 8 {
		t.Errorf("Expected to add 8 items (capacity limit), added %d", added)
	}

	// Verify size
	if rb.Size() != 8 {
		t.Errorf("Expected size 8, got %d", rb.Size())
	}
}

func TestIntelligenceRingBufferBatchConcurrent(t *testing.T) {
	rb, err := NewRingBuffer(1024)
	if err != nil {
		t.Fatalf("Failed to create ring buffer: %v", err)
	}

	const numProducers = 4
	const numConsumers = 4
	const batchSize = 20
	const numBatches = 50

	var wg sync.WaitGroup
	var totalProduced atomic.Int64
	var totalConsumed atomic.Int64

	// Producers
	for p := 0; p < numProducers; p++ {
		wg.Add(1)
		go func(producerID int) {
			defer wg.Done()

			for batch := 0; batch < numBatches; batch++ {
				items := make([]unsafe.Pointer, batchSize)
				for i := 0; i < batchSize; i++ {
					val := producerID*100000 + batch*100 + i
					items[i] = unsafe.Pointer(&val)
				}

				added := rb.PutBatch(items)
				totalProduced.Add(int64(added))
			}
		}(p)
	}

	// Consumers
	for c := 0; c < numConsumers; c++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			items := make([]unsafe.Pointer, batchSize)
			consumed := 0
			for consumed < numProducers*numBatches*batchSize {
				count := rb.GetBatch(items)
				if count > 0 {
					totalConsumed.Add(int64(count))
					consumed += count
				}
			}
		}()
	}

	wg.Wait()

	// Verify all items were consumed
	produced := totalProduced.Load()
	consumed := totalConsumed.Load()
	if produced != consumed {
		t.Errorf("Produced %d items but consumed %d", produced, consumed)
	}

	// Buffer should be empty
	if !rb.IsEmpty() {
		t.Error("Buffer should be empty after all items consumed")
	}
}

func TestEventPipelineBatchIntegration(t *testing.T) {
	// Create a simple passthrough stage
	stage := NewPassthroughStage("test")
	stages := []Stage{stage}

	config := DefaultPipelineConfig()
	config.BufferSize = 128
	config.BatchSize = 10
	config.WorkersPerStage = 2

	pipeline, err := NewEventPipeline(stages, config)
	if err != nil {
		t.Fatalf("Failed to create pipeline: %v", err)
	}

	err = pipeline.Start()
	if err != nil {
		t.Fatalf("Failed to start pipeline: %v", err)
	}
	defer pipeline.Stop()

	// Submit batch of events
	events := make([]*Event, 20)
	for i := 0; i < 20; i++ {
		events[i] = &Event{
			ID:   uint64(i),
			Type: "test",
			Data: unsafe.Pointer(&i),
		}
	}

	err = pipeline.SubmitBatch(events)
	if err != nil {
		t.Errorf("Failed to submit batch: %v", err)
	}

	// Retrieve events
	output := make([]*Event, 20)
	totalRetrieved := 0
	for totalRetrieved < 20 {
		count := pipeline.GetOutputBatch(output[totalRetrieved:])
		if count > 0 {
			totalRetrieved += count
		}
	}

	if totalRetrieved != 20 {
		t.Errorf("Expected to retrieve 20 events, got %d", totalRetrieved)
	}
}