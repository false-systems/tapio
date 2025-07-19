package correlation

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

// TestSemanticCorrelationEnginePatternDetection tests pattern detection integration
func TestSemanticCorrelationEnginePatternDetection(t *testing.T) {
	// Create engine
	engine := NewSemanticCorrelationEngine(100, 5*time.Second)
	
	// Start the engine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	err := engine.Start(ctx)
	require.NoError(t, err)
	defer engine.Stop()
	
	// Create test events that simulate a memory leak pattern
	baseTime := time.Now()
	
	// eBPF memory pressure event
	ebpfEvent := Event{
		ID:        "ebpf-001",
		Timestamp: baseTime,
		Source:    "ebpf",
		Type:      "memory_pressure",
		Severity:  SeverityHigh,
		Data: map[string]interface{}{
			"node":  "worker-node-1",
			"usage": 88.5,
			"total": 1024 * 1024 * 1024,
		},
	}
	
	// SystemD restart event
	systemdEvent := Event{
		ID:        "systemd-001",
		Timestamp: baseTime.Add(2 * time.Minute),
		Source:    "systemd",
		Type:      "service_restart",
		Severity:  SeverityHigh,
		Data: map[string]interface{}{
			"node":       "worker-node-1",
			"service":    "api-service",
			"event_type": "restart",
			"reason":     "memory limit exceeded",
		},
	}
	
	// K8s pod eviction event
	k8sEvent := Event{
		ID:        "k8s-001",
		Timestamp: baseTime.Add(3 * time.Minute),
		Source:    "kubernetes",
		Type:      "pod_evicted",
		Severity:  SeverityCritical,
		Data: map[string]interface{}{
			"node":      "worker-node-1",
			"namespace": "production",
			"pod":       "api-service-xyz",
			"reason":    "Evicted",
			"message":   "The node was low on resource: memory",
		},
	}
	
	// Send events to engine
	engine.eventChan <- ebpfEvent
	engine.eventChan <- systemdEvent
	engine.eventChan <- k8sEvent
	
	// Wait for pattern detection to run
	time.Sleep(6 * time.Second)
	
	// Check if insights were generated
	insights := []Insight{}
	timeout := time.After(2 * time.Second)
	
	for {
		select {
		case insight := <-engine.Insights():
			insights = append(insights, insight)
		case <-timeout:
			goto done
		}
	}
	
done:
	// Verify we got pattern-based insights
	assert.NotEmpty(t, insights, "Should have generated insights")
	
	// Find memory leak pattern insight
	var memoryLeakInsight *Insight
	for _, insight := range insights {
		if insight.Type == "pattern:memory_leak" {
			memoryLeakInsight = &insight
			break
		}
	}
	
	if memoryLeakInsight != nil {
		assert.Equal(t, "Memory Leak Pattern Detected", memoryLeakInsight.Title)
		assert.Contains(t, memoryLeakInsight.Description, "Confidence:")
		assert.Len(t, memoryLeakInsight.RelatedEvents, 3)
		assert.NotEmpty(t, memoryLeakInsight.Actions)
		assert.NotNil(t, memoryLeakInsight.Prediction)
	}
	
	// Check pattern statistics
	stats := engine.GetPatternStats()
	assert.NotNil(t, stats.TotalMatches)
	assert.NotNil(t, stats.ProcessingTime)
}

// TestSemanticCorrelationEngineEventConversion tests event conversion
func TestSemanticCorrelationEngineEventConversion(t *testing.T) {
	engine := NewSemanticCorrelationEngine(100, 5*time.Second)
	
	tests := []struct {
		name     string
		event    Event
		validate func(t *testing.T, domainEvent *domain.Event)
	}{
		{
			name: "memory event conversion",
			event: Event{
				ID:        "mem-001",
				Timestamp: time.Now(),
				Source:    "ebpf",
				Type:      "memory",
				Severity:  SeverityHigh,
				Data: map[string]interface{}{
					"usage":     85.5,
					"available": 150.0 * 1024 * 1024,
					"total":     1024.0 * 1024 * 1024,
					"node":      "node-1",
				},
			},
			validate: func(t *testing.T, de *domain.Event) {
				assert.Equal(t, "ebpf", string(de.Source))
				assert.Equal(t, "memory", string(de.Type))
				assert.Equal(t, "node-1", de.Context.Host)
				
				payload, ok := de.Payload.(domain.MemoryEventPayload)
				require.True(t, ok)
				assert.Equal(t, 85.5, payload.Usage)
			},
		},
		{
			name: "kubernetes event conversion",
			event: Event{
				ID:        "k8s-001",
				Timestamp: time.Now(),
				Source:    "kubernetes",
				Type:      "pod_evicted",
				Severity:  SeverityCritical,
				Data: map[string]interface{}{
					"pod":       "api-service-xyz",
					"namespace": "production",
					"reason":    "Evicted",
					"message":   "Out of memory",
				},
			},
			validate: func(t *testing.T, de *domain.Event) {
				assert.Equal(t, "kubernetes", string(de.Source))
				assert.Equal(t, "pod_evicted", string(de.Type))
				
				payload, ok := de.Payload.(domain.KubernetesEventPayload)
				require.True(t, ok)
				assert.Equal(t, "api-service-xyz", payload.Resource.Name)
				assert.Equal(t, "production", payload.Resource.Namespace)
				assert.Equal(t, "Evicted", payload.Reason)
			},
		},
		{
			name: "service event conversion",
			event: Event{
				ID:        "svc-001",
				Timestamp: time.Now(),
				Source:    "systemd",
				Type:      "service_restart",
				Severity:  SeverityHigh,
				Data: map[string]interface{}{
					"service":    "api-service",
					"event_type": "restart",
				},
			},
			validate: func(t *testing.T, de *domain.Event) {
				assert.Equal(t, "systemd", string(de.Source))
				
				payload, ok := de.Payload.(domain.ServiceEventPayload)
				require.True(t, ok)
				assert.Equal(t, "api-service", payload.ServiceName)
				assert.Equal(t, "restart", payload.EventType)
			},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			domainEvent := engine.convertToDomainEvent(tt.event)
			require.NotNil(t, domainEvent)
			tt.validate(t, domainEvent)
		})
	}
}