package services

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// BenchmarkObserver_EventProcessing benchmarks event processing throughput
func BenchmarkObserver_EventProcessing(b *testing.B) {
	config := &Config{
		ConnectionTableSize: defaultConnectionTableSize,
		ConnectionTimeout:   defaultConnectionTimeout,
		BufferSize:          10000, // Large buffer for benchmarking
		CleanupInterval:     defaultCleanupInterval,
		EnableK8sMapping:    false,
		Name:                "bench-events",
		HealthCheck:         false,
		EnableOTEL:          false,
		EnableStdout:        false,
	}

	observer, err := NewObserver(config.Name, config, zap.NewNop())
	require.NoError(b, err)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(b, err)
	defer observer.Stop()

	b.ResetTimer()

	// Measure event consumption rate
	for i := 0; i < b.N; i++ {
		select {
		case <-observer.Events():
			// Event consumed
		case <-time.After(100 * time.Millisecond):
			// No event available
		}
	}
}

// BenchmarkObserver_StatisticsCollection benchmarks stats collection overhead
func BenchmarkObserver_StatisticsCollection(b *testing.B) {
	config := &Config{
		ConnectionTableSize: defaultConnectionTableSize,
		ConnectionTimeout:   defaultConnectionTimeout,
		BufferSize:          defaultBufferSize,
		CleanupInterval:     defaultCleanupInterval,
		EnableK8sMapping:    false,
		Name:                "bench-stats",
		HealthCheck:         false,
		EnableOTEL:          false,
		EnableStdout:        false,
	}

	observer, err := NewObserver(config.Name, config, zap.NewNop())
	require.NoError(b, err)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(b, err)
	defer observer.Stop()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = observer.GetStats()
	}
}

// BenchmarkObserver_ServiceMapRetrieval benchmarks service map retrieval
func BenchmarkObserver_ServiceMapRetrieval(b *testing.B) {
	config := &Config{
		ConnectionTableSize: defaultConnectionTableSize,
		ConnectionTimeout:   defaultConnectionTimeout,
		BufferSize:          defaultBufferSize,
		CleanupInterval:     defaultCleanupInterval,
		EnableK8sMapping:    true,
		K8sRefreshInterval:  defaultK8sRefreshInterval,
		PodMappingTimeout:   defaultPodMappingTimeout,
		Name:                "bench-svcmap",
		HealthCheck:         false,
		EnableOTEL:          false,
		EnableStdout:        false,
	}

	observer, err := NewObserver(config.Name, config, zap.NewNop())
	require.NoError(b, err)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(b, err)
	defer observer.Stop()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = observer.GetServiceMap()
	}
}

// BenchmarkConnectionTracker_Processing benchmarks connection tracker performance
func BenchmarkConnectionTracker_Processing(b *testing.B) {
	config := &Config{
		ConnectionTableSize: defaultConnectionTableSize,
		ConnectionTimeout:   defaultConnectionTimeout,
		BufferSize:          10000,
		CleanupInterval:     defaultCleanupInterval,
	}

	tracker := NewConnectionTracker(config, zap.NewNop())
	require.NotNil(b, tracker)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	err := tracker.Start(ctx)
	require.NoError(b, err)
	defer tracker.Stop()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = tracker.GetStats()
	}
}

// TestObserver_MemoryUsage tests memory usage under load
func TestObserver_MemoryUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	config := &Config{
		ConnectionTableSize: defaultConnectionTableSize,
		ConnectionTimeout:   defaultConnectionTimeout,
		BufferSize:          defaultBufferSize,
		CleanupInterval:     defaultCleanupInterval,
		EnableK8sMapping:    false,
		Name:                "mem-test",
		HealthCheck:         true,
		EnableOTEL:          false,
		EnableStdout:        false,
	}

	observer, err := NewObserver(config.Name, config, zap.NewNop())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Run for a period to check for memory leaks
	for i := 0; i < 1000; i++ {
		select {
		case <-observer.Events():
			// Consume event
		case <-time.After(10 * time.Millisecond):
			// Continue
		}
	}

	// Memory should remain stable
	// Note: Actual memory profiling would require additional tooling
}

// TestObserver_Throughput tests event throughput under load
func TestObserver_Throughput(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	config := &Config{
		ConnectionTableSize: defaultConnectionTableSize,
		ConnectionTimeout:   defaultConnectionTimeout,
		BufferSize:          10000,
		CleanupInterval:     defaultCleanupInterval,
		EnableK8sMapping:    false,
		Name:                "throughput-test",
		HealthCheck:         true,
		EnableOTEL:          false,
		EnableStdout:        false,
	}

	observer, err := NewObserver(config.Name, config, zap.NewNop())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Measure throughput
	eventCount := 0
	start := time.Now()
	timeout := time.After(10 * time.Second)

throughputLoop:
	for {
		select {
		case <-observer.Events():
			eventCount++
		case <-timeout:
			break throughputLoop
		}
	}

	duration := time.Since(start)
	eventsPerSecond := float64(eventCount) / duration.Seconds()

	t.Logf("Processed %d events in %v (%.2f events/sec)", eventCount, duration, eventsPerSecond)
}
