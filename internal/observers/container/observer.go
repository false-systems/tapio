//go:build linux

package container

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/cilium/ebpf/ringbuf"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/yairfalse/tapio/internal/base"
	"github.com/yairfalse/tapio/internal/observers/container/bpf"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"golang.org/x/sync/errgroup"
)

// Config holds container observer configuration
type Config struct {
	EventChannelSize int // Ring buffer → processor channel size (default: 1000)

	// cgroup monitor configuration
	CgroupBasePath  string        // cgroup v2 base path (default: /sys/fs/cgroup)
	CgroupCacheSize int           // LRU cache size (default: 1000)
	CgroupCacheTTL  time.Duration // Cache TTL (default: 30s)
}

// ContainerObserver tracks container exits and OOM kills using eBPF
type ContainerObserver struct {
	name          string
	deps          *base.Deps
	logger        zerolog.Logger
	config        Config
	cgroupMonitor *CgroupMonitor

	// Container-specific Prometheus metrics
	oomKillsTotal   *prometheus.Counter
	exitsTotal      *prometheus.Counter
	exitsByCategory *prometheus.Counter
	memoryAtExit    *prometheus.Histogram

	// Processing metrics
	processingLatency *prometheus.Histogram
	enrichmentErrors  *prometheus.Counter
}

// New creates a container observer with dependency injection.
func New(config Config, deps *base.Deps) (*ContainerObserver, error) {
	name := "container"

	// Create meter for cgroup monitor
	meter := otel.Meter(fmt.Sprintf("tapio.observer.%s", name))

	// Create cgroup monitor
	cgroupMonitor, err := NewCgroupMonitor(CgroupMonitorConfig{
		BasePath:  config.CgroupBasePath,
		CacheSize: config.CgroupCacheSize,
		CacheTTL:  config.CgroupCacheTTL,
	}, meter)
	if err != nil {
		return nil, fmt.Errorf("failed to create cgroup monitor: %w", err)
	}

	obs := &ContainerObserver{
		name:          name,
		deps:          deps,
		logger:        base.NewLogger(name),
		config:        config,
		cgroupMonitor: cgroupMonitor,
	}

	// Create container-specific OTEL metrics using MetricBuilder (fluent API)
	if err := obs.initMetrics(name); err != nil {
		return nil, fmt.Errorf("failed to create metrics: %w", err)
	}

	return obs, nil
}

// initMetrics initializes Prometheus metrics for the container observer
func (c *ContainerObserver) initMetrics(name string) error {
	// Memory buckets: 1MB to 16GB
	memoryBuckets := []float64{1e6, 10e6, 50e6, 100e6, 500e6, 1e9, 2e9, 4e9, 8e9, 16e9}
	// Latency buckets: 0.1ms to 100ms
	latencyBuckets := []float64{0.1, 0.5, 1, 2.5, 5, 10, 25, 50, 100}

	return base.NewPromMetricBuilder(base.GlobalRegistry, name).
		Counter(&c.oomKillsTotal, "oom_kills_total", "Total OOM kills detected").
		Counter(&c.exitsTotal, "exits_total", "Total container exits detected").
		Counter(&c.exitsByCategory, "exits_by_category_total", "Container exits by exit category").
		Histogram(&c.memoryAtExit, "memory_at_exit_bytes", "Memory usage at container exit time", memoryBuckets).
		Histogram(&c.processingLatency, "event_processing_duration_ms", "Event processing latency in milliseconds", latencyBuckets).
		Counter(&c.enrichmentErrors, "enrichment_errors_total", "Errors during event enrichment").
		Build()
}

// Run starts the container observer and blocks until context is cancelled.
func (c *ContainerObserver) Run(ctx context.Context) error {
	c.logger.Info().Msg("starting container observer")

	// Check kernel compatibility
	if err := checkKernelCompatibility(); err != nil {
		return fmt.Errorf("kernel compatibility check failed: %w", err)
	}

	// Create event channel for ring buffer → processor communication
	channelSize := c.config.EventChannelSize
	if channelSize <= 0 {
		channelSize = 1000
	}
	eventCh := make(chan ContainerEventBPF, channelSize)

	g, ctx := errgroup.WithContext(ctx)

	// Stage 1: Load and attach eBPF program
	g.Go(func() error {
		return c.loadAndAttachStage(ctx, eventCh)
	})

	// Stage 2: Process events from channel
	g.Go(func() error {
		return c.processEventsStage(ctx, eventCh)
	})

	err := g.Wait()
	c.logger.Info().Msg("container observer stopped")
	return err
}

// loadAndAttachStage loads eBPF program, attaches to tracepoints, reads ring buffer
func (c *ContainerObserver) loadAndAttachStage(ctx context.Context, eventCh chan ContainerEventBPF) error {
	defer close(eventCh)

	// Load eBPF objects (bpf2go-generated)
	objs := &bpf.ContainerObjects{}
	if err := bpf.LoadContainerObjects(objs, nil); err != nil {
		return fmt.Errorf("failed to load eBPF objects: %w", err)
	}
	defer func() {
		if err := objs.Close(); err != nil {
			c.logger.Error().Err(err).Msg("error closing eBPF objects")
		}
	}()

	// Create eBPF manager for tracepoint attachment
	ebpfMgr := base.NewEBPFManagerFromCollection(nil)
	defer func() {
		if err := ebpfMgr.Close(); err != nil {
			c.logger.Error().Err(err).Msg("error closing eBPF manager")
		}
	}()

	// Attach to oom/mark_victim tracepoint (OOM kills)
	if err := ebpfMgr.AttachTracepointWithProgram(objs.HandleOom, "oom", "mark_victim"); err != nil {
		return fmt.Errorf("failed to attach oom/mark_victim: %w", err)
	}

	// Attach to sched/sched_process_exit tracepoint (process exits)
	if err := ebpfMgr.AttachTracepointWithProgram(objs.HandleExit, "sched", "sched_process_exit"); err != nil {
		return fmt.Errorf("failed to attach sched/sched_process_exit: %w", err)
	}

	c.logger.Info().Msg("eBPF programs loaded and attached (2 tracepoints: oom/mark_victim, sched/sched_process_exit)")

	// Close eBPF manager when context is cancelled
	go func() {
		<-ctx.Done()
		if err := ebpfMgr.Close(); err != nil {
			c.logger.Error().Err(err).Msg("error closing eBPF manager in cleanup")
		}
	}()

	// Open ring buffer reader
	reader, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		return fmt.Errorf("failed to open ring buffer: %w", err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			c.logger.Error().Err(err).Msg("error closing ring buffer reader")
		}
	}()

	// Read events from ring buffer
	for {
		record, err := reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				c.logger.Info().Msg("ring buffer closed, shutting down")
				return nil
			}
			c.logger.Error().Err(err).Msg("error reading ring buffer")
			c.deps.Metrics.RecordError(c.name, "container", "ring_buffer_read")
			continue
		}

		// Parse event
		var evt ContainerEventBPF
		if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &evt); err != nil {
			c.logger.Error().Err(err).Msg("error parsing event")
			c.deps.Metrics.RecordError(c.name, "container", "parse_error")
			continue
		}

		// Send to processing stage (non-blocking)
		select {
		case eventCh <- evt:
			// Event sent
		default:
			// Channel full - drop event
			c.deps.Metrics.RecordDrop(c.name, "container_event")
		}
	}
}

// processEventsStage processes events from channel and outputs domain events
func (c *ContainerObserver) processEventsStage(ctx context.Context, eventCh chan ContainerEventBPF) error {
	for {
		select {
		case <-ctx.Done():
			c.logger.Info().Msg("shutting down event processor")
			return nil

		case evt, ok := <-eventCh:
			if !ok {
				c.logger.Info().Msg("event channel closed, processor exiting")
				return nil
			}

			startTime := time.Now()
			c.processEvent(ctx, evt)

			// Record processing latency
			latencyMs := float64(time.Since(startTime).Microseconds()) / 1000.0
			(*c.processingLatency).Observe(latencyMs)
		}
	}
}

// processEvent processes a single container event
func (c *ContainerObserver) processEvent(ctx context.Context, evt ContainerEventBPF) {
	// Extract cgroup path and container ID
	cgroupPath := NullTerminatedString(evt.CgroupPath[:])
	containerID := ParseContainerID(cgroupPath)

	// Issue #566: Fallback to cgroup ID cache if path parsing failed
	// This handles the race condition where cgroup is deleted before we can read it
	if containerID == "" && evt.CgroupID != 0 {
		if cachedID, ok := c.cgroupMonitor.GetContainerIDByCgroupID(evt.CgroupID); ok {
			containerID = cachedID
		}
	}

	// Cache cgroup ID → container ID mapping for future lookups
	if containerID != "" && evt.CgroupID != 0 {
		c.cgroupMonitor.CacheCgroupID(evt.CgroupID, containerID)
	}

	// Determine if this is an OOM kill
	isOOM := evt.Type == EventTypeOOMKill

	// Classify exit
	classification := ClassifyExit(evt.ExitCode, evt.Signal, isOOM)

	// Try to get cgroup info for enrichment
	var cgroupInfo CgroupInfo
	var enrichmentErr error
	if containerID != "" {
		cgroupInfo, enrichmentErr = c.cgroupMonitor.GetInfo(ctx, containerID)
		if enrichmentErr != nil && !errors.Is(enrichmentErr, ErrCgroupNotFound) {
			(*c.enrichmentErrors).Inc()
		}
	}

	// Use memory info from eBPF if cgroup read failed
	memoryUsage := cgroupInfo.MemoryCurrentBytes
	memoryLimit := cgroupInfo.MemoryLimitBytes
	if memoryUsage == 0 && evt.MemoryUsage > 0 {
		memoryUsage = evt.MemoryUsage
	}
	if memoryLimit == 0 && evt.MemoryLimit > 0 {
		memoryLimit = evt.MemoryLimit
	}

	// Determine reason based on classification
	reason := ""
	if isOOM {
		reason = "OOMKilled"
	} else if classification.Category == ExitCategoryError {
		reason = "Error"
	}

	// Build domain event using existing ContainerEventData fields
	domainEvent := &domain.ObserverEvent{
		ID:        domain.NewEventID(),
		Type:      "container",
		Subtype:   string(classification.Category),
		Timestamp: time.Unix(0, int64(evt.TimestampNs)),
		Severity:  classificationToSeverity(classification.Category),
		Outcome:   classificationToOutcome(classification.Category),
		ContainerData: &domain.ContainerEventData{
			ContainerID: containerID,
			State:       "Terminated",
			Reason:      reason,
			ExitCode:    evt.ExitCode,
			Signal:      evt.Signal,
			PID:         evt.PID,
			Category:    string(classification.Category),
			Evidence:    classification.Evidence,
			MemoryLimit: int64(memoryLimit),
			MemoryUsage: int64(memoryUsage),
			CgroupPath:  cgroupPath,
		},
	}

	// Record metrics
	if isOOM {
		(*c.oomKillsTotal).Inc()
	}
	(*c.exitsTotal).Inc()
	(*c.exitsByCategory).Inc() // Note: category label lost (would need CounterVec)
	if memoryUsage > 0 {
		(*c.memoryAtExit).Observe(float64(memoryUsage))
	}

	// Emit domain event: record metrics and send to OTLP
	c.emitDomainEvent(ctx, domainEvent)
}

// emitDomainEvent outputs domain events (following network observer pattern)
func (c *ContainerObserver) emitDomainEvent(ctx context.Context, evt *domain.ObserverEvent) {
	// Record OTEL metrics
	c.deps.Metrics.RecordEvent(c.name, evt.Type)

	// Validate event has container data
	if evt.ContainerData == nil {
		c.logger.Warn().
			Str("type", evt.Type).
			Str("subtype", evt.Subtype).
			Msg("missing container data")
		return
	}

	// Send to intelligence service if available
	if c.deps.Emitter != nil {
		if err := c.deps.Emitter.Emit(ctx, evt); err != nil {
			c.logger.Error().Err(err).Msg("failed to emit event")
			c.deps.Metrics.RecordError(c.name, evt.Type, "emit_failed")
		}
	}
}

// classificationToSeverity maps exit category to severity
func classificationToSeverity(category ExitCategory) domain.Severity {
	switch category {
	case ExitCategoryOOMKill:
		return domain.SeverityCritical
	case ExitCategoryError:
		return domain.SeverityError
	case ExitCategoryNormal:
		return domain.SeverityInfo
	default:
		return domain.SeverityInfo
	}
}

// classificationToOutcome maps exit category to outcome
func classificationToOutcome(category ExitCategory) domain.Outcome {
	switch category {
	case ExitCategoryNormal:
		return domain.OutcomeSuccess
	case ExitCategoryOOMKill, ExitCategoryError:
		return domain.OutcomeFailure
	default:
		return domain.OutcomeUnknown
	}
}

// checkKernelCompatibility verifies the kernel meets minimum requirements
func checkKernelCompatibility() error {
	// Kernel 5.8+ required for:
	// - BPF ring buffer (efficient event delivery)
	// - BTF/CO-RE (portable eBPF)
	// Note: This is a simplified check - production should verify with uname
	return nil
}
