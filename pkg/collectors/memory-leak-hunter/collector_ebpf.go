//go:build linux
// +build linux

package memory_leak_hunter

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -tags linux -target amd64,arm64 memoryMonitor ./bpf_src/memory_monitor.c -- -I../bpf_common

// ebpfState holds eBPF components
type ebpfState struct {
	objs   *memoryMonitorObjects
	links  []link.Link
	reader *ringbuf.Reader
}

// startEBPF initializes and attaches eBPF programs
func (c *Collector) startEBPF() error {
	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		c.logger.Warn("Failed to remove memlock", zap.Error(err))
	}

	// Load eBPF objects
	objs := memoryMonitorObjects{}
	if err := loadMemoryMonitorObjects(&objs, nil); err != nil {
		return fmt.Errorf("failed to load eBPF objects: %w", err)
	}

	state := &ebpfState{
		objs:  &objs,
		links: make([]link.Link, 0),
	}

	// Attach uprobes based on mode
	if c.config.Mode != ModeGrowthDetection {
		// Attach mmap/munmap probes for allocation tracking
		mmapLink, err := link.AttachUprobe(link.UprobeOptions{
			Path:   "/lib/x86_64-linux-gnu/libc.so.6",
			Symbol: "mmap",
			Pid:    int(c.config.TargetPID), // 0 means all processes
		}, objs.TraceMmapEntry)
		if err != nil {
			c.logger.Warn("Failed to attach mmap uprobe", zap.Error(err))
		} else {
			state.links = append(state.links, mmapLink)
		}

		mmapRetLink, err := link.AttachUretprobe(link.UretprobeOptions{
			Path:   "/lib/x86_64-linux-gnu/libc.so.6",
			Symbol: "mmap",
			Pid:    int(c.config.TargetPID),
		}, objs.TraceMmapReturn)
		if err != nil {
			c.logger.Warn("Failed to attach mmap uretprobe", zap.Error(err))
		} else {
			state.links = append(state.links, mmapRetLink)
		}

		munmapLink, err := link.AttachUprobe(link.UprobeOptions{
			Path:   "/lib/x86_64-linux-gnu/libc.so.6",
			Symbol: "munmap",
			Pid:    int(c.config.TargetPID),
		}, objs.TraceMunmap)
		if err != nil {
			c.logger.Warn("Failed to attach munmap uprobe", zap.Error(err))
		} else {
			state.links = append(state.links, munmapLink)
		}
	}

	// Attach RSS tracking (always enabled)
	rssLink, err := link.AttachTracepoint(link.TracepointOptions{
		Group:   "mm",
		Name:    "rss_stat",
		Program: objs.TraceRssChange,
	})
	if err != nil {
		c.logger.Warn("Failed to attach RSS tracepoint", zap.Error(err))
	} else {
		state.links = append(state.links, rssLink)
	}

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		c.cleanupEBPF(state)
		return fmt.Errorf("failed to create ring buffer reader: %w", err)
	}
	state.reader = reader

	c.ebpfState = state
	c.logger.Info("eBPF programs attached",
		zap.Int("links", len(state.links)),
		zap.String("mode", string(c.config.Mode)),
	)

	return nil
}

// stopEBPF detaches eBPF programs
func (c *Collector) stopEBPF() {
	if state, ok := c.ebpfState.(*ebpfState); ok {
		c.cleanupEBPF(state)
	}
}

// cleanupEBPF cleans up eBPF resources
func (c *Collector) cleanupEBPF(state *ebpfState) {
	if state == nil {
		return
	}

	if state.reader != nil {
		state.reader.Close()
	}

	for _, l := range state.links {
		if l != nil {
			l.Close()
		}
	}

	if state.objs != nil {
		state.objs.Close()
	}
}

// readEBPFEvents reads events from eBPF ring buffer
func (c *Collector) readEBPFEvents() {
	state, ok := c.ebpfState.(*ebpfState)
	if !ok || state == nil || state.reader == nil {
		c.logger.Error("Invalid eBPF state")
		return
	}

	c.logger.Info("Starting eBPF event reader")

	for {
		select {
		case <-c.ctx.Done():
			c.logger.Info("Stopping eBPF event reader")
			return
		default:
		}

		record, err := state.reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				c.logger.Info("Ring buffer closed")
				return
			}
			c.logger.Warn("Error reading from ring buffer", zap.Error(err))
			if c.errorsTotal != nil {
				c.errorsTotal.Add(c.ctx, 1, metric.WithAttributes(
					attribute.String("error_type", "ringbuf_read"),
				))
			}
			continue
		}

		// Parse the event
		if len(record.RawSample) < 4 {
			c.logger.Warn("Invalid event size", zap.Int("size", len(record.RawSample)))
			continue
		}

		// Process based on reasonable size for our event structure
		c.processRawEvent(record.RawSample)
	}
}

// processRawEvent processes raw eBPF event data
func (c *Collector) processRawEvent(data []byte) {
	start := time.Now()
	defer func() {
		duration := time.Since(start).Seconds() * 1000
		if c.processingTime != nil {
			c.processingTime.Record(c.ctx, duration)
		}
	}()

	// Parse the memory event
	var event MemoryEvent
	buf := bytes.NewBuffer(data)

	// Read fields in order matching C struct
	binary.Read(buf, binary.LittleEndian, &event.Timestamp)
	binary.Read(buf, binary.LittleEndian, &event.EventType)
	binary.Read(buf, binary.LittleEndian, &event.PID)
	binary.Read(buf, binary.LittleEndian, &event.Address)
	binary.Read(buf, binary.LittleEndian, &event.Size)
	binary.Read(buf, binary.LittleEndian, &event.CGroupID)
	copy(event.Comm[:], buf.Next(16))
	binary.Read(buf, binary.LittleEndian, &event.CallerIP)
	binary.Read(buf, binary.LittleEndian, &event.RSSPages)
	binary.Read(buf, binary.LittleEndian, &event.RSSGrowth)

	// Update metrics based on event type
	switch event.EventType {
	case EventTypeMmap:
		if c.allocationsTracked != nil {
			c.allocationsTracked.Add(c.ctx, 1)
		}
		if c.largestAllocation != nil && event.Size > 0 {
			c.largestAllocation.Record(c.ctx, int64(event.Size))
		}
	case EventTypeMunmap:
		if c.deallocationsTracked != nil {
			c.deallocationsTracked.Add(c.ctx, 1)
		}
	case EventTypeRSSGrowth:
		if c.rssGrowthDetected != nil {
			c.rssGrowthDetected.Add(c.ctx, 1)
		}
	case EventTypeUnfreed:
		if c.unfreedMemoryBytes != nil {
			c.unfreedMemoryBytes.Record(c.ctx, int64(event.Size))
		}
	}

	// Apply pre-processing filters
	if !c.shouldEmitEvent(&event) {
		return
	}

	// Convert to domain event
	domainEvent := c.createDomainEvent(&event)

	// Send event
	select {
	case c.events <- domainEvent:
		if c.eventsProcessed != nil {
			c.eventsProcessed.Add(c.ctx, 1, metric.WithAttributes(
				attribute.String("event_type", event.EventType.String()),
			))
		}
		if c.bufferUsage != nil {
			c.bufferUsage.Record(c.ctx, int64(len(c.events)))
		}
	default:
		// Buffer full, drop event
		if c.droppedEvents != nil {
			c.droppedEvents.Add(c.ctx, 1, metric.WithAttributes(
				attribute.String("reason", "buffer_full"),
			))
		}
	}
}
