package cqrs

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/integrations/otel/domain"
)

// EventBus orchestrates event processing in the CQRS/Event Sourcing pattern
// Provides reliable event publishing, subscription management, and delivery guarantees
type EventBus struct {
	// Event handlers registry
	handlers    map[EventType][]EventHandler
	handlersMux sync.RWMutex

	// Subscription management
	subscriptions   map[SubscriptionID]*Subscription
	subscriptionMux sync.RWMutex

	// Event store integration
	eventStore EventStore

	// Message broker integration
	messageBroker MessageBroker

	// Event processing pipeline
	processors []EventProcessor

	// Dead letter queue
	deadLetterQueue *DeadLetterQueue

	// Performance optimization
	eventPool  sync.Pool
	bufferPool sync.Pool

	// Configuration
	config EventBusConfig

	// Metrics and monitoring
	metrics *EventMetrics

	// Circuit breaker for resilience
	circuitBreaker *CircuitBreaker

	// Event filtering and routing
	router *EventRouter
	filter *EventFilter

	// Saga coordination
	sagaManager *SagaManager

	// Background processing
	workers      []*EventWorker
	workerPool   chan *EventWorker
	shutdownChan chan struct{}
	shutdownOnce sync.Once
}

// EventHandler defines the interface for handling domain events
type EventHandler interface {
	Handle(ctx context.Context, event domain.TraceEvent) error
	GetEventTypes() []EventType
	GetHandlerName() string
	GetHandlerVersion() string
	IsAsync() bool
	GetRetryPolicy() *RetryPolicy
}

// EventProcessor defines middleware for event processing pipeline
type EventProcessor interface {
	Process(ctx context.Context, event domain.TraceEvent, next EventHandler) error
	GetProcessorName() string
	GetOrder() int
}

// EventBusConfig configures the event bus behavior
type EventBusConfig struct {
	// Processing configuration
	WorkerCount         int
	BufferSize          int
	MaxConcurrentEvents int
	EventTimeout        time.Duration

	// Delivery guarantees
	DeliveryMode          DeliveryMode
	AcknowledgmentTimeout time.Duration
	MaxRetryAttempts      int
	RetryBackoff          time.Duration

	// Dead letter queue
	EnableDeadLetterQueue bool
	DeadLetterTTL         time.Duration
	DeadLetterMaxSize     int

	// Performance settings
	EnableBatching    bool
	BatchSize         int
	BatchTimeout      time.Duration
	EnableCompression bool

	// Persistence settings
	EnableEventStore bool
	PersistenceMode  PersistenceMode

	// Circuit breaker settings
	EnableCircuitBreaker bool
	FailureThreshold     int
	RecoveryTimeout      time.Duration

	// Monitoring settings
	EnableMetrics   bool
	MetricsInterval time.Duration

	// Message broker settings
	BrokerType   BrokerType
	BrokerConfig map[string]any
}

// NewEventBus creates a new event bus with configuration
func NewEventBus(
	config EventBusConfig,
	eventStore EventStore,
	messageBroker MessageBroker,
) *EventBus {

	applyEventBusDefaults(&config)

	bus := &EventBus{
		handlers:      make(map[EventType][]EventHandler),
		subscriptions: make(map[SubscriptionID]*Subscription),
		eventStore:    eventStore,
		messageBroker: messageBroker,
		config:        config,
		metrics:       NewEventMetrics(),
		shutdownChan:  make(chan struct{}),
		workerPool:    make(chan *EventWorker, config.WorkerCount),
	}

	// Initialize pools for performance
	bus.initializePools()

	// Initialize components
	bus.deadLetterQueue = NewDeadLetterQueue(DeadLetterConfig{
		TTL:     config.DeadLetterTTL,
		MaxSize: config.DeadLetterMaxSize,
	})

	bus.router = NewEventRouter()
	bus.filter = NewEventFilter()
	bus.sagaManager = NewSagaManager()

	// Initialize circuit breaker if enabled
	if config.EnableCircuitBreaker {
		bus.circuitBreaker = NewCircuitBreaker(CircuitBreakerConfig{
			FailureThreshold: config.FailureThreshold,
			RecoveryTimeout:  config.RecoveryTimeout,
		})
	}

	// Initialize workers
	bus.initializeWorkers()

	// Start background processing
	go bus.startBackgroundProcessing()

	return bus
}

// Subscribe registers an event handler for specific event types
func (bus *EventBus) Subscribe(handler EventHandler) (SubscriptionID, error) {
	bus.handlersMux.Lock()
	defer bus.handlersMux.Unlock()

	// Register handler for each event type it handles
	for _, eventType := range handler.GetEventTypes() {
		bus.handlers[eventType] = append(bus.handlers[eventType], handler)
	}

	// Create subscription
	subscriptionID := generateSubscriptionID()
	subscription := &Subscription{
		ID:         subscriptionID,
		Handler:    handler,
		EventTypes: handler.GetEventTypes(),
		CreatedAt:  time.Now(),
		IsActive:   true,
	}

	bus.subscriptionMux.Lock()
	bus.subscriptions[subscriptionID] = subscription
	bus.subscriptionMux.Unlock()

	bus.metrics.RecordSubscription(handler.GetHandlerName(), len(handler.GetEventTypes()))

	return subscriptionID, nil
}

// Unsubscribe removes an event handler subscription
func (bus *EventBus) Unsubscribe(subscriptionID SubscriptionID) error {
	bus.subscriptionMux.Lock()
	subscription, exists := bus.subscriptions[subscriptionID]
	if !exists {
		bus.subscriptionMux.Unlock()
		return fmt.Errorf("subscription not found: %s", subscriptionID)
	}

	subscription.IsActive = false
	delete(bus.subscriptions, subscriptionID)
	bus.subscriptionMux.Unlock()

	// Remove handler from event type mappings
	bus.handlersMux.Lock()
	defer bus.handlersMux.Unlock()

	for _, eventType := range subscription.EventTypes {
		handlers := bus.handlers[eventType]
		for i, handler := range handlers {
			if handler.GetHandlerName() == subscription.Handler.GetHandlerName() {
				// Remove handler from slice
				bus.handlers[eventType] = append(handlers[:i], handlers[i+1:]...)
				break
			}
		}

		// Clean up empty handler lists
		if len(bus.handlers[eventType]) == 0 {
			delete(bus.handlers, eventType)
		}
	}

	bus.metrics.RecordUnsubscription(subscription.Handler.GetHandlerName())

	return nil
}

// Publish publishes an event to all registered handlers
func (bus *EventBus) Publish(ctx context.Context, event domain.TraceEvent) error {
	startTime := time.Now()
	eventType := getEventType(event)

	// Record publication attempt
	bus.metrics.RecordEventPublished(eventType)

	// Apply event filtering
	if !bus.filter.ShouldProcess(event) {
		bus.metrics.RecordEventFiltered(eventType)
		return nil
	}

	// Persist event if event store is enabled
	if bus.config.EnableEventStore && bus.eventStore != nil {
		if err := bus.persistEvent(ctx, event); err != nil {
			bus.metrics.RecordPersistenceError(eventType, err)
			// Continue with in-memory processing even if persistence fails
		}
	}

	// Route event to appropriate handlers
	if bus.config.EnableBatching {
		return bus.publishBatched(ctx, event, startTime)
	}

	return bus.publishImmediate(ctx, event, startTime)
}

// PublishBatch publishes multiple events efficiently
func (bus *EventBus) PublishBatch(ctx context.Context, events []domain.TraceEvent) error {
	if len(events) == 0 {
		return nil
	}

	startTime := time.Now()
	bus.metrics.RecordBatchPublishAttempt(len(events))

	// Group events by type for efficient processing
	eventGroups := bus.groupEventsByType(events)

	// Process each group
	var errors []error
	successCount := 0

	for eventType, groupEvents := range eventGroups {
		if err := bus.publishEventGroup(ctx, eventType, groupEvents); err != nil {
			errors = append(errors, fmt.Errorf("failed to publish events of type %s: %w", eventType, err))
		} else {
			successCount += len(groupEvents)
		}
	}

	bus.metrics.RecordBatchPublishComplete(successCount, len(errors), time.Since(startTime))

	if len(errors) > 0 {
		return fmt.Errorf("batch publish completed with errors: %v", errors)
	}

	return nil
}

// PublishAsync publishes an event asynchronously
func (bus *EventBus) PublishAsync(ctx context.Context, event domain.TraceEvent) (*AsyncEventResult, error) {
	// Get worker from pool
	worker := bus.getWorker()
	if worker == nil {
		return nil, fmt.Errorf("no workers available")
	}

	// Create async result
	result := &AsyncEventResult{
		EventID:     event.GetEventID().String(),
		Status:      AsyncStatusPending,
		ResultChan:  make(chan error, 1),
		SubmittedAt: time.Now(),
	}

	// Submit to worker
	worker.SubmitEvent(ctx, event, result)

	bus.metrics.RecordAsyncEventSubmitted(getEventType(event))

	return result, nil
}

// StartSaga starts a new saga with the given event
func (bus *EventBus) StartSaga(ctx context.Context, sagaType string, event domain.TraceEvent) (SagaID, error) {
	if bus.sagaManager == nil {
		return "", fmt.Errorf("saga manager not initialized")
	}

	sagaID, err := bus.sagaManager.StartSaga(ctx, sagaType, event)
	if err != nil {
		return "", fmt.Errorf("failed to start saga: %w", err)
	}

	bus.metrics.RecordSagaStarted(sagaType)

	return sagaID, nil
}

// Private implementation methods

func (bus *EventBus) publishImmediate(ctx context.Context, event domain.TraceEvent, startTime time.Time) error {
	eventType := getEventType(event)

	// Get handlers for this event type
	handlers := bus.getHandlers(eventType)
	if len(handlers) == 0 {
		bus.metrics.RecordNoHandlers(eventType)
		return nil
	}

	// Use circuit breaker if enabled
	if bus.config.EnableCircuitBreaker && bus.circuitBreaker != nil {
		return bus.circuitBreaker.Execute(func() error {
			return bus.processEventWithHandlers(ctx, event, handlers, startTime)
		})
	}

	return bus.processEventWithHandlers(ctx, event, handlers, startTime)
}

func (bus *EventBus) publishBatched(ctx context.Context, event domain.TraceEvent, startTime time.Time) error {
	// For now, delegate to immediate processing
	// In a full implementation, this would buffer events and process them in batches
	return bus.publishImmediate(ctx, event, startTime)
}

func (bus *EventBus) processEventWithHandlers(
	ctx context.Context,
	event domain.TraceEvent,
	handlers []EventHandler,
	startTime time.Time,
) error {

	var errors []error
	successCount := 0

	// Process async and sync handlers separately
	asyncHandlers := make([]EventHandler, 0)
	syncHandlers := make([]EventHandler, 0)

	for _, handler := range handlers {
		if handler.IsAsync() {
			asyncHandlers = append(asyncHandlers, handler)
		} else {
			syncHandlers = append(syncHandlers, handler)
		}
	}

	// Process sync handlers first
	for _, handler := range syncHandlers {
		if err := bus.processEventWithHandler(ctx, event, handler); err != nil {
			errors = append(errors, fmt.Errorf("handler %s failed: %w", handler.GetHandlerName(), err))
		} else {
			successCount++
		}
	}

	// Process async handlers concurrently
	if len(asyncHandlers) > 0 {
		asyncResults := bus.processAsyncHandlers(ctx, event, asyncHandlers)
		for _, result := range asyncResults {
			if result.Error != nil {
				errors = append(errors, result.Error)
			} else {
				successCount++
			}
		}
	}

	// Record metrics
	eventType := getEventType(event)
	bus.metrics.RecordEventProcessed(eventType, successCount, len(errors), time.Since(startTime))

	// Handle partial failures
	if len(errors) > 0 {
		if successCount == 0 {
			// All handlers failed - send to dead letter queue
			bus.sendToDeadLetterQueue(event, errors)
			return fmt.Errorf("all handlers failed for event %s: %v", eventType, errors)
		}

		// Some handlers failed - log but don't fail the entire operation
		bus.metrics.RecordPartialFailure(eventType, len(errors))
	}

	return nil
}

func (bus *EventBus) processEventWithHandler(
	ctx context.Context,
	event domain.TraceEvent,
	handler EventHandler,
) error {

	// Apply timeout
	handlerCtx, cancel := context.WithTimeout(ctx, bus.config.EventTimeout)
	defer cancel()

	// Build processor chain
	finalHandler := &finalEventHandler{handler: handler}

	// Apply processors in reverse order
	for i := len(bus.processors) - 1; i >= 0; i-- {
		finalHandler = &processorHandler{
			processor: bus.processors[i],
			next:      finalHandler,
		}
	}

	// Execute with retry policy
	retryPolicy := handler.GetRetryPolicy()
	if retryPolicy == nil {
		retryPolicy = &RetryPolicy{
			MaxAttempts: bus.config.MaxRetryAttempts,
			Backoff:     bus.config.RetryBackoff,
		}
	}

	return bus.executeWithRetry(handlerCtx, func() error {
		return finalHandler.Handle(handlerCtx, event)
	}, retryPolicy)
}

func (bus *EventBus) processAsyncHandlers(
	ctx context.Context,
	event domain.TraceEvent,
	handlers []EventHandler,
) []*AsyncHandlerResult {

	results := make([]*AsyncHandlerResult, len(handlers))
	resultsChan := make(chan *AsyncHandlerResult, len(handlers))

	// Process handlers concurrently
	var wg sync.WaitGroup
	for i, handler := range handlers {
		wg.Add(1)
		go func(index int, h EventHandler) {
			defer wg.Done()

			err := bus.processEventWithHandler(ctx, event, h)
			resultsChan <- &AsyncHandlerResult{
				HandlerName: h.GetHandlerName(),
				Error:       err,
				ProcessedAt: time.Now(),
			}
		}(i, handler)
	}

	// Wait for all handlers to complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results
	i := 0
	for result := range resultsChan {
		results[i] = result
		i++
	}

	return results
}

func (bus *EventBus) getHandlers(eventType EventType) []EventHandler {
	bus.handlersMux.RLock()
	defer bus.handlersMux.RUnlock()

	handlers, exists := bus.handlers[eventType]
	if !exists {
		return nil
	}

	// Return a copy to avoid concurrent access issues
	handlersCopy := make([]EventHandler, len(handlers))
	copy(handlersCopy, handlers)

	return handlersCopy
}

func (bus *EventBus) persistEvent(ctx context.Context, event domain.TraceEvent) error {
	// Create persistence context with timeout
	persistCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return bus.eventStore.AppendEvent(persistCtx, event)
}

func (bus *EventBus) sendToDeadLetterQueue(event domain.TraceEvent, errors []error) {
	if !bus.config.EnableDeadLetterQueue || bus.deadLetterQueue == nil {
		return
	}

	deadLetterEvent := &DeadLetterEvent{
		OriginalEvent: event,
		Errors:        errors,
		Timestamp:     time.Now(),
		Attempts:      bus.config.MaxRetryAttempts,
	}

	bus.deadLetterQueue.Enqueue(deadLetterEvent)
	bus.metrics.RecordDeadLetter(getEventType(event))
}

func (bus *EventBus) groupEventsByType(events []domain.TraceEvent) map[EventType][]domain.TraceEvent {
	groups := make(map[EventType][]domain.TraceEvent)

	for _, event := range events {
		eventType := getEventType(event)
		groups[eventType] = append(groups[eventType], event)
	}

	return groups
}

func (bus *EventBus) publishEventGroup(
	ctx context.Context,
	eventType EventType,
	events []domain.TraceEvent,
) error {

	handlers := bus.getHandlers(eventType)
	if len(handlers) == 0 {
		return nil
	}

	// Process events in group
	for _, event := range events {
		if err := bus.processEventWithHandlers(ctx, event, handlers, time.Now()); err != nil {
			return err
		}
	}

	return nil
}

func (bus *EventBus) executeWithRetry(
	ctx context.Context,
	operation func() error,
	retryPolicy *RetryPolicy,
) error {

	var lastErr error

	for attempt := 0; attempt <= retryPolicy.MaxAttempts; attempt++ {
		if attempt > 0 {
			// Apply backoff
			backoff := time.Duration(attempt) * retryPolicy.Backoff
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		err := operation()
		if err == nil {
			return nil
		}

		lastErr = err

		// Check if error is retryable
		if !isRetryableError(err) {
			break
		}
	}

	return lastErr
}

func (bus *EventBus) initializePools() {
	bus.eventPool = sync.Pool{
		New: func() interface{} {
			return &pooledEvent{}
		},
	}

	bus.bufferPool = sync.Pool{
		New: func() interface{} {
			return make([]byte, 0, 1024)
		},
	}
}

func (bus *EventBus) initializeWorkers() {
	bus.workers = make([]*EventWorker, bus.config.WorkerCount)

	for i := 0; i < bus.config.WorkerCount; i++ {
		worker := NewEventWorker(i, bus.config.BufferSize)
		bus.workers[i] = worker
		bus.workerPool <- worker

		// Start worker
		go worker.Start(bus.shutdownChan)
	}
}

func (bus *EventBus) getWorker() *EventWorker {
	select {
	case worker := <-bus.workerPool:
		return worker
	default:
		return nil // No workers available
	}
}

func (bus *EventBus) returnWorker(worker *EventWorker) {
	select {
	case bus.workerPool <- worker:
	default:
		// Pool is full, worker will be garbage collected
	}
}

func (bus *EventBus) startBackgroundProcessing() {
	ticker := time.NewTicker(bus.config.MetricsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Perform periodic maintenance
			bus.performMaintenance()

		case <-bus.shutdownChan:
			return
		}
	}
}

func (bus *EventBus) performMaintenance() {
	// Clean up dead letter queue
	if bus.deadLetterQueue != nil {
		bus.deadLetterQueue.Cleanup()
	}

	// Update metrics
	if bus.config.EnableMetrics {
		bus.metrics.UpdateSystemMetrics()
	}
}

// Shutdown gracefully shuts down the event bus
func (bus *EventBus) Shutdown(ctx context.Context) error {
	bus.shutdownOnce.Do(func() {
		close(bus.shutdownChan)

		// Wait for workers to finish
		for _, worker := range bus.workers {
			worker.Stop()
		}

		// Flush any remaining events
		bus.flushPendingEvents(ctx)
	})

	return nil
}

func (bus *EventBus) flushPendingEvents(ctx context.Context) {
	// Implementation would flush any buffered events
}

// Supporting types

type EventType string
type SubscriptionID string
type SagaID string
type DeliveryMode int
type PersistenceMode int
type BrokerType int

type Subscription struct {
	ID         SubscriptionID
	Handler    EventHandler
	EventTypes []EventType
	CreatedAt  time.Time
	IsActive   bool
}

type AsyncEventResult struct {
	EventID     string
	Status      AsyncStatus
	ResultChan  chan error
	SubmittedAt time.Time
}

type AsyncHandlerResult struct {
	HandlerName string
	Error       error
	ProcessedAt time.Time
}

type RetryPolicy struct {
	MaxAttempts int
	Backoff     time.Duration
}

type DeadLetterEvent struct {
	OriginalEvent domain.TraceEvent
	Errors        []error
	Timestamp     time.Time
	Attempts      int
}

// Supporting interfaces and types would be defined here...

// Helper functions

func getEventType(event domain.TraceEvent) EventType {
	return EventType(event.GetEventType())
}

func generateSubscriptionID() SubscriptionID {
	return SubscriptionID(fmt.Sprintf("sub_%d", time.Now().UnixNano()))
}

func isRetryableError(err error) bool {
	// Implement logic to determine if error is retryable
	// This is a simplified implementation
	return true
}

func applyEventBusDefaults(config *EventBusConfig) {
	if config.WorkerCount == 0 {
		config.WorkerCount = 10
	}
	if config.BufferSize == 0 {
		config.BufferSize = 1000
	}
	if config.MaxConcurrentEvents == 0 {
		config.MaxConcurrentEvents = 100
	}
	if config.EventTimeout == 0 {
		config.EventTimeout = 30 * time.Second
	}
	if config.AcknowledgmentTimeout == 0 {
		config.AcknowledgmentTimeout = 5 * time.Second
	}
	if config.MaxRetryAttempts == 0 {
		config.MaxRetryAttempts = 3
	}
	if config.RetryBackoff == 0 {
		config.RetryBackoff = 100 * time.Millisecond
	}
	if config.DeadLetterTTL == 0 {
		config.DeadLetterTTL = 24 * time.Hour
	}
	if config.DeadLetterMaxSize == 0 {
		config.DeadLetterMaxSize = 10000
	}
	if config.BatchSize == 0 {
		config.BatchSize = 100
	}
	if config.BatchTimeout == 0 {
		config.BatchTimeout = time.Second
	}
	if config.FailureThreshold == 0 {
		config.FailureThreshold = 5
	}
	if config.RecoveryTimeout == 0 {
		config.RecoveryTimeout = 30 * time.Second
	}
	if config.MetricsInterval == 0 {
		config.MetricsInterval = 30 * time.Second
	}
}

// Additional supporting handlers and types would be defined here...
