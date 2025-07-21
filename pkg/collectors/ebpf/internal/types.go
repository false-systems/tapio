package internal

import (
	"errors"
	"math/rand"
)

// Common errors
var (
	ErrAlreadyStarted     = errors.New("collector already started")
	ErrNotStarted         = errors.New("collector not started")
	ErrCircuitBreakerOpen = errors.New("circuit breaker is open")
	ErrRateLimitExceeded  = errors.New("rate limit exceeded")
	ErrResourcesExhausted = errors.New("resources exhausted")
	ErrValidationFailed   = errors.New("event validation failed")
)

// EventPriority defines event priority levels
type EventPriority int

const (
	PriorityLow EventPriority = iota
	PriorityNormal
	PriorityHigh
	PriorityCritical
)

// Helper functions

// randomFloat returns a random float between 0 and 1
func randomFloat() float64 {
	return rand.Float64()
}

// DetermineEventPriority determines priority based on event type
func DetermineEventPriority(eventType string) EventPriority {
	switch eventType {
	case "security":
		return PriorityCritical
	case "network", "file":
		return PriorityHigh
	case "process":
		return PriorityNormal
	default:
		return PriorityLow
	}
}
