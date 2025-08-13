package correlation

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"github.com/yairfalse/tapio/pkg/domain"
)

// EventProcessor handles correlation event processing
// This replaces the processEvent method in engine.go
type EventProcessor struct {
	logger      *zap.Logger
	correlators []Correlator
	eventChan   <-chan *domain.UnifiedEvent
	resultChan  chan<- *CorrelationResult
	storage     Storage
	metrics     *EngineOTELMetrics

	// For async storage
	storageJobChan chan<- *storageJob
}

// NewEventProcessor creates a new event processor
func NewEventProcessor(
	logger *zap.Logger,
	correlators []Correlator,
	eventChan <-chan *domain.UnifiedEvent,
	resultChan chan<- *CorrelationResult,
	storage Storage,
	storageJobChan chan<- *storageJob,
	metrics *EngineOTELMetrics,
) *EventProcessor {
	return &EventProcessor{
		logger:         logger,
		correlators:    correlators,
		eventChan:      eventChan,
		resultChan:     resultChan,
		storage:        storage,
		storageJobChan: storageJobChan,
		metrics:        metrics,
	}
}

// ProcessWork implements WorkerProcessor for event processing
func (ep *EventProcessor) ProcessWork(ctx context.Context, workItem interface{}) error {
	event, ok := workItem.(*domain.UnifiedEvent)
	if !ok {
		return fmt.Errorf("invalid work item type: expected *domain.UnifiedEvent, got %T", workItem)
	}

	return ep.processEvent(ctx, event)
}

// GetWorkChannel implements WorkerProcessor
func (ep *EventProcessor) GetWorkChannel() <-chan interface{} {
	// Convert the typed channel to interface{} channel
	ch := make(chan interface{}, cap(ep.eventChan))

	go func() {
		defer close(ch)
		for event := range ep.eventChan {
			ch <- event
		}
	}()

	return ch
}

// GetWorkerType implements WorkerProcessor
func (ep *EventProcessor) GetWorkerType() WorkerType {
	return EventWorker
}

// processEvent runs an event through all correlators (extracted from engine.go)
func (ep *EventProcessor) processEvent(ctx context.Context, event *domain.UnifiedEvent) error {
	startTime := time.Now()
	defer func() {
		// Record processing time
		duration := time.Since(startTime).Seconds() * 1000 // Convert to milliseconds
		if ep.metrics.ProcessingTimeHist != nil {
			ep.metrics.ProcessingTimeHist.Record(ctx, duration, metric.WithAttributes(
				attribute.String("event_type", string(event.Type)),
			))
		}
	}()

	// Update processing metrics
	ep.incrementProcessedEvents(ctx)

	// Process through each correlator
	for _, correlator := range ep.correlators {
		if err := ep.processWithCorrelator(ctx, event, correlator); err != nil {
			ep.logger.Error("Correlator processing failed",
				zap.String("correlator", correlator.Name()),
				zap.String("event_id", event.ID),
				zap.Error(err))
			// Continue with other correlators instead of failing the entire event
		}
	}

	return nil
}

// incrementProcessedEvents safely increments the events processed counter
func (ep *EventProcessor) incrementProcessedEvents(ctx context.Context) {
	// Record success metrics
	if ep.metrics.EventsProcessedCtr != nil {
		ep.metrics.EventsProcessedCtr.Add(ctx, 1, metric.WithAttributes(
			attribute.String("status", "success"),
		))
	}
}

// processWithCorrelator processes an event with a single correlator
func (ep *EventProcessor) processWithCorrelator(parentCtx context.Context, event *domain.UnifiedEvent, correlator Correlator) error {
	// Create timeout context for correlator
	ctx, cancel := context.WithTimeout(parentCtx, DefaultProcessingTimeout)
	defer cancel()

	// Process event
	results, err := correlator.Process(ctx, event)
	if err != nil {
		// Record error metrics
		if ep.metrics.ErrorsTotalCtr != nil {
			ep.metrics.ErrorsTotalCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "correlator_failed"),
				attribute.String("correlator", correlator.Name()),
				attribute.String("event_type", string(event.Type)),
			))
		}
		return err
	}

	// Handle results
	ep.handleCorrelatorResults(ctx, results)
	return nil
}

// handleCorrelatorResults processes and stores correlation results
func (ep *EventProcessor) handleCorrelatorResults(ctx context.Context, results []*CorrelationResult) {
	for _, result := range results {
		if result != nil {
			ep.sendResult(ctx, result)

			// Store result asynchronously
			if ep.storage != nil && ep.storageJobChan != nil {
				ep.asyncStoreResult(ctx, result)
			}
		}
	}
}

// sendResult sends a correlation result to the output channel
func (ep *EventProcessor) sendResult(ctx context.Context, result *CorrelationResult) {
	// Record correlation found metric
	if ep.metrics.CorrelationsFoundCtr != nil {
		ep.metrics.CorrelationsFoundCtr.Add(ctx, 1, metric.WithAttributes(
			attribute.String("correlation_type", result.Type),
			attribute.Float64("confidence", result.Confidence),
		))
	}

	// Try to send, but don't block
	select {
	case ep.resultChan <- result:
		// Success
	default:
		// Channel full, log and drop
		ep.logger.Warn("Result channel full, dropping correlation",
			zap.String("correlation_id", result.ID),
			zap.String("type", result.Type),
		)
		// Record dropped correlation
		if ep.metrics.ErrorsTotalCtr != nil {
			ep.metrics.ErrorsTotalCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "result_dropped"),
				attribute.String("correlation_type", result.Type),
			))
		}
	}
}

// asyncStoreResult stores a correlation result asynchronously using the worker pool
func (ep *EventProcessor) asyncStoreResult(ctx context.Context, result *CorrelationResult) {
	// Create a copy of the result to avoid data races
	resultCopy := *result

	// Create storage job
	job := &storageJob{
		result:    &resultCopy,
		timestamp: time.Now(),
	}

	// Update queue depth metric
	if ep.metrics.StorageQueueDepthGauge != nil {
		ep.metrics.StorageQueueDepthGauge.Add(ctx, 1)
	}

	// Try to submit job to storage worker pool
	select {
	case ep.storageJobChan <- job:
		// Job accepted
	default:
		// Queue full, record rejection
		if ep.metrics.StorageRejectedCtr != nil {
			ep.metrics.StorageRejectedCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("correlation_type", result.Type),
				attribute.String("reason", "queue_full"),
			))
		}

		if ep.metrics.StorageQueueDepthGauge != nil {
			ep.metrics.StorageQueueDepthGauge.Add(ctx, -1)
		}

		ep.logger.Warn("Storage queue full, dropping correlation",
			zap.String("correlation_id", result.ID),
			zap.String("correlation_type", result.Type),
			zap.Int("queue_size", len(ep.storageJobChan)),
		)
	}
}

// StorageProcessor handles storage operations
// This replaces the processStorageJob method in engine.go
type StorageProcessor struct {
	logger         *zap.Logger
	storage        Storage
	storageJobChan <-chan *storageJob
	metrics        *EngineOTELMetrics
}

// NewStorageProcessor creates a new storage processor
func NewStorageProcessor(
	logger *zap.Logger,
	storage Storage,
	storageJobChan <-chan *storageJob,
	metrics *EngineOTELMetrics,
) *StorageProcessor {
	return &StorageProcessor{
		logger:         logger,
		storage:        storage,
		storageJobChan: storageJobChan,
		metrics:        metrics,
	}
}

// ProcessWork implements WorkerProcessor for storage processing
func (sp *StorageProcessor) ProcessWork(ctx context.Context, workItem interface{}) error {
	job, ok := workItem.(*storageJob)
	if !ok {
		return fmt.Errorf("invalid work item type: expected *storageJob, got %T", workItem)
	}

	return sp.processStorageJob(ctx, job)
}

// GetWorkChannel implements WorkerProcessor
func (sp *StorageProcessor) GetWorkChannel() <-chan interface{} {
	// Convert the typed channel to interface{} channel
	ch := make(chan interface{}, cap(sp.storageJobChan))

	go func() {
		defer close(ch)
		for job := range sp.storageJobChan {
			ch <- job
		}
	}()

	return ch
}

// GetWorkerType implements WorkerProcessor
func (sp *StorageProcessor) GetWorkerType() WorkerType {
	return StorageWorker
}

// processStorageJob handles a single storage operation (extracted from engine.go)
func (sp *StorageProcessor) processStorageJob(ctx context.Context, job *storageJob) error {
	startTime := time.Now()
	queueLatency := startTime.Sub(job.timestamp).Seconds() * 1000 // Convert to milliseconds

	// Use a timeout context for storage operations
	storeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := sp.storage.Store(storeCtx, job.result); err != nil {
		// Record error metrics
		if sp.metrics.ErrorsTotalCtr != nil {
			sp.metrics.ErrorsTotalCtr.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "storage_failed"),
				attribute.String("operation", "store_correlation"),
			))
		}
		// Log error
		sp.logger.Error("Failed to store correlation",
			zap.String("correlation_id", job.result.ID),
			zap.Error(err),
		)
		return err
	}

	// Success - update metrics
	if sp.metrics.StorageProcessedCtr != nil {
		sp.metrics.StorageProcessedCtr.Add(ctx, 1, metric.WithAttributes(
			attribute.String("correlation_type", job.result.Type),
			attribute.String("status", "success"),
		))
	}

	// Record storage latency
	storageLatency := time.Since(startTime).Seconds() * 1000 // Convert to milliseconds
	if sp.metrics.StorageLatencyHist != nil {
		sp.metrics.StorageLatencyHist.Record(ctx, storageLatency, metric.WithAttributes(
			attribute.String("correlation_type", job.result.Type),
			attribute.Float64("queue_latency_ms", queueLatency),
		))
	}

	return nil
}
