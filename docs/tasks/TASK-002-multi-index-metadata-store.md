# TASK-002: Implement Multi-Index Metadata Store

> **NOTE**: NATS KV references in this document are outdated. TAPIO now uses **POLKU** (gRPC event gateway) instead of NATS.

**Priority:** P0 - Critical (Blocks v1.0)
**Estimated Effort:** 6-8 hours
**Skills Required:** Go, data structures, concurrency
**Depends On:** TASK-001 (PodContext with OTELAttributes)

---

## Context

Different observers need different lookup patterns for K8s metadata:
- **Network Observer** needs to lookup by **IP address** (eBPF only captures IPs)
- **Scheduler Observer** needs to lookup by **Pod UID** (K8s Events use UIDs)
- **Future OOM Observer** needs to lookup by **PID** (eBPF captures PIDs)

Currently, Context Service likely has only one index (or inefficient linear search).

**Beyla's solution:** Multiple in-memory indexes for O(1) lookups by different keys.

**Tapio's approach:** Hybrid - NATS KV for persistence + in-memory cache for hot path.

**Reference:** `/Users/yair/projects/tapio/docs/BEYLA_PATTERNS_IMPLEMENTATION.md` (Section 2)

---

## Objective

Implement multi-index metadata store in Context Service:
1. Store pods with multiple NATS KV keys (`pod.ip.X`, `pod.uid.X`, `pod.name.X`)
2. Add in-memory LRU cache for hot path performance
3. Provide type-safe lookup methods for each index
4. Handle cache invalidation on pod updates/deletes

---

## Architecture

```
┌─────────────────────────────────────────────────┐
│         Context Service Storage                 │
├─────────────────────────────────────────────────┤
│                                                 │
│  In-Memory Cache (Hot Path - Fast)             │
│  ┌───────────────────────────────────────────┐ │
│  │ podsByIP   map[string]*PodContext         │ │ ← Network observer
│  │ podsByUID  map[string]*PodContext         │ │ ← Scheduler observer
│  │ podsByName map[string]*PodContext         │ │ ← General lookup
│  └───────────────────────────────────────────┘ │
│              ↓ Cache miss                       │
│  NATS KV (Persistent - Durable)                 │
│  ┌───────────────────────────────────────────┐ │
│  │ pod.ip.10.0.1.42    → PodContext JSON     │ │
│  │ pod.uid.abc-123     → PodContext JSON     │ │
│  │ pod.name.default.my-pod → PodContext JSON │ │
│  └───────────────────────────────────────────┘ │
└─────────────────────────────────────────────────┘
```

---

## Implementation Steps

### Step 1: Update `internal/services/k8scontext/storage.go`

**Current structure** (likely has single index or no caching):
```go
type ContextStore struct {
    natsKV nats.KeyValue
    // Maybe single map or no cache
}
```

**New structure** (multi-index with LRU cache):

```go
package k8scontext

import (
    "context"
    "encoding/json"
    "fmt"
    "sync"

    "github.com/yairfalse/tapio/pkg/domain"
    "github.com/nats-io/nats.go"
)

// ContextStore manages K8s metadata with multi-index support
// Beyla pattern: Multiple indexes for different observer needs
type ContextStore struct {
    mu     sync.RWMutex
    natsKV nats.KeyValue

    // In-memory indexes for hot path (O(1) lookups)
    // These are caches - NATS KV is source of truth
    podsByIP   map[string]*domain.PodContext // Network observer: lookup by IP
    podsByUID  map[string]*domain.PodContext // Scheduler observer: lookup by UID
    podsByName map[string]*domain.PodContext // General: lookup by namespace/name

    // Future indexes (when needed)
    // podsByPID    map[uint32]*domain.PodContext // OOM observer: lookup by PID
    // podsByCgroup map[string]*domain.PodContext // Container observer: lookup by cgroup

    // Deployments, Nodes (lower priority)
    deployments map[string]*domain.DeploymentContext
    nodes       map[string]*domain.NodeContext

    // Metrics
    cacheHits   uint64
    cacheMisses uint64
}

// NewContextStore creates a new multi-index metadata store
func NewContextStore(natsKV nats.KeyValue) *ContextStore {
    return &ContextStore{
        natsKV:      natsKV,
        podsByIP:    make(map[string]*domain.PodContext),
        podsByUID:   make(map[string]*domain.PodContext),
        podsByName:  make(map[string]*domain.PodContext),
        deployments: make(map[string]*domain.DeploymentContext),
        nodes:       make(map[string]*domain.NodeContext),
    }
}
```

**Acceptance Criteria:**
- ✅ `ContextStore` has three pod indexes: IP, UID, Name
- ✅ Thread-safe with `sync.RWMutex`
- ✅ NATS KV for persistence, maps for cache
- ✅ Cache metrics (hits/misses) for observability

---

### Step 2: Implement `StorePod` with Multiple Keys

**Method:** Store pod in NATS KV with multiple key patterns

```go
// StorePod stores a pod context with multiple index keys
// Beyla pattern: Multiple keys for different lookup patterns
func (s *ContextStore) StorePod(ctx context.Context, pod *domain.PodContext) error {
    // Validate required fields
    if pod.UID == "" {
        return fmt.Errorf("pod UID is required")
    }

    // Serialize pod context
    data, err := json.Marshal(pod)
    if err != nil {
        return fmt.Errorf("failed to marshal pod context: %w", err)
    }

    // Store in NATS KV with multiple key patterns
    // This enables O(1) lookups by different fields

    // Key 1: By IP (for Network Observer)
    if pod.PodIP != "" {
        key := makePodByIPKey(pod.PodIP)
        if _, err := s.natsKV.Put(key, data); err != nil {
            return fmt.Errorf("failed to store pod by IP: %w", err)
        }
    }

    // Key 2: By UID (for Scheduler Observer)
    key := makePodByUIDKey(pod.UID)
    if _, err := s.natsKV.Put(key, data); err != nil {
        return fmt.Errorf("failed to store pod by UID: %w", err)
    }

    // Key 3: By Name (for general lookup)
    key = makePodByNameKey(pod.Namespace, pod.Name)
    if _, err := s.natsKV.Put(key, data); err != nil {
        return fmt.Errorf("failed to store pod by name: %w", err)
    }

    // Update in-memory cache (hot path)
    s.mu.Lock()
    defer s.mu.Unlock()

    if pod.PodIP != "" {
        s.podsByIP[pod.PodIP] = pod
    }
    s.podsByUID[pod.UID] = pod
    s.podsByName[makePodNameKey(pod.Namespace, pod.Name)] = pod

    return nil
}

// Helper functions for key generation
func makePodByIPKey(ip string) string {
    return fmt.Sprintf("pod.ip.%s", ip)
}

func makePodByUIDKey(uid string) string {
    return fmt.Sprintf("pod.uid.%s", uid)
}

func makePodByNameKey(namespace, name string) string {
    return fmt.Sprintf("pod.name.%s.%s", namespace, name)
}

func makePodNameKey(namespace, name string) string {
    return fmt.Sprintf("%s/%s", namespace, name)
}
```

**Acceptance Criteria:**
- ✅ Stores pod with 3 NATS KV keys (IP, UID, Name)
- ✅ Updates in-memory cache for all 3 indexes
- ✅ Returns error if UID is missing
- ✅ Handles missing IP gracefully (skip IP index only)

---

### Step 3: Implement Lookup Methods

**Method 1: GetPodByIP** (Network Observer needs this)

```go
// GetPodByIP retrieves pod context by IP address
// Used by Network Observer (eBPF only captures IPs)
func (s *ContextStore) GetPodByIP(ip string) (*domain.PodContext, error) {
    // Fast path: in-memory cache
    s.mu.RLock()
    if pod, ok := s.podsByIP[ip]; ok {
        s.mu.RUnlock()
        s.recordCacheHit()
        return pod, nil
    }
    s.mu.RUnlock()

    s.recordCacheMiss()

    // Slow path: NATS KV lookup
    key := makePodByIPKey(ip)
    entry, err := s.natsKV.Get(key)
    if err != nil {
        if err == nats.ErrKeyNotFound {
            return nil, fmt.Errorf("pod with IP %s not found", ip)
        }
        return nil, fmt.Errorf("failed to get pod by IP: %w", err)
    }

    // Deserialize
    var pod domain.PodContext
    if err := json.Unmarshal(entry.Value(), &pod); err != nil {
        return nil, fmt.Errorf("failed to unmarshal pod: %w", err)
    }

    // Warm cache for next lookup
    s.mu.Lock()
    s.podsByIP[ip] = &pod
    s.mu.Unlock()

    return &pod, nil
}
```

**Method 2: GetPodByUID** (Scheduler Observer needs this)

```go
// GetPodByUID retrieves pod context by UID
// Used by Scheduler Observer (K8s Events use UIDs)
func (s *ContextStore) GetPodByUID(uid string) (*domain.PodContext, error) {
    // Fast path: in-memory cache
    s.mu.RLock()
    if pod, ok := s.podsByUID[uid]; ok {
        s.mu.RUnlock()
        s.recordCacheHit()
        return pod, nil
    }
    s.mu.RUnlock()

    s.recordCacheMiss()

    // Slow path: NATS KV lookup
    key := makePodByUIDKey(uid)
    entry, err := s.natsKV.Get(key)
    if err != nil {
        if err == nats.ErrKeyNotFound {
            return nil, fmt.Errorf("pod with UID %s not found", uid)
        }
        return nil, fmt.Errorf("failed to get pod by UID: %w", err)
    }

    // Deserialize
    var pod domain.PodContext
    if err := json.Unmarshal(entry.Value(), &pod); err != nil {
        return nil, fmt.Errorf("failed to unmarshal pod: %w", err)
    }

    // Warm cache
    s.mu.Lock()
    s.podsByUID[uid] = &pod
    s.mu.Unlock()

    return &pod, nil
}
```

**Method 3: GetPodByName** (General lookup)

```go
// GetPodByName retrieves pod context by namespace and name
// General-purpose lookup
func (s *ContextStore) GetPodByName(namespace, name string) (*domain.PodContext, error) {
    cacheKey := makePodNameKey(namespace, name)

    // Fast path: in-memory cache
    s.mu.RLock()
    if pod, ok := s.podsByName[cacheKey]; ok {
        s.mu.RUnlock()
        s.recordCacheHit()
        return pod, nil
    }
    s.mu.RUnlock()

    s.recordCacheMiss()

    // Slow path: NATS KV lookup
    key := makePodByNameKey(namespace, name)
    entry, err := s.natsKV.Get(key)
    if err != nil {
        if err == nats.ErrKeyNotFound {
            return nil, fmt.Errorf("pod %s/%s not found", namespace, name)
        }
        return nil, fmt.Errorf("failed to get pod by name: %w", err)
    }

    // Deserialize
    var pod domain.PodContext
    if err := json.Unmarshal(entry.Value(), &pod); err != nil {
        return nil, fmt.Errorf("failed to unmarshal pod: %w", err)
    }

    // Warm cache
    s.mu.Lock()
    s.podsByName[cacheKey] = &pod
    s.mu.Unlock()

    return &pod, nil
}

// Helper: Cache metrics
func (s *ContextStore) recordCacheHit() {
    // Thread-safe increment (use atomic if performance matters)
    s.mu.Lock()
    s.cacheHits++
    s.mu.Unlock()
}

func (s *ContextStore) recordCacheMiss() {
    s.mu.Lock()
    s.cacheMisses++
    s.mu.Unlock()
}

// GetCacheStats returns cache hit/miss statistics
func (s *ContextStore) GetCacheStats() (hits, misses uint64) {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return s.cacheHits, s.cacheMisses
}
```

**Acceptance Criteria:**
- ✅ Three lookup methods: `GetPodByIP`, `GetPodByUID`, `GetPodByName`
- ✅ Cache hit → returns immediately (fast path)
- ✅ Cache miss → NATS KV lookup + warm cache (slow path)
- ✅ Cache metrics tracked (hits/misses)
- ✅ Thread-safe (RWMutex for reads, Lock for writes)

---

### Step 4: Implement Cache Invalidation

**Method:** Remove pod from all indexes on delete

```go
// DeletePod removes a pod from all indexes
func (s *ContextStore) DeletePod(ctx context.Context, pod *domain.PodContext) error {
    // Delete from NATS KV (all keys)
    if pod.PodIP != "" {
        s.natsKV.Delete(makePodByIPKey(pod.PodIP))
    }
    s.natsKV.Delete(makePodByUIDKey(pod.UID))
    s.natsKV.Delete(makePodByNameKey(pod.Namespace, pod.Name))

    // Delete from in-memory cache
    s.mu.Lock()
    defer s.mu.Unlock()

    if pod.PodIP != "" {
        delete(s.podsByIP, pod.PodIP)
    }
    delete(s.podsByUID, pod.UID)
    delete(s.podsByName, makePodNameKey(pod.Namespace, pod.Name))

    return nil
}

// UpdatePod updates a pod in all indexes
// Note: This is just StorePod (overwrites existing keys)
func (s *ContextStore) UpdatePod(ctx context.Context, pod *domain.PodContext) error {
    return s.StorePod(ctx, pod)
}
```

**Acceptance Criteria:**
- ✅ `DeletePod` removes from all NATS KV keys and in-memory cache
- ✅ `UpdatePod` overwrites existing entries
- ✅ No stale cache entries

---

### Step 5: Update Context Service to Use Multi-Index Store

**File to modify:** `internal/services/k8scontext/service.go`

**Update pod informer handlers to use new storage methods:**

```go
// Example - adapt to actual code structure
func (s *Service) handlePodDelete(obj interface{}) {
    pod, ok := obj.(*v1.Pod)
    if !ok {
        return
    }

    // Create minimal PodContext for deletion (just need keys)
    podCtx := &domain.PodContext{
        Name:      pod.Name,
        Namespace: pod.Namespace,
        UID:       string(pod.UID),
        PodIP:     pod.Status.PodIP,
    }

    // Delete from all indexes
    if err := s.store.DeletePod(context.Background(), podCtx); err != nil {
        s.logger.Error("failed to delete pod", "error", err)
    }
}
```

**Acceptance Criteria:**
- ✅ Context Service uses `StorePod`, `DeletePod` methods
- ✅ Informer handlers updated

---

## Testing Requirements

### Unit Tests

**File:** `internal/services/k8scontext/storage_test.go`

```go
package k8scontext

import (
    "context"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "github.com/yairfalse/tapio/pkg/domain"
)

func TestContextStore_MultipleIndexes(t *testing.T) {
    // Setup
    natsKV := setupTestNATSKV(t) // Helper to create test NATS KV
    store := NewContextStore(natsKV)

    pod := &domain.PodContext{
        Name:      "test-pod",
        Namespace: "default",
        UID:       "abc-123",
        PodIP:     "10.0.1.42",
        NodeName:  "node-1",
        OTELAttributes: map[string]string{
            "k8s.pod.name": "test-pod",
        },
    }

    // Store pod
    err := store.StorePod(context.Background(), pod)
    require.NoError(t, err)

    // Test: Should be retrievable by IP
    podByIP, err := store.GetPodByIP("10.0.1.42")
    require.NoError(t, err)
    assert.Equal(t, "test-pod", podByIP.Name)
    assert.Equal(t, "abc-123", podByIP.UID)

    // Test: Should be retrievable by UID
    podByUID, err := store.GetPodByUID("abc-123")
    require.NoError(t, err)
    assert.Equal(t, "test-pod", podByUID.Name)
    assert.Equal(t, "10.0.1.42", podByUID.PodIP)

    // Test: Should be retrievable by Name
    podByName, err := store.GetPodByName("default", "test-pod")
    require.NoError(t, err)
    assert.Equal(t, "abc-123", podByName.UID)
    assert.Equal(t, "10.0.1.42", podByName.PodIP)
}

func TestContextStore_CacheHitMiss(t *testing.T) {
    natsKV := setupTestNATSKV(t)
    store := NewContextStore(natsKV)

    pod := &domain.PodContext{
        Name:      "test-pod",
        Namespace: "default",
        UID:       "abc-123",
        PodIP:     "10.0.1.42",
    }

    // Store pod
    store.StorePod(context.Background(), pod)

    // First lookup: should be cache hit (just stored)
    _, err := store.GetPodByIP("10.0.1.42")
    require.NoError(t, err)

    hits, misses := store.GetCacheStats()
    assert.Equal(t, uint64(1), hits, "Expected 1 cache hit")
    assert.Equal(t, uint64(0), misses, "Expected 0 cache misses")

    // Clear cache to force miss
    store.mu.Lock()
    store.podsByIP = make(map[string]*domain.PodContext)
    store.mu.Unlock()

    // Second lookup: should be cache miss (fetch from NATS)
    _, err = store.GetPodByIP("10.0.1.42")
    require.NoError(t, err)

    hits, misses = store.GetCacheStats()
    assert.Equal(t, uint64(1), hits, "Expected 1 cache hit still")
    assert.Equal(t, uint64(1), misses, "Expected 1 cache miss now")
}

func TestContextStore_DeletePod(t *testing.T) {
    natsKV := setupTestNATSKV(t)
    store := NewContextStore(natsKV)

    pod := &domain.PodContext{
        Name:      "test-pod",
        Namespace: "default",
        UID:       "abc-123",
        PodIP:     "10.0.1.42",
    }

    // Store and verify
    store.StorePod(context.Background(), pod)
    _, err := store.GetPodByIP("10.0.1.42")
    require.NoError(t, err)

    // Delete
    err = store.DeletePod(context.Background(), pod)
    require.NoError(t, err)

    // Verify deleted from all indexes
    _, err = store.GetPodByIP("10.0.1.42")
    assert.Error(t, err, "Pod should not be found by IP after delete")

    _, err = store.GetPodByUID("abc-123")
    assert.Error(t, err, "Pod should not be found by UID after delete")

    _, err = store.GetPodByName("default", "test-pod")
    assert.Error(t, err, "Pod should not be found by name after delete")
}

func TestContextStore_ConcurrentAccess(t *testing.T) {
    natsKV := setupTestNATSKV(t)
    store := NewContextStore(natsKV)

    // Concurrent writes and reads
    done := make(chan bool)

    // Writer goroutine
    go func() {
        for i := 0; i < 100; i++ {
            pod := &domain.PodContext{
                Name:      fmt.Sprintf("pod-%d", i),
                Namespace: "default",
                UID:       fmt.Sprintf("uid-%d", i),
                PodIP:     fmt.Sprintf("10.0.1.%d", i),
            }
            store.StorePod(context.Background(), pod)
        }
        done <- true
    }()

    // Reader goroutine
    go func() {
        for i := 0; i < 100; i++ {
            store.GetPodByIP(fmt.Sprintf("10.0.1.%d", i))
        }
        done <- true
    }()

    // Wait for both
    <-done
    <-done

    // Should not panic (thread-safe)
}

// Helper to create test NATS KV
func setupTestNATSKV(t *testing.T) nats.KeyValue {
    // Implementation depends on test setup
    // Could use embedded NATS server or mock
    // See: github.com/nats-io/nats-server/v2/test
}
```

**Acceptance Criteria:**
- ✅ Tests verify all 3 indexes work (IP, UID, Name)
- ✅ Tests verify cache hit/miss tracking
- ✅ Tests verify deletion removes from all indexes
- ✅ Tests verify thread-safety (concurrent access)
- ✅ All tests pass

---

## Performance Validation

### Benchmark Cache Performance

**File:** `internal/services/k8scontext/storage_bench_test.go`

```go
package k8scontext

import (
    "context"
    "fmt"
    "testing"

    "github.com/yairfalse/tapio/pkg/domain"
)

func BenchmarkGetPodByIP_CacheHit(b *testing.B) {
    natsKV := setupTestNATSKV(b)
    store := NewContextStore(natsKV)

    // Pre-populate cache
    pod := &domain.PodContext{
        Name:      "test-pod",
        Namespace: "default",
        UID:       "abc-123",
        PodIP:     "10.0.1.42",
    }
    store.StorePod(context.Background(), pod)

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, _ = store.GetPodByIP("10.0.1.42") // Cache hit
    }
}

func BenchmarkGetPodByIP_CacheMiss(b *testing.B) {
    natsKV := setupTestNATSKV(b)
    store := NewContextStore(natsKV)

    // Pre-populate NATS (but not cache)
    pod := &domain.PodContext{
        Name:      "test-pod",
        Namespace: "default",
        UID:       "abc-123",
        PodIP:     "10.0.1.42",
    }
    store.StorePod(context.Background(), pod)

    // Clear cache to force misses
    store.mu.Lock()
    store.podsByIP = make(map[string]*domain.PodContext)
    store.mu.Unlock()

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, _ = store.GetPodByIP("10.0.1.42") // Cache miss
        // Clear cache again for next iteration
        store.mu.Lock()
        store.podsByIP = make(map[string]*domain.PodContext)
        store.mu.Unlock()
    }
}
```

**Target Performance:**
- Cache hit: < 100ns per lookup
- Cache miss (NATS): < 1ms per lookup

---

## Definition of Done

- ✅ `ContextStore` has multi-index support (IP, UID, Name)
- ✅ `StorePod` creates multiple NATS KV keys
- ✅ `GetPodByIP`, `GetPodByUID`, `GetPodByName` methods implemented
- ✅ In-memory cache for hot path (map-based)
- ✅ NATS KV for persistence (durability)
- ✅ Cache invalidation on delete
- ✅ Cache hit/miss metrics
- ✅ Thread-safe (RWMutex)
- ✅ Unit tests pass (all indexes, cache, delete, concurrency)
- ✅ Performance benchmarks show < 100ns cache hit, < 1ms cache miss
- ✅ Code follows Tapio standards
- ✅ PR description includes benchmark results

---

## References

- **Beyla Pattern:** `/Users/yair/projects/tapio/docs/BEYLA_PATTERNS_IMPLEMENTATION.md` (Section 2)
- **Beyla Source:** `vendor/go.opentelemetry.io/obi/pkg/components/kube/store.go`
- **NATS KV Docs:** https://docs.nats.io/nats-concepts/jetstream/key-value-store
- **TASK-001:** Pre-computed OTEL attributes (defines `PodContext` structure)

---

## Questions?

Ask the architect (Yair) for clarification on:
- NATS KV configuration
- Cache eviction strategy (if needed)
- Additional indexes (PID, cgroup) for future observers
