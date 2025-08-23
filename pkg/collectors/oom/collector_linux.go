//go:build linux

package oom

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/oom/bpf"
	"github.com/yairfalse/tapio/pkg/domain"
)

// Interface verification
var _ collectors.Collector = (*Collector)(nil)

// Collector implements the OOM killer monitor - the ULTIMATE root cause detector
type Collector struct {
	// Core collector state
	name   string
	config *OOMConfig
	logger *zap.Logger

	// Event channel for publisher
	events chan *domain.CollectorEvent

	// eBPF program and maps
	objects    *bpf.Objects // eBPF objects wrapper
	links      []link.Link
	ringReader *ringbuf.Reader

	// State management
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.RWMutex
	healthy bool

	// OpenTelemetry instrumentation - MANDATORY per CLAUDE.md
	tracer              trace.Tracer
	oomEventsTotal      metric.Int64Counter
	predictionsTotal    metric.Int64Counter
	errorsTotal         metric.Int64Counter
	processingTimeHist  metric.Float64Histogram
	memoryPressureGauge metric.Float64Gauge

	// Memory pressure tracking for prediction
	memoryTracker *MemoryPressureTracker

	// K8s context enricher
	k8sEnricher *KubernetesContextEnricher
}

// MemoryPressureTracker tracks memory pressure across containers for prediction
type MemoryPressureTracker struct {
	mu       sync.RWMutex
	trackers map[string]*ContainerMemoryTracker // keyed by container ID
}

// ContainerMemoryTracker tracks memory usage trends for a single container
type ContainerMemoryTracker struct {
	ContainerID       string
	LastUsage         uint64
	LastTimestamp     time.Time
	AllocationRate    float64 // MB/s
	PressureStartTime *time.Time
	MaxObservedUsage  uint64
	SampleCount       int
	PredictionHistory []MemoryPrediction
}

// MemoryPrediction represents a memory usage prediction
type MemoryPrediction struct {
	Timestamp        time.Time
	PredictedOOMTime time.Time
	Confidence       float64
	ActualOOMTime    *time.Time // Set when OOM actually occurs
	AccuracyScore    *float64   // Set after OOM occurs
}

// KubernetesContextEnricher enriches OOM events with Kubernetes context
type KubernetesContextEnricher struct {
	// This would typically interact with K8s API or local cache
	// For now, we'll implement basic cgroup parsing
}

// NewCollector creates a new OOM collector instance
func NewCollector(name string, config *OOMConfig, logger *zap.Logger) (*Collector, error) {
	if name == "" {
		return nil, fmt.Errorf("collector name cannot be empty")
	}
	if config == nil {
		config = DefaultOOMConfig()
	}
	if logger == nil {
		return nil, fmt.Errorf("logger cannot be nil")
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Initialize OpenTelemetry components - MANDATORY pattern per CLAUDE.md
	tracer := otel.Tracer(name)
	meter := otel.Meter(name)

	// Create metrics with descriptive names and descriptions
	oomEventsTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_oom_events_total", name),
		metric.WithDescription(fmt.Sprintf("Total OOM events processed by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create OOM events counter", zap.Error(err))
	}

	predictionsTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_predictions_total", name),
		metric.WithDescription(fmt.Sprintf("Total OOM predictions made by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create predictions counter", zap.Error(err))
	}

	errorsTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", name),
		metric.WithDescription(fmt.Sprintf("Total errors in %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	processingTimeHist, err := meter.Float64Histogram(
		fmt.Sprintf("%s_processing_duration_ms", name),
		metric.WithDescription(fmt.Sprintf("Processing duration for %s in milliseconds", name)),
	)
	if err != nil {
		logger.Warn("Failed to create processing time histogram", zap.Error(err))
	}

	memoryPressureGauge, err := meter.Float64Gauge(
		fmt.Sprintf("%s_memory_pressure_ratio", name),
		metric.WithDescription(fmt.Sprintf("Current memory pressure ratio tracked by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create memory pressure gauge", zap.Error(err))
	}

	collector := &Collector{
		name:    name,
		config:  config,
		logger:  logger,
		events:  make(chan *domain.CollectorEvent, config.EventBatchSize),
		healthy: false,

		// OpenTelemetry instrumentation
		tracer:              tracer,
		oomEventsTotal:      oomEventsTotal,
		predictionsTotal:    predictionsTotal,
		errorsTotal:         errorsTotal,
		processingTimeHist:  processingTimeHist,
		memoryPressureGauge: memoryPressureGauge,

		// Memory tracking
		memoryTracker: &MemoryPressureTracker{
			trackers: make(map[string]*ContainerMemoryTracker),
		},

		// K8s enricher
		k8sEnricher: &KubernetesContextEnricher{},
	}

	return collector, nil
}

// Name returns the collector name
func (c *Collector) Name() string {
	return c.name
}

// Start begins OOM monitoring - the moment we start saving lives
func (c *Collector) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		return fmt.Errorf("collector already started")
	}

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Start tracing
	ctx, span := c.tracer.Start(ctx, "oom.collector.start")
	defer span.End()

	c.logger.Info("Starting OOM killer monitor - preparing to save container lives",
		zap.String("collector", c.name),
		zap.Any("config", c.config))

	// Load eBPF program
	if err := c.loadEBPFProgram(ctx); err != nil {
		span.SetAttributes(attribute.String("error", err.Error()))
		return fmt.Errorf("failed to load eBPF program: %w", err)
	}

	// Start event processing
	c.wg.Add(1)
	go c.processEvents()

	// Start memory pressure monitoring
	if c.config.EnablePrediction {
		c.wg.Add(1)
		go c.monitorMemoryPressure()
	}

	c.healthy = true
	c.logger.Info("OOM killer monitor started successfully - ready to prevent disasters",
		zap.String("collector", c.name))

	return nil
}

// Stop gracefully shuts down the collector
func (c *Collector) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel == nil {
		return nil // Already stopped
	}

	c.logger.Info("Stopping OOM killer monitor",
		zap.String("collector", c.name))

	// Mark as unhealthy first to prevent new operations
	c.healthy = false

	// Cancel context to signal goroutines to stop
	c.cancel()

	// Wait for all goroutines to finish before closing resources
	c.wg.Wait()

	// Close ring buffer reader
	if c.ringReader != nil {
		if err := c.ringReader.Close(); err != nil {
			c.logger.Warn("Failed to close ring buffer reader",
				zap.String("collector", c.name),
				zap.Error(err))
		}
		c.ringReader = nil
	}

	// Detach eBPF programs in reverse order of attachment
	for i := len(c.links) - 1; i >= 0; i-- {
		if c.links[i] != nil {
			if err := c.links[i].Close(); err != nil {
				c.logger.Warn("Failed to close link",
					zap.String("collector", c.name),
					zap.Int("link_index", i),
					zap.Error(err))
			}
		}
	}
	c.links = nil

	// Close eBPF objects (maps and programs)
	if c.objects != nil {
		if err := c.objects.Close(); err != nil {
			c.logger.Warn("Failed to close eBPF objects",
				zap.String("collector", c.name),
				zap.Error(err))
		}
		c.objects = nil
	}

	// Close events channel
	close(c.events)

	// Clear cancel function
	c.cancel = nil

	c.logger.Info("OOM killer monitor stopped successfully",
		zap.String("collector", c.name))

	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan *domain.CollectorEvent {
	return c.events
}

// IsHealthy returns the health status
func (c *Collector) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy
}

// loadEBPFProgram loads and attaches the eBPF programs
func (c *Collector) loadEBPFProgram(ctx context.Context) error {
	ctx, span := c.tracer.Start(ctx, "oom.collector.load_ebpf")
	defer span.End()

	// Load eBPF objects using wrapper API
	objs, err := bpf.LoadObjects()
	if err != nil {
		return fmt.Errorf("failed to load eBPF objects: %w", err)
	}

	// Store objects reference
	c.objects = objs

	// Configure monitoring (enable/disable)
	configMap := c.objects.GetConfigMap()
	if configMap != nil {
		key := uint32(0)
		value := uint32(1) // enabled
		if err := configMap.Put(&key, &value); err != nil {
			return fmt.Errorf("failed to configure monitoring: %w", err)
		}
	}

	// Attach tracepoints
	if err := c.attachTracepoints(); err != nil {
		return fmt.Errorf("failed to attach tracepoints: %w", err)
	}

	// Setup ring buffer reader
	reader, err := ringbuf.NewReader(c.objects.GetOomEventsMap())
	if err != nil {
		return fmt.Errorf("failed to create ring buffer reader: %w", err)
	}
	c.ringReader = reader

	c.logger.Info("eBPF program loaded and attached successfully",
		zap.String("collector", c.name))

	return nil
}

// attachTracepoints attaches eBPF programs to kernel tracepoints
func (c *Collector) attachTracepoints() error {
	// Attach OOM kill tracepoint (critical for core functionality)
	if prog := c.objects.GetTraceOomKillProcessProgram(); prog != nil {
		l1, err := link.Tracepoint("oom", "oom_kill_process", prog, nil)
		if err != nil {
			return fmt.Errorf("failed to attach oom_kill_process tracepoint: %w", err)
		}
		c.links = append(c.links, l1)
		c.logger.Debug("Attached OOM kill tracepoint", zap.String("collector", c.name))
	} else {
		c.logger.Error("OOM kill program not found", zap.String("collector", c.name))
	}

	// Attach memory pressure tracepoint (optional for prediction)
	if prog := c.objects.GetTraceMemoryPressureProgram(); prog != nil {
		l2, err := link.Tracepoint("kmem", "mm_page_alloc_extfrag", prog, nil)
		if err != nil {
			c.logger.Warn("Failed to attach memory pressure tracepoint (optional)",
				zap.String("collector", c.name), zap.Error(err))
		} else {
			c.links = append(c.links, l2)
			c.logger.Debug("Attached memory pressure tracepoint", zap.String("collector", c.name))
		}
	}

	// Attach process exit raw tracepoint (optional for correlation)
	if prog := c.objects.GetTraceProcessExitProgram(); prog != nil {
		l3, err := link.AttachRawTracepoint(link.RawTracepointOptions{
			Name:    "sched_process_exit",
			Program: prog,
		})
		if err != nil {
			c.logger.Warn("Failed to attach process exit tracepoint (optional)",
				zap.String("collector", c.name), zap.Error(err))
		} else {
			c.links = append(c.links, l3)
			c.logger.Debug("Attached process exit tracepoint", zap.String("collector", c.name))
		}
	}

	// Ensure at least one tracepoint attached
	if len(c.links) == 0 {
		return fmt.Errorf("no tracepoints could be attached")
	}

	c.logger.Info("Successfully attached tracepoints",
		zap.String("collector", c.name),
		zap.Int("attached_count", len(c.links)))

	return nil
}

// processEvents reads events from the ring buffer and processes them
func (c *Collector) processEvents() {
	defer c.wg.Done()

	c.logger.Info("Started event processing loop",
		zap.String("collector", c.name))

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		// Read from ring buffer with timeout
		record, err := c.ringReader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				return
			}
			c.recordError(c.ctx, "ring_buffer_read", err)
			continue
		}

		// Process the raw event
		if err := c.processRawEvent(record.RawSample); err != nil {
			c.recordError(c.ctx, "event_processing", err)
		}
	}
}

// processRawEvent processes a single raw event from the ring buffer
func (c *Collector) processRawEvent(data []byte) error {
	ctx, span := c.tracer.Start(c.ctx, "oom.collector.process_raw_event")
	defer span.End()

	start := time.Now()
	defer func() {
		// Record processing time
		duration := time.Since(start).Seconds() * 1000 // Convert to milliseconds
		if c.processingTimeHist != nil {
			c.processingTimeHist.Record(ctx, duration)
		}
	}()

	// Validate data size
	expectedSize := int(GetOOMEventSize())
	if len(data) != expectedSize {
		return fmt.Errorf("invalid event size: got %d, expected %d", len(data), expectedSize)
	}

	// Cast to OOMEvent struct
	rawEvent := (*OOMEvent)(unsafe.Pointer(&data[0]))

	// Validate event
	if err := c.validateRawEvent(rawEvent); err != nil {
		return fmt.Errorf("event validation failed: %w", err)
	}

	// Convert to processed event
	processedEvent := rawEvent.ToProcessedEvent()

	// Enrich with Kubernetes context
	c.enrichKubernetesContext(processedEvent)

	// Update memory tracking for predictions
	if c.config.EnablePrediction {
		c.updateMemoryTracking(processedEvent)
	}

	// Convert to collector event
	collectorEvent := processedEvent.ToCollectorEvent()

	// Set span attributes for debugging
	span.SetAttributes(
		attribute.String("event_type", processedEvent.EventType.String()),
		attribute.Int("pid", int(processedEvent.PID)),
		attribute.String("command", processedEvent.Command),
		attribute.String("container_id", processedEvent.KubernetesContext.ContainerID),
	)

	// Send event
	select {
	case c.events <- collectorEvent:
		// Record success metrics
		if c.oomEventsTotal != nil {
			c.oomEventsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("event_type", processedEvent.EventType.String()),
				attribute.String("status", "success"),
			))
		}

		// Special handling for predictions
		if processedEvent.EventType.IsPredictive() {
			if c.predictionsTotal != nil {
				c.predictionsTotal.Add(ctx, 1, metric.WithAttributes(
					attribute.String("prediction_type", processedEvent.EventType.String()),
				))
			}

			c.logger.Warn("MEMORY PRESSURE DETECTED - Container may OOM soon",
				zap.String("container_id", processedEvent.KubernetesContext.ContainerID),
				zap.String("pod_name", processedEvent.KubernetesContext.PodName),
				zap.Float64("usage_percent", processedEvent.MemoryStats.UsagePercent),
				zap.Any("prediction_data", processedEvent.PredictionData))
		}

		// Log critical events
		if processedEvent.EventType.IsCritical() {
			c.logger.Error("CRITICAL: Container OOM event detected",
				zap.String("event_type", processedEvent.EventType.String()),
				zap.String("container_id", processedEvent.KubernetesContext.ContainerID),
				zap.String("pod_name", processedEvent.KubernetesContext.PodName),
				zap.String("command", processedEvent.Command),
				zap.Uint64("memory_usage_mb", processedEvent.MemoryStats.UsageBytes/(1024*1024)),
				zap.Uint64("memory_limit_mb", processedEvent.MemoryStats.LimitBytes/(1024*1024)))
		}

	case <-c.ctx.Done():
		return nil
	default:
		// Channel full - this should rarely happen for OOM events
		c.recordError(ctx, "event_channel_full", fmt.Errorf("event channel is full"))
	}

	return nil
}

// validateRawEvent validates a raw OOM event
func (c *Collector) validateRawEvent(event *OOMEvent) error {
	if event.Timestamp == 0 {
		return fmt.Errorf("invalid timestamp: 0")
	}

	if event.PID == 0 {
		return fmt.Errorf("invalid PID: 0")
	}

	if OOMEventType(event.EventType) == 0 {
		return fmt.Errorf("invalid event type: 0")
	}

	// Validate memory values for OOM kill events
	if OOMEventType(event.EventType) == OOMKillVictim {
		if event.MemoryLimit == 0 && event.MemoryUsage > 0 {
			// This might be a system OOM, not container OOM - still valid
		}
	}

	return nil
}

// enrichKubernetesContext enriches the event with Kubernetes context
func (c *Collector) enrichKubernetesContext(event *ProcessedOOMEvent) {
	if !c.config.EnableK8sCorrelation {
		return
	}

	// This is where we would enrich with actual K8s API data
	// For now, we rely on the cgroup path parsing done in ToProcessedEvent()

	// Example enrichment (in real implementation, this would query K8s API):
	if event.KubernetesContext.ContainerID != "" {
		// Extract more context from container ID
		// event.KubernetesContext.ContainerName = c.k8sEnricher.GetContainerName(event.KubernetesContext.ContainerID)
		// event.KubernetesContext.PodName = c.k8sEnricher.GetPodName(event.KubernetesContext.ContainerID)
		// etc.
	}
}

// updateMemoryTracking updates memory pressure tracking for predictions
func (c *Collector) updateMemoryTracking(event *ProcessedOOMEvent) {
	if event.KubernetesContext.ContainerID == "" {
		return
	}

	c.memoryTracker.mu.Lock()
	defer c.memoryTracker.mu.Unlock()

	containerID := event.KubernetesContext.ContainerID
	tracker, exists := c.memoryTracker.trackers[containerID]

	if !exists {
		// Create new tracker
		tracker = &ContainerMemoryTracker{
			ContainerID:       containerID,
			LastUsage:         event.MemoryStats.UsageBytes,
			LastTimestamp:     event.Timestamp,
			MaxObservedUsage:  event.MemoryStats.UsageBytes,
			SampleCount:       1,
			PredictionHistory: make([]MemoryPrediction, 0, 100), // Keep last 100 predictions
		}
		c.memoryTracker.trackers[containerID] = tracker
		return
	}

	// Update existing tracker
	now := event.Timestamp
	timeDiff := now.Sub(tracker.LastTimestamp).Seconds()

	if timeDiff > 0 {
		// Calculate allocation rate
		usageDiff := int64(event.MemoryStats.UsageBytes) - int64(tracker.LastUsage)
		if usageDiff > 0 {
			tracker.AllocationRate = float64(usageDiff) / (1024 * 1024) / timeDiff // MB/s
		}

		// Track pressure start time
		usagePercent := event.MemoryStats.UsagePercent
		if usagePercent >= float64(c.config.HighPressureThresholdPct) && tracker.PressureStartTime == nil {
			tracker.PressureStartTime = &now
		} else if usagePercent < float64(c.config.HighPressureThresholdPct) {
			tracker.PressureStartTime = nil
		}

		// Update max usage
		if event.MemoryStats.UsageBytes > tracker.MaxObservedUsage {
			tracker.MaxObservedUsage = event.MemoryStats.UsageBytes
		}

		tracker.LastUsage = event.MemoryStats.UsageBytes
		tracker.LastTimestamp = now
		tracker.SampleCount++

		// Update memory pressure gauge
		if c.memoryPressureGauge != nil {
			c.memoryPressureGauge.Record(c.ctx, usagePercent/100.0, metric.WithAttributes(
				attribute.String("container_id", containerID),
			))
		}
	}

	// Generate prediction if applicable
	if event.EventType.IsPredictive() && tracker.AllocationRate > 0 {
		prediction := c.generateMemoryPrediction(tracker, event)
		if prediction != nil {
			tracker.PredictionHistory = append(tracker.PredictionHistory, *prediction)

			// Limit history size
			if len(tracker.PredictionHistory) > 100 {
				tracker.PredictionHistory = tracker.PredictionHistory[1:]
			}
		}
	}
}

// generateMemoryPrediction generates a memory usage prediction
func (c *Collector) generateMemoryPrediction(tracker *ContainerMemoryTracker, event *ProcessedOOMEvent) *MemoryPrediction {
	if tracker.AllocationRate <= 0 || event.MemoryStats.LimitBytes == 0 {
		return nil
	}

	remainingBytes := event.MemoryStats.LimitBytes - event.MemoryStats.UsageBytes
	if remainingBytes <= 0 {
		return nil
	}

	// Calculate time to OOM based on current allocation rate
	remainingMB := float64(remainingBytes) / (1024 * 1024)
	secondsToOOM := remainingMB / tracker.AllocationRate

	// Calculate confidence based on sample count and rate stability
	confidence := 0.5 // Base confidence
	if tracker.SampleCount > 10 {
		confidence += 0.3
	}
	if tracker.AllocationRate > 10 { // High allocation rate
		confidence += 0.2
	}
	if confidence > 1.0 {
		confidence = 1.0
	}

	prediction := &MemoryPrediction{
		Timestamp:        event.Timestamp,
		PredictedOOMTime: event.Timestamp.Add(time.Duration(secondsToOOM) * time.Second),
		Confidence:       confidence,
	}

	return prediction
}

// monitorMemoryPressure periodically monitors memory pressure across all containers
func (c *Collector) monitorMemoryPressure() {
	defer c.wg.Done()

	ticker := time.NewTicker(time.Second * 30) // Check every 30 seconds
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.performMemoryPressureCheck()
		}
	}
}

// performMemoryPressureCheck checks for memory pressure patterns
func (c *Collector) performMemoryPressureCheck() {
	c.memoryTracker.mu.RLock()
	defer c.memoryTracker.mu.RUnlock()

	now := time.Now()

	for containerID, tracker := range c.memoryTracker.trackers {
		// Clean up old trackers (no activity for 10 minutes)
		if now.Sub(tracker.LastTimestamp) > time.Minute*10 {
			delete(c.memoryTracker.trackers, containerID)
			continue
		}

		// Check for sustained pressure
		if tracker.PressureStartTime != nil {
			pressureDuration := now.Sub(*tracker.PressureStartTime)

			// Emit warning for sustained pressure (>2 minutes)
			if pressureDuration > time.Minute*2 {
				c.logger.Warn("Sustained memory pressure detected",
					zap.String("container_id", containerID),
					zap.Duration("pressure_duration", pressureDuration),
					zap.Float64("allocation_rate_mb_s", tracker.AllocationRate))
			}
		}
	}
}

// recordError records an error with metrics and logging
func (c *Collector) recordError(ctx context.Context, errorType string, err error) {
	if c.errorsTotal != nil {
		c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("error_type", errorType),
		))
	}

	c.logger.Error("OOM collector error",
		zap.String("collector", c.name),
		zap.String("error_type", errorType),
		zap.Error(err))
}
