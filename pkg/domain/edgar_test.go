package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RED: Test ObserverEvent has Edgar correlation fields
func TestObserverEvent_EdgarFields(t *testing.T) {
	duration := uint64(5000000) // 5 seconds in microseconds

	event := &ObserverEvent{
		ID:        NewEventID(),
		Type:      "kernel",
		Subtype:   "oom_kill",
		Source:    "container-observer",
		Timestamp: time.Now(),

		// Edgar correlation fields
		TraceID:      "abc123",     // ❌ Already exists
		SpanID:       "span-oom-1", // ❌ Already exists
		ParentSpanID: "span-pod-1", // ❌ Doesn't exist yet - test will FAIL
		Duration:     &duration,    // ❌ Doesn't exist yet - test will FAIL
		TraceFlags:   0x01,         // ❌ Already exists

		// Event classification
		Severity: SeverityCritical, // ❌ Doesn't exist yet - test will FAIL
		Outcome:  OutcomeFailure,   // ❌ Doesn't exist yet - test will FAIL

		// Structured error
		Error: &EventError{ // ❌ Doesn't exist yet - test will FAIL
			Code:    "OOM_KILL",
			Message: "Container killed: out of memory",
			Cause:   "Memory limit: 512Mi, Requested: 2Gi",
		},
	}

	// Verify Edgar fields are populated
	require.NotNil(t, event)
	assert.Equal(t, "span-pod-1", event.ParentSpanID)
	assert.NotNil(t, event.Duration)
	assert.Equal(t, uint64(5000000), *event.Duration)
	assert.Equal(t, SeverityCritical, event.Severity)
	assert.Equal(t, OutcomeFailure, event.Outcome)
	require.NotNil(t, event.Error)
	assert.Equal(t, "OOM_KILL", event.Error.Code)
	assert.Equal(t, "Container killed: out of memory", event.Error.Message)
}

// RED: Test Severity enum values
func TestSeverity_Values(t *testing.T) {
	tests := []struct {
		name     string
		severity Severity // ❌ Doesn't exist yet
		expected string
	}{
		{"debug", SeverityDebug, "debug"},          // ❌ Doesn't exist yet
		{"info", SeverityInfo, "info"},             // ❌ Doesn't exist yet
		{"warning", SeverityWarning, "warning"},    // ❌ Doesn't exist yet
		{"error", SeverityError, "error"},          // ❌ Doesn't exist yet
		{"critical", SeverityCritical, "critical"}, // ❌ Doesn't exist yet
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.severity))
		})
	}
}

// RED: Test Outcome enum values
func TestOutcome_Values(t *testing.T) {
	tests := []struct {
		name     string
		outcome  Outcome // ❌ Doesn't exist yet
		expected string
	}{
		{"success", OutcomeSuccess, "success"}, // ❌ Doesn't exist yet
		{"failure", OutcomeFailure, "failure"}, // ❌ Doesn't exist yet
		{"unknown", OutcomeUnknown, "unknown"}, // ❌ Doesn't exist yet
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.outcome))
		})
	}
}

// RED: Test EventError structure
func TestEventError_Fields(t *testing.T) {
	err := &EventError{ // ❌ Doesn't exist yet
		Code:    "ECONNREFUSED",
		Message: "Connection refused",
		Stack:   "stack trace here",
		Cause:   "Service not running",
	}

	assert.Equal(t, "ECONNREFUSED", err.Code)
	assert.Equal(t, "Connection refused", err.Message)
	assert.Equal(t, "stack trace here", err.Stack)
	assert.Equal(t, "Service not running", err.Cause)
}

// RED: Test causality chain via ParentSpanID
func TestObserverEvent_CausalityChain(t *testing.T) {
	// Deployment update (root cause)
	deployment := &ObserverEvent{
		ID:       NewEventID(),
		SpanID:   "span-dep-1",
		Severity: SeverityInfo,   // ❌ Doesn't exist yet
		Outcome:  OutcomeSuccess, // ❌ Doesn't exist yet
	}

	// Pod restart (caused by deployment)
	pod := &ObserverEvent{
		ID:           NewEventID(),
		SpanID:       "span-pod-1",
		ParentSpanID: "span-dep-1",    // ❌ Doesn't exist yet - points to deployment
		Severity:     SeverityWarning, // ❌ Doesn't exist yet
		Outcome:      OutcomeFailure,  // ❌ Doesn't exist yet
	}

	// OOM kill (caused by pod restart)
	oom := &ObserverEvent{
		ID:           NewEventID(),
		SpanID:       "span-oom-1",
		ParentSpanID: "span-pod-1",     // ❌ Doesn't exist yet - points to pod
		Severity:     SeverityCritical, // ❌ Doesn't exist yet
		Outcome:      OutcomeFailure,   // ❌ Doesn't exist yet
	}

	// Verify causality chain
	assert.Empty(t, deployment.ParentSpanID, "Deployment is root (no parent)")
	assert.Equal(t, deployment.SpanID, pod.ParentSpanID, "Pod caused by deployment")
	assert.Equal(t, pod.SpanID, oom.ParentSpanID, "OOM caused by pod restart")
}

// RED: Test Duration is optional (pointer type)
func TestObserverEvent_DurationOptional(t *testing.T) {
	// Event without duration (e.g., state change)
	eventNoDuration := &ObserverEvent{
		ID:       NewEventID(),
		Duration: nil, // ❌ Doesn't exist yet - optional
	}
	assert.Nil(t, eventNoDuration.Duration)

	// Event with duration (e.g., network request)
	duration := uint64(150000) // 150ms in microseconds
	eventWithDuration := &ObserverEvent{
		ID:       NewEventID(),
		Duration: &duration, // ❌ Doesn't exist yet
	}
	require.NotNil(t, eventWithDuration.Duration)
	assert.Equal(t, uint64(150000), *eventWithDuration.Duration)
}

// RED: Test Error is optional (only present for failures)
func TestObserverEvent_ErrorOptional(t *testing.T) {
	// Success event (no error)
	success := &ObserverEvent{
		ID:       NewEventID(),
		Outcome:  OutcomeSuccess, // ❌ Doesn't exist yet
		Error:    nil,            // No error for success
		Severity: SeverityInfo,   // ❌ Doesn't exist yet
	}
	assert.Nil(t, success.Error)
	assert.Equal(t, OutcomeSuccess, success.Outcome)

	// Failure event (with error)
	failure := &ObserverEvent{
		ID:       NewEventID(),
		Outcome:  OutcomeFailure, // ❌ Doesn't exist yet
		Severity: SeverityError,  // ❌ Doesn't exist yet
		Error: &EventError{ // ❌ Doesn't exist yet
			Code:    "ETIMEDOUT",
			Message: "Connection timed out",
		},
	}
	require.NotNil(t, failure.Error)
	assert.Equal(t, OutcomeFailure, failure.Outcome)
	assert.Equal(t, "ETIMEDOUT", failure.Error.Code)
}
