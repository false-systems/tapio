// Package performance provides high-performance data structures and utilities
// for the Tapio observability platform. These components are designed to handle
// high-throughput event processing with minimal overhead.
package performance

import (
	"errors"
	"runtime"
	"sync/atomic"
	"unsafe"
)

// RingBuffer is a lock-free multi-producer multi-consumer ring buffer
// optimized for high-throughput event processing.
//
// Features:
//   - Lock-free MPMC (Multi-Producer Multi-Consumer) design
//   - Cache-line padding to prevent false sharing
//   - Power-of-2 capacity for efficient modulo operations
//   - Zero-allocation operations
//
// Usage:
//
//	rb, _ := NewRingBuffer(65536)
//
//	// Producer
//	event := &UnifiedEvent{...}
//	rb.Put(unsafe.Pointer(event))
//
//	// Consumer
//	ptr, _ := rb.Get()
//	event := (*UnifiedEvent)(ptr)
type RingBuffer struct {
	buffer       []unsafe.Pointer
	capacity     uint64
	capacityMask uint64
	_            [128]byte // padding to prevent false sharing
	writeIndex   atomic.Uint64
	_            [128]byte // padding
	readIndex    atomic.Uint64
	_            [128]byte // padding
}

// NewRingBuffer creates a new ring buffer with the given capacity.
// Capacity must be a power of 2 for performance reasons.
func NewRingBuffer(capacity uint64) (*RingBuffer, error) {
	if capacity == 0 || capacity&(capacity-1) != 0 {
		return nil, errors.New("capacity must be a power of 2")
	}

	return &RingBuffer{
		buffer:       make([]unsafe.Pointer, capacity),
		capacity:     capacity,
		capacityMask: capacity - 1,
	}, nil
}

// Put adds an item to the ring buffer.
// Returns an error if the buffer is full.
func (r *RingBuffer) Put(item unsafe.Pointer) error {
	for {
		writeIdx := r.writeIndex.Load()
		readIdx := r.readIndex.Load()

		// Check if buffer is full
		if writeIdx-readIdx >= r.capacity {
			return errors.New("ring buffer is full")
		}

		// Try to claim the slot
		if r.writeIndex.CompareAndSwap(writeIdx, writeIdx+1) {
			// Successfully claimed the slot
			idx := writeIdx & r.capacityMask
			atomic.StorePointer(&r.buffer[idx], item)
			return nil
		}

		// Failed to claim slot, retry
		runtime.Gosched()
	}
}

// Get retrieves an item from the ring buffer.
// Returns an error if the buffer is empty.
func (r *RingBuffer) Get() (unsafe.Pointer, error) {
	for {
		readIdx := r.readIndex.Load()
		writeIdx := r.writeIndex.Load()

		// Check if buffer is empty
		if readIdx >= writeIdx {
			return nil, errors.New("ring buffer is empty")
		}

		// Try to claim the slot
		if r.readIndex.CompareAndSwap(readIdx, readIdx+1) {
			// Successfully claimed the slot
			idx := readIdx & r.capacityMask

			// Spin wait for the item to be written
			for {
				item := atomic.LoadPointer(&r.buffer[idx])
				if item != nil {
					// Clear the slot and return the item
					atomic.StorePointer(&r.buffer[idx], nil)
					return item, nil
				}
				runtime.Gosched()
			}
		}

		// Failed to claim slot, retry
		runtime.Gosched()
	}
}

// TryPut attempts to add an item without blocking.
// Returns true if successful, false if the buffer is full.
func (r *RingBuffer) TryPut(item unsafe.Pointer) bool {
	writeIdx := r.writeIndex.Load()
	readIdx := r.readIndex.Load()

	// Check if buffer is full
	if writeIdx-readIdx >= r.capacity {
		return false
	}

	// Try to claim the slot
	if r.writeIndex.CompareAndSwap(writeIdx, writeIdx+1) {
		idx := writeIdx & r.capacityMask
		atomic.StorePointer(&r.buffer[idx], item)
		return true
	}

	return false
}

// TryGet attempts to retrieve an item without blocking.
// Returns the item and true if successful, nil and false if empty.
func (r *RingBuffer) TryGet() (unsafe.Pointer, bool) {
	readIdx := r.readIndex.Load()
	writeIdx := r.writeIndex.Load()

	// Check if buffer is empty
	if readIdx >= writeIdx {
		return nil, false
	}

	// Try to claim the slot
	if r.readIndex.CompareAndSwap(readIdx, readIdx+1) {
		idx := readIdx & r.capacityMask

		// Spin wait for the item to be written
		for {
			item := atomic.LoadPointer(&r.buffer[idx])
			if item != nil {
				atomic.StorePointer(&r.buffer[idx], nil)
				return item, true
			}
			runtime.Gosched()
		}
	}

	return nil, false
}

// PutBatch adds multiple items to the ring buffer in a single operation.
// Returns the number of items successfully added.
func (r *RingBuffer) PutBatch(items []unsafe.Pointer) int {
	if len(items) == 0 {
		return 0
	}

	added := 0
	for {
		writeIdx := r.writeIndex.Load()
		readIdx := r.readIndex.Load()

		// Calculate available space
		available := r.capacity - (writeIdx - readIdx)
		if available == 0 {
			return added
		}

		// Determine how many items we can add
		toAdd := uint64(len(items) - added)
		if toAdd > available {
			toAdd = available
		}

		// Try to claim slots
		if r.writeIndex.CompareAndSwap(writeIdx, writeIdx+toAdd) {
			// Successfully claimed slots, now write items
			for i := uint64(0); i < toAdd; i++ {
				idx := (writeIdx + i) & r.capacityMask
				atomic.StorePointer(&r.buffer[idx], items[added+int(i)])
			}
			added += int(toAdd)

			// Check if we've added all items
			if added >= len(items) {
				return added
			}
		}

		// Failed to claim slots or need to add more, retry
		runtime.Gosched()
	}
}

// GetBatch retrieves multiple items from the ring buffer in a single operation.
// Returns the number of items successfully retrieved.
func (r *RingBuffer) GetBatch(items []unsafe.Pointer) int {
	if len(items) == 0 {
		return 0
	}

	retrieved := 0
	for {
		readIdx := r.readIndex.Load()
		writeIdx := r.writeIndex.Load()

		// Calculate available items
		available := writeIdx - readIdx
		if available == 0 {
			return retrieved
		}

		// Determine how many items we can get
		toGet := uint64(len(items) - retrieved)
		if toGet > available {
			toGet = available
		}

		// Try to claim slots
		if r.readIndex.CompareAndSwap(readIdx, readIdx+toGet) {
			// Successfully claimed slots, now read items
			for i := uint64(0); i < toGet; i++ {
				idx := (readIdx + i) & r.capacityMask

				// Spin wait for the item to be written
				for {
					item := atomic.LoadPointer(&r.buffer[idx])
					if item != nil {
						items[retrieved+int(i)] = item
						atomic.StorePointer(&r.buffer[idx], nil)
						break
					}
					runtime.Gosched()
				}
			}
			retrieved += int(toGet)

			// Check if we've retrieved enough items
			if retrieved >= len(items) {
				return retrieved
			}
		}

		// Failed to claim slots or need to get more, retry
		runtime.Gosched()
	}
}

// Size returns the current number of items in the buffer
func (r *RingBuffer) Size() uint64 {
	writeIdx := r.writeIndex.Load()
	readIdx := r.readIndex.Load()
	if writeIdx >= readIdx {
		return writeIdx - readIdx
	}
	return 0
}

// Capacity returns the maximum capacity of the buffer
func (r *RingBuffer) Capacity() uint64 {
	return r.capacity
}

// IsEmpty returns true if the buffer is empty
func (r *RingBuffer) IsEmpty() bool {
	return r.readIndex.Load() >= r.writeIndex.Load()
}

// IsFull returns true if the buffer is full
func (r *RingBuffer) IsFull() bool {
	writeIdx := r.writeIndex.Load()
	readIdx := r.readIndex.Load()
	return writeIdx-readIdx >= r.capacity
}

// Clear empties the buffer
// Note: This is not thread-safe and should only be called when no other
// goroutines are accessing the buffer
func (r *RingBuffer) Clear() {
	r.writeIndex.Store(0)
	r.readIndex.Store(0)
	for i := range r.buffer {
		r.buffer[i] = nil
	}
}

// Stats returns statistics about the ring buffer
type RingBufferStats struct {
	Capacity   uint64
	Size       uint64
	WriteIndex uint64
	ReadIndex  uint64
	IsFull     bool
	IsEmpty    bool
}

// GetStats returns current statistics about the ring buffer
func (r *RingBuffer) GetStats() RingBufferStats {
	writeIdx := r.writeIndex.Load()
	readIdx := r.readIndex.Load()
	size := uint64(0)
	if writeIdx >= readIdx {
		size = writeIdx - readIdx
	}

	return RingBufferStats{
		Capacity:   r.capacity,
		Size:       size,
		WriteIndex: writeIdx,
		ReadIndex:  readIdx,
		IsFull:     size >= r.capacity,
		IsEmpty:    size == 0,
	}
}
