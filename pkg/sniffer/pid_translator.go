package sniffer

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// SimplePIDTranslator provides fast PID to Pod translation with caching
type SimplePIDTranslator struct {
	// Core components
	client          kubernetes.Interface
	nodeInformer    cache.SharedIndexInformer
	podInformer     cache.SharedIndexInformer
	ctx             context.Context
	cancel          context.CancelFunc

	// Caching layers
	pidCache        *PIDCache        // PID -> container info (hot cache)
	containerCache  *ContainerCache  // container ID -> pod info
	podCache        map[string]*corev1.Pod // pod key -> pod object
	cacheMutex      sync.RWMutex

	// Performance metrics
	cacheHits       uint64
	cacheMisses     uint64
	lookupLatencyNs uint64
	lastCacheUpdate time.Time
}

// PIDCache is a fixed-size LRU cache for PID lookups
type PIDCache struct {
	mu       sync.RWMutex
	entries  map[uint32]*PIDEntry
	lru      []uint32
	maxSize  int
}

// PIDEntry holds cached PID information
type PIDEntry struct {
	PID          uint32
	ContainerID  string
	Namespace    string
	Pod          string
	Container    string
	LastAccessed time.Time
	CgroupPath   string
}

// ContainerCache maps container IDs to pod information
type ContainerCache struct {
	mu      sync.RWMutex
	entries map[string]*ContainerEntry
}

// ContainerEntry holds container to pod mapping
type ContainerEntry struct {
	ContainerID string
	PodName     string
	Namespace   string
	Container   string
	NodeName    string
	Labels      map[string]string
}

// NewSimplePIDTranslator creates a new PID translator
func NewSimplePIDTranslator(client kubernetes.Interface) *SimplePIDTranslator {
	return &SimplePIDTranslator{
		client:         client,
		pidCache:       NewPIDCache(100000), // ~20 bytes per entry = 2MB
		containerCache: NewContainerCache(),
		podCache:       make(map[string]*corev1.Pod),
	}
}

// Start initializes the translator
func (t *SimplePIDTranslator) Start(ctx context.Context) error {
	t.ctx, t.cancel = context.WithCancel(ctx)

	// Initialize informers
	if err := t.initializeInformers(); err != nil {
		return fmt.Errorf("failed to initialize informers: %w", err)
	}

	// Start background processes
	go t.refreshCachePeriodically()
	go t.scanProcPeriodically()

	return nil
}

// GetPodInfo translates PID to Kubernetes context
func (t *SimplePIDTranslator) GetPodInfo(pid uint32) (*EventContext, error) {
	start := time.Now()
	defer func() {
		// Track lookup latency
		atomic.AddUint64(&t.lookupLatencyNs, uint64(time.Since(start).Nanoseconds()))
	}()

	// Check hot cache first
	if entry := t.pidCache.Get(pid); entry != nil {
		atomic.AddUint64(&t.cacheHits, 1)
		return &EventContext{
			Pod:       entry.Pod,
			Namespace: entry.Namespace,
			Container: entry.Container,
			PID:       pid,
		}, nil
	}

	atomic.AddUint64(&t.cacheMisses, 1)

	// Slow path: read from /proc
	containerID, err := t.getContainerIDFromPID(pid)
	if err != nil {
		return nil, err
	}

	// Look up container in cache
	if containerInfo := t.containerCache.Get(containerID); containerInfo != nil {
		// Update PID cache
		t.pidCache.Put(pid, &PIDEntry{
			PID:          pid,
			ContainerID:  containerID,
			Namespace:    containerInfo.Namespace,
			Pod:          containerInfo.PodName,
			Container:    containerInfo.Container,
			LastAccessed: time.Now(),
		})

		return &EventContext{
			Pod:       containerInfo.PodName,
			Namespace: containerInfo.Namespace,
			Container: containerInfo.Container,
			Node:      containerInfo.NodeName,
			Labels:    containerInfo.Labels,
			PID:       pid,
		}, nil
	}

	return nil, fmt.Errorf("no pod found for PID %d", pid)
}

// initializeInformers sets up Kubernetes watchers
func (t *SimplePIDTranslator) initializeInformers() error {
	// Pod informer - watches all pods
	t.podInformer = cache.NewSharedIndexInformer(
		cache.NewListWatchFromClient(t.client.CoreV1().RESTClient(), "pods", "", metav1.ListOptions{}),
		&corev1.Pod{},
		time.Minute,
		cache.Indexers{},
	)

	t.podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    t.onPodAdd,
		UpdateFunc: t.onPodUpdate,
		DeleteFunc: t.onPodDelete,
	})

	// Start informers
	go t.podInformer.Run(t.ctx.Done())

	// Wait for initial sync
	if !cache.WaitForCacheSync(t.ctx.Done(), t.podInformer.HasSynced) {
		return fmt.Errorf("failed to sync pod cache")
	}

	return nil
}

// onPodAdd handles new pods
func (t *SimplePIDTranslator) onPodAdd(obj interface{}) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return
	}

	t.updatePodCache(pod)
}

// onPodUpdate handles pod updates
func (t *SimplePIDTranslator) onPodUpdate(oldObj, newObj interface{}) {
	pod, ok := newObj.(*corev1.Pod)
	if !ok {
		return
	}

	t.updatePodCache(pod)
}

// onPodDelete handles pod deletions
func (t *SimplePIDTranslator) onPodDelete(obj interface{}) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		// Handle deleted final state unknown
		deletedState, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return
		}
		pod, ok = deletedState.Obj.(*corev1.Pod)
		if !ok {
			return
		}
	}

	t.removePodFromCache(pod)
}

// updatePodCache updates the container cache with pod information
func (t *SimplePIDTranslator) updatePodCache(pod *corev1.Pod) {
	t.cacheMutex.Lock()
	defer t.cacheMutex.Unlock()

	key := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
	t.podCache[key] = pod

	// Update container cache for all containers
	for _, container := range pod.Status.ContainerStatuses {
		if container.ContainerID == "" {
			continue
		}

		// Extract container ID (remove docker://, containerd://, etc.)
		containerID := extractContainerID(container.ContainerID)
		
		t.containerCache.Put(containerID, &ContainerEntry{
			ContainerID: containerID,
			PodName:     pod.Name,
			Namespace:   pod.Namespace,
			Container:   container.Name,
			NodeName:    pod.Spec.NodeName,
			Labels:      pod.Labels,
		})
	}
}

// removePodFromCache removes pod from caches
func (t *SimplePIDTranslator) removePodFromCache(pod *corev1.Pod) {
	t.cacheMutex.Lock()
	defer t.cacheMutex.Unlock()

	key := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
	delete(t.podCache, key)

	// Remove containers from cache
	for _, container := range pod.Status.ContainerStatuses {
		if container.ContainerID != "" {
			containerID := extractContainerID(container.ContainerID)
			t.containerCache.Remove(containerID)
		}
	}
}

// getContainerIDFromPID reads container ID from /proc/PID/cgroup
func (t *SimplePIDTranslator) getContainerIDFromPID(pid uint32) (string, error) {
	cgroupPath := fmt.Sprintf("/proc/%d/cgroup", pid)
	
	file, err := os.Open(cgroupPath)
	if err != nil {
		return "", fmt.Errorf("failed to open cgroup file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		
		// Look for container ID in cgroup path
		// Format: 0::/kubepods/besteffort/pod<pod-uid>/<container-id>
		if strings.Contains(line, "kubepods") {
			parts := strings.Split(line, "/")
			if len(parts) > 0 {
				// Last part is usually the container ID
				containerID := parts[len(parts)-1]
				// Clean up any prefixes
				containerID = strings.TrimPrefix(containerID, "docker-")
				containerID = strings.TrimSuffix(containerID, ".scope")
				if len(containerID) >= 12 { // Valid container ID
					return containerID[:12], nil // Use first 12 chars
				}
			}
		}
	}

	return "", fmt.Errorf("container ID not found for PID %d", pid)
}

// refreshCachePeriodically updates caches periodically
func (t *SimplePIDTranslator) refreshCachePeriodically() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			t.lastCacheUpdate = time.Now()
			// Cache is updated via informers, just clean up old entries
			t.pidCache.Cleanup()
		}
	}
}

// scanProcPeriodically pre-populates PID cache
func (t *SimplePIDTranslator) scanProcPeriodically() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			t.scanProc()
		}
	}
}

// scanProc scans /proc for container processes
func (t *SimplePIDTranslator) scanProc() {
	procDir, err := os.Open("/proc")
	if err != nil {
		return
	}
	defer procDir.Close()

	entries, err := procDir.Readdir(-1)
	if err != nil {
		return
	}

	for _, entry := range entries {
		// Skip non-PID directories
		pid, err := strconv.ParseUint(entry.Name(), 10, 32)
		if err != nil {
			continue
		}

		// Check if already cached
		if t.pidCache.Get(uint32(pid)) != nil {
			continue
		}

		// Try to get container ID
		containerID, err := t.getContainerIDFromPID(uint32(pid))
		if err != nil {
			continue
		}

		// Look up in container cache
		if containerInfo := t.containerCache.Get(containerID); containerInfo != nil {
			t.pidCache.Put(uint32(pid), &PIDEntry{
				PID:          uint32(pid),
				ContainerID:  containerID,
				Namespace:    containerInfo.Namespace,
				Pod:          containerInfo.PodName,
				Container:    containerInfo.Container,
				LastAccessed: time.Now(),
			})
		}
	}
}

// GetStats returns translator statistics
func (t *SimplePIDTranslator) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"cache_hits":          atomic.LoadUint64(&t.cacheHits),
		"cache_misses":        atomic.LoadUint64(&t.cacheMisses),
		"hit_rate":            t.getHitRate(),
		"avg_lookup_ns":       t.getAvgLookupLatency(),
		"pid_cache_size":      t.pidCache.Size(),
		"container_cache_size": t.containerCache.Size(),
		"last_cache_update":   t.lastCacheUpdate,
	}
}

// getHitRate calculates cache hit rate
func (t *SimplePIDTranslator) getHitRate() float64 {
	hits := atomic.LoadUint64(&t.cacheHits)
	misses := atomic.LoadUint64(&t.cacheMisses)
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

// getAvgLookupLatency calculates average lookup latency
func (t *SimplePIDTranslator) getAvgLookupLatency() int64 {
	total := atomic.LoadUint64(&t.cacheHits) + atomic.LoadUint64(&t.cacheMisses)
	if total == 0 {
		return 0
	}
	return int64(atomic.LoadUint64(&t.lookupLatencyNs) / total)
}

// Stop stops the translator
func (t *SimplePIDTranslator) Stop() {
	if t.cancel != nil {
		t.cancel()
	}
}

// Helper functions for caches

// NewPIDCache creates a new PID cache
func NewPIDCache(maxSize int) *PIDCache {
	return &PIDCache{
		entries: make(map[uint32]*PIDEntry),
		lru:     make([]uint32, 0, maxSize),
		maxSize: maxSize,
	}
}

// Get retrieves an entry from the cache
func (c *PIDCache) Get(pid uint32) *PIDEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if entry, exists := c.entries[pid]; exists {
		entry.LastAccessed = time.Now()
		return entry
	}
	return nil
}

// Put adds an entry to the cache
func (c *PIDCache) Put(pid uint32, entry *PIDEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if we need to evict
	if len(c.entries) >= c.maxSize {
		c.evictOldest()
	}

	c.entries[pid] = entry
	c.lru = append(c.lru, pid)
}

// evictOldest removes the least recently used entry
func (c *PIDCache) evictOldest() {
	if len(c.lru) == 0 {
		return
	}

	// Find oldest entry
	oldestPID := c.lru[0]
	delete(c.entries, oldestPID)
	
	// Remove from LRU list
	c.lru = c.lru[1:]
}

// Size returns the cache size
func (c *PIDCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// Cleanup removes stale entries
func (c *PIDCache) Cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	cutoff := time.Now().Add(-5 * time.Minute)
	
	// Remove old entries
	for pid, entry := range c.entries {
		if entry.LastAccessed.Before(cutoff) {
			delete(c.entries, pid)
		}
	}

	// Rebuild LRU list
	newLRU := make([]uint32, 0, len(c.entries))
	for pid := range c.entries {
		newLRU = append(newLRU, pid)
	}
	c.lru = newLRU
}

// NewContainerCache creates a new container cache
func NewContainerCache() *ContainerCache {
	return &ContainerCache{
		entries: make(map[string]*ContainerEntry),
	}
}

// Get retrieves a container entry
func (c *ContainerCache) Get(containerID string) *ContainerEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.entries[containerID]
}

// Put adds a container entry
func (c *ContainerCache) Put(containerID string, entry *ContainerEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[containerID] = entry
}

// Remove removes a container entry
func (c *ContainerCache) Remove(containerID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, containerID)
}

// Size returns the cache size
func (c *ContainerCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// extractContainerID extracts the container ID from various runtime formats
func extractContainerID(fullID string) string {
	// Remove runtime prefixes (docker://, containerd://, cri-o://)
	for _, prefix := range []string{"docker://", "containerd://", "cri-o://"} {
		fullID = strings.TrimPrefix(fullID, prefix)
	}
	
	// Take first 12 characters for consistency
	if len(fullID) > 12 {
		return fullID[:12]
	}
	return fullID
}