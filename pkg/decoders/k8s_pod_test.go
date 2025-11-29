package decoders

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
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

	// Should return error when KV is nil
	_, err := decoder.Decode(context.Background(), input, conf)
	require.Error(t, err)
	// Optionally, check for specific error value if defined, e.g.:
	// assert.Equal(t, ErrNilKV, err)
}

// TestK8sPod_Decode_Success tests successful pod lookup
func TestK8sPod_Decode_Success(t *testing.T) {
	podInfo := PodInfo{
		Name:      "nginx-abc123",
		Namespace: "default",
		PodIP:     "10.244.1.5",
		HostIP:    "192.168.1.1",
		Labels:    map[string]string{"app": "nginx"},
	}

	podJSON, err := json.Marshal(podInfo)
	require.NoError(t, err)

	kv := &mockKeyValue{
		data: map[string][]byte{
			"pod.ip.10.244.1.5": podJSON,
		},
	}

	decoder := NewK8sPod(kv)
	input := []byte("10.244.1.5")
	conf := Decoder{}

	result, err := decoder.Decode(context.Background(), input, conf)
	require.NoError(t, err)
	assert.Equal(t, []byte("nginx-abc123"), result)
}

// TestK8sPod_Decode_NotFound_AllowUnknown tests IP not found with AllowUnknown=true
func TestK8sPod_Decode_NotFound_AllowUnknown(t *testing.T) {
	kv := &mockKeyValue{
		data: map[string][]byte{},
	}

	decoder := NewK8sPod(kv)
	input := []byte("10.0.0.1")
	conf := Decoder{AllowUnknown: true}

	result, err := decoder.Decode(context.Background(), input, conf)
	require.NoError(t, err)
	assert.Equal(t, input, result) // Returns original IP
}

// TestK8sPod_Decode_NotFound_DisallowUnknown tests IP not found with AllowUnknown=false
func TestK8sPod_Decode_NotFound_DisallowUnknown(t *testing.T) {
	kv := &mockKeyValue{
		data: map[string][]byte{},
	}

	decoder := NewK8sPod(kv)
	input := []byte("10.0.0.1")
	conf := Decoder{AllowUnknown: false}

	_, err := decoder.Decode(context.Background(), input, conf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pod not found")
}

// TestK8sPod_Decode_InvalidJSON tests malformed JSON in KV
func TestK8sPod_Decode_InvalidJSON(t *testing.T) {
	kv := &mockKeyValue{
		data: map[string][]byte{
			"pod.ip.10.0.0.1": []byte("invalid-json"),
		},
	}

	decoder := NewK8sPod(kv)
	input := []byte("10.0.0.1")
	conf := Decoder{}

	_, err := decoder.Decode(context.Background(), input, conf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse PodInfo")
}

// TestK8sPod_Decode_KVError tests NATS KV lookup error
func TestK8sPod_Decode_KVError(t *testing.T) {
	kv := &mockKeyValue{
		err: assert.AnError, // Simulated NATS error
	}

	decoder := NewK8sPod(kv)
	input := []byte("10.0.0.1")
	conf := Decoder{}

	_, err := decoder.Decode(context.Background(), input, conf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NATS KV lookup failed")
}
