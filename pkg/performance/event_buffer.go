package performance

import (
	"errors"
	"unsafe"

	"github.com/yairfalse/tapio/pkg/domain"
)

// EventBuffer provides a type-safe wrapper around RingBuffer for UnifiedEvent processing.
// This allows components to use the high-performance ring buffer without dealing with
// unsafe pointers directly.
type EventBuffer struct {
	rb *RingBuffer
}

// NewEventBuffer creates a new event buffer with the specified capacity.
// Capacity must be a power of 2.
func NewEventBuffer(capacity uint64) (*EventBuffer, error) {
	rb, err := NewRingBuffer(capacity)
	if err != nil {
		return nil, err
	}
	return &EventBuffer{rb: rb}, nil
}

// Put adds a UnifiedEvent to the buffer.
// Returns an error if the buffer is full.
func (eb *EventBuffer) Put(event *domain.UnifiedEvent) error {
	if event == nil {
		return errors.New("cannot put nil event")
	}
	return eb.rb.Put(unsafe.Pointer(event))
}

// Get retrieves a UnifiedEvent from the buffer.
// Returns an error if the buffer is empty.
func (eb *EventBuffer) Get() (*domain.UnifiedEvent, error) {
	ptr, err := eb.rb.Get()
	if err != nil {
		return nil, err
	}
	return (*domain.UnifiedEvent)(ptr), nil
}

// TryPut attempts to add an event without blocking.
// Returns true if successful, false if the buffer is full.
func (eb *EventBuffer) TryPut(event *domain.UnifiedEvent) bool {
	if event == nil {
		return false
	}
	return eb.rb.TryPut(unsafe.Pointer(event))
}

// TryGet attempts to retrieve an event without blocking.
// Returns the event and true if successful, nil and false if empty.
func (eb *EventBuffer) TryGet() (*domain.UnifiedEvent, bool) {
	ptr, ok := eb.rb.TryGet()
	if !ok {
		return nil, false
	}
	return (*domain.UnifiedEvent)(ptr), true
}

// Size returns the current number of events in the buffer
func (eb *EventBuffer) Size() uint64 {
	return eb.rb.Size()
}

// Capacity returns the maximum capacity of the buffer
func (eb *EventBuffer) Capacity() uint64 {
	return eb.rb.Capacity()
}

// IsEmpty returns true if the buffer is empty
func (eb *EventBuffer) IsEmpty() bool {
	return eb.rb.IsEmpty()
}

// IsFull returns true if the buffer is full
func (eb *EventBuffer) IsFull() bool {
	return eb.rb.IsFull()
}

// GetStats returns statistics about the event buffer
func (eb *EventBuffer) GetStats() RingBufferStats {
	return eb.rb.GetStats()
}

// DrainTo drains all events from the buffer into the provided slice.
// Returns the number of events drained.
func (eb *EventBuffer) DrainTo(events []*domain.UnifiedEvent) int {
	count := 0
	for count < len(events) && !eb.IsEmpty() {
		if event, ok := eb.TryGet(); ok {
			events[count] = event
			count++
		} else {
			break
		}
	}
	return count
}

// EventBatchBuffer provides batch operations for event processing
type EventBatchBuffer struct {
	buffer *EventBuffer
}

// NewEventBatchBuffer creates a new batch-capable event buffer
func NewEventBatchBuffer(capacity uint64) (*EventBatchBuffer, error) {
	eb, err := NewEventBuffer(capacity)
	if err != nil {
		return nil, err
	}
	return &EventBatchBuffer{buffer: eb}, nil
}

// PutBatch adds multiple events to the buffer.
// Returns the number of events successfully added.
func (ebb *EventBatchBuffer) PutBatch(events []*domain.UnifiedEvent) (int, error) {
	added := 0
	for _, event := range events {
		if err := ebb.buffer.Put(event); err != nil {
			if added == 0 {
				return 0, err
			}
			break
		}
		added++
	}
	return added, nil
}

// GetBatch retrieves up to maxCount events from the buffer.
// Returns the events retrieved and any error.
func (ebb *EventBatchBuffer) GetBatch(maxCount int) ([]*domain.UnifiedEvent, error) {
	events := make([]*domain.UnifiedEvent, 0, maxCount)

	for i := 0; i < maxCount; i++ {
		event, err := ebb.buffer.Get()
		if err != nil {
			if i == 0 {
				return nil, err
			}
			break
		}
		events = append(events, event)
	}

	return events, nil
}

// DrainTo drains all events from the buffer into the provided slice.
// Returns the number of events drained.
func (ebb *EventBatchBuffer) DrainTo(events []*domain.UnifiedEvent) int {
	count := 0
	for count < len(events) && !ebb.buffer.IsEmpty() {
		if event, ok := ebb.buffer.TryGet(); ok {
			events[count] = event
			count++
		} else {
			break
		}
	}
	return count
}
