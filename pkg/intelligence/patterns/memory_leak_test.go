package patternrecognition

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

func TestNewMemoryLeakPattern(t *testing.T) {
	pattern := NewMemoryLeakPattern()

	if pattern == nil {
		t.Fatal("Pattern should not be nil")
	}

	if pattern.ID() != "memory_leak_pattern" {
		t.Errorf("Expected pattern ID 'memory_leak_pattern', got %s", pattern.ID())
	}

	if pattern.Category() != PatternCategoryResource {
		t.Errorf("Expected category resource, got %v", pattern.Category())
	}

	if pattern.Priority() != PatternPriorityCritical {
		t.Errorf("Expected critical priority, got %v", pattern.Priority())
	}

	expectedSources := []domain.SourceType{
		domain.SourceEBPF,
		domain.SourceSystemd,
		domain.SourceK8s,
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
			expected: true, // Still memory-related, just not high pressure
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
				createMemoryEventWithTime(domain.SourceEBPF, 85.0, time.Now().Add(-10*time.Minute)),
				createMemoryEventWithTime(domain.SourceEBPF, 90.0, time.Now().Add(-8*time.Minute)),
				createServiceRestartEventWithTime(time.Now().Add(-5 * time.Minute)),
				createPodEvictionEventWithTime(time.Now().Add(-2 * time.Minute)),
			},
			expectedCorrelations: 1,
			minConfidence:        0.8,
		},
		{
			name: "multiple hosts",
			events: []domain.Event{
				createMemoryEventOnHost(domain.SourceEBPF, 85.0, "host1"),
				createMemoryEventOnHost(domain.SourceEBPF, 90.0, "host1"),
				createServiceRestartEventOnHost("host1"),
				createMemoryEventOnHost(domain.SourceEBPF, 88.0, "host2"),
				createServiceRestartEventOnHost("host2"),
				createPodEvictionEventOnHost("host2"),
			},
			expectedCorrelations: 2, // One per host
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			correlations, err := pattern.Match(ctx, tt.events)
			if err != nil {
				t.Fatalf("Match() error = %v", err)
			}

			if len(correlations) != tt.expectedCorrelations {
				t.Errorf("Match() returned %d correlations, expected %d", len(correlations), tt.expectedCorrelations)
			}

			// Check confidence scores
			for _, correlation := range correlations {
				if tt.minConfidence > 0 && correlation.Confidence.Overall < tt.minConfidence {
					t.Errorf("Correlation confidence %f below minimum %f", correlation.Confidence.Overall, tt.minConfidence)
				}
			}
		})
	}
}

// Helper functions to create test events

func createMemoryEvent(source domain.SourceType, usage float64) domain.Event {
	return createMemoryEventWithTime(source, usage, time.Now())
}

func createMemoryEventWithTime(source domain.SourceType, usage float64, timestamp time.Time) domain.Event {
	return domain.Event{
		ID:        domain.EventID(fmt.Sprintf("mem-%d", timestamp.UnixNano())),
		Type:      domain.EventTypeMemory,
		Source:    source,
		Severity:  domain.SeverityWarn,
		Timestamp: timestamp,
		Payload: domain.MemoryEventPayload{
			Usage:     usage,
			Available: uint64((100 - usage) * 1024 * 1024 * 1024 / 100),
			Total:     1024 * 1024 * 1024,
		},
		Context: domain.EventContext{
			Host: "test-host",
		},
		Metadata: domain.EventMetadata{
			SchemaVersion: "v1",
			ProcessedAt:   time.Now(),
		},
	}
}

func createMemoryEventOnHost(source domain.SourceType, usage float64, host string) domain.Event {
	event := createMemoryEvent(source, usage)
	event.Context.Host = host
	return event
}

func createServiceRestartEvent() domain.Event {
	return createServiceRestartEventWithTime(time.Now())
}

func createServiceRestartEventWithTime(timestamp time.Time) domain.Event {
	return domain.Event{
		ID:        domain.EventID(fmt.Sprintf("restart-%d", timestamp.UnixNano())),
		Type:      domain.EventTypeService,
		Source:    domain.SourceSystemd,
		Severity:  domain.SeverityError,
		Timestamp: timestamp,
		Payload: domain.ServiceEventPayload{
			ServiceName: "test-service",
			EventType:   "restart due to memory",
			OldState:    "running",
			NewState:    "failed",
		},
		Context: domain.EventContext{
			Host: "test-host",
		},
		Metadata: domain.EventMetadata{
			SchemaVersion: "v1",
			ProcessedAt:   time.Now(),
		},
	}
}

func createServiceRestartEventOnHost(host string) domain.Event {
	event := createServiceRestartEvent()
	event.Context.Host = host
	return event
}

func createPodEvictionEvent() domain.Event {
	return createPodEvictionEventWithTime(time.Now())
}

func createPodEvictionEventWithTime(timestamp time.Time) domain.Event {
	return domain.Event{
		ID:        domain.EventID(fmt.Sprintf("evict-%d", timestamp.UnixNano())),
		Type:      domain.EventTypeKubernetes,
		Source:    domain.SourceK8s,
		Severity:  domain.SeverityCritical,
		Timestamp: timestamp,
		Payload: domain.KubernetesEventPayload{
			Resource: domain.ResourceRef{
				Kind:      "Pod",
				Name:      "test-pod",
				Namespace: "default",
			},
			EventType: "Warning",
			Reason:    "Evicted",
			Message:   "The node was low on resource: memory. Container test was using 1Gi, which exceeds its request of 0.",
		},
		Context: domain.EventContext{
			Host:      "test-host",
			Namespace: "default",
		},
		Metadata: domain.EventMetadata{
			SchemaVersion: "v1",
			ProcessedAt:   time.Now(),
		},
	}
}

func createPodEvictionEventOnHost(host string) domain.Event {
	event := createPodEvictionEvent()
	event.Context.Host = host
	return event
}

func createNetworkEvent() domain.Event {
	return domain.Event{
		ID:        domain.EventID(fmt.Sprintf("net-%d", time.Now().UnixNano())),
		Type:      domain.EventTypeNetwork,
		Source:    domain.SourceEBPF,
		Severity:  domain.SeverityInfo,
		Timestamp: time.Now(),
		Payload: domain.NetworkEventPayload{
			Protocol: "tcp",
		},
		Context: domain.EventContext{
			Host: "test-host",
		},
		Metadata: domain.EventMetadata{
			SchemaVersion: "v1",
			ProcessedAt:   time.Now(),
		},
	}
}
