//go:build linux
// +build linux

package memory_leak_hunter

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// eBPF generation is handled by bpf/generate.go

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
		// Enhancement #3: Use configurable libc path
		// Open the libc executable
		ex, err := link.OpenExecutable(c.config.LibCPath)
		if err != nil {
			c.logger.Warn("Failed to open libc", zap.Error(err))
		} else {
			// Attach mmap uprobe
			mmapLink, err := ex.Uprobe("mmap", objs.TraceMmapEntry, &link.UprobeOptions{
				PID: int(c.config.TargetPID), // 0 means all processes
			})
			if err != nil {
				c.logger.Warn("Failed to attach mmap uprobe", zap.Error(err))
			} else {
				state.links = append(state.links, mmapLink)
			}

			// Attach mmap uretprobe
			mmapRetLink, err := ex.Uretprobe("mmap", objs.TraceMmapReturn, &link.UprobeOptions{
				PID: int(c.config.TargetPID),
			})
			if err != nil {
				c.logger.Warn("Failed to attach mmap uretprobe", zap.Error(err))
			} else {
				state.links = append(state.links, mmapRetLink)
			}

			// Attach munmap uprobe
			munmapLink, err := ex.Uprobe("munmap", objs.TraceMunmap, &link.UprobeOptions{
				PID: int(c.config.TargetPID),
			})
			if err != nil {
				c.logger.Warn("Failed to attach munmap uprobe", zap.Error(err))
			} else {
				state.links = append(state.links, munmapLink)
			}
		}
	}

	// Attach RSS tracking (always enabled)
	rssLink, err := link.Tracepoint("mm", "rss_stat", objs.TraceRssChange, nil)
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

	// Enhancement #4: Fail if no probes attached
	if len(state.links) == 0 {
		c.cleanupEBPF(state)
		return fmt.Errorf("no eBPF probes or tracepoints attached")
	}

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
			c.RecordErrorWithContext(c.ctx, err)
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
		duration := time.Since(start)
		c.RecordProcessingDuration(c.ctx, duration)
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
		// Enhancement #13: Add context to metrics
		if c.largestAllocation != nil && event.Size > 0 {
			c.largestAllocation.Record(c.ctx, int64(event.Size), metric.WithAttributes(
				attribute.Int("pid", int(event.PID)),
				attribute.String("comm", string(bytes.Trim(event.Comm[:], "\x00"))),
			))
		}
	case EventTypeMunmap:
		if c.deallocationsTracked != nil {
			c.deallocationsTracked.Add(c.ctx, 1)
		}
	case EventTypeRSSGrowth:
		// Enhancement #11: Validate RSS changes with userspace data
		if c.validateRSSGrowth(&event) {
			if c.rssGrowthDetected != nil {
				c.rssGrowthDetected.Add(c.ctx, 1)
			}
		} else {
			return // Skip invalid RSS growth events
		}
	case EventTypeUnfreed:
		// Enhancement #13: Add context to metrics
		if c.unfreedMemoryBytes != nil {
			c.unfreedMemoryBytes.Record(c.ctx, int64(event.Size), metric.WithAttributes(
				attribute.Int("pid", int(event.PID)),
				attribute.String("comm", string(bytes.Trim(event.Comm[:], "\x00"))),
			))
		}
	}

	// Apply pre-processing filters
	if !c.shouldEmitEvent(&event) {
		return
	}

	// Enhancement #9: Pass context to createDomainEvent
	domainEvent := c.createDomainEvent(c.ctx, &event)

	// Send event
	select {
	case c.events <- domainEvent:
		c.RecordEventWithContext(c.ctx)
		c.RecordEventSize(c.ctx, int64(len(c.events))) // Use buffer usage as event size metric
	default:
		// Enhancement #5: Log warning when events are dropped
		c.logger.Warn("Dropping event due to full channel",
			zap.Int("buffer_size", len(c.events)),
			zap.String("event_type", event.EventType.String()))
		c.RecordDropWithReason(c.ctx, "buffer_full")
	}
}

// Enhancement #11: Validate RSS growth with userspace data
func (c *Collector) validateRSSGrowth(event *MemoryEvent) bool {
	statmPath := fmt.Sprintf("/proc/%d/statm", event.PID)
	statm, err := os.ReadFile(statmPath)
	if err != nil {
		// Process might have exited, consider the event valid
		return true
	}

	var vmSize, vmRSS, vmShared uint64
	fmt.Sscanf(string(statm), "%d %d %d", &vmSize, &vmRSS, &vmShared)

	// Convert pages to bytes (assuming 4KB pages)
	actualRSSBytes := vmRSS * 4096
	reportedRSSBytes := event.RSSPages * 4096

	// Allow some tolerance (10%)
	tolerance := actualRSSBytes / 10
	if reportedRSSBytes > actualRSSBytes+tolerance {
		c.logger.Warn("RSS growth inconsistent with /proc",
			zap.Uint32("pid", event.PID),
			zap.Uint64("reported_rss", reportedRSSBytes),
			zap.Uint64("actual_rss", actualRSSBytes))
		return false
	}

	return true
}

// Enhancement #10: Configure eBPF map sizes
func (c *Collector) configureMapSizes(maxAllocations, maxProcesses uint32) error {
	state, ok := c.ebpfState.(*ebpfState)
	if !ok || state == nil {
		return fmt.Errorf("invalid eBPF state")
	}

	// Note: This would require a config map in the eBPF program
	// For now, we'll log the intention
	c.logger.Info("Map size configuration requested",
		zap.Uint32("max_allocations", maxAllocations),
		zap.Uint32("max_processes", maxProcesses))

	return nil
}

// Enhancement #14: Configure sampling rate
func (c *Collector) configureSamplingRate(rate uint32) error {
	state, ok := c.ebpfState.(*ebpfState)
	if !ok || state == nil {
		return fmt.Errorf("invalid eBPF state")
	}

	// This would update a config map in the eBPF program
	// For now, we'll log the configuration
	c.logger.Info("Sampling rate configuration requested",
		zap.Uint32("rate", rate))

	return nil
}

// Enhancement #1: Implement unfreed allocations scanner (Linux-specific due to eBPF map iteration)
func (c *Collector) scanUnfreedAllocations() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			state, ok := c.ebpfState.(*ebpfState)
			if !ok || state == nil {
				continue
			}

			var addr uint64
			var info AllocationInfo
			now := uint64(time.Now().UnixNano())
			threshold := uint64(c.config.MinUnfreedAge.Nanoseconds())

			iter := state.objs.ActiveAllocations.Iterate()
			for iter.Next(&addr, &info) {
				if now-info.Timestamp > threshold {
					event := MemoryEvent{
						Timestamp: now,
						EventType: EventTypeUnfreed,
						PID:       info.PID,
						Address:   addr,
						Size:      info.Size,
						CGroupID:  info.CGroupID,
						Comm:      info.Comm,
						CallerIP:  info.CallerIP,
					}
					if c.shouldEmitEvent(&event) {
						domainEvent := c.createDomainEvent(c.ctx, &event)
						select {
						case c.events <- domainEvent:
							c.RecordEventWithContext(c.ctx)
						default:
							c.RecordDropWithReason(c.ctx, "buffer_full")
						}
					}
				}
			}
		}
	}
}
