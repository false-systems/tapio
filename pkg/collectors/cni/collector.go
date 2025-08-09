package cni

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// PodInfo contains extracted K8s pod information
type PodInfo struct {
	Namespace string
	PodName   string
	PodUID    string
}

// EBPFState represents platform-specific eBPF state
// This is a minimal abstraction over the actual eBPF implementation
type EBPFState interface {
	// IsLoaded returns true if eBPF programs are loaded
	IsLoaded() bool
	// LinkCount returns the number of active eBPF links
	LinkCount() int
}

// Collector implements minimal CNI monitoring with comprehensive OTEL observability
type Collector struct {
	name      string
	config    *config.CNIConfig
	events    chan collectors.RawEvent
	ctx       context.Context
	cancel    context.CancelFunc
	healthy   bool
	ebpfState EBPFState
	mutex     sync.RWMutex // Protects concurrent access

	// Health tracking with atomic operations
	healthTracker *collectors.HealthTracker

	// OTEL instrumentation
	tracer             trace.Tracer
	meter              metric.Meter
	logger             *zap.Logger
	eventsProcessed    metric.Int64Counter
	eventsDropped      metric.Int64Counter
	ebpfLoadsTotal     metric.Int64Counter
	ebpfLoadErrors     metric.Int64Counter
	ebpfAttachTotal    metric.Int64Counter
	ebpfAttachErrors   metric.Int64Counter
	collectorHealth    metric.Int64Gauge
	k8sExtractionTotal metric.Int64Counter
	k8sExtractionHits  metric.Int64Counter
	netnsOpsByType     metric.Int64Counter
	bufferUtilization  metric.Float64Gauge
	processingLatency  metric.Float64Histogram
}

// NewCollector creates a new minimal CNI collector with OTEL observability
func NewCollector(name string) (*Collector, error) {
	// Initialize OTEL components
	tracer := otel.Tracer("cni-collector")
	meter := otel.Meter("cni-collector")

	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Create metrics with graceful degradation
	eventsProcessed, err := meter.Int64Counter(
		"cni_events_processed_total",
		metric.WithDescription("Total number of CNI events processed"),
	)
	if err != nil {
		logger.Warn("Failed to create events_processed counter", zap.Error(err))
		// Continue with nil metric - graceful degradation
	}

	eventsDropped, err := meter.Int64Counter(
		"cni_events_dropped_total",
		metric.WithDescription("Total number of CNI events dropped due to buffer full"),
	)
	if err != nil {
		logger.Warn("Failed to create events_dropped counter", zap.Error(err))
		// Continue with nil metric - graceful degradation
	}

	ebpfLoadsTotal, err := meter.Int64Counter(
		"cni_ebpf_loads_total",
		metric.WithDescription("Total number of eBPF program load attempts"),
	)
	if err != nil {
		logger.Warn("Failed to create ebpf_loads_total counter", zap.Error(err))
		// Continue with nil metric - graceful degradation
	}

	ebpfLoadErrors, err := meter.Int64Counter(
		"cni_ebpf_load_errors_total",
		metric.WithDescription("Total number of eBPF program load errors"),
	)
	if err != nil {
		logger.Warn("Failed to create ebpf_load_errors counter", zap.Error(err))
		// Continue with nil metric - graceful degradation
	}

	ebpfAttachTotal, err := meter.Int64Counter(
		"cni_ebpf_attach_total",
		metric.WithDescription("Total number of eBPF program attach attempts"),
	)
	if err != nil {
		logger.Warn("Failed to create ebpf_attach_total counter", zap.Error(err))
		// Continue with nil metric - graceful degradation
	}

	ebpfAttachErrors, err := meter.Int64Counter(
		"cni_ebpf_attach_errors_total",
		metric.WithDescription("Total number of eBPF program attach errors"),
	)
	if err != nil {
		logger.Warn("Failed to create ebpf_attach_errors counter", zap.Error(err))
		// Continue with nil metric - graceful degradation
	}

	collectorHealth, err := meter.Int64Gauge(
		"cni_collector_healthy",
		metric.WithDescription("CNI collector health status (1=healthy, 0=unhealthy)"),
	)
	if err != nil {
		logger.Warn("Failed to create collector_healthy gauge", zap.Error(err))
		// Continue with nil metric - graceful degradation
	}

	k8sExtractionTotal, err := meter.Int64Counter(
		"cni_k8s_extraction_attempts_total",
		metric.WithDescription("Total number of K8s metadata extraction attempts"),
	)
	if err != nil {
		logger.Warn("Failed to create k8s_extraction_total counter", zap.Error(err))
		// Continue with nil metric - graceful degradation
	}

	k8sExtractionHits, err := meter.Int64Counter(
		"cni_k8s_extraction_hits_total",
		metric.WithDescription("Total number of successful K8s metadata extractions"),
	)
	if err != nil {
		logger.Warn("Failed to create k8s_extraction_hits counter", zap.Error(err))
		// Continue with nil metric - graceful degradation
	}

	netnsOpsByType, err := meter.Int64Counter(
		"cni_netns_operations_total",
		metric.WithDescription("Total number of network namespace operations by type"),
	)
	if err != nil {
		logger.Warn("Failed to create netns_ops_by_type counter", zap.Error(err))
		// Continue with nil metric - graceful degradation
	}

	bufferUtilization, err := meter.Float64Gauge(
		"cni_buffer_utilization_ratio",
		metric.WithDescription("Event buffer utilization ratio (0.0-1.0)"),
	)
	if err != nil {
		logger.Warn("Failed to create buffer_utilization gauge", zap.Error(err))
		// Continue with nil metric - graceful degradation
	}

	processingLatency, err := meter.Float64Histogram(
		"cni_event_processing_duration_seconds",
		metric.WithDescription("Time spent processing CNI events"),
		metric.WithUnit("s"),
	)
	if err != nil {
		logger.Warn("Failed to create processing_latency histogram", zap.Error(err))
		// Continue with nil metric - graceful degradation
	}

	c := &Collector{
		name:               name,
		events:             make(chan collectors.RawEvent, 1000),
		healthy:            true,
		tracer:             tracer,
		meter:              meter,
		logger:             logger,
		eventsProcessed:    eventsProcessed,
		eventsDropped:      eventsDropped,
		ebpfLoadsTotal:     ebpfLoadsTotal,
		ebpfLoadErrors:     ebpfLoadErrors,
		ebpfAttachTotal:    ebpfAttachTotal,
		ebpfAttachErrors:   ebpfAttachErrors,
		collectorHealth:    collectorHealth,
		k8sExtractionTotal: k8sExtractionTotal,
		k8sExtractionHits:  k8sExtractionHits,
		netnsOpsByType:     netnsOpsByType,
		bufferUtilization:  bufferUtilization,
		processingLatency:  processingLatency,
	}

	// Record initial health status
	c.recordHealthStatus()

	return c, nil
}

// NewCollectorWithConfig creates a new CNI collector with type-safe configuration
func NewCollectorWithConfig(cfg *config.CNIConfig) (*Collector, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Initialize OTEL components
	tracer := otel.Tracer("cni-collector")
	meter := otel.Meter("cni-collector")

	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Create metrics with graceful degradation
	eventsProcessed, err := meter.Int64Counter(
		"cni_events_processed_total",
		metric.WithDescription("Total number of CNI events processed"),
	)
	if err != nil {
		logger.Warn("Failed to create events_processed counter", zap.Error(err))
	}

	eventsDropped, err := meter.Int64Counter(
		"cni_events_dropped_total",
		metric.WithDescription("Total number of CNI events dropped due to buffer full"),
	)
	if err != nil {
		logger.Warn("Failed to create events_dropped counter", zap.Error(err))
	}

	ebpfLoadsTotal, err := meter.Int64Counter(
		"cni_ebpf_loads_total",
		metric.WithDescription("Total number of eBPF program loads"),
	)
	if err != nil {
		logger.Warn("Failed to create ebpf_loads_total counter", zap.Error(err))
	}

	ebpfLoadErrors, err := meter.Int64Counter(
		"cni_ebpf_load_errors_total",
		metric.WithDescription("Total number of eBPF program load errors"),
	)
	if err != nil {
		logger.Warn("Failed to create ebpf_load_errors counter", zap.Error(err))
	}

	ebpfAttachTotal, err := meter.Int64Counter(
		"cni_ebpf_attach_total",
		metric.WithDescription("Total number of eBPF program attach attempts"),
	)
	if err != nil {
		logger.Warn("Failed to create ebpf_attach_total counter", zap.Error(err))
	}

	ebpfAttachErrors, err := meter.Int64Counter(
		"cni_ebpf_attach_errors_total",
		metric.WithDescription("Total number of eBPF program attach errors"),
	)
	if err != nil {
		logger.Warn("Failed to create ebpf_attach_errors counter", zap.Error(err))
	}

	collectorHealth, err := meter.Int64Gauge(
		"cni_collector_healthy",
		metric.WithDescription("CNI collector health status (1=healthy, 0=unhealthy)"),
	)
	if err != nil {
		logger.Warn("Failed to create collector_healthy gauge", zap.Error(err))
	}

	k8sExtractionTotal, err := meter.Int64Counter(
		"cni_k8s_extraction_attempts_total",
		metric.WithDescription("Total number of K8s metadata extraction attempts"),
	)
	if err != nil {
		logger.Warn("Failed to create k8s_extraction_total counter", zap.Error(err))
	}

	k8sExtractionHits, err := meter.Int64Counter(
		"cni_k8s_extraction_hits_total",
		metric.WithDescription("Total number of successful K8s metadata extractions"),
	)
	if err != nil {
		logger.Warn("Failed to create k8s_extraction_hits counter", zap.Error(err))
	}

	netnsOpsByType, err := meter.Int64Counter(
		"cni_netns_operations_by_type_total",
		metric.WithDescription("Total number of network namespace operations by type"),
	)
	if err != nil {
		logger.Warn("Failed to create netns_ops_by_type counter", zap.Error(err))
	}

	bufferUtilization, err := meter.Float64Gauge(
		"cni_buffer_utilization_percent",
		metric.WithDescription("Event buffer utilization as percentage"),
	)
	if err != nil {
		logger.Warn("Failed to create buffer_utilization gauge", zap.Error(err))
	}

	processingLatency, err := meter.Float64Histogram(
		"cni_processing_latency_seconds",
		metric.WithDescription("Event processing latency in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		logger.Warn("Failed to create processing_latency histogram", zap.Error(err))
	}

	// Create collector with configuration
	c := &Collector{
		name:               cfg.Name,
		config:             cfg,
		events:             make(chan collectors.RawEvent, cfg.BufferSize),
		healthy:            true,
		healthTracker:      collectors.NewHealthTracker(),
		tracer:             tracer,
		meter:              meter,
		logger:             logger,
		eventsProcessed:    eventsProcessed,
		eventsDropped:      eventsDropped,
		ebpfLoadsTotal:     ebpfLoadsTotal,
		ebpfLoadErrors:     ebpfLoadErrors,
		ebpfAttachTotal:    ebpfAttachTotal,
		ebpfAttachErrors:   ebpfAttachErrors,
		collectorHealth:    collectorHealth,
		k8sExtractionTotal: k8sExtractionTotal,
		k8sExtractionHits:  k8sExtractionHits,
		netnsOpsByType:     netnsOpsByType,
		bufferUtilization:  bufferUtilization,
		processingLatency:  processingLatency,
	}

	logger.Info("CNI collector created",
		zap.String("name", cfg.Name),
		zap.Int("buffer_size", cfg.BufferSize),
		zap.String("interface_prefix", cfg.InterfacePrefix),
		zap.Bool("enable_network_policies", cfg.EnableNetworkPolicies),
		zap.Bool("track_bandwidth", cfg.TrackBandwidth),
	)

	return c, nil
}

// Name returns collector name
func (c *Collector) Name() string {
	return c.name
}

// Start begins collection with OTEL tracing
func (c *Collector) Start(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("context cannot be nil")
	}

	ctx, span := c.tracer.Start(ctx, "cni.collector.start",
		trace.WithAttributes(
			attribute.String("collector.name", c.name),
		),
	)
	defer span.End()

	if c.ctx != nil {
		err := fmt.Errorf("collector already started")
		span.RecordError(err)
		return err
	}

	c.ctx, c.cancel = context.WithCancel(ctx)

	c.logger.Info("Starting CNI collector",
		zap.String("collector.name", c.name),
		zap.String("trace.id", span.SpanContext().TraceID().String()),
	)

	// Start eBPF monitoring if available
	if err := c.startEBPF(); err != nil {
		// Log but don't fail - eBPF is optional
		c.logger.Warn("Failed to start eBPF monitoring, continuing without it",
			zap.Error(err),
			zap.String("trace.id", span.SpanContext().TraceID().String()),
		)
		span.AddEvent("ebpf_start_failed", trace.WithAttributes(
			attribute.String("error", err.Error()),
		))
	}

	// Start buffer utilization monitoring
	go c.monitorBufferUtilization()

	span.SetAttributes(attribute.Bool("collector.started", true))
	c.logger.Info("CNI collector started successfully",
		zap.String("collector.name", c.name),
		zap.String("trace.id", span.SpanContext().TraceID().String()),
	)

	return nil
}

// Stop gracefully shuts down with OTEL tracing
func (c *Collector) Stop() error {
	_, span := c.tracer.Start(context.Background(), "cni.collector.stop",
		trace.WithAttributes(
			attribute.String("collector.name", c.name),
		),
	)
	defer span.End()

	c.logger.Info("Stopping CNI collector",
		zap.String("collector.name", c.name),
		zap.String("trace.id", span.SpanContext().TraceID().String()),
	)

	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}

	// Stop eBPF if running
	c.stopEBPF()

	// Close events channel
	if c.events != nil {
		close(c.events)
		c.events = nil
	}

	c.healthy = false
	c.recordHealthStatus()

	c.logger.Info("CNI collector stopped",
		zap.String("collector.name", c.name),
		zap.String("trace.id", span.SpanContext().TraceID().String()),
	)

	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan collectors.RawEvent {
	return c.events
}

// IsHealthy returns health status
func (c *Collector) IsHealthy() bool {
	if c.healthTracker != nil {
		return c.healthTracker.IsHealthy()
	}
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.healthy
}

// GetHealthStatus returns structured health information
func (c *Collector) GetHealthStatus() collectors.HealthStatus {
	if c.healthTracker != nil {
		// Create component health map
		components := map[string]bool{
			"ebpf_loaded": c.ebpfState != nil && c.ebpfState.IsLoaded(),
			"metrics":     c.eventsProcessed != nil,
			"tracer":      c.tracer != nil,
			"logger":      c.logger != nil,
		}

		// Calculate resource usage
		usage := collectors.ResourceUsage{
			BufferUtilization: c.calculateBufferUtilization(),
			GoroutineCount:    1, // Main processing goroutine
		}

		if c.ebpfState != nil {
			usage.FileDescriptorCount = c.ebpfState.LinkCount()
		}

		return c.healthTracker.GetHealthStatusWithComponents(components, usage)
	}

	// Fallback for legacy collectors
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return collectors.HealthStatus{
		Healthy:         c.healthy,
		EventsCollected: 0, // Would need to track this
		EventsDropped:   0, // Would need to track this
		ErrorCount:      0, // Would need to track this
	}
}

// calculateBufferUtilization calculates current buffer utilization percentage
func (c *Collector) calculateBufferUtilization() float64 {
	if c.events == nil {
		return 0.0
	}

	capacity := float64(cap(c.events))
	if capacity == 0 {
		return 0.0
	}

	current := float64(len(c.events))
	return (current / capacity) * 100.0
}

// recordHealthStatus records the current health status to metrics
func (c *Collector) recordHealthStatus() {
	if c.collectorHealth != nil {
		healthValue := int64(0)
		if c.healthy {
			healthValue = 1
		}
		c.collectorHealth.Record(context.Background(), healthValue,
			metric.WithAttributes(
				attribute.String("collector.name", c.name),
			),
		)
	}
}

// monitorBufferUtilization monitors and reports buffer utilization
func (c *Collector) monitorBufferUtilization() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if c.bufferUtilization != nil {
				utilization := float64(len(c.events)) / float64(cap(c.events))
				c.bufferUtilization.Record(context.Background(), utilization,
					metric.WithAttributes(
						attribute.String("collector.name", c.name),
					),
				)
			}
		}
	}
}

// createEvent creates a CNI raw event from structured data with OTEL tracing
func (c *Collector) createEvent(eventType string, data map[string]string) collectors.RawEvent {
	start := time.Now()
	ctx, span := c.tracer.Start(context.Background(), "cni.event.create",
		trace.WithAttributes(
			attribute.String("event.type", eventType),
			attribute.String("collector.name", c.name),
		),
	)
	defer span.End()
	defer func() {
		if c.processingLatency != nil {
			duration := time.Since(start).Seconds()
			c.processingLatency.Record(ctx, duration,
				metric.WithAttributes(
					attribute.String("operation", "create_event"),
					attribute.String("event.type", eventType),
				),
			)
		}
	}()

	jsonData, err := json.Marshal(data)
	if err != nil {
		// Create error event if marshaling fails
		errorData := map[string]string{
			"error": err.Error(),
			"type":  "marshal_error",
		}
		if errorJSON, marshalErr := json.Marshal(errorData); marshalErr != nil {
			// If we can't marshal error data, use a minimal fallback
			jsonData = []byte(`{"error":"marshal_failed","type":"marshal_error"}`)
		} else {
			jsonData = errorJSON
		}
		span.RecordError(err)
		c.logger.Error("Failed to marshal event data",
			zap.Error(err),
			zap.String("event.type", eventType),
			zap.String("trace.id", span.SpanContext().TraceID().String()),
		)
	}

	metadata := map[string]string{
		"collector": c.name,
		"event":     eventType,
	}

	// Extract K8s metadata from CNI data with metrics
	if netnsPath, ok := data["netns_path"]; ok {
		if c.k8sExtractionTotal != nil {
			c.k8sExtractionTotal.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("collector.name", c.name),
				),
			)
		}
		if podInfo := c.parseK8sFromNetns(netnsPath); podInfo != nil {
			if c.k8sExtractionHits != nil {
				c.k8sExtractionHits.Add(ctx, 1,
					metric.WithAttributes(
						attribute.String("collector.name", c.name),
					),
				)
			}
			metadata["k8s_kind"] = "Pod"
			metadata["k8s_uid"] = podInfo.PodUID
			// Only set if we have values
			if podInfo.Namespace != "" {
				metadata["k8s_namespace"] = podInfo.Namespace
			}
			if podInfo.PodName != "" {
				metadata["k8s_name"] = podInfo.PodName
			}
			span.SetAttributes(
				attribute.String("k8s.pod.uid", podInfo.PodUID),
				attribute.String("k8s.namespace.name", podInfo.Namespace),
				attribute.String("k8s.pod.name", podInfo.PodName),
			)
			// Note: Full pod details would need to be resolved via K8s API
			// or from a shared pod info cache maintained by other collectors
		}
	}

	// Record network namespace operation by type
	if c.netnsOpsByType != nil {
		c.netnsOpsByType.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("operation.type", eventType),
				attribute.String("collector.name", c.name),
			),
		)
	}

	// Generate event with trace context
	traceID := span.SpanContext().TraceID().String()
	spanID := span.SpanContext().SpanID().String()
	if traceID == "00000000000000000000000000000000" {
		traceID = collectors.GenerateTraceID()
	}
	if spanID == "0000000000000000" {
		spanID = collectors.GenerateSpanID()
	}

	return collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "cni",
		Data:      jsonData,
		Metadata:  metadata,
		TraceID:   traceID,
		SpanID:    spanID,
	}
}

// parseK8sFromNetns extracts K8s pod information from network namespace path
func (c *Collector) parseK8sFromNetns(netnsPath string) *PodInfo {
	// Common patterns for K8s network namespaces:
	// 1. /var/run/netns/cni-<uuid>
	// 2. /proc/<pid>/ns/net where pid belongs to a container
	// 3. May contain pod UID in the path

	// Try to extract pod UID from CNI netns naming
	cniPattern := regexp.MustCompile(`cni-([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})`)
	if matches := cniPattern.FindStringSubmatch(netnsPath); len(matches) > 1 {
		return &PodInfo{
			PodUID: matches[1],
			// Namespace and pod name would need to be resolved via K8s API
			// or from additional context (e.g., eBPF maps)
		}
	}

	// Try to extract from containerd/docker paths
	if strings.Contains(netnsPath, "kubepods") {
		// Extract pod UID from cgroup path pattern
		// /kubepods/besteffort/pod<UID>/...
		podPattern := regexp.MustCompile(`pod([0-9a-f]{8}_[0-9a-f]{4}_[0-9a-f]{4}_[0-9a-f]{4}_[0-9a-f]{12})`)
		if matches := podPattern.FindStringSubmatch(netnsPath); len(matches) > 1 {
			// Convert underscore format to hyphen format
			podUID := strings.ReplaceAll(matches[1], "_", "-")
			return &PodInfo{
				PodUID: podUID,
			}
		}
	}

	// If we can't parse pod info, return nil
	return nil
}
