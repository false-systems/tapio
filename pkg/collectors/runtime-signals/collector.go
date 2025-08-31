package runtimesignals

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/base"
	"github.com/yairfalse/tapio/pkg/collectors/config"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// Collector implements simple namespace monitoring via eBPF
type Collector struct {
	*base.BaseCollector       // Embed for Statistics() and Health()
	*base.EventChannelManager // Embed for event channel management
	*base.LifecycleManager    // Embed for lifecycle management

	config *config.NamespaceConfig
	logger *zap.Logger

	// eBPF components (platform-specific)
	ebpfState interface{}

	// Additional OTEL metrics (beyond base)
	ebpfLoadsTotal     metric.Int64Counter
	ebpfLoadErrors     metric.Int64Counter
	ebpfAttachTotal    metric.Int64Counter
	ebpfAttachErrors   metric.Int64Counter
	k8sExtractionTotal metric.Int64Counter
	k8sExtractionHits  metric.Int64Counter
	netnsOpsByType     metric.Int64Counter
}

// NewCollector creates a new simple namespace collector
func NewCollector(name string) (*Collector, error) {
	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Initialize base components
	ctx := context.Background()
	baseCollector := base.NewBaseCollector("runtime_signals", 5*time.Minute)
	eventManager := base.NewEventChannelManager(1000, "runtime_signals", logger)
	lifecycleManager := base.NewLifecycleManager(ctx, logger)

	// Get meter from base for additional metrics
	meter := baseCollector.GetMeter()

	// Initialize namespace-specific metrics
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

	k8sExtractionTotal, err := meter.Int64Counter(
		"runtime_signals_k8s_extraction_attempts_total",
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
		"runtime_signals_process_operations_total",
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
		BaseCollector:       baseCollector,
		EventChannelManager: eventManager,
		LifecycleManager:    lifecycleManager,
		config:              cfg,
		logger:              logger.Named(name),
		// Namespace-specific metrics
		ebpfLoadsTotal:      ebpfLoadsTotal,
		ebpfLoadErrors:      ebpfLoadErrors,
		ebpfAttachTotal:     ebpfAttachTotal,
		ebpfAttachErrors:    ebpfAttachErrors,
		k8sExtractionTotal:  k8sExtractionTotal,
		k8sExtractionHits:   k8sExtractionHits,
		netnsOpsByType:      netnsOpsByType,
	}

	c.logger.Info("Runtime signals collector created", zap.String("name", name))
	return c, nil
}

// Name returns collector name
func (c *Collector) Name() string {
	return c.BaseCollector.GetName()
}

// Start starts the CNI monitoring
func (c *Collector) Start(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("context cannot be nil")
	}

	// Check if already started
	if c.LifecycleManager.IsShuttingDown() {
		return fmt.Errorf("collector is shutting down")
	}

	tracer := c.BaseCollector.GetTracer()
	ctx, span := tracer.Start(ctx, "runtime_signals.collector.start")
	defer span.End()

	// Start eBPF monitoring if enabled
	if c.config.EnableEBPF {
		if err := c.startEBPF(); err != nil {
			c.BaseCollector.RecordError(err)
			span.RecordError(err)
			return fmt.Errorf("failed to start eBPF: %w", err)
		}

		// Start event processing
		c.LifecycleManager.Start("ebpf-reader", func() {
			c.readEBPFEvents()
		})
	}

	c.BaseCollector.SetHealthy(true)
	c.logger.Info("Runtime signals collector started",
		zap.String("name", c.Name()),
		zap.Bool("ebpf_enabled", c.config.EnableEBPF),
	)
	return nil
}

// Stop stops the collector
func (c *Collector) Stop() error {
	tracer := c.BaseCollector.GetTracer()
	_, span := tracer.Start(context.Background(), "runtime_signals.collector.stop")
	defer span.End()

	c.logger.Info("Stopping runtime signals collector")

	// Stop eBPF
	c.stopEBPF()

	// Shutdown lifecycle manager
	if err := c.LifecycleManager.Stop(5 * time.Second); err != nil {
		c.logger.Warn("Timeout during shutdown", zap.Error(err))
	}

	// Close event channel
	c.EventChannelManager.Close()
	c.BaseCollector.SetHealthy(false)

	c.logger.Info("Runtime signals collector stopped")
	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan *domain.CollectorEvent {
	return c.EventChannelManager.GetChannel()
}

// IsHealthy returns health status
// Statistics delegates to base collector
func (c *Collector) Statistics() *domain.CollectorStats {
	return c.BaseCollector.Statistics()
}

// Health delegates to base collector
func (c *Collector) Health() *domain.HealthStatus {
	return c.BaseCollector.Health()
}

// createEvent creates a domain.CollectorEvent from CNI event data
func (c *Collector) createEvent(eventType string, data map[string]string) *domain.CollectorEvent {
	// Generate trace context
	ctx := context.Background()
	tracer := c.BaseCollector.GetTracer()
	ctx, span := tracer.Start(ctx, "runtime_signals.event.create")
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
		Source:    c.Name(),
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
