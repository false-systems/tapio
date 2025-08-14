package domain

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/collectors"
	"go.uber.org/zap"
)

func TestNewEventParser(t *testing.T) {
	tests := []struct {
		name        string
		logger      *zap.Logger
		expectError bool
	}{
		{
			name:        "valid logger",
			logger:      zap.NewNop(),
			expectError: false,
		},
		{
			name:        "nil logger",
			logger:      nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser, err := NewEventParser(tt.logger)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, parser)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, parser)
				assert.NotNil(t, parser.logger)
				assert.NotNil(t, parser.tracer)
			}
		})
	}
}

func TestEventParser_ParseEvent(t *testing.T) {
	parser, err := NewEventParser(zap.NewNop())
	require.NoError(t, err)

	tests := []struct {
		name         string
		rawEvent     collectors.RawEvent
		expectError  bool
		validateFunc func(t *testing.T, obs *ObservationEvent)
	}{
		{
			name: "kernel event with valid JSON",
			rawEvent: collectors.RawEvent{
				Timestamp: time.Now(),
				Type:      "kernel",
				Data:      []byte(`{"pid": 1234, "syscall": "open", "filename": "/etc/passwd", "return_value": 0}`),
				Metadata: map[string]string{
					"collector": "ebpf",
				},
				TraceID: "test-trace-id",
				SpanID:  "test-span-id",
			},
			expectError: false,
			validateFunc: func(t *testing.T, obs *ObservationEvent) {
				assert.Equal(t, "kernel", obs.Source)
				assert.Equal(t, "syscall", obs.Type)
				assert.NotNil(t, obs.PID)
				assert.Equal(t, int32(1234), *obs.PID)
				assert.NotNil(t, obs.Action)
				assert.Equal(t, "open", *obs.Action)
				assert.NotNil(t, obs.Target)
				assert.Equal(t, "/etc/passwd", *obs.Target)
				assert.NotNil(t, obs.Result)
				assert.Equal(t, "0", *obs.Result)
				assert.True(t, obs.HasCorrelationKey())
				assert.Equal(t, "ebpf", obs.Data["collector"])
			},
		},
		{
			name: "kubeapi event with namespace and pod",
			rawEvent: collectors.RawEvent{
				Timestamp: time.Now(),
				Type:      "kubeapi",
				Data:      []byte(`{"namespace": "default", "pod_name": "test-pod", "reason": "Created", "kind": "Pod", "name": "test-pod"}`),
				Metadata: map[string]string{
					"cluster": "test-cluster",
				},
			},
			expectError: false,
			validateFunc: func(t *testing.T, obs *ObservationEvent) {
				assert.Equal(t, "kubeapi", obs.Source)
				assert.Equal(t, "k8s_event", obs.Type)
				assert.NotNil(t, obs.Namespace)
				assert.Equal(t, "default", *obs.Namespace)
				assert.NotNil(t, obs.PodName)
				assert.Equal(t, "test-pod", *obs.PodName)
				assert.NotNil(t, obs.Action)
				assert.Equal(t, "Created", *obs.Action)
				assert.True(t, obs.HasCorrelationKey())
				assert.Equal(t, "test-cluster", obs.Data["cluster"])
			},
		},
		{
			name: "dns event with query and response",
			rawEvent: collectors.RawEvent{
				Timestamp: time.Now(),
				Type:      "dns",
				Data:      []byte(`{"pid": 5678, "domain": "example.com", "response": "1.2.3.4", "container_id": "abc123"}`),
				Metadata: map[string]string{
					"protocol": "udp",
				},
			},
			expectError: false,
			validateFunc: func(t *testing.T, obs *ObservationEvent) {
				assert.Equal(t, "dns", obs.Source)
				assert.Equal(t, "dns_query", obs.Type)
				assert.NotNil(t, obs.PID)
				assert.Equal(t, int32(5678), *obs.PID)
				assert.NotNil(t, obs.ContainerID)
				assert.Equal(t, "abc123", *obs.ContainerID)
				assert.NotNil(t, obs.Action)
				assert.Equal(t, "dns_query", *obs.Action)
				assert.NotNil(t, obs.Target)
				assert.Equal(t, "example.com", *obs.Target)
				assert.NotNil(t, obs.Result)
				assert.Equal(t, "1.2.3.4", *obs.Result)
				assert.True(t, obs.HasCorrelationKey())
			},
		},
		{
			name: "systemd service event",
			rawEvent: collectors.RawEvent{
				Timestamp: time.Now(),
				Type:      "systemd",
				Data:      []byte(`{"service_name": "nginx", "action": "started", "pid": 9999, "state": "active"}`),
				Metadata: map[string]string{
					"host": "worker-node-1",
				},
			},
			expectError: false,
			validateFunc: func(t *testing.T, obs *ObservationEvent) {
				assert.Equal(t, "systemd", obs.Source)
				assert.Equal(t, "service_event", obs.Type)
				assert.NotNil(t, obs.ServiceName)
				assert.Equal(t, "nginx", *obs.ServiceName)
				assert.NotNil(t, obs.PID)
				assert.Equal(t, int32(9999), *obs.PID)
				assert.NotNil(t, obs.Action)
				assert.Equal(t, "started", *obs.Action)
				assert.NotNil(t, obs.Target)
				assert.Equal(t, "active", *obs.Target)
				assert.True(t, obs.HasCorrelationKey())
			},
		},
		{
			name: "cni network event",
			rawEvent: collectors.RawEvent{
				Timestamp: time.Now(),
				Type:      "cni",
				Data:      []byte(`{"pod_name": "test-pod", "namespace": "kube-system", "action": "ADD", "interface": "eth0", "ip": "10.244.0.5"}`),
			},
			expectError: false,
			validateFunc: func(t *testing.T, obs *ObservationEvent) {
				assert.Equal(t, "cni", obs.Source)
				assert.Equal(t, "network_event", obs.Type)
				assert.NotNil(t, obs.PodName)
				assert.Equal(t, "test-pod", *obs.PodName)
				assert.NotNil(t, obs.Namespace)
				assert.Equal(t, "kube-system", *obs.Namespace)
				assert.NotNil(t, obs.Action)
				assert.Equal(t, "ADD", *obs.Action)
				assert.NotNil(t, obs.Target)
				assert.Equal(t, "eth0", *obs.Target)
				assert.True(t, obs.HasCorrelationKey())
			},
		},
		{
			name: "etcd storage event",
			rawEvent: collectors.RawEvent{
				Timestamp: time.Now(),
				Type:      "etcd",
				Data:      []byte(`{"operation": "PUT", "key": "/registry/pods/default/test-pod", "result": "success"}`),
				Metadata: map[string]string{
					"cluster": "test-cluster",
				},
			},
			expectError: false,
			validateFunc: func(t *testing.T, obs *ObservationEvent) {
				assert.Equal(t, "etcd", obs.Source)
				assert.Equal(t, "storage_event", obs.Type)
				assert.NotNil(t, obs.Action)
				assert.Equal(t, "PUT", *obs.Action)
				assert.NotNil(t, obs.Target)
				assert.Equal(t, "/registry/pods/default/test-pod", *obs.Target)
				assert.NotNil(t, obs.Result)
				assert.Equal(t, "success", *obs.Result)
				assert.NotNil(t, obs.Namespace)
				assert.Equal(t, "default", *obs.Namespace)
				assert.True(t, obs.HasCorrelationKey())
			},
		},
		{
			name: "unknown type with valid JSON",
			rawEvent: collectors.RawEvent{
				Timestamp: time.Now(),
				Type:      "unknown",
				Data:      []byte(`{"pid": 1111, "action": "custom_action", "field": "value"}`),
			},
			expectError: false,
			validateFunc: func(t *testing.T, obs *ObservationEvent) {
				assert.Equal(t, "unknown", obs.Source)
				assert.Equal(t, "unknown", obs.Type)
				assert.NotNil(t, obs.PID)
				assert.Equal(t, int32(1111), *obs.PID)
				assert.NotNil(t, obs.Action)
				assert.Equal(t, "custom_action", *obs.Action)
				assert.True(t, obs.HasCorrelationKey())
				assert.Equal(t, "value", obs.Data["field"])
			},
		},
		{
			name: "invalid JSON fallback to raw",
			rawEvent: collectors.RawEvent{
				Timestamp: time.Now(),
				Type:      "kernel",
				Data:      []byte(`invalid json {`),
				Metadata: map[string]string{
					"source": "test",
				},
			},
			expectError: false,
			validateFunc: func(t *testing.T, obs *ObservationEvent) {
				assert.Equal(t, "kernel", obs.Source)
				assert.Equal(t, "syscall", obs.Type)
				assert.NotNil(t, obs.Action)
				assert.Equal(t, "kernel_raw", *obs.Action)
				assert.Equal(t, "invalid json {", obs.Data["raw_data"])
				assert.Equal(t, "test", obs.Data["source"])
				// Should fail validation due to no correlation keys
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			obs, err := parser.ParseEvent(ctx, tt.rawEvent)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, obs)
			} else {
				if err != nil {
					// Check if it's a validation error due to missing correlation keys
					if tt.name == "invalid JSON fallback to raw" {
						assert.Error(t, err)
						assert.Contains(t, err.Error(), "validation failed")
						return
					}
				}
				assert.NoError(t, err)
				assert.NotNil(t, obs)

				// Common validations
				assert.NotEmpty(t, obs.ID)
				assert.True(t, obs.ID[:4] == "evt_")
				assert.Equal(t, tt.rawEvent.Timestamp, obs.Timestamp)
				assert.NotNil(t, obs.Data)

				// Custom validations
				if tt.validateFunc != nil {
					tt.validateFunc(t, obs)
				}
			}
		})
	}
}

func TestEventParser_ParseEvent_EdgeCases(t *testing.T) {
	parser, err := NewEventParser(zap.NewNop())
	require.NoError(t, err)

	t.Run("empty data", func(t *testing.T) {
		rawEvent := collectors.RawEvent{
			Timestamp: time.Now(),
			Type:      "kernel",
			Data:      []byte{},
		}

		obs, err := parser.ParseEvent(context.Background(), rawEvent)
		// Should fail validation due to no correlation keys
		assert.Error(t, err)
		assert.Nil(t, obs)
	})

	t.Run("nil data", func(t *testing.T) {
		rawEvent := collectors.RawEvent{
			Timestamp: time.Now(),
			Type:      "kernel",
			Data:      nil,
		}

		obs, err := parser.ParseEvent(context.Background(), rawEvent)
		// Should fail validation due to no correlation keys
		assert.Error(t, err)
		assert.Nil(t, obs)
	})

	t.Run("large PID value", func(t *testing.T) {
		rawEvent := collectors.RawEvent{
			Timestamp: time.Now(),
			Type:      "kernel",
			Data:      []byte(`{"pid": 2147483647, "syscall": "test"}`), // Max int32
		}

		obs, err := parser.ParseEvent(context.Background(), rawEvent)
		assert.NoError(t, err)
		assert.NotNil(t, obs.PID)
		assert.Equal(t, int32(2147483647), *obs.PID)
	})

	t.Run("PID as string", func(t *testing.T) {
		rawEvent := collectors.RawEvent{
			Timestamp: time.Now(),
			Type:      "kernel",
			Data:      []byte(`{"pid": "1234", "syscall": "test"}`),
		}

		obs, err := parser.ParseEvent(context.Background(), rawEvent)
		assert.NoError(t, err)
		assert.NotNil(t, obs.PID)
		assert.Equal(t, int32(1234), *obs.PID)
	})

	t.Run("invalid PID string", func(t *testing.T) {
		rawEvent := collectors.RawEvent{
			Timestamp: time.Now(),
			Type:      "kernel",
			Data:      []byte(`{"pid": "not-a-number", "syscall": "test"}`),
		}

		obs, err := parser.ParseEvent(context.Background(), rawEvent)
		// Should fail validation since no valid correlation keys are set
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "validation failed")
		assert.Nil(t, obs)
	})

	t.Run("nested JSON in raw data", func(t *testing.T) {
		nestedData := map[string]interface{}{
			"pid":     1234,
			"syscall": "open",
			"nested": map[string]interface{}{
				"key": "value",
			},
		}
		jsonData, _ := json.Marshal(nestedData)

		rawEvent := collectors.RawEvent{
			Timestamp: time.Now(),
			Type:      "kernel",
			Data:      jsonData,
		}

		obs, err := parser.ParseEvent(context.Background(), rawEvent)
		assert.NoError(t, err)
		assert.NotNil(t, obs.PID)
		assert.Equal(t, int32(1234), *obs.PID)
		assert.Contains(t, obs.Data["nested"], "key") // Should be stringified
	})
}

func TestEventParser_TypeMapping(t *testing.T) {
	parser, err := NewEventParser(zap.NewNop())
	require.NoError(t, err)

	tests := []struct {
		collectorType string
		expectedType  string
	}{
		{"kernel", "syscall"},
		{"ebpf", "syscall"},
		{"kubeapi", "k8s_event"},
		{"kubernetes", "k8s_event"},
		{"dns", "dns_query"},
		{"systemd", "service_event"},
		{"cni", "network_event"},
		{"etcd", "storage_event"},
		{"unknown", "unknown"},
		{"KERNEL", "syscall"}, // Test case insensitive
	}

	for _, tt := range tests {
		t.Run(tt.collectorType, func(t *testing.T) {
			result := parser.mapEventType(tt.collectorType)
			assert.Equal(t, tt.expectedType, result)
		})
	}
}

func TestEventParser_ExtractInt32(t *testing.T) {
	parser, err := NewEventParser(zap.NewNop())
	require.NoError(t, err)

	tests := []struct {
		name        string
		input       interface{}
		expected    int32
		expectError bool
	}{
		{"int32", int32(123), 123, false},
		{"int", int(456), 456, false},
		{"int64", int64(789), 789, false},
		{"float64", float64(101), 101, false},
		{"string valid", "999", 999, false},
		{"string invalid", "not-a-number", 0, true},
		{"nil", nil, 0, true},
		{"bool", true, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parser.extractInt32(tt.input)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestEventParser_HelperFunctions(t *testing.T) {
	parser, err := NewEventParser(zap.NewNop())
	require.NoError(t, err)

	t.Run("getStringValue", func(t *testing.T) {
		assert.Equal(t, "", parser.getStringValue(nil))

		value := "test"
		assert.Equal(t, "test", parser.getStringValue(&value))
	})

	t.Run("getInt32Value", func(t *testing.T) {
		assert.Equal(t, int32(0), parser.getInt32Value(nil))

		value := int32(42)
		assert.Equal(t, int32(42), parser.getInt32Value(&value))
	})

	t.Run("stringPtr", func(t *testing.T) {
		ptr := stringPtr("test")
		assert.NotNil(t, ptr)
		assert.Equal(t, "test", *ptr)
	})
}

func TestEventParser_GenerateEventID(t *testing.T) {
	parser, err := NewEventParser(zap.NewNop())
	require.NoError(t, err)

	// Test ID generation
	id1 := parser.generateEventID()
	id2 := parser.generateEventID()

	assert.NotEqual(t, id1, id2)
	assert.True(t, len(id1) > 4)
	assert.True(t, id1[:4] == "evt_")
	assert.True(t, id2[:4] == "evt_")
}

func TestEventParser_KubernetesResourceExtraction(t *testing.T) {
	parser, err := NewEventParser(zap.NewNop())
	require.NoError(t, err)

	t.Run("etcd kubernetes resource key parsing", func(t *testing.T) {
		rawEvent := collectors.RawEvent{
			Timestamp: time.Now(),
			Type:      "etcd",
			Data:      []byte(`{"operation": "PUT", "key": "/registry/pods/my-namespace/my-pod"}`),
		}

		obs, err := parser.ParseEvent(context.Background(), rawEvent)
		assert.NoError(t, err)
		assert.NotNil(t, obs.Namespace)
		assert.Equal(t, "my-namespace", *obs.Namespace)
	})

	t.Run("kubeapi pod event name extraction", func(t *testing.T) {
		rawEvent := collectors.RawEvent{
			Timestamp: time.Now(),
			Type:      "kubeapi",
			Data:      []byte(`{"kind": "Pod", "name": "web-server", "namespace": "production"}`),
		}

		obs, err := parser.ParseEvent(context.Background(), rawEvent)
		assert.NoError(t, err)
		assert.NotNil(t, obs.PodName)
		assert.Equal(t, "web-server", *obs.PodName)
		assert.NotNil(t, obs.Namespace)
		assert.Equal(t, "production", *obs.Namespace)
	})
}

func TestEventParser_CorrelationKeyValidation(t *testing.T) {
	parser, err := NewEventParser(zap.NewNop())
	require.NoError(t, err)

	t.Run("event with PID correlation key", func(t *testing.T) {
		rawEvent := collectors.RawEvent{
			Timestamp: time.Now(),
			Type:      "kernel",
			Data:      []byte(`{"pid": 1234, "syscall": "open"}`),
		}

		obs, err := parser.ParseEvent(context.Background(), rawEvent)
		assert.NoError(t, err)
		assert.True(t, obs.HasCorrelationKey())
	})

	t.Run("event with namespace correlation key", func(t *testing.T) {
		rawEvent := collectors.RawEvent{
			Timestamp: time.Now(),
			Type:      "kubeapi",
			Data:      []byte(`{"namespace": "default", "kind": "Service"}`),
		}

		obs, err := parser.ParseEvent(context.Background(), rawEvent)
		assert.NoError(t, err)
		assert.True(t, obs.HasCorrelationKey())
	})

	t.Run("event with no correlation keys should fail validation", func(t *testing.T) {
		rawEvent := collectors.RawEvent{
			Timestamp: time.Now(),
			Type:      "unknown",
			Data:      []byte(`{"random_field": "value"}`),
		}

		obs, err := parser.ParseEvent(context.Background(), rawEvent)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "validation failed")
		assert.Nil(t, obs)
	})
}

// Benchmark tests for performance
func BenchmarkEventParser_ParseKernelEvent(b *testing.B) {
	parser, err := NewEventParser(zap.NewNop())
	require.NoError(b, err)

	rawEvent := collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "kernel",
		Data:      []byte(`{"pid": 1234, "syscall": "open", "filename": "/etc/passwd", "return_value": 0}`),
		Metadata:  map[string]string{"collector": "ebpf"},
		TraceID:   "test-trace-id",
	}

	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := parser.ParseEvent(ctx, rawEvent)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEventParser_ParseKubeAPIEvent(b *testing.B) {
	parser, err := NewEventParser(zap.NewNop())
	require.NoError(b, err)

	rawEvent := collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "kubeapi",
		Data:      []byte(`{"namespace": "default", "pod_name": "test-pod", "reason": "Created", "kind": "Pod"}`),
	}

	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := parser.ParseEvent(ctx, rawEvent)
		if err != nil {
			b.Fatal(err)
		}
	}
}
