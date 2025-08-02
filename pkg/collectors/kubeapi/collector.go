package kubeapi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

// Collector implements K8s API collector with relationship tracking
type Collector struct {
	logger       *zap.Logger
	config       Config
	clientset    kubernetes.Interface
	traceManager *TraceManager

	// Event channel
	events chan collectors.RawEvent

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc

	// Informers
	informers []cache.SharedIndexInformer
}

// New creates a new kubeapi collector
func New(logger *zap.Logger, config Config) (*Collector, error) {
	// Get K8s config
	k8sConfig, err := getK8sConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get k8s config: %w", err)
	}

	// Create clientset
	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	return &Collector{
		logger:       logger,
		config:       config,
		clientset:    clientset,
		traceManager: NewTraceManager(),
		events:       make(chan collectors.RawEvent, config.BufferSize),
		informers:    make([]cache.SharedIndexInformer, 0),
	}, nil
}

// NewCollector creates a minimal collector (backward compatibility)
func NewCollector(name string) (*Collector, error) {
	logger := zap.NewNop()
	config := DefaultConfig()

	// For tests, create a minimal collector without K8s connection
	return &Collector{
		logger:       logger,
		config:       config,
		traceManager: NewTraceManager(),
		events:       make(chan collectors.RawEvent, config.BufferSize),
		informers:    make([]cache.SharedIndexInformer, 0),
	}, nil
}

// NewCollectorFromCollectorConfig creates from CollectorConfig
func NewCollectorFromCollectorConfig(config collectors.CollectorConfig) (*Collector, error) {
	logger := zap.NewNop()
	kubeConfig := DefaultConfig()
	return New(logger, kubeConfig)
}

// Name returns collector name
func (c *Collector) Name() string {
	return "kubeapi"
}

// Start begins collecting K8s events
func (c *Collector) Start(ctx context.Context) error {
	if c.ctx != nil {
		return fmt.Errorf("collector already started")
	}

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Set up watchers
	if err := c.setupWatchers(); err != nil {
		return fmt.Errorf("failed to setup watchers: %w", err)
	}

	// Start informers
	for _, informer := range c.informers {
		go informer.Run(c.ctx.Done())
	}

	// Wait for initial sync
	c.logger.Info("Waiting for initial K8s cache sync...")
	for _, informer := range c.informers {
		if !cache.WaitForCacheSync(c.ctx.Done(), informer.HasSynced) {
			return fmt.Errorf("failed to sync cache")
		}
	}

	c.logger.Info("K8s API collector started",
		zap.Int("watchers", len(c.informers)),
		zap.Strings("namespaces", c.config.WatchNamespaces))

	// For backward compatibility with existing k8s_watcher.go
	if err := c.startK8sWatch(); err != nil {
		c.logger.Warn("Legacy k8s watcher failed to start", zap.Error(err))
	}

	return nil
}

// Stop gracefully shuts down the collector
func (c *Collector) Stop() error {
	if c.cancel != nil {
		c.cancel()
	}

	// Stop legacy watcher
	c.stopK8sWatch()

	close(c.events)
	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan collectors.RawEvent {
	return c.events
}

// IsHealthy checks if collector is functioning
func (c *Collector) IsHealthy() bool {
	// Check if context is still active
	if c.ctx == nil {
		return false
	}

	select {
	case <-c.ctx.Done():
		return false
	default:
	}

	// Check if informers are synced
	for _, informer := range c.informers {
		if !informer.HasSynced() {
			return false
		}
	}

	return true
}

// setupWatchers creates all resource watchers
func (c *Collector) setupWatchers() error {
	// Namespace filter
	listOptions := metav1.ListOptions{}

	// 1. Pods - the most important resource
	podInformer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return c.clientset.CoreV1().Pods("").List(context.TODO(), listOptions)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return c.clientset.CoreV1().Pods("").Watch(context.TODO(), listOptions)
			},
		},
		&corev1.Pod{},
		c.config.ResyncPeriod,
		cache.Indexers{},
	)
	podInformer.AddEventHandler(c.resourceEventHandler("Pod"))
	c.informers = append(c.informers, podInformer)

	// 2. Deployments
	deploymentInformer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return c.clientset.AppsV1().Deployments("").List(context.TODO(), listOptions)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return c.clientset.AppsV1().Deployments("").Watch(context.TODO(), listOptions)
			},
		},
		&appsv1.Deployment{},
		c.config.ResyncPeriod,
		cache.Indexers{},
	)
	deploymentInformer.AddEventHandler(c.resourceEventHandler("Deployment"))
	c.informers = append(c.informers, deploymentInformer)

	// 3. Services
	serviceInformer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return c.clientset.CoreV1().Services("").List(context.TODO(), listOptions)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return c.clientset.CoreV1().Services("").Watch(context.TODO(), listOptions)
			},
		},
		&corev1.Service{},
		c.config.ResyncPeriod,
		cache.Indexers{},
	)
	serviceInformer.AddEventHandler(c.resourceEventHandler("Service"))
	c.informers = append(c.informers, serviceInformer)

	// 4. Endpoints (for service discovery)
	endpointsInformer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return c.clientset.CoreV1().Endpoints("").List(context.TODO(), listOptions)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return c.clientset.CoreV1().Endpoints("").Watch(context.TODO(), listOptions)
			},
		},
		&corev1.Endpoints{},
		c.config.ResyncPeriod,
		cache.Indexers{},
	)
	endpointsInformer.AddEventHandler(c.resourceEventHandler("Endpoints"))
	c.informers = append(c.informers, endpointsInformer)

	// 5. K8s Events (for causality)
	eventInformer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return c.clientset.CoreV1().Events("").List(context.TODO(), listOptions)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return c.clientset.CoreV1().Events("").Watch(context.TODO(), listOptions)
			},
		},
		&corev1.Event{},
		time.Minute*5, // Shorter resync for events
		cache.Indexers{},
	)
	eventInformer.AddEventHandler(c.resourceEventHandler("Event"))
	c.informers = append(c.informers, eventInformer)

	// 6. ReplicaSets (owned by Deployments)
	replicaSetInformer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return c.clientset.AppsV1().ReplicaSets("").List(context.TODO(), listOptions)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return c.clientset.AppsV1().ReplicaSets("").Watch(context.TODO(), listOptions)
			},
		},
		&appsv1.ReplicaSet{},
		c.config.ResyncPeriod,
		cache.Indexers{},
	)
	replicaSetInformer.AddEventHandler(c.resourceEventHandler("ReplicaSet"))
	c.informers = append(c.informers, replicaSetInformer)

	// 7. ConfigMaps
	configMapInformer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return c.clientset.CoreV1().ConfigMaps("").List(context.TODO(), listOptions)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return c.clientset.CoreV1().ConfigMaps("").Watch(context.TODO(), listOptions)
			},
		},
		&corev1.ConfigMap{},
		c.config.ResyncPeriod,
		cache.Indexers{},
	)
	configMapInformer.AddEventHandler(c.resourceEventHandler("ConfigMap"))
	c.informers = append(c.informers, configMapInformer)

	// Optional watchers based on config
	if c.config.TrackRBAC {
		// ServiceAccounts
		saInformer := cache.NewSharedIndexInformer(
			&cache.ListWatch{
				ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
					return c.clientset.CoreV1().ServiceAccounts("").List(context.TODO(), listOptions)
				},
				WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
					return c.clientset.CoreV1().ServiceAccounts("").Watch(context.TODO(), listOptions)
				},
			},
			&corev1.ServiceAccount{},
			c.config.ResyncPeriod,
			cache.Indexers{},
		)
		saInformer.AddEventHandler(c.resourceEventHandler("ServiceAccount"))
		c.informers = append(c.informers, saInformer)
	}

	if c.config.TrackNetworkPolicies {
		// NetworkPolicies
		npInformer := cache.NewSharedIndexInformer(
			&cache.ListWatch{
				ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
					return c.clientset.NetworkingV1().NetworkPolicies("").List(context.TODO(), listOptions)
				},
				WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
					return c.clientset.NetworkingV1().NetworkPolicies("").Watch(context.TODO(), listOptions)
				},
			},
			&networkingv1.NetworkPolicy{},
			c.config.ResyncPeriod,
			cache.Indexers{},
		)
		npInformer.AddEventHandler(c.resourceEventHandler("NetworkPolicy"))
		c.informers = append(c.informers, npInformer)
	}

	return nil
}

// resourceEventHandler creates handlers for resource events
func (c *Collector) resourceEventHandler(kind string) cache.ResourceEventHandlerFuncs {
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.handleResourceEvent("ADDED", kind, obj, nil)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			c.handleResourceEvent("MODIFIED", kind, newObj, oldObj)
		},
		DeleteFunc: func(obj interface{}) {
			c.handleResourceEvent("DELETED", kind, obj, nil)
		},
	}
}

// handleResourceEvent processes resource changes
func (c *Collector) handleResourceEvent(eventType, kind string, obj, oldObj interface{}) {
	// Extract metadata
	meta, err := getObjectMeta(obj)
	if err != nil {
		c.logger.Error("Failed to get object meta",
			zap.String("kind", kind),
			zap.Error(err))
		return
	}

	// Skip ignored namespaces
	if c.shouldIgnoreNamespace(meta.GetNamespace()) {
		return
	}

	// Get or create trace
	traceID := c.traceManager.GetOrCreateTrace(obj.(runtime.Object))
	spanID := collectors.GenerateSpanID()

	// Build resource event
	event := ResourceEvent{
		EventType:       eventType,
		Timestamp:       time.Now(),
		APIVersion:      obj.(runtime.Object).GetObjectKind().GroupVersionKind().Version,
		Kind:            kind,
		Name:            meta.GetName(),
		Namespace:       meta.GetNamespace(),
		UID:             meta.GetUID(),
		Labels:          meta.GetLabels(),
		Annotations:     meta.GetAnnotations(),
		Object:          obj.(runtime.Object),
		OldObject:       nil,
		ResourceVersion: meta.GetResourceVersion(),
		Source: EventSource{
			Component: "kubeapi-collector",
			Host:      getHostname(),
		},
	}

	if oldObj != nil {
		event.OldObject = oldObj.(runtime.Object)
	}

	// Extract owner references
	for _, owner := range meta.GetOwnerReferences() {
		event.OwnerReferences = append(event.OwnerReferences, OwnerRef{
			APIVersion: owner.APIVersion,
			Kind:       owner.Kind,
			Name:       owner.Name,
			UID:        owner.UID,
		})

		// Propagate trace from owner
		c.traceManager.PropagateTrace(
			owner.Kind, meta.GetNamespace(), owner.Name,
			kind, meta.GetNamespace(), meta.GetName(),
		)
	}

	// Extract relationships based on resource type
	c.extractRelationships(&event, obj)

	// Create raw event
	rawEvent := c.createRawEvent(&event, traceID, spanID)

	// Send event
	select {
	case c.events <- rawEvent:
	case <-c.ctx.Done():
		return
	default:
		c.logger.Warn("Event buffer full, dropping event",
			zap.String("kind", kind),
			zap.String("name", meta.GetName()))
	}
}

// extractRelationships finds related objects based on resource type
func (c *Collector) extractRelationships(event *ResourceEvent, obj interface{}) {
	switch v := obj.(type) {
	case *corev1.Pod:
		// Volume relationships
		for _, volume := range v.Spec.Volumes {
			if volume.ConfigMap != nil {
				event.RelatedObjects = append(event.RelatedObjects, ObjectReference{
					Kind:      "ConfigMap",
					Name:      volume.ConfigMap.Name,
					Namespace: v.Namespace,
					Relation:  "mounts",
				})
			}
			if volume.Secret != nil {
				event.RelatedObjects = append(event.RelatedObjects, ObjectReference{
					Kind:      "Secret",
					Name:      volume.Secret.SecretName,
					Namespace: v.Namespace,
					Relation:  "mounts",
				})
			}
			if volume.PersistentVolumeClaim != nil {
				event.RelatedObjects = append(event.RelatedObjects, ObjectReference{
					Kind:      "PersistentVolumeClaim",
					Name:      volume.PersistentVolumeClaim.ClaimName,
					Namespace: v.Namespace,
					Relation:  "mounts",
				})
			}
		}

		// ServiceAccount relationship
		if v.Spec.ServiceAccountName != "" {
			event.RelatedObjects = append(event.RelatedObjects, ObjectReference{
				Kind:      "ServiceAccount",
				Name:      v.Spec.ServiceAccountName,
				Namespace: v.Namespace,
				Relation:  "uses",
			})
		}

		// Node relationship
		if v.Spec.NodeName != "" {
			event.RelatedObjects = append(event.RelatedObjects, ObjectReference{
				Kind:     "Node",
				Name:     v.Spec.NodeName,
				Relation: "scheduled-on",
			})
		}

	case *appsv1.Deployment:
		// Deployment manages ReplicaSets
		event.Reason = "DeploymentChange"
		if v.Status.Replicas != v.Status.UpdatedReplicas {
			event.Message = fmt.Sprintf("Rolling update in progress: %d/%d replicas updated",
				v.Status.UpdatedReplicas, v.Status.Replicas)
		}

	case *corev1.Service:
		// Service selects pods
		if v.Spec.Selector != nil {
			// In production, would query matching pods
			event.RelatedObjects = append(event.RelatedObjects, ObjectReference{
				Kind:     "Pod",
				Relation: "selects",
			})
		}

		// Service creates endpoints
		event.RelatedObjects = append(event.RelatedObjects, ObjectReference{
			Kind:      "Endpoints",
			Name:      v.Name,
			Namespace: v.Namespace,
			Relation:  "creates",
		})

	case *corev1.Endpoints:
		// Endpoints link to service
		event.RelatedObjects = append(event.RelatedObjects, ObjectReference{
			Kind:      "Service",
			Name:      v.Name,
			Namespace: v.Namespace,
			Relation:  "created-by",
		})

		// Track pod addresses
		for _, subset := range v.Subsets {
			for _, addr := range subset.Addresses {
				if addr.TargetRef != nil && addr.TargetRef.Kind == "Pod" {
					event.RelatedObjects = append(event.RelatedObjects, ObjectReference{
						Kind:      "Pod",
						Name:      addr.TargetRef.Name,
						Namespace: addr.TargetRef.Namespace,
						Relation:  "endpoint-for",
					})
				}
			}
		}

	case *corev1.Event:
		// K8s Events provide causality information
		if v.InvolvedObject.Kind != "" {
			event.RelatedObjects = append(event.RelatedObjects, ObjectReference{
				Kind:      v.InvolvedObject.Kind,
				Name:      v.InvolvedObject.Name,
				Namespace: v.InvolvedObject.Namespace,
				UID:       v.InvolvedObject.UID,
				Relation:  "event-for",
			})
		}
		event.Reason = v.Reason
		event.Message = v.Message

		// Propagate trace from involved object
		c.traceManager.PropagateTrace(
			v.InvolvedObject.Kind, v.InvolvedObject.Namespace, v.InvolvedObject.Name,
			"Event", v.Namespace, v.Name,
		)

	case *appsv1.ReplicaSet:
		// ReplicaSets manage pods
		if v.Status.Replicas > 0 {
			event.Message = fmt.Sprintf("Managing %d replicas", v.Status.Replicas)
		}

	case *networkingv1.NetworkPolicy:
		// NetworkPolicy affects pods through selectors
		if v.Spec.PodSelector.MatchLabels != nil {
			event.RelatedObjects = append(event.RelatedObjects, ObjectReference{
				Kind:     "Pod",
				Relation: "applies-to",
			})
		}
	}
}

// createRawEvent converts ResourceEvent to RawEvent
func (c *Collector) createRawEvent(event *ResourceEvent, traceID, spanID string) collectors.RawEvent {
	data, _ := json.Marshal(event)

	return collectors.RawEvent{
		Timestamp: event.Timestamp,
		Type:      "k8s_" + event.EventType,
		Data:      data,
		Metadata: map[string]string{
			"collector":   "kubeapi",
			"resource":    event.Kind,
			"name":        event.Name,
			"namespace":   event.Namespace,
			"event_type":  event.EventType,
			"api_version": event.APIVersion,
		},
		TraceID: traceID,
		SpanID:  spanID,
	}
}

// createEvent creates a simple event (backward compatibility)
func (c *Collector) createEvent(eventType string, data interface{}, traceID, spanID string) collectors.RawEvent {
	jsonData, _ := json.Marshal(data)

	// Generate new span ID if not provided
	if spanID == "" {
		spanID = collectors.GenerateSpanID()
	}

	// Generate new trace ID if not provided
	if traceID == "" {
		traceID = collectors.GenerateTraceID()
	}

	return collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "kubeapi",
		Data:      jsonData,
		Metadata: map[string]string{
			"collector": c.Name(),
			"event":     eventType,
		},
		TraceID: traceID,
		SpanID:  spanID,
	}
}

// shouldIgnoreNamespace checks if namespace should be ignored
func (c *Collector) shouldIgnoreNamespace(namespace string) bool {
	for _, ignored := range c.config.IgnoreNamespaces {
		if namespace == ignored {
			return true
		}
	}

	// If WatchNamespaces is specified, only watch those
	if len(c.config.WatchNamespaces) > 0 {
		for _, watched := range c.config.WatchNamespaces {
			if namespace == watched {
				return false
			}
		}
		return true
	}

	return false
}

// getK8sConfig returns K8s client config
func getK8sConfig() (*rest.Config, error) {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		configOverrides := &clientcmd.ConfigOverrides{}
		kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
		config, err = kubeConfig.ClientConfig()
		if err != nil {
			return nil, err
		}
	}
	return config, nil
}

// getObjectMeta extracts metadata from K8s object
func getObjectMeta(obj interface{}) (metav1.Object, error) {
	return meta.Accessor(obj)
}

// getHostname returns current hostname
func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}
