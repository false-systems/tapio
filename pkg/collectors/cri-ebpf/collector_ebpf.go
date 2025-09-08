//go:build linux
// +build linux

package criebpf

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
func (c *Collector) startEBPF() error {
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
func (c *Collector) stopEBPF() {
	c.cleanup()
}

// loadEBPFPrograms loads the compiled eBPF programs
func (c *Collector) loadEBPFPrograms() error {
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
func (c *Collector) attachPrograms() error {
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
func (c *Collector) cleanup() {
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
func (c *Collector) processEvents() {
	state := c.ebpfState.(*ebpfStateImpl)

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			record, err := state.reader.Read()
			if err != nil {
				if c.ctx.Err() != nil {
					return
				}
				c.RecordErrorWithContext(c.ctx, err)
				c.logger.Error("Failed to read from ring buffer", zap.Error(err))
				continue
			}

			c.handleRingBufferEvent(record.RawSample)
		}
	}
}

// handleRingBufferEvent processes a single ring buffer event
func (c *Collector) handleRingBufferEvent(data []byte) {
	start := time.Now()
	ctx := c.ctx

	// Validate event size
	if len(data) < int(unsafe.Sizeof(BPFContainerExitEvent{})) {
		c.RecordErrorWithContext(ctx, fmt.Errorf("invalid event size: got %d, expected %d",
			len(data), unsafe.Sizeof(BPFContainerExitEvent{})))
		return
	}

	// Parse eBPF event
	var bpfEvent BPFContainerExitEvent
	if err := binary.Read(bytes.NewReader(data), binary.LittleEndian, &bpfEvent); err != nil {
		c.RecordErrorWithContext(ctx, err)
		c.logger.Error("Failed to parse BPF event", zap.Error(err))
		return
	}

	// Convert to CollectorEvent
	event, err := c.convertToCollectorEvent(&bpfEvent)
	if err != nil {
		c.RecordErrorWithContext(ctx, err)
		c.logger.Error("Failed to convert BPF event", zap.Error(err))
		return
	}

	// Update metrics based on event type
	c.updateEventMetrics(event, &bpfEvent)

	// Send event to channel
	select {
	case c.events <- event:
		c.RecordEventWithContext(ctx)
	default:
		// Channel full, drop event
		c.RecordDropWithReason(ctx, "channel_full")
		c.logger.Warn("Dropped event due to full channel",
			zap.String("event_type", string(event.Type)),
			zap.String("container_id", event.CorrelationHints.ContainerID),
		)
	}

	// Record processing time using BaseCollector
	duration := time.Since(start)
	c.RecordProcessingDuration(ctx, duration)
}

// convertToCollectorEvent converts BPF event to domain event
func (c *Collector) convertToCollectorEvent(bpfEvent *BPFContainerExitEvent) (*domain.CollectorEvent, error) {
	// Simple conversion for now - this would be more comprehensive in production
	// Convert ContainerID from []int8 to string
	containerID := make([]byte, len(bpfEvent.ContainerID))
	for i, b := range bpfEvent.ContainerID {
		containerID[i] = byte(b)
	}

	return &domain.CollectorEvent{
		EventID:   fmt.Sprintf("cri-%d-%d", bpfEvent.PID, time.Now().UnixNano()),
		Timestamp: time.Now(),
		Type:      domain.EventTypeKernelProcess,
		Source:    c.GetName(),
		Severity:  domain.EventSeverityInfo,
		EventData: domain.EventDataContainer{
			Process: &domain.ProcessData{
				PID: int32(bpfEvent.PID),
			},
		},
		Metadata: domain.EventMetadata{
			Priority: domain.PriorityNormal,
			PID:      int32(bpfEvent.PID),
		},
		CorrelationHints: &domain.CorrelationHints{
			ContainerID: string(containerID),
		},
	}, nil
}

// updateEventMetrics updates collector metrics based on event type
func (c *Collector) updateEventMetrics(event *domain.CollectorEvent, bpfEvent *BPFContainerExitEvent) {
	// Update appropriate metrics based on event type
	switch event.Type {
	case domain.EventTypeContainerOOM:
		if c.oomKillsTotal != nil {
			c.oomKillsTotal.Add(c.ctx, 1)
		}
	case domain.EventTypeMemoryPressure:
		if c.memoryPressure != nil {
			c.memoryPressure.Add(c.ctx, 1)
		}
	case domain.EventTypeKernelProcess, domain.EventTypeContainerExit:
		if c.processExits != nil {
			c.processExits.Add(c.ctx, 1)
		}
	}
}

// collectMetrics periodically collects runtime metrics
func (c *Collector) collectMetrics() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			// Collect any periodic metrics here
			c.logger.Debug("Collecting CRI eBPF metrics")
		}
	}
}
