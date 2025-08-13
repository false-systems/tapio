package behavior

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

func TestEngineBasic(t *testing.T) {
	logger := zap.NewNop()

	engine, err := NewEngine(logger)
	require.NoError(t, err)
	require.NotNil(t, engine)

	// Test health check
	ctx := context.Background()
	healthy, details := engine.Health(ctx)
	assert.True(t, healthy)
	assert.NotNil(t, details)

	// Test processing nil event
	result, err := engine.Process(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestCircuitBreaker(t *testing.T) {
	config := CircuitBreakerConfig{
		MaxFailures:  3,
		ResetTimeout: 100 * time.Millisecond,
	}

	cb := NewCircuitBreaker(config)
	require.NotNil(t, cb)

	// Test initial state
	assert.Equal(t, "closed", cb.State())

	// Test successful execution
	ctx := context.Background()
	result, err := cb.Execute(ctx, func() (*domain.PredictionResult, error) {
		return &domain.PredictionResult{}, nil
	})
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// Test failure handling
	failCount := 0
	for i := 0; i < 3; i++ {
		_, err := cb.Execute(ctx, func() (*domain.PredictionResult, error) {
			failCount++
			return nil, assert.AnError
		})
		assert.Error(t, err)
	}

	// Circuit should be open now
	assert.Equal(t, "open", cb.State())

	// Further calls should fail immediately
	_, err = cb.Execute(ctx, func() (*domain.PredictionResult, error) {
		t.Fatal("Should not be called when circuit is open")
		return nil, nil
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker is open")

	// Wait for reset timeout
	time.Sleep(150 * time.Millisecond)

	// Circuit should allow one call (half-open)
	result, err = cb.Execute(ctx, func() (*domain.PredictionResult, error) {
		return &domain.PredictionResult{}, nil
	})
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// Circuit should be closed again
	assert.Equal(t, "closed", cb.State())
}

func TestBackpressureManager(t *testing.T) {
	bp := NewBackpressureManager(3)

	// Should accept up to max
	assert.True(t, bp.TryAccept())
	assert.True(t, bp.TryAccept())
	assert.True(t, bp.TryAccept())

	// Should reject when full
	assert.False(t, bp.TryAccept())

	// Release one and try again
	bp.Release()
	assert.True(t, bp.TryAccept())

	// Check usage
	assert.Equal(t, 1.0, bp.Usage())

	// Release all
	bp.Release()
	bp.Release()
	bp.Release()

	assert.Equal(t, 0.0, bp.Usage())
	assert.Equal(t, int32(3), bp.Available())
}
