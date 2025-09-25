//go:build linux
// +build linux

package containerruntime

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"time"
	"unsafe"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// eBPF generation is handled by bpf/generate.go

// ebpfState holds eBPF components for Linux
type ebpfStateImpl struct {
	objs   *crimonitorObjects
	links  []link.Link
	reader *ringbuf.Reader
}

// startEBPF initializes and starts eBPF monitoring (Linux implementation)
func (c *Observer) startEBPF() error {
	// Validate struct sizes match eBPF expectations
	if err := ValidateBPFContainerExitEvent(); err != nil {
		return fmt.Errorf("BPF struct validation failed: %w", err)
	}

	if err := ValidateBPFContainerMetadata(); err != nil {
		return fmt.Errorf("BPF metadata validation failed: %w", err)
	}

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		c.logger.Warn("Failed to remove memlock", zap.Error(err))
	}

	// Load eBPF programs
	if err := c.loadEBPFPrograms(); err != nil {
		return fmt.Errorf("failed to load eBPF programs: %w", err)
	}

	// Attach programs to kernel
	if err := c.attachPrograms(); err != nil {
		c.cleanup()
		return fmt.Errorf("failed to attach eBPF programs: %w", err)
	}

	// Get state
	state := c.ebpfState.(*ebpfStateImpl)

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(state.objs.Events)
	if err != nil {
		c.cleanup()
		return fmt.Errorf("creating ring buffer reader: %w", err)
	}
	state.reader = reader

	c.logger.Info("eBPF programs attached",
		zap.Int("attached_programs", len(state.links)))

	return nil
}

// stopEBPF stops eBPF monitoring and cleans up resources
func (c *Observer) stopEBPF() {
	c.cleanup()
}

// loadEBPFPrograms loads the compiled eBPF programs
func (c *Observer) loadEBPFPrograms() error {
	objs := &crimonitorObjects{}
	if err := loadCrimonitorObjects(objs, nil); err != nil {
		return fmt.Errorf("loading BPF objects: %w", err)
	}

	c.ebpfState = &ebpfStateImpl{
		objs:  objs,
		links: make([]link.Link, 0),
	}

	return nil
}

// attachPrograms attaches eBPF programs to kernel events
func (c *Observer) attachPrograms() error {
	state := c.ebpfState.(*ebpfStateImpl)

	// Attach OOM kill tracepoint
	if c.config.EnableOOMKill {
		oomLink, err := link.Tracepoint("oom", "mark_victim", state.objs.TraceOomKill, nil)
		if err != nil {
			return fmt.Errorf("attaching OOM kill tracepoint: %w", err)
		}
		state.links = append(state.links, oomLink)
	}

	// Attach memory pressure tracepoint
	if c.config.EnableMemoryPressure {
		memLink, err := link.Tracepoint("vmscan", "mm_vmscan_memcg_reclaim_begin", state.objs.TraceMemcgOom, nil)
		if err != nil {
			c.logger.Warn("Failed to attach memory pressure tracepoint", zap.Error(err))
		} else {
			state.links = append(state.links, memLink)
		}
	}

	// Attach process exit tracepoint
	if c.config.EnableProcessExit {
		exitLink, err := link.Tracepoint("sched", "sched_process_exit", state.objs.TraceProcessExit, nil)
		if err != nil {
			return fmt.Errorf("attaching process exit tracepoint: %w", err)
		}
		state.links = append(state.links, exitLink)
	}

	// Attach process fork tracepoint
	if c.config.EnableProcessFork {
		forkLink, err := link.Tracepoint("sched", "sched_process_fork", state.objs.TraceProcessFork, nil)
		if err != nil {
			c.logger.Warn("Failed to attach process fork tracepoint", zap.Error(err))
		} else {
			state.links = append(state.links, forkLink)
		}
	}

	return nil
}

// cleanup releases all eBPF resources
func (c *Observer) cleanup() {
	if c.ebpfState == nil {
		return
	}

	state := c.ebpfState.(*ebpfStateImpl)

	// Close reader
	if state.reader != nil {
		state.reader.Close()
	}

	// Detach links
	for _, l := range state.links {
		if l != nil {
			l.Close()
		}
	}

	// Close objects
	if state.objs != nil {
		state.objs.Close()
	}

	c.ebpfState = nil
}

// processEvents reads and processes events from the ring buffer
func (c *Observer) processEvents() {
	state := c.ebpfState.(*ebpfStateImpl)

	for {
		select {
		case <-c.LifecycleManager.Context().Done():
			return
		default:
			record, err := state.reader.Read()
			if err != nil {
				if c.LifecycleManager.Context().Err() != nil {
					return
				}
				c.BaseObserver.RecordError(err)
				c.logger.Error("Failed to read from ring buffer", zap.Error(err))
				continue
			}

			c.handleRingBufferEvent(record.RawSample)
		}
	}
}

// handleRingBufferEvent processes a single ring buffer event
func (c *Observer) handleRingBufferEvent(data []byte) {
	start := time.Now()
	_ = c.LifecycleManager.Context() // Context available if needed

	// Validate event size
	if len(data) < int(unsafe.Sizeof(BPFContainerExitEvent{})) {
		c.BaseObserver.RecordError(fmt.Errorf("invalid event size: got %d, expected %d",
			len(data), unsafe.Sizeof(BPFContainerExitEvent{})))
		return
	}

	// Parse eBPF event
	var bpfEvent BPFContainerExitEvent
	if err := binary.Read(bytes.NewReader(data), binary.LittleEndian, &bpfEvent); err != nil {
		c.BaseObserver.RecordError(err)
		c.logger.Error("Failed to parse BPF event", zap.Error(err))
		return
	}

	// Convert to ObserverEvent
	event, err := c.convertToObserverEvent(&bpfEvent)
	if err != nil {
		c.BaseObserver.RecordError(err)
		c.logger.Error("Failed to convert BPF event", zap.Error(err))
		return
	}

	// Update metrics based on event type
	c.updateEventMetrics(event, &bpfEvent)

	// Send event to channel
	if c.EventChannelManager.SendEvent(event) {
		c.BaseObserver.RecordEvent()
	} else {
		// Channel full, drop event
		c.BaseObserver.RecordDrop()
		c.logger.Warn("Dropped event due to full channel",
			zap.String("event_type", string(event.Type)),
			zap.String("container_id", event.CorrelationHints.ContainerID),
		)
	}

	// Record processing time using BaseObserver
	_ = time.Since(start) // Duration available for future metrics
}

// convertToObserverEvent converts BPF event to domain event
func (c *Observer) convertToObserverEvent(bpfEvent *BPFContainerExitEvent) (*domain.CollectorEvent, error) {
	// Convert C strings to Go strings using helper function
	containerID := CStringToGo(bpfEvent.ContainerID[:])
	command := CStringToGo(bpfEvent.Comm[:])

	// Determine event type based on BPF event data
	eventType := domain.EventTypeKernelProcess
	severity := domain.EventSeverityInfo

	if bpfEvent.OOMKilled == 1 {
		eventType = domain.EventTypeContainerOOM
		severity = domain.EventSeverityCritical
	} else if bpfEvent.ExitCode != 0 {
		eventType = domain.EventTypeContainerExit
		severity = domain.EventSeverityWarning
	}

	// Check for memory pressure (>90% utilization)
	if bpfEvent.MemoryLimit > 0 && bpfEvent.MemoryUsage > 0 {
		utilization := float64(bpfEvent.MemoryUsage) / float64(bpfEvent.MemoryLimit)
		if utilization > 0.9 && eventType != domain.EventTypeContainerOOM {
			eventType = domain.EventTypeMemoryPressure
			severity = domain.EventSeverityWarning
		}
	}

	event := &domain.CollectorEvent{
		EventID:   fmt.Sprintf("container-runtime-%d-%d", bpfEvent.PID, time.Now().UnixNano()),
		Timestamp: time.Now(),
		Type:      eventType,
		Source:    c.GetName(),
		Severity:  severity,
		EventData: domain.EventDataContainer{
			Process: &domain.ProcessData{
				PID:     int32(bpfEvent.PID),
				Command: command,
			},
		},
		Metadata: domain.EventMetadata{
			Priority: domain.PriorityNormal,
			PID:      int32(bpfEvent.PID),
			Labels: map[string]string{
				"observer": "container-runtime",
				"version":  "1.0.0",
			},
		},
		CorrelationHints: &domain.CorrelationHints{
			ContainerID: containerID,
			CgroupPath:  fmt.Sprintf("/sys/fs/cgroup/%d", bpfEvent.CgroupID),
		},
	}

	// Add container-specific data
	containerData := domain.EventDataContainer{
		Container: &domain.ContainerData{
			ContainerID: containerID,
			ExitCode:    &bpfEvent.ExitCode,
			State:       "exited",
			Action:      "exit",
		},
	}
	event.EventData = containerData

	return event, nil
}

// updateEventMetrics updates observer metrics based on event type
func (c *Observer) updateEventMetrics(event *domain.CollectorEvent, bpfEvent *BPFContainerExitEvent) {
	// Update appropriate metrics based on event type
	switch event.Type {
	case domain.EventTypeContainerOOM:
		if c.oomKillsTotal != nil {
			c.oomKillsTotal.Add(c.LifecycleManager.Context(), 1)
		}
	case domain.EventTypeMemoryPressure:
		if c.memoryPressure != nil {
			c.memoryPressure.Add(c.LifecycleManager.Context(), 1)
		}
	case domain.EventTypeKernelProcess, domain.EventTypeContainerExit:
		if c.processExits != nil {
			c.processExits.Add(c.LifecycleManager.Context(), 1)
		}
	}
}

// collectMetrics periodically collects runtime metrics
func (c *Observer) collectMetrics() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.LifecycleManager.Context().Done():
			return
		case <-ticker.C:
			// Collect any periodic metrics here
			c.logger.Debug("Collecting Container Runtime metrics")
		}
	}
}
