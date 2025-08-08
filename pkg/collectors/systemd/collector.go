package systemd

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/systemd/bpf"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// SystemdEvent represents a systemd event from eBPF
type SystemdEvent struct {
	Timestamp uint64
	PID       uint32
	PPID      uint32
	EventType uint32
	ExitCode  uint32
	Comm      [16]byte
	Filename  [256]byte
}

// Collector implements minimal systemd monitoring via eBPF
type Collector struct {
	name         string
	logger       *zap.Logger
	tracer       trace.Tracer
	objs         *bpf.SystemdMonitorObjects
	links        []link.Link
	reader       *ringbuf.Reader
	events       chan collectors.RawEvent
	ctx          context.Context
	cancel       context.CancelFunc
	healthy      bool
	config       Config
	mu           sync.RWMutex
	unitTraceMap map[string]string // Map systemd unit names to trace IDs

	// Metrics
	eventsProcessed  int64
	eventsDropped    int64
	ebpfLoadSuccess  int64
	ebpfLoadFailures int64
	journalReadTime  int64
	correlationHits  int64

	// OTEL Metrics
	meter              metric.Meter
	eventsProcessedCtr metric.Int64Counter
	eventsDroppedCtr   metric.Int64Counter
	ebpfOperationsCtr  metric.Int64Counter
	journalPerfHist    metric.Int64Histogram
	correlationCtr     metric.Int64Counter
}

// NewCollector creates a new minimal systemd collector
func NewCollector(name string, cfg Config) (*Collector, error) {
	// Create logger
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Initialize OTEL components
	tracer := otel.Tracer("systemd-collector")
	meter := otel.Meter("systemd-collector")

	// Create metrics
	eventsProcessedCtr, err := meter.Int64Counter(
		"systemd_events_processed_total",
		metric.WithDescription("Total number of systemd events processed"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create events processed counter: %w", err)
	}

	eventsDroppedCtr, err := meter.Int64Counter(
		"systemd_events_dropped_total",
		metric.WithDescription("Total number of systemd events dropped"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create events dropped counter: %w", err)
	}

	ebpfOperationsCtr, err := meter.Int64Counter(
		"systemd_ebpf_operations_total",
		metric.WithDescription("Total number of eBPF operations"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create eBPF operations counter: %w", err)
	}

	journalPerfHist, err := meter.Int64Histogram(
		"systemd_journal_read_duration_ms",
		metric.WithDescription("Journal read performance in milliseconds"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create journal performance histogram: %w", err)
	}

	correlationCtr, err := meter.Int64Counter(
		"systemd_correlation_hits_total",
		metric.WithDescription("Total number of systemd unit correlation hits"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create correlation counter: %w", err)
	}

	c := &Collector{
		name:               name,
		logger:             logger.Named(name),
		tracer:             tracer,
		config:             cfg,
		events:             make(chan collectors.RawEvent, cfg.BufferSize),
		unitTraceMap:       make(map[string]string),
		meter:              meter,
		eventsProcessedCtr: eventsProcessedCtr,
		eventsDroppedCtr:   eventsDroppedCtr,
		ebpfOperationsCtr:  ebpfOperationsCtr,
		journalPerfHist:    journalPerfHist,
		correlationCtr:     correlationCtr,
	}

	c.logger.Info("Systemd collector created",
		zap.String("name", name),
		zap.Int("buffer_size", cfg.BufferSize),
		zap.Bool("enable_ebpf", cfg.EnableEBPF),
		zap.Bool("enable_journal", cfg.EnableJournal),
		zap.Strings("service_patterns", cfg.ServicePatterns),
	)

	return c, nil
}

// Name returns collector name
func (c *Collector) Name() string {
	return c.name
}

// Start starts the eBPF monitoring
func (c *Collector) Start(ctx context.Context) error {
	ctx, span := c.tracer.Start(ctx, "systemd.collector.start")
	defer span.End()

	c.ctx, c.cancel = context.WithCancel(ctx)
	c.logger.Info("Starting systemd collector",
		zap.Bool("enable_ebpf", c.config.EnableEBPF),
		zap.Bool("enable_journal", c.config.EnableJournal),
	)

	// Start journal reader if enabled
	if c.config.EnableJournal {
		c.logger.Info("Starting journal reader")
		if err := c.startJournalReader(); err != nil {
			c.logger.Error("Failed to start journal reader", zap.Error(err))
			span.SetAttributes(attribute.String("error", "journal_reader_failed"))
			return fmt.Errorf("failed to start journal reader: %w", err)
		}
		c.logger.Info("Journal reader started successfully")
	}

	// Start eBPF monitoring if enabled
	if c.config.EnableEBPF {
		c.logger.Info("Starting eBPF monitoring")

		// Load eBPF program
		spec, err := bpf.LoadSystemdMonitor()
		if err != nil {
			c.logger.Error("Failed to load eBPF spec", zap.Error(err))
			atomic.AddInt64(&c.ebpfLoadFailures, 1)
			c.ebpfOperationsCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("operation", "load_spec"),
				attribute.String("status", "failed"),
			))
			span.SetAttributes(attribute.String("error", "ebpf_spec_load_failed"))
			return fmt.Errorf("failed to load eBPF spec: %w", err)
		}

		c.objs = &bpf.SystemdMonitorObjects{}
		if err := spec.LoadAndAssign(c.objs, nil); err != nil {
			c.logger.Error("Failed to load and assign eBPF objects", zap.Error(err))
			atomic.AddInt64(&c.ebpfLoadFailures, 1)
			c.ebpfOperationsCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("operation", "load_assign"),
				attribute.String("status", "failed"),
			))
			span.SetAttributes(attribute.String("error", "ebpf_objects_load_failed"))
			return fmt.Errorf("failed to load eBPF objects: %w", err)
		}
		c.logger.Info("eBPF objects loaded successfully")
		atomic.AddInt64(&c.ebpfLoadSuccess, 1)
		c.ebpfOperationsCtr.Add(ctx, 1, metric.WithAttributes(
			attribute.String("operation", "load_assign"),
			attribute.String("status", "success"),
		))

		// Populate systemd PIDs
		c.logger.Info("Populating systemd PIDs")
		if err := c.populateSystemdPIDs(ctx); err != nil {
			c.logger.Error("Failed to populate systemd PIDs", zap.Error(err))
			span.SetAttributes(attribute.String("error", "populate_pids_failed"))
			return fmt.Errorf("failed to populate systemd PIDs: %w", err)
		}

		// Attach tracepoints
		c.logger.Info("Attaching tracepoints")
		execLink, err := link.Tracepoint("syscalls", "sys_enter_execve", c.objs.TraceExec, nil)
		if err != nil {
			c.logger.Error("Failed to attach execve tracepoint", zap.Error(err))
			c.ebpfOperationsCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("operation", "attach_execve"),
				attribute.String("status", "failed"),
			))
			span.SetAttributes(attribute.String("error", "execve_attach_failed"))
			return fmt.Errorf("failed to attach execve tracepoint: %w", err)
		}
		c.links = append(c.links, execLink)
		c.ebpfOperationsCtr.Add(ctx, 1, metric.WithAttributes(
			attribute.String("operation", "attach_execve"),
			attribute.String("status", "success"),
		))

		exitLink, err := link.Tracepoint("syscalls", "sys_enter_exit", c.objs.TraceExit, nil)
		if err != nil {
			c.logger.Error("Failed to attach exit tracepoint", zap.Error(err))
			c.ebpfOperationsCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("operation", "attach_exit"),
				attribute.String("status", "failed"),
			))
			span.SetAttributes(attribute.String("error", "exit_attach_failed"))
			return fmt.Errorf("failed to attach exit tracepoint: %w", err)
		}
		c.links = append(c.links, exitLink)
		c.ebpfOperationsCtr.Add(ctx, 1, metric.WithAttributes(
			attribute.String("operation", "attach_exit"),
			attribute.String("status", "success"),
		))

		// Open ring buffer
		c.reader, err = ringbuf.NewReader(c.objs.Events)
		if err != nil {
			c.logger.Error("Failed to open ring buffer", zap.Error(err))
			c.ebpfOperationsCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("operation", "open_ringbuf"),
				attribute.String("status", "failed"),
			))
			span.SetAttributes(attribute.String("error", "ringbuf_open_failed"))
			return fmt.Errorf("failed to open ring buffer: %w", err)
		}
		c.ebpfOperationsCtr.Add(ctx, 1, metric.WithAttributes(
			attribute.String("operation", "open_ringbuf"),
			attribute.String("status", "success"),
		))

		// Start event processing
		c.logger.Info("Starting event processing goroutine")
		go c.processEvents()
		c.logger.Info("eBPF monitoring started successfully")
	}

	c.healthy = true
	c.logger.Info("Systemd collector started successfully")
	span.SetAttributes(attribute.Bool("success", true))
	return nil
}

// Stop stops the collector
func (c *Collector) Stop() error {
	_, span := c.tracer.Start(context.Background(), "systemd.collector.stop")
	defer span.End()

	c.logger.Info("Stopping systemd collector")

	if c.cancel != nil {
		c.cancel()
	}

	// Close links
	c.logger.Debug("Closing eBPF links", zap.Int("link_count", len(c.links)))
	for i, l := range c.links {
		if err := l.Close(); err != nil {
			c.logger.Warn("Failed to close eBPF link", zap.Int("link_index", i), zap.Error(err))
		}
	}

	// Close ring buffer
	if c.reader != nil {
		c.logger.Debug("Closing ring buffer reader")
		if err := c.reader.Close(); err != nil {
			c.logger.Warn("Failed to close ring buffer reader", zap.Error(err))
		}
	}

	// Close eBPF objects
	if c.objs != nil {
		c.logger.Debug("Closing eBPF objects")
		c.objs.Close()
	}

	// Log final statistics
	c.logger.Info("Collector statistics",
		zap.Int64("events_processed", atomic.LoadInt64(&c.eventsProcessed)),
		zap.Int64("events_dropped", atomic.LoadInt64(&c.eventsDropped)),
		zap.Int64("ebpf_load_success", atomic.LoadInt64(&c.ebpfLoadSuccess)),
		zap.Int64("ebpf_load_failures", atomic.LoadInt64(&c.ebpfLoadFailures)),
		zap.Int64("correlation_hits", atomic.LoadInt64(&c.correlationHits)),
	)

	close(c.events)
	c.healthy = false

	c.logger.Info("Systemd collector stopped successfully")
	span.SetAttributes(attribute.Bool("success", true))
	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan collectors.RawEvent {
	return c.events
}

// IsHealthy returns health status
func (c *Collector) IsHealthy() bool {
	return c.healthy
}

// populateSystemdPIDs finds and adds systemd-related PIDs to the map
func (c *Collector) populateSystemdPIDs(ctx context.Context) error {
	ctx, span := c.tracer.Start(ctx, "systemd.collector.populate_pids")
	defer span.End()

	c.logger.Info("Starting PID population for systemd processes")

	// Find systemd PIDs (PID 1 and its children)
	pids := []uint32{1} // systemd is always PID 1
	var skippedPIDs int

	// Add some common systemd PIDs by scanning /proc
	procs, err := os.ReadDir("/proc")
	if err != nil {
		c.logger.Error("Failed to read /proc directory", zap.Error(err))
		span.SetAttributes(attribute.String("error", "proc_read_failed"))
		return fmt.Errorf("failed to read /proc: %w", err)
	}

	c.logger.Debug("Found proc entries", zap.Int("count", len(procs)))

	for _, proc := range procs {
		if !proc.IsDir() {
			continue
		}

		pid, err := strconv.ParseUint(proc.Name(), 10, 32)
		if err != nil {
			// Skip non-numeric entries
			continue
		}

		// Validate PID range
		if pid == 0 || pid > 4194304 { // Linux PID_MAX_LIMIT
			continue
		}

		// Read comm to check if it's systemd-related
		commPath := fmt.Sprintf("/proc/%d/comm", pid)
		comm, err := os.ReadFile(commPath)
		if err != nil {
			// Process might have exited, skip
			continue
		}

		// Validate comm data
		if len(comm) == 0 || len(comm) > 16 { // comm is max 15 chars + newline
			continue
		}

		commStr := strings.TrimSpace(string(comm))
		// Look for systemd processes with input validation
		if c.isValidSystemdProcess(commStr) {
			pids = append(pids, uint32(pid))
			c.logger.Debug("Found systemd process",
				zap.Uint32("pid", uint32(pid)),
				zap.String("comm", commStr),
			)
		}
	}

	c.logger.Info("Found systemd PIDs", zap.Int("total_pids", len(pids)))
	span.SetAttributes(attribute.Int("pids_found", len(pids)))

	// Add PIDs to eBPF map with error tracking
	var value uint8 = 1
	addedPIDs := 0
	for _, pid := range pids {
		if err := c.objs.SystemdPids.Put(pid, value); err != nil {
			c.logger.Warn("Failed to add PID to eBPF map",
				zap.Uint32("pid", pid),
				zap.Error(err),
			)
			skippedPIDs++
			continue
		}
		addedPIDs++
	}

	c.logger.Info("PID population completed",
		zap.Int("added_pids", addedPIDs),
		zap.Int("skipped_pids", skippedPIDs),
	)
	span.SetAttributes(
		attribute.Int("added_pids", addedPIDs),
		attribute.Int("skipped_pids", skippedPIDs),
	)

	// Log warning if too many PIDs were skipped
	if skippedPIDs > len(pids)/2 {
		c.logger.Warn("High number of PIDs skipped during population",
			zap.Int("skipped_pids", skippedPIDs),
			zap.Int("total_pids", len(pids)),
			zap.Float64("skip_rate", float64(skippedPIDs)/float64(len(pids))),
		)
	}

	return nil
}

// isValidSystemdProcess validates if a process name is a systemd process
func (c *Collector) isValidSystemdProcess(comm string) bool {
	// Input validation
	if len(comm) == 0 || len(comm) > 15 {
		return false
	}

	// Check for systemd-related process names
	systemdPrefixes := []string{
		"systemd",
		"systemd-",
	}

	for _, prefix := range systemdPrefixes {
		if strings.HasPrefix(comm, prefix) {
			return true
		}
	}

	return false
}

// parseSystemdEvent safely parses raw event bytes into SystemdEvent struct
func (c *Collector) parseSystemdEvent(rawData []byte) (*SystemdEvent, error) {
	const expectedSize = 8 + 4 + 4 + 4 + 4 + 16 + 256 // Timestamp + PID + PPID + EventType + ExitCode + Comm + Filename

	// Validate input data size
	if len(rawData) < expectedSize {
		return nil, fmt.Errorf("insufficient data: got %d bytes, expected at least %d", len(rawData), expectedSize)
	}

	// Bounds check - prevent buffer overflow
	if len(rawData) > expectedSize*2 {
		return nil, fmt.Errorf("data too large: got %d bytes, max allowed %d", len(rawData), expectedSize*2)
	}

	event := &SystemdEvent{}
	offset := 0

	// Parse Timestamp (8 bytes)
	if offset+8 > len(rawData) {
		return nil, fmt.Errorf("insufficient data for timestamp")
	}
	event.Timestamp = binary.LittleEndian.Uint64(rawData[offset : offset+8])
	offset += 8

	// Parse PID (4 bytes)
	if offset+4 > len(rawData) {
		return nil, fmt.Errorf("insufficient data for PID")
	}
	event.PID = binary.LittleEndian.Uint32(rawData[offset : offset+4])
	offset += 4

	// Parse PPID (4 bytes)
	if offset+4 > len(rawData) {
		return nil, fmt.Errorf("insufficient data for PPID")
	}
	event.PPID = binary.LittleEndian.Uint32(rawData[offset : offset+4])
	offset += 4

	// Parse EventType (4 bytes)
	if offset+4 > len(rawData) {
		return nil, fmt.Errorf("insufficient data for EventType")
	}
	event.EventType = binary.LittleEndian.Uint32(rawData[offset : offset+4])
	offset += 4

	// Parse ExitCode (4 bytes)
	if offset+4 > len(rawData) {
		return nil, fmt.Errorf("insufficient data for ExitCode")
	}
	event.ExitCode = binary.LittleEndian.Uint32(rawData[offset : offset+4])
	offset += 4

	// Parse Comm (16 bytes)
	if offset+16 > len(rawData) {
		return nil, fmt.Errorf("insufficient data for Comm")
	}
	copy(event.Comm[:], rawData[offset:offset+16])
	offset += 16

	// Parse Filename (256 bytes)
	if offset+256 > len(rawData) {
		return nil, fmt.Errorf("insufficient data for Filename")
	}
	copy(event.Filename[:], rawData[offset:offset+256])

	// Validate parsed data
	if event.PID == 0 {
		return nil, fmt.Errorf("invalid PID: 0")
	}

	// Validate event type
	if event.EventType > 10 { // Assuming reasonable upper bound
		return nil, fmt.Errorf("invalid event type: %d", event.EventType)
	}

	// Validate timestamp (should be recent)
	now := uint64(time.Now().UnixNano())
	if event.Timestamp > now+uint64(time.Hour.Nanoseconds()) {
		return nil, fmt.Errorf("invalid timestamp: %d (too far in future)", event.Timestamp)
	}

	return event, nil
}

// processEvents processes events from the ring buffer
func (c *Collector) processEvents() {
	ctx, span := c.tracer.Start(context.Background(), "systemd.collector.process_events")
	defer span.End()

	c.logger.Info("Starting event processing loop")

	for {
		select {
		case <-c.ctx.Done():
			c.logger.Info("Event processing stopped due to context cancellation")
			return
		default:
		}

		record, err := c.reader.Read()
		if err != nil {
			if c.ctx.Err() != nil {
				return
			}
			c.logger.Debug("Failed to read from ring buffer", zap.Error(err))
			continue
		}

		// Parse event with safe parsing and input validation
		event, err := c.parseSystemdEvent(record.RawSample)
		if err != nil {
			c.logger.Debug("Failed to parse systemd event", zap.Error(err))
			atomic.AddInt64(&c.eventsDropped, 1)
			c.eventsDroppedCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("reason", "parse_failed"),
			))
			continue
		}

		// Convert to RawEvent with tracing - NO BUSINESS LOGIC
		_, eventSpan := c.tracer.Start(ctx, "systemd.collector.convert_event")
		rawEvent := collectors.RawEvent{
			Timestamp: time.Unix(0, int64(event.Timestamp)),
			Type:      c.eventTypeToString(event.EventType),
			Data:      record.RawSample, // Raw eBPF event data
			Metadata: map[string]string{
				"collector": "systemd",
				"pid":       fmt.Sprintf("%d", event.PID),
				"ppid":      fmt.Sprintf("%d", event.PPID),
				"comm":      c.nullTerminatedString(event.Comm[:]),
				"filename":  c.nullTerminatedString(event.Filename[:]),
				"exit_code": fmt.Sprintf("%d", event.ExitCode),
			},
			// Extract trace ID from systemd journal metadata or generate new one
			TraceID: c.extractTraceIDFromJournal(*event),
			SpanID:  collectors.GenerateSpanID(),
		}
		eventSpan.SetAttributes(
			attribute.String("event_type", rawEvent.Type),
			attribute.Int64("pid", int64(event.PID)),
			attribute.String("comm", c.nullTerminatedString(event.Comm[:])),
		)
		eventSpan.End()

		// Track processed events
		atomic.AddInt64(&c.eventsProcessed, 1)
		c.eventsProcessedCtr.Add(ctx, 1, metric.WithAttributes(
			attribute.String("event_type", rawEvent.Type),
		))

		select {
		case c.events <- rawEvent:
			c.logger.Debug("Event sent to channel",
				zap.String("type", rawEvent.Type),
				zap.Uint32("pid", event.PID),
				zap.String("trace_id", rawEvent.TraceID),
			)
		case <-c.ctx.Done():
			return
		default:
			// Drop event if buffer full
			c.logger.Debug("Event dropped due to full buffer",
				zap.String("type", rawEvent.Type),
				zap.Uint32("pid", event.PID),
			)
			atomic.AddInt64(&c.eventsDropped, 1)
			c.eventsDroppedCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("reason", "buffer_full"),
			))
		}
	}
}

// eventTypeToString converts event type to string
func (c *Collector) eventTypeToString(eventType uint32) string {
	switch eventType {
	case 1:
		return "exec"
	case 2:
		return "exit"
	case 3:
		return "kill"
	default:
		return "unknown"
	}
}

// nullTerminatedString converts null-terminated byte array to string
func (c *Collector) nullTerminatedString(b []byte) string {
	for i, v := range b {
		if v == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// extractTraceIDFromJournal attempts to extract trace ID from systemd journal metadata
func (c *Collector) extractTraceIDFromJournal(event SystemdEvent) string {
	ctx, span := c.tracer.Start(context.Background(), "systemd.collector.extract_trace_id")
	defer span.End()

	span.SetAttributes(
		attribute.Int64("pid", int64(event.PID)),
		attribute.String("comm", c.nullTerminatedString(event.Comm[:])),
	)

	// Try to read journal metadata for this PID to get service unit information
	journalMeta, err := c.getJournalMetadata(event.PID)
	if err != nil {
		c.logger.Debug("Failed to get journal metadata",
			zap.Uint32("pid", event.PID),
			zap.Error(err),
		)
		span.SetAttributes(attribute.String("fallback_reason", "no_journal_metadata"))
		return collectors.GenerateTraceID()
	}

	// Check if this is a service unit with a persistent trace ID
	if journalMeta.unit != "" && journalMeta.unit != "unknown" {
		c.logger.Debug("Found systemd unit for correlation",
			zap.Uint32("pid", event.PID),
			zap.String("unit", journalMeta.unit),
		)

		// Track correlation hit
		atomic.AddInt64(&c.correlationHits, 1)
		c.correlationCtr.Add(ctx, 1, metric.WithAttributes(
			attribute.String("correlation_type", "unit"),
			attribute.String("unit", journalMeta.unit),
		))

		span.SetAttributes(
			attribute.String("correlation_type", "unit"),
			attribute.String("unit", journalMeta.unit),
		)

		// Use unit name to generate consistent trace ID for service events
		return c.getOrGenerateTraceIDForUnit(journalMeta.unit)
	}

	// If we have machine ID and boot ID, use them for system-level correlation
	if journalMeta.machineID != "" && journalMeta.bootID != "" {
		c.logger.Debug("Using machine/boot ID for correlation",
			zap.Uint32("pid", event.PID),
			zap.String("machine_id_prefix", journalMeta.machineID[:min(8, len(journalMeta.machineID))]),
			zap.String("boot_id_prefix", journalMeta.bootID[:min(8, len(journalMeta.bootID))]),
		)

		// Validate ID lengths to prevent panic
		machineIDLen := min(8, len(journalMeta.machineID))
		bootIDLen := min(8, len(journalMeta.bootID))

		systemKey := fmt.Sprintf("%s-%s",
			journalMeta.machineID[:machineIDLen],
			journalMeta.bootID[:bootIDLen])

		// Track correlation hit
		atomic.AddInt64(&c.correlationHits, 1)
		c.correlationCtr.Add(ctx, 1, metric.WithAttributes(
			attribute.String("correlation_type", "system"),
		))

		span.SetAttributes(
			attribute.String("correlation_type", "system"),
			attribute.String("system_key", systemKey),
		)

		return c.getOrGenerateTraceIDForUnit(systemKey)
	}

	c.logger.Debug("No correlation found, generating new trace ID",
		zap.Uint32("pid", event.PID),
	)
	span.SetAttributes(attribute.String("fallback_reason", "no_correlation_data"))

	// Fallback: generate new trace ID for each event
	return collectors.GenerateTraceID()
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// getJournalMetadata reads systemd journal metadata for a PID
func (c *Collector) getJournalMetadata(pid uint32) (*journalMetadata, error) {
	// Try to read from /proc/PID/cgroup to get systemd unit information
	cgroupPath := fmt.Sprintf("/proc/%d/cgroup", pid)
	cgroupData, err := os.ReadFile(cgroupPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read cgroup file: %w", err)
	}

	// Parse systemd unit from cgroup
	unit := c.extractUnitFromCgroup(string(cgroupData))

	// Try to get machine ID
	machineID, err := c.getMachineID()
	if err != nil {
		machineID = "unknown"
	}

	// Try to get boot ID
	bootID, err := c.getBootID()
	if err != nil {
		bootID = "unknown"
	}

	return &journalMetadata{
		unit:      unit,
		machineID: machineID,
		bootID:    bootID,
		pid:       pid,
	}, nil
}

// extractUnitFromCgroup extracts systemd unit from cgroup information
func (c *Collector) extractUnitFromCgroup(cgroupData string) string {
	lines := strings.Split(cgroupData, "\n")
	for _, line := range lines {
		if strings.Contains(line, ":pids:") || strings.Contains(line, ":systemd:") {
			// Extract unit from systemd cgroup path
			parts := strings.Split(line, ":")
			if len(parts) >= 3 {
				cgroupPath := parts[2]
				// Look for systemd unit pattern like /system.slice/nginx.service
				if strings.Contains(cgroupPath, ".service") {
					pathParts := strings.Split(cgroupPath, "/")
					for _, part := range pathParts {
						if strings.HasSuffix(part, ".service") {
							return part
						}
					}
				}
				// Look for other unit types (.timer, .socket, etc.)
				for _, unitType := range []string{".timer", ".socket", ".target", ".mount", ".device"} {
					if strings.Contains(cgroupPath, unitType) {
						pathParts := strings.Split(cgroupPath, "/")
						for _, part := range pathParts {
							if strings.HasSuffix(part, unitType) {
								return part
							}
						}
					}
				}
			}
		}
	}
	return "unknown"
}

// getMachineID reads the systemd machine ID
func (c *Collector) getMachineID() (string, error) {
	data, err := os.ReadFile("/etc/machine-id")
	if err != nil {
		// Fallback to /var/lib/dbus/machine-id
		data, err = os.ReadFile("/var/lib/dbus/machine-id")
		if err != nil {
			return "", err
		}
	}
	return strings.TrimSpace(string(data)), nil
}

// getBootID reads the system boot ID
func (c *Collector) getBootID() (string, error) {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// getOrGenerateTraceIDForUnit returns consistent trace ID for a systemd unit
func (c *Collector) getOrGenerateTraceIDForUnit(unitName string) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if we already have a trace ID for this unit
	if c.unitTraceMap == nil {
		c.unitTraceMap = make(map[string]string)
	}

	if traceID, exists := c.unitTraceMap[unitName]; exists {
		return traceID
	}

	// Generate new trace ID for this unit
	traceID := collectors.GenerateTraceID()
	c.unitTraceMap[unitName] = traceID
	return traceID
}

// journalMetadata represents systemd journal metadata
type journalMetadata struct {
	unit      string
	machineID string
	bootID    string
	pid       uint32
}

// startJournalReader starts journal reading (minimal implementation)
func (c *Collector) startJournalReader() error {
	// For now, just log that journal reader would be started
	c.logger.Info("Journal reader startup requested")
	// TODO: Implement actual journal reading if needed
	return nil
}
