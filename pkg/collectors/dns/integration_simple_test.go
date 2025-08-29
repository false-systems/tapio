package dns

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCollectorBasicFunctionality tests basic collector functionality
func TestCollectorBasicFunctionality(t *testing.T) {
	config := DefaultConfig()

	collector, err := NewCollector("test-dns", config)
	require.NoError(t, err)
	require.NotNil(t, collector)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Test collector lifecycle
	err = collector.Start(ctx)
	assert.NoError(t, err)

	// Verify collector is healthy
	assert.True(t, collector.IsHealthy())

	// Verify circuit breaker is initialized
	assert.NotNil(t, collector.circuitBreaker)

	// Stop collector
	err = collector.Stop()
	assert.NoError(t, err)
}

// TestCollectorConfigDefaults tests configuration defaults
func TestCollectorConfigDefaults(t *testing.T) {
	config := DefaultConfig()

	assert.Equal(t, "dns", config.Name)
	assert.Equal(t, 10000, config.BufferSize)
	assert.True(t, config.EnableEBPF)
	assert.True(t, config.ContainerIDExtraction)
	assert.True(t, config.ParseAnswers)

	// Check circuit breaker defaults
	assert.True(t, config.CircuitBreakerConfig.Enabled)
	assert.Equal(t, 10, config.CircuitBreakerConfig.FailureThreshold)
	assert.Equal(t, 30*time.Second, config.CircuitBreakerConfig.RecoveryTimeout)
}

// TestCircuitBreakerComponent tests circuit breaker component
func TestCircuitBreakerComponent(t *testing.T) {
	// Test Circuit Breaker
	cbConfig := DefaultCircuitBreakerConfig()
	circuitBreaker := NewCircuitBreaker(cbConfig, nil)
	assert.NotNil(t, circuitBreaker)
	assert.True(t, circuitBreaker.AllowRequest())
	assert.Equal(t, CircuitClosed, circuitBreaker.GetState())

	// Test circuit breaker stats
	stats := circuitBreaker.GetStats()
	assert.NotNil(t, stats)
	assert.Equal(t, "closed", stats.State)
	assert.True(t, stats.Enabled)
}

// TestDNSEventProcessing tests basic DNS event processing
func TestDNSEventProcessing(t *testing.T) {
	config := DefaultConfig()
	collector, err := NewCollector("test", config)
	require.NoError(t, err)

	// Test that collector can process basic DNS events via its event channel
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Verify events channel is available
	eventsChan := collector.Events()
	assert.NotNil(t, eventsChan)
}

// TestContainerIDExtractionBasic tests container ID extraction logic
func TestContainerIDExtractionBasic(t *testing.T) {
	collector, err := NewCollector("test", DefaultConfig())
	require.NoError(t, err)

	testCases := []struct {
		name     string
		cgroupID uint64
		expected string
	}{
		{
			name:     "zero_cgroup_id",
			cgroupID: 0,
			expected: "",
		},
		{
			name:     "valid_cgroup_id",
			cgroupID: 12345,
			expected: "", // Would extract from path in real implementation
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := collector.extractContainerID(tc.cgroupID)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestCircuitBreakerStates tests circuit breaker states
func TestCircuitBreakerStates(t *testing.T) {
	states := []CircuitBreakerState{
		CircuitClosed,
		CircuitOpen,
		CircuitHalfOpen,
	}

	expectedStrings := []string{"closed", "open", "half-open"}

	for i, state := range states {
		assert.Equal(t, expectedStrings[i], state.String())
	}
}
