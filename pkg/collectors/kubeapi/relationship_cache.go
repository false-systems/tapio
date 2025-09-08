package kubeapi

import (
	"sync"
	"time"
)

// RelationshipType defines the type of relationship
type RelationshipType string

const (
	RelationshipServicePods RelationshipType = "service-pods"
	RelationshipPodVolumes  RelationshipType = "pod-volumes"
	RelationshipOwnerRefs   RelationshipType = "owner-refs"
	RelationshipNodePods    RelationshipType = "node-pods"
)

// CachedRelationship stores a computed relationship with TTL
type CachedRelationship struct {
	SourceUID    string // UID of the source object
	RelationType RelationshipType
	Targets      []ObjectRef // Related objects
	ComputedAt   time.Time
	TTL          time.Duration
	Version      int64 // For invalidation
}

// ObjectRef represents a reference to a K8s object
type ObjectRef struct {
	Kind      string
	Namespace string
	Name      string
	UID       string
	Relation  string // "owns", "selects", "mounts", etc.
}

// RelationshipCache caches expensive relationship computations
type RelationshipCache struct {
	mu          sync.RWMutex
	cache       map[string]*CachedRelationship // key: "relationshipType:sourceUID"
	defaultTTL  time.Duration
	maxSize     int
	hits        int64
	misses      int64
	cleanupStop chan struct{}
	cleanupDone chan struct{}
}

// NewRelationshipCache creates a new relationship cache
func NewRelationshipCache(defaultTTL time.Duration, maxSize int) *RelationshipCache {
	rc := &RelationshipCache{
		cache:       make(map[string]*CachedRelationship),
		defaultTTL:  defaultTTL,
		maxSize:     maxSize,
		cleanupStop: make(chan struct{}),
		cleanupDone: make(chan struct{}),
	}

	// Start cleanup goroutine
	go rc.cleanupRoutine()

	return rc
}

// Get retrieves a cached relationship
func (rc *RelationshipCache) Get(relationType RelationshipType, sourceUID string) (*CachedRelationship, bool) {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	key := rc.makeKey(relationType, sourceUID)
	rel, exists := rc.cache[key]

	if !exists {
		rc.misses++
		return nil, false
	}

	// Check if expired
	if time.Since(rel.ComputedAt) > rel.TTL {
		rc.misses++
		return nil, false
	}

	rc.hits++
	return rel, true
}

// Set stores a relationship in the cache
func (rc *RelationshipCache) Set(relationType RelationshipType, sourceUID string, targets []ObjectRef) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	key := rc.makeKey(relationType, sourceUID)

	// If cache is full, make room
	if len(rc.cache) >= rc.maxSize {
		rc.evictOldest()
	}

	rc.cache[key] = &CachedRelationship{
		SourceUID:    sourceUID,
		RelationType: relationType,
		Targets:      targets,
		ComputedAt:   time.Now(),
		TTL:          rc.defaultTTL,
		Version:      time.Now().UnixNano(),
	}
}

// Invalidate removes cached relationships for a specific object
func (rc *RelationshipCache) Invalidate(sourceUID string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	// Remove all entries for this source
	for key, rel := range rc.cache {
		if rel.SourceUID == sourceUID {
			delete(rc.cache, key)
		}
	}
}

// InvalidateByTarget removes cached relationships that involve a specific target
func (rc *RelationshipCache) InvalidateByTarget(targetUID string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	// Remove entries that reference this target
	for key, rel := range rc.cache {
		for _, target := range rel.Targets {
			if target.UID == targetUID {
				delete(rc.cache, key)
				break
			}
		}
	}
}

// GetServicePods gets cached service-to-pods mapping
func (rc *RelationshipCache) GetServicePods(serviceUID string) ([]ObjectRef, bool) {
	rel, exists := rc.Get(RelationshipServicePods, serviceUID)
	if !exists {
		return nil, false
	}
	return rel.Targets, true
}

// SetServicePods caches service-to-pods mapping
func (rc *RelationshipCache) SetServicePods(serviceUID string, pods []ObjectRef) {
	rc.Set(RelationshipServicePods, serviceUID, pods)
}

// GetPodVolumes gets cached pod-to-volumes mapping
func (rc *RelationshipCache) GetPodVolumes(podUID string) ([]ObjectRef, bool) {
	rel, exists := rc.Get(RelationshipPodVolumes, podUID)
	if !exists {
		return nil, false
	}
	return rel.Targets, true
}

// SetPodVolumes caches pod-to-volumes mapping
func (rc *RelationshipCache) SetPodVolumes(podUID string, volumes []ObjectRef) {
	rc.Set(RelationshipPodVolumes, podUID, volumes)
}

// GetStats returns cache statistics
func (rc *RelationshipCache) GetStats() map[string]int64 {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	total := rc.hits + rc.misses
	hitRate := int64(0)
	if total > 0 {
		hitRate = (rc.hits * 100) / total
	}

	return map[string]int64{
		"cache_size":   int64(len(rc.cache)),
		"cache_hits":   rc.hits,
		"cache_misses": rc.misses,
		"hit_rate_pct": hitRate,
		"max_size":     int64(rc.maxSize),
	}
}

// Stop stops the cache cleanup goroutine
func (rc *RelationshipCache) Stop() {
	close(rc.cleanupStop)
	<-rc.cleanupDone
}

// makeKey creates a cache key
func (rc *RelationshipCache) makeKey(relationType RelationshipType, sourceUID string) string {
	return string(relationType) + ":" + sourceUID
}

// evictOldest removes the oldest entry to make room
func (rc *RelationshipCache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time = time.Now()

	for key, rel := range rc.cache {
		if rel.ComputedAt.Before(oldestTime) {
			oldestTime = rel.ComputedAt
			oldestKey = key
		}
	}

	if oldestKey != "" {
		delete(rc.cache, oldestKey)
	}
}

// cleanupRoutine periodically removes expired entries
func (rc *RelationshipCache) cleanupRoutine() {
	defer close(rc.cleanupDone)

	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-rc.cleanupStop:
			return
		case <-ticker.C:
			rc.cleanup()
		}
	}
}

// cleanup removes expired entries
func (rc *RelationshipCache) cleanup() {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	now := time.Now()
	for key, rel := range rc.cache {
		if now.Sub(rel.ComputedAt) > rel.TTL {
			delete(rc.cache, key)
		}
	}
}
