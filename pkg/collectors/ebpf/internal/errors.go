package internal

import "errors"

// Common errors
var (

	// ErrCircuitOpen is returned when circuit breaker is open
	ErrCircuitOpen = errors.New("circuit breaker is open")

	// ErrInvalidEvent is returned when an event fails validation
	ErrInvalidEvent = errors.New("invalid event")

	// ErrSecurityViolation is returned when an event violates security policies
	ErrSecurityViolation = errors.New("security violation")
)
