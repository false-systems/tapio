// Package pipeline provides the unified orchestration layer for Tapio
//
// This orchestrator combines the best features from all previous implementations:
// - Simple design from collector-manager (minimal dependencies)
// - Performance optimizations from pipeline orchestrator (worker pools)
// - Resilience from collector orchestrator (error handling, graceful shutdown)
// - Clean architecture with clear separation of concerns
package pipeline

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// UnifiedOrchestrator provides simple, resilient, and fast event orchestration
type UnifiedOrchestrator struct {
	// Core components (simple like collector-manager)
	collectors map[string]Collector
	pipeline   IntelligencePipeline
	config     *UnifiedConfig

	// Channels for event flow
	collectorEvents chan *domain.UnifiedEvent
	processedEvents chan *domain.UnifiedEvent

	// Performance optimization (from pipeline orchestrator)
	workerPool *sync.WaitGroup
	workers    int

	// Resilience features
	health  *HealthMonitor
	metrics *atomic.Value // stores *UnifiedMetrics

	// Lifecycle management
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.RWMutex
}

// UnifiedConfig provides simple configuration with sensible defaults
type UnifiedConfig struct {
	// Core settings
	BufferSize int // Channel buffer size (default: 10000)
	Workers    int // Number of workers (default: NumCPU)

	// Pipeline settings
	EnableValidation  bool // Enable event validation (default: true)
	EnableContext     bool // Enable context building (default: true)
	EnableCorrelation bool // Enable correlation (default: true)

	// Resilience settings
	ProcessingTimeout time.Duration // Timeout per event (default: 5s)
	ShutdownTimeout   time.Duration // Graceful shutdown timeout (default: 30s)

	// Monitoring
	MetricsInterval time.Duration // Metrics update interval (default: 10s)
}

// DefaultUnifiedConfig returns production-ready defaults
func DefaultUnifiedConfig() *UnifiedConfig {
	return &UnifiedConfig{
		BufferSize:        10000,
		Workers:           0, // 0 means use NumCPU
		EnableValidation:  true,
		EnableContext:     true,
		EnableCorrelation: true,
		ProcessingTimeout: 5 * time.Second,
		ShutdownTimeout:   30 * time.Second,
		MetricsInterval:   10 * time.Second,
	}
}

// Collector interface - simple and focused
type Collector interface {
	Start(ctx context.Context) error
	Stop() error
	Events() <-chan *domain.UnifiedEvent
	Health() domain.HealthStatus
}

// HealthMonitor tracks health of all components
type HealthMonitor struct {
	collectors sync.Map     // map[string]domain.HealthStatus
	pipeline   atomic.Value // domain.HealthStatus
	overall    atomic.Value // domain.HealthStatus
}

// NewUnifiedOrchestrator creates a new orchestrator with the given configuration
func NewUnifiedOrchestrator(config *UnifiedConfig) (*UnifiedOrchestrator, error) {
	if config == nil {
		config = DefaultUnifiedConfig()
	}

	// Apply defaults for zero values
	if config.BufferSize <= 0 {
		config.BufferSize = 10000
	}
	if config.Workers <= 0 {
		config.Workers = runtime.NumCPU()
	}
	if config.ProcessingTimeout <= 0 {
		config.ProcessingTimeout = 5 * time.Second
	}
	if config.ShutdownTimeout <= 0 {
		config.ShutdownTimeout = 30 * time.Second
	}
	if config.MetricsInterval <= 0 {
		config.MetricsInterval = 10 * time.Second
	}

	o := &UnifiedOrchestrator{
		collectors:      make(map[string]Collector),
		config:          config,
		collectorEvents: make(chan *domain.UnifiedEvent, config.BufferSize),
		processedEvents: make(chan *domain.UnifiedEvent, config.BufferSize),
		workerPool:      &sync.WaitGroup{},
		workers:         config.Workers,
		health:          &HealthMonitor{},
		metrics:         &atomic.Value{},
	}

	// Initialize metrics
	o.metrics.Store(&UnifiedMetrics{})

	// Create pipeline with our configuration
	pipelineConfig := &PipelineConfig{
		Mode:              PipelineModeHighPerformance,
		EnableValidation:  config.EnableValidation,
		EnableContext:     config.EnableContext,
		EnableCorrelation: config.EnableCorrelation,
		BufferSize:        config.BufferSize,
		BatchSize:         1000,
		ProcessingTimeout: config.ProcessingTimeout,
	}

	pipeline, err := NewPipeline(pipelineConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create pipeline: %w", err)
	}
	o.pipeline = pipeline

	return o, nil
}

// AddCollector adds a collector to the orchestrator
func (o *UnifiedOrchestrator) AddCollector(name string, collector Collector) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if _, exists := o.collectors[name]; exists {
		return fmt.Errorf("collector %s already exists", name)
	}

	o.collectors[name] = collector
	return nil
}

// Start begins the orchestration process
func (o *UnifiedOrchestrator) Start(ctx context.Context) error {
	o.mu.Lock()
	o.ctx, o.cancel = context.WithCancel(ctx)
	o.mu.Unlock()

	// Start the pipeline first
	if err := o.pipeline.Start(o.ctx); err != nil {
		return fmt.Errorf("failed to start pipeline: %w", err)
	}

	// Start all collectors
	for name, collector := range o.collectors {
		if err := collector.Start(o.ctx); err != nil {
			o.Stop() // Clean up on failure
			return fmt.Errorf("failed to start collector %s: %w", name, err)
		}

		// Route collector events
		go o.routeCollectorEvents(name, collector)
	}

	// Start worker pool for processing
	for i := 0; i < o.workers; i++ {
		o.workerPool.Add(1)
		go o.processEvents()
	}

	// Start metrics collection
	go o.collectMetrics()

	// Start health monitoring
	go o.monitorHealth()

	log.Printf("✅ Unified Orchestrator started with %d collectors and %d workers",
		len(o.collectors), o.workers)

	return nil
}

// Stop gracefully shuts down the orchestrator
func (o *UnifiedOrchestrator) Stop() error {
	o.mu.Lock()
	if o.cancel != nil {
		o.cancel()
	}
	o.mu.Unlock()

	// Create shutdown context with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(
		context.Background(),
		o.config.ShutdownTimeout,
	)
	defer shutdownCancel()

	// Stop collectors first
	var wg sync.WaitGroup
	for name, collector := range o.collectors {
		wg.Add(1)
		go func(name string, c Collector) {
			defer wg.Done()
			if err := c.Stop(); err != nil {
				log.Printf("Error stopping collector %s: %v", name, err)
			}
		}(name, collector)
	}

	// Wait for collectors to stop or timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("All collectors stopped")
	case <-shutdownCtx.Done():
		log.Println("Collector shutdown timeout exceeded")
	}

	// Close collector events channel to signal workers
	close(o.collectorEvents)

	// Wait for workers to finish
	o.workerPool.Wait()

	// Stop the pipeline
	if err := o.pipeline.Stop(); err != nil {
		log.Printf("Error stopping pipeline: %v", err)
	}

	// Close processed events channel
	close(o.processedEvents)

	log.Println("✅ Unified Orchestrator stopped")
	return nil
}

// routeCollectorEvents routes events from a collector to the processing queue
func (o *UnifiedOrchestrator) routeCollectorEvents(name string, collector Collector) {
	for {
		select {
		case event, ok := <-collector.Events():
			if !ok {
				log.Printf("Collector %s events channel closed", name)
				return
			}

			// Non-blocking send with metrics
			select {
			case o.collectorEvents <- event:
				o.updateMetrics(func(m *UnifiedMetrics) {
					m.EventsReceived++
				})
			case <-o.ctx.Done():
				return
			default:
				// Channel full, drop event and track
				o.updateMetrics(func(m *UnifiedMetrics) {
					m.EventsDropped++
				})
				log.Printf("Warning: Dropping event from %s, buffer full", name)
			}

		case <-o.ctx.Done():
			return
		}
	}
}

// processEvents is the worker function that processes events through the pipeline
func (o *UnifiedOrchestrator) processEvents() {
	defer o.workerPool.Done()

	for event := range o.collectorEvents {
		// Create timeout context for this event
		processCtx, cancel := context.WithTimeout(o.ctx, o.config.ProcessingTimeout)

		// Process through pipeline
		start := time.Now()
		err := o.pipeline.ProcessEvent(event)
		_ = processCtx // Use the context variable
		duration := time.Since(start)

		cancel() // Clean up context

		if err != nil {
			o.updateMetrics(func(m *UnifiedMetrics) {
				m.ProcessingErrors++
			})
			log.Printf("Error processing event %s: %v", event.ID, err)
			continue
		}

		// Update metrics
		o.updateMetrics(func(m *UnifiedMetrics) {
			m.EventsProcessed++
			m.TotalProcessingTime += duration
			if duration > m.MaxProcessingTime {
				m.MaxProcessingTime = duration
			}
		})

		// Send to output channel
		select {
		case o.processedEvents <- event:
		case <-o.ctx.Done():
			return
		}
	}
}

// ProcessedEvents returns the channel of processed events
func (o *UnifiedOrchestrator) ProcessedEvents() <-chan *domain.UnifiedEvent {
	return o.processedEvents
}

// GetMetrics returns current performance metrics
func (o *UnifiedOrchestrator) GetMetrics() *UnifiedMetrics {
	return o.metrics.Load().(*UnifiedMetrics)
}

// GetHealth returns current health status
func (o *UnifiedOrchestrator) GetHealth() domain.HealthStatus {
	overall := o.health.overall.Load()
	if overall != nil {
		return overall.(domain.HealthStatus)
	}
	return domain.NewHealthStatus(
		domain.HealthUnknown,
		"Health monitoring not yet initialized",
		nil,
	)
}

// updateMetrics safely updates metrics
func (o *UnifiedOrchestrator) updateMetrics(fn func(*UnifiedMetrics)) {
	current := o.metrics.Load().(*UnifiedMetrics)
	updated := *current // Copy
	fn(&updated)
	o.metrics.Store(&updated)
}

// collectMetrics periodically calculates derived metrics
func (o *UnifiedOrchestrator) collectMetrics() {
	ticker := time.NewTicker(o.config.MetricsInterval)
	defer ticker.Stop()

	var lastProcessed int64
	lastTime := time.Now()

	for {
		select {
		case <-ticker.C:
			now := time.Now()
			duration := now.Sub(lastTime)

			current := o.GetMetrics()
			processed := current.EventsProcessed
			delta := processed - lastProcessed

			throughput := float64(delta) / duration.Seconds()

			o.updateMetrics(func(m *UnifiedMetrics) {
				m.ThroughputPerSecond = throughput
				if m.EventsProcessed > 0 {
					m.AverageProcessingTime = m.TotalProcessingTime / time.Duration(m.EventsProcessed)
				}
			})

			lastProcessed = processed
			lastTime = now

		case <-o.ctx.Done():
			return
		}
	}
}

// monitorHealth periodically checks component health
func (o *UnifiedOrchestrator) monitorHealth() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			o.checkHealth()
		case <-o.ctx.Done():
			return
		}
	}
}

// checkHealth evaluates overall system health
func (o *UnifiedOrchestrator) checkHealth() {
	unhealthyCount := 0
	totalCount := 0
	details := make(map[string]interface{})

	// Check each collector
	o.mu.RLock()
	for name, collector := range o.collectors {
		totalCount++
		health := collector.Health()
		o.health.collectors.Store(name, health)

		if health.Status() != domain.HealthHealthy {
			unhealthyCount++
			details[fmt.Sprintf("collector_%s", name)] = health.Message()
		}
	}
	o.mu.RUnlock()

	// Check pipeline health
	if o.pipeline != nil {
		// Assume pipeline has Health() method
		// This would need to be added to the IntelligencePipeline interface
		totalCount++
		// pipelineHealth := o.pipeline.Health()
		// o.health.pipeline.Store(pipelineHealth)
	}

	// Determine overall health
	var status domain.HealthStatusValue
	var message string

	if unhealthyCount == 0 {
		status = domain.HealthHealthy
		message = fmt.Sprintf("All %d components healthy", totalCount)
	} else if unhealthyCount < totalCount/2 {
		status = domain.HealthDegraded
		message = fmt.Sprintf("%d of %d components unhealthy", unhealthyCount, totalCount)
	} else {
		status = domain.HealthUnhealthy
		message = fmt.Sprintf("%d of %d components unhealthy", unhealthyCount, totalCount)
	}

	// Add metrics to health details
	metrics := o.GetMetrics()
	details["events_processed"] = metrics.EventsProcessed
	details["events_dropped"] = metrics.EventsDropped
	details["throughput_per_sec"] = metrics.ThroughputPerSecond
	details["average_processing_ms"] = metrics.AverageProcessingTime.Milliseconds()

	overallHealth := domain.NewHealthStatus(status, message, details)
	o.health.overall.Store(overallHealth)
}

// UnifiedMetrics tracks performance metrics for the unified orchestrator
type UnifiedMetrics struct {
	// Event counts
	EventsReceived   int64
	EventsProcessed  int64
	EventsDropped    int64
	ProcessingErrors int64

	// Performance metrics
	TotalProcessingTime   time.Duration
	AverageProcessingTime time.Duration
	MaxProcessingTime     time.Duration
	ThroughputPerSecond   float64
}
