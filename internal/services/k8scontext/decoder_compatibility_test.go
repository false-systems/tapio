package k8scontext

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	decoders "github.com/yairfalse/tapio/pkg/decoders"
)

// TestPodInfo_MatchesDecoderType verifies PodInfo is identical to decoder PodInfo
func TestPodInfo_MatchesDecoderType(t *testing.T) {
	// Create service PodInfo
	servicePod := PodInfo{
		Name:      "nginx-abc123",
		Namespace: "default",
		PodIP:     "10.244.1.5",
		HostIP:    "192.168.1.10",
		Labels: map[string]string{
			"app": "nginx",
		},
	}

	// Marshal service PodInfo
	serviceData, err := json.Marshal(servicePod)
	require.NoError(t, err)

	// Unmarshal into decoder PodInfo
	var decoderPod decoders.PodInfo
	err = json.Unmarshal(serviceData, &decoderPod)
	require.NoError(t, err, "Decoder should be able to unmarshal service PodInfo")

	// Verify all fields transferred correctly
	assert.Equal(t, servicePod.Name, decoderPod.Name)
	assert.Equal(t, servicePod.Namespace, decoderPod.Namespace)
	assert.Equal(t, servicePod.PodIP, decoderPod.PodIP)
	assert.Equal(t, servicePod.HostIP, decoderPod.HostIP)
	assert.Equal(t, servicePod.Labels, decoderPod.Labels)
}

// TestPodInfo_DecoderCanUnmarshalServiceJSON verifies decoder can read service's JSON
func TestPodInfo_DecoderCanUnmarshalServiceJSON(t *testing.T) {
	// Simulate what Context Service writes to NATS KV
	servicePod := PodInfo{
		Name:      "test-pod",
		Namespace: "kube-system",
		PodIP:     "10.0.1.100",
		HostIP:    "192.168.1.50",
		Labels: map[string]string{
			"component": "kube-proxy",
			"tier":      "node",
		},
	}

	jsonData, err := json.Marshal(servicePod)
	require.NoError(t, err)

	// Simulate what decoder reads from NATS KV
	var decoderPod decoders.PodInfo
	err = json.Unmarshal(jsonData, &decoderPod)
	require.NoError(t, err, "Decoder MUST be able to unmarshal Context Service JSON")

	// Verify complete data integrity
	assert.Equal(t, "test-pod", decoderPod.Name)
	assert.Equal(t, "kube-system", decoderPod.Namespace)
	assert.Equal(t, "10.0.1.100", decoderPod.PodIP)
	assert.Equal(t, "192.168.1.50", decoderPod.HostIP)
	assert.Len(t, decoderPod.Labels, 2)
	assert.Equal(t, "kube-proxy", decoderPod.Labels["component"])
	assert.Equal(t, "node", decoderPod.Labels["tier"])
}

// TestServiceInfo_MatchesDecoderType verifies ServiceInfo is identical to decoder ServiceInfo
func TestServiceInfo_MatchesDecoderType(t *testing.T) {
	// Create service ServiceInfo
	serviceSvc := ServiceInfo{
		Name:      "kubernetes",
		Namespace: "default",
		ClusterIP: "10.96.0.1",
		Type:      "ClusterIP",
		Labels: map[string]string{
			"component": "apiserver",
		},
	}

	// Marshal service ServiceInfo
	serviceData, err := json.Marshal(serviceSvc)
	require.NoError(t, err)

	// Unmarshal into decoder ServiceInfo
	var decoderSvc decoders.ServiceInfo
	err = json.Unmarshal(serviceData, &decoderSvc)
	require.NoError(t, err, "Decoder should be able to unmarshal service ServiceInfo")

	// Verify all fields transferred correctly
	assert.Equal(t, serviceSvc.Name, decoderSvc.Name)
	assert.Equal(t, serviceSvc.Namespace, decoderSvc.Namespace)
	assert.Equal(t, serviceSvc.ClusterIP, decoderSvc.ClusterIP)
	assert.Equal(t, serviceSvc.Type, decoderSvc.Type)
	assert.Equal(t, serviceSvc.Labels, decoderSvc.Labels)
}

// TestServiceInfo_DecoderCanUnmarshalServiceJSON verifies decoder can read service's JSON
func TestServiceInfo_DecoderCanUnmarshalServiceJSON(t *testing.T) {
	// Simulate what Context Service writes to NATS KV
	serviceSvc := ServiceInfo{
		Name:      "api-gateway",
		Namespace: "production",
		ClusterIP: "10.96.5.10",
		Type:      "LoadBalancer",
		Labels: map[string]string{
			"app":  "gateway",
			"tier": "frontend",
		},
	}

	jsonData, err := json.Marshal(serviceSvc)
	require.NoError(t, err)

	// Simulate what decoder reads from NATS KV
	var decoderSvc decoders.ServiceInfo
	err = json.Unmarshal(jsonData, &decoderSvc)
	require.NoError(t, err, "Decoder MUST be able to unmarshal Context Service JSON")

	// Verify complete data integrity
	assert.Equal(t, "api-gateway", decoderSvc.Name)
	assert.Equal(t, "production", decoderSvc.Namespace)
	assert.Equal(t, "10.96.5.10", decoderSvc.ClusterIP)
	assert.Equal(t, "LoadBalancer", decoderSvc.Type)
	assert.Len(t, decoderSvc.Labels, 2)
	assert.Equal(t, "gateway", decoderSvc.Labels["app"])
	assert.Equal(t, "frontend", decoderSvc.Labels["tier"])
}
