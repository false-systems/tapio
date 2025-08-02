package performance

import (
	"runtime"
	"sync"

	"github.com/yairfalse/tapio/pkg/domain"
)

// ObjectPool is a generic object pool with per-CPU optimization
type ObjectPool[T any] struct {
	pools []sync.Pool
	size  int
	New   func() T
	Reset func(*T)
}

// NewObjectPool creates a new object pool
func NewObjectPool[T any](factory func() T, reset func(*T)) *ObjectPool[T] {
	cpus := runtime.NumCPU()
	pools := make([]sync.Pool, cpus)

	for i := range pools {
		pools[i] = sync.Pool{
			New: func() interface{} {
				return factory()
			},
		}
	}

	return &ObjectPool[T]{
		pools: pools,
		size:  cpus,
		New:   factory,
		Reset: reset,
	}
}

// Get retrieves an object from the pool
func (p *ObjectPool[T]) Get() *T {
	// Use current CPU to reduce contention
	pid := runtime.NumGoroutine() % p.size
	obj := p.pools[pid].Get().(T)
	return &obj
}

// Put returns an object to the pool
func (p *ObjectPool[T]) Put(obj *T) {
	if p.Reset != nil {
		p.Reset(obj)
	}
	pid := runtime.NumGoroutine() % p.size
	p.pools[pid].Put(*obj)
}

// UnifiedEventPool is a specialized pool for UnifiedEvent
type UnifiedEventPool struct {
	pool  *ObjectPool[domain.UnifiedEvent]
	stats PoolStats
	mu    sync.Mutex
}

// PoolStats tracks pool usage statistics
type PoolStats struct {
	Allocated int64
	Recycled  int64
	InUse     int64
}

// NewUnifiedEventPool creates a new UnifiedEvent pool
func NewUnifiedEventPool() *UnifiedEventPool {
	return &UnifiedEventPool{
		pool: NewObjectPool(
			func() domain.UnifiedEvent {
				return domain.UnifiedEvent{}
			},
			func(e *domain.UnifiedEvent) {
				// Reset event to zero value
				*e = domain.UnifiedEvent{}
			},
		),
	}
}

// Get retrieves an event from the pool
func (p *UnifiedEventPool) Get() *domain.UnifiedEvent {
	p.mu.Lock()
	p.stats.Allocated++
	p.stats.InUse++
	p.mu.Unlock()

	return p.pool.Get()
}

// Put returns an event to the pool
func (p *UnifiedEventPool) Put(event *domain.UnifiedEvent) {
	p.mu.Lock()
	p.stats.Recycled++
	p.stats.InUse--
	p.mu.Unlock()

	p.pool.Put(event)
}

// GetStats returns pool statistics
func (p *UnifiedEventPool) GetStats() PoolStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stats
}

// ByteSlicePool is a pool for byte slices
type ByteSlicePool struct {
	pools map[int]*sync.Pool
	mu    sync.RWMutex
}

// NewByteSlicePool creates a new byte slice pool
func NewByteSlicePool() *ByteSlicePool {
	return &ByteSlicePool{
		pools: make(map[int]*sync.Pool),
	}
}

// Get retrieves a byte slice of the requested size
func (p *ByteSlicePool) Get(size int) []byte {
	// Round up to power of 2
	poolSize := 1
	for poolSize < size {
		poolSize *= 2
	}

	p.mu.RLock()
	pool, exists := p.pools[poolSize]
	p.mu.RUnlock()

	if !exists {
		p.mu.Lock()
		pool = &sync.Pool{
			New: func() interface{} {
				return make([]byte, poolSize)
			},
		}
		p.pools[poolSize] = pool
		p.mu.Unlock()
	}

	buf := pool.Get().([]byte)
	return buf[:size]
}

// Put returns a byte slice to the pool
func (p *ByteSlicePool) Put(buf []byte) {
	size := cap(buf)

	// Find the pool size
	poolSize := 1
	for poolSize < size {
		poolSize *= 2
	}

	p.mu.RLock()
	pool, exists := p.pools[poolSize]
	p.mu.RUnlock()

	if exists {
		pool.Put(buf[:poolSize])
	}
}
