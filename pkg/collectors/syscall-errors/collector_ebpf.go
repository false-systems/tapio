//go:build linux
// +build linux

package syscallerrors

import (
	"bytes"
	"context"
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

// eBPF generation is handled by bpf/generate.go

// ebpfState holds Linux-specific eBPF components
type ebpfState struct {
	objs   *syscallmonitorObjects
	links  []link.Link
	reader *ringbuf.Reader
}

// startEBPF initializes and attaches eBPF programs (Linux-specific)
func (c *Collector) startEBPF() error {
	ctx, span := c.tracer.Start(c.LifecycleManager.Context(), "syscall_errors.start_ebpf")
	defer span.End()

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

	// Create eBPF state
	state := &ebpfState{
		objs:  objs,
		links: make([]link.Link, 0),
	}

	// Attach tracepoints
	if err := c.attachTracepoints(state); err != nil {
		objs.Close()
		return fmt.Errorf("attaching tracepoints: %w", err)
	}

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		c.cleanupEBPF(state)
		return fmt.Errorf("creating ring buffer reader: %w", err)
	}
	state.reader = reader

	// Store eBPF state
	c.ebpfState = state

	c.logger.Info("eBPF programs loaded and attached successfully")
	return nil
}

// attachTracepoints attaches the eBPF programs to kernel tracepoints
func (c *Collector) attachTracepoints(state *ebpfState) error {
	// Attach sys_enter tracepoint
	enterLink, err := link.Tracepoint("raw_syscalls", "sys_enter", state.objs.TraceSysEnter, nil)
	if err != nil {
		return fmt.Errorf("attaching sys_enter tracepoint: %w", err)
	}
	state.links = append(state.links, enterLink)

	// Attach sys_exit tracepoint
	exitLink, err := link.Tracepoint("raw_syscalls", "sys_exit", state.objs.TraceSysExit, nil)
	if err != nil {
		return fmt.Errorf("attaching sys_exit tracepoint: %w", err)
	}
	state.links = append(state.links, exitLink)

	return nil
}

// stopEBPF stops and cleans up eBPF resources (Linux-specific)
func (c *Collector) stopEBPF() {
	if c.ebpfState != nil {
		if state, ok := c.ebpfState.(*ebpfState); ok {
			c.cleanupEBPF(state)
		}
	}
}

// cleanupEBPF cleans up eBPF resources
func (c *Collector) cleanupEBPF(state *ebpfState) {
	// Close reader first
	if state.reader != nil {
		state.reader.Close()
	}

	// Detach links
	for _, l := range state.links {
		if l != nil {
			l.Close()
		}
	}

	// Close eBPF objects
	if state.objs != nil {
		state.objs.Close()
	}
}

// readEvents reads and processes events from the ring buffer (Linux-specific)
func (c *Collector) readEvents() {
	state, ok := c.ebpfState.(*ebpfState)
	if !ok || state == nil || state.reader == nil {
		c.logger.Error("Invalid eBPF state, cannot read events")
		return
	}

	for {
		select {
		case <-c.LifecycleManager.Context().Done():
			c.logger.Info("Stopping syscall error event processing")
			return
		default:
			record, err := state.reader.Read()
			if err != nil {
				if errors.Is(err, ringbuf.ErrClosed) {
					return
				}
				if c.errorsTotal != nil {
					c.errorsTotal.Add(c.LifecycleManager.Context(), 1, metric.WithAttributes(
						attribute.String("error_type", "ring_buffer_read"),
					))
				}

				// Rate-limited error logging to prevent log spam
				c.consecutiveErrors++
				now := time.Now()
				if now.Sub(c.lastErrorLogTime) > c.errorLogInterval {
					c.logger.Error("Failed to read from ring buffer",
						zap.Error(err),
						zap.Int("consecutive_errors", c.consecutiveErrors))
					c.lastErrorLogTime = now
					c.consecutiveErrors = 0
				}

				// Add a small delay to avoid busy-waiting on persistent errors
				time.Sleep(50 * time.Millisecond)
				continue
			}

			// Reset error counter on successful read
			c.consecutiveErrors = 0

			// Process the event
			if err := c.processRawEvent(record.RawSample); err != nil {
				c.logger.Warn("Failed to process event", zap.Error(err))
				if c.errorsTotal != nil {
					c.errorsTotal.Add(c.LifecycleManager.Context(), 1, metric.WithAttributes(
						attribute.String("error_type", "event_processing_failed"),
					))
				}
			}
		}
	}
}

// processRawEvent processes a single raw eBPF event
func (c *Collector) processRawEvent(data []byte) error {
	ctx, span := c.tracer.Start(c.LifecycleManager.Context(), "syscall_errors.process_event")
	defer span.End()

	start := time.Now()
	defer func() {
		if c.processingTime != nil {
			duration := time.Since(start).Seconds() * 1000
			c.processingTime.Record(ctx, duration)
		}
	}()

	// Parse the event
	expectedSize := int(unsafe.Sizeof(SyscallErrorEvent{}))
	if len(data) < expectedSize {
		return fmt.Errorf("event data too small: got %d bytes, expected %d", len(data), expectedSize)
	}

	// Validate exact size match to ensure struct alignment
	if len(data) != expectedSize {
		c.logger.Warn("Event size mismatch, potential struct alignment issue",
			zap.Int("got_size", len(data)),
			zap.Int("expected_size", expectedSize))
	}

	var event SyscallErrorEvent
	reader := bytes.NewReader(data[:expectedSize]) // Only read expected bytes
	if err := binary.Read(reader, binary.LittleEndian, &event); err != nil {
		return fmt.Errorf("failed to parse event: %w", err)
	}

	// Filter by category if enabled
	if !c.isCategoryEnabled(event.Category) {
		// Silently drop events from disabled categories
		return nil
	}

	// Convert to CollectorEvent
	collectorEvent := c.convertToCollectorEvent(&event)

	// Update metrics
	if c.eventsProcessed != nil {
		c.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
			attribute.String("error_type", getErrorName(event.ErrorCode)),
			attribute.String("category", getCategoryName(event.Category)),
		))
	}

	// Update specific error counters
	c.updateErrorMetrics(ctx, event.ErrorCode)

	// Send to event channel
	if c.EventChannelManager.SendEvent(collectorEvent) {
		span.SetAttributes(
			attribute.String("syscall", getSyscallName(event.SyscallNr)),
			attribute.String("error", getErrorName(event.ErrorCode)),
			attribute.Int("pid", int(event.PID)),
		)
		c.BaseCollector.RecordEvent()
	} else {
		// Channel full, drop event
		if c.eventsDropped != nil {
			c.eventsDropped.Add(ctx, 1, metric.WithAttributes(
				attribute.String("category", getCategoryName(event.Category)),
				attribute.String("syscall", getSyscallName(event.SyscallNr)),
			))
		}
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "channel_full"),
			))
		}
		c.BaseCollector.RecordError(fmt.Errorf("channel full, dropping event"))

		// Log warning with rate limiting
		now := time.Now()
		if now.Sub(c.lastErrorLogTime) > c.errorLogInterval {
			c.logger.Warn("Event channel full, dropping events",
				zap.String("syscall", getSyscallName(event.SyscallNr)),
				zap.String("error", getErrorName(event.ErrorCode)),
				zap.Int("pid", int(event.PID)))
			c.lastErrorLogTime = now
		}
	}

	return nil
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
	case 24: // EMFILE
		if c.emfileErrors != nil {
			c.emfileErrors.Add(ctx, 1)
		}
	case 122: // EDQUOT
		if c.edquotErrors != nil {
			c.edquotErrors.Add(ctx, 1)
		}
	}
}

// isCategoryEnabled checks if a category is enabled
func (c *Collector) isCategoryEnabled(category uint8) bool {
	categoryName := getCategoryName(category)
	// If no categories specified, enable all
	if len(c.config.EnabledCategories) == 0 {
		return true
	}
	// Check if category is explicitly enabled
	enabled, exists := c.config.EnabledCategories[categoryName]
	return exists && enabled
}

// getStatsImpl retrieves collector statistics from eBPF maps (Linux-specific)
func (c *Collector) getStatsImpl() (*CollectorStats, error) {
	state, ok := c.ebpfState.(*ebpfState)
	if !ok || state == nil || state.objs == nil {
		return nil, fmt.Errorf("eBPF not initialized")
	}

	stats := &CollectorStats{}

	// Read stats from eBPF map
	key := uint32(0)
	var values []uint64
	err := state.objs.Stats.Lookup(&key, &values)
	if err != nil {
		return nil, fmt.Errorf("failed to read stats: %w", err)
	}

	// Aggregate per-CPU values
	for _, v := range values {
		stats.TotalErrors += v
	}

	// Read specific error counters
	key = 1 // ENOSPC
	values = nil
	if err := state.objs.Stats.Lookup(&key, &values); err == nil {
		for _, v := range values {
			stats.ENOSPCCount += v
		}
	}

	key = 2 // ENOMEM
	values = nil
	if err := state.objs.Stats.Lookup(&key, &values); err == nil {
		for _, v := range values {
			stats.ENOMEMCount += v
		}
	}

	key = 3 // ECONNREFUSED
	values = nil
	if err := state.objs.Stats.Lookup(&key, &values); err == nil {
		for _, v := range values {
			stats.ECONNREFUSEDCount += v
		}
	}

	key = 4 // EIO
	values = nil
	if err := state.objs.Stats.Lookup(&key, &values); err == nil {
		for _, v := range values {
			stats.EIOCount += v
		}
	}

	return stats, nil
}
