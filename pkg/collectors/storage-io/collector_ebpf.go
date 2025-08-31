//go:build linux
// +build linux

package storageio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/collectors/storage-io/bpf"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// ebpfStateImpl holds eBPF components for Linux
type ebpfStateImpl struct {
	objs   *bpf.StoragemonitorObjects
	links  []link.Link
	reader *ringbuf.Reader
}

// startEBPF initializes and starts eBPF monitoring (Linux implementation)
func (c *Collector) startEBPF() error {
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

	// Start event processing using lifecycle manager
	c.LifecycleManager.Start("event-processor", c.processEvents)

	// Start metrics collection using lifecycle manager
	c.LifecycleManager.Start("metrics-collector", c.collectMetrics)

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
	objs := &bpf.StoragemonitorObjects{}
	if err := bpf.LoadStoragemonitorObjects(objs, nil); err != nil {
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

	// Attach VFS read kprobes
	if c.config.EnableVFSRead {
		readEnterLink, err := link.Kprobe("vfs_read", state.objs.TraceReadEnter, nil)
		if err != nil {
			c.logger.Warn("Failed to attach VFS read enter kprobe", zap.Error(err))
		} else {
			state.links = append(state.links, readEnterLink)
		}

		readExitLink, err := link.Kretprobe("vfs_read", state.objs.TraceReadExit, nil)
		if err != nil {
			c.logger.Warn("Failed to attach VFS read exit kprobe", zap.Error(err))
		} else {
			state.links = append(state.links, readExitLink)
		}
	}

	// Attach VFS write kprobes
	if c.config.EnableVFSWrite {
		writeEnterLink, err := link.Kprobe("vfs_write", state.objs.TraceWriteEnter, nil)
		if err != nil {
			c.logger.Warn("Failed to attach VFS write enter kprobe", zap.Error(err))
		} else {
			state.links = append(state.links, writeEnterLink)
		}

		writeExitLink, err := link.Kretprobe("vfs_write", state.objs.TraceWriteExit, nil)
		if err != nil {
			c.logger.Warn("Failed to attach VFS write exit kprobe", zap.Error(err))
		} else {
			state.links = append(state.links, writeExitLink)
		}
	}

	// Block I/O monitoring would go here if implemented
	// Currently only VFS monitoring is implemented in the eBPF programs

	if len(state.links) == 0 {
		return fmt.Errorf("no eBPF programs attached")
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
	ctx := c.Context()

	for {
		select {
		case <-c.StopChannel():
			return
		default:
			record, err := state.reader.Read()
			if err != nil {
				if errors.Is(err, ringbuf.ErrClosed) {
					return
				}
				if c.IsShuttingDown() {
					return
				}
				c.RecordErrorWithContext(ctx, err)
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
	ctx := c.Context()

	// Parse the event based on type
	if len(data) < 4 {
		c.RecordErrorWithContext(ctx, fmt.Errorf("invalid event size: %d", len(data)))
		return
	}

	// Read event type
	eventType := binary.LittleEndian.Uint32(data[0:4])

	var event *domain.CollectorEvent
	var err error

	switch eventType {
	case 1: // VFS event
		event, err = c.parseVFSEvent(data)
	case 2: // Block I/O event
		event, err = c.parseBlockIOEvent(data)
	default:
		c.RecordErrorWithContext(ctx, fmt.Errorf("unknown event type: %d", eventType))
		return
	}

	if err != nil {
		c.RecordErrorWithContext(ctx, err)
		c.logger.Error("Failed to parse event", zap.Error(err))
		return
	}

	// Use EventChannelManager to send event
	if c.SendEvent(event) {
		c.RecordEventWithContext(ctx)
	} else {
		// Channel full, event was dropped
		c.RecordDropWithReason(ctx, "channel_full")
	}

	// Record processing time
	duration := time.Since(start)
	c.RecordProcessingDuration(ctx, duration)
}

// parseVFSEvent parses a VFS event from raw bytes
func (c *Collector) parseVFSEvent(data []byte) (*domain.CollectorEvent, error) {
	// Simple parsing for now - would be more comprehensive in production
	var vfsEvent struct {
		EventType uint32
		PID       uint32
		Latency   uint64
		Size      uint64
		Path      [256]byte
	}

	buf := bytes.NewReader(data)
	if err := binary.Read(buf, binary.LittleEndian, &vfsEvent); err != nil {
		return nil, err
	}

	// Check for slow I/O
	latencyMs := vfsEvent.Latency / 1000000
	severity := domain.EventSeverityInfo
	if latencyMs > uint64(c.config.SlowIOThresholdMs) {
		severity = domain.EventSeverityWarning
		if c.slowIOOperations != nil {
			c.slowIOOperations.Add(c.Context(), 1)
		}
	}

	return &domain.CollectorEvent{
		EventID:   fmt.Sprintf("storage-%d-%d", vfsEvent.PID, time.Now().UnixNano()),
		Timestamp: time.Now(),
		Type:      domain.EventTypeKernelFS,
		Source:    c.GetName(),
		Severity:  severity,
		EventData: domain.EventDataContainer{
			Process: &domain.ProcessData{
				PID: int32(vfsEvent.PID),
			},
			Custom: map[string]string{
				"latency_ms": fmt.Sprintf("%d", latencyMs),
				"size_bytes": fmt.Sprintf("%d", vfsEvent.Size),
				"path":       string(bytes.Trim(vfsEvent.Path[:], "\x00")),
			},
		},
		Metadata: domain.EventMetadata{
			Priority: domain.PriorityNormal,
			PID:      int32(vfsEvent.PID),
		},
	}, nil
}

// parseBlockIOEvent parses a block I/O event from raw bytes
func (c *Collector) parseBlockIOEvent(data []byte) (*domain.CollectorEvent, error) {
	// Simple parsing for now
	var blockEvent struct {
		EventType uint32
		PID       uint32
		Latency   uint64
		Sector    uint64
		Size      uint32
		DevMajor  uint32
		DevMinor  uint32
	}

	buf := bytes.NewReader(data)
	if err := binary.Read(buf, binary.LittleEndian, &blockEvent); err != nil {
		return nil, err
	}

	return &domain.CollectorEvent{
		EventID:   fmt.Sprintf("block-%d-%d", blockEvent.PID, time.Now().UnixNano()),
		Timestamp: time.Now(),
		Type:      domain.EventTypeKernelFS,
		Source:    c.GetName(),
		Severity:  domain.EventSeverityInfo,
		EventData: domain.EventDataContainer{
			Process: &domain.ProcessData{
				PID: int32(blockEvent.PID),
			},
			Custom: map[string]string{
				"latency_us": fmt.Sprintf("%d", blockEvent.Latency),
				"sector":     fmt.Sprintf("%d", blockEvent.Sector),
				"size_bytes": fmt.Sprintf("%d", blockEvent.Size),
				"device":     fmt.Sprintf("%d:%d", blockEvent.DevMajor, blockEvent.DevMinor),
			},
		},
		Metadata: domain.EventMetadata{
			Priority: domain.PriorityNormal,
			PID:      int32(blockEvent.PID),
		},
	}, nil
}

// collectMetrics periodically collects runtime metrics
func (c *Collector) collectMetrics() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.StopChannel():
			return
		case <-ticker.C:
			// Collect any periodic metrics here
			c.logger.Debug("Collecting storage I/O metrics",
				zap.Int64("events_sent", c.EventChannelManager.GetSentCount()),
				zap.Int64("events_dropped", c.EventChannelManager.GetDroppedCount()),
				zap.Float64("channel_utilization", c.EventChannelManager.GetChannelUtilization()),
			)
		}
	}
}