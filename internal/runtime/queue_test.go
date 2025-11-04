package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

// RED: Test queue creation
func TestNewBoundedQueue(t *testing.T) {
	config := BackpressureConfig{
		QueueSize:  100,
		DropPolicy: DropOldest,
	}

	queue := NewBoundedQueue(config)
	assert.NotNil(t, queue)
	assert.Equal(t, 100, queue.Cap())
	assert.Equal(t, 0, queue.Len())
	assert.True(t, queue.IsEmpty())
	assert.False(t, queue.IsFull())
}

// RED: Test enqueue and dequeue
func TestBoundedQueue_EnqueueDequeue(t *testing.T) {
	config := BackpressureConfig{
		QueueSize:  10,
		DropPolicy: DropOldest,
	}

	queue := NewBoundedQueue(config)

	event := &domain.ObserverEvent{
		Type:    "network",
		Subtype: "dns_query",
	}

	// Enqueue
	ok := queue.Enqueue(event)
	assert.True(t, ok)
	assert.Equal(t, 1, queue.Len())
	assert.False(t, queue.IsEmpty())

	// Dequeue
	got := queue.Dequeue()
	require.NotNil(t, got)
	assert.Equal(t, "network", got.Type)
	assert.Equal(t, "dns_query", got.Subtype)
	assert.Equal(t, 0, queue.Len())
	assert.True(t, queue.IsEmpty())
}

// RED: Test nil event
func TestBoundedQueue_NilEvent(t *testing.T) {
	config := BackpressureConfig{
		QueueSize:  10,
		DropPolicy: DropOldest,
	}

	queue := NewBoundedQueue(config)
	ok := queue.Enqueue(nil)
	assert.False(t, ok)
	assert.Equal(t, 0, queue.Len())
}

// RED: Test empty queue dequeue
func TestBoundedQueue_DequeueEmpty(t *testing.T) {
	config := BackpressureConfig{
		QueueSize:  10,
		DropPolicy: DropOldest,
	}

	queue := NewBoundedQueue(config)
	event := queue.Dequeue()
	assert.Nil(t, event)
}

// RED: Test DropOldest policy
func TestBoundedQueue_DropOldest(t *testing.T) {
	config := BackpressureConfig{
		QueueSize:  3,
		DropPolicy: DropOldest,
	}

	queue := NewBoundedQueue(config)

	// Fill queue
	event1 := &domain.ObserverEvent{Type: "event1"}
	event2 := &domain.ObserverEvent{Type: "event2"}
	event3 := &domain.ObserverEvent{Type: "event3"}

	queue.Enqueue(event1)
	queue.Enqueue(event2)
	queue.Enqueue(event3)

	assert.True(t, queue.IsFull())
	assert.Equal(t, 3, queue.Len())

	// Add fourth event - should drop event1 (oldest)
	event4 := &domain.ObserverEvent{Type: "event4"}
	ok := queue.Enqueue(event4)
	assert.True(t, ok) // Enqueue succeeds
	assert.Equal(t, 3, queue.Len())

	// Verify oldest (event1) was dropped
	got := queue.Dequeue()
	assert.Equal(t, "event2", got.Type) // event2 is now oldest

	got = queue.Dequeue()
	assert.Equal(t, "event3", got.Type)

	got = queue.Dequeue()
	assert.Equal(t, "event4", got.Type)
}

// RED: Test DropNewest policy
func TestBoundedQueue_DropNewest(t *testing.T) {
	config := BackpressureConfig{
		QueueSize:  3,
		DropPolicy: DropNewest,
	}

	queue := NewBoundedQueue(config)

	// Fill queue
	event1 := &domain.ObserverEvent{Type: "event1"}
	event2 := &domain.ObserverEvent{Type: "event2"}
	event3 := &domain.ObserverEvent{Type: "event3"}

	queue.Enqueue(event1)
	queue.Enqueue(event2)
	queue.Enqueue(event3)

	// Add fourth event - should be dropped (newest)
	event4 := &domain.ObserverEvent{Type: "event4"}
	ok := queue.Enqueue(event4)
	assert.False(t, ok) // Enqueue fails
	assert.Equal(t, 3, queue.Len())

	// Verify original events still there
	got := queue.Dequeue()
	assert.Equal(t, "event1", got.Type)

	got = queue.Dequeue()
	assert.Equal(t, "event2", got.Type)

	got = queue.Dequeue()
	assert.Equal(t, "event3", got.Type)

	// event4 was dropped
	got = queue.Dequeue()
	assert.Nil(t, got)
}

// RED: Test DropRandom policy
func TestBoundedQueue_DropRandom(t *testing.T) {
	config := BackpressureConfig{
		QueueSize:  3,
		DropPolicy: DropRandom,
	}

	queue := NewBoundedQueue(config)

	// Fill queue
	queue.Enqueue(&domain.ObserverEvent{Type: "event1"})
	queue.Enqueue(&domain.ObserverEvent{Type: "event2"})
	queue.Enqueue(&domain.ObserverEvent{Type: "event3"})

	// Add fourth event - should replace random event
	event4 := &domain.ObserverEvent{Type: "event4"}
	ok := queue.Enqueue(event4)
	assert.True(t, ok) // Enqueue succeeds
	assert.Equal(t, 3, queue.Len())

	// Verify event4 is in queue (one of original events was dropped)
	events := make(map[string]bool)
	for i := 0; i < 3; i++ {
		got := queue.Dequeue()
		require.NotNil(t, got)
		events[got.Type] = true
	}

	// event4 should be present
	assert.True(t, events["event4"], "event4 should be in queue")

	// Only 3 out of 4 events should be present
	assert.Equal(t, 3, len(events))
}
