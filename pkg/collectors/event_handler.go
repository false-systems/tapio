package collectors

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// EventHandler processes events and streams them via gRPC
type EventHandler interface {
	HandleEvent(ctx context.Context, event *Event) error
	GetStats() map[string]interface{}
}

// GRPCEventHandler implements optimized event streaming to tapio-server
type GRPCEventHandler struct {
	client    *GRPCStreamingClient
	batcher   *EventBatcher
	
	// Statistics
	eventsSent     uint64
	eventsDropped  uint64
	batchesSent    uint64
	bytesStreamed  uint64
	errors         uint64
	
	// State
	mu        sync.RWMutex
	lastError error
	lastSent  time.Time
}

// NewEventHandler creates a new gRPC event handler
func NewEventHandler(client *GRPCStreamingClient) EventHandler {
	handler := &GRPCEventHandler{
		client:  client,
		lastSent: time.Now(),
	}
	
	// Create optimized batcher for high-throughput events
	handler.batcher = NewEventBatcher(BatcherConfig{
		MaxBatchSize:     100,
		MaxBatchBytes:    1024 * 1024, // 1MB
		BatchTimeout:     100 * time.Millisecond,
		CompressionLevel: CompressionLevelFast,
		OnBatch:          handler.sendBatch,
	})
	
	// Start the batcher
	handler.batcher.Start()
	
	return handler
}

// HandleEvent processes a single event
func (h *GRPCEventHandler) HandleEvent(ctx context.Context, event *Event) error {
	// Add to batcher for optimized streaming
	if err := h.batcher.Add(event); err != nil {
		atomic.AddUint64(&h.eventsDropped, 1)
		return fmt.Errorf("failed to batch event: %w", err)
	}
	
	return nil
}

// sendBatch sends a batch of events via gRPC
func (h *GRPCEventHandler) sendBatch(batch []*Event) error {
	if len(batch) == 0 {
		return nil
	}
	
	// Convert to stream format
	streamBatch := &EventStreamBatch{
		Events:    batch,
		Timestamp: time.Now(),
		NodeID:    getNodeID(),
	}
	
	// Send via gRPC client
	if err := h.client.StreamEvents(streamBatch); err != nil {
		atomic.AddUint64(&h.errors, 1)
		h.recordError(err)
		return fmt.Errorf("failed to stream batch: %w", err)
	}
	
	// Update statistics
	atomic.AddUint64(&h.eventsSent, uint64(len(batch)))
	atomic.AddUint64(&h.batchesSent, 1)
	atomic.AddUint64(&h.bytesStreamed, uint64(estimateBatchSize(batch)))
	
	h.mu.Lock()
	h.lastSent = time.Now()
	h.mu.Unlock()
	
	return nil
}

// GetStats returns handler statistics
func (h *GRPCEventHandler) GetStats() map[string]interface{} {
	h.mu.RLock()
	lastError := h.lastError
	lastSent := h.lastSent
	h.mu.RUnlock()
	
	stats := map[string]interface{}{
		"events_sent":      atomic.LoadUint64(&h.eventsSent),
		"events_dropped":   atomic.LoadUint64(&h.eventsDropped),
		"batches_sent":     atomic.LoadUint64(&h.batchesSent),
		"bytes_streamed":   atomic.LoadUint64(&h.bytesStreamed),
		"errors":           atomic.LoadUint64(&h.errors),
		"last_sent":        lastSent,
		"time_since_last":  time.Since(lastSent).Seconds(),
	}
	
	if lastError != nil {
		stats["last_error"] = lastError.Error()
	}
	
	// Add batcher stats
	for k, v := range h.batcher.GetStats() {
		stats["batcher_"+k] = v
	}
	
	// Add client stats
	if clientStats := h.client.GetStats(); clientStats != nil {
		for k, v := range clientStats {
			stats["client_"+k] = v
		}
	}
	
	return stats
}

// recordError records the last error
func (h *GRPCEventHandler) recordError(err error) {
	h.mu.Lock()
	h.lastError = err
	h.mu.Unlock()
}

// EventBatcher batches events for efficient streaming
type EventBatcher struct {
	config BatcherConfig
	
	// Batching state
	currentBatch []*Event
	currentSize  int
	batchMu      sync.Mutex
	
	// Control channels
	eventChan chan *Event
	flushChan chan struct{}
	stopChan  chan struct{}
	doneChan  chan struct{}
	
	// Statistics
	totalBatches   uint64
	totalEvents    uint64
	droppedEvents  uint64
	compressionWins uint64
}

// BatcherConfig configures the event batcher
type BatcherConfig struct {
	MaxBatchSize     int
	MaxBatchBytes    int
	BatchTimeout     time.Duration
	CompressionLevel CompressionLevel
	OnBatch          func([]*Event) error
}

// CompressionLevel defines compression settings
type CompressionLevel int

const (
	CompressionLevelNone CompressionLevel = iota
	CompressionLevelFast
	CompressionLevelBest
)

// NewEventBatcher creates a new event batcher
func NewEventBatcher(config BatcherConfig) *EventBatcher {
	return &EventBatcher{
		config:       config,
		currentBatch: make([]*Event, 0, config.MaxBatchSize),
		eventChan:    make(chan *Event, config.MaxBatchSize*10),
		flushChan:    make(chan struct{}, 1),
		stopChan:     make(chan struct{}),
		doneChan:     make(chan struct{}),
	}
}

// Start starts the batcher
func (b *EventBatcher) Start() {
	go b.run()
}

// Stop stops the batcher
func (b *EventBatcher) Stop() {
	close(b.stopChan)
	<-b.doneChan
}

// Add adds an event to the batch
func (b *EventBatcher) Add(event *Event) error {
	select {
	case b.eventChan <- event:
		return nil
	default:
		atomic.AddUint64(&b.droppedEvents, 1)
		return fmt.Errorf("event buffer full")
	}
}

// Flush forces a batch flush
func (b *EventBatcher) Flush() {
	select {
	case b.flushChan <- struct{}{}:
	default:
		// Flush already pending
	}
}

// run is the main batcher loop
func (b *EventBatcher) run() {
	defer close(b.doneChan)
	
	timer := time.NewTimer(b.config.BatchTimeout)
	defer timer.Stop()
	
	for {
		select {
		case <-b.stopChan:
			// Final flush before stopping
			b.flushBatch()
			return
			
		case event := <-b.eventChan:
			b.addToBatch(event)
			
		case <-timer.C:
			b.flushBatch()
			timer.Reset(b.config.BatchTimeout)
			
		case <-b.flushChan:
			b.flushBatch()
			timer.Reset(b.config.BatchTimeout)
		}
	}
}

// addToBatch adds an event to the current batch
func (b *EventBatcher) addToBatch(event *Event) {
	b.batchMu.Lock()
	defer b.batchMu.Unlock()
	
	eventSize := estimateEventSize(event)
	
	// Check if adding this event would exceed limits
	if len(b.currentBatch) >= b.config.MaxBatchSize ||
		b.currentSize+eventSize > b.config.MaxBatchBytes {
		// Flush current batch first
		b.flushBatchLocked()
	}
	
	// Add to batch
	b.currentBatch = append(b.currentBatch, event)
	b.currentSize += eventSize
	atomic.AddUint64(&b.totalEvents, 1)
}

// flushBatch flushes the current batch
func (b *EventBatcher) flushBatch() {
	b.batchMu.Lock()
	defer b.batchMu.Unlock()
	
	b.flushBatchLocked()
}

// flushBatchLocked flushes the batch (must hold batchMu)
func (b *EventBatcher) flushBatchLocked() {
	if len(b.currentBatch) == 0 {
		return
	}
	
	// Create a copy of the batch
	batch := make([]*Event, len(b.currentBatch))
	copy(batch, b.currentBatch)
	
	// Reset current batch
	b.currentBatch = b.currentBatch[:0]
	b.currentSize = 0
	
	// Update statistics
	atomic.AddUint64(&b.totalBatches, 1)
	
	// Send batch (in goroutine to avoid blocking)
	go func() {
		if err := b.config.OnBatch(batch); err != nil {
			// Log error (in production, this would use proper logging)
			fmt.Printf("Failed to send batch: %v\n", err)
		}
	}()
}

// GetStats returns batcher statistics
func (b *EventBatcher) GetStats() map[string]interface{} {
	b.batchMu.Lock()
	currentBatchSize := len(b.currentBatch)
	b.batchMu.Unlock()
	
	return map[string]interface{}{
		"total_batches":     atomic.LoadUint64(&b.totalBatches),
		"total_events":      atomic.LoadUint64(&b.totalEvents),
		"dropped_events":    atomic.LoadUint64(&b.droppedEvents),
		"compression_wins":  atomic.LoadUint64(&b.compressionWins),
		"current_batch_size": currentBatchSize,
		"event_buffer_size": len(b.eventChan),
	}
}

// Helper functions

func estimateEventSize(event *Event) int {
	// Rough estimate of event size in bytes
	size := 64 // Base struct size
	
	// Add data map size estimate
	for k, v := range event.Data {
		size += len(k) + estimateValueSize(v)
	}
	
	// Add metadata
	size += len(event.Source.Collector) + len(event.Source.Component)
	
	return size
}

func estimateBatchSize(batch []*Event) int {
	size := 0
	for _, event := range batch {
		size += estimateEventSize(event)
	}
	return size
}

func estimateValueSize(v interface{}) int {
	switch val := v.(type) {
	case string:
		return len(val)
	case []byte:
		return len(val)
	case map[string]interface{}:
		size := 0
		for k, v := range val {
			size += len(k) + estimateValueSize(v)
		}
		return size
	default:
		return 32 // Default size for other types
	}
}

func getNodeID() string {
	// TODO: Get actual node ID from environment
	return "node-1"
}