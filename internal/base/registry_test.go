//go:build linux

package base

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGlobalRegistry_Exists verifies GlobalRegistry is initialized
func TestGlobalRegistry_Exists(t *testing.T) {
	require.NotNil(t, GlobalRegistry, "GlobalRegistry should be initialized")
}

// TestGlobalRegistry_IsPrometheusRegistry verifies type
func TestGlobalRegistry_IsPrometheusRegistry(t *testing.T) {
	var _ prometheus.Registerer = GlobalRegistry
	var _ prometheus.Gatherer = GlobalRegistry
}

// TestGlobalRegistry_CanRegisterMetric verifies registration works
func TestGlobalRegistry_CanRegisterMetric(t *testing.T) {
	// Create a test counter
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "test_registry_counter",
		Help: "Test counter for registry",
	})

	// Should be able to register without panic
	err := GlobalRegistry.Register(counter)
	assert.NoError(t, err)

	// Cleanup
	GlobalRegistry.Unregister(counter)
}

// TestGlobalRegistry_HasGoCollector verifies Go runtime metrics are registered
func TestGlobalRegistry_HasGoCollector(t *testing.T) {
	metrics, err := GlobalRegistry.Gather()
	require.NoError(t, err)

	// Look for a Go runtime metric
	found := false
	for _, m := range metrics {
		if m.GetName() == "go_goroutines" {
			found = true
			break
		}
	}
	assert.True(t, found, "GlobalRegistry should have Go collector registered")
}
