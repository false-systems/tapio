package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/collectors"
	"go.uber.org/zap"
)

// MockCollector implements a test collector
type MockCollector struct {
	name   string
	events chan collectors.RawEvent
	ctx    context.Context
	cancel context.CancelFunc
}

func NewMockCollector(name string) *MockCollector {
	return &MockCollector{
		name:   name,
		events: make(chan collectors.RawEvent, 10),
	}
}

func (m *MockCollector) Name() string { return m.name }

func (m *MockCollector) Start(ctx context.Context) error {
	m.ctx, m.cancel = context.WithCancel(ctx)
	return nil
}

func (m *MockCollector) Stop() error {
	if m.cancel != nil {
		m.cancel()
	}
	close(m.events)
	return nil
}

func (m *MockCollector) Events() <-chan collectors.RawEvent {
	return m.events
}

func (m *MockCollector) IsHealthy() bool { return true }

func (m *MockCollector) SendEvent(event collectors.RawEvent) {
	select {
	case m.events <- event:
	case <-m.ctx.Done():
	}
}

func TestEventPipeline(t *testing.T) {
	logger := zap.NewNop()

	t.Run("RegisterCollector", func(t *testing.T) {
		config := DefaultConfig()
		config.NATSConfig = nil // Disable NATS for unit test

		pipeline, err := New(logger, config)
		require.NoError(t, err)

		collector := NewMockCollector("test")
		err = pipeline.RegisterCollector("test", collector)
		assert.NoError(t, err)

		// Cannot register same name twice
		err = pipeline.RegisterCollector("test", collector)
		assert.Error(t, err)
	})

	t.Run("StartStop", func(t *testing.T) {
		config := DefaultConfig()
		config.NATSConfig = nil // Disable NATS for unit test

		pipeline, err := New(logger, config)
		require.NoError(t, err)

		collector := NewMockCollector("test")
		err = pipeline.RegisterCollector("test", collector)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		err = pipeline.Start(ctx)
		assert.NoError(t, err)

		// Cannot start twice
		err = pipeline.Start(ctx)
		assert.Error(t, err)

		// Send test event
		collector.SendEvent(collectors.RawEvent{
			Timestamp: time.Now(),
			Type:      "test",
			Data:      []byte("test data"),
			Metadata:  map[string]string{"key": "value"},
			TraceID:   collectors.GenerateTraceID(),
			SpanID:    collectors.GenerateSpanID(),
		})

		// Give time to process
		time.Sleep(100 * time.Millisecond)

		err = pipeline.Stop()
		assert.NoError(t, err)
	})
}

func TestEnrichedEvent(t *testing.T) {
	raw := &collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "kubeapi",
		Data:      []byte(`{"kind":"Pod","name":"test-pod"}`),
		Metadata: map[string]string{
			"collector": "kubeapi",
			"event":     "pod_created",
		},
		TraceID: collectors.GenerateTraceID(),
		SpanID:  collectors.GenerateSpanID(),
	}

	enriched := &EnrichedEvent{
		Raw:     raw,
		TraceID: raw.TraceID,
		SpanID:  raw.SpanID,
		K8sObject: &K8sObjectInfo{
			Kind:      "Pod",
			Name:      "test-pod",
			Namespace: "default",
			UID:       "test-uid",
			Labels:    map[string]string{"app": "test"},
		},
	}

	unified := enriched.ConvertToUnified()

	assert.NotEmpty(t, unified.ID)
	assert.Equal(t, raw.Timestamp, unified.Timestamp)
	assert.Equal(t, "kubeapi", unified.Source)
	assert.NotNil(t, unified.K8sContext)
	assert.Equal(t, "Pod", unified.K8sContext.Kind)
	assert.Equal(t, "test-pod", unified.K8sContext.Name)
	assert.NotNil(t, unified.TraceContext)
	assert.Equal(t, raw.TraceID, unified.TraceContext.TraceID)
}

func TestMapCollectorTypeToDomain(t *testing.T) {
	tests := []struct {
		collector string
		expected  string
	}{
		{"kubeapi", "kubernetes"},
		{"etcd", "system"},
		{"ebpf", "process"},
		{"cni", "network"},
		{"systemd", "system"},
		{"unknown", "system"},
	}

	for _, tt := range tests {
		t.Run(tt.collector, func(t *testing.T) {
			result := mapCollectorTypeToDomain(tt.collector)
			assert.Equal(t, tt.expected, string(result))
		})
	}
}
