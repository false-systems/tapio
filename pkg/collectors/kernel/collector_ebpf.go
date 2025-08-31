//go:build linux
// +build linux

package kernel

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -tags linux -target amd64,arm64 kernelmonitor ./bpf_src/kernel_monitor.c -- -I../bpf_common

// ebpfComponents holds Linux-specific eBPF components
type ebpfComponents struct {
	objs   *kernelmonitorObjects
	links  []link.Link
	reader *ringbuf.Reader
}

// startEBPF initializes and starts eBPF monitoring on Linux
func (c *Collector) startEBPF() error {
	c.logger.Debug("Starting eBPF kernel monitoring")

	// Remove memory lock limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		c.logger.Warn("Failed to remove memlock limit", zap.Error(err))
	}

	// Load pre-compiled eBPF objects
	objs := &kernelmonitorObjects{}
	if err := loadKernelmonitorObjects(objs, nil); err != nil {
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
func (c *Collector) stopEBPF() {
	if c.ebpfState == nil {
		return
	}

	state, ok := c.ebpfState.(*ebpfComponents)
	if !ok {
		c.logger.Error("Invalid eBPF state type")
		return
	}

	c.logger.Debug("Stopping eBPF kernel monitoring")

	// Close reader
	if state.reader != nil {
		if err := state.reader.Close(); err != nil {
			c.logger.Warn("Failed to close ring buffer reader", zap.Error(err))
		}
	}

	// Detach links
	for _, l := range state.links {
		if err := l.Close(); err != nil {
			c.logger.Warn("Failed to close eBPF link", zap.Error(err))
		}
	}

	// Close eBPF objects
	if err := state.objs.Close(); err != nil {
		c.logger.Warn("Failed to close eBPF objects", zap.Error(err))
	}

	c.ebpfState = nil
	c.logger.Info("eBPF kernel monitoring stopped")
}

// processEvents reads and processes events from eBPF on Linux
func (c *Collector) processEvents() {
	state, ok := c.ebpfState.(*ebpfComponents)
	if !ok {
		c.logger.Error("Invalid eBPF state type for event processing")
		return
	}

	c.logger.Debug("Starting eBPF event processing loop")

	for {
		select {
		case <-c.ctx.Done():
			c.logger.Debug("Event processing stopped")
			return
		default:
			// Read event from ring buffer
			record, err := state.reader.Read()
			if err != nil {
				if err == ringbuf.ErrClosed {
					return
				}
				c.logger.Warn("Failed to read from ring buffer", zap.Error(err))
				if c.errorsTotal != nil {
					c.errorsTotal.Add(c.ctx, 1, 
						metric.WithAttributes(attribute.String("error", "ringbuf_read")))
				}
				continue
			}

			// Process the event
			if err := c.handleKernelEvent(record.RawSample); err != nil {
				c.logger.Warn("Failed to handle kernel event", zap.Error(err))
				if c.errorsTotal != nil {
					c.errorsTotal.Add(c.ctx, 1,
						metric.WithAttributes(attribute.String("error", "event_handling")))
				}
			}
		}
	}
}

// handleKernelEvent processes a single kernel event from eBPF
func (c *Collector) handleKernelEvent(data []byte) error {
	if len(data) < int(unsafe.Sizeof(KernelEvent{})) {
		return fmt.Errorf("event data too small: got %d bytes", len(data))
	}

	// Parse the kernel event
	var event KernelEvent
	reader := bytes.NewReader(data)
	if err := binary.Read(reader, binary.LittleEndian, &event); err != nil {
		return fmt.Errorf("failed to parse kernel event: %w", err)
	}

	// Convert to domain event
	collectorEvent, err := c.convertKernelEvent(&event)
	if err != nil {
		return fmt.Errorf("failed to convert kernel event: %w", err)
	}

	// Send event
	select {
	case c.events <- collectorEvent:
		if c.eventsProcessed != nil {
			c.eventsProcessed.Add(c.ctx, 1)
		}
		c.logger.Debug("Processed kernel event",
			zap.Uint32("event_type", event.EventType),
			zap.Uint32("pid", event.PID))
	case <-c.ctx.Done():
		return nil
	default:
		// Channel is full, drop event
		if c.droppedEvents != nil {
			c.droppedEvents.Add(c.ctx, 1,
				metric.WithAttributes(attribute.String("reason", "channel_full")))
		}
		c.logger.Warn("Dropped kernel event - channel full")
	}

	return nil
}

// convertKernelEvent converts a KernelEvent to domain.CollectorEvent
func (c *Collector) convertKernelEvent(event *KernelEvent) (*domain.CollectorEvent, error) {
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
		Source:    c.name,
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
				ErrorMessage: c.getErrorDescription(configInfo.ErrorCode),
			},
		},

		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"collector":   c.name,
				"event_type":  fmt.Sprintf("%d", event.EventType),
				"pid":         fmt.Sprintf("%d", event.PID),
				"tid":         fmt.Sprintf("%d", event.TID),
				"command":     comm,
				"cgroup_id":   fmt.Sprintf("%d", event.CgroupID),
				"pod_uid":     podUID,
				"mount_path":  mountPath,
				"error_code":  fmt.Sprintf("%d", configInfo.ErrorCode),
			},
		},
	}, nil
}

// mapEventSeverity determines event severity based on type and error code
func (c *Collector) mapEventSeverity(eventType uint32, errorCode int32) domain.EventSeverity {
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

// getErrorDescription returns a human-readable error description
func (c *Collector) getErrorDescription(errorCode int32) string {
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