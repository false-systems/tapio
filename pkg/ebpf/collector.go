//go:build linux && ebpf
// +build linux,ebpf

package ebpf

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -Werror" -target amd64,arm64 oomdetector ../../ebpf/oom_detector.c -- -I../../ebpf

// Collector manages eBPF programs and collects kernel events
type Collector struct {
	objs         oomdetectorObjects
	links        []link.Link
	reader       *ringbuf.Reader
	events       chan *MemoryEvent
	processStats map[uint32]*ProcessMemoryStats
	statsMutex   sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewCollector creates a new eBPF event collector
func NewCollector() (*Collector, error) {
	// Load pre-compiled eBPF program
	spec, err := loadOomdetector()
	if err != nil {
		return nil, fmt.Errorf("failed to load eBPF spec: %w", err)
	}

	// Create collection from spec
	var objs oomdetectorObjects
	if err := spec.LoadAndAssign(&objs, nil); err != nil {
		return nil, fmt.Errorf("failed to load eBPF objects: %w", err)
	}

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		objs.Close()
		return nil, fmt.Errorf("failed to create ring buffer reader: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	collector := &Collector{
		objs:         objs,
		reader:       reader,
		events:       make(chan *MemoryEvent, 1000),
		processStats: make(map[uint32]*ProcessMemoryStats),
		ctx:          ctx,
		cancel:       cancel,
	}

	// Attach eBPF programs to kernel
	if err := collector.attachPrograms(); err != nil {
		collector.Close()
		return nil, fmt.Errorf("failed to attach eBPF programs: %w", err)
	}

	// Start event processing goroutines
	go collector.readEvents()
	go collector.processEvents()

	return collector, nil
}

// attachPrograms attaches eBPF programs to kernel tracepoints
func (c *Collector) attachPrograms() error {
	var err error

	// Attach memory allocation tracker
	l1, err := link.Tracepoint("kmem", "mm_page_alloc", c.objs.TrackMemoryAlloc, nil)
	if err != nil {
		return fmt.Errorf("failed to attach memory alloc tracer: %w", err)
	}
	c.links = append(c.links, l1)

	// Attach memory free tracker
	l2, err := link.Tracepoint("kmem", "mm_page_free", c.objs.TrackMemoryFree, nil)
	if err != nil {
		return fmt.Errorf("failed to attach memory free tracer: %w", err)
	}
	c.links = append(c.links, l2)

	// Attach OOM kill tracker
	l3, err := link.Tracepoint("oom", "oom_score_adj_update", c.objs.TrackOomKill, nil)
	if err != nil {
		return fmt.Errorf("failed to attach OOM tracer: %w", err)
	}
	c.links = append(c.links, l3)

	// Attach process exit tracker
	l4, err := link.Tracepoint("sched", "sched_process_exit", c.objs.TrackProcessExit, nil)
	if err != nil {
		return fmt.Errorf("failed to attach process exit tracer: %w", err)
	}
	c.links = append(c.links, l4)

	return nil
}

// readEvents reads raw events from eBPF ring buffer
func (c *Collector) readEvents() {
	defer close(c.events)

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			record, err := c.reader.Read()
			if err != nil {
				if c.ctx.Err() != nil {
					return // Context cancelled
				}
				fmt.Printf("Error reading from ring buffer: %v\n", err)
				continue
			}

			event, err := parseRawMemoryEvent(record.RawSample)
			if err != nil {
				fmt.Printf("Error parsing event: %v\n", err)
				continue
			}

			select {
			case c.events <- event:
			case <-c.ctx.Done():
				return
			}
		}
	}
}

// processEvents processes events and maintains process statistics
func (c *Collector) processEvents() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case event := <-c.events:
			if event != nil {
				c.updateProcessStats(event)
			}
		case <-ticker.C:
			c.cleanupOldStats()
		}
	}
}

// updateProcessStats updates statistics for a process based on an event
func (c *Collector) updateProcessStats(event *MemoryEvent) {
	c.statsMutex.Lock()
	defer c.statsMutex.Unlock()

	stats, exists := c.processStats[event.PID]
	if !exists {
		stats = &ProcessMemoryStats{
			PID:           event.PID,
			Command:       event.Command,
			InContainer:   event.InContainer,
			ContainerPID:  event.ContainerPID,
			GrowthPattern: make([]MemoryDataPoint, 0, 100),
		}
		c.processStats[event.PID] = stats
	}

	stats.LastUpdate = event.Timestamp

	switch event.EventType {
	case EventMemoryAlloc:
		stats.TotalAllocated += event.Size
		stats.CurrentUsage = event.TotalMemory

		// Add data point for growth tracking
		stats.GrowthPattern = append(stats.GrowthPattern, MemoryDataPoint{
			Timestamp: event.Timestamp,
			Usage:     event.TotalMemory,
		})

		// Keep only recent data points (last 100 points)
		if len(stats.GrowthPattern) > 100 {
			stats.GrowthPattern = stats.GrowthPattern[len(stats.GrowthPattern)-100:]
		}

	case EventMemoryFree:
		stats.TotalFreed += event.Size
		stats.CurrentUsage = event.TotalMemory

	case EventOOMKill:
		// Mark this process as OOM killed
		stats.CurrentUsage = 0

	case EventProcessExit:
		// Clean up stats for exited process
		delete(c.processStats, event.PID)
	}
}

// cleanupOldStats removes statistics for processes that haven't been seen recently
func (c *Collector) cleanupOldStats() {
	c.statsMutex.Lock()
	defer c.statsMutex.Unlock()

	cutoff := time.Now().Add(-5 * time.Minute)
	for pid, stats := range c.processStats {
		if stats.LastUpdate.Before(cutoff) {
			delete(c.processStats, pid)
		}
	}
}

// GetProcessStats returns current statistics for all tracked processes
func (c *Collector) GetProcessStats() map[uint32]*ProcessMemoryStats {
	c.statsMutex.RLock()
	defer c.statsMutex.RUnlock()

	// Return a copy to avoid race conditions
	result := make(map[uint32]*ProcessMemoryStats)
	for pid, stats := range c.processStats {
		// Deep copy the stats
		statsCopy := *stats
		statsCopy.GrowthPattern = make([]MemoryDataPoint, len(stats.GrowthPattern))
		copy(statsCopy.GrowthPattern, stats.GrowthPattern)
		result[pid] = &statsCopy
	}

	return result
}

// GetContainerProcesses returns statistics for processes running in containers
func (c *Collector) GetContainerProcesses() map[uint32]*ProcessMemoryStats {
	allStats := c.GetProcessStats()
	containerStats := make(map[uint32]*ProcessMemoryStats)

	for pid, stats := range allStats {
		if stats.InContainer {
			containerStats[pid] = stats
		}
	}

	return containerStats
}

// Close stops the collector and cleans up resources
func (c *Collector) Close() error {
	c.cancel()

	// Close ring buffer reader
	if c.reader != nil {
		c.reader.Close()
	}

	// Detach all links
	for _, l := range c.links {
		l.Close()
	}

	// Close eBPF objects
	c.objs.Close()

	return nil
}

// GetMemoryPredictions returns OOM predictions for all tracked processes
func (c *Collector) GetMemoryPredictions(memoryLimits map[uint32]uint64) map[uint32]*OOMPrediction {
	stats := c.GetProcessStats()
	predictions := make(map[uint32]*OOMPrediction)

	for pid, processStats := range stats {
		if limit, hasLimit := memoryLimits[pid]; hasLimit {
			if prediction := processStats.PredictOOM(limit); prediction != nil {
				predictions[pid] = prediction
			}
		}
	}

	return predictions
}