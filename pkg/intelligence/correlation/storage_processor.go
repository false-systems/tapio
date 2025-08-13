// Package correlation provides storage processing functionality for async correlation result storage
package correlation

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// StorageProcessor handles async storage operations with worker pool management
type StorageProcessor struct {
	storage            Storage
	logger             *zap.Logger
	timeoutCoordinator *TimeoutCoordinator

	// OTEL instrumentation
	tracer              trace.Tracer
	storageProcessedCtr metric.Int64Counter
	storageErrorsCtr    metric.Int64Counter
	storageLatencyHist  metric.Float64Histogram
	queueDepthGauge     metric.Int64UpDownCounter
	activeWorkersGauge  metric.Int64UpDownCounter

	// Worker pool configuration
	workerCount int
	queueSize   int

	// Job processing
	jobChan chan *StorageJob

	// State management
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	isRunning int32

	// Metrics tracking
	processed int64
	rejected  int64
}

// StorageJob represents a storage operation to be processed
type StorageJob struct {
	Result    *CorrelationResult
	Timestamp time.Time
	Context   context.Context
}

// StorageProcessorConfig contains configuration for the storage processor
type StorageProcessorConfig struct {
	WorkerCount        int
	QueueSize          int
	TimeoutCoordinator *TimeoutCoordinator
}

// DefaultStorageProcessorConfig returns default configuration
func DefaultStorageProcessorConfig() StorageProcessorConfig {
	return StorageProcessorConfig{
		WorkerCount: 10,
		QueueSize:   100,
	}
}

// NewStorageProcessor creates a new storage processor
func NewStorageProcessor(storage Storage, config StorageProcessorConfig, logger *zap.Logger) (*StorageProcessor, error) {
	if storage == nil {
		return nil, fmt.Errorf("storage is required")
	}
	if config.TimeoutCoordinator == nil {
		return nil, fmt.Errorf("timeout coordinator is required")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	// Set defaults
	if config.WorkerCount <= 0 {
		config.WorkerCount = 10
	}
	if config.QueueSize <= 0 {
		config.QueueSize = config.WorkerCount * 10 // 10x workers
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Initialize OTEL components
	tracer := otel.Tracer("correlation.storage")
	meter := otel.Meter("correlation.storage")

	storageProcessedCtr, err := meter.Int64Counter(
		"correlation_storage_processed_total",
		metric.WithDescription("Total correlations processed by storage"),
	)
	if err != nil {
		logger.Warn("Failed to create storage processed counter", zap.Error(err))
	}

	storageErrorsCtr, err := meter.Int64Counter(
		"correlation_storage_errors_total",
		metric.WithDescription("Total storage errors"),
	)
	if err != nil {
		logger.Warn("Failed to create storage errors counter", zap.Error(err))
	}

	storageLatencyHist, err := meter.Float64Histogram(
		"correlation_storage_latency_ms",
		metric.WithDescription("Storage operation latency in milliseconds"),
	)
	if err != nil {
		logger.Warn("Failed to create storage latency histogram", zap.Error(err))
	}

	queueDepthGauge, err := meter.Int64UpDownCounter(
		"correlation_storage_queue_depth",
		metric.WithDescription("Current depth of storage queue"),
	)
	if err != nil {
		logger.Warn("Failed to create queue depth gauge", zap.Error(err))
	}

	activeWorkersGauge, err := meter.Int64UpDownCounter(
		"correlation_storage_active_workers",
		metric.WithDescription("Number of active storage workers"),
	)
	if err != nil {
		logger.Warn("Failed to create active workers gauge", zap.Error(err))
	}

	return &StorageProcessor{
		storage:             storage,
		logger:              logger,
		timeoutCoordinator:  config.TimeoutCoordinator,
		tracer:              tracer,
		storageProcessedCtr: storageProcessedCtr,
		storageErrorsCtr:    storageErrorsCtr,
		storageLatencyHist:  storageLatencyHist,
		queueDepthGauge:     queueDepthGauge,
		activeWorkersGauge:  activeWorkersGauge,
		workerCount:         config.WorkerCount,
		queueSize:           config.QueueSize,
		jobChan:             make(chan *StorageJob, config.QueueSize),
		ctx:                 ctx,
		cancel:              cancel,
	}, nil
}

// Start starts the storage processor worker pool
func (sp *StorageProcessor) Start() error {
	if !atomic.CompareAndSwapInt32(&sp.isRunning, 0, 1) {
		return fmt.Errorf("storage processor is already running")
	}

	sp.logger.Info("Starting storage processor",
		zap.Int("workers", sp.workerCount),
		zap.Int("queue_size", sp.queueSize),
	)

	// Start worker pool
	for i := 0; i < sp.workerCount; i++ {
		sp.wg.Add(1)
		go sp.worker(i)

		// Update active workers metric
		if sp.activeWorkersGauge != nil {
			sp.activeWorkersGauge.Add(context.Background(), 1)
		}
	}

	return nil
}

// Stop stops the storage processor gracefully
func (sp *StorageProcessor) Stop() error {
	if !atomic.CompareAndSwapInt32(&sp.isRunning, 1, 0) {
		return fmt.Errorf("storage processor is not running")
	}

	sp.logger.Info("Stopping storage processor")

	// Cancel context to signal workers to stop
	sp.cancel()

	// Close job channel to drain remaining jobs
	close(sp.jobChan)

	// Wait for all workers to finish
	sp.wg.Wait()

	// Update active workers metric to zero
	if sp.activeWorkersGauge != nil {
		sp.activeWorkersGauge.Add(context.Background(), -int64(sp.workerCount))
	}

	sp.logger.Info("Storage processor stopped",
		zap.Int64("processed", sp.GetProcessedCount()),
		zap.Int64("rejected", sp.GetRejectedCount()),
	)

	return nil
}

// StoreAsync submits a correlation result for async storage
func (sp *StorageProcessor) StoreAsync(ctx context.Context, result *CorrelationResult) error {
	if atomic.LoadInt32(&sp.isRunning) == 0 {
		return fmt.Errorf("storage processor is not running")
	}

	if result == nil {
		return fmt.Errorf("result is nil")
	}

	// Create a copy to avoid data races
	resultCopy := *result

	// Create storage job
	job := &StorageJob{
		Result:    &resultCopy,
		Timestamp: time.Now(),
		Context:   ctx,
	}

	select {
	case sp.jobChan <- job:
		// Successfully queued
		if sp.queueDepthGauge != nil {
			sp.queueDepthGauge.Add(ctx, 1)
		}
		return nil

	case <-ctx.Done():
		// Context cancelled
		atomic.AddInt64(&sp.rejected, 1)
		return fmt.Errorf("context cancelled while queuing storage job: %w", ctx.Err())

	case <-sp.ctx.Done():
		// Processor is shutting down
		atomic.AddInt64(&sp.rejected, 1)
		return fmt.Errorf("storage processor is shutting down")

	default:
		// Queue is full
		atomic.AddInt64(&sp.rejected, 1)
		sp.logger.Warn("Storage queue full, dropping correlation",
			zap.String("correlation_id", result.ID),
			zap.Int("queue_capacity", cap(sp.jobChan)),
		)
		return fmt.Errorf("storage queue is full")
	}
}

// worker processes storage jobs from the queue
func (sp *StorageProcessor) worker(id int) {
	defer sp.wg.Done()
	defer func() {
		// Decrement active workers on exit
		if sp.activeWorkersGauge != nil {
			sp.activeWorkersGauge.Add(context.Background(), -1)
		}
	}()

	sp.logger.Debug("Storage worker started", zap.Int("worker_id", id))

	for {
		select {
		case job, ok := <-sp.jobChan:
			if !ok {
				// Channel closed, worker should exit
				sp.logger.Debug("Storage worker stopped", zap.Int("worker_id", id))
				return
			}

			// Process the job
			sp.processJob(job)

			// Update queue depth metric
			if sp.queueDepthGauge != nil {
				sp.queueDepthGauge.Add(context.Background(), -1)
			}

		case <-sp.ctx.Done():
			// Processor is shutting down
			sp.logger.Debug("Storage worker stopped", zap.Int("worker_id", id))
			return
		}
	}
}

// processJob handles a single storage operation
func (sp *StorageProcessor) processJob(job *StorageJob) {
	// Create span for storage operation
	ctx, span := sp.tracer.Start(context.Background(), "correlation.storage.process_job")
	defer span.End()

	startTime := time.Now()
	queueLatency := startTime.Sub(job.Timestamp).Seconds() * 1000 // Convert to milliseconds

	// Set span attributes
	span.SetAttributes(
		attribute.String("correlation.id", job.Result.ID),
		attribute.String("correlation.type", job.Result.Type),
		attribute.Float64("queue.latency_ms", queueLatency),
	)

	// Use timeout coordinator for storage operations
	storageOperation := func() error {
		storageCtx := sp.timeoutCoordinator.CreateStorageContext(ctx)
		defer storageCtx.Cancel()

		return sp.storage.Store(storageCtx.Context, job.Result)
	}

	err := sp.timeoutCoordinator.WaitWithTimeout(ctx, sp.ctx, StorageLevel, storageOperation)
	if err != nil {
		// Handle storage error
		sp.handleStorageError(ctx, span, job, err, queueLatency)
	} else {
		// Handle storage success
		sp.handleStorageSuccess(ctx, span, job, startTime, queueLatency)
	}
}

// handleStorageError processes storage operation errors
func (sp *StorageProcessor) handleStorageError(ctx context.Context, span trace.Span, job *StorageJob, err error, queueLatency float64) {
	// Determine error type
	errorType := "storage_failed"
	if sp.timeoutCoordinator.IsTimeoutError(err) {
		errorType = "storage_timeout"
	} else if err.Error() == "engine is shutting down" {
		errorType = "engine_shutdown"
	}

	// Record error in span
	span.SetAttributes(
		attribute.String("error", err.Error()),
		attribute.String("error.type", errorType),
	)

	// Record error metrics
	if sp.storageErrorsCtr != nil {
		sp.storageErrorsCtr.Add(ctx, 1, metric.WithAttributes(
			attribute.String("error_type", errorType),
			attribute.String("correlation_type", job.Result.Type),
		))
	}

	// Log error
	sp.logger.Error("Failed to store correlation",
		zap.String("correlation_id", job.Result.ID),
		zap.String("error_type", errorType),
		zap.Float64("queue_latency_ms", queueLatency),
		zap.Error(err),
	)
}

// handleStorageSuccess processes successful storage operations
func (sp *StorageProcessor) handleStorageSuccess(ctx context.Context, span trace.Span, job *StorageJob, startTime time.Time, queueLatency float64) {
	// Update processed count
	atomic.AddInt64(&sp.processed, 1)

	// Record success metrics
	if sp.storageProcessedCtr != nil {
		sp.storageProcessedCtr.Add(ctx, 1, metric.WithAttributes(
			attribute.String("correlation_type", job.Result.Type),
			attribute.String("status", "success"),
		))
	}

	// Record storage latency
	storageLatency := time.Since(startTime).Seconds() * 1000 // Convert to milliseconds
	if sp.storageLatencyHist != nil {
		sp.storageLatencyHist.Record(ctx, storageLatency, metric.WithAttributes(
			attribute.String("correlation_type", job.Result.Type),
			attribute.Float64("queue_latency_ms", queueLatency),
		))
	}

	// Set span attributes for success
	span.SetAttributes(
		attribute.Float64("storage.latency_ms", storageLatency),
		attribute.Float64("total.latency_ms", storageLatency+queueLatency),
		attribute.String("status", "success"),
	)

	sp.logger.Debug("Correlation stored successfully",
		zap.String("correlation_id", job.Result.ID),
		zap.Float64("storage_latency_ms", storageLatency),
		zap.Float64("queue_latency_ms", queueLatency),
	)
}

// GetProcessedCount returns the total number of processed correlations
func (sp *StorageProcessor) GetProcessedCount() int64 {
	return atomic.LoadInt64(&sp.processed)
}

// GetRejectedCount returns the total number of rejected correlations
func (sp *StorageProcessor) GetRejectedCount() int64 {
	return atomic.LoadInt64(&sp.rejected)
}

// GetQueueDepth returns the current queue depth
func (sp *StorageProcessor) GetQueueDepth() int {
	return len(sp.jobChan)
}

// GetQueueCapacity returns the queue capacity
func (sp *StorageProcessor) GetQueueCapacity() int {
	return cap(sp.jobChan)
}

// IsRunning returns true if the processor is running
func (sp *StorageProcessor) IsRunning() bool {
	return atomic.LoadInt32(&sp.isRunning) == 1
}

// Health returns the health status of the storage processor
func (sp *StorageProcessor) Health() StorageProcessorHealth {
	queueDepth := sp.GetQueueDepth()
	queueCapacity := sp.GetQueueCapacity()
	queueUtilization := float64(queueDepth) / float64(queueCapacity)

	return StorageProcessorHealth{
		IsRunning:        sp.IsRunning(),
		WorkerCount:      sp.workerCount,
		QueueDepth:       queueDepth,
		QueueCapacity:    queueCapacity,
		QueueUtilization: queueUtilization,
		ProcessedCount:   sp.GetProcessedCount(),
		RejectedCount:    sp.GetRejectedCount(),
		IsHealthy:        sp.IsRunning() && queueUtilization < 0.9, // Consider unhealthy if queue is 90%+ full
	}
}

// StorageProcessorHealth represents the health status of the storage processor
type StorageProcessorHealth struct {
	IsRunning        bool    `json:"is_running"`
	WorkerCount      int     `json:"worker_count"`
	QueueDepth       int     `json:"queue_depth"`
	QueueCapacity    int     `json:"queue_capacity"`
	QueueUtilization float64 `json:"queue_utilization"`
	ProcessedCount   int64   `json:"processed_count"`
	RejectedCount    int64   `json:"rejected_count"`
	IsHealthy        bool    `json:"is_healthy"`
}
