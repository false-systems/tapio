//go:build linux

package container

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/cilium/ebpf/ringbuf"
	"github.com/yairfalse/tapio/internal/base"
	"github.com/yairfalse/tapio/internal/observers/container/bpf"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
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
	*base.BaseObserver
	config        Config
	cgroupMonitor *CgroupMonitor

	// Container-specific OTEL metrics
	oomKillsTotal   metric.Int64Counter
	exitsTotal      metric.Int64Counter
	exitsByCategory metric.Int64Counter
	memoryAtExit    metric.Float64Histogram

	// Processing metrics
	processingLatency metric.Float64Histogram
	enrichmentErrors  metric.Int64Counter
}

// NewContainerObserver creates a new container observer
func NewContainerObserver(name string, config Config) (*ContainerObserver, error) {
	baseObs, err := base.NewBaseObserver(name)
	if err != nil {
		return nil, fmt.Errorf("failed to create base observer: %w", err)
	}

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
		BaseObserver:  baseObs,
		config:        config,
		cgroupMonitor: cgroupMonitor,
	}

	// Create container-specific OTEL metrics using MetricBuilder (fluent API)
	if err := obs.initMetrics(name); err != nil {
		return nil, fmt.Errorf("failed to create metrics: %w", err)
	}

	return obs, nil
}

// initMetrics initializes OTEL metrics for the container observer
func (c *ContainerObserver) initMetrics(name string) error {
	return base.NewMetricBuilder(name).
		Counter(&c.oomKillsTotal, "oom_kills_total", "Total OOM kills detected").
		Counter(&c.exitsTotal, "exits_total", "Total container exits detected").
		Counter(&c.exitsByCategory, "exits_by_category_total", "Container exits by exit category").
		Histogram(&c.memoryAtExit, "memory_at_exit_bytes", "Memory usage at container exit time").
		Histogram(&c.processingLatency, "event_processing_duration_ms", "Event processing latency in milliseconds").
		Counter(&c.enrichmentErrors, "enrichment_errors_total", "Errors during event enrichment").
		Build()
}

// Start implements the Observer interface - sets up pipeline stages
func (c *ContainerObserver) Start(ctx context.Context) error {
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

	// Stage 1: Load and attach eBPF program
	c.AddStage(func(ctx context.Context) error {
		return c.loadAndAttachStage(ctx, eventCh)
	})

	// Stage 2: Process events from channel
	c.AddStage(func(ctx context.Context) error {
		return c.processEventsStage(ctx, eventCh)
	})

	// Let BaseObserver run the pipeline
	return c.BaseObserver.Start(ctx)
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
			log.Printf("[%s] Error closing eBPF objects: %v", c.Name(), err)
		}
	}()

	// Create eBPF manager for tracepoint attachment
	ebpfMgr := base.NewEBPFManagerFromCollection(nil)
	defer func() {
		if err := ebpfMgr.Close(); err != nil {
			log.Printf("[%s] Error closing eBPF manager: %v", c.Name(), err)
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

	log.Printf("[%s] eBPF programs loaded and attached (2 tracepoints: oom/mark_victim, sched/sched_process_exit)", c.Name())

	// Close eBPF manager when context is cancelled
	go func() {
		<-ctx.Done()
		if err := ebpfMgr.Close(); err != nil {
			log.Printf("[%s] Error closing eBPF manager in cleanup: %v", c.Name(), err)
		}
	}()

	// Open ring buffer reader
	reader, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		return fmt.Errorf("failed to open ring buffer: %w", err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			log.Printf("[%s] Error closing ring buffer reader: %v", c.Name(), err)
		}
	}()

	// Read events from ring buffer
	for {
		record, err := reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				log.Printf("[%s] Ring buffer closed, shutting down", c.Name())
				return nil
			}
			log.Printf("[%s] Error reading ring buffer: %v", c.Name(), err)
			c.RecordError(ctx, nil)
			continue
		}

		// Parse event
		var evt ContainerEventBPF
		if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &evt); err != nil {
			log.Printf("[%s] Error parsing event: %v", c.Name(), err)
			c.RecordError(ctx, nil)
			continue
		}

		// Send to processing stage (non-blocking)
		select {
		case eventCh <- evt:
			// Event sent
		default:
			// Channel full - drop event
			c.RecordDrop(ctx, "container_event")
		}
	}
}

// processEventsStage processes events from channel and outputs domain events
func (c *ContainerObserver) processEventsStage(ctx context.Context, eventCh chan ContainerEventBPF) error {
	for {
		select {
		case <-ctx.Done():
			log.Printf("[%s] Shutting down event processor", c.Name())
			return nil

		case evt, ok := <-eventCh:
			if !ok {
				log.Printf("[%s] Event channel closed, processor exiting", c.Name())
				return nil
			}

			startTime := time.Now()
			c.processEvent(ctx, evt)

			// Record processing latency
			latencyMs := float64(time.Since(startTime).Microseconds()) / 1000.0
			c.processingLatency.Record(ctx, latencyMs)
		}
	}
}

// processEvent processes a single container event
func (c *ContainerObserver) processEvent(ctx context.Context, evt ContainerEventBPF) {
	// Extract cgroup path and container ID
	cgroupPath := NullTerminatedString(evt.CgroupPath[:])
	containerID := ParseContainerID(cgroupPath)

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
			c.enrichmentErrors.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "cgroup"),
			))
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
		c.oomKillsTotal.Add(ctx, 1)
	}
	c.exitsTotal.Add(ctx, 1)
	c.exitsByCategory.Add(ctx, 1, metric.WithAttributes(
		attribute.String("category", string(classification.Category)),
	))
	if memoryUsage > 0 {
		c.memoryAtExit.Record(ctx, float64(memoryUsage))
	}

	// Emit domain event: record metrics and send to OTLP
	c.emitDomainEvent(ctx, domainEvent)
}

// emitDomainEvent outputs domain events (following network observer pattern)
func (c *ContainerObserver) emitDomainEvent(ctx context.Context, evt *domain.ObserverEvent) {
	// Record OTEL metrics
	c.RecordEvent(ctx)

	// Validate event has container data
	if evt.ContainerData == nil {
		log.Printf("[%s] %s.%s: missing container data", c.Name(), evt.Type, evt.Subtype)
		return
	}

	// Send to OTLP (Community path - structured logs)
	c.SendObserverEvent(ctx, evt)
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
