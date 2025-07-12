package discovery

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// KubernetesService represents a Kubernetes service with rich metadata
type KubernetesService struct {
	// Core identification
	ID        string
	Name      string
	Namespace string

	// Service details
	Type      string
	ClusterIP string
	Ports     []ServicePort

	// Metadata
	Labels      map[string]string
	Annotations map[string]string

	// Discovery context
	DiscoveredAt time.Time
	LastSeen     time.Time

	// Health and validation
	Health    HealthStatus
	Endpoints []Endpoint
}

// ServicePort represents a Kubernetes service port
type ServicePort struct {
	Name       string
	Port       int32
	TargetPort string
	Protocol   string
	NodePort   int32
}

// Implement ServiceType interface
func (ks KubernetesService) GetID() string            { return ks.ID }
func (ks KubernetesService) GetType() string          { return "kubernetes-service" }
func (ks KubernetesService) GetEndpoints() []Endpoint { return ks.Endpoints }
func (ks KubernetesService) GetMetadata() map[string]string {
	metadata := make(map[string]string)
	for k, v := range ks.Labels {
		metadata["label."+k] = v
	}
	for k, v := range ks.Annotations {
		metadata["annotation."+k] = v
	}
	metadata["namespace"] = ks.Namespace
	metadata["cluster-ip"] = ks.ClusterIP
	return metadata
}

// KubernetesDiscovery implements Discovery interface for Kubernetes services
type KubernetesDiscovery struct {
	// Dependencies
	client         kubernetes.Interface
	logger         *slog.Logger
	workerPool     WorkerPool
	circuitBreaker CircuitBreaker
	cache          Cache
	validator      Validator

	// Configuration
	config KubernetesConfig

	// State management
	mu      sync.RWMutex
	healthy bool
	stats   KubernetesStats
}

// KubernetesConfig configures Kubernetes discovery
type KubernetesConfig struct {
	// Connection
	KubeConfig string
	InCluster  bool

	// Discovery behavior
	RefreshInterval time.Duration
	Timeout         time.Duration

	// Performance
	WorkerPoolSize int
	CacheTTL       time.Duration

	// Filtering
	NamespaceFilter []string
	LabelSelector   string

	// Circuit breaker
	FailureThreshold int
	RecoveryTimeout  time.Duration
}

// KubernetesStats tracks discovery performance metrics
type KubernetesStats struct {
	DiscoveryCount    int64
	ServicesFound     int64
	ValidationsPassed int64
	ValidationsFailed int64
	CacheHits         int64
	CacheMisses       int64
	LastDiscovery     time.Time
	AverageLatency    time.Duration
}

// NewKubernetesDiscovery creates a new Kubernetes discovery instance
func NewKubernetesDiscovery(config KubernetesConfig, logger *slog.Logger) (*KubernetesDiscovery, error) {
	// Create Kubernetes client
	var restConfig *rest.Config
	var err error

	if config.InCluster {
		restConfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to create in-cluster config: %w", err)
		}
	} else {
		restConfig, err = clientcmd.BuildConfigFromFlags("", config.KubeConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create kubeconfig: %w", err)
		}
	}

	// Configure client for performance
	restConfig.QPS = 50
	restConfig.Burst = 100
	restConfig.Timeout = config.Timeout

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// Set defaults
	if config.RefreshInterval == 0 {
		config.RefreshInterval = 30 * time.Second
	}
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second
	}
	if config.WorkerPoolSize == 0 {
		config.WorkerPoolSize = 10
	}
	if config.CacheTTL == 0 {
		config.CacheTTL = 5 * time.Minute
	}
	if config.FailureThreshold == 0 {
		config.FailureThreshold = 5
	}
	if config.RecoveryTimeout == 0 {
		config.RecoveryTimeout = 30 * time.Second
	}

	// Create dependencies
	workerPool := NewBoundedWorkerPool(config.WorkerPoolSize/2, config.WorkerPoolSize, 30*time.Second)
	circuitBreaker := NewCircuitBreaker(config.FailureThreshold, config.RecoveryTimeout)
	cache := NewTTLCache(1000, config.CacheTTL)
	validator := NewKubernetesValidator(client, logger)

	kd := &KubernetesDiscovery{
		client:         client,
		logger:         logger,
		workerPool:     workerPool,
		circuitBreaker: circuitBreaker,
		cache:          cache,
		validator:      validator,
		config:         config,
		healthy:        true,
	}

	return kd, nil
}

// Discover performs Kubernetes service discovery
func (kd *KubernetesDiscovery) Discover(ctx context.Context, opts DiscoveryOptions) ([]KubernetesService, error) {
	start := time.Now()
	defer func() {
		kd.updateStats(time.Since(start))
	}()

	kd.logger.Debug("Starting Kubernetes service discovery",
		"timeout", opts.Timeout,
		"concurrency", opts.Concurrency,
		"cache_enabled", opts.EnableCache)

	// Check cache first if enabled
	if opts.EnableCache {
		cacheKey := kd.buildCacheKey(opts)
		if cached, found := kd.cache.Get(ctx, cacheKey); found {
			kd.stats.CacheHits++
			services := cached.([]KubernetesService)
			kd.logger.Debug("Cache hit for Kubernetes discovery", "services", len(services))
			return services, nil
		}
		kd.stats.CacheMisses++
	}

	// Perform discovery with circuit breaker protection
	var services []KubernetesService
	err := kd.circuitBreaker.Execute(ctx, func() error {
		var discErr error
		services, discErr = kd.performDiscovery(ctx, opts)
		return discErr
	})

	if err != nil {
		kd.logger.Error("Kubernetes discovery failed", "error", err)
		return nil, fmt.Errorf("discovery failed: %w", err)
	}

	// Validate services if requested
	if opts.EnableValidation && kd.validator != nil {
		services = kd.validateServices(ctx, services)
	}

	// Cache results if enabled
	if opts.EnableCache {
		cacheKey := kd.buildCacheKey(opts)
		ttl := opts.CacheTTL
		if ttl == 0 {
			ttl = kd.config.CacheTTL
		}
		kd.cache.Set(ctx, cacheKey, services, ttl)
	}

	kd.stats.ServicesFound += int64(len(services))
	kd.logger.Info("Kubernetes discovery completed",
		"services_found", len(services),
		"duration", time.Since(start))

	return services, nil
}

// DiscoverStream provides continuous Kubernetes service discovery
func (kd *KubernetesDiscovery) DiscoverStream(ctx context.Context, opts DiscoveryOptions) (<-chan DiscoveryResult[KubernetesService], error) {
	resultCh := make(chan DiscoveryResult[KubernetesService], 100)

	go func() {
		defer close(resultCh)

		ticker := time.NewTicker(kd.config.RefreshInterval)
		defer ticker.Stop()

		// Initial discovery
		kd.performStreamDiscovery(ctx, opts, resultCh)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				kd.performStreamDiscovery(ctx, opts, resultCh)
			}
		}
	}()

	return resultCh, nil
}

// performDiscovery executes the actual discovery logic
func (kd *KubernetesDiscovery) performDiscovery(ctx context.Context, opts DiscoveryOptions) ([]KubernetesService, error) {
	// Determine namespaces to scan
	namespaces := kd.getNamespacesToScan(opts)

	// Create discovery tasks
	var services []KubernetesService
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Use semaphore for concurrency control
	sem := make(chan struct{}, opts.Concurrency)

	for _, namespace := range namespaces {
		wg.Add(1)

		// Submit work to worker pool
		kd.workerPool.Submit(ctx, func(workerCtx context.Context) error {
			defer wg.Done()

			// Acquire semaphore
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-workerCtx.Done():
				return workerCtx.Err()
			}

			// Discover services in namespace
			nsServices, err := kd.discoverInNamespace(workerCtx, namespace, opts)
			if err != nil {
				kd.logger.Warn("Failed to discover services in namespace",
					"namespace", namespace,
					"error", err)
				return nil // Don't fail entire discovery for one namespace
			}

			// Add to results
			mu.Lock()
			services = append(services, nsServices...)
			mu.Unlock()

			return nil
		})
	}

	// Wait for all workers to complete
	wg.Wait()

	// Apply filters
	services = kd.applyFilters(services, opts.Filters)

	return services, nil
}

// discoverInNamespace discovers services in a specific namespace
func (kd *KubernetesDiscovery) discoverInNamespace(ctx context.Context, namespace string, opts DiscoveryOptions) ([]KubernetesService, error) {
	// Build list options
	listOpts := metav1.ListOptions{
		TimeoutSeconds: &[]int64{int64(opts.Timeout.Seconds())}[0],
	}

	// Apply label selector if configured
	if kd.config.LabelSelector != "" {
		listOpts.LabelSelector = kd.config.LabelSelector
	}

	// List services
	serviceList, err := kd.client.CoreV1().Services(namespace).List(ctx, listOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to list services in namespace %s: %w", namespace, err)
	}

	// Convert to our service type
	services := make([]KubernetesService, 0, len(serviceList.Items))
	now := time.Now()

	for _, svc := range serviceList.Items {
		kubeService := KubernetesService{
			ID:           string(svc.UID),
			Name:         svc.Name,
			Namespace:    svc.Namespace,
			Type:         string(svc.Spec.Type),
			ClusterIP:    svc.Spec.ClusterIP,
			Labels:       svc.Labels,
			Annotations:  svc.Annotations,
			DiscoveredAt: now,
			LastSeen:     now,
			Health:       HealthUnknown,
		}

		// Convert ports
		for _, port := range svc.Spec.Ports {
			kubeService.Ports = append(kubeService.Ports, ServicePort{
				Name:       port.Name,
				Port:       port.Port,
				TargetPort: port.TargetPort.String(),
				Protocol:   string(port.Protocol),
				NodePort:   port.NodePort,
			})
		}

		// Build endpoints
		kubeService.Endpoints = kd.buildEndpoints(svc.Spec.ClusterIP, kubeService.Ports)

		services = append(services, kubeService)
	}

	return services, nil
}

// buildEndpoints creates Endpoint objects from service information
func (kd *KubernetesDiscovery) buildEndpoints(clusterIP string, ports []ServicePort) []Endpoint {
	if clusterIP == "" || clusterIP == "None" {
		return nil
	}

	endpoints := make([]Endpoint, 0, len(ports))

	for _, port := range ports {
		endpoint := Endpoint{
			Address:  clusterIP,
			Port:     int(port.Port),
			Protocol: port.Protocol,
			Secure:   port.Name == "https" || port.Port == 443,
			Metadata: map[string]string{
				"port_name":   port.Name,
				"target_port": port.TargetPort,
			},
		}

		if port.NodePort > 0 {
			endpoint.Metadata["node_port"] = strconv.Itoa(int(port.NodePort))
		}

		endpoints = append(endpoints, endpoint)
	}

	return endpoints
}

// validateServices validates discovered services
func (kd *KubernetesDiscovery) validateServices(ctx context.Context, services []KubernetesService) []KubernetesService {
	if len(services) == 0 {
		return services
	}

	// Convert to ServiceInfo for validation
	serviceInfos := make([]ServiceInfo, len(services))
	for i, svc := range services {
		serviceInfos[i] = ServiceInfo{
			ID:        svc.ID,
			Name:      svc.Name,
			Type:      svc.Type,
			Endpoints: svc.Endpoints,
			Metadata:  svc.GetMetadata(),
			Namespace: svc.Namespace,
		}
	}

	// Perform batch validation
	validationResults := kd.validator.ValidateBatch(ctx, serviceInfos)

	// Update services with validation results
	for i, result := range validationResults.Results {
		if i < len(services) {
			if result.Valid {
				services[i].Health = HealthHealthy
				kd.stats.ValidationsPassed++
			} else {
				services[i].Health = HealthUnhealthy
				kd.stats.ValidationsFailed++
			}
		}
	}

	return services
}

// Helper methods

func (kd *KubernetesDiscovery) getNamespacesToScan(opts DiscoveryOptions) []string {
	// Use options namespaces if specified
	if len(opts.Namespaces) > 0 {
		return opts.Namespaces
	}

	// Use config filter if specified
	if len(kd.config.NamespaceFilter) > 0 {
		return kd.config.NamespaceFilter
	}

	// Default to all namespaces
	return []string{""}
}

func (kd *KubernetesDiscovery) buildCacheKey(opts DiscoveryOptions) CacheKey {
	key := fmt.Sprintf("k8s-discovery-%s-%v-%s",
		kd.config.LabelSelector,
		opts.Namespaces,
		opts.Labels)

	return CacheKey{
		Namespace: "kubernetes",
		Key:       key,
		Version:   "v1",
	}
}

func (kd *KubernetesDiscovery) applyFilters(services []KubernetesService, filters []DiscoveryFilter) []KubernetesService {
	if len(filters) == 0 {
		return services
	}

	filtered := make([]KubernetesService, 0, len(services))

	for _, service := range services {
		include := true

		for _, filter := range filters {
			if f, ok := filter.(func(KubernetesService) bool); ok {
				if !f(service) {
					include = false
					break
				}
			}
		}

		if include {
			filtered = append(filtered, service)
		}
	}

	return filtered
}

func (kd *KubernetesDiscovery) performStreamDiscovery(ctx context.Context, opts DiscoveryOptions, resultCh chan<- DiscoveryResult[KubernetesService]) {
	start := time.Now()

	services, err := kd.performDiscovery(ctx, opts)
	duration := time.Since(start)

	result := DiscoveryResult[KubernetesService]{
		Services:  services,
		Error:     err,
		Timestamp: start,
		Duration:  duration,
		Source:    "kubernetes",
		Metadata: map[string]interface{}{
			"namespaces_scanned": kd.getNamespacesToScan(opts),
			"cache_enabled":      opts.EnableCache,
			"validation_enabled": opts.EnableValidation,
		},
	}

	select {
	case resultCh <- result:
	case <-ctx.Done():
	}
}

func (kd *KubernetesDiscovery) updateStats(duration time.Duration) {
	kd.mu.Lock()
	defer kd.mu.Unlock()

	kd.stats.DiscoveryCount++
	kd.stats.LastDiscovery = time.Now()

	// Update rolling average
	if kd.stats.AverageLatency == 0 {
		kd.stats.AverageLatency = duration
	} else {
		// Simple moving average
		kd.stats.AverageLatency = (kd.stats.AverageLatency + duration) / 2
	}
}

// Validate ensures discovered services are reachable and healthy
func (kd *KubernetesDiscovery) Validate(ctx context.Context, services []KubernetesService) ValidationResults {
	serviceInfos := make([]ServiceInfo, len(services))
	for i, svc := range services {
		serviceInfos[i] = ServiceInfo{
			ID:        svc.ID,
			Name:      svc.Name,
			Type:      svc.Type,
			Endpoints: svc.Endpoints,
			Metadata:  svc.GetMetadata(),
			Namespace: svc.Namespace,
		}
	}

	return kd.validator.ValidateBatch(ctx, serviceInfos)
}

// Health returns the current health status of the discovery system
func (kd *KubernetesDiscovery) Health() HealthStatus {
	kd.mu.RLock()
	defer kd.mu.RUnlock()

	if !kd.healthy {
		return HealthUnhealthy
	}

	// Check circuit breaker state
	if kd.circuitBreaker.State() == CircuitOpen {
		return HealthDegraded
	}

	return HealthHealthy
}
