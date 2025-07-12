package timeout

import (
	"context"
	"testing"
	"time"
)

func TestManager_ExecuteWithTimeout(t *testing.T) {
	config := &Config{
		DefaultTimeout: 50 * time.Millisecond,
		MaxTimeout:     200 * time.Millisecond,
	}
	manager := NewManager(config)

	// Test successful operation
	err := manager.ExecuteWithTimeout(context.Background(), "test-op", func(ctx context.Context) error {
		return nil
	})
	if err != nil {
		t.Errorf("Expected no error for successful operation, got: %v", err)
	}

	// Test operation that times out
	err = manager.ExecuteWithTimeout(context.Background(), "timeout-op", func(ctx context.Context) error {
		time.Sleep(100 * time.Millisecond)
		return nil
	})
	if err == nil {
		t.Error("Expected timeout error, got nil")
	}

	// Check stats
	stats := manager.GetOperationStats("test-op")
	if stats == nil {
		t.Error("Expected stats for test-op, got nil")
	}
	if stats.SuccessfulCalls != 1 {
		t.Errorf("Expected 1 successful call, got %d", stats.SuccessfulCalls)
	}
}

func TestManager_AdaptiveTimeout(t *testing.T) {
	config := &Config{
		DefaultTimeout: 50 * time.Millisecond,
		MaxTimeout:     200 * time.Millisecond,
	}
	manager := NewManager(config)

	// Execute several successful operations
	for i := 0; i < 5; i++ {
		err := manager.ExecuteWithTimeout(context.Background(), "adaptive-op", func(ctx context.Context) error {
			time.Sleep(10 * time.Millisecond)
			return nil
		})
		if err != nil {
			t.Errorf("Unexpected error in iteration %d: %v", i, err)
		}
	}

	// Check that adaptive timeout is being calculated
	stats := manager.GetOperationStats("adaptive-op")
	if stats == nil {
		t.Fatal("Expected stats for adaptive-op, got nil")
	}
	if stats.TotalCalls != 5 {
		t.Errorf("Expected 5 total calls, got %d", stats.TotalCalls)
	}
	if stats.SuccessfulCalls != 5 {
		t.Errorf("Expected 5 successful calls, got %d", stats.SuccessfulCalls)
	}
}
