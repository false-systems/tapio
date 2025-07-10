package universal

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCircuitBreaker(t *testing.T) {
	t.Run("State transitions", func(t *testing.T) {
		cb := NewCircuitBreaker("test-service", 3, 100*time.Millisecond)

		// Initial state should be closed
		if !cb.CanExecute() {
			t.Error("Circuit should be closed initially")
		}

		// Record failures up to threshold
		for i := 0; i < 3; i++ {
			cb.RecordFailure()
		}

		// Circuit should be open now
		if cb.CanExecute() {
			t.Error("Circuit should be open after reaching failure threshold")
		}

		// Wait for reset timeout
		time.Sleep(150 * time.Millisecond)

		// Circuit should be half-open
		if !cb.CanExecute() {
			t.Error("Circuit should be half-open after reset timeout")
		}

		// Success should close the circuit
		cb.RecordSuccess()

		// Circuit should be closed
		if !cb.CanExecute() {
			t.Error("Circuit should be closed after success in half-open state")
		}
	})

	t.Run("Failure counting", func(t *testing.T) {
		cb := NewCircuitBreaker("test-service", 5, time.Second)

		// Record 4 failures (below threshold)
		for i := 0; i < 4; i++ {
			cb.RecordFailure()
			if !cb.CanExecute() {
				t.Errorf("Circuit opened prematurely at %d failures", i+1)
			}
		}

		// One more failure should open it
		cb.RecordFailure()
		if cb.CanExecute() {
			t.Error("Circuit should be open after 5 failures")
		}
	})

	t.Run("Success resets failures", func(t *testing.T) {
		cb := NewCircuitBreaker("test-service", 3, time.Second)

		// Record 2 failures
		cb.RecordFailure()
		cb.RecordFailure()

		// Success should reset
		cb.RecordSuccess()

		// Record 2 more failures - should not open
		cb.RecordFailure()
		cb.RecordFailure()

		if !cb.CanExecute() {
			t.Error("Circuit should remain closed after reset")
		}
	})
}

func TestMetricFallbackGenerator(t *testing.T) {
	generator := &MetricFallbackGenerator{}

	if !generator.CanHandle("metrics") {
		t.Error("Should handle 'metrics' type")
	}

	if generator.CanHandle("events") {
		t.Error("Should not handle 'events' type")
	}

	target := Target{
		Type: TargetTypeProcess,
		Name: "test-process",
		PID:  1234,
	}

	ctx := context.Background()
	result, err := generator.GenerateFallback(ctx, target)
	if err != nil {
		t.Fatalf("Failed to generate fallback: %v", err)
	}

	metric, ok := result.(*UniversalMetric)
	if !ok {
		t.Fatal("Expected UniversalMetric type")
	}

	if metric.Value != -1 {
		t.Error("Expected fallback value of -1")
	}

	if !metric.FallbackUsed {
		t.Error("Expected FallbackUsed to be true")
	}

	if metric.Quality.Confidence != 0.1 {
		t.Errorf("Expected confidence 0.1, got %f", metric.Quality.Confidence)
	}

	// Clean up
	PutMetric(metric)
}

func TestEventFallbackGenerator(t *testing.T) {
	generator := &EventFallbackGenerator{}

	if !generator.CanHandle("events") {
		t.Error("Should handle 'events' type")
	}

	target := Target{
		Type:      TargetTypePod,
		Name:      "test-pod",
		Pod:       "test-pod",
		Namespace: "default",
	}

	ctx := context.Background()
	result, err := generator.GenerateFallback(ctx, target)
	if err != nil {
		t.Fatalf("Failed to generate fallback: %v", err)
	}

	event, ok := result.(*UniversalEvent)
	if !ok {
		t.Fatal("Expected UniversalEvent type")
	}

	if event.Level != EventLevelWarning {
		t.Errorf("Expected warning level, got %s", event.Level)
	}

	if event.Quality.Confidence != 0.1 {
		t.Errorf("Expected confidence 0.1, got %f", event.Quality.Confidence)
	}

	// Clean up
	PutEvent(event)
}

func TestDefaultErrorHandler(t *testing.T) {
	handler := &DefaultErrorHandler{}

	t.Run("ShouldRetry", func(t *testing.T) {
		// Context errors should not retry
		if handler.ShouldRetry(context.Canceled) {
			t.Error("Should not retry context.Canceled")
		}

		if handler.ShouldRetry(context.DeadlineExceeded) {
			t.Error("Should not retry context.DeadlineExceeded")
		}

		// Other errors should retry
		if !handler.ShouldRetry(errors.New("temporary error")) {
			t.Error("Should retry temporary errors")
		}
	})

	t.Run("GetBackoffDuration", func(t *testing.T) {
		// Test exponential backoff
		for i := 1; i <= 5; i++ {
			duration := handler.GetBackoffDuration(i)

			// Verify it's within expected range (accounting for jitter)
			minExpected := 50 * time.Millisecond * time.Duration(1<<uint(i-1))
			maxExpected := 150 * time.Millisecond * time.Duration(1<<uint(i-1))

			if duration < minExpected || duration > maxExpected {
				t.Errorf("Attempt %d: duration %v outside expected range [%v, %v]",
					i, duration, minExpected, maxExpected)
			}
		}

		// Test max backoff
		duration := handler.GetBackoffDuration(100)
		if duration > 40*time.Second { // Max is 30s + jitter
			t.Errorf("Duration exceeds max: %v", duration)
		}
	})
}

func TestResilienceManager(t *testing.T) {
	t.Run("ExecuteWithFallback - Success", func(t *testing.T) {
		rm := NewResilienceManager()

		target := Target{Type: TargetTypeProcess, Name: "test"}

		result, usedFallback, err := rm.ExecuteWithFallback(
			context.Background(),
			"test-service",
			target,
			func() (interface{}, error) {
				return "success", nil
			},
			"metrics",
		)

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if usedFallback {
			t.Error("Should not have used fallback")
		}

		if result != "success" {
			t.Errorf("Expected 'success', got %v", result)
		}
	})

	t.Run("ExecuteWithFallback - Fallback on failure", func(t *testing.T) {
		rm := NewResilienceManager()

		target := Target{Type: TargetTypeProcess, Name: "test"}

		result, usedFallback, err := rm.ExecuteWithFallback(
			context.Background(),
			"test-service",
			target,
			func() (interface{}, error) {
				return nil, errors.New("primary failed")
			},
			"metrics",
		)

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if !usedFallback {
			t.Error("Should have used fallback")
		}

		metric, ok := result.(*UniversalMetric)
		if !ok {
			t.Fatal("Expected UniversalMetric from fallback")
		}

		if metric.Value != -1 {
			t.Error("Expected fallback metric value")
		}

		// Clean up
		PutMetric(metric)
	})

	t.Run("ExecuteWithFallback - Circuit breaker", func(t *testing.T) {
		rm := NewResilienceManager()

		target := Target{Type: TargetTypeProcess, Name: "test"}

		// Fail multiple times to open circuit
		for i := 0; i < 6; i++ {
			_, _, _ = rm.ExecuteWithFallback(
				context.Background(),
				"circuit-test",
				target,
				func() (interface{}, error) {
					return nil, errors.New("failure")
				},
				"metrics",
			)
		}

		// Circuit should be open, fallback should be used immediately
		callCount := 0
		result, usedFallback, err := rm.ExecuteWithFallback(
			context.Background(),
			"circuit-test",
			target,
			func() (interface{}, error) {
				callCount++
				return nil, errors.New("should not be called")
			},
			"metrics",
		)

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if callCount != 0 {
			t.Error("Primary function should not be called when circuit is open")
		}

		if !usedFallback {
			t.Error("Should have used fallback when circuit is open")
		}

		// Clean up
		if metric, ok := result.(*UniversalMetric); ok {
			PutMetric(metric)
		}
	})
}

func TestPartialDataHandler(t *testing.T) {
	handler := NewPartialDataHandler(0.5) // 50% threshold

	dataset := &UniversalDataset{
		ID:        "test-dataset",
		Timestamp: time.Now(),
		OverallQuality: DataQuality{
			Confidence: 1.0,
			Tags:       make(map[string]string),
			Metadata:   make(map[string]interface{}),
		},
	}

	t.Run("Sufficient data", func(t *testing.T) {
		err := handler.ProcessPartialData(dataset, 100, 75)
		if err != nil {
			t.Errorf("Should not error with 75%% data: %v", err)
		}

		if dataset.OverallQuality.Confidence != 0.75 {
			t.Errorf("Expected confidence 0.75, got %f", dataset.OverallQuality.Confidence)
		}

		if dataset.OverallQuality.Tags["partial_data"] != "true" {
			t.Error("Expected partial_data tag")
		}

		if dataset.OverallQuality.Tags["completeness"] != "0.75" {
			t.Error("Expected completeness tag")
		}
	})

	t.Run("Insufficient data", func(t *testing.T) {
		dataset.OverallQuality.Confidence = 1.0 // Reset

		err := handler.ProcessPartialData(dataset, 100, 30)
		if err == nil {
			t.Error("Expected error with 30% data (below 50% threshold)")
		}
	})
}

func TestResilienceManager_Concurrent(t *testing.T) {
	rm := NewResilienceManager()

	var wg sync.WaitGroup
	var successCount int32
	var fallbackCount int32

	// Run concurrent executions
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			target := Target{
				Type: TargetTypeProcess,
				Name: fmt.Sprintf("test-%d", id),
			}

			result, usedFallback, err := rm.ExecuteWithFallback(
				context.Background(),
				"concurrent-test",
				target,
				func() (interface{}, error) {
					// Simulate intermittent failures
					if id%3 == 0 {
						return nil, errors.New("simulated failure")
					}
					return "success", nil
				},
				"metrics",
			)

			if err == nil {
				if usedFallback {
					atomic.AddInt32(&fallbackCount, 1)
					// Clean up fallback metric
					if metric, ok := result.(*UniversalMetric); ok {
						PutMetric(metric)
					}
				} else {
					atomic.AddInt32(&successCount, 1)
				}
			}
		}(i)
	}

	wg.Wait()

	total := atomic.LoadInt32(&successCount) + atomic.LoadInt32(&fallbackCount)
	if total != 100 {
		t.Errorf("Expected 100 total executions, got %d", total)
	}

	// Roughly 2/3 should succeed, 1/3 should use fallback
	if successCount < 50 || successCount > 80 {
		t.Errorf("Unexpected success count: %d", successCount)
	}
}
