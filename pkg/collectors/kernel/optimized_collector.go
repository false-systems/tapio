//go:build linux
// +build linux

package kernel

import (
	"context"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 productionKernelMonitor ./bpf_src/kernel_monitor_production.c -- -I../bpf_common

// OptimizedKernelEvent represents the optimized kernel event from eBPF (64 bytes)
type OptimizedKernelEvent struct {
	Timestamp uint64
	CgroupID  uint64
	PID       uint32
	EventType uint32
	Comm      [16]byte
	Data      [24]byte // Union data - network, file, or raw
	Flags     uint32
}

// OptimizedCollector implements high-performance kernel monitoring via eBPF
type OptimizedCollector struct {
	name    string
	logger  *zap.Logger
	events  chan domain.RawEvent
	ctx     context.Context
	cancel  context.CancelFunc
	healthy bool
	mu      sync.RWMutex
	config  *Config

	// eBPF components (Linux-specific)
	objs   *productionKernelMonitorObjects
	links  []link.Link
	reader *ringbuf.Reader

	// Performance optimization
	eventPool sync.Pool // Reuse event structures
	batchSize int       // Process events in batches

	// OTEL instrumentation
	tracer          trace.Tracer
	eventsProcessed metric.Int64Counter
	errorsTotal     metric.Int64Counter
	processingTime  metric.Float64Histogram
	eventsDropped   metric.Int64Counter
	ebpfOperations  metric.Int64Counter
	cacheHitRatio   metric.Float64Gauge
}

// NewOptimizedCollector creates a new optimized kernel collector
func NewOptimizedCollector(name string) (*OptimizedCollector, error) {
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}
	config := &Config{Name: name}
	return NewOptimizedCollectorWithConfig(config, logger)
}

// NewOptimizedCollectorWithConfig creates an optimized collector with config
func NewOptimizedCollectorWithConfig(config *Config, logger *zap.Logger) (*OptimizedCollector, error) {
	if logger == nil {
		var err error
		logger, err = zap.NewProduction()
		if err != nil {
			return nil, fmt.Errorf("failed to create logger: %w", err)
		}
	}

	// Initialize OTEL components
	name := config.Name
	tracer := otel.Tracer(name)
	meter := otel.Meter(name)

	// Create metrics
	eventsProcessed, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_processed_total", name),
		metric.WithDescription(fmt.Sprintf("Total events processed by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create events counter", zap.Error(err))
	}

	errorsTotal, err := meter.Int64Counter(
		fmt.Sprintf("%s_errors_total", name),
		metric.WithDescription(fmt.Sprintf("Total errors in %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	processingTime, err := meter.Float64Histogram(
		fmt.Sprintf("%s_processing_duration_ms", name),
		metric.WithDescription(fmt.Sprintf("Processing duration for %s in milliseconds", name)),
	)
	if err != nil {
		logger.Warn("Failed to create processing time histogram", zap.Error(err))
	}

	eventsDropped, err := meter.Int64Counter(
		fmt.Sprintf("%s_events_dropped_total", name),
		metric.WithDescription(fmt.Sprintf("Total events dropped by %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create events dropped counter", zap.Error(err))
	}

	ebpfOperations, err := meter.Int64Counter(
		fmt.Sprintf("%s_ebpf_operations_total", name),
		metric.WithDescription(fmt.Sprintf("Total eBPF operations in %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create ebpf operations counter", zap.Error(err))
	}

	cacheHitRatio, err := meter.Float64Gauge(
		fmt.Sprintf("%s_cache_hit_ratio", name),
		metric.WithDescription(fmt.Sprintf("eBPF cache hit ratio for %s", name)),
	)
	if err != nil {
		logger.Warn("Failed to create cache hit ratio gauge", zap.Error(err))
	}

	c := &OptimizedCollector{
		name:            config.Name,
		logger:          logger,
		config:          config,
		events:          make(chan domain.RawEvent, 10000), // Optimized buffer size
		healthy:         true,
		batchSize:       32, // Process 32 events at a time
		tracer:          tracer,
		eventsProcessed: eventsProcessed,
		errorsTotal:     errorsTotal,
		processingTime:  processingTime,
		eventsDropped:   eventsDropped,
		ebpfOperations:  ebpfOperations,
		cacheHitRatio:   cacheHitRatio,
	}

	// Initialize event pool for zero-allocation event processing
	c.eventPool = sync.Pool{
		New: func() interface{} {
			return &OptimizedKernelEvent{}
		},
	}

	return c, nil
}

// Name returns collector name
func (c *OptimizedCollector) Name() string {
	return c.name
}

// Start starts the optimized kernel monitoring
func (c *OptimizedCollector) Start(ctx context.Context) error {
	ctx, span := c.tracer.Start(ctx, "kernel.optimized.start")
	defer span.End()

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Start optimized eBPF monitoring
	if err := c.startOptimizedEBPF(); err != nil {
		if c.errorsTotal != nil {
			c.errorsTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "ebpf_start_failed"),
			))
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("failed to start optimized eBPF: %w", err)
	}

	// Start optimized event processing loop
	go c.readOptimizedEBPFEvents()

	c.healthy = true
	span.SetStatus(codes.Ok, "Optimized kernel collector started successfully")
	c.logger.Info("Optimized kernel collector started",
		zap.String("name", c.name),
		zap.Int("batch_size", c.batchSize),
	)
	return nil
}

// Stop stops the optimized collector
func (c *OptimizedCollector) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}

	// Stop optimized eBPF
	c.stopOptimizedEBPF()

	if c.events != nil {
		close(c.events)
	}

	c.healthy = false
	c.logger.Info("Optimized kernel collector stopped")
	return nil
}

// Events returns the event channel
func (c *OptimizedCollector) Events() <-chan domain.RawEvent {
	return c.events
}

// IsHealthy returns health status
func (c *OptimizedCollector) IsHealthy() bool {
	return c.healthy
}

// startOptimizedEBPF initializes the optimized eBPF monitoring
func (c *OptimizedCollector) startOptimizedEBPF() error {
	ctx, span := c.tracer.Start(context.Background(), "kernel.ebpf.optimized.start")
	defer span.End()

	// Remove memory limit for eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("removing memory limit: %w", err)
	}

	// Load optimized eBPF programs
	objs := &productionKernelMonitorObjects{}
	if err := loadProductionKernelMonitorObjects(objs, nil); err != nil {
		return fmt.Errorf("loading optimized eBPF objects: %w", err)
	}
	c.objs = objs

	// Attach to optimized tracepoints
	// Process execution monitoring
	processLink, err := link.Tracepoint("sched", "sched_process_exec", objs.TraceExecOptimized, nil)
	if err != nil {
		objs.Close()
		return fmt.Errorf("attaching process tracepoint: %w", err)
	}
	c.links = append(c.links, processLink)

	// Network connection monitoring (TCP only for performance)
	netLink, err := link.Kprobe("tcp_v4_connect", objs.TraceTcpConnectOptimized, nil)
	if err != nil {
		c.logger.Warn("Failed to attach TCP kprobe, continuing without network monitoring", zap.Error(err))
		// Continue without network monitoring - it's optional
	} else {
		c.links = append(c.links, netLink)
	}

	// File access monitoring (filtered paths only)
	fileLink, err := link.Tracepoint("syscalls", "sys_enter_openat", objs.TraceOpenatOptimized, nil)
	if err != nil {
		c.logger.Warn("Failed to attach file tracepoint, continuing without file monitoring", zap.Error(err))
		// Continue without file monitoring - it's optional
	} else {
		c.links = append(c.links, fileLink)
	}

	// Create ring buffer reader
	reader, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		c.cleanup()
		return fmt.Errorf("creating ring buffer reader: %w", err)
	}
	c.reader = reader

	span.SetAttributes(
		attribute.Int("link_count", len(c.links)),
		attribute.Bool("optimized", true),
	)

	c.logger.Info("Optimized eBPF monitoring started successfully",
		zap.String("collector", c.name),
		zap.Int("links", len(c.links)),
	)

	return nil
}

// stopOptimizedEBPF cleans up optimized eBPF resources
func (c *OptimizedCollector) stopOptimizedEBPF() {
	c.cleanup()
}

// cleanup releases all eBPF resources
func (c *OptimizedCollector) cleanup() {
	if c.reader != nil {
		c.reader.Close()
		c.reader = nil
	}

	for _, link := range c.links {
		if err := link.Close(); err != nil {
			c.logger.Error("Failed to close eBPF link", zap.Error(err))
		}
	}
	c.links = nil

	if c.objs != nil {
		c.objs.Close()
		c.objs = nil
	}
}

// readOptimizedEBPFEvents processes eBPF events with batching and zero-copy optimization
func (c *OptimizedCollector) readOptimizedEBPFEvents() {
	ctx := c.ctx
	batch := make([]*OptimizedKernelEvent, 0, c.batchSize)

	// Performance monitoring
	lastStatsUpdate := time.Now()
	statsInterval := 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			// Process remaining batch before exiting
			if len(batch) > 0 {
				c.processBatch(batch)
			}
			return
		default:
		}

		start := time.Now()

		// Read event from ring buffer
		record, err := c.reader.Read()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if c.errorsTotal != nil {
				c.errorsTotal.Add(ctx, 1,
					metric.WithAttributes(
						attribute.String("error_type", "ringbuf_read_failed"),
					),
				)
			}
			continue
		}

		// Validate event size
		if len(record.RawSample) != int(unsafe.Sizeof(OptimizedKernelEvent{})) {
			if c.errorsTotal != nil {
				c.errorsTotal.Add(ctx, 1,
					metric.WithAttributes(
						attribute.String("error_type", "invalid_event_size"),
					),
				)
			}
			continue
		}

		// Zero-copy event parsing - cast directly to struct
		event := (*OptimizedKernelEvent)(unsafe.Pointer(&record.RawSample[0]))

		// Get event from pool and copy data
		pooledEvent := c.eventPool.Get().(*OptimizedKernelEvent)
		*pooledEvent = *event

		// Add to batch
		batch = append(batch, pooledEvent)

		// Process batch when full or periodically
		if len(batch) >= c.batchSize {
			c.processBatch(batch)
			batch = batch[:0] // Reset slice but keep capacity
		}

		// Record processing time
		if c.processingTime != nil {
			duration := time.Since(start).Seconds() * 1000 // Convert to milliseconds
			c.processingTime.Record(ctx, duration)
		}

		// Update performance stats periodically
		if time.Since(lastStatsUpdate) > statsInterval {
			c.updatePerformanceStats(ctx)
			lastStatsUpdate = time.Now()
		}
	}
}

// processBatch processes a batch of events efficiently
func (c *OptimizedCollector) processBatch(batch []*OptimizedKernelEvent) {
	ctx := c.ctx

	for _, event := range batch {
		// Convert to raw event efficiently
		rawEvent := c.convertOptimizedEvent(event)

		// Try to send event
		select {
		case c.events <- rawEvent:
			if c.eventsProcessed != nil {
				c.eventsProcessed.Add(ctx, 1,
					metric.WithAttributes(
						attribute.String("event_type", getEventTypeName(event.EventType)),
					),
				)
			}
		case <-ctx.Done():
			// Return events to pool
			for _, e := range batch {
				c.eventPool.Put(e)
			}
			return
		default:
			// Buffer full, drop event
			if c.eventsDropped != nil {
				c.eventsDropped.Add(ctx, 1,
					metric.WithAttributes(
						attribute.String("reason", "buffer_full"),
					),
				)
			}
		}

		// Return event to pool
		c.eventPool.Put(event)
	}
}

// convertOptimizedEvent converts optimized eBPF event to raw event format
func (c *OptimizedCollector) convertOptimizedEvent(event *OptimizedKernelEvent) domain.RawEvent {
	// Convert timestamp to time.Time
	timestamp := time.Unix(0, int64(event.Timestamp))

	// Use zero-copy approach - create byte slice directly from event
	eventBytes := (*[unsafe.Sizeof(*event)]byte)(unsafe.Pointer(event))[:]
	dataCopy := make([]byte, len(eventBytes))
	copy(dataCopy, eventBytes)

	return domain.RawEvent{
		Timestamp: timestamp,
		Source:    c.name,
		Data:      dataCopy,
	}
}

// updatePerformanceStats updates performance statistics from eBPF maps
func (c *OptimizedCollector) updatePerformanceStats(ctx context.Context) {
	if c.objs == nil {
		return
	}

	// Read performance counters from eBPF map
	var cacheHits, cacheLookups uint64

	// Counter indices from eBPF program
	cacheHitsKey := uint32(3)    // COUNTER_CGROUP_HITS
	cacheLookupsKey := uint32(2) // COUNTER_CGROUP_LOOKUPS

	if err := c.objs.PerfCounters.Lookup(cacheHitsKey, &cacheHits); err == nil {
		if err := c.objs.PerfCounters.Lookup(cacheLookupsKey, &cacheLookups); err == nil {
			if cacheLookups > 0 {
				hitRatio := float64(cacheHits) / float64(cacheLookups)
				if c.cacheHitRatio != nil {
					c.cacheHitRatio.Record(ctx, hitRatio)
				}
			}
		}
	}
}

// getEventTypeName converts event type to string
func getEventTypeName(eventType uint32) string {
	switch eventType {
	case 1:
		return "process_exec"
	case 2:
		return "network_conn"
	case 3:
		return "file_open"
	default:
		return "unknown"
	}
}
