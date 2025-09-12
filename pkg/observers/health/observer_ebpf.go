//go:build linux
// +build linux

package health

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/perf"
	"github.com/cilium/ebpf/rlimit"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// ebpfObjects contains eBPF objects
type ebpfObjects struct {
	Programs map[string]*ebpf.Program
	Maps     map[string]*ebpf.Map
}

// ebpfStateImpl contains eBPF-specific state
type ebpfStateImpl struct {
	objs       *ebpfObjects
	links      []link.Link
	perfReader *perf.Reader
}

// startEBPF initializes and attaches eBPF programs
func (o *Observer) startEBPF() error {
	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("failed to remove memlock: %w", err)
	}

	// Load embedded eBPF objects
	objs, err := o.loadEBPFObjects()
	if err != nil {
		return fmt.Errorf("failed to load eBPF objects: %w", err)
	}

	// Create state
	state := &ebpfStateImpl{
		objs:  objs,
		links: make([]link.Link, 0),
	}

	// Create perf event reader for ring buffer
	eventsMap, ok := objs.Maps["events"]
	if !ok {
		return fmt.Errorf("events map not found")
	}

	reader, err := perf.NewReader(eventsMap, o.config.RingBufferSize)
	if err != nil {
		return fmt.Errorf("failed to create perf reader: %w", err)
	}
	state.perfReader = reader

	// Attach syscall exit tracepoint
	exitProg, ok := objs.Programs["trace_syscall_exit"]
	if !ok {
		return fmt.Errorf("trace_syscall_exit program not found")
	}

	exitLink, err := link.Tracepoint("raw_syscalls", "sys_exit", exitProg, nil)
	if err != nil {
		return fmt.Errorf("failed to attach sys_exit tracepoint: %w", err)
	}
	state.links = append(state.links, exitLink)

	// Store state
	o.ebpfState = state

	o.logger.Info("eBPF programs attached successfully",
		zap.Int("programs", len(objs.Programs)),
		zap.Int("maps", len(objs.Maps)),
	)

	return nil
}

// stopEBPF detaches and cleans up eBPF programs
func (o *Observer) stopEBPF() {
	if o.ebpfState == nil {
		return
	}

	state, ok := o.ebpfState.(*ebpfStateImpl)
	if !ok {
		o.logger.Error("Invalid eBPF state type")
		return
	}

	// Close perf reader
	if state.perfReader != nil {
		state.perfReader.Close()
	}

	// Detach all programs
	for _, l := range state.links {
		if l != nil {
			l.Close()
		}
	}

	// Close eBPF objects
	if state.objs != nil {
		for name, prog := range state.objs.Programs {
			if prog != nil {
				prog.Close()
				o.logger.Debug("Closed eBPF program", zap.String("program", name))
			}
		}
		for name, m := range state.objs.Maps {
			if m != nil {
				m.Close()
				o.logger.Debug("Closed eBPF map", zap.String("map", name))
			}
		}
	}

	o.ebpfState = nil
	o.logger.Info("eBPF programs detached")
}

// readEvents reads events from the ring buffer
func (o *Observer) readEvents() {
	if o.ebpfState == nil {
		o.logger.Error("No eBPF state available")
		return
	}

	state, ok := o.ebpfState.(*ebpfStateImpl)
	if !ok {
		o.logger.Error("Invalid eBPF state type")
		return
	}

	if state.perfReader == nil {
		o.logger.Error("No perf reader available")
		return
	}

	o.logger.Info("Starting event reader loop")

	for {
		select {
		case <-o.LifecycleManager.Context().Done():
			o.logger.Info("Stopping event reader due to context cancellation")
			return
		default:
		}

		record, err := state.perfReader.Read()
		if err != nil {
			if errors.Is(err, perf.ErrClosed) {
				o.logger.Info("Perf reader closed")
				return
			}
			// Log error but continue
			o.consecutiveErrors++
			if o.consecutiveErrors <= 5 {
				o.logger.Error("Failed to read from ring buffer",
					zap.Error(err),
					zap.Int("consecutive_errors", o.consecutiveErrors))
			}
			continue
		}

		o.consecutiveErrors = 0

		// Parse the event
		if len(record.RawSample) < int(unsafe.Sizeof(HealthEvent{})) {
			o.logger.Warn("Received truncated event",
				zap.Int("size", len(record.RawSample)),
				zap.Int("expected", int(unsafe.Sizeof(HealthEvent{}))))
			continue
		}

		var event HealthEvent
		if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &event); err != nil {
			o.logger.Error("Failed to decode event", zap.Error(err))
			continue
		}

		// Filter by category
		category := getCategoryName(event.Category)
		if enabled, ok := o.config.EnabledCategories[category]; !ok || !enabled {
			continue
		}

		// Update metrics based on error code
		o.updateErrorMetrics(event.ErrorCode)

		// Convert to domain event
		domainEvent := o.convertToCollectorEvent(&event)

		// Send event
		if o.EventChannelManager.SendEvent(domainEvent) {
			o.BaseObserver.RecordEvent()
			if o.eventsProcessed != nil {
				o.eventsProcessed.Add(o.LifecycleManager.Context(), 1,
					metric.WithAttributes(
						attribute.String("error_code", getErrorName(event.ErrorCode)),
						attribute.String("category", category),
					))
			}
		} else {
			o.BaseObserver.RecordDrop()
			if o.eventsDropped != nil {
				o.eventsDropped.Add(o.LifecycleManager.Context(), 1)
			}
		}
	}
}

// updateErrorMetrics updates specific error counters
func (o *Observer) updateErrorMetrics(errorCode int32) {
	ctx := o.LifecycleManager.Context()

	switch errorCode {
	case -28: // ENOSPC
		if o.enospcErrors != nil {
			o.enospcErrors.Add(ctx, 1)
		}
	case -12: // ENOMEM
		if o.enomemErrors != nil {
			o.enomemErrors.Add(ctx, 1)
		}
	case -111: // ECONNREFUSED
		if o.econnrefErrors != nil {
			o.econnrefErrors.Add(ctx, 1)
		}
	case -24: // EMFILE
		if o.emfileErrors != nil {
			o.emfileErrors.Add(ctx, 1)
		}
	case -122: // EDQUOT
		if o.edquotErrors != nil {
			o.edquotErrors.Add(ctx, 1)
		}
	}
}

// getStatsImpl retrieves statistics from eBPF maps
func (o *Observer) getStatsImpl() (*ObserverStats, error) {
	if o.ebpfState == nil {
		return nil, fmt.Errorf("no eBPF state available")
	}

	state, ok := o.ebpfState.(*ebpfStateImpl)
	if !ok {
		return nil, fmt.Errorf("invalid eBPF state type")
	}

	stats := &ObserverStats{}

	// Get stats from eBPF maps if available
	if statsMap, ok := state.objs.Maps["error_stats"]; ok {
		var key uint32
		var value [8]uint64
		iter := statsMap.Iterate()
		for iter.Next(&key, &value) {
			switch key {
			case 0: // Total errors
				stats.TotalErrors = value[0]
			case 28: // ENOSPC
				stats.ENOSPCCount = value[0]
			case 12: // ENOMEM
				stats.ENOMEMCount = value[0]
			case 111: // ECONNREFUSED
				stats.ECONNREFUSEDCount = value[0]
			case 5: // EIO
				stats.EIOCount = value[0]
			}
		}
	}

	return stats, nil
}

// loadEBPFObjects loads pre-compiled eBPF objects
func (o *Observer) loadEBPFObjects() (*ebpfObjects, error) {
	// This would normally load from embedded bytecode
	// For now, returning placeholder
	return &ebpfObjects{
		Programs: make(map[string]*ebpf.Program),
		Maps:     make(map[string]*ebpf.Map),
	}, nil
}
