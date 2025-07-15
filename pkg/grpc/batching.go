package grpc

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/yairfalse/tapio/pkg/events"
)

// EventBatcher efficiently batches events for high-throughput transmission
type EventBatcher struct {
	config ClientConfig
	sendFn BatchSendFunc

	// Batching state
	currentBatch []*events.UnifiedEvent
	batchMu      sync.Mutex
	maxBatchSize uint32
	batchTimeout time.Duration

	// Buffer management
	eventQueue chan *events.UnifiedEvent
	bufferSize int

	// Lifecycle management
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Metrics
	eventsQueued  uint64
	eventsDropped uint64
	batchesSent   uint64
	totalEvents   uint64

	// Flow control
	utilization float32

	// Timer for batch timeout
	batchTimer *time.Timer
	timerMu    sync.Mutex

	// State
	started atomic.Bool
	stopped atomic.Bool
}

// BatchSendFunc defines the function signature for sending batches
type BatchSendFunc func(ctx context.Context, batch *EventBatch) error

// ClientMetrics tracks client performance metrics
type ClientMetrics struct {
	// Connection metrics
	connectionsEstablished uint64
	connectionsFailed      uint64
	connectionsActive      int64

	// Event metrics
	eventsSent             uint64
	eventsDropped          uint64
	eventsSendFailed       uint64
	eventsThrottled        uint64
	eventsValidationFailed uint64

	// Batch metrics
	batchesSent   uint64
	batchesFailed uint64
	avgBatchSize  float64

	// Stream metrics
	responsesReceived uint64
	streamErrors      uint64
	eventAcksReceived uint64

	// Rate metrics
	eventsPerSecond float64
	bytesPerSecond  float64

	// Timing metrics
	avgSendTime  time.Duration
	lastSendTime time.Time

	// Server metrics
	serverLoad     float64
	serverCapacity uint32

	// Lifecycle
	clientStarted bool
	clientStopped bool
	startTime     time.Time

	// Sequence tracking
	sequence uint64

	// Rate calculation
	lastMetricsUpdate time.Time
	lastEventCount    uint64
	lastByteCount     uint64

	mu sync.RWMutex
}

// ClientStats provides client statistics
type ClientStats struct {
	ConnectionsEstablished uint64
	ConnectionsFailed      uint64
	ConnectionsActive      int64

	EventsSent       uint64
	EventsDropped    uint64
	EventsSendFailed uint64
	EventsThrottled  uint64

	BatchesSent   uint64
	BatchesFailed uint64
	AvgBatchSize  float64

	EventsPerSecond float64
	BytesPerSecond  float64

	Uptime         time.Duration
	ServerLoad     float64
	ServerCapacity uint32
}

// NewEventBatcher creates a new event batcher
func NewEventBatcher(config ClientConfig, sendFn BatchSendFunc) *EventBatcher {
	ctx, cancel := context.WithCancel(context.Background())

	return &EventBatcher{
		config:       config,
		sendFn:       sendFn,
		ctx:          ctx,
		cancel:       cancel,
		maxBatchSize: config.MaxBatchSize,
		batchTimeout: config.BatchTimeout,
		eventQueue:   make(chan *events.UnifiedEvent, config.BufferSize),
		bufferSize:   config.BufferSize,
		currentBatch: make([]*events.UnifiedEvent, 0, config.MaxBatchSize),
	}
}

// NewClientMetrics creates a new client metrics instance
func NewClientMetrics() *ClientMetrics {
	return &ClientMetrics{
		lastMetricsUpdate: time.Now(),
	}
}

// Start starts the event batcher
func (eb *EventBatcher) Start(ctx context.Context) error {
	if !eb.started.CompareAndSwap(false, true) {
		return fmt.Errorf("batcher already started")
	}

	// Start batch processor
	eb.wg.Add(1)
	go eb.batchProcessor()

	return nil
}

// Stop stops the event batcher
func (eb *EventBatcher) Stop() {
	if !eb.stopped.CompareAndSwap(false, true) {
		return
	}

	// Cancel context
	eb.cancel()

	// Close event queue
	close(eb.eventQueue)

	// Wait for processor to finish
	eb.wg.Wait()

	// Send any remaining events
	eb.flushCurrentBatch()
}

// AddEvent adds an event to the batch queue
func (eb *EventBatcher) AddEvent(event *events.UnifiedEvent) error {
	if eb.stopped.Load() {
		return fmt.Errorf("batcher is stopped")
	}

	select {
	case eb.eventQueue <- event:
		atomic.AddUint64(&eb.eventsQueued, 1)
		return nil
	default:
		// Queue is full, drop event
		atomic.AddUint64(&eb.eventsDropped, 1)
		return fmt.Errorf("event queue is full")
	}
}

// batchProcessor processes events and creates batches
func (eb *EventBatcher) batchProcessor() {
	defer eb.wg.Done()

	for {
		select {
		case event, ok := <-eb.eventQueue:
			if !ok {
				// Channel closed, flush and exit
				eb.flushCurrentBatch()
				return
			}

			eb.addEventToBatch(event)

		case <-eb.ctx.Done():
			eb.flushCurrentBatch()
			return
		}
	}
}

// addEventToBatch adds an event to the current batch
func (eb *EventBatcher) addEventToBatch(event *events.UnifiedEvent) {
	eb.batchMu.Lock()
	defer eb.batchMu.Unlock()

	// Add event to current batch
	eb.currentBatch = append(eb.currentBatch, event)

	// Check if batch is full
	if len(eb.currentBatch) >= int(eb.maxBatchSize) {
		eb.sendCurrentBatch()
		return
	}

	// Start or reset batch timer
	eb.resetBatchTimer()
}

// resetBatchTimer resets the batch timeout timer
func (eb *EventBatcher) resetBatchTimer() {
	eb.timerMu.Lock()
	defer eb.timerMu.Unlock()

	if eb.batchTimer != nil {
		eb.batchTimer.Stop()
	}

	eb.batchTimer = time.AfterFunc(eb.batchTimeout, func() {
		eb.batchMu.Lock()
		defer eb.batchMu.Unlock()

		if len(eb.currentBatch) > 0 {
			eb.sendCurrentBatch()
		}
	})
}

// sendCurrentBatch sends the current batch
func (eb *EventBatcher) sendCurrentBatch() {
	if len(eb.currentBatch) == 0 {
		return
	}

	// Create batch
	batch := &EventBatch{
		BatchId:     fmt.Sprintf("batch_%d_%d", time.Now().UnixNano(), atomic.AddUint64(&eb.batchesSent, 1)),
		CreatedAt:   timestamppb.Now(),
		Events:      make([]*events.UnifiedEvent, len(eb.currentBatch)),
		Compression: CompressionType_COMPRESSION_LZ4,
	}

	// Copy events to batch
	copy(batch.Events, eb.currentBatch)

	// Clear current batch
	eb.currentBatch = eb.currentBatch[:0]

	// Stop timer
	eb.timerMu.Lock()
	if eb.batchTimer != nil {
		eb.batchTimer.Stop()
		eb.batchTimer = nil
	}
	eb.timerMu.Unlock()

	// Send batch asynchronously
	go eb.sendBatchAsync(batch)
}

// sendBatchAsync sends a batch asynchronously
func (eb *EventBatcher) sendBatchAsync(batch *EventBatch) {
	ctx, cancel := context.WithTimeout(eb.ctx, 30*time.Second)
	defer cancel()

	if err := eb.sendFn(ctx, batch); err != nil {
		// Log error or handle failure
		// For now, just increment dropped counter
		atomic.AddUint64(&eb.eventsDropped, uint64(len(batch.Events)))
	} else {
		atomic.AddUint64(&eb.totalEvents, uint64(len(batch.Events)))
	}
}

// flushCurrentBatch flushes any remaining events
func (eb *EventBatcher) flushCurrentBatch() {
	eb.batchMu.Lock()
	defer eb.batchMu.Unlock()

	if len(eb.currentBatch) > 0 {
		eb.sendCurrentBatch()
	}
}

// GetUtilization returns the current buffer utilization (0.0 to 1.0)
func (eb *EventBatcher) GetUtilization() float32 {
	queueLen := len(eb.eventQueue)
	if eb.bufferSize == 0 {
		return 0.0
	}

	utilization := float32(queueLen) / float32(eb.bufferSize)
	eb.utilization = utilization
	return utilization
}

// UpdateMaxBatchSize updates the maximum batch size
func (eb *EventBatcher) UpdateMaxBatchSize(size uint32) {
	eb.batchMu.Lock()
	eb.maxBatchSize = size
	eb.batchMu.Unlock()
}

// ProcessAck processes an acknowledgment for a sent batch
func (eb *EventBatcher) ProcessAck(ack *EventAck) {
	// Implementation for processing acknowledgments
	// This could involve updating metrics, handling failed events, etc.
}

// GetStats returns batcher statistics
func (eb *EventBatcher) GetStats() BatcherStats {
	return BatcherStats{
		EventsQueued:  atomic.LoadUint64(&eb.eventsQueued),
		EventsDropped: atomic.LoadUint64(&eb.eventsDropped),
		BatchesSent:   atomic.LoadUint64(&eb.batchesSent),
		TotalEvents:   atomic.LoadUint64(&eb.totalEvents),
		QueueLength:   uint64(len(eb.eventQueue)),
		QueueCapacity: uint64(eb.bufferSize),
		Utilization:   eb.utilization,
	}
}

// BatcherStats provides batcher statistics
type BatcherStats struct {
	EventsQueued  uint64
	EventsDropped uint64
	BatchesSent   uint64
	TotalEvents   uint64
	QueueLength   uint64
	QueueCapacity uint64
	Utilization   float32
}

// Client metrics implementation

// ClientStarted records client start
func (cm *ClientMetrics) ClientStarted() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.clientStarted = true
	cm.startTime = time.Now()
}

// ClientStopped records client stop
func (cm *ClientMetrics) ClientStopped() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.clientStopped = true
}

// ConnectionEstablished records a successful connection
func (cm *ClientMetrics) ConnectionEstablished() {
	atomic.AddUint64(&cm.connectionsEstablished, 1)
	atomic.AddInt64(&cm.connectionsActive, 1)
}

// ConnectionFailed records a failed connection attempt
func (cm *ClientMetrics) ConnectionFailed() {
	atomic.AddUint64(&cm.connectionsFailed, 1)
}

// EventsSent records successfully sent events
func (cm *ClientMetrics) EventsSent(count uint64) {
	atomic.AddUint64(&cm.eventsSent, count)
	cm.lastSendTime = time.Now()
}

// EventSendFailed records failed event sends
func (cm *ClientMetrics) EventSendFailed(count uint64) {
	atomic.AddUint64(&cm.eventsSendFailed, count)
}

// EventSendTimeout records send timeouts
func (cm *ClientMetrics) EventSendTimeout() {
	atomic.AddUint64(&cm.eventsSendFailed, 1)
}

// EventsThrottled records throttled events
func (cm *ClientMetrics) EventsThrottled(count uint64) {
	atomic.AddUint64(&cm.eventsThrottled, count)
}

// EventValidationFailed records validation failures
func (cm *ClientMetrics) EventValidationFailed() {
	atomic.AddUint64(&cm.eventsValidationFailed, 1)
}

// ResponseReceived records a received response
func (cm *ClientMetrics) ResponseReceived() {
	atomic.AddUint64(&cm.responsesReceived, 1)
}

// StreamError records a stream error
func (cm *ClientMetrics) StreamError() {
	atomic.AddUint64(&cm.streamErrors, 1)
}

// EventAckReceived records a received event acknowledgment
func (cm *ClientMetrics) EventAckReceived() {
	atomic.AddUint64(&cm.eventAcksReceived, 1)
}

// CollectorRegistered records successful collector registration
func (cm *ClientMetrics) CollectorRegistered() {
	// No-op for now, could track registration time
}

// HeartbeatSent records a sent heartbeat
func (cm *ClientMetrics) HeartbeatSent() {
	// No-op for now, could track heartbeat frequency
}

// HeartbeatFailed records a failed heartbeat
func (cm *ClientMetrics) HeartbeatFailed() {
	// No-op for now, could track heartbeat failures
}

// FlowControlUpdated records flow control updates
func (cm *ClientMetrics) FlowControlUpdated() {
	// No-op for now, could track flow control changes
}

// FlowControlSendFailed records flow control send failures
func (cm *ClientMetrics) FlowControlSendFailed() {
	// No-op for now, could track flow control failures
}

// RateLimited records rate limiting events
func (cm *ClientMetrics) RateLimited() {
	// Could track rate limiting frequency
}

// GenericError records generic errors
func (cm *ClientMetrics) GenericError() {
	// Could track various error types
}

// UpdateServerStatus updates server status information
func (cm *ClientMetrics) UpdateServerStatus(status *ServerStatus) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.serverLoad = float64(status.Load)
	cm.serverCapacity = status.AvailableCapacity
}

// NextSequence returns the next sequence number
func (cm *ClientMetrics) NextSequence() uint64 {
	return atomic.AddUint64(&cm.sequence, 1)
}

// UpdateRates calculates and updates rate metrics
func (cm *ClientMetrics) UpdateRates() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(cm.lastMetricsUpdate).Seconds()

	if elapsed < 1.0 {
		return // Update at most once per second
	}

	// Calculate events per second
	currentEvents := atomic.LoadUint64(&cm.eventsSent)
	if cm.lastEventCount > 0 {
		eventsInPeriod := currentEvents - cm.lastEventCount
		cm.eventsPerSecond = float64(eventsInPeriod) / elapsed
	}
	cm.lastEventCount = currentEvents

	// Calculate average batch size
	batchesSent := atomic.LoadUint64(&cm.batchesSent)
	if batchesSent > 0 {
		cm.avgBatchSize = float64(currentEvents) / float64(batchesSent)
	}

	cm.lastMetricsUpdate = now
}

// GetStats returns a snapshot of client metrics
func (cm *ClientMetrics) GetStats() ClientStats {
	cm.UpdateRates()

	cm.mu.RLock()
	defer cm.mu.RUnlock()

	var uptime time.Duration
	if cm.clientStarted && !cm.startTime.IsZero() {
		uptime = time.Since(cm.startTime)
	}

	return ClientStats{
		ConnectionsEstablished: atomic.LoadUint64(&cm.connectionsEstablished),
		ConnectionsFailed:      atomic.LoadUint64(&cm.connectionsFailed),
		ConnectionsActive:      atomic.LoadInt64(&cm.connectionsActive),
		EventsSent:             atomic.LoadUint64(&cm.eventsSent),
		EventsDropped:          atomic.LoadUint64(&cm.eventsDropped),
		EventsSendFailed:       atomic.LoadUint64(&cm.eventsSendFailed),
		EventsThrottled:        atomic.LoadUint64(&cm.eventsThrottled),
		BatchesSent:            atomic.LoadUint64(&cm.batchesSent),
		BatchesFailed:          atomic.LoadUint64(&cm.batchesFailed),
		AvgBatchSize:           cm.avgBatchSize,
		EventsPerSecond:        cm.eventsPerSecond,
		BytesPerSecond:         cm.bytesPerSecond,
		Uptime:                 uptime,
		ServerLoad:             cm.serverLoad,
		ServerCapacity:         cm.serverCapacity,
	}
}
