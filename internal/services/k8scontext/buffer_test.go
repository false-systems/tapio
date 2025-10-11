package k8scontext

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnqueueEvent_Success verifies events are buffered
func TestEnqueueEvent_Success(t *testing.T) {
	service := &Service{
		eventBuffer: make(chan func() error, 10),
	}

	executed := false
	event := func() error {
		executed = true
		return nil
	}

	service.enqueueEvent(event)

	// Consume event from buffer
	bufferedEvent := <-service.eventBuffer
	err := bufferedEvent()

	require.NoError(t, err)
	assert.True(t, executed, "Event should be executed")
}

// TestEnqueueEvent_BufferFull verifies buffer overflow drops oldest
func TestEnqueueEvent_BufferFull(t *testing.T) {
	service := &Service{
		eventBuffer: make(chan func() error, 2), // Small buffer
	}

	var counter int32

	// Fill buffer
	for i := 0; i < 2; i++ {
		service.enqueueEvent(func() error {
			atomic.AddInt32(&counter, 1)
			return nil
		})
	}

	// Buffer is full, this should drop oldest and add new
	service.enqueueEvent(func() error {
		atomic.AddInt32(&counter, 1)
		return nil
	})

	// Should still have 2 events
	assert.Equal(t, 2, len(service.eventBuffer))
}

// TestProcessEvents_Success verifies worker processes events
func TestProcessEvents_Success(t *testing.T) {
	service := &Service{
		eventBuffer: make(chan func() error, 10),
		config: Config{
			MaxRetries:    3,
			RetryInterval: 10 * time.Millisecond,
		},
	}

	var executed int32
	event := func() error {
		atomic.AddInt32(&executed, 1)
		return nil
	}

	// Start worker
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go service.processEvents(ctx)

	// Enqueue event
	service.enqueueEvent(event)

	// Wait for processing
	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, int32(1), atomic.LoadInt32(&executed))
}

// TestProcessEvents_RetryOnFailure verifies exponential backoff retry
func TestProcessEvents_RetryOnFailure(t *testing.T) {
	service := &Service{
		eventBuffer: make(chan func() error, 10),
		config: Config{
			MaxRetries:    3,
			RetryInterval: 10 * time.Millisecond,
		},
	}

	var attempts int32
	event := func() error {
		count := atomic.AddInt32(&attempts, 1)
		if count < 3 {
			return errors.New("temporary failure")
		}
		return nil // Succeed on 3rd attempt
	}

	// Start worker
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go service.processEvents(ctx)

	// Enqueue event
	service.enqueueEvent(event)

	// Wait for retries
	time.Sleep(150 * time.Millisecond)

	// Should have retried 3 times
	assert.Equal(t, int32(3), atomic.LoadInt32(&attempts))
}

// TestProcessEvents_MaxRetriesExceeded verifies event dropped after max retries
func TestProcessEvents_MaxRetriesExceeded(t *testing.T) {
	service := &Service{
		eventBuffer: make(chan func() error, 10),
		config: Config{
			MaxRetries:    3,
			RetryInterval: 10 * time.Millisecond,
		},
	}

	var attempts int32
	event := func() error {
		atomic.AddInt32(&attempts, 1)
		return errors.New("permanent failure")
	}

	// Start worker
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go service.processEvents(ctx)

	// Enqueue event
	service.enqueueEvent(event)

	// Wait for all retries
	time.Sleep(200 * time.Millisecond)

	// Should have tried MaxRetries + 1 times (initial + 3 retries)
	assert.Equal(t, int32(4), atomic.LoadInt32(&attempts))
}

// TestProcessEvents_ContextCancellation verifies graceful shutdown
func TestProcessEvents_ContextCancellation(t *testing.T) {
	service := &Service{
		eventBuffer: make(chan func() error, 10),
		config: Config{
			MaxRetries:    3,
			RetryInterval: 10 * time.Millisecond,
		},
	}

	// Start worker
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool)
	go func() {
		service.processEvents(ctx)
		done <- true
	}()

	// Cancel context
	cancel()

	// Worker should exit
	select {
	case <-done:
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Fatal("processEvents did not exit on context cancellation")
	}
}
