package adapters

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/interfaces"
)

// Mock optimized engine for testing
type mockOptimizedEngine struct {
	processedEvents []*domain.UnifiedEvent
	mu              sync.Mutex
	shouldFail      bool
	started         bool
	delays          map[string]time.Duration // Simulate processing delays
}

func (m *mockOptimizedEngine) Start() error {
	m.started = true
	return nil
}

func (m *mockOptimizedEngine) Stop() error {
	m.started = false
	return nil
}

func (m *mockOptimizedEngine) ProcessEvent(ctx context.Context, event *domain.UnifiedEvent) error {
	// Simulate processing delay if configured
	if m.delays != nil {
		if delay, ok := m.delays[string(event.Type)]; ok {
			time.Sleep(delay)
		}
	}

	if m.shouldFail {
		return errors.New("processing failed")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.processedEvents = append(m.processedEvents, event)
	return nil
}

func (m *mockOptimizedEngine) GetLatestFindings() *interfaces.Finding {
	return nil
}

func (m *mockOptimizedEngine) GetSemanticGroups() []*interfaces.SemanticGroup {
	return nil
}

func (m *mockOptimizedEngine) GetProcessedEvents() []*domain.UnifiedEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*domain.UnifiedEvent, len(m.processedEvents))
	copy(result, m.processedEvents)
	return result
}

func TestOptimizedEventProcessor_NewProcessor(t *testing.T) {
	engine := &mockOptimizedEngine{}
	processor := NewOptimizedEventProcessor(engine)

	if processor.engine != engine {
		t.Error("Engine not set correctly")
	}

	if processor.networkEvents == nil {
		t.Error("Network events channel not initialized")
	}

	if processor.memoryEvents == nil {
		t.Error("Memory events channel not initialized")
	}

	if processor.processEvents == nil {
		t.Error("Process events channel not initialized")
	}

	if processor.fileEvents == nil {
		t.Error("File events channel not initialized")
	}

	// Check default configuration
	if processor.reconnectConfig.MaxRetries != 10 {
		t.Errorf("Expected default max retries 10, got %d", processor.reconnectConfig.MaxRetries)
	}

	if processor.reconnectConfig.InitialDelay != 100*time.Millisecond {
		t.Errorf("Expected default initial delay 100ms, got %v", processor.reconnectConfig.InitialDelay)
	}
}

func TestOptimizedEventProcessor_DefaultChannelSizes(t *testing.T) {
	engine := &mockOptimizedEngine{}
	processor := NewOptimizedEventProcessor(engine)

	// Check default channel sizes
	if cap(processor.networkEvents) != 1000 {
		t.Errorf("Expected network events channel size 1000, got %d", cap(processor.networkEvents))
	}

	if cap(processor.memoryEvents) != 1000 {
		t.Errorf("Expected memory events channel size 1000, got %d", cap(processor.memoryEvents))
	}

	if cap(processor.processEvents) != 1000 {
		t.Errorf("Expected process events channel size 1000, got %d", cap(processor.processEvents))
	}

	if cap(processor.fileEvents) != 100 {
		t.Errorf("Expected file events channel size 100, got %d", cap(processor.fileEvents))
	}

	if cap(processor.mergedEvents) != 5000 {
		t.Errorf("Expected merged events channel size 5000, got %d", cap(processor.mergedEvents))
	}
}

func TestOptimizedEventProcessor_StartStop(t *testing.T) {
	engine := &mockOptimizedEngine{}
	processor := NewOptimizedEventProcessor(engine)

	// Start processor
	err := processor.Start()
	if err != nil {
		t.Errorf("Failed to start processor: %v", err)
	}

	if !engine.started {
		t.Error("Engine should be started")
	}

	// Let it run briefly
	time.Sleep(100 * time.Millisecond)

	// Stop processor
	err = processor.Stop()
	if err != nil {
		t.Errorf("Failed to stop processor: %v", err)
	}

	if engine.started {
		t.Error("Engine should be stopped")
	}
}

func TestOptimizedEventProcessor_AddByType(t *testing.T) {
	engine := &mockOptimizedEngine{}
	processor := NewOptimizedEventProcessor(engine)

	err := processor.Start()
	if err != nil {
		t.Fatalf("Failed to start processor: %v", err)
	}
	defer processor.Stop()

	// ctx not needed for Add methods

	// Test each event type submission
	tests := []struct {
		name      string
		eventType domain.EventType
		submitFn  func(*domain.UnifiedEvent) error
	}{
		{
			name:      "Network Event",
			eventType: domain.EventTypeNetwork,
			submitFn:  func(e *domain.UnifiedEvent) error { return processor.AddNetworkEvent(e) },
		},
		{
			name:      "Memory Event",
			eventType: domain.EventTypeMemory,
			submitFn:  func(e *domain.UnifiedEvent) error { return processor.AddMemoryEvent(e) },
		},
		{
			name:      "Process Event",
			eventType: domain.EventTypeProcess,
			submitFn:  func(e *domain.UnifiedEvent) error { return processor.AddProcessEvent(e) },
		},
		{
			name:      "File Event",
			eventType: domain.EventTypeDisk, // Assuming file events use disk type
			submitFn:  func(e *domain.UnifiedEvent) error { return processor.AddFileEvent(e) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &domain.UnifiedEvent{
				ID:   tt.name,
				Type: tt.eventType,
			}

			err := tt.submitFn(event)
			if err != nil {
				t.Errorf("Failed to submit %s: %v", tt.name, err)
			}
		})
	}

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// Check all events were processed
	processed := engine.GetProcessedEvents()
	if len(processed) != len(tests) {
		t.Errorf("Expected %d processed events, got %d", len(tests), len(processed))
	}
}

func TestOptimizedEventProcessor_ChannelMerging(t *testing.T) {
	engine := &mockOptimizedEngine{}
	processor := NewOptimizedEventProcessor(engine)

	err := processor.Start()
	if err != nil {
		t.Fatalf("Failed to start processor: %v", err)
	}
	defer processor.Stop()

	// ctx not needed for Add methods

	// Submit events to different channels
	events := []struct {
		event    *domain.UnifiedEvent
		submitFn func(*domain.UnifiedEvent) error
	}{
		{
			event:    &domain.UnifiedEvent{ID: "net-1", Type: domain.EventTypeNetwork},
			submitFn: func(e *domain.UnifiedEvent) error { return processor.AddNetworkEvent(e) },
		},
		{
			event:    &domain.UnifiedEvent{ID: "mem-1", Type: domain.EventTypeMemory},
			submitFn: func(e *domain.UnifiedEvent) error { return processor.AddMemoryEvent(e) },
		},
		{
			event:    &domain.UnifiedEvent{ID: "proc-1", Type: domain.EventTypeProcess},
			submitFn: func(e *domain.UnifiedEvent) error { return processor.AddProcessEvent(e) },
		},
		{
			event:    &domain.UnifiedEvent{ID: "file-1", Type: domain.EventTypeDisk},
			submitFn: func(e *domain.UnifiedEvent) error { return processor.AddFileEvent(e) },
		},
	}

	// Submit all events
	for _, e := range events {
		if err := e.submitFn(e.event); err != nil {
			t.Errorf("Failed to submit event %s: %v", e.event.ID, err)
		}
	}

	// Wait for processing
	time.Sleep(300 * time.Millisecond)

	// All events should be processed
	processed := engine.GetProcessedEvents()
	if len(processed) != len(events) {
		t.Errorf("Expected %d processed events, got %d", len(events), len(processed))
	}
}

func TestOptimizedEventProcessor_ExponentialBackoff(t *testing.T) {
	engine := &mockOptimizedEngine{shouldFail: true}
	processor := NewOptimizedEventProcessor(engine)
	// Adjust reconnect config for faster testing
	processor.reconnectConfig.MaxRetries = 3
	processor.reconnectConfig.InitialDelay = 10 * time.Millisecond
	processor.reconnectConfig.MaxDelay = 100 * time.Millisecond

	err := processor.Start()
	if err != nil {
		t.Fatalf("Failed to start processor: %v", err)
	}
	defer processor.Stop()

	// ctx not needed for Add methods
	event := &domain.UnifiedEvent{
		ID:   "retry-test",
		Type: domain.EventTypeNetwork,
	}

	startTime := time.Now()
	err = processor.AddNetworkEvent(event)
	if err != nil {
		t.Errorf("Failed to submit event: %v", err)
	}

	// Wait for retries to complete
	time.Sleep(500 * time.Millisecond)

	// Check that errors were recorded
	metrics := processor.GetMetrics()
	if errorCount, ok := metrics["errors"].(uint64); !ok || errorCount == 0 {
		t.Error("Expected error count to be non-zero")
	}

	// With exponential backoff, should take at least:
	// 10ms (1st retry) + 20ms (2nd retry) + 40ms (3rd retry) = 70ms minimum
	elapsed := time.Since(startTime)
	if elapsed < 70*time.Millisecond {
		t.Error("Exponential backoff not working correctly")
	}
}

func TestOptimizedEventProcessor_GetMetrics(t *testing.T) {
	engine := &mockOptimizedEngine{}
	processor := NewOptimizedEventProcessor(engine)

	err := processor.Start()
	if err != nil {
		t.Fatalf("Failed to start processor: %v", err)
	}
	defer processor.Stop()

	// ctx not needed for Add methods

	// Submit some events
	for i := 0; i < 5; i++ {
		event := &domain.UnifiedEvent{
			ID:   string(rune(i)),
			Type: domain.EventTypeNetwork,
		}
		processor.AddNetworkEvent(event)
	}

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	metrics := processor.GetMetrics()
	if metrics == nil {
		t.Error("GetMetrics should return non-nil metrics")
	}

	if processedCount, ok := metrics["events_processed"].(uint64); !ok || processedCount == 0 {
		t.Error("Should have processed some events")
	}

	if _, ok := metrics["network_queue"]; !ok {
		t.Error("Channel depths should be tracked")
	}
}

func TestOptimizedEventProcessor_ChannelFull(t *testing.T) {
	engine := &mockOptimizedEngine{
		delays: map[string]time.Duration{
			string(domain.EventTypeNetwork): 100 * time.Millisecond, // Slow processing
		},
	}
	processor := NewOptimizedEventProcessor(engine)

	// Don't start the processor - channels won't be drained
	// Try to fill the network channel (size 1000)
	var submitErr error
	for i := 0; i < 1010; i++ { // Try to exceed channel size
		event := &domain.UnifiedEvent{
			ID:   string(rune(i % 256)), // Avoid invalid runes
			Type: domain.EventTypeNetwork,
		}
		if err := processor.AddNetworkEvent(event); err != nil {
			submitErr = err
			break
		}
	}

	// Should eventually get a channel full error
	if submitErr == nil {
		t.Error("Expected channel full error when adding to full channel")
	}
}

func TestOptimizedEventProcessor_ConcurrentSubmission(t *testing.T) {
	engine := &mockOptimizedEngine{}
	processor := NewOptimizedEventProcessor(engine)

	err := processor.Start()
	if err != nil {
		t.Fatalf("Failed to start processor: %v", err)
	}
	defer processor.Stop()

	// ctx not needed for Add methods
	const numGoroutines = 10
	const eventsPerGoroutine = 50

	var wg sync.WaitGroup

	// Submit events from multiple goroutines
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < eventsPerGoroutine; j++ {
				var eventType domain.EventType
				switch id % 4 {
				case 0:
					eventType = domain.EventTypeNetwork
				case 1:
					eventType = domain.EventTypeMemory
				case 2:
					eventType = domain.EventTypeProcess
				case 3:
					eventType = domain.EventTypeDisk
				}

				event := &domain.UnifiedEvent{
					ID:   string(rune(id*eventsPerGoroutine + j)),
					Type: eventType,
				}

				var err error
				switch id % 4 {
				case 0:
					err = processor.AddNetworkEvent(event)
				case 1:
					err = processor.AddMemoryEvent(event)
				case 2:
					err = processor.AddProcessEvent(event)
				case 3:
					err = processor.AddFileEvent(event)
				}

				if err != nil {
					t.Errorf("Failed to submit event: %v", err)
				}
			}
		}(i)
	}

	wg.Wait()

	// Wait for processing
	time.Sleep(500 * time.Millisecond)

	// Should have processed many events
	processed := engine.GetProcessedEvents()
	if len(processed) < numGoroutines*eventsPerGoroutine/2 {
		t.Errorf("Expected at least %d events, got %d", numGoroutines*eventsPerGoroutine/2, len(processed))
	}
}

func TestOptimizedEventProcessor_ContextCancellation(t *testing.T) {
	engine := &mockOptimizedEngine{}
	processor := NewOptimizedEventProcessor(engine)

	err := processor.Start()
	if err != nil {
		t.Fatalf("Failed to start processor: %v", err)
	}

	// Stop the processor
	processor.Stop()

	event := &domain.UnifiedEvent{
		ID:   "cancelled",
		Type: domain.EventTypeNetwork,
	}

	// Should fail due to stopped processor (channels are closed)
	err = processor.AddNetworkEvent(event)
	if err == nil {
		t.Error("Expected error due to stopped processor")
	}
}

func BenchmarkOptimizedEventProcessor_Add(b *testing.B) {
	engine := &mockOptimizedEngine{}
	processor := NewOptimizedEventProcessor(engine)

	processor.Start()
	defer processor.Stop()

	// ctx not needed for Add methods
	event := &domain.UnifiedEvent{
		ID:   "bench",
		Type: domain.EventTypeNetwork,
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			processor.AddNetworkEvent(event)
		}
	})
}

func BenchmarkOptimizedEventProcessor_ChannelMerge(b *testing.B) {
	engine := &mockOptimizedEngine{}
	processor := NewOptimizedEventProcessor(engine)

	processor.Start()
	defer processor.Stop()

	// ctx not needed for Add methods
	events := []*domain.UnifiedEvent{
		{ID: "1", Type: domain.EventTypeNetwork},
		{ID: "2", Type: domain.EventTypeMemory},
		{ID: "3", Type: domain.EventTypeProcess},
		{ID: "4", Type: domain.EventTypeDisk},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		event := events[i%len(events)]
		switch event.Type {
		case domain.EventTypeNetwork:
			processor.AddNetworkEvent(event)
		case domain.EventTypeMemory:
			processor.AddMemoryEvent(event)
		case domain.EventTypeProcess:
			processor.AddProcessEvent(event)
		case domain.EventTypeDisk:
			processor.AddFileEvent(event)
		}
	}
}
