package runtime_signals

import (
	"context"
	"testing"
	"time"
)

func TestCollectorIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	setupOTELForTesting(t)

	collector, err := NewCollector("integration-cni")
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := collector.Start(ctx); err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}

	// Let it run for a short time
	time.Sleep(100 * time.Millisecond)

	if !collector.IsHealthy() {
		t.Error("Collector should be healthy during operation")
	}

	// Test graceful shutdown
	if err := collector.Stop(); err != nil {
		t.Fatalf("Failed to stop collector: %v", err)
	}

	// Verify channel is closed
	select {
	case _, ok := <-collector.Events():
		if ok {
			t.Error("Events channel should be closed after stop")
		}
	case <-time.After(100 * time.Millisecond):
		// Channel might already be drained
	}
}

func TestCollectorEventGeneration(t *testing.T) {
	setupOTELForTesting(t)

	collector, err := NewCollector("event-test")
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Test various event types
	eventTypes := []string{
		"netns_create",
		"netns_enter",
		"netns_exit",
	}

	for _, eventType := range eventTypes {
		data := map[string]string{
			"pid":        "1234",
			"comm":       "test-process",
			"netns_path": "/var/run/netns/test",
		}

		event := collector.createEvent(eventType, data)

		// Check that event type is set properly based on the eventType parameter
		if event.Type == "" {
			t.Error("Event type should not be empty")
		}

		if event.Metadata.Labels["event_type"] != eventType {
			t.Errorf("Expected event type '%s', got '%s'", eventType, event.Metadata.Labels["event_type"])
		}

		if event.TraceContext == nil || !event.TraceContext.TraceID.IsValid() || !event.TraceContext.SpanID.IsValid() {
			t.Error("Event should have valid trace and span IDs")
		}
	}
}

func TestCollectorEdgeCases(t *testing.T) {
	setupOTELForTesting(t)

	collector, err := NewCollector("edge-test")
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Test with empty data
	event := collector.createEvent("test", map[string]string{})
	if event.EventData.RawData == nil || len(event.EventData.RawData.Data) == 0 {
		t.Error("Event should have raw data even when empty")
	}

	// Test with nil context
	if err := collector.Start(nil); err == nil {
		t.Error("Should fail with nil context")
	}

	// Test double start - start it first time successfully
	ctx := context.Background()
	if err := collector.Start(ctx); err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}

	// Test double start - should fail on second start
	if err := collector.Start(ctx); err == nil {
		t.Error("Should fail when starting already started collector")
	}

	if err := collector.Stop(); err != nil {
		t.Fatalf("Failed first stop: %v", err)
	}

	if err := collector.Stop(); err != nil {
		t.Fatalf("Failed second stop: %v", err)
	}
}
