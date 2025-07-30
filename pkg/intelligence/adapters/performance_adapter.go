package adapters

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/interfaces"
)

// PerformanceAdapter provides high-throughput event processing with zero-copy and batching
type PerformanceAdapter struct {
	engine         interfaces.CorrelationEngine
	batchProcessor interfaces.BatchProcessor
	eventPool      sync.Pool
	inputChan      chan *interfaces.PipelineEvent
	outputChan     chan *interfaces.PipelineEvent
	batchChan      chan []*domain.UnifiedEvent
	stopChan       chan struct{}
	wg             sync.WaitGroup
	metrics        atomic.Value // *interfaces.PipelineMetrics
	batchSize      int32
	maxBatchSize   int
	flushInterval  time.Duration

	// Performance optimization fields
	ringBuffer     []interfaces.PipelineEvent
	ringBufferSize int
	writePos       uint64
	readPos        uint64

	// Zero-copy optimization
	eventBuffer  []byte
	eventOffsets []uint32
}

// NewPerformanceAdapter creates a new high-performance adapter
func NewPerformanceAdapter(engine interfaces.CorrelationEngine, opts ...PerformanceOption) *PerformanceAdapter {
	// Default configuration
	channelSize := 10000

	pa := &PerformanceAdapter{
		engine:         engine,
		stopChan:       make(chan struct{}),
		maxBatchSize:   100,
		batchSize:      50, // Default batch size
		flushInterval:  100 * time.Millisecond,
		ringBufferSize: 65536,
		eventBuffer:    make([]byte, 0, 1024*1024), // 1MB initial buffer
		eventOffsets:   make([]uint32, 0, 10000),
	}

	// Apply options before creating channels (so channel size can be configured)
	for _, opt := range opts {
		opt(pa)
	}

	// Use channelSize from options if WithChannelSize was called
	if pa.inputChan == nil {
		pa.inputChan = make(chan *interfaces.PipelineEvent, channelSize)
		pa.outputChan = make(chan *interfaces.PipelineEvent, channelSize)
	}
	pa.batchChan = make(chan []*domain.UnifiedEvent, 100)

	// Initialize ring buffer
	pa.ringBuffer = make([]interfaces.PipelineEvent, pa.ringBufferSize)

	// Initialize event pool for zero-allocation
	pa.eventPool = sync.Pool{
		New: func() interface{} {
			return &interfaces.PipelineEvent{}
		},
	}

	// Initialize metrics
	pa.metrics.Store(&interfaces.PipelineMetrics{})

	// Set initial batch size
	atomic.StoreInt32(&pa.batchSize, int32(pa.maxBatchSize))

	return pa
}

// Start initializes the performance adapter
func (pa *PerformanceAdapter) Start() error {
	if pa.engine != nil {
		if err := pa.engine.Start(); err != nil {
			return err
		}
	}

	// Start worker goroutines
	pa.wg.Add(3)
	go pa.inputWorker()
	go pa.batchWorker()
	go pa.metricsWorker()

	return nil
}

// Stop gracefully shuts down the adapter
func (pa *PerformanceAdapter) Stop() error {
	close(pa.stopChan)
	pa.wg.Wait()

	if pa.engine != nil {
		return pa.engine.Stop()
	}

	return nil
}

// Submit submits an event for processing using lock-free ring buffer
func (pa *PerformanceAdapter) Submit(event *interfaces.PipelineEvent) error {
	// Fast path: try to write to ring buffer
	for {
		writePos := atomic.LoadUint64(&pa.writePos)
		readPos := atomic.LoadUint64(&pa.readPos)

		// Check if buffer is full
		if writePos-readPos >= uint64(pa.ringBufferSize) {
			// Slow path: use channel
			select {
			case pa.inputChan <- event:
				return nil
			case <-pa.stopChan:
				return errors.New("adapter is stopped")
			default:
				// Update dropped events metric
				pa.updateDroppedEvents(1)
				return errors.New("ring buffer and channel full")
			}
		}

		// Try to claim the write position
		if atomic.CompareAndSwapUint64(&pa.writePos, writePos, writePos+1) {
			// Successfully claimed position, write event
			idx := writePos % uint64(pa.ringBufferSize)
			pa.ringBuffer[idx] = *event
			return nil
		}
		// CAS failed, retry
	}
}

// GetOutput retrieves a processed event
func (pa *PerformanceAdapter) GetOutput() (*interfaces.PipelineEvent, error) {
	select {
	case event := <-pa.outputChan:
		return event, nil
	case <-pa.stopChan:
		return nil, errors.New("adapter is stopped")
	default:
		return nil, nil
	}
}

// GetMetrics returns pipeline performance metrics
func (pa *PerformanceAdapter) GetMetrics() *interfaces.PipelineMetrics {
	return pa.metrics.Load().(*interfaces.PipelineMetrics)
}

// GetEvent gets an event from the object pool (zero allocation)
func (pa *PerformanceAdapter) GetEvent() *interfaces.PipelineEvent {
	return pa.eventPool.Get().(*interfaces.PipelineEvent)
}

// PutEvent returns an event to the object pool
func (pa *PerformanceAdapter) PutEvent(event *interfaces.PipelineEvent) {
	// Reset event to avoid memory leaks
	event.ID = 0
	event.Type = ""
	event.Timestamp = 0
	event.Priority = 0
	for i := range event.Metadata {
		event.Metadata[i] = 0
	}
	pa.eventPool.Put(event)
}

// ProcessBatch processes multiple events in a batch
func (pa *PerformanceAdapter) ProcessBatch(ctx context.Context, events []*domain.UnifiedEvent) error {
	if pa.batchProcessor != nil {
		return pa.batchProcessor.ProcessBatch(ctx, events)
	}

	// Fallback to individual processing
	for _, event := range events {
		if err := pa.engine.ProcessEvent(ctx, event); err != nil {
			return err
		}
	}

	return nil
}

// GetBatchSize returns the current batch size
func (pa *PerformanceAdapter) GetBatchSize() int {
	return int(atomic.LoadInt32(&pa.batchSize))
}

// SetBatchSize sets the batch size
func (pa *PerformanceAdapter) SetBatchSize(size int) {
	if size > 0 && size <= pa.maxBatchSize {
		atomic.StoreInt32(&pa.batchSize, int32(size))
	}
}

// inputWorker processes events from the ring buffer
func (pa *PerformanceAdapter) inputWorker() {
	defer pa.wg.Done()

	batch := make([]*domain.UnifiedEvent, 0, pa.maxBatchSize)
	ticker := time.NewTicker(pa.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-pa.stopChan:
			// Flush remaining batch
			if len(batch) > 0 {
				pa.sendBatch(batch)
			}
			return

		case <-ticker.C:
			// Periodic flush
			if len(batch) > 0 {
				pa.sendBatch(batch)
				batch = batch[:0]
			}

		default:
			// Try to read from ring buffer
			readPos := atomic.LoadUint64(&pa.readPos)
			writePos := atomic.LoadUint64(&pa.writePos)

			if readPos < writePos {
				// Read event from ring buffer
				idx := readPos % uint64(pa.ringBufferSize)
				event := &pa.ringBuffer[idx]

				// Convert PipelineEvent to UnifiedEvent (zero-copy where possible)
				unifiedEvent := pa.convertToUnifiedEvent(event)
				batch = append(batch, unifiedEvent)

				// Update read position
				atomic.AddUint64(&pa.readPos, 1)

				// Check if batch is full
				if len(batch) >= int(atomic.LoadInt32(&pa.batchSize)) {
					pa.sendBatch(batch)
					batch = batch[:0]
				}
			} else {
				// No events in ring buffer, check channel
				select {
				case event := <-pa.inputChan:
					unifiedEvent := pa.convertToUnifiedEvent(event)
					batch = append(batch, unifiedEvent)

					if len(batch) >= int(atomic.LoadInt32(&pa.batchSize)) {
						pa.sendBatch(batch)
						batch = batch[:0]
					}
				default:
					// No events available, continue
					time.Sleep(time.Microsecond)
				}
			}
		}
	}
}

// batchWorker processes batches of events
func (pa *PerformanceAdapter) batchWorker() {
	defer pa.wg.Done()

	for {
		select {
		case <-pa.stopChan:
			return

		case batch := <-pa.batchChan:
			startTime := time.Now()

			// Process batch
			ctx := context.Background()
			err := pa.ProcessBatch(ctx, batch)

			// Update metrics
			pa.updateMetrics(len(batch), time.Since(startTime), err)

			// Return events to pool
			for _, event := range batch {
				if pipelineEvent := pa.convertToPipelineEvent(event); pipelineEvent != nil {
					select {
					case pa.outputChan <- pipelineEvent:
					default:
						// Output channel full, drop event
						pa.updateDroppedEvents(1)
					}
				}
			}
		}
	}
}

// metricsWorker periodically updates metrics
func (pa *PerformanceAdapter) metricsWorker() {
	defer pa.wg.Done()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	var lastProcessed uint64
	var lastTime time.Time = time.Now()

	for {
		select {
		case <-pa.stopChan:
			return

		case <-ticker.C:
			metrics := pa.metrics.Load().(*interfaces.PipelineMetrics)
			now := time.Now()
			duration := now.Sub(lastTime)

			// Calculate throughput
			processed := metrics.EventsProcessed
			if duration > 0 {
				throughput := uint64(float64(processed-lastProcessed) / duration.Seconds())

				// Update metrics with new throughput
				newMetrics := *metrics
				newMetrics.Throughput = throughput
				newMetrics.QueueDepth = len(pa.inputChan)
				pa.metrics.Store(&newMetrics)
			}

			lastProcessed = processed
			lastTime = now
		}
	}
}

// Helper methods

func (pa *PerformanceAdapter) sendBatch(batch []*domain.UnifiedEvent) {
	if len(batch) == 0 {
		return
	}

	// Copy batch to avoid data races
	batchCopy := make([]*domain.UnifiedEvent, len(batch))
	copy(batchCopy, batch)

	select {
	case pa.batchChan <- batchCopy:
	case <-pa.stopChan:
	default:
		// Batch channel full, drop batch
		pa.updateDroppedEvents(uint64(len(batchCopy)))
	}
}

func (pa *PerformanceAdapter) updateMetrics(count int, latency time.Duration, err error) {
	metrics := pa.metrics.Load().(*interfaces.PipelineMetrics)
	newMetrics := *metrics

	newMetrics.EventsProcessed += uint64(count)

	// Update average latency (exponential moving average)
	if newMetrics.AverageLatency == 0 {
		newMetrics.AverageLatency = latency
	} else {
		alpha := 0.1 // Smoothing factor
		newMetrics.AverageLatency = time.Duration(
			float64(newMetrics.AverageLatency)*(1-alpha) + float64(latency)*alpha,
		)
	}

	// Update error rate
	if err != nil {
		newMetrics.ErrorRate = (newMetrics.ErrorRate*float64(newMetrics.EventsProcessed) + 1) /
			float64(newMetrics.EventsProcessed+1)
	} else {
		newMetrics.ErrorRate = (newMetrics.ErrorRate * float64(newMetrics.EventsProcessed)) /
			float64(newMetrics.EventsProcessed+1)
	}

	pa.metrics.Store(&newMetrics)
}

func (pa *PerformanceAdapter) updateDroppedEvents(count uint64) {
	metrics := pa.metrics.Load().(*interfaces.PipelineMetrics)
	newMetrics := *metrics
	newMetrics.EventsDropped += count
	pa.metrics.Store(&newMetrics)
}

func (pa *PerformanceAdapter) convertToUnifiedEvent(event *interfaces.PipelineEvent) *domain.UnifiedEvent {
	// Zero-copy conversion where possible
	return &domain.UnifiedEvent{
		ID:        fmt.Sprintf("%d", event.ID),
		Type:      domain.EventType(event.Type),
		Timestamp: time.Unix(0, event.Timestamp),
		// Note: Priority is handled through Impact.Severity or semantic context
		// Additional fields would be populated from metadata
	}
}

func (pa *PerformanceAdapter) convertToPipelineEvent(event *domain.UnifiedEvent) *interfaces.PipelineEvent {
	pEvent := pa.GetEvent()
	// Convert string ID to uint64 (simplified conversion)
	if id, err := strconv.ParseUint(event.ID, 10, 64); err == nil {
		pEvent.ID = id
	} else {
		pEvent.ID = 0 // Default if conversion fails
	}
	pEvent.Type = string(event.Type)
	pEvent.Timestamp = event.Timestamp.UnixNano()
	pEvent.Priority = 1 // Default priority - would be derived from Impact.Severity in real implementation
	// Additional metadata would be populated here
	return pEvent
}

// PerformanceOption is a configuration option for the PerformanceAdapter
type PerformanceOption func(*PerformanceAdapter)

// WithBatchSize sets the batch size
func WithBatchSize(size int) PerformanceOption {
	return func(pa *PerformanceAdapter) {
		pa.maxBatchSize = size
		pa.batchSize = int32(size)
	}
}

// WithFlushInterval sets the flush interval
func WithFlushInterval(interval time.Duration) PerformanceOption {
	return func(pa *PerformanceAdapter) {
		pa.flushInterval = interval
	}
}

// WithRingBufferSize sets the ring buffer size
func WithRingBufferSize(size int) PerformanceOption {
	return func(pa *PerformanceAdapter) {
		pa.ringBufferSize = size
	}
}

// WithBatchProcessor sets a custom batch processor
func WithBatchProcessor(processor interfaces.BatchProcessor) PerformanceOption {
	return func(pa *PerformanceAdapter) {
		pa.batchProcessor = processor
	}
}

// WithChannelSize sets the channel buffer size
func WithChannelSize(size int) PerformanceOption {
	return func(pa *PerformanceAdapter) {
		pa.inputChan = make(chan *interfaces.PipelineEvent, size)
		pa.outputChan = make(chan *interfaces.PipelineEvent, size)
	}
}
