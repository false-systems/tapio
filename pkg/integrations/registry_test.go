package integrations

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/integrations/core"
)

// mockIntegration implements core.Integration for testing
type mockIntegration struct {
	name         string
	healthDelay  time.Duration
	healthError  error
	healthStatus *domain.HealthStatus
	callCount    int64
}

func (m *mockIntegration) Name() string {
	return m.name
}

func (m *mockIntegration) Initialize(ctx context.Context, config core.Config) error {
	return nil
}

func (m *mockIntegration) ProcessEvent(ctx context.Context, event *domain.Event) error {
	return nil
}

func (m *mockIntegration) ProcessFinding(ctx context.Context, finding *domain.Finding) error {
	return nil
}

func (m *mockIntegration) ProcessCorrelation(ctx context.Context, correlation *domain.Correlation) error {
	return nil
}

func (m *mockIntegration) Health(ctx context.Context) (*domain.HealthStatus, error) {
	atomic.AddInt64(&m.callCount, 1)

	if m.healthDelay > 0 {
		select {
		case <-time.After(m.healthDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if m.healthError != nil {
		return nil, m.healthError
	}

	if m.healthStatus != nil {
		return m.healthStatus, nil
	}

	return domain.NewHealthyStatus("mock integration is healthy"), nil
}

func (m *mockIntegration) Close() error {
	return nil
}

func (m *mockIntegration) GetCallCount() int64 {
	return atomic.LoadInt64(&m.callCount)
}

func TestNewRegistry(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("successful creation", func(t *testing.T) {
		registry, err := NewRegistry(logger)
		require.NoError(t, err)
		require.NotNil(t, registry)
		assert.NotNil(t, registry.logger)
		assert.NotNil(t, registry.tracer)
		assert.Equal(t, 0, len(registry.integrations))
	})

	t.Run("nil logger fails", func(t *testing.T) {
		registry, err := NewRegistry(nil)
		require.Error(t, err)
		assert.Nil(t, registry)
		assert.Contains(t, err.Error(), "logger is required")
	})
}

func TestRegistry_Register(t *testing.T) {
	logger := zaptest.NewLogger(t)
	registry, err := NewRegistry(logger)
	require.NoError(t, err)

	t.Run("successful registration", func(t *testing.T) {
		integration := &mockIntegration{name: "test-integration"}

		err := registry.Register(integration)
		require.NoError(t, err)

		// Verify integration is registered
		retrieved, exists := registry.Get("test-integration")
		assert.True(t, exists)
		assert.Equal(t, integration, retrieved)
	})

	t.Run("nil integration fails", func(t *testing.T) {
		err := registry.Register(nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "integration cannot be nil")
	})

	t.Run("empty name fails", func(t *testing.T) {
		integration := &mockIntegration{name: ""}
		err := registry.Register(integration)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "integration name cannot be empty")
	})

	t.Run("duplicate registration fails", func(t *testing.T) {
		integration1 := &mockIntegration{name: "duplicate"}
		integration2 := &mockIntegration{name: "duplicate"}

		err := registry.Register(integration1)
		require.NoError(t, err)

		err = registry.Register(integration2)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "integration duplicate already registered")
	})
}

func TestRegistry_HealthCheck(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("empty registry", func(t *testing.T) {
		registry, err := NewRegistry(logger)
		require.NoError(t, err)

		ctx := context.Background()
		results := registry.HealthCheck(ctx)
		assert.Empty(t, results)
	})

	t.Run("single healthy integration", func(t *testing.T) {
		registry, err := NewRegistry(logger)
		require.NoError(t, err)

		integration := &mockIntegration{
			name:         "healthy-integration",
			healthStatus: domain.NewHealthyStatus("all good"),
		}
		err = registry.Register(integration)
		require.NoError(t, err)

		ctx := context.Background()
		results := registry.HealthCheck(ctx)

		require.Len(t, results, 1)
		assert.Contains(t, results, "healthy-integration")
		assert.True(t, results["healthy-integration"].Healthy)
		assert.Equal(t, int64(1), integration.GetCallCount())
	})

	t.Run("integration with health error", func(t *testing.T) {
		registry, err := NewRegistry(logger)
		require.NoError(t, err)

		integration := &mockIntegration{
			name:        "error-integration",
			healthError: errors.New("health check failed"),
		}
		err = registry.Register(integration)
		require.NoError(t, err)

		ctx := context.Background()
		results := registry.HealthCheck(ctx)

		require.Len(t, results, 1)
		assert.Contains(t, results, "error-integration")
		assert.False(t, results["error-integration"].Healthy)
		assert.Contains(t, results["error-integration"].Message, "health check failed")
		assert.Equal(t, int64(1), integration.GetCallCount())
	})

	t.Run("concurrent health checks performance", func(t *testing.T) {
		registry, err := NewRegistry(logger)
		require.NoError(t, err)

		// Register multiple integrations with delays to simulate slow health checks
		const numIntegrations = 20
		const healthDelay = 50 * time.Millisecond

		integrations := make([]*mockIntegration, numIntegrations)
		for i := 0; i < numIntegrations; i++ {
			integrations[i] = &mockIntegration{
				name:        fmt.Sprintf("integration-%d", i),
				healthDelay: healthDelay,
			}
			err = registry.Register(integrations[i])
			require.NoError(t, err)
		}

		// Measure time for concurrent health checks
		ctx := context.Background()
		start := time.Now()
		results := registry.HealthCheck(ctx)
		duration := time.Since(start)

		// Verify all integrations were checked
		require.Len(t, results, numIntegrations)
		for _, integration := range integrations {
			assert.Equal(t, int64(1), integration.GetCallCount(), "Integration %s should be called once", integration.name)
			assert.Contains(t, results, integration.name)
			assert.True(t, results[integration.name].Healthy)
		}

		// Verify concurrent execution - should be much faster than sequential
		// Sequential would take numIntegrations * healthDelay = 1 second
		// Concurrent should take approximately healthDelay = 50ms (plus overhead)
		maxExpectedDuration := healthDelay + 200*time.Millisecond // Add 200ms buffer for overhead
		assert.Less(t, duration, maxExpectedDuration,
			"Concurrent health checks should be much faster than sequential. Got %v, expected less than %v",
			duration, maxExpectedDuration)

		// Log actual performance for verification
		t.Logf("Health check performance: %d integrations checked in %v (expected ~%v for concurrent)",
			numIntegrations, duration, healthDelay)
	})

	t.Run("context cancellation", func(t *testing.T) {
		registry, err := NewRegistry(logger)
		require.NoError(t, err)

		// Register integration with long delay
		integration := &mockIntegration{
			name:        "slow-integration",
			healthDelay: 5 * time.Second,
		}
		err = registry.Register(integration)
		require.NoError(t, err)

		// Create context that will be cancelled quickly
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		start := time.Now()
		results := registry.HealthCheck(ctx)
		duration := time.Since(start)

		// Should return quickly due to context cancellation
		assert.Less(t, duration, time.Second)

		// May have partial results depending on timing
		assert.LessOrEqual(t, len(results), 1)
	})

	t.Run("mixed healthy and unhealthy integrations", func(t *testing.T) {
		registry, err := NewRegistry(logger)
		require.NoError(t, err)

		// Register mix of healthy and unhealthy integrations
		healthyIntegration := &mockIntegration{
			name:         "healthy",
			healthStatus: domain.NewHealthyStatus("working fine"),
		}
		err = registry.Register(healthyIntegration)
		require.NoError(t, err)

		errorIntegration := &mockIntegration{
			name:        "error",
			healthError: errors.New("connection failed"),
		}
		err = registry.Register(errorIntegration)
		require.NoError(t, err)

		unhealthyIntegration := &mockIntegration{
			name:         "unhealthy",
			healthStatus: domain.NewUnhealthyStatus("degraded performance", nil),
		}
		err = registry.Register(unhealthyIntegration)
		require.NoError(t, err)

		ctx := context.Background()
		results := registry.HealthCheck(ctx)

		require.Len(t, results, 3)

		// Check healthy integration
		assert.True(t, results["healthy"].Healthy)
		assert.Equal(t, "working fine", results["healthy"].Message)

		// Check error integration (should be unhealthy due to error)
		assert.False(t, results["error"].Healthy)
		assert.Contains(t, results["error"].Message, "health check failed")

		// Check unhealthy integration
		assert.False(t, results["unhealthy"].Healthy)
		assert.Equal(t, "degraded performance", results["unhealthy"].Message)
	})
}

func TestRegistry_List(t *testing.T) {
	logger := zaptest.NewLogger(t)
	registry, err := NewRegistry(logger)
	require.NoError(t, err)

	t.Run("empty registry", func(t *testing.T) {
		names := registry.List()
		assert.Empty(t, names)
	})

	t.Run("multiple integrations", func(t *testing.T) {
		integrations := []string{"integration-1", "integration-2", "integration-3"}

		for _, name := range integrations {
			err := registry.Register(&mockIntegration{name: name})
			require.NoError(t, err)
		}

		names := registry.List()
		assert.Len(t, names, len(integrations))

		// Verify all integration names are present
		for _, expected := range integrations {
			assert.Contains(t, names, expected)
		}
	})
}

func TestRegistry_CloseAll(t *testing.T) {
	logger := zaptest.NewLogger(t)
	registry, err := NewRegistry(logger)
	require.NoError(t, err)

	// Register some integrations
	for i := 0; i < 3; i++ {
		integration := &mockIntegration{name: fmt.Sprintf("integration-%d", i)}
		err := registry.Register(integration)
		require.NoError(t, err)
	}

	// Verify integrations are registered
	assert.Len(t, registry.List(), 3)

	// Close all
	err = registry.CloseAll()
	require.NoError(t, err)

	// Verify registry is now empty
	assert.Empty(t, registry.List())
}

// Benchmark to verify performance improvement
func BenchmarkRegistry_HealthCheck(b *testing.B) {
	logger := zaptest.NewLogger(b)

	b.Run("10_integrations", func(b *testing.B) {
		benchmarkHealthCheck(b, logger, 10)
	})

	b.Run("50_integrations", func(b *testing.B) {
		benchmarkHealthCheck(b, logger, 50)
	})

	b.Run("100_integrations", func(b *testing.B) {
		benchmarkHealthCheck(b, logger, 100)
	})
}

func benchmarkHealthCheck(b *testing.B, logger *zaptest.Logger, numIntegrations int) {
	registry, err := NewRegistry(logger)
	require.NoError(b, err)

	// Register integrations with small delay to simulate real health checks
	for i := 0; i < numIntegrations; i++ {
		integration := &mockIntegration{
			name:        fmt.Sprintf("integration-%d", i),
			healthDelay: time.Millisecond,
		}
		err = registry.Register(integration)
		require.NoError(b, err)
	}

	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		results := registry.HealthCheck(ctx)
		if len(results) != numIntegrations {
			b.Fatalf("Expected %d results, got %d", numIntegrations, len(results))
		}
	}
}
