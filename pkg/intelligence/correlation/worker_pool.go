package correlation

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// WorkerType represents different types of workers in the correlation engine
type WorkerType int

const (
	// EventWorker processes correlation events
	EventWorker WorkerType = iota
	// StorageWorker processes storage operations
	StorageWorker
)

func (wt WorkerType) String() string {
	switch wt {
	case EventWorker:
		return "event"
	case StorageWorker:
		return "storage"
	default:
		return "unknown"
	}
}

// WorkerPoolManager manages all worker pools in the correlation engine
// This replaces the scattered worker management logic in engine.go
type WorkerPoolManager struct {
	logger *zap.Logger
	tracer trace.Tracer

	// Worker pools
	eventWorkers   *WorkerPool
	storageWorkers *WorkerPool

	// Synchronization
	wg     *sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc

	// Metrics (shared references to engine metrics)
	activeWorkersGauge  metric.Int64UpDownCounter
	storageWorkersGauge metric.Int64UpDownCounter
	queueDepthGauge     metric.Int64UpDownCounter
	storageQueueGauge   metric.Int64UpDownCounter
}

// WorkerPool represents a pool of workers processing a specific type of work
type WorkerPool struct {
	workerType  WorkerType
	workerCount int
	processor   WorkerProcessor
	logger      *zap.Logger
	tracer      trace.Tracer

	// Metrics
	activeGauge metric.Int64UpDownCounter
	queueGauge  metric.Int64UpDownCounter
}

// WorkerProcessor defines the interface for processing work items
type WorkerProcessor interface {
	// ProcessWork handles a single work item
	ProcessWork(ctx context.Context, workItem interface{}) error
	// GetWorkChannel returns the channel to receive work from
	GetWorkChannel() <-chan interface{}
	// GetWorkerType returns the type of worker this processor is for
	GetWorkerType() WorkerType
}

// NewWorkerPoolManager creates a new worker pool manager
func NewWorkerPoolManager(
	logger *zap.Logger,
	eventWorkerCount, storageWorkerCount int,
	metrics *EngineOTELMetrics,
	wg *sync.WaitGroup,
) *WorkerPoolManager {
	ctx, cancel := context.WithCancel(context.Background())

	return &WorkerPoolManager{
		logger: logger,
		tracer: otel.Tracer("correlation-worker-pool"),
		wg:     wg,
		ctx:    ctx,
		cancel: cancel,

		// Extract metrics references
		activeWorkersGauge:  metrics.ActiveWorkersGauge,
		storageWorkersGauge: metrics.StorageWorkersGauge,
		queueDepthGauge:     metrics.QueueDepthGauge,
		storageQueueGauge:   metrics.StorageQueueDepthGauge,
	}
}

// StartWorkerPools starts all worker pools with their respective processors
func (wpm *WorkerPoolManager) StartWorkerPools(
	eventProcessor WorkerProcessor,
	storageProcessor WorkerProcessor,
	eventWorkerCount, storageWorkerCount int,
) error {
	ctx, span := wpm.tracer.Start(wpm.ctx, "worker_pool.start_all")
	defer span.End()

	// Create event worker pool
	wpm.eventWorkers = &WorkerPool{
		workerType:  EventWorker,
		workerCount: eventWorkerCount,
		processor:   eventProcessor,
		logger:      wpm.logger,
		tracer:      wpm.tracer,
		activeGauge: wpm.activeWorkersGauge,
		queueGauge:  wpm.queueDepthGauge,
	}

	// Create storage worker pool
	wpm.storageWorkers = &WorkerPool{
		workerType:  StorageWorker,
		workerCount: storageWorkerCount,
		processor:   storageProcessor,
		logger:      wpm.logger,
		tracer:      wpm.tracer,
		activeGauge: wpm.storageWorkersGauge,
		queueGauge:  wpm.storageQueueGauge,
	}

	// Start event workers
	if err := wpm.startWorkerPool(ctx, wpm.eventWorkers); err != nil {
		span.SetAttributes(attribute.String("error", err.Error()))
		return err
	}

	// Start storage workers (if processor is provided)
	if storageProcessor != nil {
		if err := wpm.startWorkerPool(ctx, wpm.storageWorkers); err != nil {
			span.SetAttributes(attribute.String("error", err.Error()))
			return err
		}
	}

	span.SetAttributes(
		attribute.Int("event_workers", eventWorkerCount),
		attribute.Int("storage_workers", storageWorkerCount),
	)

	wpm.logger.Info("Worker pools started",
		zap.Int("event_workers", eventWorkerCount),
		zap.Int("storage_workers", storageWorkerCount))

	return nil
}

// startWorkerPool starts workers for a specific worker pool
func (wpm *WorkerPoolManager) startWorkerPool(ctx context.Context, pool *WorkerPool) error {
	for i := 0; i < pool.workerCount; i++ {
		wpm.wg.Add(1)
		go wpm.runWorker(pool, i)

		// Update active workers metric
		if pool.activeGauge != nil {
			pool.activeGauge.Add(ctx, 1)
		}
	}

	pool.logger.Info("Worker pool started",
		zap.String("type", pool.workerType.String()),
		zap.Int("worker_count", pool.workerCount))

	return nil
}

// runWorker runs a single worker in a pool
func (wpm *WorkerPoolManager) runWorker(pool *WorkerPool, workerID int) {
	defer wpm.wg.Done()
	defer func() {
		// Decrement active workers on exit
		if pool.activeGauge != nil {
			pool.activeGauge.Add(context.Background(), -1)
		}
	}()

	workerLogger := pool.logger.With(
		zap.String("worker_type", pool.workerType.String()),
		zap.Int("worker_id", workerID),
	)

	workerLogger.Debug("Worker started")

	workChannel := pool.processor.GetWorkChannel()

	// Process work items from the channel
	for {
		select {
		case workItem, ok := <-workChannel:
			if !ok {
				// Channel closed, worker should exit
				workerLogger.Debug("Work channel closed, worker exiting")
				return
			}

			// Process the work item
			if err := wpm.processWorkItem(pool, workerLogger, workItem); err != nil {
				workerLogger.Error("Work item processing failed",
					zap.Error(err),
					zap.Any("work_item", workItem))
			}

		case <-wpm.ctx.Done():
			workerLogger.Debug("Worker context cancelled, exiting")
			return
		}
	}
}

// processWorkItem processes a single work item with tracing and metrics
func (wpm *WorkerPoolManager) processWorkItem(
	pool *WorkerPool,
	logger *zap.Logger,
	workItem interface{},
) error {
	// Create span for work processing
	ctx, span := pool.tracer.Start(wpm.ctx,
		"worker_pool.process_"+pool.workerType.String())
	defer span.End()

	startTime := time.Now()

	// Process the work item
	err := pool.processor.ProcessWork(ctx, workItem)

	duration := time.Since(startTime)

	// Set span attributes
	span.SetAttributes(
		attribute.String("worker.type", pool.workerType.String()),
		attribute.Float64("processing.duration_ms",
			float64(duration.Nanoseconds())/1000000),
	)

	if err != nil {
		span.SetAttributes(
			attribute.String("error", err.Error()),
			attribute.String("error.type", "processing_failed"),
		)
		return err
	}

	return nil
}

// Stop gracefully stops all worker pools
func (wpm *WorkerPoolManager) Stop() {
	_, span := wpm.tracer.Start(context.Background(), "worker_pool.stop_all")
	defer span.End()

	wpm.logger.Info("Stopping worker pools")

	// Cancel context to signal all workers to stop
	wpm.cancel()

	span.SetAttributes(attribute.String("status", "stopped"))

	wpm.logger.Info("Worker pools stopped")
}

// GetStats returns statistics for all worker pools
func (wpm *WorkerPoolManager) GetStats() WorkerPoolStats {
	return WorkerPoolStats{
		EventWorkers: WorkerStats{
			Type:   EventWorker,
			Count:  wpm.getWorkerCount(wpm.eventWorkers),
			Active: true,
		},
		StorageWorkers: WorkerStats{
			Type:   StorageWorker,
			Count:  wpm.getWorkerCount(wpm.storageWorkers),
			Active: wpm.storageWorkers != nil,
		},
	}
}

// getWorkerCount safely gets worker count from a pool
func (wpm *WorkerPoolManager) getWorkerCount(pool *WorkerPool) int {
	if pool == nil {
		return 0
	}
	return pool.workerCount
}

// WorkerPoolStats contains statistics for worker pools
type WorkerPoolStats struct {
	EventWorkers   WorkerStats `json:"event_workers"`
	StorageWorkers WorkerStats `json:"storage_workers"`
}

// WorkerStats contains statistics for a single worker pool
type WorkerStats struct {
	Type   WorkerType `json:"type"`
	Count  int        `json:"count"`
	Active bool       `json:"active"`
}
