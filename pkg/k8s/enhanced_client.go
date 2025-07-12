package k8s

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/yairfalse/tapio/pkg/universal"
)

type EnhancedK8sClient struct {
	baseClient       kubernetes.Interface
	resilientClient  *ResilientClient
	resilienceManager *universal.ResilienceManager
	smartCache       *CacheManager
	watchManager     *WatchManager
	stateTracker     *StateTracker
	config           *EnhancedConfig
}

type EnhancedConfig struct {
	// Cache settings for CI stability
	CacheConfig *CacheConfig
	
	// Watch management
	WatchConfig *WatchConfig
	
	// State tracking
	StateConfig *StateConfig
	
	// Resilient client settings
	ResilientConfig *ResilientConfig
	
	// Request timeouts
	DefaultTimeout time.Duration
	
	// Cache warmup settings
	EnableCacheWarmup bool
	WarmupNamespaces  []string
	
	// Circuit breaker settings
	CircuitBreakerThreshold int
	CircuitBreakerTimeout   time.Duration
}

type AdvancedCache struct {
	*CacheManager
	warmupManager *CacheWarmupManager
}

type CacheWarmupManager struct {
	client     kubernetes.Interface
	namespaces []string
	interval   time.Duration
	stopCh     chan struct{}
}

func DefaultEnhancedConfig() *EnhancedConfig {
	return &EnhancedConfig{
		CacheConfig:             DefaultCacheConfig(),
		WatchConfig:             DefaultWatchConfig(),
		StateConfig:             DefaultStateConfig(),
		ResilientConfig:         DefaultResilientConfig(),
		DefaultTimeout:          30 * time.Second,
		EnableCacheWarmup:       true,
		WarmupNamespaces:        []string{"default", "kube-system"},
		CircuitBreakerThreshold: 5,
		CircuitBreakerTimeout:   30 * time.Second,
	}
}

func NewEnhancedK8sClient(kubeconfigPath string, config *EnhancedConfig) (*EnhancedK8sClient, error) {
	if config == nil {
		config = DefaultEnhancedConfig()
	}

	// Create resilient client
	resilientClient, err := NewResilientClient(kubeconfigPath, config.ResilientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create resilient client: %w", err)
	}

	// Create resilience manager
	resilienceManager := universal.NewResilienceManager()

	// Create cache manager
	cacheManager := NewCacheManager(config.CacheConfig)

	// Create watch manager
	watchManager := NewWatchManager(resilientClient.Clientset, config.WatchConfig)

	// Create state tracker
	stateTracker := NewStateTracker(config.StateConfig)

	// Create advanced cache with warmup
	advancedCache := &AdvancedCache{
		CacheManager: cacheManager,
	}

	if config.EnableCacheWarmup {
		advancedCache.warmupManager = &CacheWarmupManager{
			client:     resilientClient.Clientset,
			namespaces: config.WarmupNamespaces,
			interval:   5 * time.Minute,
			stopCh:     make(chan struct{}),
		}
		go advancedCache.warmupManager.Start()
	}

	return &EnhancedK8sClient{
		baseClient:        resilientClient.Clientset,
		resilientClient:   resilientClient,
		resilienceManager: resilienceManager,
		smartCache:        cacheManager,
		watchManager:      watchManager,
		stateTracker:      stateTracker,
		config:            config,
	}, nil
}

// GetPod retrieves a pod with smart caching and circuit breaker protection
func (c *EnhancedK8sClient) GetPod(namespace, name string) (*corev1.Pod, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.config.DefaultTimeout)
	defer cancel()

	cacheKey := fmt.Sprintf("pod/%s/%s", namespace, name)
	
	// Try cache first for faster CI
	if cached, found := c.smartCache.Get(ctx, cacheKey); found {
		if pod, ok := cached.(*corev1.Pod); ok {
			return pod, nil
		}
	}

	// Use resilience manager with circuit breaker for API call
	target := universal.Target{
		Name:      fmt.Sprintf("%s/%s", namespace, name),
		Type:      "pod",
		Namespace: namespace,
	}

	result, usedFallback, err := c.resilienceManager.ExecuteWithFallback(
		ctx,
		"k8s-api",
		target,
		func() (interface{}, error) {
			return c.baseClient.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		},
		"k8s",
	)

	if err != nil {
		return nil, err
	}

	if pod, ok := result.(*corev1.Pod); ok && !usedFallback {
		c.smartCache.Put(cacheKey, pod)
		// Track state changes
		c.stateTracker.TrackResource(pod)
		return pod, nil
	}

	// If we got fallback data, return error indicating cache miss
	return nil, fmt.Errorf("pod not found and fallback used")
}

// GetService retrieves a service with caching and resilience
func (c *EnhancedK8sClient) GetService(namespace, name string) (*corev1.Service, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.config.DefaultTimeout)
	defer cancel()

	cacheKey := fmt.Sprintf("service/%s/%s", namespace, name)
	
	if cached, found := c.smartCache.Get(ctx, cacheKey); found {
		if svc, ok := cached.(*corev1.Service); ok {
			return svc, nil
		}
	}

	// Use resilience manager for API call
	target := universal.Target{
		Name:      fmt.Sprintf("%s/%s", namespace, name),
		Type:      "service",
		Namespace: namespace,
	}

	result, usedFallback, err := c.resilienceManager.ExecuteWithFallback(
		ctx,
		"k8s-api",
		target,
		func() (interface{}, error) {
			return c.baseClient.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
		},
		"k8s",
	)

	if err != nil {
		return nil, err
	}

	if service, ok := result.(*corev1.Service); ok && !usedFallback {
		c.smartCache.Put(cacheKey, service)
		c.stateTracker.TrackResource(service)
		return service, nil
	}

	return nil, fmt.Errorf("service not found and fallback used")
}

// GetDeployment retrieves a deployment with caching and resilience
func (c *EnhancedK8sClient) GetDeployment(namespace, name string) (*appsv1.Deployment, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.config.DefaultTimeout)
	defer cancel()

	cacheKey := fmt.Sprintf("deployment/%s/%s", namespace, name)
	
	if cached, found := c.smartCache.Get(ctx, cacheKey); found {
		if dep, ok := cached.(*appsv1.Deployment); ok {
			return dep, nil
		}
	}

	// Use resilience manager for API call
	target := universal.Target{
		Name:      fmt.Sprintf("%s/%s", namespace, name),
		Type:      "deployment",
		Namespace: namespace,
	}

	result, usedFallback, err := c.resilienceManager.ExecuteWithFallback(
		ctx,
		"k8s-api",
		target,
		func() (interface{}, error) {
			return c.baseClient.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		},
		"k8s",
	)

	if err != nil {
		return nil, err
	}

	if deployment, ok := result.(*appsv1.Deployment); ok && !usedFallback {
		c.smartCache.Put(cacheKey, deployment)
		c.stateTracker.TrackResource(deployment)
		return deployment, nil
	}

	return nil, fmt.Errorf("deployment not found and fallback used")
}

// ListPods lists pods with smart caching for CI stability
func (c *EnhancedK8sClient) ListPods(namespace string, opts metav1.ListOptions) (*corev1.PodList, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.config.DefaultTimeout)
	defer cancel()

	cacheKey := fmt.Sprintf("pods-list/%s/%s", namespace, opts.LabelSelector)
	
	// For list operations, use shorter cache TTL
	if cached, found := c.smartCache.Get(ctx, cacheKey); found {
		if podList, ok := cached.(*corev1.PodList); ok {
			return podList, nil
		}
	}

	// Use resilience manager for API call
	target := universal.Target{
		Name:      fmt.Sprintf("pods-list/%s", namespace),
		Type:      "podlist",
		Namespace: namespace,
	}

	result, usedFallback, err := c.resilienceManager.ExecuteWithFallback(
		ctx,
		"k8s-api",
		target,
		func() (interface{}, error) {
			return c.baseClient.CoreV1().Pods(namespace).List(ctx, opts)
		},
		"k8s",
	)

	if err != nil {
		return nil, err
	}

	if podList, ok := result.(*corev1.PodList); ok && !usedFallback {
		c.smartCache.Put(cacheKey, podList)
		// Track individual pods
		for i := range podList.Items {
			c.stateTracker.TrackResource(&podList.Items[i])
		}
		return podList, nil
	}

	return nil, fmt.Errorf("pod list not available and fallback used")
}

// WatchPods creates a resilient watch for pods
func (c *EnhancedK8sClient) WatchPods(namespace string) (<-chan WatchEvent, error) {
	return c.watchManager.WatchPods(context.Background(), namespace)
}

// WatchServices creates a resilient watch for services
func (c *EnhancedK8sClient) WatchServices(namespace string) (<-chan WatchEvent, error) {
	return c.watchManager.WatchServices(context.Background(), namespace)
}

// WatchDeployments creates a resilient watch for deployments
func (c *EnhancedK8sClient) WatchDeployments(namespace string) (<-chan WatchEvent, error) {
	return c.watchManager.WatchDeployments(context.Background(), namespace)
}

// InvalidateCache invalidates cache entries for a resource
func (c *EnhancedK8sClient) InvalidateCache(resourceType, namespace, name string) {
	cacheKey := fmt.Sprintf("%s/%s/%s", resourceType, namespace, name)
	c.smartCache.Invalidate(cacheKey)
}

// InvalidateCachePattern invalidates cache entries matching a pattern
func (c *EnhancedK8sClient) InvalidateCachePattern(pattern string) {
	c.smartCache.InvalidatePattern(pattern)
}

// GetCircuitBreakerStatus returns the current circuit breaker status
func (c *EnhancedK8sClient) GetCircuitBreakerStatus() string {
	cb := c.resilienceManager.GetCircuitBreaker("k8s-api")
	if cb.CanExecute() {
		return "Closed"
	}
	return "Open"
}

// GetCacheStats returns cache statistics for monitoring
func (c *EnhancedK8sClient) GetCacheStats() map[string]interface{} {
	// This would return cache hit rates, sizes, etc.
	return map[string]interface{}{
		"circuit_breaker_state": c.GetCircuitBreakerStatus(),
		"cache_enabled":         true,
		"watch_streams":         len(c.watchManager.streams),
	}
}

// Close properly shuts down all components
func (c *EnhancedK8sClient) Close() error {
	if c.smartCache != nil {
		c.smartCache.Close()
	}
	if c.watchManager != nil {
		c.watchManager.Close()
	}
	if c.stateTracker != nil {
		c.stateTracker.Close()
	}
	if c.resilientClient != nil {
		c.resilientClient.Close()
	}
	return nil
}

// CacheWarmupManager methods
func (cwm *CacheWarmupManager) Start() {
	ticker := time.NewTicker(cwm.interval)
	defer ticker.Stop()

	// Initial warmup
	cwm.warmup()

	for {
		select {
		case <-ticker.C:
			cwm.warmup()
		case <-cwm.stopCh:
			return
		}
	}
}

func (cwm *CacheWarmupManager) warmup() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, namespace := range cwm.namespaces {
		// Warmup pods
		go func(ns string) {
			if _, err := cwm.client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{Limit: 100}); err != nil {
				// Log error but continue
			}
		}(namespace)

		// Warmup services
		go func(ns string) {
			if _, err := cwm.client.CoreV1().Services(ns).List(ctx, metav1.ListOptions{Limit: 100}); err != nil {
				// Log error but continue
			}
		}(namespace)

		// Warmup deployments
		go func(ns string) {
			if _, err := cwm.client.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{Limit: 100}); err != nil {
				// Log error but continue
			}
		}(namespace)
	}
}

func (cwm *CacheWarmupManager) Stop() {
	close(cwm.stopCh)
}