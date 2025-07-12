package k8s

import (
	"context"
	"fmt"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
)

func TestWatchManager_EventDeduplication(t *testing.T) {
	config := DefaultWatchConfig()
	client := fake.NewSimpleClientset()
	wm := NewWatchManager(client, config)
	defer wm.Close()

	deduplicator := wm.eventMerger.deduplicator

	// Create a sample event
	event := watch.Event{
		Type:   watch.Added,
		Object: nil,
	}

	// First occurrence should not be duplicate
	if deduplicator.IsDuplicate(event) {
		t.Error("First occurrence should not be marked as duplicate")
	}

	// Immediate second occurrence should be duplicate
	if !deduplicator.IsDuplicate(event) {
		t.Error("Immediate second occurrence should be marked as duplicate")
	}

	// Wait for dedup window to expire
	time.Sleep(deduplicator.window + 10*time.Millisecond)

	// Should not be duplicate after window expires
	if deduplicator.IsDuplicate(event) {
		t.Error("Event should not be duplicate after window expires")
	}
}

func TestWatchManager_EventSequencing(t *testing.T) {
	sequencer := &EventSequencer{
		sequences: make(map[string]uint64),
	}

	source1 := "source1"
	source2 := "source2"

	// Test sequence generation
	seq1 := sequencer.NextSequence(source1)
	seq2 := sequencer.NextSequence(source1)
	seq3 := sequencer.NextSequence(source2)

	if seq1 != 1 {
		t.Errorf("Expected first sequence to be 1, got %d", seq1)
	}

	if seq2 != 2 {
		t.Errorf("Expected second sequence to be 2, got %d", seq2)
	}

	if seq3 != 1 {
		t.Errorf("Expected first sequence for source2 to be 1, got %d", seq3)
	}
}

func TestEventQueue_BackpressureHandling(t *testing.T) {
	capacity := 3
	eq := &EventQueue{
		events:   make([]watch.Event, 0, capacity),
		capacity: capacity,
	}

	// Fill queue to capacity
	for i := 0; i < capacity; i++ {
		event := watch.Event{Type: watch.Added}
		eq.AddWithBackpressure(event)
	}

	if len(eq.events) != capacity {
		t.Errorf("Expected queue to have %d events, got %d", capacity, len(eq.events))
	}

	// Add one more event, should remove oldest
	event := watch.Event{Type: watch.Modified}
	eq.AddWithBackpressure(event)

	if len(eq.events) != capacity {
		t.Errorf("Queue should still have %d events after backpressure, got %d", capacity, len(eq.events))
	}

	// Last event should be the one we just added
	lastEvent := eq.events[len(eq.events)-1]
	if lastEvent.Type != watch.Modified {
		t.Errorf("Expected last event to be Modified, got %v", lastEvent.Type)
	}
}

func TestWatchManager_StreamManagement(t *testing.T) {
	config := &WatchConfig{
		ReconnectInterval: 100 * time.Millisecond,
		EventBufferSize:   10,
		MaxRetries:        3,
		BackoffDuration:   10 * time.Millisecond,
		BackoffFactor:     1.5,
		HeartbeatInterval: 200 * time.Millisecond,
	}

	client := fake.NewSimpleClientset()
	wm := NewWatchManager(client, config)
	defer wm.Close()

	namespace := "test-namespace"
	resource := "pods"

	// Create a watch stream
	eventChan, err := wm.WatchResource(context.Background(), namespace, resource)
	if err != nil {
		t.Fatalf("Failed to create watch stream: %v", err)
	}

	// Verify stream is created
	streamID := fmt.Sprintf("%s/%s", namespace, resource)
	wm.mu.RLock()
	stream, exists := wm.streams[streamID]
	wm.mu.RUnlock()

	if !exists {
		t.Error("Watch stream should have been created")
	}

	if !stream.active {
		t.Error("Watch stream should be active")
	}

	// Test stopping the watch
	err = wm.StopWatch(namespace, resource)
	if err != nil {
		t.Errorf("Failed to stop watch: %v", err)
	}

	// Verify stream is removed
	wm.mu.RLock()
	_, exists = wm.streams[streamID]
	wm.mu.RUnlock()

	if exists {
		t.Error("Watch stream should have been removed")
	}

	// Verify event channel is closed
	select {
	case _, ok := <-eventChan:
		if ok {
			t.Error("Event channel should be closed")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Event channel should have been closed quickly")
	}
}

func TestWatchManager_ReconnectionLogic(t *testing.T) {
	config := &WatchConfig{
		ReconnectInterval: 50 * time.Millisecond,
		ReconnectTimeout:  1 * time.Second,
		MaxRetries:        2,
		BackoffDuration:   10 * time.Millisecond,
		BackoffFactor:     2.0,
		HeartbeatInterval: 100 * time.Millisecond,
	}

	client := fake.NewSimpleClientset()
	wm := NewWatchManager(client, config)
	defer wm.Close()

	// Create a mock stream with failed state
	stream := &WatchStream{
		id:             "test/pods",
		namespace:      "test",
		resource:       "pods",
		eventChan:      make(chan watch.Event, 10),
		errorChan:      make(chan error, 10),
		stopChan:       make(chan struct{}),
		active:         false,
		reconnectCount: 1,
		lastHeartbeat:  time.Now().Add(-2 * config.HeartbeatInterval),
	}

	wm.mu.Lock()
	wm.streams["test/pods"] = stream
	wm.mu.Unlock()

	// Wait for reconnection logic to trigger
	time.Sleep(config.ReconnectInterval + 50*time.Millisecond)

	// Stream should have been marked for reconnection
	stream.mu.RLock()
	reconnectAttempted := stream.reconnectCount > 1 || stream.active
	stream.mu.RUnlock()

	if !reconnectAttempted {
		t.Error("Reconnection should have been attempted")
	}
}

func TestEventDeduplicator_Cleanup(t *testing.T) {
	deduplicator := &EventDeduplicator{
		recentEvents: make(map[string]time.Time),
		window:       100 * time.Millisecond,
	}

	// Add some events
	now := time.Now()
	deduplicator.recentEvents["old-key"] = now.Add(-5 * deduplicator.window)
	deduplicator.recentEvents["recent-key"] = now.Add(-deduplicator.window / 2)

	// Run cleanup
	deduplicator.cleanup()

	// Old event should be cleaned up
	if _, exists := deduplicator.recentEvents["old-key"]; exists {
		t.Error("Old event should have been cleaned up")
	}

	// Recent event should remain
	if _, exists := deduplicator.recentEvents["recent-key"]; !exists {
		t.Error("Recent event should not have been cleaned up")
	}
}

func BenchmarkEventDeduplicator_IsDuplicate(b *testing.B) {
	deduplicator := &EventDeduplicator{
		recentEvents: make(map[string]time.Time),
		window:       1 * time.Second,
	}

	event := watch.Event{
		Type:   watch.Added,
		Object: nil,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		deduplicator.IsDuplicate(event)
	}
}

func BenchmarkEventSequencer_NextSequence(b *testing.B) {
	sequencer := &EventSequencer{
		sequences: make(map[string]uint64),
	}

	source := "benchmark-source"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sequencer.NextSequence(source)
	}
}
