package correlation

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// ResilientSemanticTracer enhances the semantic tracer with production-grade resilience
type ResilientSemanticTracer struct {
	*SemanticOTELTracer

	// Resilience components
	circuitBreaker *CircuitBreaker
	rateLimiter    *RateLimiter
	errorHandler   *ErrorHandler
	healthChecker  *HealthChecker

	// Metrics
	meter            metric.Meter
	processedCounter metric.Int64Counter
	errorCounter     metric.Int64Counter
	groupGauge       metric.Int64UpDownCounter
	latencyHistogram metric.Float64Histogram

	// State management
	mu           sync.RWMutex
	running      atomic.Bool
	shuttingDown atomic.Bool
	shutdownChan chan struct{}
	wg           sync.WaitGroup
}

// CircuitBreaker prevents cascading failures
type CircuitBreaker struct {
	mu              sync.RWMutex
	failureCount    int64
	successCount    int64
	lastFailureTime time.Time
	state           string // "closed", "open", "half-open"
	threshold       int64
	timeout         time.Duration
	halfOpenCalls   int64
	maxHalfOpen     int64
}

// RateLimiter controls event processing rate
type RateLimiter struct {
	limiter     *time.Ticker
	maxRate     int
	burstSize   int
	tokenBucket chan struct{}
	mu          sync.Mutex
}

// ErrorHandler manages error recovery strategies
type ErrorHandler struct {
	strategies   map[string]RecoveryStrategy
	retryPolicy  *RetryPolicy
	fallbackFunc func(context.Context, *domain.UnifiedEvent) error
	errorLog     []ErrorRecord
	mu           sync.RWMutex
}

// HealthChecker monitors tracer health
type HealthChecker struct {
	checks        []HealthCheck
	lastCheckTime time.Time
	status        string
	details       map[string]string
	mu            sync.RWMutex
}

// RecoveryStrategy defines how to recover from specific errors
type RecoveryStrategy interface {
	Recover(ctx context.Context, err error, event *domain.UnifiedEvent) error
	ShouldRetry(err error) bool
}

// RetryPolicy defines retry behavior
type RetryPolicy struct {
	MaxAttempts   int
	InitialDelay  time.Duration
	MaxDelay      time.Duration
	BackoffFactor float64
	JitterFactor  float64
}

// ErrorRecord tracks error history
type ErrorRecord struct {
	Timestamp time.Time
	Error     error
	EventID   string
	EventType string
	Recovered bool
}

// HealthCheck defines a health check function
type HealthCheck struct {
	Name     string
	CheckFn  func() error
	Critical bool
}

// NewResilientSemanticTracer creates a production-grade semantic tracer
func NewResilientSemanticTracer() (*ResilientSemanticTracer, error) {
	// Create base semantic tracer
	baseTracer := NewSemanticOTELTracer()

	// Initialize meter
	meter := otel.Meter("tapio.semantic.resilient")

	// Create metrics
	processedCounter, err := meter.Int64Counter(
		"tapio.semantic.events.processed",
		metric.WithDescription("Total events processed by semantic tracer"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create processed counter: %w", err)
	}

	errorCounter, err := meter.Int64Counter(
		"tapio.semantic.errors.total",
		metric.WithDescription("Total errors in semantic tracer"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create error counter: %w", err)
	}

	groupGauge, err := meter.Int64UpDownCounter(
		"tapio.semantic.groups.active",
		metric.WithDescription("Active semantic groups"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create group gauge: %w", err)
	}

	latencyHistogram, err := meter.Float64Histogram(
		"tapio.semantic.processing.latency",
		metric.WithDescription("Event processing latency"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create latency histogram: %w", err)
	}

	rst := &ResilientSemanticTracer{
		SemanticOTELTracer: baseTracer,
		meter:              meter,
		processedCounter:   processedCounter,
		errorCounter:       errorCounter,
		groupGauge:         groupGauge,
		latencyHistogram:   latencyHistogram,
		shutdownChan:       make(chan struct{}),
	}

	// Initialize resilience components
	rst.initializeCircuitBreaker()
	rst.initializeRateLimiter()
	rst.initializeErrorHandler()
	rst.initializeHealthChecker()

	// Start background routines
	rst.running.Store(true)
	rst.startBackgroundRoutines()

	return rst, nil
}

// ProcessUnifiedEventWithResilience processes events with full resilience
func (rst *ResilientSemanticTracer) ProcessUnifiedEventWithResilience(ctx context.Context, event *domain.UnifiedEvent) error {
	// Check if shutting down
	if rst.shuttingDown.Load() {
		return fmt.Errorf("tracer is shutting down")
	}

	// Start timing
	start := time.Now()
	defer func() {
		rst.latencyHistogram.Record(ctx, time.Since(start).Seconds())
	}()

	// Check circuit breaker
	if !rst.circuitBreaker.Allow() {
		rst.errorCounter.Add(ctx, 1,
			metric.WithAttributes(attribute.String("error.type", "circuit_breaker_open")),
		)
		return fmt.Errorf("circuit breaker is open")
	}

	// Apply rate limiting
	if !rst.rateLimiter.Allow(ctx) {
		rst.errorCounter.Add(ctx, 1,
			metric.WithAttributes(attribute.String("error.type", "rate_limited")),
		)
		return fmt.Errorf("rate limit exceeded")
	}

	// Process with retry logic
	err := rst.processWithRetry(ctx, event)

	// Update circuit breaker
	if err != nil {
		rst.circuitBreaker.RecordFailure()
		rst.errorCounter.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("error.type", "processing_failed"),
				attribute.String("event.type", string(event.Type)),
			),
		)

		// Try error recovery
		if recoveryErr := rst.errorHandler.HandleError(ctx, err, event); recoveryErr != nil {
			return fmt.Errorf("processing failed and recovery failed: %v, recovery: %v", err, recoveryErr)
		}
		return err
	}

	rst.circuitBreaker.RecordSuccess()
	rst.processedCounter.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("event.type", string(event.Type)),
			attribute.String("event.severity", event.GetSeverity()),
		),
	)

	// Update group count
	rst.updateGroupMetrics(ctx)

	return nil
}

// processWithRetry implements retry logic
func (rst *ResilientSemanticTracer) processWithRetry(ctx context.Context, event *domain.UnifiedEvent) error {
	policy := rst.errorHandler.retryPolicy

	var lastErr error
	for attempt := 0; attempt < policy.MaxAttempts; attempt++ {
		// Create span for retry attempt
		_, span := rst.tracer.Start(ctx, fmt.Sprintf("semantic.process.attempt_%d", attempt+1),
			trace.WithAttributes(
				attribute.Int("retry.attempt", attempt+1),
				attribute.String("event.id", event.ID),
			),
		)

		// Try processing
		err := rst.SemanticOTELTracer.ProcessEventWithSemanticTrace(ctx, &domain.Event{
			ID:        domain.EventID(event.ID),
			Type:      domain.EventType(event.Type),
			Timestamp: event.Timestamp,
			Source:    domain.SourceType(event.Source),
			Severity:  domain.EventSeverity(event.Severity),
		})
		span.End()

		if err == nil {
			return nil
		}

		lastErr = err

		// Check if we should retry
		if !rst.errorHandler.ShouldRetry(err) {
			return err
		}

		// Calculate backoff
		delay := rst.calculateBackoff(attempt, policy)

		// Wait before retry
		select {
		case <-time.After(delay):
			// Continue to next attempt
		case <-ctx.Done():
			return ctx.Err()
		case <-rst.shutdownChan:
			return fmt.Errorf("shutdown during retry")
		}
	}

	return fmt.Errorf("max retries exceeded: %w", lastErr)
}

// initializeCircuitBreaker sets up circuit breaker
func (rst *ResilientSemanticTracer) initializeCircuitBreaker() {
	rst.circuitBreaker = &CircuitBreaker{
		state:       "closed",
		threshold:   5,
		timeout:     30 * time.Second,
		maxHalfOpen: 3,
	}
}

// initializeRateLimiter sets up rate limiting
func (rst *ResilientSemanticTracer) initializeRateLimiter() {
	maxRate := 1000 // events per second
	burstSize := 100

	rst.rateLimiter = &RateLimiter{
		maxRate:     maxRate,
		burstSize:   burstSize,
		tokenBucket: make(chan struct{}, burstSize),
		limiter:     time.NewTicker(time.Second / time.Duration(maxRate)),
	}

	// Fill token bucket
	for i := 0; i < burstSize; i++ {
		rst.rateLimiter.tokenBucket <- struct{}{}
	}

	// Start refill routine
	go rst.rateLimiter.refillTokens()
}

// initializeErrorHandler sets up error handling
func (rst *ResilientSemanticTracer) initializeErrorHandler() {
	rst.errorHandler = &ErrorHandler{
		strategies: make(map[string]RecoveryStrategy),
		retryPolicy: &RetryPolicy{
			MaxAttempts:   3,
			InitialDelay:  100 * time.Millisecond,
			MaxDelay:      5 * time.Second,
			BackoffFactor: 2.0,
			JitterFactor:  0.1,
		},
		errorLog: make([]ErrorRecord, 0, 1000),
	}

	// Register recovery strategies
	rst.errorHandler.strategies["timeout"] = &TimeoutRecovery{}
	rst.errorHandler.strategies["memory"] = &MemoryPressureRecovery{}
	rst.errorHandler.strategies["correlation"] = &CorrelationFailureRecovery{}
}

// initializeHealthChecker sets up health monitoring
func (rst *ResilientSemanticTracer) initializeHealthChecker() {
	rst.healthChecker = &HealthChecker{
		status:  "healthy",
		details: make(map[string]string),
	}

	// Add health checks
	rst.healthChecker.checks = []HealthCheck{
		{
			Name:     "semantic_groups",
			Critical: true,
			CheckFn: func() error {
				groups := len(rst.SemanticOTELTracer.GetSemanticGroups())
				if groups > 10000 {
					return fmt.Errorf("too many semantic groups: %d", groups)
				}
				return nil
			},
		},
		{
			Name:     "circuit_breaker",
			Critical: true,
			CheckFn: func() error {
				if rst.circuitBreaker.state == "open" {
					return fmt.Errorf("circuit breaker is open")
				}
				return nil
			},
		},
		{
			Name:     "error_rate",
			Critical: false,
			CheckFn: func() error {
				// Check error rate in last minute
				recentErrors := rst.errorHandler.GetRecentErrorCount(time.Minute)
				if recentErrors > 100 {
					return fmt.Errorf("high error rate: %d errors in last minute", recentErrors)
				}
				return nil
			},
		},
	}
}

// startBackgroundRoutines starts maintenance routines
func (rst *ResilientSemanticTracer) startBackgroundRoutines() {
	// Group cleanup routine
	rst.wg.Add(1)
	go func() {
		defer rst.wg.Done()
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				rst.SemanticOTELTracer.CleanupOldGroups(30 * time.Minute)
				rst.updateGroupMetrics(context.Background())
			case <-rst.shutdownChan:
				return
			}
		}
	}()

	// Health check routine
	rst.wg.Add(1)
	go func() {
		defer rst.wg.Done()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				rst.healthChecker.runHealthChecks()
			case <-rst.shutdownChan:
				return
			}
		}
	}()

	// Error log cleanup routine
	rst.wg.Add(1)
	go func() {
		defer rst.wg.Done()
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				rst.errorHandler.CleanupOldErrors(24 * time.Hour)
			case <-rst.shutdownChan:
				return
			}
		}
	}()
}

// updateGroupMetrics updates semantic group metrics
func (rst *ResilientSemanticTracer) updateGroupMetrics(ctx context.Context) {
	rst.mu.RLock()
	groups := len(rst.SemanticOTELTracer.GetSemanticGroups())
	rst.mu.RUnlock()

	// Update gauge to current value
	rst.groupGauge.Add(ctx, int64(groups)-rst.getLastGroupCount())
	rst.setLastGroupCount(int64(groups))
}

// Shutdown gracefully shuts down the tracer
func (rst *ResilientSemanticTracer) Shutdown(ctx context.Context) error {
	// Mark as shutting down
	if !rst.shuttingDown.CompareAndSwap(false, true) {
		return fmt.Errorf("already shutting down")
	}

	// Stop accepting new events
	rst.running.Store(false)

	// Signal shutdown to routines
	close(rst.shutdownChan)

	// Wait for routines with timeout
	done := make(chan struct{})
	go func() {
		rst.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Circuit Breaker implementation
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case "open":
		// Check if timeout has passed
		if time.Since(cb.lastFailureTime) > cb.timeout {
			cb.state = "half-open"
			cb.halfOpenCalls = 0
			return true
		}
		return false

	case "half-open":
		if cb.halfOpenCalls >= cb.maxHalfOpen {
			return false
		}
		cb.halfOpenCalls++
		return true

	default: // closed
		return true
	}
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.successCount++

	if cb.state == "half-open" {
		// Transition to closed after successful calls
		if cb.successCount > cb.threshold {
			cb.state = "closed"
			cb.failureCount = 0
			cb.successCount = 0
		}
	}
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount++
	cb.lastFailureTime = time.Now()

	if cb.failureCount >= cb.threshold {
		cb.state = "open"
	}
}

// Rate Limiter implementation
func (rl *RateLimiter) Allow(ctx context.Context) bool {
	select {
	case <-rl.tokenBucket:
		return true
	case <-ctx.Done():
		return false
	default:
		return false
	}
}

func (rl *RateLimiter) refillTokens() {
	for range rl.limiter.C {
		select {
		case rl.tokenBucket <- struct{}{}:
			// Token added
		default:
			// Bucket full
		}
	}
}

// Error Handler implementation
func (eh *ErrorHandler) HandleError(ctx context.Context, err error, event *domain.UnifiedEvent) error {
	// Log error
	eh.mu.Lock()
	eh.errorLog = append(eh.errorLog, ErrorRecord{
		Timestamp: time.Now(),
		Error:     err,
		EventID:   event.ID,
		EventType: string(event.Type),
		Recovered: false,
	})
	eh.mu.Unlock()

	// Try recovery strategies
	for errType, strategy := range eh.strategies {
		if strategy.ShouldRetry(err) {
			if recoveryErr := strategy.Recover(ctx, err, event); recoveryErr == nil {
				// Mark as recovered
				eh.mu.Lock()
				if len(eh.errorLog) > 0 {
					eh.errorLog[len(eh.errorLog)-1].Recovered = true
				}
				eh.mu.Unlock()
				return nil
			}
		}
		_ = errType // unused
	}

	// Try fallback function
	if eh.fallbackFunc != nil {
		return eh.fallbackFunc(ctx, event)
	}

	return err
}

func (eh *ErrorHandler) ShouldRetry(err error) bool {
	// Check if any strategy thinks we should retry
	for _, strategy := range eh.strategies {
		if strategy.ShouldRetry(err) {
			return true
		}
	}
	return false
}

func (eh *ErrorHandler) GetRecentErrorCount(duration time.Duration) int {
	eh.mu.RLock()
	defer eh.mu.RUnlock()

	cutoff := time.Now().Add(-duration)
	count := 0

	for _, record := range eh.errorLog {
		if record.Timestamp.After(cutoff) && !record.Recovered {
			count++
		}
	}

	return count
}

func (eh *ErrorHandler) CleanupOldErrors(retention time.Duration) {
	eh.mu.Lock()
	defer eh.mu.Unlock()

	cutoff := time.Now().Add(-retention)
	newLog := make([]ErrorRecord, 0, len(eh.errorLog))

	for _, record := range eh.errorLog {
		if record.Timestamp.After(cutoff) {
			newLog = append(newLog, record)
		}
	}

	eh.errorLog = newLog
}

// Health Checker implementation
func (hc *HealthChecker) runHealthChecks() {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.lastCheckTime = time.Now()
	healthy := true

	for _, check := range hc.checks {
		if err := check.CheckFn(); err != nil {
			hc.details[check.Name] = err.Error()
			if check.Critical {
				healthy = false
			}
		} else {
			hc.details[check.Name] = "ok"
		}
	}

	if healthy {
		hc.status = "healthy"
	} else {
		hc.status = "unhealthy"
	}
}

// Helper functions
func (rst *ResilientSemanticTracer) calculateBackoff(attempt int, policy *RetryPolicy) time.Duration {
	delay := policy.InitialDelay * time.Duration(attempt+1)
	if delay > policy.MaxDelay {
		delay = policy.MaxDelay
	}

	// Add jitter
	jitter := time.Duration(float64(delay) * policy.JitterFactor)
	delay += jitter

	return delay
}

var lastGroupCount int64

func (rst *ResilientSemanticTracer) getLastGroupCount() int64 {
	return atomic.LoadInt64(&lastGroupCount)
}

func (rst *ResilientSemanticTracer) setLastGroupCount(count int64) {
	atomic.StoreInt64(&lastGroupCount, count)
}

// Recovery Strategy implementations

type TimeoutRecovery struct{}

func (tr *TimeoutRecovery) Recover(ctx context.Context, err error, event *domain.UnifiedEvent) error {
	// For timeout errors, we might want to process with reduced scope
	return fmt.Errorf("timeout recovery not implemented")
}

func (tr *TimeoutRecovery) ShouldRetry(err error) bool {
	return err != nil && err.Error() == "context deadline exceeded"
}

type MemoryPressureRecovery struct{}

func (mr *MemoryPressureRecovery) Recover(ctx context.Context, err error, event *domain.UnifiedEvent) error {
	// For memory pressure, we might want to trigger cleanup
	return fmt.Errorf("memory recovery not implemented")
}

func (mr *MemoryPressureRecovery) ShouldRetry(err error) bool {
	return false // Don't retry memory errors
}

type CorrelationFailureRecovery struct{}

func (cr *CorrelationFailureRecovery) Recover(ctx context.Context, err error, event *domain.UnifiedEvent) error {
	// For correlation failures, we might want to process without correlation
	return fmt.Errorf("correlation recovery not implemented")
}

func (cr *CorrelationFailureRecovery) ShouldRetry(err error) bool {
	return true // Retry correlation errors
}
