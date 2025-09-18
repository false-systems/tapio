package network

import (
	"sync"
	"time"
)

// RateLimiter provides token bucket rate limiting
type RateLimiter struct {
	maxEvents  int
	refillRate int
	tokens     int
	lastRefill time.Time
	mu         sync.Mutex
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(maxEventsPerSecond int) *RateLimiter {
	return &RateLimiter{
		maxEvents:  maxEventsPerSecond,
		refillRate: maxEventsPerSecond,
		tokens:     maxEventsPerSecond,
		lastRefill: time.Now(),
	}
}

// Allow checks if an event is allowed
func (r *RateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Refill tokens based on time elapsed
	now := time.Now()
	elapsed := now.Sub(r.lastRefill)
	tokensToAdd := int(elapsed.Seconds() * float64(r.refillRate))

	if tokensToAdd > 0 {
		r.tokens = min(r.tokens+tokensToAdd, r.maxEvents)
		r.lastRefill = now
	}

	// Check if we have tokens available
	if r.tokens > 0 {
		r.tokens--
		return true
	}

	return false
}

// Reset resets the rate limiter
func (r *RateLimiter) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tokens = r.maxEvents
	r.lastRefill = time.Now()
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
