package etcd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Collector implements minimal etcd monitoring
type Collector struct {
	name      string
	config    Config
	client    *clientv3.Client
	events    chan collectors.RawEvent
	ctx       context.Context
	cancel    context.CancelFunc
	healthy   bool
	mu        sync.RWMutex
	ebpfState interface{} // Platform-specific eBPF state
	stats     CollectorStats
	logger    *zap.Logger

	// OTEL instrumentation - REQUIRED fields
	tracer          trace.Tracer
	eventsProcessed metric.Int64Counter
	errorsTotal     metric.Int64Counter
	processingTime  metric.Float64Histogram
	watchOperations metric.Int64UpDownCounter
	apiLatency      metric.Float64Histogram
}

// CollectorStats tracks collector statistics
type CollectorStats struct {
	EventsCollected uint64
	EventsDropped   uint64
	ErrorCount      uint64
	LastEventTime   time.Time
}

// NewCollector creates a new minimal etcd collector
func NewCollector(name string, config Config) (*Collector, error) {
	if len(config.Endpoints) == 0 {
		config.Endpoints = []string{"localhost:2379"}
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
		events:          make(chan collectors.RawEvent, 10000), // Large buffer
		healthy:         true,
		logger:          logger,
		tracer:          tracer,
		eventsProcessed: eventsProcessed,
		errorsTotal:     errorsTotal,
		processingTime:  processingTime,
		watchOperations: watchOperations,
		apiLatency:      apiLatency,
	}, nil
}

// Name returns collector name
func (c *Collector) Name() string {
	return c.name
}

// Start begins collection
func (c *Collector) Start(ctx context.Context) error {
	// Create span for startup
	ctx, span := c.tracer.Start(ctx, "etcd.start")
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
		DialTimeout: 5 * time.Second,
	}

	if c.config.Username != "" {
		clientConfig.Username = c.config.Username
		clientConfig.Password = c.config.Password
	}

	// TLS configuration can be added via Config struct fields if needed in future

	span.SetAttributes(
		attribute.StringSlice("etcd.endpoints", c.config.Endpoints),
		attribute.Bool("etcd.auth_enabled", c.config.Username != ""),
		attribute.String("etcd.dial_timeout", "5s"),
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

	// Start eBPF monitoring if available
	if err := c.startEBPF(); err != nil {
		// Log but don't fail - eBPF is optional
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "ebpf_setup"),
			))
		}
		span.AddEvent("eBPF setup failed, continuing without eBPF monitoring",
			trace.WithAttributes(attribute.String("error", err.Error())))
		c.logger.Warn("eBPF monitoring setup failed", zap.Error(err))
	}

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
	c.logger.Info("etcd collector started",
		zap.String("name", c.name),
		zap.Strings("endpoints", c.config.Endpoints),
		zap.Duration("startup_duration", duration))

	return nil
}

// Stop gracefully shuts down
func (c *Collector) Stop() error {
	// Create span for shutdown
	ctx, span := c.tracer.Start(context.Background(), "etcd.stop")
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

	// Stop eBPF if running
	c.stopEBPF()
	span.AddEvent("eBPF monitoring stopped")

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

	c.logger.Info("etcd collector stopped",
		zap.String("name", c.name),
		zap.Duration("shutdown_duration", duration))

	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan collectors.RawEvent {
	return c.events
}

// IsHealthy returns health status
func (c *Collector) IsHealthy() bool {
	return c.healthy
}

// watchRegistry watches the K8s registry prefix in etcd
func (c *Collector) watchRegistry() {
	ctx, span := c.tracer.Start(c.ctx, "etcd.watch_registry")
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
		attribute.String("etcd.watch_prefix", "/registry/"),
		attribute.String("operation", "watch"),
	)

	// Watch K8s registry prefix
	watchChan := c.client.Watch(c.ctx, "/registry/", clientv3.WithPrefix())

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

// processEtcdEvent processes an etcd watch event
func (c *Collector) processEtcdEvent(ctx context.Context, event *clientv3.Event) {
	start := time.Now()
	ctx, span := c.tracer.Start(ctx, "etcd.process_event")
	defer span.End()

	// Extract resource type from key
	key := string(event.Kv.Key)
	resourceType := c.extractResourceType(key)

	// Determine operation type
	var operation string
	switch event.Type {
	case clientv3.EventTypePut:
		operation = "PUT"
	case clientv3.EventTypeDelete:
		operation = "DELETE"
	default:
		operation = "UNKNOWN"
	}

	span.SetAttributes(
		attribute.String("etcd.key", key),
		attribute.String("etcd.operation", operation),
		attribute.String("etcd.resource_type", resourceType),
		attribute.Int64("etcd.mod_revision", event.Kv.ModRevision),
		attribute.Int64("etcd.create_revision", event.Kv.CreateRevision),
		attribute.Int64("etcd.version", event.Kv.Version),
		attribute.Int("etcd.value_size", len(event.Kv.Value)),
	)

	// Create event data with raw etcd information
	eventData := map[string]interface{}{
		"key":             key,
		"value":           string(event.Kv.Value),
		"mod_revision":    event.Kv.ModRevision,
		"create_revision": event.Kv.CreateRevision,
		"version":         event.Kv.Version,
		"resource_type":   resourceType,
	}

	rawEvent := c.createEventWithContext(ctx, operation, eventData)
	rawEvent.Metadata["resource_type"] = resourceType
	rawEvent.Metadata["operation"] = operation

	// Add enhanced K8s metadata - STANDARD for all collectors
	k8sMetadata := c.extractK8sMetadata(key)
	for k, v := range k8sMetadata {
		rawEvent.Metadata[k] = v
	}

	// Send event
	select {
	case c.events <- rawEvent:
		c.mu.Lock()
		c.stats.EventsCollected++
		c.stats.LastEventTime = time.Now()
		c.mu.Unlock()

		// Record OTEL event metric
		if c.eventsProcessed != nil {
			c.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
				attribute.String("event_type", "etcd"),
				attribute.String("operation", operation),
				attribute.String("resource_type", resourceType),
			))
		}

		duration := time.Since(start)
		span.SetAttributes(
			attribute.Float64("processing_duration_seconds", duration.Seconds()),
		)

	case <-c.ctx.Done():
		span.AddEvent("Context cancelled while sending event")
		return
	default:
		// Buffer full, drop event
		c.mu.Lock()
		c.stats.EventsDropped++
		c.mu.Unlock()

		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "buffer_full"),
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

// extractK8sMetadata extracts full K8s metadata from etcd key
func (c *Collector) extractK8sMetadata(key string) map[string]string {
	metadata := make(map[string]string)

	// Expected formats:
	// /registry/{resource}/{namespace}/{name} (namespaced)
	// /registry/{resource}/{name} (cluster-scoped)
	parts := strings.Split(key, "/")

	if len(parts) < 3 || parts[1] != "registry" {
		return metadata
	}

	// Extract resource type/kind
	resourceType := parts[2]
	metadata["k8s_kind"] = c.normalizeResourceType(resourceType)

	// Check if namespaced or cluster-scoped
	if len(parts) == 5 {
		// Namespaced resource: /registry/{resource}/{namespace}/{name}
		metadata["k8s_namespace"] = parts[3]
		metadata["k8s_name"] = parts[4]
	} else if len(parts) == 4 {
		// Cluster-scoped resource: /registry/{resource}/{name}
		metadata["k8s_name"] = parts[3]
	}

	// Note: Additional K8s metadata would need to be extracted from the value:
	// - k8s_uid: Parse from the stored K8s object
	// - k8s_labels: Extract from metadata.labels in the K8s object
	// - k8s_owner_refs: Extract from metadata.ownerReferences
	// This would require deserializing the etcd value as a K8s object

	return metadata
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

// Helper to create an etcd raw event with trace context
func (c *Collector) createEventWithContext(ctx context.Context, eventType string, data interface{}) collectors.RawEvent {
	jsonData, err := json.Marshal(data)
	if err != nil {
		// If marshaling fails, create minimal event with error information
		jsonData = []byte(`{"error": "failed to marshal event data", "event_type": "` + eventType + `"}`)
		c.mu.Lock()
		c.stats.ErrorCount++
		c.mu.Unlock()
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "marshal"),
			))
		}
	}

	// Extract trace context from current span if available
	traceID, spanID := c.extractTraceContext(ctx)

	return collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "etcd",
		Data:      jsonData,
		Metadata: map[string]string{
			"collector": c.name,
			"event":     eventType,
		},
		TraceID: traceID,
		SpanID:  spanID,
	}
}

// Helper to create an etcd raw event (backward compatibility)
func (c *Collector) createEvent(eventType string, data interface{}) collectors.RawEvent {
	return c.createEventWithContext(context.Background(), eventType, data)
}

// extractTraceContext extracts trace and span IDs from context
func (c *Collector) extractTraceContext(ctx context.Context) (traceID, spanID string) {
	traceID = collectors.GenerateTraceID()
	spanID = collectors.GenerateSpanID()
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		traceID = span.SpanContext().TraceID().String()
		spanID = span.SpanContext().SpanID().String()
	}
	return traceID, spanID
}

// Health returns detailed health information
func (c *Collector) Health() (bool, map[string]interface{}) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	health := map[string]interface{}{
		"healthy":          c.healthy,
		"events_collected": c.stats.EventsCollected,
		"events_dropped":   c.stats.EventsDropped,
		"error_count":      c.stats.ErrorCount,
		"last_event":       c.stats.LastEventTime,
		"client_connected": c.client != nil,
		"ebpf_active":      c.ebpfState != nil,
		"instrumentation":  "etcd",
		"endpoints":        c.config.Endpoints,
		"buffer_size":      cap(c.events),
		"buffer_available": cap(c.events) - len(c.events),
	}

	return c.healthy, health
}

// Statistics returns collector statistics
func (c *Collector) Statistics() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := map[string]interface{}{
		"events_collected":    c.stats.EventsCollected,
		"events_dropped":      c.stats.EventsDropped,
		"error_count":         c.stats.ErrorCount,
		"last_event_time":     c.stats.LastEventTime,
		"collector_name":      c.name,
		"service_name":        "etcd",
		"config_endpoints":    c.config.Endpoints,
		"buffer_capacity":     cap(c.events),
		"buffer_current_size": len(c.events),
		"buffer_utilization":  float64(len(c.events)) / float64(cap(c.events)),
		"ebpf_enabled":        c.ebpfState != nil,
		"auth_enabled":        c.config.Username != "",
	}

	return stats
}
