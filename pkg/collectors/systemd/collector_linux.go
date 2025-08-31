//go:build linux
// +build linux

package systemd

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/perf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// eBPF generation is handled by bpf/generate.go

// ebpfState holds Linux-specific eBPF components
type ebpfSystemdState struct {
	objs   systemdmonitorObjects
	links  []link.Link
	reader *perf.Reader
}

// startMonitoring initializes and attaches eBPF programs on Linux
func (c *Collector) startMonitoring() error {
	// Remove memory lock limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		c.logger.Warn("Failed to remove memlock limit", zap.Error(err))
	}

	// Load pre-compiled eBPF objects
	objs := systemdmonitorObjects{}
	if err := loadSystemdmonitorObjects(&objs, nil); err != nil {
		var ve *ebpf.VerifierError
		if errors.As(err, &ve) {
			c.logger.Error("eBPF verifier error", zap.String("details", ve.Error()))
		}
		return fmt.Errorf("failed to load eBPF objects: %w", err)
	}

	// Create perf event reader
	reader, err := perf.NewReader(objs.Events, 64*1024)
	if err != nil {
		objs.Close()
		return fmt.Errorf("failed to create perf reader: %w", err)
	}

	// Attach to relevant systemd tracepoints
	var links []link.Link

	// Monitor service state changes
	if c.config.MonitorServiceStates {
		l, err := link.Tracepoint("sched", "sched_process_exec", objs.TraceSyscallExec)
		if err != nil {
			c.logger.Warn("Failed to attach to sched_process_exec", zap.Error(err))
		} else {
			links = append(links, l)
		}

		l, err = link.Tracepoint("sched", "sched_process_exit", objs.TraceSyscallExit)
		if err != nil {
			c.logger.Warn("Failed to attach to sched_process_exit", zap.Error(err))
		} else {
			links = append(links, l)
		}
	}

	// Monitor cgroup events if enabled
	if c.config.MonitorCgroups {
		l, err := link.Tracepoint("cgroup", "cgroup_attach_task", objs.TraceCgroupAttach)
		if err != nil {
			c.logger.Warn("Failed to attach to cgroup_attach_task", zap.Error(err))
		} else {
			links = append(links, l)
		}
	}

	if len(links) == 0 {
		objs.Close()
		reader.Close()
		return fmt.Errorf("failed to attach any eBPF programs")
	}

	// Store eBPF state
	c.ebpfState = &ebpfSystemdState{
		objs:   objs,
		links:  links,
		reader: reader,
	}

	// Start event processing goroutine
	go c.processEvents()

	c.logger.Info("eBPF systemd monitoring started",
		zap.Int("attached_programs", len(links)),
		zap.Bool("monitor_services", c.config.MonitorServiceStates),
		zap.Bool("monitor_cgroups", c.config.MonitorCgroups),
	)

	return nil
}

// stopMonitoring cleans up eBPF resources on Linux
func (c *Collector) stopMonitoring() {
	if c.ebpfState == nil {
		return
	}

	state, ok := c.ebpfState.(*ebpfSystemdState)
	if !ok {
		c.logger.Warn("Invalid eBPF state type")
		return
	}

	// Close all links
	for _, l := range state.links {
		if err := l.Close(); err != nil {
			c.logger.Warn("Failed to close eBPF link", zap.Error(err))
		}
	}

	// Close reader
	if state.reader != nil {
		if err := state.reader.Close(); err != nil {
			c.logger.Warn("Failed to close perf reader", zap.Error(err))
		}
	}

	// Close eBPF objects
	if err := state.objs.Close(); err != nil {
		c.logger.Warn("Failed to close eBPF objects", zap.Error(err))
	}

	c.ebpfState = nil
	c.logger.Info("eBPF systemd monitoring stopped")
}

// processEvents reads and processes events from eBPF on Linux
func (c *Collector) processEvents() {
	state, ok := c.ebpfState.(*ebpfSystemdState)
	if !ok {
		c.logger.Error("Invalid eBPF state for event processing")
		return
	}

	c.logger.Info("Starting systemd event processing")
	
	for {
		select {
		case <-c.ctx.Done():
			c.logger.Info("Stopping event processing due to context cancellation")
			return
		default:
			record, err := state.reader.Read()
			if err != nil {
				if errors.Is(err, perf.ErrClosed) {
					c.logger.Info("Perf reader closed, stopping event processing")
					return
				}
				c.recordMetric(c.errorsTotal, 1, attribute.String("error", "read_failed"))
				c.logger.Warn("Failed to read event", zap.Error(err))
				continue
			}

			if record.LostSamples > 0 {
				c.recordMetric(c.droppedEvents, int64(record.LostSamples))
				c.logger.Warn("Lost eBPF samples", zap.Uint64("lost", record.LostSamples))
			}

			// Parse the raw event
			var event SystemdEvent
			if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &event); err != nil {
				c.recordMetric(c.errorsTotal, 1, attribute.String("error", "parse_failed"))
				c.logger.Warn("Failed to parse event", zap.Error(err))
				continue
			}

			// Process the event
			c.handleSystemdEvent(&event)
		}
	}
}

// handleSystemdEvent processes a single systemd event
func (c *Collector) handleSystemdEvent(event *SystemdEvent) {
	startTime := time.Now()
	defer func() {
		c.recordMetric(c.processingTime, float64(time.Since(startTime).Milliseconds()))
	}()

	// Convert to domain event
	domainEvent := &domain.CollectorEvent{
		Type:      "systemd",
		Timestamp: time.Unix(0, int64(event.Timestamp)),
		Source:    c.name,
		Data: map[string]interface{}{
			"pid":          event.PID,
			"ppid":         event.PPID,
			"uid":          event.UID,
			"gid":          event.GID,
			"cgroup_id":    event.CgroupID,
			"event_type":   c.getEventTypeName(event.EventType),
			"comm":         string(bytes.TrimRight(event.Comm[:], "\x00")),
			"service_name": string(bytes.TrimRight(event.ServiceName[:], "\x00")),
			"cgroup_path":  string(bytes.TrimRight(event.CgroupPath[:], "\x00")),
			"exit_code":    event.ExitCode,
			"signal":       event.Signal,
		},
	}

	// Send event to channel
	select {
	case c.events <- domainEvent:
		c.recordMetric(c.eventsProcessed, 1, attribute.String("type", "systemd"))
	default:
		c.recordMetric(c.droppedEvents, 1, attribute.String("reason", "channel_full"))
		c.logger.Warn("Event channel full, dropping event")
	}

	// Update buffer usage
	if c.bufferUsage != nil {
		c.bufferUsage.Record(c.ctx, int64(len(c.events)))
	}
}

// getEventTypeName converts event type to string
func (c *Collector) getEventTypeName(eventType uint8) string {
	switch eventType {
	case 1:
		return "service_start"
	case 2:
		return "service_stop"
	case 3:
		return "service_restart"
	case 4:
		return "cgroup_attach"
	case 5:
		return "cgroup_detach"
	default:
		return fmt.Sprintf("unknown_%d", eventType)
	}
}

// recordMetric is a helper to record metrics with error handling
func (c *Collector) recordMetric(metric interface{}, value interface{}, attrs ...attribute.KeyValue) {
	if metric == nil {
		return
	}

	switch m := metric.(type) {
	case metric.Int64Counter:
		if v, ok := value.(int64); ok {
			m.Add(c.ctx, v, metric.WithAttributes(attrs...))
		}
	case metric.Float64Histogram:
		if v, ok := value.(float64); ok {
			m.Record(c.ctx, v, metric.WithAttributes(attrs...))
		}
	case metric.Int64Gauge:
		if v, ok := value.(int64); ok {
			m.Record(c.ctx, v, metric.WithAttributes(attrs...))
		}
	}
}