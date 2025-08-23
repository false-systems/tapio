//go:build linux
// +build linux

package storageio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"time"
	"unsafe"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 storagemonitor ./bpf_src/storage_monitor_premium.c -- -I../bpf_common

// eBPF components - Linux-specific
type ebpfComponents struct {
	objs   *storagemonitorObjects
	links  []link.Link
	reader *ringbuf.Reader
}

// startEBPFImpl initializes eBPF monitoring - Linux only
func (c *Collector) startEBPFImpl() error {
	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		c.logger.Warn("Failed to remove memory limit", zap.Error(err))
	}

	// Load pre-compiled eBPF programs
	objs := &storagemonitorObjects{}
	if err := loadStoragemonitorObjects(objs, nil); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(c.ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "ebpf_load_failed"),
			))
		}
		return fmt.Errorf("loading eBPF objects: %w", err)
	}

	state := &ebpfComponents{
		objs:  objs,
		links: make([]link.Link, 0),
	}

	// Tracepoints auto-attach when loaded, no manual attachment needed

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		c.closeAllLinks(state.links)
		objs.Close()
		return fmt.Errorf("creating ring buffer reader: %w", err)
	}
	state.reader = reader

	c.ebpfState = state
	c.logger.Info("eBPF programs loaded and attached successfully",
		zap.Int("attached_probes", len(state.links)),
	)
	return nil
}

// attachVFSProbes attaches the configured VFS probes
func (c *Collector) attachVFSProbes(state *ebpfComponents) error {
	enabledProbes := c.config.GetEnabledVFSProbes()

	for _, probeType := range enabledProbes {
		if err := c.attachVFSProbe(state, probeType); err != nil {
			c.closeAllLinks(state.links)
			return fmt.Errorf("failed to attach %s probe: %w", probeType.String(), err)
		}
	}

	c.logger.Info("VFS probes attached successfully",
		zap.Int("probe_count", len(enabledProbes)),
		zap.Strings("probe_types", c.getProbeTypeStrings(enabledProbes)),
	)

	return nil
}

// attachVFSProbe attaches tracepoints for the premium collector
func (c *Collector) attachVFSProbe(state *ebpfComponents, probeType VFSProbeType) error {
	// Premium collector uses tracepoints, not kprobes
	// The eBPF programs are already attached via tracepoints in the SEC() definitions
	// No manual attachment needed for tracepoints
	return nil
}

// stopEBPFImpl stops eBPF monitoring - Linux only
func (c *Collector) stopEBPFImpl() {
	if c.ebpfState == nil {
		return
	}

	state, ok := c.ebpfState.(*ebpfComponents)
	if !ok {
		return
	}

	// Close reader first
	if state.reader != nil {
		state.reader.Close()
	}

	// Close all links
	c.closeAllLinks(state.links)

	// Close eBPF objects
	if state.objs != nil {
		state.objs.Close()
	}

	c.ebpfState = nil
	c.logger.Info("eBPF programs stopped")
}

// processStorageEventsImpl reads and processes eBPF events
func (c *Collector) processStorageEventsImpl() {
	defer c.wg.Done()

	if c.ebpfState == nil {
		return
	}

	state, ok := c.ebpfState.(*ebpfComponents)
	if !ok || state.reader == nil {
		return
	}

	c.logger.Info("Starting eBPF storage I/O event processing")

	for {
		select {
		case <-c.ctx.Done():
			c.logger.Info("Stopping eBPF storage I/O event processing")
			return
		default:
			record, err := state.reader.Read()
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

			// Process the raw event
			if err := c.processRawStorageEvent(record.RawSample); err != nil {
				c.logger.Warn("Failed to process storage event", zap.Error(err))
				if c.errorsTotal != nil {
					c.errorsTotal.Add(c.ctx, 1, metric.WithAttributes(
						attribute.String("error_type", "event_processing_failed"),
					))
				}
			}
		}
	}
}

// processRawStorageEvent processes a single raw eBPF storage event
func (c *Collector) processRawStorageEvent(data []byte) error {
	// Parse eBPF event
	rawEvent, err := c.parseStorageEventRaw(data)
	if err != nil {
		return fmt.Errorf("failed to parse raw event: %w", err)
	}

	// Convert to internal event structure
	storageEvent, err := c.convertRawToStorageEvent(rawEvent)
	if err != nil {
		return fmt.Errorf("failed to convert raw event: %w", err)
	}

	// Apply filtering
	if c.shouldFilterEvent(storageEvent) {
		return nil
	}

	// Process the event through the main processing pipeline
	return c.processStorageEvent(storageEvent)
}

// parseStorageEventRaw parses raw eBPF event data
func (c *Collector) parseStorageEventRaw(data []byte) (*StorageIOEventRaw, error) {
	if len(data) < int(unsafe.Sizeof(StorageIOEventRaw{})) {
		return nil, fmt.Errorf("event data too small: got %d bytes, expected %d", len(data), unsafe.Sizeof(StorageIOEventRaw{}))
	}

	// Parse the event
	var rawEvent StorageIOEventRaw
	reader := bytes.NewReader(data)
	if err := binary.Read(reader, binary.LittleEndian, &rawEvent); err != nil {
		return nil, fmt.Errorf("failed to parse event: %w", err)
	}

	// Validate the event
	if err := c.validateRawEvent(&rawEvent); err != nil {
		return nil, fmt.Errorf("invalid event: %w", err)
	}

	return &rawEvent, nil
}

// validateRawEvent validates a raw eBPF event
func (c *Collector) validateRawEvent(event *StorageIOEventRaw) error {
	if event.PID == 0 {
		return fmt.Errorf("invalid PID: 0")
	}

	if event.StartTimeNs == 0 || event.EndTimeNs == 0 {
		return fmt.Errorf("invalid timestamps: start=%d, end=%d", event.StartTimeNs, event.EndTimeNs)
	}

	if event.EndTimeNs < event.StartTimeNs {
		return fmt.Errorf("end time before start time: start=%d, end=%d", event.StartTimeNs, event.EndTimeNs)
	}

	if event.EventType == 0 || event.EventType > 6 {
		return fmt.Errorf("invalid event type: %d", event.EventType)
	}

	return nil
}

// convertRawToStorageEvent converts raw eBPF event to internal StorageIOEvent
func (c *Collector) convertRawToStorageEvent(rawEvent *StorageIOEventRaw) (*StorageIOEvent, error) {
	// Calculate duration
	duration := time.Duration(rawEvent.EndTimeNs - rawEvent.StartTimeNs)

	// Convert event type
	operation := c.convertEventType(rawEvent.EventType)
	vfsLayer := VFSProbeType(rawEvent.EventType).String()

	// Extract path and command strings
	path := bytesToString(rawEvent.Path[:])
	command := bytesToString(rawEvent.Comm[:])

	// Calculate timestamp from end time
	timestamp := time.Unix(0, int64(rawEvent.EndTimeNs))

	// Determine device string
	device := fmt.Sprintf("%d:%d", rawEvent.DevMajor, rawEvent.DevMinor)

	// Create storage event
	event := &StorageIOEvent{
		Operation: operation,
		Path:      path,
		Timestamp: timestamp,
		Size:      rawEvent.Size,
		Offset:    rawEvent.Offset,
		Duration:  duration,
		SlowIO:    duration > time.Duration(c.config.SlowIOThresholdMs)*time.Millisecond,
		BlockedIO: false, // Blocked I/O detection requires additional kernel tracing
		Device:    device,
		Inode:     rawEvent.Inode,
		PID:       int32(rawEvent.PID),
		PPID:      int32(rawEvent.PPID),
		UID:       int32(rawEvent.UID),
		GID:       int32(rawEvent.GID),
		Command:   command,
		CgroupID:  rawEvent.CgroupID,
		ErrorCode: rawEvent.ErrorCode,
		VFSLayer:  vfsLayer,
		Flags:     rawEvent.Flags,
		Mode:      rawEvent.Mode,
	}

	// Set error message if there's an error code
	if rawEvent.ErrorCode != 0 {
		event.ErrorMessage = c.getErrorMessage(rawEvent.ErrorCode)
	}

	return event, nil
}

// convertEventType converts numeric event type to operation string
func (c *Collector) convertEventType(eventType uint8) string {
	switch VFSProbeType(eventType) {
	case VFSProbeRead:
		return "read"
	case VFSProbeWrite:
		return "write"
	case VFSProbeFsync:
		return "fsync"
	case VFSProbeIterateDir:
		return "iterate_dir"
	case VFSProbeOpen:
		return "open"
	case VFSProbeClose:
		return "close"
	default:
		return fmt.Sprintf("unknown_%d", eventType)
	}
}

// shouldFilterEvent determines if an event should be filtered out
func (c *Collector) shouldFilterEvent(event *StorageIOEvent) bool {
	// Filter by minimum I/O size
	if event.Size > 0 && event.Size < c.config.MinIOSize {
		return true
	}

	// Filter excluded paths
	if c.config.ShouldExcludePath(event.Path) {
		return true
	}

	// Filter excluded processes
	if c.config.ShouldExcludeProcess(event.Command) {
		return true
	}

	// Apply include process filter if configured
	if !c.config.ShouldIncludeProcess(event.Command) {
		return true
	}

	return false
}

// getErrorMessage converts error code to human-readable message
func (c *Collector) getErrorMessage(errorCode int32) string {
	// Common error codes for I/O operations
	switch errorCode {
	case 2:
		return "No such file or directory"
	case 5:
		return "Input/output error"
	case 13:
		return "Permission denied"
	case 16:
		return "Device or resource busy"
	case 28:
		return "No space left on device"
	case 30:
		return "Read-only file system"
	default:
		return fmt.Sprintf("Error code %d", errorCode)
	}
}

// closeAllLinks closes all eBPF links
func (c *Collector) closeAllLinks(links []link.Link) {
	for _, l := range links {
		if l != nil {
			if err := l.Close(); err != nil {
				c.logger.Warn("Failed to close eBPF link", zap.Error(err))
			}
		}
	}
}

// getProbeTypeStrings converts probe types to string slice for logging
func (c *Collector) getProbeTypeStrings(probes []VFSProbeType) []string {
	strings := make([]string, len(probes))
	for i, probe := range probes {
		strings[i] = probe.String()
	}
	return strings
}

// bytesToString converts null-terminated byte array to string
func bytesToString(data []byte) string {
	n := bytes.IndexByte(data, 0)
	if n == -1 {
		n = len(data)
	}
	return string(data[:n])
}
