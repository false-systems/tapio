package k8s

import (
	"context"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
)

func TestNewCollector(t *testing.T) {
	config := collectors.CollectorConfig{
		BufferSize: 100,
		Labels: map[string]string{
			"test": "true",
		},
	}

	collector, err := NewCollector(config)
	if err != nil {
		t.Logf("Expected error creating collector without K8s cluster: %v", err)
		return // This is expected if no K8s cluster is available
	}

	// Verify basic properties
	name := collector.Name()
	if name != "k8s-minimal" {
		t.Errorf("Expected name 'k8s-minimal', got '%s'", name)
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

func TestMinimalK8sCollector(t *testing.T) {
	config := collectors.CollectorConfig{
		BufferSize: 100,
		Labels: map[string]string{
			"test": "true",
		},
	}

	collector, err := NewMinimalK8sCollector(config)
	if err != nil {
		t.Logf("Expected error creating collector without K8s cluster: %v", err)
		return
	}

	// Test start and stop
	ctx := context.Background()
	err = collector.Start(ctx)
	if err != nil {
		t.Logf("Expected error starting collector without K8s cluster: %v", err)
		collector.Stop()
		return
	}
	defer collector.Stop()

	// Should be healthy after start
	if !collector.IsHealthy() {
		t.Error("Collector should be healthy after start")
	}
}

func TestMinimalK8sCollectorStartStop(t *testing.T) {
	config := collectors.CollectorConfig{
		BufferSize: 100,
		Labels: map[string]string{
			"test": "true",
		},
	}

	collector, err := NewMinimalK8sCollector(config)
	if err != nil {
		t.Logf("Expected error creating collector without K8s cluster: %v", err)
		return
	}

	ctx := context.Background()
	err = collector.Start(ctx)
	if err != nil {
		t.Logf("Expected error starting collector without K8s cluster: %v", err)
		collector.Stop()
		return
	}

	// Test double start should not fail
	err = collector.Start(ctx)
	if err != nil {
		t.Errorf("Double start should not fail: %v", err)
	}

	// Test stop
	err = collector.Stop()
	if err != nil {
		t.Errorf("Failed to stop collector: %v", err)
	}

	// Test double stop should not fail
	err = collector.Stop()
	if err != nil {
		t.Errorf("Double stop should not fail: %v", err)
	}
}

func TestMinimalK8sCollectorWithEBPF(t *testing.T) {
	config := collectors.CollectorConfig{
		BufferSize: 100,
		Labels: map[string]string{
			"enable_ebpf": "true",
			"test":        "true",
		},
	}

	collector, err := NewMinimalK8sCollector(config)
	if err != nil {
		t.Logf("Expected error creating collector without K8s cluster: %v", err)
		return
	}

	// Should handle eBPF creation gracefully even if it fails
	ctx := context.Background()
	err = collector.Start(ctx)
	if err != nil {
		t.Logf("Expected error starting collector without K8s cluster: %v", err)
		collector.Stop()
		return
	}
	defer collector.Stop()

	// Collector should still work without eBPF
	if !collector.IsHealthy() {
		t.Error("Collector should be healthy even if eBPF fails")
	}
}

func TestMinimalK8sCollectorContextCancellation(t *testing.T) {
	config := collectors.CollectorConfig{
		BufferSize: 100,
		Labels: map[string]string{
			"test": "true",
		},
	}

	collector, err := NewMinimalK8sCollector(config)
	if err != nil {
		t.Logf("Expected error creating collector without K8s cluster: %v", err)
		return
	}

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Always call cancel to avoid leak

	err = collector.Start(ctx)
	if err != nil {
		t.Logf("Expected error starting collector without K8s cluster: %v", err)
		collector.Stop()
		return
	}

	// Cancel context
	cancel()

	// Give collector time to handle cancellation
	time.Sleep(100 * time.Millisecond)

	// Clean shutdown
	err = collector.Stop()
	if err != nil {
		t.Errorf("Failed to stop collector: %v", err)
	}
}

func TestDefaultK8sConfig(t *testing.T) {
	config := DefaultK8sConfig()

	if config.BufferSize != 1000 {
		t.Errorf("Expected buffer size 1000, got %d", config.BufferSize)
	}

	if !config.MetricsEnabled {
		t.Error("Expected metrics to be enabled")
	}

	if config.Labels == nil {
		t.Error("Expected labels map to be initialized")
	}
}
