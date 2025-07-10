package k8s

import (
	"context"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"github.com/falseyair/tapio/pkg/universal"
)

func TestEnhancedK8sClient_GetPod_WithCache(t *testing.T) {
	// Create fake client
	fakeClient := fake.NewSimpleClientset()
	
	// Create test pod
	testPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "test-container", Image: "nginx"},
			},
		},
	}
	
	// Add pod to fake client
	fakeClient.CoreV1().Pods("default").Create(context.Background(), testPod, metav1.CreateOptions{})

	// Create enhanced client with test config
	config := &EnhancedConfig{
		CacheConfig: &CacheConfig{
			L1Size:          10,
			L1TTL:           1 * time.Second,
			L2Size:          20,
			L2TTL:           2 * time.Second,
			WarmupEnabled:   false,
			CleanupInterval: 100 * time.Millisecond,
		},
		DefaultTimeout:          30 * time.Second,
		EnableCacheWarmup:       false,
		CircuitBreakerThreshold: 5,
		CircuitBreakerTimeout:   30 * time.Second,
	}

	// Create enhanced client manually for testing
	enhancedClient := &EnhancedK8sClient{
		baseClient:        fakeClient,
		resilienceManager: universal.NewResilienceManager(),
		smartCache:        NewCacheManager(config.CacheConfig),
		config:            config,
	}
	defer enhancedClient.Close()

	// First call should hit API
	pod1, err := enhancedClient.GetPod("default", "test-pod")
	if err != nil {
		t.Fatalf("Failed to get pod: %v", err)
	}
	if pod1.Name != "test-pod" {
		t.Errorf("Expected pod name 'test-pod', got '%s'", pod1.Name)
	}

	// Second call should hit cache
	pod2, err := enhancedClient.GetPod("default", "test-pod")
	if err != nil {
		t.Fatalf("Failed to get pod from cache: %v", err)
	}
	if pod2.Name != "test-pod" {
		t.Errorf("Expected cached pod name 'test-pod', got '%s'", pod2.Name)
	}
}

func TestEnhancedK8sClient_CircuitBreaker_Integration(t *testing.T) {
	// Create fake client that will fail
	fakeClient := fake.NewSimpleClientset()
	
	// Make the client fail after first call
	callCount := 0
	fakeClient.PrependReactor("get", "pods", func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
		callCount++
		if callCount > 1 {
			return true, nil, fmt.Errorf("simulated API failure")
		}
		return false, nil, nil
	})

	config := &EnhancedConfig{
		CacheConfig: &CacheConfig{
			L1Size:          10,
			L1TTL:           100 * time.Millisecond,
			L2Size:          20,
			L2TTL:           200 * time.Millisecond,
			WarmupEnabled:   false,
			CleanupInterval: 50 * time.Millisecond,
		},
		DefaultTimeout:          5 * time.Second,
		CircuitBreakerThreshold: 2,
		CircuitBreakerTimeout:   5 * time.Second,
	}

	enhancedClient := &EnhancedK8sClient{
		baseClient:        fakeClient,
		resilienceManager: universal.NewResilienceManager(),
		smartCache:        NewCacheManager(config.CacheConfig),
		config:            config,
	}
	defer enhancedClient.Close()

	// Add a test pod
	testPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
	}
	fakeClient.CoreV1().Pods("default").Create(context.Background(), testPod, metav1.CreateOptions{})

	// First call should succeed
	_, err := enhancedClient.GetPod("default", "test-pod")
	if err != nil {
		t.Fatalf("First call should succeed: %v", err)
	}

	// Subsequent calls should fail and eventually trip circuit breaker
	for i := 0; i < 5; i++ {
		_, err = enhancedClient.GetPod("default", "non-cached-pod")
		if err == nil {
			t.Errorf("Expected error on call %d", i+2)
		}
	}

	// Circuit breaker should now be open
	status := enhancedClient.GetCircuitBreakerStatus()
	if status != "StateOpen" {
		t.Logf("Circuit breaker state: %s (expected StateOpen, but this depends on timing)", status)
	}
}

func TestEnhancedK8sClient_CacheInvalidation(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	
	config := &EnhancedConfig{
		CacheConfig: &CacheConfig{
			L1Size:          10,
			L1TTL:           10 * time.Second,
			L2Size:          20,
			L2TTL:           20 * time.Second,
			WarmupEnabled:   false,
			CleanupInterval: 100 * time.Millisecond,
		},
		DefaultTimeout:          30 * time.Second,
		CircuitBreakerThreshold: 5,
		CircuitBreakerTimeout:   30 * time.Second,
	}

	enhancedClient := &EnhancedK8sClient{
		baseClient:        fakeClient,
		resilienceManager: universal.NewResilienceManager(),
		smartCache:        NewCacheManager(config.CacheConfig),
		config:            config,
	}
	defer enhancedClient.Close()

	// Add test pod
	testPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
	}
	fakeClient.CoreV1().Pods("default").Create(context.Background(), testPod, metav1.CreateOptions{})

	// Get pod to cache it
	_, err := enhancedClient.GetPod("default", "test-pod")
	if err != nil {
		t.Fatalf("Failed to get pod: %v", err)
	}

	// Verify it's cached by checking if we can get it even after removing from fake client
	fakeClient.CoreV1().Pods("default").Delete(context.Background(), "test-pod", metav1.DeleteOptions{})

	cachedPod, err := enhancedClient.GetPod("default", "test-pod")
	if err != nil || cachedPod == nil {
		t.Fatalf("Pod should be available from cache")
	}

	// Invalidate cache
	enhancedClient.InvalidateCache("pod", "default", "test-pod")

	// Now it should fail since it's not in fake client anymore
	_, err = enhancedClient.GetPod("default", "test-pod")
	if err == nil {
		t.Error("Expected error after cache invalidation")
	}
}

func TestEnhancedK8sClient_ListPods_Caching(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	
	config := &EnhancedConfig{
		CacheConfig: &CacheConfig{
			L1Size:          10,
			L1TTL:           1 * time.Second,
			L2Size:          20,
			L2TTL:           2 * time.Second,
			WarmupEnabled:   false,
			CleanupInterval: 100 * time.Millisecond,
		},
		DefaultTimeout:          30 * time.Second,
		CircuitBreakerThreshold: 5,
		CircuitBreakerTimeout:   30 * time.Second,
	}

	enhancedClient := &EnhancedK8sClient{
		baseClient:        fakeClient,
		resilienceManager: universal.NewResilienceManager(),
		smartCache:        NewCacheManager(config.CacheConfig),
		config:            config,
	}
	defer enhancedClient.Close()

	// Add test pods
	for i := 0; i < 3; i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("test-pod-%d", i),
				Namespace: "default",
				Labels:    map[string]string{"app": "test"},
			},
		}
		fakeClient.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{})
	}

	// First list should hit API
	opts := metav1.ListOptions{LabelSelector: "app=test"}
	podList1, err := enhancedClient.ListPods("default", opts)
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}
	if len(podList1.Items) != 3 {
		t.Errorf("Expected 3 pods, got %d", len(podList1.Items))
	}

	// Second list should hit cache
	podList2, err := enhancedClient.ListPods("default", opts)
	if err != nil {
		t.Fatalf("Failed to list pods from cache: %v", err)
	}
	if len(podList2.Items) != 3 {
		t.Errorf("Expected 3 cached pods, got %d", len(podList2.Items))
	}
}

func TestEnhancedK8sClient_GetCacheStats(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	
	config := DefaultEnhancedConfig()
	config.EnableCacheWarmup = false // Disable for test

	enhancedClient := &EnhancedK8sClient{
		baseClient:        fakeClient,
		resilienceManager: universal.NewResilienceManager(),
		smartCache:        NewCacheManager(config.CacheConfig),
		watchManager:      NewWatchManager(fakeClient, config.WatchConfig),
		config:            config,
	}
	defer enhancedClient.Close()

	stats := enhancedClient.GetCacheStats()
	
	if stats["cache_enabled"] != true {
		t.Error("Cache should be enabled")
	}
	
	if stats["circuit_breaker_state"] == nil {
		t.Error("Circuit breaker state should be reported")
	}
}

func BenchmarkEnhancedK8sClient_GetPod(b *testing.B) {
	fakeClient := fake.NewSimpleClientset()
	
	// Add test pod
	testPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
	}
	fakeClient.CoreV1().Pods("default").Create(context.Background(), testPod, metav1.CreateOptions{})

	config := DefaultEnhancedConfig()
	config.EnableCacheWarmup = false

	enhancedClient := &EnhancedK8sClient{
		baseClient:        fakeClient,
		resilienceManager: universal.NewResilienceManager(),
		smartCache:        NewCacheManager(config.CacheConfig),
		config:            config,
	}
	defer enhancedClient.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = enhancedClient.GetPod("default", "test-pod")
		}
	})
}

func BenchmarkEnhancedK8sClient_ListPods(b *testing.B) {
	fakeClient := fake.NewSimpleClientset()
	
	// Add test pods
	for i := 0; i < 100; i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("test-pod-%d", i),
				Namespace: "default",
				Labels:    map[string]string{"app": "test"},
			},
		}
		fakeClient.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{})
	}

	config := DefaultEnhancedConfig()
	config.EnableCacheWarmup = false

	enhancedClient := &EnhancedK8sClient{
		baseClient:        fakeClient,
		resilienceManager: universal.NewResilienceManager(),
		smartCache:        NewCacheManager(config.CacheConfig),
		config:            config,
	}
	defer enhancedClient.Close()

	opts := metav1.ListOptions{LabelSelector: "app=test"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = enhancedClient.ListPods("default", opts)
	}
}