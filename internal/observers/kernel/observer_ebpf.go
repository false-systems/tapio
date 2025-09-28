//go:build linux
// +build linux

package kernel

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/internal/observers/kernel/bpf"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// eBPF generation is handled by bpf/generate.go

// ebpfComponents holds Linux-specific eBPF components
type ebpfComponents struct {
	objs   *bpf.KernelmonitorObjects
	links  []link.Link
	reader *ringbuf.Reader
}

// startEBPF initializes and starts eBPF monitoring on Linux
func (c *Observer) startEBPF() error {
	c.logger.Debug("Starting eBPF kernel monitoring")

	// Remove memory lock limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		c.logger.Warn("Failed to remove memlock limit", zap.Error(err))
	}

	// Load pre-compiled eBPF objects
	objs := &bpf.KernelmonitorObjects{}
	if err := bpf.LoadKernelmonitorObjects(objs, nil); err != nil {
		var ve *ebpf.VerifierError
		if errors.As(err, &ve) {
			c.logger.Error("eBPF verifier error", zap.String("details", ve.Error()))
		}
		return fmt.Errorf("failed to load eBPF objects: %w", err)
	}

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		objs.Close()
		return fmt.Errorf("failed to create ring buffer reader: %w", err)
	}

	var links []link.Link

	// Attach tracepoints for openat syscall monitoring (entry and exit)
	if objs.TraceOpenat != nil {
		openatLink, err := link.Tracepoint("syscalls", "sys_enter_openat", objs.TraceOpenat, nil)
		if err != nil {
			c.logger.Warn("Failed to attach openat entry tracepoint", zap.Error(err))
		} else {
			links = append(links, openatLink)
			c.logger.Debug("Attached openat entry tracepoint")
		}
	}

	if objs.TraceOpenatExit != nil {
		openatExitLink, err := link.Tracepoint("syscalls", "sys_exit_openat", objs.TraceOpenatExit, nil)
		if err != nil {
			c.logger.Warn("Failed to attach openat exit tracepoint", zap.Error(err))
		} else {
			links = append(links, openatExitLink)
			c.logger.Debug("Attached openat exit tracepoint")
		}
	}

	if len(links) == 0 {
		reader.Close()
		objs.Close()
		return fmt.Errorf("failed to attach any tracepoints")
	}

	// Store eBPF state as interface{}
	c.ebpfState = &ebpfComponents{
		objs:   objs,
		links:  links,
		reader: reader,
	}

	c.logger.Info("eBPF kernel monitoring started successfully",
		zap.Int("attached_links", len(links)))

	return nil
}

// stopEBPF cleans up eBPF resources on Linux
func (c *Observer) stopEBPF() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ebpfState == nil {
		return
	}

	state, ok := c.ebpfState.(*ebpfComponents)
	if !ok {
		c.logger.Error("Invalid eBPF state type")
		return
	}

	c.logger.Debug("Stopping eBPF kernel monitoring")

	// Detach links first to stop new events
	for _, l := range state.links {
		if err := l.Close(); err != nil {
			c.logger.Warn("Failed to close eBPF link", zap.Error(err))
		}
	}

	// Close eBPF objects
	if err := state.objs.Close(); err != nil {
		c.logger.Warn("Failed to close eBPF objects", zap.Error(err))
	}

	// Close reader last after links are detached
	if state.reader != nil {
		if err := state.reader.Close(); err != nil {
			c.logger.Warn("Failed to close ring buffer reader", zap.Error(err))
		}
	}

	c.ebpfState = nil
	c.logger.Info("eBPF kernel monitoring stopped")
}

// processEvents reads and processes events from eBPF on Linux
func (c *Observer) processEvents() {
	ctx := c.LifecycleManager.Context()
	_, span := c.tracer.Start(ctx, "kernel.process_events")
	defer span.End()

	c.mu.RLock()
	state, ok := c.ebpfState.(*ebpfComponents)
	c.mu.RUnlock()

	if !ok || state == nil {
		// This can happen during shutdown or if eBPF is not initialized
		c.logger.Debug("eBPF state not ready for event processing")
		span.SetStatus(codes.Error, "eBPF state not ready")
		return
	}

	c.logger.Debug("Starting eBPF event processing loop")

	for {
		select {
		case <-c.LifecycleManager.Context().Done():
			c.logger.Debug("Event processing stopped")
			return
		default:
			// Check if state is still valid
			c.mu.RLock()
			currentState := c.ebpfState
			c.mu.RUnlock()

			if currentState == nil {
				c.logger.Debug("eBPF state cleared, exiting event processing")
				return
			}

			// Check if reader is still available before reading
			if state.reader == nil {
				c.logger.Debug("Ring buffer reader not available, exiting")
				return
			}

			// Use ReadTimeout to allow periodic context checks
			record, err := state.reader.ReadTimeout(100 * time.Millisecond)
			if err != nil {
				if err == ringbuf.ErrClosed {
					c.logger.Debug("Ring buffer closed, exiting event processing")
					return
				}
				// Check for timeout - this is normal and allows us to check context
				if err == os.ErrDeadlineExceeded {
					continue // Check context at top of loop
				}
				// Check for common closure errors
				if strings.Contains(err.Error(), "file already closed") ||
					strings.Contains(err.Error(), "closed") {
					c.logger.Debug("Ring buffer closed during read, exiting")
					return
				}
				c.logger.Debug("Ring buffer read error", zap.Error(err))
				c.BaseObserver.RecordError(err)
				continue
			}

			// Process the event
			if err := c.handleKernelEvent(record.RawSample); err != nil {
				c.logger.Warn("Failed to handle kernel event", zap.Error(err))
				c.BaseObserver.RecordError(err)
			} else {
				// Successfully processed
				c.logger.Debug("Successfully processed kernel event")
			}
		}
	}
}

// handleKernelEvent processes a single kernel event from eBPF
func (c *Observer) handleKernelEvent(data []byte) error {
	start := time.Now()
	ctx := context.Background()
	_, span := c.tracer.Start(ctx, "kernel.handle_event")
	defer span.End()

	// Record event size metric
	c.eventSize.Record(ctx, int64(len(data)))
	// Accept both 152 bytes (from eBPF) and 160 bytes (Go struct with padding)
	minSize := 152 // Actual eBPF event size
	if len(data) < minSize {
		return fmt.Errorf("event data too small: got %d bytes, need at least %d", len(data), minSize)
	}

	// Parse the kernel event
	// Pad the data to 160 bytes if it's 152 (eBPF size)
	eventData := data
	if len(data) == 152 {
		// Add 8 bytes of padding to match Go struct size
		padded := make([]byte, 160)
		copy(padded, data)
		eventData = padded
	}

	var event KernelEvent
	reader := bytes.NewReader(eventData)
	if err := binary.Read(reader, binary.LittleEndian, &event); err != nil {
		return fmt.Errorf("failed to parse kernel event: %w", err)
	}

	// Convert to domain event - use the existing method from observer.go
	collectorEvent := c.convertKernelEvent(&event)

	// Update metrics based on event type
	c.syscallsTracked.Add(ctx, 1)
	switch event.EventType {
	case uint32(EventTypeConfigMapAccess):
		c.configAccesses.Add(ctx, 1)
	case uint32(EventTypeSecretAccess):
		c.secretAccesses.Add(ctx, 1)
	case uint32(EventTypeConfigAccessFailed):
		c.accessFailures.Add(ctx, 1)
	}

	// Send event
	if c.EventChannelManager.SendEvent(collectorEvent) {
		c.BaseObserver.RecordEvent()
		c.eventsProcessed.Add(ctx, 1, metric.WithAttributes(
			attribute.String("event_type", fmt.Sprintf("%d", event.EventType)),
			attribute.Uint64("pid", uint64(event.PID)),
		))
		c.logger.Debug("Processed kernel event",
			zap.Uint32("event_type", event.EventType),
			zap.Uint32("pid", event.PID))
		span.SetStatus(codes.Ok, "Event processed")
	} else {
		c.BaseObserver.RecordDrop()
		c.BaseObserver.RecordError(fmt.Errorf("dropped kernel event - channel full or validation failed"))
		c.eventsDropped.Add(ctx, 1, metric.WithAttributes(
			attribute.String("reason", "channel_full_or_validation_failed"),
		))
		c.logger.Error("Dropped kernel event - channel full or validation failed",
			zap.String("event_id", collectorEvent.EventID))
		span.RecordError(fmt.Errorf("dropped event"))
		span.SetStatus(codes.Error, "Event dropped")
	}

	// Record processing time
	duration := time.Since(start).Milliseconds()
	c.processingTime.Record(ctx, float64(duration), metric.WithAttributes(
		attribute.String("event_type", fmt.Sprintf("%d", event.EventType)),
	))

	span.SetAttributes(
		attribute.String("event_id", collectorEvent.EventID),
		attribute.String("event_type", fmt.Sprintf("%d", event.EventType)),
		attribute.Int64("processing_time_ms", duration),
	)

	return nil
}

// convertKernelEventEBPF converts a KernelEvent to domain.CollectorEvent with extended info from eBPF
func (c *Observer) convertKernelEventEBPF(event *KernelEvent) (*domain.CollectorEvent, error) {
	// Extract command string
	comm := strings.TrimRight(string(event.Comm[:]), "\x00")
	podUID := strings.TrimRight(string(event.PodUID[:]), "\x00")

	// Determine event type based on kernel event type
	var eventType domain.CollectorEventType
	switch event.EventType {
	case uint32(EventTypeConfigMapAccess):
		eventType = domain.EventTypeKernelFS
	case uint32(EventTypeSecretAccess):
		eventType = domain.EventTypeKernelFS
	case uint32(EventTypePodSyscall):
		eventType = domain.EventTypeKernelSyscall
	case uint32(EventTypeConfigAccessFailed):
		eventType = domain.EventTypeKernelFS
	default:
		eventType = domain.EventTypeKernelProcess
	}

	// Parse config info from event data
	var configInfo ConfigInfo
	reader := bytes.NewReader(event.Data[:])
	if err := binary.Read(reader, binary.LittleEndian, &configInfo); err == nil {
		// Successfully parsed config info
	}

	mountPath := strings.TrimRight(string(configInfo.MountPath[:]), "\x00")

	return &domain.CollectorEvent{
		EventID:   fmt.Sprintf("kernel-%d-%d", event.PID, event.Timestamp),
		Timestamp: time.Unix(0, int64(event.Timestamp)),
		Type:      eventType,
		Source:    c.config.Name,
		Severity:  c.mapEventSeverity(event.EventType, configInfo.ErrorCode),

		EventData: domain.EventDataContainer{
			Kernel: &domain.KernelData{
				EventType:    fmt.Sprintf("kernel_event_%d", event.EventType),
				PID:          int32(event.PID),
				PPID:         0, // Not available in kernel events
				UID:          0, // Not available in kernel events
				GID:          0, // Not available in kernel events
				Command:      comm,
				CgroupID:     event.CgroupID,
				ErrorMessage: c.getErrorDescriptionEBPF(configInfo.ErrorCode),
			},
		},

		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"observer":   c.config.Name,
				"version":    "1.0.0",
				"event_type": fmt.Sprintf("%d", event.EventType),
				"pid":        fmt.Sprintf("%d", event.PID),
				"tid":        fmt.Sprintf("%d", event.TID),
				"command":    comm,
				"cgroup_id":  fmt.Sprintf("%d", event.CgroupID),
				"pod_uid":    podUID,
				"mount_path": mountPath,
				"error_code": fmt.Sprintf("%d", configInfo.ErrorCode),
			},
		},
	}, nil
}

// mapEventSeverity determines event severity based on type and error code
func (c *Observer) mapEventSeverity(eventType uint32, errorCode int32) domain.EventSeverity {
	if errorCode != 0 {
		// Failed access is always a warning or error
		if errorCode == 13 { // EACCES
			return domain.EventSeverityError
		}
		return domain.EventSeverityWarning
	}

	switch eventType {
	case uint32(EventTypeSecretAccess):
		return domain.EventSeverityWarning // Secret access should be monitored
	case uint32(EventTypeConfigMapAccess):
		return domain.EventSeverityInfo // ConfigMap access is normal
	default:
		return domain.EventSeverityInfo
	}
}

// getErrorDescriptionEBPF returns a human-readable error description from eBPF
func (c *Observer) getErrorDescriptionEBPF(errorCode int32) string {
	switch errorCode {
	case 0:
		return ""
	case 2:
		return "No such file or directory (ENOENT)"
	case 13:
		return "Permission denied (EACCES)"
	case 22:
		return "Invalid argument (EINVAL)"
	default:
		return fmt.Sprintf("Error code %d", errorCode)
	}
}
