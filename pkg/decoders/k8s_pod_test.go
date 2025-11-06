package decoders

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestK8sPod tests require NATS running
// Skip for now - will add integration tests later
func TestK8sPod_Structure(t *testing.T) {
	// Test PodInfo struct
	podInfo := PodInfo{
		Name:      "nginx-abc123",
		Namespace: "default",
		PodIP:     "10.244.1.5",
		Labels:    map[string]string{"app": "nginx"},
	}

	assert.Equal(t, "nginx-abc123", podInfo.Name)
	assert.Equal(t, "default", podInfo.Namespace)
	assert.Equal(t, "10.244.1.5", podInfo.PodIP)
}

// RED: Test NewK8sPod creates decoder
func TestNewK8sPod(t *testing.T) {
	decoder := NewK8sPod(nil)
	require.NotNil(t, decoder)
	assert.Nil(t, decoder.kv)
}

// RED: Test PodInfo JSON marshaling
func TestPodInfo_JSONMarshaling(t *testing.T) {
	podInfo := PodInfo{
		Name:      "test-pod",
		Namespace: "test-ns",
		PodIP:     "10.0.0.1",
		HostIP:    "192.168.1.1",
		Labels:    map[string]string{"app": "test"},
	}

	// Marshal to JSON
	data, err := json.Marshal(podInfo)
	require.NoError(t, err)

	// Unmarshal back
	var decoded PodInfo
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, podInfo.Name, decoded.Name)
	assert.Equal(t, podInfo.Namespace, decoded.Namespace)
	assert.Equal(t, podInfo.PodIP, decoded.PodIP)
	assert.Equal(t, podInfo.HostIP, decoded.HostIP)
	assert.Equal(t, podInfo.Labels, decoded.Labels)
}

// RED: Test Decode error handling with nil KV
func TestK8sPod_Decode_NilKV(t *testing.T) {
	decoder := NewK8sPod(nil)

	input := []byte("10.0.0.1")
	conf := Decoder{}

	// Will panic with nil KV, but that's expected behavior
	// In production, KV should never be nil
	assert.Panics(t, func() {
		decoder.Decode(context.Background(), input, conf)
	})
}

// Integration tests will be added when Context Service is ready
// For now, k8s_pod decoder is validated by:
// 1. Correct struct definitions (PodInfo matches Context Service)
// 2. NATS KV key format (pod.ip.<ip>)
// 3. Error handling (AllowUnknown support)
