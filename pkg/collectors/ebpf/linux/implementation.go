//go:build linux
// +build linux

package linux

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/perf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/collectors/ebpf/core"
)

// Implementation provides Linux-specific eBPF functionality
type Implementation struct {
	config core.Config

	// eBPF objects
	memoryObjects *memorytrackerObjects
	links         []link.Link

	// Event processing
	perfReader *perf.Reader
	eventChan  chan core.RawEvent

	// State
	ctx    context.Context
	cancel context.CancelFunc
}

// New creates a new Linux eBPF implementation
func New() *Implementation {
	return &Implementation{
		links:     make([]link.Link, 0),
		eventChan: make(chan core.RawEvent, 1000),
	}
}

// Init initializes the implementation
func (impl *Implementation) Init(config core.Config) error {
	impl.config = config

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("failed to remove memlock: %w", err)
	}

	return nil
}

// Start starts the eBPF collection
func (impl *Implementation) Start(ctx context.Context) error {
	impl.ctx, impl.cancel = context.WithCancel(ctx)

	// Load eBPF programs based on configuration
	if impl.config.EnableMemory {
		if err := impl.loadMemoryPrograms(); err != nil {
			return fmt.Errorf("failed to load memory programs: %w", err)
		}
	}

	if impl.config.EnableProcess {
		if err := impl.loadProcessPrograms(); err != nil {
			return fmt.Errorf("failed to load process programs: %w", err)
		}
	}

	if impl.config.EnableNetwork {
		if err := impl.loadNetworkPrograms(); err != nil {
			return fmt.Errorf("failed to load network programs: %w", err)
		}
	}

	// Start event reader
	go impl.readEvents()

	return nil
}

// Stop stops the eBPF collection
func (impl *Implementation) Stop() error {
	if impl.cancel != nil {
		impl.cancel()
	}

	// Close perf reader
	if impl.perfReader != nil {
		impl.perfReader.Close()
	}

	// Detach all links
	for _, l := range impl.links {
		l.Close()
	}

	// Close eBPF objects
	if impl.memoryObjects != nil {
		impl.memoryObjects.Close()
	}

	close(impl.eventChan)

	return nil
}

// Events returns the event channel
func (impl *Implementation) Events() <-chan core.RawEvent {
	return impl.eventChan
}

// ProgramsLoaded returns the number of loaded programs
func (impl *Implementation) ProgramsLoaded() int {
	if impl.memoryObjects != nil {
		return 4 // Number of memory tracking programs
	}
	return 0
}

// MapsCreated returns the number of created maps
func (impl *Implementation) MapsCreated() int {
	if impl.memoryObjects != nil {
		return 3 // Number of memory tracking maps
	}
	return 0
}

// loadMemoryPrograms loads memory-related eBPF programs
func (impl *Implementation) loadMemoryPrograms() error {
	// Load the eBPF memory tracker programs
	objs := &memorytrackerObjects{}
	if err := loadMemorytrackerObjects(objs, nil); err != nil {
		return fmt.Errorf("failed to load memory tracker objects: %w", err)
	}
	impl.memoryObjects = objs

	// Create perf reader for events map
	reader, err := perf.NewReader(objs.Events, os.Getpagesize())
	if err != nil {
		impl.memoryObjects.Close()
		return fmt.Errorf("failed to create perf reader: %w", err)
	}
	impl.perfReader = reader

	// Attach tracepoint programs
	if l, err := link.Tracepoint("kmem", "mm_page_alloc", objs.TraceMmPageAlloc, nil); err != nil {
		impl.memoryObjects.Close()
		return fmt.Errorf("failed to attach mm_page_alloc: %w", err)
	} else {
		impl.links = append(impl.links, l)
	}

	if l, err := link.Tracepoint("kmem", "mm_page_free", objs.TraceMmPageFree, nil); err != nil {
		impl.memoryObjects.Close()
		return fmt.Errorf("failed to attach mm_page_free: %w", err)
	} else {
		impl.links = append(impl.links, l)
	}

	if l, err := link.Tracepoint(link.TracepointOptions{
		Group:   "oom",
		Name:    "oom_kill_process",
		Program: impl.memoryObjects.TraceOomKillProcess,
	}); err != nil {
		impl.memoryObjects.Close()
		return fmt.Errorf("failed to attach oom_kill_process: %w", err)
	} else {
		impl.links = append(impl.links, l)
	}

	return nil
}

// loadProcessPrograms loads process-related eBPF programs
func (impl *Implementation) loadProcessPrograms() error {
	// Process tracking implementation
	// This would load actual eBPF programs for process events
	return nil
}

// loadNetworkPrograms loads network-related eBPF programs
func (impl *Implementation) loadNetworkPrograms() error {
	// Network monitoring implementation
	// This would load actual eBPF programs for network events
	return nil
}

// readEvents reads events from the perf buffer
func (impl *Implementation) readEvents() {
	if impl.perfReader == nil {
		return
	}

	for {
		select {
		case <-impl.ctx.Done():
			return

		default:
			record, err := impl.perfReader.Read()
			if err != nil {
				if errors.Is(err, perf.ErrClosed) {
					return
				}
				// Log error and continue
				continue
			}

			// Parse the event
			event := impl.parseEvent(record)

			// Send event
			select {
			case impl.eventChan <- event:
			case <-impl.ctx.Done():
				return
			}
		}
	}
}

// parseEvent parses a raw perf event record from eBPF memory tracker
func (impl *Implementation) parseEvent(record perf.Record) core.RawEvent {
	event := core.RawEvent{
		Timestamp: time.Now(),
		CPU:       record.CPU,
		Data:      record.RawSample,
		Decoded:   make(map[string]interface{}),
	}

	// Parse memory tracking event structure
	if len(record.RawSample) >= 24 { // Minimum event size
		event.PID = binary.LittleEndian.Uint32(record.RawSample[0:4])
		event.TID = binary.LittleEndian.Uint32(record.RawSample[4:8])

		// Extract memory-specific fields
		eventType := binary.LittleEndian.Uint32(record.RawSample[8:12])
		size := binary.LittleEndian.Uint64(record.RawSample[12:20])
		addr := binary.LittleEndian.Uint64(record.RawSample[20:28])

		switch eventType {
		case 1: // mm_page_alloc
			event.Type = "memory_alloc"
			event.Decoded["operation"] = "page_alloc"
			event.Decoded["size"] = size
			event.Decoded["address"] = fmt.Sprintf("0x%x", addr)
		case 2: // mm_page_free
			event.Type = "memory_free"
			event.Decoded["operation"] = "page_free"
			event.Decoded["size"] = size
			event.Decoded["address"] = fmt.Sprintf("0x%x", addr)
		case 3: // oom_kill_process
			event.Type = "memory_oom"
			event.Decoded["operation"] = "oom_kill"
			event.Decoded["victim_pid"] = size // Reused field for victim PID
		default:
			event.Type = "memory_unknown"
			event.Decoded["operation"] = "unknown"
		}

		// Add common memory context
		event.Decoded["memory_event"] = true
		event.Decoded["kernel_source"] = "tracepoint"
	} else {
		// Fallback for incomplete events
		event.Type = "memory_partial"
		event.Decoded["error"] = "incomplete_event_data"
	}

	return event
}
