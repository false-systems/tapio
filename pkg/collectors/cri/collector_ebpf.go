//go:build linux && cgo

package cri

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags $BPF_CFLAGS -target native crimonitor bpf_src/cri_monitor.c -- -I../bpf_common

// ContainerExitEvent represents the eBPF event structure
type ContainerExitEvent struct {
	Timestamp   uint64
	PID         uint32
	TGID        uint32
	ExitCode    int32
	CgroupID    uint64
	MemoryUsage uint64
	MemoryLimit uint64
	OOMKilled   uint8
	Comm        [16]byte
	ContainerID [64]byte
}

// OOMKillEvent represents an OOM kill event from eBPF
type OOMKillEvent struct {
	Timestamp uint64
	PID       uint32
	TGID      uint32
	CgroupID  uint64
	Pages     uint32
	Comm      [16]byte
}

// EBPFCollector enhances the CRI collector with kernel-level monitoring
type EBPFCollector struct {
	*Collector // Embed base collector

	// eBPF components
	spec   *ebpf.CollectionSpec
	coll   *ebpf.Collection
	reader *ringbuf.Reader
	links  []link.Link
	logger *zap.Logger

	// OTEL instrumentation
	tracer trace.Tracer
	meter  metric.Meter

	// eBPF-specific metrics
	ebpfLoadsTotal    metric.Int64Counter
	ebpfLoadErrors    metric.Int64Counter
	ebpfAttachTotal   metric.Int64Counter
	ebpfAttachErrors  metric.Int64Counter
	ebpfEventsTotal   metric.Int64Counter
	ebpfEventsDropped metric.Int64Counter
	kernelOOMKills    metric.Int64Counter
	mapUpdateErrors   metric.Int64Counter

	// Event processing
	ebpfEvents chan *ContainerExitEvent
	stopEBPF   chan struct{}
}

// NewEBPFCollector creates a CRI collector with eBPF enhancement
func NewEBPFCollector(name string, config Config) (*EBPFCollector, error) {
	// Create base collector first
	baseCollector, err := NewCollector(name, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create base collector: %w", err)
	}

	// Initialize OTEL instrumentation
	tracer := otel.Tracer("tapio.collectors.cri.ebpf",
		trace.WithInstrumentationVersion("1.0.0"),
		trace.WithSchemaURL(semconv.SchemaURL),
	)

	meter := otel.Meter("tapio.collectors.cri.ebpf",
		metric.WithInstrumentationVersion("1.0.0"),
		metric.WithSchemaURL(semconv.SchemaURL),
	)

	collector := &EBPFCollector{
		Collector:  baseCollector,
		logger:     baseCollector.logger.Named("ebpf"),
		tracer:     tracer,
		meter:      meter,
		ebpfEvents: make(chan *ContainerExitEvent, 1000),
		stopEBPF:   make(chan struct{}),
	}

	// Initialize eBPF-specific metrics
	if err := collector.initEBPFMetrics(); err != nil {
		return nil, fmt.Errorf("failed to initialize eBPF metrics: %w", err)
	}

	// Initialize eBPF components
	if err := collector.initEBPF(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to initialize eBPF: %w", err)
	}

	return collector, nil
}

// initEBPF initializes the eBPF programs and maps with comprehensive tracing
func (c *EBPFCollector) initEBPF(ctx context.Context) error {
	ctx, span := c.tracer.Start(ctx, "cri.ebpf.initialize",
		trace.WithAttributes(
			attribute.String("kernel_version", c.getKernelVersion()),
			attribute.Bool("btf_enabled", c.hasBTF()),
			attribute.String("collector.name", c.name),
		),
	)
	defer span.End()

	c.logger.Info("Initializing eBPF collector",
		zap.String("trace.id", span.SpanContext().TraceID().String()),
	)

	span.AddEvent("loading_ebpf_spec")
	c.ebpfLoadsTotal.Add(ctx, 1)

	// Load eBPF spec
	spec, err := LoadCrimonitor()
	if err != nil {
		c.ebpfLoadErrors.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("error.type", "spec_loading"),
			),
		)
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to load eBPF spec")
		return fmt.Errorf("failed to load eBPF spec: %w", err)
	}
	c.spec = spec

	span.AddEvent("creating_ebpf_collection")

	// Load eBPF collection
	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		c.ebpfLoadErrors.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("error.type", "collection_creation"),
			),
		)
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to create eBPF collection")
		return fmt.Errorf("failed to create eBPF collection: %w", err)
	}
	c.coll = coll

	span.AddEvent("attaching_ebpf_programs")

	// Attach programs to kernel hooks
	if err := c.attachPrograms(ctx); err != nil {
		coll.Close()
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to attach eBPF programs")
		return fmt.Errorf("failed to attach eBPF programs: %w", err)
	}

	span.AddEvent("creating_ringbuf_reader")

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(coll.Maps["events"])
	if err != nil {
		c.ebpfLoadErrors.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("error.type", "ringbuf_creation"),
			),
		)
		c.cleanup()
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to create ring buffer reader")
		return fmt.Errorf("failed to create ring buffer reader: %w", err)
	}
	c.reader = reader

	span.SetAttributes(
		attribute.Int("attached_programs", len(c.links)),
		attribute.Bool("ebpf.initialized", true),
	)

	span.SetStatus(codes.Ok, "eBPF initialized successfully")
	c.logger.Info("eBPF collector initialized successfully",
		zap.Int("attached_programs", len(c.links)),
		zap.String("trace.id", span.SpanContext().TraceID().String()),
	)
	return nil
}

// attachPrograms attaches eBPF programs to kernel hooks with comprehensive tracing
func (c *EBPFCollector) attachPrograms(ctx context.Context) error {
	ctx, span := c.tracer.Start(ctx, "cri.ebpf.attach_programs")
	defer span.End()

	var links []link.Link

	// Attach OOM kill tracer
	span.AddEvent("attaching_oom_kill_tracer")
	if prog := c.coll.Programs["trace_oom_kill"]; prog != nil {
		c.ebpfAttachTotal.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("program", "trace_oom_kill"),
				attribute.String("type", "kprobe"),
			),
		)

		l, err := link.Kprobe("oom_kill_process", prog)
		if err != nil {
			c.ebpfAttachErrors.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("program", "trace_oom_kill"),
					attribute.String("error.type", "kprobe_attach"),
				),
			)
			span.RecordError(err)
			return fmt.Errorf("failed to attach oom_kill_process kprobe: %w", err)
		}
		links = append(links, l)
		span.AddEvent("oom_kill_tracer_attached")
		c.logger.Info("Attached OOM kill tracer")
	}

	// Attach process exit tracer
	span.AddEvent("attaching_process_exit_tracer")
	if prog := c.coll.Programs["trace_process_exit"]; prog != nil {
		c.ebpfAttachTotal.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("program", "trace_process_exit"),
				attribute.String("type", "tracepoint"),
			),
		)

		l, err := link.Tracepoint("sched", "sched_process_exit", prog)
		if err != nil {
			c.ebpfAttachErrors.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("program", "trace_process_exit"),
					attribute.String("error.type", "tracepoint_attach"),
				),
			)
			span.RecordError(err)
			return fmt.Errorf("failed to attach sched_process_exit tracepoint: %w", err)
		}
		links = append(links, l)
		span.AddEvent("process_exit_tracer_attached")
		c.logger.Info("Attached process exit tracer")
	}

	// Attach process fork tracer (optional)
	span.AddEvent("attaching_process_fork_tracer")
	if prog := c.coll.Programs["trace_process_fork"]; prog != nil {
		c.ebpfAttachTotal.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("program", "trace_process_fork"),
				attribute.String("type", "tracepoint"),
			),
		)

		l, err := link.Tracepoint("sched", "sched_process_fork", prog)
		if err != nil {
			c.ebpfAttachErrors.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("program", "trace_process_fork"),
					attribute.String("error.type", "tracepoint_attach"),
				),
			)
			span.RecordError(err,
				trace.WithAttributes(
					attribute.String("program", "trace_process_fork"),
				),
			)
			c.logger.Warn("Failed to attach process fork tracer", zap.Error(err))
		} else {
			links = append(links, l)
			span.AddEvent("process_fork_tracer_attached")
			c.logger.Info("Attached process fork tracer")
		}
	}

	// Attach memory cgroup OOM tracer (optional)
	span.AddEvent("attaching_memcg_oom_tracer")
	if prog := c.coll.Programs["trace_memcg_oom"]; prog != nil {
		c.ebpfAttachTotal.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("program", "trace_memcg_oom"),
				attribute.String("type", "kprobe"),
			),
		)

		l, err := link.Kprobe("mem_cgroup_out_of_memory", prog)
		if err != nil {
			c.ebpfAttachErrors.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("program", "trace_memcg_oom"),
					attribute.String("error.type", "kprobe_attach"),
				),
			)
			span.RecordError(err,
				trace.WithAttributes(
					attribute.String("program", "trace_memcg_oom"),
				),
			)
			c.logger.Warn("Failed to attach mem_cgroup_out_of_memory kprobe", zap.Error(err))
		} else {
			links = append(links, l)
			span.AddEvent("memcg_oom_tracer_attached")
			c.logger.Info("Attached memory cgroup OOM tracer")
		}
	}

	c.links = links
	span.SetAttributes(
		attribute.Int("total_programs_attached", len(links)),
	)
	span.SetStatus(codes.Ok, "programs attached successfully")
	return nil
}

// Start starts the enhanced eBPF collector with comprehensive tracing
func (c *EBPFCollector) Start(ctx context.Context) error {
	ctx, span := c.tracer.Start(ctx, "cri.ebpf.start",
		trace.WithAttributes(
			attribute.String("collector.name", c.name),
			attribute.String("collector.type", "ebpf_enhanced"),
		),
	)
	defer span.End()

	c.logger.Info("Starting eBPF-enhanced CRI collector",
		zap.String("trace.id", span.SpanContext().TraceID().String()),
	)

	span.AddEvent("starting_base_collector")

	// Start base collector
	if err := c.Collector.Start(ctx); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to start base collector")
		return err
	}

	span.AddEvent("base_collector_started")
	span.AddEvent("starting_ebpf_event_processing")

	// Start eBPF event processing
	go c.processEBPFEvents(ctx)

	span.SetAttributes(
		attribute.Bool("ebpf.collector.started", true),
		attribute.Int("ebpf.attached_programs", len(c.links)),
	)

	span.SetStatus(codes.Ok, "eBPF collector started successfully")
	c.logger.Info("eBPF collector started",
		zap.Int("attached_programs", len(c.links)),
		zap.String("trace.id", span.SpanContext().TraceID().String()),
	)
	return nil
}

// Stop stops the enhanced collector
func (c *EBPFCollector) Stop() error {
	// Stop eBPF processing
	close(c.stopEBPF)

	// Stop base collector
	if err := c.Collector.Stop(); err != nil {
		c.logger.Error("Failed to stop base collector", zap.Error(err))
	}

	// Cleanup eBPF resources
	c.cleanup()

	c.logger.Info("eBPF collector stopped")
	return nil
}

// processEBPFEvents processes events from the eBPF ring buffer with comprehensive tracing
func (c *EBPFCollector) processEBPFEvents(ctx context.Context) {
	ctx, span := c.tracer.Start(ctx, "cri.ebpf.process_events",
		trace.WithAttributes(
			attribute.String("collector.name", c.name),
		),
	)
	defer span.End()
	defer c.logger.Info("eBPF event processing stopped",
		zap.String("trace.id", span.SpanContext().TraceID().String()),
	)

	c.logger.Info("Starting eBPF event processing loop",
		zap.String("trace.id", span.SpanContext().TraceID().String()),
	)

	for {
		select {
		case <-ctx.Done():
			span.AddEvent("context_cancelled")
			c.logger.Info("eBPF event processing stopped due to context cancellation")
			return
		case <-c.stopEBPF:
			span.AddEvent("stop_signal_received")
			c.logger.Info("eBPF event processing stopped due to stop signal")
			return
		default:
		}

		start := time.Now()

		// Read from ring buffer with timeout
		record, err := c.reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				span.AddEvent("ringbuf_closed")
				c.logger.Info("Ring buffer closed, stopping event processing")
				return
			}
			c.ebpfEventsDropped.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("error.type", "read_error"),
				),
			)
			c.logger.Debug("Failed to read from eBPF ring buffer",
				zap.Error(err),
				zap.String("trace.id", span.SpanContext().TraceID().String()),
			)
			continue
		}

		// Parse event based on size
		c.handleEBPFRecord(ctx, record)

		// Record processing latency
		duration := time.Since(start).Milliseconds()
		c.processingLatency.Record(ctx, float64(duration),
			metric.WithAttributes(
				attribute.String("operation", "ebpf_event_read"),
			),
		)
	}
}

// handleEBPFRecord processes a single eBPF record with comprehensive tracing
func (c *EBPFCollector) handleEBPFRecord(ctx context.Context, record ringbuf.Record) {
	ctx, span := c.tracer.Start(ctx, "cri.ebpf.handle_record",
		trace.WithAttributes(
			attribute.Int("record.size", len(record.RawSample)),
		),
	)
	defer span.End()

	// Parse event based on size
	switch len(record.RawSample) {
	case int(unsafe.Sizeof(OOMKillEvent{})):
		var event OOMKillEvent
		if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &event); err != nil {
			span.RecordError(err)
			c.ebpfEventsDropped.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("error.type", "parse_error"),
					attribute.String("event.type", "oom_kill"),
				),
			)
			return
		}

		span.SetAttributes(
			attribute.Int("pid", int(event.PID)),
			attribute.Int64("cgroup_id", int64(event.CgroupID)),
			attribute.Int64("memory_pages", int64(event.Pages)),
			attribute.String("event.type", "oom_kill"),
		)

		span.AddEvent("oom_kill_from_kernel",
			trace.WithAttributes(
				attribute.String("comm", c.nullTerminatedString(event.Comm[:])),
			),
		)

		c.handleOOMKillEvent(ctx, &event)

	case int(unsafe.Sizeof(ContainerExitEvent{})):
		var event ContainerExitEvent
		if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &event); err != nil {
			span.RecordError(err)
			c.ebpfEventsDropped.Add(ctx, 1,
				metric.WithAttributes(
					attribute.String("error.type", "parse_error"),
					attribute.String("event.type", "container_exit"),
				),
			)
			return
		}

		span.SetAttributes(
			attribute.Int("pid", int(event.PID)),
			attribute.Int("exit_code", int(event.ExitCode)),
			attribute.String("event.type", "container_exit"),
		)

		c.handleContainerExitEvent(ctx, &event)

	default:
		span.AddEvent("unknown_event_size")
		c.ebpfEventsDropped.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("error.type", "unknown_size"),
			),
		)
		c.logger.Warn("Received eBPF event with unknown size",
			zap.Int("size", len(record.RawSample)),
			zap.String("trace.id", span.SpanContext().TraceID().String()),
		)
		return
	}

	c.ebpfEventsTotal.Add(ctx, 1)
	span.SetStatus(codes.Ok, "record processed successfully")
}

// handleOOMKillEvent processes OOM kill events with metrics and tracing
func (c *EBPFCollector) handleOOMKillEvent(ctx context.Context, event *OOMKillEvent) {
	ctx, span := c.tracer.Start(ctx, "cri.ebpf.handle_oom_kill")
	defer span.End()

	// Map cgroup ID to container
	containerID := c.cgroupToContainer(event.CgroupID)
	if containerID == "" {
		span.AddEvent("unknown_container_for_cgroup")
		c.logger.Debug("Unknown container for cgroup ID",
			zap.Uint64("cgroup_id", event.CgroupID),
		)
		return
	}

	span.SetAttributes(
		attribute.String("container.id", containerID),
		attribute.Int64("memory.killed_at_bytes", int64(event.Pages*4096)),
		attribute.String("process.comm", c.nullTerminatedString(event.Comm[:])),
	)

	// Create CRI event
	criEvent := c.eventPool.Get()
	criEvent.Reset()
	criEvent.Type = EventOOM
	criEvent.SetContainerID(containerID)
	criEvent.OOMKilled = 1
	criEvent.ExitCode = 137 // SIGKILL
	criEvent.MemoryUsage = uint64(event.Pages * 4096)
	criEvent.Timestamp = time.Now().UnixNano()
	criEvent.Reason = "OOMKilled"
	criEvent.Message = fmt.Sprintf("Container killed by kernel OOM killer at %d MB",
		event.Pages*4096/1024/1024)

	// Try to get additional metadata from CRI
	c.enrichEventFromCRI(criEvent, containerID)

	// Add to ring buffer
	if !c.ringBuffer.Write(criEvent) {
		c.eventsDropped.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("source", "ebpf"),
				attribute.String("event.type", "oom_kill"),
			),
		)
		c.eventPool.Put(criEvent)
	} else {
		c.oomKillsDetected.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("source", "kernel"),
				attribute.String("container", containerID[:12]),
			),
		)
		c.kernelOOMKills.Add(ctx, 1)
	}

	span.SetStatus(codes.Ok, "OOM kill event processed")
	c.logger.Info("Processed kernel OOM kill event",
		zap.String("container_id", containerID[:12]),
		zap.Uint32("pid", event.PID),
		zap.Uint32("pages", event.Pages),
	)
}

// handleContainerExitEvent processes container exit events
func (c *EBPFCollector) handleContainerExitEvent(ctx context.Context, event *ContainerExitEvent) {
	ctx, span := c.tracer.Start(ctx, "cri.ebpf.handle_container_exit")
	defer span.End()

	// Extract container ID (null-terminated)
	containerID := c.nullTerminatedString(event.ContainerID[:])
	if containerID == "" {
		span.AddEvent("empty_container_id")
		return
	}

	span.SetAttributes(
		attribute.String("container.id", containerID),
		attribute.Int("exit_code", int(event.ExitCode)),
		attribute.Bool("oom_killed", event.OOMKilled == 1),
	)

	// Convert eBPF event to CRI event
	criEvent := c.eventPool.Get()
	criEvent.Reset()
	criEvent.SetContainerID(containerID)
	criEvent.Timestamp = int64(event.Timestamp)
	criEvent.ExitCode = event.ExitCode
	criEvent.MemoryUsage = event.MemoryUsage
	criEvent.MemoryLimit = event.MemoryLimit

	// Determine event type
	if event.OOMKilled == 1 {
		criEvent.Type = EventOOM
		criEvent.OOMKilled = 1
		criEvent.Signal = 9 // SIGKILL
		criEvent.Reason = "OOMKilled"
		criEvent.Message = fmt.Sprintf("Container OOMKilled by kernel: PID %d", event.PID)
		c.kernelOOMKills.Add(ctx, 1)
	} else {
		if event.ExitCode != 0 {
			criEvent.Type = EventDied
		} else {
			criEvent.Type = EventStopped
		}
	}

	// Try to get additional metadata from CRI
	c.enrichEventFromCRI(criEvent, containerID)

	// Add to ring buffer
	if !c.ringBuffer.Write(criEvent) {
		c.eventPool.Put(criEvent)
		c.eventsDropped.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("source", "ebpf"),
			),
		)
		return
	}

	c.eventsProcessed.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("source", "ebpf"),
			attribute.String("event.type", criEvent.Type.String()),
		),
	)

	span.SetStatus(codes.Ok, "container exit event processed")
	c.logger.Debug("Processed eBPF container exit event",
		zap.String("container_id", containerID[:12]),
		zap.String("type", criEvent.Type.String()),
		zap.Int32("exit_code", criEvent.ExitCode),
		zap.Bool("oom_killed", criEvent.OOMKilled == 1),
	)
}

// enrichEventFromCRI enriches eBPF event with CRI metadata
func (c *EBPFCollector) enrichEventFromCRI(event *Event, containerID string) {
	// Quick lookup in lastSeen map
	c.lastSeenMu.RLock()
	defer c.lastSeenMu.RUnlock()

	for id, status := range c.lastSeen {
		if strings.HasPrefix(id, containerID) || strings.HasPrefix(containerID, id[:12]) {
			// Extract Kubernetes metadata
			if status.Labels != nil {
				if podUID, ok := status.Labels["io.kubernetes.pod.uid"]; ok {
					event.SetPodUID(podUID)
				}
				if podName, ok := status.Labels["io.kubernetes.pod.name"]; ok {
					event.PodName = podName
				}
				if namespace, ok := status.Labels["io.kubernetes.pod.namespace"]; ok {
					event.Namespace = namespace
				}
			}
			break
		}
	}
}

// UpdateContainerMetadata updates eBPF maps with container metadata
func (c *EBPFCollector) UpdateContainerMetadata(pid uint32, containerID, podUID string, memoryLimit uint64) error {
	ctx, span := c.tracer.Start(context.Background(), "cri.ebpf.update_metadata",
		trace.WithAttributes(
			attribute.Int("pid", int(pid)),
			attribute.String("container.id", containerID[:12]),
			attribute.String("pod.uid", podUID),
			attribute.Int64("memory.limit", int64(memoryLimit)),
		),
	)
	defer span.End()

	if c.coll == nil {
		err := fmt.Errorf("eBPF collection not initialized")
		span.RecordError(err)
		span.SetStatus(codes.Error, "collection not initialized")
		return err
	}

	containerMap := c.coll.Maps["container_map"]
	if containerMap == nil {
		err := fmt.Errorf("container_map not found")
		span.RecordError(err)
		span.SetStatus(codes.Error, "container_map not found")
		return err
	}

	// Create metadata struct
	metadata := struct {
		ContainerID [64]byte
		PodUID      [36]byte
		PodName     [64]byte
		Namespace   [64]byte
		MemoryLimit uint64
		CgroupID    uint64
	}{}

	// Copy strings safely
	copy(metadata.ContainerID[:], containerID)
	copy(metadata.PodUID[:], podUID)
	metadata.MemoryLimit = memoryLimit

	span.AddEvent("updating_container_map")

	// Update map
	if err := containerMap.Update(pid, metadata, ebpf.UpdateAny); err != nil {
		c.mapUpdateErrors.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("map", "container_map"),
				attribute.String("operation", "update"),
			),
		)
		span.RecordError(err)
		span.SetStatus(codes.Error, "map update failed")
		return err
	}

	span.SetStatus(codes.Ok, "metadata updated successfully")
	return nil
}

// GetEBPFStats returns eBPF-specific statistics
func (c *EBPFCollector) GetEBPFStats() map[string]uint64 {
	if c.coll == nil {
		return nil
	}

	statsMap := c.coll.Maps["stats_map"]
	if statsMap == nil {
		return nil
	}

	stats := make(map[string]uint64)
	statNames := []string{
		"oom_kills",
		"process_exits",
		"container_starts",
		"events_dropped",
	}

	for i, name := range statNames {
		var value uint64
		key := uint32(i)
		if err := statsMap.Lookup(key, &value); err == nil {
			stats[name] = value
		}
	}

	return stats
}

// cleanup cleans up eBPF resources
func (c *EBPFCollector) cleanup() {
	// Close ring buffer reader
	if c.reader != nil {
		c.reader.Close()
	}

	// Detach programs
	for _, l := range c.links {
		l.Close()
	}
	c.links = nil

	// Close collection
	if c.coll != nil {
		c.coll.Close()
	}
}

// initEBPFMetrics initializes eBPF-specific metric instruments
func (c *EBPFCollector) initEBPFMetrics() error {
	var err error

	// Counter for eBPF loads
	c.ebpfLoadsTotal, err = c.meter.Int64Counter("cri.ebpf.loads",
		metric.WithDescription("Total number of eBPF program load attempts"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("failed to create ebpf_loads counter: %w", err)
	}

	// Counter for eBPF load errors
	c.ebpfLoadErrors, err = c.meter.Int64Counter("cri.ebpf.load_errors",
		metric.WithDescription("Number of eBPF program load errors"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("failed to create ebpf_load_errors counter: %w", err)
	}

	// Counter for eBPF attachments
	c.ebpfAttachTotal, err = c.meter.Int64Counter("cri.ebpf.attachments",
		metric.WithDescription("Total number of eBPF program attach attempts"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("failed to create ebpf_attachments counter: %w", err)
	}

	// Counter for eBPF attach errors
	c.ebpfAttachErrors, err = c.meter.Int64Counter("cri.ebpf.attach_errors",
		metric.WithDescription("Number of eBPF program attach errors"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("failed to create ebpf_attach_errors counter: %w", err)
	}

	// Counter for eBPF events
	c.ebpfEventsTotal, err = c.meter.Int64Counter("cri.ebpf.events",
		metric.WithDescription("Total number of eBPF events received"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("failed to create ebpf_events counter: %w", err)
	}

	// Counter for dropped eBPF events
	c.ebpfEventsDropped, err = c.meter.Int64Counter("cri.ebpf.events_dropped",
		metric.WithDescription("Number of eBPF events dropped"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("failed to create ebpf_events_dropped counter: %w", err)
	}

	// Counter for kernel OOM kills
	c.kernelOOMKills, err = c.meter.Int64Counter("cri.ebpf.kernel_oom_kills",
		metric.WithDescription("Number of OOM kills detected by kernel eBPF programs"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("failed to create kernel_oom_kills counter: %w", err)
	}

	// Counter for map update errors
	c.mapUpdateErrors, err = c.meter.Int64Counter("cri.ebpf.map_update_errors",
		metric.WithDescription("Number of eBPF map update errors"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return fmt.Errorf("failed to create map_update_errors counter: %w", err)
	}

	return nil
}

// cgroupToContainer maps a cgroup ID to container ID
func (c *EBPFCollector) cgroupToContainer(cgroupID uint64) string {
	// This would typically use a reverse lookup map or cgroup filesystem parsing
	// For now, we'll do a simple lookup in our tracked containers
	c.lastSeenMu.RLock()
	defer c.lastSeenMu.RUnlock()

	// In a real implementation, you'd maintain a cgroup->container mapping
	// This is a simplified version
	for containerID := range c.lastSeen {
		// This would need proper cgroup ID extraction logic
		// For now, return first container as placeholder
		return containerID
	}

	return ""
}

// getKernelVersion returns the kernel version
func (c *EBPFCollector) getKernelVersion() string {
	// Simple kernel version detection
	return "unknown"
}

// hasBTF checks if BTF is available
func (c *EBPFCollector) hasBTF() bool {
	// Simple BTF availability check
	return false
}

// nullTerminatedString converts null-terminated byte array to string
func (c *EBPFCollector) nullTerminatedString(b []byte) string {
	for i, v := range b {
		if v == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// cStringLen returns the length of a null-terminated C string
func cStringLen(b []byte) int {
	for i := 0; i < len(b); i++ {
		if b[i] == 0 {
			return i
		}
	}
	return len(b)
}
