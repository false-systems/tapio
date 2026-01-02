package k8scontext

import (
	"context"
	"sync"
	"time"
)

// TombstoneCache holds deleted pods for a TTL period.
// This allows enrichment of trailing TCP FIN packets after pod deletion.
type TombstoneCache struct {
	mu         sync.RWMutex
	tombstones map[string]*tombstone // UID -> tombstone
	ttl        time.Duration
}

type tombstone struct {
	meta      *PodMeta
	deletedAt time.Time
}

// NewTombstoneCache creates a tombstone cache with the given TTL.
func NewTombstoneCache(ttl time.Duration) *TombstoneCache {
	return &TombstoneCache{
		tombstones: make(map[string]*tombstone),
		ttl:        ttl,
	}
}

// Add adds a deleted pod to the tombstone cache.
func (tc *TombstoneCache) Add(pod *PodMeta) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	tc.tombstones[pod.UID] = &tombstone{
		meta:      pod,
		deletedAt: time.Now(),
	}
}

// GetByIP looks up a tombstoned pod by IP.
func (tc *TombstoneCache) GetByIP(ip string) (*PodMeta, bool) {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	for _, tomb := range tc.tombstones {
		if tomb.meta.PodIP == ip && time.Since(tomb.deletedAt) < tc.ttl {
			// Return copy with Terminating flag
			meta := *tomb.meta
			meta.Terminating = true
			return &meta, true
		}
	}

	return nil, false
}

// GetByContainerID looks up a tombstoned pod by container ID.
func (tc *TombstoneCache) GetByContainerID(cid string) (*PodMeta, bool) {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	for _, tomb := range tc.tombstones {
		if time.Since(tomb.deletedAt) >= tc.ttl {
			continue
		}
		for _, c := range tomb.meta.Containers {
			if c.ContainerID == cid {
				meta := *tomb.meta
				meta.Terminating = true
				return &meta, true
			}
		}
	}

	return nil, false
}

// Count returns the number of tombstones.
func (tc *TombstoneCache) Count() int {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	return len(tc.tombstones)
}

// Cleanup removes expired tombstones.
func (tc *TombstoneCache) Cleanup() {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	for uid, tomb := range tc.tombstones {
		if time.Since(tomb.deletedAt) >= tc.ttl {
			delete(tc.tombstones, uid)
		}
	}
}

// StartCleanupLoop runs periodic cleanup until context is cancelled.
func (tc *TombstoneCache) StartCleanupLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tc.Cleanup()
		}
	}
}
