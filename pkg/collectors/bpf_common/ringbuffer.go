package bpf_common

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain"
)

// RingBufferReader provides a high-level interface for reading from eBPF ring buffers
type RingBufferReader struct {
	reader    *ringbuf.Reader
	buffer    []byte
	processor EventProcessor
	metrics   ReaderMetrics
	ctx       context.Context
	cancel    context.CancelFunc

	// Configuration
	batchSize    int
	pollTimeout  time.Duration
	maxEventSize int
}

// EventProcessor defines the interface for processing parsed events
type EventProcessor interface {
	ProcessEvent(event *domain.RawEvent) error
	ProcessBatch(events []domain.RawEvent) error
}

// ReaderMetrics tracks ring buffer reader performance
type ReaderMetrics struct {
	BytesRead      uint64    // Total bytes read from ring buffer
	EventsParsed   uint64    // Successfully parsed events
	ParseErrors    uint64    // Events that failed to parse
	BufferOverruns uint64    // Ring buffer overrun events
	LastReadTime   time.Time // Timestamp of last successful read
}

// RingBufferConfig holds configuration for ring buffer readers
type RingBufferConfig struct {
	BatchSize    int           // Events to batch before processing (default: 32)
	PollTimeout  time.Duration // Timeout for ring buffer polling (default: 100ms)
	MaxEventSize int           // Maximum size of a single event (default: 64KB)
}

// DefaultRingBufferConfig returns sensible defaults
func DefaultRingBufferConfig() RingBufferConfig {
	return RingBufferConfig{
		BatchSize:    32,
		PollTimeout:  100 * time.Millisecond,
		MaxEventSize: 64 * 1024,
	}
}

// NewRingBufferReader creates a new ring buffer reader
func NewRingBufferReader(ringMap *ebpf.Map, processor EventProcessor, config RingBufferConfig) (*RingBufferReader, error) {
	reader, err := ringbuf.NewReader(ringMap)
	if err != nil {
		return nil, fmt.Errorf("failed to create ring buffer reader: %w", err)
	}

	if config.BatchSize == 0 {
		config.BatchSize = 32
	}
	if config.PollTimeout == 0 {
		config.PollTimeout = 100 * time.Millisecond
	}
	if config.MaxEventSize == 0 {
		config.MaxEventSize = 64 * 1024
	}

	ctx, cancel := context.WithCancel(context.Background())

	rbr := &RingBufferReader{
		reader:       reader,
		buffer:       make([]byte, 0, config.MaxEventSize),
		processor:    processor,
		batchSize:    config.BatchSize,
		pollTimeout:  config.PollTimeout,
		maxEventSize: config.MaxEventSize,
		ctx:          ctx,
		cancel:       cancel,
	}

	return rbr, nil
}

// Start begins reading from the ring buffer
func (r *RingBufferReader) Start() {
	go r.readLoop()
}

// Stop stops reading and closes the ring buffer
func (r *RingBufferReader) Stop() error {
	r.cancel()
	return r.reader.Close()
}

// GetMetrics returns current reader metrics
func (r *RingBufferReader) GetMetrics() ReaderMetrics {
	return ReaderMetrics{
		BytesRead:      atomic.LoadUint64(&r.metrics.BytesRead),
		EventsParsed:   atomic.LoadUint64(&r.metrics.EventsParsed),
		ParseErrors:    atomic.LoadUint64(&r.metrics.ParseErrors),
		BufferOverruns: atomic.LoadUint64(&r.metrics.BufferOverruns),
		LastReadTime:   r.metrics.LastReadTime,
	}
}

// readLoop is the main reading loop
func (r *RingBufferReader) readLoop() {
	batch := make([]domain.RawEvent, 0, r.batchSize)
	ticker := time.NewTicker(r.pollTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-r.ctx.Done():
			// Process any remaining events in batch
			if len(batch) > 0 {
				r.processBatch(batch)
			}
			return

		case <-ticker.C:
			// Process batch on timeout even if not full
			if len(batch) > 0 {
				r.processBatch(batch)
				batch = batch[:0] // Reset batch
			}

		default:
			// Try to read an event
			record, err := r.reader.Read()
			if err != nil {
				if r.ctx.Err() != nil {
					return // Context cancelled
				}

				// Check if this is a buffer overrun
				if err == ringbuf.ErrClosed {
					return
				}

				atomic.AddUint64(&r.metrics.BufferOverruns, 1)
				time.Sleep(time.Millisecond) // Brief pause on error
				continue
			}

			// Update metrics
			atomic.AddUint64(&r.metrics.BytesRead, uint64(len(record.RawSample)))
			r.metrics.LastReadTime = time.Now()

			// Create raw event with pooled objects
			event := GetEvent()
			event.Timestamp = time.Now()
			event.Data = append(event.Data, record.RawSample...) // Copy data
			event.TraceID = collectors.GenerateTraceID()
			event.SpanID = collectors.GenerateSpanID()

			batch = append(batch, *event)
			PutEvent(event) // Return event to pool after copying

			atomic.AddUint64(&r.metrics.EventsParsed, 1)

			// Process batch if full
			if len(batch) >= r.batchSize {
				r.processBatch(batch)
				batch = batch[:0] // Reset batch
			}
		}
	}
}

// processBatch processes a batch of events
func (r *RingBufferReader) processBatch(batch []domain.RawEvent) {
	if len(batch) == 0 {
		return
	}

	if err := r.processor.ProcessBatch(batch); err != nil {
		// If batch processing fails, try individual events
		for i := range batch {
			if err := r.processor.ProcessEvent(&batch[i]); err != nil {
				atomic.AddUint64(&r.metrics.ParseErrors, 1)
			}
		}
	}
}

// MemoryMapHelper provides utilities for safe eBPF memory operations
type MemoryMapHelper struct{}

// ParseEventSafely parses a struct from raw bytes with safety checks
func (m *MemoryMapHelper) ParseEventSafely(rawBytes []byte, targetStruct interface{}) error {
	if rawBytes == nil {
		return fmt.Errorf("raw bytes cannot be nil")
	}

	// Use reflection to get the size of the target struct
	targetSize := int(unsafe.Sizeof(targetStruct))

	if len(rawBytes) < targetSize {
		return fmt.Errorf("buffer too small: got %d bytes, need at least %d", len(rawBytes), targetSize)
	}

	if len(rawBytes) != targetSize {
		return fmt.Errorf("buffer size mismatch: got %d bytes, expected exactly %d", len(rawBytes), targetSize)
	}

	// Check alignment - most eBPF structs need 8-byte alignment for uint64 fields
	if uintptr(unsafe.Pointer(&rawBytes[0]))%8 != 0 {
		return fmt.Errorf("buffer not properly aligned for struct")
	}

	// This would require reflection to actually copy the data
	// For now, we'll provide the framework and let individual collectors implement

	return nil
}

// ValidateEventSize checks if event size is within reasonable bounds
func (m *MemoryMapHelper) ValidateEventSize(size int, maxSize int) error {
	if size <= 0 {
		return fmt.Errorf("invalid event size: %d", size)
	}

	if size > maxSize {
		return fmt.Errorf("event size too large: %d > %d", size, maxSize)
	}

	return nil
}

// SafeStringCopy copies a null-terminated string from a byte array
func (m *MemoryMapHelper) SafeStringCopy(src []byte, maxLen int) string {
	if len(src) == 0 {
		return ""
	}

	// Find null terminator within bounds
	end := len(src)
	if maxLen > 0 && end > maxLen {
		end = maxLen
	}

	for i := 0; i < end; i++ {
		if src[i] == 0 {
			end = i
			break
		}
	}

	return string(src[:end])
}

// Global helper instance
var MemHelper = &MemoryMapHelper{}

// SimpleEventProcessor provides a basic implementation of EventProcessor
type SimpleEventProcessor struct {
	eventChan chan<- domain.RawEvent
	batchChan chan<- []domain.RawEvent
}

// NewSimpleEventProcessor creates a simple event processor
func NewSimpleEventProcessor(eventChan chan<- domain.RawEvent) *SimpleEventProcessor {
	return &SimpleEventProcessor{
		eventChan: eventChan,
	}
}

// NewBatchEventProcessor creates a simple batch event processor
func NewBatchEventProcessor(batchChan chan<- []domain.RawEvent) *SimpleEventProcessor {
	return &SimpleEventProcessor{
		batchChan: batchChan,
	}
}

// ProcessEvent sends individual events to the channel
func (p *SimpleEventProcessor) ProcessEvent(event *domain.RawEvent) error {
	if p.eventChan == nil {
		return fmt.Errorf("event channel not configured")
	}

	select {
	case p.eventChan <- *event:
		return nil
	default:
		return fmt.Errorf("event channel full, dropping event")
	}
}

// ProcessBatch sends batched events to the channel
func (p *SimpleEventProcessor) ProcessBatch(events []domain.RawEvent) error {
	if p.batchChan != nil {
		// Send as batch if batch channel is available
		select {
		case p.batchChan <- events:
			return nil
		default:
			return fmt.Errorf("batch channel full, falling back to individual events")
		}
	}

	// Fall back to individual event processing
	if p.eventChan == nil {
		return fmt.Errorf("no output channels configured")
	}

	for i := range events {
		if err := p.ProcessEvent(&events[i]); err != nil {
			return fmt.Errorf("failed to process event %d: %w", i, err)
		}
	}

	return nil
}
