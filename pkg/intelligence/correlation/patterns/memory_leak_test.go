package patterns
import (
	"context"
	"testing"
	"time"
	"github.com/falseyair/tapio/pkg/domain"
	"github.com/falseyair/tapio/pkg/intelligence/correlation/core"
)
func TestNewMemoryLeakPattern(t *testing.T) {
	pattern := NewMemoryLeakPattern()
	if pattern == nil {
		t.Fatal("Pattern should not be nil")
	}
	if pattern.ID() != "memory_leak_pattern" {
		t.Errorf("Expected pattern ID 'memory_leak_pattern', got %s", pattern.ID())
	}
	if pattern.Category() != core.PatternCategoryResource {
		t.Errorf("Expected category resource, got %v", pattern.Category())
	}
	if pattern.Priority() != core.PatternPriorityHigh {
		t.Errorf("Expected high priority, got %v", pattern.Priority())
	}
	expectedSources := []domain.Source{
		domain.SourceEBPF,
		domain.SourceSystemd,
		domain.SourceKubernetes,
	}
	requiredSources := pattern.RequiredSources()
	if len(requiredSources) != len(expectedSources) {
		t.Errorf("Expected %d required sources, got %d", len(expectedSources), len(requiredSources))
	}
}
func TestMemoryLeakPattern_CanMatch(t *testing.T) {
	pattern := NewMemoryLeakPattern()
	tests := []struct {
		name     string
		event    domain.Event
		expected bool
	}{
		{
			name:     "memory event from eBPF",
			event:    createMemoryEvent(domain.SourceEBPF, 85.0),
			expected: true,
		},
		{
			name:     "low memory usage",
			event:    createMemoryEvent(domain.SourceEBPF, 30.0),
			expected: false,
		},
		{
			name:     "service restart event",
			event:    createServiceRestartEvent(),
			expected: true,
		},
		{
			name:     "kubernetes pod eviction",
			event:    createPodEvictionEvent(),
			expected: true,
		},
		{
			name:     "network event",
			event:    createNetworkEvent(),
			expected: false,
		},
		{
			name:     "unrelated log event",
			event:    createUnrelatedLogEvent(),
			expected: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pattern.CanMatch(tt.event)
			if result != tt.expected {
				t.Errorf("CanMatch() = %v, expected %v", result, tt.expected)
			}
		})
	}
}
func TestMemoryLeakPattern_Match(t *testing.T) {
	pattern := NewMemoryLeakPattern()
	ctx := context.Background()
	tests := []struct {
		name                 string
		events               []domain.Event
		expectedCorrelations int
		minConfidence        float64
	}{
		{
			name:                 "no events",
			events:               []domain.Event{},
			expectedCorrelations: 0,
		},
		{
			name:                 "single event",
			events:               []domain.Event{createMemoryEvent(domain.SourceEBPF, 85.0)},
			expectedCorrelations: 0,
		},
		{
			name: "memory leak sequence",
			events: []domain.Event{
				createMemoryEventAtTime(domain.SourceEBPF, 70.0, time.Now().Add(-10*time.Minute)),
				createMemoryEventAtTime(domain.SourceEBPF, 80.0, time.Now().Add(-8*time.Minute)),
				createServiceRestartEventAtTime(time.Now().Add(-5*time.Minute)),
				createMemoryEventAtTime(domain.SourceEBPF, 85.0, time.Now().Add(-3*time.Minute)),
			},
			expectedCorrelations: 1,
			minConfidence:        0.7,
		},
		{
			name: "kubernetes memory pressure correlation",
			events: []domain.Event{
				createMemoryEventAtTime(domain.SourceEBPF, 90.0, time.Now().Add(-10*time.Minute)),
				createPodEvictionEventAtTime(time.Now().Add(-8*time.Minute)),
				createServiceRestartEventAtTime(time.Now().Add(-5*time.Minute)),
			},
			expectedCorrelations: 1,
			minConfidence:        0.7,
		},
		{
			name: "unrelated events",
			events: []domain.Event{
				createNetworkEvent(),
				createUnrelatedLogEvent(),
			},
			expectedCorrelations: 0,
		},
		{
			name: "events too far apart",
			events: []domain.Event{
				createMemoryEventAtTime(domain.SourceEBPF, 85.0, time.Now().Add(-2*time.Hour)),
				createServiceRestartEventAtTime(time.Now()),
			},
			expectedCorrelations: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			correlations, err := pattern.Match(ctx, tt.events)
			if err != nil {
				t.Errorf("Match() error = %v", err)
				return
			}
			if len(correlations) != tt.expectedCorrelations {
				t.Errorf("Expected %d correlations, got %d", tt.expectedCorrelations, len(correlations))
				return
			}
			// Check confidence if correlations were found
			if len(correlations) > 0 && tt.minConfidence > 0 {
				for i, correlation := range correlations {
					if correlation.Confidence < tt.minConfidence {
						t.Errorf("Correlation %d confidence %f is below minimum %f", 
							i, correlation.Confidence, tt.minConfidence)
					}
					// Verify correlation has proper structure
					if correlation.ID == "" {
						t.Errorf("Correlation %d should have an ID", i)
					}
					if correlation.Type != domain.CorrelationTypeResource {
						t.Errorf("Correlation %d should have resource type, got %v", i, correlation.Type)
					}
					if len(correlation.Events) == 0 {
						t.Errorf("Correlation %d should have events", i)
					}
					if correlation.Description == "" {
						t.Errorf("Correlation %d should have a description", i)
					}
				}
			}
		})
	}
}
func TestMemoryLeakPattern_MemoryTrendAnalysis(t *testing.T) {
	pattern := NewMemoryLeakPattern().(*MemoryLeakPattern)
	// Test increasing memory trend
	increasingEvents := []domain.Event{
		createMemoryEventAtTime(domain.SourceEBPF, 60.0, time.Now().Add(-15*time.Minute)),
		createMemoryEventAtTime(domain.SourceEBPF, 70.0, time.Now().Add(-10*time.Minute)),
		createMemoryEventAtTime(domain.SourceEBPF, 80.0, time.Now().Add(-5*time.Minute)),
		createMemoryEventAtTime(domain.SourceEBPF, 90.0, time.Now()),
	}
	isIncreasing := pattern.hasIncreasingMemoryTrend(increasingEvents)
	if !isIncreasing {
		t.Error("Should detect increasing memory trend")
	}
	// Test stable memory trend
	stableEvents := []domain.Event{
		createMemoryEventAtTime(domain.SourceEBPF, 50.0, time.Now().Add(-15*time.Minute)),
		createMemoryEventAtTime(domain.SourceEBPF, 52.0, time.Now().Add(-10*time.Minute)),
		createMemoryEventAtTime(domain.SourceEBPF, 48.0, time.Now().Add(-5*time.Minute)),
		createMemoryEventAtTime(domain.SourceEBPF, 51.0, time.Now()),
	}
	isStable := pattern.hasIncreasingMemoryTrend(stableEvents)
	if isStable {
		t.Error("Should not detect increasing trend in stable memory")
	}
	// Test decreasing memory trend
	decreasingEvents := []domain.Event{
		createMemoryEventAtTime(domain.SourceEBPF, 90.0, time.Now().Add(-15*time.Minute)),
		createMemoryEventAtTime(domain.SourceEBPF, 80.0, time.Now().Add(-10*time.Minute)),
		createMemoryEventAtTime(domain.SourceEBPF, 70.0, time.Now().Add(-5*time.Minute)),
		createMemoryEventAtTime(domain.SourceEBPF, 60.0, time.Now()),
	}
	isDecreasing := pattern.hasIncreasingMemoryTrend(decreasingEvents)
	if isDecreasing {
		t.Error("Should not detect increasing trend in decreasing memory")
	}
}
func TestMemoryLeakPattern_ServiceRestartDetection(t *testing.T) {
	pattern := NewMemoryLeakPattern().(*MemoryLeakPattern)
	// Test service restart detection
	serviceEvent := createServiceRestartEvent()
	if !pattern.isServiceRestart(serviceEvent) {
		t.Error("Should detect service restart event")
	}
	// Test non-restart service event
	nonRestartEvent := domain.Event{
		Source: domain.SourceSystemd,
		Type:   domain.EventTypeService,
		Payload: domain.ServiceEventPayload{
			ServiceName: "test-service",
			State:       "running",
		},
	}
	if pattern.isServiceRestart(nonRestartEvent) {
		t.Error("Should not detect non-restart as restart")
	}
	// Test non-service event
	memoryEvent := createMemoryEvent(domain.SourceEBPF, 80.0)
	if pattern.isServiceRestart(memoryEvent) {
		t.Error("Should not detect memory event as service restart")
	}
}
// Helper functions for testing
func createMemoryEvent(source domain.Source, usage float64) domain.Event {
	return createMemoryEventAtTime(source, usage, time.Now())
}
func createMemoryEventAtTime(source domain.Source, usage float64, timestamp time.Time) domain.Event {
	return domain.Event{
		ID:          domain.EventID("memory-" + timestamp.Format("150405")),
		Source:      source,
		Type:        domain.EventTypeMemory,
		Timestamp:   timestamp,
		Confidence:  0.9,
		Severity:    getSeverityForMemoryUsage(usage),
		Description: "Memory usage event",
		Context: domain.EventContext{
			Host: "test-host",
			Labels: map[string]string{
				"process": "test-process",
			},
		},
		Payload: domain.MemoryEventPayload{
			Usage:     usage,
			Available: uint64((100 - usage) * 1024 * 1024 * 10), // Simulate available memory
			Total:     uint64(1024 * 1024 * 1024),              // 1GB total
		},
		Metadata: domain.EventMetadata{
			Version: "1.0",
		},
	}
}
func createServiceRestartEvent() domain.Event {
	return createServiceRestartEventAtTime(time.Now())
}
func createServiceRestartEventAtTime(timestamp time.Time) domain.Event {
	return domain.Event{
		ID:          domain.EventID("service-restart-" + timestamp.Format("150405")),
		Source:      domain.SourceSystemd,
		Type:        domain.EventTypeService,
		Timestamp:   timestamp,
		Confidence:  0.95,
		Severity:    domain.SeverityWarn,
		Description: "Service restart detected",
		Context: domain.EventContext{
			Host: "test-host",
			Labels: map[string]string{
				"service": "test-service",
			},
		},
		Payload: domain.ServiceEventPayload{
			ServiceName: "test-service",
			State:       "restarting",
			PID:         1234,
			ExitCode:    &[]int{0}[0],
		},
		Metadata: domain.EventMetadata{
			Version: "1.0",
		},
	}
}
func createPodEvictionEvent() domain.Event {
	return createPodEvictionEventAtTime(time.Now())
}
func createPodEvictionEventAtTime(timestamp time.Time) domain.Event {
	return domain.Event{
		ID:          domain.EventID("pod-eviction-" + timestamp.Format("150405")),
		Source:      domain.SourceKubernetes,
		Type:        domain.EventTypeKubernetes,
		Timestamp:   timestamp,
		Confidence:  0.95,
		Severity:    domain.SeverityWarn,
		Description: "Pod evicted due to memory pressure",
		Context: domain.EventContext{
			Host: "test-host",
			Labels: map[string]string{
				"namespace": "default",
				"pod":       "test-pod",
			},
		},
		Payload: domain.KubernetesEventPayload{
			ObjectKind: "Pod",
			ObjectName: "test-pod",
			Namespace:  "default",
			Reason:     "Evicted",
			Message:    "Pod evicted due to memory pressure",
		},
		Metadata: domain.EventMetadata{
			Version: "1.0",
		},
	}
}
func createNetworkEvent() domain.Event {
	return domain.Event{
		ID:          "network-event",
		Source:      domain.SourceEBPF,
		Type:        domain.EventTypeNetwork,
		Timestamp:   time.Now(),
		Confidence:  0.8,
		Severity:    domain.SeverityInfo,
		Description: "Network activity",
		Context: domain.EventContext{
			Host: "test-host",
		},
		Payload: domain.NetworkEventPayload{
			Protocol:      "tcp",
			SourceIP:      "10.0.0.1",
			DestinationIP: "10.0.0.2",
		},
		Metadata: domain.EventMetadata{
			Version: "1.0",
		},
	}
}
func createUnrelatedLogEvent() domain.Event {
	return domain.Event{
		ID:          "log-event",
		Source:      domain.SourceJournald,
		Type:        domain.EventTypeLog,
		Timestamp:   time.Now(),
		Confidence:  0.7,
		Severity:    domain.SeverityInfo,
		Description: "Generic log message",
		Context: domain.EventContext{
			Host: "test-host",
		},
		Payload: domain.LogEventPayload{
			Message:   "This is a generic log message",
			Level:     "info",
			Timestamp: time.Now(),
		},
		Metadata: domain.EventMetadata{
			Version: "1.0",
		},
	}
}
func getSeverityForMemoryUsage(usage float64) domain.Severity {
	if usage >= 90 {
		return domain.SeverityCritical
	} else if usage >= 80 {
		return domain.SeverityError
	} else if usage >= 70 {
		return domain.SeverityWarn
	} else {
		return domain.SeverityInfo
	}
}