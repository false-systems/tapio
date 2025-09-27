package base

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

// TestEventChannelManagerBasicOperations tests basic send and receive operations
func TestEventChannelManagerBasicOperations(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ecm := NewEventChannelManager(10, "test", logger)
	require.NotNil(t, ecm)

	// Test sending valid event
	event := &domain.CollectorEvent{
		EventID:   "test-1",
		Timestamp: time.Now(),
		Type:      domain.EventTypeKernelSyscall,
		Source:    "test",
		Severity:  domain.EventSeverityInfo,
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"observer": "test",
				"version":  "1.0.0",
			},
		},
	}

	sent := ecm.SendEvent(event)
	assert.True(t, sent)
	assert.Equal(t, int64(1), ecm.GetSentCount())
	assert.Equal(t, int64(0), ecm.GetDroppedCount())

	// Test receiving event
	select {
	case received := <-ecm.GetChannel():
		assert.Equal(t, event.EventID, received.EventID)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for event")
	}
}

// TestEventChannelManagerChannelFull tests behavior when channel is full
func TestEventChannelManagerChannelFull(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ecm := NewEventChannelManager(2, "test", logger)

	// Fill the channel
	for i := 0; i < 2; i++ {
		event := &domain.CollectorEvent{
			EventID:   fmt.Sprintf("test-%d", i),
			Timestamp: time.Now(),
			Type:      domain.EventTypeKernelSyscall,
			Source:    "test",
			Severity:  domain.EventSeverityInfo,
			Metadata: domain.EventMetadata{
				Labels: map[string]string{
					"observer": "test",
					"version":  "1.0.0",
				},
			},
		}
		sent := ecm.SendEvent(event)
		assert.True(t, sent)
	}

	// Channel should be full
	assert.True(t, ecm.IsChannelFull())
	assert.Equal(t, 100.0, ecm.GetChannelUtilization())

	// Next event should be dropped
	event := &domain.CollectorEvent{
		EventID:   "test-dropped",
		Timestamp: time.Now(),
		Type:      domain.EventTypeKernelSyscall,
		Source:    "test",
		Severity:  domain.EventSeverityInfo,
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"observer": "test",
				"version":  "1.0.0",
			},
		},
	}
	sent := ecm.SendEvent(event)
	assert.False(t, sent)
	assert.Equal(t, int64(2), ecm.GetSentCount())
	assert.Equal(t, int64(1), ecm.GetDroppedCount())
}

// TestEventChannelManagerConcurrentSend tests concurrent sending
func TestEventChannelManagerConcurrentSend(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ecm := NewEventChannelManager(1000, "test", logger)

	const numGoroutines = 10
	const eventsPerGoroutine = 100

	var wg sync.WaitGroup
	var sentCount atomic.Int64

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < eventsPerGoroutine; j++ {
				event := &domain.CollectorEvent{
					EventID:   fmt.Sprintf("test-%d-%d", id, j),
					Timestamp: time.Now(),
					Type:      domain.EventTypeKernelSyscall,
					Source:    "test",
					Severity:  domain.EventSeverityInfo,
					Metadata: domain.EventMetadata{
						Labels: map[string]string{
							"observer": "test",
							"version":  "1.0.0",
						},
					},
				}
				if ecm.SendEvent(event) {
					sentCount.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()

	// All events should be sent (channel is large enough)
	assert.Equal(t, int64(numGoroutines*eventsPerGoroutine), sentCount.Load())
	assert.Equal(t, sentCount.Load(), ecm.GetSentCount())
	assert.Equal(t, int64(0), ecm.GetDroppedCount())
}

// TestEventChannelManagerConcurrentSendReceive tests concurrent send and receive
func TestEventChannelManagerConcurrentSendReceive(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ecm := NewEventChannelManager(100, "test", logger)

	const numProducers = 5
	const numConsumers = 3
	const eventsPerProducer = 100

	var wg sync.WaitGroup
	var receivedCount atomic.Int64

	// Start consumers
	for i := 0; i < numConsumers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ch := ecm.GetChannel()
			for {
				select {
				case event := <-ch:
					if event == nil {
						return
					}
					receivedCount.Add(1)
				case <-time.After(500 * time.Millisecond):
					return
				}
			}
		}(i)
	}

	// Start producers
	for i := 0; i < numProducers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < eventsPerProducer; j++ {
				event := &domain.CollectorEvent{
					EventID:   fmt.Sprintf("test-%d-%d", id, j),
					Timestamp: time.Now(),
					Type:      domain.EventTypeKernelSyscall,
					Source:    "test",
					Severity:  domain.EventSeverityInfo,
					Metadata: domain.EventMetadata{
						Labels: map[string]string{
							"observer": "test",
							"version":  "1.0.0",
						},
					},
				}
				ecm.SendEvent(event)
			}
		}(i)
	}

	// Wait for producers to finish and consumers to timeout
	wg.Wait()

	// Should have received all events
	assert.Equal(t, int64(numProducers*eventsPerProducer), ecm.GetSentCount())
	assert.Equal(t, ecm.GetSentCount(), receivedCount.Load())
}

// TestEventChannelManagerRaceConditionClose tests race condition with Close
func TestEventChannelManagerRaceConditionClose(t *testing.T) {
	for i := 0; i < 100; i++ {
		logger := zaptest.NewLogger(t)
		ecm := NewEventChannelManager(10, "test", logger)

		var wg sync.WaitGroup
		stopCh := make(chan struct{})

		// Start senders
		for j := 0; j < 5; j++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for {
					select {
					case <-stopCh:
						return
					default:
						event := &domain.CollectorEvent{
							EventID:   fmt.Sprintf("test-%d", id),
							Timestamp: time.Now(),
							Type:      domain.EventTypeKernelSyscall,
							Source:    "test",
							Severity:  domain.EventSeverityInfo,
							Metadata: domain.EventMetadata{
								Labels: map[string]string{
									"observer": "test",
									"version":  "1.0.0",
								},
							},
						}
						ecm.SendEvent(event)
						runtime.Gosched()
					}
				}
			}(j)
		}

		// Start readers
		for j := 0; j < 3; j++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for {
					select {
					case <-stopCh:
						return
					case <-ecm.GetChannel():
						// Process event
					}
				}
			}(j)
		}

		// Let them run briefly
		time.Sleep(10 * time.Millisecond)

		// Close channel while operations are in progress
		ecm.Close()
		close(stopCh)

		// Wait for all goroutines to finish
		wg.Wait()
	}
}

// TestEventChannelManagerMultipleClose tests multiple close calls
func TestEventChannelManagerMultipleClose(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ecm := NewEventChannelManager(10, "test", logger)

	// Send an event
	event := &domain.CollectorEvent{
		EventID:   "test-1",
		Timestamp: time.Now(),
		Type:      domain.EventTypeKernelSyscall,
		Source:    "test",
		Severity:  domain.EventSeverityInfo,
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"observer": "test",
				"version":  "1.0.0",
			},
		},
	}
	ecm.SendEvent(event)

	// Close multiple times should not panic
	ecm.Close()
	ecm.Close()
	ecm.Close()

	// Operations after close should handle gracefully
	sent := ecm.SendEvent(event)
	assert.False(t, sent, "Should not send after close")

	// Channel should be nil after close
	ch := ecm.GetChannel()
	assert.Nil(t, ch, "Channel should be nil after close")
}

// TestEventChannelManagerValidation tests event validation
func TestEventChannelManagerValidation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ecm := NewEventChannelManager(10, "test", logger)

	// Send invalid event (missing required fields)
	invalidEvent := &domain.CollectorEvent{
		EventID: "invalid",
		// Missing Timestamp, Type, Source, Severity
	}

	sent := ecm.SendEvent(invalidEvent)
	assert.False(t, sent, "Should not send invalid event")
	assert.Equal(t, int64(0), ecm.GetSentCount())
	assert.Equal(t, int64(1), ecm.GetDroppedCount())

	// Send valid event
	validEvent := &domain.CollectorEvent{
		EventID:   "valid",
		Timestamp: time.Now(),
		Type:      domain.EventTypeKernelSyscall,
		Source:    "test",
		Severity:  domain.EventSeverityInfo,
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"observer": "test",
				"version":  "1.0.0",
			},
		},
	}

	sent = ecm.SendEvent(validEvent)
	assert.True(t, sent, "Should send valid event")
	assert.Equal(t, int64(1), ecm.GetSentCount())
}

// TestEventChannelManagerUtilization tests channel utilization calculation
func TestEventChannelManagerUtilization(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ecm := NewEventChannelManager(4, "test", logger)

	assert.Equal(t, 0.0, ecm.GetChannelUtilization())

	// Send events and check utilization
	for i := 0; i < 4; i++ {
		event := &domain.CollectorEvent{
			EventID:   fmt.Sprintf("test-%d", i),
			Timestamp: time.Now(),
			Type:      domain.EventTypeKernelSyscall,
			Source:    "test",
			Severity:  domain.EventSeverityInfo,
			Metadata: domain.EventMetadata{
				Labels: map[string]string{
					"observer": "test",
					"version":  "1.0.0",
				},
			},
		}
		ecm.SendEvent(event)
		expectedUtilization := float64(i+1) / 4.0 * 100.0
		assert.Equal(t, expectedUtilization, ecm.GetChannelUtilization())
	}

	assert.True(t, ecm.IsChannelFull())
}

// BenchmarkEventChannelManagerSend benchmarks send operation
func BenchmarkEventChannelManagerSend(b *testing.B) {
	logger := zaptest.NewLogger(b)
	ecm := NewEventChannelManager(10000, "test", logger)

	event := &domain.CollectorEvent{
		EventID:   "bench",
		Timestamp: time.Now(),
		Type:      domain.EventTypeKernelSyscall,
		Source:    "test",
		Severity:  domain.EventSeverityInfo,
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"observer": "test",
				"version":  "1.0.0",
			},
		},
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ecm.SendEvent(event)
		}
	})
}
