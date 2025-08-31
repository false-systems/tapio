package memory_leak_hunter

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// Collector implements lean memory leak detection using BaseCollector
type Collector struct {
	*base.BaseCollector // Embed BaseCollector for standard functionality
	
	config  *Config
	events  chan *domain.CollectorEvent
	ctx     context.Context
	cancel  context.CancelFunc
	mutex   sync.RWMutex

	// eBPF components (Linux-only)
	ebpfState interface{}

	logger *zap.Logger

	// Memory-specific metrics (custom metrics beyond BaseCollector)
	allocationsTracked   metric.Int64Counter
	deallocationsTracked metric.Int64Counter
	rssGrowthDetected    metric.Int64Counter
	unfreedMemoryBytes   metric.Int64Gauge
	largestAllocation    metric.Int64Gauge
	filteredEvents       metric.Int64Counter

	// Pre-processing state (lean filtering)
	recentStacks map[uint64]time.Time // Simple dedup
	stackMutex   sync.RWMutex
	lastCleanup  time.Time
}

// NewCollector creates a new memory leak hunter
func NewCollector(name string, logger *zap.Logger) (*Collector, error) {
	// Initialize logger if not provided
	if logger == nil {
		var err error
		logger, err = zap.NewProduction()
		if err != nil {
			return nil, fmt.Errorf("failed to create logger: %w", err)
		}
	}

	// Create BaseCollector with 5-minute health check timeout
	baseConfig := base.BaseCollectorConfig{
		Name:               name,
		HealthCheckTimeout: 5 * time.Minute,
		ErrorRateThreshold: 0.05, // 5% error rate threshold for memory collector (stricter than default)
	}
	baseCollector := base.NewBaseCollectorWithConfig(baseConfig)

	// Get the meter from BaseCollector for consistency
	meter := baseCollector.GetMeter()

	// Create memory-specific metrics using the same meter
	allocationsTracked, err := meter.Int64Counter(
		"memory_leak_hunter_allocations_tracked_total",
		metric.WithDescription("Total memory allocations tracked"),
	)
	if err != nil {
		logger.Warn("Failed to create allocations counter", zap.Error(err))
	}

	deallocationsTracked, err := meter.Int64Counter(
		"memory_leak_hunter_deallocations_tracked_total",
		metric.WithDescription("Total memory deallocations tracked"),
	)
	if err != nil {
		logger.Warn("Failed to create deallocations counter", zap.Error(err))
	}

	rssGrowthDetected, err := meter.Int64Counter(
		"memory_leak_hunter_rss_growth_detected_total",
		metric.WithDescription("Total RSS growth events detected"),
	)
	if err != nil {
		logger.Warn("Failed to create RSS growth counter", zap.Error(err))
	}

	unfreedMemoryBytes, err := meter.Int64Gauge(
		"memory_leak_hunter_unfreed_memory_bytes",
		metric.WithDescription("Current unfreed memory in bytes"),
	)
	if err != nil {
		logger.Warn("Failed to create unfreed memory gauge", zap.Error(err))
	}

	largestAllocation, err := meter.Int64Gauge(
		"memory_leak_hunter_largest_allocation_bytes",
		metric.WithDescription("Largest single allocation in bytes"),
	)
	if err != nil {
		logger.Warn("Failed to create largest allocation gauge", zap.Error(err))
	}

	filteredEvents, err := meter.Int64Counter(
		"memory_leak_hunter_filtered_events_total",
		metric.WithDescription("Total memory events filtered"),
	)
	if err != nil {
		logger.Warn("Failed to create filtered events counter", zap.Error(err))
	}

	// Default config
	cfg := DefaultConfig()
	cfg.Name = name

	c := &Collector{
		BaseCollector:        baseCollector, // Use BaseCollector
		config:               cfg,
		events:               make(chan *domain.CollectorEvent, cfg.BufferSize),
		logger:               logger.Named(name),
		allocationsTracked:   allocationsTracked,
		deallocationsTracked: deallocationsTracked,
		rssGrowthDetected:    rssGrowthDetected,
		unfreedMemoryBytes:   unfreedMemoryBytes,
		largestAllocation:    largestAllocation,
		filteredEvents:       filteredEvents,
		recentStacks:         make(map[uint64]time.Time),
		lastCleanup:          time.Now(),
	}

	c.logger.Info("Memory leak hunter created",
		zap.String("name", name),
		zap.Int64("min_allocation_size", cfg.MinAllocationSize),
		zap.Duration("min_unfreed_age", cfg.MinUnfreedAge),
	)

	return c, nil
}

// Start starts memory monitoring
func (c *Collector) Start(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("context cannot be nil")
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.ctx != nil {
		return fmt.Errorf("collector already started")
	}

	ctx, span := c.StartSpan(ctx, "memory_leak_hunter.start")
	defer span.End()

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Start eBPF monitoring if enabled
	if c.config.EnableEBPF {
		if err := c.startEBPF(); err != nil {
			c.RecordErrorWithContext(ctx, err) // Use BaseCollector method
			span.RecordError(err)
			c.ctx = nil
			c.cancel = nil
			return fmt.Errorf("failed to start eBPF: %w", err)
		}

		// Start event processing
		go c.readEBPFEvents()

		// Enhancement #1: Start unfreed allocations scanner
		go c.scanUnfreedAllocations()
	}

	// Start cleanup routine for dedup cache
	go c.cleanupRoutine()

	c.SetHealthy(true)
	c.logger.Info("Memory leak hunter started",
		zap.String("name", c.GetName()),
		zap.Bool("ebpf_enabled", c.config.EnableEBPF),
		zap.String("mode", string(c.config.Mode)),
	)

	return nil
}

// Stop stops the collector
func (c *Collector) Stop() error {
	_, span := c.StartSpan(context.Background(), "memory_leak_hunter.stop")
	defer span.End()

	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.ctx == nil {
		return nil
	}

	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}

	// Stop eBPF if running
	c.stopEBPF()

	if c.events != nil {
		close(c.events)
		c.events = nil
	}

	c.ctx = nil
	c.SetHealthy(false)
	c.logger.Info("Memory leak hunter stopped")
	return nil
}

// Events returns the event channel
func (c *Collector) Events() <-chan *domain.CollectorEvent {
	return c.events
}

// Name returns collector name (required by Collector interface)
func (c *Collector) Name() string {
	return c.GetName()
}

// shouldEmitEvent implements lean pre-processing logic
func (c *Collector) shouldEmitEvent(event *MemoryEvent) bool {
	// Age filtering
	if event.EventType == EventTypeUnfreed {
		age := time.Since(time.Unix(0, int64(event.Timestamp)))
		if age < c.config.MinUnfreedAge {
			if c.filteredEvents != nil {
				c.filteredEvents.Add(c.ctx, 1, metric.WithAttributes(
					attribute.String("event_type", event.EventType.String()),
					attribute.String("reason", "age_too_young"),
				))
			}
			return false
		}
	}

	// Size filtering
	if event.Size < uint64(c.config.MinAllocationSize) {
		if c.filteredEvents != nil {
			c.filteredEvents.Add(c.ctx, 1, metric.WithAttributes(
				attribute.String("event_type", event.EventType.String()),
				attribute.String("reason", "size_too_small"),
			))
		}
		return false
	}

	// Enhancement #8: Improved stack deduplication with composite key
	if event.CallerIP != 0 {
		c.stackMutex.Lock()
		defer c.stackMutex.Unlock()

		// Use composite key combining CallerIP, PID, and Address for better deduplication
		stackKey := uint64(event.CallerIP) ^ uint64(event.PID)<<32 ^ event.Address

		if lastSeen, exists := c.recentStacks[stackKey]; exists {
			if time.Since(lastSeen) < c.config.StackDedupWindow {
				if c.filteredEvents != nil {
					c.filteredEvents.Add(c.ctx, 1, metric.WithAttributes(
						attribute.String("event_type", event.EventType.String()),
						attribute.String("reason", "stack_deduplicated"),
					))
				}
				return false // Already reported this stack recently
			}
		}
		c.recentStacks[stackKey] = time.Now()
	}

	return true
}

// createDomainEvent converts memory event to domain event
// Enhancement #9: Fixed context propagation - now accepts parent context
func (c *Collector) createDomainEvent(ctx context.Context, event *MemoryEvent) *domain.CollectorEvent {
	ctx, span := c.StartSpan(ctx, "memory_leak_hunter.create_event")
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
		"caller_ip":  fmt.Sprintf("0x%x", event.CallerIP),
		"rss_pages":  fmt.Sprintf("%d", event.RSSPages),
		"rss_growth": fmt.Sprintf("%d", event.RSSGrowth),
	}

	return &domain.CollectorEvent{
		EventID:   eventID,
		Timestamp: time.Unix(0, int64(event.Timestamp)),
		Type:      eventType,
		Source:    c.GetName(),
		Severity:  c.determineSeverity(event),

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
func (c *Collector) cleanupRoutine() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.stackMutex.Lock()
			now := time.Now()
			for stack, lastSeen := range c.recentStacks {
				if now.Sub(lastSeen) > c.config.StackDedupWindow {
					delete(c.recentStacks, stack)
				}
			}
			c.stackMutex.Unlock()
		}
	}
}

// scanUnfreedAllocations is platform-specific - implemented in collector_ebpf.go for Linux

// determineSeverity determines event severity based on event characteristics
func (c *Collector) determineSeverity(event *MemoryEvent) domain.EventSeverity {
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
func (c *Collector) UpdateBufferSize(size int) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.ctx != nil {
		return fmt.Errorf("cannot update buffer size while running")
	}
	c.config.BufferSize = size
	c.events = make(chan *domain.CollectorEvent, size)
	return nil
}
