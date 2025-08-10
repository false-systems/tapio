package correlation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// Engine orchestrates all correlators
type Engine struct {
	logger *zap.Logger

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

	engine := &Engine{
		logger:      logger,
		correlators: make([]Correlator, 0),
		storage:     storage,
		eventChan:   make(chan *domain.UnifiedEvent, config.EventBufferSize),
		resultChan:  make(chan *CorrelationResult, config.ResultBufferSize),
		config:      config,
		ctx:         ctx,
		cancel:      cancel,
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
	e.logger.Info("Starting correlation engine",
		zap.Int("workers", e.config.WorkerCount),
		zap.Int("event_buffer", e.config.EventBufferSize),
	)

	// Start worker goroutines
	for i := 0; i < e.config.WorkerCount; i++ {
		e.wg.Add(1)
		go e.worker(i)
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
	e.logger.Info("Stopping correlation engine")

	// Cancel context to signal shutdown
	e.cancel()

	// Close input channel
	close(e.eventChan)

	// Wait for workers to finish
	e.wg.Wait()

	// Close output channel
	close(e.resultChan)

	e.logger.Info("Correlation engine stopped",
		zap.Int64("events_processed", e.eventsProcessed),
		zap.Int64("correlations_found", e.correlationsFound),
	)

	return nil
}

// Process submits an event for correlation processing
func (e *Engine) Process(ctx context.Context, event *domain.UnifiedEvent) error {
	if event == nil {
		return fmt.Errorf("event is nil")
	}

	// Use a timeout to prevent indefinite blocking
	timer := time.NewTimer(e.config.ProcessingTimeout)
	defer timer.Stop()

	select {
	case e.eventChan <- event:
		return nil
	case <-timer.C:
		return fmt.Errorf("timeout sending event to processing queue")
	case <-ctx.Done():
		return ctx.Err()
	case <-e.ctx.Done():
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
	startTime := time.Now()

	// Update processing metrics
	e.incrementProcessedEvents()

	// Process through each correlator
	for _, correlator := range e.correlators {
		e.processWithCorrelator(event, correlator)
	}

	// Monitor processing performance
	e.checkProcessingPerformance(event.ID, startTime)
}

// incrementProcessedEvents safely increments the events processed counter
func (e *Engine) incrementProcessedEvents() {
	e.mu.Lock()
	e.eventsProcessed++
	e.mu.Unlock()
}

// processWithCorrelator processes an event with a single correlator
func (e *Engine) processWithCorrelator(event *domain.UnifiedEvent, correlator Correlator) {
	// Create timeout context for correlator
	ctx, cancel := context.WithTimeout(e.ctx, DefaultProcessingTimeout)
	defer cancel()

	// Process event
	results, err := correlator.Process(ctx, event)
	if err != nil {
		e.logCorrelatorError(correlator.Name(), event.ID, err)
		return
	}

	// Handle results
	e.handleCorrelatorResults(results)
}

// handleCorrelatorResults processes and stores correlation results
func (e *Engine) handleCorrelatorResults(results []*CorrelationResult) {
	for _, result := range results {
		if result != nil {
			e.sendResult(result)

			// Store result asynchronously
			if e.storage != nil {
				e.asyncStoreResult(result)
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
func (e *Engine) sendResult(result *CorrelationResult) {
	// Update metrics
	e.mu.Lock()
	e.correlationsFound++
	e.mu.Unlock()

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
func (e *Engine) asyncStoreResult(result *CorrelationResult) {
	// Create a copy of the result to avoid data races
	resultCopy := *result

	// Store in a goroutine to avoid blocking event processing
	go func() {
		// Use a timeout context for storage operations
		storeCtx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
		defer cancel()

		if err := e.storage.Store(storeCtx, &resultCopy); err != nil {
			// Log error but don't block processing
			e.logger.Error("Failed to store correlation asynchronously",
				zap.String("correlation_id", resultCopy.ID),
				zap.Error(err),
			)

			// Update error metrics
			e.mu.Lock()
			// Note: Add error counter to Engine struct if needed for monitoring
			e.mu.Unlock()
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
