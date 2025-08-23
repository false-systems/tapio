//go:build linux
// +build linux

package syscallerrors

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 syscallmonitor ./bpf_src/syscall_monitor.c -- -I./bpf_src

// SyscallErrorEvent represents a syscall error captured by eBPF
type SyscallErrorEvent struct {
	TimestampNs uint64
	PID         uint32
	PPID        uint32
	TID         uint32
	UID         uint32
	GID         uint32
	CgroupID    uint64
	SyscallNr   int32
	ErrorCode   int32
	Category    uint8
	_pad        [3]uint8
	Comm        [16]byte
	Path        [256]byte
	SrcIP       uint32
	DstIP       uint32
	SrcPort     uint16
	DstPort     uint16
	Arg1        uint64
	Arg2        uint64
	Arg3        uint64
	ErrorCount  uint32
	_pad2       uint32
}

// CollectorStats represents collector statistics
type CollectorStats struct {
	TotalErrors       uint64
	ENOSPCCount       uint64
	ENOMEMCount       uint64
	ECONNREFUSEDCount uint64
	EIOCount          uint64
	EventsSent        uint64
	EventsDropped     uint64
}

// Collector implements the syscall error collector
type Collector struct {
	name   string
	logger *zap.Logger
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// eBPF components
	objs   *syscallmonitorObjects
	links  []link.Link
	reader *ringbuf.Reader

	// Event processing
	eventChan chan *domain.ObservationEvent
	stopOnce  sync.Once

	// OpenTelemetry instrumentation
	tracer          trace.Tracer
	eventsProcessed metric.Int64Counter
	errorsTotal     metric.Int64Counter
	processingTime  metric.Float64Histogram
	enospcErrors    metric.Int64Counter
	enomemErrors    metric.Int64Counter
	econnrefErrors  metric.Int64Counter

	// Configuration
	config *Config
}

// Config holds collector configuration
type Config struct {
	RingBufferSize    int
	EventChannelSize  int
	RateLimitMs       int
	EnabledCategories []string
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		RingBufferSize:    8 * 1024 * 1024, // 8MB
		EventChannelSize:  10000,
		RateLimitMs:       100,
		EnabledCategories: []string{"file", "network", "memory"},
	}
}

// NewCollector creates a new syscall error collector
func NewCollector(logger *zap.Logger, config *Config) (*Collector, error) {
	if config == nil {
		config = DefaultConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Initialize OpenTelemetry
	tracer := otel.Tracer("syscall-errors-collector")
	meter := otel.Meter("syscall-errors-collector")

	eventsProcessed, err := meter.Int64Counter(
		"syscall_errors_events_processed_total",
		metric.WithDescription("Total syscall error events processed"),
	)
	if err != nil {
		logger.Warn("Failed to create events counter", zap.Error(err))
	}

	errorsTotal, err := meter.Int64Counter(
		"syscall_errors_collector_errors_total",
		metric.WithDescription("Total errors in syscall error collector"),
	)
	if err != nil {
		logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	processingTime, err := meter.Float64Histogram(
		"syscall_errors_processing_duration_ms",
		metric.WithDescription("Processing duration for syscall errors in milliseconds"),
	)
	if err != nil {
		logger.Warn("Failed to create processing time histogram", zap.Error(err))
	}

	enospcErrors, err := meter.Int64Counter(
		"syscall_errors_enospc_total",
		metric.WithDescription("Total ENOSPC errors captured"),
	)
	if err != nil {
		logger.Warn("Failed to create ENOSPC counter", zap.Error(err))
	}

	enomemErrors, err := meter.Int64Counter(
		"syscall_errors_enomem_total",
		metric.WithDescription("Total ENOMEM errors captured"),
	)
	if err != nil {
		logger.Warn("Failed to create ENOMEM counter", zap.Error(err))
	}

	econnrefErrors, err := meter.Int64Counter(
		"syscall_errors_econnrefused_total",
		metric.WithDescription("Total ECONNREFUSED errors captured"),
	)
	if err != nil {
		logger.Warn("Failed to create ECONNREFUSED counter", zap.Error(err))
	}

	return &Collector{
		name:            "syscall-errors",
		logger:          logger,
		ctx:             ctx,
		cancel:          cancel,
		eventChan:       make(chan *domain.ObservationEvent, config.EventChannelSize),
		tracer:          tracer,
		eventsProcessed: eventsProcessed,
		errorsTotal:     errorsTotal,
		processingTime:  processingTime,
		enospcErrors:    enospcErrors,
		enomemErrors:    enomemErrors,
		econnrefErrors:  econnrefErrors,
		config:          config,
	}, nil
}

// Start begins collecting syscall errors
func (c *Collector) Start(ctx context.Context) error {
	ctx, span := c.tracer.Start(ctx, "syscall_errors.start")
	defer span.End()

	c.logger.Info("Starting syscall error collector")

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		c.logger.Warn("Failed to remove memory limit", zap.Error(err))
	}

	// Load eBPF programs
	objs := &syscallmonitorObjects{}
	if err := loadSyscallmonitorObjects(objs, nil); err != nil {
		span.SetAttributes(attribute.String("error", err.Error()))
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "ebpf_load_failed"),
			))
		}
		return fmt.Errorf("loading eBPF objects: %w", err)
	}
	c.objs = objs

	// Attach tracepoints
	if err := c.attachTracepoints(); err != nil {
		objs.Close()
		return fmt.Errorf("attaching tracepoints: %w", err)
	}

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		c.cleanup()
		return fmt.Errorf("creating ring buffer reader: %w", err)
	}
	c.reader = reader

	// Start event processing
	c.wg.Add(1)
	go c.processEvents()

	c.logger.Info("Syscall error collector started successfully")
	return nil
}

// attachTracepoints attaches the eBPF programs to kernel tracepoints
func (c *Collector) attachTracepoints() error {
	// Attach sys_enter tracepoint
	enterLink, err := link.Tracepoint("raw_syscalls", "sys_enter", c.objs.TraceSysEnter, nil)
	if err != nil {
		return fmt.Errorf("attaching sys_enter tracepoint: %w", err)
	}
	c.links = append(c.links, enterLink)

	// Attach sys_exit tracepoint
	exitLink, err := link.Tracepoint("raw_syscalls", "sys_exit", c.objs.TraceSysExit, nil)
	if err != nil {
		return fmt.Errorf("attaching sys_exit tracepoint: %w", err)
	}
	c.links = append(c.links, exitLink)

	return nil
}

// processEvents reads and processes events from the ring buffer
func (c *Collector) processEvents() {
	defer c.wg.Done()

	for {
		select {
		case <-c.ctx.Done():
			c.logger.Info("Stopping syscall error event processing")
			return
		default:
			record, err := c.reader.Read()
			if err != nil {
				if errors.Is(err, ringbuf.ErrClosed) {
					return
				}
				if c.errorsTotal != nil {
					c.errorsTotal.Add(c.ctx, 1, metric.WithAttributes(
						attribute.String("error_type", "ring_buffer_read"),
					))
				}
				c.logger.Error("Failed to read from ring buffer", zap.Error(err))
				continue
			}

			// Process the event
			if err := c.processRawEvent(record.RawSample); err != nil {
				c.logger.Warn("Failed to process event", zap.Error(err))
				if c.errorsTotal != nil {
					c.errorsTotal.Add(c.ctx, 1, metric.WithAttributes(
						attribute.String("error_type", "event_processing_failed"),
					))
				}
			}
		}
	}
}

// processRawEvent processes a single raw eBPF event
func (c *Collector) processRawEvent(data []byte) error {
	ctx, span := c.tracer.Start(c.ctx, "syscall_errors.process_event")
	defer span.End()

	start := time.Now()
	defer func() {
		if c.processingTime != nil {
			duration := time.Since(start).Seconds() * 1000
			c.processingTime.Record(ctx, duration)
		}
	}()

	// Parse the event
	if len(data) < int(unsafe.Sizeof(SyscallErrorEvent{})) {
		return fmt.Errorf("event data too small: got %d bytes", len(data))
	}

	var event SyscallErrorEvent
	reader := bytes.NewReader(data)
	if err := binary.Read(reader, binary.LittleEndian, &event); err != nil {
		return fmt.Errorf("failed to parse event: %w", err)
	}

	// Convert to ObservationEvent
	obsEvent := c.convertToObservationEvent(&event)

	// Update metrics
	if c.eventsProcessed != nil {
		c.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
			attribute.String("error_type", c.getErrorName(event.ErrorCode)),
			attribute.String("category", c.getCategoryName(event.Category)),
		))
	}

	// Update specific error counters
	c.updateErrorMetrics(ctx, event.ErrorCode)

	// Send to event channel
	select {
	case c.eventChan <- obsEvent:
		span.SetAttributes(
			attribute.String("syscall", c.getSyscallName(event.SyscallNr)),
			attribute.String("error", c.getErrorName(event.ErrorCode)),
			attribute.Int("pid", int(event.PID)),
		)
	case <-c.ctx.Done():
		return nil
	default:
		// Channel full, drop event
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "channel_full"),
			))
		}
	}

	return nil
}

// convertToObservationEvent converts eBPF event to domain ObservationEvent
func (c *Collector) convertToObservationEvent(event *SyscallErrorEvent) *domain.ObservationEvent {
	timestamp := time.Unix(0, int64(event.TimestampNs))

	// Extract strings from fixed-size arrays
	comm := bytesToString(event.Comm[:])
	path := bytesToString(event.Path[:])

	// Build context map
	context := domain.ObservationContext{
		"pid":         fmt.Sprintf("%d", event.PID),
		"ppid":        fmt.Sprintf("%d", event.PPID),
		"tid":         fmt.Sprintf("%d", event.TID),
		"uid":         fmt.Sprintf("%d", event.UID),
		"gid":         fmt.Sprintf("%d", event.GID),
		"cgroup_id":   fmt.Sprintf("%d", event.CgroupID),
		"command":     comm,
		"syscall":     c.getSyscallName(event.SyscallNr),
		"error_code":  fmt.Sprintf("%d", event.ErrorCode),
		"error_name":  c.getErrorName(event.ErrorCode),
		"category":    c.getCategoryName(event.Category),
		"error_count": fmt.Sprintf("%d", event.ErrorCount),
	}

	// Add path if present
	if path != "" {
		context["path"] = path
	}

	// Determine severity based on error type
	severity := c.getSeverityForError(event.ErrorCode)

	// Create observation event
	return &domain.ObservationEvent{
		Type:      domain.EventTypeSyscallError,
		Severity:  severity,
		Timestamp: timestamp,
		Source: domain.EventSource{
			Component: "syscall-errors",
			Host:      "", // Will be filled by the collector framework
		},
		Resource: domain.ResourceIdentifier{
			Type: "process",
			ID:   fmt.Sprintf("%d", event.PID),
			Name: comm,
		},
		Description: fmt.Sprintf("Syscall %s failed with %s",
			c.getSyscallName(event.SyscallNr),
			c.getErrorName(event.ErrorCode)),
		Context: context,
	}
}

// updateErrorMetrics updates specific error counters
func (c *Collector) updateErrorMetrics(ctx context.Context, errorCode int32) {
	switch -errorCode {
	case 28: // ENOSPC
		if c.enospcErrors != nil {
			c.enospcErrors.Add(ctx, 1)
		}
	case 12: // ENOMEM
		if c.enomemErrors != nil {
			c.enomemErrors.Add(ctx, 1)
		}
	case 111: // ECONNREFUSED
		if c.econnrefErrors != nil {
			c.econnrefErrors.Add(ctx, 1)
		}
	}
}

// getSeverityForError determines severity based on error type
func (c *Collector) getSeverityForError(errorCode int32) domain.EventSeverity {
	switch -errorCode {
	case 28, 12: // ENOSPC, ENOMEM - critical resource exhaustion
		return domain.SeverityCritical
	case 111, 110: // ECONNREFUSED, ETIMEDOUT - service connectivity issues
		return domain.SeverityHigh
	case 5: // EIO - I/O errors
		return domain.SeverityHigh
	case 13, 1: // EACCES, EPERM - permission issues
		return domain.SeverityMedium
	default:
		return domain.SeverityLow
	}
}

// getErrorName returns human-readable error name
func (c *Collector) getErrorName(errorCode int32) string {
	switch -errorCode {
	case 1:
		return "EPERM"
	case 2:
		return "ENOENT"
	case 5:
		return "EIO"
	case 11:
		return "EAGAIN"
	case 12:
		return "ENOMEM"
	case 13:
		return "EACCES"
	case 28:
		return "ENOSPC"
	case 110:
		return "ETIMEDOUT"
	case 111:
		return "ECONNREFUSED"
	default:
		return fmt.Sprintf("ERROR_%d", -errorCode)
	}
}

// getCategoryName returns category name
func (c *Collector) getCategoryName(category uint8) string {
	switch category {
	case 1:
		return "file"
	case 2:
		return "network"
	case 3:
		return "memory"
	case 4:
		return "process"
	default:
		return "other"
	}
}

// getSyscallName returns syscall name (simplified for key syscalls)
func (c *Collector) getSyscallName(syscallNr int32) string {
	switch syscallNr {
	case 0:
		return "read"
	case 1:
		return "write"
	case 2:
		return "open"
	case 3:
		return "close"
	case 41:
		return "socket"
	case 42:
		return "connect"
	case 43:
		return "accept"
	case 257:
		return "openat"
	default:
		return fmt.Sprintf("syscall_%d", syscallNr)
	}
}

// Stop stops the collector
func (c *Collector) Stop() error {
	c.stopOnce.Do(func() {
		c.logger.Info("Stopping syscall error collector")
		c.cancel()

		// Close ring buffer reader
		if c.reader != nil {
			c.reader.Close()
		}

		// Wait for event processing to complete
		c.wg.Wait()

		// Cleanup eBPF resources
		c.cleanup()

		// Close event channel
		close(c.eventChan)
	})
	return nil
}

// cleanup cleans up eBPF resources
func (c *Collector) cleanup() {
	// Close all links
	for _, l := range c.links {
		if l != nil {
			l.Close()
		}
	}

	// Close eBPF objects
	if c.objs != nil {
		c.objs.Close()
	}
}

// GetEventChannel returns the event channel
func (c *Collector) GetEventChannel() <-chan *domain.ObservationEvent {
	return c.eventChan
}

// GetName returns the collector name
func (c *Collector) GetName() string {
	return c.name
}

// IsHealthy checks if the collector is healthy
func (c *Collector) IsHealthy() bool {
	// Check if context is still active
	select {
	case <-c.ctx.Done():
		return false
	default:
		return true
	}
}

// GetStats returns current collector statistics
func (c *Collector) GetStats() (*CollectorStats, error) {
	if c.objs == nil || c.objs.Stats == nil {
		return nil, fmt.Errorf("eBPF objects not initialized")
	}

	var key uint32 = 0
	var stats CollectorStats

	// Read from per-CPU map and aggregate
	values, err := c.objs.Stats.Lookup(&key)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup stats: %w", err)
	}

	// values is []CollectorStats for per-CPU map
	if cpuStats, ok := values.([]CollectorStats); ok {
		for _, cpuStat := range cpuStats {
			stats.TotalErrors += cpuStat.TotalErrors
			stats.ENOSPCCount += cpuStat.ENOSPCCount
			stats.ENOMEMCount += cpuStat.ENOMEMCount
			stats.ECONNREFUSEDCount += cpuStat.ECONNREFUSEDCount
			stats.EIOCount += cpuStat.EIOCount
			stats.EventsSent += cpuStat.EventsSent
			stats.EventsDropped += cpuStat.EventsDropped
		}
	}

	return &stats, nil
}

// bytesToString converts null-terminated byte array to string
func bytesToString(data []byte) string {
	n := bytes.IndexByte(data, 0)
	if n == -1 {
		n = len(data)
	}
	return string(data[:n])
}
