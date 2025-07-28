package hardening

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestCircuitBreaker_NewCircuitBreaker(t *testing.T) {
	failureThreshold := 5
	timeout := 30 * time.Second
	
	cb := NewCircuitBreaker(failureThreshold, timeout)
	
	if cb.failureThreshold != failureThreshold {
		t.Errorf("Expected failure threshold %d, got %d", failureThreshold, cb.failureThreshold)
	}
	
	if cb.timeout != timeout {
		t.Errorf("Expected timeout %v, got %v", timeout, cb.timeout)
	}
	
	if cb.successThreshold != failureThreshold/2 {
		t.Errorf("Expected success threshold %d, got %d", failureThreshold/2, cb.successThreshold)
	}
	
	if cb.GetState() != "closed" {
		t.Errorf("Expected initial state 'closed', got '%s'", cb.GetState())
	}
}

func TestCircuitBreaker_ClosedState(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Second)
	
	// Should allow calls in closed state
	err := cb.Call(func() error { return nil })
	if err != nil {
		t.Errorf("Expected nil error in closed state, got %v", err)
	}
	
	if cb.GetState() != "closed" {
		t.Errorf("Expected state 'closed', got '%s'", cb.GetState())
	}
}

func TestCircuitBreaker_OpenState(t *testing.T) {
	cb := NewCircuitBreaker(2, time.Hour) // Long timeout to keep it open
	
	// Cause failures to open the circuit
	testErr := errors.New("test error")
	cb.Call(func() error { return testErr })
	cb.Call(func() error { return testErr })
	
	// Should be open now
	if cb.GetState() != "open" {
		t.Errorf("Expected state 'open', got '%s'", cb.GetState())
	}
	
	// Should reject calls
	err := cb.Call(func() error { return nil })
	if !errors.Is(err, ErrCircuitBreakerOpen) {
		t.Errorf("Expected ErrCircuitBreakerOpen, got %v", err)
	}
}

func TestCircuitBreaker_HalfOpenState(t *testing.T) {
	cb := NewCircuitBreaker(2, 10*time.Millisecond) // Short timeout
	
	// Cause failures to open the circuit
	testErr := errors.New("test error")
	cb.Call(func() error { return testErr })
	cb.Call(func() error { return testErr })
	
	// Wait for timeout
	time.Sleep(20 * time.Millisecond)
	
	// Next call should transition to half-open
	callMade := false
	err := cb.Call(func() error { 
		callMade = true
		return nil 
	})
	
	if err != nil {
		t.Errorf("Expected nil error in half-open state, got %v", err)
	}
	
	if !callMade {
		t.Error("Expected function to be called in half-open state")
	}
}

func TestCircuitBreaker_HalfOpenToOpen(t *testing.T) {
	cb := NewCircuitBreaker(2, 10*time.Millisecond)
	
	// Open the circuit
	testErr := errors.New("test error")
	cb.Call(func() error { return testErr })
	cb.Call(func() error { return testErr })
	
	// Wait for timeout to go half-open
	time.Sleep(20 * time.Millisecond)
	
	// Fail in half-open state - should go back to open
	cb.Call(func() error { return testErr })
	
	if cb.GetState() != "open" {
		t.Errorf("Expected state 'open' after failure in half-open, got '%s'", cb.GetState())
	}
}

func TestCircuitBreaker_HalfOpenToClosed(t *testing.T) {
	cb := NewCircuitBreaker(2, 10*time.Millisecond)
	
	// Open the circuit
	testErr := errors.New("test error")
	cb.Call(func() error { return testErr })
	cb.Call(func() error { return testErr })
	
	// Wait for timeout to go half-open
	time.Sleep(20 * time.Millisecond)
	
	// Succeed enough times to close the circuit
	successThreshold := cb.successThreshold
	for i := 0; i < successThreshold; i++ {
		err := cb.Call(func() error { return nil })
		if err != nil {
			t.Errorf("Expected nil error during recovery, got %v", err)
		}
	}
	
	if cb.GetState() != "closed" {
		t.Errorf("Expected state 'closed' after recovery, got '%s'", cb.GetState())
	}
}

func TestCircuitBreaker_Metrics(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Second)
	
	// Make some successful calls
	cb.Call(func() error { return nil })
	cb.Call(func() error { return nil })
	
	// Make some failed calls
	testErr := errors.New("test error")
	cb.Call(func() error { return testErr })
	
	metrics := cb.GetMetrics()
	
	if metrics.TotalRequests != 3 {
		t.Errorf("Expected 3 total requests, got %d", metrics.TotalRequests)
	}
	
	if metrics.SuccessCount != 2 {
		t.Errorf("Expected 2 successes, got %d", metrics.SuccessCount)
	}
	
	if metrics.FailureCount != 1 {
		t.Errorf("Expected 1 failure, got %d", metrics.FailureCount)
	}
	
	if metrics.CurrentState != "closed" {
		t.Errorf("Expected current state 'closed', got '%s'", metrics.CurrentState)
	}
}

func TestCircuitBreaker_RejectionMetrics(t *testing.T) {
	cb := NewCircuitBreaker(1, time.Hour) // Long timeout
	
	// Open the circuit
	testErr := errors.New("test error")
	cb.Call(func() error { return testErr })
	
	// Make rejected calls
	cb.Call(func() error { return nil })
	cb.Call(func() error { return nil })
	
	metrics := cb.GetMetrics()
	
	if metrics.RejectedCount != 2 {
		t.Errorf("Expected 2 rejected requests, got %d", metrics.RejectedCount)
	}
	
	if metrics.CurrentState != "open" {
		t.Errorf("Expected current state 'open', got '%s'", metrics.CurrentState)
	}
}

func TestCircuitBreaker_ConcurrentAccess(t *testing.T) {
	cb := NewCircuitBreaker(50, time.Second)
	
	const numGoroutines = 100
	const callsPerGoroutine = 10
	
	var wg sync.WaitGroup
	successCount := int64(0)
	errorCount := int64(0)
	
	var mu sync.Mutex
	
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			localSuccess := 0
			localError := 0
			
			for j := 0; j < callsPerGoroutine; j++ {
				err := cb.Call(func() error {
					if (id+j)%10 == 0 { // 10% failure rate
						return errors.New("test error")
					}
					return nil
				})
				
				if err != nil {
					localError++
				} else {
					localSuccess++
				}
			}
			
			mu.Lock()
			successCount += int64(localSuccess)
			errorCount += int64(localError)
			mu.Unlock()
		}(i)
	}
	
	wg.Wait()
	
	totalCalls := int64(numGoroutines * callsPerGoroutine)
	if successCount+errorCount != totalCalls {
		t.Errorf("Total processed calls %d != expected %d", successCount+errorCount, totalCalls)
	}
	
	// Should have some successes
	if successCount == 0 {
		t.Error("Expected some successful calls")
	}
}

func TestCircuitBreaker_StateTransitions(t *testing.T) {
	cb := NewCircuitBreaker(2, 50*time.Millisecond)
	
	// Start in closed state
	if cb.GetState() != "closed" {
		t.Errorf("Expected initial state 'closed', got '%s'", cb.GetState())
	}
	
	// Transition to open
	testErr := errors.New("test error")
	cb.Call(func() error { return testErr })
	cb.Call(func() error { return testErr })
	
	if cb.GetState() != "open" {
		t.Errorf("Expected state 'open' after failures, got '%s'", cb.GetState())
	}
	
	// Wait for half-open transition
	time.Sleep(60 * time.Millisecond)
	
	// Should allow one call (transitions to half-open internally)
	cb.Call(func() error { return nil })
	
	// Succeed enough to close
	for i := 0; i < cb.successThreshold-1; i++ {
		cb.Call(func() error { return nil })
	}
	
	if cb.GetState() != "closed" {
		t.Errorf("Expected state 'closed' after recovery, got '%s'", cb.GetState())
	}
}

func TestCircuitBreaker_EdgeCases(t *testing.T) {
	// Test with threshold of 1
	cb := NewCircuitBreaker(1, time.Millisecond)
	
	// Should open immediately after first failure
	testErr := errors.New("test error")
	cb.Call(func() error { return testErr })
	
	if cb.GetState() != "open" {
		t.Errorf("Expected state 'open' after single failure, got '%s'", cb.GetState())
	}
	
	// Wait and test recovery
	time.Sleep(5 * time.Millisecond)
	
	// Should succeed and close immediately (success threshold is 0 for threshold 1)
	err := cb.Call(func() error { return nil })
	if err != nil {
		t.Errorf("Expected success during recovery, got %v", err)
	}
}

func BenchmarkCircuitBreaker_Call(b *testing.B) {
	cb := NewCircuitBreaker(1000, time.Second) // High threshold to avoid opening
	
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cb.Call(func() error { return nil })
		}
	})
}

func BenchmarkCircuitBreaker_CallWithFailures(b *testing.B) {
	cb := NewCircuitBreaker(1000, time.Second) // High threshold
	testErr := errors.New("test error")
	
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		counter := 0
		for pb.Next() {
			counter++
			var err error
			if counter%10 == 0 { // 10% failure rate
				err = testErr
			}
			cb.Call(func() error { return err })
		}
	})
}