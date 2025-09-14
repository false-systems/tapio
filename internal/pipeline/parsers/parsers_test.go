package parsers

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestKernelParser(t *testing.T) {
	parser := NewKernelParser()
	assert.Equal(t, "kernel", parser.Source())

	// Test nil event
	_, err := parser.Parse(nil)
	assert.Error(t, err)

	// Test wrong source
	raw := &domain.RawEvent{
		Source: "dns",
		Data:   []byte("{}"),
	}
	_, err = parser.Parse(raw)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid source")

	// Test valid kernel event
	kernelData := KernelEvent{
		EventType:   "syscall",
		PID:         1234,
		ContainerID: "container-123",
		Syscall:     "open",
		Path:        "/etc/passwd",
		Result:      0,
		Timestamp:   time.Now().UnixNano(),
		ProcessName: "test-process",
		Namespace:   "test-ns",
	}

	data, err := json.Marshal(kernelData)
	require.NoError(t, err)

	raw = &domain.RawEvent{
		Timestamp: time.Now(),
		Source:    "kernel",
		Data:      data,
	}

	obs, err := parser.Parse(raw)
	require.NoError(t, err)
	require.NotNil(t, obs)

	assert.Equal(t, "kernel", obs.Source)
	assert.Equal(t, "syscall.open", obs.Type)
	assert.NotNil(t, obs.PID)
	assert.Equal(t, int32(1234), *obs.PID)
	assert.NotNil(t, obs.ContainerID)
	assert.Equal(t, "container-123", *obs.ContainerID)
	assert.NotNil(t, obs.Namespace)
	assert.Equal(t, "test-ns", *obs.Namespace)
	assert.NotNil(t, obs.Action)
	assert.Equal(t, "open", *obs.Action)
	assert.NotNil(t, obs.Target)
	assert.Equal(t, "/etc/passwd", *obs.Target)
	assert.NotNil(t, obs.Result)
	assert.Equal(t, "success", *obs.Result)

	// Test error result
	kernelData.Result = -1
	data, _ = json.Marshal(kernelData)
	raw.Data = data

	obs, err = parser.Parse(raw)
	require.NoError(t, err)
	assert.NotNil(t, obs.Result)
	assert.Equal(t, "error:-1", *obs.Result)

	// Test invalid JSON
	raw.Data = []byte("invalid json")
	_, err = parser.Parse(raw)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal")
}

func TestDNSParser(t *testing.T) {
	parser := NewDNSParser()
	assert.Equal(t, "dns", parser.Source())

	// Test valid DNS event
	dnsData := DNSEvent{
		QueryID:      12345,
		QueryType:    "A",
		QueryName:    "example.com",
		ResponseIPs:  []string{"192.168.1.1"},
		ResponseCode: "NOERROR",
		LatencyMS:    25,
		PID:          5678,
		ContainerID:  "container-456",
		PodName:      "test-pod",
		Namespace:    "test-ns",
	}

	data, err := json.Marshal(dnsData)
	require.NoError(t, err)

	raw := &domain.RawEvent{
		Timestamp: time.Now(),
		Source:    "dns",
		Data:      data,
	}

	obs, err := parser.Parse(raw)
	require.NoError(t, err)
	require.NotNil(t, obs)

	assert.Equal(t, "dns", obs.Source)
	assert.Equal(t, "dns.a", obs.Type)
	assert.NotNil(t, obs.PID)
	assert.Equal(t, int32(5678), *obs.PID)
	assert.NotNil(t, obs.Target)
	assert.Equal(t, "example.com", *obs.Target)
	assert.NotNil(t, obs.Duration)
	assert.Equal(t, int64(25), *obs.Duration)
	assert.Contains(t, obs.Data["response_ips"], "192.168.1.1")

	// Test empty response code (should default to success)
	dnsData.ResponseCode = ""
	data, _ = json.Marshal(dnsData)
	raw.Data = data

	obs, err = parser.Parse(raw)
	require.NoError(t, err)
	assert.NotNil(t, obs.Result)
	assert.Equal(t, "success", *obs.Result)
}

func TestKubeAPIParser(t *testing.T) {
	parser := NewKubeAPIParser()
	assert.Equal(t, "kubeapi", parser.Source())

	// Test Pod event
	kubeData := KubeAPIEvent{
		Kind:      "Pod",
		Name:      "test-pod",
		Namespace: "default",
		Operation: "CREATE",
		UID:       "uid-123",
		Node:      "node-1",
	}

	data, err := json.Marshal(kubeData)
	require.NoError(t, err)

	raw := &domain.RawEvent{
		Timestamp: time.Now(),
		Source:    "kubeapi",
		Data:      data,
	}

	obs, err := parser.Parse(raw)
	require.NoError(t, err)
	require.NotNil(t, obs)

	assert.Equal(t, "kubeapi", obs.Source)
	assert.Equal(t, "k8s.pod.create", obs.Type)
	assert.NotNil(t, obs.PodName)
	assert.Equal(t, "test-pod", *obs.PodName)
	assert.NotNil(t, obs.Namespace)
	assert.Equal(t, "default", *obs.Namespace)
	assert.NotNil(t, obs.NodeName)
	assert.Equal(t, "node-1", *obs.NodeName)

	// Test Service event
	kubeData = KubeAPIEvent{
		Kind:      "Service",
		Name:      "test-service",
		Namespace: "default",
		Operation: "UPDATE",
		Reason:    "ConfigUpdated",
		Message:   "Service configuration updated",
	}

	data, _ = json.Marshal(kubeData)
	raw.Data = data

	obs, err = parser.Parse(raw)
	require.NoError(t, err)
	assert.NotNil(t, obs.ServiceName)
	assert.Equal(t, "test-service", *obs.ServiceName)
	assert.NotNil(t, obs.Reason)
	assert.Equal(t, "ConfigUpdated", *obs.Reason)
	assert.Equal(t, "Service configuration updated", obs.Data["message"])
}

func TestGenericParser(t *testing.T) {
	parser := NewGenericParser("custom")
	assert.Equal(t, "custom", parser.Source())

	// Test with structured event
	genericData := GenericEvent{
		Type:      "custom_event",
		Action:    "process",
		Target:    "file.txt",
		PID:       9999,
		Namespace: "custom-ns",
		Data:      map[string]string{"key": "value"},
	}

	data, err := json.Marshal(genericData)
	require.NoError(t, err)

	raw := &domain.RawEvent{
		Timestamp: time.Now(),
		Source:    "custom",
		Data:      data,
	}

	obs, err := parser.Parse(raw)
	require.NoError(t, err)
	require.NotNil(t, obs)

	assert.Equal(t, "custom", obs.Source)
	assert.Equal(t, "custom_event", obs.Type)
	assert.NotNil(t, obs.Action)
	assert.Equal(t, "process", *obs.Action)
	assert.NotNil(t, obs.Target)
	assert.Equal(t, "file.txt", *obs.Target)
	assert.Equal(t, "value", obs.Data["key"])

	// Test minimal event (unparseable data)
	raw.Data = []byte("not json")
	obs, err = parser.Parse(raw)
	require.NoError(t, err) // Should not error, creates minimal event
	require.NotNil(t, obs)
	assert.Equal(t, "custom", obs.Source)
	assert.Equal(t, "minimal", obs.Data["parse_status"])

	// Test with metadata fallback
	raw.Type = "fallback_type"
	raw.Metadata = map[string]string{
		"namespace": "meta-ns",
		"pod_name":  "meta-pod",
	}
	obs, err = parser.Parse(raw)
	require.NoError(t, err)
	assert.NotNil(t, obs.Namespace)
	assert.Equal(t, "meta-ns", *obs.Namespace)
	assert.NotNil(t, obs.PodName)
	assert.Equal(t, "meta-pod", *obs.PodName)

	// Test empty type uses raw.Type
	genericData.Type = ""
	data, _ = json.Marshal(genericData)
	raw.Data = data
	raw.Type = "raw_type"

	obs, err = parser.Parse(raw)
	require.NoError(t, err)
	assert.Equal(t, "raw_type", obs.Type)

	// Test no correlation key adds default namespace
	emptyData := GenericEvent{}
	data, _ = json.Marshal(emptyData)
	raw.Data = data
	raw.Metadata = nil

	obs, err = parser.Parse(raw)
	require.NoError(t, err)
	assert.NotNil(t, obs.Namespace)
	assert.Equal(t, "system", *obs.Namespace)
}
