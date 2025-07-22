package correlation

import (
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// BufferedEvent wraps an event with buffer metadata
type BufferedEvent struct {
	Event     *domain.UnifiedEvent
	Timestamp time.Time
	Index     int
}

// CircularBuffer implements a thread-safe circular buffer for events
type CircularBuffer struct {
	mu       sync.RWMutex
	buffer   []*BufferedEvent
	capacity int
	head     int
	tail     int
	size     int
}

// NewCircularBuffer creates a new circular buffer with the specified capacity
func NewCircularBuffer(capacity int) (*CircularBuffer, error) {
	if capacity <= 0 {
		return nil, fmt.Errorf("capacity must be positive")
	}

	return &CircularBuffer{
		buffer:   make([]*BufferedEvent, capacity),
		capacity: capacity,
		head:     0,
		tail:     0,
		size:     0,
	}, nil
}

// Add adds an event to the buffer
func (cb *CircularBuffer) Add(event *domain.UnifiedEvent) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	bufferedEvent := &BufferedEvent{
		Event:     event,
		Timestamp: event.Timestamp,
		Index:     cb.tail,
	}

	cb.buffer[cb.tail] = bufferedEvent
	cb.tail = (cb.tail + 1) % cb.capacity

	if cb.size < cb.capacity {
		cb.size++
	} else {
		// Buffer is full, move head forward
		cb.head = (cb.head + 1) % cb.capacity
	}
}

// Get returns the most recent n events from the buffer
func (cb *CircularBuffer) Get(n int) []*BufferedEvent {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if n > cb.size {
		n = cb.size
	}

	result := make([]*BufferedEvent, 0, n)

	// Start from the most recent and work backwards
	idx := cb.tail - 1
	if idx < 0 {
		idx = cb.capacity - 1
	}

	for i := 0; i < n && i < cb.size; i++ {
		if cb.buffer[idx] != nil {
			result = append(result, cb.buffer[idx])
		}
		idx--
		if idx < 0 {
			idx = cb.capacity - 1
		}
	}

	// Reverse to get chronological order
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	return result
}

// GetByTimeWindow returns events within the specified time window
func (cb *CircularBuffer) GetByTimeWindow(window time.Duration) []*BufferedEvent {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	cutoff := time.Now().Add(-window)
	result := make([]*BufferedEvent, 0)

	// Iterate through the buffer
	for i := 0; i < cb.size; i++ {
		idx := (cb.head + i) % cb.capacity
		if cb.buffer[idx] != nil && cb.buffer[idx].Timestamp.After(cutoff) {
			result = append(result, cb.buffer[idx])
		}
	}

	return result
}

// GetByPattern returns events matching the specified pattern
func (cb *CircularBuffer) GetByPattern(pattern func(*BufferedEvent) bool) []*BufferedEvent {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	result := make([]*BufferedEvent, 0)

	for i := 0; i < cb.size; i++ {
		idx := (cb.head + i) % cb.capacity
		if cb.buffer[idx] != nil && pattern(cb.buffer[idx]) {
			result = append(result, cb.buffer[idx])
		}
	}

	return result
}

// Size returns the current number of events in the buffer
func (cb *CircularBuffer) Size() int {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.size
}

// Clear removes all events from the buffer
func (cb *CircularBuffer) Clear() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	for i := range cb.buffer {
		cb.buffer[i] = nil
	}
	cb.head = 0
	cb.tail = 0
	cb.size = 0
}
