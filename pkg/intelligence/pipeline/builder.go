package pipeline

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/interfaces"
)

// PipelineBuilder provides a fluent interface for building pipelines
type PipelineBuilder struct {
	config *PipelineConfig
	stages []ProcessingStage
	errors []error
}

// NewPipelineBuilder creates a new pipeline builder
func NewPipelineBuilder() *PipelineBuilder {
	return &PipelineBuilder{
		config: DefaultPipelineConfig(),
		stages: make([]ProcessingStage, 0),
		errors: make([]error, 0),
	}
}

// WithMode sets the pipeline mode
func (pb *PipelineBuilder) WithMode(mode PipelineMode) *PipelineBuilder {
	pb.config.Mode = mode
	return pb
}

// WithConfig applies a complete configuration
func (pb *PipelineBuilder) WithConfig(config *PipelineConfig) *PipelineBuilder {
	if config != nil {
		pb.config = config.Clone()
	}
	return pb
}

// WithOrchestratorConfig sets the orchestrator configuration
func (pb *PipelineBuilder) WithOrchestratorConfig(config *OrchestratorConfig) *PipelineBuilder {
	pb.config.OrchestratorConfig = config
	return pb
}

// WithBatchSize sets the batch size
func (pb *PipelineBuilder) WithBatchSize(size int) *PipelineBuilder {
	pb.config.BatchSize = size
	return pb
}

// WithBufferSize sets the buffer size
func (pb *PipelineBuilder) WithBufferSize(size int) *PipelineBuilder {
	pb.config.BufferSize = size
	return pb
}

// WithMaxConcurrency sets the maximum concurrency
func (pb *PipelineBuilder) WithMaxConcurrency(max int) *PipelineBuilder {
	pb.config.MaxConcurrency = max
	return pb
}

// WithProcessingTimeout sets the processing timeout
func (pb *PipelineBuilder) WithProcessingTimeout(timeout time.Duration) *PipelineBuilder {
	pb.config.ProcessingTimeout = timeout
	return pb
}

// WithMetricsInterval sets the metrics collection interval
func (pb *PipelineBuilder) WithMetricsInterval(interval time.Duration) *PipelineBuilder {
	pb.config.MetricsInterval = interval
	return pb
}

// EnableValidation enables the validation stage
func (pb *PipelineBuilder) EnableValidation(enable bool) *PipelineBuilder {
	pb.config.EnableValidation = enable
	return pb
}

// EnableContext enables the context building stage
func (pb *PipelineBuilder) EnableContext(enable bool) *PipelineBuilder {
	pb.config.EnableContext = enable
	return pb
}

// EnableCorrelation enables the correlation stage
func (pb *PipelineBuilder) EnableCorrelation(enable bool) *PipelineBuilder {
	pb.config.EnableCorrelation = enable
	return pb
}

// EnableMetrics enables metrics collection
func (pb *PipelineBuilder) EnableMetrics(enable bool) *PipelineBuilder {
	pb.config.EnableMetrics = enable
	return pb
}

// EnableTracing enables distributed tracing
func (pb *PipelineBuilder) EnableTracing(enable bool) *PipelineBuilder {
	pb.config.EnableTracing = enable
	return pb
}

// EnableCircuitBreaker enables the circuit breaker
func (pb *PipelineBuilder) EnableCircuitBreaker(enable bool) *PipelineBuilder {
	pb.config.EnableCircuitBreaker = enable
	return pb
}

// WithErrorThreshold sets the error threshold for circuit breaker
func (pb *PipelineBuilder) WithErrorThreshold(threshold float64) *PipelineBuilder {
	pb.config.ErrorThreshold = threshold
	return pb
}

// WithCircuitBreakerThreshold sets the circuit breaker threshold
func (pb *PipelineBuilder) WithCircuitBreakerThreshold(threshold float64) *PipelineBuilder {
	pb.config.CircuitBreakerThreshold = threshold
	return pb
}

// AddStage adds a custom processing stage
func (pb *PipelineBuilder) AddStage(stage ProcessingStage) *PipelineBuilder {
	pb.stages = append(pb.stages, stage)
	return pb
}

// Build creates the pipeline based on configuration
func (pb *PipelineBuilder) Build() (IntelligencePipeline, error) {
	// Validate configuration
	if err := pb.config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Check for builder errors
	if len(pb.errors) > 0 {
		return nil, fmt.Errorf("builder errors: %v", pb.errors)
	}

	// Create pipeline based on mode
	switch pb.config.Mode {
	case PipelineModeHighPerformance:
		return pb.buildHighPerformancePipeline()
	case PipelineModeStandard:
		return pb.buildStandardPipeline()
	case PipelineModeDebug:
		return pb.buildDebugPipeline()
	default:
		return nil, fmt.Errorf("unknown pipeline mode: %s", pb.config.Mode)
	}
}

// buildHighPerformancePipeline creates a high-performance pipeline
func (pb *PipelineBuilder) buildHighPerformancePipeline() (IntelligencePipeline, error) {
	// Create default implementations for now
	// TODO: These should be injected as dependencies
	var contextProcessor interfaces.ContextProcessor
	var correlationEngine interfaces.CorrelationEngine

	// Use the existing HighPerformanceOrchestrator
	orchestrator, err := NewHighPerformanceOrchestrator(pb.config.OrchestratorConfig, contextProcessor, correlationEngine)
	if err != nil {
		return nil, fmt.Errorf("failed to create orchestrator: %w", err)
	}

	// Wrap in pipeline adapter
	pipeline := &pipelineAdapter{
		orchestrator: orchestrator,
		config:       pb.config.Clone(),
		metrics:      NewMetricsCollector(),
	}

	// Add custom stages if any
	for _, stage := range pb.stages {
		// Custom stages would be added to the orchestrator here
		// For now, we'll store them for future use
		pipeline.customStages = append(pipeline.customStages, stage)
	}

	return pipeline, nil
}

// buildStandardPipeline creates a standard pipeline
func (pb *PipelineBuilder) buildStandardPipeline() (IntelligencePipeline, error) {
	// For standard mode, use reduced concurrency
	config := pb.config.Clone()
	if config.MaxConcurrency == 0 {
		config.MaxConcurrency = 4
	}
	config.OrchestratorConfig.WorkerCount = config.MaxConcurrency

	return pb.buildHighPerformancePipeline()
}

// buildDebugPipeline creates a debug pipeline
func (pb *PipelineBuilder) buildDebugPipeline() (IntelligencePipeline, error) {
	// For debug mode, use single-threaded processing
	config := pb.config.Clone()
	config.MaxConcurrency = 1
	config.OrchestratorConfig.WorkerCount = 1

	return pb.buildHighPerformancePipeline()
}

// pipelineAdapter adapts the orchestrator to the IntelligencePipeline interface
type pipelineAdapter struct {
	orchestrator *HighPerformanceOrchestrator
	config       *PipelineConfig
	metrics      *MetricsCollector
	customStages []ProcessingStage

	// State
	running int32
	mu      sync.RWMutex

	// Circuit breaker
	circuitBreaker *CircuitBreaker
}

// ProcessEvent processes a single event
func (pa *pipelineAdapter) ProcessEvent(event *domain.UnifiedEvent) error {
	if !pa.IsRunning() {
		return fmt.Errorf("pipeline is not running")
	}

	// Check circuit breaker
	if pa.config.EnableCircuitBreaker && pa.circuitBreaker != nil {
		if !pa.circuitBreaker.Allow() {
			pa.metrics.IncrementDropped(1)
			return fmt.Errorf("circuit breaker is open")
		}
	}

	// Record metrics
	pa.metrics.IncrementReceived(1)
	startTime := time.Now()

	// Process through orchestrator
	err := pa.orchestrator.ProcessEvent(event)

	// Record latency
	latency := time.Since(startTime)
	pa.metrics.RecordLatency(latency)

	if err != nil {
		pa.metrics.IncrementFailed(1)
		if pa.circuitBreaker != nil {
			pa.circuitBreaker.RecordFailure()
		}
		return err
	}

	if pa.circuitBreaker != nil {
		pa.circuitBreaker.RecordSuccess()
	}

	return nil
}

// ProcessBatch processes a batch of events
func (pa *pipelineAdapter) ProcessBatch(events []*domain.UnifiedEvent) error {
	if !pa.IsRunning() {
		return fmt.Errorf("pipeline is not running")
	}

	// Check circuit breaker
	if pa.config.EnableCircuitBreaker && pa.circuitBreaker != nil {
		if !pa.circuitBreaker.Allow() {
			pa.metrics.IncrementDropped(int64(len(events)))
			return fmt.Errorf("circuit breaker is open")
		}
	}

	// Record metrics
	pa.metrics.IncrementReceived(int64(len(events)))
	pa.metrics.RecordBatch(len(events))

	// Process through orchestrator
	err := pa.orchestrator.ProcessBatch(events)

	if err != nil {
		pa.metrics.IncrementFailed(int64(len(events)))
		if pa.circuitBreaker != nil {
			pa.circuitBreaker.RecordFailure()
		}
		return err
	}

	if pa.circuitBreaker != nil {
		pa.circuitBreaker.RecordSuccess()
	}

	return nil
}

// Start starts the pipeline
func (pa *pipelineAdapter) Start(ctx context.Context) error {
	if !atomic.CompareAndSwapInt32(&pa.running, 0, 1) {
		return fmt.Errorf("pipeline is already running")
	}

	// Initialize circuit breaker if enabled
	if pa.config.EnableCircuitBreaker {
		pa.circuitBreaker = NewCircuitBreaker(
			pa.config.CircuitBreakerThreshold,
			pa.config.ErrorThreshold,
			time.Minute, // Recovery timeout
		)
	}

	// Start orchestrator
	if err := pa.orchestrator.Start(ctx); err != nil {
		atomic.StoreInt32(&pa.running, 0)
		return fmt.Errorf("failed to start orchestrator: %w", err)
	}

	// Start metrics collection
	if pa.config.EnableMetrics {
		go pa.collectMetrics(ctx)
	}

	return nil
}

// Stop stops the pipeline
func (pa *pipelineAdapter) Stop() error {
	if !atomic.CompareAndSwapInt32(&pa.running, 1, 0) {
		return fmt.Errorf("pipeline is not running")
	}

	return pa.orchestrator.Stop()
}

// Shutdown is an alias for Stop
func (pa *pipelineAdapter) Shutdown() error {
	return pa.Stop()
}

// GetMetrics returns current pipeline metrics
func (pa *pipelineAdapter) GetMetrics() PipelineMetrics {
	// Get orchestrator metrics
	orchMetrics := pa.orchestrator.GetMetrics()

	// Update our metrics from orchestrator
	pa.metrics.mu.Lock()
	pa.metrics.metrics.EventsProcessed = orchMetrics.EventsProcessed
	pa.metrics.metrics.EventsValidated = orchMetrics.EventsValidated
	pa.metrics.metrics.EventsContextBuilt = orchMetrics.EventsContextBuilt
	pa.metrics.metrics.EventsCorrelated = orchMetrics.EventsCorrelated
	pa.metrics.metrics.ValidationErrors = orchMetrics.ValidationErrors
	pa.metrics.metrics.ContextErrors = orchMetrics.ContextErrors
	pa.metrics.metrics.CorrelationErrors = orchMetrics.CorrelationErrors
	pa.metrics.mu.Unlock()

	// Update queue metrics
	if pa.orchestrator.workerPool != nil {
		poolMetrics := pa.orchestrator.workerPool.GetMetrics()
		pa.metrics.UpdateQueueMetrics(poolMetrics.QueueSize, poolMetrics.QueueCapacity)
		pa.metrics.UpdateWorkerMetrics(poolMetrics.WorkerCount)
	}

	// Update circuit breaker state
	if pa.circuitBreaker != nil {
		pa.metrics.UpdateCircuitBreakerState(pa.circuitBreaker.State())
	}

	return pa.metrics.GetMetrics()
}

// IsRunning returns whether the pipeline is running
func (pa *pipelineAdapter) IsRunning() bool {
	return atomic.LoadInt32(&pa.running) == 1
}

// GetConfig returns the pipeline configuration
func (pa *pipelineAdapter) GetConfig() PipelineConfig {
	pa.mu.RLock()
	defer pa.mu.RUnlock()
	return *pa.config
}

// collectMetrics periodically collects metrics
func (pa *pipelineAdapter) collectMetrics(ctx context.Context) {
	ticker := time.NewTicker(pa.config.MetricsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pa.GetMetrics() // This updates all metrics
		}
	}
}

// CircuitBreaker implements a simple circuit breaker pattern
type CircuitBreaker struct {
	threshold       float64
	errorThreshold  float64
	recoveryTimeout time.Duration

	mu           sync.RWMutex
	failures     int64
	successes    int64
	state        string
	lastFailTime time.Time
	trips        int64
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(threshold, errorThreshold float64, recoveryTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		threshold:       threshold,
		errorThreshold:  errorThreshold,
		recoveryTimeout: recoveryTimeout,
		state:           "closed",
	}
}

// Allow checks if the circuit breaker allows the operation
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	switch cb.state {
	case "open":
		// Check if we should transition to half-open
		if time.Since(cb.lastFailTime) > cb.recoveryTimeout {
			cb.mu.RUnlock()
			cb.mu.Lock()
			cb.state = "half-open"
			cb.mu.Unlock()
			cb.mu.RLock()
			return true
		}
		return false
	case "half-open":
		// Allow limited traffic
		return true
	default:
		return true
	}
}

// RecordSuccess records a successful operation
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.successes++

	// If half-open and successful, close the circuit
	if cb.state == "half-open" {
		cb.state = "closed"
		cb.failures = 0
		cb.successes = 0
	}
}

// RecordFailure records a failed operation
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailTime = time.Now()

	total := float64(cb.failures + cb.successes)
	if total > 0 {
		errorRate := float64(cb.failures) / total

		// Only trip if not already open
		if cb.state != "open" && (errorRate > cb.threshold || (cb.state == "half-open" && errorRate > cb.errorThreshold)) {
			cb.state = "open"
			cb.trips++
		}
	}
}

// State returns the current circuit breaker state
func (cb *CircuitBreaker) State() string {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// GetTrips returns the number of times the circuit breaker has tripped
func (cb *CircuitBreaker) GetTrips() int64 {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.trips
}

// Factory function for creating pipelines
func NewPipeline(config *PipelineConfig) (IntelligencePipeline, error) {
	builder := NewPipelineBuilder()
	if config != nil {
		builder.WithConfig(config)
	}
	return builder.Build()
}

// Convenience functions for common configurations

// NewHighPerformancePipeline creates a high-performance pipeline
func NewHighPerformancePipeline() (IntelligencePipeline, error) {
	return NewPipeline(DefaultPipelineConfig())
}

// NewStandardPipeline creates a standard pipeline
func NewStandardPipeline() (IntelligencePipeline, error) {
	return NewPipeline(StandardPipelineConfig())
}

// NewDebugPipeline creates a debug pipeline
func NewDebugPipeline() (IntelligencePipeline, error) {
	return NewPipeline(DebugPipelineConfig())
}

// NewCustomPipeline creates a pipeline with custom configuration
func NewCustomPipeline(opts ...func(*PipelineConfig)) (IntelligencePipeline, error) {
	config := DefaultPipelineConfig()
	for _, opt := range opts {
		opt(config)
	}
	return NewPipeline(config)
}

// Ensure defaults are set correctly
func init() {
	// Set default worker count based on CPU cores
	if runtime.NumCPU() > 0 {
		defaultWorkerCount := runtime.NumCPU()
		if defaultWorkerCount > 16 {
			defaultWorkerCount = 16 // Cap at 16 to avoid excessive resource usage
		}
	}
}
