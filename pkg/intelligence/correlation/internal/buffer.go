package internal
import (
	"fmt"
	"sort"
	"sync"
	"time"
	"github.com/falseyair/tapio/pkg/domain"
	"github.com/falseyair/tapio/pkg/intelligence/correlation/core"
)
// eventBuffer implements core.EventBuffer
type eventBuffer struct {
	events   map[domain.EventID]domain.Event
	timeline []domain.Event // Events sorted by timestamp for efficient range queries
	capacity int
	mutex    sync.RWMutex
}
// NewEventBuffer creates a new event buffer
func NewEventBuffer(capacity int) core.EventBuffer {
	return &eventBuffer{
		events:   make(map[domain.EventID]domain.Event),
		timeline: make([]domain.Event, 0, capacity),
		capacity: capacity,
	}
}
// Add adds an event to the buffer
func (b *eventBuffer) Add(event domain.Event) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	// Check if event already exists
	if _, exists := b.events[event.ID]; exists {
		return nil // Event already exists, ignore
	}
	// Check capacity
	if len(b.events) >= b.capacity {
		// Remove oldest event
		if err := b.removeOldestEvent(); err != nil {
			return fmt.Errorf("failed to remove oldest event: %w", err)
		}
	}
	// Add event
	b.events[event.ID] = event
	// Insert into timeline maintaining sort order
	b.insertIntoTimeline(event)
	return nil
}
// Remove removes an event from the buffer
func (b *eventBuffer) Remove(eventID domain.EventID) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	event, exists := b.events[eventID]
	if !exists {
		return core.ErrEventNotFound
	}
	// Remove from events map
	delete(b.events, eventID)
	// Remove from timeline
	b.removeFromTimeline(event)
	return nil
}
// Clear clears all events from the buffer
func (b *eventBuffer) Clear() error {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.events = make(map[domain.EventID]domain.Event)
	b.timeline = b.timeline[:0]
	return nil
}
// Get retrieves an event by ID
func (b *eventBuffer) Get(eventID domain.EventID) (domain.Event, error) {
	b.mutex.RLock()
	defer b.mutex.RUnlock()
	event, exists := b.events[eventID]
	if !exists {
		return domain.Event{}, core.ErrEventNotFound
	}
	return event, nil
}
// GetByTimeRange retrieves events within a time range
func (b *eventBuffer) GetByTimeRange(start, end time.Time) ([]domain.Event, error) {
	b.mutex.RLock()
	defer b.mutex.RUnlock()
	if end.Before(start) {
		return nil, core.ErrInvalidTimeRange
	}
	var result []domain.Event
	// Binary search for start position
	startIdx := b.findTimeIndex(start, true)
	endIdx := b.findTimeIndex(end, false)
	// Extract events in range
	for i := startIdx; i <= endIdx && i < len(b.timeline); i++ {
		event := b.timeline[i]
		if !event.Timestamp.Before(start) && !event.Timestamp.After(end) {
			result = append(result, event)
		}
	}
	return result, nil
}
// GetBySource retrieves events from a specific source
func (b *eventBuffer) GetBySource(source domain.Source) ([]domain.Event, error) {
	b.mutex.RLock()
	defer b.mutex.RUnlock()
	var result []domain.Event
	for _, event := range b.events {
		if event.Source == source {
			result = append(result, event)
		}
	}
	// Sort by timestamp
	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp.Before(result[j].Timestamp)
	})
	return result, nil
}
// GetByType retrieves events of a specific type
func (b *eventBuffer) GetByType(eventType domain.EventType) ([]domain.Event, error) {
	b.mutex.RLock()
	defer b.mutex.RUnlock()
	var result []domain.Event
	for _, event := range b.events {
		if event.Type == eventType {
			result = append(result, event)
		}
	}
	// Sort by timestamp
	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp.Before(result[j].Timestamp)
	})
	return result, nil
}
// Size returns the current number of events in the buffer
func (b *eventBuffer) Size() int {
	b.mutex.RLock()
	defer b.mutex.RUnlock()
	return len(b.events)
}
// Capacity returns the buffer capacity
func (b *eventBuffer) Capacity() int {
	return b.capacity
}
// OldestEvent returns the oldest event in the buffer
func (b *eventBuffer) OldestEvent() (domain.Event, error) {
	b.mutex.RLock()
	defer b.mutex.RUnlock()
	if len(b.timeline) == 0 {
		return domain.Event{}, core.ErrBufferEmpty
	}
	return b.timeline[0], nil
}
// NewestEvent returns the newest event in the buffer
func (b *eventBuffer) NewestEvent() (domain.Event, error) {
	b.mutex.RLock()
	defer b.mutex.RUnlock()
	if len(b.timeline) == 0 {
		return domain.Event{}, core.ErrBufferEmpty
	}
	return b.timeline[len(b.timeline)-1], nil
}
// Expire removes events older than the specified time
func (b *eventBuffer) Expire(before time.Time) (int, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	var expiredCount int
	var toRemove []domain.EventID
	// Find events to expire
	for id, event := range b.events {
		if event.Timestamp.Before(before) {
			toRemove = append(toRemove, id)
		}
	}
	// Remove expired events
	for _, id := range toRemove {
		event := b.events[id]
		delete(b.events, id)
		b.removeFromTimeline(event)
		expiredCount++
	}
	return expiredCount, nil
}
// Helper methods
// insertIntoTimeline inserts an event into the timeline maintaining sort order
func (b *eventBuffer) insertIntoTimeline(event domain.Event) {
	// Find insertion point using binary search
	idx := sort.Search(len(b.timeline), func(i int) bool {
		return b.timeline[i].Timestamp.After(event.Timestamp)
	})
	// Insert at the correct position
	b.timeline = append(b.timeline, domain.Event{})
	copy(b.timeline[idx+1:], b.timeline[idx:])
	b.timeline[idx] = event
}
// removeFromTimeline removes an event from the timeline
func (b *eventBuffer) removeFromTimeline(event domain.Event) {
	for i, e := range b.timeline {
		if e.ID == event.ID {
			b.timeline = append(b.timeline[:i], b.timeline[i+1:]...)
			break
		}
	}
}
// removeOldestEvent removes the oldest event from the buffer
func (b *eventBuffer) removeOldestEvent() error {
	if len(b.timeline) == 0 {
		return core.ErrBufferEmpty
	}
	oldest := b.timeline[0]
	delete(b.events, oldest.ID)
	b.timeline = b.timeline[1:]
	return nil
}
// findTimeIndex finds the index for a given time using binary search
func (b *eventBuffer) findTimeIndex(target time.Time, findStart bool) int {
	if findStart {
		// Find first event >= target
		return sort.Search(len(b.timeline), func(i int) bool {
			return !b.timeline[i].Timestamp.Before(target)
		})
	} else {
		// Find last event <= target
		idx := sort.Search(len(b.timeline), func(i int) bool {
			return b.timeline[i].Timestamp.After(target)
		})
		if idx > 0 {
			idx--
		}
		return idx
	}
}