package bpf_common

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// EventPool provides efficient event object pooling to reduce GC pressure
type EventPool struct {
	pool    sync.Pool
	metrics PoolMetrics
}

// PoolMetrics tracks pool usage statistics
type PoolMetrics struct {
	Gets     uint64 // Number of Get operations
	Puts     uint64 // Number of Put operations
	Creates  uint64 // Number of new object creations
	InUse    int64  // Current objects in use
	PoolSize int64  // Current pool size estimate
}

// NewEventPool creates a new event pool
func NewEventPool() *EventPool {
	ep := &EventPool{}
	ep.pool = sync.Pool{
		New: func() interface{} {
			atomic.AddUint64(&ep.metrics.Creates, 1)
			return &domain.RawEvent{
				Metadata: make(map[string]string, 8), // Pre-allocate common metadata size
			}
		},
	}
	return ep
}

// Get retrieves an event from the pool
func (p *EventPool) Get() *domain.RawEvent {
	atomic.AddUint64(&p.metrics.Gets, 1)
	atomic.AddInt64(&p.metrics.InUse, 1)

	event := p.pool.Get().(*domain.RawEvent)

	// Reset the event to clean state
	event.Timestamp = time.Time{}
	event.Type = ""
	event.Data = event.Data[:0] // Keep underlying array, reset length
	event.TraceID = ""
	event.SpanID = ""

	// Clear metadata map but keep underlying map
	for k := range event.Metadata {
		delete(event.Metadata, k)
	}

	return event
}

// Put returns an event to the pool
func (p *EventPool) Put(event *domain.RawEvent) {
	if event == nil {
		return
	}

	atomic.AddUint64(&p.metrics.Puts, 1)
	atomic.AddInt64(&p.metrics.InUse, -1)

	// Don't pool events with very large data slices to avoid memory leaks
	if cap(event.Data) > 64*1024 {
		return
	}

	// Don't pool events with too many metadata entries
	if len(event.Metadata) > 32 {
		event.Metadata = make(map[string]string, 8)
	}

	p.pool.Put(event)
}

// GetMetrics returns current pool metrics
func (p *EventPool) GetMetrics() PoolMetrics {
	return PoolMetrics{
		Gets:     atomic.LoadUint64(&p.metrics.Gets),
		Puts:     atomic.LoadUint64(&p.metrics.Puts),
		Creates:  atomic.LoadUint64(&p.metrics.Creates),
		InUse:    atomic.LoadInt64(&p.metrics.InUse),
		PoolSize: atomic.LoadInt64(&p.metrics.PoolSize),
	}
}

// Global event pool for use across collectors
var globalEventPool = NewEventPool()

// GetEvent retrieves an event from the global pool
func GetEvent() *domain.RawEvent {
	return globalEventPool.Get()
}

// PutEvent returns an event to the global pool
func PutEvent(event *domain.RawEvent) {
	globalEventPool.Put(event)
}

// GetPoolMetrics returns metrics for the global event pool
func GetPoolMetrics() PoolMetrics {
	return globalEventPool.GetMetrics()
}

// BufferPool provides pooled byte buffers for eBPF data parsing
type BufferPool struct {
	pool    sync.Pool
	metrics PoolMetrics
}

// NewBufferPool creates a new buffer pool with specified buffer size
func NewBufferPool(bufferSize int) *BufferPool {
	bp := &BufferPool{}
	bp.pool = sync.Pool{
		New: func() interface{} {
			atomic.AddUint64(&bp.metrics.Creates, 1)
			return make([]byte, bufferSize)
		},
	}
	return bp
}

// Get retrieves a buffer from the pool
func (p *BufferPool) Get() []byte {
	atomic.AddUint64(&p.metrics.Gets, 1)
	atomic.AddInt64(&p.metrics.InUse, 1)

	buf := p.pool.Get().([]byte)
	return buf[:0] // Reset length but keep capacity
}

// Put returns a buffer to the pool
func (p *BufferPool) Put(buf []byte) {
	if buf == nil {
		return
	}

	atomic.AddUint64(&p.metrics.Puts, 1)
	atomic.AddInt64(&p.metrics.InUse, -1)

	// Don't pool buffers that have grown too large
	if cap(buf) > 1024*1024 { // 1MB limit
		return
	}

	p.pool.Put(buf)
}

// Global buffer pools for common sizes
var (
	globalBufferPool   = NewBufferPool(4096)  // 4KB for general use
	globalLargePool    = NewBufferPool(65536) // 64KB for large events
	globalMetadataPool = NewBufferPool(1024)  // 1KB for metadata serialization
)

// GetBuffer retrieves a 4KB buffer from the global pool
func GetBuffer() []byte {
	return globalBufferPool.Get()
}

// PutBuffer returns a buffer to the global pool
func PutBuffer(buf []byte) {
	globalBufferPool.Put(buf)
}

// GetLargeBuffer retrieves a 64KB buffer from the global large pool
func GetLargeBuffer() []byte {
	return globalLargePool.Get()
}

// PutLargeBuffer returns a large buffer to the global pool
func PutLargeBuffer(buf []byte) {
	globalLargePool.Put(buf)
}

// GetMetadataBuffer retrieves a 1KB buffer for metadata operations
func GetMetadataBuffer() []byte {
	return globalMetadataPool.Get()
}

// PutMetadataBuffer returns a metadata buffer to the global pool
func PutMetadataBuffer(buf []byte) {
	globalMetadataPool.Put(buf)
}

// Note: BatchProcessor moved to batch.go to avoid conflicts
