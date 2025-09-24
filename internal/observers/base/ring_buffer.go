package base

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// LocalConsumer processes events locally without going through NATS
// This is inspired by Hubble's consumer pattern for critical path processing
type LocalConsumer interface {
	// ConsumeEvent processes an event locally
	// Should return quickly to avoid blocking the ring buffer
	ConsumeEvent(ctx context.Context, event *domain.CollectorEvent) error

	// Priority determines processing order (higher = first)
	Priority() int

	// Name returns the consumer name for debugging
	Name() string

	// ShouldConsume allows filtering which events this consumer wants
	ShouldConsume(event *domain.CollectorEvent) bool
}

// RingBuffer provides high-performance event buffering with local consumer support
// Inspired by Hubble's perf ring buffer pattern but in userspace
type RingBuffer struct {
	// Core buffer - using atomic.Pointer for race-free access
	buffer   []atomic.Pointer[domain.CollectorEvent]
	capacity uint64
	mask     uint64 // capacity - 1 for fast modulo

	// Position tracking (cache-line aligned)
	_    [64 - unsafe.Sizeof(uint64(0))]byte
	head atomic.Uint64 // next write position

	_    [64 - unsafe.Sizeof(uint64(0))]byte
	tail atomic.Uint64 // next read position

	// Statistics
	_        [64 - unsafe.Sizeof(uint64(0))]byte
	dropped  atomic.Uint64
	produced atomic.Uint64
	consumed atomic.Uint64

	// Configuration
	logger        *zap.Logger
	collectorName string

	// Local consumers (sorted by priority)
	consumersLock sync.RWMutex
	consumers     []LocalConsumer

	// Output channel (optional - for backward compatibility)
	outputChan chan *domain.CollectorEvent

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// CPU affinity for better cache performance
	cpuCount int

	// Batch processing
	batchSize    int
	batchTimeout time.Duration
}

// RingBufferConfig configures the ring buffer
type RingBufferConfig struct {
	// Size must be power of 2 for performance
	Size int

	// Optional output channel for backward compatibility
	OutputChannel chan *domain.CollectorEvent

	// Batch processing settings
	BatchSize    int           // Default: 32
	BatchTimeout time.Duration // Default: 10ms

	// CPU affinity
	EnableCPUAffinity bool

	Logger        *zap.Logger
	CollectorName string
}

// NewRingBuffer creates a new ring buffer
func NewRingBuffer(config RingBufferConfig) (*RingBuffer, error) {
	// Ensure size is power of 2
	size := uint64(config.Size)
	if size == 0 {
		size = 8192 // Default size
	}
	if size&(size-1) != 0 {
		// Round up to next power of 2
		v := size
		v--
		v |= v >> 1
		v |= v >> 2
		v |= v >> 4
		v |= v >> 8
		v |= v >> 16
		v |= v >> 32
		v++
		size = v
	}

	if config.BatchSize == 0 {
		config.BatchSize = 32
	}
	if config.BatchTimeout == 0 {
		config.BatchTimeout = 10 * time.Millisecond
	}

	rb := &RingBuffer{
		buffer:        make([]atomic.Pointer[domain.CollectorEvent], size),
		capacity:      size,
		mask:          size - 1,
		logger:        config.Logger,
		collectorName: config.CollectorName,
		outputChan:    config.OutputChannel,
		consumers:     make([]LocalConsumer, 0),
		batchSize:     config.BatchSize,
		batchTimeout:  config.BatchTimeout,
		cpuCount:      runtime.NumCPU(),
	}

	return rb, nil
}

// Start begins processing events
func (rb *RingBuffer) Start(ctx context.Context) {
	rb.ctx, rb.cancel = context.WithCancel(ctx)

	// Start processor goroutine
	rb.wg.Add(1)
	go rb.processEvents()
}

// Stop gracefully shuts down the ring buffer
func (rb *RingBuffer) Stop() {
	if rb.cancel != nil {
		rb.cancel()
	}
	rb.wg.Wait()
}

// Write adds an event to the ring buffer
// This is lock-free and will overwrite old events if buffer is full
func (rb *RingBuffer) Write(event *domain.CollectorEvent) bool {
	if event == nil {
		return false
	}

	// Get next write position
	head := rb.head.Add(1) - 1
	tail := rb.tail.Load()

	// Check if buffer is full (will overwrite)
	if head-tail >= rb.capacity {
		rb.dropped.Add(1)
		if rb.logger != nil && rb.dropped.Load()%1000 == 0 {
			rb.logger.Warn("Ring buffer overwriting old events",
				zap.String("collector", rb.collectorName),
				zap.Uint64("dropped_total", rb.dropped.Load()),
			)
		}
	}

	// Write to buffer atomically (may overwrite old data)
	rb.buffer[head&rb.mask].Store(event)
	rb.produced.Add(1)

	return true
}

// RegisterLocalConsumer adds a local consumer for events
func (rb *RingBuffer) RegisterLocalConsumer(consumer LocalConsumer) {
	rb.consumersLock.Lock()
	defer rb.consumersLock.Unlock()

	rb.consumers = append(rb.consumers, consumer)

	// Sort by priority (highest first)
	for i := len(rb.consumers) - 1; i > 0; i-- {
		if rb.consumers[i].Priority() > rb.consumers[i-1].Priority() {
			rb.consumers[i], rb.consumers[i-1] = rb.consumers[i-1], rb.consumers[i]
		} else {
			break
		}
	}

	if rb.logger != nil {
		rb.logger.Info("Registered local consumer",
			zap.String("collector", rb.collectorName),
			zap.String("consumer", consumer.Name()),
			zap.Int("priority", consumer.Priority()),
		)
	}
}

// processEvents is the main processing loop
func (rb *RingBuffer) processEvents() {
	defer rb.wg.Done()

	batch := make([]*domain.CollectorEvent, 0, rb.batchSize)
	ticker := time.NewTicker(rb.batchTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-rb.ctx.Done():
			// Process remaining events
			rb.processBatch(rb.drainBuffer())
			return

		case <-ticker.C:
			// Process batch on timeout
			if len(batch) > 0 {
				rb.processBatch(batch)
				batch = batch[:0]
			}

		default:
			// Try to read events
			if event := rb.read(); event != nil {
				batch = append(batch, event)

				// Process batch when full
				if len(batch) >= rb.batchSize {
					rb.processBatch(batch)
					batch = batch[:0]
					ticker.Reset(rb.batchTimeout)
				}
			} else {
				// No events available, wait a bit
				time.Sleep(time.Microsecond)
			}
		}
	}
}

// read attempts to read an event from the buffer
func (rb *RingBuffer) read() *domain.CollectorEvent {
	tail := rb.tail.Load()
	head := rb.head.Load()

	// Check if buffer is empty
	if tail >= head {
		return nil
	}

	// Read event atomically
	event := rb.buffer[tail&rb.mask].Load()

	// Only advance tail if we successfully read
	if event != nil {
		rb.tail.Add(1)
		rb.consumed.Add(1)
		return event
	}

	return nil
}

// drainBuffer reads all remaining events
func (rb *RingBuffer) drainBuffer() []*domain.CollectorEvent {
	var events []*domain.CollectorEvent

	for {
		event := rb.read()
		if event == nil {
			break
		}
		events = append(events, event)
	}

	return events
}

// processBatch processes a batch of events
func (rb *RingBuffer) processBatch(events []*domain.CollectorEvent) {
	if len(events) == 0 {
		return
	}

	// Process through local consumers first
	rb.processLocalConsumers(events)

	// Send to output channel if configured
	if rb.outputChan != nil {
		for _, event := range events {
			select {
			case rb.outputChan <- event:
				// Sent successfully
			case <-rb.ctx.Done():
				return
			default:
				// Channel full, drop
				rb.dropped.Add(1)
			}
		}
	}
}

// processLocalConsumers sends events to local consumers
func (rb *RingBuffer) processLocalConsumers(events []*domain.CollectorEvent) {
	rb.consumersLock.RLock()
	consumers := rb.consumers
	rb.consumersLock.RUnlock()

	if len(consumers) == 0 {
		return
	}

	// Process each event through consumers
	for _, event := range events {
		for _, consumer := range consumers {
			// Check if consumer wants this event
			if !consumer.ShouldConsume(event) {
				continue
			}

			// Process with timeout to prevent blocking
			ctx, cancel := context.WithTimeout(rb.ctx, 100*time.Millisecond)
			err := consumer.ConsumeEvent(ctx, event)
			cancel()

			if err != nil && rb.logger != nil {
				rb.logger.Debug("Local consumer error",
					zap.String("collector", rb.collectorName),
					zap.String("consumer", consumer.Name()),
					zap.Error(err),
				)
			}
		}
	}
}

// Statistics returns buffer statistics
func (rb *RingBuffer) Statistics() RingBufferStats {
	head := rb.head.Load()
	tail := rb.tail.Load()

	var utilization float64
	if head >= tail {
		used := head - tail
		if used > rb.capacity {
			used = rb.capacity
		}
		utilization = float64(used) / float64(rb.capacity) * 100
	}

	return RingBufferStats{
		Capacity:    rb.capacity,
		Produced:    rb.produced.Load(),
		Consumed:    rb.consumed.Load(),
		Dropped:     rb.dropped.Load(),
		Utilization: utilization,
		Consumers:   len(rb.consumers),
	}
}

// RingBufferStats contains ring buffer statistics
type RingBufferStats struct {
	Capacity    uint64  `json:"capacity"`
	Produced    uint64  `json:"produced"`
	Consumed    uint64  `json:"consumed"`
	Dropped     uint64  `json:"dropped"`
	Utilization float64 `json:"utilization_percent"`
	Consumers   int     `json:"local_consumers"`
}
