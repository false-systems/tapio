package transformer

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestEventTransformer_Transform(t *testing.T) {
	transformer := NewEventTransformer()

	tests := []struct {
		name     string
		input    collectors.RawEvent
		validate func(t *testing.T, event *domain.UnifiedEvent)
		wantErr  bool
	}{
		{
			name: "systemd event with JSON data",
			input: collectors.RawEvent{
				Type:      "systemd",
				Timestamp: time.Now(),
				Data: []byte(`{
					"message": "API server started",
					"level": "info",
					"unit": "api-server.service"
				}`),
				Metadata: map[string]string{
					"node":     "node-1",
					"trace_id": "trace-123",
					"span_id":  "span-456",
				},
			},
			validate: func(t *testing.T, event *domain.UnifiedEvent) {
				assert.NotEmpty(t, event.ID)
				assert.Equal(t, domain.EventTypeLog, event.Type)
				assert.Equal(t, "systemd", event.Source)
				assert.Equal(t, "system", event.Category)
				assert.Equal(t, domain.EventSeverityInfo, event.Severity)
				assert.Contains(t, event.Message, "API server started")

				// Check OTEL context
				assert.NotNil(t, event.TraceContext)
				assert.Equal(t, "trace-123", event.TraceContext.TraceID)
				assert.Equal(t, "span-456", event.TraceContext.SpanID)

				// Check entity context
				assert.NotNil(t, event.Entity)
				assert.Equal(t, "systemd", event.Entity.Type)
				assert.Equal(t, "node-1", event.Entity.Attributes["node"])
			},
		},
		{
			name: "k8s pod OOM event",
			input: collectors.RawEvent{
				Type:      "kubeapi",
				Timestamp: time.Now(),
				Data: []byte(`{
					"type": "Warning",
					"reason": "OOMKilling",
					"object": {
						"kind": "Pod",
						"name": "api-server-xyz",
						"namespace": "production"
					},
					"message": "Container api exceeded memory limit"
				}`),
				Metadata: map[string]string{
					"cluster":  "prod-cluster",
					"trace_id": "trace-789",
				},
			},
			validate: func(t *testing.T, event *domain.UnifiedEvent) {
				assert.Equal(t, domain.EventTypeKubernetes, event.Type)
				assert.Equal(t, "kubeapi", event.Source)
				assert.Equal(t, "orchestration", event.Category)
				assert.Equal(t, domain.EventSeverityCritical, event.Severity) // OOM is critical

				// Check K8s specific data
				assert.NotNil(t, event.Kubernetes)
				assert.Equal(t, "Warning", event.Kubernetes.EventType)
				assert.Equal(t, "OOMKilling", event.Kubernetes.Reason)
				assert.Equal(t, "Pod", event.Kubernetes.ObjectKind)
				assert.Equal(t, "Pod/api-server-xyz", event.Kubernetes.Object)
				assert.Equal(t, "production", event.Entity.Namespace)

				// Check semantic context
				assert.NotNil(t, event.Semantic)
				assert.Equal(t, "memory_exhaustion", event.Semantic.Intent)
				assert.Contains(t, event.Semantic.Tags, "oom")
				assert.Contains(t, event.Semantic.Tags, "pod-failure")
			},
		},
		{
			name: "ebpf syscall event",
			input: collectors.RawEvent{
				Type:      "ebpf",
				Timestamp: time.Now(),
				Data: []byte(`{
					"syscall": "openat",
					"pid": 1234,
					"comm": "nginx",
					"filename": "/etc/nginx/nginx.conf",
					"flags": "O_RDONLY"
				}`),
				Metadata: map[string]string{
					"container_id": "abc123",
					"pod":          "nginx-deployment-xxx",
				},
			},
			validate: func(t *testing.T, event *domain.UnifiedEvent) {
				assert.Equal(t, domain.EventTypeSystem, event.Type)
				assert.Equal(t, "kernel", event.Category)

				// Check kernel data
				assert.NotNil(t, event.Kernel)
				assert.Equal(t, "openat", event.Kernel.Syscall)
				assert.Equal(t, uint32(1234), event.Kernel.PID)
				assert.Equal(t, "nginx", event.Kernel.Comm)

				// Check entity enrichment
				assert.NotNil(t, event.Entity)
				assert.Equal(t, "nginx-deployment-xxx", event.Entity.Attributes["pod"])
			},
		},
		{
			name: "etcd key operation",
			input: collectors.RawEvent{
				Type:      "etcd",
				Timestamp: time.Now(),
				Data: []byte(`{
					"operation": "put",
					"key": "/registry/pods/default/test-pod",
					"value_size": 2048,
					"revision": 12345
				}`),
				Metadata: map[string]string{
					"trace_id": "trace-etcd-999",
				},
			},
			validate: func(t *testing.T, event *domain.UnifiedEvent) {
				assert.Equal(t, domain.EventTypeSystem, event.Type)
				assert.Equal(t, "storage", event.Category)
				assert.Contains(t, event.Message, "put")

				// Check if we detect K8s resource updates
				assert.NotNil(t, event.Semantic)
				assert.Contains(t, event.Semantic.Tags, "k8s-resource-update")
				assert.Contains(t, event.Tags, "k8s-resource-update") // Also in main tags
			},
		},
		{
			name: "cni network event",
			input: collectors.RawEvent{
				Type:      "cni",
				Timestamp: time.Now(),
				Data: []byte(`{
					"action": "ADD",
					"pod": "web-server-123",
					"namespace": "default",
					"ip": "10.244.1.5",
					"interface": "eth0"
				}`),
				Metadata: map[string]string{
					"node": "worker-2",
				},
			},
			validate: func(t *testing.T, event *domain.UnifiedEvent) {
				assert.Equal(t, domain.EventTypeNetwork, event.Type)
				assert.Equal(t, "network", event.Category)

				// Check network data
				assert.NotNil(t, event.Network)
				assert.Equal(t, "ADD", event.Entity.Attributes["action"])
				assert.Equal(t, "10.244.1.5", event.Entity.Attributes["ip"])
				assert.Equal(t, "10.244.1.5", event.Network.SourceIP)
			},
		},
		{
			name: "raw text event",
			input: collectors.RawEvent{
				Type:      "systemd",
				Timestamp: time.Now(),
				Data:      []byte("Dec 25 10:15:00 node-1 kubelet[1234]: E1225 10:15:00.123456 1234 pod_workers.go:190] Error syncing pod"),
				Metadata:  map[string]string{},
			},
			validate: func(t *testing.T, event *domain.UnifiedEvent) {
				assert.Equal(t, domain.EventTypeLog, event.Type)
				assert.Contains(t, event.Message, "Error syncing pod")
				assert.Equal(t, domain.EventSeverityError, event.Severity) // Detected 'E' prefix
			},
		},
		{
			name: "event with parent span",
			input: collectors.RawEvent{
				Type:      "kubeapi",
				Timestamp: time.Now(),
				Data:      []byte(`{"type": "Normal", "reason": "Created"}`),
				Metadata: map[string]string{
					"trace_id":       "trace-abc",
					"span_id":        "span-child",
					"parent_span_id": "span-parent",
				},
			},
			validate: func(t *testing.T, event *domain.UnifiedEvent) {
				assert.NotNil(t, event.TraceContext)
				assert.Equal(t, "trace-abc", event.TraceContext.TraceID)
				assert.Equal(t, "span-child", event.TraceContext.SpanID)
				assert.Equal(t, "span-parent", event.TraceContext.ParentSpanID)
			},
		},
		{
			name: "malformed JSON data",
			input: collectors.RawEvent{
				Type:      "kubeapi",
				Timestamp: time.Now(),
				Data:      []byte(`{"invalid": json`),
				Metadata:  map[string]string{},
			},
			validate: func(t *testing.T, event *domain.UnifiedEvent) {
				// Should not fail, just treat as raw text
				assert.NotEmpty(t, event.ID)
				assert.Contains(t, event.Message, "invalid")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, err := transformer.Transform(context.Background(), tt.input)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, event)

			// Common validations
			assert.NotEmpty(t, event.ID)
			assert.Equal(t, tt.input.Timestamp, event.Timestamp)
			assert.Contains(t, event.Tags, tt.input.Type)

			// Test-specific validations
			tt.validate(t, event)
		})
	}
}

func TestEventTransformer_InferSemantics(t *testing.T) {
	transformer := NewEventTransformer()

	tests := []struct {
		name     string
		event    *domain.UnifiedEvent
		expected *domain.SemanticContext
	}{
		{
			name: "OOM event semantic inference",
			event: &domain.UnifiedEvent{
				Type: domain.EventTypeKubernetes,
				Kubernetes: &domain.KubernetesData{
					Reason: "OOMKilling",
				},
			},
			expected: &domain.SemanticContext{
				Intent:     "memory_exhaustion",
				Category:   "resource_management",
				Tags:       []string{"oom", "pod-failure", "critical"},
				Confidence: 0.95,
			},
		},
		{
			name: "Network timeout semantic inference",
			event: &domain.UnifiedEvent{
				Type: domain.EventTypeNetwork,
				Network: &domain.NetworkData{
					StatusCode: 504,
				},
				Message: "Gateway timeout",
			},
			expected: &domain.SemanticContext{
				Intent:     "network_failure",
				Category:   "connectivity",
				Tags:       []string{"timeout", "network", "availability"},
				Confidence: 0.85,
			},
		},
		{
			name: "Disk pressure semantic inference",
			event: &domain.UnifiedEvent{
				Type:    domain.EventTypeLog,
				Message: "No space left on device",
			},
			expected: &domain.SemanticContext{
				Intent:     "disk_exhaustion",
				Category:   "storage",
				Tags:       []string{"disk-full", "storage", "critical"},
				Confidence: 0.9,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := transformer.inferSemantics(context.Background(), tt.event)

			assert.Equal(t, tt.expected.Intent, ctx.Intent)
			assert.Equal(t, tt.expected.Category, ctx.Category)
			assert.GreaterOrEqual(t, ctx.Confidence, 0.0)
			assert.LessOrEqual(t, ctx.Confidence, 1.0)

			// Check tags
			for _, tag := range tt.expected.Tags {
				assert.Contains(t, ctx.Tags, tag)
			}
		})
	}
}

func TestEventTransformer_ExtractOTELContext(t *testing.T) {
	transformer := NewEventTransformer()

	tests := []struct {
		name     string
		metadata map[string]string
		expected *domain.TraceContext
	}{
		{
			name: "full OTEL context",
			metadata: map[string]string{
				"trace_id":        "trace-123",
				"span_id":         "span-456",
				"parent_span_id":  "span-parent",
				"trace_state":     "vendor1=value1",
				"baggage.user.id": "user-789",
				"baggage.tenant":  "acme",
			},
			expected: &domain.TraceContext{
				TraceID:      "trace-123",
				SpanID:       "span-456",
				ParentSpanID: "span-parent",
				TraceState:   "vendor1=value1",
				Baggage: map[string]string{
					"user.id": "user-789",
					"tenant":  "acme",
				},
			},
		},
		{
			name: "no OTEL context",
			metadata: map[string]string{
				"foo": "bar",
			},
			expected: nil,
		},
		{
			name: "partial OTEL context",
			metadata: map[string]string{
				"trace_id": "trace-999",
			},
			expected: &domain.TraceContext{
				TraceID: "trace-999",
				Baggage: map[string]string{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := transformer.extractOTELContext(tt.metadata)

			if tt.expected == nil {
				assert.Nil(t, ctx)
				return
			}

			require.NotNil(t, ctx)
			assert.Equal(t, tt.expected.TraceID, ctx.TraceID)
			assert.Equal(t, tt.expected.SpanID, ctx.SpanID)
			assert.Equal(t, tt.expected.ParentSpanID, ctx.ParentSpanID)
			assert.Equal(t, tt.expected.TraceState, ctx.TraceState)
			assert.Equal(t, tt.expected.Baggage, ctx.Baggage)
		})
	}
}

func TestEventTransformer_Concurrent(t *testing.T) {
	transformer := NewEventTransformer()

	// Test concurrent transformation
	events := make([]collectors.RawEvent, 100)
	for i := 0; i < 100; i++ {
		events[i] = collectors.RawEvent{
			Type:      "test",
			Timestamp: time.Now(),
			Data:      []byte(`{"index": ` + string(rune(i)) + `}`),
			Metadata: map[string]string{
				"trace_id": "trace-" + string(rune(i)),
			},
		}
	}

	results := make([]*domain.UnifiedEvent, 100)
	errors := make([]error, 100)

	// Transform concurrently
	done := make(chan bool, 100)
	for i := 0; i < 100; i++ {
		go func(idx int) {
			results[idx], errors[idx] = transformer.Transform(context.Background(), events[idx])
			done <- true
		}(i)
	}

	// Wait for all
	for i := 0; i < 100; i++ {
		<-done
	}

	// Verify all succeeded
	for i := 0; i < 100; i++ {
		assert.NoError(t, errors[i])
		assert.NotNil(t, results[i])
		assert.NotEmpty(t, results[i].ID)
	}
}

func BenchmarkEventTransformer_Transform(b *testing.B) {
	transformer := NewEventTransformer()

	event := collectors.RawEvent{
		Type:      "kubeapi",
		Timestamp: time.Now(),
		Data: []byte(`{
			"type": "Warning",
			"reason": "OOMKilling",
			"object": {"kind": "Pod", "name": "test"}
		}`),
		Metadata: map[string]string{
			"trace_id": "trace-bench",
		},
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := transformer.Transform(context.Background(), event)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
