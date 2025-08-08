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
	"github.com/yairfalse/tapio/pkg/integrations/telemetry"
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
type EBPFState struct {
	Objects interface{} // Generated eBPF objects
	Links   []interface{}
}

// Collector implements minimal CNI monitoring with comprehensive OTEL observability
type Collector struct {
	name      string
	events    chan collectors.RawEvent
	ctx       context.Context
	cancel    context.CancelFunc
	healthy   bool
	ebpfState *EBPFState
	mutex     sync.RWMutex // Protects concurrent access

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
	tracer := telemetry.GetTracer("cni-collector")
	meter := telemetry.GetMeter("cni-collector")

	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Create metrics
	eventsProcessed, err := meter.Int64Counter(
		"cni_events_processed_total",
		metric.WithDescription("Total number of CNI events processed"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create events_processed counter: %w", err)
	}

	eventsDropped, err := meter.Int64Counter(
		"cni_events_dropped_total",
		metric.WithDescription("Total number of CNI events dropped due to buffer full"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create events_dropped counter: %w", err)
	}

	ebpfLoadsTotal, err := meter.Int64Counter(
		"cni_ebpf_loads_total",
		metric.WithDescription("Total number of eBPF program load attempts"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create ebpf_loads_total counter: %w", err)
	}

	ebpfLoadErrors, err := meter.Int64Counter(
		"cni_ebpf_load_errors_total",
		metric.WithDescription("Total number of eBPF program load errors"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create ebpf_load_errors counter: %w", err)
	}

	ebpfAttachTotal, err := meter.Int64Counter(
		"cni_ebpf_attach_total",
		metric.WithDescription("Total number of eBPF program attach attempts"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create ebpf_attach_total counter: %w", err)
	}

	ebpfAttachErrors, err := meter.Int64Counter(
		"cni_ebpf_attach_errors_total",
		metric.WithDescription("Total number of eBPF program attach errors"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create ebpf_attach_errors counter: %w", err)
	}

	collectorHealth, err := meter.Int64Gauge(
		"cni_collector_healthy",
		metric.WithDescription("CNI collector health status (1=healthy, 0=unhealthy)"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create collector_healthy gauge: %w", err)
	}

	k8sExtractionTotal, err := meter.Int64Counter(
		"cni_k8s_extraction_attempts_total",
		metric.WithDescription("Total number of K8s metadata extraction attempts"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s_extraction_total counter: %w", err)
	}

	k8sExtractionHits, err := meter.Int64Counter(
		"cni_k8s_extraction_hits_total",
		metric.WithDescription("Total number of successful K8s metadata extractions"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s_extraction_hits counter: %w", err)
	}

	netnsOpsByType, err := meter.Int64Counter(
		"cni_netns_operations_total",
		metric.WithDescription("Total number of network namespace operations by type"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create netns_ops_by_type counter: %w", err)
	}

	bufferUtilization, err := meter.Float64Gauge(
		"cni_buffer_utilization_ratio",
		metric.WithDescription("Event buffer utilization ratio (0.0-1.0)"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create buffer_utilization gauge: %w", err)
	}

	processingLatency, err := meter.Float64Histogram(
		"cni_event_processing_duration_seconds",
		metric.WithDescription("Time spent processing CNI events"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create processing_latency histogram: %w", err)
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
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.healthy
}

// recordHealthStatus records the current health status to metrics
func (c *Collector) recordHealthStatus() {
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

// monitorBufferUtilization monitors and reports buffer utilization
func (c *Collector) monitorBufferUtilization() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			utilization := float64(len(c.events)) / float64(cap(c.events))
			c.bufferUtilization.Record(context.Background(), utilization,
				metric.WithAttributes(
					attribute.String("collector.name", c.name),
				),
			)
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
		duration := time.Since(start).Seconds()
		c.processingLatency.Record(ctx, duration,
			metric.WithAttributes(
				attribute.String("operation", "create_event"),
				attribute.String("event.type", eventType),
			),
		)
	}()

	jsonData, err := json.Marshal(data)
	if err != nil {
		// Create error event if marshaling fails
		errorData := map[string]string{
			"error": err.Error(),
			"type":  "marshal_error",
		}
		jsonData, _ = json.Marshal(errorData)
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
		c.k8sExtractionTotal.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("collector.name", c.name),
			),
		)
		if podInfo := c.parseK8sFromNetns(netnsPath); podInfo != nil {
			c.k8sExtractionHits.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("collector.name", c.name),
				),
			)
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
	c.netnsOpsByType.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("operation.type", eventType),
			attribute.String("collector.name", c.name),
		),
	)

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
