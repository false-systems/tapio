package performance

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"
)

func TestRingBufferPutBatch(t *testing.T) {
	tests := []struct {
		name         string
		bufferSize   uint64
		batchSize    int
		expectAdded  int
	}{
		{
			name:         "empty buffer full batch",
			bufferSize:   16,
			batchSize:    10,
			expectAdded:  10,
		},
		{
			name:         "partial space available",
			bufferSize:   8,
			batchSize:    10,
			expectAdded:  8,
		},
		{
			name:         "exact fit",
			bufferSize:   16,
			batchSize:    16,
			expectAdded:  16,
		},
		{
			name:         "empty batch",
			bufferSize:   16,
			batchSize:    0,
			expectAdded:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb, err := NewRingBuffer(tt.bufferSize)
			if err != nil {
				t.Fatalf("Failed to create ring buffer: %v", err)
			}

			// Create batch
			items := make([]unsafe.Pointer, tt.batchSize)
			for i := 0; i < tt.batchSize; i++ {
				val := i
				items[i] = unsafe.Pointer(&val)
			}

			// Put batch
			added := rb.PutBatch(items)
			if added != tt.expectAdded {
				t.Errorf("Expected to add %d items, but added %d", tt.expectAdded, added)
			}

			// Verify size
			if rb.Size() != uint64(added) {
				t.Errorf("Expected size %d, got %d", added, rb.Size())
			}
		})
	}
}

func TestRingBufferGetBatch(t *testing.T) {
	rb, err := NewRingBuffer(16)
	if err != nil {
		t.Fatalf("Failed to create ring buffer: %v", err)
	}

	// Fill buffer with test data
	expected := make([]int, 10)
	items := make([]unsafe.Pointer, 10)
	for i := 0; i < 10; i++ {
		expected[i] = i * 100
		items[i] = unsafe.Pointer(&expected[i])
	}
	rb.PutBatch(items)

	// Get batch
	retrieved := make([]unsafe.Pointer, 15) // Larger than what's available
	count := rb.GetBatch(retrieved)

	if count != 10 {
		t.Errorf("Expected to retrieve 10 items, got %d", count)
	}

	// Verify values
	for i := 0; i < count; i++ {
		val := *(*int)(retrieved[i])
		if val != expected[i] {
			t.Errorf("Expected value %d at index %d, got %d", expected[i], i, val)
		}
	}

	// Verify buffer is empty
	if !rb.IsEmpty() {
		t.Error("Buffer should be empty after retrieving all items")
	}
}

func TestRingBufferBatchConcurrent(t *testing.T) {
	rb, err := NewRingBuffer(1024)
	if err != nil {
		t.Fatalf("Failed to create ring buffer: %v", err)
	}

	const numProducers = 4
	const numConsumers = 4
	const batchSize = 10
	const numBatches = 100

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
					val := producerID*1000000 + batch*1000 + i
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
			for {
				count := rb.GetBatch(items)
				if count == 0 {
					// Check if producers are done
					if totalProduced.Load() == totalConsumed.Load() {
						break
					}
					continue
				}
				totalConsumed.Add(int64(count))
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

func TestRingBufferMixedBatchOperations(t *testing.T) {
	rb, err := NewRingBuffer(32)
	if err != nil {
		t.Fatalf("Failed to create ring buffer: %v", err)
	}

	// Mix of batch and single operations
	items := make([]unsafe.Pointer, 5)
	for i := 0; i < 5; i++ {
		val := i
		items[i] = unsafe.Pointer(&val)
	}

	// Batch put
	added := rb.PutBatch(items)
	if added != 5 {
		t.Errorf("Expected to add 5 items, added %d", added)
	}

	// Single put
	val := 99
	err = rb.Put(unsafe.Pointer(&val))
	if err != nil {
		t.Errorf("Single put failed: %v", err)
	}

	// Batch get
	retrieved := make([]unsafe.Pointer, 3)
	count := rb.GetBatch(retrieved)
	if count != 3 {
		t.Errorf("Expected to retrieve 3 items, got %d", count)
	}

	// Single get
	ptr, err := rb.Get()
	if err != nil {
		t.Errorf("Single get failed: %v", err)
	}
	if *(*int)(ptr) != 3 {
		t.Errorf("Expected value 3, got %d", *(*int)(ptr))
	}

	// Verify remaining size
	if rb.Size() != 2 {
		t.Errorf("Expected size 2, got %d", rb.Size())
	}
}

func BenchmarkRingBufferPutBatch(b *testing.B) {
	sizes := []int{10, 100, 1000}
	
	for _, size := range sizes {
		b.Run(fmt.Sprintf("batch_%d", size), func(b *testing.B) {
			rb, _ := NewRingBuffer(65536)
			items := make([]unsafe.Pointer, size)
			for i := 0; i < size; i++ {
				val := i
				items[i] = unsafe.Pointer(&val)
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rb.PutBatch(items)
				// Clear buffer for next iteration
				rb.Clear()
			}
		})
	}
}

func BenchmarkRingBufferGetBatch(b *testing.B) {
	sizes := []int{10, 100, 1000}
	
	for _, size := range sizes {
		b.Run(fmt.Sprintf("batch_%d", size), func(b *testing.B) {
			rb, _ := NewRingBuffer(65536)
			
			// Pre-fill buffer
			items := make([]unsafe.Pointer, size)
			for i := 0; i < size; i++ {
				val := i
				items[i] = unsafe.Pointer(&val)
			}
			
			retrieved := make([]unsafe.Pointer, size)
			
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rb.PutBatch(items)
				rb.GetBatch(retrieved)
			}
		})
	}
}

// Compare batch vs individual operations
func BenchmarkBatchVsIndividual(b *testing.B) {
	const batchSize = 100
	
	b.Run("individual_put", func(b *testing.B) {
		rb, _ := NewRingBuffer(65536)
		items := make([]unsafe.Pointer, batchSize)
		for i := 0; i < batchSize; i++ {
			val := i
			items[i] = unsafe.Pointer(&val)
		}
		
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, item := range items {
				rb.Put(item)
			}
			rb.Clear()
		}
	})
	
	b.Run("batch_put", func(b *testing.B) {
		rb, _ := NewRingBuffer(65536)
		items := make([]unsafe.Pointer, batchSize)
		for i := 0; i < batchSize; i++ {
			val := i
			items[i] = unsafe.Pointer(&val)
		}
		
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			rb.PutBatch(items)
			rb.Clear()
		}
	})
}