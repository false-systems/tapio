package performance

import (
	"errors"
	"sync/atomic"
	"unsafe"
)

var (
	ErrBufferFull  = errors.New("ring buffer is full")
	ErrBufferEmpty = errors.New("ring buffer is empty")
)

// RingBuffer is a lock-free multi-producer multi-consumer ring buffer
type RingBuffer struct {
	_        [64]byte // cache line padding
	capacity uint64
	mask     uint64
	_        [56]byte // padding to separate capacity/mask from positions

	head uint64
	_    [56]byte // padding between head and tail

	tail uint64
	_    [56]byte // padding

	data []unsafe.Pointer
}

// NewRingBuffer creates a new ring buffer with the given capacity
// Capacity must be a power of 2
func NewRingBuffer(capacity uint64) (*RingBuffer, error) {
	if capacity == 0 || (capacity&(capacity-1)) != 0 {
		return nil, errors.New("capacity must be a power of 2")
	}

	return &RingBuffer{
		capacity: capacity,
		mask:     capacity - 1,
		data:     make([]unsafe.Pointer, capacity),
	}, nil
}

// Put adds an item to the ring buffer
func (rb *RingBuffer) Put(item unsafe.Pointer) error {
	var head, tail uint64

	for {
		head = atomic.LoadUint64(&rb.head)
		tail = atomic.LoadUint64(&rb.tail)

		if head-tail >= rb.capacity {
			return ErrBufferFull
		}

		if atomic.CompareAndSwapUint64(&rb.head, head, head+1) {
			break
		}
	}

	rb.data[head&rb.mask] = item
	return nil
}

// Get retrieves an item from the ring buffer
func (rb *RingBuffer) Get() (unsafe.Pointer, error) {
	var head, tail uint64

	for {
		head = atomic.LoadUint64(&rb.head)
		tail = atomic.LoadUint64(&rb.tail)

		if tail >= head {
			return nil, ErrBufferEmpty
		}

		item := rb.data[tail&rb.mask]
		if item == nil {
			continue
		}

		if atomic.CompareAndSwapUint64(&rb.tail, tail, tail+1) {
			return item, nil
		}
	}
}

// TryPut attempts to add an item without blocking
func (rb *RingBuffer) TryPut(item unsafe.Pointer) bool {
	head := atomic.LoadUint64(&rb.head)
	tail := atomic.LoadUint64(&rb.tail)

	if head-tail >= rb.capacity {
		return false
	}

	if !atomic.CompareAndSwapUint64(&rb.head, head, head+1) {
		return false
	}

	rb.data[head&rb.mask] = item
	return true
}

// Size returns the current number of items in the buffer
func (rb *RingBuffer) Size() uint64 {
	head := atomic.LoadUint64(&rb.head)
	tail := atomic.LoadUint64(&rb.tail)
	return head - tail
}

// Capacity returns the capacity of the buffer
func (rb *RingBuffer) Capacity() uint64 {
	return rb.capacity
}

// Stats returns buffer statistics
type BufferStats struct {
	Size        uint64
	Capacity    uint64
	Utilization float64
}

func (rb *RingBuffer) GetStats() BufferStats {
	size := rb.Size()
	return BufferStats{
		Size:        size,
		Capacity:    rb.capacity,
		Utilization: float64(size) / float64(rb.capacity),
	}
}
