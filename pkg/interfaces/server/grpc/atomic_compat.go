package grpc

import (
	"math"
	"sync/atomic"
)

// AtomicFloat64 compatibility wrapper for atomic float64 operations
type AtomicFloat64 struct {
	bits atomic.Uint64
}

// Load returns the float64 value
func (f *AtomicFloat64) Load() float64 {
	return math.Float64frombits(f.bits.Load())
}

// Store sets the float64 value
func (f *AtomicFloat64) Store(val float64) {
	f.bits.Store(math.Float64bits(val))
}

// Add adds the delta value and returns the new value
func (f *AtomicFloat64) Add(delta float64) float64 {
	for {
		old := f.bits.Load()
		oldFloat := math.Float64frombits(old)
		newFloat := oldFloat + delta
		newBits := math.Float64bits(newFloat)
		if f.bits.CompareAndSwap(old, newBits) {
			return newFloat
		}
	}
}

// CompareAndSwap performs atomic compare-and-swap
func (f *AtomicFloat64) CompareAndSwap(old, new float64) bool {
	return f.bits.CompareAndSwap(math.Float64bits(old), math.Float64bits(new))
}
