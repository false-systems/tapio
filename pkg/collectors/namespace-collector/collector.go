//go:build linux

package namespace_collector

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/config"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Collector implements simple namespace monitoring via eBPF
type Collector struct {
	name    string
	config  *config.NamespaceConfig
	events  chan *domain.CollectorEvent
	ctx     context.Context
	cancel  context.CancelFunc
	healthy bool
	mutex   sync.RWMutex

	// eBPF components (platform-specific)
	ebpfState interface{}

	// OTEL instrumentation
	tracer trace.Tracer
	meter  metric.Meter
	logger *zap.Logger

	// 5 Core Metrics (MANDATORY - same for all collectors)
	eventsProcessed metric.Int64Counter
	errorsTotal     metric.Int64Counter
	processingTime  metric.Float64Histogram
	droppedEvents   metric.Int64Counter
	bufferUsage     metric.Int64Gauge

	// Namespace-specific metrics (optional)
	ebpfLoadsTotal     metric.Int64Counter
	ebpfLoadErrors     metric.Int64Counter
	ebpfAttachTotal    metric.Int64Counter
	ebpfAttachErrors   metric.Int64Counter
	collectorHealth    metric.Float64Gauge
	k8sExtractionTotal metric.Int64Counter
	k8sExtractionHits  metric.Int64Counter
	netnsOpsByType     metric.Int64Counter
}

// NewCollector creates a new simple namespace collector
func NewCollector(name string) (*Collector, error) {
	// Initialize OTEL components
	tracer := otel.Tracer("namespace-collector")
	meter := otel.Meter("namespace-collector")

	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Initialize all metrics
	eventsProcessed, err := meter.Int64Counter(
		"namespace_events_processed_total",
		metric.WithDescription(fmt.Sprintf("Total events processed by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create events_processed counter", zap.Error(err))
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

	ebpfLoadsTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_ebpf_loads_total", name),
		metric.WithDescription(fmt.Sprintf("Total eBPF loads attempted by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create ebpf_loads counter", zap.Error(err))
	}

	ebpfLoadErrors, err := meter.Int64Counter(
		fmt.Sprintf("%s_ebpf_load_errors_total", name),
		metric.WithDescription(fmt.Sprintf("Total eBPF load errors in %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create ebpf_load_errors counter", zap.Error(err))
	}

	ebpfAttachTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_ebpf_attach_total", name),
		metric.WithDescription(fmt.Sprintf("Total eBPF attach attempts by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create ebpf_attach counter", zap.Error(err))
	}

	ebpfAttachErrors, err := meter.Int64Counter(
		fmt.Sprintf("%s_ebpf_attach_errors_total", name),
		metric.WithDescription(fmt.Sprintf("Total eBPF attach errors in %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create ebpf_attach_errors counter", zap.Error(err))
	}

	collectorHealth, err := meter.Float64Gauge(
		"namespace_collector_healthy",
		metric.WithDescription(fmt.Sprintf("Health status of %s collector", name)),
	)
	if err != nil {
		logger.Warn("Failed to create health gauge", zap.Error(err))
	}

	k8sExtractionTotal, err := meter.Int64Counter(
		"namespace_k8s_extraction_attempts_total",
		metric.WithDescription(fmt.Sprintf("Total K8s metadata extraction attempts by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create k8s_extraction counter", zap.Error(err))
	}

	k8sExtractionHits, err := meter.Int64Counter(
		fmt.Sprintf("%s_k8s_extraction_hits_total", name),
		metric.WithDescription(fmt.Sprintf("Total K8s metadata extraction hits by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create k8s_extraction_hits counter", zap.Error(err))
	}

	netnsOpsByType, err := meter.Int64Counter(
		"namespace_netns_operations_total",
		metric.WithDescription(fmt.Sprintf("Total network namespace operations by type in %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create netns_ops counter", zap.Error(err))
	}

	// Default config
	cfg := &config.NamespaceConfig{
		BaseConfig: &config.BaseConfig{
			Name:       name,
			BufferSize: 10000,
		},
		EnableEBPF: true,
	}

	c := &Collector{
		name:    name,
		config:  cfg,
		events:  make(chan *domain.CollectorEvent, cfg.BaseConfig.BufferSize),
		healthy: true,
		tracer:  tracer,
		meter:   meter,
		logger:  logger.Named(name),
		// Core metrics
		eventsProcessed: eventsProcessed,
		errorsTotal:     errorsTotal,
		processingTime:  processingTime,
		droppedEvents:   droppedEvents,
		bufferUsage:     bufferUsage,
		// Namespace-specific metrics
		ebpfLoadsTotal:     ebpfLoadsTotal,
		ebpfLoadErrors:     ebpfLoadErrors,
		ebpfAttachTotal:    ebpfAttachTotal,
		ebpfAttachErrors:   ebpfAttachErrors,
		collectorHealth:    collectorHealth,
		k8sExtractionTotal: k8sExtractionTotal,
		k8sExtractionHits:  k8sExtractionHits,
		netnsOpsByType:     netnsOpsByType,
	}

	c.logger.Info("CNI collector created", zap.String("name", name))
	return c, nil
}

// Name returns collector name
func (c *Collector) Name() string {
	return c.name
}

// Start starts the CNI monitoring
func (c *Collector) Start(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("context cannot be nil")
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Check if already started
	if c.ctx != nil {
		return fmt.Errorf("collector already started")
	}

	ctx, span := c.tracer.Start(ctx, "cni.collector.start")
	defer span.End()

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Start eBPF monitoring if enabled
	if c.config.EnableEBPF {
		if err := c.startEBPF(); err != nil {
			if c.errorsTotal != nil {
				c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
					attribute.String("error_type", "ebpf_start_failed"),
				))
			}
			span.RecordError(err)
			c.ctx = nil
			c.cancel = nil
			return fmt.Errorf("failed to start eBPF: %w", err)
		}

		// Start event processing
		go c.readEBPFEvents()
	}

	c.healthy = true
	c.logger.Info("CNI collector started",
		zap.String("name", c.name),
		zap.Bool("ebpf_enabled", c.config.EnableEBPF),
	)
	return nil
}

// Stop stops the collector
func (c *Collector) Stop() error {
	// Add tracing for stop operation
	_, span := c.tracer.Start(context.Background(), "cni.collector.stop")
	defer span.End()

	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Check if already stopped
	if c.ctx == nil {
		return nil // Already stopped, no error
	}

	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}

	// Stop eBPF if running
	c.stopEBPF()

	// Close events channel only once
	if c.events != nil {
		close(c.events)
		c.events = nil
	}

	c.ctx = nil
	c.healthy = false
	c.logger.Info("CNI collector stopped")
	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan *domain.CollectorEvent {
	return c.events
}

// IsHealthy returns health status
func (c *Collector) IsHealthy() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.healthy
}

// createEvent creates a domain.CollectorEvent from CNI event data
func (c *Collector) createEvent(eventType string, data map[string]string) *domain.CollectorEvent {
	// Generate trace context
	ctx := context.Background()
	ctx, span := c.tracer.Start(ctx, "cni.event.create")
	defer span.End()

	spanCtx := span.SpanContext()
	eventID := fmt.Sprintf("runtime-%s-%d", eventType, time.Now().UnixNano())

	// Parse namespace path to extract k8s metadata
	var podInfo *PodInfo
	if netnsPath, ok := data["netns_path"]; ok {
		podInfo = c.parseK8sFromNetns(netnsPath)
	}

	// Build network data from the namespace event
	networkData := &domain.NetworkData{
		Protocol:  "tcp", // Default, could be extracted from data if available
		Direction: "outbound",
	}

	// Build container data if we have container info
	var containerData *domain.ContainerData
	if containerID, ok := data["container_id"]; ok {
		containerData = &domain.ContainerData{
			ContainerID: containerID,
			Runtime:     "containerd", // Default, could be detected
			State:       "running",
		}
	}

	// Build process data if available
	var processData *domain.ProcessData
	if pidStr, ok := data["pid"]; ok {
		if pid, err := strconv.ParseInt(pidStr, 10, 32); err == nil {
			processData = &domain.ProcessData{
				PID: int32(pid),
			}
			if comm, ok := data["comm"]; ok {
				processData.Command = comm
			}
		}
	}

	// Create CollectorEvent with proper type
	var collectorEventType domain.CollectorEventType
	switch eventType {
	case "process_exec", "process_exit":
		collectorEventType = domain.EventTypeKernelProcess
	case "signal_sent", "signal_received":
		collectorEventType = domain.EventTypeKernelProcess
	case "oom_kill":
		collectorEventType = domain.EventTypeContainerOOM
	case "namespace_create":
		collectorEventType = domain.EventTypeContainerCreate
	case "namespace_delete":
		collectorEventType = domain.EventTypeContainerDestroy
	default:
		collectorEventType = domain.EventTypeCNI
	}

	// Convert data to JSON for raw storage
	dataBytes, _ := json.Marshal(data)

	collectorEvent := &domain.CollectorEvent{
		EventID:   eventID,
		Timestamp: time.Now(),
		Type:      collectorEventType,
		Source:    c.name,
		Severity:  domain.EventSeverityInfo,

		EventData: domain.EventDataContainer{
			Network:   networkData,
			Container: containerData,
			Process:   processData,
			RawData: &domain.RawData{
				Format:      "json",
				ContentType: "application/json",
				Data:        dataBytes,
				Size:        int64(len(dataBytes)),
			},
		},

		Metadata: domain.EventMetadata{
			Priority: domain.PriorityNormal,
			Tags:     []string{"runtime", "signals"},
			Labels: map[string]string{
				"event_type": eventType,
			},
		},

		TraceContext: &domain.TraceContext{
			TraceID: spanCtx.TraceID(),
			SpanID:  spanCtx.SpanID(),
		},
	}

	// Add K8s context if we have pod info
	if podInfo != nil {
		collectorEvent.K8sContext = &domain.K8sContext{
			UID: podInfo.PodUID,
		}

		if podInfo.PodName != "" {
			collectorEvent.K8sContext.Name = podInfo.PodName
		}
		if podInfo.Namespace != "" {
			collectorEvent.K8sContext.Namespace = podInfo.Namespace
		}

		collectorEvent.CorrelationHints = &domain.CorrelationHints{
			PodUID: podInfo.PodUID,
		}
	}

	// Add process correlation if available
	if processData != nil {
		if collectorEvent.CorrelationHints == nil {
			collectorEvent.CorrelationHints = &domain.CorrelationHints{}
		}
		collectorEvent.CorrelationHints.ProcessID = processData.PID
		collectorEvent.Metadata.PID = processData.PID
		collectorEvent.Metadata.Command = processData.Command
	}

	return collectorEvent
}

// parseK8sUIDFromNetns extracts Kubernetes UID from network namespace path
func parseK8sUIDFromNetns(path string) string {
	// Pattern: /var/run/netns/cni-<uid>
	// Extract the UID part after "cni-"
	parts := strings.Split(path, "cni-")
	if len(parts) == 2 {
		return strings.TrimSpace(parts[1])
	}
	return ""
}

// parseK8sFromNetns extracts Kubernetes pod information from network namespace path
func (c *Collector) parseK8sFromNetns(netnsPath string) *PodInfo {
	if netnsPath == "" {
		return nil
	}

	// Count extraction attempts
	if c.k8sExtractionTotal != nil {
		c.k8sExtractionTotal.Add(context.Background(), 1)
	}

	// Pattern 1: CNI format - /var/run/netns/cni-<uuid>
	if strings.Contains(netnsPath, "cni-") {
		parts := strings.Split(netnsPath, "cni-")
		if len(parts) == 2 {
			uid := strings.TrimSpace(parts[1])
			if uid != "" {
				if c.k8sExtractionHits != nil {
					c.k8sExtractionHits.Add(context.Background(), 1)
				}
				return &PodInfo{
					PodUID: uid,
				}
			}
		}
	}

	// Pattern 2: Kubepods cgroup format - contains kubepods and pod<uuid_with_underscores>
	if strings.Contains(netnsPath, "kubepods") && strings.Contains(netnsPath, "pod") {
		// Look for pod<uuid> pattern
		parts := strings.Split(netnsPath, "pod")
		for _, part := range parts {
			if len(part) > 32 { // UUID length with underscores
				// Extract potential UUID part (first 36+ chars)
				potential := strings.Split(part, "/")[0]
				if len(potential) >= 36 { // UUID with underscores is at least 36 chars (32 hex + 4 underscores)
					// Take exactly 36 characters and convert underscores to hyphens for standard UUID format
					uid := strings.ReplaceAll(potential[:36], "_", "-")
					if c.k8sExtractionHits != nil {
						c.k8sExtractionHits.Add(context.Background(), 1)
					}
					return &PodInfo{
						PodUID: uid,
					}
				}
			}
		}
	}

	return nil
}
