package extraction

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// K8sContextExtractor enriches events with comprehensive K8s context
type K8sContextExtractor struct {
	k8sClient kubernetes.Interface
	cache     *K8sCache
	logger    *zap.Logger

	// Extraction strategies
	extractors map[ExtractionDepth][]Extractor

	// Performance
	mu                sync.RWMutex
	extractionMetrics map[string]*ExtractionMetrics
}

// ExtractionDepth controls how much context to extract
type ExtractionDepth int

const (
	// Shallow extraction - basic identity only
	Shallow ExtractionDepth = iota
	// Medium extraction - identity + ownership + basic topology
	Medium
	// Deep extraction - everything including historical data
	Deep
)

// Extractor is a function that extracts specific context
type Extractor func(ctx context.Context, event *domain.UnifiedEvent, cache *K8sCache) error

// K8sCache provides fast lookups for K8s resources
type K8sCache struct {
	// Primary caches
	podInformer      cache.SharedIndexInformer
	serviceInformer  cache.SharedIndexInformer
	endpointInformer cache.SharedIndexInformer
	nodeInformer     cache.SharedIndexInformer

	// Workload caches
	deploymentInformer  cache.SharedIndexInformer
	replicaSetInformer  cache.SharedIndexInformer
	statefulSetInformer cache.SharedIndexInformer
	daemonSetInformer   cache.SharedIndexInformer

	// Config caches
	configMapInformer cache.SharedIndexInformer
	secretInformer    cache.SharedIndexInformer

	// Custom indexes for fast lookup
	containerIDIndex string
	podIPIndex       string
}

// ExtractionMetrics tracks extraction performance
type ExtractionMetrics struct {
	TotalExtractions int64
	ShallowCount     int64
	MediumCount      int64
	DeepCount        int64
	AverageLatency   time.Duration
	CacheHits        int64
	CacheMisses      int64
	ExtractionErrors int64
}

// NewK8sContextExtractor creates a new context extractor
func NewK8sContextExtractor(k8sClient kubernetes.Interface, logger *zap.Logger) (*K8sContextExtractor, error) {
	cache, err := NewK8sCache(k8sClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create K8s cache: %w", err)
	}

	extractor := &K8sContextExtractor{
		k8sClient:         k8sClient,
		cache:             cache,
		logger:            logger,
		extractionMetrics: make(map[string]*ExtractionMetrics),
	}

	// Register extraction strategies
	extractor.extractors = map[ExtractionDepth][]Extractor{
		Shallow: {
			extractor.extractBasicIdentity,
			extractor.extractLabelsAndAnnotations,
		},
		Medium: {
			extractor.extractBasicIdentity,
			extractor.extractLabelsAndAnnotations,
			extractor.extractOwnership,
			extractor.extractTopology,
			extractor.extractResourceSpecs,
		},
		Deep: {
			extractor.extractBasicIdentity,
			extractor.extractLabelsAndAnnotations,
			extractor.extractOwnership,
			extractor.extractTopology,
			extractor.extractResourceSpecs,
			extractor.extractDependencies,
			extractor.extractState,
			extractor.extractOperationalContext,
			extractor.extractNetworkContext,
		},
	}

	return extractor, nil
}

// Process enriches an event with K8s context
func (e *K8sContextExtractor) Process(ctx context.Context, event *domain.UnifiedEvent) error {
	// Skip if not K8s related
	if !e.isK8sRelated(event) {
		return nil
	}

	// Determine extraction depth
	depth := e.determineExtractionDepth(event)

	// Initialize K8s context if needed
	if event.K8sContext == nil {
		event.K8sContext = &domain.K8sContext{}
	}

	// Apply extractors for the determined depth
	extractors := e.extractors[depth]
	for _, extractor := range extractors {
		if err := extractor(ctx, event, e.cache); err != nil {
			e.logger.Warn("Extraction failed",
				zap.Error(err),
				zap.String("event_id", event.ID),
			)
			// Continue with other extractors
		}
	}

	// Update metrics
	e.updateMetrics(event.Source, depth)

	return nil
}

// isK8sRelated checks if event needs K8s context
func (e *K8sContextExtractor) isK8sRelated(event *domain.UnifiedEvent) bool {
	// Check if already has K8s data
	if event.Kubernetes != nil {
		return true
	}

	// Check if has container/pod identifiers
	// Note: ContainerID would need to be extracted from kernel namespace info

	// Check if entity is K8s related
	if event.Entity != nil {
		switch event.Entity.Type {
		case "pod", "service", "deployment", "node", "container":
			return true
		}
	}

	// Check source
	switch event.Source {
	case "kubeapi", "kubernetes", "ebpf", "cni":
		return true
	}

	return false
}

// determineExtractionDepth decides how much context to extract
func (e *K8sContextExtractor) determineExtractionDepth(event *domain.UnifiedEvent) ExtractionDepth {
	// High severity events get deep extraction
	if event.Severity == domain.EventSeverityCritical ||
		event.Severity == domain.EventSeverityError {
		return Deep
	}

	// Events with anomalies get deep extraction
	if event.Anomaly != nil && event.Anomaly.Score > 0.8 {
		return Deep
	}

	// Events with high impact get medium extraction
	if event.Impact != nil && event.Impact.InfrastructureImpact > 0.7 {
		return Medium
	}

	// Default to shallow
	return Shallow
}

// Extraction functions

func (e *K8sContextExtractor) extractBasicIdentity(ctx context.Context, event *domain.UnifiedEvent, cache *K8sCache) error {
	k8sCtx := event.K8sContext

	// Try to find pod by various means
	var pod *corev1.Pod

	// Method 1: From Kubernetes event data
	if event.Kubernetes != nil && event.Kubernetes.Object != "" {
		parts := strings.Split(event.Kubernetes.Object, "/")
		if len(parts) == 2 && parts[0] == "pod" {
			pod, _ = cache.GetPod("", parts[1])
		}
	}

	// Method 2: From container ID (eBPF events)
	// Note: ContainerID would need to be extracted from kernel namespace info
	// For now, skip this method as ContainerID is not in KernelData

	// Method 3: From entity
	if pod == nil && event.Entity != nil && event.Entity.Type == "pod" {
		pod, _ = cache.GetPod(event.Entity.Namespace, event.Entity.Name)
	}

	if pod != nil {
		k8sCtx.APIVersion = pod.APIVersion
		k8sCtx.Kind = pod.Kind
		k8sCtx.UID = string(pod.UID)
		k8sCtx.Name = pod.Name
		k8sCtx.Namespace = pod.Namespace
		k8sCtx.ResourceVersion = pod.ResourceVersion
		k8sCtx.Generation = pod.Generation

		// Node placement
		k8sCtx.NodeName = pod.Spec.NodeName

		// Basic state
		k8sCtx.Phase = string(pod.Status.Phase)

		// QoS class
		k8sCtx.QoSClass = string(pod.Status.QOSClass)
	}

	return nil
}

func (e *K8sContextExtractor) extractLabelsAndAnnotations(ctx context.Context, event *domain.UnifiedEvent, cache *K8sCache) error {
	k8sCtx := event.K8sContext

	// Get pod for full metadata
	pod, err := cache.GetPod(k8sCtx.Namespace, k8sCtx.Name)
	if err != nil || pod == nil {
		return nil // Skip if pod not found
	}

	k8sCtx.Labels = pod.Labels
	k8sCtx.Annotations = pod.Annotations

	return nil
}

func (e *K8sContextExtractor) extractOwnership(ctx context.Context, event *domain.UnifiedEvent, cache *K8sCache) error {
	k8sCtx := event.K8sContext

	pod, err := cache.GetPod(k8sCtx.Namespace, k8sCtx.Name)
	if err != nil || pod == nil {
		return nil
	}

	// Direct owner references
	for _, ref := range pod.OwnerReferences {
		ownerRef := domain.OwnerReference{
			APIVersion:         ref.APIVersion,
			Kind:               ref.Kind,
			Name:               ref.Name,
			UID:                string(ref.UID),
			Controller:         ref.Controller,
			BlockOwnerDeletion: ref.BlockOwnerDeletion,
		}
		k8sCtx.OwnerReferences = append(k8sCtx.OwnerReferences, ownerRef)

		// Extract workload info from owner
		if ref.Controller != nil && *ref.Controller {
			k8sCtx.WorkloadKind = ref.Kind
			k8sCtx.WorkloadName = ref.Name

			// Special handling for ReplicaSet to get Deployment
			if ref.Kind == "ReplicaSet" {
				if deployment := cache.GetDeploymentForReplicaSet(k8sCtx.Namespace, ref.Name); deployment != nil {
					k8sCtx.WorkloadKind = "Deployment"
					k8sCtx.WorkloadName = deployment.Name
				}
			}
		}
	}

	return nil
}

func (e *K8sContextExtractor) extractTopology(ctx context.Context, event *domain.UnifiedEvent, cache *K8sCache) error {
	k8sCtx := event.K8sContext

	// Find services that select this pod
	services := cache.GetServicesForPod(k8sCtx.Namespace, k8sCtx.Name)
	for _, svc := range services {
		k8sCtx.Consumers = append(k8sCtx.Consumers, domain.K8sResourceRef{
			Kind:      "Service",
			Name:      svc.Name,
			Namespace: svc.Namespace,
		})
	}

	// Extract node topology info
	if k8sCtx.NodeName != "" {
		node, err := cache.GetNode(k8sCtx.NodeName)
		if err == nil && node != nil {
			// Extract zone/region from node labels
			if zone, ok := node.Labels["topology.kubernetes.io/zone"]; ok {
				k8sCtx.Zone = zone
			}
			if region, ok := node.Labels["topology.kubernetes.io/region"]; ok {
				k8sCtx.Region = region
			}
		}
	}

	return nil
}

func (e *K8sContextExtractor) extractResourceSpecs(ctx context.Context, event *domain.UnifiedEvent, cache *K8sCache) error {
	k8sCtx := event.K8sContext

	pod, err := cache.GetPod(k8sCtx.Namespace, k8sCtx.Name)
	if err != nil || pod == nil {
		return nil
	}

	// Aggregate resource requests/limits
	requests := make(domain.ResourceList)
	limits := make(domain.ResourceList)

	for _, container := range pod.Spec.Containers {
		for name, quantity := range container.Resources.Requests {
			requests[string(name)] = quantity.String()
		}
		for name, quantity := range container.Resources.Limits {
			limits[string(name)] = quantity.String()
		}
	}

	k8sCtx.ResourceRequests = requests
	k8sCtx.ResourceLimits = limits

	return nil
}

func (e *K8sContextExtractor) extractDependencies(ctx context.Context, event *domain.UnifiedEvent, cache *K8sCache) error {
	k8sCtx := event.K8sContext

	pod, err := cache.GetPod(k8sCtx.Namespace, k8sCtx.Name)
	if err != nil || pod == nil {
		return nil
	}

	// Extract ConfigMap dependencies
	for _, volume := range pod.Spec.Volumes {
		if volume.ConfigMap != nil {
			k8sCtx.Dependencies = append(k8sCtx.Dependencies, domain.ResourceDependency{
				Kind:      "ConfigMap",
				Name:      volume.ConfigMap.Name,
				Namespace: k8sCtx.Namespace,
				Type:      "config",
				Required:  volume.ConfigMap.Optional == nil || !*volume.ConfigMap.Optional,
			})
		}
		if volume.Secret != nil {
			k8sCtx.Dependencies = append(k8sCtx.Dependencies, domain.ResourceDependency{
				Kind:      "Secret",
				Name:      volume.Secret.SecretName,
				Namespace: k8sCtx.Namespace,
				Type:      "config",
				Required:  volume.Secret.Optional == nil || !*volume.Secret.Optional,
			})
		}
		if volume.PersistentVolumeClaim != nil {
			k8sCtx.Dependencies = append(k8sCtx.Dependencies, domain.ResourceDependency{
				Kind:      "PersistentVolumeClaim",
				Name:      volume.PersistentVolumeClaim.ClaimName,
				Namespace: k8sCtx.Namespace,
				Type:      "storage",
				Required:  true,
			})
		}
	}

	return nil
}

func (e *K8sContextExtractor) extractState(ctx context.Context, event *domain.UnifiedEvent, cache *K8sCache) error {
	k8sCtx := event.K8sContext

	pod, err := cache.GetPod(k8sCtx.Namespace, k8sCtx.Name)
	if err != nil || pod == nil {
		return nil
	}

	// Extract conditions
	for _, condition := range pod.Status.Conditions {
		k8sCtx.Conditions = append(k8sCtx.Conditions, domain.ConditionSnapshot{
			Type:               string(condition.Type),
			Status:             string(condition.Status),
			LastTransitionTime: condition.LastTransitionTime.Time,
			Reason:             condition.Reason,
			Message:            condition.Message,
		})
	}

	// Extract resource context (desired vs actual)
	if event.ResourceContext == nil {
		event.ResourceContext = &domain.ResourceContext{}
	}

	// Build desired state from spec
	desiredState := &domain.ResourceState{
		Replicas: e.getDesiredReplicas(k8sCtx),
	}

	// Build actual state from status
	actualState := &domain.ResourceState{
		Replicas: e.getActualReplicas(k8sCtx),
	}

	event.ResourceContext.DesiredState = desiredState
	event.ResourceContext.ActualState = actualState

	return nil
}

func (e *K8sContextExtractor) extractOperationalContext(ctx context.Context, event *domain.UnifiedEvent, cache *K8sCache) error {
	// This would extract metrics, restart patterns, etc.
	// For now, we'll create a placeholder
	if event.OperationalContext == nil {
		event.OperationalContext = &domain.OperationalContext{
			HealthStatus: "unknown",
		}
	}

	// In a real implementation, this would:
	// 1. Query metrics for resource utilization
	// 2. Check recent events for restart patterns
	// 3. Analyze traffic patterns from network data

	return nil
}

// Helper methods

func (e *K8sContextExtractor) getDesiredReplicas(k8sCtx *domain.K8sContext) *domain.ReplicaState {
	// This would look up the workload controller
	// For now, return nil
	return nil
}

func (e *K8sContextExtractor) getActualReplicas(k8sCtx *domain.K8sContext) *domain.ReplicaState {
	// This would count actual pods
	// For now, return nil
	return nil
}

func (e *K8sContextExtractor) extractNetworkContext(ctx context.Context, event *domain.UnifiedEvent, cache *K8sCache) error {
	// Delegate to specialized network extractor
	networkExtractor := &NetworkContextExtractor{cache: cache}
	return networkExtractor.ExtractNetworkContext(ctx, event, cache)
}

func (e *K8sContextExtractor) updateMetrics(source string, depth ExtractionDepth) {
	e.mu.Lock()
	defer e.mu.Unlock()

	metrics, ok := e.extractionMetrics[source]
	if !ok {
		metrics = &ExtractionMetrics{}
		e.extractionMetrics[source] = metrics
	}

	metrics.TotalExtractions++
	switch depth {
	case Shallow:
		metrics.ShallowCount++
	case Medium:
		metrics.MediumCount++
	case Deep:
		metrics.DeepCount++
	}
}

// GetMetrics returns extraction metrics
func (e *K8sContextExtractor) GetMetrics() map[string]*ExtractionMetrics {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Return a copy
	result := make(map[string]*ExtractionMetrics)
	for k, v := range e.extractionMetrics {
		metricsCopy := *v
		result[k] = &metricsCopy
	}
	return result
}
