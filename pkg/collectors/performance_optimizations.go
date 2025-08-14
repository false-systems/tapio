// Package collectors provides high-performance optimizations for event collection
package collectors

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

// PerformanceOptimizations contains system-wide performance enhancements
type PerformanceOptimizations struct {
	// Memory pools for zero-allocation patterns
	eventPool    *sync.Pool
	bufferPool   *sync.Pool
	metadataPool *sync.Pool

	// Lock-free data structures
	ringBuffer     *LockFreeRingBuffer
	batchProcessor *BatchProcessor

	// CPU cache optimization
	cacheLineSize int
	padded        [64]byte // CPU cache line padding

	// Performance metrics
	allocations      atomic.Uint64
	poolHits         atomic.Uint64
	poolMisses       atomic.Uint64
	batchesProcessed atomic.Uint64
}

// LockFreeRingBuffer implements a lock-free ring buffer for high-throughput scenarios
type LockFreeRingBuffer struct {
	buffer   []unsafe.Pointer
	mask     uint64
	_        [56]byte // Padding to prevent false sharing
	head     atomic.Uint64
	_        [56]byte // Padding
	tail     atomic.Uint64
	_        [56]byte // Padding
	capacity uint64
}

// NewLockFreeRingBuffer creates a new lock-free ring buffer
func NewLockFreeRingBuffer(size uint64) *LockFreeRingBuffer {
	// Ensure size is power of 2 for fast modulo operations
	capacity := uint64(1)
	for capacity < size {
		capacity <<= 1
	}

	return &LockFreeRingBuffer{
		buffer:   make([]unsafe.Pointer, capacity),
		mask:     capacity - 1,
		capacity: capacity,
	}
}

// Push adds an item to the ring buffer (lock-free)
func (rb *LockFreeRingBuffer) Push(item unsafe.Pointer) bool {
	for {
		head := rb.head.Load()
		tail := rb.tail.Load()

		// Check if buffer is full
		if head-tail >= rb.capacity {
			return false
		}

		// Try to claim the slot
		if rb.head.CompareAndSwap(head, head+1) {
			// Successfully claimed slot, write data
			rb.buffer[head&rb.mask] = item
			return true
		}
		// Retry on contention
		runtime.Gosched()
	}
}

// Pop removes an item from the ring buffer (lock-free)
func (rb *LockFreeRingBuffer) Pop() unsafe.Pointer {
	for {
		tail := rb.tail.Load()
		head := rb.head.Load()

		// Check if buffer is empty
		if tail >= head {
			return nil
		}

		// Read the data first
		item := rb.buffer[tail&rb.mask]

		// Try to advance tail
		if rb.tail.CompareAndSwap(tail, tail+1) {
			return item
		}
		// Retry on contention
		runtime.Gosched()
	}
}

// BatchProcessor implements efficient batch processing for events
type BatchProcessor struct {
	batchSize     int
	flushInterval time.Duration
	processor     func([]RawEvent)

	mu        sync.Mutex
	batch     []RawEvent
	lastFlush time.Time

	// Zero-copy optimization
	directBuffer []byte
	bufferOffset int
}

// NewBatchProcessor creates a new batch processor
func NewBatchProcessor(batchSize int, flushInterval time.Duration, processor func([]RawEvent)) *BatchProcessor {
	return &BatchProcessor{
		batchSize:     batchSize,
		flushInterval: flushInterval,
		processor:     processor,
		batch:         make([]RawEvent, 0, batchSize),
		lastFlush:     time.Now(),
		directBuffer:  make([]byte, 1<<20), // 1MB direct buffer
	}
}

// Add adds an event to the batch
func (bp *BatchProcessor) Add(event RawEvent) {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	bp.batch = append(bp.batch, event)

	// Check if we should flush
	shouldFlush := len(bp.batch) >= bp.batchSize ||
		time.Since(bp.lastFlush) >= bp.flushInterval

	if shouldFlush {
		bp.flushLocked()
	}
}

// flushLocked flushes the batch (must be called with lock held)
func (bp *BatchProcessor) flushLocked() {
	if len(bp.batch) == 0 {
		return
	}

	// Process batch
	bp.processor(bp.batch)

	// Reset batch - reuse backing array
	bp.batch = bp.batch[:0]
	bp.lastFlush = time.Now()
}

// EventPool provides zero-allocation event management
type EventPool struct {
	pool *sync.Pool
}

// NewEventPool creates a new event pool
func NewEventPool() *EventPool {
	return &EventPool{
		pool: &sync.Pool{
			New: func() interface{} {
				return &RawEvent{
					Metadata: make(map[string]string, 16), // Pre-allocate reasonable size
				}
			},
		},
	}
}

// Get retrieves an event from the pool
func (ep *EventPool) Get() *RawEvent {
	event := ep.pool.Get().(*RawEvent)
	// Reset event but keep allocated map
	event.Timestamp = time.Time{}
	event.Type = ""
	event.Data = event.Data[:0] // Reset slice but keep capacity
	event.TraceID = ""
	event.SpanID = ""
	// Clear map entries but keep allocated map
	for k := range event.Metadata {
		delete(event.Metadata, k)
	}
	return event
}

// Put returns an event to the pool
func (ep *EventPool) Put(event *RawEvent) {
	// Don't pool events with huge data buffers
	if cap(event.Data) > 64*1024 {
		return
	}
	ep.pool.Put(event)
}

// BufferPool provides zero-allocation buffer management
type BufferPool struct {
	small  *sync.Pool // 4KB buffers
	medium *sync.Pool // 64KB buffers
	large  *sync.Pool // 1MB buffers
}

// NewBufferPool creates a new buffer pool
func NewBufferPool() *BufferPool {
	return &BufferPool{
		small: &sync.Pool{
			New: func() interface{} {
				b := make([]byte, 4096)
				return &b
			},
		},
		medium: &sync.Pool{
			New: func() interface{} {
				b := make([]byte, 65536)
				return &b
			},
		},
		large: &sync.Pool{
			New: func() interface{} {
				b := make([]byte, 1048576)
				return &b
			},
		},
	}
}

// Get retrieves a buffer of appropriate size
func (bp *BufferPool) Get(size int) []byte {
	var pool *sync.Pool
	var bufSize int

	switch {
	case size <= 4096:
		pool = bp.small
		bufSize = 4096
	case size <= 65536:
		pool = bp.medium
		bufSize = 65536
	default:
		pool = bp.large
		bufSize = 1048576
	}

	bufPtr := pool.Get().(*[]byte)
	buf := *bufPtr

	// If requested size is larger than pool buffer, allocate new
	if size > bufSize {
		return make([]byte, size)
	}

	return buf[:size]
}

// Put returns a buffer to the pool
func (bp *BufferPool) Put(buf []byte) {
	size := cap(buf)
	// Reset buffer
	buf = buf[:0]

	switch size {
	case 4096:
		bp.small.Put(&buf)
	case 65536:
		bp.medium.Put(&buf)
	case 1048576:
		bp.large.Put(&buf)
	default:
		// Don't pool non-standard sizes
	}
}

// ParallelEventProcessor processes events in parallel with work stealing
type ParallelEventProcessor struct {
	numWorkers int
	queues     []*LockFreeRingBuffer
	workers    []worker
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

type worker struct {
	id        int
	queue     *LockFreeRingBuffer
	processor func(RawEvent)
	stats     workerStats
}

type workerStats struct {
	processed atomic.Uint64
	stolen    atomic.Uint64
	idle      atomic.Uint64
}

// NewParallelEventProcessor creates a new parallel processor
func NewParallelEventProcessor(numWorkers int, processor func(RawEvent)) *ParallelEventProcessor {
	if numWorkers <= 0 {
		numWorkers = runtime.NumCPU()
	}

	p := &ParallelEventProcessor{
		numWorkers: numWorkers,
		queues:     make([]*LockFreeRingBuffer, numWorkers),
		workers:    make([]worker, numWorkers),
	}

	// Create per-worker queues
	for i := 0; i < numWorkers; i++ {
		p.queues[i] = NewLockFreeRingBuffer(1024)
		p.workers[i] = worker{
			id:        i,
			queue:     p.queues[i],
			processor: processor,
		}
	}

	return p
}

// Start starts the parallel processor
func (p *ParallelEventProcessor) Start(ctx context.Context) {
	p.ctx, p.cancel = context.WithCancel(ctx)

	for i := range p.workers {
		p.wg.Add(1)
		go p.runWorker(&p.workers[i])
	}
}

// Stop stops the parallel processor
func (p *ParallelEventProcessor) Stop() {
	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()
}

// Submit submits an event for processing
func (p *ParallelEventProcessor) Submit(event RawEvent) bool {
	// Hash-based distribution for better cache locality
	workerID := hashToWorker(event.TraceID, p.numWorkers)

	// Try primary queue first
	eventPtr := unsafe.Pointer(&event)
	if p.queues[workerID].Push(eventPtr) {
		return true
	}

	// Try other queues if primary is full (work distribution)
	for i := 0; i < p.numWorkers; i++ {
		if p.queues[i].Push(eventPtr) {
			return true
		}
	}

	return false
}

// runWorker runs a worker goroutine
func (p *ParallelEventProcessor) runWorker(w *worker) {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			return
		default:
		}

		// Process from own queue first
		if eventPtr := w.queue.Pop(); eventPtr != nil {
			event := *(*RawEvent)(eventPtr)
			w.processor(event)
			w.stats.processed.Add(1)
			continue
		}

		// Work stealing: try to steal from other queues
		stolen := false
		for i := 0; i < p.numWorkers; i++ {
			if i == w.id {
				continue
			}
			if eventPtr := p.queues[i].Pop(); eventPtr != nil {
				event := *(*RawEvent)(eventPtr)
				w.processor(event)
				w.stats.stolen.Add(1)
				stolen = true
				break
			}
		}

		if !stolen {
			w.stats.idle.Add(1)
			runtime.Gosched()
		}
	}
}

// hashToWorker hashes a string to a worker ID
func hashToWorker(s string, numWorkers int) int {
	var hash uint32
	for i := 0; i < len(s); i++ {
		hash = hash*31 + uint32(s[i])
	}
	return int(hash % uint32(numWorkers))
}

// MemoryEfficientCache implements an LRU cache with minimal allocations
type MemoryEfficientCache struct {
	capacity int
	items    map[string]*cacheItem
	head     *cacheItem
	tail     *cacheItem
	mu       sync.RWMutex
	pool     *sync.Pool
}

type cacheItem struct {
	key   string
	value interface{}
	prev  *cacheItem
	next  *cacheItem
}

// NewMemoryEfficientCache creates a new cache
func NewMemoryEfficientCache(capacity int) *MemoryEfficientCache {
	cache := &MemoryEfficientCache{
		capacity: capacity,
		items:    make(map[string]*cacheItem, capacity),
		pool: &sync.Pool{
			New: func() interface{} {
				return &cacheItem{}
			},
		},
	}

	// Initialize sentinel nodes
	cache.head = &cacheItem{}
	cache.tail = &cacheItem{}
	cache.head.next = cache.tail
	cache.tail.prev = cache.head

	return cache
}

// Get retrieves a value from the cache
func (c *MemoryEfficientCache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	item, exists := c.items[key]
	c.mu.RUnlock()

	if !exists {
		return nil, false
	}

	// Move to front (LRU)
	c.mu.Lock()
	c.moveToFront(item)
	c.mu.Unlock()

	return item.value, true
}

// Set adds or updates a value in the cache
func (c *MemoryEfficientCache) Set(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if item, exists := c.items[key]; exists {
		item.value = value
		c.moveToFront(item)
		return
	}

	// Add new item
	item := c.pool.Get().(*cacheItem)
	item.key = key
	item.value = value

	c.items[key] = item
	c.addToFront(item)

	// Evict if over capacity
	if len(c.items) > c.capacity {
		c.evictLRU()
	}
}

// moveToFront moves an item to the front of the LRU list
func (c *MemoryEfficientCache) moveToFront(item *cacheItem) {
	c.removeFromList(item)
	c.addToFront(item)
}

// addToFront adds an item to the front of the LRU list
func (c *MemoryEfficientCache) addToFront(item *cacheItem) {
	item.prev = c.head
	item.next = c.head.next
	c.head.next.prev = item
	c.head.next = item
}

// removeFromList removes an item from the LRU list
func (c *MemoryEfficientCache) removeFromList(item *cacheItem) {
	item.prev.next = item.next
	item.next.prev = item.prev
}

// evictLRU evicts the least recently used item
func (c *MemoryEfficientCache) evictLRU() {
	item := c.tail.prev
	c.removeFromList(item)
	delete(c.items, item.key)

	// Return to pool
	item.key = ""
	item.value = nil
	item.prev = nil
	item.next = nil
	c.pool.Put(item)
}

// ZeroCopyStringBuilder builds strings without allocations
type ZeroCopyStringBuilder struct {
	buf []byte
	off int
}

// NewZeroCopyStringBuilder creates a new string builder
func NewZeroCopyStringBuilder(capacity int) *ZeroCopyStringBuilder {
	return &ZeroCopyStringBuilder{
		buf: make([]byte, 0, capacity),
	}
}

// WriteString writes a string without allocation
func (sb *ZeroCopyStringBuilder) WriteString(s string) {
	sb.buf = append(sb.buf, s...)
}

// WriteBytes writes bytes without allocation
func (sb *ZeroCopyStringBuilder) WriteBytes(b []byte) {
	sb.buf = append(sb.buf, b...)
}

// String returns the built string (may allocate)
func (sb *ZeroCopyStringBuilder) String() string {
	return *(*string)(unsafe.Pointer(&sb.buf))
}

// Reset resets the builder for reuse
func (sb *ZeroCopyStringBuilder) Reset() {
	sb.buf = sb.buf[:0]
	sb.off = 0
}
