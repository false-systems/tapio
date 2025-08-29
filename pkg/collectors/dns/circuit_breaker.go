package dns

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// NewCircuitBreaker creates a new circuit breaker for fault tolerance
func NewCircuitBreaker(config CircuitBreakerConfig, logger *zap.Logger) *CircuitBreaker {
	return &CircuitBreaker{
		config:       config,
		state:        CircuitClosed,
		recentErrors: make([]time.Time, 0),
	}
}

// AllowRequest checks if a request should be allowed through the circuit breaker
func (cb *CircuitBreaker) AllowRequest() bool {
	if !cb.config.Enabled {
		return true
	}

	cb.mu.RLock()
	state := cb.state
	cb.mu.RUnlock()

	switch state {
	case CircuitClosed:
		// Check if we're at concurrent request limit
		current := atomic.LoadInt64(&cb.concurrentRequests)
		if current >= int64(cb.config.MaxConcurrentRequests) {
			return false
		}

		// Check error rate
		if cb.shouldOpen() {
			cb.transitionToOpen()
			return false
		}
		return true

	case CircuitOpen:
		// Check if recovery timeout has passed
		cb.mu.RLock()
		lastFailure := cb.lastFailureTime
		cb.mu.RUnlock()

		if time.Since(lastFailure) >= cb.config.RecoveryTimeout {
			cb.transitionToHalfOpen()
			return true
		}
		return false

	case CircuitHalfOpen:
		// Allow limited requests to test recovery
		cb.mu.RLock()
		successCount := cb.successCount
		cb.mu.RUnlock()

		return successCount < cb.config.SuccessThreshold

	default:
		return false
	}
}

// RecordSuccess records a successful operation
func (cb *CircuitBreaker) RecordSuccess() {
	if !cb.config.Enabled {
		return
	}

	atomic.AddInt64(&cb.totalSuccesses, 1)
	atomic.AddInt64(&cb.concurrentRequests, -1) // Decrement concurrent count

	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.lastSuccessTime = time.Now()

	switch cb.state {
	case CircuitHalfOpen:
		cb.successCount++
		if cb.successCount >= cb.config.SuccessThreshold {
			cb.transitionToClosed()
		}
	case CircuitClosed:
		// Reset failure count on success
		cb.failureCount = 0
	}
}

// RecordFailure records a failed operation
func (cb *CircuitBreaker) RecordFailure(err error) {
	if !cb.config.Enabled {
		return
	}

	atomic.AddInt64(&cb.totalFailures, 1)
	atomic.AddInt64(&cb.concurrentRequests, -1) // Decrement concurrent count

	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()
	cb.lastFailureTime = now
	cb.failureCount++

	// Add to recent errors for rate calculation
	cb.recentErrors = append(cb.recentErrors, now)
	cb.cleanupOldErrors()

	switch cb.state {
	case CircuitClosed:
		if cb.failureCount >= cb.config.FailureThreshold {
			cb.transitionToOpen()
		}
	case CircuitHalfOpen:
		// Any failure in half-open transitions back to open
		cb.transitionToOpen()
	}
}

// RecordRequest increments the concurrent request counter
func (cb *CircuitBreaker) RecordRequest() {
	if cb.config.Enabled {
		atomic.AddInt64(&cb.concurrentRequests, 1)
		atomic.AddInt64(&cb.totalRequests, 1)
	}
}

// GetState returns the current circuit breaker state
func (cb *CircuitBreaker) GetState() CircuitBreakerState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// CircuitBreakerStats represents statistics for the circuit breaker
type CircuitBreakerStats struct {
	State              string    `json:"state"`
	Enabled            bool      `json:"enabled"`
	FailureCount       int       `json:"failure_count"`
	SuccessCount       int       `json:"success_count"`
	TotalRequests      int64     `json:"total_requests"`
	TotalFailures      int64     `json:"total_failures"`
	TotalSuccesses     int64     `json:"total_successes"`
	ConcurrentRequests int64     `json:"concurrent_requests"`
	ErrorRate          float64   `json:"error_rate"`
	RecentErrorRate    float64   `json:"recent_error_rate"`
	RecentErrors       int       `json:"recent_errors"`
	LastFailureTime    time.Time `json:"last_failure_time"`
	LastSuccessTime    time.Time `json:"last_success_time"`
	FailureThreshold   int       `json:"failure_threshold"`
	SuccessThreshold   int       `json:"success_threshold"`
	MaxErrorRate       float64   `json:"max_error_rate"`
}

// GetStats returns circuit breaker statistics
func (cb *CircuitBreaker) GetStats() CircuitBreakerStats {
	cb.mu.RLock()
	state := cb.state
	failureCount := cb.failureCount
	successCount := cb.successCount
	lastFailureTime := cb.lastFailureTime
	lastSuccessTime := cb.lastSuccessTime
	recentErrorCount := len(cb.recentErrors)
	cb.mu.RUnlock()

	totalRequests := atomic.LoadInt64(&cb.totalRequests)
	totalFailures := atomic.LoadInt64(&cb.totalFailures)
	totalSuccesses := atomic.LoadInt64(&cb.totalSuccesses)
	concurrentRequests := atomic.LoadInt64(&cb.concurrentRequests)

	errorRate := 0.0
	if totalRequests > 0 {
		errorRate = float64(totalFailures) / float64(totalRequests)
	}

	recentErrorRate := cb.calculateRecentErrorRate()

	return CircuitBreakerStats{
		State:              state.String(),
		Enabled:            cb.config.Enabled,
		FailureCount:       failureCount,
		SuccessCount:       successCount,
		TotalRequests:      totalRequests,
		TotalFailures:      totalFailures,
		TotalSuccesses:     totalSuccesses,
		ConcurrentRequests: concurrentRequests,
		ErrorRate:          errorRate,
		RecentErrorRate:    recentErrorRate,
		RecentErrors:       recentErrorCount,
		LastFailureTime:    lastFailureTime,
		LastSuccessTime:    lastSuccessTime,
		FailureThreshold:   cb.config.FailureThreshold,
		SuccessThreshold:   cb.config.SuccessThreshold,
		MaxErrorRate:       cb.config.MaxErrorRate,
	}
}

// IsHealthy returns true if the circuit breaker is allowing requests
func (cb *CircuitBreaker) IsHealthy() bool {
	return cb.GetState() != CircuitOpen
}

// Reset manually resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.transitionToClosed()
}

// Private methods

func (cb *CircuitBreaker) shouldOpen() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	// Check failure count threshold
	if cb.failureCount >= cb.config.FailureThreshold {
		return true
	}

	// Check error rate threshold
	recentErrorRate := cb.calculateRecentErrorRate()
	if recentErrorRate >= cb.config.MaxErrorRate {
		return true
	}

	return false
}

func (cb *CircuitBreaker) calculateRecentErrorRate() float64 {
	if len(cb.recentErrors) == 0 {
		return 0.0
	}

	now := time.Now()
	windowStart := now.Add(-cb.config.TimeWindow)

	errorCount := 0
	for _, errorTime := range cb.recentErrors {
		if errorTime.After(windowStart) {
			errorCount++
		}
	}

	// Estimate total requests in window (simplified)
	totalInWindow := atomic.LoadInt64(&cb.totalRequests)
	if totalInWindow == 0 {
		return 0.0
	}

	// This is a simplified calculation - in production, you'd want
	// a proper time-series tracking of requests
	return float64(errorCount) / float64(totalInWindow) * 100.0
}

func (cb *CircuitBreaker) cleanupOldErrors() {
	now := time.Now()
	windowStart := now.Add(-cb.config.TimeWindow)

	// Remove errors outside the time window
	validErrors := make([]time.Time, 0, len(cb.recentErrors))
	for _, errorTime := range cb.recentErrors {
		if errorTime.After(windowStart) {
			validErrors = append(validErrors, errorTime)
		}
	}

	cb.recentErrors = validErrors
}

func (cb *CircuitBreaker) transitionToOpen() {
	if cb.state != CircuitOpen {
		cb.state = CircuitOpen
		cb.lastFailureTime = time.Now()
		cb.successCount = 0
	}
}

func (cb *CircuitBreaker) transitionToHalfOpen() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state != CircuitHalfOpen {
		cb.state = CircuitHalfOpen
		cb.successCount = 0
		cb.failureCount = 0
	}
}

func (cb *CircuitBreaker) transitionToClosed() {
	cb.state = CircuitClosed
	cb.failureCount = 0
	cb.successCount = 0
	cb.recentErrors = cb.recentErrors[:0] // Clear without reallocating
}

// Execute wraps a function call with circuit breaker protection
func (cb *CircuitBreaker) Execute(ctx context.Context, operation func() error) error {
	if !cb.AllowRequest() {
		state := cb.GetState()
		return fmt.Errorf("circuit breaker %s: request blocked", state.String())
	}

	cb.RecordRequest()

	err := operation()
	if err != nil {
		cb.RecordFailure(err)
		return err
	}

	cb.RecordSuccess()
	return nil
}

// ExecuteWithFallback wraps a function call with circuit breaker protection and fallback
func (cb *CircuitBreaker) ExecuteWithFallback(ctx context.Context, operation func() error, fallback func() error) error {
	if !cb.AllowRequest() {
		if fallback != nil {
			return fallback()
		}
		state := cb.GetState()
		return fmt.Errorf("circuit breaker %s: request blocked and no fallback provided", state.String())
	}

	cb.RecordRequest()

	err := operation()
	if err != nil {
		cb.RecordFailure(err)
		if fallback != nil {
			return fallback()
		}
		return err
	}

	cb.RecordSuccess()
	return nil
}
