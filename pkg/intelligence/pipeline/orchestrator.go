package pipeline

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	contextpkg "github.com/yairfalse/tapio/pkg/intelligence/context"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
)

// Metrics tracks orchestrator performance
type Metrics struct {
	EventsProcessed     int64
	EventsValidated     int64
	EventsContextBuilt  int64
	EventsCorrelated    int64
	ValidationErrors    int64
	ContextErrors       int64
	CorrelationErrors   int64
	ProcessingDuration  time.Duration
	AverageLatency      time.Duration
	ThroughputPerSecond float64
}

// OrchestratorConfig holds configuration for the orchestrator
type OrchestratorConfig struct {
	BatchSize          int           // Events per batch
	WorkerCount        int           // Number of worker goroutines
	BufferSize         int           // Channel buffer size
	ProcessingTimeout  time.Duration // Timeout for processing operations
	MetricsInterval    time.Duration // How often to calculate metrics
	CorrelationEnabled bool          // Whether to run correlation stage
}

// DefaultOrchestratorConfig returns a configuration optimized for 165k events/sec
func DefaultOrchestratorConfig() *OrchestratorConfig {
	return &OrchestratorConfig{
		BatchSize:          1000,
		WorkerCount:        runtime.NumCPU(),
		BufferSize:         10000,
		ProcessingTimeout:  5 * time.Second,
		MetricsInterval:    1 * time.Second,
		CorrelationEnabled: true,
	}
}

// ProcessingStage represents a stage in the processing pipeline
type ProcessingStage interface {
	Name() string
	Process(ctx context.Context, event *domain.UnifiedEvent) error
}

// ValidationStage validates incoming events
type ValidationStage struct{}

func (v *ValidationStage) Name() string {
	return "validation"
}

func (v *ValidationStage) Process(ctx context.Context, event *domain.UnifiedEvent) error {
	if event == nil {
		return fmt.Errorf("event is nil")
	}
	if event.ID == "" {
		return fmt.Errorf("event ID is empty")
	}
	if event.Source == "" {
		return fmt.Errorf("event source is empty")
	}
	if event.Timestamp.IsZero() {
		return fmt.Errorf("event timestamp is zero")
	}
	if time.Since(event.Timestamp) > 24*time.Hour {
		return fmt.Errorf("event is too old: %v", event.Timestamp)
	}
	return nil
}

// ContextStage builds context for events
type ContextStage struct {
	validator *contextpkg.EventValidator
	scorer    *contextpkg.ConfidenceScorer
	analyzer  *contextpkg.ImpactAnalyzer
}

func NewContextStage() *ContextStage {
	validator := contextpkg.NewEventValidator()
	scorer := contextpkg.NewConfidenceScorer()
	analyzer := contextpkg.NewImpactAnalyzer()

	return &ContextStage{
		validator: validator,
		scorer:    scorer,
		analyzer:  analyzer,
	}
}

func (c *ContextStage) Name() string {
	return "context"
}

func (c *ContextStage) Process(ctx context.Context, event *domain.UnifiedEvent) error {
	// Stage 1: Context Validation
	if err := c.validator.Validate(event); err != nil {
		return fmt.Errorf("context validation failed: %w", err)
	}

	// Stage 2: Confidence Scoring
	confidenceResult := c.scorer.CalculateConfidence(event)

	// Stage 3: Impact Assessment
	impactResult := c.analyzer.AssessImpact(event)

	// Enrich event with context results
	if event.Correlation == nil {
		event.Correlation = &domain.CorrelationContext{}
	}

	if event.Semantic == nil {
		event.Semantic = &domain.SemanticContext{}
	}

	event.Semantic.Confidence = confidenceResult
	event.Impact = impactResult

	return nil
}

// CorrelationStage performs event correlation
type CorrelationStage struct {
	processor *correlation.RealTimeProcessor
}

func NewCorrelationStage(config *correlation.ProcessorConfig) (*CorrelationStage, error) {
	processor, err := correlation.NewRealTimeProcessor(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create correlation processor: %w", err)
	}

	return &CorrelationStage{
		processor: processor,
	}, nil
}

func (c *CorrelationStage) Name() string {
	return "correlation"
}

func (c *CorrelationStage) Process(ctx context.Context, event *domain.UnifiedEvent) error {
	result := c.processor.ProcessEvent(ctx, event)
	if result == nil {
		return fmt.Errorf("correlation processing returned nil result")
	}

	// Enrich event with correlation results
	if event.Correlation == nil {
		event.Correlation = &domain.CorrelationContext{}
	}

	event.Correlation.CorrelationID = result.ID
	event.Semantic.Confidence = result.Score
	event.Correlation.Pattern = result.PatternType

	return nil
}

// EventBatch represents a batch of events to process
type EventBatch struct {
	Events    []*domain.UnifiedEvent
	StartTime time.Time
	BatchID   string
}

// HighPerformanceOrchestrator orchestrates the processing pipeline
type HighPerformanceOrchestrator struct {
	config     *OrchestratorConfig
	stages     []ProcessingStage
	workerPool *WorkerPool
	metrics    *Metrics

	// Processing channels
	eventChan  chan *domain.UnifiedEvent
	resultChan chan *ProcessingResult

	// Control channels
	stopChan chan struct{}
	doneChan chan struct{}

	// Synchronization
	wg      sync.WaitGroup
	running int32

	// Metrics tracking
	metricsLock     sync.RWMutex
	lastMetricsTime time.Time
	processedCount  int64
}

// ProcessingResult contains the result of processing an event
type ProcessingResult struct {
	Event    *domain.UnifiedEvent
	Error    error
	Duration time.Duration
	Stage    string
}

// NewHighPerformanceOrchestrator creates a new orchestrator with all stages
func NewHighPerformanceOrchestrator(config *OrchestratorConfig) (*HighPerformanceOrchestrator, error) {
	if config == nil {
		config = DefaultOrchestratorConfig()
	}

	// Create stages
	validationStage := &ValidationStage{}
	contextStage := NewContextStage()

	stages := []ProcessingStage{
		validationStage,
		contextStage,
	}

	// Add correlation stage if enabled
	if config.CorrelationEnabled {
		correlationConfig := &correlation.ProcessorConfig{
			BufferSize:        1000,
			TimeWindow:        5 * time.Minute,
			CorrelationWindow: 10 * time.Minute,
		}

		correlationStage, err := NewCorrelationStage(correlationConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create correlation stage: %w", err)
		}
		stages = append(stages, correlationStage)
	}

	// Create worker pool
	workerPool := NewWorkerPool(config.WorkerCount, config.BufferSize)

	orchestrator := &HighPerformanceOrchestrator{
		config:          config,
		stages:          stages,
		workerPool:      workerPool,
		metrics:         &Metrics{},
		eventChan:       make(chan *domain.UnifiedEvent, config.BufferSize),
		resultChan:      make(chan *ProcessingResult, config.BufferSize),
		stopChan:        make(chan struct{}),
		doneChan:        make(chan struct{}),
		lastMetricsTime: time.Now(),
	}

	return orchestrator, nil
}

// Start begins processing events
func (o *HighPerformanceOrchestrator) Start(ctx context.Context) error {
	if !atomic.CompareAndSwapInt32(&o.running, 0, 1) {
		return fmt.Errorf("orchestrator is already running")
	}

	// Re-create stopChan if it was closed
	o.stopChan = make(chan struct{})

	// Start worker pool
	if err := o.workerPool.Start(ctx); err != nil {
		atomic.StoreInt32(&o.running, 0)
		return fmt.Errorf("failed to start worker pool: %w", err)
	}

	// Start processing goroutine
	o.wg.Add(1)
	go o.processEvents(ctx)

	// Start metrics collection goroutine
	o.wg.Add(1)
	go o.collectMetrics(ctx)

	return nil
}

// Stop gracefully stops the orchestrator
func (o *HighPerformanceOrchestrator) Stop() error {
	if !atomic.CompareAndSwapInt32(&o.running, 1, 0) {
		return fmt.Errorf("orchestrator is not running")
	}

	// Signal stop
	close(o.stopChan)

	// Stop worker pool
	o.workerPool.Stop()

	// Wait for processing to complete
	o.wg.Wait()

	return nil
}

// ProcessEvent processes a single event through the pipeline
func (o *HighPerformanceOrchestrator) ProcessEvent(event *domain.UnifiedEvent) error {
	if atomic.LoadInt32(&o.running) == 0 {
		return fmt.Errorf("orchestrator is not running")
	}

	if event == nil {
		return fmt.Errorf("event is nil")
	}

	// Ensure event has ID and timestamp
	if event.ID == "" {
		event.ID = domain.GenerateEventID()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Send event for processing
	select {
	case o.eventChan <- event:
		atomic.AddInt64(&o.metrics.EventsProcessed, 1)
		return nil
	default:
		return fmt.Errorf("event channel full, dropping event")
	}
}

// ProcessBatch processes a batch of events
func (o *HighPerformanceOrchestrator) ProcessBatch(events []*domain.UnifiedEvent) error {
	if len(events) == 0 {
		return nil
	}

	batch := &EventBatch{
		Events:    events,
		StartTime: time.Now(),
		BatchID:   domain.GenerateEventID(),
	}

	// Submit batch to worker pool
	job := &Job{
		ID:      batch.BatchID,
		Type:    "batch",
		Payload: batch,
		Handler: o.processBatchJob,
	}

	return o.workerPool.Submit(job)
}

// GetMetrics returns current processing metrics
func (o *HighPerformanceOrchestrator) GetMetrics() Metrics {
	o.metricsLock.RLock()
	defer o.metricsLock.RUnlock()
	return *o.metrics
}

// IsRunning returns whether the orchestrator is currently running
func (o *HighPerformanceOrchestrator) IsRunning() bool {
	return atomic.LoadInt32(&o.running) == 1
}

// processEvents handles event processing in a separate goroutine
func (o *HighPerformanceOrchestrator) processEvents(ctx context.Context) {
	defer o.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-o.stopChan:
			return
		case event := <-o.eventChan:
			if event != nil {
				o.processEventThroughStages(ctx, event)
			}
		}
	}
}

// processEventThroughStages processes an event through all pipeline stages
func (o *HighPerformanceOrchestrator) processEventThroughStages(ctx context.Context, event *domain.UnifiedEvent) {
	startTime := time.Now()

	// Process through each stage sequentially
	for _, stage := range o.stages {
		stageStart := time.Now()

		err := stage.Process(ctx, event)

		stageDuration := time.Since(stageStart)

		// Update stage-specific metrics
		o.updateStageMetrics(stage.Name(), err)

		if err != nil {
			// Send error result
			result := &ProcessingResult{
				Event:    event,
				Error:    err,
				Duration: stageDuration,
				Stage:    stage.Name(),
			}

			select {
			case o.resultChan <- result:
			default:
				// Result channel full, drop result
			}
			return
		}
	}

	// Send success result
	totalDuration := time.Since(startTime)
	result := &ProcessingResult{
		Event:    event,
		Error:    nil,
		Duration: totalDuration,
		Stage:    "complete",
	}

	select {
	case o.resultChan <- result:
	default:
		// Result channel full, drop result
	}
}

// processBatchJob handles batch processing
func (o *HighPerformanceOrchestrator) processBatchJob(ctx context.Context, job *Job) error {
	batch, ok := job.Payload.(*EventBatch)
	if !ok {
		return fmt.Errorf("invalid batch payload")
	}

	// Process each event in the batch
	for _, event := range batch.Events {
		if event != nil {
			o.processEventThroughStages(ctx, event)
		}
	}

	return nil
}

// updateStageMetrics updates metrics for a specific stage
func (o *HighPerformanceOrchestrator) updateStageMetrics(stage string, err error) {
	if err != nil {
		switch stage {
		case "validation":
			atomic.AddInt64(&o.metrics.ValidationErrors, 1)
		case "context":
			atomic.AddInt64(&o.metrics.ContextErrors, 1)
		case "correlation":
			atomic.AddInt64(&o.metrics.CorrelationErrors, 1)
		}
	} else {
		switch stage {
		case "validation":
			atomic.AddInt64(&o.metrics.EventsValidated, 1)
		case "context":
			atomic.AddInt64(&o.metrics.EventsContextBuilt, 1)
		case "correlation":
			atomic.AddInt64(&o.metrics.EventsCorrelated, 1)
		}
	}
}

// collectMetrics periodically calculates and updates metrics
func (o *HighPerformanceOrchestrator) collectMetrics(ctx context.Context) {
	defer o.wg.Done()

	ticker := time.NewTicker(o.config.MetricsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-o.stopChan:
			return
		case <-ticker.C:
			o.calculateMetrics()
		}
	}
}

// calculateMetrics calculates throughput and latency metrics
func (o *HighPerformanceOrchestrator) calculateMetrics() {
	o.metricsLock.Lock()
	defer o.metricsLock.Unlock()

	now := time.Now()
	duration := now.Sub(o.lastMetricsTime)

	if duration > 0 {
		currentProcessed := atomic.LoadInt64(&o.metrics.EventsProcessed)

		if o.processedCount > 0 {
			eventsDelta := currentProcessed - o.processedCount
			o.metrics.ThroughputPerSecond = float64(eventsDelta) / duration.Seconds()
		}

		o.processedCount = currentProcessed
		o.lastMetricsTime = now
		o.metrics.ProcessingDuration = duration
	}
}
