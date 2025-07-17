package internal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/ebpf_new/core"
	"github.com/yairfalse/tapio/pkg/domain"
)

// subscription represents an active event subscription
type subscription struct {
	id       string
	options  domain.SubscriptionOptions
	stream   *eventStream
	ctx      context.Context
	created  time.Time
}

// rateLimiter implements a simple token bucket rate limiter
type rateLimiter struct {
	maxPerSecond int
	tokens       atomic.Int64
	lastRefill   atomic.Int64 // Unix nano
	mu           sync.Mutex
}

func newRateLimiter(maxPerSecond int) *rateLimiter {
	rl := &rateLimiter{
		maxPerSecond: maxPerSecond,
	}
	rl.tokens.Store(int64(maxPerSecond))
	rl.lastRefill.Store(time.Now().UnixNano())
	return rl
}

func (rl *rateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now().UnixNano()
	lastRefill := rl.lastRefill.Load()
	elapsed := now - lastRefill

	// Refill tokens based on elapsed time
	if elapsed > int64(time.Second) {
		tokensToAdd := int64(rl.maxPerSecond) * elapsed / int64(time.Second)
		currentTokens := rl.tokens.Load()
		newTokens := currentTokens + tokensToAdd
		if newTokens > int64(rl.maxPerSecond) {
			newTokens = int64(rl.maxPerSecond)
		}
		rl.tokens.Store(newTokens)
		rl.lastRefill.Store(now)
	}

	// Try to consume a token
	currentTokens := rl.tokens.Load()
	if currentTokens > 0 {
		rl.tokens.Store(currentTokens - 1)
		return true
	}

	return false
}

// generateSubscriptionID generates a unique subscription ID
func generateSubscriptionID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based ID
		return time.Now().Format("20060102150405.999999999")
	}
	return hex.EncodeToString(bytes)
}

// isSystemProcess checks if a process is a system process
func isSystemProcess(info core.ProcessInfo) bool {
	// Check for kernel threads (PID < 1000 is a simple heuristic)
	if info.PID < 1000 {
		return true
	}

	// Check for common system process names
	systemProcesses := []string{
		"systemd", "init", "kernel", "kworker", "ksoftirqd",
		"migration", "rcu_", "watchdog", "kthreadd", "kdevtmpfs",
		"netns", "khungtaskd", "oom_reaper", "writeback", "kcompactd",
		"kintegrityd", "kblockd", "kswapd", "systemd-",
	}

	for _, prefix := range systemProcesses {
		if len(info.Name) >= len(prefix) && info.Name[:len(prefix)] == prefix {
			return true
		}
	}

	// Check if it's a kernel thread (PPID = 2)
	if info.PPID == 2 {
		return true
	}

	return false
}

// eventBatch represents a batch of events for processing
type eventBatch struct {
	events    []domain.Event
	timestamp time.Time
	source    string
}

// metricCollector collects internal metrics
type metricCollector struct {
	mu      sync.RWMutex
	metrics map[string]*metric
}

type metric struct {
	name  string
	value atomic.Uint64
	unit  string
}

func newMetricCollector() *metricCollector {
	return &metricCollector{
		metrics: make(map[string]*metric),
	}
}

func (mc *metricCollector) increment(name string) {
	mc.mu.RLock()
	m, exists := mc.metrics[name]
	mc.mu.RUnlock()

	if exists {
		m.value.Add(1)
		return
	}

	mc.mu.Lock()
	if _, exists := mc.metrics[name]; !exists {
		mc.metrics[name] = &metric{
			name: name,
			unit: "count",
		}
	}
	mc.mu.Unlock()

	mc.metrics[name].value.Add(1)
}

func (mc *metricCollector) add(name string, value uint64) {
	mc.mu.RLock()
	m, exists := mc.metrics[name]
	mc.mu.RUnlock()

	if exists {
		m.value.Add(value)
		return
	}

	mc.mu.Lock()
	if _, exists := mc.metrics[name]; !exists {
		mc.metrics[name] = &metric{
			name: name,
			unit: "count",
		}
	}
	mc.mu.Unlock()

	mc.metrics[name].value.Add(value)
}

func (mc *metricCollector) get(name string) uint64 {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	if m, exists := mc.metrics[name]; exists {
		return m.value.Load()
	}
	return 0
}

// circularBuffer implements a fixed-size circular buffer for events
type circularBuffer struct {
	mu       sync.RWMutex
	events   []domain.Event
	capacity int
	head     int
	tail     int
	size     int
}

func newCircularBuffer(capacity int) *circularBuffer {
	return &circularBuffer{
		events:   make([]domain.Event, capacity),
		capacity: capacity,
	}
}

func (cb *circularBuffer) Add(event domain.Event) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.events[cb.tail] = event
	cb.tail = (cb.tail + 1) % cb.capacity

	if cb.size < cb.capacity {
		cb.size++
	} else {
		// Buffer is full, advance head
		cb.head = (cb.head + 1) % cb.capacity
	}
}

func (cb *circularBuffer) GetAll() []domain.Event {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if cb.size == 0 {
		return nil
	}

	result := make([]domain.Event, cb.size)
	for i := 0; i < cb.size; i++ {
		idx := (cb.head + i) % cb.capacity
		result[i] = cb.events[idx]
	}

	return result
}

func (cb *circularBuffer) GetRecent(n int) []domain.Event {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if cb.size == 0 || n <= 0 {
		return nil
	}

	if n > cb.size {
		n = cb.size
	}

	result := make([]domain.Event, n)
	start := (cb.tail - n + cb.capacity) % cb.capacity
	for i := 0; i < n; i++ {
		idx := (start + i) % cb.capacity
		result[i] = cb.events[idx]
	}

	return result
}

// programMetadata stores additional metadata about loaded programs
type programMetadata struct {
	mu       sync.RWMutex
	metadata map[string]*programMeta
}

type programMeta struct {
	loadedAt    time.Time
	attachedTo  string
	programType core.ProgramType
	bytecode    []byte
}

func newProgramMetadata() *programMetadata {
	return &programMetadata{
		metadata: make(map[string]*programMeta),
	}
}

func (pm *programMetadata) Add(name string, meta *programMeta) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.metadata[name] = meta
}

func (pm *programMetadata) Get(name string) (*programMeta, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	meta, ok := pm.metadata[name]
	return meta, ok
}

func (pm *programMetadata) Remove(name string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.metadata, name)
}

// bytesPool provides a pool of byte slices for efficient memory usage
type bytesPool struct {
	pool sync.Pool
}

func newBytesPool(size int) *bytesPool {
	return &bytesPool{
		pool: sync.Pool{
			New: func() interface{} {
				b := make([]byte, size)
				return &b
			},
		},
	}
}

func (bp *bytesPool) Get() *[]byte {
	return bp.pool.Get().(*[]byte)
}

func (bp *bytesPool) Put(b *[]byte) {
	if b != nil && cap(*b) > 0 {
		*b = (*b)[:0]
		bp.pool.Put(b)
	}
}