package etcd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain"
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
	events    chan *domain.CollectorEvent
	ctx       context.Context
	cancel    context.CancelFunc
	healthy   bool
	mu        sync.RWMutex
	ebpfState interface{} // Platform-specific eBPF state
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

	// etcd-specific metrics (optional)
	watchOperations metric.Int64UpDownCounter
	apiLatency      metric.Float64Histogram
}

// EtcdEventData represents strongly-typed etcd event data
type EtcdEventData struct {
	Key            string `json:"key"`
	Value          string `json:"value"`
	ModRevision    int64  `json:"mod_revision"`
	CreateRevision int64  `json:"create_revision"`
	Version        int64  `json:"version"`
	ResourceType   string `json:"resource_type"`
}

// CollectorStats represents strongly-typed collector statistics
type CollectorStats struct {
	EventsProcessed int64             `json:"events_processed"`
	ErrorCount      int64             `json:"error_count"`
	LastEventTime   time.Time         `json:"last_event_time"`
	Uptime          time.Duration     `json:"uptime"`
	CustomMetrics   map[string]string `json:"custom_metrics,omitempty"`
}

// HealthStatus represents strongly-typed health status
type HealthStatus struct {
	Healthy       bool              `json:"healthy"`
	Message       string            `json:"message"`
	LastCheck     time.Time         `json:"last_check"`
	ComponentInfo map[string]string `json:"component_info,omitempty"`
}

// EBPFEventData represents strongly-typed eBPF event data
type EBPFEventData struct {
	Timestamp uint64 `json:"timestamp"`
	PID       uint32 `json:"pid"`
	TID       uint32 `json:"tid"`
	Type      uint32 `json:"type"`
	DataLen   uint32 `json:"data_len"`
	SrcIP     string `json:"src_ip,omitempty"`
	DstIP     string `json:"dst_ip,omitempty"`
	SrcPort   uint16 `json:"src_port,omitempty"`
	DstPort   uint16 `json:"dst_port,omitempty"`
	RawData   []byte `json:"raw_data,omitempty"`
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
		events:          make(chan *domain.CollectorEvent, 10000), // Large buffer
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
func (c *Collector) Events() <-chan *domain.CollectorEvent {
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

	// Create strongly-typed event data
	eventData := EtcdEventData{
		Key:            key,
		Value:          string(event.Kv.Value),
		ModRevision:    event.Kv.ModRevision,
		CreateRevision: event.Kv.CreateRevision,
		Version:        event.Kv.Version,
		ResourceType:   resourceType,
	}

	// Add enhanced K8s metadata - STANDARD for all collectors
	k8sMetadata := c.extractK8sMetadata(key)

	// Event data will be used directly in the collector event creation

	collectorEvent := c.createETCDCollectorEvent(ctx, operation, &eventData, k8sMetadata)

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
		c.stats.ErrorCount++
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

// createETCDCollectorEvent creates a properly structured CollectorEvent from ETCD data
func (c *Collector) createETCDCollectorEvent(ctx context.Context, operation string, eventData *EtcdEventData, k8sMetadata map[string]string) *domain.CollectorEvent {
	eventID := fmt.Sprintf("etcd-%s-%d", operation, time.Now().UnixNano())

	// Determine event type based on ETCD operation and resource
	var eventType domain.CollectorEventType
	if eventData.ResourceType != "" && eventData.ResourceType != "unknown" {
		// This is a K8s resource operation
		switch operation {
		case "PUT":
			eventType = domain.EventTypeK8sEvent
		case "DELETE":
			eventType = domain.EventTypeK8sEvent
		default:
			eventType = domain.EventTypeETCDOperation
		}
	} else {
		// Generic ETCD operation
		eventType = domain.EventTypeETCDOperation
	}

	// Build ETCD data structure
	etcdData := &domain.ETCDData{
		Operation:    operation,
		Key:          eventData.Key,
		Value:        eventData.Value,
		Revision:     eventData.ModRevision,
		Duration:     0,   // Would need to track operation duration
		ResponseCode: 200, // Assume success, error handling would set differently
		ResourceType: eventData.ResourceType,
	}

	// Add K8s context if we have K8s metadata
	if resourceName, ok := k8sMetadata["k8s_name"]; ok {
		etcdData.ResourceName = resourceName
	}
	if namespace, ok := k8sMetadata["k8s_namespace"]; ok {
		etcdData.Namespace = namespace
	}

	// Build K8s resource data if this is a K8s resource
	var k8sResourceData *domain.K8sResourceData
	if eventData.ResourceType != "" && eventData.ResourceType != "unknown" {
		k8sResourceData = &domain.K8sResourceData{
			Kind: k8sMetadata["k8s_kind"],
			Name: k8sMetadata["k8s_name"],
			UID:  "", // Would need to parse from value
		}

		if namespace, ok := k8sMetadata["k8s_namespace"]; ok {
			k8sResourceData.Namespace = namespace
		}

		// Determine operation type
		switch operation {
		case "PUT":
			k8sResourceData.Operation = "update" // Could be create or update
		case "DELETE":
			k8sResourceData.Operation = "delete"
		default:
			k8sResourceData.Operation = strings.ToLower(operation)
		}
	}

	// Convert complete event data to JSON for raw storage
	completeEventData := struct {
		Operation   string            `json:"operation"`
		EventData   *EtcdEventData    `json:"event_data"`
		K8sMetadata map[string]string `json:"k8s_metadata"`
	}{
		Operation:   operation,
		EventData:   eventData,
		K8sMetadata: k8sMetadata,
	}
	rawDataBytes, _ := json.Marshal(completeEventData)

	collectorEvent := &domain.CollectorEvent{
		EventID:   eventID,
		Timestamp: time.Now(),
		Type:      eventType,
		Source:    c.name,
		Severity:  domain.EventSeverityInfo,

		EventData: domain.EventDataContainer{
			ETCD:        etcdData,
			K8sResource: k8sResourceData,
			RawData: &domain.RawData{
				Format:      "json",
				ContentType: "application/json",
				Data:        rawDataBytes,
				Size:        int64(len(rawDataBytes)),
			},
		},

		Metadata: domain.EventMetadata{
			Priority: domain.PriorityNormal,
			Tags:     []string{"etcd", "storage"},
			Labels: map[string]string{
				"operation":     operation,
				"resource_type": eventData.ResourceType,
			},
		},

		CorrelationHints: &domain.CorrelationHints{},
	}

	// Add K8s context if this is a K8s resource
	if k8sResourceData != nil {
		collectorEvent.K8sContext = &domain.K8sContext{
			Name: k8sResourceData.Name,
			Kind: k8sResourceData.Kind,
		}
		if k8sResourceData.Namespace != "" {
			collectorEvent.K8sContext.Namespace = k8sResourceData.Namespace
		}
	}

	return collectorEvent
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

// Health returns strongly-typed health information
func (c *Collector) Health() *HealthStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	message := "Collector running normally"
	if !c.healthy {
		message = "Collector is unhealthy"
	}

	// Build component info map
	componentInfo := make(map[string]string)
	componentInfo["client_connected"] = fmt.Sprintf("%t", c.client != nil)
	componentInfo["ebpf_active"] = fmt.Sprintf("%t", c.ebpfState != nil)
	componentInfo["instrumentation"] = "etcd"
	componentInfo["endpoints"] = strings.Join(c.config.Endpoints, ",")
	componentInfo["buffer_size"] = fmt.Sprintf("%d", cap(c.events))
	componentInfo["buffer_available"] = fmt.Sprintf("%d", cap(c.events)-len(c.events))
	componentInfo["events_processed"] = fmt.Sprintf("%d", c.stats.EventsProcessed)
	componentInfo["error_count"] = fmt.Sprintf("%d", c.stats.ErrorCount)

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
	customMetrics["service_name"] = "etcd"
	customMetrics["config_endpoints"] = strings.Join(c.config.Endpoints, ",")
	customMetrics["buffer_capacity"] = fmt.Sprintf("%d", cap(c.events))
	customMetrics["buffer_current_size"] = fmt.Sprintf("%d", len(c.events))
	customMetrics["buffer_utilization"] = fmt.Sprintf("%.2f", float64(len(c.events))/float64(cap(c.events)))
	customMetrics["ebpf_enabled"] = fmt.Sprintf("%t", c.ebpfState != nil)
	customMetrics["auth_enabled"] = fmt.Sprintf("%t", c.config.Username != "")

	return &CollectorStats{
		EventsProcessed: c.stats.EventsProcessed,
		ErrorCount:      c.stats.ErrorCount,
		LastEventTime:   c.stats.LastEventTime,
		Uptime:          time.Since(c.startTime),
		CustomMetrics:   customMetrics,
	}
}
