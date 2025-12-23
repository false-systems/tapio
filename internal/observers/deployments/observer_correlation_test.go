package deployments

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/intelligence"
	"k8s.io/client-go/kubernetes/fake"
)

// TDD: Metrics increment tests

func TestDeploymentsObserver_IncrementsMetricsOnUpdate(t *testing.T) {
	clientset := fake.NewSimpleClientset()

	// Create emitter that captures events
	emitter := intelligence.NewMock()

	config := Config{
		Clientset: clientset,
		Namespace: "default",
		Emitter:   emitter,
	}

	observer, err := NewDeploymentsObserver("deployments", config)
	require.NoError(t, err)

	// Simulate deployment update
	old := createDeployment("app", 1, 1)
	new := createDeployment("app", 1, 1)
	observer.handleUpdate(old, new)

	// Verify metrics were incremented
	stats := observer.Stats()
	assert.Equal(t, int64(1), stats.EventsProcessed, "deploymentUpdates metric should increment")
}

func TestDeploymentsObserver_IncrementsReplicaChangeMetric(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := intelligence.NewMock()

	config := Config{
		Clientset: clientset,
		Namespace: "default",
		Emitter:   emitter,
	}

	observer, err := NewDeploymentsObserver("deployments", config)
	require.NoError(t, err)

	// Simulate replica change
	old := createDeployment("app", 1, 1)
	new := createDeployment("app", 5, 5)
	observer.handleUpdate(old, new)

	// Verify event was processed (replica change detected)
	require.Len(t, emitter.Events(), 1, "Should emit event for replica change")
	assert.Equal(t, "deployment_scaled", emitter.Events()[0].Type)
}

func TestDeploymentsObserver_IncrementsConditionChangeMetric(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := intelligence.NewMock()

	config := Config{
		Clientset: clientset,
		Namespace: "default",
		Emitter:   emitter,
	}

	observer, err := NewDeploymentsObserver("deployments", config)
	require.NoError(t, err)

	// Simulate condition change
	old := createDeploymentWithCondition("app", "Available", "False")
	new := createDeploymentWithCondition("app", "Available", "True")
	observer.handleUpdate(old, new)

	// Verify event was processed (condition change detected)
	require.Len(t, emitter.Events(), 1, "Should emit event for condition change")
	assert.Equal(t, "deployment_available", emitter.Events()[0].Type)
}

func TestDeploymentsObserver_IncrementsMetricsOnAdd(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := intelligence.NewMock()

	config := Config{
		Clientset: clientset,
		Namespace: "default",
		Emitter:   emitter,
	}

	observer, err := NewDeploymentsObserver("deployments", config)
	require.NoError(t, err)

	// Simulate deployment creation
	deploy := createDeployment("app", 3, 3)
	observer.handleAdd(deploy)

	// Verify event was processed
	require.Len(t, emitter.Events(), 1, "Should emit event for creation")
	assert.Equal(t, "deployment_created", emitter.Events()[0].Type)
}

func TestDeploymentsObserver_IncrementsMetricsOnDelete(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := intelligence.NewMock()

	config := Config{
		Clientset: clientset,
		Namespace: "default",
		Emitter:   emitter,
	}

	observer, err := NewDeploymentsObserver("deployments", config)
	require.NoError(t, err)

	// Simulate deployment deletion
	deploy := createDeployment("app", 3, 3)
	observer.handleDelete(deploy)

	// Verify event was processed
	require.Len(t, emitter.Events(), 1, "Should emit event for deletion")
	assert.Equal(t, "deployment_deleted", emitter.Events()[0].Type)
}

func TestDeploymentsObserver_IncrementsImageUpdateMetric(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := intelligence.NewMock()

	config := Config{
		Clientset: clientset,
		Namespace: "default",
		Emitter:   emitter,
	}

	observer, err := NewDeploymentsObserver("deployments", config)
	require.NoError(t, err)

	// Simulate image change
	old := createDeploymentWithImage("app", "myapp:v1.0.0")
	new := createDeploymentWithImage("app", "myapp:v2.0.0")
	observer.handleUpdate(old, new)

	// Verify event was processed (image change detected)
	require.Len(t, emitter.Events(), 1, "Should emit event for image change")
	assert.Equal(t, "deployment_image_updated", emitter.Events()[0].Type)
	assert.True(t, emitter.Events()[0].K8sData.ImageChanged)
	assert.Equal(t, "myapp:v1.0.0", emitter.Events()[0].K8sData.OldImage)
	assert.Equal(t, "myapp:v2.0.0", emitter.Events()[0].K8sData.NewImage)
}
