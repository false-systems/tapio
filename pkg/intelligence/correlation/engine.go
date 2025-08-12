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
	tracer  trace.Tracer
	metrics *EngineOTELMetrics

	// Timeout coordination
	timeoutCoordinator *TimeoutCoordinator

	// Correlators
	correlators []Correlator
	registry    *CorrelatorRegistry

	// Correlation pipeline
	pipeline *CorrelationPipeline

	// Storage
	storage Storage

	// Event processing
	eventChan  chan *domain.UnifiedEvent
	resultChan chan *CorrelationResult

	// Storage worker pool
	storageJobChan chan *storageJob
	storageWorkers int

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
	storageProcessed  int64
	storageRejected   int64
}

// storageJob represents a storage operation to be processed by the worker pool
type storageJob struct {
	result    *CorrelationResult
	timestamp time.Time
}

// EngineConfig defined in config.go - removing duplicate

// NewEngine creates a new correlation engine
func NewEngine(logger *zap.Logger, config EngineConfig, k8sClient domain.K8sClient, storage Storage) (*Engine, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Initialize OTEL components - MANDATORY pattern
	tracer := otel.Tracer("correlation-engine")

	// Create metrics using factory pattern - reduces complexity from 150+ lines to ~20 lines
	metricFactory := NewMetricFactory("correlation-engine", logger)
	engineMetrics, err := metricFactory.CreateEngineMetrics()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create engine metrics: %w", err)
	}

	// Determine storage worker count (default to 10 if not specified)
	storageWorkers := 10
	if config.StorageWorkerCount > 0 {
		storageWorkers = config.StorageWorkerCount
	}

	// Calculate storage job queue size (2x workers or minimum 100)
	storageQueueSize := storageWorkers * 2
	if storageQueueSize < 100 {
		storageQueueSize = 100
	}
	if config.StorageQueueSize > 0 {
		storageQueueSize = config.StorageQueueSize
	}

	// Create correlator registry
	registry := NewCorrelatorRegistry(logger)

	// Create timeout coordinator with configuration
	timeoutConfig := TimeoutConfig{
		ProcessingTimeout: config.ProcessingTimeout,
		CorrelatorTimeout: config.ProcessingTimeout,
		StorageTimeout:    DefaultStorageTimeout,
		QueueTimeout:      config.ProcessingTimeout,
	}
	timeoutCoordinator := NewTimeoutCoordinator(logger, tracer, timeoutConfig)

	engine := &Engine{
		logger:             logger,
		tracer:             tracer,
		metrics:            engineMetrics,
		timeoutCoordinator: timeoutCoordinator,
		correlators:        make([]Correlator, 0),
		registry:           registry,
		storage:            storage,
		eventChan:          make(chan *domain.UnifiedEvent, config.EventBufferSize),
		resultChan:         make(chan *CorrelationResult, config.ResultBufferSize),
		storageJobChan:     make(chan *storageJob, storageQueueSize),
		storageWorkers:     storageWorkers,
		config:             config,
		ctx:                ctx,
		cancel:             cancel,
	}

	if err := engine.initializeCorrelators(ctx, logger, k8sClient, config); err != nil {
		cancel()
		return nil, err
	}

	// Create correlation pipeline with result handler and error aggregator
	if err := engine.createCorrelationPipeline(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create correlation pipeline: %w", err)
	}

	logger.Info("Correlation engine created",
		zap.Int("correlators", len(engine.correlators)),
		zap.Strings("enabled", config.EnabledCorrelators),
	)

	return engine, nil
}

// initializeCorrelators sets up all enabled correlators using the registry
func (e *Engine) initializeCorrelators(ctx context.Context, logger *zap.Logger, k8sClient domain.K8sClient, config EngineConfig) error {
	for _, name := range config.EnabledCorrelators {
		correlator, err := e.registry.Create(ctx, name, logger, k8sClient)
		if err != nil {
			logger.Warn("Failed to create correlator, skipping",
				zap.String("name", name),
				zap.Error(err))
			continue
		}

		e.correlators = append(e.correlators, correlator)
		logger.Debug("Correlator initialized",
			zap.String("name", name),
			zap.String("type", fmt.Sprintf("%T", correlator)))
	}
	return nil
}

// createCorrelationPipeline creates and configures the correlation pipeline
func (e *Engine) createCorrelationPipeline() error {
	if len(e.correlators) == 0 {
		// No correlators available - create a no-op pipeline that just logs
		e.pipeline = nil
		e.logger.Warn("No correlators available - pipeline processing disabled")
		return nil
	}

	// Create result handler that uses the existing result processing logic
	resultHandler := NewDefaultResultHandler(e.resultChan, e.logger)

	// Create error aggregator for collecting correlator errors
	errorAggregator := NewSimpleErrorAggregator(e.logger)

	// Configure pipeline for sequential processing (can be made configurable later)
	pipelineConfig := &PipelineConfig{
		Mode:               PipelineModeSequential, // Start with sequential for consistency
		MaxConcurrency:     len(e.correlators),     // Allow all correlators to run concurrently in parallel mode
		TimeoutCoordinator: e.timeoutCoordinator,
		ResultHandler:      resultHandler,
		ErrorAggregator:    errorAggregator,
	}

	var err error
	e.pipeline, err = NewCorrelationPipeline(e.correlators, pipelineConfig, e.logger)
	if err != nil {
		return fmt.Errorf("failed to create correlation pipeline: %w", err)
	}

	e.logger.Debug("Correlation pipeline created",
		zap.Int("correlators", len(e.correlators)),
		zap.String("mode", "sequential"),
	)

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
		if e.metrics.ActiveWorkersGauge != nil {
			e.metrics.ActiveWorkersGauge.Add(ctx, 1)
		}
	}

	// Start storage worker pool
	if e.storage != nil {
		for i := 0; i < e.storageWorkers; i++ {
			e.wg.Add(1)
			go e.storageWorker(i)
			// Update storage workers metric
			if e.metrics.StorageWorkersGauge != nil {
				e.metrics.StorageWorkersGauge.Add(ctx, 1)
			}
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

	// Close storage job channel to signal storage workers to stop
	if e.storage != nil {
		close(e.storageJobChan)
	}

	// Wait for workers to finish
	e.wg.Wait()

	// Reset active workers metric
	if e.metrics.ActiveWorkersGauge != nil {
		e.metrics.ActiveWorkersGauge.Add(ctx, -int64(e.config.WorkerCount))
	}

	// Reset storage workers metric
	if e.metrics.StorageWorkersGauge != nil && e.storage != nil {
		e.metrics.StorageWorkersGauge.Add(ctx, -int64(e.storageWorkers))
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
		if e.metrics.ErrorsTotalCtr != nil {
			e.metrics.ErrorsTotalCtr.Add(ctx, 1, metric.WithAttributes(
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
	if e.metrics.QueueDepthGauge != nil {
		e.metrics.QueueDepthGauge.Add(ctx, 1)
		defer e.metrics.QueueDepthGauge.Add(ctx, -1)
	}

	// Use timeout coordinator for queue operation
	queueOperation := func() error {
		select {
		case e.eventChan <- event:
			return nil
		default:
			return fmt.Errorf("queue is full")
		}
	}

	err := e.timeoutCoordinator.WaitWithTimeout(ctx, e.ctx, QueueLevel, queueOperation)
	if err != nil {
		// Set span attributes based on error type
		if e.timeoutCoordinator.IsTimeoutError(err) {
			span.SetAttributes(
				attribute.String("error", err.Error()),
				attribute.String("error.type", "queue_timeout"),
			)
			// Record timeout metrics
			if e.metrics.ErrorsTotalCtr != nil {
				e.metrics.ErrorsTotalCtr.Add(ctx, 1, metric.WithAttributes(
					attribute.String("error_type", "queue_timeout"),
					attribute.String("event_type", string(event.Type)),
				))
			}
		} else if err.Error() == "engine is shutting down" {
			span.SetAttributes(attribute.String("error", "engine_shutdown"))
		} else {
			span.SetAttributes(attribute.String("error", "context_cancelled"))
		}
		return err
	}

	return nil
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
		if e.metrics.ActiveWorkersGauge != nil {
			e.metrics.ActiveWorkersGauge.Add(context.Background(), -1)
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
		if e.metrics.ProcessingTimeHist != nil {
			e.metrics.ProcessingTimeHist.Record(ctx, duration, metric.WithAttributes(
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

	// Process event through correlation pipeline (if available)
	if e.pipeline != nil {
		if err := e.pipeline.Process(ctx, event); err != nil {
			// Log pipeline error but don't stop processing
			e.logger.Error("Pipeline processing failed",
				zap.String("event_id", event.ID),
				zap.Error(err),
			)

			// Record pipeline error in metrics
			if e.metrics.ErrorsTotalCtr != nil {
				e.metrics.ErrorsTotalCtr.Add(ctx, 1, metric.WithAttributes(
					attribute.String("error_type", "pipeline_failed"),
					attribute.String("event_type", string(event.Type)),
				))
			}
		}
	} else {
		// No pipeline available - log and skip processing
		e.logger.Debug("No correlation pipeline available, skipping event processing",
			zap.String("event_id", event.ID),
		)
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
	if e.metrics.EventsProcessedCtr != nil {
		e.metrics.EventsProcessedCtr.Add(ctx, 1, metric.WithAttributes(
			attribute.String("status", "success"),
		))
	}
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
	if e.metrics.CorrelationsFoundCtr != nil {
		e.metrics.CorrelationsFoundCtr.Add(ctx, 1, metric.WithAttributes(
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
		if e.metrics.ErrorsTotalCtr != nil {
			e.metrics.ErrorsTotalCtr.Add(ctx, 1, metric.WithAttributes(
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

// asyncStoreResult stores a correlation result asynchronously using the worker pool
func (e *Engine) asyncStoreResult(ctx context.Context, result *CorrelationResult) {
	// Create a copy of the result to avoid data races
	resultCopy := *result

	// Create storage job
	job := &storageJob{
		result:    &resultCopy,
		timestamp: time.Now(),
	}

	// Update queue depth metric
	if e.metrics.StorageQueueDepthGauge != nil {
		e.metrics.StorageQueueDepthGauge.Add(ctx, 1)
	}

	// Try to submit job to storage worker pool
	select {
	case e.storageJobChan <- job:
		// Job accepted
	case <-e.ctx.Done():
		// Engine shutting down
		if e.metrics.StorageQueueDepthGauge != nil {
			e.metrics.StorageQueueDepthGauge.Add(ctx, -1)
		}
	default:
		// Queue full, record rejection
		e.mu.Lock()
		e.storageRejected++
		e.mu.Unlock()

		if e.metrics.StorageRejectedCtr != nil {
			e.metrics.StorageRejectedCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("correlation_type", result.Type),
				attribute.String("reason", "queue_full"),
			))
		}

		if e.metrics.StorageQueueDepthGauge != nil {
			e.metrics.StorageQueueDepthGauge.Add(ctx, -1)
		}

		e.logger.Warn("Storage queue full, dropping correlation",
			zap.String("correlation_id", result.ID),
			zap.String("correlation_type", result.Type),
			zap.Int("queue_size", len(e.storageJobChan)),
			zap.Int("queue_capacity", cap(e.storageJobChan)),
		)
	}
}

// storageWorker processes storage jobs from the queue
func (e *Engine) storageWorker(id int) {
	defer e.wg.Done()
	defer func() {
		// Decrement storage workers on exit
		if e.metrics.StorageWorkersGauge != nil {
			e.metrics.StorageWorkersGauge.Add(context.Background(), -1)
		}
	}()

	e.logger.Debug("Storage worker started", zap.Int("worker_id", id))

	for job := range e.storageJobChan {
		// Process the storage job
		e.processStorageJob(job)

		// Update queue depth metric
		if e.metrics.StorageQueueDepthGauge != nil {
			e.metrics.StorageQueueDepthGauge.Add(context.Background(), -1)
		}
	}

	e.logger.Debug("Storage worker stopped", zap.Int("worker_id", id))
}

// processStorageJob handles a single storage operation
func (e *Engine) processStorageJob(job *storageJob) {
	// Create span for storage operation
	ctx, span := e.tracer.Start(context.Background(), "correlation.storage.process_job")
	defer span.End()

	startTime := time.Now()
	queueLatency := startTime.Sub(job.timestamp).Seconds() * 1000 // Convert to milliseconds

	// Set span attributes
	span.SetAttributes(
		attribute.String("correlation.id", job.result.ID),
		attribute.String("correlation.type", job.result.Type),
		attribute.Float64("queue.latency_ms", queueLatency),
	)

	// Use timeout coordinator for storage operations
	storageOperation := func() error {
		storageCtx := e.timeoutCoordinator.CreateStorageContext(ctx)
		defer storageCtx.Cancel()

		return e.storage.Store(storageCtx.Context, job.result)
	}

	err := e.timeoutCoordinator.WaitWithTimeout(ctx, e.ctx, StorageLevel, storageOperation)
	if err != nil {
		// Determine error type
		errorType := "storage_failed"
		if e.timeoutCoordinator.IsTimeoutError(err) {
			errorType = "storage_timeout"
		}

		// Record error in span
		span.SetAttributes(
			attribute.String("error", err.Error()),
			attribute.String("error.type", errorType),
		)
		// Record error metrics
		if e.metrics.ErrorsTotalCtr != nil {
			e.metrics.ErrorsTotalCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", errorType),
				attribute.String("operation", "store_correlation"),
			))
		}
		// Log error
		e.logger.Error("Failed to store correlation",
			zap.String("correlation_id", job.result.ID),
			zap.Error(err),
		)
	} else {
		// Success - update metrics
		e.mu.Lock()
		e.storageProcessed++
		e.mu.Unlock()

		if e.metrics.StorageProcessedCtr != nil {
			e.metrics.StorageProcessedCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("correlation_type", job.result.Type),
				attribute.String("status", "success"),
			))
		}
	}

	// Record storage latency
	storageLatency := time.Since(startTime).Seconds() * 1000 // Convert to milliseconds
	if e.metrics.StorageLatencyHist != nil {
		e.metrics.StorageLatencyHist.Record(ctx, storageLatency, metric.WithAttributes(
			attribute.String("correlation_type", job.result.Type),
			attribute.Float64("queue_latency_ms", queueLatency),
		))
	}

	span.SetAttributes(
		attribute.Float64("storage.latency_ms", storageLatency),
		attribute.Float64("total.latency_ms", storageLatency+queueLatency),
	)
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

			e.mu.RLock()
			storageProcessed := e.storageProcessed
			storageRejected := e.storageRejected
			e.mu.RUnlock()

			e.logger.Info("Correlation engine metrics",
				zap.Int64("total_events", events),
				zap.Int64("total_correlations", correlations),
				zap.Float64("events_per_sec", eventRate),
				zap.Float64("correlations_per_sec", correlationRate),
				zap.Int("event_queue", len(e.eventChan)),
				zap.Int("result_queue", len(e.resultChan)),
				zap.Int("storage_queue", len(e.storageJobChan)),
				zap.Int64("storage_processed", storageProcessed),
				zap.Int64("storage_rejected", storageRejected),
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

	storageQueueSize := 0
	if e.storage != nil {
		storageQueueSize = len(e.storageJobChan)
	}

	return MetricsData{
		EventsProcessed:   e.eventsProcessed,
		CorrelationsFound: e.correlationsFound,
		EventQueueSize:    len(e.eventChan),
		ResultQueueSize:   len(e.resultChan),
		StorageQueueSize:  storageQueueSize,
		StorageProcessed:  e.storageProcessed,
		StorageRejected:   e.storageRejected,
		CorrelatorsCount:  len(e.correlators),
		WorkersCount:      e.config.WorkerCount,
		StorageWorkers:    e.storageWorkers,
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

	storageQueueSize := 0
	if e.storage != nil {
		storageQueueSize = len(e.storageJobChan)
	}

	metrics := EngineMetrics{
		MetricsData: MetricsData{
			EventsProcessed:   e.eventsProcessed,
			CorrelationsFound: e.correlationsFound,
			EventQueueSize:    len(e.eventChan),
			ResultQueueSize:   len(e.resultChan),
			StorageQueueSize:  storageQueueSize,
			StorageProcessed:  e.storageProcessed,
			StorageRejected:   e.storageRejected,
			CorrelatorsCount:  len(e.correlators),
			WorkersCount:      e.config.WorkerCount,
			StorageWorkers:    e.storageWorkers,
			LastReportTime:    time.Now(),
			IsHealthy:         e.ctx.Err() == nil,
			Status:            "running",
		},
	}

	return metrics
}

// HealthCheck performs comprehensive health check of the correlation engine
// Returns error if any critical component is unhealthy
func (e *Engine) HealthCheck(ctx context.Context) error {
	// Always start spans for operations
	ctx, span := e.tracer.Start(ctx, "correlation.engine.health_check")
	defer span.End()

	// Check engine state
	if e.ctx.Err() != nil {
		err := fmt.Errorf("engine is not running: %w", e.ctx.Err())
		span.SetAttributes(attribute.String("error", err.Error()))
		return err
	}

	// Check storage health if available
	if e.storage != nil {
		if healthChecker, ok := e.storage.(interface{ HealthCheck(context.Context) error }); ok {
			if err := healthChecker.HealthCheck(ctx); err != nil {
				span.SetAttributes(
					attribute.String("error", err.Error()),
					attribute.String("error.component", "storage"),
				)
				return fmt.Errorf("storage health check failed: %w", err)
			}
		}
	}

	// Check correlator health
	for _, correlator := range e.correlators {
		if healthChecker, ok := correlator.(interface{ Health(context.Context) error }); ok {
			if err := healthChecker.Health(ctx); err != nil {
				span.SetAttributes(
					attribute.String("error", err.Error()),
					attribute.String("error.component", "correlator"),
					attribute.String("correlator.name", correlator.Name()),
				)
				return fmt.Errorf("correlator %s health check failed: %w", correlator.Name(), err)
			}
		}
	}

	// Check queue health
	e.mu.RLock()
	eventQueueLen := len(e.eventChan)
	resultQueueLen := len(e.resultChan)
	storageQueueLen := 0
	if e.storageJobChan != nil {
		storageQueueLen = len(e.storageJobChan)
	}
	e.mu.RUnlock()

	// Check for queue overflow conditions
	eventQueueCap := cap(e.eventChan)
	if eventQueueCap > 0 && float64(eventQueueLen)/float64(eventQueueCap) > 0.9 {
		err := fmt.Errorf("event queue near capacity: %d/%d (%.1f%%)",
			eventQueueLen, eventQueueCap, float64(eventQueueLen)/float64(eventQueueCap)*100)
		span.SetAttributes(
			attribute.String("error", err.Error()),
			attribute.String("error.component", "event_queue"),
		)
		return err
	}

	resultQueueCap := cap(e.resultChan)
	if resultQueueCap > 0 && float64(resultQueueLen)/float64(resultQueueCap) > 0.9 {
		err := fmt.Errorf("result queue near capacity: %d/%d (%.1f%%)",
			resultQueueLen, resultQueueCap, float64(resultQueueLen)/float64(resultQueueCap)*100)
		span.SetAttributes(
			attribute.String("error", err.Error()),
			attribute.String("error.component", "result_queue"),
		)
		return err
	}

	if e.storageJobChan != nil {
		storageQueueCap := cap(e.storageJobChan)
		if storageQueueCap > 0 && float64(storageQueueLen)/float64(storageQueueCap) > 0.9 {
			err := fmt.Errorf("storage queue near capacity: %d/%d (%.1f%%)",
				storageQueueLen, storageQueueCap, float64(storageQueueLen)/float64(storageQueueCap)*100)
			span.SetAttributes(
				attribute.String("error", err.Error()),
				attribute.String("error.component", "storage_queue"),
			)
			return err
		}
	}

	// Set health check success attributes
	span.SetAttributes(
		attribute.String("health.status", "healthy"),
		attribute.Int("health.correlators", len(e.correlators)),
		attribute.Int("health.event_queue_size", eventQueueLen),
		attribute.Int("health.result_queue_size", resultQueueLen),
		attribute.Int("health.storage_queue_size", storageQueueLen),
	)

	return nil
}

// IsHealthy returns a quick health status without deep checks
func (e *Engine) IsHealthy() bool {
	return e.ctx.Err() == nil
}

// GetHealthStatus returns detailed health information
func (e *Engine) GetHealthStatus(ctx context.Context) HealthStatus {
	status := HealthStatus{
		Timestamp:    time.Now(),
		IsHealthy:    e.IsHealthy(),
		Component:    "correlation-engine",
		Version:      "1.0.0",
		Dependencies: make(map[string]DependencyHealth),
	}

	// Check storage dependency
	if e.storage != nil {
		status.Dependencies["storage"] = DependencyHealth{
			Name:      "correlation-storage",
			IsHealthy: true,
			Message:   "Connected",
		}
		if healthChecker, ok := e.storage.(interface{ HealthCheck(context.Context) error }); ok {
			if err := healthChecker.HealthCheck(ctx); err != nil {
				status.Dependencies["storage"] = DependencyHealth{
					Name:      "correlation-storage",
					IsHealthy: false,
					Message:   err.Error(),
				}
				status.IsHealthy = false
			}
		}
	}

	// Check correlators
	for _, correlator := range e.correlators {
		depName := fmt.Sprintf("correlator-%s", correlator.Name())
		status.Dependencies[depName] = DependencyHealth{
			Name:      correlator.Name(),
			IsHealthy: true,
			Message:   "Running",
		}
		if healthChecker, ok := correlator.(interface{ Health(context.Context) error }); ok {
			if err := healthChecker.Health(ctx); err != nil {
				status.Dependencies[depName] = DependencyHealth{
					Name:      correlator.Name(),
					IsHealthy: false,
					Message:   err.Error(),
				}
				status.IsHealthy = false
			}
		}
	}

	// Add queue health
	e.mu.RLock()
	eventQueueLen := len(e.eventChan)
	resultQueueLen := len(e.resultChan)
	storageQueueLen := 0
	if e.storageJobChan != nil {
		storageQueueLen = len(e.storageJobChan)
	}
	e.mu.RUnlock()

	status.QueueHealth = QueueHealth{
		EventQueue: QueueStatus{
			Size:     eventQueueLen,
			Capacity: cap(e.eventChan),
			Usage:    float64(eventQueueLen) / float64(cap(e.eventChan)) * 100,
		},
		ResultQueue: QueueStatus{
			Size:     resultQueueLen,
			Capacity: cap(e.resultChan),
			Usage:    float64(resultQueueLen) / float64(cap(e.resultChan)) * 100,
		},
	}

	if e.storageJobChan != nil {
		status.QueueHealth.StorageQueue = &QueueStatus{
			Size:     storageQueueLen,
			Capacity: cap(e.storageJobChan),
			Usage:    float64(storageQueueLen) / float64(cap(e.storageJobChan)) * 100,
		}
	}

	return status
}
