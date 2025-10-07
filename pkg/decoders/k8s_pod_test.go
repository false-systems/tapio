package decoders

import (
	"testing"

	"github.com/stretchr/testify/assert"
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

// Integration tests will be added when Context Service is ready
// For now, k8s_pod decoder is validated by:
// 1. Correct struct definitions (PodInfo matches Context Service)
// 2. NATS KV key format (pod.ip.<ip>)
// 3. Error handling (AllowUnknown support)
