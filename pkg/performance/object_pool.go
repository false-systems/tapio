package performance

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// ObjectPool provides efficient object reuse to reduce GC pressure.
// Uses per-CPU pools for better cache locality and reduced contention.
type ObjectPool[T any] struct {
	// Per-CPU pools for better cache locality
	pools []sync.Pool
	// Factory function to create new objects
	factory func() T
	// Reset function to clean objects before reuse
	reset func(*T)
	// Statistics
	allocated atomic.Int64
	recycled  atomic.Int64
	inUse     atomic.Int64
}

// NewObjectPool creates a new object pool with the given factory and reset functions
func NewObjectPool[T any](factory func() T, reset func(*T)) *ObjectPool[T] {
	numCPU := runtime.NumCPU()
	pools := make([]sync.Pool, numCPU)

	pool := &ObjectPool[T]{
		pools:   pools,
		factory: factory,
		reset:   reset,
	}

	// Initialize each CPU's pool
	for i := range pools {
		pools[i].New = func() interface{} {
			pool.allocated.Add(1)
			obj := factory()
			return &obj
		}
	}

	return pool
}

// Get retrieves an object from the pool
func (p *ObjectPool[T]) Get() *T {
	p.inUse.Add(1)

	// Get from CPU-local pool
	cpu := runtime_procPin()
	obj := p.pools[cpu%len(p.pools)].Get().(*T)
	runtime_procUnpin()

	return obj
}

// Put returns an object to the pool after resetting it
func (p *ObjectPool[T]) Put(obj *T) {
	if obj == nil {
		return
	}

	// Reset the object
	if p.reset != nil {
		p.reset(obj)
	}

	p.inUse.Add(-1)
	p.recycled.Add(1)

	// Return to CPU-local pool
	cpu := runtime_procPin()
	p.pools[cpu%len(p.pools)].Put(obj)
	runtime_procUnpin()
}

// Stats returns pool statistics
type PoolStats struct {
	Allocated int64
	Recycled  int64
	InUse     int64
}

// GetStats returns current pool statistics
func (p *ObjectPool[T]) GetStats() PoolStats {
	return PoolStats{
		Allocated: p.allocated.Load(),
		Recycled:  p.recycled.Load(),
		InUse:     p.inUse.Load(),
	}
}

// UnifiedEventPool provides a specialized pool for UnifiedEvent objects
type UnifiedEventPool struct {
	pool *ObjectPool[domain.UnifiedEvent]
}

// NewUnifiedEventPool creates a pool optimized for UnifiedEvent objects
func NewUnifiedEventPool() *UnifiedEventPool {
	factory := func() domain.UnifiedEvent {
		return domain.UnifiedEvent{
			// Pre-allocate commonly used maps
			TraceContext: &domain.TraceContext{
				Baggage: make(map[string]string),
			},
			Entity: &domain.EntityContext{
				Labels:     make(map[string]string),
				Attributes: make(map[string]string),
			},
		}
	}

	reset := func(e *domain.UnifiedEvent) {
		// Reset event to clean state
		e.ID = ""
		e.Type = ""
		e.Source = ""
		e.Timestamp = time.Time{} // Reset to zero time

		// Clear maps without reallocating
		if e.TraceContext != nil && e.TraceContext.Baggage != nil {
			for k := range e.TraceContext.Baggage {
				delete(e.TraceContext.Baggage, k)
			}
		}

		if e.Entity != nil {
			e.Entity.Type = ""
			e.Entity.Name = ""
			e.Entity.Namespace = ""
			e.Entity.UID = ""
			if e.Entity.Labels != nil {
				for k := range e.Entity.Labels {
					delete(e.Entity.Labels, k)
				}
			}
			if e.Entity.Attributes != nil {
				for k := range e.Entity.Attributes {
					delete(e.Entity.Attributes, k)
				}
			}
		}

		// Clear layer-specific data
		e.Kernel = nil
		e.Network = nil
		e.Application = nil
		e.Kubernetes = nil
		e.Metrics = nil

		// Clear contexts
		e.Semantic = nil
		e.Impact = nil
		e.Correlation = nil

		// Clear raw data
		e.RawData = e.RawData[:0]
	}

	return &UnifiedEventPool{
		pool: NewObjectPool(factory, reset),
	}
}

// Get retrieves a UnifiedEvent from the pool
func (p *UnifiedEventPool) Get() *domain.UnifiedEvent {
	return p.pool.Get()
}

// Put returns a UnifiedEvent to the pool
func (p *UnifiedEventPool) Put(event *domain.UnifiedEvent) {
	p.pool.Put(event)
}

// GetStats returns pool statistics
func (p *UnifiedEventPool) GetStats() PoolStats {
	return p.pool.GetStats()
}

// ByteSlicePool provides a pool for byte slices of various sizes
type ByteSlicePool struct {
	pools map[int]*sync.Pool
	sizes []int
}

// NewByteSlicePool creates pools for common byte slice sizes
func NewByteSlicePool() *ByteSlicePool {
	sizes := []int{
		64,    // Small messages
		512,   // Medium messages
		4096,  // Large messages
		65536, // Jumbo messages
	}

	pools := make(map[int]*sync.Pool)
	for _, size := range sizes {
		s := size // Capture loop variable
		pools[size] = &sync.Pool{
			New: func() interface{} {
				b := make([]byte, s)
				return &b
			},
		}
	}

	return &ByteSlicePool{
		pools: pools,
		sizes: sizes,
	}
}

// Get retrieves a byte slice of at least the requested size
func (p *ByteSlicePool) Get(size int) []byte {
	// Find the smallest pool that fits
	for _, poolSize := range p.sizes {
		if poolSize >= size {
			buf := p.pools[poolSize].Get().(*[]byte)
			return (*buf)[:size]
		}
	}

	// No pool large enough, allocate new
	return make([]byte, size)
}

// Put returns a byte slice to the appropriate pool
func (p *ByteSlicePool) Put(buf []byte) {
	size := cap(buf)

	// Find matching pool
	for _, poolSize := range p.sizes {
		if poolSize == size {
			// Reset slice to full capacity before returning
			buf = buf[:poolSize]
			p.pools[poolSize].Put(&buf)
			return
		}
	}

	// No matching pool, let GC handle it
}

// CPU pinning stubs - these would use runtime internals in production
func runtime_procPin() int {
	// In production, this would pin the goroutine to current CPU
	// For now, return CPU based on goroutine ID
	return int(getGoroutineID() % uint64(runtime.NumCPU()))
}

func runtime_procUnpin() {
	// In production, this would unpin the goroutine
}

func getGoroutineID() uint64 {
	// Simplified version - in production use runtime internals
	return uint64(runtime.NumGoroutine())
}
