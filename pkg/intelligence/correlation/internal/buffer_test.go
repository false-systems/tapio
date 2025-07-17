package internal
import (
	"testing"
	"time"
	"github.com/falseyair/tapio/pkg/domain"
	"github.com/falseyair/tapio/pkg/intelligence/correlation/core"
)
func TestNewEventBuffer(t *testing.T) {
	capacity := 100
	buffer := NewEventBuffer(capacity)
	if buffer == nil {
		t.Fatal("Buffer should not be nil")
	}
	if buffer.Capacity() != capacity {
		t.Errorf("Expected capacity %d, got %d", capacity, buffer.Capacity())
	}
	if buffer.Size() != 0 {
		t.Errorf("Expected empty buffer, got size %d", buffer.Size())
	}
}
func TestEventBuffer_Add(t *testing.T) {
	buffer := NewEventBuffer(10)
	event := createTestEvent()
	// Test add event
	err := buffer.Add(event)
	if err != nil {
		t.Errorf("Failed to add event: %v", err)
	}
	if buffer.Size() != 1 {
		t.Errorf("Expected size 1, got %d", buffer.Size())
	}
	// Test add duplicate event
	err = buffer.Add(event)
	if err != nil {
		t.Errorf("Failed to add duplicate event: %v", err)
	}
	// Size should still be 1 (duplicate ignored)
	if buffer.Size() != 1 {
		t.Errorf("Expected size 1 after duplicate, got %d", buffer.Size())
	}
}
func TestEventBuffer_Get(t *testing.T) {
	buffer := NewEventBuffer(10)
	event := createTestEvent()
	// Test get non-existent event
	_, err := buffer.Get(event.ID)
	if err == nil {
		t.Error("Expected error for non-existent event")
	}
	// Add event and test get
	err = buffer.Add(event)
	if err != nil {
		t.Fatalf("Failed to add event: %v", err)
	}
	retrieved, err := buffer.Get(event.ID)
	if err != nil {
		t.Errorf("Failed to get event: %v", err)
	}
	if retrieved.ID != event.ID {
		t.Errorf("Expected event ID %s, got %s", event.ID, retrieved.ID)
	}
}
func TestEventBuffer_Remove(t *testing.T) {
	buffer := NewEventBuffer(10)
	event := createTestEvent()
	// Test remove non-existent event
	err := buffer.Remove(event.ID)
	if err != core.ErrEventNotFound {
		t.Errorf("Expected ErrEventNotFound, got %v", err)
	}
	// Add event and test remove
	err = buffer.Add(event)
	if err != nil {
		t.Fatalf("Failed to add event: %v", err)
	}
	err = buffer.Remove(event.ID)
	if err != nil {
		t.Errorf("Failed to remove event: %v", err)
	}
	if buffer.Size() != 0 {
		t.Errorf("Expected empty buffer after remove, got size %d", buffer.Size())
	}
	// Verify event is gone
	_, err = buffer.Get(event.ID)
	if err != core.ErrEventNotFound {
		t.Error("Event should be gone after remove")
	}
}
func TestEventBuffer_Clear(t *testing.T) {
	buffer := NewEventBuffer(10)
	// Add multiple events
	for i := 0; i < 5; i++ {
		event := createTestEvent()
		event.ID = domain.EventID(event.ID + string(rune('0'+i)))
		err := buffer.Add(event)
		if err != nil {
			t.Fatalf("Failed to add event %d: %v", i, err)
		}
	}
	if buffer.Size() != 5 {
		t.Errorf("Expected size 5, got %d", buffer.Size())
	}
	// Test clear
	err := buffer.Clear()
	if err != nil {
		t.Errorf("Failed to clear buffer: %v", err)
	}
	if buffer.Size() != 0 {
		t.Errorf("Expected empty buffer after clear, got size %d", buffer.Size())
	}
}
func TestEventBuffer_GetByTimeRange(t *testing.T) {
	buffer := NewEventBuffer(10)
	now := time.Now()
	// Add events with different timestamps
	events := []domain.Event{
		createEventWithTimestamp(now.Add(-5*time.Minute)),
		createEventWithTimestamp(now.Add(-3*time.Minute)),
		createEventWithTimestamp(now.Add(-1*time.Minute)),
		createEventWithTimestamp(now),
		createEventWithTimestamp(now.Add(1*time.Minute)),
	}
	for i, event := range events {
		event.ID = domain.EventID(event.ID + string(rune('0'+i)))
		err := buffer.Add(event)
		if err != nil {
			t.Fatalf("Failed to add event %d: %v", i, err)
		}
	}
	// Test get events in range
	start := now.Add(-4 * time.Minute)
	end := now.Add(30 * time.Second)
	rangeEvents, err := buffer.GetByTimeRange(start, end)
	if err != nil {
		t.Errorf("Failed to get events by time range: %v", err)
	}
	// Should get 3 events (at -3min, -1min, and now)
	expectedCount := 3
	if len(rangeEvents) != expectedCount {
		t.Errorf("Expected %d events in range, got %d", expectedCount, len(rangeEvents))
	}
	// Test invalid time range
	_, err = buffer.GetByTimeRange(end, start)
	if err != core.ErrInvalidTimeRange {
		t.Errorf("Expected ErrInvalidTimeRange, got %v", err)
	}
}
func TestEventBuffer_GetBySource(t *testing.T) {
	buffer := NewEventBuffer(10)
	// Add events from different sources
	ebpfEvent := createTestEvent()
	ebpfEvent.ID = "ebpf-event"
	ebpfEvent.Source = domain.SourceEBPF
	k8sEvent := createTestEvent()
	k8sEvent.ID = "k8s-event"
	k8sEvent.Source = domain.SourceKubernetes
	journaldEvent := createTestEvent()
	journaldEvent.ID = "journald-event"
	journaldEvent.Source = domain.SourceJournald
	events := []domain.Event{ebpfEvent, k8sEvent, journaldEvent}
	for _, event := range events {
		err := buffer.Add(event)
		if err != nil {
			t.Fatalf("Failed to add event: %v", err)
		}
	}
	// Test get by source
	ebpfEvents, err := buffer.GetBySource(domain.SourceEBPF)
	if err != nil {
		t.Errorf("Failed to get eBPF events: %v", err)
	}
	if len(ebpfEvents) != 1 {
		t.Errorf("Expected 1 eBPF event, got %d", len(ebpfEvents))
	}
	if ebpfEvents[0].ID != "ebpf-event" {
		t.Errorf("Expected eBPF event ID 'ebpf-event', got %s", ebpfEvents[0].ID)
	}
	// Test get by non-existent source
	systemdEvents, err := buffer.GetBySource(domain.SourceSystemd)
	if err != nil {
		t.Errorf("Failed to get systemd events: %v", err)
	}
	if len(systemdEvents) != 0 {
		t.Errorf("Expected 0 systemd events, got %d", len(systemdEvents))
	}
}
func TestEventBuffer_GetByType(t *testing.T) {
	buffer := NewEventBuffer(10)
	// Add events of different types
	memoryEvent := createTestMemoryEvent()
	memoryEvent.ID = "memory-event"
	networkEvent := createTestNetworkEvent()
	networkEvent.ID = "network-event"
	serviceEvent := createTestEvent()
	serviceEvent.ID = "service-event"
	serviceEvent.Type = domain.EventTypeService
	events := []domain.Event{memoryEvent, networkEvent, serviceEvent}
	for _, event := range events {
		err := buffer.Add(event)
		if err != nil {
			t.Fatalf("Failed to add event: %v", err)
		}
	}
	// Test get by type
	memoryEvents, err := buffer.GetByType(domain.EventTypeMemory)
	if err != nil {
		t.Errorf("Failed to get memory events: %v", err)
	}
	if len(memoryEvents) != 1 {
		t.Errorf("Expected 1 memory event, got %d", len(memoryEvents))
	}
	if memoryEvents[0].ID != "memory-event" {
		t.Errorf("Expected memory event ID 'memory-event', got %s", memoryEvents[0].ID)
	}
}
func TestEventBuffer_CapacityAndEviction(t *testing.T) {
	buffer := NewEventBuffer(3) // Small capacity for testing
	// Add events up to capacity
	for i := 0; i < 3; i++ {
		event := createTestEvent()
		event.ID = domain.EventID(event.ID + string(rune('0'+i)))
		err := buffer.Add(event)
		if err != nil {
			t.Fatalf("Failed to add event %d: %v", i, err)
		}
	}
	if buffer.Size() != 3 {
		t.Errorf("Expected size 3, got %d", buffer.Size())
	}
	// Add one more event (should evict oldest)
	newEvent := createTestEvent()
	newEvent.ID = "new-event"
	newEvent.Timestamp = time.Now() // Newer timestamp
	err := buffer.Add(newEvent)
	if err != nil {
		t.Errorf("Failed to add event beyond capacity: %v", err)
	}
	// Size should still be 3
	if buffer.Size() != 3 {
		t.Errorf("Expected size 3 after eviction, got %d", buffer.Size())
	}
	// Oldest event should be gone
	_, err = buffer.Get("test-event-10")
	if err != core.ErrEventNotFound {
		t.Error("Oldest event should have been evicted")
	}
	// New event should be present
	_, err = buffer.Get("new-event")
	if err != nil {
		t.Error("New event should be present")
	}
}
func TestEventBuffer_OldestNewestEvent(t *testing.T) {
	buffer := NewEventBuffer(10)
	now := time.Now()
	// Test empty buffer
	_, err := buffer.OldestEvent()
	if err != core.ErrBufferEmpty {
		t.Errorf("Expected ErrBufferEmpty for oldest, got %v", err)
	}
	_, err = buffer.NewestEvent()
	if err != core.ErrBufferEmpty {
		t.Errorf("Expected ErrBufferEmpty for newest, got %v", err)
	}
	// Add events with different timestamps
	oldEvent := createEventWithTimestamp(now.Add(-10 * time.Minute))
	oldEvent.ID = "old-event"
	newEvent := createEventWithTimestamp(now)
	newEvent.ID = "new-event"
	middleEvent := createEventWithTimestamp(now.Add(-5 * time.Minute))
	middleEvent.ID = "middle-event"
	// Add in random order
	err = buffer.Add(newEvent)
	if err != nil {
		t.Fatalf("Failed to add new event: %v", err)
	}
	err = buffer.Add(oldEvent)
	if err != nil {
		t.Fatalf("Failed to add old event: %v", err)
	}
	err = buffer.Add(middleEvent)
	if err != nil {
		t.Fatalf("Failed to add middle event: %v", err)
	}
	// Test oldest event
	oldest, err := buffer.OldestEvent()
	if err != nil {
		t.Errorf("Failed to get oldest event: %v", err)
	}
	if oldest.ID != "old-event" {
		t.Errorf("Expected oldest event ID 'old-event', got %s", oldest.ID)
	}
	// Test newest event
	newest, err := buffer.NewestEvent()
	if err != nil {
		t.Errorf("Failed to get newest event: %v", err)
	}
	if newest.ID != "new-event" {
		t.Errorf("Expected newest event ID 'new-event', got %s", newest.ID)
	}
}
func TestEventBuffer_Expire(t *testing.T) {
	buffer := NewEventBuffer(10)
	now := time.Now()
	// Add events with different timestamps
	oldEvent1 := createEventWithTimestamp(now.Add(-2 * time.Hour))
	oldEvent1.ID = "old-event-1"
	oldEvent2 := createEventWithTimestamp(now.Add(-1 * time.Hour))
	oldEvent2.ID = "old-event-2"
	newEvent := createEventWithTimestamp(now.Add(-10 * time.Minute))
	newEvent.ID = "new-event"
	events := []domain.Event{oldEvent1, oldEvent2, newEvent}
	for _, event := range events {
		err := buffer.Add(event)
		if err != nil {
			t.Fatalf("Failed to add event: %v", err)
		}
	}
	if buffer.Size() != 3 {
		t.Errorf("Expected size 3, got %d", buffer.Size())
	}
	// Expire events older than 30 minutes
	cutoff := now.Add(-30 * time.Minute)
	expiredCount, err := buffer.Expire(cutoff)
	if err != nil {
		t.Errorf("Failed to expire events: %v", err)
	}
	if expiredCount != 2 {
		t.Errorf("Expected 2 expired events, got %d", expiredCount)
	}
	if buffer.Size() != 1 {
		t.Errorf("Expected size 1 after expiration, got %d", buffer.Size())
	}
	// Only new event should remain
	_, err = buffer.Get("new-event")
	if err != nil {
		t.Error("New event should still be present")
	}
	_, err = buffer.Get("old-event-1")
	if err != core.ErrEventNotFound {
		t.Error("Old event 1 should be expired")
	}
	_, err = buffer.Get("old-event-2")
	if err != core.ErrEventNotFound {
		t.Error("Old event 2 should be expired")
	}
}
// Helper functions
func createEventWithTimestamp(timestamp time.Time) domain.Event {
	event := createTestEvent()
	event.Timestamp = timestamp
	return event
}