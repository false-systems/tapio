package kubeapi

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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
	events chan *domain.CollectorEvent

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc

	// Informers
	informers []cache.SharedIndexInformer

	// Relationship cache for performance
	relationshipCache *RelationshipCache

	// OTEL instrumentation - 5 Core Metrics (MANDATORY)
	tracer          trace.Tracer
	eventsProcessed metric.Int64Counter
	errorsTotal     metric.Int64Counter
	processingTime  metric.Float64Histogram
	droppedEvents   metric.Int64Counter
	bufferUsage     metric.Int64Gauge

	// kubeapi-specific metrics (optional)
	watcherCount metric.Int64UpDownCounter
	cacheSyncs   metric.Int64Counter
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

	// Initialize OTEL components - MANDATORY pattern
	name := "kubeapi"
	tracer := otel.Tracer(name)
	meter := otel.Meter(name)

	// Create metrics with descriptive names and descriptions
	eventsProcessed, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription(fmt.Sprintf("Total events processed by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create events counter", zap.Error(err))
	}

	errorsTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", name),
		metric.WithDescription(fmt.Sprintf("Total errors in %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	processingTime, err := meter.Float64Histogram(
		fmt.Sprintf("%s_processing_duration_ms", name),
		metric.WithDescription(fmt.Sprintf("Processing duration for %s in milliseconds", name)),
	)
	if err != nil {
		logger.Warn("Failed to create processing time histogram", zap.Error(err))
	}

	droppedEvents, err := meter.Int64Counter(
		fmt.Sprintf("%s_dropped_events_total", name),
		metric.WithDescription(fmt.Sprintf("Total dropped events by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create dropped events counter", zap.Error(err))
	}

	bufferUsage, err := meter.Int64Gauge(
		fmt.Sprintf("%s_buffer_usage", name),
		metric.WithDescription(fmt.Sprintf("Current buffer usage for %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create buffer usage gauge", zap.Error(err))
	}

	watcherCount, err := meter.Int64UpDownCounter(
		fmt.Sprintf("%s_active_watchers", name),
		metric.WithDescription(fmt.Sprintf("Active watchers in %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create watcher count gauge", zap.Error(err))
	}

	cacheSyncs, err := meter.Int64Counter(
		fmt.Sprintf("%s_cache_syncs_total", name),
		metric.WithDescription(fmt.Sprintf("Total cache syncs in %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create cache syncs counter", zap.Error(err))
	}

	return &Collector{
		logger:            logger,
		config:            config,
		clientset:         clientset,
		traceManager:      NewTraceManager(),
		relationshipCache: NewRelationshipCache(10*time.Minute, 1000), // Cache for 10min, max 1000 entries
		events:            make(chan *domain.CollectorEvent, config.BufferSize),
		informers:         make([]cache.SharedIndexInformer, 0),
		tracer:            tracer,
		eventsProcessed:   eventsProcessed,
		errorsTotal:       errorsTotal,
		processingTime:    processingTime,
		droppedEvents:     droppedEvents,
		bufferUsage:       bufferUsage,
		watcherCount:      watcherCount,
		cacheSyncs:        cacheSyncs,
	}, nil
}

// NewCollector creates a minimal collector (backward compatibility)
func NewCollector(name string) (*Collector, error) {
	logger := zap.NewNop()
	config := DefaultConfig()

	// Initialize OTEL components even for minimal collector
	tracer := otel.Tracer(name)
	meter := otel.Meter(name)

	// Create metrics with graceful degradation for test scenarios
	eventsProcessed, _ := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription(fmt.Sprintf("Total events processed by %s", name)),
	)

	errorsTotal, _ := meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", name),
		metric.WithDescription(fmt.Sprintf("Total errors in %s", name)),
	)

	processingTime, _ := meter.Float64Histogram(
		fmt.Sprintf("%s_processing_duration_ms", name),
		metric.WithDescription(fmt.Sprintf("Processing duration for %s in milliseconds", name)),
	)

	droppedEvents, _ := meter.Int64Counter(
		fmt.Sprintf("%s_dropped_events_total", name),
		metric.WithDescription(fmt.Sprintf("Total dropped events by %s", name)),
	)

	bufferUsage, _ := meter.Int64Gauge(
		fmt.Sprintf("%s_buffer_usage", name),
		metric.WithDescription(fmt.Sprintf("Current buffer usage for %s", name)),
	)

	watcherCount, _ := meter.Int64UpDownCounter(
		fmt.Sprintf("%s_active_watchers", name),
		metric.WithDescription(fmt.Sprintf("Active watchers in %s", name)),
	)

	cacheSyncs, _ := meter.Int64Counter(
		fmt.Sprintf("%s_cache_syncs_total", name),
		metric.WithDescription(fmt.Sprintf("Total cache syncs in %s", name)),
	)

	// For tests, create a minimal collector without K8s connection
	return &Collector{
		logger:            logger,
		config:            config,
		traceManager:      NewTraceManager(),
		relationshipCache: NewRelationshipCache(10*time.Minute, 1000),
		events:            make(chan *domain.CollectorEvent, config.BufferSize),
		informers:         make([]cache.SharedIndexInformer, 0),
		tracer:            tracer,
		eventsProcessed:   eventsProcessed,
		errorsTotal:       errorsTotal,
		processingTime:    processingTime,
		droppedEvents:     droppedEvents,
		bufferUsage:       bufferUsage,
		watcherCount:      watcherCount,
		cacheSyncs:        cacheSyncs,
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
	// Create span for startup
	ctx, span := c.tracer.Start(ctx, "kubeapi.start")
	defer span.End()

	if c.ctx != nil {
		span.SetStatus(codes.Error, "collector already started")
		return fmt.Errorf("collector already started")
	}

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Set up watchers
	if err := c.setupWatchers(); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "watcher_setup_failed"),
			))
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("failed to setup watchers: %w", err)
	}

	// Start informers
	for _, informer := range c.informers {
		go informer.Run(c.ctx.Done())
	}

	// Update watcher count metric
	if c.watcherCount != nil {
		c.watcherCount.Add(ctx, int64(len(c.informers)), metric.WithAttributes(
			attribute.String("component", "kubeapi"),
		))
	}

	// Wait for initial sync
	c.logger.Info("Waiting for initial K8s cache sync...")
	for _, informer := range c.informers {
		if !cache.WaitForCacheSync(c.ctx.Done(), informer.HasSynced) {
			if c.errorsTotal != nil {
				c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
					attribute.String("error_type", "cache_sync_failed"),
				))
			}
			span.SetStatus(codes.Error, "failed to sync cache")
			return fmt.Errorf("failed to sync cache")
		}
		if c.cacheSyncs != nil {
			c.cacheSyncs.Add(ctx, 1)
		}
	}

	span.SetStatus(codes.Ok, "K8s API collector started successfully")
	span.SetAttributes(
		attribute.Int("watchers", len(c.informers)),
		attribute.StringSlice("namespaces", c.config.WatchNamespaces),
	)

	c.logger.Info("K8s API collector started",
		zap.Int("watchers", len(c.informers)),
		zap.Strings("namespaces", c.config.WatchNamespaces))

	// Legacy k8s_watcher.go functionality has been consolidated into setupWatchers

	return nil
}

// Stop gracefully shuts down the collector
func (c *Collector) Stop() error {
	if c.cancel != nil {
		c.cancel()
	}

	// All watchers are now stopped via lifecycle manager

	// Stop trace manager cleanup goroutine
	if c.traceManager != nil {
		c.traceManager.Stop()
	}

	// Stop relationship cache cleanup goroutine
	if c.relationshipCache != nil {
		c.relationshipCache.Stop()
	}

	close(c.events)
	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan *domain.CollectorEvent {
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
				return c.clientset.CoreV1().Pods("").List(c.ctx, listOptions)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return c.clientset.CoreV1().Pods("").Watch(c.ctx, listOptions)
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
				return c.clientset.AppsV1().Deployments("").List(c.ctx, listOptions)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return c.clientset.AppsV1().Deployments("").Watch(c.ctx, listOptions)
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
				return c.clientset.CoreV1().Services("").List(c.ctx, listOptions)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return c.clientset.CoreV1().Services("").Watch(c.ctx, listOptions)
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
				return c.clientset.CoreV1().Endpoints("").List(c.ctx, listOptions)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return c.clientset.CoreV1().Endpoints("").Watch(c.ctx, listOptions)
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
				return c.clientset.CoreV1().Events("").List(c.ctx, listOptions)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return c.clientset.CoreV1().Events("").Watch(c.ctx, listOptions)
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
				return c.clientset.AppsV1().ReplicaSets("").List(c.ctx, listOptions)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return c.clientset.AppsV1().ReplicaSets("").Watch(c.ctx, listOptions)
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
				return c.clientset.CoreV1().ConfigMaps("").List(c.ctx, listOptions)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return c.clientset.CoreV1().ConfigMaps("").Watch(c.ctx, listOptions)
			},
		},
		&corev1.ConfigMap{},
		c.config.ResyncPeriod,
		cache.Indexers{},
	)
	configMapInformer.AddEventHandler(c.resourceEventHandler("ConfigMap"))
	c.informers = append(c.informers, configMapInformer)

	// 8. Namespaces (important for multi-tenant context)
	namespaceInformer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return c.clientset.CoreV1().Namespaces().List(c.ctx, listOptions)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return c.clientset.CoreV1().Namespaces().Watch(c.ctx, listOptions)
			},
		},
		&corev1.Namespace{},
		c.config.ResyncPeriod,
		cache.Indexers{},
	)
	namespaceInformer.AddEventHandler(c.resourceEventHandler("Namespace"))
	c.informers = append(c.informers, namespaceInformer)

	// 9. Nodes (for infrastructure context)
	nodeInformer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return c.clientset.CoreV1().Nodes().List(c.ctx, listOptions)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return c.clientset.CoreV1().Nodes().Watch(c.ctx, listOptions)
			},
		},
		&corev1.Node{},
		c.config.ResyncPeriod,
		cache.Indexers{},
	)
	nodeInformer.AddEventHandler(c.resourceEventHandler("Node"))
	c.informers = append(c.informers, nodeInformer)

	// Optional watchers based on config
	if c.config.TrackRBAC {
		// ServiceAccounts
		saInformer := cache.NewSharedIndexInformer(
			&cache.ListWatch{
				ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
					return c.clientset.CoreV1().ServiceAccounts("").List(c.ctx, listOptions)
				},
				WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
					return c.clientset.CoreV1().ServiceAccounts("").Watch(c.ctx, listOptions)
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
					return c.clientset.NetworkingV1().NetworkPolicies("").List(c.ctx, listOptions)
				},
				WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
					return c.clientset.NetworkingV1().NetworkPolicies("").Watch(c.ctx, listOptions)
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
	// Create span for event handling
	ctx, span := c.tracer.Start(c.ctx, "kubeapi.handle_event")
	defer span.End()

	start := time.Now()

	// Extract metadata
	meta, err := getObjectMeta(obj)
	if err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "metadata_extraction_failed"),
				attribute.String("kind", kind),
			))
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		c.logger.Error("Failed to get object meta",
			zap.String("kind", kind),
			zap.Error(err))
		return
	}

	// Skip ignored namespaces
	if c.shouldIgnoreNamespace(meta.GetNamespace()) {
		span.SetAttributes(attribute.String("skipped", "ignored_namespace"))
		return
	}

	// Clean up trace for deleted objects
	if eventType == "DELETED" {
		c.traceManager.RemoveTrace(kind, meta.GetNamespace(), meta.GetName())
		// Also invalidate relationship cache for deleted object
		c.relationshipCache.Invalidate(string(meta.GetUID()))
		c.relationshipCache.InvalidateByTarget(string(meta.GetUID()))
	} else if eventType == "MODIFIED" {
		// Invalidate cache for modified objects to recompute relationships
		c.relationshipCache.Invalidate(string(meta.GetUID()))
	}

	// Set span attributes
	span.SetAttributes(
		attribute.String("component", "kubeapi"),
		attribute.String("operation", "handle_event"),
		attribute.String("event.type", eventType),
		attribute.String("event.kind", kind),
		attribute.String("event.name", meta.GetName()),
		attribute.String("event.namespace", meta.GetNamespace()),
	)

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
		// Record success metrics
		if c.eventsProcessed != nil {
			c.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
				attribute.String("event_type", eventType),
				attribute.String("resource_kind", kind),
			))
		}

		// Record processing time
		duration := time.Since(start).Seconds() * 1000 // Convert to milliseconds
		if c.processingTime != nil {
			c.processingTime.Record(ctx, duration, metric.WithAttributes(
				attribute.String("event_type", eventType),
				attribute.String("resource_kind", kind),
			))
		}

		span.SetStatus(codes.Ok, "")
	case <-c.ctx.Done():
		return
	default:
		if c.droppedEvents != nil {
			c.droppedEvents.Add(ctx, 1, metric.WithAttributes(
				attribute.String("reason", "buffer_full"),
				attribute.String("resource_kind", kind),
			))
		}
		span.SetAttributes(attribute.String("dropped", "buffer_full"))
		span.SetStatus(codes.Error, "event dropped - buffer full")
		c.logger.Warn("Event buffer full, dropping event",
			zap.String("kind", kind),
			zap.String("name", meta.GetName()))
	}
}

// extractRelationships finds related objects based on resource type
func (c *Collector) extractRelationships(event *ResourceEvent, obj interface{}) {
	switch v := obj.(type) {
	case *corev1.Pod:
		// Try to get cached pod volumes first
		if cachedVolumes, found := c.relationshipCache.GetPodVolumes(string(event.UID)); found {
			for _, vol := range cachedVolumes {
				event.RelatedObjects = append(event.RelatedObjects, ObjectReference{
					Kind:      vol.Kind,
					Name:      vol.Name,
					Namespace: vol.Namespace,
					UID:       types.UID(vol.UID),
					Relation:  vol.Relation,
				})
			}
		} else {
			// Compute and cache volume relationships
			var volumeRefs []ObjectRef
			for _, volume := range v.Spec.Volumes {
				if volume.ConfigMap != nil {
					ref := ObjectRef{
						Kind:      "ConfigMap",
						Name:      volume.ConfigMap.Name,
						Namespace: v.Namespace,
						UID:       "", // We don't have UID from volume spec
						Relation:  "mounts",
					}
					volumeRefs = append(volumeRefs, ref)
					event.RelatedObjects = append(event.RelatedObjects, ObjectReference{
						Kind:      ref.Kind,
						Name:      ref.Name,
						Namespace: ref.Namespace,
						Relation:  ref.Relation,
					})
				}
				if volume.Secret != nil {
					ref := ObjectRef{
						Kind:      "Secret",
						Name:      volume.Secret.SecretName,
						Namespace: v.Namespace,
						UID:       "",
						Relation:  "mounts",
					}
					volumeRefs = append(volumeRefs, ref)
					event.RelatedObjects = append(event.RelatedObjects, ObjectReference{
						Kind:      ref.Kind,
						Name:      ref.Name,
						Namespace: ref.Namespace,
						Relation:  ref.Relation,
					})
				}
				if volume.PersistentVolumeClaim != nil {
					ref := ObjectRef{
						Kind:      "PersistentVolumeClaim",
						Name:      volume.PersistentVolumeClaim.ClaimName,
						Namespace: v.Namespace,
						UID:       "",
						Relation:  "mounts",
					}
					volumeRefs = append(volumeRefs, ref)
					event.RelatedObjects = append(event.RelatedObjects, ObjectReference{
						Kind:      ref.Kind,
						Name:      ref.Name,
						Namespace: ref.Namespace,
						Relation:  ref.Relation,
					})
				}
			}

			// Cache the computed relationships
			c.relationshipCache.SetPodVolumes(string(event.UID), volumeRefs)
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

	case *corev1.Namespace:
		// Namespaces contain all namespaced resources
		event.Message = fmt.Sprintf("Namespace %s", event.EventType)
		// Could track resource quotas, limits, etc if needed

	case *corev1.Node:
		// Nodes host pods
		// Track node capacity and conditions
		if v.Status.Capacity != nil {
			cpu := v.Status.Capacity.Cpu().String()
			memory := v.Status.Capacity.Memory().String()
			event.Message = fmt.Sprintf("Node capacity: CPU=%s, Memory=%s", cpu, memory)
		}
		// Nodes have relationships with all pods scheduled on them
		event.RelatedObjects = append(event.RelatedObjects, ObjectReference{
			Kind:     "Pod",
			Relation: "hosts",
		})
	}
}

// createRawEvent converts ResourceEvent to CollectorEvent
func (c *Collector) createRawEvent(event *ResourceEvent, traceID, spanID string) *domain.CollectorEvent {
	// Generate event ID
	eventID := fmt.Sprintf("k8s-%s-%s-%s", event.Kind, event.EventType, string(event.UID))

	// Map resource kind to specific domain event type
	var eventType domain.CollectorEventType
	switch event.Kind {
	case "Pod":
		eventType = domain.EventTypeK8sPod
	case "Service":
		eventType = domain.EventTypeK8sService
	case "Deployment":
		eventType = domain.EventTypeK8sDeployment
	case "Event":
		eventType = domain.EventTypeK8sEvent
	default:
		// For other resources (ReplicaSet, ConfigMap, Node, etc.)
		// use the generic K8s event type
		eventType = domain.EventTypeK8sEvent
	}

	// Determine severity based on event type
	var severity domain.EventSeverity
	if event.EventType == "DELETED" {
		severity = domain.EventSeverityWarning
	} else {
		severity = domain.EventSeverityInfo
	}

	// Build K8s context - simplified for now
	k8sContext := &domain.K8sContext{
		Namespace: event.Namespace,
	}

	// Build metadata - use Pod fields only for Pod resources
	metadata := domain.EventMetadata{
		TraceID: traceID,
		SpanID:  spanID,
		Attributes: map[string]string{
			"collector":        "kubeapi",
			"event_type":       event.EventType,
			"api_version":      event.APIVersion,
			"k8s_kind":         event.Kind,
			"resource_version": event.ResourceVersion,
			"resource_name":    event.Name,
		},
	}

	// Set Pod-specific metadata only for Pod resources
	if event.Kind == "Pod" {
		metadata.PodName = event.Name
		metadata.PodNamespace = event.Namespace
		metadata.PodUID = string(event.UID)
	} else if event.Namespace != "" {
		// For other namespaced resources, track namespace
		metadata.Attributes["namespace"] = event.Namespace
	}

	// Add labels to attributes
	if len(event.Labels) > 0 {
		labelPairs := make([]string, 0, len(event.Labels))
		for k, v := range event.Labels {
			labelPairs = append(labelPairs, fmt.Sprintf("%s=%s", k, v))
		}
		metadata.Attributes["k8s_labels"] = strings.Join(labelPairs, ",")
	}

	return &domain.CollectorEvent{
		EventID:   eventID,
		Timestamp: event.Timestamp,
		Type:      eventType,
		Source:    "kubeapi",
		Severity:  severity,
		EventData: domain.EventDataContainer{
			KubernetesResource: &domain.K8sResourceData{
				APIVersion: event.APIVersion,
				Kind:       event.Kind,
				Name:       event.Name,
				Namespace:  event.Namespace,
				UID:        string(event.UID),
				Operation:  event.EventType,
			},
		},
		Metadata:   metadata,
		K8sContext: k8sContext,
	}
}

// Helper function to convert OwnerRef to domain OwnerReference
func convertOwnerRefs(refs []OwnerRef) []domain.OwnerReference {
	if len(refs) == 0 {
		return nil
	}

	domainRefs := make([]domain.OwnerReference, len(refs))
	for i, ref := range refs {
		domainRefs[i] = domain.OwnerReference{
			APIVersion: ref.APIVersion,
			Kind:       ref.Kind,
			Name:       ref.Name,
			UID:        string(ref.UID),
		}
	}
	return domainRefs
}

// createEvent creates a simple CollectorEvent
func (c *Collector) createEvent(eventType string, data interface{}, traceID, spanID string) *domain.CollectorEvent {
	// Generate new span ID if not provided
	if spanID == "" {
		spanID = collectors.GenerateSpanID()
	}

	// Generate new trace ID if not provided
	if traceID == "" {
		traceID = collectors.GenerateTraceID()
	}

	// Generate event ID
	eventID := fmt.Sprintf("kubeapi-%s-%d", eventType, time.Now().UnixNano())

	return &domain.CollectorEvent{
		EventID:   eventID,
		Timestamp: time.Now(),
		Type:      domain.EventTypeK8sEvent,
		Source:    "kubeapi",
		Severity:  domain.EventSeverityInfo,
		EventData: domain.EventDataContainer{
			KubernetesEvent: &domain.K8sAPIEventData{
				Action:    eventType,
				Type:      "Normal",
				Count:     1,
				FirstTime: time.Now(),
				LastTime:  time.Now(),
			},
		},
		Metadata: domain.EventMetadata{
			TraceID: traceID,
			SpanID:  spanID,
			Attributes: map[string]string{
				"collector":  "kubeapi",
				"event_type": eventType,
			},
		},
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
