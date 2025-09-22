//go:build linux
// +build linux

package scheduler

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

	// Attach to scheduler tracepoints
	tracepoints := []struct {
		group   string
		name    string
		program string
	}{
		{"sched", "sched_stat_wait", "trace_sched_wait"},
		{"sched", "sched_stat_runtime", "trace_sched_runtime"},
		{"sched", "sched_migrate_task", "trace_sched_migrate"},
		{"sched", "sched_switch", "trace_sched_switch"},
	}

	for _, tp := range tracepoints {
		if prog, ok := objs.Programs[tp.program]; ok {
			l, err := link.Tracepoint(tp.group, tp.name, prog, nil)
			if err != nil {
				o.logger.Warn("Failed to attach tracepoint",
					zap.String("group", tp.group),
					zap.String("name", tp.name),
					zap.Error(err))
				continue
			}
			state.links = append(state.links, l)
		}
	}

	// Also try to attach to CFS bandwidth controller
	if prog, ok := objs.Programs["trace_cfs_throttle"]; ok {
		l, err := link.AttachTracing(link.TracingOptions{
			Program: prog,
		})
		if err == nil {
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

// processEvents processes events from eBPF ring buffer
func (o *Observer) processEvents() {
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
		if len(record.RawSample) < int(unsafe.Sizeof(SchedEvent{})) {
			continue
		}

		var event SchedEvent
		if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &event); err != nil {
			o.logger.Error("Failed to decode event", zap.Error(err))
			continue
		}

		// Apply sampling
		if o.config.SamplingRate > 1 && (event.PID%uint32(o.config.SamplingRate)) != 0 {
			continue
		}

		// Process based on event type
		switch event.EventType {
		case 1: // Scheduling delay
			o.handleSchedDelay(&event)
		case 2: // CFS throttle
			o.handleThrottle(&event)
		case 3: // Core migration
			o.handleMigration(&event)
		case 4: // Priority inversion
			o.handlePriorityInversion(&event)
		}

		// Feed to pattern detector if enabled
		if o.patternDetector != nil {
			o.patternDetector.AddEvent(&event)
		}
	}
}

// handleSchedDelay processes scheduling delay events
func (o *Observer) handleSchedDelay(event *SchedEvent) {
	delayMs := float64(event.Value) / 1_000_000.0

	// Update metrics
	if o.schedDelayHist != nil {
		o.schedDelayHist.Record(o.LifecycleManager.Context(), delayMs/1000.0,
			metric.WithAttributes(attribute.String("comm", bytesToString(event.Comm[:]))))
	}

	// Check threshold
	if delayMs > float64(o.config.SchedDelayThresholdMs) {
		// Calculate wait ratio
		var waitRatio float64
		if event.RunTime > 0 {
			waitRatio = float64(event.WaitTime) / float64(event.RunTime)
		}

		// Gauge metrics removed due to OTEL API changes
		_ = waitRatio

		// Create domain event
		domainEvent := &domain.CollectorEvent{
			EventID:   fmt.Sprintf("scheduler-delay-%d-%d", event.PID, event.TimestampNs),
			Timestamp: time.Unix(0, int64(event.TimestampNs)),
			Type:      domain.EventTypeScheduler,
			Source:    o.name,
			Severity:  o.getDelaySeverity(delayMs),
			EventData: domain.EventDataContainer{
				Scheduler: &domain.SchedulerData{
					EventType:   "scheduling_delay",
					PID:         int32(event.PID),
					TID:         int32(event.TID),
					CPU:         int32(event.CPU),
					Command:     bytesToString(event.Comm[:]),
					DelayMs:     delayMs,
					WaitRatio:   waitRatio,
					Priority:    event.Priority,
					Nice:        event.NiceValue,
					CgroupID:    event.CgroupID,
					ContainerID: bytesToString(event.ContainerID[:]),
				},
			},
			Metadata: domain.EventMetadata{
				Priority: domain.PriorityNormal,
				Labels: map[string]string{
					"observer":   "scheduler",
					"version":    "1.0.0",
					"cpu":        fmt.Sprintf("%d", event.CPU),
					"delay_ms":   fmt.Sprintf("%.2f", delayMs),
					"wait_ratio": fmt.Sprintf("%.2f", waitRatio),
				},
			},
		}

		if o.EventChannelManager.SendEvent(domainEvent) {
			o.BaseObserver.RecordEvent()
		} else {
			o.BaseObserver.RecordDrop()
		}
	}
}

// handleThrottle processes CFS throttle events
func (o *Observer) handleThrottle(event *SchedEvent) {
	throttleMs := float64(event.Value) / 1_000_000.0

	// Update metrics
	if o.throttleTimeHist != nil {
		o.throttleTimeHist.Record(o.LifecycleManager.Context(), throttleMs/1000.0,
			metric.WithAttributes(attribute.String("container_id", bytesToString(event.ContainerID[:]))))
	}

	// Calculate throttle percentage
	if event.RunTime > 0 {
		_ = (float64(event.Value) / float64(event.RunTime+event.Value)) * 100
		// Gauge metrics removed due to OTEL API changes
	}

	// Check threshold
	if throttleMs > float64(o.config.ThrottleThresholdMs) {
		domainEvent := &domain.CollectorEvent{
			EventID:   fmt.Sprintf("scheduler-throttle-%d-%d", event.PID, event.TimestampNs),
			Timestamp: time.Unix(0, int64(event.TimestampNs)),
			Type:      domain.EventTypeScheduler,
			Source:    o.name,
			Severity:  domain.EventSeverityError,
			EventData: domain.EventDataContainer{
				Scheduler: &domain.SchedulerData{
					EventType:   "cfs_throttle",
					PID:         int32(event.PID),
					TID:         int32(event.TID),
					CPU:         int32(event.CPU),
					Command:     bytesToString(event.Comm[:]),
					ThrottleMs:  throttleMs,
					CgroupID:    event.CgroupID,
					ContainerID: bytesToString(event.ContainerID[:]),
				},
			},
			Metadata: domain.EventMetadata{
				Priority: domain.PriorityHigh,
				Labels: map[string]string{
					"observer":    "scheduler",
					"version":     "1.0.0",
					"throttle_ms": fmt.Sprintf("%.2f", throttleMs),
					"cgroup_id":   fmt.Sprintf("%d", event.CgroupID),
				},
			},
			CorrelationHints: &domain.CorrelationHints{
				CorrelationTags: map[string]string{
					"resource_type": "cpu_quota",
					"impact":        "performance_degradation",
				},
			},
		}

		if o.EventChannelManager.SendEvent(domainEvent) {
			o.BaseObserver.RecordEvent()
		} else {
			o.BaseObserver.RecordDrop()
		}
	}
}

// handleMigration processes core migration events
func (o *Observer) handleMigration(event *SchedEvent) {
	// Update metrics
	if o.coreMigrations != nil {
		o.coreMigrations.Add(o.LifecycleManager.Context(), 1,
			metric.WithAttributes(
				attribute.String("comm", bytesToString(event.Comm[:])),
				attribute.String("from_cpu", fmt.Sprintf("%d", event.PrevCPU)),
				attribute.String("to_cpu", fmt.Sprintf("%d", event.NextCPU))))
	}

	// Excessive migrations indicate cache thrashing
	// This would need rate tracking in production
}

// handlePriorityInversion processes priority inversion events
func (o *Observer) handlePriorityInversion(event *SchedEvent) {
	domainEvent := &domain.CollectorEvent{
		EventID:   fmt.Sprintf("scheduler-inversion-%d-%d", event.PID, event.TimestampNs),
		Timestamp: time.Unix(0, int64(event.TimestampNs)),
		Type:      domain.EventTypeScheduler,
		Source:    o.name,
		Severity:  domain.EventSeverityWarning,
		EventData: domain.EventDataContainer{
			Scheduler: &domain.SchedulerData{
				EventType:   "priority_inversion",
				PID:         int32(event.PID),
				TID:         int32(event.TID),
				CPU:         int32(event.CPU),
				Command:     bytesToString(event.Comm[:]),
				Priority:    event.Priority,
				Nice:        event.NiceValue,
				CgroupID:    event.CgroupID,
				ContainerID: bytesToString(event.ContainerID[:]),
			},
		},
		Metadata: domain.EventMetadata{
			Priority: domain.PriorityNormal,
			Labels: map[string]string{
				"observer": "scheduler",
				"version":  "1.0.0",
				"priority": fmt.Sprintf("%d", event.Priority),
				"nice":     fmt.Sprintf("%d", event.NiceValue),
			},
		},
	}

	if o.EventChannelManager.SendEvent(domainEvent) {
		o.BaseObserver.RecordEvent()
	} else {
		o.BaseObserver.RecordDrop()
	}
}

// getDelaySeverity determines severity based on delay
func (o *Observer) getDelaySeverity(delayMs float64) domain.EventSeverity {
	if delayMs > 1000 {
		return domain.EventSeverityCritical
	}
	if delayMs > 100 {
		return domain.EventSeverityError
	}
	if delayMs > float64(o.config.SchedDelayThresholdMs) {
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
