package internal

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// RateLimiter implements token bucket algorithm for rate limiting CNI events
type RateLimiter struct {
	mu          sync.Mutex
	maxTokens   int64
	tokens      int64
	refillRate  int64 // tokens per second
	lastRefill  time.Time
	metrics     *RateLimiterMetrics
	currentRate atomic.Int64
}

// RateLimiterMetrics tracks rate limiter statistics
type RateLimiterMetrics struct {
	mu             sync.RWMutex
	allowed        uint64
	limited        uint64
	currentTokens  int64
	utilizationPct float64
}

// NewRateLimiter creates a new rate limiter for CNI events
func NewRateLimiter(maxEventsPerSecond int64) *RateLimiter {
	if maxEventsPerSecond <= 0 {
		maxEventsPerSecond = 1000 // Default: 1k events/sec for CNI
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

// Allow checks if an event can be processed with context support
func (r *RateLimiter) Allow(ctx context.Context) bool {
	// Check context first
	select {
	case <-ctx.Done():
		return false
	default:
	}

	return r.allowInternal()
}

// allowInternal checks if an event can be processed
func (r *RateLimiter) allowInternal() bool {
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

// GetMetricsMap returns metrics as map for monitoring
func (r *RateLimiter) GetMetricsMap() map[string]interface{} {
	r.metrics.mu.RLock()
	defer r.metrics.mu.RUnlock()

	return map[string]interface{}{
		"allowed":         r.metrics.allowed,
		"limited":         r.metrics.limited,
		"current_tokens":  r.metrics.currentTokens,
		"utilization_pct": r.metrics.utilizationPct,
		"current_rate":    r.currentRate.Load(),
	}
}

// UpdateRate dynamically updates the rate limit
func (r *RateLimiter) UpdateRate(newRate int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.maxTokens = newRate
	r.refillRate = newRate
	r.currentRate.Store(newRate)

	// Adjust current tokens if needed
	if r.tokens > r.maxTokens {
		r.tokens = r.maxTokens
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
