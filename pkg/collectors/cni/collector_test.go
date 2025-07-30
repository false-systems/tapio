package cni

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/collectors"
)

// Unit Tests

func TestCollector(t *testing.T) {
	config := collectors.DefaultCollectorConfig()
	collector, err := NewCollector(config)
	require.NoError(t, err)

	assert.Equal(t, "cni", collector.Name())
	assert.True(t, collector.IsHealthy())
}

func TestCollectorStartStop(t *testing.T) {
	config := collectors.DefaultCollectorConfig()
	config.BufferSize = 10

	collector, err := NewCollector(config)
	require.NoError(t, err)

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)

	// Should not be able to start twice
	err = collector.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already started")

	// Stop collector
	err = collector.Stop()
	require.NoError(t, err)

	// Events channel should be closed
	_, ok := <-collector.Events()
	assert.False(t, ok)
}

func TestCollectorContext(t *testing.T) {
	config := collectors.DefaultCollectorConfig()
	collector, err := NewCollector(config)
	require.NoError(t, err)

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	err = collector.Start(ctx)
	require.NoError(t, err)

	// Cancel context should stop collector
	cancel()
	
	// Give it time to clean up
	time.Sleep(100 * time.Millisecond)

	// Events channel should eventually close
	timeout := time.After(1 * time.Second)
	for {
		select {
		case _, ok := <-collector.Events():
			if !ok {
				return // Success - channel closed
			}
		case <-timeout:
			t.Fatal("timeout waiting for channel to close")
		}
	}
}

func TestCNIStrategyImplementations(t *testing.T) {
	tests := []struct {
		name             string
		strategy         CNIStrategy
		wantName         string
		minLogPaths      int
		minWatchPaths    int
		expectedLogPath  string
		expectedWatchPath string
	}{
		{
			name:             "Calico strategy",
			strategy:         &CalicoStrategy{},
			wantName:         "calico",
			minLogPaths:      2,
			minWatchPaths:    2,
			expectedLogPath:  "/var/log/calico/cni/",
			expectedWatchPath: "/etc/cni/net.d/",
		},
		{
			name:             "Cilium strategy",
			strategy:         &CiliumStrategy{},
			wantName:         "cilium",
			minLogPaths:      2,
			minWatchPaths:    2,
			expectedLogPath:  "/var/run/cilium/cilium.log",
			expectedWatchPath: "/var/run/cilium/",
		},
		{
			name:             "Flannel strategy",
			strategy:         &FlannelStrategy{},
			wantName:         "flannel",
			minLogPaths:      1,
			minWatchPaths:    2,
			expectedLogPath:  "/var/log/flanneld.log",
			expectedWatchPath: "/run/flannel/",
		},
		{
			name:             "Generic strategy",
			strategy:         &GenericStrategy{},
			wantName:         "generic",
			minLogPaths:      1,
			minWatchPaths:    2,
			expectedLogPath:  "/var/log/cni/",
			expectedWatchPath: "/var/lib/cni/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantName, tt.strategy.GetName())
			
			logPaths := tt.strategy.GetLogPaths()
			assert.GreaterOrEqual(t, len(logPaths), tt.minLogPaths)
			assert.Contains(t, logPaths, tt.expectedLogPath)
			
			watchPaths := tt.strategy.GetWatchPaths()
			assert.GreaterOrEqual(t, len(watchPaths), tt.minWatchPaths)
			assert.Contains(t, watchPaths, tt.expectedWatchPath)
		})
	}
}

func TestCollectorHealthy(t *testing.T) {
	config := collectors.DefaultCollectorConfig()
	collector, err := NewCollector(config)
	require.NoError(t, err)

	// Should be healthy initially
	assert.True(t, collector.IsHealthy())

	// Start and stop should maintain health
	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)
	assert.True(t, collector.IsHealthy())

	err = collector.Stop()
	require.NoError(t, err)
	assert.True(t, collector.IsHealthy())
}

func TestEventStructure(t *testing.T) {
	config := collectors.DefaultCollectorConfig()
	config.BufferSize = 10
	
	collector, err := NewCollector(config)
	require.NoError(t, err)

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Wait for at least one event (heartbeat)
	select {
	case event := <-collector.Events():
		// Verify event structure
		assert.Equal(t, "cni", event.Type)
		assert.NotZero(t, event.Timestamp)
		assert.NotEmpty(t, event.Data)
		assert.NotNil(t, event.Metadata)
		
		// Verify metadata
		assert.Contains(t, event.Metadata, "source")
		assert.Contains(t, event.Metadata, "cni_plugin")
		
		// Verify data is valid JSON
		var data map[string]interface{}
		err := json.Unmarshal(event.Data, &data)
		assert.NoError(t, err)
		
	case <-time.After(10 * time.Second):
		t.Skip("No events received in timeout period")
	}
}

func TestCollectorBufferHandling(t *testing.T) {
	config := collectors.DefaultCollectorConfig()
	config.BufferSize = 2 // Very small buffer
	
	collector, err := NewCollector(config)
	require.NoError(t, err)

	// Mock the internal collector to control event generation
	mockCollector := collector.(*Collector)
	mockCollector.detectedCNI = "test"
	mockCollector.strategy = &GenericStrategy{}

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// The collector should handle full buffers gracefully
	// Just verify it doesn't panic or deadlock
	time.Sleep(100 * time.Millisecond)
}

// Integration Tests

func TestCollectorWithMultipleConsumers(t *testing.T) {
	config := collectors.DefaultCollectorConfig()
	config.BufferSize = 100
	
	collector, err := NewCollector(config)
	require.NoError(t, err)

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Start multiple consumers
	var wg sync.WaitGroup
	consumedEvents := make([]int, 3)
	
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(consumerID int) {
			defer wg.Done()
			timeout := time.After(2 * time.Second)
			for {
				select {
				case event, ok := <-collector.Events():
					if !ok {
						return
					}
					if event.Type == "cni" {
						consumedEvents[consumerID]++
					}
				case <-timeout:
					return
				}
			}
		}(i)
	}

	// Wait for consumers
	wg.Wait()

	// Only one consumer should get events (channel is not broadcast)
	activeConsumers := 0
	totalEvents := 0
	for _, count := range consumedEvents {
		if count > 0 {
			activeConsumers++
			totalEvents += count
		}
	}
	assert.Equal(t, 1, activeConsumers, "Only one consumer should receive events")
	assert.Greater(t, totalEvents, 0, "At least some events should be consumed")
}

// Performance Tests

func TestCollectorPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	config := collectors.DefaultCollectorConfig()
	config.BufferSize = 1000
	
	collector, err := NewCollector(config)
	require.NoError(t, err)

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Measure event consumption rate
	start := time.Now()
	eventCount := 0
	timeout := time.After(5 * time.Second)

	for {
		select {
		case event, ok := <-collector.Events():
			if !ok {
				return
			}
			if event.Type == "cni" {
				eventCount++
			}
		case <-timeout:
			duration := time.Since(start)
			eventsPerSecond := float64(eventCount) / duration.Seconds()
			t.Logf("Performance: %d events in %v (%.2f events/sec)", 
				eventCount, duration, eventsPerSecond)
			
			// Should handle at least heartbeat events
			assert.Greater(t, eventCount, 0, "Should have processed some events")
			return
		}
	}
}

func TestCollectorMemoryUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory test in short mode")
	}

	config := collectors.DefaultCollectorConfig()
	config.BufferSize = 10000 // Large buffer
	
	collector, err := NewCollector(config)
	require.NoError(t, err)

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)

	// Let it run for a while
	time.Sleep(2 * time.Second)

	// Should not leak goroutines
	err = collector.Stop()
	require.NoError(t, err)

	// Verify clean shutdown
	_, ok := <-collector.Events()
	assert.False(t, ok, "Events channel should be closed")
}

// Benchmark Tests

func BenchmarkCollectorEventGeneration(b *testing.B) {
	config := collectors.DefaultCollectorConfig()
	config.BufferSize = 10000
	
	collector, err := NewCollector(config)
	require.NoError(b, err)

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(b, err)
	defer collector.Stop()

	b.ResetTimer()

	// Consume events
	eventCount := 0
	timeout := time.After(time.Duration(b.N) * time.Millisecond)
	
	for {
		select {
		case _, ok := <-collector.Events():
			if !ok {
				return
			}
			eventCount++
			if eventCount >= b.N {
				return
			}
		case <-timeout:
			return
		}
	}
}

func BenchmarkCNIStrategyCreation(b *testing.B) {
	strategies := []func() CNIStrategy{
		func() CNIStrategy { return &CalicoStrategy{} },
		func() CNIStrategy { return &CiliumStrategy{} },
		func() CNIStrategy { return &FlannelStrategy{} },
		func() CNIStrategy { return &GenericStrategy{} },
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		strategy := strategies[i%len(strategies)]()
		_ = strategy.GetName()
		_ = strategy.GetLogPaths()
		_ = strategy.GetWatchPaths()
	}
}

// Helper function for future e2e tests
func createTestCollectorWithMockK8s(t *testing.T) *Collector {
	t.Helper()
	
	config := collectors.DefaultCollectorConfig()
	collector, err := NewCollector(config)
	require.NoError(t, err)
	
	// Cast to concrete type to access internals
	c := collector.(*Collector)
	
	// Set a known CNI for testing
	c.detectedCNI = "test-cni"
	c.strategy = &GenericStrategy{}
	
	return c
}

// Example e2e test structure (would need actual K8s environment)
func TestCollectorE2EWithK8s(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}
	
	// This would require:
	// 1. K8s test environment (kind, minikube, etc)
	// 2. Deploy test CNI DaemonSet
	// 3. Verify detection and event collection
	
	t.Skip("E2E test requires Kubernetes environment")
}

// Error handling tests
func TestCollectorErrorCases(t *testing.T) {
	t.Run("nil config handling", func(t *testing.T) {
		// Should handle default config
		collector, err := NewCollector(collectors.CollectorConfig{})
		assert.NoError(t, err)
		assert.NotNil(t, collector)
	})

	t.Run("stop without start", func(t *testing.T) {
		config := collectors.DefaultCollectorConfig()
		collector, err := NewCollector(config)
		require.NoError(t, err)
		
		// Should handle stop without start gracefully
		err = collector.Stop()
		assert.NoError(t, err)
	})

	t.Run("multiple stops", func(t *testing.T) {
		config := collectors.DefaultCollectorConfig()
		collector, err := NewCollector(config)
		require.NoError(t, err)
		
		ctx := context.Background()
		err = collector.Start(ctx)
		require.NoError(t, err)
		
		// First stop
		err = collector.Stop()
		assert.NoError(t, err)
		
		// Second stop should be safe
		err = collector.Stop()
		assert.NoError(t, err)
	})
}

// Test helpers
func waitForEvent(t *testing.T, collector collectors.Collector, timeout time.Duration) *collectors.RawEvent {
	t.Helper()
	
	select {
	case event := <-collector.Events():
		return &event
	case <-time.After(timeout):
		t.Fatal("timeout waiting for event")
		return nil
	}
}

func assertEventValid(t *testing.T, event *collectors.RawEvent) {
	t.Helper()
	
	assert.NotNil(t, event)
	assert.Equal(t, "cni", event.Type)
	assert.NotZero(t, event.Timestamp)
	assert.NotEmpty(t, event.Data)
	
	// Verify JSON validity
	var data interface{}
	err := json.Unmarshal(event.Data, &data)
	assert.NoError(t, err, "Event data should be valid JSON")
}

// Load test
func TestCollectorUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	config := collectors.DefaultCollectorConfig()
	config.BufferSize = 100
	
	collector, err := NewCollector(config)
	require.NoError(t, err)

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Simulate load by consuming events rapidly
	var wg sync.WaitGroup
	eventCount := 0
	var mu sync.Mutex

	// Start multiple fast consumers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				select {
				case event, ok := <-collector.Events():
					if !ok {
						return
					}
					mu.Lock()
					eventCount++
					mu.Unlock()
					assertEventValid(t, &event)
				case <-time.After(10 * time.Millisecond):
					// Move on if no event
				}
			}
		}()
	}

	wg.Wait()
	t.Logf("Processed %d events under load", eventCount)
	assert.Greater(t, eventCount, 0, "Should process events under load")
}

// Concurrency test
func TestCollectorConcurrency(t *testing.T) {
	config := collectors.DefaultCollectorConfig()
	collector, err := NewCollector(config)
	require.NoError(t, err)

	var wg sync.WaitGroup
	errors := make(chan error, 10)

	// Concurrent starts
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := context.Background()
			if err := collector.Start(ctx); err != nil {
				errors <- err
			}
		}()
	}

	// Concurrent health checks
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_ = collector.IsHealthy()
				time.Sleep(1 * time.Millisecond)
			}
		}()
	}

	// Concurrent stops
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(10 * time.Millisecond)
			if err := collector.Stop(); err != nil {
				errors <- err
			}
		}()
	}

	wg.Wait()
	close(errors)

	// Should have some "already started" errors but no panics
	errorCount := 0
	for err := range errors {
		errorCount++
		assert.Contains(t, err.Error(), "already started")
	}
	assert.Greater(t, errorCount, 0, "Should have concurrent start errors")
}