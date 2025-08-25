package intelligence

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewEventContextFromEvent tests event context creation
func TestNewEventContextFromEvent(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		source    string
		eventID   string
	}{
		{
			name:      "basic event context",
			eventType: "pod.created",
			source:    "kubernetes",
			eventID:   "evt-123",
		},
		{
			name:      "empty event ID",
			eventType: "deployment.updated",
			source:    "k8s-api",
			eventID:   "",
		},
		{
			name:      "special characters",
			eventType: "service/endpoint-update",
			source:    "istio-proxy",
			eventID:   "evt-456-xyz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := NewEventContextFromEvent(tt.eventType, tt.source, tt.eventID)

			require.NotNil(t, ctx)
			assert.Equal(t, tt.eventType, ctx.EventType)
			assert.Equal(t, tt.source, ctx.Source)
			assert.Equal(t, tt.eventID, ctx.EventID)
			assert.NotNil(t, ctx.CustomData)
			assert.Empty(t, ctx.CustomData)
		})
	}
}

// TestEventContextSetters tests all setter methods
func TestEventContextSetters(t *testing.T) {
	t.Run("SetPID", func(t *testing.T) {
		ctx := NewEventContextFromEvent("test", "source", "id")
		pid := uint32(12345)
		result := ctx.SetPID(pid)

		assert.Equal(t, ctx, result) // Check fluent interface
		require.NotNil(t, ctx.PID)
		assert.Equal(t, pid, *ctx.PID)
	})

	t.Run("SetContainerID", func(t *testing.T) {
		ctx := NewEventContextFromEvent("test", "source", "id")
		containerID := "docker://abc123def456"
		result := ctx.SetContainerID(containerID)

		assert.Equal(t, ctx, result)
		require.NotNil(t, ctx.ContainerID)
		assert.Equal(t, containerID, *ctx.ContainerID)
	})

	t.Run("SetPodName", func(t *testing.T) {
		ctx := NewEventContextFromEvent("test", "source", "id")
		podName := "nginx-deployment-76bf4969df-8kjht"
		result := ctx.SetPodName(podName)

		assert.Equal(t, ctx, result)
		require.NotNil(t, ctx.PodName)
		assert.Equal(t, podName, *ctx.PodName)
	})

	t.Run("SetNamespace", func(t *testing.T) {
		ctx := NewEventContextFromEvent("test", "source", "id")
		namespace := "production"
		result := ctx.SetNamespace(namespace)

		assert.Equal(t, ctx, result)
		require.NotNil(t, ctx.Namespace)
		assert.Equal(t, namespace, *ctx.Namespace)
	})

	t.Run("SetServiceName", func(t *testing.T) {
		ctx := NewEventContextFromEvent("test", "source", "id")
		serviceName := "api-gateway"
		result := ctx.SetServiceName(serviceName)

		assert.Equal(t, ctx, result)
		require.NotNil(t, ctx.ServiceName)
		assert.Equal(t, serviceName, *ctx.ServiceName)
	})

	t.Run("SetNodeName", func(t *testing.T) {
		ctx := NewEventContextFromEvent("test", "source", "id")
		nodeName := "node-worker-01"
		result := ctx.SetNodeName(nodeName)

		assert.Equal(t, ctx, result)
		require.NotNil(t, ctx.NodeName)
		assert.Equal(t, nodeName, *ctx.NodeName)
	})

	t.Run("SetAction", func(t *testing.T) {
		ctx := NewEventContextFromEvent("test", "source", "id")
		action := "create"
		result := ctx.SetAction(action)

		assert.Equal(t, ctx, result)
		require.NotNil(t, ctx.Action)
		assert.Equal(t, action, *ctx.Action)
	})

	t.Run("SetTarget", func(t *testing.T) {
		ctx := NewEventContextFromEvent("test", "source", "id")
		target := "/api/v1/users"
		result := ctx.SetTarget(target)

		assert.Equal(t, ctx, result)
		require.NotNil(t, ctx.Target)
		assert.Equal(t, target, *ctx.Target)
	})

	t.Run("SetResult", func(t *testing.T) {
		ctx := NewEventContextFromEvent("test", "source", "id")
		result := "success"
		res := ctx.SetResult(result)

		assert.Equal(t, ctx, res)
		require.NotNil(t, ctx.Result)
		assert.Equal(t, result, *ctx.Result)
	})

	t.Run("SetReason", func(t *testing.T) {
		ctx := NewEventContextFromEvent("test", "source", "id")
		reason := "resource quota exceeded"
		result := ctx.SetReason(reason)

		assert.Equal(t, ctx, result)
		require.NotNil(t, ctx.Reason)
		assert.Equal(t, reason, *ctx.Reason)
	})

	t.Run("SetDuration", func(t *testing.T) {
		ctx := NewEventContextFromEvent("test", "source", "id")
		duration := uint64(1500)
		result := ctx.SetDuration(duration)

		assert.Equal(t, ctx, result)
		require.NotNil(t, ctx.DurationMS)
		assert.Equal(t, duration, *ctx.DurationMS)
	})

	t.Run("SetSize", func(t *testing.T) {
		ctx := NewEventContextFromEvent("test", "source", "id")
		size := uint64(1024 * 1024)
		result := ctx.SetSize(size)

		assert.Equal(t, ctx, result)
		require.NotNil(t, ctx.SizeBytes)
		assert.Equal(t, size, *ctx.SizeBytes)
	})

	t.Run("SetCount", func(t *testing.T) {
		ctx := NewEventContextFromEvent("test", "source", "id")
		count := uint64(42)
		result := ctx.SetCount(count)

		assert.Equal(t, ctx, result)
		require.NotNil(t, ctx.Count)
		assert.Equal(t, count, *ctx.Count)
	})

	t.Run("SetCausedBy", func(t *testing.T) {
		ctx := NewEventContextFromEvent("test", "source", "id")
		causedBy := "parent-event-123"
		result := ctx.SetCausedBy(causedBy)

		assert.Equal(t, ctx, result)
		require.NotNil(t, ctx.CausedBy)
		assert.Equal(t, causedBy, *ctx.CausedBy)
	})

	t.Run("SetParentID", func(t *testing.T) {
		ctx := NewEventContextFromEvent("test", "source", "id")
		parentID := "parent-456"
		result := ctx.SetParentID(parentID)

		assert.Equal(t, ctx, result)
		require.NotNil(t, ctx.ParentID)
		assert.Equal(t, parentID, *ctx.ParentID)
	})
}

// TestAddCustomData tests custom data handling
func TestAddCustomData(t *testing.T) {
	t.Run("add to empty custom data", func(t *testing.T) {
		ctx := NewEventContextFromEvent("test", "source", "id")
		result := ctx.AddCustomData("key1", "value1")

		assert.Equal(t, ctx, result)
		assert.NotNil(t, ctx.CustomData)
		assert.Equal(t, "value1", ctx.CustomData["key1"])
	})

	t.Run("add multiple custom data", func(t *testing.T) {
		ctx := NewEventContextFromEvent("test", "source", "id")
		ctx.AddCustomData("key1", "value1").
			AddCustomData("key2", "value2").
			AddCustomData("key3", "value3")

		assert.Len(t, ctx.CustomData, 3)
		assert.Equal(t, "value1", ctx.CustomData["key1"])
		assert.Equal(t, "value2", ctx.CustomData["key2"])
		assert.Equal(t, "value3", ctx.CustomData["key3"])
	})

	t.Run("overwrite existing custom data", func(t *testing.T) {
		ctx := NewEventContextFromEvent("test", "source", "id")
		ctx.AddCustomData("key1", "initial")
		ctx.AddCustomData("key1", "updated")

		assert.Equal(t, "updated", ctx.CustomData["key1"])
	})

	t.Run("nil custom data map initialization", func(t *testing.T) {
		ctx := &EventContext{
			EventType: "test",
			Source:    "source",
		}
		// Explicitly set CustomData to nil
		ctx.CustomData = nil

		result := ctx.AddCustomData("key", "value")

		assert.Equal(t, ctx, result)
		assert.NotNil(t, ctx.CustomData)
		assert.Equal(t, "value", ctx.CustomData["key"])
	})
}

// TestEventContextGetters tests getter methods
func TestEventContextGetters(t *testing.T) {
	ctx := NewEventContextFromEvent("pod.created", "kubernetes", "evt-789")

	t.Run("GetEventType", func(t *testing.T) {
		assert.Equal(t, "pod.created", ctx.GetEventType())
	})

	t.Run("GetSource", func(t *testing.T) {
		assert.Equal(t, "kubernetes", ctx.GetSource())
	})

	t.Run("GetEventID", func(t *testing.T) {
		assert.Equal(t, "evt-789", ctx.GetEventID())
	})
}

// TestEventContextToMap tests conversion to map
func TestEventContextToMap(t *testing.T) {
	t.Run("minimal context", func(t *testing.T) {
		ctx := NewEventContextFromEvent("test", "source", "")
		m := ctx.ToMap()

		assert.Equal(t, "test", m["event_type"])
		assert.Equal(t, "source", m["source"])
		_, hasEventID := m["event_id"]
		assert.False(t, hasEventID)
	})

	t.Run("full context", func(t *testing.T) {
		ctx := NewEventContextFromEvent("test", "source", "id-123")

		// Set all fields
		pid := uint32(999)
		containerID := "container-123"
		podName := "pod-123"
		namespace := "default"
		serviceName := "service-123"
		nodeName := "node-123"
		action := "create"
		target := "/api/test"
		result := "success"
		reason := "test reason"
		duration := uint64(100)
		size := uint64(2048)
		count := uint64(5)
		causedBy := "parent-123"
		parentID := "parent-456"

		ctx.SetPID(pid).
			SetContainerID(containerID).
			SetPodName(podName).
			SetNamespace(namespace).
			SetServiceName(serviceName).
			SetNodeName(nodeName).
			SetAction(action).
			SetTarget(target).
			SetResult(result).
			SetReason(reason).
			SetDuration(duration).
			SetSize(size).
			SetCount(count).
			SetCausedBy(causedBy).
			SetParentID(parentID).
			AddCustomData("custom1", "value1").
			AddCustomData("custom2", "value2")

		m := ctx.ToMap()

		// Verify all fields
		assert.Equal(t, "test", m["event_type"])
		assert.Equal(t, "source", m["source"])
		assert.Equal(t, "id-123", m["event_id"])
		assert.Equal(t, "999", m["pid"])
		assert.Equal(t, containerID, m["container_id"])
		assert.Equal(t, podName, m["pod_name"])
		assert.Equal(t, namespace, m["namespace"])
		assert.Equal(t, serviceName, m["service_name"])
		assert.Equal(t, nodeName, m["node_name"])
		assert.Equal(t, action, m["action"])
		assert.Equal(t, target, m["target"])
		assert.Equal(t, result, m["result"])
		assert.Equal(t, reason, m["reason"])
		assert.Equal(t, "100", m["duration_ms"])
		assert.Equal(t, "2048", m["size_bytes"])
		assert.Equal(t, "5", m["count"])
		assert.Equal(t, causedBy, m["caused_by"])
		assert.Equal(t, parentID, m["parent_id"])
		assert.Equal(t, "value1", m["custom1"])
		assert.Equal(t, "value2", m["custom2"])
	})

	t.Run("nil fields excluded", func(t *testing.T) {
		ctx := NewEventContextFromEvent("test", "source", "id")
		ctx.SetPID(123).SetContainerID("container")

		m := ctx.ToMap()

		// Should have basic fields plus the two we set
		assert.Contains(t, m, "event_type")
		assert.Contains(t, m, "source")
		assert.Contains(t, m, "event_id")
		assert.Contains(t, m, "pid")
		assert.Contains(t, m, "container_id")

		// Should NOT have fields we didn't set
		assert.NotContains(t, m, "pod_name")
		assert.NotContains(t, m, "namespace")
		assert.NotContains(t, m, "service_name")
	})
}

// TestHealthDetails tests health status functionality
func TestHealthDetails(t *testing.T) {
	t.Run("healthy status", func(t *testing.T) {
		h := &HealthDetails{
			PatternsLoaded:  10,
			CircuitBreaker:  "closed",
			QueueUsage:      0.5,
			ComponentStatus: "running",
			LastHealthCheck: time.Now(),
		}

		assert.True(t, h.IsHealthy())
	})

	t.Run("unhealthy - circuit breaker open", func(t *testing.T) {
		h := &HealthDetails{
			CircuitBreaker: "open",
			QueueUsage:     0.3,
		}

		assert.False(t, h.IsHealthy())
	})

	t.Run("unhealthy - queue full", func(t *testing.T) {
		h := &HealthDetails{
			CircuitBreaker: "closed",
			QueueUsage:     0.95,
		}

		assert.False(t, h.IsHealthy())
	})

	t.Run("unhealthy - both conditions", func(t *testing.T) {
		h := &HealthDetails{
			CircuitBreaker: "open",
			QueueUsage:     0.99,
		}

		assert.False(t, h.IsHealthy())
	})

	t.Run("edge case - queue at threshold", func(t *testing.T) {
		h := &HealthDetails{
			CircuitBreaker: "closed",
			QueueUsage:     0.89,
		}

		assert.True(t, h.IsHealthy())
	})
}

// TestConditionValueConstructors tests all typed constructors
func TestConditionValueConstructors(t *testing.T) {
	t.Run("NewStringConditionValue", func(t *testing.T) {
		cv := NewStringConditionValue("test string")

		assert.Equal(t, ConditionValueTypeString, cv.Type)
		require.NotNil(t, cv.StringValue)
		assert.Equal(t, "test string", *cv.StringValue)
		assert.Nil(t, cv.IntValue)
		assert.Nil(t, cv.FloatValue)
		assert.Nil(t, cv.BoolValue)
		assert.Nil(t, cv.ListValue)
	})

	t.Run("NewIntConditionValue", func(t *testing.T) {
		cv := NewIntConditionValue(42)

		assert.Equal(t, ConditionValueTypeInt, cv.Type)
		require.NotNil(t, cv.IntValue)
		assert.Equal(t, int64(42), *cv.IntValue)
		assert.Nil(t, cv.StringValue)
		assert.Nil(t, cv.FloatValue)
		assert.Nil(t, cv.BoolValue)
		assert.Nil(t, cv.ListValue)
	})

	t.Run("NewFloatConditionValue", func(t *testing.T) {
		cv := NewFloatConditionValue(3.14159)

		assert.Equal(t, ConditionValueTypeFloat, cv.Type)
		require.NotNil(t, cv.FloatValue)
		assert.Equal(t, 3.14159, *cv.FloatValue)
		assert.Nil(t, cv.StringValue)
		assert.Nil(t, cv.IntValue)
		assert.Nil(t, cv.BoolValue)
		assert.Nil(t, cv.ListValue)
	})

	t.Run("NewBoolConditionValue", func(t *testing.T) {
		cv := NewBoolConditionValue(true)

		assert.Equal(t, ConditionValueTypeBool, cv.Type)
		require.NotNil(t, cv.BoolValue)
		assert.True(t, *cv.BoolValue)
		assert.Nil(t, cv.StringValue)
		assert.Nil(t, cv.IntValue)
		assert.Nil(t, cv.FloatValue)
		assert.Nil(t, cv.ListValue)
	})

	t.Run("NewListConditionValue", func(t *testing.T) {
		list := []string{"item1", "item2", "item3"}
		cv := NewListConditionValue(list)

		assert.Equal(t, ConditionValueTypeList, cv.Type)
		assert.Equal(t, list, cv.ListValue)
		assert.Nil(t, cv.StringValue)
		assert.Nil(t, cv.IntValue)
		assert.Nil(t, cv.FloatValue)
		assert.Nil(t, cv.BoolValue)
	})

	t.Run("NewNilConditionValue", func(t *testing.T) {
		cv := NewNilConditionValue()

		assert.Equal(t, ConditionValueTypeNil, cv.Type)
		assert.Nil(t, cv.StringValue)
		assert.Nil(t, cv.IntValue)
		assert.Nil(t, cv.FloatValue)
		assert.Nil(t, cv.BoolValue)
		assert.Nil(t, cv.ListValue)
	})
}

// TestNewConditionValue tests the generic constructor (deprecated but still tested)
func TestNewConditionValue(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected *ConditionValue
	}{
		{
			name:  "string",
			input: "test",
			expected: &ConditionValue{
				Type:        ConditionValueTypeString,
				StringValue: strPtr("test"),
			},
		},
		{
			name:  "int",
			input: 42,
			expected: &ConditionValue{
				Type:     ConditionValueTypeInt,
				IntValue: int64Ptr(42),
			},
		},
		{
			name:  "int32",
			input: int32(32),
			expected: &ConditionValue{
				Type:     ConditionValueTypeInt,
				IntValue: int64Ptr(32),
			},
		},
		{
			name:  "int64",
			input: int64(64),
			expected: &ConditionValue{
				Type:     ConditionValueTypeInt,
				IntValue: int64Ptr(64),
			},
		},
		{
			name:  "uint32",
			input: uint32(32),
			expected: &ConditionValue{
				Type:     ConditionValueTypeInt,
				IntValue: int64Ptr(32),
			},
		},
		{
			name:  "uint64",
			input: uint64(64),
			expected: &ConditionValue{
				Type:     ConditionValueTypeInt,
				IntValue: int64Ptr(64),
			},
		},
		{
			name:  "float32",
			input: float32(3.14),
			expected: &ConditionValue{
				Type:       ConditionValueTypeFloat,
				FloatValue: float64Ptr(float64(float32(3.14))),
			},
		},
		{
			name:  "float64",
			input: 3.14159,
			expected: &ConditionValue{
				Type:       ConditionValueTypeFloat,
				FloatValue: float64Ptr(3.14159),
			},
		},
		{
			name:  "bool",
			input: true,
			expected: &ConditionValue{
				Type:      ConditionValueTypeBool,
				BoolValue: boolPtr(true),
			},
		},
		{
			name:  "string slice",
			input: []string{"a", "b", "c"},
			expected: &ConditionValue{
				Type:      ConditionValueTypeList,
				ListValue: []string{"a", "b", "c"},
			},
		},
		{
			name:  "any slice",
			input: []any{"a", 1, true},
			expected: &ConditionValue{
				Type:      ConditionValueTypeList,
				ListValue: []string{"a", "1", "true"},
			},
		},
		{
			name:  "nil",
			input: nil,
			expected: &ConditionValue{
				Type: ConditionValueTypeNil,
			},
		},
		{
			name:  "unknown type falls back to string",
			input: struct{ Name string }{Name: "test"},
			expected: &ConditionValue{
				Type:        ConditionValueTypeString,
				StringValue: strPtr("{test}"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cv := NewConditionValue(tt.input)

			assert.Equal(t, tt.expected.Type, cv.Type)

			switch tt.expected.Type {
			case ConditionValueTypeString:
				assert.Equal(t, tt.expected.StringValue, cv.StringValue)
			case ConditionValueTypeInt:
				assert.Equal(t, tt.expected.IntValue, cv.IntValue)
			case ConditionValueTypeFloat:
				assert.InDelta(t, *tt.expected.FloatValue, *cv.FloatValue, 0.0001)
			case ConditionValueTypeBool:
				assert.Equal(t, tt.expected.BoolValue, cv.BoolValue)
			case ConditionValueTypeList:
				assert.Equal(t, tt.expected.ListValue, cv.ListValue)
			case ConditionValueTypeNil:
				assert.Nil(t, cv.StringValue)
				assert.Nil(t, cv.IntValue)
				assert.Nil(t, cv.FloatValue)
				assert.Nil(t, cv.BoolValue)
			}
		})
	}
}

// TestConditionValueToString tests string conversion
func TestConditionValueToString(t *testing.T) {
	tests := []struct {
		name     string
		cv       *ConditionValue
		expected string
	}{
		{
			name:     "string value",
			cv:       NewStringConditionValue("hello"),
			expected: "hello",
		},
		{
			name:     "int value",
			cv:       NewIntConditionValue(42),
			expected: "42",
		},
		{
			name:     "float value",
			cv:       NewFloatConditionValue(3.14),
			expected: "3.14",
		},
		{
			name:     "bool true",
			cv:       NewBoolConditionValue(true),
			expected: "true",
		},
		{
			name:     "bool false",
			cv:       NewBoolConditionValue(false),
			expected: "false",
		},
		{
			name:     "list value",
			cv:       NewListConditionValue([]string{"a", "b", "c"}),
			expected: "[a b c]",
		},
		{
			name:     "nil value",
			cv:       NewNilConditionValue(),
			expected: "",
		},
		{
			name: "nil string pointer",
			cv: &ConditionValue{
				Type:        ConditionValueTypeString,
				StringValue: nil,
			},
			expected: "",
		},
		{
			name: "nil int pointer",
			cv: &ConditionValue{
				Type:     ConditionValueTypeInt,
				IntValue: nil,
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.cv.ToString())
		})
	}
}

// TestConditionValueToFloat64 tests float conversion
func TestConditionValueToFloat64(t *testing.T) {
	tests := []struct {
		name       string
		cv         *ConditionValue
		expected   float64
		expectedOk bool
	}{
		{
			name:       "float value",
			cv:         NewFloatConditionValue(3.14),
			expected:   3.14,
			expectedOk: true,
		},
		{
			name:       "int value",
			cv:         NewIntConditionValue(42),
			expected:   42.0,
			expectedOk: true,
		},
		{
			name:       "string parseable as float",
			cv:         NewStringConditionValue("3.14159"),
			expected:   3.14159,
			expectedOk: true,
		},
		{
			name:       "string parseable as int",
			cv:         NewStringConditionValue("42"),
			expected:   42.0,
			expectedOk: true,
		},
		{
			name:       "string not parseable",
			cv:         NewStringConditionValue("not a number"),
			expected:   0,
			expectedOk: false,
		},
		{
			name:       "bool value",
			cv:         NewBoolConditionValue(true),
			expected:   0,
			expectedOk: false,
		},
		{
			name:       "list value",
			cv:         NewListConditionValue([]string{"1", "2", "3"}),
			expected:   0,
			expectedOk: false,
		},
		{
			name:       "nil value",
			cv:         NewNilConditionValue(),
			expected:   0,
			expectedOk: false,
		},
		{
			name: "nil float pointer",
			cv: &ConditionValue{
				Type:       ConditionValueTypeFloat,
				FloatValue: nil,
			},
			expected:   0,
			expectedOk: false,
		},
		{
			name: "nil int pointer",
			cv: &ConditionValue{
				Type:     ConditionValueTypeInt,
				IntValue: nil,
			},
			expected:   0,
			expectedOk: false,
		},
		{
			name: "nil string pointer",
			cv: &ConditionValue{
				Type:        ConditionValueTypeString,
				StringValue: nil,
			},
			expected:   0,
			expectedOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, ok := tt.cv.ToFloat64()
			assert.Equal(t, tt.expectedOk, ok)
			if tt.expectedOk {
				assert.InDelta(t, tt.expected, val, 0.0001)
			}
		})
	}
}

// TestConditionValueIsNil tests nil checking
func TestConditionValueIsNil(t *testing.T) {
	tests := []struct {
		name     string
		cv       *ConditionValue
		expected bool
	}{
		{
			name:     "nil value",
			cv:       NewNilConditionValue(),
			expected: true,
		},
		{
			name:     "string value",
			cv:       NewStringConditionValue("test"),
			expected: false,
		},
		{
			name:     "int value",
			cv:       NewIntConditionValue(0),
			expected: false,
		},
		{
			name:     "empty string",
			cv:       NewStringConditionValue(""),
			expected: false,
		},
		{
			name:     "empty list",
			cv:       NewListConditionValue([]string{}),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.cv.IsNil())
		})
	}
}

// TestConditionValueEquals tests equality comparison
func TestConditionValueEquals(t *testing.T) {
	tests := []struct {
		name     string
		cv1      *ConditionValue
		cv2      *ConditionValue
		expected bool
	}{
		{
			name:     "equal strings",
			cv1:      NewStringConditionValue("test"),
			cv2:      NewStringConditionValue("test"),
			expected: true,
		},
		{
			name:     "different strings",
			cv1:      NewStringConditionValue("test1"),
			cv2:      NewStringConditionValue("test2"),
			expected: false,
		},
		{
			name:     "equal ints",
			cv1:      NewIntConditionValue(42),
			cv2:      NewIntConditionValue(42),
			expected: true,
		},
		{
			name:     "different ints",
			cv1:      NewIntConditionValue(42),
			cv2:      NewIntConditionValue(43),
			expected: false,
		},
		{
			name:     "equal floats",
			cv1:      NewFloatConditionValue(3.14),
			cv2:      NewFloatConditionValue(3.14),
			expected: true,
		},
		{
			name:     "different floats",
			cv1:      NewFloatConditionValue(3.14),
			cv2:      NewFloatConditionValue(2.71),
			expected: false,
		},
		{
			name:     "equal bools",
			cv1:      NewBoolConditionValue(true),
			cv2:      NewBoolConditionValue(true),
			expected: true,
		},
		{
			name:     "different bools",
			cv1:      NewBoolConditionValue(true),
			cv2:      NewBoolConditionValue(false),
			expected: false,
		},
		{
			name:     "equal lists",
			cv1:      NewListConditionValue([]string{"a", "b", "c"}),
			cv2:      NewListConditionValue([]string{"a", "b", "c"}),
			expected: true,
		},
		{
			name:     "different list lengths",
			cv1:      NewListConditionValue([]string{"a", "b"}),
			cv2:      NewListConditionValue([]string{"a", "b", "c"}),
			expected: false,
		},
		{
			name:     "different list values",
			cv1:      NewListConditionValue([]string{"a", "b", "c"}),
			cv2:      NewListConditionValue([]string{"a", "b", "d"}),
			expected: false,
		},
		{
			name:     "both nil",
			cv1:      NewNilConditionValue(),
			cv2:      NewNilConditionValue(),
			expected: true,
		},
		{
			name:     "different types",
			cv1:      NewStringConditionValue("42"),
			cv2:      NewIntConditionValue(42),
			expected: false,
		},
		{
			name: "nil string pointers",
			cv1: &ConditionValue{
				Type:        ConditionValueTypeString,
				StringValue: nil,
			},
			cv2: &ConditionValue{
				Type:        ConditionValueTypeString,
				StringValue: nil,
			},
			expected: false, // Both have nil pointers
		},
		{
			name: "one nil string pointer",
			cv1:  NewStringConditionValue("test"),
			cv2: &ConditionValue{
				Type:        ConditionValueTypeString,
				StringValue: nil,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.cv1.Equals(tt.cv2))
		})
	}
}

// TestConditionValueContains tests contains operation
func TestConditionValueContains(t *testing.T) {
	tests := []struct {
		name     string
		cv       *ConditionValue
		other    *ConditionValue
		expected bool
	}{
		{
			name:     "string contains substring",
			cv:       NewStringConditionValue("hello world"),
			other:    NewStringConditionValue("world"),
			expected: true,
		},
		{
			name:     "string does not contain",
			cv:       NewStringConditionValue("hello world"),
			other:    NewStringConditionValue("foo"),
			expected: false,
		},
		{
			name:     "list contains string",
			cv:       NewListConditionValue([]string{"apple", "banana", "cherry"}),
			other:    NewStringConditionValue("banana"),
			expected: true,
		},
		{
			name:     "list does not contain",
			cv:       NewListConditionValue([]string{"apple", "banana", "cherry"}),
			other:    NewStringConditionValue("grape"),
			expected: false,
		},
		{
			name:     "list contains int as string",
			cv:       NewListConditionValue([]string{"1", "2", "3"}),
			other:    NewIntConditionValue(2),
			expected: true,
		},
		{
			name:     "int type cannot contain",
			cv:       NewIntConditionValue(123),
			other:    NewIntConditionValue(2),
			expected: false,
		},
		{
			name:     "nil values",
			cv:       NewNilConditionValue(),
			other:    NewNilConditionValue(),
			expected: false,
		},
		{
			name: "nil string pointer",
			cv: &ConditionValue{
				Type:        ConditionValueTypeString,
				StringValue: nil,
			},
			other:    NewStringConditionValue("test"),
			expected: false,
		},
		{
			name: "both nil string pointers",
			cv: &ConditionValue{
				Type:        ConditionValueTypeString,
				StringValue: nil,
			},
			other: &ConditionValue{
				Type:        ConditionValueTypeString,
				StringValue: nil,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.cv.Contains(tt.other))
		})
	}
}

// TestToStringHelper tests the helper function
func TestToStringHelper(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{
			name:     "nil",
			input:    nil,
			expected: "",
		},
		{
			name:     "string",
			input:    "test",
			expected: "test",
		},
		{
			name:     "int",
			input:    42,
			expected: "42",
		},
		{
			name:     "float",
			input:    3.14,
			expected: "3.14",
		},
		{
			name:     "bool",
			input:    true,
			expected: "true",
		},
		{
			name:     "slice",
			input:    []string{"a", "b"},
			expected: "[a b]",
		},
		{
			name:     "struct",
			input:    struct{ Name string }{Name: "test"},
			expected: "{test}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, toString(tt.input))
		})
	}
}

// TestEventContextChaining tests fluent interface chaining
func TestEventContextChaining(t *testing.T) {
	ctx := NewEventContextFromEvent("chain.test", "test", "123").
		SetPID(999).
		SetContainerID("container-abc").
		SetPodName("pod-xyz").
		SetNamespace("production").
		SetServiceName("api-service").
		SetNodeName("worker-01").
		SetAction("update").
		SetTarget("/api/v2/resource").
		SetResult("success").
		SetReason("user initiated").
		SetDuration(250).
		SetSize(4096).
		SetCount(10).
		SetCausedBy("trigger-event").
		SetParentID("parent-999").
		AddCustomData("env", "prod").
		AddCustomData("version", "v2.1.0")

	// Verify all fields were set
	require.NotNil(t, ctx.PID)
	assert.Equal(t, uint32(999), *ctx.PID)
	require.NotNil(t, ctx.ContainerID)
	assert.Equal(t, "container-abc", *ctx.ContainerID)
	require.NotNil(t, ctx.PodName)
	assert.Equal(t, "pod-xyz", *ctx.PodName)
	require.NotNil(t, ctx.Namespace)
	assert.Equal(t, "production", *ctx.Namespace)
	require.NotNil(t, ctx.ServiceName)
	assert.Equal(t, "api-service", *ctx.ServiceName)
	require.NotNil(t, ctx.NodeName)
	assert.Equal(t, "worker-01", *ctx.NodeName)
	require.NotNil(t, ctx.Action)
	assert.Equal(t, "update", *ctx.Action)
	require.NotNil(t, ctx.Target)
	assert.Equal(t, "/api/v2/resource", *ctx.Target)
	require.NotNil(t, ctx.Result)
	assert.Equal(t, "success", *ctx.Result)
	require.NotNil(t, ctx.Reason)
	assert.Equal(t, "user initiated", *ctx.Reason)
	require.NotNil(t, ctx.DurationMS)
	assert.Equal(t, uint64(250), *ctx.DurationMS)
	require.NotNil(t, ctx.SizeBytes)
	assert.Equal(t, uint64(4096), *ctx.SizeBytes)
	require.NotNil(t, ctx.Count)
	assert.Equal(t, uint64(10), *ctx.Count)
	require.NotNil(t, ctx.CausedBy)
	assert.Equal(t, "trigger-event", *ctx.CausedBy)
	require.NotNil(t, ctx.ParentID)
	assert.Equal(t, "parent-999", *ctx.ParentID)
	assert.Equal(t, "prod", ctx.CustomData["env"])
	assert.Equal(t, "v2.1.0", ctx.CustomData["version"])
}

// TestEventContextConcurrentAccess tests thread safety
func TestEventContextConcurrentAccess(t *testing.T) {
	// Note: EventContext is not thread-safe by design
	// This test verifies that concurrent usage is possible with external synchronization
	ctx := NewEventContextFromEvent("concurrent", "test", "123")
	var mu sync.RWMutex

	var wg sync.WaitGroup
	iterations := 100

	// Concurrent writes with synchronization
	wg.Add(iterations)
	for i := 0; i < iterations; i++ {
		go func(n int) {
			defer wg.Done()
			mu.Lock()
			ctx.AddCustomData(fmt.Sprintf("key%d", n), fmt.Sprintf("value%d", n))
			mu.Unlock()
		}(i)
	}

	// Concurrent reads with synchronization
	wg.Add(iterations)
	for i := 0; i < iterations; i++ {
		go func() {
			defer wg.Done()
			mu.RLock()
			_ = ctx.ToMap()
			mu.RUnlock()
		}()
	}

	wg.Wait()

	// Verify all custom data was added
	assert.Len(t, ctx.CustomData, iterations)
	for i := 0; i < iterations; i++ {
		key := fmt.Sprintf("key%d", i)
		expected := fmt.Sprintf("value%d", i)
		assert.Equal(t, expected, ctx.CustomData[key])
	}
}

// Benchmark tests for performance-critical paths
func BenchmarkNewEventContextFromEvent(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewEventContextFromEvent("bench.test", "source", "id-123")
	}
}

func BenchmarkEventContextToMap(b *testing.B) {
	ctx := NewEventContextFromEvent("bench", "source", "id").
		SetPID(123).
		SetContainerID("container").
		SetPodName("pod").
		SetNamespace("ns").
		AddCustomData("key1", "value1").
		AddCustomData("key2", "value2")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ctx.ToMap()
	}
}

func BenchmarkConditionValueEquals(b *testing.B) {
	cv1 := NewStringConditionValue("test value")
	cv2 := NewStringConditionValue("test value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cv1.Equals(cv2)
	}
}

func BenchmarkConditionValueContains(b *testing.B) {
	cv := NewStringConditionValue("hello world this is a test")
	other := NewStringConditionValue("world")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cv.Contains(other)
	}
}

func BenchmarkNewConditionValue(b *testing.B) {
	values := []any{
		"string",
		42,
		3.14,
		true,
		[]string{"a", "b", "c"},
		nil,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewConditionValue(values[i%len(values)])
	}
}

// Helper functions for test pointers
func strPtr(s string) *string {
	return &s
}

func int64Ptr(i int64) *int64 {
	return &i
}

func float64Ptr(f float64) *float64 {
	return &f
}

func boolPtr(b bool) *bool {
	return &b
}
