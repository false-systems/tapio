package adapters

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/hardening"
	"github.com/yairfalse/tapio/pkg/intelligence/interfaces"
)

var (
	ErrRateLimitExceeded  = errors.New("rate limit exceeded")
	ErrCircuitBreakerOpen = errors.New("circuit breaker is open")
	ErrBackpressure       = errors.New("system under high load, request dropped")
	ErrResourceExhausted  = errors.New("resource limit exceeded")
)

// ProductionHardeningAdapter provides production-grade hardening for event processing
type ProductionHardeningAdapter struct {
	engine          interfaces.CorrelationEngine
	rateLimiter     *hardening.RateLimiter
	circuitBreaker  *hardening.CircuitBreaker
	backpressure    *BackpressureController
	resourceMonitor *ResourceMonitor

	// Metrics
	metrics atomic.Value // *ProductionMetrics

	// Control
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// ProductionMetrics tracks production hardening metrics
type ProductionMetrics struct {
	// Rate limiting
	EventsAllowed   uint64
	EventsRejected  uint64
	RateUtilization float64

	// Circuit breaker
	CircuitState      string
	FailureCount      uint64
	SuccessCount      uint64
	RejectedByBreaker uint64

	// Backpressure
	LoadLevel  string
	EventsShed uint64
	ShedRate   float64

	// Resources
	MemoryUsageBytes  uint64
	MemoryUsagePct    float64
	GoroutineCount    int
	GoroutineUsagePct float64
}

// BackpressureController manages system load and sheds events when necessary
type BackpressureController struct {
	mu                sync.RWMutex
	bufferSize        int
	highWatermark     float64 // Percentage at which to start shedding
	criticalWatermark float64 // Percentage for aggressive shedding
	currentLoad       atomic.Int64
	shedRate          atomic.Value // float64
	priorityQueue     []EventPriority
}

// EventPriority defines event priority levels
type EventPriority int

const (
	PriorityCritical EventPriority = iota
	PriorityHigh
	PriorityNormal
	PriorityLow
)

// ResourceMonitor monitors system resources
type ResourceMonitor struct {
	maxMemoryBytes   uint64
	maxGoroutines    int
	checkInterval    time.Duration
	violationHandler func(violation string)

	// Current stats
	memoryUsage    atomic.Uint64
	goroutineCount atomic.Int32
}

// NewProductionHardeningAdapter creates a new production hardening adapter
func NewProductionHardeningAdapter(engine interfaces.CorrelationEngine, opts ...ProductionOption) *ProductionHardeningAdapter {
	config := defaultProductionConfig()
	for _, opt := range opts {
		opt(&config)
	}

	adapter := &ProductionHardeningAdapter{
		engine:         engine,
		rateLimiter:    hardening.NewRateLimiter(config.MaxEventsPerSecond),
		circuitBreaker: hardening.NewCircuitBreaker(config.FailureThreshold, config.RecoveryTimeout),
		stopChan:       make(chan struct{}),
	}

	// Initialize backpressure controller
	adapter.backpressure = &BackpressureController{
		bufferSize:        config.BufferSize,
		highWatermark:     0.7, // Start shedding at 70% capacity
		criticalWatermark: 0.9, // Aggressive shedding at 90%
		priorityQueue:     make([]EventPriority, 0, config.BufferSize),
	}
	adapter.backpressure.shedRate.Store(float64(0))

	// Initialize resource monitor
	adapter.resourceMonitor = &ResourceMonitor{
		maxMemoryBytes: config.MaxMemoryBytes,
		maxGoroutines:  config.MaxGoroutines,
		checkInterval:  config.ResourceCheckInterval,
		violationHandler: func(violation string) {
			// Default: log violation
			// In production, this could trigger alerts
		},
	}

	// Initialize metrics
	adapter.metrics.Store(&ProductionMetrics{})

	return adapter
}

// ProcessEvent processes an event with all production hardening features
func (pa *ProductionHardeningAdapter) ProcessEvent(ctx context.Context, event *domain.UnifiedEvent) error {
	// 1. Check rate limit
	if !pa.rateLimiter.Allow(ctx) {
		pa.updateMetric("events_rejected", 1)
		return ErrRateLimitExceeded
	}

	// 2. Check circuit breaker
	err := pa.circuitBreaker.Call(func() error {
		// 3. Check backpressure
		if !pa.backpressure.Accept(event) {
			return ErrBackpressure
		}

		// 4. Check resource limits
		if err := pa.resourceMonitor.CheckLimits(); err != nil {
			return err
		}

		// 5. Process event
		return pa.engine.ProcessEvent(ctx, event)
	})

	if err != nil {
		if errors.Is(err, hardening.ErrCircuitBreakerOpen) {
			pa.updateMetric("rejected_by_breaker", 1)
			return ErrCircuitBreakerOpen
		}
		return err
	}

	pa.updateMetric("events_allowed", 1)
	return nil
}

// ProcessBatch processes multiple events with production hardening
func (pa *ProductionHardeningAdapter) ProcessBatch(ctx context.Context, events []*domain.UnifiedEvent) error {
	// Check batch rate limit
	if !pa.rateLimiter.AllowN(int64(len(events))) {
		pa.updateMetric("events_rejected", uint64(len(events)))
		return ErrRateLimitExceeded
	}

	// Process with circuit breaker
	return pa.circuitBreaker.Call(func() error {
		// Filter events based on backpressure
		filtered := pa.backpressure.FilterBatch(events)

		if len(filtered) == 0 {
			return ErrBackpressure
		}

		// Process filtered events
		var lastErr error
		for _, event := range filtered {
			if err := pa.engine.ProcessEvent(ctx, event); err != nil {
				lastErr = err
			}
		}

		pa.updateMetric("events_allowed", uint64(len(filtered)))
		pa.updateMetric("events_shed", uint64(len(events)-len(filtered)))

		return lastErr
	})
}

// Start initializes the production hardening adapter
func (pa *ProductionHardeningAdapter) Start() error {
	if err := pa.engine.Start(); err != nil {
		return err
	}

	// Start monitoring goroutines
	pa.wg.Add(2)
	go pa.resourceMonitorLoop()
	go pa.metricsUpdateLoop()

	return nil
}

// Stop gracefully shuts down the adapter
func (pa *ProductionHardeningAdapter) Stop() error {
	close(pa.stopChan)
	pa.wg.Wait()

	pa.rateLimiter.Stop()
	return pa.engine.Stop()
}

// GetMetrics returns current production metrics
func (pa *ProductionHardeningAdapter) GetMetrics() *ProductionMetrics {
	return pa.metrics.Load().(*ProductionMetrics)
}

// BackpressureController methods

// Accept determines if an event should be accepted based on load
func (bc *BackpressureController) Accept(event *domain.UnifiedEvent) bool {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	load := float64(bc.currentLoad.Load()) / float64(bc.bufferSize)

	// Map severity to priority (since UnifiedEvent doesn't have Priority field)
	priority := bc.getPriorityFromEvent(event)
	
	// Always accept critical events
	if priority <= int(PriorityCritical) {
		return true
	}

	// Check load thresholds
	if load >= bc.criticalWatermark {
		// Critical load: only accept high priority
		return priority <= int(PriorityHigh)
	} else if load >= bc.highWatermark {
		// High load: shed low priority events based on shed rate
		if priority >= int(PriorityLow) {
			shedRate := bc.shedRate.Load().(float64)
			// Simple random shedding (in production, use better algorithm)
			return time.Now().UnixNano()%100 > int64(shedRate*100)
		}
	}

	return true
}

// FilterBatch filters a batch of events based on backpressure
func (bc *BackpressureController) FilterBatch(events []*domain.UnifiedEvent) []*domain.UnifiedEvent {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	load := float64(bc.currentLoad.Load()) / float64(bc.bufferSize)

	if load < bc.highWatermark {
		// Normal load: accept all
		return events
	}

	// High load: filter by priority
	filtered := make([]*domain.UnifiedEvent, 0, len(events))
	for _, event := range events {
		if bc.Accept(event) {
			filtered = append(filtered, event)
		}
	}

	return filtered
}

// getPriorityFromEvent maps UnifiedEvent severity to priority level
func (bc *BackpressureController) getPriorityFromEvent(event *domain.UnifiedEvent) int {
	severity := event.GetSeverity()
	switch severity {
	case "critical", "fatal":
		return int(PriorityCritical)
	case "error", "high":
		return int(PriorityHigh)
	case "warning", "warn", "medium":
		return int(PriorityNormal)
	default:
		return int(PriorityLow)
	}
}

// UpdateLoad updates the current system load
func (bc *BackpressureController) UpdateLoad(currentBufferSize int) {
	bc.currentLoad.Store(int64(currentBufferSize))

	// Calculate shed rate based on load
	load := float64(currentBufferSize) / float64(bc.bufferSize)

	var shedRate float64
	if load >= bc.criticalWatermark {
		shedRate = 0.8 // Shed 80% of low priority
	} else if load >= bc.highWatermark {
		// Linear increase from 0% to 70% shedding
		shedRate = (load - bc.highWatermark) / (bc.criticalWatermark - bc.highWatermark) * 0.7
	}

	bc.shedRate.Store(shedRate)
}

// ResourceMonitor methods

// CheckLimits checks if resource limits are exceeded
func (rm *ResourceMonitor) CheckLimits() error {
	// Check memory usage
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	rm.memoryUsage.Store(memStats.Alloc)

	if memStats.Alloc > rm.maxMemoryBytes {
		// Try to free memory
		runtime.GC()
		runtime.GC() // Double GC for aggressive cleanup

		// Check again
		runtime.ReadMemStats(&memStats)
		if memStats.Alloc > rm.maxMemoryBytes {
			if rm.violationHandler != nil {
				rm.violationHandler("memory limit exceeded")
			}
			return ErrResourceExhausted
		}
	}

	// Check goroutine count
	goroutineCount := runtime.NumGoroutine()
	rm.goroutineCount.Store(int32(goroutineCount))

	if goroutineCount > rm.maxGoroutines {
		if rm.violationHandler != nil {
			rm.violationHandler("goroutine limit exceeded")
		}
		// Don't fail for goroutine limit, just warn
	}

	return nil
}

// Helper methods

func (pa *ProductionHardeningAdapter) resourceMonitorLoop() {
	defer pa.wg.Done()

	ticker := time.NewTicker(pa.resourceMonitor.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-pa.stopChan:
			return
		case <-ticker.C:
			_ = pa.resourceMonitor.CheckLimits()
			pa.updateResourceMetrics()
		}
	}
}

func (pa *ProductionHardeningAdapter) metricsUpdateLoop() {
	defer pa.wg.Done()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-pa.stopChan:
			return
		case <-ticker.C:
			pa.updateAllMetrics()
		}
	}
}

func (pa *ProductionHardeningAdapter) updateMetric(name string, delta uint64) {
	// Atomic metric updates
	// In production, this would update prometheus metrics
}

func (pa *ProductionHardeningAdapter) updateResourceMetrics() {
	metrics := pa.metrics.Load().(*ProductionMetrics)
	newMetrics := *metrics

	newMetrics.MemoryUsageBytes = pa.resourceMonitor.memoryUsage.Load()
	newMetrics.MemoryUsagePct = float64(newMetrics.MemoryUsageBytes) / float64(pa.resourceMonitor.maxMemoryBytes) * 100
	newMetrics.GoroutineCount = int(pa.resourceMonitor.goroutineCount.Load())
	newMetrics.GoroutineUsagePct = float64(newMetrics.GoroutineCount) / float64(pa.resourceMonitor.maxGoroutines) * 100

	pa.metrics.Store(&newMetrics)
}

func (pa *ProductionHardeningAdapter) updateAllMetrics() {
	// Update rate limiter metrics
	rlMetrics := pa.rateLimiter.GetMetrics()

	// Update circuit breaker metrics
	cbMetrics := pa.circuitBreaker.GetMetrics()

	// Update backpressure metrics
	shedRate := pa.backpressure.shedRate.Load().(float64)
	load := float64(pa.backpressure.currentLoad.Load()) / float64(pa.backpressure.bufferSize)

	var loadLevel string
	if load >= pa.backpressure.criticalWatermark {
		loadLevel = "critical"
	} else if load >= pa.backpressure.highWatermark {
		loadLevel = "high"
	} else {
		loadLevel = "normal"
	}

	// Create new metrics snapshot
	metrics := &ProductionMetrics{
		// Rate limiting
		RateUtilization: rlMetrics.UtilizationPct,

		// Circuit breaker
		CircuitState:      cbMetrics.CurrentState,
		FailureCount:      cbMetrics.FailureCount,
		SuccessCount:      cbMetrics.SuccessCount,
		RejectedByBreaker: cbMetrics.RejectedCount,

		// Backpressure
		LoadLevel: loadLevel,
		ShedRate:  shedRate,

		// Copy existing counters
		EventsAllowed:  pa.metrics.Load().(*ProductionMetrics).EventsAllowed,
		EventsRejected: pa.metrics.Load().(*ProductionMetrics).EventsRejected,
		EventsShed:     pa.metrics.Load().(*ProductionMetrics).EventsShed,
	}

	pa.metrics.Store(metrics)
}

// Configuration

type productionConfig struct {
	MaxEventsPerSecond    int64
	FailureThreshold      int
	RecoveryTimeout       time.Duration
	BufferSize            int
	MaxMemoryBytes        uint64
	MaxGoroutines         int
	ResourceCheckInterval time.Duration
}

func defaultProductionConfig() productionConfig {
	return productionConfig{
		MaxEventsPerSecond:    10000,
		FailureThreshold:      100,
		RecoveryTimeout:       30 * time.Second,
		BufferSize:            10000,
		MaxMemoryBytes:        1024 * 1024 * 1024, // 1GB
		MaxGoroutines:         10000,
		ResourceCheckInterval: 10 * time.Second,
	}
}

type ProductionOption func(*productionConfig)

// WithRateLimit sets the rate limit
func WithRateLimit(eventsPerSecond int64) ProductionOption {
	return func(c *productionConfig) {
		c.MaxEventsPerSecond = eventsPerSecond
	}
}

// WithMemoryLimit sets the memory limit
func WithMemoryLimit(bytes uint64) ProductionOption {
	return func(c *productionConfig) {
		c.MaxMemoryBytes = bytes
	}
}
