package discovery

import (
	"context"
	"hash/fnv"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// TTLCache implements Cache interface with time-to-live support and sync.Pool optimization
type TTLCache struct {
	// Configuration
	maxSize         int64
	defaultTTL      time.Duration
	cleanupInterval time.Duration

	// State
	mu      sync.RWMutex
	items   map[string]*cacheItem
	lruList *lruNode
	size    int64

	// Metrics
	hits      int64
	misses    int64
	evictions int64

	// Cleanup
	stopCleanup chan struct{}

	// Object pools for memory efficiency
	itemPool sync.Pool
	nodePool sync.Pool

	// Sharding for better concurrency
	shards    []*cacheShard
	shardMask uint64

	// Logger
	logger *slog.Logger
}

// cacheItem represents a cached value with TTL
type cacheItem struct {
	key       string
	value     interface{}
	expiresAt time.Time
	size      int64

	// LRU tracking
	lruNode *lruNode

	// Frequency tracking for LFU eviction
	frequency  int64
	lastAccess time.Time
}

// lruNode represents a node in the LRU doubly-linked list
type lruNode struct {
	prev *lruNode
	next *lruNode
	item *cacheItem
}

// cacheShard provides thread-safe access to a portion of the cache
type cacheShard struct {
	mu      sync.RWMutex
	items   map[string]*cacheItem
	lruHead *lruNode
	lruTail *lruNode
	size    int64
}

// NewTTLCache creates a new TTL cache with sync.Pool optimization
func NewTTLCache(maxSize int64, defaultTTL time.Duration) *TTLCache {
	if maxSize <= 0 {
		maxSize = 1000
	}
	if defaultTTL <= 0 {
		defaultTTL = 5 * time.Minute
	}

	// Use 16 shards for good concurrency with reasonable memory overhead
	const numShards = 16
	shards := make([]*cacheShard, numShards)

	for i := 0; i < numShards; i++ {
		shard := &cacheShard{
			items: make(map[string]*cacheItem),
		}

		// Initialize LRU list
		shard.lruHead = &lruNode{}
		shard.lruTail = &lruNode{}
		shard.lruHead.next = shard.lruTail
		shard.lruTail.prev = shard.lruHead

		shards[i] = shard
	}

	cache := &TTLCache{
		maxSize:         maxSize,
		defaultTTL:      defaultTTL,
		cleanupInterval: defaultTTL / 10, // Cleanup every 10% of TTL
		items:           make(map[string]*cacheItem),
		stopCleanup:     make(chan struct{}),
		shards:          shards,
		shardMask:       numShards - 1,
		logger:          slog.Default().With("component", "cache"),
	}

	// Initialize object pools
	cache.itemPool.New = func() interface{} {
		return &cacheItem{}
	}
	cache.nodePool.New = func() interface{} {
		return &lruNode{}
	}

	// Start cleanup goroutine
	go cache.cleanup()

	return cache
}

// Get retrieves cached discovery results
func (c *TTLCache) Get(ctx context.Context, key CacheKey) (interface{}, bool) {
	keyStr := c.buildKeyString(key)
	shard := c.getShard(keyStr)

	shard.mu.RLock()
	item, exists := shard.items[keyStr]
	shard.mu.RUnlock()

	if !exists {
		atomic.AddInt64(&c.misses, 1)
		return nil, false
	}

	// Check expiration
	if time.Now().After(item.expiresAt) {
		// Item expired, remove it
		c.removeFromShard(shard, keyStr)
		atomic.AddInt64(&c.misses, 1)
		return nil, false
	}

	// Update access statistics
	atomic.AddInt64(&item.frequency, 1)
	item.lastAccess = time.Now()

	// Move to front of LRU list
	c.moveToFront(shard, item)

	atomic.AddInt64(&c.hits, 1)
	return item.value, true
}

// Set stores discovery results with TTL
func (c *TTLCache) Set(ctx context.Context, key CacheKey, value interface{}, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = c.defaultTTL
	}

	keyStr := c.buildKeyString(key)
	shard := c.getShard(keyStr)

	// Estimate size (rough approximation)
	size := c.estimateSize(value)

	// Check if we need to evict before adding
	if atomic.LoadInt64(&c.size)+size > c.maxSize {
		c.evictLRU(shard, size)
	}

	// Get item from pool
	item := c.getItem()
	item.key = keyStr
	item.value = value
	item.expiresAt = time.Now().Add(ttl)
	item.size = size
	item.frequency = 1
	item.lastAccess = time.Now()

	shard.mu.Lock()

	// Check if item already exists
	if existingItem, exists := shard.items[keyStr]; exists {
		// Update existing item
		if existingItem.lruNode != nil {
			c.removeFromLRUList(shard, existingItem.lruNode)
			c.putNode(existingItem.lruNode)
		}
		atomic.AddInt64(&c.size, -existingItem.size)
		c.putItem(existingItem)
	}

	// Add new item
	shard.items[keyStr] = item
	node := c.addToFront(shard, item)
	item.lruNode = node

	shard.mu.Unlock()

	atomic.AddInt64(&c.size, size)
	atomic.AddInt64(&shard.size, size)

	return nil
}

// Invalidate removes specific cache entries
func (c *TTLCache) Invalidate(ctx context.Context, pattern string) error {
	// Simple pattern matching - in production, could use more sophisticated patterns
	for _, shard := range c.shards {
		c.invalidateShard(shard, pattern)
	}
	return nil
}

// Clear removes all cache entries
func (c *TTLCache) Clear(ctx context.Context) error {
	for _, shard := range c.shards {
		c.clearShard(shard)
	}

	atomic.StoreInt64(&c.size, 0)
	atomic.StoreInt64(&c.hits, 0)
	atomic.StoreInt64(&c.misses, 0)
	atomic.StoreInt64(&c.evictions, 0)

	return nil
}

// Stats returns cache performance metrics
func (c *TTLCache) Stats() CacheStats {
	hits := atomic.LoadInt64(&c.hits)
	misses := atomic.LoadInt64(&c.misses)
	total := hits + misses

	var hitRate, missRate float64
	if total > 0 {
		hitRate = float64(hits) / float64(total)
		missRate = float64(misses) / float64(total)
	}

	// Count total entries across all shards
	var totalEntries int
	for _, shard := range c.shards {
		shard.mu.RLock()
		totalEntries += len(shard.items)
		shard.mu.RUnlock()
	}

	return CacheStats{
		HitRate:     hitRate,
		MissRate:    missRate,
		Size:        atomic.LoadInt64(&c.size),
		Entries:     totalEntries,
		Evictions:   atomic.LoadInt64(&c.evictions),
		LastCleanup: time.Now(), // Approximation
	}
}

// Private methods

func (c *TTLCache) buildKeyString(key CacheKey) string {
	if key.Version == "" {
		return key.Namespace + ":" + key.Key
	}
	return key.Namespace + ":" + key.Key + ":" + key.Version
}

func (c *TTLCache) getShard(key string) *cacheShard {
	hash := c.hash(key)
	return c.shards[hash&c.shardMask]
}

func (c *TTLCache) hash(key string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(key))
	return h.Sum64()
}

func (c *TTLCache) estimateSize(value interface{}) int64 {
	// Rough size estimation - in production, could use more sophisticated methods
	switch v := value.(type) {
	case string:
		return int64(len(v))
	case []byte:
		return int64(len(v))
	case []KubernetesService:
		return int64(len(v) * 1024) // Rough estimate: 1KB per service
	case []LocalService:
		return int64(len(v) * 512) // Rough estimate: 512B per service
	default:
		return 1024 // Default size estimate
	}
}

func (c *TTLCache) evictLRU(shard *cacheShard, neededSize int64) {
	shard.mu.Lock()
	defer shard.mu.Unlock()

	// Evict from tail (least recently used) until we have enough space
	current := shard.lruTail.prev
	freedSize := int64(0)

	for current != shard.lruHead && freedSize < neededSize {
		item := current.item
		next := current.prev

		// Remove from cache
		delete(shard.items, item.key)
		c.removeFromLRUList(shard, current)

		// Update metrics
		freedSize += item.size
		atomic.AddInt64(&c.size, -item.size)
		atomic.AddInt64(&shard.size, -item.size)
		atomic.AddInt64(&c.evictions, 1)

		// Return to pool
		c.putItem(item)
		c.putNode(current)

		current = next
	}
}

func (c *TTLCache) moveToFront(shard *cacheShard, item *cacheItem) {
	shard.mu.Lock()
	defer shard.mu.Unlock()

	if item.lruNode != nil {
		c.removeFromLRUList(shard, item.lruNode)
		c.addToFrontOfLRUList(shard, item.lruNode)
	}
}

func (c *TTLCache) addToFront(shard *cacheShard, item *cacheItem) *lruNode {
	node := c.getNode()
	node.item = item
	c.addToFrontOfLRUList(shard, node)
	return node
}

func (c *TTLCache) addToFrontOfLRUList(shard *cacheShard, node *lruNode) {
	node.next = shard.lruHead.next
	node.prev = shard.lruHead
	shard.lruHead.next.prev = node
	shard.lruHead.next = node
}

func (c *TTLCache) removeFromLRUList(shard *cacheShard, node *lruNode) {
	node.prev.next = node.next
	node.next.prev = node.prev
}

func (c *TTLCache) removeFromShard(shard *cacheShard, key string) {
	shard.mu.Lock()
	defer shard.mu.Unlock()

	if item, exists := shard.items[key]; exists {
		delete(shard.items, key)

		if item.lruNode != nil {
			c.removeFromLRUList(shard, item.lruNode)
			c.putNode(item.lruNode)
		}

		atomic.AddInt64(&c.size, -item.size)
		atomic.AddInt64(&shard.size, -item.size)

		c.putItem(item)
	}
}

func (c *TTLCache) invalidateShard(shard *cacheShard, pattern string) {
	shard.mu.Lock()
	defer shard.mu.Unlock()

	var toRemove []string

	for key := range shard.items {
		// Simple pattern matching - could be enhanced with regex or glob patterns
		if pattern == "*" || key == pattern {
			toRemove = append(toRemove, key)
		}
	}

	for _, key := range toRemove {
		if item, exists := shard.items[key]; exists {
			delete(shard.items, key)

			if item.lruNode != nil {
				c.removeFromLRUList(shard, item.lruNode)
				c.putNode(item.lruNode)
			}

			atomic.AddInt64(&c.size, -item.size)
			atomic.AddInt64(&shard.size, -item.size)

			c.putItem(item)
		}
	}
}

func (c *TTLCache) clearShard(shard *cacheShard) {
	shard.mu.Lock()
	defer shard.mu.Unlock()

	// Return all items and nodes to pools
	for _, item := range shard.items {
		if item.lruNode != nil {
			c.putNode(item.lruNode)
		}
		c.putItem(item)
	}

	// Clear maps and reset LRU list
	shard.items = make(map[string]*cacheItem)
	shard.lruHead.next = shard.lruTail
	shard.lruTail.prev = shard.lruHead
	atomic.StoreInt64(&shard.size, 0)
}

func (c *TTLCache) cleanup() {
	ticker := time.NewTicker(c.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCleanup:
			return
		case <-ticker.C:
			c.cleanupExpired()
		}
	}
}

func (c *TTLCache) cleanupExpired() {
	now := time.Now()

	for _, shard := range c.shards {
		c.cleanupShardExpired(shard, now)
	}
}

func (c *TTLCache) cleanupShardExpired(shard *cacheShard, now time.Time) {
	shard.mu.Lock()
	defer shard.mu.Unlock()

	var expired []string

	for key, item := range shard.items {
		if now.After(item.expiresAt) {
			expired = append(expired, key)
		}
	}

	for _, key := range expired {
		if item, exists := shard.items[key]; exists {
			delete(shard.items, key)

			if item.lruNode != nil {
				c.removeFromLRUList(shard, item.lruNode)
				c.putNode(item.lruNode)
			}

			atomic.AddInt64(&c.size, -item.size)
			atomic.AddInt64(&shard.size, -item.size)

			c.putItem(item)
		}
	}

	if len(expired) > 0 {
		c.logger.Debug("Cleaned up expired cache entries",
			"shard", shard,
			"expired_count", len(expired))
	}
}

// Object pool helpers for memory efficiency

func (c *TTLCache) getItem() *cacheItem {
	item := c.itemPool.Get().(*cacheItem)
	// Reset item
	*item = cacheItem{}
	return item
}

func (c *TTLCache) putItem(item *cacheItem) {
	c.itemPool.Put(item)
}

func (c *TTLCache) getNode() *lruNode {
	node := c.nodePool.Get().(*lruNode)
	// Reset node
	*node = lruNode{}
	return node
}

func (c *TTLCache) putNode(node *lruNode) {
	c.nodePool.Put(node)
}

// Stop gracefully stops the cache cleanup routine
func (c *TTLCache) Stop() {
	close(c.stopCleanup)
}

// NewCircuitBreaker creates a circuit breaker for resilient service discovery
func NewCircuitBreaker(failureThreshold int, recoveryTimeout time.Duration) CircuitBreaker {
	return &simpleCircuitBreaker{
		failureThreshold: failureThreshold,
		recoveryTimeout:  recoveryTimeout,
		state:            CircuitClosed,
	}
}

// simpleCircuitBreaker implements CircuitBreaker interface
type simpleCircuitBreaker struct {
	mu               sync.RWMutex
	failureThreshold int
	recoveryTimeout  time.Duration
	state            CircuitState
	failures         int
	lastFailureTime  time.Time
	successCount     int
}

func (cb *simpleCircuitBreaker) Execute(ctx context.Context, fn func() error) error {
	if !cb.canExecute() {
		return &CircuitBreakerError{State: cb.State()}
	}

	err := fn()
	cb.recordResult(err)
	return err
}

func (cb *simpleCircuitBreaker) ExecuteWithFallback(ctx context.Context, fn func() error, fallback func() error) error {
	err := cb.Execute(ctx, fn)
	if err != nil && fallback != nil {
		return fallback()
	}
	return err
}

func (cb *simpleCircuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

func (cb *simpleCircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = CircuitClosed
	cb.failures = 0
	cb.successCount = 0
}

func (cb *simpleCircuitBreaker) canExecute() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	switch cb.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		return time.Since(cb.lastFailureTime) >= cb.recoveryTimeout
	case CircuitHalfOpen:
		return true
	default:
		return false
	}
}

func (cb *simpleCircuitBreaker) recordResult(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.failures++
		cb.lastFailureTime = time.Now()

		if cb.failures >= cb.failureThreshold {
			cb.state = CircuitOpen
		}
	} else {
		if cb.state == CircuitHalfOpen {
			cb.successCount++
			if cb.successCount >= 3 { // Require 3 successes to close
				cb.state = CircuitClosed
				cb.failures = 0
				cb.successCount = 0
			}
		} else if cb.state == CircuitOpen {
			cb.state = CircuitHalfOpen
			cb.successCount = 1
		} else {
			cb.failures = 0
		}
	}
}

// CircuitBreakerError represents circuit breaker errors
type CircuitBreakerError struct {
	State CircuitState
}

func (e *CircuitBreakerError) Error() string {
	return "circuit breaker is " + string(e.State)
}
