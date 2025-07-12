package shutdown

import (
	"context"
	"testing"
	"time"
)

func TestHandler_Register(t *testing.T) {
	handler := NewHandler(5 * time.Second)

	called := false
	handler.Register("test-cleanup", func(ctx context.Context) error {
		called = true
		return nil
	})

	// Trigger shutdown
	handler.executeShutdown()

	if !called {
		t.Error("Expected cleanup function to be called")
	}
}

func TestHandler_ExecuteOrder(t *testing.T) {
	handler := NewHandler(5 * time.Second)

	order := make([]string, 0)

	handler.Register("first", func(ctx context.Context) error {
		order = append(order, "first")
		return nil
	})

	handler.Register("second", func(ctx context.Context) error {
		order = append(order, "second")
		return nil
	})

	// Trigger shutdown
	handler.executeShutdown()

	// Should execute in reverse order (LIFO)
	if len(order) != 2 {
		t.Errorf("Expected 2 cleanup calls, got %d", len(order))
	}
	if order[0] != "second" {
		t.Errorf("Expected first call to be 'second', got %s", order[0])
	}
	if order[1] != "first" {
		t.Errorf("Expected second call to be 'first', got %s", order[1])
	}
}

func TestContext(t *testing.T) {
	ctx, cancel := Context()
	defer cancel()

	// Context should not be cancelled initially
	select {
	case <-ctx.Done():
		t.Error("Context should not be cancelled initially")
	default:
		// Expected
	}

	// Cancel and check
	cancel()

	select {
	case <-ctx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Context should be cancelled after calling cancel")
	}
}
