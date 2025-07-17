package domain

import (
	"testing"
	"time"
)

func TestEventCreation(t *testing.T) {
	// Test SystemEvent creation
	payload := SystemEventPayload{
		Syscall:     "open",
		ReturnCode:  0,
		Arguments:   map[string]string{"path": "/etc/passwd"},
		MemoryUsage: int64Ptr(1024 * 1024),
	}

	event := NewEvent(EventTypeSystem, SourceEBPF, payload)

	if event.ID == "" {
		t.Error("Event ID should not be empty")
	}

	if event.Type != EventTypeSystem {
		t.Errorf("Expected type %s, got %s", EventTypeSystem, event.Type)
	}

	if event.Source != SourceEBPF {
		t.Errorf("Expected source %s, got %s", SourceEBPF, event.Source)
	}

	if event.Payload.PayloadType() != "system" {
		t.Errorf("Expected payload type 'system', got %s", event.Payload.PayloadType())
	}
}

func TestTimeWindow(t *testing.T) {
	start := time.Now()
	end := start.Add(time.Hour)

	window := TimeWindow{
		Start: start,
		End:   end,
	}

	if window.Duration() != time.Hour {
		t.Errorf("Expected duration 1h, got %v", window.Duration())
	}

	testTime := start.Add(30 * time.Minute)
	if !window.Contains(testTime) {
		t.Error("Window should contain time in the middle")
	}

	if window.Contains(start.Add(-time.Minute)) {
		t.Error("Window should not contain time before start")
	}
}

// Helper functions
func int64Ptr(i int64) *int64 {
	return &i
}

func int32Ptr(i int32) *int32 {
	return &i
}
