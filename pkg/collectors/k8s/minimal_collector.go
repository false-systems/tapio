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

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu      sync.RWMutex
	started bool
	healthy bool
}

// NewMinimalK8sCollector creates a new minimal K8s collector
func NewMinimalK8sCollector(config collectors.CollectorConfig) (*MinimalK8sCollector, error) {
	return &MinimalK8sCollector{
		config:  config,
		events:  make(chan collectors.RawEvent, config.BufferSize),
		healthy: true,
		stopCh:  make(chan struct{}),
	}, nil
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
		c.setupResourceWatcher(gvr)
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
