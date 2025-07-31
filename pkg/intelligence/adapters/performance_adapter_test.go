package adapters

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/interfaces"
)

// Mock correlation engine for testing
type mockCorrelationEngine struct {
	processedEvents []*domain.UnifiedEvent
	mu              sync.Mutex
	shouldFail      bool
	started         bool
}

func (m *mockCorrelationEngine) Start() error {
	m.started = true
	return nil
}

func (m *mockCorrelationEngine) Stop() error {
	m.started = false
	return nil
}

func (m *mockCorrelationEngine) ProcessEvent(ctx context.Context, event *domain.UnifiedEvent) error {
	if m.shouldFail {
		return errors.New("event processing failed")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.processedEvents = append(m.processedEvents, event)
	return nil
}

func (m *mockCorrelationEngine) GetLatestFindings() *interfaces.Finding {
	return nil
}

func (m *mockCorrelationEngine) GetSemanticGroups() []*interfaces.SemanticGroup {
	return nil
}

func (m *mockCorrelationEngine) GetProcessedEvents() []*domain.UnifiedEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*domain.UnifiedEvent, len(m.processedEvents))
	copy(result, m.processedEvents)
	return result
}

// Mock batch processor
type mockBatchProcessor struct {
	batchSize int
}

func (m *mockBatchProcessor) ProcessBatch(ctx context.Context, events []*domain.UnifiedEvent) error {
	// Simulate batch processing
	return nil
}

func (m *mockBatchProcessor) GetBatchSize() int {
	return m.batchSize
}

func (m *mockBatchProcessor) SetBatchSize(size int) {
	m.batchSize = size
}

func TestPerformanceAdapter_NewPerformanceAdapter(t *testing.T) {
	engine := &mockCorrelationEngine{}

	adapter := NewPerformanceAdapter(engine)

	if adapter.engine != engine {
		t.Error("Engine not set correctly")
	}

	if adapter.maxBatchSize != 100 {
		t.Errorf("Expected default batch size 100, got %d", adapter.maxBatchSize)
	}

	if adapter.ringBufferSize != 65536 {
		t.Errorf("Expected default ring buffer size 65536, got %d", adapter.ringBufferSize)
	}

	if len(adapter.ringBuffer) != adapter.ringBufferSize {
		t.Errorf("Ring buffer not initialized correctly")
	}
}

func TestPerformanceAdapter_WithOptions(t *testing.T) {
	engine := &mockCorrelationEngine{}
	batchProcessor := &mockBatchProcessor{batchSize: 50}

	adapter := NewPerformanceAdapter(engine,
		WithBatchSize(200),
		WithFlushInterval(200*time.Millisecond),
		WithRingBufferSize(1024),
		WithBatchProcessor(batchProcessor),
	)

	if adapter.maxBatchSize != 200 {
		t.Errorf("Expected batch size 200, got %d", adapter.maxBatchSize)
	}

	if adapter.flushInterval != 200*time.Millisecond {
		t.Errorf("Expected flush interval 200ms, got %v", adapter.flushInterval)
	}

	if adapter.ringBufferSize != 1024 {
		t.Errorf("Expected ring buffer size 1024, got %d", adapter.ringBufferSize)
	}

	if adapter.batchProcessor != batchProcessor {
		t.Error("Batch processor not set correctly")
	}
}

func TestPerformanceAdapter_StartStop(t *testing.T) {
	engine := &mockCorrelationEngine{}
	adapter := NewPerformanceAdapter(engine)

	// Start adapter
	err := adapter.Start()
	if err != nil {
		t.Errorf("Expected nil error on start, got %v", err)
	}

	if !engine.started {
		t.Error("Engine should be started")
	}

	// Stop adapter
	err = adapter.Stop()
	if err != nil {
		t.Errorf("Expected nil error on stop, got %v", err)
	}

	if engine.started {
		t.Error("Engine should be stopped")
	}
}

func TestPerformanceAdapter_SubmitAndRetrieve(t *testing.T) {
	engine := &mockCorrelationEngine{}
	adapter := NewPerformanceAdapter(engine, WithBatchSize(2))

	err := adapter.Start()
	if err != nil {
		t.Fatalf("Failed to start adapter: %v", err)
	}
	defer adapter.Stop()

	// Submit events
	for i := 0; i < 3; i++ {
		event := &interfaces.PipelineEvent{
			ID:        uint64(i),
			Type:      "test",
			Timestamp: time.Now().UnixNano(),
			Priority:  1,
		}

		err := adapter.Submit(event)
		if err != nil {
			t.Errorf("Failed to submit event %d: %v", i, err)
		}
	}

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// Check if events were processed
	processedEvents := engine.GetProcessedEvents()
	if len(processedEvents) == 0 {
		t.Error("No events were processed")
	}
}

func TestPerformanceAdapter_RingBufferFull(t *testing.T) {
	engine := &mockCorrelationEngine{}
	adapter := NewPerformanceAdapter(engine, WithRingBufferSize(2), WithChannelSize(0))

	// Fill ring buffer without starting adapter (no workers to drain it)
	event := &interfaces.PipelineEvent{
		ID:        1,
		Type:      "test",
		Timestamp: time.Now().UnixNano(),
		Priority:  1,
	}

	// First two should succeed
	if err := adapter.Submit(event); err != nil {
		t.Errorf("First submit should succeed: %v", err)
	}
	if err := adapter.Submit(event); err != nil {
		t.Errorf("Second submit should succeed: %v", err)
	}

	// Third should fail (ring buffer full, channel also small and not being drained)
	if err := adapter.Submit(event); err == nil {
		t.Error("Third submit should fail when ring buffer is full")
	}
}

func TestPerformanceAdapter_GetEvent_PutEvent(t *testing.T) {
	engine := &mockCorrelationEngine{}
	adapter := NewPerformanceAdapter(engine)

	// Get event from pool
	event := adapter.GetEvent()
	if event == nil {
		t.Error("GetEvent should return non-nil event")
	}

	// Modify event
	event.ID = 123
	event.Type = "test"

	// Return to pool
	adapter.PutEvent(event)

	// Event should be reset
	if event.ID != 0 || event.Type != "" {
		t.Error("PutEvent should reset event fields")
	}
}

func TestPerformanceAdapter_ProcessBatch(t *testing.T) {
	engine := &mockCorrelationEngine{}
	adapter := NewPerformanceAdapter(engine)

	events := []*domain.UnifiedEvent{
		{ID: "1", Type: domain.EventTypeSystem},
		{ID: "2", Type: domain.EventTypeSystem},
	}

	ctx := context.Background()
	err := adapter.ProcessBatch(ctx, events)
	if err != nil {
		t.Errorf("ProcessBatch failed: %v", err)
	}

	// Check events were processed
	processedEvents := engine.GetProcessedEvents()
	if len(processedEvents) != 2 {
		t.Errorf("Expected 2 processed events, got %d", len(processedEvents))
	}
}

func TestPerformanceAdapter_ProcessBatchWithProcessor(t *testing.T) {
	engine := &mockCorrelationEngine{}
	batchProcessor := &mockBatchProcessor{batchSize: 10}
	adapter := NewPerformanceAdapter(engine, WithBatchProcessor(batchProcessor))

	events := []*domain.UnifiedEvent{
		{ID: "1", Type: domain.EventTypeSystem},
	}

	ctx := context.Background()
	err := adapter.ProcessBatch(ctx, events)
	if err != nil {
		t.Errorf("ProcessBatch with processor failed: %v", err)
	}
}

func TestPerformanceAdapter_BatchSizeManagement(t *testing.T) {
	engine := &mockCorrelationEngine{}
	adapter := NewPerformanceAdapter(engine, WithBatchSize(50))

	// Test getting batch size
	if adapter.GetBatchSize() != 50 {
		t.Errorf("Expected batch size 50, got %d", adapter.GetBatchSize())
	}

	// Test setting batch size within limits
	adapter.SetBatchSize(25)
	if adapter.GetBatchSize() != 25 {
		t.Errorf("Expected batch size 25 after update, got %d", adapter.GetBatchSize())
	}

	// Test invalid batch size (too large)
	adapter.SetBatchSize(75) // Larger than maxBatchSize (50)
	if adapter.GetBatchSize() != 25 {
		t.Errorf("Should not change batch size when too large, expected 25, got %d", adapter.GetBatchSize())
	}

	// Test invalid batch size (negative)
	adapter.SetBatchSize(-1)
	if adapter.GetBatchSize() != 25 {
		t.Errorf("Should not change batch size for negative input, expected 25, got %d", adapter.GetBatchSize())
	}
}

func TestPerformanceAdapter_Metrics(t *testing.T) {
	engine := &mockCorrelationEngine{}
	adapter := NewPerformanceAdapter(engine)

	err := adapter.Start()
	if err != nil {
		t.Fatalf("Failed to start adapter: %v", err)
	}
	defer adapter.Stop()

	// Submit some events
	for i := 0; i < 5; i++ {
		event := &interfaces.PipelineEvent{
			ID:        uint64(i),
			Type:      "test",
			Timestamp: time.Now().UnixNano(),
			Priority:  1,
		}
		adapter.Submit(event)
	}

	// Wait for processing and metrics update
	time.Sleep(1200 * time.Millisecond) // Wait for metrics worker cycle

	metrics := adapter.GetMetrics()
	if metrics == nil {
		t.Error("GetMetrics should return non-nil metrics")
	}

	// Should have some processed events
	if metrics.EventsProcessed == 0 {
		t.Error("Expected some processed events")
	}
}

func TestPerformanceAdapter_ConcurrentSubmit(t *testing.T) {
	engine := &mockCorrelationEngine{}
	adapter := NewPerformanceAdapter(engine, WithRingBufferSize(1000))

	err := adapter.Start()
	if err != nil {
		t.Fatalf("Failed to start adapter: %v", err)
	}
	defer adapter.Stop()

	const numGoroutines = 10
	const eventsPerGoroutine = 100

	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < eventsPerGoroutine; j++ {
				event := &interfaces.PipelineEvent{
					ID:        uint64(id*eventsPerGoroutine + j),
					Type:      "concurrent_test",
					Timestamp: time.Now().UnixNano(),
					Priority:  uint8(j % 4),
				}

				// Don't fail on submission errors due to full buffers in high concurrency
				adapter.Submit(event)
			}
		}(i)
	}

	wg.Wait()

	// Wait for processing
	time.Sleep(500 * time.Millisecond)

	// Should have processed some events (may not be all due to buffer limits)
	processedEvents := engine.GetProcessedEvents()
	if len(processedEvents) == 0 {
		t.Error("Expected some events to be processed")
	}
}

func TestPerformanceAdapter_ErrorHandling(t *testing.T) {
	engine := &mockCorrelationEngine{shouldFail: true}
	adapter := NewPerformanceAdapter(engine)

	err := adapter.Start()
	if err != nil {
		t.Fatalf("Failed to start adapter: %v", err)
	}
	defer adapter.Stop()

	// Submit events that will fail processing
	for i := 0; i < 3; i++ {
		event := &interfaces.PipelineEvent{
			ID:        uint64(i),
			Type:      "fail_test",
			Timestamp: time.Now().UnixNano(),
			Priority:  1,
		}
		adapter.Submit(event)
	}

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// Check metrics show errors
	metrics := adapter.GetMetrics()
	if metrics.ErrorRate == 0 {
		t.Error("Expected non-zero error rate")
	}
}

func BenchmarkPerformanceAdapter_Submit(b *testing.B) {
	engine := &mockCorrelationEngine{}
	adapter := NewPerformanceAdapter(engine, WithRingBufferSize(100000))

	event := &interfaces.PipelineEvent{
		ID:        1,
		Type:      "benchmark",
		Timestamp: time.Now().UnixNano(),
		Priority:  1,
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			adapter.Submit(event)
		}
	})
}

func BenchmarkPerformanceAdapter_ProcessBatch(b *testing.B) {
	engine := &mockCorrelationEngine{}
	adapter := NewPerformanceAdapter(engine)

	events := make([]*domain.UnifiedEvent, 100)
	for i := range events {
		events[i] = &domain.UnifiedEvent{
			ID: fmt.Sprintf("%d", i), Type: domain.EventTypeSystem,
		}
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		adapter.ProcessBatch(ctx, events)
	}
}
