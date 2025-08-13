package behavior

import (
	"sync/atomic"
)

// BackpressureManager manages system load and prevents overload
type BackpressureManager struct {
	maxSize     int32
	currentSize atomic.Int32
}

// NewBackpressureManager creates a new backpressure manager
func NewBackpressureManager(maxSize int) *BackpressureManager {
	return &BackpressureManager{
		maxSize: int32(maxSize),
	}
}

// TryAccept attempts to accept a new request
func (b *BackpressureManager) TryAccept() bool {
	current := b.currentSize.Add(1)
	if current > b.maxSize {
		b.currentSize.Add(-1)
		return false
	}
	return true
}

// Release releases a slot
func (b *BackpressureManager) Release() {
	b.currentSize.Add(-1)
}

// Usage returns the current usage percentage (0.0 to 1.0)
func (b *BackpressureManager) Usage() float64 {
	current := float64(b.currentSize.Load())
	max := float64(b.maxSize)
	if max == 0 {
		return 0
	}
	return current / max
}

// Available returns the number of available slots
func (b *BackpressureManager) Available() int32 {
	current := b.currentSize.Load()
	return b.maxSize - current
}

// Reset resets the backpressure manager
func (b *BackpressureManager) Reset() {
	b.currentSize.Store(0)
}
