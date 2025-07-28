package internal

import (
	"context"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/ebpf/core"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestCollector_Integration_PerformanceAdapter(t *testing.T) {
	config := core.Config{
		Name:               "test-ebpf",
		Enabled:            true,
		EventBufferSize:    1024, // Must be power of 2
		MaxEventsPerSecond: 10000,
		EnableNetwork:      true,
		EnableMemory:       true,
		EnableProcess:      true,
	}

	collector, err := NewCollector(config)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start collector
	err = collector.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}
	defer collector.Stop()

	// Verify collector is healthy
	health := collector.Health()
	if health.Status != core.HealthStatusHealthy {
		t.Errorf("Collector health status = %v, want %v", health.Status, core.HealthStatusHealthy)
	}

	// Wait for events to be generated (dummy events in non-Linux)
	time.Sleep(100 * time.Millisecond)

	// Check statistics
	stats := collector.Statistics()
	if stats.StartTime.IsZero() {
		t.Error("Statistics StartTime is zero")
	}

	// Verify PerformanceAdapter is working
	// Verify PerformanceAdapter is working by checking health metrics
	healthMetrics := health.Metrics
	if _, exists := healthMetrics["perf_buffer_utilization"]; !exists {
		t.Error("Performance adapter metrics not included in health")
	}

	// Performance metrics are included in health metrics
	t.Logf("Health metrics: %+v", healthMetrics)
}

func TestCollector_Integration_EventFlow(t *testing.T) {
	config := core.Config{
		Name:               "test-event-flow",
		Enabled:            true,
		EventBufferSize:    128, // Must be power of 2
		MaxEventsPerSecond: 1000,
	}

	collector, err := NewCollector(config)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start collector
	err = collector.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}
	defer collector.Stop()

	// Create event consumer
	eventCount := 0
	done := make(chan bool)

	go func() {
		timer := time.NewTimer(2 * time.Second)
		for {
			select {
			case event := <-collector.Events():
				eventCount++
				// Verify event structure
				if event.ID == "" {
					t.Error("Received event with empty ID")
				}
				if event.Source != string(domain.SourceEBPF) {
					t.Errorf("Event source = %v, want %v", event.Source, domain.SourceEBPF)
				}
			case <-timer.C:
				done <- true
				return
			}
		}
	}()

	// Wait for event collection
	<-done

	// Verify events were collected
	if eventCount == 0 {
		t.Error("No events collected")
	}

	t.Logf("Collected %d events in 2 seconds", eventCount)

	// Check final statistics
	stats := collector.Statistics()
	if stats.EventsCollected == 0 {
		t.Error("Statistics show no events collected")
	}
}

func TestCollector_Integration_ProductionHardening(t *testing.T) {
	config := core.Config{
		Name:               "test-hardening",
		Enabled:            true,
		EventBufferSize:    128, // Must be power of 2
		MaxEventsPerSecond: 10,  // Low rate limit for testing
	}

	collector, err := NewCollector(config)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start collector
	err = collector.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}
	defer collector.Stop()

	// Test production hardening features through health metrics
	// The internal implementation is not directly accessible in all environments

	// Get health metrics
	health := collector.Health()

	// Verify rate limiting metrics exist
	if _, exists := health.Metrics["rate_limit_allowed"]; !exists {
		t.Error("Rate limiter metrics not found in health")
	}

	// Verify circuit breaker metrics exist
	if _, exists := health.Metrics["circuit_breaker_requests"]; !exists {
		t.Error("Circuit breaker metrics not found in health")
	}

	// Verify validator metrics exist
	if _, exists := health.Metrics["events_validated"]; !exists {
		t.Error("Event validator metrics not found in health")
	}

	// Verify backpressure metrics exist
	if _, exists := health.Metrics["backpressure_accepted"]; !exists {
		t.Error("Backpressure controller metrics not found in health")
	}

	t.Logf("Production hardening features verified through metrics")
}

func TestCollector_Integration_Metrics(t *testing.T) {
	config := core.Config{
		Name:               "test-metrics",
		Enabled:            true,
		EventBufferSize:    1024, // Must be power of 2
		MaxEventsPerSecond: 10000,
	}

	collector, err := NewCollector(config)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start collector
	err = collector.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}
	defer collector.Stop()

	// Let it run and collect events
	time.Sleep(500 * time.Millisecond)

	// Get comprehensive health metrics
	health := collector.Health()
	metrics := health.Metrics

	// Verify all metric categories are present
	metricCategories := []string{
		// PerformanceAdapter metrics
		"perf_events_processed",
		"perf_buffer_utilization",
		"perf_batches_processed",

		// Rate limiter metrics
		"rate_limit_allowed",
		"rate_limit_rejected",
		"rate_limit_utilization",

		// Circuit breaker metrics
		"circuit_breaker_requests",
		"circuit_breaker_failures",

		// Validator metrics
		"events_validated",
		"events_invalid",

		// Backpressure metrics
		"backpressure_accepted",
		"backpressure_shed",
		"backpressure_shed_rate",

		// General metrics
		"buffer_utilization",
		"events_per_second",
	}

	for _, category := range metricCategories {
		if _, exists := metrics[category]; !exists {
			t.Errorf("Missing metric category: %s", category)
		}
	}

	// Log all metrics for inspection
	t.Logf("Collector metrics:")
	for k, v := range metrics {
		t.Logf("  %s: %v", k, v)
	}
}

func TestCollector_Integration_Lifecycle(t *testing.T) {
	config := core.Config{
		Name:               "test-lifecycle",
		Enabled:            true,
		EventBufferSize:    128, // Must be power of 2
		MaxEventsPerSecond: 1000,
	}

	// Test multiple start/stop cycles
	for i := 0; i < 3; i++ {
		t.Run(string(rune(i)), func(t *testing.T) {
			collector, err := NewCollector(config)
			if err != nil {
				t.Fatalf("Failed to create collector: %v", err)
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Start
			err = collector.Start(ctx)
			if err != nil {
				t.Fatalf("Failed to start collector: %v", err)
			}

			// Verify it's running
			health := collector.Health()
			if health.Status != core.HealthStatusHealthy {
				t.Error("Collector not healthy after start")
			}

			// Stop
			err = collector.Stop()
			if err != nil {
				t.Errorf("Failed to stop collector: %v", err)
			}

			// Try to start again (should fail)
			err = collector.Start(ctx)
			if err == nil {
				t.Error("Should not be able to start stopped collector")
			}
		})
	}
}

func TestCollector_Integration_Configuration(t *testing.T) {
	// Test configuration validation
	invalidConfigs := []struct {
		name   string
		config core.Config
	}{
		{
			name: "disabled collector",
			config: core.Config{
				Name:    "disabled",
				Enabled: false,
			},
		},
		{
			name: "invalid buffer size",
			config: core.Config{
				Name:            "invalid-buffer",
				Enabled:         true,
				EventBufferSize: -1,
			},
		},
	}

	for _, tt := range invalidConfigs {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewCollector(tt.config)
			if err != nil {
				// Expected for some invalid configs
				return
			}

			ctx := context.Background()
			err = collector.Start(ctx)
			if err == nil {
				t.Error("Should fail to start with invalid config")
			}
		})
	}

	// Test runtime configuration
	t.Run("RuntimeConfig", func(t *testing.T) {
		config := core.Config{
			Name:               "test-reconfig",
			Enabled:            true,
			EventBufferSize:    128, // Must be power of 2
			MaxEventsPerSecond: 1000,
		}

		collector, err := NewCollector(config)
		if err != nil {
			t.Fatalf("Failed to create collector: %v", err)
		}

		// Update configuration
		newConfig := config
		newConfig.MaxEventsPerSecond = 5000

		err = collector.Configure(newConfig)
		if err != nil {
			t.Errorf("Failed to update configuration: %v", err)
		}
	})
}

// Benchmark tests
func BenchmarkCollector_EventProcessing(b *testing.B) {
	config := core.Config{
		Name:               "bench-collector",
		Enabled:            true,
		EventBufferSize:    10000,
		MaxEventsPerSecond: 100000,
	}

	collector, err := NewCollector(config)
	if err != nil {
		b.Fatalf("Failed to create collector: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = collector.Start(ctx)
	if err != nil {
		b.Fatalf("Failed to start collector: %v", err)
	}
	defer collector.Stop()

	// Create event consumer
	go func() {
		for range collector.Events() {
			// Consume events
		}
	}()

	// Let the system stabilize
	time.Sleep(100 * time.Millisecond)

	// Reset timer after setup
	b.ResetTimer()

	// Run for benchmark duration
	<-time.After(time.Duration(b.N) * time.Microsecond)

	stats := collector.Statistics()
	b.ReportMetric(float64(stats.EventsCollected)/b.Elapsed().Seconds(), "events/sec")
	b.ReportMetric(float64(stats.BytesProcessed)/b.Elapsed().Seconds()/1024/1024, "MB/sec")
}

func BenchmarkCollector_WithPerformanceAdapter(b *testing.B) {
	config := core.Config{
		Name:               "bench-perf-adapter",
		Enabled:            true,
		EventBufferSize:    8192, // Power of 2 for PerformanceAdapter
		MaxEventsPerSecond: 0,    // No rate limit
	}

	collector, err := NewCollector(config)
	if err != nil {
		b.Fatalf("Failed to create collector: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = collector.Start(ctx)
	if err != nil {
		b.Fatalf("Failed to start collector: %v", err)
	}
	defer collector.Stop()

	// Warm up
	time.Sleep(100 * time.Millisecond)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// Get health metrics which include performance metrics
			health := collector.Health()
			metrics := health.Metrics
			_ = metrics
		}
	})
}
