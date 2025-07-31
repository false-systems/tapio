package etcd

import (
	"context"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
)

func TestCollector(t *testing.T) {
	config := collectors.CollectorConfig{
		BufferSize: 100,
		Labels: map[string]string{
			"etcd_endpoints": "localhost:2379",
		},
	}
	collector, err := NewCollector(config)
	if err != nil {
		t.Logf("Expected error creating collector without etcd server: %v", err)
		return // This is expected if no etcd server is running
	}

	ctx := context.Background()
	err = collector.Start(ctx)
	if err != nil {
		t.Logf("Expected error starting collector without etcd server: %v", err)
		collector.Stop() // Always clean up
		return
	}
	defer collector.Stop()

	// Verify basic properties
	name := collector.Name()
	if name != "etcd" && name != "etcd-ebpf" {
		t.Errorf("Expected name 'etcd' or 'etcd-ebpf', got '%s'", name)
	}

	if !collector.IsHealthy() {
		t.Error("Collector should be healthy initially")
	}

	// Check that we can receive from the events channel
	eventsChan := collector.Events()
	if eventsChan == nil {
		t.Error("Events channel should not be nil")
	}
}

func TestCollectorStartStop(t *testing.T) {
	config := collectors.CollectorConfig{
		BufferSize: 100,
		Labels: map[string]string{
			"etcd_endpoints": "localhost:2379",
		},
	}
	collector, err := NewCollector(config)
	if err != nil {
		t.Logf("Expected error creating collector without etcd server: %v", err)
		return
	}

	// Test double start
	ctx := context.Background()
	err = collector.Start(ctx)
	if err != nil {
		t.Logf("Expected error starting collector without etcd server: %v", err)
		collector.Stop()
		return
	}

	err = collector.Start(ctx)
	if err == nil {
		t.Error("Expected error on double start")
	}

	// Test stop
	err = collector.Stop()
	if err != nil {
		t.Errorf("Failed to stop collector: %v", err)
	}
}

func TestCollectorHealthCheck(t *testing.T) {
	config := collectors.CollectorConfig{
		BufferSize: 100,
		Labels: map[string]string{
			"etcd_endpoints": "localhost:2379",
		},
	}
	collector, err := NewCollector(config)
	if err != nil {
		t.Logf("Expected error creating collector without etcd server: %v", err)
		return
	}

	// Should be healthy on creation
	if !collector.IsHealthy() {
		t.Error("Collector should be healthy on creation")
	}

	// Start and verify still healthy (may fail without etcd server)
	ctx := context.Background()
	err = collector.Start(ctx)
	if err != nil {
		t.Logf("Expected error starting collector without etcd server: %v", err)
		collector.Stop()
		return
	}
	defer collector.Stop()

	if !collector.IsHealthy() {
		t.Error("Collector should remain healthy after start")
	}
}

func TestCollectorContextCancellation(t *testing.T) {
	config := collectors.CollectorConfig{
		BufferSize: 100,
		Labels: map[string]string{
			"etcd_endpoints": "localhost:2379",
		},
	}
	collector, err := NewCollector(config)
	if err != nil {
		t.Logf("Expected error creating collector without etcd server: %v", err)
		return
	}

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Always call cancel to avoid leak

	err = collector.Start(ctx)
	if err != nil {
		t.Logf("Expected error starting collector without etcd server: %v", err)
		collector.Stop()
		return
	}

	// Cancel context
	cancel()

	// Give collector time to stop
	time.Sleep(100 * time.Millisecond)

	// Clean shutdown
	err = collector.Stop()
	if err != nil {
		t.Errorf("Failed to stop collector: %v", err)
	}
}
