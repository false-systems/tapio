package memory

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/internal/observers/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// Observer implements lean memory leak detection using BaseObserver
type Observer struct {
	*base.BaseObserver        // Embed for Statistics() and Health()
	*base.EventChannelManager // Handles event channels
	*base.LifecycleManager    // Manages goroutines

	config *Config
	mutex  sync.RWMutex

	// eBPF components (Linux-only)
	ebpfState interface{}

	logger *zap.Logger

	// Memory-specific metrics (custom metrics beyond BaseObserver)
	allocationsTracked   metric.Int64Counter
	deallocationsTracked metric.Int64Counter
	rssGrowthDetected    metric.Int64Counter
	unfreedMemoryBytes   metric.Int64Gauge
	largestAllocation    metric.Int64Gauge
	filteredEvents       metric.Int64Counter

	// CO-RE metrics (aligned with observer_ebpf.go)
	eventsProcessed metric.Int64Counter
	eventsDropped   metric.Int64Counter
	processingTime  metric.Float64Histogram
	memoryAllocated metric.Int64Counter
	memoryFreed     metric.Int64Counter
	leaksDetected   metric.Int64Counter

	// Pre-processing state (lean filtering)
	recentStacks map[uint64]time.Time // Simple dedup
	stackMutex   sync.RWMutex
	lastCleanup  time.Time
}

// NewObserver creates a new memory leak hunter observer
func NewObserver(name string, config *Config, logger *zap.Logger) (*Observer, error) {
	// Initialize logger if not provided
	if logger == nil {
		var err error
		logger, err = zap.NewProduction()
		if err != nil {
			return nil, fmt.Errorf("failed to create logger: %w", err)
		}
	}

	// Initialize config if not provided
	if config == nil {
		config = DefaultConfig()
		config.Name = name
	}

	// Initialize base components
	baseObserver := base.NewBaseObserver(name, 5*time.Minute)
	eventManager := base.NewEventChannelManager(config.BufferSize, name, logger)
	lifecycleManager := base.NewLifecycleManager(context.Background(), logger)

	// Get the meter from BaseObserver for consistency
	meter := baseObserver.GetMeter()

	// Create memory-specific metrics using the same meter
	allocationsTracked, err := meter.Int64Counter(
		fmt.Sprintf("%s_allocations_tracked_total", name),
		metric.WithDescription("Total memory allocations tracked"),
	)
	if err != nil {
		logger.Warn("Failed to create allocations counter", zap.Error(err))
	}

	deallocationsTracked, err := meter.Int64Counter(
		fmt.Sprintf("%s_deallocations_tracked_total", name),
		metric.WithDescription("Total memory deallocations tracked"),
	)
	if err != nil {
		logger.Warn("Failed to create deallocations counter", zap.Error(err))
	}

	rssGrowthDetected, err := meter.Int64Counter(
		fmt.Sprintf("%s_rss_growth_detected_total", name),
		metric.WithDescription("Total RSS growth events detected"),
	)
	if err != nil {
		logger.Warn("Failed to create RSS growth counter", zap.Error(err))
	}

	unfreedMemoryBytes, err := meter.Int64Gauge(
		fmt.Sprintf("%s_unfreed_memory_bytes", name),
		metric.WithDescription("Current unfreed memory in bytes"),
	)
	if err != nil {
		logger.Warn("Failed to create unfreed memory gauge", zap.Error(err))
	}

	largestAllocation, err := meter.Int64Gauge(
		fmt.Sprintf("%s_largest_allocation_bytes", name),
		metric.WithDescription("Largest single allocation in bytes"),
	)
	if err != nil {
		logger.Warn("Failed to create largest allocation gauge", zap.Error(err))
	}

	filteredEvents, err := meter.Int64Counter(
		fmt.Sprintf("%s_filtered_events_total", name),
		metric.WithDescription("Total memory events filtered"),
	)
	if err != nil {
		logger.Warn("Failed to create filtered events counter", zap.Error(err))
	}

	// CO-RE metrics
	eventsProcessed, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription("Total memory events processed"),
	)
	if err != nil {
		logger.Warn("Failed to create events processed counter", zap.Error(err))
	}

	eventsDropped, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_dropped_total", name),
		metric.WithDescription("Total memory events dropped"),
	)
	if err != nil {
		logger.Warn("Failed to create events dropped counter", zap.Error(err))
	}

	processingTime, err := meter.Float64Histogram(
		fmt.Sprintf("%s_processing_time_seconds", name),
		metric.WithDescription("Time spent processing memory events"),
	)
	if err != nil {
		logger.Warn("Failed to create processing time histogram", zap.Error(err))
	}

	memoryAllocated, err := meter.Int64Counter(
		fmt.Sprintf("%s_memory_allocated_bytes", name),
		metric.WithDescription("Total memory allocated in bytes"),
	)
	if err != nil {
		logger.Warn("Failed to create memory allocated counter", zap.Error(err))
	}

	memoryFreed, err := meter.Int64Counter(
		fmt.Sprintf("%s_memory_freed_bytes", name),
		metric.WithDescription("Total memory freed in bytes"),
	)
	if err != nil {
		logger.Warn("Failed to create memory freed counter", zap.Error(err))
	}

	leaksDetected, err := meter.Int64Counter(
		fmt.Sprintf("%s_leaks_detected_total", name),
		metric.WithDescription("Total memory leaks detected"),
	)
	if err != nil {
		logger.Warn("Failed to create leaks detected counter", zap.Error(err))
	}

	o := &Observer{
		BaseObserver:         baseObserver,
		EventChannelManager:  eventManager,
		LifecycleManager:     lifecycleManager,
		config:               config,
		logger:               logger.Named(name),
		allocationsTracked:   allocationsTracked,
		deallocationsTracked: deallocationsTracked,
		rssGrowthDetected:    rssGrowthDetected,
		unfreedMemoryBytes:   unfreedMemoryBytes,
		largestAllocation:    largestAllocation,
		filteredEvents:       filteredEvents,
		eventsProcessed:      eventsProcessed,
		eventsDropped:        eventsDropped,
		processingTime:       processingTime,
		memoryAllocated:      memoryAllocated,
		memoryFreed:          memoryFreed,
		leaksDetected:        leaksDetected,
		recentStacks:         make(map[uint64]time.Time),
		lastCleanup:          time.Now(),
	}

	o.logger.Info("Memory leak hunter observer created",
		zap.String("name", name),
		zap.Int64("min_allocation_size", config.MinAllocationSize),
		zap.Duration("min_unfreed_age", config.MinUnfreedAge),
	)

	return o, nil
}

// Start starts memory monitoring
func (o *Observer) Start(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("context cannot be nil")
	}

	o.mutex.Lock()
	defer o.mutex.Unlock()

	tracer := o.BaseObserver.GetTracer()
	ctx, span := tracer.Start(ctx, "memory_leak_hunter.start")
	defer span.End()

	// Start eBPF monitoring if enabled
	if o.config.EnableEBPF {
		if err := o.startEBPF(); err != nil {
			o.BaseObserver.RecordError(err)
			span.RecordError(err)
			return fmt.Errorf("failed to start eBPF: %w", err)
		}

		// Start event processing
		o.LifecycleManager.Start("ebpf-reader", o.readEBPFEvents)

		// Enhancement #1: Start unfreed allocations scanner
		o.LifecycleManager.Start("allocation-scanner", o.scanUnfreedAllocations)
	}

	// Start cleanup routine for dedup cache
	o.LifecycleManager.Start("cleanup-routine", o.cleanupRoutine)

	o.BaseObserver.SetHealthy(true)
	o.logger.Info("Memory leak hunter started",
		zap.String("name", o.Name()),
		zap.Bool("ebpf_enabled", o.config.EnableEBPF),
		zap.String("mode", string(o.config.Mode)),
	)

	return nil
}

// Stop stops the observer
func (o *Observer) Stop() error {
	tracer := o.BaseObserver.GetTracer()
	_, span := tracer.Start(context.Background(), "memory_leak_hunter.stop")
	defer span.End()

	o.mutex.Lock()
	defer o.mutex.Unlock()

	// Stop lifecycle manager
	if err := o.LifecycleManager.Stop(5 * time.Second); err != nil {
		o.logger.Warn("Timeout during shutdown", zap.Error(err))
	}

	// Stop eBPF if running
	o.stopEBPF()

	// Close event channel through EventChannelManager
	o.EventChannelManager.Close()

	o.BaseObserver.SetHealthy(false)
	o.logger.Info("Memory leak hunter stopped")
	return nil
}

// Events returns the event channel
func (o *Observer) Events() <-chan *domain.CollectorEvent {
	return o.EventChannelManager.GetChannel()
}

// Name returns observer name
func (o *Observer) Name() string {
	return o.config.Name
}

// shouldEmitEvent implements lean pre-processing logic
func (o *Observer) shouldEmitEvent(event *MemoryEvent) bool {
	// Age filtering
	if event.EventType == EventTypeUnfreed {
		age := time.Since(time.Unix(0, int64(event.Timestamp)))
		if age < o.config.MinUnfreedAge {
			if o.filteredEvents != nil {
				o.filteredEvents.Add(o.LifecycleManager.Context(), 1, metric.WithAttributes(
					attribute.String("event_type", event.EventType.String()),
					attribute.String("reason", "age_too_young"),
				))
			}
			return false
		}
	}

	// Size filtering
	if event.Size < uint64(o.config.MinAllocationSize) {
		if o.filteredEvents != nil {
			o.filteredEvents.Add(o.LifecycleManager.Context(), 1, metric.WithAttributes(
				attribute.String("event_type", event.EventType.String()),
				attribute.String("reason", "size_too_small"),
			))
		}
		return false
	}

	// Enhancement #8: Improved stack deduplication with composite key
	if event.StackTrace != nil && len(event.StackTrace.Addresses) > 0 {
		o.stackMutex.Lock()
		defer o.stackMutex.Unlock()

		// Use composite key combining first stack address, PID, and allocation address for better deduplication
		stackKey := event.StackTrace.Addresses[0] ^ uint64(event.PID)<<32 ^ event.Address

		if lastSeen, exists := o.recentStacks[stackKey]; exists {
			if time.Since(lastSeen) < o.config.StackDedupWindow {
				if o.filteredEvents != nil {
					o.filteredEvents.Add(o.LifecycleManager.Context(), 1, metric.WithAttributes(
						attribute.String("event_type", event.EventType.String()),
						attribute.String("reason", "stack_deduplicated"),
					))
				}
				return false // Already reported this stack recently
			}
		}
		o.recentStacks[stackKey] = time.Now()
	}

	return true
}

// createDomainEvent converts memory event to domain event
// Enhancement #9: Fixed context propagation - now accepts parent context
func (o *Observer) createDomainEvent(ctx context.Context, event *MemoryEvent) *domain.CollectorEvent {
	tracer := o.BaseObserver.GetTracer()
	ctx, span := tracer.Start(ctx, "memory_leak_hunter.create_event")
	defer span.End()

	spanCtx := span.SpanContext()
	eventID := fmt.Sprintf("memory-%d-%d", event.EventType, time.Now().UnixNano())

	// Determine event type - use existing types
	var eventType domain.CollectorEventType
	switch event.EventType {
	case EventTypeMmap, EventTypeMunmap:
		eventType = domain.EventTypeKernelFS // Memory operations are kernel filesystem ops
	case EventTypeRSSGrowth:
		eventType = domain.EventTypeMemoryPressure
	case EventTypeUnfreed:
		// Enhancement #7: Use more accurate event type for unfreed allocations
		eventType = domain.EventTypeMemoryPressure // More accurate than OOM
	default:
		eventType = domain.EventTypeKernelProcess
	}

	// Build process data
	processData := &domain.ProcessData{
		PID:     int32(event.PID),
		Command: string(event.Comm[:]),
	}

	// Build kernel data (memory operations are kernel-level)
	kernelData := &domain.KernelData{
		EventType: event.EventType.String(),
		PID:       int32(event.PID),
		Command:   string(event.Comm[:]),
		CgroupID:  event.CGroupID,
	}

	// Additional memory-specific data in Custom
	customData := map[string]string{
		"operation":  event.EventType.String(),
		"address":    fmt.Sprintf("0x%x", event.Address),
		"size_bytes": fmt.Sprintf("%d", event.Size),
		"rss_pages":  fmt.Sprintf("%d", event.RSSPages),
		"rss_growth": fmt.Sprintf("%d", event.RSSGrowth),
	}

	// Add stack trace info if available
	if event.StackTrace != nil && len(event.StackTrace.Addresses) > 0 {
		customData["stack_depth"] = fmt.Sprintf("%d", len(event.StackTrace.Addresses))
		customData["stack_top"] = fmt.Sprintf("0x%x", event.StackTrace.Addresses[0])
	}

	return &domain.CollectorEvent{
		EventID:   eventID,
		Timestamp: time.Unix(0, int64(event.Timestamp)),
		Type:      eventType,
		Source:    o.Name(),
		Severity:  o.determineSeverity(event),

		EventData: domain.EventDataContainer{
			Process: processData,
			Kernel:  kernelData,
			Custom:  customData,
		},

		Metadata: domain.EventMetadata{
			Priority: domain.PriorityNormal,
			Tags:     []string{"memory", "allocation"},
			Labels: map[string]string{
				"event_type": event.EventType.String(),
				"size_bytes": fmt.Sprintf("%d", event.Size),
			},
			PID:     int32(event.PID),
			Command: string(event.Comm[:]),
		},

		TraceContext: &domain.TraceContext{
			TraceID: spanCtx.TraceID(),
			SpanID:  spanCtx.SpanID(),
		},
	}
}

// cleanupRoutine periodically cleans up dedup cache
func (o *Observer) cleanupRoutine() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-o.LifecycleManager.Context().Done():
			return
		case <-ticker.C:
			o.stackMutex.Lock()
			now := time.Now()
			for stack, lastSeen := range o.recentStacks {
				if now.Sub(lastSeen) > o.config.StackDedupWindow {
					delete(o.recentStacks, stack)
				}
			}
			o.stackMutex.Unlock()
		}
	}
}

// scanUnfreedAllocations is platform-specific - implemented in collector_ebpf.go for Linux

// determineSeverity determines event severity based on event characteristics
func (o *Observer) determineSeverity(event *MemoryEvent) domain.EventSeverity {
	// Enhancement #7: Dynamic severity based on event characteristics
	switch event.EventType {
	case EventTypeUnfreed:
		if event.Size > uint64(1024*1024*10) { // >10MB
			return domain.EventSeverityError
		}
		return domain.EventSeverityWarning
	case EventTypeRSSGrowth:
		if event.RSSGrowth > 1024 { // >4MB growth
			return domain.EventSeverityWarning
		}
		return domain.EventSeverityInfo
	default:
		return domain.EventSeverityInfo
	}
}

// UpdateBufferSize dynamically updates the buffer size (Enhancement #5)
func (o *Observer) UpdateBufferSize(size int) error {
	o.mutex.Lock()
	defer o.mutex.Unlock()
	if o.LifecycleManager.Context() != nil {
		return fmt.Errorf("cannot update buffer size while running")
	}
	o.config.BufferSize = size
	// Note: EventChannelManager doesn't support dynamic resize after creation
	return nil
}
