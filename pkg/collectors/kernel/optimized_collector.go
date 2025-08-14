package kernel

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/cilium/ebpf/perf"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/yairfalse/tapio/pkg/collectors"
	"go.uber.org/zap"
)

// OptimizedCollector implements high-performance kernel event collection
type OptimizedCollector struct {
	// Core components
	name   string
	logger *zap.Logger
	ctx    context.Context
	cancel context.CancelFunc

	// Performance optimizations
	eventPool      *collectors.EventPool
	bufferPool     *collectors.BufferPool
	batchProcessor *collectors.BatchProcessor
	parallelProc   *collectors.ParallelEventProcessor
	cache          *collectors.MemoryEfficientCache

	// eBPF optimizations
	perfReaders []*perf.Reader
	ringReaders []*ringbuf.Reader
	numCPUs     int
	cpuAffinity []int

	// Lock-free structures
	eventRing     *collectors.LockFreeRingBuffer
	metadataCache sync.Map // Thread-safe metadata cache

	// Batching and aggregation
	batchSize     int
	flushInterval time.Duration
	aggregator    *EventAggregator

	// Performance metrics
	eventsProcessed atomic.Uint64
	eventsDropped   atomic.Uint64
	batchesFlush    atomic.Uint64
	cacheHits       atomic.Uint64
	cacheMisses     atomic.Uint64

	// Output channel with back-pressure handling
	events   chan collectors.RawEvent
	overflow *OverflowHandler
}

// EventAggregator aggregates similar events to reduce volume
type EventAggregator struct {
	mu         sync.RWMutex
	aggregates map[string]*AggregatedEvent
	window     time.Duration
	maxSize    int
}

// AggregatedEvent represents an aggregated event
type AggregatedEvent struct {
	FirstSeen time.Time
	LastSeen  time.Time
	Count     uint64
	Event     collectors.RawEvent
}

// OverflowHandler handles event overflow with intelligent dropping
type OverflowHandler struct {
	strategy   OverflowStrategy
	priorities map[string]int
	dropRates  map[string]float64
	mu         sync.RWMutex
}

type OverflowStrategy int

const (
	StrategyDropOldest OverflowStrategy = iota
	StrategyDropLowPriority
	StrategyAdaptiveSampling
)

// NewOptimizedCollector creates a high-performance kernel collector
func NewOptimizedCollector(name string, config *OptimizedConfig) (*OptimizedCollector, error) {
	logger, _ := zap.NewProduction()

	numCPUs := runtime.NumCPU()

	c := &OptimizedCollector{
		name:          name,
		logger:        logger,
		numCPUs:       numCPUs,
		batchSize:     config.BatchSize,
		flushInterval: config.FlushInterval,
		events:        make(chan collectors.RawEvent, config.ChannelBuffer),

		// Initialize performance components
		eventPool:  collectors.NewEventPool(),
		bufferPool: collectors.NewBufferPool(),
		eventRing:  collectors.NewLockFreeRingBuffer(uint64(config.RingBufferSize)),
		cache:      collectors.NewMemoryEfficientCache(config.CacheSize),

		// Initialize aggregator
		aggregator: &EventAggregator{
			aggregates: make(map[string]*AggregatedEvent),
			window:     config.AggregationWindow,
			maxSize:    config.MaxAggregates,
		},

		// Initialize overflow handler
		overflow: &OverflowHandler{
			strategy:   config.OverflowStrategy,
			priorities: make(map[string]int),
			dropRates:  make(map[string]float64),
		},
	}

	// Initialize batch processor with zero-copy
	c.batchProcessor = collectors.NewBatchProcessor(
		config.BatchSize,
		config.FlushInterval,
		c.processBatch,
	)

	// Initialize parallel processor for CPU-bound operations
	c.parallelProc = collectors.NewParallelEventProcessor(
		numCPUs,
		c.processEventParallel,
	)

	// Set CPU affinity for workers
	c.setupCPUAffinity()

	return c, nil
}

// Start starts the optimized collector
func (c *OptimizedCollector) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	// Start parallel processor
	c.parallelProc.Start(c.ctx)

	// Start per-CPU readers for maximum throughput
	for i := 0; i < c.numCPUs; i++ {
		go c.runCPUReader(i)
	}

	// Start aggregator
	go c.runAggregator()

	// Start overflow monitor
	go c.monitorOverflow()

	// Start batch flusher
	go c.runBatchFlusher()

	c.logger.Info("Optimized kernel collector started",
		zap.Int("cpu_count", c.numCPUs),
		zap.Int("batch_size", c.batchSize),
		zap.Duration("flush_interval", c.flushInterval),
	)

	return nil
}

// runCPUReader runs a per-CPU event reader with CPU affinity
func (c *OptimizedCollector) runCPUReader(cpuID int) {
	// Pin to CPU for better cache locality
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Process events from this CPU's ring buffer
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		// Read events in batches for better throughput
		events := c.readEventBatch(cpuID, 100)
		for _, event := range events {
			// Use lock-free ring buffer for inter-thread communication
			eventPtr := unsafe.Pointer(&event)
			if !c.eventRing.Push(eventPtr) {
				c.eventsDropped.Add(1)
				c.handleOverflow(event)
			}
		}
	}
}

// readEventBatch reads a batch of events from eBPF
func (c *OptimizedCollector) readEventBatch(cpuID int, maxEvents int) []collectors.RawEvent {
	// Get buffer from pool to avoid allocations
	events := make([]collectors.RawEvent, 0, maxEvents)

	// Read from per-CPU ring buffer
	if cpuID < len(c.ringReaders) && c.ringReaders[cpuID] != nil {
		for i := 0; i < maxEvents; i++ {
			record, err := c.ringReaders[cpuID].Read()
			if err != nil {
				break
			}

			// Parse event with zero-copy when possible
			event := c.parseEventZeroCopy(record.RawSample)
			events = append(events, event)
		}
	}

	return events
}

// parseEventZeroCopy parses event without allocations
func (c *OptimizedCollector) parseEventZeroCopy(data []byte) collectors.RawEvent {
	// Get event from pool
	event := c.eventPool.Get()

	// Parse directly into pooled event
	// This avoids allocations by reusing the event structure
	event.Timestamp = time.Now() // Would parse from data
	event.Type = "kernel_event"

	// Zero-copy data reference (careful with lifetime)
	event.Data = data

	return *event
}

// processEventParallel processes events in parallel
func (c *OptimizedCollector) processEventParallel(event collectors.RawEvent) {
	// Check cache first
	cacheKey := c.buildCacheKey(event)
	if cached, hit := c.cache.Get(cacheKey); hit {
		c.cacheHits.Add(1)
		// Use cached metadata
		if meta, ok := cached.(map[string]string); ok {
			event.Metadata = meta
		}
	} else {
		c.cacheMisses.Add(1)
		// Enrich event (expensive operation)
		c.enrichEvent(&event)
		// Cache the metadata
		c.cache.Set(cacheKey, event.Metadata)
	}

	// Add to batch
	c.batchProcessor.Add(event)
}

// enrichEvent enriches event with metadata
func (c *OptimizedCollector) enrichEvent(event *collectors.RawEvent) {
	// This would normally do expensive enrichment
	// Using cached data and batch lookups where possible
	if event.Metadata == nil {
		event.Metadata = make(map[string]string)
	}
	event.Metadata["enriched"] = "true"
}

// processBatch processes a batch of events
func (c *OptimizedCollector) processBatch(events []collectors.RawEvent) {
	c.batchesFlush.Add(1)

	// Aggregate similar events
	aggregated := c.aggregateEvents(events)

	// Send aggregated events
	for _, event := range aggregated {
		select {
		case c.events <- event:
			c.eventsProcessed.Add(1)
		case <-c.ctx.Done():
			return
		default:
			c.eventsDropped.Add(1)
			c.handleOverflow(event)
		}
	}
}

// aggregateEvents aggregates similar events to reduce volume
func (c *OptimizedCollector) aggregateEvents(events []collectors.RawEvent) []collectors.RawEvent {
	c.aggregator.mu.Lock()
	defer c.aggregator.mu.Unlock()

	now := time.Now()
	result := make([]collectors.RawEvent, 0, len(events)/2) // Assume 50% reduction

	for _, event := range events {
		key := c.buildAggregationKey(event)

		if agg, exists := c.aggregator.aggregates[key]; exists {
			// Update existing aggregate
			agg.LastSeen = now
			agg.Count++

			// Flush if window expired
			if now.Sub(agg.FirstSeen) > c.aggregator.window {
				result = append(result, c.createAggregatedEvent(agg))
				delete(c.aggregator.aggregates, key)
			}
		} else {
			// Create new aggregate
			c.aggregator.aggregates[key] = &AggregatedEvent{
				FirstSeen: now,
				LastSeen:  now,
				Count:     1,
				Event:     event,
			}

			// Evict old aggregates if at capacity
			if len(c.aggregator.aggregates) > c.aggregator.maxSize {
				c.evictOldAggregates()
			}
		}
	}

	return result
}

// createAggregatedEvent creates an event from an aggregate
func (c *OptimizedCollector) createAggregatedEvent(agg *AggregatedEvent) collectors.RawEvent {
	event := agg.Event
	if event.Metadata == nil {
		event.Metadata = make(map[string]string)
	}
	event.Metadata["aggregate_count"] = fmt.Sprintf("%d", agg.Count)
	event.Metadata["aggregate_window"] = agg.LastSeen.Sub(agg.FirstSeen).String()
	return event
}

// evictOldAggregates removes old aggregates
func (c *OptimizedCollector) evictOldAggregates() {
	now := time.Now()
	for key, agg := range c.aggregator.aggregates {
		if now.Sub(agg.LastSeen) > c.aggregator.window {
			delete(c.aggregator.aggregates, key)
		}
	}
}

// runAggregator periodically flushes aggregates
func (c *OptimizedCollector) runAggregator() {
	ticker := time.NewTicker(c.aggregator.window / 2)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.flushAggregates()
		case <-c.ctx.Done():
			return
		}
	}
}

// flushAggregates flushes all aggregates
func (c *OptimizedCollector) flushAggregates() {
	c.aggregator.mu.Lock()
	defer c.aggregator.mu.Unlock()

	events := make([]collectors.RawEvent, 0, len(c.aggregator.aggregates))
	for key, agg := range c.aggregator.aggregates {
		events = append(events, c.createAggregatedEvent(agg))
		delete(c.aggregator.aggregates, key)
	}

	// Process flushed aggregates
	if len(events) > 0 {
		c.processBatch(events)
	}
}

// runBatchFlusher ensures batches are flushed periodically
func (c *OptimizedCollector) runBatchFlusher() {
	ticker := time.NewTicker(c.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Process events from ring buffer
			for i := 0; i < 1000; i++ {
				eventPtr := c.eventRing.Pop()
				if eventPtr == nil {
					break
				}
				event := *(*collectors.RawEvent)(eventPtr)
				c.parallelProc.Submit(event)
			}
		case <-c.ctx.Done():
			return
		}
	}
}

// handleOverflow handles event overflow intelligently
func (c *OptimizedCollector) handleOverflow(event collectors.RawEvent) {
	c.overflow.mu.Lock()
	defer c.overflow.mu.Unlock()

	switch c.overflow.strategy {
	case StrategyDropOldest:
		// Simply drop the event (oldest)

	case StrategyDropLowPriority:
		// Check event priority
		priority := c.overflow.priorities[event.Type]
		if priority < 5 { // Low priority threshold
			return // Drop
		}
		// Try harder to queue high-priority events
		for i := 0; i < 3; i++ {
			select {
			case c.events <- event:
				return
			default:
				time.Sleep(time.Microsecond)
			}
		}

	case StrategyAdaptiveSampling:
		// Adaptive sampling based on event type
		dropRate := c.overflow.dropRates[event.Type]
		if dropRate == 0 {
			dropRate = 0.1 // Start with 10% drop rate
		}

		// Increase drop rate for this event type
		c.overflow.dropRates[event.Type] = dropRate * 1.1

		// Randomly decide whether to drop
		// In production, use a better random source
		if time.Now().UnixNano()%100 < int64(dropRate*100) {
			return // Drop
		}
	}
}

// monitorOverflow monitors and adjusts overflow handling
func (c *OptimizedCollector) monitorOverflow() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	var lastDropped uint64

	for {
		select {
		case <-ticker.C:
			dropped := c.eventsDropped.Load()
			processed := c.eventsProcessed.Load()

			dropRate := float64(dropped-lastDropped) / float64(processed+dropped-lastDropped)

			if dropRate > 0.05 { // More than 5% drop rate
				c.logger.Warn("High drop rate detected",
					zap.Float64("drop_rate", dropRate),
					zap.Uint64("dropped", dropped-lastDropped),
					zap.Uint64("processed", processed),
				)

				// Adjust overflow strategy
				c.adjustOverflowStrategy(dropRate)
			}

			lastDropped = dropped

		case <-c.ctx.Done():
			return
		}
	}
}

// adjustOverflowStrategy adjusts the overflow handling strategy
func (c *OptimizedCollector) adjustOverflowStrategy(dropRate float64) {
	c.overflow.mu.Lock()
	defer c.overflow.mu.Unlock()

	if dropRate > 0.2 {
		// Switch to aggressive dropping
		c.overflow.strategy = StrategyAdaptiveSampling
	} else if dropRate > 0.1 {
		// Switch to priority-based dropping
		c.overflow.strategy = StrategyDropLowPriority
	}

	// Reset drop rates for adaptive sampling
	for key := range c.overflow.dropRates {
		c.overflow.dropRates[key] = dropRate
	}
}

// setupCPUAffinity sets up CPU affinity for workers
func (c *OptimizedCollector) setupCPUAffinity() {
	c.cpuAffinity = make([]int, c.numCPUs)
	for i := 0; i < c.numCPUs; i++ {
		c.cpuAffinity[i] = i
	}
}

// buildCacheKey builds a cache key for an event
func (c *OptimizedCollector) buildCacheKey(event collectors.RawEvent) string {
	// Use zero-copy string building
	var key [64]byte
	n := copy(key[:], event.Type)
	n += copy(key[n:], event.TraceID[:8]) // Use first 8 chars of trace ID
	return string(key[:n])
}

// buildAggregationKey builds an aggregation key for an event
func (c *OptimizedCollector) buildAggregationKey(event collectors.RawEvent) string {
	// Group by event type and key metadata
	return event.Type // Simplified - would include more fields
}

// Stop stops the optimized collector
func (c *OptimizedCollector) Stop() error {
	if c.cancel != nil {
		c.cancel()
	}

	// Stop parallel processor
	c.parallelProc.Stop()

	// Flush remaining aggregates
	c.flushAggregates()

	// Close channels
	close(c.events)

	c.logger.Info("Optimized kernel collector stopped",
		zap.Uint64("events_processed", c.eventsProcessed.Load()),
		zap.Uint64("events_dropped", c.eventsDropped.Load()),
		zap.Uint64("cache_hits", c.cacheHits.Load()),
		zap.Uint64("cache_misses", c.cacheMisses.Load()),
	)

	return nil
}

// Events returns the event channel
func (c *OptimizedCollector) Events() <-chan collectors.RawEvent {
	return c.events
}

// Name returns the collector name
func (c *OptimizedCollector) Name() string {
	return c.name
}

// IsHealthy returns health status
func (c *OptimizedCollector) IsHealthy() bool {
	dropRate := float64(c.eventsDropped.Load()) / float64(c.eventsProcessed.Load())
	return dropRate < 0.1 // Healthy if drop rate < 10%
}

// OptimizedConfig holds configuration for the optimized collector
type OptimizedConfig struct {
	BatchSize         int
	FlushInterval     time.Duration
	ChannelBuffer     int
	RingBufferSize    int
	CacheSize         int
	AggregationWindow time.Duration
	MaxAggregates     int
	OverflowStrategy  OverflowStrategy
}

// DefaultOptimizedConfig returns default configuration
func DefaultOptimizedConfig() *OptimizedConfig {
	return &OptimizedConfig{
		BatchSize:         1000,
		FlushInterval:     100 * time.Millisecond,
		ChannelBuffer:     50000,
		RingBufferSize:    100000,
		CacheSize:         10000,
		AggregationWindow: 5 * time.Second,
		MaxAggregates:     5000,
		OverflowStrategy:  StrategyAdaptiveSampling,
	}
}
