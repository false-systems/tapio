// Package correlation provides correlation pipeline functionality for processing events through multiple correlators
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

// PipelineMode defines how correlators are executed
type PipelineMode int

const (
	// PipelineModeSequential executes correlators one after another
	PipelineModeSequential PipelineMode = iota
	// PipelineModeParallel executes all correlators concurrently
	PipelineModeParallel
)

// CorrelationPipeline manages the execution of multiple correlators for event processing
type CorrelationPipeline struct {
	correlators []Correlator
	mode        PipelineMode
	logger      *zap.Logger

	// OTEL instrumentation
	tracer             trace.Tracer
	pipelineProcessed  metric.Int64Counter
	pipelineErrors     metric.Int64Counter
	pipelineDuration   metric.Float64Histogram
	correlatorDuration metric.Float64Histogram

	// Configuration
	maxConcurrency     int
	timeoutCoordinator *TimeoutCoordinator

	// Result handling
	resultHandler   ResultHandler
	errorAggregator ErrorAggregator

	mu sync.RWMutex
}

// ResultHandler defines how correlation results are processed
type ResultHandler interface {
	HandleResults(ctx context.Context, results []*CorrelationResult) error
}

// ErrorAggregator defines how errors from correlators are aggregated
type ErrorAggregator interface {
	RecordError(correlatorName, eventID string, err error)
	GetErrors() []PipelineError
	Reset()
}

// PipelineError represents an error from a specific correlator in the pipeline
type PipelineError struct {
	CorrelatorName string
	EventID        string
	Error          error
	Timestamp      time.Time
	ErrorType      string
}

// PipelineConfig contains configuration for the correlation pipeline
type PipelineConfig struct {
	Mode               PipelineMode
	MaxConcurrency     int
	TimeoutCoordinator *TimeoutCoordinator
	ResultHandler      ResultHandler
	ErrorAggregator    ErrorAggregator
}

// DefaultResultHandler provides a simple result handler implementation
type DefaultResultHandler struct {
	resultsChan chan<- *CorrelationResult
	storage     interface{} // Storage interface for async storage
	logger      *zap.Logger
}

// NewDefaultResultHandler creates a new default result handler
func NewDefaultResultHandler(resultsChan chan<- *CorrelationResult, logger *zap.Logger) *DefaultResultHandler {
	return &DefaultResultHandler{
		resultsChan: resultsChan,
		logger:      logger,
	}
}

// HandleResults implements ResultHandler interface
func (h *DefaultResultHandler) HandleResults(ctx context.Context, results []*CorrelationResult) error {
	for _, result := range results {
		if result != nil {
			select {
			case h.resultsChan <- result:
				// Result sent successfully
			case <-ctx.Done():
				return fmt.Errorf("context cancelled while sending result: %w", ctx.Err())
			default:
				h.logger.Warn("Result channel full, dropping result",
					zap.String("result_id", result.ID),
				)
			}
		}
	}
	return nil
}

// SimpleErrorAggregator provides a basic error aggregation implementation
type SimpleErrorAggregator struct {
	errors []PipelineError
	mu     sync.Mutex
	logger *zap.Logger
}

// NewSimpleErrorAggregator creates a new simple error aggregator
func NewSimpleErrorAggregator(logger *zap.Logger) *SimpleErrorAggregator {
	return &SimpleErrorAggregator{
		errors: make([]PipelineError, 0),
		logger: logger,
	}
}

// RecordError implements ErrorAggregator interface
func (a *SimpleErrorAggregator) RecordError(correlatorName, eventID string, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Determine error type
	errorType := "correlator_failed"
	if err.Error() == "context deadline exceeded" {
		errorType = "correlator_timeout"
	} else if err.Error() == "engine is shutting down" {
		errorType = "engine_shutdown"
	}

	pipelineErr := PipelineError{
		CorrelatorName: correlatorName,
		EventID:        eventID,
		Error:          err,
		Timestamp:      time.Now(),
		ErrorType:      errorType,
	}

	a.errors = append(a.errors, pipelineErr)

	// Log the error
	a.logger.Error("Correlator error",
		zap.String("correlator", correlatorName),
		zap.String("event_id", eventID),
		zap.String("error_type", errorType),
		zap.Error(err),
	)
}

// GetErrors implements ErrorAggregator interface
func (a *SimpleErrorAggregator) GetErrors() []PipelineError {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Return a copy to avoid race conditions
	errorsCopy := make([]PipelineError, len(a.errors))
	copy(errorsCopy, a.errors)
	return errorsCopy
}

// Reset implements ErrorAggregator interface
func (a *SimpleErrorAggregator) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.errors = a.errors[:0] // Reset slice but keep capacity
}

// NewCorrelationPipeline creates a new correlation pipeline
func NewCorrelationPipeline(correlators []Correlator, config *PipelineConfig, logger *zap.Logger) (*CorrelationPipeline, error) {
	if len(correlators) == 0 {
		return nil, fmt.Errorf("at least one correlator is required")
	}
	if config == nil {
		return nil, fmt.Errorf("pipeline config is required")
	}
	if config.TimeoutCoordinator == nil {
		return nil, fmt.Errorf("timeout coordinator is required")
	}
	if config.ResultHandler == nil {
		return nil, fmt.Errorf("result handler is required")
	}
	if config.ErrorAggregator == nil {
		return nil, fmt.Errorf("error aggregator is required")
	}

	// Set defaults
	maxConcurrency := config.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = len(correlators) // Default to number of correlators
	}

	// Initialize OTEL components
	tracer := otel.Tracer("correlation.pipeline")
	meter := otel.Meter("correlation.pipeline")

	pipelineProcessed, err := meter.Int64Counter(
		"correlation_pipeline_events_processed_total",
		metric.WithDescription("Total events processed by correlation pipeline"),
	)
	if err != nil {
		logger.Warn("Failed to create pipeline processed counter", zap.Error(err))
	}

	pipelineErrors, err := meter.Int64Counter(
		"correlation_pipeline_errors_total",
		metric.WithDescription("Total errors in correlation pipeline"),
	)
	if err != nil {
		logger.Warn("Failed to create pipeline errors counter", zap.Error(err))
	}

	pipelineDuration, err := meter.Float64Histogram(
		"correlation_pipeline_duration_ms",
		metric.WithDescription("Correlation pipeline processing duration in milliseconds"),
	)
	if err != nil {
		logger.Warn("Failed to create pipeline duration histogram", zap.Error(err))
	}

	correlatorDuration, err := meter.Float64Histogram(
		"correlation_pipeline_correlator_duration_ms",
		metric.WithDescription("Individual correlator processing duration in milliseconds"),
	)
	if err != nil {
		logger.Warn("Failed to create correlator duration histogram", zap.Error(err))
	}

	return &CorrelationPipeline{
		correlators:        correlators,
		mode:               config.Mode,
		maxConcurrency:     maxConcurrency,
		timeoutCoordinator: config.TimeoutCoordinator,
		resultHandler:      config.ResultHandler,
		errorAggregator:    config.ErrorAggregator,
		logger:             logger,
		tracer:             tracer,
		pipelineProcessed:  pipelineProcessed,
		pipelineErrors:     pipelineErrors,
		pipelineDuration:   pipelineDuration,
		correlatorDuration: correlatorDuration,
	}, nil
}

// Process processes an event through the correlation pipeline
func (p *CorrelationPipeline) Process(ctx context.Context, event *domain.UnifiedEvent) error {
	startTime := time.Now()

	// Create span for pipeline processing
	ctx, span := p.tracer.Start(ctx, "correlation.pipeline.process")
	defer span.End()

	// Set span attributes
	span.SetAttributes(
		attribute.String("event.type", string(event.Type)),
		attribute.String("event.id", event.ID),
		attribute.Int("correlators.count", len(p.correlators)),
		attribute.String("pipeline.mode", p.getModeString()),
	)

	defer func() {
		// Record processing duration
		duration := time.Since(startTime).Seconds() * 1000 // Convert to milliseconds
		if p.pipelineDuration != nil {
			p.pipelineDuration.Record(ctx, duration, metric.WithAttributes(
				attribute.String("pipeline.mode", p.getModeString()),
				attribute.String("event.type", string(event.Type)),
			))
		}

		// Record processed event
		if p.pipelineProcessed != nil {
			p.pipelineProcessed.Add(ctx, 1, metric.WithAttributes(
				attribute.String("pipeline.mode", p.getModeString()),
				attribute.String("event.type", string(event.Type)),
			))
		}
	}()

	// Process based on mode
	switch p.mode {
	case PipelineModeSequential:
		return p.processSequential(ctx, event)
	case PipelineModeParallel:
		return p.processParallel(ctx, event)
	default:
		return fmt.Errorf("unsupported pipeline mode: %d", p.mode)
	}
}

// processSequential processes correlators one by one
func (p *CorrelationPipeline) processSequential(ctx context.Context, event *domain.UnifiedEvent) error {
	allResults := make([]*CorrelationResult, 0, len(p.correlators)*2) // Estimate capacity

	for _, correlator := range p.correlators {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled during sequential processing: %w", ctx.Err())
		default:
		}

		results, err := p.processWithCorrelator(ctx, event, correlator)
		if err != nil {
			p.errorAggregator.RecordError(correlator.Name(), event.ID, err)
			// Continue processing other correlators
			continue
		}

		allResults = append(allResults, results...)
	}

	// Handle all results
	return p.resultHandler.HandleResults(ctx, allResults)
}

// processParallel processes correlators concurrently
func (p *CorrelationPipeline) processParallel(ctx context.Context, event *domain.UnifiedEvent) error {
	type correlatorResult struct {
		results []*CorrelationResult
		err     error
		name    string
	}

	resultChan := make(chan correlatorResult, len(p.correlators))
	semaphore := make(chan struct{}, p.maxConcurrency)

	// Launch correlators
	var wg sync.WaitGroup
	for _, correlator := range p.correlators {
		wg.Add(1)
		go func(corr Correlator) {
			defer wg.Done()

			// Acquire semaphore
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }() // Release semaphore
			case <-ctx.Done():
				resultChan <- correlatorResult{nil, ctx.Err(), corr.Name()}
				return
			}

			results, err := p.processWithCorrelator(ctx, event, corr)
			resultChan <- correlatorResult{
				results: results,
				err:     err,
				name:    corr.Name(),
			}
		}(correlator)
	}

	// Close result channel when all goroutines complete
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	allResults := make([]*CorrelationResult, 0, len(p.correlators)*2) // Estimate capacity
	for result := range resultChan {
		if result.err != nil {
			p.errorAggregator.RecordError(result.name, event.ID, result.err)
			continue
		}
		allResults = append(allResults, result.results...)
	}

	// Handle all results
	return p.resultHandler.HandleResults(ctx, allResults)
}

// processWithCorrelator processes an event with a single correlator
func (p *CorrelationPipeline) processWithCorrelator(ctx context.Context, event *domain.UnifiedEvent, correlator Correlator) ([]*CorrelationResult, error) {
	startTime := time.Now()

	// Create span for correlator processing
	ctx, span := p.tracer.Start(ctx, fmt.Sprintf("correlation.pipeline.correlator.%s", correlator.Name()))
	defer span.End()

	// Set span attributes
	span.SetAttributes(
		attribute.String("correlator", correlator.Name()),
		attribute.String("event.id", event.ID),
	)

	defer func() {
		// Record correlator duration
		duration := time.Since(startTime).Seconds() * 1000 // Convert to milliseconds
		if p.correlatorDuration != nil {
			p.correlatorDuration.Record(ctx, duration, metric.WithAttributes(
				attribute.String("correlator", correlator.Name()),
				attribute.String("event.type", string(event.Type)),
			))
		}
	}()

	// Use timeout coordinator for correlator processing
	var results []*CorrelationResult
	var err error

	correlatorOperation := func() error {
		timeoutCtx := p.timeoutCoordinator.CreateCorrelatorContext(ctx, correlator.Name())
		defer timeoutCtx.Cancel()

		results, err = correlator.Process(timeoutCtx.Context, event)
		return err
	}

	processErr := p.timeoutCoordinator.WaitWithTimeout(ctx, ctx, CorrelatorLevel, correlatorOperation)
	if processErr != nil {
		// Record error metrics
		if p.pipelineErrors != nil {
			errorType := "correlator_failed"
			if p.timeoutCoordinator.IsTimeoutError(processErr) {
				errorType = "correlator_timeout"
			} else if processErr.Error() == "engine is shutting down" {
				errorType = "engine_shutdown"
			}

			p.pipelineErrors.Add(ctx, 1, metric.WithAttributes(
				attribute.String("error_type", errorType),
				attribute.String("correlator", correlator.Name()),
				attribute.String("event_type", string(event.Type)),
			))
		}

		// Record error in span
		span.SetAttributes(
			attribute.String("error", processErr.Error()),
			attribute.String("error.type", "correlator_error"),
		)

		return nil, fmt.Errorf("correlator %s processing failed: %w", correlator.Name(), processErr)
	}

	// Set result count in span
	span.SetAttributes(attribute.Int("results.count", len(results)))

	return results, nil
}

// GetCorrelators returns the list of correlators in the pipeline
func (p *CorrelationPipeline) GetCorrelators() []Correlator {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Return a copy to avoid race conditions
	correlators := make([]Correlator, len(p.correlators))
	copy(correlators, p.correlators)
	return correlators
}

// GetMode returns the current pipeline mode
func (p *CorrelationPipeline) GetMode() PipelineMode {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.mode
}

// SetMode updates the pipeline mode
func (p *CorrelationPipeline) SetMode(mode PipelineMode) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.mode = mode
}

// GetMaxConcurrency returns the maximum concurrency setting
func (p *CorrelationPipeline) GetMaxConcurrency() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.maxConcurrency
}

// SetMaxConcurrency updates the maximum concurrency setting
func (p *CorrelationPipeline) SetMaxConcurrency(maxConcurrency int) {
	if maxConcurrency <= 0 {
		maxConcurrency = len(p.correlators)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.maxConcurrency = maxConcurrency
}

// getModeString returns the string representation of the pipeline mode
func (p *CorrelationPipeline) getModeString() string {
	switch p.mode {
	case PipelineModeSequential:
		return "sequential"
	case PipelineModeParallel:
		return "parallel"
	default:
		return "unknown"
	}
}
