package kubeapi

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

// K8s watcher state
type k8sWatcherState struct {
	dynamicClient   dynamic.Interface
	informerFactory dynamicinformer.DynamicSharedInformerFactory
	stopCh          chan struct{}
}

// startK8sWatch initializes K8s API watching
func (c *Collector) startK8sWatch() error {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		config, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
		if err != nil {
			return fmt.Errorf("unable to create k8s config: %w", err)
		}
	}

	// Create dynamic client
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("unable to create dynamic client: %w", err)
	}

	// Create informer factory
	informerFactory := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, time.Minute*30)

	// Create watcher state
	state := &k8sWatcherState{
		dynamicClient:   dynamicClient,
		informerFactory: informerFactory,
		stopCh:          make(chan struct{}),
	}

	// Set up watchers for core resources
	resources := []schema.GroupVersionResource{
		{Group: "", Version: "v1", Resource: "namespaces"},
		{Group: "", Version: "v1", Resource: "pods"},
		{Group: "", Version: "v1", Resource: "services"},
		{Group: "", Version: "v1", Resource: "nodes"},
		{Group: "", Version: "v1", Resource: "events"},
		{Group: "apps", Version: "v1", Resource: "deployments"},
		{Group: "apps", Version: "v1", Resource: "replicasets"},
	}

	for _, resource := range resources {
		c.setupResourceWatcher(state, resource)
	}

	// Start informers
	informerFactory.Start(state.stopCh)

	// Store state (would need to add this field to Collector struct)
	// For now, we'll manage it within this function's lifecycle

	// Start a goroutine to manage the watcher
	go func() {
		<-c.ctx.Done()
		close(state.stopCh)
	}()

	return nil
}

// setupResourceWatcher sets up watching for a specific resource
func (c *Collector) setupResourceWatcher(state *k8sWatcherState, resource schema.GroupVersionResource) {
	informer := state.informerFactory.ForResource(resource).Informer()

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.handleK8sEvent("ADDED", resource.Resource, obj)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			c.handleK8sEvent("MODIFIED", resource.Resource, newObj)
		},
		DeleteFunc: func(obj interface{}) {
			c.handleK8sEvent("DELETED", resource.Resource, obj)
		},
	})
}

// handleK8sEvent processes K8s API events with NO business logic
func (c *Collector) handleK8sEvent(eventType, resourceType string, obj interface{}) {
	unstructuredObj, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return
	}

	// Create raw event with just the data - NO enrichment or correlation
	eventData := map[string]interface{}{
		"api_version": unstructuredObj.GetAPIVersion(),
		"kind":        unstructuredObj.GetKind(),
		"name":        unstructuredObj.GetName(),
		"namespace":   unstructuredObj.GetNamespace(),
		"uid":         string(unstructuredObj.GetUID()),
		"resource":    resourceType,
		"action":      eventType,
		"object":      unstructuredObj.Object, // Raw K8s object
	}

	rawEvent := c.createEvent("api_event", eventData)

	select {
	case c.events <- rawEvent:
	case <-c.ctx.Done():
		return
	default:
		// Buffer full, drop event
	}
}

// stopK8sWatch stops K8s watching (placeholder for now)
func (c *Collector) stopK8sWatch() {
	// In a full implementation, we would stop the informer factory here
	// For now, it's handled by the context cancellation
}
