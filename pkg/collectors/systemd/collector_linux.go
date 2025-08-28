//go:build linux
// +build linux

package systemd

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"time"
	"unsafe"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/collectors/systemd/bpf"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 systemdMonitor ./bpf_src/systemd_monitor.c -- -I../bpf_common

// eBPFState holds the Linux-specific eBPF state with proper types
type eBPFState struct {
	objs   *bpf.SystemdMonitorObjects // Generated BPF objects
	links  []link.Link                // Attached BPF program links
	reader *ringbuf.Reader            // Ring buffer for events
}

// startEBPF initializes eBPF monitoring - Linux only
func (c *Collector) startEBPF() error {
	ctx, span := c.tracer.Start(context.Background(), "systemd.ebpf.start")
	defer span.End()

	// Check if eBPF is supported
	if !bpf.IsSupported() {
		c.logger.Warn("eBPF not supported on this platform")
		return nil
	}

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1)
		}
		return fmt.Errorf("removing memory limit: %w", err)
	}

	// Load pre-compiled eBPF programs
	objs := &bpf.SystemdMonitorObjects{}
	if err := bpf.LoadSystemdMonitorObjects(objs, nil); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1)
		}
		return fmt.Errorf("loading eBPF objects: %w", err)
	}

	c.ebpfState = &eBPFState{objs: objs}

	var attachedLinks []link.Link

	// Attach execve tracepoint
	if objs.TraceExec != nil {
		execLink, err := link.AttachTracepoint(link.TracepointOptions{
			Group:   "syscalls",
			Name:    "sys_enter_execve",
			Program: objs.TraceExec,
		})
		if err != nil {
			c.logger.Warn("Failed to attach execve tracepoint", zap.Error(err))
		} else {
			attachedLinks = append(attachedLinks, execLink)
		}
	}

	// Attach exit tracepoint
	if objs.TraceExit != nil {
		exitLink, err := link.AttachTracepoint(link.TracepointOptions{
			Group:   "syscalls",
			Name:    "sys_enter_exit",
			Program: objs.TraceExit,
		})
		if err != nil {
			c.logger.Warn("Failed to attach exit tracepoint", zap.Error(err))
		} else {
			attachedLinks = append(attachedLinks, exitLink)
		}
	}

	// Attach signal tracepoint for tracking SIGKILL, SIGTERM, etc.
	if objs.TraceSignal != nil {
		signalLink, err := link.AttachTracepoint(link.TracepointOptions{
			Group:   "signal",
			Name:    "signal_deliver",
			Program: objs.TraceSignal,
		})
		if err != nil {
			c.logger.Warn("Failed to attach signal tracepoint", zap.Error(err))
		} else {
			attachedLinks = append(attachedLinks, signalLink)
		}
	}

	// Create ring buffer reader
	if objs.Events != nil {
		reader, err := ringbuf.NewReader(objs.Events)
		if err != nil {
			for _, l := range attachedLinks {
				l.Close()
			}
			objs.Close()
			return fmt.Errorf("creating ring buffer reader: %w", err)
		}
		c.ebpfState.reader = reader
	} else {
		return fmt.Errorf("events ring buffer not found in BPF objects")
	}

	c.ebpfState.links = attachedLinks

	c.logger.Info("Systemd eBPF monitoring started successfully",
		zap.String("collector", c.name),
		zap.Int("attached_programs", len(c.ebpfState.links)),
		zap.Bool("signal_tracking", objs.TraceSignal != nil),
	)

	return nil
}

// stopEBPF cleans up eBPF resources - Linux only
func (c *Collector) stopEBPF() {
	if c.ebpfState == nil {
		return
	}

	// Close reader
	if c.ebpfState.reader != nil {
		c.ebpfState.reader.Close()
	}

	// Close all links
	for _, l := range c.ebpfState.links {
		if err := l.Close(); err != nil {
			c.logger.Error("Failed to close eBPF link", zap.Error(err))
		}
	}

	// Close eBPF objects
	if c.ebpfState.objs != nil {
		c.ebpfState.objs.Close()
	}

	c.logger.Info("Systemd eBPF monitoring stopped", zap.String("collector", c.name))
}

// createSystemdCollectorEvent creates a properly structured CollectorEvent from systemd eBPF data
func (c *Collector) createSystemdCollectorEvent(ctx context.Context, event *SystemdEvent, rawData []byte) *domain.CollectorEvent {
	eventID := fmt.Sprintf("systemd-%d-%d", event.Timestamp, event.PID)
	timestamp := time.Unix(0, int64(event.Timestamp))

	// Extract command name from fixed-size array
	comm := string(bytes.TrimRight(event.Comm[:], "\x00"))
	serviceName := string(bytes.TrimRight(event.ServiceName[:], "\x00"))
	cgroupPath := string(bytes.TrimRight(event.CgroupPath[:], "\x00"))

	// Determine event type based on systemd event data
	var eventType domain.CollectorEventType
	var systemdMessage string

	switch event.EventType {
	case 1: // Service start
		eventType = domain.EventTypeSystemdService
		systemdMessage = fmt.Sprintf("Service %s started", serviceName)
	case 2: // Service stop
		eventType = domain.EventTypeSystemdService
		systemdMessage = fmt.Sprintf("Service %s stopped", serviceName)
	case 3: // Service restart
		eventType = domain.EventTypeSystemdService
		systemdMessage = fmt.Sprintf("Service %s restarted", serviceName)
	case 4: // Service failed
		eventType = domain.EventTypeSystemdService
		systemdMessage = fmt.Sprintf("Service %s failed (signal %d)", serviceName, event.Signal)
	case 5: // Process exec
		eventType = domain.EventTypeKernelProcess
		systemdMessage = fmt.Sprintf("Process %s executed", comm)
	case 6: // Process exit
		eventType = domain.EventTypeKernelProcess
		systemdMessage = fmt.Sprintf("Process %s exited with code %d", comm, event.ExitCode)
	case 7: // Signal event
		eventType = domain.EventTypeKernelProcess
		systemdMessage = fmt.Sprintf("Process %s received signal %d", comm, event.Signal)
	default:
		eventType = domain.EventTypeSystemdSystem
		systemdMessage = fmt.Sprintf("Systemd event type %d", event.EventType)
	}

	// Build SystemdData structure
	systemdData := &domain.SystemdData{
		Unit:     serviceName,
		Message:  systemdMessage,
		Priority: "info",
		MainPID:  int32(event.PID),
	}

	// Build Process data for correlation
	processData := &domain.ProcessData{
		PID:        int32(event.PID),
		PPID:       int32(event.PPID),
		Command:    comm,
		UID:        int32(event.UID),
		GID:        int32(event.GID),
		CgroupPath: cgroupPath,
	}

	// Create CollectorEvent
	collectorEvent := &domain.CollectorEvent{
		EventID:   eventID,
		Timestamp: timestamp,
		Type:      eventType,
		Source:    c.name,
		Severity:  domain.EventSeverityInfo,

		EventData: domain.EventDataContainer{
			Systemd: systemdData,
			Process: processData,
			RawData: &domain.RawData{
				Format:      "ebpf_binary",
				ContentType: "application/octet-stream",
				Data:        rawData,
				Size:        int64(len(rawData)),
			},
		},

		Metadata: domain.EventMetadata{
			PID:      int32(event.PID),
			PPID:     int32(event.PPID),
			UID:      int32(event.UID),
			GID:      int32(event.GID),
			CgroupID: event.CgroupID,
			Command:  comm,
			Priority: domain.PriorityNormal,
			Tags:     []string{"systemd", "ebpf"},
			Labels: map[string]string{
				"service_name": serviceName,
				"cgroup_path":  cgroupPath,
			},
		},

		CorrelationHints: &domain.CorrelationHints{
			ProcessID:  int32(event.PID),
			CgroupPath: cgroupPath,
		},
	}

	return collectorEvent
}

// processEvents processes events from the ring buffer - simple version
func (c *Collector) processEvents() {
	c.wg.Add(1)
	defer c.wg.Done()

	ctx, span := c.tracer.Start(context.Background(), "systemd.collector.process_events")
	defer span.End()

	if c.ebpfState == nil || c.ebpfState.reader == nil {
		return
	}

	reader := c.ebpfState.reader

	c.logger.Info("Starting systemd event processing loop")

	for {
		select {
		case <-c.ctx.Done():
			c.logger.Info("Event processing stopped due to context cancellation")
			return
		default:
		}

		record, err := reader.Read()
		if err != nil {
			if c.ctx.Err() != nil {
				return
			}
			if c.errorsTotal != nil {
				c.errorsTotal.Add(ctx, 1)
			}
			c.logger.Debug("Failed to read from ring buffer", zap.Error(err))
			continue
		}

		// Parse event safely with timing
		startTime := time.Now()
		if len(record.RawSample) < int(unsafe.Sizeof(SystemdEvent{})) {
			if c.errorsTotal != nil {
				c.errorsTotal.Add(ctx, 1)
			}
			continue
		}

		var event SystemdEvent
		if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &event); err != nil {
			if c.errorsTotal != nil {
				c.errorsTotal.Add(ctx, 1)
			}
			c.logger.Debug("Failed to parse systemd event", zap.Error(err))
			continue
		}

		// Record processing time (with nil check)
		if c.processingTime != nil {
			duration := time.Since(startTime).Seconds() * 1000 // Convert to milliseconds
			c.processingTime.Record(ctx, duration)
		}

		// Convert to CollectorEvent with proper systemd data structure
		collectorEvent := c.createSystemdCollectorEvent(ctx, &event, record.RawSample)

		// Update buffer usage gauge based on channel capacity
		if c.bufferUsage != nil {
			bufferUsed := int64(len(c.events))
			bufferCapacity := int64(cap(c.events))
			usagePercent := int64(0)
			if bufferCapacity > 0 {
				usagePercent = (bufferUsed * 100) / bufferCapacity
			}
			c.bufferUsage.Record(ctx, usagePercent)
		}

		select {
		case c.events <- collectorEvent:
			// Event sent successfully
			if c.eventsProcessed != nil {
				c.eventsProcessed.Add(ctx, 1)
			}
		case <-c.ctx.Done():
			return
		default:
			// Buffer full - drop event
			if c.droppedEvents != nil {
				c.droppedEvents.Add(ctx, 1)
			}
			if c.errorsTotal != nil {
				c.errorsTotal.Add(ctx, 1)
			}
		}
	}
}
