//go:build linux
// +build linux

package memory

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
	. "github.com/yairfalse/tapio/pkg/observers/memory/bpf"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// eBPF generation is handled by bpf/generate.go

// ebpfState holds eBPF components
type ebpfState struct {
	objs   *MemorymonitorObjects
	links  []link.Link
	reader *ringbuf.Reader
}

// startEBPF initializes and attaches eBPF programs
func (o *Observer) startEBPF() error {
	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		o.logger.Warn("Failed to remove memlock", zap.Error(err))
	}

	// Load eBPF objects
	objs := MemorymonitorObjects{}
	if err := LoadMemorymonitorObjects(&objs, nil); err != nil {
		return fmt.Errorf("failed to load eBPF objects: %w", err)
	}

	state := &ebpfState{
		objs:  &objs,
		links: make([]link.Link, 0),
	}

	// Attach uprobes based on mode
	if o.config.Mode != ModeGrowthDetection {
		// Enhancement #3: Use configurable libc path
		// Open the libc executable
		ex, err := link.OpenExecutable(o.config.LibCPath)
		if err != nil {
			o.logger.Warn("Failed to open libc", zap.Error(err))
		} else {
			// Attach mmap uprobe
			mmapLink, err := ex.Uprobe("mmap", objs.TraceMmapEntry, &link.UprobeOptions{
				PID: int(o.config.TargetPID), // 0 means all processes
			})
			if err != nil {
				o.logger.Warn("Failed to attach mmap uprobe", zap.Error(err))
			} else {
				state.links = append(state.links, mmapLink)
			}

			// Attach mmap uretprobe
			mmapRetLink, err := ex.Uretprobe("mmap", objs.TraceMmapReturn, &link.UprobeOptions{
				PID: int(o.config.TargetPID),
			})
			if err != nil {
				o.logger.Warn("Failed to attach mmap uretprobe", zap.Error(err))
			} else {
				state.links = append(state.links, mmapRetLink)
			}

			// Attach munmap uprobe
			munmapLink, err := ex.Uprobe("munmap", objs.TraceMunmap, &link.UprobeOptions{
				PID: int(o.config.TargetPID),
			})
			if err != nil {
				o.logger.Warn("Failed to attach munmap uprobe", zap.Error(err))
			} else {
				state.links = append(state.links, munmapLink)
			}
		}
	}

	// Attach RSS tracking (always enabled)
	rssLink, err := link.Tracepoint("mm", "rss_stat", objs.TraceRssChange, nil)
	if err != nil {
		o.logger.Warn("Failed to attach RSS tracepoint", zap.Error(err))
	} else {
		state.links = append(state.links, rssLink)
	}

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		o.cleanupEBPF(state)
		return fmt.Errorf("failed to create ring buffer reader: %w", err)
	}
	state.reader = reader

	// Enhancement #4: Fail if no probes attached
	if len(state.links) == 0 {
		o.cleanupEBPF(state)
		return fmt.Errorf("no eBPF probes or tracepoints attached")
	}

	o.ebpfState = state
	o.logger.Info("eBPF programs attached",
		zap.Int("links", len(state.links)),
		zap.String("mode", string(o.config.Mode)),
	)

	return nil
}

// stopEBPF detaches eBPF programs
func (o *Observer) stopEBPF() {
	if state, ok := o.ebpfState.(*ebpfState); ok {
		o.cleanupEBPF(state)
	}
}

// cleanupEBPF cleans up eBPF resources
func (o *Observer) cleanupEBPF(state *ebpfState) {
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
func (o *Observer) readEBPFEvents() {
	state, ok := o.ebpfState.(*ebpfState)
	if !ok || state == nil || state.reader == nil {
		o.logger.Error("Invalid eBPF state")
		return
	}

	o.logger.Info("Starting eBPF event reader")

	for {
		select {
		case <-o.LifecycleManager.Context().Done():
			o.logger.Info("Stopping eBPF event reader")
			return
		default:
		}

		record, err := state.reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				o.logger.Info("Ring buffer closed")
				return
			}
			o.logger.Warn("Error reading from ring buffer", zap.Error(err))
			o.RecordErrorWithContext(o.LifecycleManager.Context(), err)
			continue
		}

		// Parse the event
		if len(record.RawSample) < 4 {
			o.logger.Warn("Invalid event size", zap.Int("size", len(record.RawSample)))
			continue
		}

		// Process based on reasonable size for our event structure
		o.processRawEvent(record.RawSample)
	}
}

// processRawEvent processes raw eBPF event data
func (o *Observer) processRawEvent(data []byte) {
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		o.RecordProcessingDuration(o.LifecycleManager.Context(), duration)
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
		if o.allocationsTracked != nil {
			o.allocationsTracked.Add(o.LifecycleManager.Context(), 1)
		}
		// Enhancement #13: Add context to metrics
		if o.largestAllocation != nil && event.Size > 0 {
			o.largestAllocation.Record(o.LifecycleManager.Context(), int64(event.Size), metric.WithAttributes(
				attribute.Int("pid", int(event.PID)),
				attribute.String("comm", string(bytes.Trim(event.Comm[:], "\x00"))),
			))
		}
	case EventTypeMunmap:
		if o.deallocationsTracked != nil {
			o.deallocationsTracked.Add(o.LifecycleManager.Context(), 1)
		}
	case EventTypeRSSGrowth:
		// Enhancement #11: Validate RSS changes with userspace data
		if o.validateRSSGrowth(&event) {
			if o.rssGrowthDetected != nil {
				o.rssGrowthDetected.Add(o.LifecycleManager.Context(), 1)
			}
		} else {
			return // Skip invalid RSS growth events
		}
	case EventTypeUnfreed:
		// Enhancement #13: Add context to metrics
		if o.unfreedMemoryBytes != nil {
			o.unfreedMemoryBytes.Record(o.LifecycleManager.Context(), int64(event.Size), metric.WithAttributes(
				attribute.Int("pid", int(event.PID)),
				attribute.String("comm", string(bytes.Trim(event.Comm[:], "\x00"))),
			))
		}
	}

	// Apply pre-processing filters
	if !o.shouldEmitEvent(&event) {
		return
	}

	// Enhancement #9: Pass context to createDomainEvent
	domainEvent := o.createDomainEvent(o.LifecycleManager.Context(), &event)

	// Send event
	if o.EventChannelManager.SendEvent(domainEvent) {
		o.RecordEventWithContext(o.LifecycleManager.Context())
	} else {
		// Enhancement #5: Log warning when events are dropped
		o.logger.Warn("Dropping event due to full channel",
			zap.String("event_type", event.EventType.String()))
		o.RecordDropWithReason(o.LifecycleManager.Context(), "buffer_full")
	}
}

// Enhancement #11: Validate RSS growth with userspace data
func (o *Observer) validateRSSGrowth(event *MemoryEvent) bool {
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
		o.logger.Warn("RSS growth inconsistent with /proc",
			zap.Uint32("pid", event.PID),
			zap.Uint64("reported_rss", reportedRSSBytes),
			zap.Uint64("actual_rss", actualRSSBytes))
		return false
	}

	return true
}

// Enhancement #10: Configure eBPF map sizes
func (o *Observer) configureMapSizes(maxAllocations, maxProcesses uint32) error {
	state, ok := o.ebpfState.(*ebpfState)
	if !ok || state == nil {
		return fmt.Errorf("invalid eBPF state")
	}

	// Note: This would require a config map in the eBPF program
	// For now, we'll log the intention
	o.logger.Info("Map size configuration requested",
		zap.Uint32("max_allocations", maxAllocations),
		zap.Uint32("max_processes", maxProcesses))

	return nil
}

// Enhancement #14: Configure sampling rate
func (o *Observer) configureSamplingRate(rate uint32) error {
	state, ok := o.ebpfState.(*ebpfState)
	if !ok || state == nil {
		return fmt.Errorf("invalid eBPF state")
	}

	// This would update a config map in the eBPF program
	// For now, we'll log the configuration
	o.logger.Info("Sampling rate configuration requested",
		zap.Uint32("rate", rate))

	return nil
}

// Enhancement #1: Implement unfreed allocations scanner (Linux-specific due to eBPF map iteration)
func (o *Observer) scanUnfreedAllocations() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-o.LifecycleManager.Context().Done():
			return
		case <-ticker.C:
			state, ok := o.ebpfState.(*ebpfState)
			if !ok || state == nil {
				continue
			}

			var addr uint64
			var info AllocationInfo
			now := uint64(time.Now().UnixNano())
			threshold := uint64(o.config.MinUnfreedAge.Nanoseconds())

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
					if o.shouldEmitEvent(&event) {
						domainEvent := o.createDomainEvent(o.LifecycleManager.Context(), &event)
						if o.EventChannelManager.SendEvent(domainEvent) {
							o.RecordEventWithContext(o.LifecycleManager.Context())
						} else {
							o.RecordDropWithReason(o.LifecycleManager.Context(), "buffer_full")
						}
					}
				}
			}
		}
	}
}
