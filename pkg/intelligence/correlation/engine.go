package correlation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
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

// EngineConfig configures the correlation engine
type EngineConfig struct {
	// Processing
	EventBufferSize   int
	ResultBufferSize  int
	WorkerCount       int
	ProcessingTimeout time.Duration

	// Features
	EnableK8s         bool
	EnableTemporal    bool
	EnableSequence    bool
	EnablePerformance bool
	EnableServiceMap  bool

	// Storage
	StorageCleanupInterval time.Duration
	StorageRetention       time.Duration
}

// DefaultEngineConfig returns production-ready defaults
func DefaultEngineConfig() EngineConfig {
	return EngineConfig{
		EventBufferSize:        1000,
		ResultBufferSize:       1000,
		WorkerCount:            4,
		ProcessingTimeout:      30 * time.Second,
		EnableK8s:              true,
		EnableTemporal:         true,
		EnableSequence:         true,
		EnablePerformance:      true,
		EnableServiceMap:       true,
		StorageCleanupInterval: 5 * time.Minute,
		StorageRetention:       24 * time.Hour,
	}
}

// NewEngine creates a new correlation engine
func NewEngine(logger *zap.Logger, config EngineConfig, k8sClient kubernetes.Interface, storage Storage) (*Engine, error) {
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

	// Initialize correlators based on config
	if config.EnableK8s && k8sClient != nil {
		k8sCorrelator := NewK8sCorrelator(logger, k8sClient)
		engine.correlators = append(engine.correlators, k8sCorrelator)

		// Start K8s correlator
		if err := k8sCorrelator.Start(ctx); err != nil {
			cancel()
			return nil, fmt.Errorf("failed to start K8s correlator: %w", err)
		}
	}

	if config.EnableTemporal {
		temporalCorrelator := NewTemporalCorrelator(logger, DefaultTemporalConfig())
		engine.correlators = append(engine.correlators, temporalCorrelator)
	}

	if config.EnableSequence {
		sequenceCorrelator := NewSequenceCorrelator(logger, DefaultSequenceConfig())
		engine.correlators = append(engine.correlators, sequenceCorrelator)
	}

	if config.EnablePerformance {
		performanceCorrelator := NewPerformanceCorrelator(logger)
		engine.correlators = append(engine.correlators, performanceCorrelator)
	}

	if config.EnableServiceMap {
		serviceMapCorrelator := NewServiceMapCorrelator(logger)
		engine.correlators = append(engine.correlators, serviceMapCorrelator)
	}

	logger.Info("Correlation engine created",
		zap.Int("correlators", len(engine.correlators)),
		zap.Bool("k8s", config.EnableK8s),
		zap.Bool("temporal", config.EnableTemporal),
		zap.Bool("sequence", config.EnableSequence),
		zap.Bool("performance", config.EnablePerformance),
		zap.Bool("servicemap", config.EnableServiceMap),
	)

	return engine, nil
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

	// Update metrics
	e.mu.Lock()
	e.eventsProcessed++
	e.mu.Unlock()

	// Process through each correlator
	for _, correlator := range e.correlators {
		// Create a timeout context for each correlator
		ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)

		results, err := correlator.Process(ctx, event)
		cancel()

		if err != nil {
			e.logger.Error("Correlator error",
				zap.String("correlator", correlator.Name()),
				zap.String("event_id", event.ID),
				zap.Error(err),
			)
			continue
		}

		// Send results
		for _, result := range results {
			if result != nil {
				e.sendResult(result)

				// Store result
				if e.storage != nil {
					if err := e.storage.Store(e.ctx, result); err != nil {
						e.logger.Error("Failed to store correlation",
							zap.String("correlation_id", result.ID),
							zap.Error(err),
						)
					}
				}
			}
		}
	}

	// Log processing time if it's slow
	duration := time.Since(startTime)
	if duration > 100*time.Millisecond {
		e.logger.Warn("Slow event processing",
			zap.String("event_id", event.ID),
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

// metricsReporter periodically logs metrics
func (e *Engine) metricsReporter() {
	defer e.wg.Done()

	ticker := time.NewTicker(1 * time.Minute)
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
func (e *Engine) GetMetrics() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return map[string]interface{}{
		"events_processed":   e.eventsProcessed,
		"correlations_found": e.correlationsFound,
		"event_queue_size":   len(e.eventChan),
		"result_queue_size":  len(e.resultChan),
		"correlators_count":  len(e.correlators),
		"workers_count":      e.config.WorkerCount,
	}
}
