package correlation

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
	"go.uber.org/zap"
)

func TestMemoryStorageMetrics(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultMemoryStorageConfig()
	config.MaxSize = 100

	storage := NewMemoryStorage(logger, config)

	// Test GetMetrics returns StorageMetrics type (not map[string]interface{})
	metrics := storage.GetMetrics()

	// Verify the type is StorageMetrics
	if metrics.Capacity != 100 {
		t.Errorf("Expected capacity 100, got %d", metrics.Capacity)
	}

	// Add a correlation
	ctx := context.Background()
	result := &correlation.CorrelationResult{
		ID:        "test-1",
		Type:      "test",
		TraceID:   "trace-1",
		StartTime: time.Now(),
	}

	err := storage.Store(ctx, result)
	if err != nil {
		t.Errorf("Failed to store correlation: %v", err)
	}

	// Check metrics updated
	metrics = storage.GetMetrics()
	if metrics.CorrelationsStored != 1 {
		t.Errorf("Expected 1 correlation stored, got %d", metrics.CorrelationsStored)
	}
	if metrics.TotalStores != 1 {
		t.Errorf("Expected 1 total store, got %d", metrics.TotalStores)
	}
}

func TestMemoryStorageEvictionPolicies(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultMemoryStorageConfig()
	config.MaxSize = 3
	config.EvictionPolicy = "lru"

	storage := NewMemoryStorage(logger, config)
	ctx := context.Background()

	// Add correlations to trigger eviction
	for i := 0; i < 5; i++ {
		result := &correlation.CorrelationResult{
			ID:        fmt.Sprintf("test-%d", i),
			Type:      "test",
			StartTime: time.Now(),
		}
		err := storage.Store(ctx, result)
		if err != nil {
			t.Errorf("Failed to store correlation %d: %v", i, err)
		}
	}

	// Check that max size is respected
	metrics := storage.GetMetrics()
	if metrics.CorrelationsStored > 3 {
		t.Errorf("Expected max 3 correlations, got %d", metrics.CorrelationsStored)
	}
	if metrics.TotalEvictions < 2 {
		t.Errorf("Expected at least 2 evictions, got %d", metrics.TotalEvictions)
	}
}

func TestMemoryStorageBounds(t *testing.T) {
	logger := zap.NewNop()
	config := DefaultMemoryStorageConfig()
	config.MaxSize = 10
	config.MaxCorrelationsPerTrace = 5

	storage := NewMemoryStorage(logger, config)
	ctx := context.Background()

	// Add many correlations for the same trace
	for i := 0; i < 10; i++ {
		result := &correlation.CorrelationResult{
			ID:        fmt.Sprintf("test-%d", i),
			Type:      "test",
			TraceID:   "trace-1",
			StartTime: time.Now(),
		}
		err := storage.Store(ctx, result)
		if err != nil {
			t.Errorf("Failed to store correlation %d: %v", i, err)
		}
	}

	// Retrieve by trace and check bounds are respected
	results, err := storage.GetByTraceID(ctx, "trace-1")
	if err != nil {
		t.Errorf("Failed to get by trace ID: %v", err)
	}

	// Should have at most 5 correlations per trace (the most recent ones)
	if len(results) > 5 {
		t.Errorf("Expected max 5 correlations per trace, got %d", len(results))
	}
}
