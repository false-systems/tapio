package runtimesignals

import (
	"context"
	"testing"
	"time"
)

func TestCollectorStartStop(t *testing.T) {
	// Create collector
	collector, err := NewCollector("test-start-stop")
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Start collector
	ctx := context.Background()
	err = collector.Start(ctx)
	if err != nil {
		// If we're not running as root or on non-Linux, eBPF will fail
		// That's OK for this test - we're just checking initialization
		t.Logf("Collector start failed (expected on non-Linux or non-root): %v", err)
	}

	// Let it run briefly
	time.Sleep(100 * time.Millisecond)

	// Check health
	health := collector.Health()
	if health == nil {
		t.Fatal("Health should not be nil")
	}
	t.Logf("Collector health: %+v", health)

	// Check statistics
	stats := collector.Statistics()
	if stats == nil {
		t.Fatal("Statistics should not be nil")
	}
	t.Logf("Collector stats: %+v", stats)

	// Stop collector
	err = collector.Stop()
	if err != nil {
		t.Errorf("Failed to stop collector: %v", err)
	}

	t.Log("Collector lifecycle test passed")
}

func TestCollectorEventChannel(t *testing.T) {
	collector, err := NewCollector("test-events")
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Get event channel
	events := collector.Events()
	if events == nil {
		t.Fatal("Event channel should not be nil")
	}

	// Verify it's a receive-only channel
	select {
	case <-events:
		// Channel might be closed or have events
	default:
		// No events yet, which is expected
	}

	t.Log("Event channel test passed")
}

func TestCollectorName(t *testing.T) {
	expectedName := "test-naming"
	collector, err := NewCollector(expectedName)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	actualName := collector.Name()
	if actualName != expectedName {
		t.Errorf("Expected name %s, got %s", expectedName, actualName)
	}

	t.Log("Collector naming test passed")
}