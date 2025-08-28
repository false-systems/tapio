package dns

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

// TestCollectorBasicFunctionality tests basic collector functionality
func TestCollectorBasicFunctionality(t *testing.T) {
	config := DefaultConfig()
	config.EnableIntelligence = true

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

	// Verify intelligence components are initialized
	if config.EnableIntelligence {
		assert.NotNil(t, collector.smartFilter)
		assert.NotNil(t, collector.learningEngine)
		assert.NotNil(t, collector.circuitBreaker)
	}

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
	assert.True(t, config.EnableIntelligence)
	assert.True(t, config.ContainerIDExtraction)
	assert.True(t, config.EnableDNSCacheMetrics)
	assert.True(t, config.ParseAnswers)

	// Check learning config defaults
	assert.True(t, config.LearningConfig.Enabled)
	assert.Equal(t, 24*time.Hour, config.LearningConfig.BaselinePeriod)
	assert.Equal(t, 3.0, config.LearningConfig.AnomalyThreshold)

	// Check circuit breaker defaults
	assert.True(t, config.CircuitBreakerConfig.Enabled)
	assert.Equal(t, 10, config.CircuitBreakerConfig.FailureThreshold)

	// Check smart filter defaults
	assert.Equal(t, FilteringModeBaseline, config.SmartFilterConfig.Mode)
	assert.Equal(t, 0.1, config.SmartFilterConfig.SamplingRate)
	assert.True(t, config.SmartFilterConfig.AdaptiveSampling)
}

// TestIntelligenceComponents tests individual intelligence components
func TestIntelligenceComponents(t *testing.T) {
	// Test Learning Engine
	learningConfig := DefaultLearningConfig()
	learningEngine := NewDNSLearningEngine(learningConfig, nil)
	assert.NotNil(t, learningEngine)

	stats := learningEngine.GetLearningStats()
	assert.NotNil(t, stats)

	// Test Circuit Breaker
	cbConfig := DefaultCircuitBreakerConfig()
	circuitBreaker := NewCircuitBreaker(cbConfig, nil)
	assert.NotNil(t, circuitBreaker)
	assert.True(t, circuitBreaker.AllowRequest())
	assert.Equal(t, CircuitClosed, circuitBreaker.GetState())

	// Test Smart Filter
	smartConfig := DefaultSmartFilterConfig()
	smartFilter := NewSmartFilter(smartConfig, learningEngine, circuitBreaker, nil)
	assert.NotNil(t, smartFilter)

	stats = smartFilter.GetStats()
	assert.NotNil(t, stats)
}

// TestDNSEventProcessing tests basic DNS event processing
func TestDNSEventProcessing(t *testing.T) {
	config := DefaultConfig()
	collector, err := NewCollector("test", config)
	require.NoError(t, err)

	// Create a test DNS event
	event := &domain.CollectorEvent{
		EventID:   "test-1",
		Type:      domain.EventTypeDNS,
		Timestamp: time.Now(),
		Source:    "test",
		Severity:  domain.EventSeverityInfo,
		EventData: domain.EventDataContainer{
			DNS: &domain.DNSData{
				QueryName:    "test.com",
				QueryType:    "A",
				ResponseCode: 0,
				Duration:     30 * time.Millisecond,
			},
		},
		Metadata: domain.EventMetadata{
			Attributes: make(map[string]string),
		},
	}

	// Test event conversion
	dnsEvent := collector.convertToLearningEvent(event)
	assert.NotNil(t, dnsEvent)
	assert.Equal(t, "test.com", dnsEvent.QueryName)
	assert.Equal(t, DNSQueryTypeA, dnsEvent.QueryType)
	assert.Equal(t, uint32(30), dnsEvent.LatencyMs)
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

// TestFilteringModes tests different filtering modes
func TestFilteringModes(t *testing.T) {
	modes := []FilteringMode{
		FilteringModePassthrough,
		FilteringModeBaseline,
		FilteringModeIntelligent,
		FilteringModeEmergency,
	}

	expectedStrings := []string{"passthrough", "baseline", "intelligent", "emergency"}

	for i, mode := range modes {
		assert.Equal(t, expectedStrings[i], mode.String())
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
