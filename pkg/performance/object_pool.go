package performance

import (
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"
)

// ObjectPool is a high-performance object pool
type ObjectPool struct {
	// Per-CPU pools for better cache locality
	localPools []localPool
	
	// Global pool for overflow
	globalPool *RingBuffer
	
	// Object factory
	factory func() interface{}
	reset   func(interface{})
	
	// Metrics
	allocations atomic.Uint64
	recycled    atomic.Uint64
	inUse       atomic.Int64
	
	// Configuration
	maxPerCPU   int
	maxGlobal   int
}

// localPool is a per-CPU pool
type localPool struct {
	_       [128]byte // padding
	objects []*poolObject
	head    int
	tail    int
	size    int
	_       [128]byte // padding
}

// poolObject wraps an object with metadata
type poolObject struct {
	value     interface{}
	lastUsed  int64
	useCount  uint32
}

// ObjectPoolConfig configures the object pool
type ObjectPoolConfig struct {
	Factory    func() interface{}
	Reset      func(interface{})
	MaxPerCPU  int
	MaxGlobal  int
}

// NewObjectPool creates a new object pool
func NewObjectPool(config ObjectPoolConfig) (*ObjectPool, error) {
	if config.Factory == nil {
		config.Factory = func() interface{} { return nil }
	}
	if config.MaxPerCPU == 0 {
		config.MaxPerCPU = 64
	}
	if config.MaxGlobal == 0 {
		config.MaxGlobal = 1024
	}

	numCPU := runtime.GOMAXPROCS(0)
	localPools := make([]localPool, numCPU)
	
	for i := range localPools {
		localPools[i].objects = make([]*poolObject, config.MaxPerCPU)
	}

	globalCapacity := uint64(config.MaxGlobal)
	// Ensure power of 2
	globalCapacity = nextPowerOf2(globalCapacity)
	
	globalPool, err := NewRingBuffer(globalCapacity)
	if err != nil {
		return nil, err
	}

	return &ObjectPool{
		localPools: localPools,
		globalPool: globalPool,
		factory:    config.Factory,
		reset:      config.Reset,
		maxPerCPU:  config.MaxPerCPU,
		maxGlobal:  config.MaxGlobal,
	}, nil
}

// Get retrieves an object from the pool
func (p *ObjectPool) Get() interface{} {
	// Try local pool first
	pid := runtime_procPin()
	local := &p.localPools[pid]
	
	if local.size > 0 {
		// Get from local pool
		obj := local.objects[local.head]
		local.objects[local.head] = nil
		local.head = (local.head + 1) % p.maxPerCPU
		local.size--
		runtime_procUnpin()
		
		p.inUse.Add(1)
		return obj.value
	}
	runtime_procUnpin()
	
	// Try global pool
	if ptr, err := p.globalPool.Get(); err == nil {
		obj := (*poolObject)(ptr)
		p.inUse.Add(1)
		return obj.value
	}
	
	// Create new object
	p.allocations.Add(1)
	p.inUse.Add(1)
	return p.factory()
}

// Put returns an object to the pool
func (p *ObjectPool) Put(obj interface{}) {
	if obj == nil {
		return
	}
	
	// Reset object if reset function is provided
	if p.reset != nil {
		p.reset(obj)
	}
	
	p.inUse.Add(-1)
	
	// Wrap in pool object
	poolObj := &poolObject{
		value:    obj,
		lastUsed: nanotime(),
		useCount: 1,
	}
	
	// Try local pool first
	pid := runtime_procPin()
	local := &p.localPools[pid]
	
	if local.size < p.maxPerCPU {
		// Add to local pool
		local.objects[local.tail] = poolObj
		local.tail = (local.tail + 1) % p.maxPerCPU
		local.size++
		runtime_procUnpin()
		
		p.recycled.Add(1)
		return
	}
	runtime_procUnpin()
	
	// Try global pool
	if p.globalPool.TryPut(unsafe.Pointer(poolObj)) {
		p.recycled.Add(1)
		return
	}
	
	// Pool is full, let GC handle it
}

// GetMetrics returns pool metrics
func (p *ObjectPool) GetMetrics() ObjectPoolMetrics {
	localSize := 0
	for i := range p.localPools {
		localSize += p.localPools[i].size
	}
	
	return ObjectPoolMetrics{
		Allocations:  p.allocations.Load(),
		Recycled:     p.recycled.Load(),
		InUse:        p.inUse.Load(),
		LocalSize:    localSize,
		GlobalSize:   int(p.globalPool.Size()),
		TotalSize:    localSize + int(p.globalPool.Size()),
	}
}

// ObjectPoolMetrics contains pool metrics
type ObjectPoolMetrics struct {
	Allocations  uint64
	Recycled     uint64
	InUse        int64
	LocalSize    int
	GlobalSize   int
	TotalSize    int
}

// TypedPool is a type-safe wrapper around ObjectPool
type TypedPool[T any] struct {
	pool    *ObjectPool
	factory func() *T
	reset   func(*T)
}

// NewTypedPool creates a new typed pool
func NewTypedPool[T any](factory func() *T, reset func(*T), maxPerCPU, maxGlobal int) (*TypedPool[T], error) {
	pool, err := NewObjectPool(ObjectPoolConfig{
		Factory: func() interface{} {
			return factory()
		},
		Reset: func(obj interface{}) {
			if reset != nil && obj != nil {
				reset(obj.(*T))
			}
		},
		MaxPerCPU: maxPerCPU,
		MaxGlobal: maxGlobal,
	})
	
	if err != nil {
		return nil, err
	}
	
	return &TypedPool[T]{
		pool:    pool,
		factory: factory,
		reset:   reset,
	}, nil
}

// Get retrieves a typed object
func (p *TypedPool[T]) Get() *T {
	obj := p.pool.Get()
	if obj == nil {
		return p.factory()
	}
	return obj.(*T)
}

// Put returns a typed object
func (p *TypedPool[T]) Put(obj *T) {
	if obj != nil {
		p.pool.Put(obj)
	}
}

// GetMetrics returns pool metrics
func (p *TypedPool[T]) GetMetrics() ObjectPoolMetrics {
	return p.pool.GetMetrics()
}

// SlicePool pools byte slices of specific sizes
type SlicePool struct {
	pools map[int]*sync.Pool
	sizes []int
	mutex sync.RWMutex
}

// NewSlicePool creates a new slice pool
func NewSlicePool(sizes ...int) *SlicePool {
	sp := &SlicePool{
		pools: make(map[int]*sync.Pool),
		sizes: sizes,
	}
	
	for _, size := range sizes {
		size := size // capture
		sp.pools[size] = &sync.Pool{
			New: func() interface{} {
				return make([]byte, size)
			},
		}
	}
	
	return sp
}

// Get retrieves a slice of at least the requested size
func (sp *SlicePool) Get(size int) []byte {
	sp.mutex.RLock()
	defer sp.mutex.RUnlock()
	
	// Find the smallest suitable size
	bestSize := -1
	for _, s := range sp.sizes {
		if s >= size && (bestSize == -1 || s < bestSize) {
			bestSize = s
		}
	}
	
	if bestSize != -1 {
		if pool, ok := sp.pools[bestSize]; ok {
			slice := pool.Get().([]byte)
			return slice[:size]
		}
	}
	
	// No suitable size, allocate new
	return make([]byte, size)
}

// Put returns a slice to the pool
func (sp *SlicePool) Put(slice []byte) {
	if slice == nil {
		return
	}
	
	sp.mutex.RLock()
	defer sp.mutex.RUnlock()
	
	// Find matching pool by capacity
	cap := cap(slice)
	if pool, ok := sp.pools[cap]; ok {
		// Reset slice
		slice = slice[:cap]
		for i := range slice {
			slice[i] = 0
		}
		pool.Put(slice)
	}
}

// Helper functions

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

// Runtime functions (these would be implemented in assembly for production)

//go:linkname runtime_procPin runtime.procPin
func runtime_procPin() int

//go:linkname runtime_procUnpin runtime.procUnpin
func runtime_procUnpin()

//go:linkname nanotime runtime.nanotime
func nanotime() int64