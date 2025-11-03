package runtime

import (
	"math/rand"
	"sync"

	"github.com/yairfalse/tapio/pkg/domain"
)

// BoundedQueue implements a fixed-size queue with configurable drop policies.
// Thread-safe for concurrent use. All rng operations are protected by mu.
type BoundedQueue struct {
	config BackpressureConfig
	mu     sync.RWMutex
	events []*domain.ObserverEvent
	rng    *rand.Rand // Protected by mu
}

// NewBoundedQueue creates a new bounded queue with the given configuration
func NewBoundedQueue(config BackpressureConfig) *BoundedQueue {
	return &BoundedQueue{
		config: config,
		events: make([]*domain.ObserverEvent, 0, config.QueueSize),
		rng:    rand.New(rand.NewSource(rand.Int63())),
	}
}

// Enqueue adds an event to the queue.
// Returns true if event was added, false if it was dropped.
func (q *BoundedQueue) Enqueue(event *domain.ObserverEvent) bool {
	if event == nil {
		return false
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	// Check if queue is full
	if len(q.events) >= q.config.QueueSize {
		// Queue full - apply drop policy
		switch q.config.DropPolicy {
		case DropOldest:
			// Remove oldest (index 0), add new at end
			q.events = q.events[1:]
			q.events = append(q.events, event)
			return true

		case DropNewest:
			// Drop incoming event, keep existing
			return false

		case DropRandom:
			// Drop random event, add new
			dropIdx := q.rng.Intn(q.config.QueueSize)
			q.events[dropIdx] = event
			return true

		default:
			// Unknown policy - drop newest (safe default)
			return false
		}
	}

	// Queue not full - add event
	q.events = append(q.events, event)
	return true
}

// Dequeue removes and returns the oldest event from the queue.
// Returns nil if queue is empty.
func (q *BoundedQueue) Dequeue() *domain.ObserverEvent {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.events) == 0 {
		return nil
	}

	event := q.events[0]
	q.events = q.events[1:]
	return event
}

// Len returns the current number of events in the queue
func (q *BoundedQueue) Len() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.events)
}

// Cap returns the maximum capacity of the queue
func (q *BoundedQueue) Cap() int {
	return q.config.QueueSize
}

// IsFull returns true if queue is at capacity
func (q *BoundedQueue) IsFull() bool {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.events) >= q.config.QueueSize
}

// IsEmpty returns true if queue is empty
func (q *BoundedQueue) IsEmpty() bool {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.events) == 0
}
