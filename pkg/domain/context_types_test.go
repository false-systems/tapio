package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSimplePredictionContext_GetEventType(t *testing.T) {
	tests := []struct {
		name     string
		context  *SimplePredictionContext
		expected string
	}{
		{
			name: "normal_event_type",
			context: &SimplePredictionContext{
				EventType: "pod_created",
			},
			expected: "pod_created",
		},
		{
			name: "empty_event_type",
			context: &SimplePredictionContext{
				EventType: "",
			},
			expected: "",
		},
		{
			name: "special_characters",
			context: &SimplePredictionContext{
				EventType: "event-type.with_special/chars",
			},
			expected: "event-type.with_special/chars",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.context.GetEventType()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSimplePredictionContext_GetSource(t *testing.T) {
	tests := []struct {
		name     string
		context  *SimplePredictionContext
		expected string
	}{
		{
			name: "kernel_source",
			context: &SimplePredictionContext{
				Source: "kernel",
			},
			expected: "kernel",
		},
		{
			name: "kubeapi_source",
			context: &SimplePredictionContext{
				Source: "kubeapi",
			},
			expected: "kubeapi",
		},
		{
			name: "empty_source",
			context: &SimplePredictionContext{
				Source: "",
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.context.GetSource()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSimplePredictionContext_GetEventID(t *testing.T) {
	tests := []struct {
		name     string
		context  *SimplePredictionContext
		expected string
	}{
		{
			name: "uuid_event_id",
			context: &SimplePredictionContext{
				EventID: "550e8400-e29b-41d4-a716-446655440000",
			},
			expected: "550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name: "simple_event_id",
			context: &SimplePredictionContext{
				EventID: "event-123",
			},
			expected: "event-123",
		},
		{
			name: "empty_event_id",
			context: &SimplePredictionContext{
				EventID: "",
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.context.GetEventID()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSimplePredictionContext_ToMap(t *testing.T) {
	tests := []struct {
		name     string
		context  *SimplePredictionContext
		expected map[string]string
	}{
		{
			name: "basic_context_without_event_id",
			context: &SimplePredictionContext{
				EventType: "pod_created",
				Source:    "kubeapi",
				EventID:   "",
			},
			expected: map[string]string{
				"event_type": "pod_created",
				"source":     "kubeapi",
			},
		},
		{
			name: "context_with_event_id",
			context: &SimplePredictionContext{
				EventType: "container_started",
				Source:    "kernel",
				EventID:   "evt-001",
			},
			expected: map[string]string{
				"event_type": "container_started",
				"source":     "kernel",
				"event_id":   "evt-001",
			},
		},
		{
			name: "context_with_data",
			context: &SimplePredictionContext{
				EventType: "dns_query",
				Source:    "dns",
				EventID:   "dns-123",
				Data: map[string]string{
					"query":    "example.com",
					"response": "192.168.1.1",
					"ttl":      "300",
				},
			},
			expected: map[string]string{
				"event_type": "dns_query",
				"source":     "dns",
				"event_id":   "dns-123",
				"query":      "example.com",
				"response":   "192.168.1.1",
				"ttl":        "300",
			},
		},
		{
			name: "context_with_nil_data",
			context: &SimplePredictionContext{
				EventType: "process_exec",
				Source:    "kernel",
				EventID:   "proc-456",
				Data:      nil,
			},
			expected: map[string]string{
				"event_type": "process_exec",
				"source":     "kernel",
				"event_id":   "proc-456",
			},
		},
		{
			name: "empty_context",
			context: &SimplePredictionContext{
				EventType: "",
				Source:    "",
				EventID:   "",
				Data:      nil,
			},
			expected: map[string]string{
				"event_type": "",
				"source":     "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.context.ToMap()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewSimplePredictionContext(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		source    string
		eventID   string
		validate  func(t *testing.T, ctx *SimplePredictionContext)
	}{
		{
			name:      "create_basic_context",
			eventType: "pod_created",
			source:    "kubeapi",
			eventID:   "pod-123",
			validate: func(t *testing.T, ctx *SimplePredictionContext) {
				assert.Equal(t, "pod_created", ctx.EventType)
				assert.Equal(t, "kubeapi", ctx.Source)
				assert.Equal(t, "pod-123", ctx.EventID)
				assert.NotNil(t, ctx.Data)
				assert.Empty(t, ctx.Data)
			},
		},
		{
			name:      "create_with_empty_values",
			eventType: "",
			source:    "",
			eventID:   "",
			validate: func(t *testing.T, ctx *SimplePredictionContext) {
				assert.Equal(t, "", ctx.EventType)
				assert.Equal(t, "", ctx.Source)
				assert.Equal(t, "", ctx.EventID)
				assert.NotNil(t, ctx.Data)
			},
		},
		{
			name:      "create_with_special_characters",
			eventType: "event.type-with_special/chars",
			source:    "source@123!",
			eventID:   "id#456$",
			validate: func(t *testing.T, ctx *SimplePredictionContext) {
				assert.Equal(t, "event.type-with_special/chars", ctx.EventType)
				assert.Equal(t, "source@123!", ctx.Source)
				assert.Equal(t, "id#456$", ctx.EventID)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := NewSimplePredictionContext(tt.eventType, tt.source, tt.eventID)
			assert.NotNil(t, ctx)
			tt.validate(t, ctx)
		})
	}
}

func TestSimplePredictionContext_AddData(t *testing.T) {
	tests := []struct {
		name     string
		initial  *SimplePredictionContext
		adds     []struct{ key, value string }
		expected map[string]string
	}{
		{
			name: "add_to_empty_data",
			initial: &SimplePredictionContext{
				EventType: "test",
				Source:    "test",
				Data:      nil,
			},
			adds: []struct{ key, value string }{
				{"key1", "value1"},
				{"key2", "value2"},
			},
			expected: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
		},
		{
			name: "add_to_existing_data",
			initial: &SimplePredictionContext{
				EventType: "test",
				Source:    "test",
				Data: map[string]string{
					"existing": "data",
				},
			},
			adds: []struct{ key, value string }{
				{"new", "value"},
			},
			expected: map[string]string{
				"existing": "data",
				"new":      "value",
			},
		},
		{
			name: "overwrite_existing_key",
			initial: &SimplePredictionContext{
				EventType: "test",
				Source:    "test",
				Data: map[string]string{
					"key": "old_value",
				},
			},
			adds: []struct{ key, value string }{
				{"key", "new_value"},
			},
			expected: map[string]string{
				"key": "new_value",
			},
		},
		{
			name: "add_empty_values",
			initial: &SimplePredictionContext{
				EventType: "test",
				Source:    "test",
				Data:      nil,
			},
			adds: []struct{ key, value string }{
				{"", "value"},
				{"key", ""},
				{"", ""}, // This will overwrite the first empty key
			},
			expected: map[string]string{
				"":    "", // Last value for empty key wins
				"key": "",
			},
		},
		{
			name: "add_special_characters",
			initial: &SimplePredictionContext{
				EventType: "test",
				Source:    "test",
				Data:      nil,
			},
			adds: []struct{ key, value string }{
				{"key.with-special_chars", "value@123!"},
				{"🔑", "🎯"},
			},
			expected: map[string]string{
				"key.with-special_chars": "value@123!",
				"🔑":                      "🎯",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.initial
			for _, add := range tt.adds {
				ctx.AddData(add.key, add.value)
			}
			assert.Equal(t, tt.expected, ctx.Data)
		})
	}
}

func TestSimplePredictionContext_InterfaceCompliance(t *testing.T) {
	// Verify that SimplePredictionContext implements PredictionContext interface
	var _ PredictionContext = (*SimplePredictionContext)(nil)

	// Test interface methods work correctly
	ctx := NewSimplePredictionContext("event_type", "source", "event_id")
	ctx.AddData("key", "value")

	var iface PredictionContext = ctx

	assert.Equal(t, "event_type", iface.GetEventType())
	assert.Equal(t, "source", iface.GetSource())
	assert.Equal(t, "event_id", iface.GetEventID())

	mapResult := iface.ToMap()
	assert.Equal(t, "event_type", mapResult["event_type"])
	assert.Equal(t, "source", mapResult["source"])
	assert.Equal(t, "event_id", mapResult["event_id"])
	assert.Equal(t, "value", mapResult["key"])
}

func TestCausalityContext(t *testing.T) {
	// Test CausalityContext struct initialization and field access
	tests := []struct {
		name     string
		context  CausalityContext
		validate func(t *testing.T, ctx CausalityContext)
	}{
		{
			name: "basic_causality_context",
			context: CausalityContext{
				CauseID:    "cause-001",
				EffectIDs:  []string{"effect-001", "effect-002"},
				ChainID:    "chain-001",
				ChainDepth: 3,
				RootCause:  "root-001",
				Confidence: 0.95,
				Type:       "direct",
			},
			validate: func(t *testing.T, ctx CausalityContext) {
				assert.Equal(t, "cause-001", ctx.CauseID)
				assert.Equal(t, []string{"effect-001", "effect-002"}, ctx.EffectIDs)
				assert.Equal(t, "chain-001", ctx.ChainID)
				assert.Equal(t, 3, ctx.ChainDepth)
				assert.Equal(t, "root-001", ctx.RootCause)
				assert.Equal(t, 0.95, ctx.Confidence)
				assert.Equal(t, "direct", ctx.Type)
			},
		},
		{
			name:    "empty_causality_context",
			context: CausalityContext{},
			validate: func(t *testing.T, ctx CausalityContext) {
				assert.Equal(t, "", ctx.CauseID)
				assert.Nil(t, ctx.EffectIDs)
				assert.Equal(t, "", ctx.ChainID)
				assert.Equal(t, 0, ctx.ChainDepth)
				assert.Equal(t, "", ctx.RootCause)
				assert.Equal(t, 0.0, ctx.Confidence)
				assert.Equal(t, "", ctx.Type)
			},
		},
		{
			name: "complex_causality_chain",
			context: CausalityContext{
				CauseID:       "cause-100",
				EffectIDs:     []string{"effect-101", "effect-102", "effect-103"},
				ChainID:       "chain-complex",
				ChainDepth:    10,
				RootCause:     "root-100",
				RelatedEvents: []string{"related-001", "related-002"},
				Confidence:    0.75,
				Type:          "indirect",
			},
			validate: func(t *testing.T, ctx CausalityContext) {
				assert.Equal(t, "cause-100", ctx.CauseID)
				assert.Len(t, ctx.EffectIDs, 3)
				assert.Equal(t, "chain-complex", ctx.ChainID)
				assert.Equal(t, 10, ctx.ChainDepth)
				assert.Equal(t, "root-100", ctx.RootCause)
				assert.Len(t, ctx.RelatedEvents, 2)
				assert.Equal(t, 0.75, ctx.Confidence)
				assert.Equal(t, "indirect", ctx.Type)
			},
		},
		{
			name: "confidence_boundaries",
			context: CausalityContext{
				Confidence: 1.0, // Maximum confidence
			},
			validate: func(t *testing.T, ctx CausalityContext) {
				assert.Equal(t, 1.0, ctx.Confidence)
			},
		},
		{
			name: "negative_chain_depth",
			context: CausalityContext{
				ChainDepth: -1, // Edge case
			},
			validate: func(t *testing.T, ctx CausalityContext) {
				assert.Equal(t, -1, ctx.ChainDepth)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.validate(t, tt.context)
		})
	}
}

func BenchmarkSimplePredictionContext_ToMap(b *testing.B) {
	ctx := &SimplePredictionContext{
		EventType: "benchmark_event",
		Source:    "benchmark_source",
		EventID:   "bench-001",
		Data: map[string]string{
			"key1": "value1",
			"key2": "value2",
			"key3": "value3",
			"key4": "value4",
			"key5": "value5",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ctx.ToMap()
	}
}

func BenchmarkSimplePredictionContext_AddData(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		ctx := &SimplePredictionContext{
			EventType: "benchmark_event",
			Source:    "benchmark_source",
			EventID:   "bench-001",
		}
		b.StartTimer()

		for j := 0; j < 10; j++ {
			ctx.AddData("key", "value")
		}
	}
}

func BenchmarkNewSimplePredictionContext(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewSimplePredictionContext("event_type", "source", "event_id")
	}
}
