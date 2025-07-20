package performance

import (
	"errors"
	"runtime"
	"sync/atomic"
	"unsafe"
)

// PerCPUBuffer provides per-CPU buffers for high-performance data collection.
// This eliminates cache-line contention between CPUs by giving each CPU its own buffer.
// When a CPU's buffer is full, data overflows to a shared ring buffer.
type PerCPUBuffer struct {
	buffers    []cpuBuffer
	numCPU     int
	bufferSize int

	// Global overflow buffer
	overflow *RingBuffer

	// Aggregation
	aggregator BufferAggregator

	// Metrics
	writes    atomic.Uint64
	reads     atomic.Uint64
	overflows atomic.Uint64
}

// cpuBuffer is a single CPU's buffer with proper cache-line padding
type cpuBuffer struct {
	_      [128]byte // padding to prevent false sharing
	buffer []byte
	head   uint32
	tail   uint32
	size   uint32
	_      [128]byte // padding
}

// BufferAggregator aggregates data from multiple CPU buffers
type BufferAggregator interface {
	Aggregate(buffers [][]byte) ([]byte, error)
}

// PerCPUBufferConfig configures per-CPU buffers
type PerCPUBufferConfig struct {
	BufferSize   int              // Size of each CPU buffer (default: 64KB)
	OverflowSize uint64           // Size of overflow buffer (default: 1MB)
	Aggregator   BufferAggregator // Optional aggregator for data
}

// NewPerCPUBuffer creates a new per-CPU buffer system
func NewPerCPUBuffer(config PerCPUBufferConfig) (*PerCPUBuffer, error) {
	if config.BufferSize == 0 {
		config.BufferSize = 64 * 1024 // 64KB per CPU
	}
	if config.OverflowSize == 0 {
		config.OverflowSize = 1024 * 1024 // 1MB overflow
	}

	numCPU := runtime.GOMAXPROCS(0)
	buffers := make([]cpuBuffer, numCPU)

	for i := range buffers {
		buffers[i].buffer = make([]byte, config.BufferSize)
		buffers[i].size = uint32(config.BufferSize)
	}

	// Ensure overflow size is power of 2
	overflowSize := nextPowerOf2(config.OverflowSize)
	overflow, err := NewRingBuffer(overflowSize)
	if err != nil {
		return nil, err
	}

	return &PerCPUBuffer{
		buffers:    buffers,
		numCPU:     numCPU,
		bufferSize: config.BufferSize,
		overflow:   overflow,
		aggregator: config.Aggregator,
	}, nil
}

// Write writes data to the current CPU's buffer
func (b *PerCPUBuffer) Write(data []byte) error {
	if len(data) == 0 {
		return nil
	}

	b.writes.Add(1)

	// Get current CPU buffer
	// Note: In production, use runtime.Pinner or CPUID instruction
	// For now, we'll use a simple hash based on goroutine
	cpu := int(uintptr(unsafe.Pointer(&data))>>12) % b.numCPU

	buffer := &b.buffers[cpu]

	// Try to write to CPU buffer
	if !b.writeToBuffer(buffer, data) {
		// Buffer full, try overflow
		b.overflows.Add(1)

		// Allocate in overflow buffer
		overflow := make([]byte, len(data))
		copy(overflow, data)

		if err := b.overflow.Put(unsafe.Pointer(&overflow)); err != nil {
			return errors.New("per-CPU buffer overflow")
		}
	}

	return nil
}

// writeToBuffer attempts to write to a specific buffer
func (b *PerCPUBuffer) writeToBuffer(buffer *cpuBuffer, data []byte) bool {
	dataLen := uint32(len(data))

	for {
		head := atomic.LoadUint32(&buffer.head)
		tail := atomic.LoadUint32(&buffer.tail)

		// Calculate available space
		var available uint32
		if head >= tail {
			available = buffer.size - head + tail
		} else {
			available = tail - head
		}

		// Need space for length header + data
		needed := 4 + dataLen
		if available < needed {
			return false
		}

		// Try to claim space
		newHead := (head + needed) % buffer.size
		if !atomic.CompareAndSwapUint32(&buffer.head, head, newHead) {
			continue
		}

		// Write length header
		b.writeUint32(buffer, head, dataLen)
		head = (head + 4) % buffer.size

		// Write data (may wrap around)
		if head+dataLen <= buffer.size {
			// No wrap
			copy(buffer.buffer[head:], data)
		} else {
			// Wrap around
			firstPart := buffer.size - head
			copy(buffer.buffer[head:], data[:firstPart])
			copy(buffer.buffer[0:], data[firstPart:])
		}

		return true
	}
}

// writeUint32 writes a uint32 to the buffer
func (b *PerCPUBuffer) writeUint32(buffer *cpuBuffer, offset uint32, value uint32) {
	offset = offset % buffer.size
	buffer.buffer[offset] = byte(value)
	buffer.buffer[(offset+1)%buffer.size] = byte(value >> 8)
	buffer.buffer[(offset+2)%buffer.size] = byte(value >> 16)
	buffer.buffer[(offset+3)%buffer.size] = byte(value >> 24)
}

// readUint32 reads a uint32 from the buffer
func (b *PerCPUBuffer) readUint32(buffer *cpuBuffer, offset uint32) uint32 {
	offset = offset % buffer.size
	return uint32(buffer.buffer[offset]) |
		uint32(buffer.buffer[(offset+1)%buffer.size])<<8 |
		uint32(buffer.buffer[(offset+2)%buffer.size])<<16 |
		uint32(buffer.buffer[(offset+3)%buffer.size])<<24
}

// Read reads all data from all CPU buffers
func (b *PerCPUBuffer) Read() ([][]byte, error) {
	b.reads.Add(1)

	results := make([][]byte, 0, b.numCPU+int(b.overflow.Size()))

	// Read from each CPU buffer
	for i := range b.buffers {
		data := b.readFromBuffer(&b.buffers[i])
		results = append(results, data...)
	}

	// Read from overflow
	for {
		ptr, err := b.overflow.Get()
		if err != nil {
			break
		}

		data := *(*[]byte)(ptr)
		results = append(results, data)
	}

	return results, nil
}

// readFromBuffer reads all data from a specific buffer
func (b *PerCPUBuffer) readFromBuffer(buffer *cpuBuffer) [][]byte {
	var results [][]byte

	for {
		tail := atomic.LoadUint32(&buffer.tail)
		head := atomic.LoadUint32(&buffer.head)

		if tail == head {
			// Buffer empty
			break
		}

		// Read length header
		length := b.readUint32(buffer, tail)
		if length == 0 || length > buffer.size {
			// Corrupted data, reset buffer
			atomic.StoreUint32(&buffer.tail, head)
			break
		}

		// Allocate result
		data := make([]byte, length)

		// Read data
		dataStart := (tail + 4) % buffer.size
		if dataStart+length <= buffer.size {
			// No wrap
			copy(data, buffer.buffer[dataStart:dataStart+length])
		} else {
			// Wrap around
			firstPart := buffer.size - dataStart
			copy(data[:firstPart], buffer.buffer[dataStart:])
			copy(data[firstPart:], buffer.buffer[:length-firstPart])
		}

		// Update tail
		newTail := (tail + 4 + length) % buffer.size
		atomic.StoreUint32(&buffer.tail, newTail)

		results = append(results, data)
	}

	return results
}

// Aggregate aggregates all data using the configured aggregator
func (b *PerCPUBuffer) Aggregate() ([]byte, error) {
	if b.aggregator == nil {
		return nil, errors.New("no aggregator configured")
	}

	data, err := b.Read()
	if err != nil {
		return nil, err
	}

	return b.aggregator.Aggregate(data)
}

// Reset resets all buffers
func (b *PerCPUBuffer) Reset() {
	for i := range b.buffers {
		atomic.StoreUint32(&b.buffers[i].head, 0)
		atomic.StoreUint32(&b.buffers[i].tail, 0)
	}

	// Clear overflow
	for {
		if _, err := b.overflow.Get(); err != nil {
			break
		}
	}
}

// GetMetrics returns buffer metrics
func (b *PerCPUBuffer) GetMetrics() PerCPUBufferMetrics {
	metrics := PerCPUBufferMetrics{
		Writes:     b.writes.Load(),
		Reads:      b.reads.Load(),
		Overflows:  b.overflows.Load(),
		CPUMetrics: make([]CPUBufferMetrics, b.numCPU),
	}

	for i := range b.buffers {
		buffer := &b.buffers[i]
		head := atomic.LoadUint32(&buffer.head)
		tail := atomic.LoadUint32(&buffer.tail)

		var used uint32
		if head >= tail {
			used = head - tail
		} else {
			used = buffer.size - tail + head
		}

		metrics.CPUMetrics[i] = CPUBufferMetrics{
			CPU:         i,
			Used:        used,
			Capacity:    buffer.size,
			Utilization: float64(used) / float64(buffer.size),
		}
	}

	metrics.OverflowSize = b.overflow.Size()

	return metrics
}

// PerCPUBufferMetrics contains per-CPU buffer metrics
type PerCPUBufferMetrics struct {
	Writes       uint64
	Reads        uint64
	Overflows    uint64
	CPUMetrics   []CPUBufferMetrics
	OverflowSize uint64
}

// CPUBufferMetrics contains metrics for a single CPU buffer
type CPUBufferMetrics struct {
	CPU         int
	Used        uint32
	Capacity    uint32
	Utilization float64
}

// SimpleAggregator concatenates all buffers
type SimpleAggregator struct{}

// Aggregate concatenates all buffer data
func (a *SimpleAggregator) Aggregate(buffers [][]byte) ([]byte, error) {
	totalSize := 0
	for _, buf := range buffers {
		totalSize += len(buf)
	}

	result := make([]byte, 0, totalSize)
	for _, buf := range buffers {
		result = append(result, buf...)
	}

	return result, nil
}

// nextPowerOf2 returns the next power of 2
func nextPowerOf2(n uint64) uint64 {
	n--
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	n |= n >> 32
	n++
	return n
}
