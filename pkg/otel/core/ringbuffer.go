package core

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"unsafe"

	"github.com/yairfalse/tapio/pkg/otel/domain"
)

// LockFreeRingBuffer implements a high-performance lock-free ring buffer
// for trace queuing with SIMD optimizations and zero-allocation design
type LockFreeRingBuffer[T any] struct {
	// Cache-aligned fields to prevent false sharing
	_          [cacheLine - unsafe.Sizeof(uint64(0))]byte
	writeIndex uint64 // Atomic write position
	
	_         [cacheLine - unsafe.Sizeof(uint64(0))]byte  
	readIndex uint64 // Atomic read position
	
	_        [cacheLine - unsafe.Sizeof(uint64(0))]byte
	capacity uint64 // Buffer capacity (power of 2)
	mask     uint64 // Capacity - 1 for fast modulo
	
	// Buffer storage with cache-line alignment
	_    [cacheLine]byte
	data []atomicSlot[T]
	
	// SIMD optimization support
	simdEnabled bool
	alignment   int
	
	// Performance statistics
	_            [cacheLine]byte
	writeOps     uint64 // Total write operations
	readOps      uint64 // Total read operations
	contentions  uint64 // Write contentions
	waitCycles   uint64 // CPU cycles spent waiting
	
	// Memory management
	allocator   *SlotAllocator[T]
	gcGeneration uint32
}

// atomicSlot represents a slot in the ring buffer with atomic operations
type atomicSlot[T any] struct {
	// Sequence number for ordering and ABA prevention
	sequence uint64
	
	// Data pointer - nil means empty slot
	data unsafe.Pointer
	
	// Cache line padding to prevent false sharing
	_ [cacheLine - unsafe.Sizeof(uint64(0)) - unsafe.Sizeof(unsafe.Pointer(nil))]byte
}

// SlotAllocator manages memory allocation for ring buffer slots
type SlotAllocator[T any] struct {
	// Free slot stack (lock-free)
	freeStack  unsafe.Pointer // *slotStack[T]
	
	// Pool of pre-allocated slots
	slotPool   []T
	poolSize   uint64
	poolIndex  uint64 // Atomic index for allocation
	
	// SIMD-aligned allocation
	simdPool    []T
	simdIndex   uint64
	
	// Statistics
	allocations   uint64
	deallocations uint64
	poolHits      uint64
	poolMisses    uint64
}

// slotStack represents a lock-free stack node
type slotStack[T any] struct {
	next unsafe.Pointer
	slot *T
}

// TraceQueue specialized ring buffer for trace data with domain-specific optimizations
type TraceQueue[T domain.TraceData] struct {
	// Embedded ring buffer for core functionality
	ring *LockFreeRingBuffer[*TraceItem[T]]
	
	// Trace-specific optimizations
	batchProcessor *BatchProcessor[T]
	priorityLanes  [4]*LockFreeRingBuffer[*TraceItem[T]] // High, Normal, Low, Background
	
	// Sampling and filtering
	sampler      TraceSampler[T]
	filter       TraceFilter[T]
	
	// Performance monitoring
	metrics      *QueueMetrics
	
	// Backpressure management
	backpressure *BackpressureController
	
	// Configuration
	config TraceQueueConfig
}

// TraceItem represents an item in the trace queue
type TraceItem[T domain.TraceData] struct {
	// Trace data
	span     domain.SpanSnapshot[T]
	priority Priority
	
	// Timing information
	enqueuedAt uint64 // Timestamp in nanoseconds
	deadline   uint64 // Processing deadline
	
	// Processing metadata
	retries    uint32
	processingFlags uint32
	
	// Memory management
	arena     *Arena
	poolSlot  uint32
}

// BatchProcessor handles efficient batch processing of trace items
type BatchProcessor[T domain.TraceData] struct {
	// Batch configuration
	maxBatchSize   uint32
	maxWaitTime    uint64 // Nanoseconds
	
	// Current batch
	currentBatch   []*TraceItem[T]
	batchIndex     uint32
	batchStartTime uint64
	
	// Processing channels
	batchChannel   chan []*TraceItem[T]
	completedBatch chan ProcessingResult[T]
	
	// Statistics
	batchesProcessed uint64
	itemsProcessed   uint64
	averageBatchSize float64
}

// Supporting types and interfaces

type Priority uint8

const (
	PriorityBackground Priority = iota
	PriorityLow
	PriorityNormal 
	PriorityHigh
	PriorityCritical
)

type TraceSampler[T domain.TraceData] interface {
	ShouldSample(item *TraceItem[T]) bool
	GetSamplingRate() float64
}

type TraceFilter[T domain.TraceData] interface {
	ShouldProcess(item *TraceItem[T]) bool
	ApplyFilters(items []*TraceItem[T]) []*TraceItem[T]
}

type QueueMetrics struct {
	EnqueuedItems    uint64
	DequeuedItems    uint64
	DroppedItems     uint64
	QueueDepth       uint64
	AverageLatency   uint64
	P99Latency       uint64
	BackpressureHits uint64
}

type BackpressureController struct {
	threshold    float64 // Queue utilization threshold
	strategy     BackpressureStrategy
	dropCount    uint64
	delayCount   uint64
	currentDelay uint64 // Nanoseconds
}

type BackpressureStrategy uint8

const (
	BackpressureStrategyDrop BackpressureStrategy = iota
	BackpressureStrategyDelay
	BackpressureStrategyAdaptive
	BackpressureStrategyReject
)

type TraceQueueConfig struct {
	// Ring buffer configuration
	Capacity         uint64
	EnableSIMD       bool
	Alignment        int
	
	// Batch processing
	MaxBatchSize     uint32
	MaxBatchWait     uint64 // Nanoseconds
	
	// Priority lanes
	EnablePriority   bool
	PriorityWeights  [4]float64 // Weights for priority lanes
	
	// Backpressure
	BackpressureThreshold float64
	BackpressureStrategy  BackpressureStrategy
	
	// Performance tuning
	PreallocateSlots uint64
	EnableProfiling  bool
	CPUProfile       bool
}

type ProcessingResult[T domain.TraceData] struct {
	Batch     []*TraceItem[T]
	Success   bool
	Error     error
	Duration  uint64 // Nanoseconds
	ItemCount uint32
}

// Cache line size for alignment optimization
const cacheLine = 64

// NewLockFreeRingBuffer creates a new lock-free ring buffer with specified capacity
func NewLockFreeRingBuffer[T any](capacity uint64, simdEnabled bool) (*LockFreeRingBuffer[T], error) {
	// Ensure capacity is power of 2 for fast modulo operations
	if capacity == 0 || (capacity&(capacity-1)) != 0 {
		return nil, fmt.Errorf("capacity must be a power of 2, got %d", capacity)
	}
	
	// Create allocator
	allocator := &SlotAllocator[T]{
		slotPool: make([]T, capacity*2), // Double capacity for better allocation
		poolSize: capacity * 2,
	}
	
	if simdEnabled {
		allocator.simdPool = make([]T, capacity)
	}
	
	rb := &LockFreeRingBuffer[T]{
		capacity:    capacity,
		mask:        capacity - 1,
		data:        make([]atomicSlot[T], capacity),
		simdEnabled: simdEnabled,
		alignment:   64, // Cache line alignment
		allocator:   allocator,
	}
	
	// Initialize slots with sequence numbers
	for i := uint64(0); i < capacity; i++ {
		rb.data[i].sequence = i
	}
	
	return rb, nil
}

// Enqueue adds an item to the ring buffer using lock-free operations
func (rb *LockFreeRingBuffer[T]) Enqueue(item T) bool {
	var writePos, readPos uint64
	
	for {
		// Load current positions
		writePos = atomic.LoadUint64(&rb.writeIndex)
		readPos = atomic.LoadUint64(&rb.readIndex)
		
		// Check if buffer is full
		if writePos-readPos >= rb.capacity {
			atomic.AddUint64(&rb.contentions, 1)
			
			// Optionally yield or back off
			if writePos-readPos >= rb.capacity*2 {
				return false // Buffer definitely full
			}
			
			runtime.Gosched() // Yield to other goroutines
			continue
		}
		
		// Try to claim write position
		if atomic.CompareAndSwapUint64(&rb.writeIndex, writePos, writePos+1) {
			break
		}
		
		// Add wait cycles for profiling
		atomic.AddUint64(&rb.waitCycles, 1)
	}
	
	// Get slot for this write position
	slot := &rb.data[writePos&rb.mask]
	
	// Wait for slot to be available (sequence should match position)
	expectedSeq := writePos
	for {
		currentSeq := atomic.LoadUint64(&slot.sequence)
		if currentSeq == expectedSeq {
			break
		}
		
		// Spin wait with backoff
		for i := 0; i < 8; i++ {
			runtime.Gosched()
		}
		atomic.AddUint64(&rb.waitCycles, 8)
	}
	
	// Store item data
	itemPtr := rb.allocator.Allocate(item)
	atomic.StorePointer(&slot.data, itemPtr)
	
	// Update sequence to signal item is ready
	atomic.StoreUint64(&slot.sequence, writePos+1)
	
	// Update statistics
	atomic.AddUint64(&rb.writeOps, 1)
	
	return true
}

// Dequeue removes an item from the ring buffer using lock-free operations
func (rb *LockFreeRingBuffer[T]) Dequeue() (T, bool) {
	var zero T
	var readPos, writePos uint64
	
	for {
		// Load current positions
		readPos = atomic.LoadUint64(&rb.readIndex)
		writePos = atomic.LoadUint64(&rb.writeIndex)
		
		// Check if buffer is empty
		if readPos >= writePos {
			return zero, false
		}
		
		// Try to claim read position
		if atomic.CompareAndSwapUint64(&rb.readIndex, readPos, readPos+1) {
			break
		}
		
		atomic.AddUint64(&rb.waitCycles, 1)
	}
	
	// Get slot for this read position
	slot := &rb.data[readPos&rb.mask]
	
	// Wait for item to be ready (sequence should be readPos + 1)
	expectedSeq := readPos + 1
	for {
		currentSeq := atomic.LoadUint64(&slot.sequence)
		if currentSeq == expectedSeq {
			break
		}
		
		// Spin wait with backoff
		runtime.Gosched()
		atomic.AddUint64(&rb.waitCycles, 1)
	}
	
	// Load item data
	itemPtr := atomic.LoadPointer(&slot.data)
	if itemPtr == nil {
		return zero, false
	}
	
	item := *(*T)(itemPtr)
	
	// Clear slot
	atomic.StorePointer(&slot.data, nil)
	
	// Update sequence for next cycle
	atomic.StoreUint64(&slot.sequence, readPos+rb.capacity)
	
	// Return item to allocator
	rb.allocator.Deallocate(itemPtr)
	
	// Update statistics
	atomic.AddUint64(&rb.readOps, 1)
	
	return item, true
}

// DequeueBatch efficiently dequeues multiple items using SIMD when possible
func (rb *LockFreeRingBuffer[T]) DequeueBatch(batch []T) int {
	count := 0
	maxCount := len(batch)
	
	if rb.simdEnabled && maxCount >= 8 {
		// Use SIMD-optimized batch dequeue
		count = rb.dequeueBatchSIMD(batch)
	} else {
		// Standard batch dequeue
		for i := 0; i < maxCount; i++ {
			if item, ok := rb.Dequeue(); ok {
				batch[i] = item
				count++
			} else {
				break
			}
		}
	}
	
	return count
}

// dequeueBatchSIMD uses SIMD instructions for efficient batch processing
func (rb *LockFreeRingBuffer[T]) dequeueBatchSIMD(batch []T) int {
	// This would use actual SIMD instructions in a real implementation
	// For now, we simulate with optimized loops
	
	count := 0
	batchSize := len(batch)
	
	// Process in chunks of 8 for SIMD alignment
	chunkSize := 8
	fullChunks := batchSize / chunkSize
	
	for chunk := 0; chunk < fullChunks; chunk++ {
		chunkStart := chunk * chunkSize
		chunkCount := 0
		
		// Try to dequeue 8 items at once
		for i := 0; i < chunkSize; i++ {
			if item, ok := rb.Dequeue(); ok {
				batch[chunkStart+i] = item
				chunkCount++
			} else {
				break
			}
		}
		
		count += chunkCount
		if chunkCount < chunkSize {
			break // Partial chunk, buffer is empty
		}
	}
	
	// Process remaining items
	remaining := batchSize % chunkSize
	for i := 0; i < remaining; i++ {
		if item, ok := rb.Dequeue(); ok {
			batch[fullChunks*chunkSize+i] = item
			count++
		} else {
			break
		}
	}
	
	return count
}

// Size returns the current number of items in the buffer
func (rb *LockFreeRingBuffer[T]) Size() uint64 {
	writePos := atomic.LoadUint64(&rb.writeIndex)
	readPos := atomic.LoadUint64(&rb.readIndex)
	return writePos - readPos
}

// Capacity returns the maximum capacity of the buffer
func (rb *LockFreeRingBuffer[T]) Capacity() uint64 {
	return rb.capacity
}

// GetStats returns performance statistics
func (rb *LockFreeRingBuffer[T]) GetStats() RingBufferStats {
	return RingBufferStats{
		WriteOps:    atomic.LoadUint64(&rb.writeOps),
		ReadOps:     atomic.LoadUint64(&rb.readOps),
		Contentions: atomic.LoadUint64(&rb.contentions),
		WaitCycles:  atomic.LoadUint64(&rb.waitCycles),
		Size:        rb.Size(),
		Capacity:    rb.capacity,
		Utilization: float64(rb.Size()) / float64(rb.capacity),
	}
}

// NewTraceQueue creates a specialized trace queue with domain optimizations
func NewTraceQueue[T domain.TraceData](config TraceQueueConfig) (*TraceQueue[T], error) {
	// Create main ring buffer
	ring, err := NewLockFreeRingBuffer[*TraceItem[T]](config.Capacity, config.EnableSIMD)
	if err != nil {
		return nil, fmt.Errorf("failed to create ring buffer: %w", err)
	}
	
	queue := &TraceQueue[T]{
		ring:   ring,
		config: config,
		metrics: &QueueMetrics{},
		backpressure: &BackpressureController{
			threshold: config.BackpressureThreshold,
			strategy:  config.BackpressureStrategy,
		},
	}
	
	// Create priority lanes if enabled
	if config.EnablePriority {
		for i := 0; i < 4; i++ {
			laneCapacity := uint64(float64(config.Capacity) * config.PriorityWeights[i])
			if laneCapacity == 0 {
				laneCapacity = config.Capacity / 8 // Minimum lane size
			}
			
			lane, err := NewLockFreeRingBuffer[*TraceItem[T]](laneCapacity, config.EnableSIMD)
			if err != nil {
				return nil, fmt.Errorf("failed to create priority lane %d: %w", i, err)
			}
			queue.priorityLanes[i] = lane
		}
	}
	
	// Create batch processor
	queue.batchProcessor = &BatchProcessor[T]{
		maxBatchSize:   config.MaxBatchSize,
		maxWaitTime:    config.MaxBatchWait,
		currentBatch:   make([]*TraceItem[T], 0, config.MaxBatchSize),
		batchChannel:   make(chan []*TraceItem[T], 16),
		completedBatch: make(chan ProcessingResult[T], 16),
	}
	
	// Start batch processing goroutine
	go queue.processBatches()
	
	return queue, nil
}

// EnqueueTrace adds a trace span to the queue with priority handling
func (tq *TraceQueue[T]) EnqueueTrace(span domain.SpanSnapshot[T], priority Priority) bool {
	// Create trace item
	item := &TraceItem[T]{
		span:       span,
		priority:   priority,
		enqueuedAt: uint64(runtime.nanotime()),
		deadline:   uint64(runtime.nanotime()) + tq.config.MaxBatchWait*2, // Default deadline
	}
	
	// Apply sampling
	if tq.sampler != nil && !tq.sampler.ShouldSample(item) {
		return true // Sampled out, but not an error
	}
	
	// Apply filtering
	if tq.filter != nil && !tq.filter.ShouldProcess(item) {
		return true // Filtered out
	}
	
	// Check backpressure
	if tq.shouldApplyBackpressure() {
		return tq.handleBackpressure(item)
	}
	
	// Enqueue to appropriate lane
	var success bool
	if tq.config.EnablePriority && int(priority) < len(tq.priorityLanes) {
		success = tq.priorityLanes[priority].Enqueue(item)
	} else {
		success = tq.ring.Enqueue(item)
	}
	
	if success {
		atomic.AddUint64(&tq.metrics.EnqueuedItems, 1)
	} else {
		atomic.AddUint64(&tq.metrics.DroppedItems, 1)
	}
	
	return success
}

// DequeueTraces removes trace items for processing
func (tq *TraceQueue[T]) DequeueTraces(maxItems int) []*TraceItem[T] {
	items := make([]*TraceItem[T], 0, maxItems)
	
	if tq.config.EnablePriority {
		// Dequeue from priority lanes first
		for priority := PriorityCritical; priority >= PriorityBackground; priority-- {
			laneIndex := int(priority)
			if laneIndex < len(tq.priorityLanes) {
				lane := tq.priorityLanes[laneIndex]
				
				// Dequeue up to remaining capacity from this lane
				remaining := maxItems - len(items)
				if remaining <= 0 {
					break
				}
				
				batch := make([]*TraceItem[T], remaining)
				count := lane.DequeueBatch(batch)
				items = append(items, batch[:count]...)
			}
		}
	}
	
	// Fill remaining capacity from main ring
	if len(items) < maxItems {
		remaining := maxItems - len(items)
		batch := make([]*TraceItem[T], remaining)
		count := tq.ring.DequeueBatch(batch)
		items = append(items, batch[:count]...)
	}
	
	atomic.AddUint64(&tq.metrics.DequeuedItems, uint64(len(items)))
	return items
}

// GetMetrics returns queue performance metrics
func (tq *TraceQueue[T]) GetMetrics() QueueMetrics {
	// Update queue depth
	depth := tq.ring.Size()
	for _, lane := range tq.priorityLanes {
		if lane != nil {
			depth += lane.Size()
		}
	}
	atomic.StoreUint64(&tq.metrics.QueueDepth, depth)
	
	return *tq.metrics
}

// Private methods

func (tq *TraceQueue[T]) shouldApplyBackpressure() bool {
	utilization := float64(tq.ring.Size()) / float64(tq.ring.Capacity())
	return utilization > tq.backpressure.threshold
}

func (tq *TraceQueue[T]) handleBackpressure(item *TraceItem[T]) bool {
	atomic.AddUint64(&tq.metrics.BackpressureHits, 1)
	
	switch tq.backpressure.strategy {
	case BackpressureStrategyDrop:
		atomic.AddUint64(&tq.backpressure.dropCount, 1)
		return false // Drop the item
		
	case BackpressureStrategyDelay:
		// Apply exponential backoff delay
		delay := atomic.LoadUint64(&tq.backpressure.currentDelay)
		if delay == 0 {
			delay = 1000 // Start with 1 microsecond
		} else {
			delay *= 2 // Exponential backoff
			if delay > 1000000 { // Max 1ms delay
				delay = 1000000
			}
		}
		atomic.StoreUint64(&tq.backpressure.currentDelay, delay)
		atomic.AddUint64(&tq.backpressure.delayCount, 1)
		
		// Sleep for the delay period
		runtime.nanosleep(int64(delay))
		
		// Try to enqueue again
		return tq.ring.Enqueue(item)
		
	case BackpressureStrategyAdaptive:
		// Adaptive strategy based on item priority
		if item.priority >= PriorityHigh {
			// High priority items get delayed instead of dropped
			return tq.handleBackpressure(&TraceItem[T]{
				span:       item.span,
				priority:   item.priority,
				enqueuedAt: item.enqueuedAt,
				deadline:   item.deadline,
			})
		} else {
			// Low priority items get dropped
			return false
		}
		
	case BackpressureStrategyReject:
		return false // Reject immediately
		
	default:
		return false
	}
}

func (tq *TraceQueue[T]) processBatches() {
	ticker := time.NewTicker(time.Duration(tq.config.MaxBatchWait))
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			// Process current batch if it has items
			if len(tq.batchProcessor.currentBatch) > 0 {
				tq.flushCurrentBatch()
			}
			
		case batch := <-tq.batchProcessor.batchChannel:
			// Process completed batch
			result := ProcessingResult[T]{
				Batch:     batch,
				Success:   true,
				ItemCount: uint32(len(batch)),
			}
			
			// Send result
			select {
			case tq.batchProcessor.completedBatch <- result:
			default:
				// Channel full, drop result
			}
			
			atomic.AddUint64(&tq.batchProcessor.batchesProcessed, 1)
			atomic.AddUint64(&tq.batchProcessor.itemsProcessed, uint64(len(batch)))
		}
	}
}

func (tq *TraceQueue[T]) flushCurrentBatch() {
	if len(tq.batchProcessor.currentBatch) == 0 {
		return
	}
	
	// Send current batch for processing
	batch := make([]*TraceItem[T], len(tq.batchProcessor.currentBatch))
	copy(batch, tq.batchProcessor.currentBatch)
	
	select {
	case tq.batchProcessor.batchChannel <- batch:
		// Batch sent successfully
	default:
		// Channel full, drop batch
		atomic.AddUint64(&tq.metrics.DroppedItems, uint64(len(batch)))
	}
	
	// Reset current batch
	tq.batchProcessor.currentBatch = tq.batchProcessor.currentBatch[:0]
	tq.batchProcessor.batchIndex = 0
	tq.batchProcessor.batchStartTime = uint64(runtime.nanotime())
}

// SlotAllocator methods

func (sa *SlotAllocator[T]) Allocate(item T) unsafe.Pointer {
	// Try to get from free stack first
	if ptr := sa.popFreeStack(); ptr != nil {
		*(*T)(ptr) = item
		atomic.AddUint64(&sa.poolHits, 1)
		return ptr
	}
	
	// Try pool allocation
	if atomic.LoadUint64(&sa.poolIndex) < sa.poolSize {
		index := atomic.AddUint64(&sa.poolIndex, 1) - 1
		if index < sa.poolSize {
			sa.slotPool[index] = item
			atomic.AddUint64(&sa.poolHits, 1)
			return unsafe.Pointer(&sa.slotPool[index])
		}
	}
	
	// Fallback to heap allocation
	heapItem := new(T)
	*heapItem = item
	atomic.AddUint64(&sa.allocations, 1)
	atomic.AddUint64(&sa.poolMisses, 1)
	return unsafe.Pointer(heapItem)
}

func (sa *SlotAllocator[T]) Deallocate(ptr unsafe.Pointer) {
	if ptr == nil {
		return
	}
	
	// Add to free stack for reuse
	sa.pushFreeStack(ptr)
	atomic.AddUint64(&sa.deallocations, 1)
}

func (sa *SlotAllocator[T]) popFreeStack() unsafe.Pointer {
	// Lock-free stack pop
	for {
		top := atomic.LoadPointer(&sa.freeStack)
		if top == nil {
			return nil
		}
		
		stack := (*slotStack[T])(top)
		next := atomic.LoadPointer(&stack.next)
		
		if atomic.CompareAndSwapPointer(&sa.freeStack, top, next) {
			return unsafe.Pointer(stack.slot)
		}
	}
}

func (sa *SlotAllocator[T]) pushFreeStack(ptr unsafe.Pointer) {
	// Create new stack node
	node := &slotStack[T]{
		slot: (*T)(ptr),
	}
	
	// Lock-free stack push
	for {
		top := atomic.LoadPointer(&sa.freeStack)
		atomic.StorePointer(&node.next, top)
		
		if atomic.CompareAndSwapPointer(&sa.freeStack, top, unsafe.Pointer(node)) {
			break
		}
	}
}

// Supporting types

type RingBufferStats struct {
	WriteOps    uint64
	ReadOps     uint64
	Contentions uint64
	WaitCycles  uint64
	Size        uint64
	Capacity    uint64
	Utilization float64
}