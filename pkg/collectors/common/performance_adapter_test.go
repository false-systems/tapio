package common

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestNewPerformanceAdapter(t *testing.T) {
	tests := []struct {
		name    string
		config  PerformanceConfig
		wantErr bool
	}{
		{
			name:    "valid config",
			config:  DefaultPerformanceConfig("test"),
			wantErr: false,
		},
		{
			name: "invalid buffer size (not power of 2)",
			config: PerformanceConfig{
				CollectorName: "test",
				BufferSize:    1000, // Not power of 2
				BatchSize:     100,
				BatchTimeout:  100 * time.Millisecond,
			},
			wantErr: true,
		},
		{
			name: "valid custom buffer size",
			config: PerformanceConfig{
				CollectorName: "test",
				BufferSize:    4096, // Power of 2
				BatchSize:     50,
				BatchTimeout:  50 * time.Millisecond,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter, err := NewPerformanceAdapter(tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, adapter)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, adapter)
			}
		})
	}
}

func TestPerformanceAdapterLifecycle(t *testing.T) {
	config := DefaultPerformanceConfig("test")
	adapter, err := NewPerformanceAdapter(config)
	require.NoError(t, err)

	// Test Start
	err = adapter.Start()
	assert.NoError(t, err)

	// Test double start
	err = adapter.Start()
	assert.Error(t, err)

	// Test Stop
	err = adapter.Stop()
	assert.NoError(t, err)

	// Test double stop
	err = adapter.Stop()
	assert.NoError(t, err)

	// Test submit after stop
	event := &domain.UnifiedEvent{ID: "test-event"}
	err = adapter.Submit(event)
	assert.Error(t, err)
}

func TestPerformanceAdapterSubmit(t *testing.T) {
	config := DefaultPerformanceConfig("test")
	config.BufferSize = 16 // Small buffer for testing
	config.BatchSize = 4
	config.BatchTimeout = 50 * time.Millisecond

	adapter, err := NewPerformanceAdapter(config)
	require.NoError(t, err)

	err = adapter.Start()
	require.NoError(t, err)
	defer adapter.Stop()

	// Submit events
	var submitted atomic.Int32
	var wg sync.WaitGroup

	// Consumer
	wg.Add(1)
	go func() {
		defer wg.Done()
		timeout := time.After(2 * time.Second)
		for {
			select {
			case event, ok := <-adapter.Events():
				if !ok {
					return
				}
				assert.NotEmpty(t, event.ID)
			case <-timeout:
				return
			}
		}
	}()

	// Submit some events
	for i := 0; i < 10; i++ {
		event := &domain.UnifiedEvent{
			ID:        fmt.Sprintf("event-%d", i),
			Timestamp: time.Now(),
		}
		err := adapter.Submit(event)
		if err == nil {
			submitted.Add(1)
		}
	}

	// Wait a bit for processing
	time.Sleep(200 * time.Millisecond)

	// Check metrics
	metrics := adapter.GetMetrics()
	assert.Greater(t, metrics.EventsProcessed, uint64(0))
	assert.LessOrEqual(t, metrics.EventsProcessed, uint64(submitted.Load()))

	adapter.Stop()
	wg.Wait()
}

func TestPerformanceAdapterBatching(t *testing.T) {
	config := DefaultPerformanceConfig("test")
	config.BufferSize = 64
	config.BatchSize = 5
	config.BatchTimeout = 100 * time.Millisecond
	config.EnableBatching = true

	adapter, err := NewPerformanceAdapter(config)
	require.NoError(t, err)

	err = adapter.Start()
	require.NoError(t, err)
	defer adapter.Stop()

	// Track received events
	var received atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	go func() {
		for {
			select {
			case <-adapter.Events():
				received.Add(1)
			case <-ctx.Done():
				return
			}
		}
	}()

	// Submit exactly one batch worth of events
	for i := 0; i < config.BatchSize; i++ {
		event := &domain.UnifiedEvent{
			ID: fmt.Sprintf("batch-event-%d", i),
		}
		err := adapter.Submit(event)
		require.NoError(t, err)
	}

	// Wait for batch processing
	time.Sleep(200 * time.Millisecond)

	// Should have processed one batch
	metrics := adapter.GetMetrics()
	assert.Equal(t, uint64(1), metrics.BatchesProcessed)
	assert.Equal(t, uint64(config.BatchSize), metrics.EventsProcessed)
}

func TestPerformanceAdapterBufferOverflow(t *testing.T) {
	config := DefaultPerformanceConfig("test")
	config.BufferSize = 8 // Very small buffer
	config.BatchSize = 4
	config.BatchTimeout = 1 * time.Second // Long timeout to ensure buffer fills

	adapter, err := NewPerformanceAdapter(config)
	require.NoError(t, err)

	err = adapter.Start()
	require.NoError(t, err)
	defer adapter.Stop()

	// Submit more events than buffer can hold
	var dropCount int
	for i := 0; i < 20; i++ {
		event := &domain.UnifiedEvent{
			ID: fmt.Sprintf("overflow-event-%d", i),
		}
		err := adapter.Submit(event)
		if err != nil {
			dropCount++
		}
	}

	// Some events should have been dropped
	assert.Greater(t, dropCount, 0)

	// Check metrics
	metrics := adapter.GetMetrics()
	assert.Greater(t, metrics.EventsDropped, uint64(0))
}

func TestPerformanceAdapterZeroCopy(t *testing.T) {
	config := DefaultPerformanceConfig("test")
	config.EnableZeroCopy = true
	config.EventPoolSize = 100

	adapter, err := NewPerformanceAdapter(config)
	require.NoError(t, err)

	err = adapter.Start()
	require.NoError(t, err)
	defer adapter.Stop()

	// Submit and consume events
	go func() {
		for range adapter.Events() {
			// Consume events
		}
	}()

	// Submit many events
	for i := 0; i < 50; i++ {
		event := &domain.UnifiedEvent{
			ID: fmt.Sprintf("zerocopy-event-%d", i),
		}
		adapter.Submit(event)
	}

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// Check pool metrics
	metrics := adapter.GetMetrics()
	assert.Greater(t, metrics.PoolRecycled, uint64(0)) // Some events should be recycled
}

func TestPerformanceAdapterConcurrentSubmit(t *testing.T) {
	config := DefaultPerformanceConfig("test")
	config.BufferSize = 1024

	adapter, err := NewPerformanceAdapter(config)
	require.NoError(t, err)

	err = adapter.Start()
	require.NoError(t, err)
	defer adapter.Stop()

	// Concurrent submitters
	var wg sync.WaitGroup
	numGoroutines := 10
	eventsPerGoroutine := 100

	// Consumer
	var received atomic.Int32
	go func() {
		for range adapter.Events() {
			received.Add(1)
		}
	}()

	// Submit events concurrently
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < eventsPerGoroutine; i++ {
				event := &domain.UnifiedEvent{
					ID: fmt.Sprintf("concurrent-%d-%d", goroutineID, i),
				}
				adapter.Submit(event)
			}
		}(g)
	}

	wg.Wait()
	time.Sleep(500 * time.Millisecond) // Wait for processing

	// Check that we processed events
	metrics := adapter.GetMetrics()
	assert.Greater(t, metrics.EventsProcessed, uint64(0))

	// Most events should be processed (allow for some drops due to timing)
	totalExpected := numGoroutines * eventsPerGoroutine
	assert.Greater(t, int(metrics.EventsProcessed), totalExpected/2)
}

func TestDefaultPerformanceConfig(t *testing.T) {
	collectors := []string{"ebpf", "k8s", "cni", "systemd"}

	for _, collector := range collectors {
		config := DefaultPerformanceConfig(collector)

		assert.Equal(t, collector, config.CollectorName)
		assert.Equal(t, uint64(8192), config.BufferSize)
		assert.Equal(t, 100, config.BatchSize)
		assert.Equal(t, 100*time.Millisecond, config.BatchTimeout)
		assert.Equal(t, 10000, config.EventPoolSize)
		assert.Equal(t, 5000, config.BytePoolSize)
		assert.True(t, config.EnableZeroCopy)
		assert.True(t, config.EnableBatching)
		assert.Equal(t, 30*time.Second, config.MetricsInterval)

		// Verify buffer size is power of 2
		assert.Equal(t, uint64(0), config.BufferSize&(config.BufferSize-1))
	}
}

func TestPerformanceAdapterMetrics(t *testing.T) {
	config := DefaultPerformanceConfig("test")
	adapter, err := NewPerformanceAdapter(config)
	require.NoError(t, err)

	err = adapter.Start()
	require.NoError(t, err)
	defer adapter.Stop()

	// Submit some events
	for i := 0; i < 10; i++ {
		event := &domain.UnifiedEvent{
			ID: fmt.Sprintf("metric-event-%d", i),
		}
		adapter.Submit(event)
	}

	// Consume events
	go func() {
		for range adapter.Events() {
			// Consume
		}
	}()

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// Get metrics
	metrics := adapter.GetMetrics()

	assert.NotNil(t, metrics)
	assert.Greater(t, metrics.BufferCapacity, uint64(0))
	assert.GreaterOrEqual(t, metrics.BufferUtilization, float64(0))
	assert.LessOrEqual(t, metrics.BufferUtilization, float64(1))
}
