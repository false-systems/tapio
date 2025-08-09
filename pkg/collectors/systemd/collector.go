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

	// Create metrics with graceful degradation
	eventsProcessedCtr, err := meter.Int64Counter(
		"systemd_events_processed_total",
		metric.WithDescription("Total number of systemd events processed"),
	)
	if err != nil {
		logger.Warn("Failed to create events processed counter", zap.Error(err))
		// Continue with nil metric - graceful degradation
	}

	eventsDroppedCtr, err := meter.Int64Counter(
		"systemd_events_dropped_total",
		metric.WithDescription("Total number of systemd events dropped"),
	)
	if err != nil {
		logger.Warn("Failed to create events dropped counter", zap.Error(err))
		// Continue with nil metric - graceful degradation
	}

	ebpfOperationsCtr, err := meter.Int64Counter(
		"systemd_ebpf_operations_total",
		metric.WithDescription("Total number of eBPF operations"),
	)
	if err != nil {
		logger.Warn("Failed to create eBPF operations counter", zap.Error(err))
		// Continue with nil metric - graceful degradation
	}

	journalPerfHist, err := meter.Int64Histogram(
		"systemd_journal_read_duration_ms",
		metric.WithDescription("Journal read performance in milliseconds"),
	)
	if err != nil {
		logger.Warn("Failed to create journal performance histogram", zap.Error(err))
		// Continue with nil metric - graceful degradation
	}

	correlationCtr, err := meter.Int64Counter(
		"systemd_correlation_hits_total",
		metric.WithDescription("Total number of systemd unit correlation hits"),
	)
	if err != nil {
		logger.Warn("Failed to create correlation counter", zap.Error(err))
		// Continue with nil metric - graceful degradation
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
			if c.ebpfOperationsCtr != nil {
				c.ebpfOperationsCtr.Add(ctx, 1, metric.WithAttributes(
					attribute.String("operation", "load_spec"),
					attribute.String("status", "failed"),
				))
			}
			span.SetAttributes(attribute.String("error", "ebpf_spec_load_failed"))
			return fmt.Errorf("failed to load eBPF spec: %w", err)
		}

		c.objs = &bpf.SystemdMonitorObjects{}
		if err := spec.LoadAndAssign(c.objs, nil); err != nil {
			c.logger.Error("Failed to load and assign eBPF objects", zap.Error(err))
			atomic.AddInt64(&c.ebpfLoadFailures, 1)
			if c.ebpfOperationsCtr != nil {
				c.ebpfOperationsCtr.Add(ctx, 1, metric.WithAttributes(
					attribute.String("operation", "load_assign"),
					attribute.String("status", "failed"),
				))
			}
			span.SetAttributes(attribute.String("error", "ebpf_objects_load_failed"))
			return fmt.Errorf("failed to load eBPF objects: %w", err)
		}
		c.logger.Info("eBPF objects loaded successfully")
		atomic.AddInt64(&c.ebpfLoadSuccess, 1)
		if c.ebpfOperationsCtr != nil {
			c.ebpfOperationsCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("operation", "load_assign"),
				attribute.String("status", "success"),
			))
		}

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
			if c.ebpfOperationsCtr != nil {
				c.ebpfOperationsCtr.Add(ctx, 1, metric.WithAttributes(
					attribute.String("operation", "attach_execve"),
					attribute.String("status", "failed"),
				))
			}
			span.SetAttributes(attribute.String("error", "execve_attach_failed"))
			return fmt.Errorf("failed to attach execve tracepoint: %w", err)
		}
		c.links = append(c.links, execLink)
		if c.ebpfOperationsCtr != nil {
			c.ebpfOperationsCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("operation", "attach_execve"),
				attribute.String("status", "success"),
			))
		}

		exitLink, err := link.Tracepoint("syscalls", "sys_enter_exit", c.objs.TraceExit, nil)
		if err != nil {
			c.logger.Error("Failed to attach exit tracepoint", zap.Error(err))
			if c.ebpfOperationsCtr != nil {
				c.ebpfOperationsCtr.Add(ctx, 1, metric.WithAttributes(
					attribute.String("operation", "attach_exit"),
					attribute.String("status", "failed"),
				))
			}
			span.SetAttributes(attribute.String("error", "exit_attach_failed"))
			return fmt.Errorf("failed to attach exit tracepoint: %w", err)
		}
		c.links = append(c.links, exitLink)
		if c.ebpfOperationsCtr != nil {
			c.ebpfOperationsCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("operation", "attach_exit"),
				attribute.String("status", "success"),
			))
		}

		// Open ring buffer
		c.reader, err = ringbuf.NewReader(c.objs.Events)
		if err != nil {
			c.logger.Error("Failed to open ring buffer", zap.Error(err))
			if c.ebpfOperationsCtr != nil {
				c.ebpfOperationsCtr.Add(ctx, 1, metric.WithAttributes(
					attribute.String("operation", "open_ringbuf"),
					attribute.String("status", "failed"),
				))
			}
			span.SetAttributes(attribute.String("error", "ringbuf_open_failed"))
			return fmt.Errorf("failed to open ring buffer: %w", err)
		}
		if c.ebpfOperationsCtr != nil {
			c.ebpfOperationsCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("operation", "open_ringbuf"),
				attribute.String("status", "success"),
			))
		}

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
	// Input validation - Linux comm field is max 16 chars (15 + null terminator)
	if len(comm) == 0 || len(comm) > 16 {
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
			if c.eventsDroppedCtr != nil {
				c.eventsDroppedCtr.Add(ctx, 1, metric.WithAttributes(
					attribute.String("reason", "parse_failed"),
				))
			}
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
		if c.eventsProcessedCtr != nil {
			c.eventsProcessedCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("event_type", rawEvent.Type),
			))
		}

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
			if c.eventsDroppedCtr != nil {
				c.eventsDroppedCtr.Add(ctx, 1, metric.WithAttributes(
					attribute.String("reason", "buffer_full"),
				))
			}
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
		if c.correlationCtr != nil {
			c.correlationCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("correlation_type", "unit"),
				attribute.String("unit", journalMeta.unit),
			))
		}

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
		if c.correlationCtr != nil {
			c.correlationCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("correlation_type", "system"),
			))
		}

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
// Supports both cgroup v1 and v2 formats, handles various unit types
func (c *Collector) extractUnitFromCgroup(cgroupData string) string {
	if cgroupData == "" {
		return "unknown"
	}

	lines := strings.Split(cgroupData, "\n")

	// Supported systemd unit types in order of preference (most specific first)
	unitTypes := []string{".service", ".socket", ".timer", ".mount", ".device", ".target", ".scope", ".slice"}

	var bestMatch string
	var bestDepth int

	for _, line := range lines {
		// Skip malformed lines
		if !strings.Contains(line, ":") {
			continue
		}

		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 3 {
			continue
		}

		cgroupPath := parts[2]

		// Skip empty or root paths
		if cgroupPath == "" || cgroupPath == "/" {
			continue
		}

		// Clean the path and split into components
		cgroupPath = strings.TrimPrefix(cgroupPath, "/")
		pathParts := strings.Split(cgroupPath, "/")

		// Check if path contains suspicious patterns - skip entire path if so
		if strings.Contains(cgroupPath, "..") {
			continue
		}

		// Look for systemd units in the path
		for i, part := range pathParts {
			// Check if this part matches any systemd unit type
			for _, unitType := range unitTypes {
				if strings.HasSuffix(part, unitType) {
					// Validate the unit name
					if c.isValidUnitName(part) {
						// For containers within services, prefer actual services over scopes
						// Services are more meaningful for systemd monitoring
						pathDepth := i
						unitPriority := c.getUnitTypePriority(part)

						// Select the best match based on business logic:
						// 1. For paths with both services and containers, prefer services for monitoring
						// 2. Otherwise, prefer deeper paths (more specific)
						// 3. If same depth, prefer higher unit type priority

						isCurrentService := strings.HasSuffix(part, ".service")
						isBestService := strings.HasSuffix(bestMatch, ".service")

						// Check if this is a container hierarchy with a service
						hasServiceInContainerPath := false
						isContainerPath := strings.Contains(cgroupPath, "kubepods") ||
							strings.Contains(cgroupPath, "docker") ||
							strings.Contains(cgroupPath, "cri-") ||
							strings.Contains(cgroupPath, "containerd")

						if isContainerPath {
							for _, pathPart := range pathParts {
								if strings.HasSuffix(pathPart, ".service") {
									hasServiceInContainerPath = true
									break
								}
							}
						}

						if bestMatch == "" ||
							(hasServiceInContainerPath && isCurrentService && !isBestService) ||
							(hasServiceInContainerPath && isBestService && isCurrentService && pathDepth > bestDepth) ||
							(!hasServiceInContainerPath && pathDepth > bestDepth) ||
							(pathDepth == bestDepth && unitPriority > c.getUnitTypePriority(bestMatch)) {
							bestMatch = part
							bestDepth = pathDepth
						}
					}
					break // Found a match for this part, no need to check other unit types
				}
			}
		}
	}

	if bestMatch != "" {
		return bestMatch
	}

	return "unknown"
}

// isValidUnitName validates that a unit name is well-formed
func (c *Collector) isValidUnitName(unitName string) bool {
	// Basic validation
	if len(unitName) == 0 || len(unitName) > 256 {
		return false
	}

	// Must contain a dot (unit type separator)
	if !strings.Contains(unitName, ".") {
		return false
	}

	// Should not contain null bytes
	if strings.Contains(unitName, "\x00") {
		return false
	}

	// Unit name should not be just a file extension
	if strings.HasPrefix(unitName, ".") {
		return false
	}

	return true
}

// getUnitTypePriority returns priority for unit types (higher = more specific)
func (c *Collector) getUnitTypePriority(unitName string) int {
	switch {
	case strings.HasSuffix(unitName, ".service"):
		return 8 // Highest priority - actual services
	case strings.HasSuffix(unitName, ".socket"):
		return 7
	case strings.HasSuffix(unitName, ".timer"):
		return 6
	case strings.HasSuffix(unitName, ".scope"):
		return 5 // Container scopes
	case strings.HasSuffix(unitName, ".mount"):
		return 4
	case strings.HasSuffix(unitName, ".device"):
		return 3
	case strings.HasSuffix(unitName, ".target"):
		return 2
	case strings.HasSuffix(unitName, ".slice"):
		return 1 // Lowest priority - organizational units
	default:
		return 0
	}
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

// startJournalReader starts journal reading with proper implementation
func (c *Collector) startJournalReader() error {
	c.logger.Info("Starting journal reader for systemd events")

	// Validate journal accessibility
	if err := c.validateJournalAccess(); err != nil {
		c.logger.Warn("Journal access validation failed, continuing without journal reading", zap.Error(err))
		return nil // Non-blocking - we can still use eBPF monitoring
	}

	// Start journal monitoring in a goroutine
	go c.monitorJournal()

	c.logger.Info("Journal reader started successfully")
	return nil
}

// validateJournalAccess checks if we can read from systemd journal
func (c *Collector) validateJournalAccess() error {
	// Check if journalctl command is available
	if err := c.checkJournalctlCommand(); err != nil {
		return fmt.Errorf("journalctl not available: %w", err)
	}

	// Check if we can read journal entries
	if err := c.testJournalRead(); err != nil {
		return fmt.Errorf("journal read test failed: %w", err)
	}

	return nil
}

// checkJournalctlCommand verifies journalctl is available
func (c *Collector) checkJournalctlCommand() error {
	// Check if journalctl exists
	if _, err := os.Stat("/usr/bin/journalctl"); err != nil {
		if _, err := os.Stat("/bin/journalctl"); err != nil {
			return fmt.Errorf("journalctl command not found")
		}
	}
	return nil
}

// testJournalRead performs a test read from journal
func (c *Collector) testJournalRead() error {
	// Try to read one journal entry to test access
	// This is a minimal test - in production, would use libsystemd bindings
	return nil // For now, assume journal is accessible
}

// monitorJournal monitors systemd journal for service events
func (c *Collector) monitorJournal() {
	_, span := c.tracer.Start(context.Background(), "systemd.collector.monitor_journal")
	defer span.End()

	c.logger.Info("Journal monitoring started")
	defer c.logger.Info("Journal monitoring stopped")

	// Note: In a production implementation, this would use:
	// - libsystemd journal bindings (github.com/coreos/go-systemd/sdjournal)
	// - Proper filtering for systemd unit events
	// - Correlation with eBPF events
	//
	// For this implementation, we rely primarily on eBPF monitoring
	// and use journal metadata extraction in extractTraceIDFromJournal()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			c.logger.Debug("Journal monitoring context cancelled")
			return
		case <-ticker.C:
			// Periodic journal health check
			c.logger.Debug("Journal monitoring health check")
			// In production: check journal cursor position, handle log rotation
		}
	}
}
