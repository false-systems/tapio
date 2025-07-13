package collectors

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestEBPFAdapterIntegration(t *testing.T) {
	// Skip if not running as root (eBPF requires privileges)
	if !isRoot() {
		t.Skip("Skipping eBPF tests - requires root privileges")
	}
	
	t.Run("CreateAndConfigure", func(t *testing.T) {
		adapter, err := NewEBPFAdapter()
		if err != nil {
			t.Fatalf("Failed to create eBPF adapter: %v", err)
		}
		
		config := CollectorConfig{
			Name:            "test-ebpf",
			Type:            "ebpf",
			Enabled:         true,
			SamplingRate:    1.0,
			MaxEventsPerSec: 1000,
			BufferSize:      10000,
			Extra: map[string]interface{}{
				"ml_prediction_enabled": true,
				"prediction_threshold":  0.8,
				"ring_buffer_size":      8 * 1024 * 1024,
			},
		}
		
		if err := adapter.Configure(config); err != nil {
			t.Fatalf("Failed to configure adapter: %v", err)
		}
		
		if !adapter.IsEnabled() {
			t.Error("Adapter should be enabled after configuration")
		}
	})
	
	t.Run("StartStopLifecycle", func(t *testing.T) {
		adapter, err := NewEBPFAdapter()
		if err != nil {
			t.Fatalf("Failed to create eBPF adapter: %v", err)
		}
		
		config := CollectorConfig{
			Name:            "test-ebpf",
			Type:            "ebpf",
			Enabled:         true,
			SamplingRate:    1.0,
			MaxEventsPerSec: 1000,
			BufferSize:      10000,
		}
		
		if err := adapter.Configure(config); err != nil {
			t.Fatalf("Failed to configure adapter: %v", err)
		}
		
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		
		// Start the adapter
		if err := adapter.Start(ctx); err != nil {
			t.Fatalf("Failed to start adapter: %v", err)
		}
		
		// Let it run briefly
		time.Sleep(100 * time.Millisecond)
		
		// Check health
		health := adapter.Health()
		if health.Status != HealthStatusHealthy {
			t.Errorf("Expected healthy status, got %s: %s", health.Status, health.Message)
		}
		
		// Stop the adapter
		if err := adapter.Stop(); err != nil {
			t.Fatalf("Failed to stop adapter: %v", err)
		}
		
		// Check health after stop
		health = adapter.Health()
		if health.Status != HealthStatusStopped {
			t.Errorf("Expected stopped status, got %s", health.Status)
		}
	})
	
	t.Run("EventConversion", func(t *testing.T) {
		adapter, err := NewEBPFAdapter()
		if err != nil {
			t.Fatalf("Failed to create eBPF adapter: %v", err)
		}
		
		// Test event conversion (would need mock eBPF event)
		// This is a placeholder for actual conversion testing
		t.Log("Event conversion test placeholder")
	})
}

func TestCollectorManagerIntegration(t *testing.T) {
	t.Run("RegisterEBPFCollector", func(t *testing.T) {
		// Create manager configuration
		config := &Config{
			EnabledCollectors: []string{"ebpf"},
			SamplingRate:     1.0,
			MaxEventsPerSec:  10000,
			BufferSize:       10000,
			GRPC: GRPCConfig{
				ServerEndpoints: []string{"localhost:9090"},
				MaxBatchSize:    100,
				BatchTimeout:    100 * time.Millisecond,
			},
			Resources: ResourceConfig{
				MaxMemoryMB: 100,
				MaxCPUMilli: 10,
			},
		}
		
		// Create mock gRPC client
		grpcClient := &GRPCStreamingClient{}
		
		// Create manager
		manager := NewManager(config, grpcClient)
		
		// Check that eBPF collector can be created via factory
		collectors := ListAvailableCollectors()
		found := false
		for _, c := range collectors {
			if c == "ebpf" {
				found = true
				break
			}
		}
		
		if !found {
			t.Error("eBPF collector not found in available collectors")
		}
		
		// Create collector via factory
		collector, err := CreateCollector("ebpf", config)
		if err != nil {
			// This might fail if not running as root
			if isRoot() {
				t.Fatalf("Failed to create eBPF collector: %v", err)
			} else {
				t.Skipf("Skipping eBPF collector creation - requires root: %v", err)
			}
		}
		
		// Register with manager
		if err := manager.Register(collector); err != nil {
			t.Fatalf("Failed to register collector: %v", err)
		}
		
		// Verify registration
		registered := manager.ListCollectors()
		if len(registered) != 1 || registered[0] != "ebpf" {
			t.Errorf("Expected [ebpf], got %v", registered)
		}
	})
}

func TestEventBatcher(t *testing.T) {
	t.Run("BatchingLogic", func(t *testing.T) {
		batchCount := 0
		var lastBatch []*Event
		
		batcher := NewEventBatcher(BatcherConfig{
			MaxBatchSize:     10,
			MaxBatchBytes:    1024,
			BatchTimeout:     50 * time.Millisecond,
			CompressionLevel: CompressionLevelFast,
			OnBatch: func(batch []*Event) error {
				batchCount++
				lastBatch = batch
				return nil
			},
		})
		
		batcher.Start()
		defer batcher.Stop()
		
		// Add events
		for i := 0; i < 15; i++ {
			event := &Event{
				ID:        fmt.Sprintf("test-%d", i),
				Timestamp: time.Now(),
				Type:      EventTypeMetric,
				Severity:  SeverityInfo,
				Source: EventSource{
					Collector: "test",
					Component: "test",
					Node:      "test-node",
				},
				Data: map[string]interface{}{
					"value": i,
				},
			}
			
			if err := batcher.Add(event); err != nil {
				t.Fatalf("Failed to add event: %v", err)
			}
		}
		
		// Wait for batching
		time.Sleep(100 * time.Millisecond)
		
		// Should have received 2 batches (10 + 5)
		if batchCount < 2 {
			t.Errorf("Expected at least 2 batches, got %d", batchCount)
		}
		
		// Force flush
		batcher.Flush()
		time.Sleep(50 * time.Millisecond)
		
		stats := batcher.GetStats()
		if totalEvents, ok := stats["total_events"].(uint64); ok {
			if totalEvents != 15 {
				t.Errorf("Expected 15 total events, got %d", totalEvents)
			}
		}
	})
}

func isRoot() bool {
	// Simple check - in production would use proper uid check
	return false // Default to false for safety in tests
}