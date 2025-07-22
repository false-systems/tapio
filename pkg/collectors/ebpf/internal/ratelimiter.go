package internal

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// RateLimiter implements token bucket algorithm for rate limiting
type RateLimiter struct {
	mu          sync.Mutex
	maxTokens   int64
	tokens      int64
	refillRate  int64 // tokens per second
	lastRefill  time.Time
	metrics     *RateLimiterMetrics
	currentRate atomic.Int64 // Current rate limit
}

// RateLimiterMetrics tracks rate limiter statistics
type RateLimiterMetrics struct {
	mu             sync.RWMutex
	allowed        uint64
	limited        uint64
	currentTokens  int64
	utilizationPct float64
}

// NewRateLimiterSimple creates a new rate limiter with simple params
func NewRateLimiterSimple(maxEventsPerSecond int64) *RateLimiter {
	if maxEventsPerSecond <= 0 {
		maxEventsPerSecond = 10000 // Default: 10k events/sec
	}

	rl := &RateLimiter{
		maxTokens:  maxEventsPerSecond,
		tokens:     maxEventsPerSecond,
		refillRate: maxEventsPerSecond,
		lastRefill: time.Now(),
		metrics:    &RateLimiterMetrics{},
	}
	rl.currentRate.Store(maxEventsPerSecond)
	return rl
}

// AllowInternal checks if an event can be processed (internal method)
func (r *RateLimiter) AllowInternal() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Refill tokens based on elapsed time
	r.refill()

	// Check if we have tokens available
	if r.tokens > 0 {
		r.tokens--
		r.updateMetrics(true)
		return true
	}

	r.updateMetrics(false)
	return false
}

// AllowN checks if N events can be processed
func (r *RateLimiter) AllowN(n int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.refill()

	if r.tokens >= n {
		r.tokens -= n
		r.updateMetrics(true)
		return true
	}

	r.updateMetrics(false)
	return false
}

// refill adds tokens based on elapsed time
func (r *RateLimiter) refill() {
	now := time.Now()
	elapsed := now.Sub(r.lastRefill).Seconds()

	if elapsed > 0 {
		tokensToAdd := int64(elapsed * float64(r.refillRate))
		r.tokens += tokensToAdd

		// Cap at max tokens
		if r.tokens > r.maxTokens {
			r.tokens = r.maxTokens
		}

		r.lastRefill = now
	}
}

// updateMetrics updates rate limiter metrics
func (r *RateLimiter) updateMetrics(allowed bool) {
	r.metrics.mu.Lock()
	defer r.metrics.mu.Unlock()

	if allowed {
		r.metrics.allowed++
	} else {
		r.metrics.limited++
	}

	r.metrics.currentTokens = r.tokens
	if r.maxTokens > 0 {
		r.metrics.utilizationPct = float64(r.maxTokens-r.tokens) / float64(r.maxTokens) * 100
	}
}

// GetMetrics returns current rate limiter metrics
func (r *RateLimiter) GetMetrics() RateLimiterMetrics {
	r.metrics.mu.RLock()
	defer r.metrics.mu.RUnlock()

	return RateLimiterMetrics{
		allowed:        r.metrics.allowed,
		limited:        r.metrics.limited,
		currentTokens:  r.metrics.currentTokens,
		utilizationPct: r.metrics.utilizationPct,
	}
}

// Reset resets the rate limiter
func (r *RateLimiter) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.tokens = r.maxTokens
	r.lastRefill = time.Now()

	r.metrics.mu.Lock()
	r.metrics.allowed = 0
	r.metrics.limited = 0
	r.metrics.mu.Unlock()
}

// RateLimiterConfig configures the rate limiter
type RateLimiterConfig struct {
	MaxEventsPerSecond   int64
	EnableAdaptive       bool
	EnableCircuitBreaker bool
	EnableBackpressure   bool
}

// DefaultRateLimiterConfig returns default rate limiter configuration
func DefaultRateLimiterConfig() RateLimiterConfig {
	return RateLimiterConfig{
		MaxEventsPerSecond:   10000,
		EnableAdaptive:       true,
		EnableCircuitBreaker: true,
		EnableBackpressure:   true,
	}
}

// NewRateLimiter creates a new rate limiter with config
func NewRateLimiter(config RateLimiterConfig) *RateLimiter {
	if config.MaxEventsPerSecond <= 0 {
		config.MaxEventsPerSecond = 10000
	}

	rl := &RateLimiter{
		maxTokens:  config.MaxEventsPerSecond,
		tokens:     config.MaxEventsPerSecond,
		refillRate: config.MaxEventsPerSecond,
		lastRefill: time.Now(),
		metrics:    &RateLimiterMetrics{},
	}
	rl.currentRate.Store(config.MaxEventsPerSecond)
	return rl
}

// AllowWithContext checks if an event can be processed (with context support)
func (r *RateLimiter) Allow(ctx context.Context) bool {
	// Check context first
	select {
	case <-ctx.Done():
		return false
	default:
	}

	return r.AllowInternal()
}

// ReportSuccess reports a successful event processing
func (r *RateLimiter) ReportSuccess() {
	// Update metrics for successful processing
	r.metrics.mu.Lock()
	defer r.metrics.mu.Unlock()
	// Could track success rate here
}

// ReportError reports an error during event processing
func (r *RateLimiter) ReportError(err error) {
	// Update metrics for errors
	r.metrics.mu.Lock()
	defer r.metrics.mu.Unlock()
	// Could track error types and rates here
}

// UpdateLoad updates the current system load
func (r *RateLimiter) UpdateLoad(load int64) {
	// Could adjust rate based on load
	r.metrics.mu.Lock()
	defer r.metrics.mu.Unlock()
	// Adaptive rate limiting based on load
}

// Stop gracefully stops the rate limiter
func (r *RateLimiter) Stop() {
	// Clean shutdown
	r.Reset()
}

// GetMetricsMap returns metrics as map[string]interface{}
func (r *RateLimiter) GetMetricsMap() map[string]interface{} {
	r.metrics.mu.RLock()
	defer r.metrics.mu.RUnlock()

	return map[string]interface{}{
		"allowed":         r.metrics.allowed,
		"limited":         r.metrics.limited,
		"current_tokens":  r.metrics.currentTokens,
		"utilization_pct": r.metrics.utilizationPct,
	}
}
