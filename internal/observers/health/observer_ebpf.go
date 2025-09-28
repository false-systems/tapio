//go:build linux
// +build linux

package health

import (
	"errors"
	"fmt"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// ebpfStateImpl contains eBPF-specific state using generated objects
type ebpfStateImpl struct {
	objs       *healthMonitorObjects
	links      []link.Link
	ringReader *ringbuf.Reader
}

// startEBPF initializes and attaches eBPF programs
func (o *Observer) startEBPF() error {
	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("failed to remove memlock: %w", err)
	}

	// Load pre-compiled eBPF objects using generated bindings
	objs := &healthMonitorObjects{}
	if err := loadHealthMonitorObjects(objs, nil); err != nil {
		var ve *ebpf.VerifierError
		if errors.As(err, &ve) {
			o.logger.Error("eBPF verifier error", zap.String("error", ve.Error()))
		}
		return fmt.Errorf("failed to load eBPF objects: %w", err)
	}

	// Configure the health monitoring
	if err := o.updateEBPFConfig(objs); err != nil {
		objs.Close()
		return fmt.Errorf("failed to update config: %w", err)
	}

	// Create ring buffer reader for health events
	reader, err := ringbuf.NewReader(objs.HealthEvents)
	if err != nil {
		objs.Close()
		return fmt.Errorf("failed to create ring buffer reader: %w", err)
	}

	// Create state
	state := &ebpfStateImpl{
		objs:       objs,
		links:      make([]link.Link, 0),
		ringReader: reader,
	}

	// Attach all the syscall tracepoints
	if err := o.attachTracepoints(state); err != nil {
		reader.Close()
		objs.Close()
		return fmt.Errorf("failed to attach tracepoints: %w", err)
	}

	// Store state
	o.ebpfState = state

	o.logger.Info("eBPF health monitor loaded successfully",
		zap.Int("tracepoints", len(state.links)))

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

	// Close ring buffer reader
	if state.ringReader != nil {
		if err := state.ringReader.Close(); err != nil {
			o.logger.Warn("Failed to close ring buffer reader", zap.Error(err))
		}
	}

	// Detach all tracepoints
	for _, l := range state.links {
		if l != nil {
			if err := l.Close(); err != nil {
				o.logger.Warn("Failed to close link", zap.Error(err))
			}
		}
	}

	// Close eBPF objects
	if state.objs != nil {
		if err := state.objs.Close(); err != nil {
			o.logger.Warn("Failed to close eBPF objects", zap.Error(err))
		}
	}

	o.ebpfState = nil
	o.logger.Info("eBPF health monitor stopped")
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

	if state.ringReader == nil {
		o.logger.Error("No ring buffer reader available")
		return
	}

	o.logger.Info("Starting health event reader loop")

	for {
		select {
		case <-o.LifecycleManager.Context().Done():
			o.logger.Info("Stopping event reader due to context cancellation")
			return
		default:
		}

		record, err := state.ringReader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				o.logger.Info("Ring buffer reader closed")
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

		// Parse the raw event from kernel
		if len(record.RawSample) < int(unsafe.Sizeof(HealthEvent{})) {
			o.logger.Warn("Received truncated event",
				zap.Int("size", len(record.RawSample)),
				zap.Int("expected", int(unsafe.Sizeof(HealthEvent{}))))
			continue
		}

		// Convert kernel event to Go struct
		kernelEvent := (*HealthEvent)(unsafe.Pointer(&record.RawSample[0]))
		event := o.kernelEventToGo(kernelEvent)

		// Filter by category
		category := getCategoryName(event.Category)
		if enabled, ok := o.config.EnabledCategories[category]; !ok || !enabled {
			continue
		}

		// Update metrics based on error code
		o.updateErrorMetrics(event.ErrorCode)

		// Convert to domain event
		domainEvent := o.convertToCollectorEvent(event)

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

	// Count active error tracking entries
	var key healthMonitorErrorKey
	var value healthMonitorErrorStats
	iter := state.objs.ErrorTracking.Iterate()
	for iter.Next(&key, &value) {
		stats.TotalErrors += uint64(value.Count)

		// Count specific error types
		switch key.ErrorCode {
		case -28: // ENOSPC
			stats.ENOSPCCount += uint64(value.Count)
		case -12: // ENOMEM
			stats.ENOMEMCount += uint64(value.Count)
		case -111: // ECONNREFUSED
			stats.ECONNREFUSEDCount += uint64(value.Count)
		case -5: // EIO
			stats.EIOCount += uint64(value.Count)
		}
	}

	return stats, nil
}

// updateEBPFConfig updates the eBPF program configuration
func (o *Observer) updateEBPFConfig(objs *healthMonitorObjects) error {
	config := healthMonitorHealthConfig{
		RateLimitNs:   uint32(o.config.RateLimitMs * 1_000_000), // Convert ms to ns
		MaxErrorCount: 100,                                      // Max errors before rate limiting
		EnableFile:    boolToUint8(o.config.EnabledCategories["file"]),
		EnableNetwork: boolToUint8(o.config.EnabledCategories["network"]),
		EnableMemory:  boolToUint8(o.config.EnabledCategories["memory"]),
		EnableProcess: boolToUint8(o.config.EnabledCategories["process"]),
	}

	key := uint32(0)
	if err := objs.Config.Put(key, config); err != nil {
		return fmt.Errorf("failed to update config map: %w", err)
	}

	return nil
}

// attachTracepoints attaches all syscall tracepoints
func (o *Observer) attachTracepoints(state *ebpfStateImpl) error {
	tracepoints := []struct {
		name    string
		group   string
		program *ebpf.Program
	}{
		// Remove sys_exit_open - doesn't exist on this kernel
		{"sys_exit_openat", "syscalls", state.objs.TraceExitOpenat},
		{"sys_exit_write", "syscalls", state.objs.TraceExitWrite},
		{"sys_exit_mmap", "syscalls", state.objs.TraceExitMmap},
		{"sys_exit_brk", "syscalls", state.objs.TraceExitBrk},
		{"sys_exit_connect", "syscalls", state.objs.TraceExitConnect},
		{"sys_exit_bind", "syscalls", state.objs.TraceExitBind},
		// Remove sys_exit_fork - doesn't exist on this kernel
		{"sys_exit_clone", "syscalls", state.objs.TraceExitClone},
	}

	for _, tp := range tracepoints {
		if tp.program == nil {
			o.logger.Warn("Program not found, skipping", zap.String("program", tp.name))
			continue
		}

		l, err := link.Tracepoint(tp.group, tp.name, tp.program, nil)
		if err != nil {
			o.logger.Warn("Failed to attach tracepoint",
				zap.String("tracepoint", tp.name),
				zap.Error(err))
			continue
		}
		state.links = append(state.links, l)
	}

	if len(state.links) == 0 {
		return fmt.Errorf("no tracepoints attached successfully")
	}

	return nil
}

// kernelEventToGo converts kernel health event to Go struct (same as DNS observer pattern)
func (o *Observer) kernelEventToGo(ke *HealthEvent) *HealthEvent {
	// For health events, the kernel struct matches Go struct exactly
	// so we can return it directly
	return ke
}

// boolToUint8 converts boolean to uint8 for eBPF config
func boolToUint8(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}
