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

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/perf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/collectors/ebpf/core"
)

// Implementation provides Linux-specific eBPF functionality
type Implementation struct {
	config core.Config

	// eBPF objects
	programs map[string]*ebpf.Program
	maps     map[string]*ebpf.Map
	links    []link.Link

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
		programs:  make(map[string]*ebpf.Program),
		maps:      make(map[string]*ebpf.Map),
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

	// Close all programs
	for _, prog := range impl.programs {
		prog.Close()
	}

	// Close all maps
	for _, m := range impl.maps {
		m.Close()
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
	return len(impl.programs)
}

// MapsCreated returns the number of created maps
func (impl *Implementation) MapsCreated() int {
	return len(impl.maps)
}

// loadMemoryPrograms loads memory-related eBPF programs
func (impl *Implementation) loadMemoryPrograms() error {
	// Create perf event map
	perfMap, err := ebpf.NewMap(&ebpf.MapSpec{
		Type:       ebpf.PerfEventArray,
		KeySize:    4,
		ValueSize:  4,
		MaxEntries: uint32(os.Getpagesize()),
	})
	if err != nil {
		return fmt.Errorf("failed to create perf map: %w", err)
	}
	impl.maps["memory_events"] = perfMap

	// Create perf reader
	reader, err := perf.NewReader(perfMap, os.Getpagesize())
	if err != nil {
		return fmt.Errorf("failed to create perf reader: %w", err)
	}
	impl.perfReader = reader

	// Note: Actual eBPF program loading would go here
	// For now, we're creating a functional skeleton

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

// parseEvent parses a raw perf event record
func (impl *Implementation) parseEvent(record perf.Record) core.RawEvent {
	event := core.RawEvent{
		Timestamp: time.Now(),
		CPU:       record.CPU,
		Data:      record.RawSample,
		Decoded:   make(map[string]interface{}),
	}

	// Basic parsing - would be extended based on actual event structure
	if len(record.RawSample) >= 8 {
		event.PID = binary.LittleEndian.Uint32(record.RawSample[0:4])
		event.TID = binary.LittleEndian.Uint32(record.RawSample[4:8])
	}

	// Determine event type based on context
	event.Type = "system"

	return event
}
