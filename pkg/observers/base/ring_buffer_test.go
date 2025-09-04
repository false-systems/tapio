package base

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

// Test consumer that counts events
type testConsumer struct {
	name     string
	priority int
	count    atomic.Int64
	filter   func(*domain.CollectorEvent) bool
}

func (t *testConsumer) ConsumeEvent(ctx context.Context, event *domain.CollectorEvent) error {
	t.count.Add(1)
	return nil
}

func (t *testConsumer) Priority() int {
	return t.priority
}

func (t *testConsumer) Name() string {
	return t.name
}

func (t *testConsumer) ShouldConsume(event *domain.CollectorEvent) bool {
	if t.filter != nil {
		return t.filter(event)
	}
	return true
}

func TestRingBuffer_BasicOperations(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()

	rb, err := NewRingBuffer(RingBufferConfig{
		Size:          128, // Small for testing
		BatchSize:     4,
		BatchTimeout:  10 * time.Millisecond,
		Logger:        logger.Desugar(),
		CollectorName: "test",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rb.Start(ctx)
	defer rb.Stop()

	// Write some events
	for i := 0; i < 10; i++ {
		event := &domain.CollectorEvent{
			EventID:   fmt.Sprintf("test-%d", i),
			Timestamp: time.Now(),
			Type:      domain.EventTypeKernelNetwork,
			Source:    "test",
		}
		assert.True(t, rb.Write(event))
	}

	// Give time to process
	time.Sleep(50 * time.Millisecond)

	stats := rb.Statistics()
	assert.Equal(t, uint64(10), stats.Produced)
	assert.Equal(t, uint64(10), stats.Consumed)
}

func TestRingBuffer_LocalConsumers(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()

	rb, err := NewRingBuffer(RingBufferConfig{
		Size:          256,
		BatchSize:     8,
		BatchTimeout:  5 * time.Millisecond,
		Logger:        logger.Desugar(),
		CollectorName: "test",
	})
	require.NoError(t, err)

	// Add consumers
	highPriority := &testConsumer{
		name:     "high",
		priority: 100,
		filter: func(e *domain.CollectorEvent) bool {
			return e.Severity == domain.EventSeverityCritical
		},
	}

	lowPriority := &testConsumer{
		name:     "low",
		priority: 10,
	}

	rb.RegisterLocalConsumer(highPriority)
	rb.RegisterLocalConsumer(lowPriority)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rb.Start(ctx)
	defer rb.Stop()

	// Write events
	for i := 0; i < 5; i++ {
		event := &domain.CollectorEvent{
			EventID:   fmt.Sprintf("normal-%d", i),
			Timestamp: time.Now(),
			Type:      domain.EventTypeKernelNetwork,
			Source:    "test",
			Severity:  domain.EventSeverityInfo,
		}
		rb.Write(event)
	}

	for i := 0; i < 3; i++ {
		event := &domain.CollectorEvent{
			EventID:   fmt.Sprintf("critical-%d", i),
			Timestamp: time.Now(),
			Type:      domain.EventTypeKernelNetwork,
			Source:    "test",
			Severity:  domain.EventSeverityCritical,
		}
		rb.Write(event)
	}

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// High priority should only see critical events
	assert.Equal(t, int64(3), highPriority.count.Load())
	// Low priority sees all events
	assert.Equal(t, int64(8), lowPriority.count.Load())
}

func TestRingBuffer_Overflow(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()

	rb, err := NewRingBuffer(RingBufferConfig{
		Size:          16, // Very small
		BatchSize:     4,
		BatchTimeout:  50 * time.Millisecond,
		Logger:        logger.Desugar(),
		CollectorName: "test",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rb.Start(ctx)
	defer rb.Stop()

	// Write more events than capacity
	for i := 0; i < 100; i++ {
		event := &domain.CollectorEvent{
			EventID:   fmt.Sprintf("test-%d", i),
			Timestamp: time.Now(),
			Type:      domain.EventTypeKernelNetwork,
			Source:    "test",
		}
		rb.Write(event)
	}

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	stats := rb.Statistics()
	assert.Equal(t, uint64(100), stats.Produced)
	// Some events will be dropped due to overflow
	assert.Greater(t, stats.Dropped, uint64(0))
}

func TestBaseCollector_WithRingBuffer(t *testing.T) {
	// Test BaseCollector with ring buffer enabled
	bc := NewBaseObserverWithConfig(BaseObserverConfig{
		Name:               "test-collector",
		HealthCheckTimeout: 30 * time.Second,
		EnableRingBuffer:   true,
		RingBufferSize:     256,
		BatchSize:          16,
		BatchTimeout:       5 * time.Millisecond,
	})

	assert.True(t, bc.IsRingBufferEnabled())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bc.StartRingBuffer(ctx)
	defer bc.StopRingBuffer()

	// Add a local consumer
	consumer := &testConsumer{name: "test", priority: 50}
	err := bc.RegisterLocalConsumer(consumer)
	require.NoError(t, err)

	// Write events through ring buffer
	for i := 0; i < 10; i++ {
		event := &domain.CollectorEvent{
			EventID:   fmt.Sprintf("test-%d", i),
			Timestamp: time.Now(),
			Type:      domain.EventTypeKernelNetwork,
			Source:    "test",
		}
		assert.True(t, bc.WriteToRingBuffer(event))
	}

	// Wait for processing
	time.Sleep(50 * time.Millisecond)

	// Check stats
	stats := bc.Statistics()
	assert.Equal(t, int64(10), stats.EventsProcessed)
	assert.Contains(t, stats.CustomMetrics, "ring_buffer_capacity")
	assert.Contains(t, stats.CustomMetrics, "ring_buffer_produced")
	assert.Equal(t, "10", stats.CustomMetrics["ring_buffer_produced"])
}

func TestBaseCollector_WithoutRingBuffer(t *testing.T) {
	// Test BaseCollector without ring buffer (backward compatibility)
	bc := NewBaseObserver("test-collector", 30*time.Second)

	assert.False(t, bc.IsRingBufferEnabled())

	// Try to register consumer - should fail
	consumer := &testConsumer{name: "test", priority: 50}
	err := bc.RegisterLocalConsumer(consumer)
	assert.Error(t, err)

	// Write to ring buffer should return false
	event := &domain.CollectorEvent{
		EventID:   "test-1",
		Timestamp: time.Now(),
		Type:      domain.EventTypeKernelNetwork,
		Source:    "test",
	}
	assert.False(t, bc.WriteToRingBuffer(event))

	// Stats should not include ring buffer metrics
	stats := bc.Statistics()
	assert.NotContains(t, stats.CustomMetrics, "ring_buffer_capacity")
}

func BenchmarkRingBuffer_Write(b *testing.B) {
	rb, _ := NewRingBuffer(RingBufferConfig{
		Size:      8192,
		BatchSize: 64,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rb.Start(ctx)
	defer rb.Stop()

	event := &domain.CollectorEvent{
		EventID:   "bench",
		Timestamp: time.Now(),
		Type:      domain.EventTypeKernelNetwork,
		Source:    "bench",
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			rb.Write(event)
		}
	})
}
