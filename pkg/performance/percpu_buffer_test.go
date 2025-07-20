package performance

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/yairfalse/tapio/pkg/domain"
)

// TestPerCPUBuffer verifies basic per-CPU buffer operations
func TestPerCPUBuffer(t *testing.T) {
	buffer, err := NewPerCPUBuffer(PerCPUBufferConfig{
		BufferSize:   1024,
		OverflowSize: 4096,
	})
	if err != nil {
		t.Fatalf("Failed to create per-CPU buffer: %v", err)
	}

	// Write data
	testData := []byte("test data")
	if err := buffer.Write(testData); err != nil {
		t.Fatalf("Failed to write data: %v", err)
	}

	// Read data
	results, err := buffer.Read()
	if err != nil {
		t.Fatalf("Failed to read data: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
	}

	if string(results[0]) != string(testData) {
		t.Errorf("Data mismatch: expected %s, got %s", testData, results[0])
	}
}

// TestPerCPUBufferOverflow tests overflow behavior
func TestPerCPUBufferOverflow(t *testing.T) {
	buffer, err := NewPerCPUBuffer(PerCPUBufferConfig{
		BufferSize:   64, // Very small buffer to force overflow
		OverflowSize: 1024,
	})
	if err != nil {
		t.Fatalf("Failed to create per-CPU buffer: %v", err)
	}

	// Write enough data to overflow
	largeData := make([]byte, 50)
	for i := range largeData {
		largeData[i] = byte(i)
	}

	// First write should succeed (fits with header)
	if err := buffer.Write(largeData); err != nil {
		t.Fatalf("First write failed: %v", err)
	}

	// Second write should overflow
	if err := buffer.Write(largeData); err != nil {
		t.Fatalf("Second write failed: %v", err)
	}

	// Check metrics
	metrics := buffer.GetMetrics()
	if metrics.Overflows == 0 {
		t.Errorf("Expected overflows, got none")
	}

	// Read all data
	results, err := buffer.Read()
	if err != nil {
		t.Fatalf("Failed to read data: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}
}

// TestPerCPUBufferConcurrent tests concurrent access
func TestPerCPUBufferConcurrent(t *testing.T) {
	buffer, err := NewPerCPUBuffer(PerCPUBufferConfig{
		BufferSize:   16384,
		OverflowSize: 65536,
	})
	if err != nil {
		t.Fatalf("Failed to create per-CPU buffer: %v", err)
	}

	numGoroutines := runtime.GOMAXPROCS(0) * 2
	numWrites := 1000
	var wg sync.WaitGroup
	var totalWrites atomic.Int32

	// Concurrent writers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			data := make([]byte, 8)
			for j := 0; j < numWrites; j++ {
				// Write goroutine ID and sequence
				data[0] = byte(id)
				data[1] = byte(id >> 8)
				data[2] = byte(j)
				data[3] = byte(j >> 8)

				if err := buffer.Write(data); err == nil {
					totalWrites.Add(1)
				}
			}
		}(i)
	}

	// Wait for writers
	wg.Wait()

	// Read all data
	results, err := buffer.Read()
	if err != nil {
		t.Fatalf("Failed to read data: %v", err)
	}

	t.Logf("Wrote %d items, read %d items", totalWrites.Load(), len(results))

	// Verify data integrity
	seen := make(map[uint32]bool)
	for _, data := range results {
		if len(data) != 8 {
			t.Errorf("Invalid data length: %d", len(data))
			continue
		}

		id := uint32(data[0]) | uint32(data[1])<<8
		seq := uint32(data[2]) | uint32(data[3])<<8
		key := (id << 16) | seq

		if seen[key] {
			t.Errorf("Duplicate data: id=%d, seq=%d", id, seq)
		}
		seen[key] = true
	}
}

// TestPerCPUEventBuffer tests the event-specific buffer
func TestPerCPUEventBuffer(t *testing.T) {
	buffer, err := NewPerCPUEventBuffer(PerCPUEventBufferConfig{
		BufferSizePerCPU: 16384,
		OverflowSize:     65536,
		EnablePooling:    true,
	})
	if err != nil {
		t.Fatalf("Failed to create per-CPU event buffer: %v", err)
	}

	// Create test event
	event := &domain.UnifiedEvent{
		ID:     domain.GenerateEventID(),
		Type:   "test",
		Source: "percpu-test",
	}

	// Write event
	if err := buffer.Put(event); err != nil {
		t.Fatalf("Failed to put event: %v", err)
	}

	// Read events
	events, err := buffer.Get()
	if err != nil {
		t.Fatalf("Failed to get events: %v", err)
	}

	if len(events) != 1 {
		t.Errorf("Expected 1 event, got %d", len(events))
	}

	if events[0].ID != event.ID {
		t.Errorf("Event ID mismatch: expected %s, got %s", event.ID, events[0].ID)
	}
}

// TestPerCPUEventBufferBatch tests batch operations
func TestPerCPUEventBufferBatch(t *testing.T) {
	buffer, err := NewPerCPUEventBuffer(PerCPUEventBufferConfig{
		EnablePooling: true,
	})
	if err != nil {
		t.Fatalf("Failed to create buffer: %v", err)
	}

	// Create batch of events
	events := make([]*domain.UnifiedEvent, 100)
	for i := range events {
		events[i] = &domain.UnifiedEvent{
			ID:   domain.GenerateEventID(),
			Type: domain.EventType("batch-test"),
		}
	}

	// Put batch
	added, err := buffer.PutBatch(events)
	if err != nil && added == 0 {
		t.Fatalf("Failed to add batch: %v", err)
	}
	t.Logf("Added %d events", added)

	// Get all events
	retrieved, err := buffer.Get()
	if err != nil {
		t.Fatalf("Failed to get events: %v", err)
	}

	if len(retrieved) != added {
		t.Errorf("Expected %d events, got %d", added, len(retrieved))
	}

	// Check stats
	stats := buffer.GetStats()
	t.Logf("Stats: %+v", stats)
}

// TestPerCPUBufferMetrics tests metrics collection
func TestPerCPUBufferMetrics(t *testing.T) {
	buffer, err := NewPerCPUBuffer(PerCPUBufferConfig{
		BufferSize: 1024,
	})
	if err != nil {
		t.Fatalf("Failed to create buffer: %v", err)
	}

	// Write some data
	for i := 0; i < 10; i++ {
		data := make([]byte, 50)
		buffer.Write(data)
	}

	// Get metrics
	metrics := buffer.GetMetrics()

	if metrics.Writes != 10 {
		t.Errorf("Expected 10 writes, got %d", metrics.Writes)
	}

	// Check CPU metrics
	totalUtilization := 0.0
	for _, cpu := range metrics.CPUMetrics {
		t.Logf("CPU %d: %d/%d bytes (%.1f%%)",
			cpu.CPU, cpu.Used, cpu.Capacity, cpu.Utilization*100)
		totalUtilization += cpu.Utilization
	}

	if totalUtilization == 0 {
		t.Errorf("No data in any CPU buffer")
	}
}

// TestAggregator tests the aggregator interface
func TestAggregator(t *testing.T) {
	buffer, err := NewPerCPUBuffer(PerCPUBufferConfig{
		BufferSize: 1024,
		Aggregator: &SimpleAggregator{},
	})
	if err != nil {
		t.Fatalf("Failed to create buffer: %v", err)
	}

	// Write data to multiple CPUs
	data1 := []byte("hello ")
	data2 := []byte("world")

	buffer.Write(data1)
	buffer.Write(data2)

	// Aggregate
	result, err := buffer.Aggregate()
	if err != nil {
		t.Fatalf("Failed to aggregate: %v", err)
	}

	// The exact result depends on which CPU each write went to
	// but it should contain both strings
	resultStr := string(result)
	if len(resultStr) < len(data1)+len(data2) {
		t.Errorf("Aggregated result too short: %s", resultStr)
	}
}

// BenchmarkPerCPUBufferWrite benchmarks write performance
func BenchmarkPerCPUBufferWrite(b *testing.B) {
	buffer, _ := NewPerCPUBuffer(PerCPUBufferConfig{
		BufferSize:   65536,
		OverflowSize: 1048576,
	})

	data := make([]byte, 64)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buffer.Write(data)
		}
	})
}

// BenchmarkPerCPUEventBuffer benchmarks event buffer performance
func BenchmarkPerCPUEventBuffer(b *testing.B) {
	buffer, _ := NewPerCPUEventBuffer(PerCPUEventBufferConfig{
		BufferSizePerCPU: 262144,
		EnablePooling:    true,
	})

	event := &domain.UnifiedEvent{
		ID:   "bench",
		Type: "test",
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buffer.Put(event)
		}
	})
}
