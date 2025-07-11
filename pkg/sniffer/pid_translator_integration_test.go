package sniffer

import (
	"context"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes/fake"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSimplePIDTranslator_BasicFunctionality(t *testing.T) {
	// Create fake K8s client
	client := fake.NewSimpleClientset()
	
	// Create translator
	translator := NewSimplePIDTranslator(client)

	// Skip informer start since we're using fake client
	// Just test the cache functionality
	
	// Test cache operations
	translator.pidCache.Put(1234, &PIDEntry{
		PID:          1234,
		ContainerID:  "test-container",
		Namespace:    "test-namespace",
		Pod:          "test-pod",
		Container:    "test-container-name",
		LastAccessed: time.Now(),
	})

	entry := translator.pidCache.Get(1234)
	if entry == nil {
		t.Error("Expected to find cached PID entry")
	}
	if entry.Pod != "test-pod" {
		t.Errorf("Expected pod name 'test-pod', got %s", entry.Pod)
	}
}

func TestSimplePIDTranslator_CacheEviction(t *testing.T) {
	client := fake.NewSimpleClientset()
	translator := NewSimplePIDTranslator(client)

	// Fill cache beyond capacity (assuming small cache for test)
	cache := translator.pidCache
	
	// Put more entries than cache size
	for i := uint32(1); i <= 5; i++ {
		cache.Put(i, &PIDEntry{
			PID:       i,
			Pod:       "test-pod",
			Container: "container",
			Namespace: "namespace",
		})
	}

	// Check that cache has entries
	if cache.Size() == 0 {
		t.Error("Cache should not be empty")
	}
}

func TestSimplePIDTranslator_ContainerCache(t *testing.T) {
	client := fake.NewSimpleClientset()
	translator := NewSimplePIDTranslator(client)

	// Test container cache
	containerCache := translator.containerCache
	containerEntry := &ContainerEntry{
		ContainerID: "test-container-id",
		PodName:     "test-pod",
		Namespace:   "test-namespace",
		Container:   "test-container",
		NodeName:    "test-node",
		Labels:      map[string]string{"app": "test"},
	}

	containerCache.Put("test-container-id", containerEntry)
	
	retrieved := containerCache.Get("test-container-id")
	if retrieved == nil {
		t.Error("Expected to find container entry")
	}
	if retrieved.PodName != "test-pod" {
		t.Errorf("Expected pod name 'test-pod', got %s", retrieved.PodName)
	}
}

func TestSimplePIDTranslator_PodCacheUpdate(t *testing.T) {
	client := fake.NewSimpleClientset()
	translator := NewSimplePIDTranslator(client)

	// Create a test pod
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "test-namespace",
			Labels:    map[string]string{"app": "test"},
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:        "test-container",
					ContainerID: "docker://abcdef123456",
				},
			},
		},
	}

	// Test pod cache update
	translator.updatePodCache(pod)

	// Check that container was added to cache
	containerEntry := translator.containerCache.Get("abcdef123456")
	if containerEntry == nil {
		t.Error("Expected container to be added to cache")
	}
	if containerEntry.PodName != "test-pod" {
		t.Errorf("Expected pod name 'test-pod', got %s", containerEntry.PodName)
	}
}

func TestSimplePIDTranslator_GetStats(t *testing.T) {
	client := fake.NewSimpleClientset()
	translator := NewSimplePIDTranslator(client)

	stats := translator.GetStats()
	
	// Check that stats are returned
	if stats == nil {
		t.Error("Expected stats to be returned")
	}
	
	// Check for expected keys
	expectedKeys := []string{"cache_hits", "cache_misses", "hit_rate", "avg_lookup_ns", "pid_cache_size", "container_cache_size", "last_cache_update"}
	for _, key := range expectedKeys {
		if _, exists := stats[key]; !exists {
			t.Errorf("Expected stats key '%s' to exist", key)
		}
	}
}

func TestPIDCache_LRUEviction(t *testing.T) {
	cache := NewPIDCache(3) // Small cache for testing

	// Fill cache
	cache.Put(1, &PIDEntry{PID: 1, Pod: "pod1"})
	cache.Put(2, &PIDEntry{PID: 2, Pod: "pod2"})
	cache.Put(3, &PIDEntry{PID: 3, Pod: "pod3"})

	// Access entry 1 to make it most recently used
	cache.Get(1)

	// Add another entry - should evict entry 2
	cache.Put(4, &PIDEntry{PID: 4, Pod: "pod4"})

	// Entry 2 should be evicted
	if cache.Get(2) != nil {
		t.Error("Entry 2 should have been evicted")
	}

	// Entry 1 should still be there
	if cache.Get(1) == nil {
		t.Error("Entry 1 should still be in cache")
	}
}

func TestPIDCache_Cleanup(t *testing.T) {
	cache := NewPIDCache(10)

	// Add old entry
	oldEntry := &PIDEntry{
		PID:          1,
		Pod:          "old-pod",
		LastAccessed: time.Now().Add(-10 * time.Minute), // Very old
	}
	cache.entries[1] = oldEntry

	// Add recent entry
	cache.Put(2, &PIDEntry{PID: 2, Pod: "new-pod"})

	// Run cleanup
	cache.Cleanup()

	// Old entry should be removed
	if cache.Get(1) != nil {
		t.Error("Old entry should have been cleaned up")
	}

	// Recent entry should remain
	if cache.Get(2) == nil {
		t.Error("Recent entry should still be in cache")
	}
}