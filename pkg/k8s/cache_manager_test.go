package k8s

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestCacheManager_GetPut(t *testing.T) {
	config := &CacheConfig{
		L1Size:          10,
		L1TTL:           1 * time.Second,
		L2Size:          20,
		L2TTL:           2 * time.Second,
		WarmupEnabled:   false,
		CleanupInterval: 100 * time.Millisecond,
	}

	cm := NewCacheManager(config)
	defer cm.Close()

	key := "test-key"
	value := "test-value"

	// Test Put and Get
	cm.Put(key, value)
	result, found := cm.Get(context.Background(), key)
	if !found {
		t.Error("Expected to find cached value")
	}
	if result != value {
		t.Errorf("Expected %v, got %v", value, result)
	}
}

func TestCacheManager_L1Eviction(t *testing.T) {
	config := &CacheConfig{
		L1Size:          2,
		L1TTL:           10 * time.Second,
		L2Size:          10,
		L2TTL:           20 * time.Second,
		WarmupEnabled:   false,
		CleanupInterval: 100 * time.Millisecond,
	}

	cm := NewCacheManager(config)
	defer cm.Close()

	// Fill L1 cache beyond capacity
	cm.Put("key1", "value1")
	cm.Put("key2", "value2")
	cm.Put("key3", "value3") // Should evict key1

	// key1 should be evicted from L1 but still in L2
	_, found := cm.l1Cache.Get("key1")
	if found {
		t.Error("key1 should have been evicted from L1")
	}

	// But should still be accessible through cache manager (from L2)
	result, found := cm.Get(context.Background(), "key1")
	if !found {
		t.Error("key1 should still be accessible from L2")
	}
	if result != "value1" {
		t.Errorf("Expected value1, got %v", result)
	}
}

func TestCacheManager_TTLExpiration(t *testing.T) {
	config := &CacheConfig{
		L1Size:          10,
		L1TTL:           100 * time.Millisecond,
		L2Size:          20,
		L2TTL:           200 * time.Millisecond,
		WarmupEnabled:   false,
		CleanupInterval: 50 * time.Millisecond,
	}

	cm := NewCacheManager(config)
	defer cm.Close()

	key := "expire-key"
	value := "expire-value"

	cm.Put(key, value)

	// Should be available immediately
	_, found := cm.Get(context.Background(), key)
	if !found {
		t.Error("Value should be available immediately after put")
	}

	// Wait for L1 expiration
	time.Sleep(150 * time.Millisecond)

	// Should still be in L2
	result, found := cm.Get(context.Background(), key)
	if !found {
		t.Error("Value should still be in L2 after L1 expiration")
	}
	if result != value {
		t.Errorf("Expected %v, got %v", value, result)
	}

	// Wait for L2 expiration
	time.Sleep(100 * time.Millisecond)

	// Should be completely expired
	_, found = cm.Get(context.Background(), key)
	if found {
		t.Error("Value should be expired from both caches")
	}
}

func TestCacheManager_Invalidation(t *testing.T) {
	cm := NewCacheManager(DefaultCacheConfig())
	defer cm.Close()

	key := "invalidate-key"
	value := "invalidate-value"

	cm.Put(key, value)

	// Verify it's cached
	_, found := cm.Get(context.Background(), key)
	if !found {
		t.Error("Value should be cached before invalidation")
	}

	// Invalidate
	cm.Invalidate(key)

	// Should be gone
	_, found = cm.Get(context.Background(), key)
	if found {
		t.Error("Value should be invalidated")
	}
}

func TestCacheManager_GetWithFallback(t *testing.T) {
	cm := NewCacheManager(DefaultCacheConfig())
	defer cm.Close()

	key := "fallback-key"
	fallbackValue := "fallback-value"
	fallbackCalled := false

	fallback := func() (interface{}, error) {
		fallbackCalled = true
		return fallbackValue, nil
	}

	// First call should invoke fallback
	result, err := cm.GetWithFallback(context.Background(), key, fallback)
	if err != nil {
		t.Errorf("GetWithFallback failed: %v", err)
	}
	if !fallbackCalled {
		t.Error("Fallback should have been called")
	}
	if result != fallbackValue {
		t.Errorf("Expected %v, got %v", fallbackValue, result)
	}

	// Reset flag
	fallbackCalled = false

	// Second call should use cache
	result, err = cm.GetWithFallback(context.Background(), key, fallback)
	if err != nil {
		t.Errorf("GetWithFallback failed: %v", err)
	}
	if fallbackCalled {
		t.Error("Fallback should not have been called on cache hit")
	}
	if result != fallbackValue {
		t.Errorf("Expected %v, got %v", fallbackValue, result)
	}
}

func TestL1Cache_LRUEviction(t *testing.T) {
	l1 := NewL1Cache(3, 10*time.Second)

	// Fill cache
	l1.Put("key1", "value1")
	l1.Put("key2", "value2")
	l1.Put("key3", "value3")

	// Access key1 to make it most recently used
	l1.Get("key1")

	// Add another item, should evict key2 (least recently used)
	l1.Put("key4", "value4")

	// key2 should be evicted
	_, found := l1.Get("key2")
	if found {
		t.Error("key2 should have been evicted")
	}

	// key1 should still be there
	_, found = l1.Get("key1")
	if !found {
		t.Error("key1 should still be in cache")
	}
}

func TestL2Cache_StaleData(t *testing.T) {
	l2 := NewL2Cache(10, 100*time.Millisecond, 500*time.Millisecond)

	key := "stale-key"
	value := "stale-value"

	l2.Put(key, value)

	// Should be fresh initially
	result, found := l2.Get(key)
	if !found {
		t.Error("Value should be found initially")
	}
	if result != value {
		t.Errorf("Expected %v, got %v", value, result)
	}

	// Wait for data to become stale but not expired
	time.Sleep(150 * time.Millisecond)

	// Should still be accessible
	result, found = l2.Get(key)
	if !found {
		t.Error("Stale value should still be accessible")
	}

	// Should be available as stale
	result, found = l2.GetStale(key)
	if !found {
		t.Error("Should be able to get stale value")
	}

	// Wait for complete expiration
	time.Sleep(400 * time.Millisecond)

	// Should be completely gone
	_, found = l2.GetStale(key)
	if found {
		t.Error("Value should be completely expired")
	}
}

func BenchmarkCacheManager_Get(b *testing.B) {
	cm := NewCacheManager(DefaultCacheConfig())
	defer cm.Close()

	// Populate cache
	for i := 0; i < 1000; i++ {
		cm.Put(fmt.Sprintf("key-%d", i), fmt.Sprintf("value-%d", i))
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cm.Get(context.Background(), "key-500")
		}
	})
}

func BenchmarkCacheManager_Put(b *testing.B) {
	cm := NewCacheManager(DefaultCacheConfig())
	defer cm.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cm.Put(fmt.Sprintf("key-%d", i), fmt.Sprintf("value-%d", i))
	}
}
