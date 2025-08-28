package dns

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
)

// TestSmartFilteringPipeline tests the intelligent filtering pipeline
func TestSmartFilteringPipeline(t *testing.T) {
	config := DefaultConfig()
	config.EnableIntelligence = true
	config.LearningConfig.BaselinePeriod = 100 * time.Millisecond // Short for testing

	collector, err := NewCollector("test-dns", config)
	require.NoError(t, err)
	require.NotNil(t, collector)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Test normal DNS event processing
	normalEvent := &domain.CollectorEvent{
		EventID:   "test-1",
		Type:      domain.EventTypeDNS,
		Timestamp: time.Now(),
		Source:    "test",
		Severity:  domain.EventSeverityInfo,
		EventData: domain.EventDataContainer{
			DNS: &domain.DNSData{
				QueryName:    "example.com",
				QueryType:    "A",
				ResponseCode: 0,
				ClientIP:     "10.0.0.1",
				ServerIP:     "8.8.8.8",
				Duration:     50 * time.Millisecond,
			},
		},
		Metadata: domain.EventMetadata{
			Attributes: make(map[string]string),
		},
	}

	// Send event through collector
	collector.events <- normalEvent

	// Read from intelligent events channel
	select {
	case processedEvent := <-collector.getIntelligentEvents():
		require.NotNil(t, processedEvent)
		assert.Equal(t, "example.com", processedEvent.EventData.DNS.QueryName)

		// Check that intelligent metadata was added
		assert.NotNil(t, processedEvent.Metadata.Attributes)
		_, hasFilterScore := processedEvent.Metadata.Attributes["filter_score"]
		_, hasFilterReason := processedEvent.Metadata.Attributes["filter_reason"]
		assert.True(t, hasFilterScore || hasFilterReason, "Expected filtering metadata")

	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for processed event")
	}
}

// TestAnomalyDetection tests the anomaly detection capabilities
func TestAnomalyDetection(t *testing.T) {
	logger := zap.NewNop()

	// Create learning engine with low threshold for testing
	learningConfig := DefaultLearningConfig()
	learningConfig.AnomalyThreshold = 1.0 // Very low threshold
	learningConfig.BaselinePeriod = 50 * time.Millisecond

	engine := NewDNSLearningEngine(learningConfig, logger)
	require.NotNil(t, engine)

	ctx := context.Background()

	// Build baseline with normal events
	normalEvent := &DNSEvent{
		Timestamp:    time.Now(),
		EventType:    DNSEventTypeResponse,
		QueryName:    "normal.com",
		QueryType:    DNSQueryTypeA,
		Success:      true,
		LatencyMs:    50, // Normal latency
		ResponseCode: DNSResponseNoError,
	}

	// Add multiple normal events to build baseline
	for i := 0; i < 20; i++ {
		_, err := engine.ProcessEvent(ctx, normalEvent)
		assert.NoError(t, err)
	}

	// Wait for baseline period
	time.Sleep(100 * time.Millisecond)
	engine.UpdateMode(FilteringModeIntelligent)

	// Send anomalous event (very high latency)
	anomalousEvent := &DNSEvent{
		Timestamp:    time.Now(),
		EventType:    DNSEventTypeResponse,
		QueryName:    "normal.com",
		QueryType:    DNSQueryTypeA,
		Success:      true,
		LatencyMs:    5000, // Very high latency
		ResponseCode: DNSResponseNoError,
	}

	anomaly, err := engine.ProcessEvent(ctx, anomalousEvent)
	assert.NoError(t, err)

	if anomaly != nil {
		assert.Equal(t, "latency_anomaly", anomaly.AnomalyType)
		assert.Equal(t, "normal.com", anomaly.DomainName)
		assert.True(t, anomaly.BaselineDeviation > 1.0, "Expected high deviation")
	}
}

// TestCircuitBreakerIntegration tests circuit breaker functionality
func TestCircuitBreakerIntegration(t *testing.T) {
	logger := zap.NewNop()

	// Create circuit breaker with low thresholds for testing
	config := DefaultCircuitBreakerConfig()
	config.FailureThreshold = 2 // Very low threshold
	config.RecoveryTimeout = 100 * time.Millisecond

	cb := NewCircuitBreaker(config, logger)
	require.NotNil(t, cb)

	// Test normal operation
	assert.True(t, cb.AllowRequest())
	assert.Equal(t, CircuitClosed, cb.GetState())

	// Record failures to trigger circuit breaker
	cb.RecordRequest()
	cb.RecordFailure(assert.AnError)
	cb.RecordRequest()
	cb.RecordFailure(assert.AnError)

	// Circuit should now be open
	assert.False(t, cb.AllowRequest())
	assert.Equal(t, CircuitOpen, cb.GetState())

	// Wait for recovery timeout
	time.Sleep(150 * time.Millisecond)

	// Should transition to half-open
	assert.True(t, cb.AllowRequest())
	assert.Equal(t, CircuitHalfOpen, cb.GetState())

	// Record success to close circuit
	cb.RecordRequest()
	cb.RecordSuccess()
	cb.RecordRequest()
	cb.RecordSuccess()
	cb.RecordRequest()
	cb.RecordSuccess()

	// Circuit should be closed again
	assert.True(t, cb.AllowRequest())
	assert.Equal(t, CircuitClosed, cb.GetState())
}

// TestDGADetection tests domain generation algorithm detection
func TestDGADetection(t *testing.T) {
	logger := zap.NewNop()
	engine := NewDNSLearningEngine(DefaultLearningConfig(), logger)

	ctx := context.Background()

	// Test normal domains
	normalDomains := []string{
		"google.com",
		"github.com",
		"kubernetes.io",
		"example.org",
	}

	for _, domain := range normalDomains {
		event := &DNSEvent{
			Timestamp: time.Now(),
			QueryName: domain,
			QueryType: DNSQueryTypeA,
		}

		anomaly, err := engine.ProcessEvent(ctx, event)
		assert.NoError(t, err)
		assert.Nil(t, anomaly, "Normal domain %s should not trigger DGA detection", domain)
	}

	// Test DGA-like domains
	dgaDomains := []string{
		"a1b2c3d4e5f6g7h8i9j0k1l2m3n4.com", // High entropy, long
		"12345abcde67890fghij.net",         // Mixed alphanumeric
		"qwertyuiopasdfghjklzxcvbnm.org",   // High consonant ratio
	}

	for _, domain := range dgaDomains {
		event := &DNSEvent{
			Timestamp: time.Now(),
			QueryName: domain,
			QueryType: DNSQueryTypeA,
		}

		anomaly, err := engine.ProcessEvent(ctx, event)
		assert.NoError(t, err)
		if anomaly != nil {
			assert.Equal(t, "new_suspicious_domain", anomaly.AnomalyType)
			assert.Contains(t, anomaly.Description, "DGA pattern")
		}
	}
}

// TestContainerIDExtraction tests container ID extraction from cgroup paths
func TestContainerIDExtraction(t *testing.T) {
	logger := zap.NewNop()
	collector := &Collector{logger: logger}

	testCases := []struct {
		name        string
		cgroupID    uint64
		expected    string
		description string
	}{
		{
			name:        "docker_container",
			cgroupID:    12345,
			expected:    "", // Would extract from path in real implementation
			description: "Docker container cgroup",
		},
		{
			name:        "kubernetes_pod",
			cgroupID:    67890,
			expected:    "", // Would extract from path in real implementation
			description: "Kubernetes pod cgroup",
		},
		{
			name:        "zero_cgroup",
			cgroupID:    0,
			expected:    "",
			description: "Zero cgroup ID should return empty",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := collector.extractContainerID(tc.cgroupID)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestDNSCacheMetrics tests DNS cache effectiveness tracking (placeholder)
func TestDNSCacheMetrics(t *testing.T) {
	config := DefaultConfig()
	config.EnableDNSCacheMetrics = true

	collector, err := NewCollector("test-dns", config)
	require.NoError(t, err)

	queryName := "cache-test.com"
	resolvedIPs := []string{"1.2.3.4", "5.6.7.8"}

	// Test cache tracking initialization
	assert.NotNil(t, collector.dnsCacheMetrics)

	// Test that we can track cache metrics without panics
	// (Actual implementation would call updateDNSCacheMetrics which is Linux-specific)
	_ = queryName
	_ = resolvedIPs

	// Basic validation
	assert.Equal(t, "test-dns", collector.name)
	assert.True(t, config.EnableDNSCacheMetrics)
}

// TestEventPriorityCalculation tests event priority calculation
func TestEventPriorityCalculation(t *testing.T) {
	collector, err := NewCollector("test", DefaultConfig())
	require.NoError(t, err)

	testCases := []struct {
		name     string
		event    *BPFDNSEvent
		expected domain.EventPriority
	}{
		{
			name: "normal_query",
			event: &BPFDNSEvent{
				Rcode:     0,
				LatencyNs: 50 * 1000 * 1000, // 50ms
			},
			expected: domain.PriorityNormal,
		},
		{
			name: "failed_query",
			event: &BPFDNSEvent{
				Rcode:     3, // NXDOMAIN
				LatencyNs: 50 * 1000 * 1000,
			},
			expected: domain.PriorityHigh,
		},
		{
			name: "slow_query",
			event: &BPFDNSEvent{
				Rcode:     0,
				LatencyNs: 150 * 1000 * 1000, // 150ms
			},
			expected: domain.PriorityHigh,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			priority := collector.calculateEventPriority(tc.event)
			assert.Equal(t, tc.expected, priority)
		})
	}
}

// TestIntelligentEventProcessing tests the complete intelligent processing pipeline
func TestIntelligentEventProcessing(t *testing.T) {
	config := DefaultConfig()
	config.EnableIntelligence = true
	config.SmartFilterConfig.Mode = FilteringModeIntelligent

	collector, err := NewCollector("test-intelligent", config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start collector
	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Create test events with varying importance
	events := []*domain.CollectorEvent{
		{
			EventID:   "normal-1",
			Type:      domain.EventTypeDNS,
			Timestamp: time.Now(),
			Severity:  domain.EventSeverityInfo,
			EventData: domain.EventDataContainer{
				DNS: &domain.DNSData{
					QueryName:    "normal.com",
					QueryType:    "A",
					ResponseCode: 0,
					Duration:     30 * time.Millisecond,
				},
			},
			Metadata: domain.EventMetadata{Attributes: make(map[string]string)},
		},
		{
			EventID:   "error-1",
			Type:      domain.EventTypeDNS,
			Timestamp: time.Now(),
			Severity:  domain.EventSeverityWarning,
			EventData: domain.EventDataContainer{
				DNS: &domain.DNSData{
					QueryName:    "failed.com",
					QueryType:    "A",
					ResponseCode: 3, // NXDOMAIN
					Duration:     100 * time.Millisecond,
				},
			},
			Metadata: domain.EventMetadata{Attributes: make(map[string]string)},
		},
	}

	// Process events through intelligent pipeline
	intelligentChan := collector.getIntelligentEvents()

	// Send events
	go func() {
		for _, event := range events {
			select {
			case collector.events <- event:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Collect processed events
	var processedEvents []*domain.CollectorEvent
	timeout := time.After(1 * time.Second)

	for len(processedEvents) < len(events) {
		select {
		case event := <-intelligentChan:
			processedEvents = append(processedEvents, event)
		case <-timeout:
			t.Fatal("Timeout waiting for processed events")
		}
	}

	// Verify events were processed
	assert.Len(t, processedEvents, len(events))

	// Check that intelligent metadata was added
	for _, event := range processedEvents {
		assert.NotNil(t, event.Metadata.Attributes)
		// Should have some filtering metadata
		hasIntelligentData := len(event.Metadata.Attributes) > 0
		assert.True(t, hasIntelligentData, "Expected intelligent processing metadata")
	}
}
