package k8s

import (
	"context"
	"fmt"
	"hash/fnv"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

type CacheManager struct {
	l1Cache *L1Cache
	l2Cache *L2Cache
	warmup  *WarmupManager
	mu      sync.RWMutex
	closed  bool
	stopCh  chan struct{}
	config  *CacheConfig
}

type CacheConfig struct {
	L1Size          int
	L1TTL           time.Duration
	L2Size          int
	L2TTL           time.Duration
	WarmupEnabled   bool
	WarmupInterval  time.Duration
	CleanupInterval time.Duration
	StaleDataTTL    time.Duration
}

type CacheEntry struct {
	Key        string
	Value      interface{}
	CreatedAt  time.Time
	AccessedAt time.Time
	Version    string
	Stale      bool
	mu         sync.RWMutex
}

type L1Cache struct {
	entries map[string]*CacheEntry
	lru     *LRUList
	maxSize int
	ttl     time.Duration
	mu      sync.RWMutex
}

type L2Cache struct {
	entries      map[string]*CacheEntry
	maxSize      int
	ttl          time.Duration
	staleDataTTL time.Duration
	mu           sync.RWMutex
}

type LRUList struct {
	head, tail *LRUNode
	size       int
}

type LRUNode struct {
	key  string
	prev *LRUNode
	next *LRUNode
}

type WarmupManager struct {
	strategies map[string]WarmupStrategy
	scheduler  *WarmupScheduler
	mu         sync.RWMutex
}

type WarmupStrategy interface {
	ShouldWarmup(ctx context.Context, key string) bool
	Execute(ctx context.Context, key string) (interface{}, error)
}

type WarmupScheduler struct {
	interval time.Duration
	stopCh   chan struct{}
}

func DefaultCacheConfig() *CacheConfig {
	return &CacheConfig{
		L1Size:          1000,
		L1TTL:           5 * time.Minute,
		L2Size:          10000,
		L2TTL:           30 * time.Minute,
		WarmupEnabled:   true,
		WarmupInterval:  2 * time.Minute,
		CleanupInterval: 1 * time.Minute,
		StaleDataTTL:    10 * time.Minute,
	}
}

func NewCacheManager(config *CacheConfig) *CacheManager {
	if config == nil {
		config = DefaultCacheConfig()
	}

	cm := &CacheManager{
		l1Cache: NewL1Cache(config.L1Size, config.L1TTL),
		l2Cache: NewL2Cache(config.L2Size, config.L2TTL, config.StaleDataTTL),
		warmup:  NewWarmupManager(config.WarmupInterval),
		stopCh:  make(chan struct{}),
		config:  config,
	}

	go cm.cleanupLoop()

	if config.WarmupEnabled {
		go cm.warmup.Start(cm.stopCh)
	}

	return cm
}

func NewL1Cache(maxSize int, ttl time.Duration) *L1Cache {
	return &L1Cache{
		entries: make(map[string]*CacheEntry),
		lru:     NewLRUList(),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

func NewL2Cache(maxSize int, ttl, staleDataTTL time.Duration) *L2Cache {
	return &L2Cache{
		entries:      make(map[string]*CacheEntry),
		maxSize:      maxSize,
		ttl:          ttl,
		staleDataTTL: staleDataTTL,
	}
}

func NewLRUList() *LRUList {
	head := &LRUNode{}
	tail := &LRUNode{}
	head.next = tail
	tail.prev = head

	return &LRUList{
		head: head,
		tail: tail,
		size: 0,
	}
}

func NewWarmupManager(interval time.Duration) *WarmupManager {
	return &WarmupManager{
		strategies: make(map[string]WarmupStrategy),
		scheduler:  &WarmupScheduler{interval: interval},
	}
}

func (cm *CacheManager) Get(ctx context.Context, key string) (interface{}, bool) {
	cm.mu.RLock()
	if cm.closed {
		cm.mu.RUnlock()
		return nil, false
	}
	cm.mu.RUnlock()

	if value, found := cm.l1Cache.Get(key); found {
		return value, true
	}

	if value, found := cm.l2Cache.Get(key); found {
		cm.l1Cache.Put(key, value)
		return value, true
	}

	return nil, false
}

func (cm *CacheManager) Put(key string, value interface{}) {
	cm.mu.RLock()
	if cm.closed {
		cm.mu.RUnlock()
		return
	}
	cm.mu.RUnlock()

	version := cm.generateVersion(value)
	cm.l1Cache.Put(key, value, version)
	cm.l2Cache.Put(key, value, version)
}

func (cm *CacheManager) Invalidate(key string) {
	cm.mu.RLock()
	if cm.closed {
		cm.mu.RUnlock()
		return
	}
	cm.mu.RUnlock()

	cm.l1Cache.Remove(key)
	cm.l2Cache.Remove(key)
}

func (cm *CacheManager) InvalidatePattern(pattern string) {
	cm.mu.RLock()
	if cm.closed {
		cm.mu.RUnlock()
		return
	}
	cm.mu.RUnlock()

	cm.l1Cache.RemovePattern(pattern)
	cm.l2Cache.RemovePattern(pattern)
}

func (cm *CacheManager) GetWithFallback(ctx context.Context, key string, fallback func() (interface{}, error)) (interface{}, error) {
	if value, found := cm.Get(ctx, key); found {
		return value, nil
	}

	value, err := fallback()
	if err != nil {
		if staleValue, found := cm.getStale(key); found {
			return staleValue, nil
		}
		return nil, err
	}

	cm.Put(key, value)
	return value, nil
}

func (cm *CacheManager) getStale(key string) (interface{}, bool) {
	return cm.l2Cache.GetStale(key)
}

func (cm *CacheManager) generateVersion(value interface{}) string {
	h := fnv.New64a()

	if obj, ok := value.(runtime.Object); ok {
		if accessor, err := meta.Accessor(obj); err == nil {
			h.Write([]byte(accessor.GetResourceVersion()))
		}
	}

	h.Write([]byte(fmt.Sprintf("%v", value)))
	return fmt.Sprintf("%x", h.Sum64())
}

func (cm *CacheManager) Close() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.closed {
		return nil
	}

	cm.closed = true
	close(cm.stopCh)
	return nil
}

func (cm *CacheManager) cleanupLoop() {
	ticker := time.NewTicker(cm.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cm.cleanup()
		case <-cm.stopCh:
			return
		}
	}
}

func (cm *CacheManager) cleanup() {
	cm.l1Cache.cleanup()
	cm.l2Cache.cleanup()
}

func (l1 *L1Cache) Get(key string) (interface{}, bool) {
	l1.mu.RLock()
	defer l1.mu.RUnlock()

	entry, exists := l1.entries[key]
	if !exists {
		return nil, false
	}

	if time.Since(entry.CreatedAt) > l1.ttl {
		delete(l1.entries, key)
		l1.lru.Remove(key)
		return nil, false
	}

	entry.mu.Lock()
	entry.AccessedAt = time.Now()
	entry.mu.Unlock()

	l1.lru.MoveToFront(key)
	return entry.Value, true
}

func (l1 *L1Cache) Put(key string, value interface{}, version ...string) {
	l1.mu.Lock()
	defer l1.mu.Unlock()

	if l1.lru.size >= l1.maxSize {
		l1.evictLRU()
	}

	ver := ""
	if len(version) > 0 {
		ver = version[0]
	}

	entry := &CacheEntry{
		Key:        key,
		Value:      value,
		CreatedAt:  time.Now(),
		AccessedAt: time.Now(),
		Version:    ver,
	}

	l1.entries[key] = entry
	l1.lru.AddToFront(key)
}

func (l1 *L1Cache) Remove(key string) {
	l1.mu.Lock()
	defer l1.mu.Unlock()

	delete(l1.entries, key)
	l1.lru.Remove(key)
}

func (l1 *L1Cache) RemovePattern(pattern string) {
	l1.mu.Lock()
	defer l1.mu.Unlock()

	for key := range l1.entries {
		if matched, _ := cache.MetaNamespaceKeyFunc(pattern); matched == key {
			delete(l1.entries, key)
			l1.lru.Remove(key)
		}
	}
}

func (l1 *L1Cache) evictLRU() {
	if l1.lru.tail.prev != l1.lru.head {
		key := l1.lru.tail.prev.key
		delete(l1.entries, key)
		l1.lru.RemoveLast()
	}
}

func (l1 *L1Cache) cleanup() {
	l1.mu.Lock()
	defer l1.mu.Unlock()

	now := time.Now()
	for key, entry := range l1.entries {
		if now.Sub(entry.CreatedAt) > l1.ttl {
			delete(l1.entries, key)
			l1.lru.Remove(key)
		}
	}
}

func (l2 *L2Cache) Get(key string) (interface{}, bool) {
	l2.mu.RLock()
	defer l2.mu.RUnlock()

	entry, exists := l2.entries[key]
	if !exists {
		return nil, false
	}

	if time.Since(entry.CreatedAt) > l2.ttl && !entry.Stale {
		entry.mu.Lock()
		entry.Stale = true
		entry.mu.Unlock()
	}

	if entry.Stale && time.Since(entry.CreatedAt) > l2.staleDataTTL {
		return nil, false
	}

	entry.mu.Lock()
	entry.AccessedAt = time.Now()
	entry.mu.Unlock()

	return entry.Value, true
}

func (l2 *L2Cache) GetStale(key string) (interface{}, bool) {
	l2.mu.RLock()
	defer l2.mu.RUnlock()

	entry, exists := l2.entries[key]
	if !exists {
		return nil, false
	}

	if time.Since(entry.CreatedAt) > l2.staleDataTTL {
		return nil, false
	}

	return entry.Value, true
}

func (l2 *L2Cache) Put(key string, value interface{}, version ...string) {
	l2.mu.Lock()
	defer l2.mu.Unlock()

	ver := ""
	if len(version) > 0 {
		ver = version[0]
	}

	entry := &CacheEntry{
		Key:        key,
		Value:      value,
		CreatedAt:  time.Now(),
		AccessedAt: time.Now(),
		Version:    ver,
	}

	l2.entries[key] = entry

	if len(l2.entries) > l2.maxSize {
		l2.evictOldest()
	}
}

func (l2 *L2Cache) Remove(key string) {
	l2.mu.Lock()
	defer l2.mu.Unlock()

	delete(l2.entries, key)
}

func (l2 *L2Cache) RemovePattern(pattern string) {
	l2.mu.Lock()
	defer l2.mu.Unlock()

	for key := range l2.entries {
		if matched, _ := cache.MetaNamespaceKeyFunc(pattern); matched == key {
			delete(l2.entries, key)
		}
	}
}

func (l2 *L2Cache) evictOldest() {
	oldest := ""
	oldestTime := time.Now()

	for key, entry := range l2.entries {
		if entry.AccessedAt.Before(oldestTime) {
			oldest = key
			oldestTime = entry.AccessedAt
		}
	}

	if oldest != "" {
		delete(l2.entries, oldest)
	}
}

func (l2 *L2Cache) cleanup() {
	l2.mu.Lock()
	defer l2.mu.Unlock()

	now := time.Now()
	for key, entry := range l2.entries {
		if entry.Stale && now.Sub(entry.CreatedAt) > l2.staleDataTTL {
			delete(l2.entries, key)
		}
	}
}

func (lru *LRUList) AddToFront(key string) {
	node := &LRUNode{key: key}
	node.next = lru.head.next
	node.prev = lru.head
	lru.head.next.prev = node
	lru.head.next = node
	lru.size++
}

func (lru *LRUList) Remove(key string) {
	current := lru.head.next
	for current != lru.tail {
		if current.key == key {
			current.prev.next = current.next
			current.next.prev = current.prev
			lru.size--
			return
		}
		current = current.next
	}
}

func (lru *LRUList) RemoveLast() {
	if lru.tail.prev != lru.head {
		node := lru.tail.prev
		node.prev.next = lru.tail
		lru.tail.prev = node.prev
		lru.size--
	}
}

func (lru *LRUList) MoveToFront(key string) {
	lru.Remove(key)
	lru.AddToFront(key)
}

func (wm *WarmupManager) AddStrategy(name string, strategy WarmupStrategy) {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	wm.strategies[name] = strategy
}

func (wm *WarmupManager) Start(stopCh chan struct{}) {
	ticker := time.NewTicker(wm.scheduler.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			wm.executeWarmup(context.Background())
		case <-stopCh:
			return
		}
	}
}

func (wm *WarmupManager) executeWarmup(ctx context.Context) {
	wm.mu.RLock()
	strategies := make(map[string]WarmupStrategy)
	for name, strategy := range wm.strategies {
		strategies[name] = strategy
	}
	wm.mu.RUnlock()

	for name, strategy := range strategies {
		go func(name string, s WarmupStrategy) {
			if s.ShouldWarmup(ctx, name) {
				if _, err := s.Execute(ctx, name); err != nil {
					// Log error but continue with other strategies
				}
			}
		}(name, strategy)
	}
}
