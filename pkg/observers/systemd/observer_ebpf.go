//go:build linux
// +build linux

package systemd

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"unsafe"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/perf"
	"github.com/cilium/ebpf/rlimit"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// ebpfState contains the eBPF programs and maps
type ebpfState struct {
	objs   *systemdmonitorObjects
	links  []link.Link
	reader *perf.Reader
}

// startEBPFMonitoring starts eBPF-based monitoring on Linux
func (o *Observer) startEBPFMonitoring(ctx context.Context) error {
	o.logger.Info("Starting eBPF monitoring for systemd")

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("failed to remove memlock: %w", err)
	}

	// Load pre-compiled eBPF objects
	objs := &systemdmonitorObjects{}
	if err := loadSystemdmonitorObjects(objs, nil); err != nil {
		return fmt.Errorf("failed to load eBPF objects: %w", err)
	}

	// Open perf event reader
	reader, err := perf.NewReader(objs.Events, 64*1024) // 64KB buffer
	if err != nil {
		objs.Close()
		return fmt.Errorf("failed to create perf reader: %w", err)
	}

	// Attach programs
	var links []link.Link

	// Attach to process exec events
	execLink, err := link.Tracepoint("sched", "sched_process_exec", objs.TracepointSchedProcessExec)
	if err != nil {
		reader.Close()
		objs.Close()
		return fmt.Errorf("failed to attach to sched_process_exec: %w", err)
	}
	links = append(links, execLink)

	// Attach to process exit events
	exitLink, err := link.Tracepoint("sched", "sched_process_exit", objs.TracepointSchedProcessExit)
	if err != nil {
		for _, l := range links {
			l.Close()
		}
		reader.Close()
		objs.Close()
		return fmt.Errorf("failed to attach to sched_process_exit: %w", err)
	}
	links = append(links, exitLink)

	// Store eBPF state
	o.ebpfState = &ebpfState{
		objs:   objs,
		links:  links,
		reader: reader,
	}

	// Start event processor
	o.LifecycleManager.Start("ebpf-processor", func() {
		o.processEBPFEvents(o.LifecycleManager.Context())
	})

	o.logger.Info("eBPF monitoring started successfully")
	return nil
}

// stopEBPFMonitoring stops eBPF monitoring
func (o *Observer) stopEBPFMonitoring() {
	if o.ebpfState == nil {
		return
	}

	state, ok := o.ebpfState.(*ebpfState)
	if !ok {
		return
	}

	o.logger.Info("Stopping eBPF monitoring")

	// Close reader
	if state.reader != nil {
		state.reader.Close()
	}

	// Detach all links
	for _, link := range state.links {
		link.Close()
	}

	// Close eBPF objects
	if state.objs != nil {
		state.objs.Close()
	}

	o.ebpfState = nil
	o.logger.Info("eBPF monitoring stopped")
}

// processEBPFEvents processes events from the eBPF program
func (o *Observer) processEBPFEvents(ctx context.Context) {
	if o.ebpfState == nil {
		o.logger.Error("eBPF state not initialized")
		return
	}

	state, ok := o.ebpfState.(*ebpfState)
	if !ok || state.reader == nil {
		o.logger.Error("Invalid eBPF state")
		return
	}

	o.logger.Info("Starting eBPF event processor")

	for {
		select {
		case <-ctx.Done():
			o.logger.Info("eBPF event processor stopped")
			return
		default:
		}

		record, err := state.reader.Read()
		if err != nil {
			if errors.Is(err, perf.ErrClosed) {
				return
			}
			o.logger.Error("Failed to read eBPF event", zap.Error(err))
			if o.errorsTotal != nil {
				o.errorsTotal.Add(ctx, 1, metric.WithAttributes(
					attribute.String("error_type", "ebpf_read_failed"),
				))
			}
			continue
		}

		// Parse the event
		if len(record.RawSample) < int(unsafe.Sizeof(SystemdEvent{})) {
			o.logger.Warn("Invalid event size", zap.Int("size", len(record.RawSample)))
			continue
		}

		event := (*SystemdEvent)(unsafe.Pointer(&record.RawSample[0]))
		o.processSystemdEvent(ctx, event)
	}
}

// startJournalMonitoring starts journal-based monitoring on Linux
func (o *Observer) startJournalMonitoring(ctx context.Context) error {
	// Journal monitoring would require systemd library integration
	// For now, this is a placeholder
	o.logger.Info("Journal monitoring not yet implemented")
	return nil
}

// stopJournalMonitoring stops journal monitoring
func (o *Observer) stopJournalMonitoring() {
	// Placeholder for journal monitoring cleanup
}

// Helper function to check if running as systemd unit
func isSystemdService(pid uint32) bool {
	// Check if process is managed by systemd
	// This is simplified - real implementation would check cgroup hierarchy
	return pid == 1 // Init process
}

// Helper to extract service name from cgroup path
func extractServiceName(cgroupPath string) string {
	// Extract service name from cgroup path
	// Example: /system.slice/docker.service -> docker.service
	if len(cgroupPath) == 0 {
		return ""
	}

	// Find last slash
	lastSlash := -1
	for i := len(cgroupPath) - 1; i >= 0; i-- {
		if cgroupPath[i] == '/' {
			lastSlash = i
			break
		}
	}

	if lastSlash >= 0 && lastSlash < len(cgroupPath)-1 {
		return cgroupPath[lastSlash+1:]
	}

	return cgroupPath
}

// Helper to convert IP to string (for network-related systemd services)
func ipToString(ip uint32) string {
	bytes := make([]byte, 4)
	binary.BigEndian.PutUint32(bytes, ip)
	return fmt.Sprintf("%d.%d.%d.%d", bytes[0], bytes[1], bytes[2], bytes[3])
}
