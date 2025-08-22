//go:build linux
// +build linux

package criebpf

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/google/uuid"
	"github.com/yairfalse/tapio/pkg/collectors/cri-ebpf/bpf"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 crimonitor ./bpf_src/cri_monitor.c -- -I../bpf_common

// Collector implements eBPF-based container runtime monitoring
// Provides real-time OOM detection and container process tracking
type Collector struct {
	name    string
	logger  *zap.Logger
	tracer  trace.Tracer
	events  chan *domain.CollectorEvent
	ctx     context.Context
	cancel  context.CancelFunc
	healthy bool
	config  *Config
	mu      sync.RWMutex

	// eBPF components
	objs   *bpf.CrimonitorObjects
	links  []link.Link
	reader *ringbuf.Reader

	// Container metadata tracking
	containerCache map[string]*ContainerMetadata
	cacheMu        sync.RWMutex

	// Essential OTEL Metrics
	eventsProcessed metric.Int64Counter
	errorsTotal     metric.Int64Counter
	processingTime  metric.Float64Histogram
	droppedEvents   metric.Int64Counter
	bufferUsage     metric.Int64Gauge
	oomKillsTotal   metric.Int64Counter
	memoryPressure  metric.Int64Counter
}

// NewCollector creates a new CRI eBPF collector
func NewCollector(name string, cfg *Config) (*Collector, error) {
	if cfg == nil {
		cfg = NewDefaultConfig(name)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Create logger
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Initialize OTEL components
	tracer := otel.Tracer("cri-ebpf-collector")
	meter := otel.Meter("cri-ebpf-collector")

	// Create metrics
	eventsProcessed, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription(fmt.Sprintf("Total events processed by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create events processed counter", zap.Error(err))
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

	oomKillsTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_oom_kills_total", name),
		metric.WithDescription(fmt.Sprintf("Total OOM kills detected by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create OOM kills counter", zap.Error(err))
	}

	memoryPressure, err := meter.Int64Counter(
		fmt.Sprintf("%s_memory_pressure_total", name),
		metric.WithDescription(fmt.Sprintf("Total memory pressure events by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create memory pressure counter", zap.Error(err))
	}

	c := &Collector{
		name:            name,
		logger:          logger.Named(name),
		tracer:          tracer,
		config:          cfg,
		events:          make(chan *domain.CollectorEvent, cfg.BufferSize),
		containerCache:  make(map[string]*ContainerMetadata),
		eventsProcessed: eventsProcessed,
		errorsTotal:     errorsTotal,
		processingTime:  processingTime,
		droppedEvents:   droppedEvents,
		bufferUsage:     bufferUsage,
		oomKillsTotal:   oomKillsTotal,
		memoryPressure:  memoryPressure,
	}

	c.logger.Info("CRI eBPF collector created",
		zap.String("name", name),
		zap.Int("buffer_size", cfg.BufferSize),
		zap.Bool("enable_oom_kill", cfg.EnableOOMKill),
		zap.Bool("enable_memory_pressure", cfg.EnableMemoryPressure),
	)

	return c, nil
}

// Name returns collector name
func (c *Collector) Name() string {
	return c.name
}

// Start starts the eBPF monitoring
func (c *Collector) Start(ctx context.Context) error {
	ctx, span := c.tracer.Start(ctx, "cri-ebpf.collector.start")
	defer span.End()

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Validate struct sizes match eBPF expectations
	if err := ValidateBPFContainerExitEvent(); err != nil {
		span.SetAttributes(attribute.String("error", "struct_validation_failed"))
		return fmt.Errorf("BPF struct validation failed: %w", err)
	}

	if err := ValidateBPFContainerMetadata(); err != nil {
		span.SetAttributes(attribute.String("error", "metadata_validation_failed"))
		return fmt.Errorf("BPF metadata validation failed: %w", err)
	}

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		span.SetAttributes(attribute.String("error", "memlock_removal_failed"))
		return fmt.Errorf("removing memory limit: %w", err)
	}

	// Load eBPF programs
	if err := c.loadEBPFPrograms(); err != nil {
		return fmt.Errorf("failed to load eBPF programs: %w", err)
	}

	// Attach programs to kernel
	if err := c.attachPrograms(); err != nil {
		c.cleanup()
		return fmt.Errorf("failed to attach eBPF programs: %w", err)
	}

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(c.objs.Events)
	if err != nil {
		c.cleanup()
		return fmt.Errorf("creating ring buffer reader: %w", err)
	}
	c.reader = reader

	// Start event processing goroutine
	go c.processEvents()

	// Start metrics collection goroutine
	go c.collectMetrics()

	c.healthy = true
	c.logger.Info("CRI eBPF collector started successfully",
		zap.String("ring_buffer_size", fmt.Sprintf("%d bytes", c.config.RingBufferSize)),
		zap.Int("attached_programs", len(c.links)),
	)

	span.SetAttributes(
		attribute.Bool("success", true),
		attribute.Int("attached_programs", len(c.links)),
	)

	return nil
}

// Stop stops the collector
func (c *Collector) Stop() error {
	c.logger.Info("Stopping CRI eBPF collector")

	if c.cancel != nil {
		c.cancel()
	}

	c.cleanup()
	close(c.events)
	c.healthy = false

	c.logger.Info("CRI eBPF collector stopped successfully")
	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan *domain.CollectorEvent {
	return c.events
}

// IsHealthy returns health status
func (c *Collector) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy
}

// loadEBPFPrograms loads the compiled eBPF programs
func (c *Collector) loadEBPFPrograms() error {
	objs := &bpf.CrimonitorObjects{}
	if err := bpf.LoadCrimonitorObjects(objs, nil); err != nil {
		return fmt.Errorf("loading eBPF objects: %w", err)
	}

	c.objs = objs
	c.logger.Info("eBPF programs loaded successfully")
	return nil
}

// attachPrograms attaches eBPF programs to kernel hooks
func (c *Collector) attachPrograms() error {
	var links []link.Link

	// Attach OOM kill kprobe if enabled
	if c.config.EnableOOMKill {
		oomLink, err := link.Kprobe(link.KprobeOptions{
			Symbol:  "oom_kill_process",
			Program: c.objs.TraceOomKill,
		})
		if err != nil {
			return fmt.Errorf("attaching OOM kill kprobe: %w", err)
		}
		links = append(links, oomLink)
		c.logger.Debug("Attached OOM kill kprobe")
	}

	// Attach memory cgroup OOM kprobe if enabled
	if c.config.EnableMemoryPressure {
		memcgOomLink, err := link.Kprobe(link.KprobeOptions{
			Symbol:  "mem_cgroup_out_of_memory",
			Program: c.objs.TraceMemcgOom,
		})
		if err != nil {
			// This is optional - some kernels might not have this symbol
			c.logger.Warn("Failed to attach memory cgroup OOM kprobe", zap.Error(err))
		} else {
			links = append(links, memcgOomLink)
			c.logger.Debug("Attached memory cgroup OOM kprobe")
		}
	}

	// Attach process exit tracepoint if enabled
	if c.config.EnableProcessExit {
		exitLink, err := link.Tracepoint(link.TracepointOptions{
			Group:   "sched",
			Name:    "sched_process_exit",
			Program: c.objs.TraceProcessExit,
		})
		if err != nil {
			return fmt.Errorf("attaching process exit tracepoint: %w", err)
		}
		links = append(links, exitLink)
		c.logger.Debug("Attached process exit tracepoint")
	}

	// Attach process fork tracepoint if enabled
	if c.config.EnableProcessFork {
		forkLink, err := link.Tracepoint(link.TracepointOptions{
			Group:   "sched",
			Name:    "sched_process_fork",
			Program: c.objs.TraceProcessFork,
		})
		if err != nil {
			return fmt.Errorf("attaching process fork tracepoint: %w", err)
		}
		links = append(links, forkLink)
		c.logger.Debug("Attached process fork tracepoint")
	}

	c.links = links
	return nil
}

// processEvents reads events from the ring buffer and converts them to CollectorEvents
func (c *Collector) processEvents() {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			record, err := c.reader.Read()
			if err != nil {
				if c.ctx.Err() != nil {
					return
				}
				if c.errorsTotal != nil {
					c.errorsTotal.Add(c.ctx, 1, metric.WithAttributes(
						attribute.String("error_type", "ringbuf_read_failed"),
					))
				}
				c.logger.Error("Failed to read from ring buffer", zap.Error(err))
				continue
			}

			c.handleRingBufferEvent(record.RawSample)
		}
	}
}

// handleRingBufferEvent processes a single ring buffer event
func (c *Collector) handleRingBufferEvent(data []byte) {
	start := time.Now()
	ctx := c.ctx

	// Validate event size
	if len(data) < int(unsafe.Sizeof(BPFContainerExitEvent{})) {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "invalid_event_size"),
			))
		}
		return
	}

	// Parse eBPF event
	var bpfEvent BPFContainerExitEvent
	if err := binary.Read(bytes.NewReader(data), binary.LittleEndian, &bpfEvent); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "event_parse_failed"),
			))
		}
		c.logger.Error("Failed to parse BPF event", zap.Error(err))
		return
	}

	// Convert to CollectorEvent
	event, err := c.convertToCollectorEvent(&bpfEvent)
	if err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "event_conversion_failed"),
			))
		}
		c.logger.Error("Failed to convert BPF event", zap.Error(err))
		return
	}

	// Update metrics based on event type
	c.updateEventMetrics(event, &bpfEvent)

	// Send event to channel
	select {
	case c.events <- event:
		if c.eventsProcessed != nil {
			c.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
				attribute.String("event_type", string(event.Type)),
				attribute.String("container_id", event.CorrelationHints.ContainerID),
			))
		}
	default:
		// Channel full, drop event
		if c.droppedEvents != nil {
			c.droppedEvents.Add(ctx, 1, metric.WithAttributes(
				attribute.String("reason", "channel_full"),
				attribute.String("event_type", string(event.Type)),
			))
		}
		c.logger.Warn("Dropped event due to full channel",
			zap.String("event_type", string(event.Type)),
			zap.String("container_id", event.CorrelationHints.ContainerID),
		)
	}

	// Record processing time
	if c.processingTime != nil {
		duration := time.Since(start).Milliseconds()
		c.processingTime.Record(ctx, float64(duration), metric.WithAttributes(
			attribute.String("operation", "handle_event"),
		))
	}
}

// convertToCollectorEvent converts BPF event to domain.CollectorEvent
func (c *Collector) convertToCollectorEvent(bpfEvent *BPFContainerExitEvent) (*domain.CollectorEvent, error) {
	timestamp := time.Unix(0, int64(bpfEvent.Timestamp))
	containerID := CStringToGo(bpfEvent.ContainerID[:])
	command := CStringToGo(bpfEvent.Comm[:])

	// Determine event type
	var eventType domain.CollectorEventType
	if bpfEvent.OOMKilled == 1 {
		eventType = domain.EventTypeContainerOOM
	} else if bpfEvent.ExitCode != 0 {
		eventType = domain.EventTypeContainerExit
	} else {
		eventType = domain.EventTypeContainerStop
	}

	// Get container metadata
	containerMeta := c.getContainerMetadata(containerID)

	// Build process data
	processData := &domain.ProcessData{
		PID:         int32(bpfEvent.PID),
		Command:     command,
		ContainerID: containerID,
		CgroupPath:  fmt.Sprintf("/proc/cgroup/%d", bpfEvent.CgroupID),
	}

	// Build container data
	containerData := &domain.ContainerData{
		ContainerID: containerID,
		State:       "exited",
		Action:      "exit",
		PID:         int32(bpfEvent.PID),
	}

	// Set exit code
	if bpfEvent.ExitCode != 0 {
		exitCode := bpfEvent.ExitCode
		containerData.ExitCode = &exitCode
	}

	// Set signal if OOM killed
	if bpfEvent.OOMKilled == 1 {
		signal := int32(9) // SIGKILL
		containerData.Signal = &signal
	}

	// Build correlation hints
	correlationHints := domain.CorrelationHints{
		ProcessID:   int32(bpfEvent.PID),
		ContainerID: containerID,
		CgroupPath:  fmt.Sprintf("/proc/cgroup/%d", bpfEvent.CgroupID),
	}

	// Extract K8s context if available
	var k8sContext *domain.K8sContext
	if containerMeta != nil {
		k8sContext = &domain.K8sContext{
			Kind:        "Pod",
			Name:        containerMeta.PodName,
			UID:         containerMeta.PodUID,
			Namespace:   containerMeta.Namespace,
			ContainerID: containerID,
		}
		correlationHints.PodUID = containerMeta.PodUID
	}

	// Determine event priority
	priority := domain.PriorityNormal
	if bpfEvent.OOMKilled == 1 {
		priority = domain.PriorityCritical
	} else if bpfEvent.ExitCode != 0 {
		priority = domain.PriorityHigh
	}

	// Create CollectorEvent
	event := &domain.CollectorEvent{
		EventID:   uuid.New().String(),
		Timestamp: timestamp,
		Type:      eventType,
		Source:    c.name,
		EventData: domain.EventDataContainer{
			Process:   processData,
			Container: containerData,
		},
		Metadata: domain.EventMetadata{
			Priority:      priority,
			SchemaVersion: "v1",
			Labels:        make(map[string]string),
		},
		CorrelationHints: correlationHints,
		K8sContext:       k8sContext,
		CollectionContext: domain.CollectionContext{
			CollectorVersion: "1.0.0",
			HostInfo: domain.HostInfo{
				KernelVersion: c.getKernelVersion(),
			},
			CollectionConfig: domain.CollectionConfig{
				BufferSize:    c.config.BufferSize,
				FlushInterval: c.config.MetricsInterval,
			},
		},
	}

	// Add metadata labels
	event.AddMetadataLabel("collector_type", "ebpf")
	event.AddMetadataLabel("command", command)
	event.AddMetadataLabel("memory_usage", fmt.Sprintf("%d", bpfEvent.MemoryUsage))
	event.AddMetadataLabel("memory_limit", fmt.Sprintf("%d", bpfEvent.MemoryLimit))

	if bpfEvent.OOMKilled == 1 {
		event.AddMetadataLabel("oom_killed", "true")
	}

	return event, nil
}

// updateEventMetrics updates metrics based on event type
func (c *Collector) updateEventMetrics(event *domain.CollectorEvent, bpfEvent *BPFContainerExitEvent) {
	ctx := c.ctx

	if bpfEvent.OOMKilled == 1 && c.oomKillsTotal != nil {
		c.oomKillsTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("container_id", event.CorrelationHints.ContainerID),
		))
	}

	// Check for memory pressure (>90% utilization)
	if bpfEvent.MemoryLimit > 0 {
		utilization := float64(bpfEvent.MemoryUsage) / float64(bpfEvent.MemoryLimit)
		if utilization > 0.9 && c.memoryPressure != nil {
			c.memoryPressure.Add(ctx, 1, metric.WithAttributes(
				attribute.String("container_id", event.CorrelationHints.ContainerID),
				attribute.Float64("utilization", utilization),
			))
		}
	}
}

// collectMetrics periodically collects eBPF statistics
func (c *Collector) collectMetrics() {
	ticker := time.NewTicker(c.config.MetricsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.updateBPFStats()
		}
	}
}

// updateBPFStats reads and updates eBPF statistics
func (c *Collector) updateBPFStats() {
	if c.objs == nil || c.objs.StatsMap == nil {
		return
	}

	// Read statistics from eBPF map
	for i := 0; i < 4; i++ {
		key := uint32(i)
		var value uint64
		if err := c.objs.StatsMap.Lookup(&key, &value); err != nil {
			continue
		}

		// Update metrics based on stat type
		switch i {
		case StatOOMKills:
			if c.oomKillsTotal != nil {
				// Note: This is cumulative, so we don't add but set
				c.logger.Debug("eBPF OOM kills stat", zap.Uint64("total", value))
			}
		case StatProcessExits:
			c.logger.Debug("eBPF process exits stat", zap.Uint64("total", value))
		case StatContainerStarts:
			c.logger.Debug("eBPF container starts stat", zap.Uint64("total", value))
		case StatEventsDropped:
			c.logger.Debug("eBPF events dropped stat", zap.Uint64("total", value))
		}
	}
}

// getContainerMetadata retrieves container metadata from cache
func (c *Collector) getContainerMetadata(containerID string) *ContainerMetadata {
	if containerID == "" {
		return nil
	}

	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()
	return c.containerCache[containerID]
}

// UpdateContainerMetadata updates container metadata in cache
func (c *Collector) UpdateContainerMetadata(containerID string, meta *ContainerMetadata) {
	if containerID == "" || meta == nil {
		return
	}

	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	meta.LastSeen = time.Now()
	c.containerCache[containerID] = meta

	c.logger.Debug("Updated container metadata",
		zap.String("container_id", containerID),
		zap.String("pod_name", meta.PodName),
		zap.String("namespace", meta.Namespace),
	)
}

// cleanup cleans up eBPF resources
func (c *Collector) cleanup() {
	// Close ring buffer reader
	if c.reader != nil {
		c.reader.Close()
		c.reader = nil
	}

	// Close all links
	for _, link := range c.links {
		if err := link.Close(); err != nil {
			c.logger.Error("Failed to close eBPF link", zap.Error(err))
		}
	}
	c.links = nil

	// Close eBPF objects
	if c.objs != nil {
		c.objs.Close()
		c.objs = nil
	}

	c.logger.Debug("eBPF resources cleaned up")
}

// getKernelVersion returns the kernel version
func (c *Collector) getKernelVersion() string {
	// This is a simplified implementation
	// In production, read from /proc/version
	return "unknown"
}
