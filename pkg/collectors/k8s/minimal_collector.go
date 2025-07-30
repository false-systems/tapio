package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

// MinimalK8sCollector implements minimal K8s collection following the blueprint
type MinimalK8sCollector struct {
	config collectors.CollectorConfig
	events chan collectors.RawEvent

	// K8s clients
	clientset     kubernetes.Interface
	dynamicClient dynamic.Interface

	// Informer factory
	informerFactory dynamicinformer.DynamicSharedInformerFactory
	stopCh          chan struct{}

	// eBPF integration
	ebpfCollector *K8sEBPFCollector
	enableEBPF    bool
	podCgroupMap  map[string]uint64 // pod UID -> cgroup ID
	cgroupPodMap  map[uint64]string // cgroup ID -> pod UID

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu      sync.RWMutex
	started bool
	healthy bool
}

// NewMinimalK8sCollector creates a new minimal K8s collector
func NewMinimalK8sCollector(config collectors.CollectorConfig) (*MinimalK8sCollector, error) {
	collector := &MinimalK8sCollector{
		config:       config,
		events:       make(chan collectors.RawEvent, config.BufferSize),
		healthy:      true,
		stopCh:       make(chan struct{}),
		podCgroupMap: make(map[string]uint64),
		cgroupPodMap: make(map[uint64]string),
	}

	// Check if eBPF is enabled in config
	if ebpfEnabled, ok := config.Labels["enable_ebpf"]; ok && ebpfEnabled == "true" {
		ebpfCollector, err := NewK8sEBPFCollector()
		if err != nil {
			// Log error but don't fail - eBPF is optional
			fmt.Printf("Failed to initialize eBPF collector: %v\n", err)
		} else {
			collector.ebpfCollector = ebpfCollector
			collector.enableEBPF = true
		}
	}

	return collector, nil
}

// Name returns the collector name
func (c *MinimalK8sCollector) Name() string {
	return "k8s-minimal"
}

// Start begins collection
func (c *MinimalK8sCollector) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return nil
	}

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Initialize K8s clients
	if err := c.initializeClients(); err != nil {
		return fmt.Errorf("failed to initialize clients: %w", err)
	}

	// Create informer factory
	c.informerFactory = dynamicinformer.NewDynamicSharedInformerFactory(c.dynamicClient, time.Minute*30)

	// Set up watchers for resources
	c.setupWatchers()

	// Start informers
	c.informerFactory.Start(c.stopCh)

	// Start eBPF collection if enabled
	if c.enableEBPF && c.ebpfCollector != nil {
		if err := c.ebpfCollector.Start(ctx); err != nil {
			fmt.Printf("Failed to start eBPF collector: %v\n", err)
			// Don't fail - continue without eBPF
		} else {
			// Start goroutine to process eBPF events
			c.wg.Add(1)
			go c.processEBPFEvents()
		}
	}

	c.started = true
	return nil
}

// Stop gracefully shuts down
func (c *MinimalK8sCollector) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.started {
		return nil
	}

	// Stop informers
	close(c.stopCh)

	// Stop eBPF collector if running
	if c.enableEBPF && c.ebpfCollector != nil {
		c.ebpfCollector.Stop()
	}

	// Cancel context
	c.cancel()

	// Wait for goroutines
	c.wg.Wait()

	close(c.events)
	c.started = false
	c.healthy = false

	return nil
}

// Events returns the event channel
func (c *MinimalK8sCollector) Events() <-chan collectors.RawEvent {
	return c.events
}

// IsHealthy returns health status
func (c *MinimalK8sCollector) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy
}

// initializeClients creates K8s clients
func (c *MinimalK8sCollector) initializeClients() error {
	var config *rest.Config
	var err error

	// Try in-cluster config first
	config, err = rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		kubeconfig := clientcmd.NewDefaultClientConfigLoadingRules().GetDefaultFilename()
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return fmt.Errorf("failed to create config: %w", err)
		}
	}

	// Create clientset
	c.clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create clientset: %w", err)
	}

	// Create dynamic client
	c.dynamicClient, err = dynamic.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	return nil
}

// setupWatchers sets up informers for K8s resources
func (c *MinimalK8sCollector) setupWatchers() {
	// Core resources to watch
	resources := []schema.GroupVersionResource{
		{Group: "", Version: "v1", Resource: "pods"},
		{Group: "", Version: "v1", Resource: "services"},
		{Group: "", Version: "v1", Resource: "nodes"},
		{Group: "", Version: "v1", Resource: "events"},
		{Group: "apps", Version: "v1", Resource: "deployments"},
		{Group: "apps", Version: "v1", Resource: "replicasets"},
	}

	for _, gvr := range resources {
		// Special handling for pods when eBPF is enabled
		if gvr.Resource == "pods" && c.enableEBPF {
			c.setupPodWatcher(gvr)
		} else {
			c.setupResourceWatcher(gvr)
		}
	}
}

// setupResourceWatcher sets up a watcher for a specific resource
func (c *MinimalK8sCollector) setupResourceWatcher(gvr schema.GroupVersionResource) {
	informer := c.informerFactory.ForResource(gvr).Informer()

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.handleResourceEvent("ADDED", gvr.Resource, obj)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			c.handleResourceEvent("MODIFIED", gvr.Resource, newObj)
		},
		DeleteFunc: func(obj interface{}) {
			c.handleResourceEvent("DELETED", gvr.Resource, obj)
		},
	})
}

// handleResourceEvent handles K8s resource events
func (c *MinimalK8sCollector) handleResourceEvent(eventType, resource string, obj interface{}) {
	// Extract unstructured object
	unstructuredObj, ok := obj.(*unstructured.Unstructured)
	if !ok {
		// Try to handle delete events with DeletedFinalStateUnknown
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return
		}
		unstructuredObj, ok = tombstone.Obj.(*unstructured.Unstructured)
		if !ok {
			return
		}
	}

	// Marshal to JSON - this is our raw data
	data, err := json.Marshal(unstructuredObj.Object)
	if err != nil {
		return
	}

	// Extract basic metadata for the event
	metadata := map[string]string{
		"event_type": eventType,
		"resource":   resource,
		"namespace":  unstructuredObj.GetNamespace(),
		"name":       unstructuredObj.GetName(),
		"uid":        string(unstructuredObj.GetUID()),
	}

	// Create raw event
	event := collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "k8s",
		Data:      data, // Raw K8s object as JSON
		Metadata:  metadata,
	}

	// Send event
	select {
	case c.events <- event:
	case <-c.ctx.Done():
		return
	default:
		// Buffer full, drop event
	}
}

// MinimalK8sConfig returns a minimal K8s collector config
func MinimalK8sConfig() collectors.CollectorConfig {
	config := DefaultK8sConfig()
	config.Labels["collector"] = "k8s-minimal"
	return config
}

// setupPodWatcher sets up a pod watcher with eBPF integration
func (c *MinimalK8sCollector) setupPodWatcher(gvr schema.GroupVersionResource) {
	informer := c.informerFactory.ForResource(gvr).Informer()

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.handlePodEvent("ADDED", obj)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			c.handlePodEvent("MODIFIED", newObj)
		},
		DeleteFunc: func(obj interface{}) {
			c.handlePodEvent("DELETED", obj)
		},
	})
}

// handlePodEvent handles pod events with eBPF cgroup tracking
func (c *MinimalK8sCollector) handlePodEvent(eventType string, obj interface{}) {
	// First handle as normal resource event
	c.handleResourceEvent(eventType, "pods", obj)

	// Then update cgroup tracking if eBPF is enabled
	if !c.enableEBPF || c.ebpfCollector == nil {
		return
	}

	// Extract pod info
	unstructuredObj, ok := obj.(*unstructured.Unstructured)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return
		}
		unstructuredObj, ok = tombstone.Obj.(*unstructured.Unstructured)
		if !ok {
			return
		}
	}

	podUID := string(unstructuredObj.GetUID())
	namespace := unstructuredObj.GetNamespace()
	podName := unstructuredObj.GetName()

	// Extract cgroup ID from pod status if available
	status, found, err := unstructured.NestedMap(unstructuredObj.Object, "status")
	if err != nil || !found {
		return
	}

	// Look for container statuses
	containerStatuses, found, err := unstructured.NestedSlice(status, "containerStatuses")
	if err != nil || !found {
		return
	}

	for _, cs := range containerStatuses {
		containerStatus, ok := cs.(map[string]interface{})
		if !ok {
			continue
		}

		// Extract container ID
		containerID, found, err := unstructured.NestedString(containerStatus, "containerID")
		if err != nil || !found {
			continue
		}

		// Generate a pseudo cgroup ID based on container ID
		// In production, we'd extract the actual cgroup ID
		cgroupID := generateCgroupID(containerID)

		// Update mappings
		c.mu.Lock()
		if eventType == "DELETED" {
			delete(c.podCgroupMap, podUID)
			delete(c.cgroupPodMap, cgroupID)
		} else {
			c.podCgroupMap[podUID] = cgroupID
			c.cgroupPodMap[cgroupID] = podUID
		}
		c.mu.Unlock()

		// Update eBPF collector
		if eventType != "DELETED" {
			c.ebpfCollector.UpdatePodInfo(cgroupID, podUID, namespace, podName)
		}
	}
}

// processEBPFEvents processes events from the eBPF collector
func (c *MinimalK8sCollector) processEBPFEvents() {
	defer c.wg.Done()

	for {
		select {
		case event, ok := <-c.ebpfCollector.Events():
			if !ok {
				return
			}

			// Convert to raw event
			rawEvent := ConvertK8sSyscallEvent(event)

			// Enrich with pod info if we have it
			c.mu.RLock()
			if podUID, ok := c.cgroupPodMap[parseCgroupID(event.ContainerID)]; ok {
				rawEvent.Metadata["k8s_pod_uid"] = podUID
			}
			c.mu.RUnlock()

			// Send event
			select {
			case c.events <- rawEvent:
			case <-c.ctx.Done():
				return
			}

		case <-c.ctx.Done():
			return
		}
	}
}

// Helper function to generate cgroup ID from container ID
func generateCgroupID(containerID string) uint64 {
	// Simple hash function for demo
	// In production, extract actual cgroup ID
	var hash uint64
	for i := 0; i < len(containerID) && i < 8; i++ {
		hash = hash*31 + uint64(containerID[i])
	}
	return hash
}

// Helper function to parse cgroup ID from container ID string
func parseCgroupID(containerID string) uint64 {
	// Reverse of generateCgroupID
	var hash uint64
	for i := 0; i < len(containerID) && i < 8; i++ {
		hash = hash*31 + uint64(containerID[i])
	}
	return hash
}
