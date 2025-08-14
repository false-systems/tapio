package loader

import (
	"context"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// startBatchAggregator starts the batch aggregation goroutine
func (l *Loader) startBatchAggregator(ctx context.Context) {
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		l.runBatchAggregator(ctx)
	}()
}

// runBatchAggregator aggregates observation events into batches for processing
func (l *Loader) runBatchAggregator(ctx context.Context) {
	ctx, span := l.tracer.Start(ctx, "loader.batch_aggregator")
	defer span.End()

	l.logger.Info("Starting batch aggregator",
		zap.Int("batch_size", l.config.BatchSize),
		zap.Duration("batch_timeout", l.config.BatchTimeout))

	var currentBatch []*domain.ObservationEvent
	batchTimer := time.NewTimer(l.config.BatchTimeout)
	defer batchTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			l.logger.Info("Batch aggregator context cancelled")
			// Process any remaining events in the current batch
			if len(currentBatch) > 0 {
				l.submitBatch(ctx, currentBatch)
			}
			return

		case event, ok := <-l.batchChannel:
			if !ok {
				l.logger.Info("Batch channel closed")
				// Process any remaining events
				if len(currentBatch) > 0 {
					l.submitBatch(ctx, currentBatch)
				}
				return
			}

			// Add event to current batch
			currentBatch = append(currentBatch, event)

			// Check if batch is full
			if len(currentBatch) >= l.config.BatchSize {
				l.submitBatch(ctx, currentBatch)
				currentBatch = make([]*domain.ObservationEvent, 0, l.config.BatchSize)

				// Reset timer
				if !batchTimer.Stop() {
					<-batchTimer.C
				}
				batchTimer.Reset(l.config.BatchTimeout)
			}

		case <-batchTimer.C:
			// Batch timeout reached
			if len(currentBatch) > 0 {
				l.logger.Debug("Batch timeout reached, submitting partial batch",
					zap.Int("batch_size", len(currentBatch)))
				l.submitBatch(ctx, currentBatch)
				currentBatch = make([]*domain.ObservationEvent, 0, l.config.BatchSize)
			}
			batchTimer.Reset(l.config.BatchTimeout)
		}
	}
}

// submitBatch creates a batch job and submits it to the worker pool
func (l *Loader) submitBatch(ctx context.Context, events []*domain.ObservationEvent) {
	if len(events) == 0 {
		return
	}

	ctx, span := l.tracer.Start(ctx, "loader.submit_batch")
	defer span.End()

	batch := &BatchJob{
		ID:        l.generateBatchID(),
		Events:    events,
		CreatedAt: time.Now(),
		Retries:   0,
	}

	span.SetAttributes(
		attribute.String("batch.id", batch.ID),
		attribute.Int("batch.size", len(events)),
	)

	select {
	case l.jobQueue <- batch:
		l.logger.Debug("Submitted batch for processing",
			zap.String("batch_id", batch.ID),
			zap.Int("batch_size", len(events)))
	case <-ctx.Done():
		l.logger.Warn("Context cancelled while submitting batch",
			zap.String("batch_id", batch.ID))
	case <-time.After(5 * time.Second):
		l.logger.Error("Timeout submitting batch to job queue",
			zap.String("batch_id", batch.ID),
			zap.Int("batch_size", len(events)))

		// Record batch submission failure
		if l.batchesFailed != nil {
			l.batchesFailed.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", "submission_timeout"),
			))
		}
	}
}

// startWorkers starts the worker goroutines for processing batches
func (l *Loader) startWorkers(ctx context.Context) {
	l.logger.Info("Starting batch workers", zap.Int("worker_count", l.config.MaxConcurrency))

	// Start message fetcher
	l.startMessageFetcher(ctx)

	// Start worker goroutines
	for i := 0; i < l.config.MaxConcurrency; i++ {
		l.wg.Add(1)
		go func(workerID int) {
			defer l.wg.Done()
			l.worker(ctx, workerID)
		}(i)
	}
}

// worker processes batch jobs from the job queue
func (l *Loader) worker(ctx context.Context, workerID int) {
	ctx, span := l.tracer.Start(ctx, "loader.worker")
	defer span.End()

	span.SetAttributes(attribute.Int("worker.id", workerID))

	l.logger.Debug("Worker started", zap.Int("worker_id", workerID))

	// Increment active workers counter
	if l.activeWorkers != nil {
		l.activeWorkers.Add(ctx, 1)
		defer l.activeWorkers.Add(ctx, -1)
	}

	for {
		select {
		case <-ctx.Done():
			l.logger.Debug("Worker context cancelled", zap.Int("worker_id", workerID))
			return

		case job, ok := <-l.jobQueue:
			if !ok {
				l.logger.Debug("Job queue closed", zap.Int("worker_id", workerID))
				return
			}

			// Acquire worker slot
			select {
			case l.workerPool <- struct{}{}:
				// Process the job
				result := l.processBatch(ctx, job)
				l.handleBatchResult(ctx, job, result)

				// Release worker slot
				<-l.workerPool
			case <-ctx.Done():
				return
			}
		}
	}
}

// processBatch processes a batch of observation events
func (l *Loader) processBatch(ctx context.Context, job *BatchJob) *ProcessingResult {
	ctx, span := l.tracer.Start(ctx, "loader.process_batch")
	defer span.End()

	start := time.Now()

	span.SetAttributes(
		attribute.String("batch.id", job.ID),
		attribute.Int("batch.size", len(job.Events)),
		attribute.Int("batch.retries", job.Retries),
	)

	l.logger.Debug("Processing batch",
		zap.String("batch_id", job.ID),
		zap.Int("batch_size", len(job.Events)),
		zap.Int("retries", job.Retries))

	result := &ProcessingResult{
		BatchID:        job.ID,
		ProcessingTime: time.Since(start),
	}

	// Process batch with timeout
	batchCtx, cancel := context.WithTimeout(ctx, l.config.ProcessTimeout)
	defer cancel()

	// Store events in Neo4j
	stats, err := l.storeObservationEvents(batchCtx, job.Events)
	if err != nil {
		result.Success = false
		result.Error = err
		span.SetStatus(codes.Error, err.Error())

		l.logger.Error("Failed to store batch",
			zap.String("batch_id", job.ID),
			zap.Error(err),
			zap.Duration("processing_time", result.ProcessingTime))
	} else {
		result.Success = true
		result.EventsProcessed = len(job.Events)
		result.NodesCreated = stats.NodesCreated
		result.RelationshipsCreated = stats.RelationshipsCreated
		span.SetStatus(codes.Ok, "Batch processed successfully")

		l.logger.Debug("Successfully processed batch",
			zap.String("batch_id", job.ID),
			zap.Int("events_processed", result.EventsProcessed),
			zap.Int64("nodes_created", result.NodesCreated),
			zap.Int64("relationships_created", result.RelationshipsCreated),
			zap.Duration("processing_time", result.ProcessingTime))
	}

	result.ProcessingTime = time.Since(start)

	// Record storage latency
	if l.storageLatency != nil {
		l.storageLatency.Record(ctx, result.ProcessingTime.Seconds()*1000, metric.WithAttributes(
			attribute.Bool("success", result.Success),
			attribute.Int("batch_size", len(job.Events)),
		))
	}

	span.SetAttributes(
		attribute.Bool("result.success", result.Success),
		attribute.Int("result.events_processed", result.EventsProcessed),
		attribute.Int64("result.nodes_created", result.NodesCreated),
		attribute.Int64("result.relationships_created", result.RelationshipsCreated),
		attribute.Float64("result.processing_time_ms", result.ProcessingTime.Seconds()*1000),
	)

	return result
}

// handleBatchResult handles the result of batch processing
func (l *Loader) handleBatchResult(ctx context.Context, job *BatchJob, result *ProcessingResult) {
	if result.Success {
		// Record successful batch processing
		if l.batchesProcessed != nil {
			l.batchesProcessed.Add(ctx, 1, metric.WithAttributes(
				attribute.Int("batch_size", len(job.Events)),
			))
		}

		// Update metrics
		l.updateMetrics(func(m *LoaderMetrics) {
			m.BatchesProcessed++
			m.EventsProcessed += int64(result.EventsProcessed)
			m.LastProcessedTime = time.Now()

			// Update throughput calculation
			if m.EventsReceived > 0 {
				duration := time.Since(m.LastProcessedTime).Seconds()
				if duration > 0 {
					m.ThroughputPerSecond = float64(m.EventsProcessed) / duration
				}
			}
		})

		return
	}

	// Handle batch failure
	if l.batchesFailed != nil {
		l.batchesFailed.Add(ctx, 1, metric.WithAttributes(
			attribute.String("error_type", "storage_failed"),
			attribute.Int("batch_size", len(job.Events)),
			attribute.Int("retries", job.Retries),
		))
	}

	// Update failure metrics
	l.updateMetrics(func(m *LoaderMetrics) {
		m.BatchesFailed++
		m.EventsFailed += int64(len(job.Events))

		// Update error rate
		if m.EventsReceived > 0 {
			m.ErrorRate = float64(m.EventsFailed) / float64(m.EventsReceived)
		}
	})

	// Retry logic
	if job.Retries < l.config.MaxRetries {
		job.Retries++

		// Calculate retry delay with exponential backoff
		delay := l.calculateRetryDelay(job.Retries)

		l.logger.Warn("Retrying failed batch",
			zap.String("batch_id", job.ID),
			zap.Int("retry_attempt", job.Retries),
			zap.Duration("retry_delay", delay),
			zap.Error(result.Error))

		// Schedule retry
		time.AfterFunc(delay, func() {
			select {
			case l.jobQueue <- job:
				// Successfully queued for retry
			default:
				l.logger.Error("Failed to queue batch for retry - queue full",
					zap.String("batch_id", job.ID))
			}
		})
	} else {
		l.logger.Error("Batch failed permanently after max retries",
			zap.String("batch_id", job.ID),
			zap.Int("max_retries", l.config.MaxRetries),
			zap.Error(result.Error))
	}
}

// calculateRetryDelay calculates retry delay with exponential backoff
func (l *Loader) calculateRetryDelay(retryAttempt int) time.Duration {
	baseDelay := l.config.RetryBackoff

	// Exponential backoff: delay = baseDelay * 2^(retryAttempt-1)
	delay := time.Duration(int64(baseDelay) * (1 << (retryAttempt - 1)))

	// Cap at maximum backoff
	if delay > l.config.MaxRetryBackoff {
		delay = l.config.MaxRetryBackoff
	}

	return delay
}
