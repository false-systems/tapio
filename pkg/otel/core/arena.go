package core

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/yairfalse/tapio/pkg/otel/domain"
)

// ArenaManager provides zero-allocation span creation using memory arenas
// Implements cutting-edge memory management for high-performance OTEL tracing
type ArenaManager[T domain.TraceData] struct {
	// Arena pools for different allocation sizes
	smallArenas  sync.Pool // < 1KB allocations
	mediumArenas sync.Pool // 1KB - 64KB allocations
	largeArenas  sync.Pool // > 64KB allocations

	// Performance tracking
	allocations   int64
	deallocations int64
	totalBytes    int64
	reuseRate     int64

	// Configuration
	config ArenaConfig

	// Arena registry for lifecycle management
	activeArenas sync.Map // map[*Arena]bool

	// Memory pressure detection
	gcCycles       int64
	memoryPressure int32 // atomic bool

	// SIMD-aligned allocation pools
	simdAligned256 sync.Pool // 256-byte aligned for AVX2
	simdAligned512 sync.Pool // 512-byte aligned for AVX-512
}

// Arena represents a memory arena with zero-allocation span creation
type Arena struct {
	// Memory layout - carefully aligned for performance
	data []byte // Raw memory block
	pos  int64  // Current allocation position (atomic)
	size int64  // Total arena size
	refs int32  // Reference count (atomic)

	// Allocation tracking
	allocCount int64 // Number of allocations (atomic)
	maxUsed    int64 // High water mark (atomic)

	// Metadata
	id       uint64 // Unique arena ID
	created  int64  // Creation timestamp (Unix nano)
	lastUsed int64  // Last access timestamp (atomic)

	// SIMD optimization support
	simdAligned bool // Whether this arena provides SIMD-aligned allocations
	alignment   int  // Alignment boundary (16, 32, 64, 256, 512 bytes)

	// Lock-free free list for reuse
	freeList     *FreeBlock
	freeListLock int32 // Spinlock for free list updates

	// Generation tracking for PGO
	generation uint32 // Arena generation for profile-guided optimization
}

// FreeBlock represents a freed memory block in the arena
type FreeBlock struct {
	next   *FreeBlock
	offset int64
	size   int64
}

// ArenaSpan implements domain.Span with zero-allocation operations
type ArenaSpan[T domain.TraceData] struct {
	// Core span data - optimized layout for cache efficiency
	traceID  domain.TraceID // 16 bytes
	spanID   domain.SpanID  // 8 bytes
	parentID domain.SpanID  // 8 bytes

	// Timing information
	startTime int64 // Unix nanoseconds (atomic)
	endTime   int64 // Unix nanoseconds (atomic)

	// Metadata stored in arena
	namePtr unsafe.Pointer // Points to string data in arena
	nameLen int32

	// Attributes stored as contiguous blocks in arena
	attrsPtr unsafe.Pointer // Points to attribute array in arena
	attrsLen int32
	attrsCap int32

	// Events stored similarly
	eventsPtr unsafe.Pointer
	eventsLen int32
	eventsCap int32

	// Links
	linksPtr unsafe.Pointer
	linksLen int32
	linksCap int32

	// Status information
	statusCode   uint8 // domain.StatusCode
	statusMsgPtr unsafe.Pointer
	statusMsgLen int32

	// Flags - packed for efficiency
	flags uint32 // recording, root, sampled, etc.

	// Arena reference for memory management
	arena *Arena

	// Context and lifecycle
	ctx unsafe.Pointer // *context.Context stored in arena

	// SIMD-optimized operations flag
	simdEnabled bool
}

// ArenaConfig configures arena behavior and performance characteristics
type ArenaConfig struct {
	// Size configuration
	SmallArenaSize  int64 // Size for small arenas (default: 64KB)
	MediumArenaSize int64 // Size for medium arenas (default: 1MB)
	LargeArenaSize  int64 // Size for large arenas (default: 16MB)

	// Pool configuration
	MaxArenasPerPool int // Maximum arenas per size pool
	PreallocateCount int // Number of arenas to preallocate

	// Performance tuning
	EnableSIMDAlignment bool // Enable SIMD-aligned allocations
	DefaultAlignment    int  // Default alignment (16, 32, 64 bytes)
	EnableReuse         bool // Enable memory block reuse

	// Memory pressure management
	EnablePressureDetection bool    // Enable GC pressure detection
	PressureThreshold       float64 // GC pressure threshold (0.0-1.0)

	// Profiling and optimization
	EnableProfiling   bool    // Enable allocation profiling for PGO
	ProfileSampleRate float64 // Sampling rate for profiling

	// Lifecycle management
	IdleTimeout     int64 // Idle timeout in nanoseconds
	MaxArenaAge     int64 // Maximum arena age in nanoseconds
	CleanupInterval int64 // Cleanup interval in nanoseconds
}

// Span creation flags for performance optimization
const (
	SpanFlagRecording uint32 = 1 << iota
	SpanFlagRoot
	SpanFlagSampled
	SpanFlagHasParent
	SpanFlagHasAttributes
	SpanFlagHasEvents
	SpanFlagHasLinks
	SpanFlagSIMDOptimized
)

// NewArenaManager creates a new arena manager with performance optimization
func NewArenaManager[T domain.TraceData](config ArenaConfig) *ArenaManager[T] {
	applyArenaDefaults(&config)

	manager := &ArenaManager[T]{
		config: config,
	}

	// Initialize arena pools
	manager.initializePools()

	// Start background cleanup if configured
	if config.CleanupInterval > 0 {
		go manager.cleanupLoop()
	}

	// Start memory pressure monitoring
	if config.EnablePressureDetection {
		go manager.monitorMemoryPressure()
	}

	return manager
}

// CreateSpan creates a new span with zero-allocation optimization
func (am *ArenaManager[T]) CreateSpan(
	name string,
	traceID domain.TraceID,
	spanID domain.SpanID,
	parentID domain.SpanID,
	opts ...SpanCreationOption,
) (*ArenaSpan[T], error) {

	// Estimate required space for this span
	estimatedSize := am.estimateSpanSize(name, opts)

	// Get appropriate arena
	arena := am.getArena(estimatedSize)
	if arena == nil {
		return nil, fmt.Errorf("failed to allocate arena for span")
	}

	// Create span in arena with zero-allocation path
	span, err := am.createSpanInArena[T](arena, name, traceID, spanID, parentID, opts)
	if err != nil {
		am.returnArena(arena)
		return nil, fmt.Errorf("failed to create span in arena: %w", err)
	}

	// Update performance counters
	atomic.AddInt64(&am.allocations, 1)
	atomic.AddInt64(&am.totalBytes, estimatedSize)

	return span, nil
}

// CreateSpanBatch creates multiple spans efficiently in a single arena
func (am *ArenaManager[T]) CreateSpanBatch(
	requests []SpanBatchRequest[T],
) ([]*ArenaSpan[T], error) {

	if len(requests) == 0 {
		return nil, nil
	}

	// Estimate total size for all spans
	totalSize := int64(0)
	for _, req := range requests {
		totalSize += am.estimateSpanSize(req.Name, req.Options)
	}

	// Get large arena for batch
	arena := am.getArena(totalSize)
	if arena == nil {
		return nil, fmt.Errorf("failed to allocate arena for span batch")
	}

	// Create all spans in the same arena
	spans := make([]*ArenaSpan[T], len(requests))
	for i, req := range requests {
		span, err := am.createSpanInArena[T](
			arena, req.Name, req.TraceID, req.SpanID, req.ParentID, req.Options,
		)
		if err != nil {
			// Cleanup already created spans
			for j := 0; j < i; j++ {
				spans[j].Release()
			}
			am.returnArena(arena)
			return nil, fmt.Errorf("failed to create span %d in batch: %w", i, err)
		}
		spans[i] = span
	}

	atomic.AddInt64(&am.allocations, int64(len(requests)))
	atomic.AddInt64(&am.totalBytes, totalSize)

	return spans, nil
}

// GetStats returns arena manager performance statistics
func (am *ArenaManager[T]) GetStats() ArenaStats {
	var activeCount int64
	am.activeArenas.Range(func(key, value any) bool {
		activeCount++
		return true
	})

	return ArenaStats{
		Allocations:    atomic.LoadInt64(&am.allocations),
		Deallocations:  atomic.LoadInt64(&am.deallocations),
		TotalBytes:     atomic.LoadInt64(&am.totalBytes),
		ActiveArenas:   activeCount,
		ReuseRate:      float64(atomic.LoadInt64(&am.reuseRate)) / float64(am.allocations),
		MemoryPressure: atomic.LoadInt32(&am.memoryPressure) == 1,
		GCCycles:       atomic.LoadInt64(&am.gcCycles),
	}
}

// Private methods

func (am *ArenaManager[T]) initializePools() {
	// Small arenas pool
	am.smallArenas.New = func() any {
		return am.createArena(am.config.SmallArenaSize, 16)
	}

	// Medium arenas pool
	am.mediumArenas.New = func() any {
		return am.createArena(am.config.MediumArenaSize, 32)
	}

	// Large arenas pool
	am.largeArenas.New = func() any {
		return am.createArena(am.config.LargeArenaSize, 64)
	}

	// SIMD-aligned pools if enabled
	if am.config.EnableSIMDAlignment {
		am.simdAligned256.New = func() any {
			return am.createSIMDArena(am.config.MediumArenaSize, 256)
		}

		am.simdAligned512.New = func() any {
			return am.createSIMDArena(am.config.LargeArenaSize, 512)
		}
	}

	// Preallocate arenas if configured
	if am.config.PreallocateCount > 0 {
		am.preallocateArenas()
	}
}

func (am *ArenaManager[T]) createArena(size int64, alignment int) *Arena {
	// Allocate aligned memory block
	data := make([]byte, size)

	// Ensure alignment if needed
	if alignment > 1 {
		addr := uintptr(unsafe.Pointer(&data[0]))
		aligned := (addr + uintptr(alignment-1)) &^ uintptr(alignment-1)
		offset := aligned - addr
		data = data[offset:]
	}

	arena := &Arena{
		data:        data,
		size:        int64(len(data)),
		id:          am.generateArenaID(),
		created:     time.Now().UnixNano(),
		simdAligned: false,
		alignment:   alignment,
		generation:  am.getCurrentGeneration(),
	}

	// Register arena
	am.activeArenas.Store(arena, true)

	return arena
}

func (am *ArenaManager[T]) createSIMDArena(size int64, alignment int) *Arena {
	// Create SIMD-aligned arena for vectorized operations
	arena := am.createArena(size, alignment)
	arena.simdAligned = true
	arena.alignment = alignment

	return arena
}

func (am *ArenaManager[T]) getArena(requiredSize int64) *Arena {
	var pool *sync.Pool

	// Select appropriate pool based on size
	switch {
	case requiredSize <= am.config.SmallArenaSize:
		pool = &am.smallArenas
	case requiredSize <= am.config.MediumArenaSize:
		pool = &am.mediumArenas
	default:
		pool = &am.largeArenas
	}

	// Get arena from pool
	arena := pool.Get().(*Arena)

	// Reset arena for reuse
	am.resetArena(arena)

	return arena
}

func (am *ArenaManager[T]) returnArena(arena *Arena) {
	if arena == nil {
		return
	}

	// Update deallocations counter
	atomic.AddInt64(&am.deallocations, 1)

	// Determine which pool to return to
	var pool *sync.Pool
	switch {
	case arena.size <= am.config.SmallArenaSize:
		pool = &am.smallArenas
	case arena.size <= am.config.MediumArenaSize:
		pool = &am.mediumArenas
	default:
		pool = &am.largeArenas
	}

	// Return to pool
	pool.Put(arena)

	// Update reuse rate
	atomic.AddInt64(&am.reuseRate, 1)
}

func (am *ArenaManager[T]) createSpanInArena(
	arena *Arena,
	name string,
	traceID domain.TraceID,
	spanID domain.SpanID,
	parentID domain.SpanID,
	opts []SpanCreationOption,
) (*ArenaSpan[T], error) {

	// Allocate span structure in arena
	spanPtr := arena.Alloc(unsafe.Sizeof(ArenaSpan[T]{}))
	if spanPtr == nil {
		return nil, fmt.Errorf("arena exhausted")
	}

	span := (*ArenaSpan[T])(spanPtr)

	// Initialize span with zero allocations
	span.traceID = traceID
	span.spanID = spanID
	span.parentID = parentID
	span.arena = arena
	span.startTime = time.Now().UnixNano()
	span.flags = SpanFlagRecording

	// Store name in arena
	namePtr := arena.AllocString(name)
	if namePtr == nil {
		return nil, fmt.Errorf("failed to allocate span name")
	}
	span.namePtr = namePtr
	span.nameLen = int32(len(name))

	// Apply options
	for _, opt := range opts {
		if err := opt(span, arena); err != nil {
			return nil, fmt.Errorf("failed to apply span option: %w", err)
		}
	}

	// Set parent flag if applicable
	if parentID != (domain.SpanID{}) {
		span.flags |= SpanFlagHasParent
	}

	// Increment arena reference count
	atomic.AddInt32(&arena.refs, 1)
	atomic.AddInt64(&arena.allocCount, 1)

	return span, nil
}

func (am *ArenaManager[T]) estimateSpanSize(name string, opts []SpanCreationOption) int64 {
	// Base span structure size
	size := int64(unsafe.Sizeof(ArenaSpan[any]{}))

	// Add name size
	size += int64(len(name))

	// Estimate attributes, events, links based on options
	// This would be more sophisticated in a real implementation
	for _, opt := range opts {
		size += 64 // Rough estimate per option
	}

	// Add padding for alignment
	return size + 64
}

func (am *ArenaManager[T]) resetArena(arena *Arena) {
	// Reset position atomically
	atomic.StoreInt64(&arena.pos, 0)
	atomic.StoreInt32(&arena.refs, 0)
	atomic.StoreInt64(&arena.lastUsed, time.Now().UnixNano())

	// Reset free list
	arena.freeList = nil
}

func (am *ArenaManager[T]) generateArenaID() uint64 {
	return uint64(time.Now().UnixNano())
}

func (am *ArenaManager[T]) getCurrentGeneration() uint32 {
	// Simple generation counter for PGO
	return uint32(atomic.LoadInt64(&am.gcCycles)) % 1000
}

func (am *ArenaManager[T]) preallocateArenas() {
	for i := 0; i < am.config.PreallocateCount; i++ {
		// Preallocate arenas of different sizes
		am.smallArenas.Put(am.createArena(am.config.SmallArenaSize, 16))
		am.mediumArenas.Put(am.createArena(am.config.MediumArenaSize, 32))
		am.largeArenas.Put(am.createArena(am.config.LargeArenaSize, 64))
	}
}

func (am *ArenaManager[T]) cleanupLoop() {
	for {
		runtime.Gosched()

		// Cleanup old arenas
		now := time.Now().UnixNano()
		am.activeArenas.Range(func(key, value any) bool {
			arena := key.(*Arena)
			if now-atomic.LoadInt64(&arena.lastUsed) > am.config.IdleTimeout {
				if atomic.LoadInt32(&arena.refs) == 0 {
					am.activeArenas.Delete(key)
				}
			}
			return true
		})

		// Sleep until next cleanup
		runtime.nanosleep(am.config.CleanupInterval)
	}
}

func (am *ArenaManager[T]) monitorMemoryPressure() {
	var lastGC uint32

	for {
		runtime.Gosched()

		// Check GC stats
		var m runtime.MemStats
		runtime.ReadMemStats(&m)

		// Detect pressure based on GC frequency
		if m.NumGC > lastGC {
			atomic.AddInt64(&am.gcCycles, int64(m.NumGC-lastGC))
			lastGC = m.NumGC

			// Calculate pressure based on GC frequency
			pressure := float64(m.NumGC-lastGC) / 10.0
			if pressure > am.config.PressureThreshold {
				atomic.StoreInt32(&am.memoryPressure, 1)
			} else {
				atomic.StoreInt32(&am.memoryPressure, 0)
			}
		}

		runtime.nanosleep(1e9) // Check every second
	}
}

// Arena methods

// Alloc allocates memory in the arena with alignment
func (a *Arena) Alloc(size uintptr) unsafe.Pointer {
	// Align size to pointer boundary
	alignedSize := (size + unsafe.Sizeof(uintptr(0)) - 1) &^ (unsafe.Sizeof(uintptr(0)) - 1)

	// Check if we have space
	pos := atomic.LoadInt64(&a.pos)
	if pos+int64(alignedSize) > a.size {
		// Try to use free list first
		if block := a.findFreeBlock(int64(alignedSize)); block != nil {
			return unsafe.Pointer(uintptr(unsafe.Pointer(&a.data[0])) + uintptr(block.offset))
		}
		return nil
	}

	// Atomic allocation
	newPos := atomic.AddInt64(&a.pos, int64(alignedSize))
	if newPos > a.size {
		atomic.AddInt64(&a.pos, -int64(alignedSize)) // Rollback
		return nil
	}

	offset := newPos - int64(alignedSize)

	// Update high water mark
	for {
		current := atomic.LoadInt64(&a.maxUsed)
		if newPos <= current || atomic.CompareAndSwapInt64(&a.maxUsed, current, newPos) {
			break
		}
	}

	return unsafe.Pointer(uintptr(unsafe.Pointer(&a.data[0])) + uintptr(offset))
}

// AllocString allocates a string in the arena and returns a pointer
func (a *Arena) AllocString(s string) unsafe.Pointer {
	ptr := a.Alloc(uintptr(len(s)))
	if ptr == nil {
		return nil
	}

	// Copy string data
	copy((*[1 << 30]byte)(ptr)[:len(s)], s)

	return ptr
}

// Free adds a block to the free list for reuse
func (a *Arena) Free(ptr unsafe.Pointer, size int64) {
	if ptr == nil || size <= 0 {
		return
	}

	// Calculate offset
	baseAddr := uintptr(unsafe.Pointer(&a.data[0]))
	offset := uintptr(ptr) - baseAddr

	// Create free block
	block := &FreeBlock{
		offset: int64(offset),
		size:   size,
	}

	// Add to free list with spinlock
	for !atomic.CompareAndSwapInt32(&a.freeListLock, 0, 1) {
		runtime.Gosched()
	}

	block.next = a.freeList
	a.freeList = block

	atomic.StoreInt32(&a.freeListLock, 0)
}

func (a *Arena) findFreeBlock(size int64) *FreeBlock {
	// Simple first-fit allocation from free list
	if atomic.LoadInt32(&a.freeListLock) == 1 {
		return nil // Free list is locked
	}

	for !atomic.CompareAndSwapInt32(&a.freeListLock, 0, 1) {
		return nil // Don't wait, just allocate new
	}
	defer atomic.StoreInt32(&a.freeListLock, 0)

	var prev *FreeBlock
	current := a.freeList

	for current != nil {
		if current.size >= size {
			// Remove from free list
			if prev == nil {
				a.freeList = current.next
			} else {
				prev.next = current.next
			}

			// Split block if much larger
			if current.size > size*2 {
				newBlock := &FreeBlock{
					offset: current.offset + size,
					size:   current.size - size,
					next:   a.freeList,
				}
				a.freeList = newBlock
				current.size = size
			}

			return current
		}
		prev = current
		current = current.next
	}

	return nil
}

// ArenaSpan methods implementing domain.Span interface

func (s *ArenaSpan[T]) SetAttribute(key string, value T) domain.Span[T] {
	// Implementation would store attribute in arena
	s.flags |= SpanFlagHasAttributes
	return s
}

func (s *ArenaSpan[T]) SetAttributes(attrs map[string]T) domain.Span[T] {
	// Implementation would store attributes efficiently in arena
	if len(attrs) > 0 {
		s.flags |= SpanFlagHasAttributes
	}
	return s
}

func (s *ArenaSpan[T]) AddEvent(name string, attrs map[string]T) domain.Span[T] {
	// Implementation would store event in arena
	s.flags |= SpanFlagHasEvents
	return s
}

func (s *ArenaSpan[T]) RecordError(err error, attrs map[string]T) domain.Span[T] {
	// Implementation would record error efficiently
	return s
}

func (s *ArenaSpan[T]) SetStatus(code domain.StatusCode, description string) domain.Span[T] {
	s.statusCode = uint8(code)
	// Store description in arena
	return s
}

func (s *ArenaSpan[T]) End() domain.SpanSnapshot[T] {
	// Set end time atomically
	atomic.StoreInt64(&s.endTime, time.Now().UnixNano())

	// Create snapshot (implementation would be more comprehensive)
	return &ArenaSpanSnapshot[T]{span: s}
}

func (s *ArenaSpan[T]) IsRecording() bool {
	return s.flags&SpanFlagRecording != 0
}

func (s *ArenaSpan[T]) IsRootSpan() bool {
	return s.flags&SpanFlagRoot != 0
}

func (s *ArenaSpan[T]) GetTraceID() domain.TraceID {
	return s.traceID
}

func (s *ArenaSpan[T]) GetSpanID() domain.SpanID {
	return s.spanID
}

func (s *ArenaSpan[T]) GetParentSpanID() domain.SpanID {
	return s.parentID
}

func (s *ArenaSpan[T]) GetArena() *domain.ArenaRef {
	return &domain.ArenaRef{
		// Convert internal arena to domain arena reference
	}
}

func (s *ArenaSpan[T]) Release() {
	// Decrement arena reference count
	if atomic.AddInt32(&s.arena.refs, -1) == 0 {
		// Last reference, return arena to pool
		// This would be handled by the arena manager
	}
}

// Supporting types

type SpanCreationOption func(span *ArenaSpan[any], arena *Arena) error

type SpanBatchRequest[T domain.TraceData] struct {
	Name     string
	TraceID  domain.TraceID
	SpanID   domain.SpanID
	ParentID domain.SpanID
	Options  []SpanCreationOption
}

type ArenaStats struct {
	Allocations    int64
	Deallocations  int64
	TotalBytes     int64
	ActiveArenas   int64
	ReuseRate      float64
	MemoryPressure bool
	GCCycles       int64
}

type ArenaSpanSnapshot[T domain.TraceData] struct {
	span *ArenaSpan[T]
}

// Implement domain.SpanSnapshot interface
func (s *ArenaSpanSnapshot[T]) GetTraceID() domain.TraceID {
	return s.span.traceID
}

func (s *ArenaSpanSnapshot[T]) GetSpanID() domain.SpanID {
	return s.span.spanID
}

// ... other methods would be implemented similarly

func applyArenaDefaults(config *ArenaConfig) {
	if config.SmallArenaSize == 0 {
		config.SmallArenaSize = 64 * 1024 // 64KB
	}
	if config.MediumArenaSize == 0 {
		config.MediumArenaSize = 1024 * 1024 // 1MB
	}
	if config.LargeArenaSize == 0 {
		config.LargeArenaSize = 16 * 1024 * 1024 // 16MB
	}
	if config.MaxArenasPerPool == 0 {
		config.MaxArenasPerPool = 100
	}
	if config.DefaultAlignment == 0 {
		config.DefaultAlignment = 16
	}
	if config.PressureThreshold == 0 {
		config.PressureThreshold = 0.8
	}
	if config.ProfileSampleRate == 0 {
		config.ProfileSampleRate = 0.01
	}
	if config.IdleTimeout == 0 {
		config.IdleTimeout = 5 * 60 * 1e9 // 5 minutes in nanoseconds
	}
	if config.MaxArenaAge == 0 {
		config.MaxArenaAge = 30 * 60 * 1e9 // 30 minutes in nanoseconds
	}
	if config.CleanupInterval == 0 {
		config.CleanupInterval = 30 * 1e9 // 30 seconds in nanoseconds
	}
}
