package behavior

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

// TestObservationEventIntegration tests the complete intelligence layer with ObservationEvent
func TestObservationEventIntegration(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ctx := context.Background()

	// Create behavior engine
	engine, err := NewEngine(logger)
	require.NoError(t, err)
	require.NotNil(t, engine)

	// Create test observation event with correlation keys
	podName := "test-pod"
	namespace := "default"
	serviceName := "test-service"
	nodeName := "worker-node-1"
	pid := int32(1234)
	action := "connect"
	target := "database.example.com:5432"
	result := "success"
	duration := int64(150) // 150ms

	observationEvent := &domain.ObservationEvent{
		ID:        "obs-test-123",
		Timestamp: time.Now(),
		Source:    "kernel",
		Type:      "syscall",

		// Correlation keys
		PID:         &pid,
		PodName:     &podName,
		Namespace:   &namespace,
		ServiceName: &serviceName,
		NodeName:    &nodeName,

		// Event data
		Action:   &action,
		Target:   &target,
		Result:   &result,
		Duration: &duration,

		// Additional data
		Data: map[string]string{
			"syscall":  "connect",
			"family":   "AF_INET",
			"protocol": "tcp",
		},
	}

	// Validate the observation event
	err = observationEvent.Validate()
	require.NoError(t, err, "ObservationEvent should be valid")

	// Test correlation key extraction
	correlationKeys := observationEvent.GetCorrelationKeys()
	assert.NotEmpty(t, correlationKeys, "Should have correlation keys")
	assert.Equal(t, "1234", correlationKeys["pid"])
	assert.Equal(t, "test-pod", correlationKeys["pod_name"])
	assert.Equal(t, "default", correlationKeys["namespace"])
	assert.Equal(t, "test-service", correlationKeys["service_name"])
	assert.Equal(t, "worker-node-1", correlationKeys["node_name"])

	// Test context extraction
	context := extractContext(observationEvent)
	assert.NotNil(t, context, "Should extract context from observation event")
	assert.Equal(t, "kernel", context.GetSource())
	assert.Equal(t, "syscall", context.GetEventType())
	assert.Equal(t, uint32(pid), *context.PID)
	assert.Equal(t, podName, *context.PodName)
	assert.Equal(t, namespace, *context.Namespace)
	assert.Equal(t, serviceName, *context.ServiceName)
	assert.Equal(t, nodeName, *context.NodeName)
	assert.Equal(t, action, *context.Action)
	assert.Equal(t, target, *context.Target)
	assert.Equal(t, result, *context.Result)
	assert.Equal(t, uint64(duration), *context.DurationMS)

	// Test pattern matcher field extraction
	matcher := NewPatternMatcher(logger)

	// Test various field extractions
	assert.Equal(t, "syscall", matcher.getFieldValue(observationEvent, "type").ToInterface())
	assert.Equal(t, "kernel", matcher.getFieldValue(observationEvent, "source").ToInterface())
	assert.Equal(t, int64(pid), matcher.getFieldValue(observationEvent, "pid").ToInterface())
	assert.Equal(t, podName, matcher.getFieldValue(observationEvent, "pod_name").ToInterface())
	assert.Equal(t, namespace, matcher.getFieldValue(observationEvent, "namespace").ToInterface())
	assert.Equal(t, serviceName, matcher.getFieldValue(observationEvent, "service_name").ToInterface())
	assert.Equal(t, nodeName, matcher.getFieldValue(observationEvent, "node_name").ToInterface())
	assert.Equal(t, action, matcher.getFieldValue(observationEvent, "action").ToInterface())
	assert.Equal(t, target, matcher.getFieldValue(observationEvent, "target").ToInterface())
	assert.Equal(t, result, matcher.getFieldValue(observationEvent, "result").ToInterface())
	assert.Equal(t, uint64(duration), matcher.getFieldValue(observationEvent, "duration").ToInterface())
	assert.Equal(t, "connect", matcher.getFieldValue(observationEvent, "data.syscall").ToInterface())
	assert.Equal(t, "AF_INET", matcher.getFieldValue(observationEvent, "data.family").ToInterface())

	// Test processing through engine (without actual patterns)
	predictionResult, err := engine.Process(ctx, observationEvent)
	// We expect no error but no result since we don't have loaded patterns
	assert.NoError(t, err)
	// Result should be nil since no patterns are loaded
	assert.Nil(t, predictionResult)

	// Test health check
	healthy, details := engine.Health(ctx)
	assert.True(t, healthy, "Engine should be healthy")
	assert.NotNil(t, details, "Should have health details")
	assert.Equal(t, 0, details.PatternsLoaded, "Should have 0 patterns loaded")
	assert.Equal(t, "closed", details.CircuitBreaker, "Circuit breaker should be closed")
	assert.Equal(t, float64(0), details.QueueUsage, "Queue usage should be 0")

	t.Logf("✅ Successfully tested ObservationEvent integration")
	t.Logf("✅ ObservationEvent ID: %s", observationEvent.ID)
	t.Logf("✅ Correlation keys: %v", correlationKeys)
	t.Logf("✅ Context extraction: source=%s, type=%s, pod=%s",
		context.GetSource(), context.GetEventType(), *context.PodName)
}

// TestObservationEventProcessingPipeline tests the complete processing pipeline
func TestObservationEventProcessingPipeline(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ctx := context.Background()

	// Create components
	engine, err := NewEngine(logger)
	require.NoError(t, err)

	// Test multiple observation events with different correlation keys
	testEvents := []*domain.ObservationEvent{
		// Kernel event
		{
			ID:        "obs-kernel-001",
			Timestamp: time.Now(),
			Source:    "kernel",
			Type:      "syscall",
			PID:       intPtr(1001),
			PodName:   stringPtr("web-app"),
			Namespace: stringPtr("production"),
			Action:    stringPtr("open"),
			Target:    stringPtr("/etc/passwd"),
			Result:    stringPtr("denied"),
		},
		// DNS event
		{
			ID:          "obs-dns-001",
			Timestamp:   time.Now(),
			Source:      "dns",
			Type:        "dns_query",
			PodName:     stringPtr("web-app"),
			Namespace:   stringPtr("production"),
			ServiceName: stringPtr("web-service"),
			Action:      stringPtr("query"),
			Target:      stringPtr("malicious.domain.com"),
			Result:      stringPtr("resolved"),
		},
		// Kubernetes event
		{
			ID:        "obs-k8s-001",
			Timestamp: time.Now(),
			Source:    "kubeapi",
			Type:      "pod_created",
			PodName:   stringPtr("suspicious-pod"),
			Namespace: stringPtr("production"),
			NodeName:  stringPtr("worker-node-2"),
			Action:    stringPtr("create"),
			Target:    stringPtr("Pod/suspicious-pod"),
			Result:    stringPtr("success"),
		},
	}

	// Process each event
	for _, event := range testEvents {
		t.Run(event.ID, func(t *testing.T) {
			// Validate event
			err := event.Validate()
			require.NoError(t, err, "Event should be valid")

			// Check correlation keys
			assert.True(t, event.HasCorrelationKey(), "Should have at least one correlation key")

			// Process through engine
			result, err := engine.Process(ctx, event)
			assert.NoError(t, err, "Processing should not error")
			// Result may be nil if no patterns match, which is expected for this test
			_ = result

			t.Logf("✅ Processed %s event: %s", event.Source, event.ID)
		})
	}

	// Verify health after processing
	healthy, details := engine.Health(ctx)
	assert.True(t, healthy, "Engine should still be healthy after processing")
	t.Logf("✅ Engine health after processing: %v", details)
}

// TestObservationEventFieldMapping verifies field mapping between UnifiedEvent and ObservationEvent
func TestObservationEventFieldMapping(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Test that we can map common fields that existed in UnifiedEvent
	observationEvent := &domain.ObservationEvent{
		ID:        "obs-mapping-test",
		Timestamp: time.Now(),
		Source:    "kernel",
		Type:      "process_exec", // Maps to what used to be event.Type

		// These were previously in nested structures, now at top level
		PID:         intPtr(9999),
		ContainerID: stringPtr("container-abc123"),
		PodName:     stringPtr("migrated-pod"),
		Namespace:   stringPtr("migration-test"),
		ServiceName: stringPtr("migrated-service"),
		NodeName:    stringPtr("migration-node"),

		// Event-specific data (previously in different nested structures)
		Action: stringPtr("exec"),
		Target: stringPtr("/bin/bash"),
		Result: stringPtr("success"),

		// Custom data (previously in attributes or custom fields)
		Data: map[string]string{
			"command":    "/bin/bash -c 'ls -la'",
			"parent_pid": "1",
			"user":       "root",
		},
	}

	// Test field extraction with the pattern matcher
	matcher := NewPatternMatcher(logger)

	// Verify all the key fields can be extracted
	tests := []struct {
		field    string
		expected any
	}{
		{"type", "process_exec"},
		{"source", "kernel"},
		{"pid", int64(9999)}, // Note: FieldValue converts int32 to int64
		{"container_id", "container-abc123"},
		{"pod_name", "migrated-pod"},
		{"namespace", "migration-test"},
		{"service_name", "migrated-service"},
		{"node_name", "migration-node"},
		{"action", "exec"},
		{"target", "/bin/bash"},
		{"result", "success"},
		{"data.command", "/bin/bash -c 'ls -la'"},
		{"data.parent_pid", "1"},
		{"data.user", "root"},
	}

	for _, test := range tests {
		t.Run(test.field, func(t *testing.T) {
			fieldValue := matcher.getFieldValue(observationEvent, test.field)
			require.NotNil(t, fieldValue, "Field value should not be nil")
			assert.False(t, fieldValue.IsNil, "Field should not be nil")

			// Convert to interface for comparison
			actualValue := fieldValue.ToInterface()
			assert.Equal(t, test.expected, actualValue, "Field %s should have expected value", test.field)
		})
	}

	t.Logf("✅ Successfully verified field mapping for ObservationEvent")
}

// Helper functions
func intPtr(i int32) *int32 {
	return &i
}

func stringPtr(s string) *string {
	return &s
}
