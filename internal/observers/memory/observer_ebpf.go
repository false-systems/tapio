//go:build linux
// +build linux

package memory

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/internal/observers/memory/bpf"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// Memory event from BPF - must match C struct exactly
type memoryEvent struct {
	Timestamp      uint64
	EventType      uint32
	PID            uint32
	TID            uint32
	UID            uint32
	GID            uint32
	Address        uint64
	Size           uint64
	CgroupID       uint64
	StackID        int64
	AllocationTime uint64
	RSSPages       uint64
	RSSGrowth      int64
	NamespacePID   uint32
	Comm           [16]byte
	IsOOMRisk      uint8
	Pad            [3]uint8
}

// Allocation info for tracking
type allocationInfo struct {
	Size      uint64
	Timestamp uint64
	PID       uint32
	TID       uint32
	StackID   int64
	CgroupID  uint64
	Comm      [16]byte
}

// Overflow stats from BPF
type overflowStats struct {
	RingbufDrops   uint64
	RateLimitDrops uint64
	SamplingDrops  uint64
}

// eBPF state
type memoryEBPF struct {
	objs   *bpf.MemoryObjects
	links  []link.Link
	reader *ringbuf.Reader

	// Maps for direct access
	activeAllocations *ebpf.Map
	stackTraces       *ebpf.Map

	// Metrics
	eventsProcessed metric.Int64Counter
	eventsDropped   metric.Int64Counter
	processingTime  metric.Float64Histogram
	memoryAllocated metric.Int64Counter
	memoryFreed     metric.Int64Counter
	leaksDetected   metric.Int64Counter

	logger *zap.Logger
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// K8s enricher
	k8sEnricher *K8sEnricher
}

// startEBPF initializes and attaches eBPF programs with CO-RE support
func (o *Observer) startEBPF() error {
	o.logger.Info("Starting memory observer with CO-RE eBPF support")
	return o.loadEBPF()
}

// Load eBPF programs
func (o *Observer) loadEBPF() error {
	o.logger.Info("Loading CO-RE eBPF programs for Memory observer")

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("removing memlock: %w", err)
	}

	// Load eBPF objects
	var objs bpf.MemoryObjects
	opts := &ebpf.CollectionOptions{
		Programs: ebpf.ProgramOptions{
			LogSize: 64 * 1024 * 1024, // 64MB for verifier logs
		},
	}

	err := bpf.LoadMemoryObjects(&objs, opts)
	if err != nil {
		var ve *ebpf.VerifierError
		if errors.As(err, &ve) {
			o.logger.Error("BPF verifier error",
				zap.String("error", ve.Error()))
			return fmt.Errorf("BPF verifier rejected program: %w", err)
		}
		return fmt.Errorf("loading BPF objects: %w", err)
	}

	// Initialize K8s enricher
	k8sEnricher, err := NewK8sEnricher(o.logger)
	if err != nil {
		o.logger.Warn("K8s enricher not available", zap.Error(err))
		// Continue without K8s enrichment
	}

	o.ebpfState = &memoryEBPF{
		objs:              &objs,
		links:             make([]link.Link, 0),
		activeAllocations: objs.ActiveAllocations,
		stackTraces:       objs.StackTraces,
		eventsProcessed:   o.eventsProcessed,
		eventsDropped:     o.eventsDropped,
		processingTime:    o.processingTime,
		memoryAllocated:   o.memoryAllocated,
		memoryFreed:       o.memoryFreed,
		leaksDetected:     o.leaksDetected,
		logger:            o.logger,
		k8sEnricher:       k8sEnricher,
	}

	// Attach probes
	if err := o.attachMemoryProbes(); err != nil {
		objs.Close()
		return fmt.Errorf("attaching probes: %w", err)
	}

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(objs.MemoryEvents)
	if err != nil {
		o.closeEBPF()
		return fmt.Errorf("creating ringbuf reader: %w", err)
	}

	ebpfState := o.ebpfState.(*memoryEBPF)
	ebpfState.reader = reader

	// Start event processor
	ctx, cancel := context.WithCancel(context.Background())
	ebpfState.cancel = cancel

	ebpfState.wg.Add(1)
	go o.processEvents(ctx)

	// Start leak scanner
	ebpfState.wg.Add(1)
	go o.scanForLeaks(ctx)

	// Start metrics collector
	ebpfState.wg.Add(1)
	go o.collectMetrics(ctx)

	o.logger.Info("CO-RE eBPF programs loaded successfully for Memory observer")
	return nil
}

// Attach probes
func (o *Observer) attachMemoryProbes() error {
	ebpfState := o.ebpfState.(*memoryEBPF)

	// Determine libc path
	libcPath := o.config.LibCPath
	if libcPath == "" {
		libcPath = "/lib/x86_64-linux-gnu/libc.so.6" // Default for Ubuntu/Debian
	}

	// Open libc executable
	ex, err := link.OpenExecutable(libcPath)
	if err != nil {
		o.logger.Warn("Failed to open libc, malloc/free tracking disabled",
			zap.String("path", libcPath),
			zap.Error(err))
	} else {
		// Attach malloc uprobe
		if prog := ebpfState.objs.TraceMallocEnter; prog != nil {
			l, err := ex.Uprobe("malloc", prog, nil)
			if err != nil {
				o.logger.Warn("Failed to attach malloc uprobe", zap.Error(err))
			} else {
				ebpfState.links = append(ebpfState.links, l)
			}
		}

		// Attach malloc uretprobe
		if prog := ebpfState.objs.TraceMallocReturn; prog != nil {
			l, err := ex.Uretprobe("malloc", prog, nil)
			if err != nil {
				o.logger.Warn("Failed to attach malloc uretprobe", zap.Error(err))
			} else {
				ebpfState.links = append(ebpfState.links, l)
			}
		}

		// Attach free uprobe
		if prog := ebpfState.objs.TraceFree; prog != nil {
			l, err := ex.Uprobe("free", prog, nil)
			if err != nil {
				o.logger.Warn("Failed to attach free uprobe", zap.Error(err))
			} else {
				ebpfState.links = append(ebpfState.links, l)
			}
		}

		// Attach mmap uprobe for large allocations
		if prog := ebpfState.objs.TraceMmap; prog != nil {
			l, err := ex.Uprobe("mmap", prog, nil)
			if err != nil {
				o.logger.Warn("Failed to attach mmap uprobe", zap.Error(err))
			} else {
				ebpfState.links = append(ebpfState.links, l)
			}
		}

		// Attach munmap uprobe
		if prog := ebpfState.objs.TraceMunmap; prog != nil {
			l, err := ex.Uprobe("munmap", prog, nil)
			if err != nil {
				o.logger.Warn("Failed to attach munmap uprobe", zap.Error(err))
			} else {
				ebpfState.links = append(ebpfState.links, l)
			}
		}
	}

	// RSS tracking has been removed - we'll track memory through allocations instead

	o.logger.Info("Attached memory probes",
		zap.Int("count", len(ebpfState.links)))

	return nil
}

// Process events from ring buffer
func (o *Observer) processEvents(ctx context.Context) {
	defer o.ebpfState.(*memoryEBPF).wg.Done()

	ebpfState := o.ebpfState.(*memoryEBPF)

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
			o.logger.Warn("Error reading from ringbuf", zap.Error(err))
			continue
		}

		// Parse event
		if len(record.RawSample) < int(unsafe.Sizeof(memoryEvent{})) {
			o.logger.Warn("Invalid event size", zap.Int("size", len(record.RawSample)))
			continue
		}

		event := (*memoryEvent)(unsafe.Pointer(&record.RawSample[0]))

		// Get stack trace if available
		var stackTrace []string
		if event.StackID >= 0 {
			stackTrace = o.getStackTrace(event.StackID)
		}

		// Convert to domain event
		domainEvent := o.convertToDomainEvent(event, stackTrace)

		// Enrich with K8s metadata
		if ebpfState.k8sEnricher != nil {
			ebpfState.k8sEnricher.EnrichEvent(domainEvent)
		}

		// Update metrics
		switch event.EventType {
		case 1: // EVENT_ALLOCATION
			if o.memoryAllocated != nil {
				o.memoryAllocated.Add(ctx, int64(event.Size),
					metric.WithAttributes(
						attribute.String("type", "malloc")))
			}
		case 2: // EVENT_DEALLOCATION
			if o.memoryFreed != nil {
				o.memoryFreed.Add(ctx, int64(event.Size),
					metric.WithAttributes(
						attribute.String("type", "free")))
			}
		case 4: // EVENT_LEAK
			if o.leaksDetected != nil {
				o.leaksDetected.Add(ctx, 1)
			}
		}

		// Send to channel
		if o.EventChannelManager.SendEvent(domainEvent) {
			o.RecordEvent()
			if o.eventsProcessed != nil {
				o.eventsProcessed.Add(ctx, 1)
			}
		} else {
			o.RecordDrop()
			if o.eventsDropped != nil {
				o.eventsDropped.Add(ctx, 1)
			}
		}
	}
}

// Get stack trace from BPF map
func (o *Observer) getStackTrace(stackID int64) []string {
	ebpfState := o.ebpfState.(*memoryEBPF)
	if ebpfState.stackTraces == nil {
		return nil
	}

	var stack [20]uint64
	key := uint32(stackID)
	if err := ebpfState.stackTraces.Lookup(key, &stack); err != nil {
		return nil
	}

	// Convert addresses to function names
	// This would need symbol resolution logic
	stackTrace := make([]string, 0)
	for _, addr := range stack {
		if addr == 0 {
			break
		}
		stackTrace = append(stackTrace, fmt.Sprintf("0x%x", addr))
	}

	return stackTrace
}

// Scan for memory leaks
func (o *Observer) scanForLeaks(ctx context.Context) {
	defer o.ebpfState.(*memoryEBPF).wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			o.checkForLeaks()
		}
	}
}

// Collect metrics from BPF maps
func (o *Observer) collectMetrics(ctx context.Context) {
	defer o.ebpfState.(*memoryEBPF).wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			o.readOverflowStats()
		}
	}
}

// Read overflow statistics
func (o *Observer) readOverflowStats() {
	// Implementation would read from BPF overflow stats map
	// Similar to what we had before
}

// Convert BPF event to domain event
func (o *Observer) convertToDomainEvent(event *memoryEvent, stackTrace []string) *domain.CollectorEvent {
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
	} else if event.EventType == 4 { // EVENT_LEAK
		severity = domain.EventSeverityWarning
	} else if event.RSSGrowth > 262144 { // > 1GB growth
		severity = domain.EventSeverityWarning
	}

	// Determine event type and operation
	var eventType domain.CollectorEventType
	var operation string
	var isLeak bool

	switch event.EventType {
	case 1: // EVENT_ALLOCATION
		eventType = domain.EventTypeMemoryAllocation
		operation = "allocation"
	case 2: // EVENT_DEALLOCATION
		eventType = domain.EventTypeMemoryDeallocation
		operation = "deallocation"
	case 3: // EVENT_RSS_GROWTH
		eventType = domain.EventTypeMemoryRSSGrowth
		operation = "rss_growth"
	case 4: // EVENT_LEAK
		eventType = domain.EventTypeMemoryLeak
		operation = "leak"
		isLeak = true
	case 5: // EVENT_OOM_RISK
		eventType = domain.EventTypeMemoryOOMRisk
		operation = "oom_risk"
	default:
		eventType = domain.EventTypeMemoryAllocation
		operation = "unknown"
	}

	// Calculate allocation age for leaks
	var allocationAge time.Duration
	if event.AllocationTime > 0 {
		allocationAge = time.Duration(event.Timestamp - event.AllocationTime)
	}

	return &domain.CollectorEvent{
		EventID:   fmt.Sprintf("memory-%d-%d-%d", event.PID, event.EventType, event.Timestamp),
		Timestamp: timestamp,
		Type:      eventType,
		Source:    "memory-observer",
		Severity:  severity,
		EventData: domain.EventDataContainer{
			Process: &domain.ProcessData{
				PID:        int32(event.PID),
				TID:        int32(event.TID),
				UID:        int32(event.UID),
				GID:        int32(event.GID),
				Command:    comm,
				CgroupPath: fmt.Sprintf("/sys/fs/cgroup/%d", event.CgroupID),
			},
			Memory: &domain.MemoryData{
				Operation:     operation,
				Address:       event.Address,
				Size:          int64(event.Size),
				AllocatedSize: int64(event.Size),
				StackTrace:    stackTrace,
				IsLeak:        isLeak,
				AllocationAge: allocationAge,
				RSSPages:      event.RSSPages,
				RSSBytes:      int64(event.RSSPages * 4096),
				RSSGrowth:     event.RSSGrowth,
				OOMRisk:       event.IsOOMRisk > 0,
			},
		},
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"observer": "memory",
				"core":     "true",
				"version":  "2.0",
			},
		},
	}
}

// Helper function to get event type name
func getMemoryEventTypeName(eventType uint32) string {
	switch eventType {
	case 1:
		return "allocation"
	case 2:
		return "deallocation"
	case 3:
		return "rss_growth"
	case 4:
		return "leak"
	case 5:
		return "oom_risk"
	default:
		return "unknown"
	}
}

// Close eBPF resources
func (o *Observer) closeEBPF() {
	if o.ebpfState == nil {
		return
	}

	ebpfState := o.ebpfState.(*memoryEBPF)

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

	// Close K8s enricher
	if ebpfState.k8sEnricher != nil {
		ebpfState.k8sEnricher.Close()
	}

	o.logger.Info("CO-RE eBPF programs closed for Memory observer")
}

// stopEBPF detaches eBPF programs
func (o *Observer) stopEBPF() {
	o.closeEBPF()
}

// convertRawEvent converts the raw BPF event to our internal MemoryEvent
func convertRawEvent(raw *struct {
	Timestamp      uint64
	EventType      uint32
	Pid            uint32
	Tid            uint32
	Uid            uint32
	Gid            uint32
	Address        uint64
	Size           uint64
	CgroupId       uint64
	StackId        int64
	AllocationTime uint64
	RssPages       uint64
	RssGrowth      int64
	NamespacePid   uint32
	Comm           [16]int8
	IsOomRisk      uint8
	Pad            [3]uint8
}) *MemoryEvent {
	// Convert comm from [16]int8 to [16]byte
	var comm [16]byte
	for i, c := range raw.Comm {
		comm[i] = byte(c)
	}

	return &MemoryEvent{
		Timestamp:       raw.Timestamp,
		EventType:       EventType(raw.EventType),
		PID:             raw.Pid,
		TID:             raw.Tid,
		UID:             raw.Uid,
		GID:             raw.Gid,
		Address:         raw.Address,
		Size:            raw.Size,
		CGroupID:        raw.CgroupId,
		Comm:            comm,
		AllocationAgeNs: raw.AllocationTime,
		RSSPages:        raw.RssPages,
		RSSGrowth:       raw.RssGrowth,
		IsLeak:          raw.EventType == 4, // EVENT_LEAK
	}
}

// readEBPFEvents reads events from the eBPF ring buffer
func (o *Observer) readEBPFEvents() {
	if o.ebpfState == nil {
		o.logger.Error("Cannot read events: eBPF not initialized")
		return
	}

	ebpfState := o.ebpfState.(*memoryEBPF)

	o.logger.Info("Started reading memory eBPF events")
	defer o.logger.Info("Stopped reading memory eBPF events")

	for {
		select {
		case <-o.LifecycleManager.Context().Done():
			return
		default:
			record, err := ebpfState.reader.Read()
			if err != nil {
				if errors.Is(err, ringbuf.ErrClosed) {
					return
				}
				o.logger.Debug("Error reading from ring buffer", zap.Error(err))
				continue
			}

			// Parse the raw event - define the struct that matches our BPF C struct
			var rawEvent struct {
				Timestamp      uint64
				EventType      uint32
				Pid            uint32
				Tid            uint32
				Uid            uint32
				Gid            uint32
				Address        uint64
				Size           uint64
				CgroupId       uint64
				StackId        int64
				AllocationTime uint64
				RssPages       uint64
				RssGrowth      int64
				NamespacePid   uint32
				Comm           [16]int8
				IsOomRisk      uint8
				Pad            [3]uint8
			}

			if err := binary.Read(bytes.NewReader(record.RawSample), binary.LittleEndian, &rawEvent); err != nil {
				o.logger.Error("Failed to parse memory event", zap.Error(err))
				continue
			}

			// Convert to internal event type
			event := convertRawEvent(&rawEvent)

			// Apply pre-processing filters
			if !o.shouldEmitEvent(event) {
				continue
			}

			// Create domain event with context from lifecycle manager
			domainEvent := o.createDomainEvent(o.LifecycleManager.Context(), event)

			// Enrich with K8s metadata if available
			if ebpfState.k8sEnricher != nil {
				ebpfState.k8sEnricher.EnrichEvent(domainEvent)
			}

			// Send event
			if o.EventChannelManager.SendEvent(domainEvent) {
				o.BaseObserver.RecordEvent()
				if o.eventsProcessed != nil {
					o.eventsProcessed.Add(o.LifecycleManager.Context(), 1)
				}
			} else {
				o.BaseObserver.RecordDrop()
				if o.eventsDropped != nil {
					o.eventsDropped.Add(o.LifecycleManager.Context(), 1)
				}
			}
		}
	}
}

// scanUnfreedAllocations periodically scans for memory leaks
func (o *Observer) scanUnfreedAllocations() {
	if o.ebpfState == nil {
		o.logger.Error("Cannot scan allocations: eBPF not initialized")
		return
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	o.logger.Info("Started scanning for unfreed allocations")
	defer o.logger.Info("Stopped scanning for unfreed allocations")

	for {
		select {
		case <-o.LifecycleManager.Context().Done():
			return
		case <-ticker.C:
			o.checkForLeaks()
		}
	}
}

// checkForLeaks iterates through active allocations to find potential leaks
func (o *Observer) checkForLeaks() {
	if o.ebpfState == nil {
		return
	}

	ebpfState := o.ebpfState.(*memoryEBPF)
	now := time.Now().UnixNano()

	var key uint64
	var value bpf.MemoryAllocationInfo
	entries := ebpfState.activeAllocations.Iterate()

	leaksFound := 0
	for entries.Next(&key, &value) {
		age := time.Duration(now - int64(value.Timestamp))

		// Check if allocation is old enough to be a leak
		if age > o.config.MinUnfreedAge {
			// Create leak event
			var comm [16]byte
			for i, c := range value.Comm {
				comm[i] = byte(c)
			}

			event := &MemoryEvent{
				Timestamp:       uint64(now),
				EventType:       EventTypeUnfreed,
				PID:             value.Pid,
				TID:             value.Tid,
				Address:         key,
				Size:            value.Size,
				CGroupID:        value.CgroupId,
				Comm:            comm,
				AllocationAgeNs: uint64(age),
				IsLeak:          true,
			}

			// Get stack trace if available
			if value.StackId >= 0 {
				var stackAddrs [20]uint64
				if err := ebpfState.stackTraces.Lookup(uint32(value.StackId), &stackAddrs); err == nil {
					var addrs []uint64
					for _, addr := range stackAddrs {
						if addr != 0 {
							addrs = append(addrs, addr)
						}
					}
					if len(addrs) > 0 {
						event.StackTrace = &StackTrace{
							Addresses: addrs,
						}
					}
				}
			}

			// Apply filters
			if !o.shouldEmitEvent(event) {
				continue
			}

			// Create and send domain event
			domainEvent := o.createDomainEvent(o.LifecycleManager.Context(), event)

			// Enrich with K8s metadata
			if ebpfState.k8sEnricher != nil {
				ebpfState.k8sEnricher.EnrichEvent(domainEvent)
			}

			// Send event
			if o.EventChannelManager.SendEvent(domainEvent) {
				o.BaseObserver.RecordEvent()
				if o.leaksDetected != nil {
					o.leaksDetected.Add(o.LifecycleManager.Context(), 1)
				}
				leaksFound++
			} else {
				o.BaseObserver.RecordDrop()
			}

			// Optionally remove very old allocations to prevent map overflow
			if age > o.config.MinUnfreedAge*10 {
				ebpfState.activeAllocations.Delete(key)
			}
		}
	}

	if leaksFound > 0 {
		o.logger.Info("Found potential memory leaks",
			zap.Int("count", leaksFound),
		)
	}
}
