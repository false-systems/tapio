package pipeline

import (
	"sync"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// TestEventRingAtomicMethods verifies that the new atomic method-based API works correctly
func TestEventRingAtomicMethods(t *testing.T) {
	ring := NewEventRing(64)

	// Test atomic Load/Store methods
	// Initial state
	if ring.writePos.Load() != 0 {
		t.Errorf("Expected initial writePos 0, got %d", ring.writePos.Load())
	}
	if ring.readPos.Load() != 0 {
		t.Errorf("Expected initial readPos 0, got %d", ring.readPos.Load())
	}

	// Test Put increments writePos atomically
	event := &domain.UnifiedEvent{
		ID:        "test-1",
		Timestamp: time.Now(),
		Type:      domain.EventTypeMetric,
		Source:    "test",
	}
	ring.Put(event)

	if ring.writePos.Load() != 1 {
		t.Errorf("Expected writePos 1 after Put, got %d", ring.writePos.Load())
	}

	// Test Get increments readPos atomically
	retrieved := ring.Get()
	if retrieved == nil {
		t.Error("Expected to retrieve event")
	}
	if ring.readPos.Load() != 1 {
		t.Errorf("Expected readPos 1 after Get, got %d", ring.readPos.Load())
	}

	// Test Reset sets both positions to 0
	ring.Reset()
	if ring.writePos.Load() != 0 {
		t.Errorf("Expected writePos 0 after Reset, got %d", ring.writePos.Load())
	}
	if ring.readPos.Load() != 0 {
		t.Errorf("Expected readPos 0 after Reset, got %d", ring.readPos.Load())
	}
}

// TestEventRingConcurrentAccess verifies atomic operations work correctly under concurrent access
func TestEventRingConcurrentAccess(t *testing.T) {
	ring := NewEventRing(1024)
	numWriters := 10
	numEvents := 100
	var wg sync.WaitGroup

	// Start concurrent writers
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			for i := 0; i < numEvents; i++ {
				event := &domain.UnifiedEvent{
					ID:        string(rune('A'+writerID)) + string(rune('0'+i)),
					Timestamp: time.Now(),
					Type:      domain.EventTypeMetric,
					Source:    "writer",
				}
				ring.Put(event)
			}
		}(w)
	}

	// Start concurrent reader
	readCount := 0
	wg.Add(1)
	go func() {
		defer wg.Done()
		for readCount < numWriters*numEvents {
			if event := ring.Get(); event != nil {
				readCount++
			} else {
				// Small delay if no events available
				time.Sleep(time.Microsecond)
			}
		}
	}()

	wg.Wait()

	// Verify all events were processed
	if readCount != numWriters*numEvents {
		t.Errorf("Expected to read %d events, got %d", numWriters*numEvents, readCount)
	}

	// Ring should be empty now
	if ring.Size() != 0 {
		t.Errorf("Expected empty ring, got size %d", ring.Size())
	}
}
