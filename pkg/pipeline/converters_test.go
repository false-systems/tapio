package pipeline

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestEBPFConverter(t *testing.T) {
	ctx := context.Background()
	converter := NewEBPFConverter()
	
	assert.Equal(t, "ebpf", converter.SourceType())
	
	tests := []struct {
		name      string
		eventType string
		data      []byte
		check     func(t *testing.T, event *domain.UnifiedEvent)
	}{
		{
			name:      "memory allocation event",
			eventType: "memory_alloc",
			data:      makeEBPFData(1234, 5678, 4096),
			check: func(t *testing.T, event *domain.UnifiedEvent) {
				assert.Equal(t, domain.EventTypeKernel, event.Type)
				assert.Equal(t, "memory-event", event.SemanticContext.Event)
				assert.Equal(t, "resource", event.SemanticContext.Category)
				assert.Equal(t, uint32(1234), event.Kernel.PID)
				assert.Equal(t, uint32(5678), event.Kernel.TID)
			},
		},
		{
			name:      "OOM kill event",
			eventType: "oom_kill",
			data:      makeEBPFData(999, 999, 0),
			check: func(t *testing.T, event *domain.UnifiedEvent) {
				assert.Equal(t, "oom-kill", event.SemanticContext.Event)
				assert.Equal(t, "critical", event.SemanticContext.Severity)
			},
		},
		{
			name:      "network event",
			eventType: "network",
			data:      makeEBPFData(100, 100, 0),
			check: func(t *testing.T, event *domain.UnifiedEvent) {
				assert.Equal(t, domain.EventTypeNetwork, event.Type)
				assert.Equal(t, "network-activity", event.SemanticContext.Event)
			},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := collectors.RawEvent{
				Timestamp: time.Now(),
				Type:      "ebpf",
				Data:      tt.data,
				Metadata: map[string]string{
					"event_type": tt.eventType,
					"cpu":        "2",
				},
			}
			
			event, err := converter.Convert(ctx, raw)
			require.NoError(t, err)
			require.NotNil(t, event)
			
			assert.Equal(t, "ebpf", event.Source)
			tt.check(t, event)
		})
	}
}

func TestK8sConverter(t *testing.T) {
	ctx := context.Background()
	converter := NewK8sConverter()
	
	assert.Equal(t, "k8s", converter.SourceType())
	
	// Test pod creation event
	podObj := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]interface{}{
			"name":      "test-pod",
			"namespace": "default",
			"uid":       "12345",
		},
	}
	
	podData, err := json.Marshal(podObj)
	require.NoError(t, err)
	
	raw := collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "k8s",
		Data:      podData,
		Metadata: map[string]string{
			"resource":  "pods",
			"type":      "ADDED",
			"namespace": "default",
			"name":      "test-pod",
			"uid":       "12345",
		},
	}
	
	event, err := converter.Convert(ctx, raw)
	require.NoError(t, err)
	require.NotNil(t, event)
	
	assert.Equal(t, "k8s", event.Source)
	assert.Equal(t, domain.EventTypeKubernetes, event.Type)
	assert.Equal(t, "pod-created", event.SemanticContext.Event)
	assert.Equal(t, "lifecycle", event.SemanticContext.Category)
	
	assert.NotNil(t, event.K8s)
	assert.Equal(t, "default", event.K8s.Namespace)
	assert.Equal(t, "pods", event.K8s.Resource)
	assert.Equal(t, "test-pod", event.K8s.Name)
	assert.Equal(t, "12345", event.K8s.UID)
}

func TestSystemdConverter(t *testing.T) {
	ctx := context.Background()
	converter := NewSystemdConverter()
	
	assert.Equal(t, "systemd", converter.SourceType())
	
	entry := map[string]interface{}{
		"_SYSTEMD_UNIT": "docker.service",
		"MESSAGE":       "Started Docker Application Container Engine",
		"PRIORITY":      "6",
	}
	
	data, err := json.Marshal(entry)
	require.NoError(t, err)
	
	raw := collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "systemd",
		Data:      data,
	}
	
	event, err := converter.Convert(ctx, raw)
	require.NoError(t, err)
	require.NotNil(t, event)
	
	assert.Equal(t, "systemd", event.Source)
	assert.Equal(t, domain.EventTypeSystem, event.Type)
	assert.Equal(t, "systemd-log", event.SemanticContext.Event)
	assert.Equal(t, "docker.service", event.Entity.ID)
	assert.Equal(t, "Started Docker Application Container Engine", event.Application.LogMessage)
}

func TestCNIConverter(t *testing.T) {
	ctx := context.Background()
	converter := NewCNIConverter()
	
	assert.Equal(t, "cni", converter.SourceType())
	
	logLine := "2024-01-15 10:30:45 [INFO] CNI ADD: pod=test-pod namespace=default"
	
	raw := collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "cni",
		Data:      []byte(logLine),
	}
	
	event, err := converter.Convert(ctx, raw)
	require.NoError(t, err)
	require.NotNil(t, event)
	
	assert.Equal(t, "cni", event.Source)
	assert.Equal(t, domain.EventTypeNetwork, event.Type)
	assert.Equal(t, "cni-event", event.SemanticContext.Event)
	assert.Equal(t, logLine, event.Application.LogMessage)
}

func TestDetermineEventSeverity(t *testing.T) {
	tests := []struct {
		event    map[string]interface{}
		expected string
	}{
		{
			event:    map[string]interface{}{"type": "Warning"},
			expected: "warning",
		},
		{
			event:    map[string]interface{}{"type": "Error"},
			expected: "error",
		},
		{
			event:    map[string]interface{}{"type": "Normal"},
			expected: "info",
		},
		{
			event:    map[string]interface{}{},
			expected: "info",
		},
	}
	
	for _, tt := range tests {
		severity := determineEventSeverity(tt.event)
		assert.Equal(t, tt.expected, severity)
	}
}

// Helper to create eBPF event data
func makeEBPFData(pid, tid uint32, size uint64) []byte {
	data := make([]byte, 32)
	binary.LittleEndian.PutUint32(data[8:12], pid)
	binary.LittleEndian.PutUint32(data[12:16], tid)
	binary.LittleEndian.PutUint64(data[24:32], size)
	return data
}