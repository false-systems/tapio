package performance

import (
	"unsafe"

	"github.com/yairfalse/tapio/pkg/domain"
)

// EventBuffer is a type-safe wrapper around RingBuffer for UnifiedEvent
type EventBuffer struct {
	rb *RingBuffer
}

// NewEventBuffer creates a new event buffer
func NewEventBuffer(capacity uint64) (*EventBuffer, error) {
	rb, err := NewRingBuffer(capacity)
	if err != nil {
		return nil, err
	}

	return &EventBuffer{rb: rb}, nil
}

// Put adds an event to the buffer
func (eb *EventBuffer) Put(event *domain.UnifiedEvent) error {
	return eb.rb.Put(unsafe.Pointer(event))
}

// Get retrieves an event from the buffer
func (eb *EventBuffer) Get() (*domain.UnifiedEvent, error) {
	ptr, err := eb.rb.Get()
	if err != nil {
		return nil, err
	}
	return (*domain.UnifiedEvent)(ptr), nil
}

// TryPut attempts to add an event without blocking
func (eb *EventBuffer) TryPut(event *domain.UnifiedEvent) bool {
	return eb.rb.TryPut(unsafe.Pointer(event))
}

// GetStats returns buffer statistics
func (eb *EventBuffer) GetStats() BufferStats {
	return eb.rb.GetStats()
}

// EventBatchBuffer provides batch operations for events
type EventBatchBuffer struct {
	*EventBuffer
}

// NewEventBatchBuffer creates a new batch-capable event buffer
func NewEventBatchBuffer(capacity uint64) (*EventBatchBuffer, error) {
	eb, err := NewEventBuffer(capacity)
	if err != nil {
		return nil, err
	}

	return &EventBatchBuffer{EventBuffer: eb}, nil
}

// PutBatch adds multiple events to the buffer
func (ebb *EventBatchBuffer) PutBatch(events []*domain.UnifiedEvent) (int, error) {
	added := 0
	for _, event := range events {
		if err := ebb.Put(event); err != nil {
			return added, err
		}
		added++
	}
	return added, nil
}

// GetBatch retrieves multiple events from the buffer
func (ebb *EventBatchBuffer) GetBatch(maxSize int) ([]*domain.UnifiedEvent, error) {
	events := make([]*domain.UnifiedEvent, 0, maxSize)

	for i := 0; i < maxSize; i++ {
		event, err := ebb.Get()
		if err != nil {
			if err == ErrBufferEmpty && len(events) > 0 {
				return events, nil
			}
			return events, err
		}
		events = append(events, event)
	}

	return events, nil
}

// DrainTo drains events into the provided slice
func (ebb *EventBatchBuffer) DrainTo(events []*domain.UnifiedEvent) int {
	count := 0
	for i := range events {
		event, err := ebb.Get()
		if err != nil {
			break
		}
		events[i] = event
		count++
	}
	return count
}
