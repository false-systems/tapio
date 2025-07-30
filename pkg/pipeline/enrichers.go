package pipeline

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

// K8sEnricher enriches events with Kubernetes context
type K8sEnricher struct {
	clientset       *kubernetes.Clientset
	informerFactory informers.SharedInformerFactory

	// Caches
	podCache     cache.SharedIndexInformer
	serviceCache cache.SharedIndexInformer
	nodeCache    cache.SharedIndexInformer

	// Reverse lookups
	mu             sync.RWMutex
	ipToPod        map[string]*v1.Pod
	containerToPod map[string]*v1.Pod

	started bool
}

// NewK8sEnricher creates a new Kubernetes enricher
func NewK8sEnricher(kubeconfig string) (Enricher, error) {
	var config *rest.Config
	var err error

	if kubeconfig == "" {
		// Try in-cluster config
		config, err = rest.InClusterConfig()
		if err != nil {
			// Fall back to default kubeconfig
			kubeconfig = clientcmd.RecommendedHomeFile
			config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
			if err != nil {
				return nil, fmt.Errorf("failed to build k8s config: %w", err)
			}
		}
	} else {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to build k8s config: %w", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client: %w", err)
	}

	enricher := &K8sEnricher{
		clientset:      clientset,
		ipToPod:        make(map[string]*v1.Pod),
		containerToPod: make(map[string]*v1.Pod),
	}

	// Create informer factory
	enricher.informerFactory = informers.NewSharedInformerFactory(clientset, 30*time.Second)

	// Set up informers
	enricher.setupInformers()

	return enricher, nil
}

// setupInformers sets up the K8s informers
func (e *K8sEnricher) setupInformers() {
	// Pod informer
	e.podCache = e.informerFactory.Core().V1().Pods().Informer()
	e.podCache.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    e.onPodAdd,
		UpdateFunc: e.onPodUpdate,
		DeleteFunc: e.onPodDelete,
	})

	// Service informer
	e.serviceCache = e.informerFactory.Core().V1().Services().Informer()

	// Node informer
	e.nodeCache = e.informerFactory.Core().V1().Nodes().Informer()
}

// Start starts the enricher
func (e *K8sEnricher) Start(ctx context.Context) error {
	if e.started {
		return nil
	}

	// Start informers
	e.informerFactory.Start(ctx.Done())

	// Wait for caches to sync
	if !cache.WaitForCacheSync(ctx.Done(),
		e.podCache.HasSynced,
		e.serviceCache.HasSynced,
		e.nodeCache.HasSynced) {
		return fmt.Errorf("failed to sync k8s caches")
	}

	e.started = true
	return nil
}

// Enrich adds K8s context to an event
func (e *K8sEnricher) Enrich(ctx context.Context, event *domain.UnifiedEvent) error {
	if !e.started {
		// Start on first use
		if err := e.Start(ctx); err != nil {
			return err
		}
	}

	// Skip if already has K8s context
	if event.K8s != nil && event.K8s.PodName != "" {
		return nil
	}

	// Try to find pod by different methods
	var pod *v1.Pod

	// Method 1: By PID (for eBPF events)
	if event.Kernel != nil && event.Kernel.PID > 0 {
		pod = e.findPodByPID(event.Kernel.PID)
	}

	// Method 2: By IP (for network events)
	if pod == nil && event.Network != nil && event.Network.SourceIP != "" {
		pod = e.findPodByIP(event.Network.SourceIP)
	}

	// Method 3: By container ID (if available in metadata)
	if pod == nil && event.Metadata != nil {
		if containerID, ok := event.Metadata["container_id"]; ok {
			pod = e.findPodByContainer(containerID)
		}
	}

	// If we found a pod, enrich the event
	if pod != nil {
		if event.K8s == nil {
			event.K8s = &domain.K8sContext{}
		}

		event.K8s.Namespace = pod.Namespace
		event.K8s.PodName = pod.Name
		event.K8s.PodUID = string(pod.UID)

		// Add labels
		if event.K8s.Labels == nil {
			event.K8s.Labels = make(map[string]string)
		}
		for k, v := range pod.Labels {
			event.K8s.Labels[k] = v
		}

		// Try to find owning workload
		for _, owner := range pod.OwnerReferences {
			event.K8s.WorkloadType = owner.Kind
			event.K8s.WorkloadName = owner.Name
			break
		}

		// Find node
		event.K8s.NodeName = pod.Spec.NodeName

		// Find services
		services := e.findServicesForPod(pod)
		if len(services) > 0 {
			event.K8s.ServiceName = services[0].Name
		}
	}

	return nil
}

// Pod event handlers
func (e *K8sEnricher) onPodAdd(obj interface{}) {
	pod := obj.(*v1.Pod)
	e.updatePodCache(pod)
}

func (e *K8sEnricher) onPodUpdate(oldObj, newObj interface{}) {
	pod := newObj.(*v1.Pod)
	e.updatePodCache(pod)
}

func (e *K8sEnricher) onPodDelete(obj interface{}) {
	pod := obj.(*v1.Pod)
	e.removePodFromCache(pod)
}

// updatePodCache updates the reverse lookup caches
func (e *K8sEnricher) updatePodCache(pod *v1.Pod) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Update IP to pod mapping
	if pod.Status.PodIP != "" {
		e.ipToPod[pod.Status.PodIP] = pod
	}

	// Update container to pod mapping
	for _, container := range pod.Status.ContainerStatuses {
		if container.ContainerID != "" {
			e.containerToPod[container.ContainerID] = pod
		}
	}
}

// removePodFromCache removes pod from caches
func (e *K8sEnricher) removePodFromCache(pod *v1.Pod) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Remove from IP mapping
	delete(e.ipToPod, pod.Status.PodIP)

	// Remove from container mapping
	for _, container := range pod.Status.ContainerStatuses {
		delete(e.containerToPod, container.ContainerID)
	}
}

// Lookup methods
func (e *K8sEnricher) findPodByPID(pid uint32) *v1.Pod {
	// This would require a more sophisticated mapping
	// For now, return nil - in production, you'd maintain PID to pod mapping
	return nil
}

func (e *K8sEnricher) findPodByIP(ip string) *v1.Pod {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.ipToPod[ip]
}

func (e *K8sEnricher) findPodByContainer(containerID string) *v1.Pod {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.containerToPod[containerID]
}

func (e *K8sEnricher) findServicesForPod(pod *v1.Pod) []*v1.Service {
	services := []*v1.Service{}

	// Get all services from cache
	for _, obj := range e.serviceCache.GetStore().List() {
		svc := obj.(*v1.Service)

		// Skip if different namespace
		if svc.Namespace != pod.Namespace {
			continue
		}

		// Check if pod matches service selector
		if matchesSelector(pod.Labels, svc.Spec.Selector) {
			services = append(services, svc)
		}
	}

	return services
}

// matchesSelector checks if labels match selector
func matchesSelector(labels, selector map[string]string) bool {
	if len(selector) == 0 {
		return false
	}

	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}

	return true
}

// TraceEnricher adds OTEL trace context to events
type TraceEnricher struct{}

func NewTraceEnricher() Enricher {
	return &TraceEnricher{}
}

func (e *TraceEnricher) Enrich(ctx context.Context, event *domain.UnifiedEvent) error {
	// Extract trace context from event metadata if available
	if event.Metadata != nil {
		if traceID, ok := event.Metadata["trace_id"]; ok {
			if event.Context == nil {
				event.Context = &domain.EventContext{}
			}
			event.Context.TraceID = traceID
		}

		if spanID, ok := event.Metadata["span_id"]; ok {
			if event.Context == nil {
				event.Context = &domain.EventContext{}
			}
			event.Context.SpanID = spanID
		}
	}

	return nil
}
