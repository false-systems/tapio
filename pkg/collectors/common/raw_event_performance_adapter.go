package common

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain/performance"
)

// RawEventPerformanceConfig holds configuration for the raw event performance adapter
type RawEventPerformanceConfig struct {
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

// DefaultRawEventPerformanceConfig returns optimized configuration for a collector
func DefaultRawEventPerformanceConfig(collectorName string) RawEventPerformanceConfig {
	return RawEventPerformanceConfig{
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

// RawEventPerformanceAdapter provides high-performance event processing for raw event collectors
type RawEventPerformanceAdapter struct {
	config     RawEventPerformanceConfig
	buffer     *performance.RingBuffer
	eventPool  sync.Pool // Pool for RawEvent objects
	bytePool   *performance.ByteSlicePool
	outputChan chan collectors.RawEvent

	// Metrics
	eventsProcessed  atomic.Uint64
	eventsDropped    atomic.Uint64
	batchesProcessed atomic.Uint64
	poolAllocated    atomic.Uint64
	poolRecycled     atomic.Uint64

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.RWMutex

	started atomic.Bool
	stopped atomic.Bool
}

// NewRawEventPerformanceAdapter creates a new performance adapter for RawEvent
func NewRawEventPerformanceAdapter(config RawEventPerformanceConfig) (*RawEventPerformanceAdapter, error) {
	// Ensure buffer size is power of 2
	if config.BufferSize&(config.BufferSize-1) != 0 {
		return nil, fmt.Errorf("buffer size must be power of 2, got %d", config.BufferSize)
	}

	// Create buffer
	buffer, err := performance.NewRingBuffer(config.BufferSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create ring buffer: %w", err)
	}

	adapter := &RawEventPerformanceAdapter{
		config:     config,
		buffer:     buffer,
		bytePool:   performance.NewByteSlicePool(),
		outputChan: make(chan collectors.RawEvent, config.BufferSize),
		eventPool: sync.Pool{
			New: func() interface{} {
				return &collectors.RawEvent{
					Metadata: make(map[string]string),
				}
			},
		},
	}

	return adapter, nil
}

// Start begins the performance adapter
func (a *RawEventPerformanceAdapter) Start() error {
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
func (a *RawEventPerformanceAdapter) Stop() error {
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
func (a *RawEventPerformanceAdapter) Submit(event *collectors.RawEvent) error {
	if !a.started.Load() || a.stopped.Load() {
		return fmt.Errorf("adapter not running")
	}

	// Serialize event for ring buffer
	data := a.serializeEvent(event)

	// Try to put in buffer
	if err := a.buffer.Put(unsafe.Pointer(&data)); err != nil {
		a.eventsDropped.Add(1)
		// Return byte slice to pool
		if a.config.EnableZeroCopy {
			a.bytePool.Put(data)
		}
		return fmt.Errorf("buffer full: %w", err)
	}

	return nil
}

// Events returns the output channel for processed events
func (a *RawEventPerformanceAdapter) Events() <-chan collectors.RawEvent {
	return a.outputChan
}

// GetMetrics returns current performance metrics
func (a *RawEventPerformanceAdapter) GetMetrics() PerformanceMetrics {
	return PerformanceMetrics{
		EventsProcessed:   a.eventsProcessed.Load(),
		EventsDropped:     a.eventsDropped.Load(),
		BatchesProcessed:  a.batchesProcessed.Load(),
		BufferSize:        uint64(a.buffer.Size()),
		BufferCapacity:    a.config.BufferSize,
		BufferUtilization: float64(a.buffer.Size()) / float64(a.config.BufferSize),
		PoolAllocated:     a.poolAllocated.Load(),
		PoolRecycled:      a.poolRecycled.Load(),
		PoolInUse:         a.poolAllocated.Load() - a.poolRecycled.Load(),
	}
}

// processEvents is the main event processing loop
func (a *RawEventPerformanceAdapter) processEvents() {
	defer a.wg.Done()

	ticker := time.NewTicker(a.config.BatchTimeout)
	defer ticker.Stop()

	batch := make([][]byte, 0, a.config.BatchSize)

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
			// Try to get event from buffer
			ptr, err := a.buffer.Get()
			if err != nil {
				time.Sleep(1 * time.Millisecond)
				continue
			}

			// Convert pointer back to byte slice
			if ptr != nil {
				data := *(*[]byte)(ptr)
				batch = append(batch, data)
			}

			// Process if batch is full
			if len(batch) >= a.config.BatchSize {
				a.flushBatch(batch)
				batch = batch[:0]
			}
		}
	}
}

// flushBatch sends a batch of events to the output channel
func (a *RawEventPerformanceAdapter) flushBatch(batch [][]byte) {
	if len(batch) == 0 {
		return
	}

	for _, data := range batch {
		if data == nil {
			continue
		}

		// Deserialize event
		event := a.deserializeEvent(data)
		if event == nil {
			continue
		}

		select {
		case a.outputChan <- *event:
			a.eventsProcessed.Add(1)

			// Return event to pool if zero-copy enabled
			if a.config.EnableZeroCopy {
				a.poolRecycled.Add(1)
				// Clear the event data before returning to pool
				event.Data = nil
				event.Metadata = make(map[string]string)
				a.eventPool.Put(event)
			}

		case <-a.ctx.Done():
			return

		default:
			// Output channel full
			a.eventsDropped.Add(1)
		}

		// Return byte slice to pool
		if a.config.EnableZeroCopy {
			a.bytePool.Put(data)
		}
	}

	a.batchesProcessed.Add(1)
}

// collectMetrics periodically collects performance metrics
func (a *RawEventPerformanceAdapter) collectMetrics() {
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

// serializeEvent converts RawEvent to bytes for ring buffer storage
func (a *RawEventPerformanceAdapter) serializeEvent(event *collectors.RawEvent) []byte {
	// Simple serialization: timestamp(8) + type_len(2) + type + data_len(4) + data + metadata_len(2) + metadata + traceid_len(1) + traceid + spanid_len(1) + spanid

	// Calculate total size
	typeLen := len(event.Type)
	dataLen := len(event.Data)
	metadataSize := 0
	for k, v := range event.Metadata {
		metadataSize += len(k) + len(v) + 2 // 2 bytes for lengths
	}
	traceLen := len(event.TraceID)
	spanLen := len(event.SpanID)

	totalSize := 8 + 2 + typeLen + 4 + dataLen + 2 + metadataSize + 1 + traceLen + 1 + spanLen

	// Get buffer from pool
	buf := a.bytePool.Get(totalSize)

	offset := 0

	// Write timestamp
	ts := event.Timestamp.UnixNano()
	buf[offset] = byte(ts)
	buf[offset+1] = byte(ts >> 8)
	buf[offset+2] = byte(ts >> 16)
	buf[offset+3] = byte(ts >> 24)
	buf[offset+4] = byte(ts >> 32)
	buf[offset+5] = byte(ts >> 40)
	buf[offset+6] = byte(ts >> 48)
	buf[offset+7] = byte(ts >> 56)
	offset += 8

	// Write type
	buf[offset] = byte(typeLen)
	buf[offset+1] = byte(typeLen >> 8)
	offset += 2
	copy(buf[offset:], event.Type)
	offset += typeLen

	// Write data
	buf[offset] = byte(dataLen)
	buf[offset+1] = byte(dataLen >> 8)
	buf[offset+2] = byte(dataLen >> 16)
	buf[offset+3] = byte(dataLen >> 24)
	offset += 4
	copy(buf[offset:], event.Data)
	offset += dataLen

	// Write metadata count
	metadataCount := len(event.Metadata)
	buf[offset] = byte(metadataCount)
	buf[offset+1] = byte(metadataCount >> 8)
	offset += 2

	// Write metadata entries
	for k, v := range event.Metadata {
		kLen := len(k)
		vLen := len(v)
		buf[offset] = byte(kLen)
		offset++
		copy(buf[offset:], k)
		offset += kLen
		buf[offset] = byte(vLen)
		offset++
		copy(buf[offset:], v)
		offset += vLen
	}

	// Write trace ID
	buf[offset] = byte(traceLen)
	offset++
	if traceLen > 0 {
		copy(buf[offset:], event.TraceID)
		offset += traceLen
	}

	// Write span ID
	buf[offset] = byte(spanLen)
	offset++
	if spanLen > 0 {
		copy(buf[offset:], event.SpanID)
	}

	return buf[:totalSize]
}

// deserializeEvent converts bytes back to RawEvent
func (a *RawEventPerformanceAdapter) deserializeEvent(data []byte) *collectors.RawEvent {
	if len(data) < 8 {
		return nil
	}

	// Get event from pool
	var event *collectors.RawEvent
	if a.config.EnableZeroCopy {
		event = a.eventPool.Get().(*collectors.RawEvent)
		a.poolAllocated.Add(1)
	} else {
		event = &collectors.RawEvent{
			Metadata: make(map[string]string),
		}
	}

	offset := 0

	// Read timestamp
	ts := int64(data[offset]) |
		int64(data[offset+1])<<8 |
		int64(data[offset+2])<<16 |
		int64(data[offset+3])<<24 |
		int64(data[offset+4])<<32 |
		int64(data[offset+5])<<40 |
		int64(data[offset+6])<<48 |
		int64(data[offset+7])<<56
	event.Timestamp = time.Unix(0, ts)
	offset += 8

	// Read type
	typeLen := int(data[offset]) | int(data[offset+1])<<8
	offset += 2
	event.Type = string(data[offset : offset+typeLen])
	offset += typeLen

	// Read data
	dataLen := int(data[offset]) |
		int(data[offset+1])<<8 |
		int(data[offset+2])<<16 |
		int(data[offset+3])<<24
	offset += 4
	event.Data = make([]byte, dataLen)
	copy(event.Data, data[offset:offset+dataLen])
	offset += dataLen

	// Read metadata
	metadataCount := int(data[offset]) | int(data[offset+1])<<8
	offset += 2

	for i := 0; i < metadataCount; i++ {
		kLen := int(data[offset])
		offset++
		key := string(data[offset : offset+kLen])
		offset += kLen

		vLen := int(data[offset])
		offset++
		value := string(data[offset : offset+vLen])
		offset += vLen

		event.Metadata[key] = value
	}

	// Read trace ID
	traceLen := int(data[offset])
	offset++
	if traceLen > 0 {
		event.TraceID = string(data[offset : offset+traceLen])
		offset += traceLen
	}

	// Read span ID
	spanLen := int(data[offset])
	offset++
	if spanLen > 0 {
		event.SpanID = string(data[offset : offset+spanLen])
	}

	return event
}
