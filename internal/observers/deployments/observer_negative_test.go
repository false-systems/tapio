package deployments

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"k8s.io/client-go/kubernetes/fake"
)

// TDD: Negative tests for error handling

func TestDeploymentsObserver_HandlesNilEmitter(t *testing.T) {
	clientset := fake.NewSimpleClientset()

	config := Config{
		Clientset: clientset,
		Namespace: "default",
		Emitter:   nil, // Nil emitter should not crash
	}

	observer, err := NewDeploymentsObserver("deployments", config)
	require.NoError(t, err)

	// Simulate deployment creation with nil emitter
	deploy := createDeployment("app", 3, 3)
	observer.handleAdd(deploy)

	// Should not crash, emitEvent handles nil emitter gracefully
	stats := observer.Stats()
	assert.Equal(t, int64(0), stats.EventsProcessed, "No events should be processed with nil emitter")
}

func TestDeploymentsObserver_HandlesEmitterError(t *testing.T) {
	clientset := fake.NewSimpleClientset()

	// Create emitter that fails
	emitter := &failingEmitter{err: errors.New("emit failed")}

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

	// Should handle error gracefully
	stats := observer.Stats()
	assert.Equal(t, int64(1), stats.EventsProcessed, "Event should be recorded")
	assert.Equal(t, int64(1), stats.ErrorsTotal, "Error should be recorded")
}

func TestDeploymentsObserver_HandlesInvalidObjectType(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := &captureEmitter{events: make([]*capturedEvent, 0)}

	config := Config{
		Clientset: clientset,
		Namespace: "default",
		Emitter:   emitter,
	}

	observer, err := NewDeploymentsObserver("deployments", config)
	require.NoError(t, err)

	// Pass invalid object type (not *appsv1.Deployment)
	observer.handleAdd("not-a-deployment")

	// Should not emit event for invalid type
	assert.Len(t, emitter.events, 0, "Should not emit event for invalid object type")
}

func TestDeploymentsObserver_HandlesInvalidUpdateObjects(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := &captureEmitter{events: make([]*capturedEvent, 0)}

	config := Config{
		Clientset: clientset,
		Namespace: "default",
		Emitter:   emitter,
	}

	observer, err := NewDeploymentsObserver("deployments", config)
	require.NoError(t, err)

	// Pass invalid old object
	deploy := createDeployment("app", 3, 3)
	observer.handleUpdate("not-a-deployment", deploy)
	assert.Len(t, emitter.events, 0, "Should not emit event with invalid old object")

	// Pass invalid new object
	observer.handleUpdate(deploy, "not-a-deployment")
	assert.Len(t, emitter.events, 0, "Should not emit event with invalid new object")
}

func TestDeploymentsObserver_HandlesInvalidDeleteObject(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := &captureEmitter{events: make([]*capturedEvent, 0)}

	config := Config{
		Clientset: clientset,
		Namespace: "default",
		Emitter:   emitter,
	}

	observer, err := NewDeploymentsObserver("deployments", config)
	require.NoError(t, err)

	// Pass invalid object type
	observer.handleDelete("not-a-deployment")

	// Should not emit event for invalid type
	assert.Len(t, emitter.events, 0, "Should not emit event for invalid object type")
}

// Helper emitter that always fails
type failingEmitter struct {
	err error
}

func (e *failingEmitter) Emit(ctx context.Context, event *domain.ObserverEvent) error {
	return e.err
}

func (e *failingEmitter) Close() error {
	return nil
}
