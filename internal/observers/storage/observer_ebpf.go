//go:build linux

package storage

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/cilium/ebpf/ringbuf"
	"github.com/google/uuid"
	"github.com/yairfalse/tapio/internal/base"
	"github.com/yairfalse/tapio/internal/observers/storage/bpf"
	"github.com/yairfalse/tapio/pkg/domain"
	"golang.org/x/sync/errgroup"
)

// Run starts the storage observer with two-stage pipeline.
func (s *StorageObserver) Run(ctx context.Context) error {
	channelSize := s.config.EventChannelSize
	if channelSize == 0 {
		channelSize = 1000
	}

	eventCh := make(chan StorageEventBPF, channelSize)

	g, ctx := errgroup.WithContext(ctx)

	// Stage 1: Load eBPF and read ring buffer
	g.Go(func() error {
		return s.loadAndAttachStage(ctx, eventCh)
	})

	// Stage 2: Process events and emit to intelligence service
	g.Go(func() error {
		return s.processEventsStage(ctx, eventCh)
	})

	return g.Wait()
}

// loadAndAttachStage loads eBPF objects and reads events from ring buffer.
func (s *StorageObserver) loadAndAttachStage(ctx context.Context, eventCh chan StorageEventBPF) error {
	defer close(eventCh)

	// Load eBPF objects
	objs := &bpf.StorageObjects{}
	if err := bpf.LoadStorageObjects(objs, nil); err != nil {
		return fmt.Errorf("failed to load eBPF objects: %w", err)
	}
	defer objs.Close() //nolint:errcheck // cleanup on exit, error non-actionable

	// Store map reference
	s.inflightIOMap = objs.InflightIo

	// Create manager for attachment
	s.ebpfMgr = base.NewEBPFManagerFromCollection(nil)
	defer s.ebpfMgr.Close() //nolint:errcheck // cleanup on exit, error non-actionable

	// Attach tracepoints
	if err := s.ebpfMgr.AttachTracepointWithProgram(
		objs.TraceBlockRqIssue, "block", "block_rq_issue"); err != nil {
		return fmt.Errorf("failed to attach block_rq_issue: %w", err)
	}

	if err := s.ebpfMgr.AttachTracepointWithProgram(
		objs.TraceBlockRqComplete, "block", "block_rq_complete"); err != nil {
		return fmt.Errorf("failed to attach block_rq_complete: %w", err)
	}

	// Open ring buffer reader
	reader, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		return fmt.Errorf("failed to open ring buffer: %w", err)
	}
	defer reader.Close() //nolint:errcheck // cleanup on exit, error non-actionable

	// Read events until context cancelled
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		record, err := reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				return nil
			}
			s.deps.Metrics.RecordError(s.name, "storage_event", "ring_buffer_error")
			continue
		}

		var evt StorageEventBPF
		if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &evt); err != nil {
			s.deps.Metrics.RecordError(s.name, "storage_event", "parse_error")
			continue
		}

		// Send to processing stage (non-blocking)
		select {
		case eventCh <- evt:
		default:
			s.deps.Metrics.RecordDrop(s.name, "storage_event")
		}
	}
}

// processEventsStage processes eBPF events and emits domain events.
func (s *StorageObserver) processEventsStage(ctx context.Context, eventCh chan StorageEventBPF) error {
	for {
		select {
		case <-ctx.Done():
			return nil

		case evt, ok := <-eventCh:
			if !ok {
				return nil // Channel closed
			}

			s.processEvent(ctx, evt)
		}
	}
}

// processEvent processes a single storage event.
func (s *StorageObserver) processEvent(ctx context.Context, evt StorageEventBPF) {
	// Update Prometheus metrics
	(*s.ioOpsTotal).Inc()
	latencyMs := float64(evt.LatencyNs) / 1_000_000.0
	(*s.ioLatencyMs).Set(latencyMs)

	// Create domain event
	domainEvt := s.createDomainEvent(evt)

	// Classify and update specific metrics
	if evt.ErrorCode != 0 {
		(*s.ioErrorsTotal).Inc()
		domainEvt.Subtype = SubtypeIOError
		domainEvt.Severity = domain.SeverityCritical
		domainEvt.Outcome = domain.OutcomeFailure
		domainEvt.Error = &domain.EventError{
			Code:    ErrorName(evt.ErrorCode),
			Message: fmt.Sprintf("I/O error %d on device %d:%d", evt.ErrorCode, evt.DevMajor, evt.DevMinor),
		}
	} else if evt.Severity == SeverityCritical || evt.Severity == SeverityWarning {
		(*s.ioLatencySpikeTotal).Inc()
		domainEvt.Subtype = SubtypeIOLatencySpike
		domainEvt.Outcome = domain.OutcomeSuccess // Slow but successful I/O
		if evt.Severity == SeverityCritical {
			domainEvt.Severity = domain.SeverityCritical
		} else {
			domainEvt.Severity = domain.SeverityWarning
		}
	}

	// Record metrics
	s.deps.Metrics.RecordEvent(s.name, domainEvt.Subtype)

	// Emit via intelligence service
	if err := s.deps.Emitter.Emit(ctx, domainEvt); err != nil {
		log.Printf("[%s] failed to emit event: %v", s.name, err)
	}
}

// createDomainEvent converts a StorageEventBPF to domain.ObserverEvent.
func (s *StorageObserver) createDomainEvent(evt StorageEventBPF) *domain.ObserverEvent {
	latencyMs := float64(evt.LatencyNs) / 1_000_000.0

	storageData := &domain.StorageEventData{
		DeviceMajor:   evt.DevMajor,
		DeviceMinor:   evt.DevMinor,
		OperationType: OperationName(evt.Opcode),
		Bytes:         uint64(evt.Bytes),
		LatencyMs:     latencyMs,
		Sector:        evt.Sector,
		CgroupID:      evt.CgroupID,
		ProcessName:   extractComm(evt.Comm),
		PID:           evt.PID,
	}

	if evt.ErrorCode != 0 {
		storageData.ErrorCode = evt.ErrorCode
		storageData.ErrorName = ErrorName(evt.ErrorCode)
	}

	return &domain.ObserverEvent{
		ID:          uuid.New().String(),
		Type:        string(domain.EventTypeStorage),
		Subtype:     "", // Default, will be set during classification
		Source:      s.name,
		Timestamp:   time.Now(),
		StorageData: storageData,
	}
}
