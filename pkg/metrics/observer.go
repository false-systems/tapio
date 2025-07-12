package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// MetricEventPublisher implements MetricPublisher using observer pattern for real-time metric updates
type MetricEventPublisher[T MetricType] struct {
	// Dependencies
	logger *slog.Logger

	// Observer management
	mu        sync.RWMutex
	observers map[string]registeredObserver[T]
	sequence  int64

	// Event buffering for performance
	eventBuffer     chan bufferedEvent[T]
	bufferSize      int
	flushInterval   time.Duration
	flushThreshold  int
	batchProcessor  *batchProcessor[T]

	// State management
	running      int32
	shutdown     chan struct{}
	shutdownOnce sync.Once

	// Performance metrics
	stats PublisherStats

	// Configuration
	config PublisherConfig
}

// registeredObserver tracks registered observers with metadata
type registeredObserver[T MetricType] struct {
	observer     MetricObserver[T]
	id           string
	priority     ObserverPriority
	registered   time.Time
	lastNotified time.Time
	errorCount   int64
	eventCount   int64
	enabled      bool
}

// bufferedEvent represents an event in the buffer
type bufferedEvent[T MetricType] struct {
	event     MetricEvent[T]
	timestamp time.Time
	sequence  int64
	retries   int
}

// batchProcessor handles efficient batch processing of events
type batchProcessor[T MetricType] struct {
	publisher    *MetricEventPublisher[T]
	batchBuffer  []MetricEvent[T]
	batchMutex   sync.Mutex
	lastFlush    time.Time
	flushTicker  *time.Ticker
}

// PublisherConfig configures the event publisher
type PublisherConfig struct {
	// Buffer configuration
	DefaultBufferSize      int
	DefaultFlushInterval   time.Duration
	DefaultFlushThreshold  int
	MaxBufferSize          int

	// Observer management
	MaxObservers           int
	ObserverTimeout        time.Duration
	ErrorThreshold         int
	RetryAttempts          int
	RetryBackoff           time.Duration

	// Performance tuning
	EnableBatching         bool
	EnableAsync            bool
	EnableMetrics          bool
	WorkerPoolSize         int

	// Error handling
	ErrorStrategy          string
	DeadLetterQueue        bool
	MaxRetries             int
}

// PublisherStats tracks publisher performance metrics
type PublisherStats struct {
	ObserverCount      int64
	EventsPublished    int64
	EventsBuffered     int64
	EventsDropped      int64
	BatchesProcessed   int64
	ErrorCount         int64
	AverageLatency     time.Duration
	LastEvent          time.Time
	BufferUtilization  float64
}

// NewMetricEventPublisher creates a new event publisher with observer pattern
func NewMetricEventPublisher[T MetricType](config PublisherConfig, logger *slog.Logger) *MetricEventPublisher[T] {
	// Set defaults
	if config.DefaultBufferSize == 0 {
		config.DefaultBufferSize = 1000
	}
	if config.DefaultFlushInterval == 0 {
		config.DefaultFlushInterval = time.Second
	}
	if config.DefaultFlushThreshold == 0 {
		config.DefaultFlushThreshold = 100
	}
	if config.MaxBufferSize == 0 {
		config.MaxBufferSize = 10000
	}
	if config.MaxObservers == 0 {
		config.MaxObservers = 100
	}
	if config.ObserverTimeout == 0 {
		config.ObserverTimeout = 5 * time.Second
	}
	if config.ErrorThreshold == 0 {
		config.ErrorThreshold = 10
	}
	if config.RetryAttempts == 0 {
		config.RetryAttempts = 3
	}
	if config.RetryBackoff == 0 {
		config.RetryBackoff = 100 * time.Millisecond
	}
	if config.WorkerPoolSize == 0 {
		config.WorkerPoolSize = 5
	}

	if logger == nil {
		logger = slog.Default().With("component", "metric-publisher")
	}

	publisher := &MetricEventPublisher[T]{
		logger:         logger,
		observers:      make(map[string]registeredObserver[T]),
		eventBuffer:    make(chan bufferedEvent[T], config.DefaultBufferSize),
		bufferSize:     config.DefaultBufferSize,
		flushInterval:  config.DefaultFlushInterval,
		flushThreshold: config.DefaultFlushThreshold,
		shutdown:       make(chan struct{}),
		config:         config,
		stats: PublisherStats{
			LastEvent: time.Now(),
		},
	}

	// Initialize batch processor if batching is enabled
	if config.EnableBatching {
		publisher.batchProcessor = &batchProcessor[T]{
			publisher:   publisher,
			batchBuffer: make([]MetricEvent[T], 0, config.DefaultFlushThreshold),
			lastFlush:   time.Now(),
			flushTicker: time.NewTicker(config.DefaultFlushInterval),
		}
	}

	// Start background processing
	publisher.start()

	return publisher
}

// Subscribe adds an observer with priority-based ordering
func (p *MetricEventPublisher[T]) Subscribe(observer MetricObserver[T]) error {
	if observer == nil {
		return fmt.Errorf("observer cannot be nil")
	}

	observerID := observer.GetID()
	if observerID == "" {
		return fmt.Errorf("observer ID cannot be empty")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if observer already exists
	if _, exists := p.observers[observerID]; exists {
		return fmt.Errorf("observer with ID %s already exists", observerID)
	}

	// Check observer limit
	if len(p.observers) >= p.config.MaxObservers {
		return fmt.Errorf("maximum number of observers (%d) reached", p.config.MaxObservers)
	}

	// Register observer
	p.observers[observerID] = registeredObserver[T]{
		observer:     observer,
		id:           observerID,
		priority:     observer.GetPriority(),
		registered:   time.Now(),
		lastNotified: time.Now(),
		enabled:      true,
	}

	atomic.AddInt64(&p.stats.ObserverCount, 1)

	p.logger.Info("Observer subscribed",
		"observer_id", observerID,
		"priority", observer.GetPriority(),
		"total_observers", len(p.observers))

	return nil
}

// Unsubscribe removes an observer
func (p *MetricEventPublisher[T]) Unsubscribe(observerID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	observer, exists := p.observers[observerID]
	if !exists {
		return fmt.Errorf("observer with ID %s not found", observerID)
	}

	delete(p.observers, observerID)
	atomic.AddInt64(&p.stats.ObserverCount, -1)

	p.logger.Info("Observer unsubscribed",
		"observer_id", observerID,
		"event_count", observer.eventCount,
		"error_count", observer.errorCount)

	return nil
}

// Publish publishes a metric event to all observers
func (p *MetricEventPublisher[T]) Publish(ctx context.Context, event MetricEvent[T]) error {
	if atomic.LoadInt32(&p.running) == 0 {
		return fmt.Errorf("publisher is not running")
	}

	// Add sequence number and timestamp
	event.Timestamp = time.Now()
	sequence := atomic.AddInt64(&p.sequence, 1)

	bufferedEvent := bufferedEvent[T]{
		event:     event,
		timestamp: time.Now(),
		sequence:  sequence,
		retries:   0,
	}

	// Try to send to buffer
	select {
	case p.eventBuffer <- bufferedEvent:
		atomic.AddInt64(&p.stats.EventsBuffered, 1)
		p.stats.LastEvent = time.Now()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Buffer is full, handle based on strategy
		return p.handleBufferOverflow(ctx, bufferedEvent)
	}
}

// PublishBatch publishes multiple events efficiently
func (p *MetricEventPublisher[T]) PublishBatch(ctx context.Context, events []MetricEvent[T]) error {
	if atomic.LoadInt32(&p.running) == 0 {
		return fmt.Errorf("publisher is not running")
	}

	if len(events) == 0 {
		return nil
	}

	// Process events in batches if batch processing is enabled
	if p.config.EnableBatching && p.batchProcessor != nil {
		return p.batchProcessor.processBatch(ctx, events)
	}

	// Publish events individually
	var errors []error
	for _, event := range events {
		if err := p.Publish(ctx, event); err != nil {
			errors = append(errors, err)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("batch publish errors: %v", errors)
	}

	return nil
}

// GetObservers returns all registered observers
func (p *MetricEventPublisher[T]) GetObservers() []MetricObserver[T] {
	p.mu.RLock()
	defer p.mu.RUnlock()

	observers := make([]MetricObserver[T], 0, len(p.observers))
	for _, regObserver := range p.observers {
		if regObserver.enabled {
			observers = append(observers, regObserver.observer)
		}
	}

	return observers
}

// SetEventBuffer configures event buffering for performance
func (p *MetricEventPublisher[T]) SetEventBuffer(size int, flushInterval time.Duration) error {
	if size <= 0 {
		return fmt.Errorf("buffer size must be positive")
	}
	if size > p.config.MaxBufferSize {
		return fmt.Errorf("buffer size %d exceeds maximum %d", size, p.config.MaxBufferSize)
	}
	if flushInterval <= 0 {
		return fmt.Errorf("flush interval must be positive")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Update configuration
	p.bufferSize = size
	p.flushInterval = flushInterval

	p.logger.Info("Event buffer configuration updated",
		"buffer_size", size,
		"flush_interval", flushInterval)

	return nil
}

// GetStats returns publisher statistics
func (p *MetricEventPublisher[T]) GetStats() PublisherStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := p.stats
	stats.BufferUtilization = float64(len(p.eventBuffer)) / float64(p.bufferSize)

	return stats
}

// Close gracefully shuts down the publisher
func (p *MetricEventPublisher[T]) Close(ctx context.Context) error {
	var closeError error

	p.shutdownOnce.Do(func() {
		p.logger.Info("Starting publisher shutdown")

		// Stop accepting new events
		atomic.StoreInt32(&p.running, 0)

		// Signal shutdown
		close(p.shutdown)

		// Process remaining events with timeout
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		closeError = p.drainEvents(shutdownCtx)

		p.logger.Info("Publisher shutdown completed", "error", closeError)
	})

	return closeError
}

// Private methods

func (p *MetricEventPublisher[T]) start() {
	atomic.StoreInt32(&p.running, 1)

	// Start event processor workers
	for i := 0; i < p.config.WorkerPoolSize; i++ {
		go p.eventProcessor(i)
	}

	// Start batch processor if enabled
	if p.config.EnableBatching && p.batchProcessor != nil {
		go p.batchProcessor.run()
	}

	// Start statistics updater
	go p.updateStats()

	p.logger.Info("Publisher started",
		"worker_pool_size", p.config.WorkerPoolSize,
		"batch_processing", p.config.EnableBatching,
		"buffer_size", p.bufferSize)
}

func (p *MetricEventPublisher[T]) eventProcessor(workerID int) {
	logger := p.logger.With("worker_id", workerID)
	logger.Debug("Event processor started")

	defer logger.Debug("Event processor stopped")

	for {
		select {
		case <-p.shutdown:
			return
		case bufferedEvent := <-p.eventBuffer:
			p.processEvent(bufferedEvent)
		}
	}
}

func (p *MetricEventPublisher[T]) processEvent(bufferedEvent bufferedEvent[T]) {
	start := time.Now()

	// Get sorted observers by priority
	observers := p.getSortedObservers()

	// Notify observers
	for _, regObserver := range observers {
		if !regObserver.enabled {
			continue
		}

		// Create timeout context for observer notification
		notifyCtx, cancel := context.WithTimeout(context.Background(), p.config.ObserverTimeout)

		// Notify observer based on event type
		err := p.notifyObserver(notifyCtx, regObserver.observer, bufferedEvent.event)
		cancel()

		// Update observer statistics
		p.updateObserverStats(regObserver.id, err)

		if err != nil {
			p.logger.Warn("Observer notification failed",
				"observer_id", regObserver.id,
				"event_type", bufferedEvent.event.Type,
				"error", err)

			// Check if observer should be disabled due to errors
			if atomic.LoadInt64(&regObserver.errorCount) > int64(p.config.ErrorThreshold) {
				p.disableObserver(regObserver.id)
			}
		}
	}

	// Update statistics
	duration := time.Since(start)
	atomic.AddInt64(&p.stats.EventsPublished, 1)
	p.updateAverageLatency(duration)
}

func (p *MetricEventPublisher[T]) notifyObserver(ctx context.Context, observer MetricObserver[T], event MetricEvent[T]) error {
	switch event.Type {
	case EventTypeCreated:
		return observer.OnMetricCreated(ctx, event.Metric)
	case EventTypeUpdated:
		return observer.OnMetricUpdated(ctx, event.Metric, event.OldValue, event.NewValue)
	case EventTypeDeleted:
		return observer.OnMetricDeleted(ctx, event.Metric)
	case EventTypeError:
		if errorValue, ok := event.NewValue.(error); ok {
			return observer.OnError(ctx, errorValue, event.Metric)
		}
		return observer.OnError(ctx, fmt.Errorf("unknown error"), event.Metric)
	default:
		return fmt.Errorf("unknown event type: %s", event.Type)
	}
}

func (p *MetricEventPublisher[T]) getSortedObservers() []registeredObserver[T] {
	p.mu.RLock()
	observers := make([]registeredObserver[T], 0, len(p.observers))
	for _, observer := range p.observers {
		observers = append(observers, observer)
	}
	p.mu.RUnlock()

	// Sort by priority (high -> medium -> low)
	sort.Slice(observers, func(i, j int) bool {
		return p.getPriorityValue(observers[i].priority) > p.getPriorityValue(observers[j].priority)
	})

	return observers
}

func (p *MetricEventPublisher[T]) getPriorityValue(priority ObserverPriority) int {
	switch priority {
	case ObserverPriorityHigh:
		return 3
	case ObserverPriorityMedium:
		return 2
	case ObserverPriorityLow:
		return 1
	default:
		return 0
	}
}

func (p *MetricEventPublisher[T]) updateObserverStats(observerID string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if regObserver, exists := p.observers[observerID]; exists {
		regObserver.lastNotified = time.Now()
		atomic.AddInt64(&regObserver.eventCount, 1)

		if err != nil {
			atomic.AddInt64(&regObserver.errorCount, 1)
			atomic.AddInt64(&p.stats.ErrorCount, 1)
		}

		p.observers[observerID] = regObserver
	}
}

func (p *MetricEventPublisher[T]) disableObserver(observerID string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if regObserver, exists := p.observers[observerID]; exists {
		regObserver.enabled = false
		p.observers[observerID] = regObserver

		p.logger.Warn("Observer disabled due to excessive errors",
			"observer_id", observerID,
			"error_count", regObserver.errorCount)
	}
}

func (p *MetricEventPublisher[T]) handleBufferOverflow(ctx context.Context, event bufferedEvent[T]) error {
	switch p.config.ErrorStrategy {
	case "drop":
		atomic.AddInt64(&p.stats.EventsDropped, 1)
		p.logger.Warn("Event dropped due to buffer overflow", "event_type", event.event.Type)
		return nil
	case "block":
		// Try to send with context timeout
		select {
		case p.eventBuffer <- event:
			atomic.AddInt64(&p.stats.EventsBuffered, 1)
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	default:
		atomic.AddInt64(&p.stats.EventsDropped, 1)
		return fmt.Errorf("event buffer overflow")
	}
}

func (p *MetricEventPublisher[T]) updateAverageLatency(duration time.Duration) {
	// Simple moving average
	currentAvg := p.stats.AverageLatency
	if currentAvg == 0 {
		p.stats.AverageLatency = duration
	} else {
		p.stats.AverageLatency = (currentAvg + duration) / 2
	}
}

func (p *MetricEventPublisher[T]) updateStats() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.shutdown:
			return
		case <-ticker.C:
			p.mu.Lock()
			p.stats.BufferUtilization = float64(len(p.eventBuffer)) / float64(p.bufferSize)
			p.mu.Unlock()
		}
	}
}

func (p *MetricEventPublisher[T]) drainEvents(ctx context.Context) error {
	p.logger.Info("Draining remaining events", "buffer_size", len(p.eventBuffer))

	for {
		select {
		case <-ctx.Done():
			remaining := len(p.eventBuffer)
			if remaining > 0 {
				p.logger.Warn("Shutdown timeout reached with events remaining", "remaining", remaining)
			}
			return ctx.Err()
		case bufferedEvent := <-p.eventBuffer:
			p.processEvent(bufferedEvent)
		default:
			p.logger.Info("All events drained successfully")
			return nil
		}
	}
}

// Batch processor implementation

func (bp *batchProcessor[T]) run() {
	defer bp.flushTicker.Stop()

	for {
		select {
		case <-bp.publisher.shutdown:
			bp.flushBatch() // Final flush
			return
		case <-bp.flushTicker.C:
			bp.flushBatch()
		}
	}
}

func (bp *batchProcessor[T]) processBatch(ctx context.Context, events []MetricEvent[T]) error {
	bp.batchMutex.Lock()
	defer bp.batchMutex.Unlock()

	// Add events to batch buffer
	bp.batchBuffer = append(bp.batchBuffer, events...)

	// Check if we should flush based on threshold
	if len(bp.batchBuffer) >= bp.publisher.flushThreshold {
		return bp.flushBatch()
	}

	return nil
}

func (bp *batchProcessor[T]) flushBatch() error {
	bp.batchMutex.Lock()
	defer bp.batchMutex.Unlock()

	if len(bp.batchBuffer) == 0 {
		return nil
	}

	// Process batch
	batch := make([]MetricEvent[T], len(bp.batchBuffer))
	copy(batch, bp.batchBuffer)
	bp.batchBuffer = bp.batchBuffer[:0] // Clear buffer

	// Update statistics
	atomic.AddInt64(&bp.publisher.stats.BatchesProcessed, 1)
	bp.lastFlush = time.Now()

	// Process events in batch
	for _, event := range batch {
		bufferedEvent := bufferedEvent[T]{
			event:     event,
			timestamp: time.Now(),
			sequence:  atomic.AddInt64(&bp.publisher.sequence, 1),
		}
		bp.publisher.processEvent(bufferedEvent)
	}

	return nil
}