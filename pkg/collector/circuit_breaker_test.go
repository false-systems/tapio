package collector

import (
	"fmt"
	"testing"
	"time"
)

func TestCircuitBreaker_Closed(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 3,
		RecoveryTimeout:  100 * time.Millisecond,
	})

	// Should allow execution when closed
	err := cb.Execute(func() error {
		return nil
	})
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if cb.GetState() != "closed" {
		t.Errorf("Expected state to be closed, got %s", cb.GetState())
	}
}

func TestCircuitBreaker_OpenAfterFailures(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 3,
		RecoveryTimeout:  100 * time.Millisecond,
	})

	// Trigger failures to open the circuit breaker
	for i := 0; i < 3; i++ {
		cb.Execute(func() error {
			return fmt.Errorf("test failure")
		})
	}

	if cb.GetState() != "open" {
		t.Errorf("Expected state to be open after failures, got %s", cb.GetState())
	}

	// Should reject execution when open
	err := cb.Execute(func() error {
		return nil
	})
	if err == nil {
		t.Error("Expected error when circuit breaker is open")
	}
}

func TestCircuitBreaker_HalfOpenRecovery(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 2,
		RecoveryTimeout:  50 * time.Millisecond,
	})

	// Trigger failures to open the circuit breaker
	for i := 0; i < 2; i++ {
		cb.Execute(func() error {
			return fmt.Errorf("test failure")
		})
	}

	if cb.GetState() != "open" {
		t.Errorf("Expected state to be open, got %s", cb.GetState())
	}

	// Wait for recovery timeout
	time.Sleep(60 * time.Millisecond)

	// Should allow one test request (half-open)
	err := cb.Execute(func() error {
		return nil // Success
	})
	if err != nil {
		t.Errorf("Expected success in half-open state, got %v", err)
	}

	// Should be closed again after success
	if cb.GetState() != "closed" {
		t.Errorf("Expected state to be closed after successful recovery, got %s", cb.GetState())
	}
}

func TestCircuitBreaker_HalfOpenFailure(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 1,
		RecoveryTimeout:  50 * time.Millisecond,
	})

	// Trigger failure to open
	cb.Execute(func() error {
		return fmt.Errorf("test failure")
	})

	// Wait for recovery timeout
	time.Sleep(60 * time.Millisecond)

	// Fail in half-open state
	err := cb.Execute(func() error {
		return fmt.Errorf("half-open failure")
	})
	if err == nil {
		t.Error("Expected error when half-open test fails")
	}

	// Should be open again
	if cb.GetState() != "open" {
		t.Errorf("Expected state to be open after half-open failure, got %s", cb.GetState())
	}
}

func TestCircuitBreaker_StateChangeCallback(t *testing.T) {
	stateChanges := []string{}
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 2,
		RecoveryTimeout:  50 * time.Millisecond,
		OnStateChange: func(state string) {
			stateChanges = append(stateChanges, state)
		},
	})

	// Trigger failures
	for i := 0; i < 2; i++ {
		cb.Execute(func() error {
			return fmt.Errorf("test failure")
		})
	}

	// Wait for recovery
	time.Sleep(60 * time.Millisecond)

	// Success in half-open
	cb.Execute(func() error {
		return nil
	})

	expectedStates := []string{"open", "half-open", "closed"}
	if len(stateChanges) != len(expectedStates) {
		t.Errorf("Expected %d state changes, got %d", len(expectedStates), len(stateChanges))
	}

	for i, expected := range expectedStates {
		if i >= len(stateChanges) || stateChanges[i] != expected {
			t.Errorf("Expected state change %d to be %s, got %v", i, expected, stateChanges)
		}
	}
}

func TestCircuitBreaker_GetStats(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 2,
		RecoveryTimeout:  100 * time.Millisecond,
	})

	// Execute some operations
	cb.Execute(func() error { return nil })
	cb.Execute(func() error { return fmt.Errorf("failure") })

	stats := cb.GetStats()

	if stats["state"] != "closed" {
		t.Errorf("Expected state to be closed, got %v", stats["state"])
	}

	if stats["failure_count"].(int32) != 1 {
		t.Errorf("Expected failure_count to be 1, got %v", stats["failure_count"])
	}

	if stats["success_count"].(int32) != 1 {
		t.Errorf("Expected success_count to be 1, got %v", stats["success_count"])
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 1,
		RecoveryTimeout:  100 * time.Millisecond,
	})

	// Trigger failure to open
	cb.Execute(func() error {
		return fmt.Errorf("test failure")
	})

	if cb.GetState() != "open" {
		t.Errorf("Expected state to be open, got %s", cb.GetState())
	}

	// Reset
	cb.Reset()

	if cb.GetState() != "closed" {
		t.Errorf("Expected state to be closed after reset, got %s", cb.GetState())
	}

	stats := cb.GetStats()
	if stats["failure_count"].(int32) != 0 {
		t.Errorf("Expected failure_count to be 0 after reset, got %v", stats["failure_count"])
	}
}