package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPodContext_Structure(t *testing.T) {
	// Test that PodContext can be created with all required fields
	ctx := PodContext{
		Name:      "test-pod",
		Namespace: "default",
		UID:       "pod-123",
		PodIP:     "10.0.1.42",
		NodeName:  "node-1",
		OwnerKind: "Deployment",
		OwnerName: "test-app",
		OTELAttributes: map[string]string{
			"service.name":       "test-service",
			"k8s.pod.name":       "test-pod",
			"k8s.namespace.name": "default",
		},
	}

	assert.Equal(t, "test-pod", ctx.Name)
	assert.Equal(t, "default", ctx.Namespace)
	assert.Equal(t, "pod-123", ctx.UID)
	assert.Equal(t, "10.0.1.42", ctx.PodIP)
	assert.Equal(t, "node-1", ctx.NodeName)
	assert.Equal(t, "Deployment", ctx.OwnerKind)
	assert.Equal(t, "test-app", ctx.OwnerName)
	assert.NotNil(t, ctx.OTELAttributes)
	assert.Equal(t, "test-service", ctx.OTELAttributes["service.name"])
}

func TestPodContext_EmptyOTELAttributes(t *testing.T) {
	// Test that PodContext works with empty OTEL attributes
	ctx := PodContext{
		Name:           "test-pod",
		Namespace:      "default",
		OTELAttributes: map[string]string{},
	}

	assert.NotNil(t, ctx.OTELAttributes)
	assert.Empty(t, ctx.OTELAttributes)
}

func TestPodContext_NilOTELAttributes(t *testing.T) {
	// Test that PodContext works with nil OTEL attributes
	ctx := PodContext{
		Name:      "test-pod",
		Namespace: "default",
	}

	// Should be nil (not initialized)
	assert.Nil(t, ctx.OTELAttributes)
}
