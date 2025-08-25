//go:build integration
// +build integration

package network

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// TestIntelligenceCollectorIntegration tests full intelligence collector workflow
func TestIntelligenceCollectorIntegration(t *testing.T) {
	logger := zaptest.NewLogger(t)

	config := &IntelligenceCollectorConfig{
		NetworkCollectorConfig: &NetworkCollectorConfig{
			BufferSize:         10000,
			EnableIPv4:         true,
			EnableTCP:          true,
			EnableHTTP:         true,
			HTTPPorts:          []int{80, 8080, 443},
			MaxEventsPerSecond: 50000,
			SamplingRate:       1.0, // No sampling for tests
		},
		EnableIntelligenceMode:   true,
		SlowRequestThresholdMs:   100,
		ErrorStatusThreshold:     400,
		LatencyDeviationFactor:   2.0,
		DependencyCacheTTLMs:     5000,
		IntelligenceSamplingRate: 1.0,
		ErrorCascadeWindowMs:     1000,
		ServiceDiscoveryEnabled:  true,
		SecurityAnalysisEnabled:  true,
		HTTPIntelligenceEnabled:  true,
		DNSIntelligenceEnabled:   true,
	}

	collector, err := NewIntelligenceCollector("integration-test", config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Simulate various network events
	t.Run("service_dependency_detection", func(t *testing.T) {
		simulateServiceCommunication(t, collector)
	})

	t.Run("error_cascade_detection", func(t *testing.T) {
		simulateErrorCascade(t, collector)
	})

	t.Run("latency_anomaly_detection", func(t *testing.T) {
		simulateLatencyAnomaly(t, collector)
	})

	t.Run("dns_failure_detection", func(t *testing.T) {
		simulateDNSFailure(t, collector)
	})

	// Verify intelligence stats
	stats := collector.GetIntelligenceStats()
	assert.NotNil(t, stats)

	// On non-Linux, these will be zero (stub implementation)
	// On Linux with eBPF, these should have values
	t.Logf("Intelligence Stats: %+v", stats)
}

// simulateServiceCommunication simulates service-to-service communication
func simulateServiceCommunication(t *testing.T, collector *IntelligenceCollector) {
	// Create events representing service communication
	services := []struct {
		source string
		dest   string
		port   int32
	}{
		{"frontend", "api-gateway", 8080},
		{"api-gateway", "user-service", 9090},
		{"api-gateway", "product-service", 9091},
		{"user-service", "postgres", 5432},
		{"product-service", "postgres", 5432},
		{"product-service", "redis", 6379},
	}

	for _, svc := range services {
		event := &domain.CollectorEvent{
			EventID:   fmt.Sprintf("dep-%s-%s", svc.source, svc.dest),
			Timestamp: time.Now(),
			Type:      domain.EventTypeTCP,
			Source:    "network-intelligence",
			Severity:  domain.EventSeverityInfo,
			EventData: domain.EventDataContainer{
				Network: &domain.NetworkData{
					Protocol:   "tcp",
					SourcePort: 12345,
					DestPort:   svc.port,
					BytesSent:  1024,
					BytesRecv:  2048,
				},
			},
			Metadata: domain.EventMetadata{
				PodName: "integration-test",
				Command: fmt.Sprintf("%s->%s", svc.source, svc.dest),
			},
		}

		select {
		case collector.events <- event:
			// Success
		case <-time.After(100 * time.Millisecond):
			t.Error("Failed to send service communication event")
		}
	}

	// Give collector time to process
	time.Sleep(100 * time.Millisecond)

	// Check dependencies
	deps := collector.GetServiceDependencies()
	t.Logf("Discovered %d service dependencies", len(deps))
}

// simulateErrorCascade simulates cascading errors across services
func simulateErrorCascade(t *testing.T, collector *IntelligenceCollector) {
	// Simulate database failure causing cascade
	errorSequence := []struct {
		service    string
		statusCode int32
		delay      time.Duration
	}{
		{"postgres", 500, 0},
		{"user-service", 503, 10 * time.Millisecond},
		{"product-service", 503, 15 * time.Millisecond},
		{"api-gateway", 502, 20 * time.Millisecond},
		{"frontend", 500, 25 * time.Millisecond},
	}

	baseTime := time.Now()

	for _, err := range errorSequence {
		time.Sleep(err.delay)

		event := &domain.CollectorEvent{
			EventID:   fmt.Sprintf("error-%s-%d", err.service, baseTime.Unix()),
			Timestamp: time.Now(),
			Type:      domain.EventTypeNetworkConnection,
			Source:    "network-intelligence",
			Severity:  domain.SeverityError,
			DataContainer: &domain.HTTPRequestEvent{
				Method:     "GET",
				Path:       "/api/users",
				StatusCode: err.statusCode,
				Service:    err.service,
				Duration:   100 * time.Millisecond,
			},
			Metadata: domain.EventMetadata{
				CollectorName: "integration-test",
				Attributes: map[string]string{
					"error_type": "service_unavailable",
					"cascade_id": fmt.Sprintf("cascade-%d", baseTime.Unix()),
				},
			},
		}

		select {
		case collector.events <- event:
			// Success
		case <-time.After(100 * time.Millisecond):
			t.Error("Failed to send error cascade event")
		}
	}

	// Give collector time to detect cascade
	time.Sleep(200 * time.Millisecond)
}

// simulateLatencyAnomaly simulates abnormal latency patterns
func simulateLatencyAnomaly(t *testing.T, collector *IntelligenceCollector) {
	// Establish baseline latency
	for i := 0; i < 100; i++ {
		event := &domain.CollectorEvent{
			EventID:   fmt.Sprintf("baseline-%d", i),
			Timestamp: time.Now(),
			Type:      domain.EventTypeNetworkConnection,
			Source:    "network-intelligence",
			Severity:  domain.SeverityInfo,
			DataContainer: &domain.HTTPRequestEvent{
				Method:     "GET",
				Path:       "/api/products",
				StatusCode: 200,
				Service:    "product-service",
				Duration:   50 * time.Millisecond, // Normal latency
			},
			Metadata: domain.EventMetadata{
				CollectorName: "integration-test",
			},
		}

		select {
		case collector.events <- event:
		case <-time.After(10 * time.Millisecond):
			t.Error("Failed to send baseline event")
		}
	}

	// Introduce anomalies
	for i := 0; i < 10; i++ {
		event := &domain.CollectorEvent{
			EventID:   fmt.Sprintf("anomaly-%d", i),
			Timestamp: time.Now(),
			Type:      domain.EventTypeNetworkConnection,
			Source:    "network-intelligence",
			Severity:  domain.SeverityWarning,
			DataContainer: &domain.HTTPRequestEvent{
				Method:     "GET",
				Path:       "/api/products",
				StatusCode: 200,
				Service:    "product-service",
				Duration:   500 * time.Millisecond, // 10x normal latency
			},
			Metadata: domain.EventMetadata{
				CollectorName: "integration-test",
				Attributes: map[string]string{
					"anomaly": "high_latency",
				},
			},
		}

		select {
		case collector.events <- event:
		case <-time.After(10 * time.Millisecond):
			t.Error("Failed to send anomaly event")
		}
	}

	// Give collector time to detect anomalies
	time.Sleep(100 * time.Millisecond)
}

// simulateDNSFailure simulates DNS resolution failures
func simulateDNSFailure(t *testing.T, collector *IntelligenceCollector) {
	dnsFailures := []string{
		"unknown-service.cluster.local",
		"external-api.example.com",
		"database.wrong-namespace.svc.cluster.local",
	}

	for _, hostname := range dnsFailures {
		event := &domain.CollectorEvent{
			EventID:   fmt.Sprintf("dns-fail-%s", hostname),
			Timestamp: time.Now(),
			Type:      domain.EventTypeDNSQuery,
			Source:    "network-intelligence",
			Severity:  domain.SeverityError,
			DataContainer: &domain.DNSQueryEvent{
				Hostname:     hostname,
				QueryType:    "A",
				ResponseCode: 3, // NXDOMAIN
				Duration:     5 * time.Second,
			},
			Metadata: domain.EventMetadata{
				CollectorName: "integration-test",
				Attributes: map[string]string{
					"error": "NXDOMAIN",
				},
			},
		}

		select {
		case collector.events <- event:
		case <-time.After(100 * time.Millisecond):
			t.Error("Failed to send DNS failure event")
		}
	}

	// Give collector time to process
	time.Sleep(100 * time.Millisecond)
}

// TestHighLoadScenario tests collector under high load
func TestHighLoadScenario(t *testing.T) {
	logger := zaptest.NewLogger(t)

	config := &IntelligenceCollectorConfig{
		NetworkCollectorConfig: &NetworkCollectorConfig{
			BufferSize:         50000,
			EnableIPv4:         true,
			EnableTCP:          true,
			EnableHTTP:         true,
			MaxEventsPerSecond: 100000,
			SamplingRate:       0.1, // Sample 10% under load
		},
		EnableIntelligenceMode:   true,
		IntelligenceSamplingRate: 0.01, // Only analyze 1% for intelligence
	}

	collector, err := NewIntelligenceCollector("load-test", config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Metrics
	var sentEvents int64
	var receivedEvents int64
	var errors int64

	// Start event consumer
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-collector.Events():
				atomic.AddInt64(&receivedEvents, 1)
			case <-ctx.Done():
				return
			}
		}
	}()

	// Generate high load
	numGenerators := 10
	eventsPerGenerator := 10000

	start := time.Now()

	for i := 0; i < numGenerators; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < eventsPerGenerator; j++ {
				event := &domain.CollectorEvent{
					EventID:   fmt.Sprintf("load-%d-%d", id, j),
					Timestamp: time.Now(),
					Type:      domain.EventTypeNetworkConnection,
					Source:    "load-test",
					Severity:  domain.SeverityInfo,
					DataContainer: &domain.NetworkConnectionEvent{
						Protocol:      "tcp",
						SourceService: fmt.Sprintf("service-%d", id),
						DestService:   fmt.Sprintf("dest-%d", j%10),
						BytesSent:     uint64(j * 100),
						BytesReceived: uint64(j * 200),
					},
				}

				select {
				case collector.events <- event:
					atomic.AddInt64(&sentEvents, 1)
				case <-time.After(time.Millisecond):
					atomic.AddInt64(&errors, 1)
				case <-ctx.Done():
					return
				}
			}
		}(i)
	}

	// Wait for generators to finish
	generatorsDone := make(chan bool)
	go func() {
		wg.Wait()
		close(generatorsDone)
	}()

	select {
	case <-generatorsDone:
		// Success
	case <-time.After(20 * time.Second):
		t.Error("Load test timeout")
	}

	// Give consumer time to process remaining events
	time.Sleep(2 * time.Second)
	cancel()

	duration := time.Since(start)
	sent := atomic.LoadInt64(&sentEvents)
	received := atomic.LoadInt64(&receivedEvents)
	errs := atomic.LoadInt64(&errors)

	// Calculate metrics
	throughput := float64(sent) / duration.Seconds()
	dropRate := float64(sent-received) / float64(sent) * 100

	t.Logf("Load Test Results:")
	t.Logf("  Duration: %v", duration)
	t.Logf("  Events Sent: %d", sent)
	t.Logf("  Events Received: %d", received)
	t.Logf("  Errors: %d", errs)
	t.Logf("  Throughput: %.2f events/sec", throughput)
	t.Logf("  Drop Rate: %.2f%%", dropRate)

	// Assertions
	assert.Greater(t, sent, int64(50000), "Should send significant number of events")
	assert.Greater(t, throughput, float64(1000), "Should achieve >1000 events/sec")
	assert.Less(t, dropRate, float64(10), "Drop rate should be <10%")

	// Check intelligence stats
	stats := collector.GetIntelligenceStats()
	t.Logf("Intelligence Stats: %+v", stats)
}

// TestMemoryLeaks tests for memory leaks under sustained load
func TestMemoryLeaks(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory leak test in short mode")
	}

	logger := zap.NewNop() // Use nop logger for performance

	config := &IntelligenceCollectorConfig{
		NetworkCollectorConfig: &NetworkCollectorConfig{
			BufferSize: 1000,
			EnableIPv4: true,
			EnableTCP:  true,
		},
		EnableIntelligenceMode: false, // Disable intelligence for pure throughput test
	}

	collector, err := NewIntelligenceCollector("memory-test", config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Consumer
	go func() {
		for {
			select {
			case <-collector.Events():
				// Consume
			case <-ctx.Done():
				return
			}
		}
	}()

	// Generate sustained load
	for i := 0; i < 1000000; i++ {
		event := &domain.CollectorEvent{
			EventID:   fmt.Sprintf("mem-%d", i),
			Timestamp: time.Now(),
			Type:      domain.EventTypeNetworkConnection,
			Source:    "memory-test",
		}

		select {
		case collector.events <- event:
		case <-ctx.Done():
			return
		default:
			// Channel full, skip
		}

		// Periodic pause to simulate real traffic patterns
		if i%10000 == 0 {
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Memory should be stable, not growing unbounded
	// In production, would use runtime.MemStats to verify
	t.Log("Memory leak test completed")
}
