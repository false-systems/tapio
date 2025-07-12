package ebpf

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cilium/ebpf/ringbuf"
	"github.com/falseyair/tapio/pkg/performance"
)

// RingBufferManager manages multiple eBPF ring buffers for event collection
type RingBufferManager struct {
	// Ring buffer readers
	readers map[string]*ringbuf.Reader
	
	// Event processors
	processors map[string]*performance.BatchProcessor[[]byte]
	
	// Event channels
	eventChans map[string]chan interface{}
	
	// Parsers for different event types
	parsers map[string]EventParser
	
	// Configuration
	config RingBufferConfig
	
	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	
	// Metrics
	eventsReceived atomic.Uint64
	eventsDropped  atomic.Uint64
	parseErrors    atomic.Uint64
	
	// State
	mu      sync.RWMutex
	started bool
}

// RingBufferConfig configures the ring buffer manager
type RingBufferConfig struct {
	// Processing configuration
	BatchSize        int
	BatchTimeout     time.Duration
	MaxQueueSize     int
	WorkerCount      int
	
	// Error handling
	MaxParseErrors   int
	ErrorBackoff     time.Duration
	
	// Metrics
	EnableMetrics    bool
	MetricsInterval  time.Duration
}

// DefaultRingBufferConfig returns default configuration
func DefaultRingBufferConfig() RingBufferConfig {
	return RingBufferConfig{
		BatchSize:        100,
		BatchTimeout:     50 * time.Millisecond,
		MaxQueueSize:     10000,
		WorkerCount:      4,
		MaxParseErrors:   1000,
		ErrorBackoff:     100 * time.Millisecond,
		EnableMetrics:    true,
		MetricsInterval:  10 * time.Second,
	}
}

// EventParser parses raw bytes into event structures
type EventParser interface {
	Parse(data []byte) (interface{}, error)
	EventType() string
}

// NewRingBufferManager creates a new ring buffer manager
func NewRingBufferManager(config RingBufferConfig) (*RingBufferManager, error) {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &RingBufferManager{
		readers:    make(map[string]*ringbuf.Reader),
		processors: make(map[string]*performance.BatchProcessor[[]byte]),
		eventChans: make(map[string]chan interface{}),
		parsers:    make(map[string]EventParser),
		config:     config,
		ctx:        ctx,
		cancel:     cancel,
	}, nil
}

// AddRingBuffer adds a ring buffer with its parser
func (m *RingBufferManager) AddRingBuffer(name string, reader *ringbuf.Reader, parser EventParser, eventChan chan interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if m.started {
		return errors.New("cannot add ring buffer after manager is started")
	}
	
	if _, exists := m.readers[name]; exists {
		return fmt.Errorf("ring buffer %s already exists", name)
	}
	
	// Store components
	m.readers[name] = reader
	m.parsers[name] = parser
	m.eventChans[name] = eventChan
	
	// Create batch processor for this ring buffer
	processFn := m.createProcessFunc(name, parser, eventChan)
	processor := performance.NewBatchProcessor(
		m.config.BatchSize,
		m.config.BatchTimeout,
		m.config.MaxQueueSize,
		processFn,
	)
	m.processors[name] = processor
	
	return nil
}

// Start starts the ring buffer manager
func (m *RingBufferManager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if m.started {
		return errors.New("ring buffer manager already started")
	}
	
	// Start all batch processors
	for name, processor := range m.processors {
		if err := processor.Start(); err != nil {
			return fmt.Errorf("failed to start processor for %s: %w", name, err)
		}
	}
	
	// Start reader goroutines for each ring buffer
	for name, reader := range m.readers {
		m.wg.Add(1)
		go m.readLoop(name, reader)
	}
	
	// Start metrics collector if enabled
	if m.config.EnableMetrics {
		m.wg.Add(1)
		go m.metricsLoop()
	}
	
	m.started = true
	return nil
}

// Stop stops the ring buffer manager
func (m *RingBufferManager) Stop() error {
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()
	
	// Cancel context to stop all goroutines
	m.cancel()
	
	// Close all readers
	m.mu.RLock()
	for _, reader := range m.readers {
		reader.Close()
	}
	m.mu.RUnlock()
	
	// Wait for goroutines to finish
	m.wg.Wait()
	
	// Stop all processors
	m.mu.RLock()
	var firstErr error
	for name, processor := range m.processors {
		if err := processor.Stop(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to stop processor for %s: %w", name, err)
		}
	}
	m.mu.RUnlock()
	
	// Close event channels
	m.mu.Lock()
	for _, ch := range m.eventChans {
		close(ch)
	}
	m.started = false
	m.mu.Unlock()
	
	return firstErr
}

// readLoop reads events from a ring buffer
func (m *RingBufferManager) readLoop(name string, reader *ringbuf.Reader) {
	defer m.wg.Done()
	
	processor := m.processors[name]
	consecutiveErrors := 0
	
	for {
		select {
		case <-m.ctx.Done():
			return
		default:
		}
		
		record, err := reader.Read()
		if err != nil {
			if errors.Is(err, ringbuf.ErrClosed) {
				return
			}
			
			consecutiveErrors++
			if consecutiveErrors > m.config.MaxParseErrors {
				// Too many errors, backing off
				select {
				case <-m.ctx.Done():
					return
				case <-time.After(m.config.ErrorBackoff):
					consecutiveErrors = 0
				}
			}
			continue
		}
		
		consecutiveErrors = 0
		m.eventsReceived.Add(1)
		
		// Copy the data as the ringbuf record will be reused
		data := make([]byte, len(record.RawSample))
		copy(data, record.RawSample)
		
		// Submit to batch processor
		if err := processor.Submit(data); err != nil {
			m.eventsDropped.Add(1)
		}
	}
}

// createProcessFunc creates a batch processing function for a specific ring buffer
func (m *RingBufferManager) createProcessFunc(name string, parser EventParser, eventChan chan interface{}) performance.BatchProcessFunc[[]byte] {
	return func(ctx context.Context, batch [][]byte) error {
		for _, data := range batch {
			// Parse the event
			event, err := parser.Parse(data)
			if err != nil {
				m.parseErrors.Add(1)
				continue
			}
			
			// Send to event channel
			select {
			case eventChan <- event:
			case <-ctx.Done():
				return ctx.Err()
			default:
				// Channel full, drop event
				m.eventsDropped.Add(1)
			}
		}
		return nil
	}
}

// metricsLoop periodically logs metrics
func (m *RingBufferManager) metricsLoop() {
	defer m.wg.Done()
	
	ticker := time.NewTicker(m.config.MetricsInterval)
	defer ticker.Stop()
	
	var lastReceived, lastDropped, lastErrors uint64
	
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			received := m.eventsReceived.Load()
			dropped := m.eventsDropped.Load()
			errors := m.parseErrors.Load()
			
			receivedDelta := received - lastReceived
			droppedDelta := dropped - lastDropped
			errorsDelta := errors - lastErrors
			
			if receivedDelta > 0 || droppedDelta > 0 || errorsDelta > 0 {
				fmt.Printf("RingBufferManager: received=%d/s, dropped=%d/s, errors=%d/s\n",
					receivedDelta*uint64(time.Second)/uint64(m.config.MetricsInterval),
					droppedDelta*uint64(time.Second)/uint64(m.config.MetricsInterval),
					errorsDelta*uint64(time.Second)/uint64(m.config.MetricsInterval),
				)
			}
			
			lastReceived = received
			lastDropped = dropped
			lastErrors = errors
		}
	}
}

// GetMetrics returns current metrics
func (m *RingBufferManager) GetMetrics() RingBufferMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	metrics := RingBufferMetrics{
		EventsReceived: m.eventsReceived.Load(),
		EventsDropped:  m.eventsDropped.Load(),
		ParseErrors:    m.parseErrors.Load(),
		BufferMetrics:  make(map[string]BufferMetrics),
	}
	
	// Collect per-buffer metrics
	for name, processor := range m.processors {
		procMetrics := processor.GetMetrics()
		metrics.BufferMetrics[name] = BufferMetrics{
			Processed:    procMetrics.Processed,
			Batches:      procMetrics.Batches,
			Dropped:      procMetrics.Dropped,
			Errors:       procMetrics.Errors,
			QueueSize:    procMetrics.QueueSize,
			AvgBatchSize: procMetrics.AvgBatchSize,
		}
	}
	
	return metrics
}

// RingBufferMetrics contains ring buffer manager metrics
type RingBufferMetrics struct {
	EventsReceived uint64
	EventsDropped  uint64
	ParseErrors    uint64
	BufferMetrics  map[string]BufferMetrics
}

// BufferMetrics contains metrics for a single buffer
type BufferMetrics struct {
	Processed    uint64
	Batches      uint64
	Dropped      uint64
	Errors       uint64
	QueueSize    int
	AvgBatchSize uint64
}

// EventBatch represents a batch of events for processing
type EventBatch struct {
	Events    []interface{}
	EventType string
	Timestamp time.Time
	Size      int
}

// NewEventBatch creates a new event batch
func NewEventBatch(eventType string, capacity int) *EventBatch {
	return &EventBatch{
		Events:    make([]interface{}, 0, capacity),
		EventType: eventType,
		Timestamp: time.Now(),
	}
}

// Add adds an event to the batch
func (b *EventBatch) Add(event interface{}) {
	b.Events = append(b.Events, event)
	b.Size = len(b.Events)
}

// Clear clears the batch
func (b *EventBatch) Clear() {
	b.Events = b.Events[:0]
	b.Size = 0
}

// IsFull checks if the batch is at capacity
func (b *EventBatch) IsFull() bool {
	return b.Size >= cap(b.Events)
}