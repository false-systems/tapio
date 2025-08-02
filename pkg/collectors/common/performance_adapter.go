package common

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/domain/performance"
)

// PerformanceConfig holds configuration for the performance adapter
type PerformanceConfig struct {
	CollectorName   string
	BufferSize      uint64        // Must be power of 2
	BatchSize       int           // Number of events per batch
	BatchTimeout    time.Duration // Max time to wait for batch
	EventPoolSize   int           // Object pool size for events
	BytePoolSize    int           // Object pool size for byte slices
	EnableZeroCopy  bool          // Enable zero-copy operations
	EnableBatching  bool          // Enable batch processing
	MetricsInterval time.Duration // Metrics collection interval
}

// DefaultPerformanceConfig returns optimized configuration for a collector
func DefaultPerformanceConfig(collectorName string) PerformanceConfig {
	return PerformanceConfig{
		CollectorName:   collectorName,
		BufferSize:      8192, // Power of 2
		BatchSize:       100,  // Process 100 events at a time
		BatchTimeout:    100 * time.Millisecond,
		EventPoolSize:   10000, // Large pool for high-throughput
		BytePoolSize:    5000,  // For string allocations
		EnableZeroCopy:  true,
		EnableBatching:  true,
		MetricsInterval: 30 * time.Second,
	}
}

// PerformanceMetrics tracks performance statistics
type PerformanceMetrics struct {
	EventsProcessed   uint64
	EventsDropped     uint64
	BatchesProcessed  uint64
	BufferSize        uint64
	BufferCapacity    uint64
	BufferUtilization float64
	PoolAllocated     uint64
	PoolRecycled      uint64
	PoolInUse         uint64
}

// PerformanceAdapter provides high-performance event processing for collectors
type PerformanceAdapter struct {
	config     PerformanceConfig
	buffer     *performance.EventBatchBuffer
	eventPool  *performance.UnifiedEventPool
	bytePool   *performance.ByteSlicePool
	outputChan chan domain.UnifiedEvent

	// Metrics
	eventsProcessed  atomic.Uint64
	eventsDropped    atomic.Uint64
	batchesProcessed atomic.Uint64

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.RWMutex

	started atomic.Bool
	stopped atomic.Bool
}

// NewPerformanceAdapter creates a new performance adapter
func NewPerformanceAdapter(config PerformanceConfig) (*PerformanceAdapter, error) {
	// Ensure buffer size is power of 2
	if config.BufferSize&(config.BufferSize-1) != 0 {
		return nil, fmt.Errorf("buffer size must be power of 2, got %d", config.BufferSize)
	}

	// Create pools
	eventPool := performance.NewUnifiedEventPool()
	bytePool := performance.NewByteSlicePool()

	// Create buffer
	buffer, err := performance.NewEventBatchBuffer(config.BufferSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create event buffer: %w", err)
	}

	adapter := &PerformanceAdapter{
		config:     config,
		buffer:     buffer,
		eventPool:  eventPool,
		bytePool:   bytePool,
		outputChan: make(chan domain.UnifiedEvent, config.BufferSize),
	}

	return adapter, nil
}

// Start begins the performance adapter
func (a *PerformanceAdapter) Start() error {
	if a.started.Swap(true) {
		return fmt.Errorf("already started")
	}

	a.ctx, a.cancel = context.WithCancel(context.Background())

	// Start processing goroutine
	a.wg.Add(1)
	go a.processEvents()

	// Start metrics collection if enabled
	if a.config.MetricsInterval > 0 {
		a.wg.Add(1)
		go a.collectMetrics()
	}

	return nil
}

// Stop gracefully stops the performance adapter
func (a *PerformanceAdapter) Stop() error {
	if !a.started.Load() {
		return fmt.Errorf("not started")
	}

	if a.stopped.Swap(true) {
		return nil
	}

	// Cancel context
	if a.cancel != nil {
		a.cancel()
	}

	// Wait for goroutines
	a.wg.Wait()

	// Close output channel
	close(a.outputChan)

	return nil
}

// Submit adds an event to the processing pipeline
func (a *PerformanceAdapter) Submit(event *domain.UnifiedEvent) error {
	if !a.started.Load() || a.stopped.Load() {
		return fmt.Errorf("adapter not running")
	}

	// Try to put in buffer
	if err := a.buffer.Put(event); err != nil {
		a.eventsDropped.Add(1)
		return fmt.Errorf("buffer full: %w", err)
	}

	return nil
}

// Events returns the output channel for processed events
func (a *PerformanceAdapter) Events() <-chan domain.UnifiedEvent {
	return a.outputChan
}

// GetMetrics returns current performance metrics
func (a *PerformanceAdapter) GetMetrics() PerformanceMetrics {
	stats := a.buffer.GetStats()
	poolStats := a.eventPool.GetStats()

	return PerformanceMetrics{
		EventsProcessed:   a.eventsProcessed.Load(),
		EventsDropped:     a.eventsDropped.Load(),
		BatchesProcessed:  a.batchesProcessed.Load(),
		BufferSize:        stats.Size,
		BufferCapacity:    stats.Capacity,
		BufferUtilization: float64(stats.Size) / float64(stats.Capacity),
		PoolAllocated:     uint64(poolStats.Allocated),
		PoolRecycled:      uint64(poolStats.Recycled),
		PoolInUse:         uint64(poolStats.InUse),
	}
}

// processEvents is the main event processing loop
func (a *PerformanceAdapter) processEvents() {
	defer a.wg.Done()

	ticker := time.NewTicker(a.config.BatchTimeout)
	defer ticker.Stop()

	batch := make([]*domain.UnifiedEvent, 0, a.config.BatchSize)

	for {
		select {
		case <-a.ctx.Done():
			// Process remaining events
			a.flushBatch(batch)
			return

		case <-ticker.C:
			// Timeout - process what we have
			if len(batch) > 0 {
				a.flushBatch(batch)
				batch = batch[:0]
			}

		default:
			// Try to get events from buffer
			events, err := a.buffer.GetBatch(a.config.BatchSize)
			if err != nil || len(events) == 0 {
				time.Sleep(1 * time.Millisecond)
				continue
			}

			// Add to batch
			batch = append(batch, events...)

			// Process if batch is full
			if len(batch) >= a.config.BatchSize {
				a.flushBatch(batch)
				batch = batch[:0]
			}
		}
	}
}

// flushBatch sends a batch of events to the output channel
func (a *PerformanceAdapter) flushBatch(batch []*domain.UnifiedEvent) {
	if len(batch) == 0 {
		return
	}

	for _, event := range batch {
		if event == nil {
			continue
		}

		select {
		case a.outputChan <- *event:
			a.eventsProcessed.Add(1)

			// Return event to pool if zero-copy enabled
			if a.config.EnableZeroCopy {
				a.eventPool.Put(event)
			}

		case <-a.ctx.Done():
			return

		default:
			// Output channel full
			a.eventsDropped.Add(1)
		}
	}

	a.batchesProcessed.Add(1)
}

// collectMetrics periodically collects performance metrics
func (a *PerformanceAdapter) collectMetrics() {
	defer a.wg.Done()

	ticker := time.NewTicker(a.config.MetricsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return

		case <-ticker.C:
			metrics := a.GetMetrics()
			// In production, these would be exported to monitoring system
			_ = metrics
		}
	}
}
