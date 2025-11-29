package decoders

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RED: Test ServiceInfo structure
func TestK8sService_Structure(t *testing.T) {
	serviceInfo := ServiceInfo{
		Name:      "kubernetes",
		Namespace: "default",
		ClusterIP: "10.96.0.1",
		Type:      "ClusterIP",
		Labels:    map[string]string{"component": "apiserver"},
	}

	assert.Equal(t, "kubernetes", serviceInfo.Name)
	assert.Equal(t, "default", serviceInfo.Namespace)
	assert.Equal(t, "10.96.0.1", serviceInfo.ClusterIP)
	assert.Equal(t, "ClusterIP", serviceInfo.Type)
}

// RED: Test NewK8sService creates decoder
func TestNewK8sService(t *testing.T) {
	decoder := NewK8sService(nil)
	require.NotNil(t, decoder)
	assert.Nil(t, decoder.kv)
}

// RED: Test ServiceInfo JSON marshaling
func TestServiceInfo_JSONMarshaling(t *testing.T) {
	serviceInfo := ServiceInfo{
		Name:      "test-service",
		Namespace: "test-ns",
		ClusterIP: "10.96.0.10",
		Type:      "LoadBalancer",
		Labels:    map[string]string{"app": "test"},
	}

	// Marshal to JSON
	data, err := json.Marshal(serviceInfo)
	require.NoError(t, err)

	// Unmarshal back
	var decoded ServiceInfo
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, serviceInfo.Name, decoded.Name)
	assert.Equal(t, serviceInfo.Namespace, decoded.Namespace)
	assert.Equal(t, serviceInfo.ClusterIP, decoded.ClusterIP)
	assert.Equal(t, serviceInfo.Type, decoded.Type)
	assert.Equal(t, serviceInfo.Labels, decoded.Labels)
}

// RED: Test Decode error handling with nil KV
func TestK8sService_Decode_NilKV(t *testing.T) {
	decoder := NewK8sService(nil)

	ctx := context.Background()
	input := []byte("10.96.0.1")
	conf := Decoder{}

	// Should return error with nil KV, as KV must be initialized
	// In production, KV should never be nil
	_, err := decoder.Decode(ctx, input, conf)
	assert.Error(t, err)
}

// TestK8sService_Decode_Success tests successful service lookup
func TestK8sService_Decode_Success(t *testing.T) {
	serviceInfo := ServiceInfo{
		Name:      "kubernetes",
		Namespace: "default",
		ClusterIP: "10.96.0.1",
		Type:      "ClusterIP",
		Labels:    map[string]string{"component": "apiserver"},
	}

	serviceJSON, err := json.Marshal(serviceInfo)
	require.NoError(t, err)

	kv := &mockKeyValue{
		data: map[string][]byte{
			"service.ip.10.96.0.1": serviceJSON,
		},
	}

	decoder := NewK8sService(kv)
	input := []byte("10.96.0.1")
	conf := Decoder{}

	result, err := decoder.Decode(context.Background(), input, conf)
	require.NoError(t, err)
	assert.Equal(t, []byte("kubernetes"), result)
}

// TestK8sService_Decode_NotFound_AllowUnknown tests IP not found with AllowUnknown=true
func TestK8sService_Decode_NotFound_AllowUnknown(t *testing.T) {
	kv := &mockKeyValue{
		data: map[string][]byte{},
	}

	decoder := NewK8sService(kv)
	input := []byte("10.96.0.100")
	conf := Decoder{AllowUnknown: true}

	result, err := decoder.Decode(context.Background(), input, conf)
	require.NoError(t, err)
	assert.Equal(t, input, result) // Returns original IP
}

// TestK8sService_Decode_NotFound_DisallowUnknown tests IP not found with AllowUnknown=false
func TestK8sService_Decode_NotFound_DisallowUnknown(t *testing.T) {
	kv := &mockKeyValue{
		data: map[string][]byte{},
	}

	decoder := NewK8sService(kv)
	input := []byte("10.96.0.100")
	conf := Decoder{AllowUnknown: false}

	_, err := decoder.Decode(context.Background(), input, conf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "service not found")
}

// TestK8sService_Decode_InvalidJSON tests malformed JSON in KV
func TestK8sService_Decode_InvalidJSON(t *testing.T) {
	kv := &mockKeyValue{
		data: map[string][]byte{
			"service.ip.10.96.0.1": []byte("invalid-json"),
		},
	}

	decoder := NewK8sService(kv)
	input := []byte("10.96.0.1")
	conf := Decoder{}

	_, err := decoder.Decode(context.Background(), input, conf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse ServiceInfo")
}

// TestK8sService_Decode_KVError tests NATS KV lookup error
func TestK8sService_Decode_KVError(t *testing.T) {
	kv := &mockKeyValue{
		err: assert.AnError, // Simulated NATS error
	}

	decoder := NewK8sService(kv)
	input := []byte("10.96.0.1")
	conf := Decoder{}

	_, err := decoder.Decode(context.Background(), input, conf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NATS KV lookup failed")
}
