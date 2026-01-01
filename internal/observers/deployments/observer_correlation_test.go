package deployments

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/internal/base"
	"github.com/yairfalse/tapio/pkg/intelligence"
	"k8s.io/client-go/kubernetes/fake"
)

// TDD: Event emission tests

func TestDeploymentsObserver_EmitsEventOnUpdate(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := intelligence.NewMock()
	deps := base.NewDeps(nil, emitter)

	config := Config{
		Clientset: clientset,
		Namespace: "default",
	}

	observer, err := New(config, deps)
	require.NoError(t, err)

	// Simulate deployment update
	old := createDeployment("app", 1, 1)
	new := createDeployment("app", 1, 1)
	observer.handleUpdate(old, new)

	// Verify event was emitted
	require.Len(t, emitter.Events(), 1, "Should emit event for update")
}

func TestDeploymentsObserver_EmitsReplicaChangeEvent(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := intelligence.NewMock()
	deps := base.NewDeps(nil, emitter)

	config := Config{
		Clientset: clientset,
		Namespace: "default",
	}

	observer, err := New(config, deps)
	require.NoError(t, err)

	// Simulate replica change
	old := createDeployment("app", 1, 1)
	new := createDeployment("app", 5, 5)
	observer.handleUpdate(old, new)

	// Verify event was processed (replica change detected)
	require.Len(t, emitter.Events(), 1, "Should emit event for replica change")
	assert.Equal(t, "deployment_scaled", emitter.Events()[0].Type)
}

func TestDeploymentsObserver_EmitsConditionChangeEvent(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := intelligence.NewMock()
	deps := base.NewDeps(nil, emitter)

	config := Config{
		Clientset: clientset,
		Namespace: "default",
	}

	observer, err := New(config, deps)
	require.NoError(t, err)

	// Simulate condition change
	old := createDeploymentWithCondition("app", "Available", "False")
	new := createDeploymentWithCondition("app", "Available", "True")
	observer.handleUpdate(old, new)

	// Verify event was processed (condition change detected)
	require.Len(t, emitter.Events(), 1, "Should emit event for condition change")
	assert.Equal(t, "deployment_available", emitter.Events()[0].Type)
}

func TestDeploymentsObserver_EmitsEventOnAdd(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := intelligence.NewMock()
	deps := base.NewDeps(nil, emitter)

	config := Config{
		Clientset: clientset,
		Namespace: "default",
	}

	observer, err := New(config, deps)
	require.NoError(t, err)

	// Simulate deployment creation
	deploy := createDeployment("app", 3, 3)
	observer.handleAdd(deploy)

	// Verify event was processed
	require.Len(t, emitter.Events(), 1, "Should emit event for creation")
	assert.Equal(t, "deployment_created", emitter.Events()[0].Type)
}

func TestDeploymentsObserver_EmitsEventOnDelete(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := intelligence.NewMock()
	deps := base.NewDeps(nil, emitter)

	config := Config{
		Clientset: clientset,
		Namespace: "default",
	}

	observer, err := New(config, deps)
	require.NoError(t, err)

	// Simulate deployment deletion
	deploy := createDeployment("app", 3, 3)
	observer.handleDelete(deploy)

	// Verify event was processed
	require.Len(t, emitter.Events(), 1, "Should emit event for deletion")
	assert.Equal(t, "deployment_deleted", emitter.Events()[0].Type)
}

func TestDeploymentsObserver_EmitsImageUpdateEvent(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := intelligence.NewMock()
	deps := base.NewDeps(nil, emitter)

	config := Config{
		Clientset: clientset,
		Namespace: "default",
	}

	observer, err := New(config, deps)
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
