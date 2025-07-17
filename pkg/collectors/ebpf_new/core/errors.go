package core

import (
	"fmt"
	"time"
)

// ValidationError represents a configuration validation error
type ValidationError struct {
	Field   string
	Value   interface{}
	Message string
}

func (e ValidationError) Error() string {
	if e.Value != nil {
		return fmt.Sprintf("validation failed for field %s (value: %v): %s", e.Field, e.Value, e.Message)
	}
	return fmt.Sprintf("validation failed for field %s: %s", e.Field, e.Message)
}

// ProgramLoadError represents an error loading an eBPF program
type ProgramLoadError struct {
	ProgramName string
	ProgramType ProgramType
	Cause       error
}

func (e ProgramLoadError) Error() string {
	return fmt.Sprintf("failed to load eBPF program %s (type: %s): %v", e.ProgramName, e.ProgramType, e.Cause)
}

func (e ProgramLoadError) Unwrap() error {
	return e.Cause
}

// AttachError represents an error attaching an eBPF program
type AttachError struct {
	ProgramName  string
	AttachTarget string
	Cause        error
}

func (e AttachError) Error() string {
	return fmt.Sprintf("failed to attach eBPF program %s to %s: %v", e.ProgramName, e.AttachTarget, e.Cause)
}

func (e AttachError) Unwrap() error {
	return e.Cause
}

// MapError represents an error with eBPF map operations
type MapError struct {
	MapName   string
	Operation string
	Cause     error
}

func (e MapError) Error() string {
	return fmt.Sprintf("eBPF map error on %s (operation: %s): %v", e.MapName, e.Operation, e.Cause)
}

func (e MapError) Unwrap() error {
	return e.Cause
}

// RingBufferError represents an error with ring buffer operations
type RingBufferError struct {
	Operation string
	Lost      uint64
	Cause     error
}

func (e RingBufferError) Error() string {
	if e.Lost > 0 {
		return fmt.Sprintf("ring buffer error during %s (lost %d events): %v", e.Operation, e.Lost, e.Cause)
	}
	return fmt.Sprintf("ring buffer error during %s: %v", e.Operation, e.Cause)
}

func (e RingBufferError) Unwrap() error {
	return e.Cause
}

// ParseError represents an error parsing eBPF events
type ParseError struct {
	EventType EventType
	DataSize  int
	Cause     error
}

func (e ParseError) Error() string {
	return fmt.Sprintf("failed to parse eBPF event (type: %s, size: %d bytes): %v", e.EventType, e.DataSize, e.Cause)
}

func (e ParseError) Unwrap() error {
	return e.Cause
}

// PermissionError represents insufficient privileges for eBPF operations
type PermissionError struct {
	Operation   string
	Requirement string
}

func (e PermissionError) Error() string {
	return fmt.Sprintf("insufficient privileges for %s: %s", e.Operation, e.Requirement)
}

// RateLimitError represents rate limit exceeded
type RateLimitError struct {
	Limit      int
	Window     time.Duration
	RetryAfter time.Duration
}

func (e RateLimitError) Error() string {
	return fmt.Sprintf("rate limit exceeded: %d events per %v (retry after %v)", e.Limit, e.Window, e.RetryAfter)
}

// TimeoutError represents an operation timeout
type TimeoutError struct {
	Operation string
	Timeout   time.Duration
}

func (e TimeoutError) Error() string {
	return fmt.Sprintf("operation %s timed out after %v", e.Operation, e.Timeout)
}

// NotSupportedError represents an unsupported operation on the current platform
type NotSupportedError struct {
	Feature  string
	Platform string
	Reason   string
}

func (e NotSupportedError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("%s is not supported on %s: %s", e.Feature, e.Platform, e.Reason)
	}
	return fmt.Sprintf("%s is not supported on %s", e.Feature, e.Platform)
}

// CollectorClosedError represents an operation on a closed collector
type CollectorClosedError struct {
	Operation string
}

func (e CollectorClosedError) Error() string {
	return fmt.Sprintf("cannot perform %s: collector is closed", e.Operation)
}

// InvalidEventError represents an invalid event that cannot be processed
type InvalidEventError struct {
	Reason string
}

func (e InvalidEventError) Error() string {
	return fmt.Sprintf("invalid event: %s", e.Reason)
}