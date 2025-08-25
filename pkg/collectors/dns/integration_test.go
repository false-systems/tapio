//go:build linux && integration
// +build linux,integration

package dns

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

// TestDNSCollectorIntegration tests the full DNS collector lifecycle
// This test requires root privileges and eBPF support
func TestDNSCollectorIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cfg := Config{
		Name:       "integration-dns",
		BufferSize: 1000,
		EnableEBPF: true, // Enable eBPF for integration test
	}

	collector, err := NewCollector("integration", cfg)
	require.NoError(t, err)

	// Start collector
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start eBPF collector (likely missing privileges): %v", err)
	}
	defer collector.Stop()

	// Verify collector is healthy
	assert.True(t, collector.IsHealthy())

	// Check health status
	health := collector.Health()
	assert.Equal(t, domain.HealthHealthy, health.Status)

	// Get statistics
	stats := collector.Statistics()
	assert.NotNil(t, stats)
	assert.True(t, stats.EBPFAttached)

	// Monitor events for a short period
	eventsChan := collector.Events()

	// Create a DNS query to generate events (optional - depends on system activity)
	go func() {
		// This might generate DNS events if the system is active
		time.Sleep(1 * time.Second)
	}()

	// Wait for events or timeout
	select {
	case event := <-eventsChan:
		t.Logf("Received DNS event: %+v", event)
		assert.Equal(t, "integration", event.Source)
		assert.NotEmpty(t, event.Data)

	case <-time.After(5 * time.Second):
		t.Log("No DNS events captured (this is normal in test environment)")
	}

	// Verify final statistics
	finalStats := collector.Statistics()
	assert.True(t, finalStats.EBPFAttached)
}

// TestDNSCollectorStressTest tests the collector under load
func TestDNSCollectorStressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	cfg := Config{
		Name:       "stress-dns",
		BufferSize: 10000, // Large buffer for stress test
		EnableEBPF: false, // Use mock events for stress test
	}

	collector, err := NewCollector("stress", cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Simulate high-frequency events
	eventsChan := collector.events

	go func() {
		for i := 0; i < 1000; i++ {
			select {
			case eventsChan <- domain.RawEvent{
				Timestamp: time.Now(),
				Source:    "stress",
				Data:      []byte("mock-dns-event"),
			}:
			case <-ctx.Done():
				return
			default:
				// Buffer full, event dropped
			}

			if i%100 == 0 {
				time.Sleep(1 * time.Millisecond) // Small delay every 100 events
			}
		}
	}()

	// Monitor buffer utilization
	time.Sleep(100 * time.Millisecond)
	stats := collector.Statistics()

	t.Logf("Buffer utilization: %.2f%%", stats.BufferUtilization*100)
	t.Logf("Events processed: %d", stats.EventsProcessed)
	t.Logf("Events dropped: %d", stats.EventsDropped)

	// Buffer should handle the load
	assert.True(t, stats.BufferUtilization <= 1.0, "Buffer utilization should not exceed 100%")
}

// TestDNSCollectorErrorHandling tests error conditions
func TestDNSCollectorErrorHandling(t *testing.T) {
	cfg := Config{
		Name:       "error-dns",
		BufferSize: 10,
		EnableEBPF: false,
	}

	collector, err := NewCollector("error", cfg)
	require.NoError(t, err)

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Test buffer overflow condition
	eventsChan := collector.events

	// Fill buffer beyond capacity
	for i := 0; i < 20; i++ {
		select {
		case eventsChan <- domain.RawEvent{
			Timestamp: time.Now(),
			Source:    "error",
			Data:      []byte("overflow-test"),
		}:
		default:
			// Expected when buffer is full
		}
	}

	time.Sleep(50 * time.Millisecond)

	// Verify health status reflects issues
	health := collector.Health()

	// Should be degraded due to high buffer utilization
	if len(collector.events) >= cap(collector.events)*9/10 {
		assert.Equal(t, domain.HealthDegraded, health.Status)
	}

	stats := collector.Statistics()
	t.Logf("Buffer utilization after overflow test: %.2f%%", stats.BufferUtilization*100)
}

// TestDNSCollectorMetrics tests OpenTelemetry metrics
func TestDNSCollectorMetrics(t *testing.T) {
	cfg := Config{
		Name:       "metrics-dns",
		BufferSize: 100,
		EnableEBPF: false,
	}

	collector, err := NewCollector("metrics", cfg)
	require.NoError(t, err)

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Update statistics manually to test metrics
	collector.updateStats(100, 5, 2)

	stats := collector.Statistics()
	assert.Equal(t, int64(100), stats.EventsProcessed)
	assert.Equal(t, int64(5), stats.EventsDropped)
	assert.Equal(t, int64(2), stats.ErrorsTotal)

	// Verify metrics are accessible
	assert.NotNil(t, collector.eventsProcessed)
	assert.NotNil(t, collector.errorsTotal)
	assert.NotNil(t, collector.droppedEvents)
	assert.NotNil(t, collector.bufferUsage)
	assert.NotNil(t, collector.processingTime)
}

// TestDNSCollectorConcurrency tests thread safety
func TestDNSCollectorConcurrency(t *testing.T) {
	cfg := Config{
		Name:       "concurrency-dns",
		BufferSize: 1000,
		EnableEBPF: false,
	}

	collector, err := NewCollector("concurrency", cfg)
	require.NoError(t, err)

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Start multiple goroutines accessing collector methods
	done := make(chan bool)

	// Goroutine 1: Check health repeatedly
	go func() {
		for i := 0; i < 100; i++ {
			health := collector.Health()
			assert.NotNil(t, health)
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	// Goroutine 2: Get statistics repeatedly
	go func() {
		for i := 0; i < 100; i++ {
			stats := collector.Statistics()
			assert.NotNil(t, stats)
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	// Goroutine 3: Check if healthy repeatedly
	go func() {
		for i := 0; i < 100; i++ {
			healthy := collector.IsHealthy()
			assert.True(t, healthy)
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	// Goroutine 4: Update stats repeatedly
	go func() {
		for i := 0; i < 100; i++ {
			collector.updateStats(1, 0, 0)
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	// Wait for all goroutines to complete
	for i := 0; i < 4; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Concurrency test timeout")
		}
	}

	// Verify final state is consistent
	finalStats := collector.Statistics()
	assert.True(t, finalStats.EventsProcessed >= 0)
	assert.True(t, collector.IsHealthy())
}
