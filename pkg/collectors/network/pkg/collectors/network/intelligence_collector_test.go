//go:build linux
// +build linux

package network

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

func TestIntelligenceCollector_Creation(t *testing.T) {
	logger := zaptest.NewLogger(t)

	config := DefaultIntelligenceConfig()
	config.EnableIntelligenceMode = true
	config.SlowRequestThresholdMs = 500
	config.ErrorStatusThreshold = 400

	collector, err := NewIntelligenceCollector("test-intelligence", config, logger)
	require.NoError(t, err)
	require.NotNil(t, collector)

	assert.Equal(t, "test-intelligence", collector.name)
	assert.True(t, config.EnableIntelligenceMode)
	assert.Equal(t, int64(500), config.SlowRequestThresholdMs)
	assert.Equal(t, int32(400), config.ErrorStatusThreshold)
	assert.NotNil(t, collector.intelligenceEvents)
	assert.NotNil(t, collector.serviceDependencies)
	assert.NotNil(t, collector.latencyBaselines)
}

func TestIntelligenceCollector_DefaultConfig(t *testing.T) {
	config := DefaultIntelligenceConfig()

	assert.True(t, config.EnableIntelligenceMode)
	assert.Equal(t, int64(1000), config.SlowRequestThresholdMs)
	assert.Equal(t, int32(400), config.ErrorStatusThreshold)
	assert.Equal(t, 3.0, config.LatencyDeviationFactor)
	assert.Equal(t, int64(300000), config.DependencyCacheTTLMs)
	assert.Equal(t, 1.0, config.IntelligenceSamplingRate)
	assert.True(t, config.ServiceDiscoveryEnabled)
	assert.True(t, config.SecurityAnalysisEnabled)
	assert.True(t, config.HTTPIntelligenceEnabled)
	assert.True(t, config.GRPCIntelligenceEnabled)
	assert.True(t, config.DNSIntelligenceEnabled)

	// Check suspicious patterns are configured
	assert.Contains(t, config.SuspiciousUserAgents, "nmap")
	assert.Contains(t, config.SuspiciousEndpoints, "/.env")
	assert.Contains(t, config.KnownGoodServices, "kubernetes")
}

func TestIntelligenceCollector_ServiceDependencyHandling(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultIntelligenceConfig()

	collector, err := NewIntelligenceCollector("test-intelligence", config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Create a service dependency event
	serviceDep := &ServiceDependency{
		SourceService: "frontend",
		DestService:   "backend-api",
		Protocol:      "tcp",
		DestPort:      8080,
		FirstSeen:     time.Now(),
		LastSeen:      time.Now(),
		RequestCount:  1,
		ErrorCount:    0,
		IsNewService:  true,
	}

	intelEvent := &IntelligenceEvent{
		EventID:           "intel-dep-001",
		Timestamp:         time.Now(),
		Type:              IntelEventServiceDependency,
		Severity:          IntelSeverityInfo,
		SourceService:     serviceDep.SourceService,
		DestService:       serviceDep.DestService,
		ServiceDependency: serviceDep,
	}

	// Test service dependency handling
	collector.handleServiceDependency(ctx, intelEvent)

	// Verify dependency was tracked
	deps := collector.GetServiceDependencies()
	require.Len(t, deps, 1)

	key := "frontend->backend-api:8080"
	dep, exists := deps[key]
	require.True(t, exists)
	assert.Equal(t, "frontend", dep.SourceService)
	assert.Equal(t, "backend-api", dep.DestService)
	assert.Equal(t, int32(8080), dep.DestPort)
	assert.Equal(t, int64(1), dep.RequestCount)
	assert.True(t, dep.IsNewService)

	// Test updating existing dependency
	serviceDep2 := &ServiceDependency{
		SourceService: "frontend",
		DestService:   "backend-api",
		Protocol:      "tcp",
		DestPort:      8080,
		FirstSeen:     time.Now(),
		LastSeen:      time.Now(),
		RequestCount:  5,
		ErrorCount:    1,
		IsNewService:  false,
	}

	intelEvent2 := &IntelligenceEvent{
		EventID:           "intel-dep-002",
		Timestamp:         time.Now(),
		Type:              IntelEventServiceDependency,
		Severity:          IntelSeverityInfo,
		SourceService:     serviceDep2.SourceService,
		DestService:       serviceDep2.DestService,
		ServiceDependency: serviceDep2,
	}

	collector.handleServiceDependency(ctx, intelEvent2)

	// Verify dependency was updated
	deps = collector.GetServiceDependencies()
	require.Len(t, deps, 1)

	dep, exists = deps[key]
	require.True(t, exists)
	assert.Equal(t, int64(6), dep.RequestCount) // 1 + 5
	assert.Equal(t, int64(1), dep.ErrorCount)

	// Verify stats
	stats := collector.GetIntelligenceStats()
	assert.Equal(t, int64(1), stats.NewServicesDiscovered)
	assert.Equal(t, int64(2), stats.ServiceDependencies)
}

func TestIntelligenceCollector_ErrorPatternHandling(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultIntelligenceConfig()

	collector, err := NewIntelligenceCollector("test-intelligence", config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Create an error pattern event
	errorPattern := &ErrorPattern{
		Timestamp:       time.Now(),
		SourceService:   "frontend",
		DestService:     "backend-api",
		Endpoint:        "/api/users",
		Method:          "GET",
		StatusCode:      500,
		IsCascade:       false,
		FirstOccurrence: true,
		Frequency:       1,
	}

	intelEvent := &IntelligenceEvent{
		EventID:       "intel-error-001",
		Timestamp:     time.Now(),
		Type:          IntelEventErrorPattern,
		Severity:      IntelSeverityCritical,
		SourceService: errorPattern.SourceService,
		DestService:   errorPattern.DestService,
		ErrorPattern:  errorPattern,
	}

	// Test error pattern handling
	collector.handleErrorPattern(ctx, intelEvent)

	// Verify stats
	stats := collector.GetIntelligenceStats()
	assert.Equal(t, int64(1), stats.ErrorPatterns)
	assert.Equal(t, int64(1), stats.IntelligentEventsEmitted)

	// Test error cascade detection
	// Add multiple error events from different services in the same time window
	services := []string{"service-a", "service-b", "service-c"}
	for i, service := range services {
		errorPattern := &ErrorPattern{
			Timestamp:       time.Now(),
			SourceService:   service,
			DestService:     "backend-api",
			Endpoint:        "/api/data",
			Method:          "POST",
			StatusCode:      502,
			IsCascade:       false,
			FirstOccurrence: i == 0,
			Frequency:       1,
		}

		intelEvent := &IntelligenceEvent{
			EventID:       fmt.Sprintf("intel-cascade-%d", i),
			Timestamp:     time.Now(),
			Type:          IntelEventErrorPattern,
			Severity:      IntelSeverityCritical,
			SourceService: errorPattern.SourceService,
			DestService:   errorPattern.DestService,
			ErrorPattern:  errorPattern,
		}

		collector.handleErrorPattern(ctx, intelEvent)
	}

	// After processing multiple services, should detect cascade
	stats = collector.GetIntelligenceStats()
	assert.Greater(t, stats.ErrorCascadesDetected, int64(0))
}

func TestIntelligenceCollector_LatencyAnomalyHandling(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultIntelligenceConfig()

	collector, err := NewIntelligenceCollector("test-intelligence", config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Create a latency anomaly event
	latencyAnomaly := &LatencyAnomaly{
		Timestamp:       time.Now(),
		SourceService:   "frontend",
		DestService:     "slow-service",
		Endpoint:        "/api/heavy-computation",
		Latency:         5 * time.Second,
		BaselineLatency: 500 * time.Millisecond,
		DeviationFactor: 10.0,
		Severity:        "critical",
	}

	intelEvent := &IntelligenceEvent{
		EventID:        "intel-latency-001",
		Timestamp:      time.Now(),
		Type:           IntelEventLatencyAnomaly,
		Severity:       IntelSeverityWarning,
		SourceService:  latencyAnomaly.SourceService,
		DestService:    latencyAnomaly.DestService,
		LatencyAnomaly: latencyAnomaly,
	}

	// Test latency anomaly handling
	collector.handleLatencyAnomaly(ctx, intelEvent)

	// Verify baseline was created/updated
	baselineKey := "slow-service:/api/heavy-computation"
	baseline, exists := collector.latencyBaselines[baselineKey]
	require.True(t, exists)
	assert.Equal(t, "/api/heavy-computation", baseline.Endpoint)
	assert.Equal(t, int64(1), baseline.RequestCount)

	// Verify stats
	stats := collector.GetIntelligenceStats()
	assert.Equal(t, int64(1), stats.LatencyAnomalies)
	assert.Equal(t, int64(1), stats.LatencyBaselinesTracked)
}

func TestIntelligenceCollector_DNSFailureHandling(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultIntelligenceConfig()

	collector, err := NewIntelligenceCollector("test-intelligence", config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Create a DNS failure event
	dnsFailure := &DNSFailure{
		Timestamp:     time.Now(),
		SourceService: "frontend",
		Domain:        "non-existent.service.local",
		ResponseCode:  3, // NXDOMAIN
		ResponseText:  "Domain not found",
	}

	intelEvent := &IntelligenceEvent{
		EventID:       "intel-dns-001",
		Timestamp:     time.Now(),
		Type:          IntelEventDNSFailure,
		Severity:      IntelSeverityWarning,
		SourceService: dnsFailure.SourceService,
		DNSFailure:    dnsFailure,
	}

	// Test DNS failure handling
	collector.handleDNSFailure(ctx, intelEvent)

	// Verify stats
	stats := collector.GetIntelligenceStats()
	assert.Equal(t, int64(1), stats.DNSFailures)
	assert.Equal(t, int64(1), stats.IntelligentEventsEmitted)
}

func TestIntelligenceCollector_SecurityConcernHandling(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultIntelligenceConfig()

	collector, err := NewIntelligenceCollector("test-intelligence", config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Create a security concern event
	securityConcern := &SecurityConcern{
		Timestamp:     time.Now(),
		SourceService: "web-service",
		DestService:   "internal-api",
		ConcernType:   "suspicious_user_agent",
		Description:   "Detected scanning tool user agent",
		Evidence: map[string]string{
			"user_agent": "nmap/7.80",
			"endpoint":   "/.env",
		},
		Severity: "high",
	}

	intelEvent := &IntelligenceEvent{
		EventID:         "intel-security-001",
		Timestamp:       time.Now(),
		Type:            IntelEventSecurityConcern,
		Severity:        IntelSeverityCritical,
		SourceService:   securityConcern.SourceService,
		DestService:     securityConcern.DestService,
		SecurityConcern: securityConcern,
	}

	// Test security concern handling
	collector.handleSecurityConcern(ctx, intelEvent)

	// Verify stats
	stats := collector.GetIntelligenceStats()
	assert.Equal(t, int64(1), stats.SecurityConcerns)
	assert.Equal(t, int64(1), stats.IntelligentEventsEmitted)
}

func TestIntelligenceCollector_EventConversion(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultIntelligenceConfig()

	collector, err := NewIntelligenceCollector("test-intelligence", config, logger)
	require.NoError(t, err)

	// Test conversion of error pattern event
	errorPattern := &ErrorPattern{
		Timestamp:       time.Now(),
		SourceService:   "frontend",
		DestService:     "backend",
		Endpoint:        "/api/test",
		Method:          "GET",
		StatusCode:      404,
		IsCascade:       false,
		FirstOccurrence: true,
		Frequency:       1,
	}

	intelEvent := &IntelligenceEvent{
		EventID:       "test-conversion",
		Timestamp:     time.Now(),
		Type:          IntelEventErrorPattern,
		Severity:      IntelSeverityWarning,
		SourceService: "frontend",
		DestService:   "backend",
		SourceIP:      "10.0.0.1",
		DestIP:        "10.0.0.2",
		SourcePort:    45678,
		DestPort:      8080,
		Protocol:      "tcp",
		ErrorPattern:  errorPattern,
		ProcessID:     12345,
		CgroupID:      67890,
		PodUID:        "test-pod-uid",
	}

	// Convert to domain event
	domainEvent := collector.convertIntelligenceEventToDomain(intelEvent)
	require.NotNil(t, domainEvent)

	// Verify conversion
	assert.Equal(t, "test-conversion", domainEvent.EventID)
	assert.Equal(t, domain.EventTypeHTTP, domainEvent.Type)
	assert.Equal(t, domain.SeverityWarning, domainEvent.Severity)
	assert.Equal(t, "test-intelligence", domainEvent.Source)

	// Check network data
	require.NotNil(t, domainEvent.EventData.Network)
	assert.Equal(t, "tcp", domainEvent.EventData.Network.Protocol)
	assert.Equal(t, "outbound", domainEvent.EventData.Network.Direction)
	assert.Equal(t, "10.0.0.1", domainEvent.EventData.Network.SourceIP)
	assert.Equal(t, "10.0.0.2", domainEvent.EventData.Network.DestIP)
	assert.Equal(t, int32(45678), domainEvent.EventData.Network.SourcePort)
	assert.Equal(t, int32(8080), domainEvent.EventData.Network.DestPort)

	// Check HTTP data
	require.NotNil(t, domainEvent.EventData.HTTP)
	assert.Equal(t, int32(404), domainEvent.EventData.HTTP.StatusCode)
	assert.Equal(t, "/api/test", domainEvent.EventData.HTTP.URL)
	assert.Equal(t, "GET", domainEvent.EventData.HTTP.Method)

	// Check metadata
	assert.Equal(t, int32(12345), domainEvent.Metadata.PID)
	assert.Equal(t, uint64(67890), domainEvent.Metadata.CgroupID)
	assert.Equal(t, "test-pod-uid", domainEvent.Metadata.PodUID)
}

func TestIntelligenceCollector_FilteringEfficiency(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultIntelligenceConfig()

	collector, err := NewIntelligenceCollector("test-intelligence", config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Simulate processing 100 events but only 10 are intelligence-worthy
	collector.intelStats.TotalEventsProcessed = 100
	collector.intelStats.IntelligentEventsEmitted = 10

	// Process a single intelligence event to trigger efficiency calculation
	serviceDep := &ServiceDependency{
		SourceService: "test-service",
		DestService:   "test-dest",
		Protocol:      "tcp",
		DestPort:      8080,
		FirstSeen:     time.Now(),
		LastSeen:      time.Now(),
		RequestCount:  1,
		ErrorCount:    0,
		IsNewService:  true,
	}

	intelEvent := &IntelligenceEvent{
		EventID:           "efficiency-test",
		Timestamp:         time.Now(),
		Type:              IntelEventServiceDependency,
		Severity:          IntelSeverityInfo,
		SourceService:     serviceDep.SourceService,
		DestService:       serviceDep.DestService,
		ServiceDependency: serviceDep,
	}

	collector.analyzeIntelligenceEvent(intelEvent)

	// Check filtering efficiency (should be ~90% since only 11 out of 101 events are intelligence-worthy)
	stats := collector.GetIntelligenceStats()
	assert.Greater(t, stats.FilteringEfficiency, 85.0) // Should be around 89%
	assert.Less(t, stats.FilteringEfficiency, 95.0)
}

func TestIntelligenceCollector_PeriodicCleanup(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultIntelligenceConfig()
	config.ErrorCascadeWindowMs = 1000 // 1 second for testing

	collector, err := NewIntelligenceCollector("test-intelligence", config, logger)
	require.NoError(t, err)

	// Add some old error cascade data
	oldTime := time.Now().Add(-5 * time.Second)
	collector.errorCascadeTracker["cascade-old"] = &ErrorCascade{
		WindowStart: oldTime,
		ErrorCount:  5,
		Services:    map[string]int32{"service1": 3, "service2": 2},
		StatusCodes: map[string]int32{"500": 5},
	}

	// Add some recent error cascade data
	recentTime := time.Now()
	collector.errorCascadeTracker["cascade-recent"] = &ErrorCascade{
		WindowStart: recentTime,
		ErrorCount:  2,
		Services:    map[string]int32{"service3": 2},
		StatusCodes: map[string]int32{"404": 2},
	}

	// Run periodic analysis
	collector.performPeriodicAnalysis()

	// Verify old cascade was cleaned up but recent one remains
	assert.NotContains(t, collector.errorCascadeTracker, "cascade-old")
	assert.Contains(t, collector.errorCascadeTracker, "cascade-recent")
}

// MockIntelEventProcessor for testing intelligence event processing
type MockIntelEventProcessor struct {
	events []*domain.CollectorEvent
}

func (m *MockIntelEventProcessor) Process(ctx context.Context, event *domain.CollectorEvent) error {
	m.events = append(m.events, event)
	return nil
}

func TestIntelligenceCollector_EventProcessing(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultIntelligenceConfig()

	collector, err := NewIntelligenceCollector("test-intelligence", config, logger)
	require.NoError(t, err)

	// Set up mock event processor
	mockProcessor := &MockIntelEventProcessor{}
	collector.SetEventProcessor(mockProcessor)

	ctx := context.Background()

	// Create and analyze an intelligence event
	serviceDep := &ServiceDependency{
		SourceService: "test-service",
		DestService:   "test-dest",
		Protocol:      "tcp",
		DestPort:      8080,
		FirstSeen:     time.Now(),
		LastSeen:      time.Now(),
		RequestCount:  1,
		ErrorCount:    0,
		IsNewService:  true,
	}

	intelEvent := &IntelligenceEvent{
		EventID:           "processor-test",
		Timestamp:         time.Now(),
		Type:              IntelEventServiceDependency,
		Severity:          IntelSeverityInfo,
		SourceService:     serviceDep.SourceService,
		DestService:       serviceDep.DestService,
		ServiceDependency: serviceDep,
	}

	collector.analyzeIntelligenceEvent(intelEvent)

	// Verify event was processed
	require.Len(t, mockProcessor.events, 1)
	processedEvent := mockProcessor.events[0]
	assert.Equal(t, "processor-test", processedEvent.EventID)
	assert.Equal(t, domain.EventTypeKernelNetwork, processedEvent.Type)
}

func BenchmarkIntelligenceCollector_ServiceDependencyHandling(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultIntelligenceConfig()

	collector, err := NewIntelligenceCollector("bench-intelligence", config, logger)
	require.NoError(b, err)

	ctx := context.Background()

	serviceDep := &ServiceDependency{
		SourceService: "frontend",
		DestService:   "backend-api",
		Protocol:      "tcp",
		DestPort:      8080,
		FirstSeen:     time.Now(),
		LastSeen:      time.Now(),
		RequestCount:  1,
		ErrorCount:    0,
		IsNewService:  true,
	}

	intelEvent := &IntelligenceEvent{
		EventID:           "bench-dep",
		Timestamp:         time.Now(),
		Type:              IntelEventServiceDependency,
		Severity:          IntelSeverityInfo,
		SourceService:     serviceDep.SourceService,
		DestService:       serviceDep.DestService,
		ServiceDependency: serviceDep,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		collector.handleServiceDependency(ctx, intelEvent)
	}
}
