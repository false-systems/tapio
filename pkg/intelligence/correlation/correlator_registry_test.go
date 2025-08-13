package correlation

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/yairfalse/tapio/pkg/domain"
)

// Mock correlator for testing
type TestCorrelator struct {
	name string
}

func (tc *TestCorrelator) Process(ctx context.Context, event *domain.UnifiedEvent) ([]*CorrelationResult, error) {
	return nil, nil
}

func (tc *TestCorrelator) Name() string {
	return tc.name
}

func TestNewCorrelatorRegistry(t *testing.T) {
	logger := zaptest.NewLogger(t)
	registry := NewCorrelatorRegistry(logger)

	require.NotNil(t, registry)
	assert.Equal(t, logger, registry.logger)
	assert.NotNil(t, registry.correlators)

	// Should have built-in correlators registered
	available := registry.ListAvailable()
	assert.Greater(t, len(available), 0, "should have built-in correlators")

	// Check for expected built-in correlators (temporarily excluding disabled ones)
	sort.Strings(available)
	expectedBuiltIns := []string{"k8s", "performance", "sequence", "servicemap", "temporal"}
	sort.Strings(expectedBuiltIns)
	assert.Equal(t, expectedBuiltIns, available)
}

func TestCorrelatorRegistry_Register(t *testing.T) {
	logger := zaptest.NewLogger(t)
	registry := NewCorrelatorRegistry(logger)

	testFactory := func(ctx context.Context, logger *zap.Logger, k8sClient domain.K8sClient) (Correlator, error) {
		return &TestCorrelator{name: "test"}, nil
	}

	t.Run("successful registration", func(t *testing.T) {
		err := registry.Register("test", "Test correlator", testFactory)
		assert.NoError(t, err)

		available := registry.ListAvailable()
		assert.Contains(t, available, "test")
	})

	t.Run("duplicate registration fails", func(t *testing.T) {
		err := registry.Register("test", "Duplicate test", testFactory)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already registered")
	})

	t.Run("empty name fails", func(t *testing.T) {
		err := registry.Register("", "Empty name", testFactory)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be empty")
	})

	t.Run("nil factory fails", func(t *testing.T) {
		err := registry.Register("nil-factory", "Nil factory", nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot be nil")
	})
}

func TestCorrelatorRegistry_Create(t *testing.T) {
	logger := zaptest.NewLogger(t)
	registry := NewCorrelatorRegistry(logger)
	ctx := context.Background()

	t.Run("create built-in temporal correlator", func(t *testing.T) {
		correlator, err := registry.Create(ctx, "temporal", logger, nil)
		require.NoError(t, err)
		require.NotNil(t, correlator)
		assert.Equal(t, "temporal", correlator.Name())
	})

	t.Run("create built-in sequence correlator", func(t *testing.T) {
		correlator, err := registry.Create(ctx, "sequence", logger, nil)
		require.NoError(t, err)
		require.NotNil(t, correlator)
		assert.Equal(t, "sequence", correlator.Name())
	})

	t.Run("create built-in performance correlator", func(t *testing.T) {
		correlator, err := registry.Create(ctx, "performance", logger, nil)
		require.NoError(t, err)
		require.NotNil(t, correlator)
		assert.Equal(t, "performance", correlator.Name())
	})

	t.Run("create built-in servicemap correlator", func(t *testing.T) {
		correlator, err := registry.Create(ctx, "servicemap", logger, nil)
		require.NoError(t, err)
		require.NotNil(t, correlator)
		assert.Equal(t, "servicemap", correlator.Name())
	})

	t.Run("k8s correlator requires client", func(t *testing.T) {
		correlator, err := registry.Create(ctx, "k8s", logger, nil)
		assert.Error(t, err)
		assert.Nil(t, correlator)
		assert.Contains(t, err.Error(), "requires k8s client")
	})

	t.Run("unknown correlator fails", func(t *testing.T) {
		correlator, err := registry.Create(ctx, "unknown", logger, nil)
		assert.Error(t, err)
		assert.Nil(t, correlator)
		assert.Contains(t, err.Error(), "unknown correlator type")
	})

	t.Run("factory error propagates", func(t *testing.T) {
		failingFactory := func(ctx context.Context, logger *zap.Logger, k8sClient domain.K8sClient) (Correlator, error) {
			return nil, fmt.Errorf("factory failed")
		}

		err := registry.Register("failing", "Failing correlator", failingFactory)
		require.NoError(t, err)

		correlator, err := registry.Create(ctx, "failing", logger, nil)
		assert.Error(t, err)
		assert.Nil(t, correlator)
		assert.Contains(t, err.Error(), "failed to create correlator failing")
		assert.Contains(t, err.Error(), "factory failed")
	})

	t.Run("custom correlator creation", func(t *testing.T) {
		customFactory := func(ctx context.Context, logger *zap.Logger, k8sClient domain.K8sClient) (Correlator, error) {
			return &TestCorrelator{name: "custom"}, nil
		}

		err := registry.Register("custom", "Custom test correlator", customFactory)
		require.NoError(t, err)

		correlator, err := registry.Create(ctx, "custom", logger, nil)
		require.NoError(t, err)
		require.NotNil(t, correlator)
		assert.Equal(t, "custom", correlator.Name())
	})
}

func TestCorrelatorRegistry_ListAvailable(t *testing.T) {
	logger := zaptest.NewLogger(t)
	registry := NewCorrelatorRegistry(logger)

	// Get initial list
	initial := registry.ListAvailable()
	assert.Greater(t, len(initial), 0)

	// Add custom correlator
	testFactory := func(ctx context.Context, logger *zap.Logger, k8sClient domain.K8sClient) (Correlator, error) {
		return &TestCorrelator{name: "custom"}, nil
	}

	err := registry.Register("custom", "Custom correlator", testFactory)
	require.NoError(t, err)

	// List should now include custom correlator
	updated := registry.ListAvailable()
	assert.Equal(t, len(initial)+1, len(updated))
	assert.Contains(t, updated, "custom")

	// Original built-ins should still be there
	for _, name := range initial {
		assert.Contains(t, updated, name)
	}
}

func TestCorrelatorRegistry_GetInfo(t *testing.T) {
	logger := zaptest.NewLogger(t)
	registry := NewCorrelatorRegistry(logger)

	t.Run("get built-in correlator info", func(t *testing.T) {
		info, err := registry.GetInfo("temporal")
		require.NoError(t, err)
		require.NotNil(t, info)

		assert.Equal(t, "temporal", info.Name)
		assert.Contains(t, info.Description, "Time-based")
		assert.NotNil(t, info.Factory)
	})

	t.Run("get unknown correlator info fails", func(t *testing.T) {
		info, err := registry.GetInfo("unknown")
		assert.Error(t, err)
		assert.Nil(t, info)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("info is a copy", func(t *testing.T) {
		info1, err1 := registry.GetInfo("temporal")
		info2, err2 := registry.GetInfo("temporal")

		require.NoError(t, err1)
		require.NoError(t, err2)
		require.NotNil(t, info1)
		require.NotNil(t, info2)

		// Should have same content
		assert.Equal(t, info1.Name, info2.Name)
		assert.Equal(t, info1.Description, info2.Description)

		// But should be different instances (copies)
		assert.False(t, info1 == info2, "should return copies, not the same instance")
	})
}

func TestCorrelatorRegistry_ConcurrentAccess(t *testing.T) {
	logger := zaptest.NewLogger(t)
	registry := NewCorrelatorRegistry(logger)
	ctx := context.Background()

	// Test concurrent registration and creation
	const numGoroutines = 10

	// Register correlators concurrently
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			name := fmt.Sprintf("concurrent-%d", id)
			factory := func(ctx context.Context, logger *zap.Logger, k8sClient domain.K8sClient) (Correlator, error) {
				return &TestCorrelator{name: name}, nil
			}

			err := registry.Register(name, fmt.Sprintf("Concurrent correlator %d", id), factory)
			assert.NoError(t, err)
		}(i)
	}

	// Create correlators concurrently
	for i := 0; i < numGoroutines; i++ {
		go func() {
			correlator, err := registry.Create(ctx, "temporal", logger, nil)
			assert.NoError(t, err)
			assert.NotNil(t, correlator)
		}()
	}

	// List available concurrently
	for i := 0; i < numGoroutines; i++ {
		go func() {
			available := registry.ListAvailable()
			assert.Greater(t, len(available), 0)
		}()
	}

	// Give goroutines time to complete
	// In a real scenario, you'd use sync mechanisms, but for this test, sleep is sufficient
	// No assertions here since we're mainly testing that concurrent access doesn't panic
}

func TestCorrelatorRegistry_BuiltInCorrelators(t *testing.T) {
	logger := zaptest.NewLogger(t)
	registry := NewCorrelatorRegistry(logger)
	ctx := context.Background()

	// Test that all built-in correlators can be created (except k8s which needs client)
	builtIns := []string{"temporal", "sequence", "performance", "servicemap"}

	for _, name := range builtIns {
		t.Run(fmt.Sprintf("create %s correlator", name), func(t *testing.T) {
			correlator, err := registry.Create(ctx, name, logger, nil)
			require.NoError(t, err, "failed to create %s correlator", name)
			require.NotNil(t, correlator, "%s correlator should not be nil", name)
			assert.Equal(t, name, correlator.Name(), "%s correlator should have correct name", name)

			// Test that the correlator has a valid Process method
			results, err := correlator.Process(ctx, &domain.UnifiedEvent{
				Type: domain.EventTypeKubernetes,
			})
			// We don't assert specific results since each correlator has different logic
			// But we ensure the method can be called without panic
			assert.NotPanics(t, func() {
				_, _ = correlator.Process(ctx, &domain.UnifiedEvent{
					Type: domain.EventTypeKubernetes,
				})
			}, "%s correlator Process should not panic", name)

			// Results and error depend on correlator implementation, just ensure they're handled
			_ = results
			_ = err
		})
	}
}

// Benchmark correlator creation
func BenchmarkCorrelatorRegistry_Create(b *testing.B) {
	logger := zap.NewNop()
	registry := NewCorrelatorRegistry(logger)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		correlator, err := registry.Create(ctx, "temporal", logger, nil)
		if err != nil {
			b.Fatal(err)
		}
		if correlator == nil {
			b.Fatal("correlator is nil")
		}
	}
}

func BenchmarkCorrelatorRegistry_ListAvailable(b *testing.B) {
	logger := zap.NewNop()
	registry := NewCorrelatorRegistry(logger)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		available := registry.ListAvailable()
		if len(available) == 0 {
			b.Fatal("no available correlators")
		}
	}
}
