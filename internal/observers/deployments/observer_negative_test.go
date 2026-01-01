package deployments

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/internal/base"
	"github.com/yairfalse/tapio/pkg/intelligence"
	"k8s.io/client-go/kubernetes/fake"
)

// TDD: Negative tests for error handling

func TestDeploymentsObserver_HandlesNilEmitter(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	deps := base.NewDeps(nil, nil) // Nil emitter

	config := Config{
		Clientset: clientset,
		Namespace: "default",
	}

	observer, err := New(config, deps)
	require.NoError(t, err)

	// Simulate deployment creation with nil emitter
	deploy := createDeployment("app", 3, 3)
	observer.handleAdd(deploy)

	// Should not crash - emitEvent handles nil emitter gracefully
}

func TestDeploymentsObserver_HandlesEmitterError(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := intelligence.NewMock()
	emitter.SetEmitError(errors.New("emit failed"))
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

	// Should handle error gracefully (logs but doesn't crash)
	// Emitter received the event attempt even though it failed
	assert.Len(t, emitter.Events(), 0, "Failed events should not be in mock")
}

func TestDeploymentsObserver_HandlesInvalidObjectType(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := intelligence.NewMock()
	deps := base.NewDeps(nil, emitter)

	config := Config{
		Clientset: clientset,
		Namespace: "default",
	}

	observer, err := New(config, deps)
	require.NoError(t, err)

	// Pass invalid object type (not *appsv1.Deployment)
	observer.handleAdd("not-a-deployment")

	// Should not emit event for invalid type
	assert.Len(t, emitter.Events(), 0, "Should not emit event for invalid object type")
}

func TestDeploymentsObserver_HandlesInvalidUpdateObjects(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := intelligence.NewMock()
	deps := base.NewDeps(nil, emitter)

	config := Config{
		Clientset: clientset,
		Namespace: "default",
	}

	observer, err := New(config, deps)
	require.NoError(t, err)

	// Pass invalid old object
	deploy := createDeployment("app", 3, 3)
	observer.handleUpdate("not-a-deployment", deploy)
	assert.Len(t, emitter.Events(), 0, "Should not emit event with invalid old object")

	// Pass invalid new object
	observer.handleUpdate(deploy, "not-a-deployment")
	assert.Len(t, emitter.Events(), 0, "Should not emit event with invalid new object")
}

func TestDeploymentsObserver_HandlesInvalidDeleteObject(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := intelligence.NewMock()
	deps := base.NewDeps(nil, emitter)

	config := Config{
		Clientset: clientset,
		Namespace: "default",
	}

	observer, err := New(config, deps)
	require.NoError(t, err)

	// Pass invalid object type
	observer.handleDelete("not-a-deployment")

	// Should not emit event for invalid type
	assert.Len(t, emitter.Events(), 0, "Should not emit event for invalid object type")
}
