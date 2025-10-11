package k8scontext

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPodInfo_JSONMarshaling verifies PodInfo can be marshaled/unmarshaled
func TestPodInfo_JSONMarshaling(t *testing.T) {
	original := PodInfo{
		Name:      "nginx-abc123",
		Namespace: "default",
		PodIP:     "10.244.1.5",
		HostIP:    "192.168.1.10",
		Labels: map[string]string{
			"app": "nginx",
			"env": "prod",
		},
	}

	// Marshal to JSON
	data, err := json.Marshal(original)
	require.NoError(t, err, "Failed to marshal PodInfo")

	// Unmarshal back
	var decoded PodInfo
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err, "Failed to unmarshal PodInfo")

	// Verify all fields match
	assert.Equal(t, original.Name, decoded.Name)
	assert.Equal(t, original.Namespace, decoded.Namespace)
	assert.Equal(t, original.PodIP, decoded.PodIP)
	assert.Equal(t, original.HostIP, decoded.HostIP)
	assert.Equal(t, original.Labels, decoded.Labels)
}

// TestPodInfo_JSONFieldNames verifies JSON field names match decoder expectations
func TestPodInfo_JSONFieldNames(t *testing.T) {
	podInfo := PodInfo{
		Name:      "test-pod",
		Namespace: "default",
		PodIP:     "10.0.1.5",
		HostIP:    "192.168.1.1",
		Labels:    map[string]string{"app": "test"},
	}

	data, err := json.Marshal(podInfo)
	require.NoError(t, err)

	// Verify JSON has exact field names expected by decoder
	var jsonMap map[string]interface{}
	err = json.Unmarshal(data, &jsonMap)
	require.NoError(t, err)

	// These field names MUST match pkg/decoders/k8s_pod.go:PodInfo
	assert.Contains(t, jsonMap, "name")
	assert.Contains(t, jsonMap, "namespace")
	assert.Contains(t, jsonMap, "pod_ip")
	assert.Contains(t, jsonMap, "host_ip")
	assert.Contains(t, jsonMap, "labels")

	assert.Equal(t, "test-pod", jsonMap["name"])
	assert.Equal(t, "default", jsonMap["namespace"])
	assert.Equal(t, "10.0.1.5", jsonMap["pod_ip"])
	assert.Equal(t, "192.168.1.1", jsonMap["host_ip"])
}

// TestServiceInfo_JSONMarshaling verifies ServiceInfo can be marshaled/unmarshaled
func TestServiceInfo_JSONMarshaling(t *testing.T) {
	original := ServiceInfo{
		Name:      "kubernetes",
		Namespace: "default",
		ClusterIP: "10.96.0.1",
		Type:      "ClusterIP",
		Labels: map[string]string{
			"component": "apiserver",
		},
	}

	// Marshal to JSON
	data, err := json.Marshal(original)
	require.NoError(t, err, "Failed to marshal ServiceInfo")

	// Unmarshal back
	var decoded ServiceInfo
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err, "Failed to unmarshal ServiceInfo")

	// Verify all fields match
	assert.Equal(t, original.Name, decoded.Name)
	assert.Equal(t, original.Namespace, decoded.Namespace)
	assert.Equal(t, original.ClusterIP, decoded.ClusterIP)
	assert.Equal(t, original.Type, decoded.Type)
	assert.Equal(t, original.Labels, decoded.Labels)
}

// TestServiceInfo_JSONFieldNames verifies JSON field names match decoder expectations
func TestServiceInfo_JSONFieldNames(t *testing.T) {
	serviceInfo := ServiceInfo{
		Name:      "api-service",
		Namespace: "default",
		ClusterIP: "10.96.0.10",
		Type:      "LoadBalancer",
		Labels:    map[string]string{"app": "api"},
	}

	data, err := json.Marshal(serviceInfo)
	require.NoError(t, err)

	// Verify JSON has exact field names expected by decoder
	var jsonMap map[string]interface{}
	err = json.Unmarshal(data, &jsonMap)
	require.NoError(t, err)

	// These field names MUST match pkg/decoders/k8s_service.go:ServiceInfo
	assert.Contains(t, jsonMap, "name")
	assert.Contains(t, jsonMap, "namespace")
	assert.Contains(t, jsonMap, "cluster_ip")
	assert.Contains(t, jsonMap, "type")
	assert.Contains(t, jsonMap, "labels")

	assert.Equal(t, "api-service", jsonMap["name"])
	assert.Equal(t, "default", jsonMap["namespace"])
	assert.Equal(t, "10.96.0.10", jsonMap["cluster_ip"])
	assert.Equal(t, "LoadBalancer", jsonMap["type"])
}

// TestDeploymentInfo_JSONMarshaling verifies DeploymentInfo can be marshaled/unmarshaled
func TestDeploymentInfo_JSONMarshaling(t *testing.T) {
	original := DeploymentInfo{
		Name:      "nginx-deployment",
		Namespace: "default",
		Replicas:  3,
		Image:     "nginx:1.21",
		Labels: map[string]string{
			"app": "nginx",
		},
	}

	// Marshal to JSON
	data, err := json.Marshal(original)
	require.NoError(t, err, "Failed to marshal DeploymentInfo")

	// Unmarshal back
	var decoded DeploymentInfo
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err, "Failed to unmarshal DeploymentInfo")

	// Verify all fields match
	assert.Equal(t, original.Name, decoded.Name)
	assert.Equal(t, original.Namespace, decoded.Namespace)
	assert.Equal(t, original.Replicas, decoded.Replicas)
	assert.Equal(t, original.Image, decoded.Image)
	assert.Equal(t, original.Labels, decoded.Labels)
}

// TestNodeInfo_JSONMarshaling verifies NodeInfo can be marshaled/unmarshaled
func TestNodeInfo_JSONMarshaling(t *testing.T) {
	original := NodeInfo{
		Name: "node-1",
		Labels: map[string]string{
			"kubernetes.io/hostname": "node-1",
		},
		Zone:   "us-east-1a",
		Region: "us-east-1",
	}

	// Marshal to JSON
	data, err := json.Marshal(original)
	require.NoError(t, err, "Failed to marshal NodeInfo")

	// Unmarshal back
	var decoded NodeInfo
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err, "Failed to unmarshal NodeInfo")

	// Verify all fields match
	assert.Equal(t, original.Name, decoded.Name)
	assert.Equal(t, original.Labels, decoded.Labels)
	assert.Equal(t, original.Zone, decoded.Zone)
	assert.Equal(t, original.Region, decoded.Region)
}

// TestOwnerInfo_JSONMarshaling verifies OwnerInfo can be marshaled/unmarshaled
func TestOwnerInfo_JSONMarshaling(t *testing.T) {
	original := OwnerInfo{
		OwnerKind: "Deployment",
		OwnerName: "nginx-deployment",
		Namespace: "default",
	}

	// Marshal to JSON
	data, err := json.Marshal(original)
	require.NoError(t, err, "Failed to marshal OwnerInfo")

	// Unmarshal back
	var decoded OwnerInfo
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err, "Failed to unmarshal OwnerInfo")

	// Verify all fields match
	assert.Equal(t, original.OwnerKind, decoded.OwnerKind)
	assert.Equal(t, original.OwnerName, decoded.OwnerName)
	assert.Equal(t, original.Namespace, decoded.Namespace)
}

// TestConfig_Defaults verifies Config struct
func TestConfig_Defaults(t *testing.T) {
	config := Config{
		NATSConn:        nil, // Set by caller
		KVBucket:        "tapio-k8s-context",
		K8sConfig:       nil, // Will use InClusterConfig
		EventBufferSize: 1000,
		MaxRetries:      3,
	}

	assert.Equal(t, "tapio-k8s-context", config.KVBucket)
	assert.Equal(t, 1000, config.EventBufferSize)
	assert.Equal(t, 3, config.MaxRetries)
}

// TestPodInfo_NilLabels verifies nil labels are handled correctly
func TestPodInfo_NilLabels(t *testing.T) {
	podInfo := PodInfo{
		Name:      "test-pod",
		Namespace: "default",
		PodIP:     "10.0.1.5",
		HostIP:    "192.168.1.1",
		Labels:    nil, // nil labels
	}

	// Should marshal without error
	data, err := json.Marshal(podInfo)
	require.NoError(t, err)

	// Should unmarshal without error
	var decoded PodInfo
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	// nil labels should remain nil (or become empty map, both acceptable)
	assert.True(t, decoded.Labels == nil || len(decoded.Labels) == 0)
}
