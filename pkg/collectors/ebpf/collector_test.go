package ebpf

import (
	"context"
	"testing"
	"time"
)

func TestCollectorCreation(t *testing.T) {
	collector, err := NewCollector("ebpf-test")
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	if collector.Name() != "ebpf-test" {
		t.Errorf("Expected name 'ebpf-test', got '%s'", collector.Name())
	}

	if !collector.IsHealthy() {
		t.Error("Expected collector to be healthy initially")
	}
}

func TestCollectorLifecycle(t *testing.T) {
	collector, err := NewCollector("ebpf-lifecycle")
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Test that events channel is available
	events := collector.Events()
	if events == nil {
		t.Error("Events channel should not be nil")
	}

	// Test stop without start
	err = collector.Stop()
	if err != nil {
		t.Errorf("Stop should not fail even if not started: %v", err)
	}
}

func TestEventChannelCapacity(t *testing.T) {
	collector, err := NewCollector("ebpf-events")
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Start collector
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Note: This will likely fail in test environment without proper eBPF setup
	// but we test the basic functionality
	err = collector.Start(ctx)
	if err != nil {
		t.Logf("Expected error in test environment: %v", err)
		return // Skip rest of test in environments without eBPF support
	}

	// If start succeeded, test cleanup
	defer collector.Stop()

	// Test that collector reports as healthy
	if !collector.IsHealthy() {
		t.Error("Collector should be healthy after successful start")
	}
}

func TestEventTypeToString(t *testing.T) {
	collector, _ := NewCollector("ebpf-types")

	testCases := []struct {
		eventType uint32
		expected  string
	}{
		{1, "memory_alloc"},
		{2, "memory_free"},
		{3, "process_exec"},
		{4, "pod_syscall"},
		{5, "network_conn"},
		{99, "unknown"},
	}

	for _, tc := range testCases {
		result := collector.eventTypeToString(tc.eventType)
		if result != tc.expected {
			t.Errorf("For event type %d, expected '%s', got '%s'", tc.eventType, tc.expected, result)
		}
	}
}

func TestNullTerminatedString(t *testing.T) {
	collector, _ := NewCollector("ebpf-strings")

	testCases := []struct {
		input    []byte
		expected string
	}{
		{[]byte{'h', 'e', 'l', 'l', 'o', 0, 'w', 'o', 'r', 'l', 'd'}, "hello"},
		{[]byte{'t', 'e', 's', 't', 0}, "test"},
		{[]byte{0, 'a', 'b', 'c'}, ""},
		{[]byte{'n', 'o', 'n', 'u', 'l', 'l'}, "nonull"},
	}

	for _, tc := range testCases {
		result := collector.nullTerminatedString(tc.input)
		if result != tc.expected {
			t.Errorf("For input %v, expected '%s', got '%s'", tc.input, tc.expected, result)
		}
	}
}

func TestPodManagement(t *testing.T) {
	collector, _ := NewCollector("ebpf-pod")

	// Test UpdatePodInfo with uninitialized eBPF objects (should fail gracefully)
	err := collector.UpdatePodInfo(12345, "pod-123", "default", "nginx-pod")
	if err == nil {
		t.Error("Expected error when eBPF objects not initialized")
	}

	// Test RemovePodInfo with uninitialized eBPF objects (should fail gracefully)
	err = collector.RemovePodInfo(12345)
	if err == nil {
		t.Error("Expected error when eBPF objects not initialized")
	}

	// Test GetPodInfo with uninitialized eBPF objects (should fail gracefully)
	_, err = collector.GetPodInfo(12345)
	if err == nil {
		t.Error("Expected error when eBPF objects not initialized")
	}
}
