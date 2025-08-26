//go:build linux
// +build linux

package etcdebpf

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// IMPORTANT: eBPF compilation requires Linux
// To regenerate the eBPF bytecode after modifying etcd_monitor.c:
// 1. Run this command on a Linux system (not macOS/Windows)
// 2. Ensure clang/LLVM and kernel headers are installed
// 3. Run: go generate ./...
// The generated files (etcdMonitor_bpfel_*.go) contain the compiled bytecode
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -tags linux -target amd64,arm64 -cc clang etcdMonitor ./bpf_src/etcd_monitor.c -- -I../bpf_common

// etcdEvent represents a raw event from eBPF
type etcdEvent struct {
	Timestamp uint64
	PID       uint32
	TID       uint32
	EventType uint8
	_         [3]byte // padding
	SrcIP     uint32
	DstIP     uint32
	SrcPort   uint16
	DstPort   uint16
	DataLen   uint32
	Data      [256]byte
}

// eBPF components with PID management
type ebpfState struct {
	objs            *etcdMonitorObjects
	links           []link.Link
	reader          *ringbuf.Reader
	verifiedProcs   map[int32]*EtcdProcessInfo
	discoveryTicker *time.Ticker
	discoveryCancel context.CancelFunc
}

// startEBPF initializes eBPF monitoring
func (c *Collector) startEBPF() error {
	start := time.Now()
	ctx, span := c.tracer.Start(c.ctx, "etcd-ebpf.start_ebpf")
	defer span.End()

	span.SetAttributes(
		attribute.String("ebpf.target", "etcd"),
		attribute.StringSlice("ebpf.syscalls", []string{"write", "fsync"}),
	)

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "ebpf_memlock"),
			))
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to remove memlock")
		return fmt.Errorf("failed to remove memlock: %w", err)
	}

	// Load eBPF objects
	objs := &etcdMonitorObjects{}
	if err := loadEtcdMonitorObjects(objs, nil); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "ebpf_load"),
			))
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "loading eBPF objects failed")
		return fmt.Errorf("loading eBPF objects: %w", err)
	}

	// Create eBPF state with PID management
	state := &ebpfState{
		objs:          objs,
		links:         make([]link.Link, 0),
		verifiedProcs: make(map[int32]*EtcdProcessInfo),
	}

	// Attach to write syscalls (etcd WAL writes)
	l1, err := link.Tracepoint("syscalls", "sys_enter_write", objs.TraceSysEnterWrite, nil)
	if err != nil {
		objs.Close()
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "ebpf_attach"),
				attribute.String("syscall", "write"),
			))
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "attaching write tracepoint failed")
		return fmt.Errorf("attaching write tracepoint: %w", err)
	}
	state.links = append(state.links, l1)
	span.AddEvent("Attached write tracepoint")

	// Attach to fsync syscalls (etcd WAL syncs)
	l2, err := link.Tracepoint("syscalls", "sys_enter_fsync", objs.TraceSysEnterFsync, nil)
	if err != nil {
		state.cleanup()
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "ebpf_attach"),
				attribute.String("syscall", "fsync"),
			))
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "attaching fsync tracepoint failed")
		return fmt.Errorf("attaching fsync tracepoint: %w", err)
	}
	state.links = append(state.links, l2)
	span.AddEvent("Attached fsync tracepoint")

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		state.cleanup()
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "ebpf_ringbuf"),
			))
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "creating ring buffer reader failed")
		return fmt.Errorf("creating ring buffer reader: %w", err)
	}
	state.reader = reader
	span.AddEvent("Created ring buffer reader")

	// Store state and start reading events
	c.ebpfState = state
	go c.readEBPFEvents()

	// Start process discovery and PID allowlist management
	go c.manageEtcdProcesses(state)

	// Record setup duration
	duration := time.Since(start)
	if c.processingTime != nil {
		c.processingTime.Record(ctx, duration.Seconds()*1000, metric.WithAttributes(
			attribute.String("operation", "ebpf_setup"),
		))
	}

	span.SetAttributes(
		attribute.Float64("setup_duration_seconds", duration.Seconds()),
		attribute.Int("tracepoints_attached", len(state.links)),
	)

	c.logger.Info("eBPF monitoring initialized",
		zap.Duration("setup_duration", duration),
		zap.Int("tracepoints", len(state.links)))

	return nil
}

// stopEBPF cleans up eBPF resources
func (c *Collector) stopEBPF() {
	if state, ok := c.ebpfState.(*ebpfState); ok && state != nil {
		state.cleanup()
		c.ebpfState = nil
	}
}

// cleanup releases all eBPF resources
func (s *ebpfState) cleanup() {
	// Stop process discovery
	if s.discoveryCancel != nil {
		s.discoveryCancel()
	}
	if s.discoveryTicker != nil {
		s.discoveryTicker.Stop()
	}

	// Close eBPF resources
	if s.reader != nil {
		s.reader.Close()
	}
	for _, l := range s.links {
		l.Close()
	}
	if s.objs != nil {
		s.objs.Close()
	}
}

// readEBPFEvents reads events from eBPF ring buffer
func (c *Collector) readEBPFEvents() {
	ctx, span := c.tracer.Start(c.ctx, "etcd-ebpf.read_ebpf_events")
	defer span.End()

	state, ok := c.ebpfState.(*ebpfState)
	if !ok || state == nil {
		span.AddEvent("eBPF state not available")
		return
	}

	var eventsProcessed uint64
	var eventsDropped uint64

	defer func() {
		span.SetAttributes(
			attribute.Int64("events_processed", int64(eventsProcessed)),
			attribute.Int64("events_dropped", int64(eventsDropped)),
		)
		c.logger.Info("eBPF event reader stopped",
			zap.Uint64("events_processed", eventsProcessed),
			zap.Uint64("events_dropped", eventsDropped))
	}()

	for {
		record, err := state.reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				span.AddEvent("Ring buffer closed")
				return
			}
			// Record error but continue
			if c.errorsTotal != nil {
				c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
					attribute.String("error_type", "ebpf_read"),
				))
			}
			span.AddEvent("Ring buffer read error",
				trace.WithAttributes(attribute.String("error", err.Error())))
			continue
		}

		start := time.Now()

		// Parse the raw event
		var event etcdEvent
		if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &event); err != nil {
			if c.errorsTotal != nil {
				c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
					attribute.String("error_type", "ebpf_parse"),
				))
			}
			continue
		}

		// Track syscall events
		if c.syscallsMonitored != nil {
			c.syscallsMonitored.Add(ctx, 1, metric.WithAttributes(
				attribute.String("syscall_type", fmt.Sprintf("%d", event.EventType)),
				attribute.Int("pid", int(event.PID)),
			))
		}

		// Create strongly-typed eBPF event data
		eventData := EBPFEventData{
			Timestamp: event.Timestamp,
			PID:       event.PID,
			TID:       event.TID,
			Type:      uint32(event.EventType), // Raw type, no interpretation
			DataLen:   event.DataLen,
		}

		// Add network info if present
		if event.SrcIP != 0 || event.DstIP != 0 {
			eventData.SrcIP = fmt.Sprintf("%d.%d.%d.%d",
				byte(event.SrcIP), byte(event.SrcIP>>8),
				byte(event.SrcIP>>16), byte(event.SrcIP>>24))
			eventData.DstIP = fmt.Sprintf("%d.%d.%d.%d",
				byte(event.DstIP), byte(event.DstIP>>8),
				byte(event.DstIP>>16), byte(event.DstIP>>24))
			eventData.SrcPort = event.SrcPort
			eventData.DstPort = event.DstPort
		}

		// Include raw data if present and enabled
		if c.config.CaptureDataPayload && event.DataLen > 0 {
			maxCapture := uint32(c.config.MaxDataCaptureSize)
			if event.DataLen > maxCapture {
				event.DataLen = maxCapture
			}
			if event.DataLen <= 256 {
				eventData.RawData = event.Data[:event.DataLen]
			}
		}

		collectorEvent := c.createCollectorEvent(ctx, &event, &eventData, state)

		select {
		case c.events <- collectorEvent:
			eventsProcessed++
			// Record event metric
			if c.eventsProcessed != nil {
				c.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
					attribute.String("event_type", "ebpf_syscall"),
					attribute.String("syscall_type", fmt.Sprintf("%d", event.EventType)),
					attribute.Int("pid", int(event.PID)),
				))
			}

			// Update stats
			c.mu.Lock()
			c.stats.EventsProcessed++
			c.stats.LastEventTime = time.Now()
			c.mu.Unlock()

		case <-c.ctx.Done():
			span.AddEvent("Context cancelled")
			return
		default:
			// Buffer full, drop event
			eventsDropped++
			if c.errorsTotal != nil {
				c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
					attribute.String("error_type", "ebpf_buffer_full"),
				))
			}
			if c.droppedEvents != nil {
				c.droppedEvents.Add(ctx, 1, metric.WithAttributes(
					attribute.String("event_type", "ebpf_syscall"),
				))
			}
			c.mu.Lock()
			c.stats.ErrorCount++
			c.mu.Unlock()
		}

		// Record processing time periodically
		if eventsProcessed%1000 == 0 && eventsProcessed > 0 {
			processingTime := time.Since(start)
			if c.processingTime != nil {
				c.processingTime.Record(ctx, processingTime.Seconds()*1000, metric.WithAttributes(
					attribute.String("operation", "ebpf_event_processing"),
				))
			}
		}
	}
}

// createCollectorEvent creates a CollectorEvent with full context from eBPF syscall data
func (c *Collector) createCollectorEvent(ctx context.Context, ebpfEvent *etcdEvent, eventData *EBPFEventData, state *ebpfState) *domain.CollectorEvent {
	timestamp := time.Now()
	eventID := fmt.Sprintf("etcd-ebpf-%d-%d-%d", ebpfEvent.PID, ebpfEvent.Timestamp, ebpfEvent.EventType)

	// Determine syscall name from event type
	syscallName := "unknown"
	switch ebpfEvent.EventType {
	case 1: // EVENT_WRITE
		syscallName = "write"
	case 2: // EVENT_FSYNC
		syscallName = "fsync"
	}

	// Extract process info from verified processes
	var processInfo *EtcdProcessInfo
	if state != nil && state.verifiedProcs != nil {
		processInfo = state.verifiedProcs[int32(ebpfEvent.PID)]
	}

	// Build correlation hints from eBPF context
	correlationHints := c.buildCorrelationHints(ebpfEvent, processInfo)

	// Try to extract K8s context from cgroup path
	k8sContext := c.extractK8sContextFromCgroup(ebpfEvent.PID)

	// Build trace context
	traceContext := c.extractTraceContextFromSpan(ctx)

	// Create syscall data
	syscallData := &domain.SystemCallData{
		Number:    int64(ebpfEvent.EventType),
		Name:      syscallName,
		PID:       int32(ebpfEvent.PID),
		TID:       int32(ebpfEvent.TID),
		Arguments: []domain.SystemCallArg{},
	}

	// Add file descriptor info if available
	if eventData.DataLen >= 4 {
		fd := int32(ebpfEvent.Data[0]) | int32(ebpfEvent.Data[1])<<8 |
			int32(ebpfEvent.Data[2])<<16 | int32(ebpfEvent.Data[3])<<24
		syscallData.Arguments = append(syscallData.Arguments, domain.SystemCallArg{
			Index: 0,
			Type:  "int",
			Value: fmt.Sprintf("%d", fd),
		})
	}

	// Create process data if we have info
	var processData *domain.ProcessData
	if processInfo != nil {
		processData = &domain.ProcessData{
			PID:       int32(ebpfEvent.PID),
			PPID:      int32(processInfo.PPID),
			Command:   processInfo.Comm,
			StartTime: processInfo.StartTime,
		}
	}

	// Create ETCD data
	etcdData := &domain.ETCDData{
		Operation: syscallName,
		Key:       fmt.Sprintf("pid:%d", ebpfEvent.PID), // Use PID as key for correlation
		Revision:  int64(ebpfEvent.Timestamp),
		Duration:  0, // Not measured in eBPF
	}

	return &domain.CollectorEvent{
		EventID:   eventID,
		Timestamp: timestamp,
		Type:      domain.EventTypeETCD,
		Source:    c.name,

		EventData: domain.EventDataContainer{
			SystemCall: syscallData,
			Process:    processData,
			ETCD:       etcdData,
		},

		Metadata: domain.EventMetadata{
			Priority:      domain.PriorityLow, // Syscall events are low priority
			Tags:          []string{"etcd", "syscall", syscallName},
			Labels:        map[string]string{"collector": "etcd-ebpf", "type": "syscall"},
			Attributes:    map[string]string{"pid": fmt.Sprintf("%d", ebpfEvent.PID)},
			SchemaVersion: "1.0",
		},

		CorrelationHints: &correlationHints,
		K8sContext:       k8sContext,
		TraceContext:     traceContext,
		CollectionContext: func() *domain.CollectionContext {
			ctx := c.buildCollectionContext()
			return &ctx
		}(),
	}
}

// buildCorrelationHints builds correlation hints from eBPF event data
func (c *Collector) buildCorrelationHints(event *etcdEvent, processInfo *EtcdProcessInfo) domain.CorrelationHints {
	hints := domain.CorrelationHints{
		ProcessID: int32(event.PID),
		NodeName:  "", // NodeName not available in config
	}

	// ParentPID field doesn't exist in domain.CorrelationHints
	// This info is already in ProcessData

	// Try to extract container ID from cgroup (will be enhanced later)
	if cgroupID := c.extractCgroupID(event.PID); cgroupID != "" {
		hints.ContainerID = cgroupID
	}

	return hints
}

// extractK8sContextFromCgroup attempts to extract K8s context from cgroup path
func (c *Collector) extractK8sContextFromCgroup(pid uint32) *domain.K8sContext {
	// This would parse /proc/{pid}/cgroup to extract pod UID and container ID
	// Example: /sys/fs/cgroup/kubepods/burstable/pod<uid>/<container_id>
	// For now, return nil as this requires proc filesystem access
	return nil
}

// extractCgroupID extracts cgroup ID for a process
func (c *Collector) extractCgroupID(pid uint32) string {
	// Would read from /proc/{pid}/cgroup
	// For now, return empty
	return ""
}

// extractTraceContextFromSpan extracts trace context from current span
func (c *Collector) extractTraceContextFromSpan(ctx context.Context) *domain.TraceContext {
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return nil
	}

	return &domain.TraceContext{
		TraceID:    span.SpanContext().TraceID(),
		SpanID:     span.SpanContext().SpanID(),
		TraceFlags: span.SpanContext().TraceFlags(),
		TraceState: span.SpanContext().TraceState(),
		Baggage:    make(map[string]string),
	}
}

// buildCollectionContext builds collection context
func (c *Collector) buildCollectionContext() domain.CollectionContext {
	c.mu.RLock()
	defer c.mu.RUnlock()

	hostname, _ := os.Hostname()

	return domain.CollectionContext{
		CollectorVersion: "1.0.0",
		HostInfo: domain.HostInfo{
			Hostname: hostname,
		},
		CollectionConfig: domain.CollectionConfig{
			FlushInterval:   time.Second,
			BufferSize:      100,
			SamplingRate:    1.0,
			EnabledFeatures: []string{"etcd"},
		},
		BufferStats: domain.BufferStats{
			EventsQueued:  int64(len(c.events)),
			EventsDropped: c.stats.EventsDropped,
			BufferSize:    int64(cap(c.events)),
		},
	}
}

// manageEtcdProcesses discovers and manages verified etcd processes
func (c *Collector) manageEtcdProcesses(state *ebpfState) {
	ctx, cancel := context.WithCancel(c.ctx)
	state.discoveryCancel = cancel

	// Initial discovery
	c.discoverEtcdProcesses(ctx, state)

	// Set up periodic discovery
	state.discoveryTicker = time.NewTicker(time.Duration(c.config.ProcessDiscoveryInterval) * time.Second)
	defer state.discoveryTicker.Stop()

	ctx, span := c.tracer.Start(ctx, "etcd-ebpf.manage_processes")
	defer span.End()

	for {
		select {
		case <-ctx.Done():
			span.AddEvent("Process management stopped")
			return
		case <-state.discoveryTicker.C:
			c.discoverEtcdProcesses(ctx, state)
		}
	}
}

// discoverEtcdProcesses discovers and validates etcd processes on the system
func (c *Collector) discoverEtcdProcesses(ctx context.Context, state *ebpfState) {
	ctx, span := c.tracer.Start(ctx, "etcd-ebpf.discover_processes")
	defer span.End()

	procs, err := c.findEtcdProcesses()
	if err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "process_discovery"),
			))
		}
		span.RecordError(err)
		c.logger.Warn("Failed to discover etcd processes", zap.Error(err))
		return
	}

	span.SetAttributes(
		attribute.Int("processes_discovered", len(procs)),
	)

	// Update verified processes map
	newVerified := make(map[int32]*EtcdProcessInfo)
	currentTime := time.Now()

	for _, proc := range procs {
		// Validate this is a legitimate etcd process
		if c.validateEtcdProcess(proc) {
			proc.VerifiedAt = currentTime
			newVerified[proc.PID] = proc

			// Add to eBPF PID allowlist with current timestamp
			key := uint32(proc.PID)
			value := uint64(currentTime.UnixNano())
			if err := state.objs.EtcdPids.Update(&key, &value, 0); err != nil {
				c.logger.Warn("Failed to update PID allowlist",
					zap.Int32("pid", proc.PID), zap.Error(err))
			}
		}
	}

	// Clean up old verified processes that are no longer running
	for pid := range state.verifiedProcs {
		if _, exists := newVerified[pid]; !exists {
			// Remove from eBPF map
			key := uint32(pid)
			if err := state.objs.EtcdPids.Delete(&key); err != nil {
				c.logger.Warn("Failed to remove PID from allowlist",
					zap.Int32("pid", pid), zap.Error(err))
			}
		}
	}

	state.verifiedProcs = newVerified

	// Update metrics
	if c.processesTracked != nil {
		c.processesTracked.Set(ctx, int64(len(newVerified)), metric.WithAttributes(
			attribute.String("operation", "process_tracking"),
		))
	}

	c.logger.Debug("Updated etcd process allowlist",
		zap.Int("verified_processes", len(newVerified)))

	span.SetAttributes(
		attribute.Int("verified_processes", len(newVerified)),
	)
}

// findEtcdProcesses scans /proc to find potential etcd processes
func (c *Collector) findEtcdProcesses() ([]*EtcdProcessInfo, error) {
	var processes []*EtcdProcessInfo

	procDir := "/proc"
	entries, err := os.ReadDir(procDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read /proc directory: %w", err)
	}

	// Compile regex for PID directories once
	pidRegex := regexp.MustCompile(`^\d+$`)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Check if directory name is a PID
		if !pidRegex.MatchString(entry.Name()) {
			continue
		}

		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		proc, err := c.readProcessInfo(int32(pid))
		if err != nil {
			continue // Skip processes we can't read
		}

		// Only consider processes with "etcd" in the name
		if strings.Contains(proc.Comm, "etcd") {
			processes = append(processes, proc)
		}
	}

	return processes, nil
}

// readProcessInfo reads process information from /proc/{pid}/
func (c *Collector) readProcessInfo(pid int32) (*EtcdProcessInfo, error) {
	procPath := fmt.Sprintf("/proc/%d", pid)

	// Read comm (command name)
	commData, err := os.ReadFile(filepath.Join(procPath, "comm"))
	if err != nil {
		return nil, fmt.Errorf("failed to read comm: %w", err)
	}
	comm := strings.TrimSpace(string(commData))

	// Read cmdline (full command line)
	cmdlineData, err := os.ReadFile(filepath.Join(procPath, "cmdline"))
	if err != nil {
		return nil, fmt.Errorf("failed to read cmdline: %w", err)
	}
	// cmdline is null-separated, convert to space-separated
	cmdline := strings.ReplaceAll(string(cmdlineData), "\x00", " ")
	cmdline = strings.TrimSpace(cmdline)

	// Read stat for PPID and start time
	statData, err := os.ReadFile(filepath.Join(procPath, "stat"))
	if err != nil {
		return nil, fmt.Errorf("failed to read stat: %w", err)
	}

	// Parse stat file - PPID is field 4 (1-indexed)
	statFields := strings.Fields(string(statData))
	if len(statFields) < 22 {
		return nil, fmt.Errorf("invalid stat format")
	}

	ppid, err := strconv.Atoi(statFields[3]) // Field 4, 0-indexed = 3
	if err != nil {
		return nil, fmt.Errorf("failed to parse PPID: %w", err)
	}

	// Field 22 is start time in clock ticks since boot
	startTimeTicks, err := strconv.ParseInt(statFields[21], 10, 64) // Field 22, 0-indexed = 21
	if err != nil {
		return nil, fmt.Errorf("failed to parse start time: %w", err)
	}

	// Convert to actual time (approximate)
	startTime := time.Now().Add(-time.Duration(startTimeTicks) * time.Millisecond / 100) // Rough conversion

	return &EtcdProcessInfo{
		PID:       pid,
		PPID:      int32(ppid),
		Comm:      comm,
		Cmdline:   cmdline,
		StartTime: startTime,
	}, nil
}

// validateEtcdProcess performs comprehensive validation of etcd process
func (c *Collector) validateEtcdProcess(proc *EtcdProcessInfo) bool {
	// Layer 1: Exact command name validation
	if proc.Comm != "etcd" {
		return false // Must be exactly "etcd", not "etcd-backup", "etcdctl", etc.
	}

	// Layer 2: Command line validation
	if proc.Cmdline == "" {
		return false // Empty command line is suspicious
	}

	// Check that the command line contains etcd binary
	if !strings.Contains(proc.Cmdline, "etcd") {
		return false
	}

	// Layer 3: Reasonable process characteristics
	if proc.PID <= 0 || proc.PPID < 0 {
		return false // Invalid PID/PPID
	}

	// Layer 4: Check for legitimate etcd command line patterns
	// etcd typically has these patterns:
	// - etcd --data-dir=...
	// - /usr/bin/etcd --name=...
	// - Contains common etcd flags
	cmdLower := strings.ToLower(proc.Cmdline)
	hasEtcdFlags := strings.Contains(cmdLower, "--data-dir") ||
		strings.Contains(cmdLower, "--listen-client") ||
		strings.Contains(cmdLower, "--listen-peer") ||
		strings.Contains(cmdLower, "--name") ||
		strings.Contains(cmdLower, "--initial-") ||
		strings.Contains(cmdLower, "etcd") // At minimum, must contain etcd

	if !hasEtcdFlags {
		c.logger.Debug("Process failed etcd flag validation",
			zap.Int32("pid", proc.PID),
			zap.String("cmdline", proc.Cmdline))
		return false
	}

	// Layer 5: Process age check - etcd processes should run for a while
	processAge := time.Since(proc.StartTime)
	if processAge < 1*time.Second {
		// Allow very new processes, but this could be a sign of something suspicious
		c.logger.Debug("Very new etcd process detected",
			zap.Int32("pid", proc.PID),
			zap.Duration("age", processAge))
	}

	c.logger.Debug("Validated etcd process",
		zap.Int32("pid", proc.PID),
		zap.String("comm", proc.Comm),
		zap.String("cmdline", proc.Cmdline),
		zap.Int32("ppid", proc.PPID),
		zap.Duration("age", processAge))

	return true
}
