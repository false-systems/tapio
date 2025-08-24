package etcdapi

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Collector implements etcd API monitoring focused on K8s resources
type Collector struct {
	name      string
	config    Config
	client    *clientv3.Client
	events    chan *domain.CollectorEvent
	ctx       context.Context
	cancel    context.CancelFunc
	healthy   bool
	mu        sync.RWMutex
	stats     CollectorStats
	startTime time.Time
	logger    *zap.Logger

	// OTEL instrumentation - 5 Core Metrics (MANDATORY)
	tracer          trace.Tracer
	eventsProcessed metric.Int64Counter
	errorsTotal     metric.Int64Counter
	processingTime  metric.Float64Histogram
	droppedEvents   metric.Int64Counter
	bufferUsage     metric.Int64Gauge

	// etcd API-specific metrics
	watchOperations metric.Int64UpDownCounter
	apiLatency      metric.Float64Histogram
}

// NewCollector creates a new etcd API collector focused on K8s resource monitoring
func NewCollector(name string, config Config) (*Collector, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Create logger if not provided
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Initialize OTEL components - MANDATORY pattern
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

	watchOperations, err := meter.Int64UpDownCounter(
		fmt.Sprintf("%s_active_watches", name),
		metric.WithDescription(fmt.Sprintf("Active watch operations in %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create watch operations gauge", zap.Error(err))
	}

	apiLatency, err := meter.Float64Histogram(
		fmt.Sprintf("%s_api_latency_ms", name),
		metric.WithDescription(fmt.Sprintf("API call latency for %s in milliseconds", name)),
	)
	if err != nil {
		logger.Warn("Failed to create API latency histogram", zap.Error(err))
	}

	return &Collector{
		name:            name,
		config:          config,
		events:          make(chan *domain.CollectorEvent, config.BufferSize),
		healthy:         true,
		startTime:       time.Now(),
		logger:          logger,
		tracer:          tracer,
		eventsProcessed: eventsProcessed,
		errorsTotal:     errorsTotal,
		processingTime:  processingTime,
		droppedEvents:   droppedEvents,
		bufferUsage:     bufferUsage,
		watchOperations: watchOperations,
		apiLatency:      apiLatency,
	}, nil
}

// Name returns collector name
func (c *Collector) Name() string {
	return c.name
}

// Start begins API-based etcd monitoring
func (c *Collector) Start(ctx context.Context) error {
	// Create span for startup
	ctx, span := c.tracer.Start(ctx, "etcd-api.start")
	defer span.End()

	start := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ctx != nil {
		err := fmt.Errorf("collector already started")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Create etcd client
	clientConfig := clientv3.Config{
		Endpoints:   c.config.Endpoints,
		DialTimeout: time.Duration(c.config.DialTimeout) * time.Second,
	}

	if c.config.Username != "" {
		clientConfig.Username = c.config.Username
		clientConfig.Password = c.config.Password
	}

	if c.config.TLS != nil {
		tlsConfig, err := c.buildTLSConfig()
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to build TLS config")
			return fmt.Errorf("failed to build TLS config: %w", err)
		}
		clientConfig.TLS = tlsConfig
	}

	span.SetAttributes(
		attribute.StringSlice("etcd.endpoints", c.config.Endpoints),
		attribute.Bool("etcd.auth_enabled", c.config.Username != ""),
		attribute.Bool("etcd.tls_enabled", c.config.TLS != nil),
		attribute.String("etcd.watch_prefix", c.config.WatchPrefix),
		attribute.String("etcd.dial_timeout", fmt.Sprintf("%ds", c.config.DialTimeout)),
	)

	client, err := clientv3.New(clientConfig)
	if err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "client_creation"),
			))
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to create etcd client")
		return fmt.Errorf("failed to create etcd client: %w", err)
	}
	c.client = client

	// Test connection
	connStart := time.Now()
	ctxTimeout, cancel := context.WithTimeout(c.ctx, 3*time.Second)
	status, err := c.client.Status(ctxTimeout, c.config.Endpoints[0])
	cancel()

	// Record API latency for connection test
	if c.apiLatency != nil {
		c.apiLatency.Record(ctx, time.Since(connStart).Seconds()*1000, metric.WithAttributes(
			attribute.String("endpoint", "/status"),
			attribute.String("operation", "connection_test"),
		))
	}

	if err != nil {
		c.client.Close()
		c.client = nil
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "connectivity"),
			))
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to connect to etcd")
		return fmt.Errorf("failed to connect to etcd: %w", err)
	}

	// Connection successful - record cluster info
	span.SetAttributes(
		attribute.String("etcd.cluster_id", fmt.Sprintf("%d", status.Header.ClusterId)),
		attribute.String("etcd.member_id", fmt.Sprintf("%d", status.Header.MemberId)),
		attribute.Int64("etcd.revision", status.Header.Revision),
		attribute.String("etcd.version", status.Version),
	)

	// Start watching K8s registry
	go c.watchRegistry()

	// Record startup duration
	duration := time.Since(start)
	if c.processingTime != nil {
		c.processingTime.Record(ctx, duration.Seconds()*1000, metric.WithAttributes(
			attribute.String("operation", "startup"),
		))
	}

	span.SetAttributes(
		attribute.Float64("startup_duration_seconds", duration.Seconds()),
	)

	c.healthy = true
	c.logger.Info("etcd API collector started",
		zap.String("name", c.name),
		zap.Strings("endpoints", c.config.Endpoints),
		zap.String("watch_prefix", c.config.WatchPrefix),
		zap.Duration("startup_duration", duration))

	return nil
}

// Stop gracefully shuts down the API collector
func (c *Collector) Stop() error {
	// Create span for shutdown
	ctx, span := c.tracer.Start(context.Background(), "etcd-api.stop")
	defer span.End()

	start := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()

	span.AddEvent("Starting collector shutdown")

	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
		span.AddEvent("Context cancelled")
	}

	// Close etcd client
	if c.client != nil {
		c.client.Close()
		c.client = nil
		span.AddEvent("etcd client closed")
	}

	// Close events channel
	if c.events != nil {
		close(c.events)
		c.events = nil
		span.AddEvent("Events channel closed")
	}

	c.healthy = false

	// Record shutdown duration
	duration := time.Since(start)
	if c.processingTime != nil {
		c.processingTime.Record(ctx, duration.Seconds()*1000, metric.WithAttributes(
			attribute.String("operation", "shutdown"),
		))
	}

	span.SetAttributes(
		attribute.Float64("shutdown_duration_ms", duration.Seconds()*1000),
	)

	c.logger.Info("etcd API collector stopped",
		zap.String("name", c.name),
		zap.Duration("shutdown_duration", duration))

	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan *domain.CollectorEvent {
	return c.events
}

// IsHealthy returns health status
func (c *Collector) IsHealthy() bool {
	return c.healthy
}

// watchRegistry watches the configured prefix in etcd (defaults to K8s registry)
func (c *Collector) watchRegistry() {
	ctx, span := c.tracer.Start(c.ctx, "etcd-api.watch_registry")
	defer span.End()

	// Track active watch operation
	if c.watchOperations != nil {
		c.watchOperations.Add(ctx, 1, metric.WithAttributes(
			attribute.String("operation", "watch_registry"),
		))
		defer c.watchOperations.Add(ctx, -1, metric.WithAttributes(
			attribute.String("operation", "watch_registry"),
		))
	}

	span.SetAttributes(
		attribute.String("etcd.watch_prefix", c.config.WatchPrefix),
		attribute.String("operation", "watch"),
	)

	// Watch configured prefix
	watchChan := c.client.Watch(c.ctx, c.config.WatchPrefix, clientv3.WithPrefix())

	for {
		select {
		case <-c.ctx.Done():
			span.AddEvent("Context cancelled, stopping watch")
			return
		case watchResp, ok := <-watchChan:
			if !ok {
				span.AddEvent("Watch channel closed")
				return
			}

			if watchResp.Err() != nil {
				c.mu.Lock()
				c.stats.ErrorCount++
				c.mu.Unlock()
				if c.errorsTotal != nil {
					c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
						attribute.String("error_type", "watch_response"),
					))
				}
				span.RecordError(watchResp.Err())
				span.AddEvent("Watch response error", trace.WithAttributes(
					attribute.String("error", watchResp.Err().Error()),
				))
				continue
			}

			// Process events with instrumentation
			eventCount := len(watchResp.Events)
			span.AddEvent("Received watch events", trace.WithAttributes(
				attribute.Int("event_count", eventCount),
				attribute.Int64("revision", watchResp.Header.Revision),
			))

			for _, event := range watchResp.Events {
				c.processEtcdEvent(ctx, event)
			}
		}
	}
}

// processEtcdEvent processes an etcd watch event and creates a CollectorEvent with rich context
func (c *Collector) processEtcdEvent(ctx context.Context, event *clientv3.Event) {
	start := time.Now()
	ctx, span := c.tracer.Start(ctx, "etcd-api.process_event")
	defer span.End()

	defer func() {
		// Record processing time
		duration := time.Since(start).Seconds() * 1000 // Convert to milliseconds
		if c.processingTime != nil {
			c.processingTime.Record(ctx, duration, metric.WithAttributes(
				attribute.String("operation", "process_event"),
			))
		}
	}()

	// Extract comprehensive context from etcd event
	key := string(event.Kv.Key)
	value := string(event.Kv.Value)

	// Determine operation type
	var operation string
	switch event.Type {
	case clientv3.EventTypePut:
		operation = "put"
	case clientv3.EventTypeDelete:
		operation = "delete"
	default:
		operation = "unknown"
	}

	span.SetAttributes(
		attribute.String("etcd.key", key),
		attribute.String("etcd.operation", operation),
		attribute.Int64("etcd.mod_revision", event.Kv.ModRevision),
		attribute.Int64("etcd.create_revision", event.Kv.CreateRevision),
		attribute.Int64("etcd.version", event.Kv.Version),
		attribute.Int("etcd.value_size", len(event.Kv.Value)),
	)

	// Create CollectorEvent with full context
	collectorEvent := c.buildCollectorEvent(ctx, key, value, operation, event.Kv)

	// Set span attributes for the created event
	span.SetAttributes(
		attribute.String("collector_event.id", collectorEvent.EventID),
		attribute.String("collector_event.type", string(collectorEvent.Type)),
		attribute.String("collector_event.correlation_key", collectorEvent.GetCorrelationKey()),
	)

	// Send event
	select {
	case c.events <- collectorEvent:
		c.mu.Lock()
		c.stats.EventsProcessed++
		c.stats.LastEventTime = time.Now()
		c.mu.Unlock()

		// Record OTEL event metric
		if c.eventsProcessed != nil {
			c.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
				attribute.String("event_type", string(collectorEvent.Type)),
				attribute.String("operation", operation),
				attribute.String("k8s_kind", collectorEvent.K8sContext.Kind),
				attribute.String("k8s_namespace", collectorEvent.K8sContext.Namespace),
				attribute.String("status", "success"),
			))
		}

		span.SetAttributes(
			attribute.Float64("processing_duration_seconds", time.Since(start).Seconds()),
		)

	case <-c.ctx.Done():
		span.AddEvent("Context cancelled while sending event")
		return
	default:
		// Buffer full, drop event
		c.mu.Lock()
		c.stats.ErrorCount++
		c.mu.Unlock()

		if c.droppedEvents != nil {
			c.droppedEvents.Add(ctx, 1, metric.WithAttributes(
				attribute.String("reason", "buffer_full"),
				attribute.String("event_type", string(collectorEvent.Type)),
			))
		}
		span.AddEvent("Event dropped - buffer full", trace.WithAttributes(
			attribute.String("key", key),
			attribute.String("operation", operation),
		))
	}
}

// extractResourceType extracts the K8s resource type from etcd key
func (c *Collector) extractResourceType(key string) string {
	// Expected format: /registry/{resource}/{namespace}/{name}
	// or /registry/{resource}/{name} for cluster-scoped resources
	parts := strings.Split(key, "/")
	if len(parts) >= 3 && parts[1] == "registry" {
		return parts[2]
	}
	return "unknown"
}

// extractK8sContext extracts comprehensive K8s context from etcd key and value
func (c *Collector) extractK8sContext(key, value string) *domain.K8sContext {
	// Parse the etcd key to extract basic resource information
	// Expected formats:
	// /registry/{resource}/{namespace}/{name} (namespaced)
	// /registry/{resource}/{name} (cluster-scoped)
	parts := strings.Split(key, "/")

	if len(parts) < 3 || parts[1] != "registry" {
		return &domain.K8sContext{}
	}

	resourceType := parts[2]
	kind := c.normalizeResourceType(resourceType)

	k8sContext := &domain.K8sContext{
		Kind: kind,
	}

	// Extract namespace and name from key
	if len(parts) == 5 {
		// Namespaced resource: /registry/{resource}/{namespace}/{name}
		k8sContext.Namespace = parts[3]
		k8sContext.Name = parts[4]
	} else if len(parts) == 4 {
		// Cluster-scoped resource: /registry/{resource}/{name}
		k8sContext.Name = parts[3]
	}

	// If we have value data, parse additional context from the K8s object
	if value != "" {
		c.enrichK8sContextFromValue(k8sContext, value)
	}

	return k8sContext
}

// enrichK8sContextFromValue extracts additional context from the K8s object JSON
func (c *Collector) enrichK8sContextFromValue(k8sContext *domain.K8sContext, value string) {
	var obj K8sObject
	if err := json.Unmarshal([]byte(value), &obj); err != nil {
		// If parsing fails, we'll work with what we have from the key
		return
	}

	// Extract metadata
	if obj.Metadata.UID != "" {
		k8sContext.UID = obj.Metadata.UID
	}
	if obj.APIVersion != "" {
		k8sContext.APIVersion = obj.APIVersion
	}
	if obj.Metadata.ResourceVersion != "" {
		k8sContext.ResourceVersion = obj.Metadata.ResourceVersion
	}
	if obj.Metadata.Generation != 0 {
		k8sContext.Generation = obj.Metadata.Generation
	}

	// Extract labels
	if len(obj.Metadata.Labels) > 0 {
		k8sContext.Labels = obj.Metadata.Labels
	}

	// Extract annotations
	if len(obj.Metadata.Annotations) > 0 {
		k8sContext.Annotations = obj.Metadata.Annotations
	}

	// Extract owner references
	if len(obj.Metadata.OwnerReferences) > 0 {
		k8sContext.OwnerReferences = make([]domain.OwnerReference, 0, len(obj.Metadata.OwnerReferences))
		for _, ref := range obj.Metadata.OwnerReferences {
			ownerRef := domain.OwnerReference{
				APIVersion: ref.APIVersion,
				Kind:       ref.Kind,
				Name:       ref.Name,
				UID:        ref.UID,
				Controller: ref.Controller,
			}
			k8sContext.OwnerReferences = append(k8sContext.OwnerReferences, ownerRef)
		}
	}

	// Extract spec information for workload context
	c.extractWorkloadContext(k8sContext, &obj.Spec)

	// Extract status information
	c.extractStatusContext(k8sContext, &obj.Status)
}

// extractWorkloadContext extracts workload-specific context from spec
func (c *Collector) extractWorkloadContext(k8sContext *domain.K8sContext, spec *K8sSpec) {
	// Extract selectors
	if spec.Selector != nil && len(spec.Selector.MatchLabels) > 0 {
		k8sContext.Selectors = spec.Selector.MatchLabels
	}

	// Extract node placement information
	if spec.NodeName != "" {
		k8sContext.NodeName = spec.NodeName
	}

	// Extract replica information for controllers
	if spec.Replicas != nil {
		k8sContext.WorkloadKind = k8sContext.Kind
		k8sContext.WorkloadName = k8sContext.Name
	}
}

// extractStatusContext extracts status information
func (c *Collector) extractStatusContext(k8sContext *domain.K8sContext, status *K8sStatus) {
	// Extract phase
	if status.Phase != "" {
		k8sContext.Phase = status.Phase
	}

	// Extract conditions
	if len(status.Conditions) > 0 {
		k8sContext.Conditions = make([]domain.ConditionSnapshot, 0, len(status.Conditions))
		for _, cond := range status.Conditions {
			condition := domain.ConditionSnapshot{
				Type:    cond.Type,
				Status:  cond.Status,
				Reason:  cond.Reason,
				Message: cond.Message,
			}
			k8sContext.Conditions = append(k8sContext.Conditions, condition)
		}
	}
}

// normalizeResourceType converts etcd resource types to K8s kinds
func (c *Collector) normalizeResourceType(resourceType string) string {
	// Map common etcd resource types to K8s kinds
	switch resourceType {
	case "pods":
		return "Pod"
	case "services":
		return "Service"
	case "deployments":
		return "Deployment"
	case "replicasets":
		return "ReplicaSet"
	case "configmaps":
		return "ConfigMap"
	case "secrets":
		return "Secret"
	case "namespaces":
		return "Namespace"
	case "nodes":
		return "Node"
	case "persistentvolumes":
		return "PersistentVolume"
	case "persistentvolumeclaims":
		return "PersistentVolumeClaim"
	case "statefulsets":
		return "StatefulSet"
	case "daemonsets":
		return "DaemonSet"
	case "jobs":
		return "Job"
	case "cronjobs":
		return "CronJob"
	case "ingresses":
		return "Ingress"
	case "endpoints":
		return "Endpoints"
	case "events":
		return "Event"
	default:
		// Capitalize first letter as convention
		if len(resourceType) > 0 {
			return strings.ToUpper(resourceType[:1]) + resourceType[1:]
		}
		return resourceType
	}
}

// buildTLSConfig creates TLS configuration from config
func (c *Collector) buildTLSConfig() (*tls.Config, error) {
	if c.config.TLS == nil {
		return nil, nil
	}

	tlsConfig := &tls.Config{}

	// Load client certificate if provided
	if c.config.TLS.CertFile != "" && c.config.TLS.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(c.config.TLS.CertFile, c.config.TLS.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	// Load CA certificate if provided
	if c.config.TLS.CAFile != "" {
		caCert, err := os.ReadFile(c.config.TLS.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caCert)
		tlsConfig.RootCAs = caCertPool
	}

	return tlsConfig, nil
}

// buildCollectorEvent creates a comprehensive CollectorEvent with full context extraction
func (c *Collector) buildCollectorEvent(ctx context.Context, key, value, operation string, kv *mvccpb.KeyValue) *domain.CollectorEvent {
	timestamp := time.Now()
	eventID := c.generateEventID(key, kv.ModRevision)

	// Parse K8s context from etcd key and value
	k8sContext := c.extractK8sContext(key, value)

	// Determine event type based on resource kind
	eventType := c.mapK8sKindToEventType(k8sContext.Kind)

	// Create ETCD data structure
	etcdData := &domain.ETCDData{
		Operation:    operation,
		Key:          key,
		Value:        value,
		Revision:     kv.ModRevision,
		Duration:     0,   // API calls don't have duration measurement here
		ResponseCode: 200, // Successful watch events
		LeaseID:      0,   // Not available in watch events
	}

	// Extract K8s resource data if this is a K8s object
	var k8sResourceData *domain.K8sResourceData
	if k8sContext.Kind != "" && value != "" && operation != "delete" {
		k8sResourceData = c.parseK8sResource(value, k8sContext)
	}

	// Build correlation hints for efficient correlation
	correlationHints := c.buildCorrelationHints(k8sContext, k8sResourceData)

	// Extract trace context from current span
	traceContext := c.extractTraceContextFromSpan(ctx)

	// Build causality context for K8s object relationships
	causalityContext := c.buildCausalityContext(k8sContext, k8sResourceData)

	// Create collection context
	collectionContext := c.buildCollectionContext()

	return &domain.CollectorEvent{
		EventID:   eventID,
		Timestamp: timestamp,
		Type:      eventType,
		Source:    c.name,

		EventData: domain.EventDataContainer{
			ETCD:               etcdData,
			KubernetesResource: k8sResourceData,
		},

		Metadata: domain.EventMetadata{
			Priority:      c.determineEventPriority(k8sContext, operation),
			Tags:          c.buildEventTags(k8sContext, operation),
			Labels:        c.buildEventLabels(k8sContext),
			Attributes:    c.buildEventAttributes(kv),
			SchemaVersion: "1.0",
		},

		CorrelationHints:  &correlationHints,
		K8sContext:        k8sContext,
		TraceContext:      traceContext,
		CausalityContext:  causalityContext,
		CollectionContext: &collectionContext,
	}
}

// generateEventID creates a unique event ID from key and revision
func (c *Collector) generateEventID(key string, revision int64) string {
	return fmt.Sprintf("etcd-api-%s-%d", hashString(key), revision)
}

// mapK8sKindToEventType maps K8s resource kinds to CollectorEventTypes
func (c *Collector) mapK8sKindToEventType(kind string) domain.CollectorEventType {
	switch kind {
	case "Pod":
		return domain.EventTypeK8sPod
	case "Service":
		return domain.EventTypeK8sService
	case "Deployment":
		return domain.EventTypeK8sDeployment
	case "ConfigMap":
		return domain.EventTypeK8sConfigMap
	case "Secret":
		return domain.EventTypeK8sSecret
	default:
		return domain.EventTypeETCD
	}
}

// parseK8sResource creates K8sResourceData from the K8s object JSON
func (c *Collector) parseK8sResource(value string, k8sContext *domain.K8sContext) *domain.K8sResourceData {
	return &domain.K8sResourceData{
		APIVersion:        k8sContext.APIVersion,
		Kind:              k8sContext.Kind,
		Name:              k8sContext.Name,
		Namespace:         k8sContext.Namespace,
		UID:               k8sContext.UID,
		ResourceVersion:   k8sContext.ResourceVersion,
		Generation:        k8sContext.Generation,
		Labels:            k8sContext.Labels,
		Annotations:       k8sContext.Annotations,
		OwnerReferences:   k8sContext.OwnerReferences,
		DeletionTimestamp: nil, // Would need to parse from metadata.deletionTimestamp
	}
}

// buildCorrelationHints creates correlation hints for efficient event correlation
func (c *Collector) buildCorrelationHints(k8sContext *domain.K8sContext, k8sResourceData *domain.K8sResourceData) domain.CorrelationHints {
	hints := domain.CorrelationHints{}

	// Pod-level correlation
	if k8sContext.UID != "" {
		if k8sContext.Kind == "Pod" {
			hints.PodUID = k8sContext.UID
		}
	}

	// Node-level correlation
	if k8sContext.NodeName != "" {
		hints.NodeName = k8sContext.NodeName
	}

	// Build correlation tags for advanced correlation
	hints.CorrelationTags = make(map[string]string)
	if k8sContext.Namespace != "" {
		hints.CorrelationTags["k8s_namespace"] = k8sContext.Namespace
	}
	if k8sContext.Kind != "" {
		hints.CorrelationTags["k8s_kind"] = k8sContext.Kind
	}
	if k8sContext.WorkloadName != "" {
		hints.CorrelationTags["workload_name"] = k8sContext.WorkloadName
		hints.CorrelationTags["workload_kind"] = k8sContext.WorkloadKind
	}

	// Add labels as correlation tags for advanced matching
	for k, v := range k8sContext.Labels {
		hints.CorrelationTags[fmt.Sprintf("label_%s", k)] = v
	}

	return hints
}

// extractTraceContextFromSpan extracts trace context from the current span
func (c *Collector) extractTraceContextFromSpan(ctx context.Context) *domain.TraceContext {
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return nil
	}

	spanContext := span.SpanContext()
	return &domain.TraceContext{
		TraceID:    spanContext.TraceID(),
		SpanID:     spanContext.SpanID(),
		TraceFlags: spanContext.TraceFlags(),
		TraceState: spanContext.TraceState(),
	}
}

// buildCausalityContext creates causality context for K8s object relationships
func (c *Collector) buildCausalityContext(k8sContext *domain.K8sContext, k8sResourceData *domain.K8sResourceData) *domain.CausalityContext {
	causalityContext := &domain.CausalityContext{
		Confidence: 0.8, // High confidence for etcd events
		Type:       "k8s_resource_change",
	}

	// If this resource has an owner, establish causality
	if len(k8sContext.OwnerReferences) > 0 {
		for _, owner := range k8sContext.OwnerReferences {
			if owner.Controller != nil && *owner.Controller {
				causalityContext.CauseID = fmt.Sprintf("k8s-%s-%s", owner.Kind, owner.UID)
				causalityContext.RootCause = causalityContext.CauseID
				causalityContext.ChainDepth = 1
				break
			}
		}
	}

	return causalityContext
}

// buildCollectionContext creates collection context with current collector state
func (c *Collector) buildCollectionContext() domain.CollectionContext {
	hostname := getHostname()

	return domain.CollectionContext{
		CollectorVersion: "1.0.0", // Should be from build info
		HostInfo: domain.HostInfo{
			Hostname:      hostname,
			KernelVersion: getKernelVersion(),
			OSVersion:     getOSVersion(),
			Architecture:  getArchitecture(),
		},
		CollectionConfig: domain.CollectionConfig{
			SamplingRate:    1.0, // etcd-api doesn't sample
			BufferSize:      c.config.BufferSize,
			FlushInterval:   time.Duration(c.config.DialTimeout) * time.Second,
			EnabledFeatures: []string{"k8s_context_extraction", "causality_detection"},
		},
		BufferStats: domain.BufferStats{
			TotalCapacity:   int64(cap(c.events)),
			CurrentUsage:    int64(len(c.events)),
			UtilizationRate: float64(len(c.events)) / float64(cap(c.events)),
			ProcessedEvents: int64(c.stats.EventsProcessed),
		},
	}
}

// determineEventPriority assigns priority based on K8s resource type and operation
func (c *Collector) determineEventPriority(k8sContext *domain.K8sContext, operation string) domain.EventPriority {
	// Critical operations get high priority
	if operation == "delete" {
		return domain.PriorityHigh
	}

	// Critical resource types
	switch k8sContext.Kind {
	case "Node", "Namespace":
		return domain.PriorityHigh
	case "Pod", "Service", "Deployment":
		return domain.PriorityNormal
	case "ConfigMap", "Secret":
		return domain.PriorityNormal
	default:
		return domain.PriorityLow
	}
}

// buildEventTags creates event tags for categorization
func (c *Collector) buildEventTags(k8sContext *domain.K8sContext, operation string) []string {
	tags := []string{"etcd", "k8s", operation}

	if k8sContext.Kind != "" {
		tags = append(tags, strings.ToLower(k8sContext.Kind))
	}

	if k8sContext.Namespace != "" {
		tags = append(tags, "namespaced")
	} else {
		tags = append(tags, "cluster-scoped")
	}

	return tags
}

// buildEventLabels creates event labels from K8s context
func (c *Collector) buildEventLabels(k8sContext *domain.K8sContext) map[string]string {
	labels := make(map[string]string)

	if k8sContext.Kind != "" {
		labels["k8s.kind"] = k8sContext.Kind
	}
	if k8sContext.Namespace != "" {
		labels["k8s.namespace"] = k8sContext.Namespace
	}
	if k8sContext.Name != "" {
		labels["k8s.name"] = k8sContext.Name
	}
	if k8sContext.NodeName != "" {
		labels["k8s.node"] = k8sContext.NodeName
	}

	return labels
}

// buildEventAttributes creates event attributes from etcd key-value data
func (c *Collector) buildEventAttributes(kv *mvccpb.KeyValue) map[string]string {
	return map[string]string{
		"etcd.mod_revision":    fmt.Sprintf("%d", kv.ModRevision),
		"etcd.create_revision": fmt.Sprintf("%d", kv.CreateRevision),
		"etcd.version":         fmt.Sprintf("%d", kv.Version),
		"etcd.value_size":      fmt.Sprintf("%d", len(kv.Value)),
	}
}

// Health returns strongly-typed health information
func (c *Collector) Health() *HealthStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	message := "API collector running normally"
	if !c.healthy {
		message = "API collector is unhealthy"
	}

	// Build component info map
	componentInfo := make(map[string]string)
	componentInfo["client_connected"] = fmt.Sprintf("%t", c.client != nil)
	componentInfo["instrumentation"] = "etcd-api"
	componentInfo["endpoints"] = strings.Join(c.config.Endpoints, ",")
	componentInfo["watch_prefix"] = c.config.WatchPrefix
	componentInfo["buffer_size"] = fmt.Sprintf("%d", cap(c.events))
	componentInfo["buffer_available"] = fmt.Sprintf("%d", cap(c.events)-len(c.events))
	componentInfo["events_processed"] = fmt.Sprintf("%d", c.stats.EventsProcessed)
	componentInfo["error_count"] = fmt.Sprintf("%d", c.stats.ErrorCount)
	componentInfo["tls_enabled"] = fmt.Sprintf("%t", c.config.TLS != nil)

	return &HealthStatus{
		Healthy:       c.healthy,
		Message:       message,
		LastCheck:     time.Now(),
		ComponentInfo: componentInfo,
	}
}

// Statistics returns strongly-typed collector statistics
func (c *Collector) Statistics() *CollectorStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Build custom metrics map
	customMetrics := make(map[string]string)
	customMetrics["collector_name"] = c.name
	customMetrics["service_name"] = "etcd-api"
	customMetrics["config_endpoints"] = strings.Join(c.config.Endpoints, ",")
	customMetrics["watch_prefix"] = c.config.WatchPrefix
	customMetrics["buffer_capacity"] = fmt.Sprintf("%d", cap(c.events))
	customMetrics["buffer_current_size"] = fmt.Sprintf("%d", len(c.events))
	customMetrics["buffer_utilization"] = fmt.Sprintf("%.2f", float64(len(c.events))/float64(cap(c.events)))
	customMetrics["auth_enabled"] = fmt.Sprintf("%t", c.config.Username != "")
	customMetrics["tls_enabled"] = fmt.Sprintf("%t", c.config.TLS != nil)

	return &CollectorStats{
		EventsProcessed: c.stats.EventsProcessed,
		ErrorCount:      c.stats.ErrorCount,
		LastEventTime:   c.stats.LastEventTime,
		Uptime:          time.Since(c.startTime),
		CustomMetrics:   customMetrics,
	}
}
