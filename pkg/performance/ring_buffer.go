package performance

import (
	"errors"
	"runtime"
	"sync/atomic"
	"unsafe"
)

// RingBuffer is a lock-free multi-producer multi-consumer ring buffer
type RingBuffer struct {
	buffer         []unsafe.Pointer
	capacity       uint64
	capacityMask   uint64
	_              [128]byte // padding to prevent false sharing
	writeIndex     atomic.Uint64
	_              [128]byte // padding
	readIndex      atomic.Uint64
	_              [128]byte // padding
}

// NewRingBuffer creates a new ring buffer with the given capacity
// Capacity must be a power of 2
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

// Put adds an item to the ring buffer
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

// Get retrieves an item from the ring buffer
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

// TryPut attempts to add an item without blocking
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

// TryGet attempts to retrieve an item without blocking
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
		
		// Wait for the item to be written
		for i := 0; i < 1000; i++ {
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

// Size returns the current number of items in the buffer
func (r *RingBuffer) Size() uint64 {
	writeIdx := r.writeIndex.Load()
	readIdx := r.readIndex.Load()
	
	if writeIdx >= readIdx {
		return writeIdx - readIdx
	}
	return 0
}

// Capacity returns the capacity of the buffer
func (r *RingBuffer) Capacity() uint64 {
	return r.capacity
}

// IsEmpty returns true if the buffer is empty
func (r *RingBuffer) IsEmpty() bool {
	return r.Size() == 0
}

// IsFull returns true if the buffer is full
func (r *RingBuffer) IsFull() bool {
	return r.Size() >= r.capacity
}

// SPSCRingBuffer is a lock-free single-producer single-consumer ring buffer
// Optimized for single producer/consumer scenarios
type SPSCRingBuffer struct {
	buffer       []unsafe.Pointer
	capacity     uint64
	capacityMask uint64
	_            [128]byte // padding
	writeIndex   uint64
	_            [128]byte // padding
	readIndex    uint64
	_            [128]byte // padding
}

// NewSPSCRingBuffer creates a new SPSC ring buffer
func NewSPSCRingBuffer(capacity uint64) (*SPSCRingBuffer, error) {
	if capacity == 0 || capacity&(capacity-1) != 0 {
		return nil, errors.New("capacity must be a power of 2")
	}

	return &SPSCRingBuffer{
		buffer:       make([]unsafe.Pointer, capacity),
		capacity:     capacity,
		capacityMask: capacity - 1,
	}, nil
}

// Put adds an item (single producer)
func (r *SPSCRingBuffer) Put(item unsafe.Pointer) error {
	writeIdx := atomic.LoadUint64(&r.writeIndex)
	readIdx := atomic.LoadUint64(&r.readIndex)

	if writeIdx-readIdx >= r.capacity {
		return errors.New("ring buffer is full")
	}

	idx := writeIdx & r.capacityMask
	r.buffer[idx] = item
	
	// Ensure write is visible before updating index
	atomic.StoreUint64(&r.writeIndex, writeIdx+1)
	return nil
}

// Get retrieves an item (single consumer)
func (r *SPSCRingBuffer) Get() (unsafe.Pointer, error) {
	readIdx := atomic.LoadUint64(&r.readIndex)
	writeIdx := atomic.LoadUint64(&r.writeIndex)

	if readIdx >= writeIdx {
		return nil, errors.New("ring buffer is empty")
	}

	idx := readIdx & r.capacityMask
	item := r.buffer[idx]
	r.buffer[idx] = nil
	
	// Ensure read is complete before updating index
	atomic.StoreUint64(&r.readIndex, readIdx+1)
	return item, nil
}

// BatchRingBuffer supports batch operations for better performance
type BatchRingBuffer struct {
	*RingBuffer
	batchSize uint64
}

// NewBatchRingBuffer creates a new batch ring buffer
func NewBatchRingBuffer(capacity, batchSize uint64) (*BatchRingBuffer, error) {
	rb, err := NewRingBuffer(capacity)
	if err != nil {
		return nil, err
	}

	return &BatchRingBuffer{
		RingBuffer: rb,
		batchSize:  batchSize,
	}, nil
}

// PutBatch adds multiple items in a batch
func (r *BatchRingBuffer) PutBatch(items []unsafe.Pointer) error {
	n := uint64(len(items))
	if n == 0 {
		return nil
	}

	for {
		writeIdx := r.writeIndex.Load()
		readIdx := r.readIndex.Load()

		// Check if buffer has enough space
		if writeIdx-readIdx+n > r.capacity {
			return errors.New("ring buffer does not have enough space")
		}

		// Try to claim the slots
		if r.writeIndex.CompareAndSwap(writeIdx, writeIdx+n) {
			// Successfully claimed the slots
			for i := uint64(0); i < n; i++ {
				idx := (writeIdx + i) & r.capacityMask
				atomic.StorePointer(&r.buffer[idx], items[i])
			}
			return nil
		}

		runtime.Gosched()
	}
}

// GetBatch retrieves multiple items in a batch
func (r *BatchRingBuffer) GetBatch(items []unsafe.Pointer) int {
	maxItems := uint64(len(items))
	if maxItems == 0 {
		return 0
	}

	for {
		readIdx := r.readIndex.Load()
		writeIdx := r.writeIndex.Load()

		// Calculate available items
		available := writeIdx - readIdx
		if available == 0 {
			return 0
		}

		// Limit to requested size
		if available > maxItems {
			available = maxItems
		}

		// Try to claim the slots
		if r.readIndex.CompareAndSwap(readIdx, readIdx+available) {
			// Successfully claimed the slots
			retrieved := 0
			for i := uint64(0); i < available; i++ {
				idx := (readIdx + i) & r.capacityMask
				
				// Spin wait for item
				for j := 0; j < 1000; j++ {
					item := atomic.LoadPointer(&r.buffer[idx])
					if item != nil {
						atomic.StorePointer(&r.buffer[idx], nil)
						items[retrieved] = item
						retrieved++
						break
					}
					runtime.Gosched()
				}
			}
			return retrieved
		}

		runtime.Gosched()
	}
}