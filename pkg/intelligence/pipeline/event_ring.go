package pipeline

import (
	"sync/atomic"
	"unsafe"

	"github.com/yairfalse/tapio/pkg/domain"
)

// EventRing implements a lock-free ring buffer for real-time event streaming
// Design: In-memory only, lock-free, power-of-2 sizing, overwrites old events
type EventRing struct {
	// Ring buffer storage - power of 2 size for efficient masking
	buffer []unsafe.Pointer

	// Bit mask for fast modulo operations (size - 1)
	mask uint64

	// Atomic positions for lock-free operations
	writePos atomic.Uint64 // Next write position
	readPos  atomic.Uint64 // Next read position

	// Capacity for metrics
	capacity uint64
}

// NewEventRing creates a new lock-free event ring
// capacity must be power of 2 for efficient bit operations
func NewEventRing(capacity uint64) *EventRing {
	// Ensure power of 2
	if capacity == 0 || capacity&(capacity-1) != 0 {
		capacity = 65536 // Default 64K events
	}

	return &EventRing{
		buffer:   make([]unsafe.Pointer, capacity),
		mask:     capacity - 1,
		capacity: capacity,
	}
}

// Put adds an event to the ring buffer (lock-free)
// Returns true if added, false if would overwrite (though we overwrite anyway)
func (er *EventRing) Put(event *domain.UnifiedEvent) bool {
	// Get current write position atomically
	writePos := er.writePos.Load()
	readPos := er.readPos.Load()

	// Calculate if we're about to overwrite
	willOverwrite := (writePos - readPos) >= er.capacity

	// Store the event at current position (power-of-2 masking)
	idx := writePos & er.mask
	atomic.StorePointer(&er.buffer[idx], unsafe.Pointer(event))

	// Advance write position atomically
	er.writePos.Store(writePos + 1)

	// If we overwrote, advance read position too
	if willOverwrite {
		er.readPos.Store(readPos + 1)
	}

	return !willOverwrite
}

// Get retrieves an event from the ring buffer (lock-free)
// Returns nil if no events available
func (er *EventRing) Get() *domain.UnifiedEvent {
	readPos := er.readPos.Load()
	writePos := er.writePos.Load()

	// Check if data available
	if readPos >= writePos {
		return nil // No events available
	}

	// Get event at read position
	idx := readPos & er.mask
	ptr := atomic.LoadPointer(&er.buffer[idx])
	if ptr == nil {
		return nil
	}

	// Advance read position atomically
	er.readPos.Store(readPos + 1)

	return (*domain.UnifiedEvent)(ptr)
}

// GetBatch retrieves multiple events efficiently
func (er *EventRing) GetBatch(events []*domain.UnifiedEvent) int {
	readPos := er.readPos.Load()
	writePos := er.writePos.Load()

	// Calculate available events
	available := writePos - readPos
	if available == 0 {
		return 0
	}

	// Limit to buffer size
	maxEvents := uint64(len(events))
	if available > maxEvents {
		available = maxEvents
	}

	// Copy events in batch
	count := 0
	for i := uint64(0); i < available; i++ {
		idx := (readPos + i) & er.mask
		ptr := atomic.LoadPointer(&er.buffer[idx])
		if ptr != nil {
			events[count] = (*domain.UnifiedEvent)(ptr)
			count++
		}
	}

	// Advance read position by actual events retrieved
	er.readPos.Store(readPos + uint64(count))

	return count
}

// Size returns current number of events in ring (approximate - lock-free)
func (er *EventRing) Size() uint64 {
	writePos := er.writePos.Load()
	readPos := er.readPos.Load()

	if writePos >= readPos {
		return writePos - readPos
	}
	return 0
}

// IsEmpty returns true if no events available
func (er *EventRing) IsEmpty() bool {
	return er.Size() == 0
}

// IsFull returns true if ring buffer is at capacity
func (er *EventRing) IsFull() bool {
	return er.Size() >= er.capacity
}

// Capacity returns the ring buffer capacity
func (er *EventRing) Capacity() uint64 {
	return er.capacity
}

// Reset clears the ring buffer (for testing)
func (er *EventRing) Reset() {
	er.writePos.Store(0)
	er.readPos.Store(0)
}

// RealtimeEventPipeline implements the hybrid approach
// Events → Ring Buffer → Correlation Engine → Persistent Store
type RealtimeEventPipeline struct {
	// Raw events flow through this ring for real-time consumption
	eventRing *EventRing

	// Intelligence pipeline for correlation
	intelligencePipeline IntelligencePipeline

	// Correlation persister - we persist findings, not raw events
	correlationPersister *CorrelationPersister
}

// CorrelationPersister handles intelligent persistence of findings
// Unlike raw event storage: We persist correlations only
type CorrelationPersister struct {
	// Source ring to consume from
	sourceRing *EventRing

	// Persistent store for findings
	store CorrelationStore

	// Buffer for batch persistence
	buffer *CorrelationBuffer

	// Control
	running atomic.Bool
}

// NewRealtimeEventPipeline creates a real-time event pipeline
func NewRealtimeEventPipeline(capacity uint64, store CorrelationStore) *RealtimeEventPipeline {
	eventRing := NewEventRing(capacity)

	// Create intelligence pipeline (our existing ring buffer pipeline)
	intelligencePipeline, _ := NewRingBufferPipeline()

	// Create correlation persister
	correlationPersister := &CorrelationPersister{
		sourceRing: eventRing,
		store:      store,
		buffer:     NewCorrelationBuffer(1024),
	}

	return &RealtimeEventPipeline{
		eventRing:            eventRing,
		intelligencePipeline: intelligencePipeline,
		correlationPersister: correlationPersister,
	}
}

// ProcessEvent adds event to ring buffer for immediate real-time availability
func (rep *RealtimeEventPipeline) ProcessEvent(event *domain.UnifiedEvent) error {
	// Add to event ring for real-time consumption
	rep.eventRing.Put(event)

	// Also process through intelligence pipeline for correlation
	return rep.intelligencePipeline.ProcessEvent(event)
}

// GetRealtimeEvents provides real-time event stream
func (rep *RealtimeEventPipeline) GetRealtimeEvents(events []*domain.UnifiedEvent) int {
	return rep.eventRing.GetBatch(events)
}

// GetCorrelations provides intelligence findings
func (rep *RealtimeEventPipeline) GetCorrelations(outputs []CorrelationOutput) int {
	if rbPipeline, ok := rep.intelligencePipeline.(*RingBufferPipeline); ok {
		return rbPipeline.GetCorrelationOutputs(outputs)
	}
	return 0
}

// Start begins processing
func (rep *RealtimeEventPipeline) Start() error {
	rep.correlationPersister.running.Store(true)
	return nil
}

// Stop ends processing
func (rep *RealtimeEventPipeline) Stop() error {
	rep.correlationPersister.running.Store(false)
	return nil
}

// GetEventRingMetrics returns ring buffer statistics
func (rep *RealtimeEventPipeline) GetEventRingMetrics() EventRingMetrics {
	return EventRingMetrics{
		Capacity:    rep.eventRing.Capacity(),
		Size:        rep.eventRing.Size(),
		WritePos:    rep.eventRing.writePos.Load(),
		ReadPos:     rep.eventRing.readPos.Load(),
		Utilization: float64(rep.eventRing.Size()) / float64(rep.eventRing.Capacity()),
	}
}

// EventRingMetrics provides ring buffer statistics
type EventRingMetrics struct {
	Capacity    uint64  `json:"capacity"`
	Size        uint64  `json:"size"`
	WritePos    uint64  `json:"write_pos"`
	ReadPos     uint64  `json:"read_pos"`
	Utilization float64 `json:"utilization"`
}
