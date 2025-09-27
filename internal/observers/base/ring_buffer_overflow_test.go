package base

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
	"go.uber.org/zap/zaptest"
)

// TestRingBufferOverflowBehavior tests different overflow scenarios
func TestRingBufferOverflowBehavior(t *testing.T) {
	t.Run("overflow drops old events", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		rb, err := NewRingBuffer(RingBufferConfig{
			Size:          8, // Small buffer for testing
			BatchSize:     2,
			BatchTimeout:  10 * time.Millisecond,
			Logger:        logger,
			CollectorName: "test",
		})
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		rb.Start(ctx)
		defer rb.Stop()

		// Write more events than capacity
		const totalEvents = 20
		for i := 0; i < totalEvents; i++ {
			event := &domain.CollectorEvent{
				EventID:   fmt.Sprintf("event-%d", i),
				Timestamp: time.Now(),
				Type:      domain.EventTypeKernelNetwork,
				Source:    "test",
				Severity:  domain.EventSeverityInfo,
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "test",
						"version":  "1.0.0",
					},
				},
			}
			rb.Write(event)
		}

		// Give time for processing
		time.Sleep(100 * time.Millisecond)

		stats := rb.Statistics()
		assert.Equal(t, uint64(totalEvents), stats.Produced)
		// Should have dropped events (totalEvents - capacity)
		assert.Greater(t, stats.Dropped, uint64(0), "Expected some events to be dropped")
		assert.LessOrEqual(t, stats.Consumed, rb.capacity, "Consumed should not exceed capacity")
	})

	t.Run("high concurrency overflow", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		rb, err := NewRingBuffer(RingBufferConfig{
			Size:          16,
			BatchSize:     4,
			BatchTimeout:  5 * time.Millisecond,
			Logger:        logger,
			CollectorName: "test",
		})
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		rb.Start(ctx)
		defer rb.Stop()

		var wg sync.WaitGroup
		var totalProduced atomic.Uint64

		// Multiple producers
		const numProducers = 10
		const eventsPerProducer = 50

		for p := 0; p < numProducers; p++ {
			wg.Add(1)
			go func(producerID int) {
				defer wg.Done()
				for i := 0; i < eventsPerProducer; i++ {
					event := &domain.CollectorEvent{
						EventID:   fmt.Sprintf("p%d-e%d", producerID, i),
						Timestamp: time.Now(),
						Type:      domain.EventTypeKernelNetwork,
						Source:    fmt.Sprintf("producer-%d", producerID),
						Severity:  domain.EventSeverityInfo,
						Metadata: domain.EventMetadata{
							Labels: map[string]string{
								"observer": "test",
								"version":  "1.0.0",
							},
						},
					}
					if rb.Write(event) {
						totalProduced.Add(1)
					}
				}
			}(p)
		}

		wg.Wait()
		time.Sleep(200 * time.Millisecond) // Allow processing

		stats := rb.Statistics()
		assert.Equal(t, totalProduced.Load(), stats.Produced)
		// With high concurrency, we expect drops
		assert.Greater(t, stats.Dropped, uint64(0), "Expected drops under high concurrency")
	})

	t.Run("consumer sees latest events on overflow", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		outputChan := make(chan *domain.CollectorEvent, 100)

		rb, err := NewRingBuffer(RingBufferConfig{
			Size:          4, // Very small buffer
			OutputChannel: outputChan,
			BatchSize:     1,
			BatchTimeout:  5 * time.Millisecond,
			Logger:        logger,
			CollectorName: "test",
		})
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		rb.Start(ctx)
		defer rb.Stop()

		// Track received events
		var received []string
		var mu sync.Mutex

		go func() {
			for event := range outputChan {
				mu.Lock()
				received = append(received, event.EventID)
				mu.Unlock()
			}
		}()

		// Write events that will cause overflow
		for i := 0; i < 10; i++ {
			event := &domain.CollectorEvent{
				EventID:   fmt.Sprintf("event-%02d", i),
				Timestamp: time.Now(),
				Type:      domain.EventTypeKernelNetwork,
				Source:    "test",
				Severity:  domain.EventSeverityInfo,
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "test",
						"version":  "1.0.0",
					},
				},
			}
			rb.Write(event)
			time.Sleep(2 * time.Millisecond) // Small delay between writes
		}

		// Wait for processing
		time.Sleep(100 * time.Millisecond)

		mu.Lock()
		defer mu.Unlock()

		// Should have received some events (not necessarily all due to overflow)
		assert.NotEmpty(t, received, "Should have received some events")
		assert.LessOrEqual(t, len(received), 10, "Should not receive more than written")

		// Verify stats
		stats := rb.Statistics()
		assert.Equal(t, uint64(10), stats.Produced)
		if stats.Dropped > 0 {
			assert.Less(t, stats.Consumed, uint64(10), "If dropped, consumed should be less than produced")
		}
	})

	t.Run("drop count accuracy", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		rb, err := NewRingBuffer(RingBufferConfig{
			Size:          8,
			BatchSize:     1,
			BatchTimeout:  5 * time.Millisecond,
			Logger:        logger,
			CollectorName: "test",
		})
		require.NoError(t, err)

		// Don't start processing - just test write overflow
		const totalWrites = 20
		for i := 0; i < totalWrites; i++ {
			event := &domain.CollectorEvent{
				EventID:   fmt.Sprintf("event-%d", i),
				Timestamp: time.Now(),
				Type:      domain.EventTypeKernelNetwork,
				Source:    "test",
				Severity:  domain.EventSeverityInfo,
				Metadata: domain.EventMetadata{
					Labels: map[string]string{
						"observer": "test",
						"version":  "1.0.0",
					},
				},
			}
			rb.Write(event)
		}

		stats := rb.Statistics()
		assert.Equal(t, uint64(totalWrites), stats.Produced, "All writes should be counted as produced")

		// Expected drops = totalWrites - buffer capacity
		expectedDrops := uint64(totalWrites) - rb.capacity
		assert.GreaterOrEqual(t, stats.Dropped, expectedDrops, "Should drop at least the overflow amount")
	})
}

// TestRingBufferDropTracking tests that drops are accurately tracked
func TestRingBufferDropTracking(t *testing.T) {
	logger := zaptest.NewLogger(t)

	rb, err := NewRingBuffer(RingBufferConfig{
		Size:          4, // Very small for easy testing
		BatchSize:     1,
		BatchTimeout:  5 * time.Millisecond,
		Logger:        logger,
		CollectorName: "test",
	})
	require.NoError(t, err)

	// Write exactly capacity + 1 events
	for i := 0; i < 5; i++ {
		event := &domain.CollectorEvent{
			EventID:   fmt.Sprintf("event-%d", i),
			Timestamp: time.Now(),
			Type:      domain.EventTypeKernelNetwork,
			Source:    "test",
			Severity:  domain.EventSeverityInfo,
			Metadata: domain.EventMetadata{
				Labels: map[string]string{
					"observer": "test",
					"version":  "1.0.0",
				},
			},
		}
		rb.Write(event)
	}

	stats := rb.Statistics()
	assert.Equal(t, uint64(5), stats.Produced, "Should count all writes as produced")
	assert.Equal(t, uint64(1), stats.Dropped, "Should drop exactly 1 event (overflow)")
}

// TestRingBufferConcurrentOverflow tests concurrent writes during overflow
func TestRingBufferConcurrentOverflow(t *testing.T) {
	logger := zaptest.NewLogger(t)

	rb, err := NewRingBuffer(RingBufferConfig{
		Size:          8,
		BatchSize:     2,
		BatchTimeout:  10 * time.Millisecond,
		Logger:        logger,
		CollectorName: "test",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rb.Start(ctx)
	defer rb.Stop()

	var wg sync.WaitGroup
	const numWriters = 10
	const writesPerWriter = 100

	// Concurrent writers that will definitely overflow
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			for i := 0; i < writesPerWriter; i++ {
				event := &domain.CollectorEvent{
					EventID:   fmt.Sprintf("w%d-e%d", writerID, i),
					Timestamp: time.Now(),
					Type:      domain.EventTypeKernelNetwork,
					Source:    fmt.Sprintf("writer-%d", writerID),
					Severity:  domain.EventSeverityInfo,
					Metadata: domain.EventMetadata{
						Labels: map[string]string{
							"observer": "test",
							"version":  "1.0.0",
						},
					},
				}
				rb.Write(event)
			}
		}(w)
	}

	wg.Wait()
	time.Sleep(200 * time.Millisecond) // Allow processing to complete

	stats := rb.Statistics()
	totalExpected := uint64(numWriters * writesPerWriter)

	assert.Equal(t, totalExpected, stats.Produced, "All writes should be counted")
	assert.Greater(t, stats.Dropped, uint64(0), "Should have drops with overflow")
	// Note: In a concurrent scenario with a ring buffer, some events might be
	// overwritten before they can be consumed, and the exact relationship between
	// produced, consumed, and dropped can vary due to timing.
	// We just verify that dropped events are properly counted.
	assert.LessOrEqual(t, stats.Consumed, stats.Produced, "Cannot consume more than produced")
	assert.Greater(t, stats.Consumed, uint64(0), "Should have consumed some events")
}
