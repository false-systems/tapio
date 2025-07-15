package relay

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/yairfalse/tapio/pkg/api"
)

// RingBuffer implements a high-performance ring buffer for events
// Lock-free for single producer, multiple consumer pattern
type RingBuffer struct {
	items    []*api.Event
	capacity uint64
	head     uint64 // Write position
	tail     uint64 // Read position
	size     atomic.Uint64
	mu       sync.Mutex // Only for multi-writer scenarios
}

// NewRingBuffer creates a new ring buffer
func NewRingBuffer(capacity int) *RingBuffer {
	// Round up to power of 2 for efficient modulo
	cap := uint64(1)
	for cap < uint64(capacity) {
		cap <<= 1
	}
	
	return &RingBuffer{
		items:    make([]*api.Event, cap),
		capacity: cap,
	}
}

// Add adds an event to the buffer
func (rb *RingBuffer) Add(event *api.Event) error {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	
	if rb.size.Load() >= rb.capacity {
		return fmt.Errorf("buffer full")
	}
	
	// Write to current head position
	rb.items[rb.head&(rb.capacity-1)] = event
	rb.head++
	rb.size.Add(1)
	
	return nil
}

// AddBatch adds multiple events efficiently
func (rb *RingBuffer) AddBatch(events []*api.Event) error {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	
	available := rb.capacity - rb.size.Load()
	if uint64(len(events)) > available {
		return fmt.Errorf("insufficient buffer space: need %d, have %d", len(events), available)
	}
	
	for _, event := range events {
		rb.items[rb.head&(rb.capacity-1)] = event
		rb.head++
	}
	rb.size.Add(uint64(len(events)))
	
	return nil
}

// Drain retrieves up to maxCount events
func (rb *RingBuffer) Drain(maxCount int) []*api.Event {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	
	count := min(int(rb.size.Load()), maxCount)
	if count == 0 {
		return nil
	}
	
	events := make([]*api.Event, count)
	for i := 0; i < count; i++ {
		events[i] = rb.items[rb.tail&(rb.capacity-1)]
		rb.items[rb.tail&(rb.capacity-1)] = nil // Help GC
		rb.tail++
	}
	rb.size.Add(-uint64(count))
	
	return events
}

// Size returns current buffer size
func (rb *RingBuffer) Size() int {
	return int(rb.size.Load())
}

// IsFull checks if buffer is at capacity
func (rb *RingBuffer) IsFull() bool {
	return rb.size.Load() >= rb.capacity
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}