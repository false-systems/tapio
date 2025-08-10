package correlation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Engine orchestrates all correlators
type Engine struct {
	logger *zap.Logger

	// OTEL instrumentation - REQUIRED fields
	tracer               trace.Tracer
	eventsProcessedCtr   metric.Int64Counter
	errorsTotalCtr       metric.Int64Counter
	processingTimeHist   metric.Float64Histogram
	correlationsFoundCtr metric.Int64Counter
	queueDepthGauge      metric.Int64UpDownCounter
	activeWorkersGauge   metric.Int64UpDownCounter

	// Correlators
	correlators []Correlator

	// Storage
	storage Storage

	// Event processing
	eventChan  chan *domain.UnifiedEvent
	resultChan chan *CorrelationResult

	// Configuration
	config EngineConfig

	// State
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Metrics
	mu                sync.RWMutex
	eventsProcessed   int64
	correlationsFound int64
}

// EngineConfig defined in config.go - removing duplicate

// NewEngine creates a new correlation engine
func NewEngine(logger *zap.Logger, config EngineConfig, k8sClient domain.K8sClient, storage Storage) (*Engine, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Initialize OTEL components - MANDATORY pattern
	tracer := otel.Tracer("correlation-engine")
	meter := otel.Meter("correlation-engine")

	// Create metrics with descriptive names and descriptions
	eventsProcessedCtr, err := meter.Int64Counter(
		"correlation_events_processed_total",
		metric.WithDescription("Total events processed by correlation engine"),
	)
	if err != nil {
		logger.Warn("Failed to create events counter", zap.Error(err))
	}

	errorsTotalCtr, err := meter.Int64Counter(
		"correlation_errors_total",
		metric.WithDescription("Total errors in correlation engine"),
	)
	if err != nil {
		logger.Warn("Failed to create errors counter", zap.Error(err))
	}

	processingTimeHist, err := meter.Float64Histogram(
		"correlation_processing_duration_ms",
		metric.WithDescription("Processing duration for correlation engine in milliseconds"),
	)
	if err != nil {
		logger.Warn("Failed to create processing time histogram", zap.Error(err))
	}

	correlationsFoundCtr, err := meter.Int64Counter(
		"correlation_correlations_found_total",
		metric.WithDescription("Total correlations found by correlation engine"),
	)
	if err != nil {
		logger.Warn("Failed to create correlations found counter", zap.Error(err))
	}

	queueDepthGauge, err := meter.Int64UpDownCounter(
		"correlation_queue_depth",
		metric.WithDescription("Current depth of event processing queue"),
	)
	if err != nil {
		logger.Warn("Failed to create queue depth gauge", zap.Error(err))
	}

	activeWorkersGauge, err := meter.Int64UpDownCounter(
		"correlation_active_workers",
		metric.WithDescription("Number of active correlation workers"),
	)
	if err != nil {
		logger.Warn("Failed to create active workers gauge", zap.Error(err))
	}

	engine := &Engine{
		logger:               logger,
		tracer:               tracer,
		eventsProcessedCtr:   eventsProcessedCtr,
		errorsTotalCtr:       errorsTotalCtr,
		processingTimeHist:   processingTimeHist,
		correlationsFoundCtr: correlationsFoundCtr,
		queueDepthGauge:      queueDepthGauge,
		activeWorkersGauge:   activeWorkersGauge,
		correlators:          make([]Correlator, 0),
		storage:              storage,
		eventChan:            make(chan *domain.UnifiedEvent, config.EventBufferSize),
		resultChan:           make(chan *CorrelationResult, config.ResultBufferSize),
		config:               config,
		ctx:                  ctx,
		cancel:               cancel,
	}

	if err := engine.initializeCorrelators(ctx, logger, k8sClient, config); err != nil {
		cancel()
		return nil, err
	}

	logger.Info("Correlation engine created",
		zap.Int("correlators", len(engine.correlators)),
		zap.Strings("enabled", config.EnabledCorrelators),
	)

	return engine, nil
}

// initializeCorrelators sets up all enabled correlators
func (e *Engine) initializeCorrelators(ctx context.Context, logger *zap.Logger, k8sClient domain.K8sClient, config EngineConfig) error {
	for _, name := range config.EnabledCorrelators {
		if err := e.addCorrelator(ctx, name, logger, k8sClient); err != nil {
			return err
		}
	}
	return nil
}

// addCorrelator creates and adds a specific correlator type
func (e *Engine) addCorrelator(ctx context.Context, name string, logger *zap.Logger, k8sClient domain.K8sClient) error {
	switch name {
	case "k8s":
		return e.addK8sCorrelator(ctx, logger, k8sClient)
	case "temporal":
		e.correlators = append(e.correlators, NewTemporalCorrelator(logger, *TestTemporalConfig()))
	case "sequence":
		e.correlators = append(e.correlators, NewSequenceCorrelator(logger, *TestSequenceConfig()))
	case "performance":
		e.correlators = append(e.correlators, NewPerformanceCorrelator(logger))
	case "servicemap":
		e.correlators = append(e.correlators, NewServiceMapCorrelator(logger))
	default:
		logger.Warn("Unknown correlator in config", zap.String("name", name))
	}
	return nil
}

// addK8sCorrelator adds and starts a K8s correlator
func (e *Engine) addK8sCorrelator(ctx context.Context, logger *zap.Logger, k8sClient domain.K8sClient) error {
	if k8sClient == nil {
		return nil
	}

	k8sCorrelator := NewK8sCorrelator(logger, k8sClient)
	e.correlators = append(e.correlators, k8sCorrelator)

	if err := k8sCorrelator.Start(ctx); err != nil {
		return fmt.Errorf("failed to start K8s correlator: %w", err)
	}
	return nil
}

// Start begins processing events
func (e *Engine) Start(ctx context.Context) error {
	// Always start spans for operations
	ctx, span := e.tracer.Start(ctx, "correlation.engine.start")
	defer span.End()

	// Set span attributes for debugging
	span.SetAttributes(
		attribute.String("component", "correlation-engine"),
		attribute.String("operation", "start"),
		attribute.Int("workers", e.config.WorkerCount),
		attribute.Int("event_buffer", e.config.EventBufferSize),
	)

	e.logger.Info("Starting correlation engine",
		zap.Int("workers", e.config.WorkerCount),
		zap.Int("event_buffer", e.config.EventBufferSize),
	)

	// Start worker goroutines
	for i := 0; i < e.config.WorkerCount; i++ {
		e.wg.Add(1)
		go e.worker(i)
		// Update active workers metric
		if e.activeWorkersGauge != nil {
			e.activeWorkersGauge.Add(ctx, 1)
		}
	}

	// Start storage cleanup routine
	e.wg.Add(1)
	go e.storageCleanup()

	// Start metrics reporter
	e.wg.Add(1)
	go e.metricsReporter()

	return nil
}

// Stop gracefully shuts down the engine
func (e *Engine) Stop() error {
	// Always start spans for operations
	ctx, span := e.tracer.Start(context.Background(), "correlation.engine.stop")
	defer span.End()

	e.logger.Info("Stopping correlation engine")

	// Cancel context to signal shutdown
	e.cancel()

	// Close input channel
	close(e.eventChan)

	// Wait for workers to finish
	e.wg.Wait()

	// Reset active workers metric
	if e.activeWorkersGauge != nil {
		e.activeWorkersGauge.Add(ctx, -int64(e.config.WorkerCount))
	}

	// Close output channel
	close(e.resultChan)

	// Set final metrics in span
	span.SetAttributes(
		attribute.Int64("events.processed", e.eventsProcessed),
		attribute.Int64("correlations.found", e.correlationsFound),
	)

	e.logger.Info("Correlation engine stopped",
		zap.Int64("events_processed", e.eventsProcessed),
		zap.Int64("correlations_found", e.correlationsFound),
	)

	return nil
}

// Process submits an event for correlation processing
func (e *Engine) Process(ctx context.Context, event *domain.UnifiedEvent) error {
	// Always start spans for operations
	ctx, span := e.tracer.Start(ctx, "correlation.engine.process")
	defer span.End()

	if event == nil {
		err := fmt.Errorf("event is nil")
		span.SetAttributes(attribute.String("error", err.Error()))
		// Record error metrics
		if e.errorsTotalCtr != nil {
			e.errorsTotalCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "nil_event"),
			))
		}
		return err
	}

	// Set span attributes for debugging
	span.SetAttributes(
		attribute.String("component", "correlation-engine"),
		attribute.String("operation", "process_event"),
		attribute.String("event.type", string(event.Type)),
		attribute.String("event.id", event.ID),
	)

	// Record queue depth
	if e.queueDepthGauge != nil {
		e.queueDepthGauge.Add(ctx, 1)
		defer e.queueDepthGauge.Add(ctx, -1)
	}

	// Use a timeout to prevent indefinite blocking
	timer := time.NewTimer(e.config.ProcessingTimeout)
	defer timer.Stop()

	select {
	case e.eventChan <- event:
		return nil
	case <-timer.C:
		err := fmt.Errorf("timeout sending event to processing queue")
		span.SetAttributes(
			attribute.String("error", err.Error()),
			attribute.String("error.type", "queue_timeout"),
		)
		// Record error metrics
		if e.errorsTotalCtr != nil {
			e.errorsTotalCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "queue_timeout"),
				attribute.String("event_type", string(event.Type)),
			))
		}
		return err
	case <-ctx.Done():
		span.SetAttributes(attribute.String("error", "context_cancelled"))
		return ctx.Err()
	case <-e.ctx.Done():
		span.SetAttributes(attribute.String("error", "engine_shutdown"))
		return fmt.Errorf("engine is shutting down")
	}
}

// Results returns the channel of correlation results
func (e *Engine) Results() <-chan *CorrelationResult {
	return e.resultChan
}

// worker processes events from the queue
func (e *Engine) worker(id int) {
	defer e.wg.Done()
	defer func() {
		// Decrement active workers on exit
		if e.activeWorkersGauge != nil {
			e.activeWorkersGauge.Add(context.Background(), -1)
		}
	}()

	e.logger.Debug("Correlation worker started", zap.Int("worker_id", id))

	for event := range e.eventChan {
		select {
		case <-e.ctx.Done():
			return
		default:
			e.processEvent(event)
		}
	}

	e.logger.Debug("Correlation worker stopped", zap.Int("worker_id", id))
}

// processEvent runs an event through all correlators
func (e *Engine) processEvent(event *domain.UnifiedEvent) {
	// Create span for event processing
	ctx, span := e.tracer.Start(context.Background(), "correlation.engine.process_event")
	defer span.End()

	startTime := time.Now()
	defer func() {
		// Record processing time
		duration := time.Since(startTime).Seconds() * 1000 // Convert to milliseconds
		if e.processingTimeHist != nil {
			e.processingTimeHist.Record(ctx, duration, metric.WithAttributes(
				attribute.String("event_type", string(event.Type)),
			))
		}
	}()

	// Set span attributes
	span.SetAttributes(
		attribute.String("event.type", string(event.Type)),
		attribute.String("event.id", event.ID),
		attribute.Int("correlators.count", len(e.correlators)),
	)

	// Update processing metrics
	e.incrementProcessedEvents(ctx)

	// Process through each correlator
	for _, correlator := range e.correlators {
		e.processWithCorrelator(ctx, event, correlator)
	}

	// Monitor processing performance
	e.checkProcessingPerformance(event.ID, startTime)
}

// incrementProcessedEvents safely increments the events processed counter
func (e *Engine) incrementProcessedEvents(ctx context.Context) {
	e.mu.Lock()
	e.eventsProcessed++
	e.mu.Unlock()

	// Record success metrics
	if e.eventsProcessedCtr != nil {
		e.eventsProcessedCtr.Add(ctx, 1, metric.WithAttributes(
			attribute.String("status", "success"),
		))
	}
}

// processWithCorrelator processes an event with a single correlator
func (e *Engine) processWithCorrelator(parentCtx context.Context, event *domain.UnifiedEvent, correlator Correlator) {
	// Create span for correlator processing
	ctx, span := e.tracer.Start(parentCtx, fmt.Sprintf("correlation.%s.process", correlator.Name()))
	defer span.End()

	// Set span attributes
	span.SetAttributes(
		attribute.String("correlator", correlator.Name()),
		attribute.String("event.id", event.ID),
	)

	// Create timeout context for correlator
	ctx, cancel := context.WithTimeout(ctx, DefaultProcessingTimeout)
	defer cancel()

	// Process event
	results, err := correlator.Process(ctx, event)
	if err != nil {
		// Record error in span
		span.SetAttributes(
			attribute.String("error", err.Error()),
			attribute.String("error.type", "correlator_failed"),
		)
		// Record error metrics
		if e.errorsTotalCtr != nil {
			e.errorsTotalCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "correlator_failed"),
				attribute.String("correlator", correlator.Name()),
				attribute.String("event_type", string(event.Type)),
			))
		}
		e.logCorrelatorError(correlator.Name(), event.ID, err)
		return
	}

	// Set result count in span
	span.SetAttributes(attribute.Int("results.count", len(results)))

	// Handle results
	e.handleCorrelatorResults(ctx, results)
}

// handleCorrelatorResults processes and stores correlation results
func (e *Engine) handleCorrelatorResults(ctx context.Context, results []*CorrelationResult) {
	for _, result := range results {
		if result != nil {
			e.sendResult(ctx, result)

			// Store result asynchronously
			if e.storage != nil {
				e.asyncStoreResult(ctx, result)
			}
		}
	}
}

// logCorrelatorError logs an error from a correlator
func (e *Engine) logCorrelatorError(correlatorName, eventID string, err error) {
	e.logger.Error("Correlator error",
		zap.String("correlator", correlatorName),
		zap.String("event_id", eventID),
		zap.Error(err),
	)
}

// checkProcessingPerformance logs if processing was slow
func (e *Engine) checkProcessingPerformance(eventID string, startTime time.Time) {
	duration := time.Since(startTime)
	if duration > SlowProcessingThreshold {
		e.logger.Warn("Slow event processing",
			zap.String("event_id", eventID),
			zap.Duration("duration", duration),
		)
	}
}

// sendResult sends a correlation result to the output channel
func (e *Engine) sendResult(ctx context.Context, result *CorrelationResult) {
	// Update metrics
	e.mu.Lock()
	e.correlationsFound++
	e.mu.Unlock()

	// Record correlation found metric
	if e.correlationsFoundCtr != nil {
		e.correlationsFoundCtr.Add(ctx, 1, metric.WithAttributes(
			attribute.String("correlation_type", result.Type),
			attribute.Float64("confidence", result.Confidence),
		))
	}

	// Try to send, but don't block
	select {
	case e.resultChan <- result:
		// Success
	case <-e.ctx.Done():
		// Shutting down
	default:
		// Channel full, log and drop
		e.logger.Warn("Result channel full, dropping correlation",
			zap.String("correlation_id", result.ID),
			zap.String("type", result.Type),
		)
		// Record dropped correlation
		if e.errorsTotalCtr != nil {
			e.errorsTotalCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "result_dropped"),
				attribute.String("correlation_type", result.Type),
			))
		}
	}
}

// storageCleanup periodically cleans old correlations
func (e *Engine) storageCleanup() {
	defer e.wg.Done()

	if e.storage == nil {
		return
	}

	ticker := time.NewTicker(e.config.StorageCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := e.storage.Cleanup(e.ctx, e.config.StorageRetention); err != nil {
				e.logger.Error("Storage cleanup failed", zap.Error(err))
			}
		case <-e.ctx.Done():
			return
		}
	}
}

// asyncStoreResult stores a correlation result asynchronously
func (e *Engine) asyncStoreResult(parentCtx context.Context, result *CorrelationResult) {
	// Create a copy of the result to avoid data races
	resultCopy := *result

	// Store in a goroutine to avoid blocking event processing
	go func() {
		// Create span for storage operation
		ctx, span := e.tracer.Start(context.Background(), "correlation.storage.store")
		defer span.End()

		// Set span attributes
		span.SetAttributes(
			attribute.String("correlation.id", resultCopy.ID),
			attribute.String("correlation.type", resultCopy.Type),
		)

		// Use a timeout context for storage operations
		storeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		if err := e.storage.Store(storeCtx, &resultCopy); err != nil {
			// Record error in span
			span.SetAttributes(
				attribute.String("error", err.Error()),
				attribute.String("error.type", "storage_failed"),
			)
			// Record error metrics
			if e.errorsTotalCtr != nil {
				e.errorsTotalCtr.Add(ctx, 1, metric.WithAttributes(
					attribute.String("error_type", "storage_failed"),
					attribute.String("operation", "store_correlation"),
				))
			}
			// Log error but don't block processing
			e.logger.Error("Failed to store correlation asynchronously",
				zap.String("correlation_id", resultCopy.ID),
				zap.Error(err),
			)
		}
	}()
}

// metricsReporter periodically logs metrics
func (e *Engine) metricsReporter() {
	defer e.wg.Done()

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	var lastEvents, lastCorrelations int64
	lastReport := time.Now()

	for {
		select {
		case <-ticker.C:
			e.mu.RLock()
			events := e.eventsProcessed
			correlations := e.correlationsFound
			e.mu.RUnlock()

			// Calculate rates
			duration := time.Since(lastReport)
			eventRate := float64(events-lastEvents) / duration.Seconds()
			correlationRate := float64(correlations-lastCorrelations) / duration.Seconds()

			e.logger.Info("Correlation engine metrics",
				zap.Int64("total_events", events),
				zap.Int64("total_correlations", correlations),
				zap.Float64("events_per_sec", eventRate),
				zap.Float64("correlations_per_sec", correlationRate),
				zap.Int("event_queue", len(e.eventChan)),
				zap.Int("result_queue", len(e.resultChan)),
			)

			lastEvents = events
			lastCorrelations = correlations
			lastReport = time.Now()

		case <-e.ctx.Done():
			return
		}
	}
}

// GetMetrics returns current engine metrics
// Returns a properly typed MetricsData struct instead of map[string]interface{}
// This complies with CLAUDE.md requirement: "No map[string]interface{} in public APIs"
func (e *Engine) GetMetrics() MetricsData {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return MetricsData{
		EventsProcessed:   e.eventsProcessed,
		CorrelationsFound: e.correlationsFound,
		EventQueueSize:    len(e.eventChan),
		ResultQueueSize:   len(e.resultChan),
		CorrelatorsCount:  len(e.correlators),
		WorkersCount:      e.config.WorkerCount,
		LastReportTime:    time.Now(),
		IsHealthy:         e.ctx.Err() == nil,
		Status:            "running",
	}
}

// GetDetailedMetrics returns comprehensive engine metrics
// This provides more detailed metrics for monitoring and debugging
func (e *Engine) GetDetailedMetrics() EngineMetrics {
	e.mu.RLock()
	defer e.mu.RUnlock()

	metrics := EngineMetrics{
		MetricsData: MetricsData{
			EventsProcessed:   e.eventsProcessed,
			CorrelationsFound: e.correlationsFound,
			EventQueueSize:    len(e.eventChan),
			ResultQueueSize:   len(e.resultChan),
			CorrelatorsCount:  len(e.correlators),
			WorkersCount:      e.config.WorkerCount,
			LastReportTime:    time.Now(),
			IsHealthy:         e.ctx.Err() == nil,
			Status:            "running",
		},
	}

	return metrics
}
