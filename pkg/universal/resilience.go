package universal

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ResilienceManager handles graceful degradation and fallback patterns
type ResilienceManager struct {
	fallbackGenerators map[string]FallbackGenerator
	errorHandlers      map[string]ErrorHandler
	circuitBreakers    map[string]*CircuitBreaker
	mu                 sync.RWMutex
}

// FallbackGenerator generates fallback data when primary sources fail
type FallbackGenerator interface {
	GenerateFallback(ctx context.Context, target Target) (interface{}, error)
	CanHandle(dataType string) bool
}

// ErrorHandler handles specific error conditions
type ErrorHandler interface {
	HandleError(err error, context map[string]interface{}) error
	ShouldRetry(err error) bool
	GetBackoffDuration(attempt int) time.Duration
}

// NewResilienceManager creates a new resilience manager
func NewResilienceManager() *ResilienceManager {
	rm := &ResilienceManager{
		fallbackGenerators: make(map[string]FallbackGenerator),
		errorHandlers:      make(map[string]ErrorHandler),
		circuitBreakers:    make(map[string]*CircuitBreaker),
	}

	// Register default components
	rm.RegisterFallbackGenerator("metrics", &MetricFallbackGenerator{})
	rm.RegisterFallbackGenerator("events", &EventFallbackGenerator{})
	rm.RegisterErrorHandler("default", &DefaultErrorHandler{})

	return rm
}

// RegisterFallbackGenerator registers a fallback generator
func (rm *ResilienceManager) RegisterFallbackGenerator(name string, generator FallbackGenerator) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	rm.fallbackGenerators[name] = generator
}

// RegisterErrorHandler registers an error handler
func (rm *ResilienceManager) RegisterErrorHandler(name string, handler ErrorHandler) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	rm.errorHandlers[name] = handler
}

// GetCircuitBreaker gets or creates a circuit breaker for a service
func (rm *ResilienceManager) GetCircuitBreaker(service string) *CircuitBreaker {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if cb, exists := rm.circuitBreakers[service]; exists {
		return cb
	}

	cb := NewCircuitBreaker(service, 5, 30*time.Second)
	rm.circuitBreakers[service] = cb
	return cb
}

// ExecuteWithFallback executes a function with fallback support
func (rm *ResilienceManager) ExecuteWithFallback(
	ctx context.Context,
	service string,
	target Target,
	fn func() (interface{}, error),
	fallbackType string,
) (result interface{}, usedFallback bool, err error) {

	// Get circuit breaker
	cb := rm.GetCircuitBreaker(service)

	// Check if circuit is open
	if !cb.CanExecute() {
		// Use fallback immediately
		rm.mu.RLock()
		generator, exists := rm.fallbackGenerators[fallbackType]
		rm.mu.RUnlock()

		if exists && generator.CanHandle(fallbackType) {
			result, err = generator.GenerateFallback(ctx, target)
			return result, true, err
		}

		return nil, false, fmt.Errorf("circuit breaker open for service %s", service)
	}

	// Try primary function
	result, err = fn()

	if err == nil {
		cb.RecordSuccess()
		return result, false, nil
	}

	// Record failure
	cb.RecordFailure()

	// Try error handling
	rm.mu.RLock()
	handler, exists := rm.errorHandlers["default"]
	rm.mu.RUnlock()

	if exists && handler.ShouldRetry(err) {
		// Simple retry with backoff
		for attempt := 1; attempt <= 3; attempt++ {
			select {
			case <-ctx.Done():
				return nil, false, ctx.Err()
			case <-time.After(handler.GetBackoffDuration(attempt)):
				result, err = fn()
				if err == nil {
					cb.RecordSuccess()
					return result, false, nil
				}
			}
		}
	}

	// All retries failed, use fallback
	rm.mu.RLock()
	generator, exists := rm.fallbackGenerators[fallbackType]
	rm.mu.RUnlock()

	if exists && generator.CanHandle(fallbackType) {
		result, err = generator.GenerateFallback(ctx, target)
		return result, true, err
	}

	return nil, false, fmt.Errorf("all attempts failed and no fallback available: %w", err)
}

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	service          string
	failureThreshold int
	resetTimeout     time.Duration

	mu              sync.Mutex
	failures        int
	lastFailureTime time.Time
	state           CircuitState
}

// CircuitState represents the state of a circuit breaker
type CircuitState int

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(service string, threshold int, timeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		service:          service,
		failureThreshold: threshold,
		resetTimeout:     timeout,
		state:            CircuitClosed,
	}
}

// CanExecute checks if the circuit breaker allows execution
func (cb *CircuitBreaker) CanExecute() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()

	switch cb.state {
	case CircuitClosed:
		return true

	case CircuitOpen:
		// Check if we should transition to half-open
		if now.Sub(cb.lastFailureTime) > cb.resetTimeout {
			cb.state = CircuitHalfOpen
			cb.failures = 0
			return true
		}
		return false

	case CircuitHalfOpen:
		return true

	default:
		return false
	}
}

// RecordSuccess records a successful execution
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == CircuitHalfOpen {
		cb.state = CircuitClosed
	}
	cb.failures = 0
}

// RecordFailure records a failed execution
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailureTime = time.Now()

	if cb.failures >= cb.failureThreshold {
		cb.state = CircuitOpen
	}
}

// MetricFallbackGenerator generates fallback metrics
type MetricFallbackGenerator struct{}

// GenerateFallback generates a fallback metric
func (g *MetricFallbackGenerator) GenerateFallback(ctx context.Context, target Target) (interface{}, error) {
	metric := GetMetric()

	metric.ID = fmt.Sprintf("fallback_%s_%d", target.Name, time.Now().UnixNano())
	metric.Timestamp = time.Now()
	metric.Target = target
	metric.Name = "fallback_metric"
	metric.Value = -1 // Indicates fallback
	metric.Unit = "none"
	metric.Type = MetricTypeGauge
	metric.FallbackUsed = true
	metric.ErrorContext = "Primary data source unavailable"

	metric.Quality = DataQuality{
		Confidence: 0.1, // Very low confidence for fallback data
		Source:     "fallback_generator",
		Version:    "1.0",
		Tags: map[string]string{
			"type": "synthetic",
		},
	}

	return metric, nil
}

// CanHandle checks if this generator can handle the data type
func (g *MetricFallbackGenerator) CanHandle(dataType string) bool {
	return dataType == "metrics"
}

// EventFallbackGenerator generates fallback events
type EventFallbackGenerator struct{}

// GenerateFallback generates a fallback event
func (g *EventFallbackGenerator) GenerateFallback(ctx context.Context, target Target) (interface{}, error) {
	event := GetEvent()

	event.ID = fmt.Sprintf("fallback_event_%s_%d", target.Name, time.Now().UnixNano())
	event.Timestamp = time.Now()
	event.Target = target
	event.Type = EventTypeCustom
	event.Level = EventLevelWarning
	event.Message = "Data collection temporarily unavailable"
	event.Details = map[string]interface{}{
		"reason": "fallback_mode",
		"target": target,
	}

	event.Quality = DataQuality{
		Confidence: 0.1,
		Source:     "fallback_generator",
		Version:    "1.0",
		Tags: map[string]string{
			"type": "synthetic",
		},
	}

	return event, nil
}

// CanHandle checks if this generator can handle the data type
func (g *EventFallbackGenerator) CanHandle(dataType string) bool {
	return dataType == "events"
}

// DefaultErrorHandler provides default error handling
type DefaultErrorHandler struct {
	baseBackoff time.Duration
	maxBackoff  time.Duration
}

// HandleError handles an error
func (h *DefaultErrorHandler) HandleError(err error, context map[string]interface{}) error {
	// Log error with context
	// In a real implementation, this would use proper logging
	return err
}

// ShouldRetry determines if an error should trigger a retry
func (h *DefaultErrorHandler) ShouldRetry(err error) bool {
	// Don't retry on context errors
	if err == context.Canceled || err == context.DeadlineExceeded {
		return false
	}

	// Retry on temporary errors
	// In a real implementation, check for specific error types
	return true
}

// GetBackoffDuration calculates backoff duration for an attempt
func (h *DefaultErrorHandler) GetBackoffDuration(attempt int) time.Duration {
	if h.baseBackoff == 0 {
		h.baseBackoff = 100 * time.Millisecond
	}
	if h.maxBackoff == 0 {
		h.maxBackoff = 30 * time.Second
	}

	// Exponential backoff with jitter
	backoff := h.baseBackoff * time.Duration(1<<uint(attempt-1))
	if backoff > h.maxBackoff {
		backoff = h.maxBackoff
	}

	// Add jitter (±25%)
	jitter := time.Duration(float64(backoff) * 0.25)
	if jitter > 0 {
		backoff = backoff - jitter + time.Duration(time.Now().UnixNano()%(int64(jitter)*2))
	}

	return backoff
}

// PartialDataHandler handles partial data scenarios
type PartialDataHandler struct {
	minDataThreshold float64 // Minimum percentage of data required
}

// NewPartialDataHandler creates a new partial data handler
func NewPartialDataHandler(threshold float64) *PartialDataHandler {
	return &PartialDataHandler{
		minDataThreshold: threshold,
	}
}

// ProcessPartialData processes partial data and adds quality indicators
func (h *PartialDataHandler) ProcessPartialData(
	dataset *UniversalDataset,
	expectedCount int,
	actualCount int,
) error {

	completeness := float64(actualCount) / float64(expectedCount)

	// Update quality based on completeness
	dataset.OverallQuality.Confidence *= completeness
	dataset.OverallQuality.Tags["partial_data"] = "true"
	dataset.OverallQuality.Tags["completeness"] = fmt.Sprintf("%.2f", completeness)
	dataset.OverallQuality.Metadata["expected_count"] = expectedCount
	dataset.OverallQuality.Metadata["actual_count"] = actualCount

	if completeness < h.minDataThreshold {
		return fmt.Errorf("insufficient data: %.2f%% (minimum: %.2f%%)",
			completeness*100, h.minDataThreshold*100)
	}

	return nil
}
