package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateEventID(t *testing.T) {
	t.Run("generates unique IDs", func(t *testing.T) {
		id1 := GenerateEventID()
		id2 := GenerateEventID()

		assert.NotEmpty(t, id1)
		assert.NotEmpty(t, id2)
		assert.NotEqual(t, id1, id2)
	})

	t.Run("generates hex-encoded 32-character strings", func(t *testing.T) {
		id := GenerateEventID()

		// Should be hex-encoded 16 bytes = 32 characters
		assert.Len(t, id, 32)

		// Should only contain hex characters
		for _, char := range id {
			assert.True(t, (char >= '0' && char <= '9') || (char >= 'a' && char <= 'f'),
				"ID should only contain hex characters, got: %c", char)
		}
	})
}

func TestUnifiedEvent_HasTraceContext(t *testing.T) {
	tests := []struct {
		name     string
		event    *UnifiedEvent
		expected bool
	}{
		{
			name:     "nil trace context",
			event:    &UnifiedEvent{},
			expected: false,
		},
		{
			name: "empty trace ID",
			event: &UnifiedEvent{
				TraceContext: &TraceContext{TraceID: ""},
			},
			expected: false,
		},
		{
			name: "valid trace context",
			event: &UnifiedEvent{
				TraceContext: &TraceContext{TraceID: "trace123"},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.event.HasTraceContext())
		})
	}
}

func TestUnifiedEvent_GetSeverity(t *testing.T) {
	tests := []struct {
		name     string
		event    *UnifiedEvent
		expected string
	}{
		{
			name:     "default severity",
			event:    &UnifiedEvent{},
			expected: "info",
		},
		{
			name: "impact severity",
			event: &UnifiedEvent{
				Impact: &ImpactContext{Severity: "critical"},
			},
			expected: "critical",
		},
		{
			name: "application level",
			event: &UnifiedEvent{
				Application: &ApplicationData{Level: "error"},
			},
			expected: "error",
		},
		{
			name: "kubernetes warning",
			event: &UnifiedEvent{
				Kubernetes: &KubernetesData{EventType: "Warning"},
			},
			expected: "warning",
		},
		{
			name: "impact overrides application",
			event: &UnifiedEvent{
				Impact:      &ImpactContext{Severity: "high"},
				Application: &ApplicationData{Level: "error"},
			},
			expected: "high",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.event.GetSeverity())
		})
	}
}

func TestUnifiedEvent_GetEntityID(t *testing.T) {
	tests := []struct {
		name     string
		event    *UnifiedEvent
		expected string
	}{
		{
			name:     "nil entity",
			event:    &UnifiedEvent{},
			expected: "",
		},
		{
			name: "with UID",
			event: &UnifiedEvent{
				Entity: &EntityContext{
					UID:       "uid-123",
					Name:      "test-pod",
					Namespace: "default",
				},
			},
			expected: "uid-123",
		},
		{
			name: "with namespace and name",
			event: &UnifiedEvent{
				Entity: &EntityContext{
					Name:      "test-pod",
					Namespace: "default",
				},
			},
			expected: "default/test-pod",
		},
		{
			name: "name only",
			event: &UnifiedEvent{
				Entity: &EntityContext{
					Name: "test-pod",
				},
			},
			expected: "test-pod",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.event.GetEntityID())
		})
	}
}

func TestUnifiedEvent_EventTypeCheckers(t *testing.T) {
	tests := []struct {
		name    string
		event   *UnifiedEvent
		kernel  bool
		network bool
		app     bool
		k8s     bool
		metrics bool
	}{
		{
			name:  "empty event",
			event: &UnifiedEvent{},
		},
		{
			name: "kernel event",
			event: &UnifiedEvent{
				Kernel: &KernelData{Syscall: "open"},
			},
			kernel: true,
		},
		{
			name: "network event",
			event: &UnifiedEvent{
				Network: &NetworkData{Protocol: "TCP"},
			},
			network: true,
		},
		{
			name: "application event",
			event: &UnifiedEvent{
				Application: &ApplicationData{Level: "error"},
			},
			app: true,
		},
		{
			name: "kubernetes event",
			event: &UnifiedEvent{
				Kubernetes: &KubernetesData{Reason: "BackOff"},
			},
			k8s: true,
		},
		{
			name: "metrics event",
			event: &UnifiedEvent{
				Metrics: &MetricsData{MetricName: "cpu.usage"},
			},
			metrics: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.kernel, tt.event.IsKernelEvent())
			assert.Equal(t, tt.network, tt.event.IsNetworkEvent())
			assert.Equal(t, tt.app, tt.event.IsApplicationEvent())
			assert.Equal(t, tt.k8s, tt.event.IsKubernetesEvent())
			assert.Equal(t, tt.metrics, tt.event.IsMetricEvent())
		})
	}
}

func TestUnifiedEvent_GetSemanticIntent(t *testing.T) {
	tests := []struct {
		name     string
		event    *UnifiedEvent
		expected string
	}{
		{
			name:     "nil semantic",
			event:    &UnifiedEvent{},
			expected: "",
		},
		{
			name: "with semantic intent",
			event: &UnifiedEvent{
				Semantic: &SemanticContext{Intent: "user-login"},
			},
			expected: "user-login",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.event.GetSemanticIntent())
		})
	}
}

func TestUnifiedEventBuilder(t *testing.T) {
	t.Run("basic builder", func(t *testing.T) {
		event := NewUnifiedEvent().
			WithType(EventTypeProcess).
			WithSource("test-collector").
			Build()

		assert.NotEmpty(t, event.ID)
		assert.Equal(t, EventTypeProcess, event.Type)
		assert.Equal(t, "test-collector", event.Source)
		assert.WithinDuration(t, time.Now(), event.Timestamp, time.Second)
	})

	t.Run("with trace context", func(t *testing.T) {
		event := NewUnifiedEvent().
			WithTraceContext("trace123", "span456").
			Build()

		require.NotNil(t, event.TraceContext)
		assert.Equal(t, "trace123", event.TraceContext.TraceID)
		assert.Equal(t, "span456", event.TraceContext.SpanID)
		assert.True(t, event.TraceContext.Sampled)
		assert.True(t, event.HasTraceContext())
	})

	t.Run("with semantic context", func(t *testing.T) {
		event := NewUnifiedEvent().
			WithSemantic("user-login", "security", "auth", "critical").
			Build()

		require.NotNil(t, event.Semantic)
		assert.Equal(t, "user-login", event.Semantic.Intent)
		assert.Equal(t, "security", event.Semantic.Category)
		assert.Equal(t, []string{"auth", "critical"}, event.Semantic.Tags)
		assert.Equal(t, 1.0, event.Semantic.Confidence)
		assert.Equal(t, "user-login", event.GetSemanticIntent())
	})

	t.Run("with entity context", func(t *testing.T) {
		event := NewUnifiedEvent().
			WithEntity("pod", "test-pod", "default").
			Build()

		require.NotNil(t, event.Entity)
		assert.Equal(t, "pod", event.Entity.Type)
		assert.Equal(t, "test-pod", event.Entity.Name)
		assert.Equal(t, "default", event.Entity.Namespace)
		assert.Equal(t, "default/test-pod", event.GetEntityID())
	})

	t.Run("with kernel data", func(t *testing.T) {
		event := NewUnifiedEvent().
			WithKernelData("open", 1234).
			Build()

		require.NotNil(t, event.Kernel)
		assert.Equal(t, "open", event.Kernel.Syscall)
		assert.Equal(t, uint32(1234), event.Kernel.PID)
		assert.True(t, event.IsKernelEvent())
	})

	t.Run("with network data", func(t *testing.T) {
		event := NewUnifiedEvent().
			WithNetworkData("TCP", "192.168.1.1", 80, "10.0.0.1", 8080).
			Build()

		require.NotNil(t, event.Network)
		assert.Equal(t, "TCP", event.Network.Protocol)
		assert.Equal(t, "192.168.1.1", event.Network.SourceIP)
		assert.Equal(t, uint16(80), event.Network.SourcePort)
		assert.Equal(t, "10.0.0.1", event.Network.DestIP)
		assert.Equal(t, uint16(8080), event.Network.DestPort)
		assert.True(t, event.IsNetworkEvent())
	})

	t.Run("with application data", func(t *testing.T) {
		event := NewUnifiedEvent().
			WithApplicationData("error", "Database connection failed").
			Build()

		require.NotNil(t, event.Application)
		assert.Equal(t, "error", event.Application.Level)
		assert.Equal(t, "Database connection failed", event.Application.Message)
		assert.True(t, event.IsApplicationEvent())
		assert.Equal(t, "error", event.GetSeverity())
	})

	t.Run("with impact context", func(t *testing.T) {
		event := NewUnifiedEvent().
			WithImpact("critical", 0.9).
			Build()

		require.NotNil(t, event.Impact)
		assert.Equal(t, "critical", event.Impact.Severity)
		assert.Equal(t, 0.9, event.Impact.InfrastructureImpact)
		assert.Equal(t, "critical", event.GetSeverity())
	})

	t.Run("complex event", func(t *testing.T) {
		event := NewUnifiedEvent().
			WithType(EventTypeSystem).
			WithSource("ebpf-collector").
			WithTraceContext("trace123", "span456").
			WithSemantic("oom-kill", "reliability", "memory", "critical").
			WithEntity("pod", "app-pod", "production").
			WithKernelData("kill", 9876).
			WithImpact("high", 0.8).
			Build()

		// Verify all components are set
		assert.NotEmpty(t, event.ID)
		assert.Equal(t, EventTypeSystem, event.Type)
		assert.Equal(t, "ebpf-collector", event.Source)

		require.NotNil(t, event.TraceContext)
		assert.True(t, event.HasTraceContext())

		require.NotNil(t, event.Semantic)
		assert.Equal(t, "oom-kill", event.GetSemanticIntent())

		require.NotNil(t, event.Entity)
		assert.Equal(t, "production/app-pod", event.GetEntityID())

		require.NotNil(t, event.Kernel)
		assert.True(t, event.IsKernelEvent())

		require.NotNil(t, event.Impact)
		assert.Equal(t, "high", event.GetSeverity())
	})
}

func TestUnifiedEventBuilder_Chaining(t *testing.T) {
	// Test that builder methods can be chained in any order
	builder := NewUnifiedEvent()

	// Chain methods in different orders
	event1 := builder.
		WithType(EventTypeNetwork).
		WithSource("network-tap").
		WithSemantic("http-request", "traffic").
		Build()

	event2 := NewUnifiedEvent().
		WithSemantic("database-query", "performance").
		WithType(EventTypeLog).
		WithSource("app-tracer").
		Build()

	assert.Equal(t, EventTypeNetwork, event1.Type)
	assert.Equal(t, "network-tap", event1.Source)
	assert.Equal(t, "http-request", event1.GetSemanticIntent())

	assert.Equal(t, EventTypeLog, event2.Type)
	assert.Equal(t, "app-tracer", event2.Source)
	assert.Equal(t, "database-query", event2.GetSemanticIntent())
}

// Test struct completeness
func TestUnifiedEvent_StructCompleteness(t *testing.T) {
	// Ensure we can create events with all possible data types
	event := &UnifiedEvent{
		ID:        "test-id",
		Timestamp: time.Now(),
		Type:      EventTypeProcess,
		Source:    "test-source",

		TraceContext: &TraceContext{
			TraceID:      "trace123",
			SpanID:       "span456",
			ParentSpanID: "parent789",
			TraceState:   "state",
			Baggage:      map[string]string{"key": "value"},
			Sampled:      true,
		},

		Semantic: &SemanticContext{
			Intent:     "test-intent",
			Category:   "test-category",
			Tags:       []string{"tag1", "tag2"},
			Narrative:  "test narrative",
			Confidence: 0.95,
		},

		Entity: &EntityContext{
			Type:       "pod",
			Name:       "test-pod",
			Namespace:  "default",
			UID:        "uid123",
			Labels:     map[string]string{"app": "test"},
			Attributes: map[string]string{"env": "prod"},
		},

		Kernel: &KernelData{
			Syscall:    "open",
			PID:        1234,
			TID:        5678,
			UID:        1000,
			GID:        1000,
			Comm:       "test-process",
			ReturnCode: 0,
			Args:       map[string]string{"file": "/tmp/test"},
			StackTrace: []string{"func1", "func2"},
		},

		Network: &NetworkData{
			Protocol:   "TCP",
			SourceIP:   "192.168.1.1",
			SourcePort: 80,
			DestIP:     "10.0.0.1",
			DestPort:   8080,
			Direction:  "ingress",
			BytesSent:  1024,
			BytesRecv:  2048,
			Latency:    1000000,
			StatusCode: 200,
			Method:     "GET",
			Path:       "/api/test",
			Headers:    map[string]string{"Content-Type": "application/json"},
		},

		Application: &ApplicationData{
			Level:      "error",
			Message:    "test error",
			Logger:     "test-logger",
			ErrorType:  "RuntimeError",
			StackTrace: "stack trace",
			UserID:     "user123",
			SessionID:  "session456",
			RequestID:  "req789",
			Custom: &ApplicationCustomData{
				HTTPMethod: "POST",
				Tags:       []string{"test"},
				Payload:    map[string]interface{}{"custom_field": "value"},
			},
		},

		Kubernetes: &KubernetesData{
			EventType:       "Warning",
			Reason:          "BackOff",
			Object:          "pod/test-pod",
			ObjectKind:      "Pod",
			Message:         "Back-off restarting failed container",
			Action:          "MODIFIED",
			APIVersion:      "v1",
			ResourceVersion: "12345",
			Labels:          map[string]string{"app": "test"},
			Annotations:     map[string]string{"annotation": "value"},
		},

		Metrics: &MetricsData{
			MetricName:  "cpu.usage",
			Value:       0.75,
			Unit:        "percent",
			Labels:      map[string]string{"host": "server1"},
			Aggregation: "avg",
			Period:      60000,
		},

		Impact: &ImpactContext{
			Severity:             "high",
			InfrastructureImpact: 0.8,
			AffectedServices:     []string{"service1", "service2"},
			AffectedComponents:   100,
			SLOImpact:            true,
			SystemCritical:       true,
			CascadeRisk:          false,
		},

		Correlation: &CorrelationContext{
			CorrelationID: "corr123",
			GroupID:       "group456",
			ParentEventID: "parent789",
			CausalChain:   []string{"event1", "event2", "event3"},
			RelatedEvents: []string{"related1", "related2"},
			Pattern:       "cascade-failure",
			Stage:         "propagation",
		},

		RawData: []byte("raw event data"),
	}

	// Test all helper methods work with fully populated event
	assert.True(t, event.HasTraceContext())
	assert.Equal(t, "high", event.GetSeverity())
	assert.Equal(t, "uid123", event.GetEntityID())
	assert.True(t, event.IsKernelEvent())
	assert.True(t, event.IsNetworkEvent())
	assert.True(t, event.IsApplicationEvent())
	assert.True(t, event.IsKubernetesEvent())
	assert.True(t, event.IsMetricEvent())
	assert.Equal(t, "test-intent", event.GetSemanticIntent())
}
