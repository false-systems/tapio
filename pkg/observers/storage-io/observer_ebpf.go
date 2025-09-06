//go:build linux
// +build linux

package storageio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/perf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
)

// IOEvent represents an I/O event from eBPF
type IOEvent struct {
	TimestampNs uint64
	PID         uint32
	TID         uint32
	CgroupID    uint64
	OpType      uint32 // 1=read, 2=write, 3=fsync, 4=iterate_dir
	Major       uint32
	Minor       uint32
	Inode       uint64
	Size        uint64
	Latency     uint64 // nanoseconds
	IsBlocking  uint8
	_pad        [7]uint8
	Comm        [16]byte
	Path        [256]byte
}

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

	// Create perf event reader
	eventsMap, ok := objs.Maps["events"]
	if !ok {
		return fmt.Errorf("events map not found")
	}

	reader, err := perf.NewReader(eventsMap, o.config.RingBufferSize)
	if err != nil {
		return fmt.Errorf("failed to create perf reader: %w", err)
	}
	state.perfReader = reader

	// Attach to VFS functions
	vfsFuncs := []string{
		"vfs_read",
		"vfs_write",
		"vfs_fsync",
		"iterate_dir",
	}

	for _, fn := range vfsFuncs {
		if prog, ok := objs.Programs[fmt.Sprintf("trace_%s", fn)]; ok {
			l, err := link.AttachTracing(link.TracingOptions{
				Program: prog,
			})
			if err != nil {
				o.logger.Warn("Failed to attach to VFS function",
					zap.String("function", fn),
					zap.Error(err))
				continue
			}
			state.links = append(state.links, l)
		}
	}

	o.ebpfState = state

	o.logger.Info("eBPF programs attached",
		zap.Int("programs", len(state.links)))

	return nil
}

// stopEBPF detaches and cleans up eBPF programs
func (o *Observer) stopEBPF() {
	if o.ebpfState == nil {
		return
	}

	state, ok := o.ebpfState.(*ebpfStateImpl)
	if !ok {
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
		for _, prog := range state.objs.Programs {
			if prog != nil {
				prog.Close()
			}
		}
		for _, m := range state.objs.Maps {
			if m != nil {
				m.Close()
			}
		}
	}

	o.ebpfState = nil
}

// processEventsImpl processes events from eBPF ring buffer
func (o *Observer) processEventsImpl() {
	if o.ebpfState == nil {
		o.logger.Error("No eBPF state available")
		return
	}

	state, ok := o.ebpfState.(*ebpfStateImpl)
	if !ok || state.perfReader == nil {
		return
	}

	for {
		select {
		case <-o.LifecycleManager.Context().Done():
			return
		default:
		}

		record, err := state.perfReader.Read()
		if err != nil {
			if errors.Is(err, perf.ErrClosed) {
				return
			}
			o.logger.Warn("Failed to read from ring buffer", zap.Error(err))
			continue
		}

		// Parse the event
		if len(record.RawSample) < int(unsafe.Sizeof(IOEvent{})) {
			continue
		}

		var event IOEvent
		if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &event); err != nil {
			o.logger.Error("Failed to decode event", zap.Error(err))
			continue
		}

		// Process the event
		o.handleIOEvent(&event)
	}
}

// handleIOEvent processes a single I/O event
func (o *Observer) handleIOEvent(event *IOEvent) {
	// Convert to domain event
	latencyMs := float64(event.Latency) / 1_000_000.0
	path := bytesToString(event.Path[:])
	comm := bytesToString(event.Comm[:])

	// Update metrics
	if o.vfsOperations != nil {
		o.vfsOperations.Add(o.LifecycleManager.Context(), 1,
			attribute.String("operation", getOpTypeName(event.OpType)))
	}

	if o.ioLatencyHistogram != nil {
		o.ioLatencyHistogram.Record(o.LifecycleManager.Context(), latencyMs,
			attribute.String("operation", getOpTypeName(event.OpType)))
	}

	// Check for slow I/O
	if latencyMs > float64(o.config.SlowIOThresholdMs) {
		o.handleSlowIO(event, path, latencyMs)
	}

	// Check for blocking I/O
	if event.IsBlocking == 1 && latencyMs > float64(o.config.BlockingIOThresholdMs) {
		o.handleBlockingIO(event, path, latencyMs)
	}

	// Check if it's a K8s volume operation
	if o.isK8sVolumePath(path) {
		if o.k8sVolumeOperations != nil {
			o.k8sVolumeOperations.Add(o.LifecycleManager.Context(), 1)
		}
	}

	// Create domain event for significant I/O issues
	if latencyMs > float64(o.config.SlowIOThresholdMs) || event.IsBlocking == 1 {
		domainEvent := &domain.CollectorEvent{
			EventID:   fmt.Sprintf("storage-io-%d-%d", event.PID, event.TimestampNs),
			Timestamp: time.Unix(0, int64(event.TimestampNs)),
			Type:      domain.EventTypeStorageIO,
			Source:    o.name,
			Severity:  o.getSeverity(latencyMs, event.IsBlocking == 1),
			EventData: domain.EventDataContainer{
				StorageIO: &domain.StorageIOData{
					Operation: getOpTypeName(event.OpType),
					Path:      path,
					Duration:  time.Duration(latencyMs) * time.Millisecond,
					Size:      int64(event.Size),
					SlowIO:    latencyMs > float64(o.config.SlowIOThresholdMs),
					BlockedIO: event.IsBlocking == 1,
					Device:    fmt.Sprintf("%d:%d", event.Major, event.Minor),
					Inode:     event.Inode,
				},
				Process: &domain.ProcessData{
					PID:     int32(event.PID),
					TID:     int32(event.TID),
					Command: comm,
				},
			},
			Metadata: domain.EventMetadata{
				Labels: map[string]string{
					"slow":     fmt.Sprintf("%v", latencyMs > float64(o.config.SlowIOThresholdMs)),
					"blocking": fmt.Sprintf("%v", event.IsBlocking == 1),
				},
			},
		}

		if o.EventChannelManager.SendEvent(domainEvent) {
			o.BaseObserver.RecordEvent()
			if o.eventsProcessed != nil {
				o.eventsProcessed.Add(o.LifecycleManager.Context(), 1)
			}
		} else {
			o.BaseObserver.RecordDrop()
		}
	}
}

// handleSlowIO handles slow I/O events
func (o *Observer) handleSlowIO(event *IOEvent, path string, latencyMs float64) {
	if o.slowIOOperations != nil {
		o.slowIOOperations.Add(o.LifecycleManager.Context(), 1,
			attribute.String("path", path),
			attribute.String("operation", getOpTypeName(event.OpType)))
	}

	// Update cache
	o.slowIOCacheMu.Lock()
	if entry, ok := o.slowIOCache[path]; ok {
		entry.Count++
		entry.LastSeen = time.Now()
		if time.Duration(latencyMs)*time.Millisecond > entry.Latency {
			entry.Latency = time.Duration(latencyMs) * time.Millisecond
		}
	} else {
		o.slowIOCache[path] = &SlowIOEvent{
			Path:      path,
			Operation: getOpTypeName(event.OpType),
			Latency:   time.Duration(latencyMs) * time.Millisecond,
			Count:     1,
			LastSeen:  time.Now(),
		}
	}
	o.slowIOCacheMu.Unlock()
}

// handleBlockingIO handles blocking I/O events
func (o *Observer) handleBlockingIO(event *IOEvent, path string, latencyMs float64) {
	if o.blockingIOEvents != nil {
		o.blockingIOEvents.Add(o.LifecycleManager.Context(), 1,
			attribute.String("path", path),
			attribute.String("operation", getOpTypeName(event.OpType)))
	}

	o.logger.Warn("Blocking I/O detected",
		zap.String("path", path),
		zap.String("operation", getOpTypeName(event.OpType)),
		zap.Float64("latencyMs", latencyMs),
		zap.Uint32("pid", event.PID))
}

// isK8sVolumePath checks if a path is a Kubernetes volume path
func (o *Observer) isK8sVolumePath(path string) bool {
	for _, monitored := range o.config.MonitoredK8sPaths {
		if len(path) >= len(monitored) && path[:len(monitored)] == monitored {
			return true
		}
	}
	return false
}

// getSeverity determines event severity based on latency and blocking status
func (o *Observer) getSeverity(latencyMs float64, isBlocking bool) domain.EventSeverity {
	if isBlocking && latencyMs > 5000 {
		return domain.EventSeverityCritical
	}
	if latencyMs > 1000 || (isBlocking && latencyMs > 1000) {
		return domain.EventSeverityError
	}
	if latencyMs > float64(o.config.SlowIOThresholdMs) {
		return domain.EventSeverityWarning
	}
	return domain.EventSeverityInfo
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

// Helper functions
func bytesToString(data []byte) string {
	for i, b := range data {
		if b == 0 {
			return string(data[:i])
		}
	}
	return string(data)
}

func getOpTypeName(opType uint32) string {
	switch opType {
	case 1:
		return "read"
	case 2:
		return "write"
	case 3:
		return "fsync"
	case 4:
		return "iterate_dir"
	default:
		return fmt.Sprintf("unknown_%d", opType)
	}
}
