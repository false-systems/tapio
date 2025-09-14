//go:build linux
// +build linux

package memory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/observers/memory/bpf"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// Memory event from BPF - must match C struct exactly
type memoryEventCore struct {
	Timestamp    uint64
	EventType    uint32
	PID          uint32
	TID          uint32
	UID          uint32
	GID          uint32
	Address      uint64
	Size         uint64
	CgroupID     uint64
	CallerIP     uint64
	RSSPages     uint64
	RSSGrowth    int64
	NamespacePID uint32
	Comm         [16]byte
	IsOOMRisk    uint8
	Pad          [3]uint8
}

// Allocation info for tracking
type allocationInfoCore struct {
	Size         uint64
	Timestamp    uint64
	PID          uint32
	TID          uint32
	CgroupID     uint64
	CallerIP     uint64
	NamespacePID uint32
	Comm         [16]byte
}

// Overflow stats from BPF
type memoryOverflowStats struct {
	RingbufDrops   uint64
	RateLimitDrops uint64
	SamplingDrops  uint64
}

// CO-RE eBPF implementation
type coreMemoryEBPF struct {
	objs   *bpf.Memorymonitor_coreObjects
	links  []link.Link
	reader *ringbuf.Reader

	// Metrics
	eventsProcessed metric.Int64Counter
	eventsDropped   metric.Int64Counter
	processingTime  metric.Float64Histogram
	memoryAllocated metric.Int64Counter
	memoryFreed     metric.Int64Counter

	logger *zap.Logger
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Load CO-RE eBPF programs
func (o *Observer) loadCoreMemoryEBPF() error {
	o.logger.Info("Loading CO-RE eBPF programs for Memory observer")

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("removing memlock: %w", err)
	}

	// Load eBPF objects directly using the generated Objects type
	var objs bpf.Memorymonitor_coreObjects

	opts := &ebpf.CollectionOptions{
		Programs: ebpf.ProgramOptions{
			LogSize: 64 * 1024 * 1024, // 64MB for verifier logs
		},
	}

	err := bpf.LoadMemorymonitor_coreObjects(&objs, opts)
	if err != nil {
		var ve *ebpf.VerifierError
		if errors.As(err, &ve) {
			// Log verifier error details
			o.logger.Error("BPF verifier error",
				zap.String("error", ve.Error()))
			return fmt.Errorf("BPF verifier rejected program: %w", err)
		}
		return fmt.Errorf("loading BPF objects: %w", err)
	}

	o.ebpfState = &coreMemoryEBPF{
		objs:            &objs,
		links:           make([]link.Link, 0),
		eventsProcessed: o.eventsProcessed,
		eventsDropped:   o.eventsDropped,
		processingTime:  o.processingTime,
		memoryAllocated: o.memoryAllocated,
		memoryFreed:     o.memoryFreed,
		logger:          o.logger,
	}

	// Attach probes
	if err := o.attachCoreMemoryProbes(); err != nil {
		objs.Close()
		return fmt.Errorf("attaching probes: %w", err)
	}

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(objs.MemoryEvents)
	if err != nil {
		o.closeCoreMemoryEBPF()
		return fmt.Errorf("creating ringbuf reader: %w", err)
	}

	ebpfState := o.ebpfState.(*coreMemoryEBPF)
	ebpfState.reader = reader

	// Start event processor
	ctx, cancel := context.WithCancel(context.Background())
	ebpfState.cancel = cancel

	ebpfState.wg.Add(1)
	go o.processCoreMemoryEvents(ctx)

	// Start metrics collector
	ebpfState.wg.Add(1)
	go o.collectCoreMemoryMetrics(ctx)

	// Start unfreed allocation scanner
	ebpfState.wg.Add(1)
	go o.scanCoreUnfreedAllocations(ctx)

	o.logger.Info("CO-RE eBPF programs loaded successfully for Memory observer")
	return nil
}

// Attach CO-RE probes
func (o *Observer) attachCoreMemoryProbes() error {
	ebpfState := o.ebpfState.(*coreMemoryEBPF)

	// Attach RSS tracepoint (our simplified approach uses this only)
	prog := ebpfState.objs.TraceRssChange
	if prog == nil {
		return fmt.Errorf("trace_rss_change program not found")
	}

	l, err := link.Tracepoint("mm", "rss_stat", prog, nil)
	if err != nil {
		return fmt.Errorf("attaching rss_stat tracepoint: %w", err)
	}
	ebpfState.links = append(ebpfState.links, l)

	// Attach periodic scanner (perf event)
	prog = ebpfState.objs.ScanMemoryPeriodic
	if prog != nil {
		// This is a perf_event program that will be triggered from userspace
		o.logger.Debug("Periodic scanner program loaded, will be triggered from userspace")
	}

	o.logger.Debug("Attached CO-RE memory probes",
		zap.Int("count", len(ebpfState.links)))

	return nil
}

// Process events from ring buffer
func (o *Observer) processCoreMemoryEvents(ctx context.Context) {
	defer o.ebpfState.(*coreMemoryEBPF).wg.Done()

	ebpfState := o.ebpfState.(*coreMemoryEBPF)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		record, err := ebpfState.reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				return
			}
			o.logger.Warn("Error reading from ringbuf",
				zap.Error(err))
			continue
		}

		// Parse event
		if len(record.RawSample) < int(unsafe.Sizeof(memoryEventCore{})) {
			o.logger.Warn("Invalid event size",
				zap.Int("size", len(record.RawSample)))
			continue
		}

		event := (*memoryEventCore)(unsafe.Pointer(&record.RawSample[0]))

		// Convert to domain event
		domainEvent := o.convertCoreMemoryToDomainEvent(event)

		// Update metrics
		if event.EventType == 1 { // EVENT_MMAP
			if o.memoryAllocated != nil {
				o.memoryAllocated.Add(ctx, int64(event.Size),
					metric.WithAttributes(
						attribute.String("type", "mmap")))
			}
		} else if event.EventType == 2 { // EVENT_MUNMAP
			if o.memoryFreed != nil {
				o.memoryFreed.Add(ctx, int64(event.Size),
					metric.WithAttributes(
						attribute.String("type", "munmap")))
			}
		}

		// Send to channel using EventChannelManager's SendEvent method
		if o.EventChannelManager.SendEvent(domainEvent) {
			o.RecordEvent()
			if o.eventsProcessed != nil {
				o.eventsProcessed.Add(ctx, 1,
					metric.WithAttributes(
						attribute.String("type", "memory"),
						attribute.String("event", getMemoryEventTypeName(event.EventType))))
			}
		} else {
			o.RecordDrop()
			if o.eventsDropped != nil {
				o.eventsDropped.Add(ctx, 1,
					metric.WithAttributes(
						attribute.String("reason", "channel_full")))
			}
		}
	}
}

// Collect metrics from BPF maps
func (o *Observer) collectCoreMemoryMetrics(ctx context.Context) {
	defer o.ebpfState.(*coreMemoryEBPF).wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			o.readCoreMemoryOverflowStats()
		}
	}
}

// Read overflow statistics from BPF
func (o *Observer) readCoreMemoryOverflowStats() {
	ebpfState := o.ebpfState.(*coreMemoryEBPF)

	var stats memoryOverflowStats
	key := uint32(0)

	// Read overflow stats
	if err := ebpfState.objs.MemoryOverflow.Lookup(key, &stats); err == nil {
		if o.eventsDropped != nil {
			ctx := context.Background()

			o.eventsDropped.Add(ctx, int64(stats.RingbufDrops),
				metric.WithAttributes(attribute.String("reason", "ringbuf_full")))

			o.eventsDropped.Add(ctx, int64(stats.RateLimitDrops),
				metric.WithAttributes(attribute.String("reason", "rate_limit")))

			o.eventsDropped.Add(ctx, int64(stats.SamplingDrops),
				metric.WithAttributes(attribute.String("reason", "sampling")))
		}

		// Reset counters after reading
		stats = memoryOverflowStats{}
		ebpfState.objs.MemoryOverflow.Update(key, &stats, ebpf.UpdateAny)
	}
}

// Scan for unfreed allocations
func (o *Observer) scanCoreUnfreedAllocations(ctx context.Context) {
	defer o.ebpfState.(*coreMemoryEBPF).wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			o.checkCoreUnfreedAllocations()
		}
	}
}

// Check for long-lived allocations
func (o *Observer) checkCoreUnfreedAllocations() {
	// Since the simplified BPF program doesn't track individual allocations,
	// we can generate periodic memory health events based on RSS stats

	now := time.Now().UnixNano()

	// Create a periodic memory status event
	event := &memoryEventCore{
		Timestamp: uint64(now),
		EventType: 4, // EVENT_UNFREED (using as periodic check)
		PID:       uint32(os.Getpid()),
		TID:       uint32(os.Getpid()),
		Size:      0, // Will be filled with RSS data if available
		CgroupID:  0,
	}

	// Get current process name
	if exe, err := os.Executable(); err == nil {
		name := filepath.Base(exe)
		copy(event.Comm[:], []byte(name))
	}

	// Convert and send periodic health check
	domainEvent := o.convertCoreMemoryToDomainEvent(event)

	if o.EventChannelManager.SendEvent(domainEvent) {
		o.RecordEvent()
		o.logger.Debug("Sent periodic memory health check")
	} else {
		o.RecordDrop()
	}
}

// Convert BPF event to domain event
func (o *Observer) convertCoreMemoryToDomainEvent(event *memoryEventCore) *domain.CollectorEvent {
	// Convert timestamp
	timestamp := time.Unix(0, int64(event.Timestamp))

	// Convert comm to string
	comm := string(event.Comm[:])
	for i, b := range event.Comm {
		if b == 0 {
			comm = string(event.Comm[:i])
			break
		}
	}

	// Determine severity
	severity := domain.EventSeverityInfo
	if event.IsOOMRisk > 0 {
		severity = domain.EventSeverityCritical
	} else if event.EventType == 4 { // EVENT_UNFREED
		severity = domain.EventSeverityWarning
	} else if event.RSSGrowth > 262144 { // > 1GB growth
		severity = domain.EventSeverityWarning
	}

	// Create domain event
	return &domain.CollectorEvent{
		EventID:   fmt.Sprintf("memory-%d-%d-%d", event.PID, event.EventType, event.Timestamp),
		Timestamp: timestamp,
		Type:      domain.EventTypeMemory,
		Source:    "memory-observer",
		Severity:  severity,
		EventData: domain.EventDataContainer{
			Process: &domain.ProcessData{
				PID:     int32(event.PID),
				TID:     int32(event.TID),
				UID:     int32(event.UID),
				GID:     int32(event.GID),
				Command: comm,
			},
			Custom: map[string]string{
				"memory_event_type": getMemoryEventTypeName(event.EventType),
				"address":           fmt.Sprintf("0x%x", event.Address),
				"size_bytes":        fmt.Sprintf("%d", event.Size),
				"size_mb":           fmt.Sprintf("%.2f", float64(event.Size)/1048576.0),
				"rss_pages":         fmt.Sprintf("%d", event.RSSPages),
				"rss_growth":        fmt.Sprintf("%d", event.RSSGrowth),
				"rss_gb":            fmt.Sprintf("%.2f", float64(event.RSSPages*4096)/1073741824.0),
				"caller_ip":         fmt.Sprintf("0x%x", event.CallerIP),
				"namespace_pid":     fmt.Sprintf("%d", event.NamespacePID),
				"is_oom_risk":       fmt.Sprintf("%t", event.IsOOMRisk > 0),
				"cgroup_id":         fmt.Sprintf("%d", event.CgroupID),
			},
		},
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"observer": "memory",
				"core":     "true",
				"version":  "1.0",
			},
		},
	}
}

// Helper functions
func getMemoryEventTypeName(eventType uint32) string {
	switch eventType {
	case 1:
		return "mmap"
	case 2:
		return "munmap"
	case 3:
		return "rss_growth"
	case 4:
		return "unfreed"
	case 5:
		return "oom_risk"
	default:
		return "unknown"
	}
}

// Close CO-RE eBPF
func (o *Observer) closeCoreMemoryEBPF() {
	if o.ebpfState == nil {
		return
	}

	ebpfState := o.ebpfState.(*coreMemoryEBPF)

	// Cancel context
	if ebpfState.cancel != nil {
		ebpfState.cancel()
	}

	// Close reader
	if ebpfState.reader != nil {
		ebpfState.reader.Close()
	}

	// Wait for goroutines
	ebpfState.wg.Wait()

	// Detach probes
	for _, l := range ebpfState.links {
		l.Close()
	}

	// Close objects
	if ebpfState.objs != nil {
		ebpfState.objs.Close()
	}

	o.logger.Info("CO-RE eBPF programs closed for Memory observer")
}
