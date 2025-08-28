//go:build stress
// +build stress

package etcdmetrics

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.etcd.io/etcd/tests/v3/integration"
)

func TestStress_ConcurrentHealthChecks(t *testing.T) {
	integration.BeforeTestExternal(t)
	cluster := integration.NewClusterV3(t, &integration.ClusterConfig{
		Size: 3,
	})
	defer cluster.Terminate(t)

	cfg := Config{
		Name:                  "stress-concurrent",
		BufferSize:            10000,
		Endpoints:             getClusterEndpoints(cluster),
		HealthCheckInterval:   10 * time.Millisecond, // Very aggressive
		ResponseTimeThreshold: 50 * time.Millisecond,
		DbSizeThreshold:       100 * 1024 * 1024,
	}

	collector, err := NewCollector("stress-concurrent", cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Track metrics
	var eventsProcessed uint64
	var errorsCount uint64

	// Start event consumer
	go func() {
		for {
			select {
			case event := <-collector.Events():
				if event != nil {
					atomic.AddUint64(&eventsProcessed, 1)
					if event.Severity == domain.EventSeverityError {
						atomic.AddUint64(&errorsCount, 1)
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Run for 10 seconds with aggressive health checks
	time.Sleep(10 * time.Second)

	// Should have processed many events
	processed := atomic.LoadUint64(&eventsProcessed)
	errors := atomic.LoadUint64(&errorsCount)

	t.Logf("Processed %d events, %d errors", processed, errors)

	// With 10ms interval over 10 seconds, should have ~1000 health checks
	assert.Greater(t, processed, uint64(500), "Should have processed many events")
	assert.Less(t, errors, processed/2, "Errors should be less than half of events")

	// Collector should still be healthy
	assert.True(t, collector.IsHealthy())
}

func TestStress_EventBufferSaturation(t *testing.T) {
	integration.BeforeTestExternal(t)
	cluster := integration.NewClusterV3(t, &integration.ClusterConfig{
		Size: 1,
	})
	defer cluster.Terminate(t)

	cfg := Config{
		Name:                  "stress-buffer",
		BufferSize:            100, // Small buffer
		Endpoints:             []string{cluster.Members[0].GRPCAddr()},
		HealthCheckInterval:   10 * time.Millisecond, // Generate many events
		ResponseTimeThreshold: 1 * time.Nanosecond,   // Always trigger slow response
		DbSizeThreshold:       1,                     // Always trigger large DB
	}

	collector, err := NewCollector("stress-buffer", cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Don't consume events to cause buffer saturation
	time.Sleep(2 * time.Second)

	// Now start consuming
	var consumed int
	done := time.After(1 * time.Second)
consumeLoop:
	for {
		select {
		case event := <-collector.Events():
			if event != nil {
				consumed++
			}
		case <-done:
			break consumeLoop
		default:
			time.Sleep(1 * time.Millisecond)
		}
	}

	t.Logf("Consumed %d events from saturated buffer", consumed)

	// Should have consumed up to buffer size
	assert.LessOrEqual(t, consumed, 100, "Should not exceed buffer size")

	// Collector should still be healthy despite buffer saturation
	assert.True(t, collector.IsHealthy())
}

func TestStress_MemoryLeaks(t *testing.T) {
	integration.BeforeTestExternal(t)
	cluster := integration.NewClusterV3(t, &integration.ClusterConfig{
		Size: 1,
	})
	defer cluster.Terminate(t)

	// Get initial memory stats
	var initialMem runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&initialMem)

	cfg := Config{
		Name:                  "stress-memory",
		BufferSize:            1000,
		Endpoints:             []string{cluster.Members[0].GRPCAddr()},
		HealthCheckInterval:   50 * time.Millisecond,
		ResponseTimeThreshold: 100 * time.Millisecond,
	}

	// Create and destroy collectors multiple times
	for i := 0; i < 100; i++ {
		collector, err := NewCollector(fmt.Sprintf("stress-memory-%d", i), cfg)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		err = collector.Start(ctx)
		require.NoError(t, err)

		// Consume some events
		done := time.After(50 * time.Millisecond)
	drainLoop:
		for {
			select {
			case <-collector.Events():
				// Drain events
			case <-done:
				break drainLoop
			default:
				time.Sleep(1 * time.Millisecond)
			}
		}

		err = collector.Stop()
		assert.NoError(t, err)
		cancel()
	}

	// Force GC and get final memory stats
	runtime.GC()
	runtime.GC() // Run twice to ensure cleanup
	var finalMem runtime.MemStats
	runtime.ReadMemStats(&finalMem)

	// Memory growth should be reasonable (less than 100MB)
	memGrowth := int64(finalMem.HeapAlloc) - int64(initialMem.HeapAlloc)
	memGrowthMB := memGrowth / (1024 * 1024)

	t.Logf("Memory growth: %d MB", memGrowthMB)
	assert.Less(t, memGrowthMB, int64(100), "Memory growth should be less than 100MB")
}

func TestStress_RapidStartStop(t *testing.T) {
	integration.BeforeTestExternal(t)
	cluster := integration.NewClusterV3(t, &integration.ClusterConfig{
		Size: 1,
	})
	defer cluster.Terminate(t)

	cfg := Config{
		Name:                "stress-startstop",
		BufferSize:          100,
		Endpoints:           []string{cluster.Members[0].GRPCAddr()},
		HealthCheckInterval: 100 * time.Millisecond,
	}

	collector, err := NewCollector("stress-startstop", cfg)
	require.NoError(t, err)

	// Rapidly start and stop
	for i := 0; i < 50; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

		err = collector.Start(ctx)
		require.NoError(t, err)

		// Run briefly
		time.Sleep(10 * time.Millisecond)

		err = collector.Stop()
		require.NoError(t, err)

		cancel()
	}

	// Final start to verify it still works
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	assert.True(t, collector.IsHealthy())
}

func TestStress_ParallelCollectors(t *testing.T) {
	integration.BeforeTestExternal(t)
	cluster := integration.NewClusterV3(t, &integration.ClusterConfig{
		Size: 3,
	})
	defer cluster.Terminate(t)

	const numCollectors = 20
	collectors := make([]*Collector, numCollectors)
	var wg sync.WaitGroup

	// Create and start multiple collectors in parallel
	for i := 0; i < numCollectors; i++ {
		cfg := Config{
			Name:                fmt.Sprintf("stress-parallel-%d", i),
			BufferSize:          500,
			Endpoints:           getClusterEndpoints(cluster),
			HealthCheckInterval: 100 * time.Millisecond,
		}

		collector, err := NewCollector(cfg.Name, cfg)
		require.NoError(t, err)
		collectors[i] = collector

		wg.Add(1)
		go func(c *Collector) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			err := c.Start(ctx)
			assert.NoError(t, err)

			// Consume events
			done := time.After(5 * time.Second)
		consumeLoop:
			for {
				select {
				case <-c.Events():
					// Consume
				case <-done:
					break consumeLoop
				}
			}
		}(collector)
	}

	// Wait for all to run
	wg.Wait()

	// Stop all collectors
	for _, collector := range collectors {
		err := collector.Stop()
		assert.NoError(t, err)
	}
}

func BenchmarkCollector_HealthCheck(b *testing.B) {
	integration.BeforeTestExternal(b)
	cluster := integration.NewClusterV3(b, &integration.ClusterConfig{
		Size: 1,
	})
	defer cluster.Terminate(b)

	cfg := Config{
		Name:                "bench-health",
		BufferSize:          1000,
		Endpoints:           []string{cluster.Members[0].GRPCAddr()},
		HealthCheckInterval: 1 * time.Hour, // Don't auto-trigger
	}

	collector, err := NewCollector("bench-health", cfg)
	require.NoError(b, err)

	ctx := context.Background()
	collector.ctx = ctx

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.performHealthCheck()
	}
}

func BenchmarkCollector_EventProcessing(b *testing.B) {
	collector := &Collector{
		name:   "bench-events",
		events: make(chan *domain.CollectorEvent, 10000),
		ctx:    context.Background(),
		logger: nil, // No logging in benchmark
	}

	events := make([]*domain.CollectorEvent, b.N)
	for i := 0; i < b.N; i++ {
		events[i] = &domain.CollectorEvent{
			EventID:   fmt.Sprintf("event-%d", i),
			Timestamp: time.Now(),
			Type:      domain.EventTypeETCD,
			Source:    "bench",
			Severity:  domain.EventSeverityInfo,
			EventData: domain.EventDataContainer{
				ETCD: &domain.ETCDData{
					Operation: "test",
					Key:       fmt.Sprintf("key-%d", i),
					Value:     fmt.Sprintf("value-%d", i),
				},
			},
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.sendEvent(events[i])
	}
}

func BenchmarkCollector_Concurrent(b *testing.B) {
	integration.BeforeTestExternal(b)
	cluster := integration.NewClusterV3(b, &integration.ClusterConfig{
		Size: 1,
	})
	defer cluster.Terminate(b)

	cfg := Config{
		Name:                "bench-concurrent",
		BufferSize:          10000,
		Endpoints:           []string{cluster.Members[0].GRPCAddr()},
		HealthCheckInterval: 10 * time.Millisecond,
	}

	collector, err := NewCollector("bench-concurrent", cfg)
	require.NoError(b, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(b, err)
	defer collector.Stop()

	// Start consumer
	go func() {
		for {
			select {
			case <-collector.Events():
				// Consume
			case <-ctx.Done():
				return
			}
		}
	}()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			collector.performHealthCheck()
		}
	})
}
